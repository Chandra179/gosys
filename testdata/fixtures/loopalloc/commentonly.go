package loopalloc

// RunCommentOnly demonstrates the alloc-in-loop-without-pool anti-pattern in
// a function whose comment happens to mention sync.Pool without ever using
// one: this file's earlier fix meant "sync.Pool" is only recognized as an
// actual sync.Pool reference, not any occurrence of that text.
func RunCommentOnly(n int, data []byte) {
	for i := 0; i < n; i++ {
		buf := make([]byte, 0, 4096)
		buf = append(buf, data...)
		Sink = append(Sink, buf)
	}
}
