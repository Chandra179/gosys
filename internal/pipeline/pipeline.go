// Package pipeline wires pprof parsing, AST resolution, and rule matching
// into a single Analyze call, shared by the CLI and by tests.
package pipeline

import (
	"fmt"
	"sort"

	"gosys/internal/astsite"
	"gosys/internal/pprofstats"
	"gosys/internal/rules"
)

// Config controls one analysis run.
type Config struct {
	ProfilePath string // heap pprof file
	RepoDir     string // repo root to load via go/packages
	Top         int    // how many hot allocation sites to inspect
}

// Result is one hot site plus whatever rule findings it matched. Sites with
// no findings are still returned (with Findings == nil) so the report can
// show "this was hot but nothing looked wrong" rather than going silent.
type Result struct {
	Site     pprofstats.Site
	Findings []rules.Finding
}

// Analyze runs the full pprof -> AST -> rules pipeline.
func Analyze(cfg Config) ([]Result, error) {
	if cfg.Top <= 0 {
		cfg.Top = 10
	}

	idx, err := astsite.Load(cfg.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("load repo: %w", err)
	}

	resolvable := func(file string, line int64) bool {
		_, _, ok := idx.PathAt(file, line)
		return ok
	}

	sites, err := pprofstats.Top(cfg.ProfilePath, cfg.Top, resolvable)
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}

	results := make([]Result, 0, len(sites))
	for _, site := range sites {
		results = append(results, Result{
			Site:     site,
			Findings: rules.Run(idx, site),
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Site.Flat > results[j].Site.Flat
	})
	return results, nil
}
