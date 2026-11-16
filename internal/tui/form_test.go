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
	"github.com/jabiOP/clindar/internal/timeparse"
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

func TestEscOnAnUntouchedFormCloses(t *testing.T) {
	f := newFixture(t)
	f.keys("enter", "j", "e") // edit Lunch, change nothing
	f.keys("esc")
	if m := f.model(); m.mode != modeNormal {
		t.Errorf("mode = %v, want closed", m.mode)
	}
}

func TestEscWithChangesAsksBeforeDiscarding(t *testing.T) {
	f := newFixture(t)
	f.openFullForm()
	f.keys("Dentist", "esc")
	if m := f.model(); m.mode != modeForm || !strings.Contains(f.m.View(), "enter discard") {
		t.Fatalf("mode = %v; want the form, asking:\n%s", m.mode, f.m.View())
	}

	f.keys("esc") // esc esc: keep editing, nothing lost
	if m := f.model(); m.mode != modeForm || m.form.value(fTitle) != "Dentist" || m.form.confirmDiscard {
		t.Fatalf("after esc esc: mode = %v, title = %q", m.mode, m.form.value(fTitle))
	}
	f.keys("esc", "x") // another key also keeps editing, and isn't typed
	if m := f.model(); m.mode != modeForm || m.form.value(fTitle) != "Dentist" {
		t.Fatalf("after esc x: mode = %v, title = %q", m.mode, m.form.value(fTitle))
	}
	f.keys("esc", "enter")
	if m := f.model(); m.mode != modeNormal {
		t.Errorf("after esc enter: mode = %v, want closed", m.mode)
	}
	if got := f.titlesOn(12); slices.Contains(got, "Dentist") {
		t.Error("discarding must not save")
	}
}

func TestFormHintSaysHowToDiscard(t *testing.T) {
	f := newFixture(t)
	f.openFullForm()
	if !strings.Contains(f.m.View(), "esc then enter discard") {
		t.Errorf("hint should say how to discard:\n%s", f.m.View())
	}
}

// moreCalendars adds a writable Work and a read-only Holidays calendar, and reloads.
func (f *fixture) moreCalendars() {
	f.t.Helper()
	for _, c := range []model.Calendar{
		{AccountID: testutil.Account, ID: "work", Name: "Work", Selected: true, AccessRole: "writer"},
		{AccountID: testutil.Account, ID: "holidays", Name: "Holidays", Selected: true, AccessRole: "reader"},
	} {
		if err := f.st.UpsertCalendar(context.Background(), c); err != nil {
			f.t.Fatal(err)
		}
	}
	f.send(f.model().load()())
}

func TestEditFormListsWritableCalendars(t *testing.T) {
	f := newFixture(t)
	f.moreCalendars()
	f.keys("enter", "j", "e") // edit Lunch
	f.focus(fCalendar)
	f.keys(" ")

	var labels []string
	for _, o := range f.model().form.pickers[fCalendar].options {
		labels = append(labels, o.label)
	}
	if !slices.Equal(labels, []string{"Me", "Work"}) {
		t.Errorf("options = %v, want the writable calendars", labels)
	}
}

func TestEditFormMovesAnEvent(t *testing.T) {
	f := newFixture(t)
	f.moreCalendars()
	f.keys("enter", "j", "e")
	f.focus(fCalendar)
	f.keys("l", "enter") // Me → Work, save

	if m := f.model(); m.mode != modeNormal {
		t.Fatalf("mode = %v, want saved", m.mode)
	}
	if e, err := f.svc.Get(context.Background(), "lunch"); err != nil || e.CalendarID != "work" {
		t.Errorf("lunch in %q (%v), want work", e.CalendarID, err)
	}
}

func TestEditFormLocksCalendarOnAnInvitation(t *testing.T) {
	f := newFixture(t)
	f.moreCalendars()
	inv := testutil.Timed("inv", "Their meeting", time.Date(2026, 11, 13, 10, 0, 0, 0, f.ny), time.Hour)
	inv.Attendees = []model.Attendee{{Email: testutil.Account, Self: true}, {Email: "boss@example.com", Organizer: true}}
	f.put(inv)

	form := editEventForm(inv, f.ny, f.model().calList, timeparse.Parser{})

	if got := form.locked[fCalendar]; got != "can't move: an invitation" {
		t.Errorf("locked = %q", got)
	}
}

func TestMovingAnOccurrenceOffersOnlyTheWholeSeries(t *testing.T) {
	f := newFixture(t)
	f.moreCalendars()
	f.keys("enter", "e") // the Standup occurrence on Thu Nov 12
	f.focus(fCalendar)
	f.keys("l", "enter")

	if m := f.model(); m.mode != modeScope || m.allowedScopes != "a" || !strings.Contains(f.m.View(), "Move the whole series?") {
		t.Fatalf("mode %v scopes %q:\n%s", m.mode, m.allowedScopes, f.m.View())
	}
	f.keys("a")
	if e, err := f.svc.Get(context.Background(), "std"); err != nil || e.CalendarID != "work" {
		t.Errorf("series in %q (%v), want work", e.CalendarID, err)
	}
}

// An event stored in two calendars (one shared with you) is saved in the copy
// being edited, not refused as ambiguous.
func TestEditingOneOfTwoCopies(t *testing.T) {
	f := newFixture(t)
	f.moreCalendars()
	lunch, _ := f.st.GetEvent(context.Background(), testutil.Account, testutil.Calendar, "lunch")
	lunch.CalendarID = "work"
	f.put(lunch)

	m, cmd := f.model().applyChanges(lunch, service.Changes{Title: new("Lunch moved")})
	f.m = f.drain(m, cmd)

	if got, _ := f.st.GetEvent(context.Background(), testutil.Account, "work", "lunch"); got.Title != "Lunch moved" {
		t.Errorf("work copy = %q (status: %q)", got.Title, f.model().message)
	}
	if got, _ := f.st.GetEvent(context.Background(), testutil.Account, testutil.Calendar, "lunch"); got.Title != "Lunch with Sam" {
		t.Errorf("the other copy changed: %q", got.Title)
	}
}

func TestEscOnTheScopePickerReturnsToTheForm(t *testing.T) {
	f := newFixture(t)
	f.moreCalendars()
	f.keys("enter", "e") // the Standup occurrence
	f.focus(fCalendar)
	f.keys("l", "enter")
	if f.model().mode != modeScope {
		t.Fatalf("mode = %v, want the scope picker", f.model().mode)
	}

	f.keys("esc")

	if m := f.model(); m.mode != modeForm || m.form.value(fCalendar) != "work" {
		t.Errorf("mode = %v, calendar = %q; want the form back, unchanged", m.mode, m.form.value(fCalendar))
	}
}
