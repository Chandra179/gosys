package rules

import (
	"go/ast"
	"go/types"
)

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
