// Package mapgrowth demonstrates the map-pointer-growth anti-pattern: a
// long-lived map keyed by pointers, only ever grown and never evicted.
package mapgrowth

type Value struct {
	Data [4096]byte
}

var Cache = map[int]*Value{}

func Run(n int) {
	for i := 0; i < n; i++ {
		Cache[i] = &Value{}
	}
}

type Small struct {
	N int
}

var IndirectCache = map[int]*Small{}

// RunIndirect exercises the same anti-pattern one level removed: the
// pointer is assigned to a local variable first, and that variable (not a
// literal &x or new(...)) is what's stored into the map. It uses its own
// small value type and a separate, larger n from Run: with the pointee's
// allocation and the map store split across two lines, the store line's
// own hot bytes (the map's internal bucket growth, not the pointee) need
// enough iterations to be a distinguishable pprof site in its own right.
func RunIndirect(n int) {
	for i := 0; i < n; i++ {
		v := &Small{N: i}
		IndirectCache[i] = v
	}
}
