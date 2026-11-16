package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestNewEventDefaultsToAllDayOnTheSelectedDay(t *testing.T) {
	f := newFixture(t)
	f.keys("l") // Fri 13
	f.openFullForm()

	fm := f.model().form
	if fm.value(fDate) != "13/11/2026" || fm.pickers[fAllDay].current().value != "yes" || fm.value(fStart) != "" {
		t.Errorf("date %q allDay %q start %q", fm.value(fDate), fm.pickers[fAllDay].current().value, fm.value(fStart))
	}
}

func TestTypingAStartTimeMakesItTimed(t *testing.T) {
	f := newFixture(t)
	f.openFullForm()
	f.keys("Dentist")
	f.focus(fStart)
	f.keys("15:30")
	if got := f.model().form.pickers[fAllDay].current().value; got != "no" {
		t.Fatalf("all-day = %q after typing a time", got)
	}
	f.keys("enter")

	if got := f.titlesOn(12); !slices.Contains(got, "Dentist") {
		t.Errorf("Nov 12 = %v", got)
	}
}

func TestCalendarPickerOffersOnlyWritableCalendars(t *testing.T) {
	f := newFixture(t)
	for _, c := range []model.Calendar{
		{AccountID: testutil.Account, ID: "work", Name: "Work", Selected: true, AccessRole: "writer"},
		{AccountID: testutil.Account, ID: "hol", Name: "Holidays", Selected: true, AccessRole: "reader"},
	} {
		f.st.UpsertCalendar(context.Background(), c)
	}
	f.send(f.model().load()())
	f.openFullForm()

	var labels []string
	for _, o := range f.model().form.pickers[fCalendar].options {
		labels = append(labels, o.label)
	}
	if !slices.Equal(labels, []string{"Me", "Work"}) {
		t.Errorf("calendars = %v", labels)
	}
}

func TestRepeatPickerMakesASeries(t *testing.T) {
	f := newFixture(t)
	f.openFullForm()
	f.keys("Yoga")
	f.focus(fRepeat)
	f.keys("l", "l", "l") // none → daily → weekdays → weekly
	f.keys("enter")       // on a picker: opens the dropdown…
	f.keys("enter")       // …and chooses "weekly" again; then save from a text field:
	f.focus(fTitle)
	f.keys("enter")

	events, _ := f.svc.Agenda(context.Background(), time.Date(2026, 11, 19, 0, 0, 0, 0, f.ny),
		time.Date(2026, 11, 20, 0, 0, 0, 0, f.ny), service.Filter{})
	found := false
	for _, e := range events {
		found = found || (e.Title == "Yoga" && e.IsInstance())
	}
	if !found {
		t.Error("Yoga should repeat weekly (an instance on Nov 19)")
	}
}

func TestFieldErrorShowsUnderItsField(t *testing.T) {
	f := newFixture(t)
	f.openFullForm()
	f.keys("X")
	f.focus(fStart)
	f.keys("25:00", "enter")

	m := f.model()
	if m.mode != modeForm || m.form.focus != fStart {
		t.Fatalf("mode %v focus %d, want the form with Start focused", m.mode, m.form.focus)
	}
	screen := f.m.View()
	_, y := find(t, screen, "Start")
	if line := strings.Split(screen, "\n")[y+1]; !strings.Contains(line, "time") {
		t.Errorf("the line under Start should explain the error: %q", line)
	}
}

func TestEditTimedToAllDay(t *testing.T) {
	f := newFixture(t)
	f.keys("enter", "j", "e") // event mode, Lunch, edit
	f.focus(fAllDay)
	f.keys("h") // no → yes
	f.focus(fTitle)
	f.keys("enter")

	events, _ := f.svc.Agenda(context.Background(), time.Date(2026, 11, 12, 0, 0, 0, 0, f.ny),
		time.Date(2026, 11, 13, 0, 0, 0, 0, f.ny), service.Filter{})
	for _, e := range events {
		if e.Title == "Lunch with Sam" && !e.AllDay {
			t.Error("Lunch should now be all-day")
		}
	}
}

// focus moves the form's cursor to field i with ctrl+j/ctrl+k, like a user.
func (f *fixture) focus(i int) {
	f.t.Helper()
	for n := 0; f.model().form.focus != i; n++ {
		if n > fieldCount {
			f.t.Fatalf("can't reach field %d", i)
		}
		f.send(tea.KeyMsg{Type: tea.KeyCtrlJ})
	}
}
