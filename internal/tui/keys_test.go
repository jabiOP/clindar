package tui

import (
	"strings"
	"testing"
)

func TestHelpExplainsTextSelection(t *testing.T) {
	if got := helpText(true); !strings.Contains(got, "Option-drag in iTerm2, fn-drag in macOS Terminal, Shift-drag elsewhere") {
		t.Errorf("help text:\n%s", got)
	}
}
