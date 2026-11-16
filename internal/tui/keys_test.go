package tui

import (
	"strings"
	"testing"
)

func TestHelpExplainsTextSelection(t *testing.T) {
	if got := helpText(true); !strings.Contains(got, "Option-drag on macOS, Shift-drag elsewhere") {
		t.Errorf("help text:\n%s", got)
	}
}
