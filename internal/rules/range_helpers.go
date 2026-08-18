package rules

import (
	"go/ast"
	"go/token"
)

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
