package rules

import (
	"fmt"
	"go/ast"
	"strings"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
)

// PointerEscapeReturn flags a composite literal whose address is taken and
// returned from the enclosing function: `return &Struct{...}` directly, or
// the single-hop indirect form `v := &Struct{...}; ...; return v`. This is
// the textbook escape-analysis case documented in the Go compiler itself
// (cmd/compile/internal/escape): a pointer to a stack-allocated object
// can't outlive the stack frame it points into, so once the address
// crosses the function's return boundary the compiler is forced to move
// the object to the heap. A hot pprof site at a line shaped like this is
// exactly that per-call escape, not a leak — just an allocation forced by
// the function's own signature.
func PointerEscapeReturn(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding {
	fn := findEnclosingFunc(path)
	if fn == nil || fn.Body == nil {
		return nil
	}

	expr, ok := escapingReturnedPointer(path, fn)
	if !ok {
		return nil
	}

	src, _ := idx.NodeSource(expr)
	return &Finding{
		Site:    site,
		Pattern: "pointer-escape-return",
		Message: fmt.Sprintf(
			"line %d takes the address of a composite literal (%s) that is returned from %s, "+
				"so escape analysis moves it to the heap on every call: a pointer to a stack frame "+
				"can't outlive that frame. If callers only read the result, consider returning the "+
				"value type instead of a pointer, or writing into a caller-supplied buffer.",
			site.Line, strings.TrimSpace(src), funcName(fn)),
		Source: strings.TrimSpace(src),
	}
}
