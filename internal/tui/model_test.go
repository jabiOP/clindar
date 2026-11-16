package tui

import (
	"context"
	"os"
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
