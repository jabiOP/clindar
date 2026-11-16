package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/service"
)

func TestAddEventThroughForm(t *testing.T) {
	f := newFixture(t)

	f.keys("l", "a", "Dentist", "enter") // tomorrow, quick add, type title, save (all-day)

	if got := f.titlesOn(13); len(got) != 1 || got[0] != "Dentist" {
		t.Errorf("Nov 13 = %v, want [Dentist]", got)
	}
	if f.model().mode != modeNormal {
		t.Errorf("form still open")
	}
}

func TestFormShowsParseErrors(t *testing.T) {
	f := newFixture(t)
	f.openFullForm()
	f.keys("X", "tab")
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
	f.openFullForm()

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

// openFullForm opens the add form the long way: quick add, then tab.
func (f *fixture) openFullForm() {
	f.t.Helper()
	f.keys("a", "tab")
	if f.model().mode != modeForm {
		f.t.Fatalf("mode = %v, want the full form", f.model().mode)
	}
}

func TestQuickAddCreatesATimedEvent(t *testing.T) {
	f := newFixture(t)
	f.keys("a", "Dentist 15:30 45m", "enter")

	events, _ := f.svc.Agenda(context.Background(), time.Date(2026, 11, 12, 0, 0, 0, 0, f.ny),
		time.Date(2026, 11, 13, 0, 0, 0, 0, f.ny), service.Filter{})
	var got []string
	for _, e := range events {
		got = append(got, e.Start.In(f.ny).Format("15:04")+" "+e.Title)
	}
	if !slices.Contains(got, "15:30 Dentist") {
		t.Errorf("Nov 12 = %v", got)
	}
}

func TestQuickAddPreviewIsLive(t *testing.T) {
	f := newFixture(t)
	f.keys("a", "Standup weekdays 9:00")
	if !strings.Contains(f.m.View(), "every weekday") {
		t.Errorf("no preview:\n%s", f.m.View())
	}
}

func TestQuickAddErrorKeepsItOpen(t *testing.T) {
	f := newFixture(t)
	f.keys("a", "Nap 30m", "enter")
	if f.model().mode != modeQuick || !strings.Contains(f.m.View(), "start time") {
		t.Errorf("mode = %v:\n%s", f.model().mode, f.m.View())
	}
}

func TestQuickAddTabOpensThePrefilledForm(t *testing.T) {
	f := newFixture(t)
	f.keys("a", "Dentist 20/11 15:30", "tab")
	m := f.model()
	if m.mode != modeForm || m.form.value(fTitle) != "Dentist" || !strings.Contains(m.form.value(fStart), "15:30") {
		t.Errorf("mode %v, title %q, start %q", m.mode, m.form.value(fTitle), m.form.value(fStart))
	}
}
