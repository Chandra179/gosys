// Package strconcat demonstrates the string-concat-in-loop anti-pattern:
// repeatedly appending to a string with + inside a loop reallocates and
// copies the whole accumulated string on every iteration.
package strconcat

var Sink string

func Run(n int, chunk string) {
	s := ""
	for i := 0; i < n; i++ {
		s = s + chunk
	}
	Sink = s
}
