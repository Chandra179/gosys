package rules

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
)

// StringConcatInLoop flags `s = s + x` or `s += x` assignments inside a
// for/range loop. This rule only ever runs at a site pprof has already
// identified as allocating real bytes, and Go's + operator only allocates
// for strings — numeric and complex addition never shows up as a pprof
// allocation site — so a self-concat match here at a hot site is strong
// evidence of string concatenation, not a coincidental numeric +=.
// Rebuilding a string this way copies the whole accumulated value on every
// iteration, an O(n^2) cost as the loop grows; strings.Builder (or
// bytes.Buffer) avoids it by writing once per iteration into a single
// growing buffer.
func StringConcatInLoop(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding {
	assign := findAssign(path)
	if assign == nil {
		return nil
	}
	if !insideLoop(path) {
		return nil
	}
	op, ok := selfConcatOp(idx, assign)
	if !ok {
		return nil
	}
	src, _ := idx.NodeSource(assign)
	return &Finding{
		Site:    site,
		Pattern: "string-concat-in-loop",
		Message: fmt.Sprintf(
			"line %d concatenates a string inside a loop with %q. "+
				"Each iteration allocates a new backing array sized to the accumulated string so far, an O(n^2) cost as the loop grows. "+
				"Consider building the result with strings.Builder (or bytes.Buffer) and writing once per iteration instead.",
			site.Line, op),
		Source: strings.TrimSpace(src),
	}
}

// selfConcatOp reports whether assign is `s += x` or `s = s + x` (the same
// left-hand expression reappearing as the left operand of the addition),
// and if so returns the operator text for the finding message.
func selfConcatOp(idx *astsite.Index, assign *ast.AssignStmt) (string, bool) {
	if len(assign.Lhs) != 1 {
		return "", false
	}
	switch assign.Tok {
	case token.ADD_ASSIGN:
		return "+=", true
	case token.ASSIGN:
		if len(assign.Rhs) != 1 {
			return "", false
		}
		bin, ok := assign.Rhs[0].(*ast.BinaryExpr)
		if !ok || bin.Op != token.ADD {
			return "", false
		}
		lhsSrc, err1 := idx.NodeSource(assign.Lhs[0])
		rhsXSrc, err2 := idx.NodeSource(bin.X)
		if err1 != nil || err2 != nil {
			return "", false
		}
		if strings.TrimSpace(lhsSrc) != strings.TrimSpace(rhsXSrc) {
			return "", false
		}
		return "s = s + x", true
	}
	return "", false
}
