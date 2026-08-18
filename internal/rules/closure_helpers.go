package rules

import (
	"go/ast"
	"go/token"
	"slices"
)

// closureEscape describes an escaping closure found in a function body: the
// literal itself, the statement responsible for the escape, and why.
type closureEscape struct {
	lit    *ast.FuncLit
	stmt   ast.Node // *ast.ReturnStmt, *ast.AssignStmt, or *ast.GoStmt
	reason string
}

// findEscapingCapturingClosure looks for a closure literal in fn.Body that
// escapes fn's own stack frame, in one of three shapes:
//
//   - `return func() {...}`, or the single-hop indirect `f := func(){...};
//     ...; return f`
//   - `go func() {...}()`, or the indirect `f := func(){...}; ...; go f()`
//   - assigned (directly or via append/etc.) into a package-level variable:
//     `Sink = append(Sink, f)`
//
// This is a structural, whole-function search rather than one anchored to
// the pprof hot line: empirically the Go compiler attributes the escaping
// allocation to different lines depending on the surrounding code shape
// (the closure's own declaration line when declared inside a loop; the
// escape statement's line otherwise) -- see the doc comment on
// ClosureCapture. The caller matches this result against the hot site
// afterward.
func findEscapingCapturingClosure(fn *ast.FuncDecl, file *ast.File) *closureEscape {
	funcLitVars := map[string]*ast.FuncLit{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		id, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		if fl, ok := assign.Rhs[0].(*ast.FuncLit); ok {
			funcLitVars[id.Name] = fl
		}
		return true
	})

	pkgVars := packageLevelVarNames(file)

	var result *closureEscape
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if result != nil {
			return false
		}
		switch v := n.(type) {
		case *ast.GoStmt:
			if lit := funcLitOf(v.Call.Fun, funcLitVars); lit != nil {
				result = &closureEscape{lit: lit, stmt: v, reason: "launched as a goroutine"}
				return false
			}
		case *ast.ReturnStmt:
			for _, r := range v.Results {
				if lit := funcLitOf(r, funcLitVars); lit != nil {
					result = &closureEscape{lit: lit, stmt: v, reason: "returned from the function"}
					return false
				}
			}
		case *ast.AssignStmt:
			if v.Tok == token.DEFINE || len(v.Lhs) == 0 {
				return true
			}
			lhsID, ok := v.Lhs[0].(*ast.Ident)
			if !ok || !pkgVars[lhsID.Name] {
				return true
			}
			for _, rhs := range v.Rhs {
				if lit := funcLitReferencedIn(rhs, funcLitVars); lit != nil {
					result = &closureEscape{lit: lit, stmt: v, reason: "stored into a package-level variable"}
					return false
				}
			}
		}
		return true
	})
	return result
}

// funcLitOf returns the *ast.FuncLit expr refers to: either expr is the
// literal itself, or expr is an identifier previously assigned one.
func funcLitOf(expr ast.Expr, vars map[string]*ast.FuncLit) *ast.FuncLit {
	if fl, ok := expr.(*ast.FuncLit); ok {
		return fl
	}
	if id, ok := expr.(*ast.Ident); ok {
		return vars[id.Name]
	}
	return nil
}

// funcLitReferencedIn reports whether expr's subtree references an
// identifier bound to a closure literal in vars (e.g. `f` inside
// `append(Sink, f)`), returning that literal.
func funcLitReferencedIn(expr ast.Expr, vars map[string]*ast.FuncLit) *ast.FuncLit {
	if lit := funcLitOf(expr, vars); lit != nil {
		return lit
	}
	var found *ast.FuncLit
	ast.Inspect(expr, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if fl, ok := vars[id.Name]; ok {
				found = fl
			}
		}
		return true
	})
	return found
}

// packageLevelVarNames returns the names declared by top-level `var ...`
// declarations in file.
func packageLevelVarNames(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, id := range vs.Names {
				names[id.Name] = true
			}
		}
	}
	return names
}

// findDeclStmt locates the statement in fn.Body that declares name, either
// as a `:=` short assignment or a `var` declaration, without descending
// into any nested closure. Empirically, a captured variable's own
// declaration line -- not the closure literal's line or the escape
// statement's line -- is where pprof attributes the dominant share of the
// allocation when the captured value is large (see ClosureCapture's doc
// comment), so this is a third valid anchor for the hot site.
func findDeclStmt(fn *ast.FuncDecl, name string) ast.Node {
	var found ast.Node
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch v := n.(type) {
		case *ast.AssignStmt:
			if v.Tok != token.DEFINE {
				return true
			}
			for _, lhs := range v.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
					found = v
				}
			}
		case *ast.DeclStmt:
			gd, ok := v.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				return true
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, id := range vs.Names {
					if id.Name == name {
						found = v
					}
				}
			}
		}
		return true
	})
	return found
}

// nodeInPath reports whether target appears (by identity) anywhere in
// path.
func nodeInPath(path []ast.Node, target ast.Node) bool {
	return slices.Contains(path, target)
}

// capturesOuterVariable reports whether lit references an identifier
// declared in fn's own scope (a parameter or a `:=` local), other than one
// declared inside lit itself. Go closures capture by reference, so any
// such identifier is exactly what escape analysis is forced to move to the
// heap once lit escapes fn's stack frame.
func capturesOuterVariable(fn *ast.FuncDecl, lit *ast.FuncLit) (string, bool) {
	outer := outerScopeNames(fn)
	inner := innerScopeNames(lit)

	var captured string
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok || !outer[id.Name] || inner[id.Name] {
			return true
		}
		captured = id.Name
		found = true
		return false
	})
	return captured, found
}

// outerScopeNames collects fn's parameter names plus every `:=`-declared
// local in fn.Body, without descending into any nested closure (those
// names belong to the closure's own scope, not fn's).
func outerScopeNames(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, id := range field.Names {
				names[id.Name] = true
			}
		}
	}
	if fn.Body != nil {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			collectDeclaredNames(n, names)
			return true
		})
	}
	return names
}

// collectDeclaredNames adds any names n declares -- a `:=` short
// assignment or a `var` declaration -- to names.
func collectDeclaredNames(n ast.Node, names map[string]bool) {
	switch v := n.(type) {
	case *ast.AssignStmt:
		if v.Tok != token.DEFINE {
			return
		}
		for _, lhs := range v.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				names[id.Name] = true
			}
		}
	case *ast.GenDecl:
		if v.Tok != token.VAR {
			return
		}
		for _, spec := range v.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, id := range vs.Names {
				names[id.Name] = true
			}
		}
	}
}

// innerScopeNames collects lit's own parameter, result, and `:=`-declared
// local names, so capturesOuterVariable doesn't mistake a shadowing
// identifier for a captured one.
func innerScopeNames(lit *ast.FuncLit) map[string]bool {
	names := map[string]bool{}
	if lit.Type.Params != nil {
		for _, field := range lit.Type.Params.List {
			for _, id := range field.Names {
				names[id.Name] = true
			}
		}
	}
	if lit.Type.Results != nil {
		for _, field := range lit.Type.Results.List {
			for _, id := range field.Names {
				names[id.Name] = true
			}
		}
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		collectDeclaredNames(n, names)
		return true
	})
	return names
}
