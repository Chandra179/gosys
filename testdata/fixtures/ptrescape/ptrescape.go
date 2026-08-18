// Package ptrescape demonstrates the pointer-escape-return anti-pattern:
// returning the address of a locally-constructed composite literal.
package ptrescape

// Big is intentionally large so each escaping pointer costs real bytes.
type Big struct {
	Data [4096]byte
}

var Sink []*Big

// newBig returns &Big{...}: escape analysis moves this to the heap because
// the pointer crosses newBig's return boundary.
func newBig() *Big {
	return &Big{}
}

func Run(n int) {
	for i := 0; i < n; i++ {
		Sink = append(Sink, newBig())
	}
}

// newBigIndirect exercises the single-hop indirect shape: the composite
// literal's address is stashed in a local variable before being returned.
func newBigIndirect() *Big {
	v := &Big{}
	return v
}

func RunIndirect(n int) {
	for i := 0; i < n; i++ {
		Sink = append(Sink, newBigIndirect())
	}
}
