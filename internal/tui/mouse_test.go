package tui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/testutil"
)

// newMouseFixture is the usual fixture (Thu Nov 12 2026; Standup Mon+Thu 09:00,
// Lunch Thu 12, Lisbon trip Fri 20–Sat 21) with the mouse on, in a terminal
// wide enough for titles next to the ◂ ▸ handles.
func newMouseFixture(t *testing.T) *fixture {
	f := newFixtureWith(t, Deps{Mouse: true})
	f.send(tea.WindowSizeMsg{Width: 112, Height: 20})
	return f
}

// where returns the middle of the topmost on-screen target matching ok.
func (f *fixture) where(ok func(target) bool) (int, int) {
	f.t.Helper()
	_, hits := f.model().layout()
	for i := len(hits) - 1; i >= 0; i-- {
		if h := hits[i]; ok(h.t) {
			return h.x + h.w/2, h.y
		}
	}
	f.t.Fatal("no such target on screen")
	return 0, 0
}

func (f *fixture) dayAt(d int) (int, int) {
	day := time.Date(2026, 11, d, 0, 0, 0, 0, f.ny)
	return f.where(func(t target) bool { return t.kind == targetDay && t.day.Equal(day) })
}

func (f *fixture) eventAt(title string, d int, p part) (int, int) {
	day := time.Date(2026, 11, d, 0, 0, 0, 0, f.ny)
	return f.where(func(t target) bool {
		return t.kind == targetEvent && t.event.Title == title && t.day.Equal(day) && t.part == p
	})
}

func (f *fixture) button(key string) (int, int) {
	return f.where(func(t target) bool { return t.kind == targetKey && t.key == key })
}

