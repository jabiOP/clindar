package clipboard

import (
	"strings"
	"testing"
)

// Over SSH the system clipboard is the wrong computer's: copy through the terminal.
func TestOverSSHCopyGoesThroughTheTerminal(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 22 10.0.0.2 22")
	t.Setenv("TMUX", "")
	seq, err := Copy("https://meet.google.com/abc")
	if err != nil || !strings.HasPrefix(seq, "\x1b]52;c;") {
		t.Errorf("seq = %q, %v; want an OSC 52 sequence", seq, err)
	}
}
