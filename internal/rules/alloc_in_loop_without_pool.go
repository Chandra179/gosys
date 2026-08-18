package rules

import (
	"fmt"
	"go/ast"
	"strings"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
)

// AllocInLoopWithoutPool flags make([]byte, ...) or json.Unmarshal calls
// that sit inside a for/range loop, in a file with no visible sync.Pool
// usage. Per-iteration allocation of short-lived buffers is a classic
// GC-pressure source that sync.Pool exists specifically to fix.
func AllocInLoopWithoutPool(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding {
	call := findAllocCall(path)
	if call == nil {
		return nil
	}
	if !insideLoop(path) {
		return nil
	}
	fn := findEnclosingFunc(path)
	if fn == nil {
		return nil
	}
	// Checked at file scope, not just the enclosing function: the common
	// pooling pattern declares `var p = sync.Pool{...}` once at package
	// level and only calls p.Get()/p.Put() inside the hot function, so a
	// function-body-only check would false-positive on already-pooled code.
	// AST-based (fileUsesSyncPool), not a source-text search, so a comment
	// that merely mentions "sync.Pool" can't suppress a real finding.
	if file := findFile(path); file != nil && fileUsesSyncPool(file) {
		return nil // pool usage present somewhere in the file; don't flag
	}
	callSrc, _ := idx.NodeSource(call)
	return &Finding{
		Site:    site,
		Pattern: "alloc-in-loop-without-pool",
		Message: fmt.Sprintf(
			"line %d allocates inside a loop with no sync.Pool visible in the enclosing function %q. "+
				"Per-iteration allocation of short-lived buffers is a common source of GC pressure at high throughput; "+
				"consider reusing a buffer via sync.Pool if this loop runs frequently.",
			site.Line, funcName(fn)),
		Source: strings.TrimSpace(callSrc),
	}
}
