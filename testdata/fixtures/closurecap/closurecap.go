// Package closurecap demonstrates the closure-capture-escape anti-pattern:
// a closure that captures a variable by reference and escapes the function
// that declared it.
package closurecap

// state is intentionally large so each captured-and-escaped closure costs
// real bytes -- the default heap-profile sampling rate (one sample per
// ~512KB allocated) would otherwise miss a plain `int` capture entirely.
type state struct {
	Data  [4096]byte
	Total int
}

var Sink []func() int

// RunLoop declares the closure inside a loop: pprof attributes the
// allocation to the closure literal's own declaration line in this shape.
func RunLoop(n int) {
	for i := 0; i < n; i++ {
		var st state
		f := func() int {
			st.Total++
			return st.Total
		}
		Sink = append(Sink, f)
	}
}

// RunSingle declares the closure once per call, outside any loop: pprof
// attributes the allocation to the escape statement's line (the append)
// in this shape instead of the closure literal's own line.
//
//go:noinline
func RunSingle() func() int {
	var st state
	f := func() int {
		st.Total++
		return st.Total
	}
	Sink = append(Sink, f)
	return f
}

func RunSingleMany(n int) {
	for i := 0; i < n; i++ {
		RunSingle()
	}
}
