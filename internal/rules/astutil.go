package rules

import (
	"go/ast"
	"go/token"
	"go/types"
)

func findAssign(path []ast.Node) *ast.AssignStmt {
	for _, n := range path {
		if a, ok := n.(*ast.AssignStmt); ok {
			return a
		}
	}
	return nil
}

// findAllocCall locates the allocation call (make([]byte, ...) or
// json.Unmarshal) responsible for the hot line. PathEnclosingInterval's
// path holds ancestors of the line's interval, but pprof attributes the
// allocation to a CallExpr that is typically a *descendant* of the
// innermost enclosing statement (e.g. the make(...) inside
// `buf := make([]byte, n)`), so this checks ancestors first, then walks
// into the innermost statement's subtree.
func findAllocCall(path []ast.Node) *ast.CallExpr {
	for _, n := range path {
		if c, ok := n.(*ast.CallExpr); ok && isAllocCall(c) {
			return c
		}
	}
	if len(path) == 0 {
		return nil
	}
	var found *ast.CallExpr
	ast.Inspect(path[0], func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok && isAllocCall(c) {
			found = c
			return false
		}
		return true
	})
	return found
}

func findEnclosingFunc(path []ast.Node) *ast.FuncDecl {
	for _, n := range path {
		if fn, ok := n.(*ast.FuncDecl); ok {
			return fn
		}
	}
	return nil
}

// findFile returns the *ast.File the path was resolved in.
// astutil.PathEnclosingInterval always includes it as the outermost
// (last) element of the path.
func findFile(path []ast.Node) *ast.File {
	if len(path) == 0 {
		return nil
	}
	if f, ok := path[len(path)-1].(*ast.File); ok {
		return f
	}
	return nil
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Name != nil {
		return fn.Name.Name
	}
	return "<anonymous>"
}

func insideLoop(path []ast.Node) bool {
	for _, n := range path {
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			return true
		}
	}
	return false
}

// isPointerProducing reports whether expr looks like it produces a pointer:
// &x, a call to new(...), or an identifier that was itself assigned one of
// those earlier in fn (e.g. `p := &Value{}` followed by `m[k] = p`). fn may
// be nil, in which case only the direct &x/new(...) forms are recognized.
func isPointerProducing(expr ast.Expr, fn *ast.FuncDecl) bool {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		return e.Op == token.AND
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok {
			return id.Name == "new"
		}
	case *ast.Ident:
		return fn != nil && identAssignedPointer(fn, e.Name)
	}
	return false
}

// identAssignedPointer reports whether name is assigned a &x or new(...)
// value somewhere in fn's body.
func identAssignedPointer(fn *ast.FuncDecl, name string) bool {
	if fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != name || i >= len(assign.Rhs) {
				continue
			}
			// Only check the direct &x/new(...) forms here, not another
			// level of identifier indirection, to keep this a bounded,
			// single-hop trace rather than a full dataflow analysis.
			switch rhs := assign.Rhs[i].(type) {
			case *ast.UnaryExpr:
				found = rhs.Op == token.AND
			case *ast.CallExpr:
				if fid, ok := rhs.Fun.(*ast.Ident); ok && fid.Name == "new" {
					found = true
				}
			}
		}
		return true
	})
	return found
}

// fileUsesSyncPool reports whether file references sync.Pool anywhere in
// its syntax tree (declarations, composite literals, field types, etc.).
// AST-based rather than a raw substring search over the file's source, so
// a comment that merely mentions "sync.Pool" doesn't count.
func fileUsesSyncPool(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "sync" && sel.Sel.Name == "Pool" {
			found = true
			return false
		}
		return true
	})
	return found
}

// findStringConv locates a string(x) conversion call responsible for the
// hot line, the same way findAllocCall locates make(...)/json.Unmarshal:
// check ancestors first, then walk into the innermost statement's subtree.
func findStringConv(path []ast.Node) *ast.CallExpr {
	for _, n := range path {
		if c, ok := n.(*ast.CallExpr); ok && isStringConv(c) {
			return c
		}
	}
	if len(path) == 0 {
		return nil
	}
	var found *ast.CallExpr
	ast.Inspect(path[0], func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok && isStringConv(c) {
			found = c
			return false
		}
		return true
	})
	return found
}

