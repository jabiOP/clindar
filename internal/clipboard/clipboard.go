// Package clipboard builds the terminal sequence (OSC 52) that copies text:
// it works over SSH and in most terminals, but not in macOS Terminal.app.
package clipboard

import (
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// Sequence returns the escape sequence that asks the terminal to put text on
// the clipboard; inside tmux or screen it is wrapped to reach the outer
// terminal. The TUI sends it with a frame, so it can't interleave with one.
func Sequence(text string) string {
	seq := osc52.New(text)
	switch {
	case os.Getenv("TMUX") != "":
		seq = seq.Tmux()
	case strings.HasPrefix(os.Getenv("TERM"), "screen"):
		seq = seq.Screen()
	}
	return seq.String()
}
