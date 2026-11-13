# Phase 10: TUI

The full-screen interface: a scrollable **4-week grid** and an **agenda list** (design §6).

```bash
go get github.com/charmbracelet/bubbletea@v1.3.10 github.com/charmbracelet/bubbles@v1.0.0 github.com/mattn/go-runewidth@v0.0.31
```
**Do not** `go get` any `github.com/charmbracelet/x/...` module (README primer).

Read the README's Bubble Tea section and the Bubble Tea tutorial before starting. The structure:

| File | Role |
|---|---|
| `grid.go`, `list.go` | **Pure** render functions: data in, string out. Golden-tested. |
| `model.go` | Model state, messages, commands (`load`, `syncCmd`, `tick`, `act`), `Update` dispatch, `View`, `Run` |
| `nav.go` | Key handling for moving around |
| `actions.go` | Key handling for add/edit/delete/RSVP/scope picking |
| `form.go` | The add/edit form (produces a `service.Draft` or `service.Changes`) |
| `keys.go` | All key bindings, in one place, which also feeds the help screen |

Rules:
- `Update` never blocks. Service calls go in `tea.Cmd`s and return messages (`loadedMsg`, `actionDoneMsg`, …).
- The TUI writes **only** through `service` (via `m.act`), exactly like the CLI.
- Selection markers (`▶`) are visible **text** as well as color. That helps colorblind users and monochrome terminals, and it
  makes golden files meaningful.
- Text inputs use a **static cursor**: no blink timers. That keeps tests synchronous.

**Golden files:** `testutil.Golden(t, name, got)` compares with `testdata/<name>.golden`.
Run `go test ./internal/tui -update` to (re)write them, then **open the .golden files and check they look right**.
A golden test only proves the output didn't *change*. You are the one who decides it was *correct*.

---

## Task 10.1: Golden helper, styles, and the grid

**Goal:** `renderGrid` draws 4 rows × 7 days. Each cell has a day label, then `HH:MM title` lines in the calendar's color,
with `+N more` on overflow. Today is marked "today" and the selected day is marked `▶`. The first day of a month shows the month name.

**Files:** create `internal/testutil/golden.go`, `internal/tui/styles.go`, `internal/tui/grid_test.go`, `internal/tui/grid.go`

**Steps**
1. Golden helper:
```go
package testutil

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// Golden compares got with testdata/<name>.golden. Run `go test -update` to
// (re)write the file, then REVIEW THE DIFF before committing.
func Golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run `go test -update` to create it)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (rerun with -update if the change is intended)\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
```
2. Test:
```go
package tui

import (
	"testing"
	"time"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/testutil"
)

func TestWeekStart(t *testing.T) {
	ny := testutil.NY(t)
	thu := time.Date(2026, 11, 12, 15, 0, 0, 0, ny)
	if got := weekStart(thu, time.Monday); !got.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, ny)) {
		t.Errorf("monday start = %v", got)
	}
	if got := weekStart(thu, time.Sunday); !got.Equal(time.Date(2026, 11, 8, 0, 0, 0, 0, ny)) {
		t.Errorf("sunday start = %v", got)
	}
}

func TestRenderGridCells(t *testing.T) {
	ny := testutil.NY(t)
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
	var events []model.Event
	for h := 8; h < 13; h++ { // five events on Tuesday: more than a cell can show
		events = append(events, testutil.Timed("e", "Meeting", time.Date(2026, 11, 10, h, 0, 0, 0, ny), time.Hour))
	}
	events = append(events, testutil.AllDay("t", "A very long trip name", 2026, time.November, 12, 2))

	got := renderGrid(gridView{
		start: start, today: time.Date(2026, 11, 11, 0, 0, 0, 0, ny), selected: time.Date(2026, 11, 13, 0, 0, 0, 0, ny),
		days: model.GroupByDay(events, start, gridDays), width: 70, height: 18, loc: ny,
	})

	testutil.Golden(t, "grid_cells", got)
}
```
3. Run: compile failure.
4. `styles.go`. Note `fit()`: it truncates and pads by **display width** (`go-runewidth`), never `len()`.
```go
package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var (
	headerStyle   = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	todayStyle    = lipgloss.NewStyle().Bold(true).Underline(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	modalStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

// fit truncates s to w terminal cells (with "…") and pads it to exactly w.
// Uses display width, not len(): "–" is 3 bytes, CJK runes are 2 cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return runewidth.FillRight(runewidth.Truncate(s, w, "…"), w)
}

func calendarStyle(color string) lipgloss.Style {
	if color == "" {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}
```
5. `grid.go`:
```go
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/yourname/clindar/internal/model"
)

const gridDays = 28

// gridView is everything renderGrid needs. Keeping rendering a pure function
// of plain data makes it easy to golden-test.
type gridView struct {
	start           time.Time   // first cell (local midnight, a week start)
	today, selected time.Time   // local midnights
	days            []model.Day // gridDays days from start
	calendars       map[string]model.Calendar
	width, height   int // space available for the grid, including its header
	loc             *time.Location
}

func renderGrid(g gridView) string {
	colW := max(g.width/7, 8)
	cellH := max((g.height-2)/4, 2) // 2 header lines
	var b strings.Builder
	b.WriteString(headerStyle.Render(g.selected.Format("January 2006")) + "\n")
	var names []string
	for i := range 7 {
		names = append(names, fit(g.start.AddDate(0, 0, i).Format("Mon"), colW))
	}
	b.WriteString(dimStyle.Render(strings.Join(names, "")) + "\n")
	for week := range 4 {
		var cells []string
		for dow := range 7 {
			cells = append(cells, g.cell(g.days[week*7+dow], colW, cellH))
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cells...) + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// cell renders one day: a label line, then as many events as fit.
func (g gridView) cell(day model.Day, w, h int) string {
	label := fmt.Sprintf("%2d", day.Date.Day())
	if day.Date.Day() == 1 {
		label = day.Date.Format("Jan 2")
	}
	switch {
	case day.Date.Equal(g.selected):
		label = selectedStyle.Render(fit("▶"+label, w-1))
	case day.Date.Equal(g.today):
		label = todayStyle.Render(fit(label+" today", w-1))
	default:
		label = fit(label, w-1)
	}
	lines := []string{label}
	room := h - 1
	for i, e := range day.Events {
		if i == room-1 && len(day.Events) > room {
			lines = append(lines, dimStyle.Render(fit(fmt.Sprintf("+%d more", len(day.Events)-i), w-1)))
			break
		}
		if i == room {
			break
		}
		lines = append(lines, calendarStyle(g.calendars[e.CalendarID].Color).Render(fit(eventLabel(e, g.loc), w-1)))
	}
	for len(lines) < h {
		lines = append(lines, fit("", w-1))
	}
	// One space of gutter on the right of every cell.
	for i := range lines {
		lines[i] += " "
	}
	return strings.Join(lines, "\n")
}

// eventLabel is "09:00 Title" for timed events and just the title for all-day.
func eventLabel(e model.Event, loc *time.Location) string {
	title := strings.Join(strings.Fields(e.Title), " ")
	if e.AllDay {
		return title
	}
	start, _ := e.Span(loc)
	return start.Format("15:04") + " " + title
}

// weekStart returns the first day of t's week.
func weekStart(t time.Time, first time.Weekday) time.Time {
	d := model.StartOfDay(t)
	back := (int(d.Weekday()) - int(first) + 7) % 7
	return d.AddDate(0, 0, -back)
}
```
6. `go test ./internal/tui -update`, then inspect `testdata/grid_cells.golden`. It should look like this (trailing
   spaces are expected):
