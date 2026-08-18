// Package rangeescape demonstrates the range-value-escape anti-pattern:
// taking the address of a range loop's per-iteration value.
package rangeescape

// Big is intentionally large so each escaped range value costs real bytes.
type Big struct {
	Data [4096]byte
}

var Sink []*Big

// Run: since Go 1.22, v below is a fresh per-iteration variable, so &v
// escapes a new 4096-byte copy to the heap on every iteration.
func Run(items []Big) {
	for _, v := range items {
		Sink = append(Sink, &v)
	}
}
