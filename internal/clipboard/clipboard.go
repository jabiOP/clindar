// Package clipboard copies text through the terminal (OSC 52): it works over
// SSH and in most terminals, but not in macOS Terminal.app.
package clipboard

import (
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// Copy asks the terminal to put text on the clipboard. Inside tmux or screen
// the sequence is wrapped so it reaches the outer terminal.
func Copy(text string) error {
	seq := osc52.New(text)
	switch {
	case os.Getenv("TMUX") != "":
		seq = seq.Tmux()
	case strings.HasPrefix(os.Getenv("TERM"), "screen"):
		seq = seq.Screen()
	}
	_, err := seq.WriteTo(os.Stderr) // the TUI owns stdout; stderr reaches the same terminal
	return err
}
