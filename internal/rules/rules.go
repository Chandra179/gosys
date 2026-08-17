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
	if file := findFile(path); file != nil {
		if fileSrc, err := idx.NodeSource(file); err == nil && strings.Contains(fileSrc, "sync.Pool") {
			return nil // pool usage present somewhere in the file; don't flag
		}
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