func isStringConv(call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "string" && len(call.Args) == 1
}

// isByteSlice reports whether t is a []byte (or a named type whose
// underlying type is []byte).
func isByteSlice(t types.Type) bool {
	if t == nil {
		return false
	}
	slice, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}
	basic, ok := slice.Elem().Underlying().(*types.Basic)
	return ok && basic.Kind() == types.Uint8
}

// findEnclosingRange returns the innermost *ast.RangeStmt in path, if any.
func findEnclosingRange(path []ast.Node) *ast.RangeStmt {
	for _, n := range path {
		if r, ok := n.(*ast.RangeStmt); ok {
			return r
		}
	}
	return nil
}

// rangeValueAddressTaken reports whether rs's per-iteration value variable
// has its address taken (&v) anywhere in the loop body, and if so returns
// the value's name and the &v expression.
func rangeValueAddressTaken(rs *ast.RangeStmt) (name string, expr *ast.UnaryExpr, found bool) {
	val, ok := rs.Value.(*ast.Ident)
	if !ok || val.Name == "_" {
		return "", nil, false
	}
	ast.Inspect(rs.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		u, ok := n.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		if id, ok := u.X.(*ast.Ident); ok && id.Name == val.Name {
			expr = u
			found = true
			return false
		}
		return true
	})
	return val.Name, expr, found
}

// findEnclosingReturn returns the innermost *ast.ReturnStmt in path, if any.
func findEnclosingReturn(path []ast.Node) *ast.ReturnStmt {
	for _, n := range path {
		if r, ok := n.(*ast.ReturnStmt); ok {
			return r
		}
	}
	return nil
}

// identReturned reports whether name is one of the results of some return
// statement in fn's body (single-hop trace, same bound as
// identAssignedPointer: it doesn't follow the value through further
// indirection).
func identReturned(fn *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		rs, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range rs.Results {
			if id, ok := r.(*ast.Ident); ok && id.Name == name {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// escapingReturnedPointer looks for a composite literal whose address
// escapes the function via return, in one of two shapes:
//
//  1. Direct: the hot line is itself `return &Struct{...}` (or one of
//     several results in a multi-value return).
//  2. Indirect: the hot line is `v := &Struct{...}`, and v is returned
//     later in the same function body.
//
// Returns the &Struct{...} expression responsible for the escape.
func escapingReturnedPointer(path []ast.Node, fn *ast.FuncDecl) (ast.Expr, bool) {
	if rs := findEnclosingReturn(path); rs != nil {
		for _, r := range rs.Results {
			if u, ok := r.(*ast.UnaryExpr); ok && u.Op == token.AND {
				if _, isLit := u.X.(*ast.CompositeLit); isLit {
					return u, true
				}
			}
		}
	}

	assign := findAssign(path)
	if assign == nil {
		return nil, false
	}
	for i, lhs := range assign.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || i >= len(assign.Rhs) {
			continue
		}
		u, ok := assign.Rhs[i].(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			continue
		}
		if _, isLit := u.X.(*ast.CompositeLit); !isLit {
			continue
		}
		if identReturned(fn, id.Name) {
			return u, true
		}
	}
	return nil, false
}

// isAllocCall reports whether call is make([]byte, ...) or a call to
// json.Unmarshal.
func isAllocCall(call *ast.CallExpr) bool {
	if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "make" {
		if len(call.Args) > 0 {
			if arr, ok := call.Args[0].(*ast.ArrayType); ok {
				if elt, ok := arr.Elt.(*ast.Ident); ok && elt.Name == "byte" {
					return true
				}
			}
		}
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "json" && sel.Sel.Name == "Unmarshal" {
			return true
		}
	}
	return false
}
