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
func Top(path string, n int) ([]Site, error) {
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
		if len(s.Location) == 0 || len(s.Value) <= idx {
			continue
		}
		leaf := s.Location[0]
		if len(leaf.Line) == 0 {
			continue
		}
		ln := leaf.Line[0]
		if ln.Function == nil {
			continue
		}
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
