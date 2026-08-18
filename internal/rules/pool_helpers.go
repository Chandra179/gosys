package rules

import "go/ast"

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
