// Package sliceleak demonstrates the slice-into-struct-field anti-pattern:
// a large temporary buffer is allocated and only a small window of it is
// kept, but the backing array stays pinned for as long as the struct does.
package sliceleak

type Holder struct {
	buf []byte
}

var Kept []*Holder

func Run(n int) {
	for i := 0; i < n; i++ {
		h := &Holder{}
		h.buf = make([]byte, 1<<20)[:16]
		Kept = append(Kept, h)
	}
}
