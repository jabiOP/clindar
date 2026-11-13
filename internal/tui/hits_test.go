package tui

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestHitMapTopmostWins(t *testing.T) {
	var m hitMap
	m.add(0, 0, 10, 5, target{kind: targetModal})
	m.add(2, 1, 4, 1, target{kind: targetKey, key: "d"})

	if got, _ := m.at(3, 1); got.kind != targetKey {
		t.Errorf("at(3,1) = %v, want the button on top", got.kind)
	}
	if got, _ := m.at(8, 3); got.kind != targetModal {
		t.Errorf("at(8,3) = %v, want the pop-up", got.kind)
	}
	if _, ok := m.at(20, 20); ok {
		t.Error("at(20,20) should hit nothing")
	}
}

// find returns the screen position of the first occurrence of s in screen.
func find(t *testing.T, screen, s string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(screen, "\n") {
		if i := strings.Index(line, s); i >= 0 {
			return runewidth.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", s, screen)
	return 0, 0
}

// The real test of a hit map: click where the label is DRAWN and get its target.
func TestModalButtonsAreWhereTheyAreDrawn(t *testing.T) {
	var m modal
	m.text("Delete this event?")
	m.buttons(button{"Delete", "y"}, button{"Cancel", "esc"})
	box, hits := m.render()
	screen, x0, y0 := overlay(60, 12, box)
	hits = hits.offset(x0, y0)

	for label, wantKey := range map[string]string{"[Delete]": "y", "[Cancel]": "esc"} {
		x, y := find(t, screen, label)
		for dx := range len(label) {
			if got, _ := hits.at(x+dx, y); got.kind != targetKey || got.key != wantKey {
				t.Fatalf("click on %s (+%d) = %+v, want key %q", label, dx, got, wantKey)
			}
		}
	}
	x, y := find(t, screen, "Delete this")
	if got, _ := hits.at(x, y); got.kind != targetModal {
		t.Errorf("click on the text = %v, want targetModal", got.kind)
	}
}