```text
November 2026
Mon       Tue       Wed       Thu       Fri       Sat       Sun       
 9        10        11 today  12        ▶13       14        15        
          08:00 Me…           A very l… A very l…                     
          09:00 Me…                                                   
          +3 more                                                     
16        17        18        19        20        21        22        
                                                                      
                                                                      
                                                                      
23        24        25        26        27        28        29        
                                                                      
                                                                      
                                                                      
30        Dec 1      2         3         4         5         6        
                                                                      
                                                                      
                                                                      
```
   Check: Tuesday shows 2 meetings and "+3 more"; Wed is "today"; Fri has `▶`; the long trip title is
   truncated with "…" on both Thu and Fri.
7. Run without `-update`: pass.

**Commit:** `feat(tui): 4-week grid rendering`

---

## Task 10.2: The list

**Goal:** `renderList` draws the day-grouped agenda for the two weeks from the selected day. The cursor row is
marked `▶` and the view scrolls to keep it visible.

**Files:** create `internal/tui/list_test.go`, `internal/tui/list.go`

**Steps**
1. Test:
```go
package tui

import (
	"testing"
	"time"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/testutil"
)

func TestRenderListScrollsToCursor(t *testing.T) {
	ny := testutil.NY(t)
	from := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
	var events []model.Event
	for d := 9; d < 16; d++ {
		events = append(events, testutil.Timed("e", "Day "+time.Date(2026, 11, d, 0, 0, 0, 0, ny).Format("02"),
			time.Date(2026, 11, d, 9, 0, 0, 0, ny), time.Hour))
	}

	got := renderList(listView{days: model.GroupByDay(events, from, 7), cursor: 6, width: 40, height: 5, loc: ny})

	testutil.Golden(t, "list_scrolled", got)
}
```
2. Run: compile failure.
3. Implement:
```go
package tui

import (
	"strings"
	"time"

	"github.com/yourname/clindar/internal/model"
)

const listDays = 14

// listView renders the agenda list. entries is the flat, selectable list of
// events (an event spanning days appears once per day, as in GroupByDay).
type listView struct {
	days          []model.Day
	cursor        int
	calendars     map[string]model.Calendar
	width, height int
	loc           *time.Location
}

func listEntries(days []model.Day) []model.Event {
	var out []model.Event
	for _, d := range days {
		out = append(out, d.Events...)
	}
	return out
}

func renderList(v listView) string {
	type row struct {
		text  string
		entry int // -1 for day headers
	}
	var rows []row
	n := 0
	for _, d := range v.days {
		if len(d.Events) == 0 {
			continue
		}
		rows = append(rows, row{headerStyle.Render(d.Date.Format("Mon 02 Jan")), -1})
		for _, e := range d.Events {
			rows = append(rows, row{v.line(e), n})
			n++
		}
	}
	if len(rows) == 0 {
		return dimStyle.Render("No events in the next two weeks. Press a to add one.")
	}
	// Scroll so the cursor's row is visible.
	cursorRow := 0
	for i, r := range rows {
		if r.entry == v.cursor {
			cursorRow = i
		}
	}
	top := max(0, cursorRow-v.height+2)
	var b strings.Builder
	for i := top; i < len(rows) && i < top+v.height; i++ {
		text := rows[i].text
		if rows[i].entry == v.cursor {
			// A visible marker as well as reverse video: works without color, and in golden files.
			text = selectedStyle.Render(fit("▶"+strings.TrimPrefix(text, " "), v.width))
		}
		b.WriteString(text + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (v listView) line(e model.Event) string {
	when := "all day    "
	if !e.AllDay {
		s, end := e.Span(v.loc)
		when = s.Format("15:04") + "–" + end.Format("15:04")
	}
	cal := v.calendars[e.CalendarID]
	resp := ""
	if self := e.SelfAttendee(); self != nil && self.Response != model.ResponseAccepted {
		resp = " [" + string(self.Response) + "]"
	}
	text := fit("  "+when+"  "+strings.Join(strings.Fields(e.Title), " ")+resp, max(v.width-len(cal.Name)-4, 10))
	return text + "  " + calendarStyle(cal.Color).Render("● "+cal.Name)
}
```
4. `go test ./internal/tui -update -run List`, then inspect `testdata/list_scrolled.golden`. It must end with the `▶` row:
```text
Sat 14 Nov
  09:00–10:00  Day 14                 ● 
Sun 15 Nov
▶ 09:00–10:00  Day 15                 ● 
```
5. Run without `-update`: pass.

