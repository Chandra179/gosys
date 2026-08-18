// Package bytesconv demonstrates the bytes-to-string-in-loop anti-pattern:
// the same []byte gets converted to a string on every loop iteration.
package bytesconv

var Sink []string

func Run(n int, b []byte) {
	for i := 0; i < n; i++ {
		s := string(b)
		Sink = append(Sink, s)
	}
}
