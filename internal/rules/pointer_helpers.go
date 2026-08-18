package rules

import (
	"go/ast"
	"go/token"
)

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
