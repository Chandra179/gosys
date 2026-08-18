package pipeline_test

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"

	"gosys/internal/pipeline"
	"gosys/testdata/fixtures/bytesconv"
	"gosys/testdata/fixtures/loopalloc"
	"gosys/testdata/fixtures/mapgrowth"
	"gosys/testdata/fixtures/ptrescape"
	"gosys/testdata/fixtures/rangeescape"
	"gosys/testdata/fixtures/sliceleak"
	"gosys/testdata/fixtures/strconcat"
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

// hasPatternForFunc is like hasPattern but scoped to a single function name.
// Needed because pipeline_test.go's fixtures share one process: an earlier
// test's now-freed allocation sites can still show up as zero-byte entries
// in a later test's captured profile (the runtime's profiling buckets are
// process-lifetime), and an unscoped hasPattern check would false-positive
// on that stale entry instead of the function actually under test.
func hasPatternForFunc(results []pipeline.Result, fn, pattern string) bool {
	for _, r := range results {
		if r.Site.Function != fn {
			continue
		}
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

// TestMapPointerGrowth_Indirect guards against a false negative: a pointer
// stored via an intermediate local variable (`v := &Value{}; Cache[i] = v`)
// must still be flagged, not just the literal `Cache[i] = &Value{}` form.
func TestMapPointerGrowth_Indirect(t *testing.T) {
	mapgrowth.IndirectCache = map[int]*mapgrowth.Small{}
	mapgrowth.RunIndirect(200000)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/mapgrowth",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	const fn = "gosys/testdata/fixtures/mapgrowth.RunIndirect"
	if !hasPatternForFunc(results, fn, "map-pointer-growth") {
		t.Errorf("expected map-pointer-growth finding for RunIndirect, got results: %+v", results)
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

// TestAllocInLoopWithoutPool_FileScopePool guards against a false positive:
// a sync.Pool declared at package level, used only via Get()/Put() in the
// hot function, must still suppress the finding even though the literal
// text "sync.Pool" never appears in that function's own source.
func TestAllocInLoopWithoutPool_FileScopePool(t *testing.T) {
	loopalloc.Sink = nil // avoid the previous test's still-live buffers polluting this profile
	data := make([]byte, 4096)
	loopalloc.RunPooled(2000, data)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/loopalloc",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	const fn = "gosys/testdata/fixtures/loopalloc.RunPooled"
	if hasPatternForFunc(results, fn, "alloc-in-loop-without-pool") {
		t.Errorf("expected no alloc-in-loop-without-pool finding for pooled code, got results: %+v", results)
	}
}

// TestAllocInLoopWithoutPool_CommentOnly guards against a false negative:
// a file that merely mentions "sync.Pool" in a comment, without an actual
// sync.Pool reference anywhere in its syntax, must still be flagged.
func TestAllocInLoopWithoutPool_CommentOnly(t *testing.T) {
	loopalloc.Sink = nil
	data := make([]byte, 4096)
	loopalloc.RunCommentOnly(2000, data)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/loopalloc",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	const fn = "gosys/testdata/fixtures/loopalloc.RunCommentOnly"
	if !hasPatternForFunc(results, fn, "alloc-in-loop-without-pool") {
		t.Errorf("expected alloc-in-loop-without-pool finding for RunCommentOnly, got results: %+v", results)
	}
}

func TestStringConcatInLoop(t *testing.T) {
	strconcat.Run(20000, "x")
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/strconcat",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !hasPattern(results, "string-concat-in-loop") {
		t.Errorf("expected string-concat-in-loop finding, got results: %+v", results)
	}
}

func TestBytesToStringInLoop(t *testing.T) {
	b := make([]byte, 4096)
	bytesconv.Run(2000, b)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/bytesconv",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !hasPattern(results, "bytes-to-string-in-loop") {
		t.Errorf("expected bytes-to-string-in-loop finding, got results: %+v", results)
	}
}

func TestRangeValueEscape(t *testing.T) {
	items := make([]rangeescape.Big, 1000)
	rangeescape.Run(items)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/rangeescape",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !hasPattern(results, "range-value-escape") {
		t.Errorf("expected range-value-escape finding, got results: %+v", results)
	}
}

func TestPointerEscapeReturn(t *testing.T) {
	ptrescape.Sink = nil
	ptrescape.Run(2000)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/ptrescape",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !hasPattern(results, "pointer-escape-return") {
		t.Errorf("expected pointer-escape-return finding, got results: %+v", results)
	}
}

// TestPointerEscapeReturn_Indirect guards against a false negative: the
// single-hop indirect shape (`v := &Struct{}; return v`) must be flagged
// too, not just the literal `return &Struct{}` form.
func TestPointerEscapeReturn_Indirect(t *testing.T) {
	ptrescape.Sink = nil
	ptrescape.RunIndirect(2000)
	profile := captureHeapProfile(t)

	results, err := pipeline.Analyze(pipeline.Config{
		ProfilePath: profile,
		RepoDir:     "../../testdata/fixtures/ptrescape",
		Top:         20,
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	const fn = "gosys/testdata/fixtures/ptrescape.newBigIndirect"
	if !hasPatternForFunc(results, fn, "pointer-escape-return") {
		t.Errorf("expected pointer-escape-return finding for newBigIndirect, got results: %+v", results)
	}
}
