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
	f.keys(" ")           // space opens the list…
	f.keys("enter")       // …and enter chooses "weekly" again (inside a list, enter chooses); then save:
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

func TestColorPickerSetsAndClearsTheColor(t *testing.T) {
	f := newFixture(t)
	f.openFullForm()
	f.keys("Dinner")
	f.focus(fColor)
	f.keys("l") // Calendar default → Tomato
	f.focus(fTitle)
	f.keys("enter")

	dinner := func() model.Event {
		t.Helper()
		events, _ := f.svc.Agenda(context.Background(), time.Date(2026, 11, 12, 0, 0, 0, 0, f.ny),
			time.Date(2026, 11, 13, 0, 0, 0, 0, f.ny), service.Filter{})
		for _, e := range events {
			if e.Title == "Dinner" {
				return e
			}
		}
		t.Fatal("no Dinner on Nov 12")
		return model.Event{}
	}
	if got := dinner().ColorID; got != "11" {
		t.Fatalf("ColorID = %q, want 11 (Tomato)", got)
	}

	f.keys("enter")
	for f.model().dayEvents()[f.model().dayCursor].Title != "Dinner" {
		f.keys("j")
	}
	f.keys("e")
	f.focus(fColor)
	f.keys("h") // → Calendar default
	f.focus(fTitle)
	f.keys("enter")

	if got := dinner().ColorID; got != "" {
		t.Errorf("ColorID = %q, want \"\" (calendar default)", got)
	}
	ops, _ := f.st.PendingOps(context.Background(), testutil.Account)
	for _, op := range ops {
		if op.Payload.Title == "Dinner" && op.Payload.ColorID != "" {
			t.Errorf("queued op payload ColorID = %q, want \"\"", op.Payload.ColorID)
		}
	}
}

// Enter saves the form even when the cursor is on a (closed) picker; Space opens it.
func TestEnterOnAPickerSaves(t *testing.T) {
	f := newFixture(t)
	f.openFullForm()
	f.keys("Yoga")
	f.focus(fRepeat)
	f.keys("l", "enter") // none → daily, then save

	if m := f.model(); m.mode != modeNormal {
		t.Fatalf("mode = %v, want the form saved and closed", m.mode)
	}
	if got := f.titlesOn(13); !slices.Contains(got, "Yoga") {
		t.Errorf("Nov 13 = %v, want a daily Yoga", got)
	}
}

// [Save] (ctrl+s) saves even while a picker's list is open, keeping the highlighted choice.
func TestSaveButtonWithAListOpen(t *testing.T) {
	f := newMouseFixture(t)
	f.click(f.button("a"))
	f.click(f.button("tab"))
	f.keys("Yoga")
	f.focus(fRepeat)
	f.keys(" ", "j") // open the list, highlight "daily"
	if !f.model().form.pickerOpen() {
		t.Fatal("space should open the Repeat list")
	}

	f.click(f.button("ctrl+s"))

	if m := f.model(); m.mode != modeNormal {
		t.Fatalf("mode = %v, want saved", m.mode)
	}
	if got := f.titlesOn(13); !slices.Contains(got, "Yoga") {
		t.Errorf("Nov 13 = %v, want a daily Yoga", got)
	}
}
