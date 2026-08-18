package pipeline

import (
	"path/filepath"
	"sync"
	"time"

	"gosys/internal/astsite"
)

// astsite.Load type-checks the whole repo via go/packages, which shells out
// to the Go toolchain. Timed against real repos (etcd, syncthing): well
// over a minute when the toolchain's own build cache is cold for that
// repo, but ~1s once it's warm — astsite itself doesn't get any slower,
// the cache does. Analyze is the dashboard's per-request entry point, and
// its common case is re-running analysis against the same repo repeatedly
// (watching a live target), so cache the loaded Index here rather than in
// astsite: astsite stays a pure loader, and caching is this package's call
// to make since it's the one that knows Analyze gets called repeatedly.
//
// The TTL bounds staleness (a repo edited while the dashboard keeps
// running) against a cache that never expires and silently serves AST from
// before the edit.
const indexCacheTTL = 30 * time.Second

type cachedIndex struct {
	idx    *astsite.Index
	loaded time.Time
}

var (
	indexCacheMu sync.Mutex
	indexCache   = map[string]cachedIndex{}
)

func loadIndex(repoDir string) (*astsite.Index, error) {
	key, err := filepath.Abs(repoDir)
	if err != nil {
		key = repoDir
	}

	indexCacheMu.Lock()
	if c, ok := indexCache[key]; ok && time.Since(c.loaded) < indexCacheTTL {
		indexCacheMu.Unlock()
		return c.idx, nil
	}
	indexCacheMu.Unlock()

	idx, err := astsite.Load(repoDir)
	if err != nil {
		return nil, err
	}

	indexCacheMu.Lock()
	indexCache[key] = cachedIndex{idx: idx, loaded: time.Now()}
	indexCacheMu.Unlock()
	return idx, nil
}
