package rules

import "go/ast"

// Generic path/node navigation helpers shared across rule files. Helpers
// specific to one concern (pointer escapes, sync.Pool detection, string
// conversions, range loops, alloc calls) live in their own
// <concern>_helpers.go file instead of growing this one.

func findAssign(path []ast.Node) *ast.AssignStmt {
	for _, n := range path {
		if a, ok := n.(*ast.AssignStmt); ok {
			return a
		}
	}
	return nil
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
