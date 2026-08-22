package rules

import (
	"fmt"
	"go/ast"
	"strings"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
)

// ClosureCapture flags a closure that captures a variable from its
// enclosing function's scope and escapes that function (by being
// returned, launched with `go`, or stored into a package-level variable).
// Go closures capture by reference: the compiler shares the captured
// variable's memory between the function and the closure rather than
// copying it, so once the closure can outlive the function's own stack
// frame, escape analysis is forced to move the captured variable (and the
// closure itself) to the heap -- see cmd/compile/internal/escape.
//
// This rule is scoped to closures declared and escaping within the same
// function (single-hop, matching the rest of this package): a helper
// function whose only job is to construct-and-return a closure is a
// separate, harder case. Empirically that shape attributes the whole
// allocation to the *caller's* call-site line, not to any line inside the
// helper -- a cross-function trace this rule set doesn't attempt (see the
// "cross-function dependency chains" gap in docs/plan.md).
//
// The hot line pprof reports for this pattern isn't fixed to one spot: for
// a closure declared inside a loop, allocation is attributed to the
// closure literal's own line; for a closure escaping via a later statement
// with no loop involved, it's attributed to that escape statement's line
// instead; and when the captured variable itself is large, its own
// declaration line can dominate (a separate allocation from the closure
// struct itself). findEscapingCapturingClosure finds the pattern
// structurally across the whole function, and this rule fires if the hot
// site falls on any of those three lines.
func ClosureCapture(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding {
	fn := findEnclosingFunc(path)
	if fn == nil || fn.Body == nil {
		return nil
	}
	file := findFile(path)
	if file == nil {
		return nil
	}

	esc := findEscapingCapturingClosure(fn, file)
	if esc == nil {
		return nil
	}

	name, ok := capturesOuterVariable(fn, esc.lit)
	if !ok {
		return nil
	}

	decl := findDeclStmt(fn, name)
	if !nodeInPath(path, esc.lit) && !nodeInPath(path, esc.stmt) &&
		(decl == nil || !nodeInPath(path, decl)) {
		return nil
	}

	src, _ := idx.NodeSource(esc.lit)
	return &Finding{
		Site:    site,
		Pattern: "closure-capture-escape",
		Message: fmt.Sprintf(
			"line %d is part of a closure that captures %q from %s's scope and is %s. "+
				"Go closures capture variables by reference, so once the closure outlives "+
				"the stack frame that declared it, escape analysis moves the captured "+
				"variable onto the heap. %s",
			site.Line, name, funcName(fn), esc.reason, escapeGuidance(esc.reason)),
		Source:     strings.TrimSpace(src),
		SourceLine: int64(idx.Fset.Position(esc.lit.Pos()).Line),
	}
}

// escapeGuidance returns judgment guidance tailored to *why* the closure
// escapes, since that changes what "safe" looks like: a goroutine launch is
// a concurrency-safety question, a returned closure is an ordinary
// factory/currying pattern with no concurrency angle at all, and a
// package-level store is a lifetime question. A single generic checklist
// across all three would misdirect on at least two of them.
func escapeGuidance(reason string) string {
	switch reason {
	case "launched as a goroutine":
		return "Likely fine if the capture is a sync primitive (sync.WaitGroup, sync.Mutex, a channel) or read-only for the goroutine's lifetime. " +
			"Worth a closer look if it's a large or mutable buffer/struct captured per-goroutine, or if nothing bounds how many goroutines can be in flight at once."
	case "returned from the function":
		return "This is the ordinary closure-factory/currying pattern (e.g. building a middleware or callback) — the escape is expected and usually not a concern by itself. " +
			"Worth a closer look only if the captured variable is unexpectedly large, since every call to this function now pins a copy of it on the heap."
	case "stored into a package-level variable":
		return "The capture is now pinned for the life of the program. Worth a closer look if this runs repeatedly (e.g. inside a loop appending to the global) with nothing ever removing old entries, since that's unbounded growth rather than a one-time cost."
	default:
		return ""
	}
}
