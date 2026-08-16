// Package rules matches AST patterns around a hot allocation site against a
// small, deliberately narrow set of known Go memory anti-patterns.
//
// Every rule here is a heuristic, not a proof: a match means "this pattern
// is present at a line that pprof says is retaining real memory," not
// "this line is definitely a bug." Findings carry the evidence (the pprof
// value and the matched source) so a human can judge them, and rules are
// kept few and specific on purpose — precision over recall.
package rules

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
)

// Finding is one rule match at one hot allocation site.
type Finding struct {
	Site    pprofstats.Site
	Pattern string
	Message string
	Source  string // the matched source snippet, for the reader to judge
}

// Rule inspects the AST path at a hot site (innermost node first) and
// returns a Finding if its pattern matches, or nil otherwise.
type Rule func(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding

// All is the full v1 rule set.
var All = []Rule{
	SliceIntoStructField,
	MapPointerGrowth,
	AllocInLoopWithoutPool,
}

// Run applies every rule in All to the given site and returns all matches
// (usually zero or one, but a line can trip more than one pattern).
func Run(idx *astsite.Index, site pprofstats.Site) []Finding {
	path, _, ok := idx.PathAt(site.File, site.Line)
	if !ok {
		return nil
	}
	var findings []Finding
	for _, rule := range All {
		if f := rule(idx, path, site); f != nil {
			findings = append(findings, *f)
		}
	}
	return findings
}

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

// MapPointerGrowth flags `m[key] = &T{...}` (or an existing pointer)
// assignments into a map. Maps of pointers never shrink their bucket
// memory as entries are deleted, and each live pointer keeps its pointee
// on the heap — a common source of unbounded growth in long-lived caches.
func MapPointerGrowth(idx *astsite.Index, path []ast.Node, site pprofstats.Site) *Finding {
	assign := findAssign(path)
	if assign == nil {
		return nil
	}
	for i, lhs := range assign.Lhs {
		if i >= len(assign.Rhs) {
			break
		}
		idxExpr, ok := lhs.(*ast.IndexExpr)
		if !ok {
			continue
		}
		if !isPointerProducing(assign.Rhs[i]) {
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

// AllocInLoopWithoutPool flags make([]byte, ...) or json.Unmarshal calls
// that sit inside a for/range loop, in a function with no visible
// sync.Pool usage. Per-iteration allocation of short-lived buffers is a
// classic GC-pressure source that sync.Pool exists specifically to fix.
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
	fnSrc, err := idx.NodeSource(fn)
	if err == nil && strings.Contains(fnSrc, "sync.Pool") {
		return nil // pool usage present somewhere in the function; don't flag
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

func findAssign(path []ast.Node) *ast.AssignStmt {
	for _, n := range path {
		if a, ok := n.(*ast.AssignStmt); ok {
			return a
		}
	}
	return nil
}

// findAllocCall locates the allocation call (make([]byte, ...) or
// json.Unmarshal) responsible for the hot line. PathEnclosingInterval's
// path holds ancestors of the line's interval, but pprof attributes the
// allocation to a CallExpr that is typically a *descendant* of the
// innermost enclosing statement (e.g. the make(...) inside
// `buf := make([]byte, n)`), so this checks ancestors first, then walks
// into the innermost statement's subtree.
func findAllocCall(path []ast.Node) *ast.CallExpr {
	for _, n := range path {
		if c, ok := n.(*ast.CallExpr); ok && isAllocCall(c) {
			return c
		}
	}
	if len(path) == 0 {
		return nil
	}
	var found *ast.CallExpr
	ast.Inspect(path[0], func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok && isAllocCall(c) {
			found = c
			return false
		}
		return true
	})
	return found
}

func findEnclosingFunc(path []ast.Node) *ast.FuncDecl {
	for _, n := range path {
		if fn, ok := n.(*ast.FuncDecl); ok {
			return fn
		}
	}
	return nil
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Name != nil {
		return fn.Name.Name
	}
	return "<anonymous>"
}

func insideLoop(path []ast.Node) bool {
	for _, n := range path {
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			return true
		}
	}
	return false
}

// isPointerProducing reports whether expr syntactically looks like it
// produces a pointer: &x, or a call to new(...).
func isPointerProducing(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		return e.Op == token.AND
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok {
			return id.Name == "new"
		}
	}
	return false
}

// isAllocCall reports whether call is make([]byte, ...) or a call to
// json.Unmarshal.
func isAllocCall(call *ast.CallExpr) bool {
	if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "make" {
		if len(call.Args) > 0 {
			if arr, ok := call.Args[0].(*ast.ArrayType); ok {
				if elt, ok := arr.Elt.(*ast.Ident); ok && elt.Name == "byte" {
					return true
				}
			}
		}
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "json" && sel.Sel.Name == "Unmarshal" {
			return true
		}
	}
	return false
}
