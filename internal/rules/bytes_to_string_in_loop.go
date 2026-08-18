package rules

import (
	"fmt"
	"go/ast"
	"strings"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
)

// BytesToStringInLoop flags `string(b)`, where b is a []byte, sitting
// inside a for/range loop. Converting a byte slice to a string always
// copies its backing array (runtime.slicebytetostring); doing that once
// per iteration in a hot loop — e.g. re-converting the same bytes just to
// compare or key a lookup with them — is a repeated, avoidable copy.
func BytesToStringInLoop(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding {
	call := findStringConv(path)
	if call == nil {
		return nil
	}
	if !insideLoop(path) {
		return nil
	}
	file := findFile(path)
	if file == nil {
		return nil
	}
	if !isByteSlice(idx.TypeOf(file, call.Args[0])) {
		return nil // string(x) where x isn't a []byte (e.g. a rune) isn't this pattern
	}
	src, _ := idx.NodeSource(call)
	return &Finding{
		Site:    site,
		Pattern: "bytes-to-string-in-loop",
		Message: fmt.Sprintf(
			"line %d converts a []byte to string inside a loop. "+
				"Each conversion copies the byte slice's backing array; if the same bytes are "+
				"converted every iteration, consider converting once outside the loop, or comparing "+
				"the []byte directly with bytes.Equal instead.",
			site.Line),
		Source: strings.TrimSpace(src),
	}
}
