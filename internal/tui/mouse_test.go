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

func TestClickSelectsDayThenOpensList(t *testing.T) {
	f := newMouseFixture(t)

	f.click(f.dayAt(17))
	if got := f.model().selected; !got.Equal(time.Date(2026, 11, 17, 0, 0, 0, 0, f.ny)) {
		t.Fatalf("selected = %v, want Nov 17", got)
	}
	f.click(f.dayAt(17))

	if f.model().view != viewList {
		t.Error("clicking the selected day should open the list")
	}
}

func (f *fixture) wheelDown() {
	f.send(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
}

func TestWheelMovesOneDayAndIsThrottled(t *testing.T) {
	f := newMouseFixture(t)
	sel := f.model().selected

	f.wheelDown()
	f.wheelDown() // same instant: dropped
	if got := f.model().selected; !got.Equal(sel.AddDate(0, 0, 1)) {
		t.Fatalf("selected = %v, want one day later", got)
	}
	f.clock = f.clock.Add(150 * time.Millisecond)
	f.wheelDown()

	if got := f.model().selected; !got.Equal(sel.AddDate(0, 0, 2)) {
		t.Errorf("selected = %v, want two days later", got)
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

	f.click(f.button("a")) // [+ Add]
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

// With the mouse on, the delete confirmation still names its keys, and a key
// other than y cancels visibly rather than silently.
func TestDeleteConfirmShowsKeysAndSaysWhenCancelled(t *testing.T) {
	f := newMouseFixture(t)
	f.keys("tab", "j", "d") // list from Thu 12: Standup, then Lunch (a one-off)

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
