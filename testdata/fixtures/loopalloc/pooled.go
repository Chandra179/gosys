package loopalloc

import "sync"

// bufPool backs RunPooled below. Its declaration is the only place the
// literal text "sync.Pool" appears in this file — RunPooled itself only
// calls bufPool.Get()/Put(). RunPooled's make() call, on a pool miss, sits
// directly inside the for loop, so this exercises the file-scope (not just
// function-scope) sync.Pool check: alloc-in-loop-without-pool must stay
// silent here even though the enclosing function's own source never
// mentions "sync.Pool".
var bufPool = sync.Pool{
	New: func() any { return nil },
}

func RunPooled(n int, data []byte) {
	for i := 0; i < n; i++ {
		v := bufPool.Get()
		var buf []byte
		if v == nil {
			buf = make([]byte, 0, 4096)
		} else {
			buf = v.([]byte)
		}
		buf = append(buf[:0], data...)
		Sink = append(Sink, buf)
		bufPool.Put(buf)
	}
}
