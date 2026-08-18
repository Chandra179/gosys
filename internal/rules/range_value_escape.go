package rules

import (
	"fmt"
	"go/ast"
	"strings"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
)

// RangeValueEscape flags `for _, v := range xs { ... &v ... }`: taking the
// address of a range loop's per-iteration value variable. Since Go 1.22,
// each iteration gets its own copy of v, so &v escapes a fresh copy of it
// to the heap on every iteration it's taken — a hot pprof site inside a
// loop shaped like this is exactly that per-iteration escape.
func RangeValueEscape(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding {
	rs := findEnclosingRange(path)
	if rs == nil {
		return nil
	}
	name, expr, ok := rangeValueAddressTaken(rs)
	if !ok {
		return nil
	}
	src, _ := idx.NodeSource(expr)
	return &Finding{
		Site:    site,
		Pattern: "range-value-escape",
		Message: fmt.Sprintf(
			"line %d is a range loop whose value %q has its address taken (%s) in the body. "+
				"Each iteration gets its own copy of the value, so this escapes a fresh copy to the heap every time. "+
				"If you need a stable pointer per element, index the slice directly (&xs[i]) instead of the range value.",
			site.Line, name, strings.TrimSpace(src)),
		Source: strings.TrimSpace(src),
	}
}
