// Package pprofstats aggregates flat allocation values from a heap pprof
// profile by the source line where the allocation happened (the leaf frame
// of each sample's stack).
package pprofstats

import (
	"fmt"
	"os"
	"sort"

	"github.com/google/pprof/profile"
)

// Site is a single source location responsible for allocations, aggregated
// across every sample whose leaf frame landed there.
type Site struct {
	Function string
	File     string
	Line     int64
	Flat     int64 // sum of sample values attributed to this site
}

// preferredSampleTypes lists the heap sample type names to look for, in
// priority order. inuse_space reflects live retained memory, which is what
// the anti-patterns in this tool care about (GC can't reclaim it).
var preferredSampleTypes = []string{"inuse_space", "alloc_space"}

// Top parses a heap profile at path and returns the top n allocation sites
// by flat value, aggregated per (function, file, line).
//
// resolvable reports whether a file:line is resolvable against the repo
// being analyzed (see astsite.Index.PathAt). It's used to re-attribute
// allocations from stdlib/framework frames back to the repo call site that
// triggered them; see attributedSite. Pass nil to use the raw leaf frame.
func Top(path string, n int, resolvable func(file string, line int64) bool) ([]Site, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open profile: %w", err)
	}
	defer f.Close()

	p, err := profile.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}

	idx, sampleType, err := pickSampleIndex(p)
	if err != nil {
		return nil, err
	}

	type key struct {
		fn   string
		file string
		line int64
	}
	agg := make(map[key]int64)

	for _, s := range p.Sample {
		if len(s.Value) <= idx {
			continue
		}
		loc, ok := attributedSite(s, resolvable)
		if !ok {
			continue
		}
		ln := loc.Line[0]
		k := key{fn: ln.Function.Name, file: ln.Function.Filename, line: ln.Line}
		agg[k] += s.Value[idx]
	}

	sites := make([]Site, 0, len(agg))
	for k, v := range agg {
		sites = append(sites, Site{Function: k.fn, File: k.file, Line: k.line, Flat: v})
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Flat > sites[j].Flat })

	if n > 0 && len(sites) > n {
		sites = sites[:n]
	}

	_ = sampleType // reserved for report headers
	return sites, nil
}

// attributedSite walks a sample's stack root->leaf and returns the deepest
// resolvable frame, i.e. the repo call site that triggered whatever
// non-repo code did the actual allocating beneath it:
//
//	root  main -> yourpkg.Handler -> json.Marshal -> reflect.Value -> mallocgc  leaf
//	                        ^ resolvable      ^ not resolvable, stop here
//	                        `-- attributed site
//
// With resolvable == nil, or if no frame resolves, falls back to the raw
// leaf frame (s.Location[0]).
func attributedSite(s *profile.Sample, resolvable func(file string, line int64) bool) (*profile.Location, bool) {
	if resolvable == nil {
		if len(s.Location) == 0 || len(s.Location[0].Line) == 0 || s.Location[0].Line[0].Function == nil {
			return nil, false
		}
		return s.Location[0], true
	}

	lastResolvable := -1
	foundResolvable := false
	for i := len(s.Location) - 1; i >= 0; i-- {
		loc := s.Location[i]
		if len(loc.Line) == 0 || loc.Line[0].Function == nil {
			continue
		}
		ln := loc.Line[0]
		if resolvable(ln.Function.Filename, ln.Line) {
			foundResolvable = true
			lastResolvable = i
			continue
		}
		if foundResolvable {
			break
		}
	}
	if lastResolvable == -1 {
		return nil, false
	}
	return s.Location[lastResolvable], true
}

func pickSampleIndex(p *profile.Profile) (int, string, error) {
	for _, want := range preferredSampleTypes {
		for i, st := range p.SampleType {
			if st.Type == want {
				return i, want, nil
			}
		}
	}
	if len(p.SampleType) == 0 {
		return 0, "", fmt.Errorf("profile has no sample types")
	}
	return 0, p.SampleType[0].Type, nil
}