func (f *fixture) click(x, y int) {
	f.send(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
}

func (f *fixture) dragTo(fromX, fromY, toX, toY int) {
	f.send(tea.MouseMsg{X: fromX, Y: fromY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		tea.MouseMsg{X: toX, Y: toY, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
		tea.MouseMsg{X: toX, Y: toY, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})
}

func TestMouseGridGolden(t *testing.T) {
	f := newMouseFixture(t)
	testutil.Golden(t, "grid_mouse", f.m.View())
}

func TestClickSelectedDayStartsEventMode(t *testing.T) {
	f := newMouseFixture(t)

	f.click(f.dayAt(16)) // Mon 16: a standup
	if got := f.model().selected; !got.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, f.ny)) {
		t.Fatalf("selected = %v, want Nov 16", got)
	}
	f.click(f.dayAt(16))

	if m := f.model(); m.view != viewGrid || !m.dayMode {
		t.Errorf("view = %v, dayMode = %v; clicking the selected day should enter event mode", m.view, m.dayMode)
	}
}

// Like Enter: a second click on a day with no events says so instead of entering
// an empty event mode.
func TestClickSelectedEmptyDaySaysSo(t *testing.T) {
	f := newMouseFixture(t)
	f.click(f.dayAt(17)) // Tue 17: nothing
	f.click(f.dayAt(17))

	if m := f.model(); m.dayMode || !strings.Contains(m.message, "no events") {
		t.Errorf("dayMode = %v, message = %q", m.dayMode, m.message)
	}
}

func (f *fixture) wheelDown() {
	f.send(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
}

// The wheel is off: trackpad swipes made it unusable even throttled (user, after Task 14.4).
func TestWheelDoesNothing(t *testing.T) {
	f := newMouseFixture(t)
	before := f.model()

	for range 5 {
		f.wheelDown()
		f.clock = f.clock.Add(time.Second)
	}
	f.keys("tab") // the list too
	f.wheelDown()

	if m := f.model(); !m.selected.Equal(before.selected) || !m.gridStart.Equal(before.gridStart) || m.cursor != 0 {
		t.Errorf("selected %v gridStart %v cursor %d: the wheel must not move anything", m.selected, m.gridStart, m.cursor)
	}
}

func TestClickEventOpensDetailWithButtons(t *testing.T) {
	f := newMouseFixture(t)

	f.click(f.eventAt("Lunch with Sam", 12, partBody))
	if m := f.model(); m.mode != modeDetail || m.detail.Title != "Lunch with Sam" {
		t.Fatalf("mode = %v, detail = %q", m.mode, m.detail.Title)
	}
	f.click(f.button("e"))

	if m := f.model(); m.mode != modeForm || m.form.editing == nil || m.form.editing.Title != "Lunch with Sam" {
		t.Errorf("[Edit] should open the edit form; mode = %v", m.mode)
	}
}

func TestClickOutsideClosesDetail(t *testing.T) {
	f := newMouseFixture(t)
	f.click(f.eventAt("Lunch with Sam", 12, partBody))

	f.click(0, 0)

	if f.model().mode != modeNormal {
		t.Errorf("mode = %v, want normal", f.model().mode)
	}
}

func TestDragMovesEventToAnotherDay(t *testing.T) {
	f := newMouseFixture(t)
	x, y := f.eventAt("Lunch with Sam", 12, partBody)
	toX, toY := f.dayAt(14)

	f.dragTo(x, y, toX, toY)

	if got := f.titlesOn(14); !slices.Contains(got, "Lunch with Sam") {
		t.Errorf("Nov 14 = %v, want lunch", got)
	}
	if got := f.titlesOn(12); slices.Contains(got, "Lunch with Sam") {
		t.Errorf("Nov 12 = %v, lunch should have moved", got)
	}
}

// It never hovers, so it also proves edges are grabbable without hover
// (terminals that don't report motion without a button held).
func TestDragEndHandleLengthensTrip(t *testing.T) {
	f := newMouseFixture(t)
	x, y := f.eventAt("Trip to Lisbon", 21, partEnd)
	toX, toY := f.dayAt(23)

	f.dragTo(x, y, toX, toY)

	for _, d := range []int{20, 21, 22, 23} {
		if got := f.titlesOn(d); !slices.Contains(got, "Trip to Lisbon") {
			t.Errorf("Nov %d = %v, want the trip", d, got)
		}
	}
	if got := f.titlesOn(24); slices.Contains(got, "Trip to Lisbon") {
		t.Errorf("Nov 24 = %v: the trip should end on the 23rd", got)
	}
}

func TestDragRecurringAsksForScope(t *testing.T) {
	f := newMouseFixture(t)
	x, y := f.eventAt("Standup", 12, partBody)
	toX, toY := f.dayAt(13)

	f.dragTo(x, y, toX, toY)
	if f.model().mode != modeScope {
		t.Fatalf("mode = %v, want the scope picker", f.model().mode)
	}
	f.click(f.button("t")) // [This event]

	if got := f.titlesOn(13); !slices.Contains(got, "Standup") {
		t.Errorf("Nov 13 = %v, want the moved standup", got)
	}
	if got := f.titlesOn(19); !slices.Contains(got, "Standup") {
		t.Errorf("Nov 19 = %v: other instances must stay", got)
	}
}

func TestEscCancelsDrag(t *testing.T) {
	f := newMouseFixture(t)
	x, y := f.eventAt("Lunch with Sam", 12, partBody)
	toX, toY := f.dayAt(14)

	f.send(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		tea.MouseMsg{X: toX, Y: toY, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	f.keys("esc")
	f.send(tea.MouseMsg{X: toX, Y: toY, Button: tea.MouseButtonNone, Action: tea.MouseActionRelease})

	if got := f.titlesOn(12); !slices.Contains(got, "Lunch with Sam") {
		t.Errorf("Nov 12 = %v: a cancelled drag must not move anything", got)
	}
}

func TestStatusBarAndFormButtons(t *testing.T) {
	f := newMouseFixture(t)

	f.click(f.button("a")) // [+ Add]: quick add
	if f.model().mode != modeQuick {
		t.Fatalf("mode = %v, want quick add", f.model().mode)
	}
	f.click(f.button("tab")) // [More…]: the full form
	if f.model().mode != modeForm {
		t.Fatalf("mode = %v, want the add form", f.model().mode)
	}
	f.click(f.where(func(t target) bool { return t.kind == targetField && t.field == fWhere }))
	if f.model().form.focus != fWhere {
		t.Errorf("focus = %d, want the Where field", f.model().form.focus)
	}
	f.click(f.button("esc")) // [Cancel]

	if f.model().mode != modeNormal {
		t.Errorf("mode = %v, want normal after [Cancel]", f.model().mode)
	}
}

func TestMouseOffIgnoresMouse(t *testing.T) {
	f := newFixture(t)
	before := f.model().selected

	f.send(tea.MouseMsg{X: 50, Y: 10, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})

	if !f.model().selected.Equal(before) {
		t.Error("with mouse off, clicks must do nothing")
	}
}

// hover moves the pointer without a button pressed (all-motion mode).
func (f *fixture) hover(x, y int) {
	f.send(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonNone, Action: tea.MouseActionMotion})
}

func TestHoverOnEdgeRevealsHandle(t *testing.T) {
	f := newMouseFixture(t)
	if strings.Contains(f.m.View(), "▸") {
		t.Fatal("no handle may show before hovering")
	}

	f.hover(f.eventAt("Trip to Lisbon", 21, partEnd))
	if !strings.Contains(f.m.View(), "▸") {
		t.Error("hovering the trip's end edge should show ▸")
	}
	f.hover(f.eventAt("Trip to Lisbon", 21, partBody))
	if strings.Contains(f.m.View(), "▸") {
		t.Error("moving off the edge should hide ▸")
	}
}

// A click on [Delete] asks first (no undo); the question names its keys, and
// a key other than y cancels visibly rather than silently.
func TestDeleteConfirmShowsKeysAndSaysWhenCancelled(t *testing.T) {
	f := newMouseFixture(t)
	f.keys("tab", "j", "enter") // list from Thu 12: Standup, then Lunch (a one-off): its details
	f.click(f.button("delete"))

	if screen := f.m.View(); !strings.Contains(screen, "(y/n)") || !strings.Contains(screen, "[Delete]") {
		t.Fatalf("confirm pop-up should show (y/n) and the buttons:\n%s", screen)
	}
	f.keys("enter")

	if m := f.model(); m.mode != modeNormal || !strings.Contains(m.message, "not deleted") {
		t.Errorf("mode = %v, message = %q; want back to normal with \"not deleted\"", m.mode, m.message)
	}
	if got := f.titlesOn(12); !slices.Contains(got, "Lunch with Sam") {
		t.Errorf("Nov 12 = %v: lunch must still be there", got)
	}
}

func TestEdgeReleasedOnSameDayDoesNothing(t *testing.T) {
	f := newMouseFixture(t)
	f.click(f.eventAt("Trip to Lisbon", 21, partEnd))

	if m := f.model(); m.mode != modeNormal {
		t.Errorf("mode = %v, want normal (no details box)", m.mode)
	}
}

func TestEdgeOfReadOnlyEventShowsReasonOnly(t *testing.T) {
	f := newMouseFixture(t)
	f.st.UpsertCalendar(context.Background(), model.Calendar{AccountID: testutil.Account, ID: "hol",
		Name: "Holidays", Selected: true, AccessRole: "reader"})
	hol := testutil.AllDay("thx", "Thanksgiving", 2026, time.November, 26, 1)
	hol.CalendarID = "hol"
	f.put(hol)

	f.click(f.eventAt("Thanksgiving", 26, partEnd))

	if m := f.model(); m.mode != modeNormal || !strings.Contains(m.message, "read-only") {
		t.Errorf("mode = %v, message = %q", m.mode, m.message)
	}
}

func TestCalendarsButtonOpensMenuAndRowClickToggles(t *testing.T) {
	f := newMouseFixture(t)
	f.click(f.button("c"))
	if m := f.model(); m.mode != modeCalendars {
		t.Fatalf("mode = %v, want the calendar menu", m.mode)
	}

	f.click(f.where(func(t target) bool { return t.kind == targetOption && t.field == 0 })) // the "Me" row

	if got := f.titlesOn(12); len(got) != 0 {
		t.Errorf("Nov 12 = %v: clicking the row should hide Me", got)
	}
	if !strings.Contains(f.m.View(), "[ ]") {
		t.Errorf("the row should show unticked:\n%s", f.m.View())
	}
}

func (f *fixture) emptyAt(d int) (int, int) {
	day := time.Date(2026, 11, d, 0, 0, 0, 0, f.ny)
	return f.where(func(t target) bool { return t.kind == targetEmpty && t.day.Equal(day) })
}

func TestClickEmptySpaceOpensQuickAdd(t *testing.T) {
	f := newMouseFixture(t)
	f.click(f.emptyAt(17))

	if m := f.model(); m.mode != modeQuick || !strings.Contains(f.m.View(), "Tue 17 Nov, all day") {
		t.Errorf("mode = %v:\n%s", m.mode, f.m.View())
	}
}

func TestDragAcrossEmptyDaysAddsASpan(t *testing.T) {
	f := newMouseFixture(t)
	x1, y1 := f.emptyAt(17)
	x2, y2 := f.emptyAt(19)
	f.dragTo(x1, y1, x2, y2)
	f.keys("Conference", "enter")

	for d, want := range map[int]bool{16: false, 17: true, 18: true, 19: true, 20: false} {
		if got := slices.Contains(f.titlesOn(d), "Conference"); got != want {
			t.Errorf("Nov %d has Conference = %v, want %v", d, got, want)
		}
	}
}
