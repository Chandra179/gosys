package pipeline_test

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"

	"gosys/internal/pipeline"
	"gosys/testdata/fixtures/loopalloc"
	"gosys/testdata/fixtures/mapgrowth"
	"gosys/testdata/fixtures/sliceleak"
)

// captureHeapProfile forces a GC (so the profile reflects live/inuse data,
// not garbage awaiting collection) and writes a heap profile to a temp
// file, returning its path.
func captureHeapProfile(t *testing.T) string {
	t.Helper()
	runtime.GC()

	path := filepath.Join(t.TempDir(), "heap.pprof")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create profile file: %v", err)
	}
	defer f.Close()

	if err := pprof.WriteHeapProfile(f); err != nil {
		t.Fatalf("write heap profile: %v", err)
	}
	return path
}

func hasPattern(results []pipeline.Result, pattern string) bool {
	for _, r := range results {
		for _, f := range r.Findings {
			if f.Pattern == pattern {
				return true
			}
		}
	}
	return false
}

func TestSliceIntoStructField(t *testing.T) {
	sliceleak.Run(2000)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/sliceleak",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !hasPattern(results, "slice-into-struct-field") {
		t.Errorf("expected slice-into-struct-field finding, got results: %+v", results)
	}
}

func TestMapPointerGrowth(t *testing.T) {
	mapgrowth.Run(2000)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/mapgrowth",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !hasPattern(results, "map-pointer-growth") {
		t.Errorf("expected map-pointer-growth finding, got results: %+v", results)
	}
}

func TestAllocInLoopWithoutPool(t *testing.T) {
	data := make([]byte, 4096)
	loopalloc.Run(2000, data)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/loopalloc",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !hasPattern(results, "alloc-in-loop-without-pool") {
		t.Errorf("expected alloc-in-loop-without-pool finding, got results: %+v", results)
	}
}
