package rules

import (
	"fmt"
	"go/ast"
	"strings"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
)

// SliceIntoStructField flags `s.field = buf[x:y]`: a slice expression
// assigned directly into a struct field. The field keeps the backing array
// of buf alive for as long as the struct lives, even if buf itself is huge
// and only a small window of it was ever needed.
func SliceIntoStructField(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding {
	assign := findAssign(path)
	if assign == nil {
		return nil
	}
	for i, lhs := range assign.Lhs {
		if i >= len(assign.Rhs) {
			break
		}
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if _, ok := assign.Rhs[i].(*ast.SliceExpr); !ok {
			continue
		}
		src, _ := idx.NodeSource(assign)
		return &Finding{
			Site:    site,
			Pattern: "slice-into-struct-field",
			Message: fmt.Sprintf(
				"line %d assigns a slice expression into struct field %q. "+
					"The field keeps the whole backing array alive for as long as the struct lives — "+
					"if the source slice is large and long-lived, this can pin far more memory than the field needs. "+
					"Consider copying just the needed bytes instead of re-slicing.",
				site.Line, sel.Sel.Name),
			Source: strings.TrimSpace(src),
		}
	}
	return nil
}
