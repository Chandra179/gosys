package rules

import (
	"fmt"
	"go/ast"
	"strings"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
)

// MapPointerGrowth flags `m[key] = &T{...}` (or an existing pointer)
// assignments into a map. Maps of pointers never shrink their bucket
// memory as entries are deleted, and each live pointer keeps its pointee
// on the heap — a common source of unbounded growth in long-lived caches.
func MapPointerGrowth(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding {
	assign := findAssign(path)
	if assign == nil {
		return nil
	}
	fn := findEnclosingFunc(path)
	for i, lhs := range assign.Lhs {
		if i >= len(assign.Rhs) {
			break
		}
		idxExpr, ok := lhs.(*ast.IndexExpr)
		if !ok {
			continue
		}
		if !isPointerProducing(assign.Rhs[i], fn) {
			continue
		}
		src, _ := idx.NodeSource(assign)
		mapName, _ := idx.NodeSource(idxExpr.X)
		return &Finding{
			Site:    site,
			Pattern: "map-pointer-growth",
			Message: fmt.Sprintf(
				"line %d stores a pointer into map %q. "+
					"map[K]*V retains every entry's pointee on the heap and never releases bucket memory as entries are removed. "+
					"If this map is long-lived, confirm entries are actively evicted (TTL, LRU, explicit delete) rather than only appended to.",
				site.Line, strings.TrimSpace(mapName)),
			Source: strings.TrimSpace(src),
		}
	}
	return nil
}
