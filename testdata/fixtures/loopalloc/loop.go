// Package loopalloc demonstrates the alloc-in-loop-without-pool
// anti-pattern: a fresh buffer is allocated on every loop iteration with no
// sync.Pool in sight to reuse it.
package loopalloc

var Sink [][]byte

func Run(n int, data []byte) {
	for i := 0; i < n; i++ {
		buf := make([]byte, 0, 4096)
		buf = append(buf, data...)
		Sink = append(Sink, buf)
	}
}
