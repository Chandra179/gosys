// Package rules matches AST patterns around a hot allocation site against a
// small, deliberately narrow set of known Go memory anti-patterns.
//
// Every rule here is a heuristic, not a proof: a match means "this pattern
// is present at a line that pprof says is retaining real memory," not
// "this line is definitely a bug." Findings carry the evidence (the pprof
// value and the matched source) so a human can judge them, and rules are
// kept few and specific on purpose — precision over recall.
//
// Each rule lives in its own file (slice_into_struct_field.go,
// map_pointer_growth.go, alloc_in_loop_without_pool.go,
// string_concat_in_loop.go, bytes_to_string_in_loop.go,
// range_value_escape.go, pointer_escape_return.go); this file only holds
// the shared Finding/Rule types and the All/Run entry points. AST helpers
// are split by concern rather than dumped in one file: generic path/node
// navigation lives in astutil.go, and alloc_helpers.go, pointer_helpers.go,
// pool_helpers.go, conv_helpers.go, range_helpers.go each hold the helpers
// specific to one rule family.
package rules

import (
	"go/ast"

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
	StringConcatInLoop,
	BytesToStringInLoop,
	RangeValueEscape,
	PointerEscapeReturn,
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