**Commit:** `feat(tui): agenda list rendering`

---

## Task 10.3: The model, navigation, and actions

**Goal:** the interactive app. This task is larger than the others because the files reference each other.
Write all the tests first, then the files in the order below, and **commit after each step that compiles**.

How the tests work: `fixture.drain` does what the Bubble Tea runtime does, synchronously. It runs each
returned command and feeds its message back into `Update`, until nothing is left. `fixture.keys("l", "a",
"Dentist", "enter")` presses keys like a user. Assertions check **outcomes**: what's in the store, what's on
screen, which mode the model is in.

**Files:** create `internal/tui/model_test.go`, `internal/tui/actions_test.go`, `internal/tui/keys.go`,
`internal/tui/form.go`, `internal/tui/model.go`, `internal/tui/nav.go`, `internal/tui/actions.go`

**Steps**
1. Tests for rendering and navigation:
```go
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

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/service"
	"github.com/yourname/clindar/internal/testutil"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii) // golden files contain no ANSI codes
	os.Exit(m.Run())
}

type fixture struct {
	t   *testing.T
	svc *service.Service
	ny  *time.Location
	m   tea.Model
}

// newFixture: Thursday Nov 12 2026, a real service over a temp store with a
// weekly standup, a lunch, and a 2-day trip.
func newFixture(t *testing.T) *fixture {
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
	f := &fixture{t: t, svc: svc, ny: ny}
	m := New(ctx, Deps{Service: svc, Now: func() time.Time { return now }, WeekStart: time.Monday})
	f.m = f.drain(m, m.Init())
	f.send(tea.WindowSizeMsg{Width: 84, Height: 20})
	return f
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
```
2. Tests for actions:
```go
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
```
3. `keys.go`:
```go
package tui

import "github.com/charmbracelet/bubbles/key"

// keys is the single source of truth for bindings: Update matches on them
// and the help screen renders them.
var keys = struct {
	Left, Right, Up, Down, ScrollUp, ScrollDown, Today, Goto, Toggle, Open,
	Add, Edit, Delete, RSVP, Sync, Help, Quit, Back key.Binding
}{
	Left:       key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "previous day")),
	Right:      key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "next day")),
	Up:         key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up (week / event)")),
	Down:       key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down (week / event)")),
	ScrollUp:   key.NewBinding(key.WithKeys("K", "pgup"), key.WithHelp("K", "scroll a week back")),
	ScrollDown: key.NewBinding(key.WithKeys("J", "pgdown"), key.WithHelp("J", "scroll a week forward")),
	Today:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "today")),
	Goto:       key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "go to date")),
	Toggle:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "grid ⇄ list")),
	Open:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open day / event")),
	Add:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add event")),
	Edit:       key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit event (list)")),
	Delete:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete event (list)")),
	RSVP:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "RSVP (list)")),
	Sync:       key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync now")),
	Help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back / cancel")),
}

func helpText() string {
	s := "Keys\n\n"
	for _, b := range []key.Binding{keys.Left, keys.Right, keys.Up, keys.Down, keys.ScrollUp, keys.ScrollDown,
		keys.Today, keys.Goto, keys.Toggle, keys.Open, keys.Add, keys.Edit, keys.Delete, keys.RSVP,
		keys.Sync, keys.Back, keys.Quit} {
		s += "  " + fit(b.Help().Key, 8) + b.Help().Desc + "\n"
	}
	return s + "\nPress any key to close."
}
```
4. `form.go`. It reuses `timeparse.ParseSpan` and `recur.ParseRepeat`, so its inputs mean the same as the CLI flags.
   The add form's Calendar field is pre-filled from `Deps.DefaultCalendar` (config `default_calendar`), the same
   default the CLI uses:
