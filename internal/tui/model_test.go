package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii) // golden files contain no ANSI codes
	os.Exit(m.Run())
}

type fixture struct {
	t     *testing.T
	svc   *service.Service
	st    *store.Store
	ny    *time.Location
	m     tea.Model
	clock time.Time // what Deps.Now returns; tests may move it
}

// newFixture: Thursday Nov 12 2026, a real service over a temp store with a
// weekly standup, a lunch, and a 2-day trip.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWith(t, Deps{})
}

// newFixtureWith is newFixture with extra settings (e.g. Mouse) merged into Deps.
func newFixtureWith(t *testing.T, extra Deps) *fixture {
	t.Helper()
	ny := testutil.NY(t)
	ctx := context.Background()
	st := testutil.NewStore(t)
	st.UpsertCalendar(ctx, model.Calendar{AccountID: testutil.Account, ID: testutil.Calendar, Name: "Me",
		Primary: true, Selected: true, AccessRole: "owner", TimeZone: "America/New_York"})
	standup := testutil.Timed("std", "Standup", time.Date(2026, 11, 2, 9, 0, 0, 0, ny), 15*time.Minute)
	standup.Recurrence = []string{"RRULE:FREQ=WEEKLY;BYDAY=MO,TH"}
	for _, e := range []model.Event{
		standup,
		testutil.Timed("lunch", "Lunch with Sam", time.Date(2026, 11, 12, 12, 0, 0, 0, ny), time.Hour),
		testutil.AllDay("trip", "Trip to Lisbon", 2026, time.November, 20, 2),
	} {
		st.PutEvent(ctx, e)
	}
	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny)
	svc := service.New(st, testutil.Account, func() time.Time { return now }, "none")
	f := &fixture{t: t, svc: svc, st: st, ny: ny, clock: now}
	d := extra
	d.Service, d.Now, d.WeekStart = svc, func() time.Time { return f.clock }, time.Monday
	m := New(ctx, d)
	f.m = f.drain(m, m.Init())
	f.send(tea.WindowSizeMsg{Width: 84, Height: 20})
	return f
}

// put stores e and reloads, as if a sync brought it in.
func (f *fixture) put(e model.Event) {
	f.t.Helper()
	if err := f.st.PutEvent(context.Background(), e); err != nil {
		f.t.Fatal(err)
	}
	f.send(f.model().load()())
}

// drain runs cmd and feeds its messages back into the model until nothing is
// left, which is what the Bubble Tea runtime would do, but synchronously.
func (f *fixture) drain(m tea.Model, cmd tea.Cmd) tea.Model {
	f.t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 100 {
			f.t.Fatal("drain: command loop never settled")
		}
		switch msg := cmd().(type) {
		case nil:
			return m
		case tea.BatchMsg:
			for _, c := range msg {
				m = f.drain(m, c)
			}
			return m
		default:
			m, cmd = m.Update(msg)
		}
	}
	return m
}

func (f *fixture) send(msgs ...tea.Msg) {
	f.t.Helper()
	for _, msg := range msgs {
		var cmd tea.Cmd
		f.m, cmd = f.m.Update(msg)
		f.m = f.drain(f.m, cmd)
	}
}

// keys sends each string as one key press ("tab", "enter", "esc", or runes).
func (f *fixture) keys(names ...string) {
	f.t.Helper()
	for _, n := range names {
		var k tea.KeyMsg
		switch n {
		case "tab":
			k = tea.KeyMsg{Type: tea.KeyTab}
		case "enter":
			k = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			k = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(n)}
		}
		f.send(k)
	}
}

func (f *fixture) model() Model { return f.m.(Model) }

func (f *fixture) titlesOn(day int) []string {
	f.t.Helper()
	from := time.Date(2026, 11, day, 0, 0, 0, 0, f.ny)
	events, err := f.svc.Agenda(context.Background(), from, from.AddDate(0, 0, 1), service.Filter{})
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, e := range events {
		out = append(out, e.Title)
	}
	return out
}

func TestGridView(t *testing.T) {
	f := newFixture(t)
	testutil.Golden(t, "grid", f.m.View())
}

func TestListView(t *testing.T) {
	f := newFixture(t)
	f.keys("tab")
	testutil.Golden(t, "list", f.m.View())
}

func TestGridNavigationScrollsWindow(t *testing.T) {
	f := newFixture(t)
	start := f.model().gridStart

	f.keys("j", "j", "j", "j") // four weeks down: past the bottom row

	m := f.model()
	if want := time.Date(2026, 12, 10, 0, 0, 0, 0, f.ny); !m.selected.Equal(want) {
		t.Errorf("selected = %v, want %v", m.selected, want)
	}
	if !m.gridStart.After(start) || m.selected.Before(m.gridStart) || !m.selected.Before(m.gridStart.AddDate(0, 0, gridDays)) {
		t.Errorf("grid [%v +28d) doesn't show selected %v", m.gridStart, m.selected)
	}

	f.keys("t")
	if !f.model().selected.Equal(time.Date(2026, 11, 12, 0, 0, 0, 0, f.ny)) {
		t.Errorf("t didn't return to today")
	}
}

func TestGotoDateResetsListCursor(t *testing.T) {
	f := newFixture(t)
	f.keys("tab", "j") // list from Thu 12; cursor on the 2nd event (Lunch)
	if f.model().cursor != 1 {
		t.Fatalf("setup: cursor = %d, want 1", f.model().cursor)
	}

	f.keys("g", "mon", "enter") // go to Mon Nov 16

	m := f.model()
	if !m.selected.Equal(time.Date(2026, 11, 16, 0, 0, 0, 0, f.ny)) || m.cursor != 0 {
		t.Errorf("selected = %v, cursor = %d; want Nov 16 and the first event", m.selected, m.cursor)
	}
}

