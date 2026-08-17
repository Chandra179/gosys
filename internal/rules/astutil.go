package rules

import (
	"go/ast"
	"go/token"
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

// isPointerProducing reports whether expr syntactically looks like it
// produces a pointer: &x, or a call to new(...).
func isPointerProducing(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		return e.Op == token.AND
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok {
			return id.Name == "new"
		}
	}
	return false
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
