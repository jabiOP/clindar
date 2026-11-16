package tui

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAddEventThroughForm(t *testing.T) {
	f := newFixture(t)

	f.keys("l", "a", "Dentist", "enter") // tomorrow, add, type title, save (start defaults to 09:00)

	if got := f.titlesOn(13); len(got) != 1 || got[0] != "Dentist" {
		t.Errorf("Nov 13 = %v, want [Dentist]", got)
	}
	if f.model().mode != modeNormal {
		t.Errorf("form still open")
	}
}

func TestFormShowsParseErrors(t *testing.T) {
	f := newFixture(t)
	f.keys("a", "X", "tab")
	f.send(tea.KeyMsg{Type: tea.KeyCtrlU}) // clear the Start field
	f.keys("someday", "enter")

	m := f.model()
	if m.mode != modeForm || !strings.Contains(m.form.err, "someday") {
		t.Errorf("mode = %v, form.err = %q", m.mode, m.form.err)
	}
}

func TestDeleteRecurringInstanceAsksForScope(t *testing.T) {
	f := newFixture(t)
	f.keys("tab") // list starting today (Thu Nov 12): Standup 09:00 is first

	f.keys("d")
	if f.model().mode != modeScope {
		t.Fatalf("mode = %v, want scope picker", f.model().mode)
	}
	f.keys("t")

	if got := f.titlesOn(12); len(got) != 1 || got[0] != "Lunch with Sam" {
		t.Errorf("Nov 12 = %v, want only lunch", got)
	}
	if got := f.titlesOn(16); len(got) != 1 {
		t.Errorf("Nov 16 = %v: other instances must remain", got)
	}
}

func TestAddFormUsesDefaultCalendar(t *testing.T) {
	f := newEventForm(time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC), "Work")

	if got := f.value(fCalendar); got != "Work" {
		t.Errorf("calendar field = %q, want the configured default", got)
	}
}

func TestFormCtrlJKMoveBetweenFields(t *testing.T) {
	f := newFixture(t)
	f.keys("a") // the form, until Task 14.16 (which updates this line to f.openFullForm())

	f.send(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if got := f.model().form.focus; got != 1 {
		t.Fatalf("after ctrl+j focus = %d, want 1", got)
	}
	f.send(tea.KeyMsg{Type: tea.KeyCtrlK})
	if got := f.model().form.focus; got != 0 {
		t.Errorf("after ctrl+k focus = %d, want 0", got)
	}
	if !strings.Contains(f.m.View(), "ctrl+j") {
		t.Error("the hint line should mention ctrl+j/ctrl+k")
	}
}
