package tui

import (
	"fmt"
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func abc() picker {
	return picker{title: "Calendar", options: []option{{label: "Me", value: "me"}, {label: "Work", value: "w"}, {label: "Home", value: "h"}}}
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestPickerCyclesWhenClosed(t *testing.T) {
	p := abc()
	p, ev, _ := p.Update(runes("l"))
	if ev != pickChanged || p.current().value != "w" {
		t.Fatalf("l: %v %q", ev, p.current().value)
	}
	p, _, _ = p.Update(runes("h"))
	p, _, _ = p.Update(runes("h")) // wraps from the first to the last
	if p.current().value != "h" {
		t.Errorf("h h: %q, want the last option", p.current().value)
	}
}

func TestPickerDropdownFiltersAndChooses(t *testing.T) {
	p := abc()
	p, _, _ = p.Update(runes(" ")) // open
	p, _, _ = p.Update(runes("wo"))
	if vis := p.visible(); len(vis) != 1 || p.options[vis[0]].label != "Work" {
		t.Fatalf("visible = %v", vis)
	}
	p, ev, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ev != pickChanged || p.open || p.current().label != "Work" {
		t.Errorf("enter: %v open=%v %q", ev, p.open, p.current().label)
	}
}

func TestPickerEscKeepsTheChoice(t *testing.T) {
	p := abc()
	p, _, _ = p.Update(runes(" "))
	p, _, _ = p.Update(runes("j"))
	p, ev, _ := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if ev != pickClosed || p.current().value != "me" {
		t.Errorf("esc: %v %q", ev, p.current().value)
	}
}

func TestChecklistToggles(t *testing.T) {
	p := abc()
	p.checklist, p.open = true, true
	p, _, _ = p.Update(runes("j"))
	p, ev, i := p.Update(runes(" "))
	if ev != pickToggled || i != 1 || !p.options[1].checked {
		t.Errorf("space: %v %d %v", ev, i, p.options[1].checked)
	}
}

func TestPickerRowsAreClickable(t *testing.T) {
	p := abc()
	p.open = true
	box, hits := p.view(40, true).render()
	x, y := find(t, box, "Home")
	if got, _ := hits.at(x, y); got.kind != targetOption || got.field != 2 {
		t.Errorf("click on Home = %+v", got)
	}
}

// A long list (a calendar's 200+ colors) shows a window around the cursor.
func TestLongListScrollsWithTheCursor(t *testing.T) {
	p := picker{open: true}
	for i := range 30 {
		p.options = append(p.options, option{label: fmt.Sprintf("color %d", i), value: fmt.Sprint(i)})
	}
	p.cursor = 25
	lines, idx := p.rows(40)
	if len(lines) != pickerWindow || !slices.Contains(idx, 25) || idx[0] != 20 {
		t.Errorf("rows %d, idx %v; want %d rows around 25", len(lines), idx, pickerWindow)
	}
}