```go
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/recur"
	"github.com/yourname/clindar/internal/service"
	"github.com/yourname/clindar/internal/timeparse"
)

// Form field indexes.
const (
	fTitle = iota
	fStart
	fEnd
	fDuration
	fCalendar
	fWhere
	fNotes
	fRepeat
	fInvite
	fieldCount
)

var fieldLabels = [fieldCount]string{"Title", "Start", "End", "Duration", "Calendar", "Where", "Notes", "Repeat", "Invite"}

// form edits a new or existing event. It knows nothing about the service:
// it produces a Draft or Changes and the Model decides what to do with them.
type form struct {
	inputs  [fieldCount]textinput.Model
	initial [fieldCount]string // to detect which fields an edit changed
	focus   int
	editing *model.Event // nil when creating
	err     string
}

func newForm(values [fieldCount]string, editing *model.Event) form {
	f := form{initial: values, editing: editing}
	for i := range f.inputs {
		in := textinput.New()
		in.Prompt = ""
		in.Cursor.SetMode(cursor.CursorStatic) // no blink timers: simpler, and tests stay synchronous
		in.SetValue(values[i])
		f.inputs[i] = in
	}
	f.inputs[fTitle].Placeholder = "What"
	f.inputs[fStart].Placeholder = "tomorrow 14:00, fri 2pm, 2026-11-20 (date only = all day)"
	f.inputs[fEnd].Placeholder = "15:30 (same day), or last day for all-day"
	f.inputs[fDuration].Placeholder = "45m, 1h30m, 2d"
	f.inputs[fCalendar].Placeholder = "name (empty = primary)"
	f.inputs[fRepeat].Placeholder = "daily, weekdays, weekly, monthly, yearly, FREQ=..."
	f.inputs[fInvite].Placeholder = "emails, comma-separated"
	if editing != nil { // these can't be changed on an existing event in v1
		f.inputs[fCalendar].Placeholder, f.inputs[fRepeat].Placeholder = "(can't change)", "(can't change)"
	}
	f.inputs[fTitle].Focus()
	return f
}

// newEventForm pre-fills a form for adding an event on day, in the user's
// default calendar (the same default the CLI's `add` uses).
func newEventForm(day time.Time, defaultCalendar string) form {
	var v [fieldCount]string
	v[fStart] = day.Format("2006-01-02") + " 09:00"
	v[fCalendar] = defaultCalendar
	return newForm(v, nil)
}

// editEventForm pre-fills a form from an existing event.
func editEventForm(e model.Event, loc *time.Location) form {
	var v [fieldCount]string
	start, end := e.Span(loc)
	v[fTitle], v[fWhere], v[fNotes] = e.Title, e.Location, e.Description
	if e.AllDay {
		v[fStart], v[fEnd] = start.Format("2006-01-02"), end.AddDate(0, 0, -1).Format("2006-01-02")
	} else {
		v[fStart], v[fEnd] = start.Format("2006-01-02 15:04"), end.Format("15:04")
		if !model.StartOfDay(end).Equal(model.StartOfDay(start)) {
			v[fEnd] = end.Format("2006-01-02 15:04")
		}
	}
	var emails []string
	for _, a := range e.Attendees {
		if !a.Self && !a.Organizer {
			emails = append(emails, a.Email)
		}
	}
	v[fInvite] = strings.Join(emails, ", ")
	return newForm(v, &e)
}

func (f form) Update(msg tea.Msg) (form, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "tab", "down":
			return f.move(1), nil
		case "shift+tab", "up":
			return f.move(-1), nil
		}
	}
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return f, cmd
}

func (f form) move(delta int) form {
	f.inputs[f.focus].Blur()
	f.focus = (f.focus + delta + fieldCount) % fieldCount
	f.inputs[f.focus].Focus()
	return f
}

func (f form) value(i int) string { return strings.TrimSpace(f.inputs[i].Value()) }
func (f form) changed(i int) bool { return f.value(i) != strings.TrimSpace(f.initial[i]) }

// draft validates the form as a new event.
func (f form) draft(now time.Time) (service.Draft, error) {
	span, err := timeparse.ParseSpan(now, f.value(fStart), f.value(fEnd), f.value(fDuration), false)
	if err != nil {
		return service.Draft{}, err
	}
	rec, err := recur.ParseRepeat(f.value(fRepeat), span.Start, span.AllDay, time.Time{}, 0)
	if err != nil {
		return service.Draft{}, err
	}
	return service.Draft{
		CalendarID: f.value(fCalendar), Title: f.value(fTitle), Location: f.value(fWhere),
		Description: f.value(fNotes), Start: span.Start, End: span.End, AllDay: span.AllDay,
		Recurrence: rec, Invite: splitEmails(f.value(fInvite)),
	}, nil
}

// changes returns only what the user changed in an edit form.
func (f form) changes(now time.Time) (service.Changes, error) {
	var ch service.Changes
	str := func(i int) *string { v := f.value(i); return &v }
	if f.changed(fTitle) {
		ch.Title = str(fTitle)
	}
	if f.changed(fWhere) {
		ch.Location = str(fWhere)
	}
	if f.changed(fNotes) {
		ch.Description = str(fNotes)
	}
	if f.changed(fInvite) {
		emails := splitEmails(f.value(fInvite))
		ch.Invite = &emails
	}
	if f.changed(fStart) || f.changed(fEnd) || f.changed(fDuration) {
		end := f.value(fEnd)
		if f.changed(fDuration) && !f.changed(fEnd) {
			end = "" // a new duration replaces the pre-filled end
		}
		span, err := timeparse.ParseSpan(now, f.value(fStart), end, f.value(fDuration), false)
		if err != nil {
			return ch, err
		}
		ch.Start, ch.End, ch.AllDay = &span.Start, &span.End, &span.AllDay
	}
	return ch, nil
}

func splitEmails(s string) []string {
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func (f form) View(width int) string {
	title := "New event"
	if f.editing != nil {
		title = "Edit event"
	}
	var b strings.Builder
	b.WriteString(headerStyle.Render(title) + "\n\n")
	for i, in := range f.inputs {
		label := fit(fieldLabels[i], 10)
		if i == f.focus {
			label = selectedStyle.Render(label)
		}
		in.Width = max(width-16, 20)
		b.WriteString(label + "  " + in.View() + "\n")
	}
	if f.err != "" {
		b.WriteString("\n" + errorStyle.Render(f.err) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("tab/↑↓ move · enter save · esc cancel"))
	return modalStyle.Render(b.String())
}
```
5. `model.go`:
```go
// Package tui is the Bubble Tea interface: a 4-week grid and an agenda list.
// Update never blocks: anything touching the service runs in a tea.Cmd and
// reports back with a message.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/output"
	"github.com/yourname/clindar/internal/service"
)

// Deps are the TUI's connections to the rest of the app.
type Deps struct {
	Service         *service.Service
	Sync            func(context.Context) error // nil disables syncing (tests)
	Now             func() time.Time
	WeekStart       time.Weekday
	DefaultCalendar string        // config default_calendar: pre-fills the add form
	SyncInterval    time.Duration // 0 disables periodic sync (tests)
}

type view int

const (
	viewGrid view = iota
	viewList
)

type mode int

const (
	modeNormal mode = iota
	modeDetail
	modeForm
	modeConfirm // y/n delete of a one-off event
	modeScope   // this/following/all for a recurring event
	modeRSVP    // y/n/m
	modeGoto
	modeHelp
)

type Model struct {
	deps          Deps
	ctx           context.Context
	width, height int
	view          view
	mode          mode

	today, selected, gridStart time.Time // local midnights
	cursor                     int       // list view: index into listEntries

	loadedFrom, loadedTo time.Time
	events               []model.Event
	calendars            map[string]model.Calendar
	status               service.Status
	started              time.Time
	syncing              bool
	message              string // last error or info, shown in the status bar

	form      form
	gotoInput textinput.Model
	// afterScope runs the pending action once the user picks a scope.
	afterScope    func(service.Scope) tea.Cmd
	allowedScopes string // e.g. "tfa" or "ta"
}

// --- messages ---

type loadedMsg struct {
	from, to  time.Time
	events    []model.Event
	calendars []model.Calendar
	status    service.Status
	err       error
}
type syncDoneMsg struct{ err error }
type tickMsg struct{}
type actionDoneMsg struct {
	info string
	err  error
}

func New(ctx context.Context, d Deps) Model {
	now := d.Now()
	today := model.StartOfDay(now)
	gi := textinput.New()
	gi.Placeholder = "fri, +2w, 2026-12-24"
	gi.Cursor.SetMode(cursor.CursorStatic)
	return Model{
		deps: d, ctx: ctx, width: 80, height: 24,
		today: today, selected: today, gridStart: weekStart(today, d.WeekStart),
		started: now, calendars: map[string]model.Calendar{}, gotoInput: gi,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.load(), m.syncCmd(), m.tick())
}

// --- commands (run off the UI goroutine) ---

// window is the range the views need: the grid plus the list's two weeks.
func (m Model) window() (time.Time, time.Time) {
	from := m.gridStart
	if m.selected.Before(from) {
		from = m.selected
	}
	to := m.gridStart.AddDate(0, 0, gridDays)
	if end := m.selected.AddDate(0, 0, listDays); end.After(to) {
		to = end
	}
	return from, to
}

func (m Model) load() tea.Cmd {
	from, to := m.window()
	svc, ctx, started := m.deps.Service, m.ctx, m.started
	return func() tea.Msg {
		msg := loadedMsg{from: from, to: to}
		if msg.events, msg.err = svc.Agenda(ctx, from, to, service.Filter{}); msg.err != nil {
			return msg
		}
		if msg.calendars, msg.err = svc.Calendars(ctx); msg.err != nil {
			return msg
		}
		msg.status, msg.err = svc.Status(ctx, started)
		return msg
	}
}

func (m Model) syncCmd() tea.Cmd {
	if m.deps.Sync == nil {
		return nil
	}
	sync, ctx := m.deps.Sync, m.ctx
	return func() tea.Msg { return syncDoneMsg{err: sync(ctx)} }
}

func (m Model) tick() tea.Cmd {
	if m.deps.SyncInterval <= 0 {
		return nil
	}
	return tea.Tick(m.deps.SyncInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// act runs a service call, then reloads and pushes.
func (m Model) act(info string, fn func(context.Context, *service.Service) error) tea.Cmd {
	svc, ctx := m.deps.Service, m.ctx
	return func() tea.Msg { return actionDoneMsg{info: info, err: fn(ctx, svc)} }
}

// --- update ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case loadedMsg:
		if msg.err != nil {
			m.message = "load failed: " + msg.err.Error()
			return m, nil
		}
		m.loadedFrom, m.loadedTo, m.events, m.status = msg.from, msg.to, msg.events, msg.status
		m.calendars = map[string]model.Calendar{}
		for _, c := range msg.calendars {
			m.calendars[c.ID] = c
		}
		m.cursor = min(m.cursor, max(len(m.entries())-1, 0))
		return m, nil
	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			m.message = "offline? " + msg.err.Error()
		}
		return m, m.load()
	case tickMsg:
		m.today = model.StartOfDay(m.deps.Now()) // the TUI may stay open past midnight
		m.syncing = m.deps.Sync != nil
		return m, tea.Batch(m.syncCmd(), m.tick())
	case actionDoneMsg:
		m.mode = modeNormal
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, m.load()
		}
		m.message = msg.info
		return m, tea.Batch(m.load(), m.syncCmd())
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) loc() *time.Location { return m.deps.Now().Location() }

// entries is the list view's selectable events.
func (m Model) entries() []model.Event {
	return listEntries(model.GroupByDay(m.events, m.selected, listDays))
}

func (m Model) View() string {
	bodyH := m.height - 1
	var body string
	switch m.view {
	case viewGrid:
		body = renderGrid(gridView{
			start: m.gridStart, today: m.today, selected: m.selected,
			days:      model.GroupByDay(m.events, m.gridStart, gridDays),
			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc(),
		})
	case viewList:
		body = renderList(listView{days: model.GroupByDay(m.events, m.selected, listDays), cursor: m.cursor,
			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc()})
	}
	if overlay := m.overlay(); overlay != "" {
		body = lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, overlay)
	}
	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)
	return body + "\n" + m.statusBar()
}

func (m Model) overlay() string {
	switch m.mode {
	case modeHelp:
		return modalStyle.Render(helpText())
	case modeForm:
		return m.form.View(min(m.width-4, 80))
	case modeGoto:
		return modalStyle.Render("Go to date\n\n" + m.gotoInput.View())
	case modeDetail:
		var b strings.Builder
		var cals []model.Calendar
		for _, c := range m.calendars {
			cals = append(cals, c)
		}
		p, _ := output.NewPrinter(&b, output.Pretty, "", m.loc(), cals)
		p.Event(m.entries()[m.cursor])
		return modalStyle.Render(strings.TrimRight(b.String(), "\n"))
	case modeScope:
		opts := map[byte]string{'t': "(t)his event", 'f': "(f)ollowing", 'a': "(a)ll events"}
		var parts []string
		for i := range len(m.allowedScopes) {
			parts = append(parts, opts[m.allowedScopes[i]])
		}
		return modalStyle.Render("Recurring event. Apply to:\n\n" + strings.Join(parts, "   ") + "\n\nesc cancel")
	case modeConfirm:
		return modalStyle.Render("Delete this event? (y/n)")
	case modeRSVP:
		return modalStyle.Render("Going? (y)es  (n)o  (m)aybe")
	}
	return ""
}

func (m Model) statusBar() string {
	parts := []string{}
	switch {
	case m.syncing:
		parts = append(parts, "⟳ syncing")
	case m.status.LastSync.IsZero():
		parts = append(parts, "never synced")
	default:
		parts = append(parts, "synced "+ago(m.deps.Now().Sub(m.status.LastSync)))
	}
	if m.status.Pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", m.status.Pending))
	}
	if m.status.Discarded > 0 {
		parts = append(parts, errorStyle.Render(fmt.Sprintf("%d edit(s) discarded: see clindar log", m.status.Discarded)))
	}
	if m.message != "" {
		parts = append(parts, m.message)
	}
	parts = append(parts, "? help")
	return dimStyle.Render(fit(strings.Join(parts, " · "), m.width))
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// Run starts the TUI full-screen and blocks until the user quits.
func Run(ctx context.Context, d Deps) error {
	_, err := tea.NewProgram(New(ctx, d), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil // Ctrl-C via the context
	}
	return err
}
```
6. `nav.go`:
```go
package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/service"
	"github.com/yourname/clindar/internal/timeparse"
)

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		return m.formKey(k)
	case modeGoto:
		return m.gotoKey(k)
	case modeScope:
		return m.scopeKey(k)
	case modeConfirm:
		m.mode = modeNormal
		if k.String() == "y" {
			return m, m.afterScope(service.ScopeNone)
		}
		return m, nil
	case modeRSVP:
		return m.rsvpKey(k)
	case modeHelp, modeDetail:
		m.mode = modeNormal
		return m, nil
	}
	m.message = ""
	switch {
	case key.Matches(k, keys.Quit):
		return m, tea.Quit
	case key.Matches(k, keys.Help):
		m.mode = modeHelp
	case key.Matches(k, keys.Toggle):
		m.view = 1 - m.view
		m.cursor = 0
	case key.Matches(k, keys.Today):
		m.selected, m.gridStart, m.cursor = m.today, weekStart(m.today, m.deps.WeekStart), 0
	case key.Matches(k, keys.Goto):
		m.mode = modeGoto
		m.gotoInput.SetValue("")
		return m, m.gotoInput.Focus()
	case key.Matches(k, keys.Sync):
		m.syncing = m.deps.Sync != nil
		return m, m.syncCmd()
	case key.Matches(k, keys.Add):
		m.form = newEventForm(m.selected, m.deps.DefaultCalendar)
		m.mode = modeForm
		return m, nil
	case key.Matches(k, keys.ScrollDown):
		m = m.moveDays(7, true)
	case key.Matches(k, keys.ScrollUp):
		m = m.moveDays(-7, true)
	default:
		if m.view == viewGrid {
			return m.gridKey(k)
		}
		return m.listKey(k)
	}
	return m, m.reloadIfNeeded()
}

func (m Model) gridKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(k, keys.Left):
		m = m.moveDays(-1, false)
	case key.Matches(k, keys.Right):
		m = m.moveDays(1, false)
	case key.Matches(k, keys.Up):
		m = m.moveDays(-7, false)
	case key.Matches(k, keys.Down):
		m = m.moveDays(7, false)
	case key.Matches(k, keys.Open):
		m.view, m.cursor = viewList, 0
	case key.Matches(k, keys.Edit, keys.Delete, keys.RSVP):
		m.message = "press enter to pick an event in the list"
	}
	return m, m.reloadIfNeeded()
}

func (m Model) listKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := m.entries()
	var current *model.Event
	if m.cursor < len(entries) {
		current = &entries[m.cursor]
	}
	switch {
	case key.Matches(k, keys.Down):
		m.cursor = min(m.cursor+1, max(len(entries)-1, 0))
	case key.Matches(k, keys.Up):
		m.cursor = max(m.cursor-1, 0)
	case key.Matches(k, keys.Left):
		m = m.moveDays(-1, false)
		m.cursor = 0
	case key.Matches(k, keys.Right):
		m = m.moveDays(1, false)
		m.cursor = 0
	case current == nil:
		return m, nil
	case key.Matches(k, keys.Open):
		m.mode = modeDetail
	case key.Matches(k, keys.Edit):
		m.form = editEventForm(*current, m.loc())
		m.mode = modeForm
		return m, nil
	case key.Matches(k, keys.Delete):
		return m.startDelete(*current), nil
	case key.Matches(k, keys.RSVP):
		if current.SelfAttendee() == nil {
			m.message = "you are not invited to this event"
			return m, nil
		}
		m.mode = modeRSVP
	}
	return m, m.reloadIfNeeded()
}

// moveDays moves the selection; scroll also moves the grid window.
func (m Model) moveDays(n int, scroll bool) Model {
	m.selected = m.selected.AddDate(0, 0, n)
	if scroll {
		m.gridStart = m.gridStart.AddDate(0, 0, n)
	}
	switch end := m.gridStart.AddDate(0, 0, gridDays); {
	case m.selected.Before(m.gridStart):
		m.gridStart = weekStart(m.selected, m.deps.WeekStart)
	case !m.selected.Before(end):
		m.gridStart = weekStart(m.selected, m.deps.WeekStart).AddDate(0, 0, -21)
	}
	return m
}

func (m Model) reloadIfNeeded() tea.Cmd {
	from, to := m.window()
	if from.Before(m.loadedFrom) || to.After(m.loadedTo) || m.loadedFrom.IsZero() {
		return m.load()
	}
	return nil
}

func (m Model) gotoKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
		return m, nil
	case tea.KeyEnter:
		m.mode = modeNormal
		t, _, err := timeparse.ParseTime(m.gotoInput.Value(), m.deps.Now())
		if err != nil {
			m.message = err.Error()
			return m, nil
		}
		m = m.moveDays(int(model.StartOfDay(t).Sub(m.selected).Round(24*time.Hour)/(24*time.Hour)), false)
		return m, m.reloadIfNeeded()
	}
	var cmd tea.Cmd
	m.gotoInput, cmd = m.gotoInput.Update(k)
	return m, cmd
}
```
7. `actions.go`:
```go
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/service"
)

func (m Model) startDelete(e model.Event) Model {
	id, title := e.ID, e.Title
	m.afterScope = func(sc service.Scope) tea.Cmd {
		return m.act("deleted "+title, func(ctx context.Context, s *service.Service) error {
			return s.Delete(ctx, id, sc)
		})
	}
	if e.IsInstance() {
		m.mode, m.allowedScopes = modeScope, "tfa"
	} else {
		m.mode = modeConfirm
	}
	return m
}

func (m Model) rsvpKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	answers := map[string]model.ResponseStatus{"y": model.ResponseAccepted, "n": model.ResponseDeclined, "m": model.ResponseTentative}
	resp, ok := answers[k.String()]
	if !ok {
		m.mode = modeNormal
		return m, nil
	}
	e := m.entries()[m.cursor]
	id := e.ID
	m.afterScope = func(sc service.Scope) tea.Cmd {
		return m.act("replied "+string(resp), func(ctx context.Context, s *service.Service) error {
			return s.RSVP(ctx, id, resp, sc)
		})
	}
	if e.IsInstance() {
		m.mode, m.allowedScopes = modeScope, "ta"
		return m, nil
	}
	m.mode = modeNormal
	return m, m.afterScope(service.ScopeNone)
}

func (m Model) scopeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	scopes := map[string]service.Scope{"t": service.ScopeThis, "f": service.ScopeFollowing, "a": service.ScopeAll}
	sc, ok := scopes[k.String()]
	m.mode = modeNormal
	if !ok || !strings.Contains(m.allowedScopes, k.String()) {
		return m, nil
	}
	return m, m.afterScope(sc)
}

func (m Model) formKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
		return m, nil
	case tea.KeyEnter:
		return m.submitForm()
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(k)
	return m, cmd
}

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	now := m.deps.Now()
	if m.form.editing == nil {
		d, err := m.form.draft(now)
		if err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		return m, m.act("created "+d.Title, func(ctx context.Context, s *service.Service) error {
			_, err := s.Create(ctx, d)
			return err
		})
	}
	ch, err := m.form.changes(now)
	if err != nil {
		m.form.err = err.Error()
		return m, nil
	}
	e := *m.form.editing
	m.afterScope = func(sc service.Scope) tea.Cmd {
		return m.act("saved "+e.Title, func(ctx context.Context, s *service.Service) error {
			_, err := s.Update(ctx, e.ID, ch, sc)
			return err
		})
	}
	if e.IsInstance() {
		m.mode, m.allowedScopes = modeScope, "tfa"
		return m, nil
	}
	return m, m.afterScope(service.ScopeNone)
}

// --- view ---
```
8. `go test ./internal/tui -update`, then **inspect `testdata/grid.golden` and `testdata/list.golden`**. With the fixture you should
   see Standup on Mondays and Thursdays, lunch on Thu 12, and the Lisbon trip on Fri 20 and Sat 21. Then run
   `go test -race ./internal/tui` without `-update`.