func TestJKMoveFourWeeks(t *testing.T) {
	f := newFixture(t)
	start, sel := f.model().gridStart, f.model().selected

	f.keys("J")
	if m := f.model(); !m.gridStart.Equal(start.AddDate(0, 0, 28)) || !m.selected.Equal(sel.AddDate(0, 0, 28)) {
		t.Errorf("after J: gridStart %v selected %v", m.gridStart, m.selected)
	}
	f.keys("K")
	if m := f.model(); !m.gridStart.Equal(start) || !m.selected.Equal(sel) {
		t.Errorf("after K: gridStart %v selected %v", m.gridStart, m.selected)
	}
}

func TestListFillsTheScreen(t *testing.T) {
	f := newFixture(t) // height 20: 19 rows of body
	f.keys("tab")

	lines := strings.Split(f.m.View(), "\n")
	used := 0
	for _, l := range lines[:len(lines)-1] { // without the status bar
		if strings.TrimSpace(l) != "" {
			used++
		}
	}
	if used != 19 {
		t.Errorf("list uses %d of 19 rows:\n%s", used, f.m.View())
	}
}

func TestListScrollsPastTheEnd(t *testing.T) {
	f := newFixture(t)
	f.keys("tab")
	for range 40 { // ~20 weeks of twice-weekly standups: far past the first 4 weeks
		f.keys("j")
	}
	if got := f.model().cursor; got != 40 {
		t.Errorf("cursor = %d, want 40 (the list must keep loading)", got)
	}
}

func TestListScrollsBackBeforeTheStart(t *testing.T) {
	f := newFixture(t)
	f.keys("tab", "k") // on Thu 12's first entry; k goes to the day before the list's start

	m := f.model()
	got := m.entries()[m.cursor]
	if want := time.Date(2026, 11, 9, 9, 0, 0, 0, f.ny); !got.Start.Equal(want) {
		t.Errorf("cursor on %q at %v, want Mon 9's standup", got.Title, got.Start)
	}
}

func TestListPagesWithJK(t *testing.T) {
	f := newFixture(t)
	f.keys("tab", "J")
	if got := f.model().cursor; got != 17 { // a page = height − 3
		t.Fatalf("after J cursor = %d, want 17", got)
	}
	f.keys("K")
	if got := f.model().cursor; got != 0 {
		t.Errorf("after K cursor = %d, want 0", got)
	}
}

func TestEnterStartsEventMode(t *testing.T) {
	f := newFixture(t)
	f.keys("enter")

	m := f.model()
	if !m.dayMode || m.view != viewGrid {
		t.Fatalf("dayMode = %v, view = %v", m.dayMode, m.view)
	}
	if !strings.Contains(f.m.View(), "▶09:00") {
		t.Errorf("the first event should be marked:\n%s", f.m.View())
	}
}

func TestEventModeMovesAndOpens(t *testing.T) {
	f := newFixture(t)
	f.keys("enter", "j", "enter")

	if m := f.model(); m.mode != modeDetail || m.detail.Title != "Lunch with Sam" {
		t.Errorf("mode = %v, detail = %q", m.mode, m.detail.Title)
	}
}

func TestEventModeActsLikeTheDetailsBox(t *testing.T) {
	f := newFixture(t)
	f.keys("enter", "d") // Standup is a recurring instance: delete asks for a scope

	if m := f.model(); m.mode != modeScope {
		t.Errorf("mode = %v, want the scope picker", m.mode)
	}
}

func TestEventModeOnEmptyDaySaysSo(t *testing.T) {
	f := newFixture(t)
	f.keys("l", "enter") // Fri 13: nothing

	if m := f.model(); m.dayMode || !strings.Contains(m.message, "no events") {
		t.Errorf("dayMode = %v, message = %q", m.dayMode, m.message)
	}
}

func TestLeavingEventMode(t *testing.T) {
	f := newFixture(t)
	f.keys("enter", "esc")
	if f.model().dayMode {
		t.Fatal("esc should leave event mode")
	}
	f.keys("enter", "l")
	if m := f.model(); m.dayMode || !m.selected.Equal(time.Date(2026, 11, 13, 0, 0, 0, 0, f.ny)) {
		t.Errorf("l should leave event mode and move a day: dayMode = %v, selected = %v", m.dayMode, m.selected)
	}
}

func TestEventModeScrollsThroughHiddenEvents(t *testing.T) {
	f := newFixture(t) // height 20: each cell shows 3 events
	for i := 1; i <= 6; i++ {
		f.put(testutil.Timed(fmt.Sprintf("x%d", i), fmt.Sprintf("E%d", i),
			time.Date(2026, 11, 13, 7+i, 0, 0, 0, f.ny), 30*time.Minute))
	}
	f.keys("l", "enter")
	for range 5 {
		f.keys("j")
	}

	screen := f.m.View()
	if !strings.Contains(screen, "E6") || !strings.Contains(screen, "↑") {
		t.Errorf("the 6th event and the ↑/↓ counter should show:\n%s", screen)
	}
}

func TestEscInListReturnsToGrid(t *testing.T) {
	f := newFixture(t)
	f.keys("tab", "esc")
	if f.model().view != viewGrid {
		t.Error("esc in the list should return to the grid")
	}
}
