// Package clipboard copies text: through the system clipboard when clindar
// runs on this computer, else through the terminal (OSC 52).
package clipboard

import (
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"
)

// Copy puts text on the clipboard. On this computer it uses the system's
// clipboard tool (pbcopy, wl-copy, xclip, xsel), which needs nothing from the
// terminal; it returns "". Over SSH, or with no such tool, it returns the
// OSC 52 sequence for the caller to send to the terminal (the TUI sends it with
// a frame, so the two can't interleave).
func Copy(text string) (sequence string, err error) {
	if os.Getenv("SSH_CONNECTION") == "" && os.Getenv("SSH_TTY") == "" {
		if err := clipboard.WriteAll(text); err == nil {
			return "", nil
		}
	}
	return Sequence(text), nil
}

// Sequence returns the escape sequence that asks the terminal to put text on
// the clipboard; inside tmux or screen it is wrapped to reach the outer terminal.
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
