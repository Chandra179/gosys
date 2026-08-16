// Command analyze maps hot allocation sites in a heap pprof profile to
// known Go memory anti-patterns in a repository's source.
package main

import (
	"flag"
	"fmt"
	"os"

	"gosys/internal/pipeline"
)

func main() {
	profilePath := flag.String("pprof", "", "path to a heap pprof file (required)")
	repoDir := flag.String("repo", ".", "path to the Go repository to analyze")
	top := flag.Int("top", 10, "number of hot allocation sites to inspect")
	flag.Parse()

	if *profilePath == "" {
		fmt.Fprintln(os.Stderr, "usage: analyze -pprof heap.pprof [-repo ./path] [-top 10]")
		os.Exit(2)
	}

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: *profilePath,
		RepoDir:     *repoDir,
		Top:         *top,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "analyze:", err)
		os.Exit(1)
	}

	flagged := 0
	for _, r := range results {
		if len(r.Findings) == 0 {
			continue
		}
		flagged++
		for _, f := range r.Findings {
			fmt.Printf("%s:%d  %d bytes  [%s]\n", f.Site.File, f.Site.Line, f.Site.Flat, f.Pattern)
			fmt.Printf("  %s\n", f.Message)
			if f.Source != "" {
				fmt.Printf("  > %s\n", f.Source)
			}
			fmt.Println()
		}
	}
	if flagged == 0 {
		fmt.Printf("no known anti-patterns matched in the top %d allocation sites\n", *top)
	}
}
