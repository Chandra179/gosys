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
