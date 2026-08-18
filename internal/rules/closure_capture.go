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
				"variable onto the heap: %s",
			site.Line, name, funcName(fn), esc.reason, strings.TrimSpace(src)),
		Source: strings.TrimSpace(src),
	}
}