**Commit:** `feat(tui): interactive model with navigation, forms, and actions`

---

## Task 10.4: Launch the TUI from `clindar`

**Goal:** `clindar` with no arguments opens the TUI when stdout is a terminal, and prints help otherwise.

**Files:** modify `internal/cli/root.go`

**Steps**
1. Replace the root command's `RunE` with the version below and add the import `"github.com/yourname/clindar/internal/tui"`:
```go
		RunE: func(cmd *cobra.Command, args []string) error {
			if !app.IsTTY {
				return cmd.Help()
			}
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			return tui.Run(cmd.Context(), tui.Deps{
				Service: svc,
				Sync: func(ctx context.Context) error {
					_, err := app.SyncNow(ctx) // skipped quietly if the daemon is syncing
					return err
				},
				Now:          app.Now,
				WeekStart:    app.Config.FirstWeekday(),
				SyncInterval: app.Config.SyncInterval.Duration,
			})
		},
```
2. `go build -o clindar . && ./clindar`. Walk through everything: `h j k l`, `J K`, `t`, `g` + "fri", `tab`,
   `enter` on an event, `a` to add, `e` to edit a recurring instance (you get the scope picker), `d`, `r`, `s`, `?`, `q`.
3. Resize the terminal while it runs. Nothing should wrap or crash.

**Troubleshooting:** if the first frame takes seconds to appear, your terminal isn't answering the
background-color query (some multiplexers and `script` don't). Real terminals answer instantly.

**Commit:** `feat(cli): open the TUI by default`
