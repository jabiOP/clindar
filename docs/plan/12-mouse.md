# Phase 12: Mouse

Mouse support for the TUI: clicks, the scroll wheel, clickable pop-up buttons and form fields, and
**drag-and-drop in the 4-week grid** (move an event to another day; drag its `◂`/`▸` handles to change its
first or last day). Agreed design: `00-design.md` §6b.

**Starting point:** Phase 10 code as on the implementation branch (commit `f633672`). This phase *changes* existing
files, so code is shown as **diffs** against that code for modified files and as **whole files** for new ones.
Apply a diff by hand, or save it to a file and run `git apply that.diff`.

**Rules for this phase**
- **Mouse off = exactly today's TUI.** With `mouse = false` (the default) no golden file may change.
- **One code path.** A click never has its own logic: buttons act like their keys (`keyMsg`), and a drop
  becomes a `service.Changes` that goes through the same `applyChanges` as the edit form.
- **Draw and hit-test together.** Every render function returns a **hit map** (what is drawn where) alongside its
  text. The mouse handler re-runs `layout()` and looks the click up there, so a click always matches the screen.
- **One width measure, on plain text.** Measure every width with the `width()` helper (= `lipgloss.Width`, the
  measure lipgloss itself pads and joins with), and only on plain text, never on strings that already contain
  color codes (Task 10.2's lesson). **Note:** the code in Tasks 12.2–12.7 below was written before this rule and still calls
  `runewidth.StringWidth`. Task 13.9 replaces every such call with `width()`. Each affected task has a note.

**Known trade-off (accepted in design):** the handles take 2 columns in every event line. At 70 columns (10 per day)
titles of one-day events shrink to "09:00 …". At 110+ columns it's comfortable. Tests use 112.

---

## Task 12.1: The `mouse` setting

**Goal:** a config key `mouse` (default `false`), carried to the TUI through `tuiDeps` (with its test from Task
10.4), and the program asks the terminal for mouse events only when it's on.

**Files:** modify `internal/config/config.go`, `internal/config/config_test.go`, `internal/tui/model.go`,
`internal/cli/root.go`, `internal/cli/tuideps_test.go`

**Steps**
1. Tests first:
```diff
--- a/internal/config/config_test.go
+++ b/internal/config/config_test.go
@@ -30,7 +30,7 @@ func TestLoadMissingFileGivesDefaults(t *testing.T) {
 }
 
 func TestLoadOverridesOnlyGivenKeys(t *testing.T) {
-	c, err := config.Load(writeFile(t, "sync_interval = \"20s\"\nweek_start = \"sunday\"\n"))
+	c, err := config.Load(writeFile(t, "sync_interval = \"20s\"\nweek_start = \"sunday\"\nmouse = true\n"))
 	if err != nil {
 		t.Fatal(err)
 	}
@@ -40,6 +40,9 @@ func TestLoadOverridesOnlyGivenKeys(t *testing.T) {
 	if c.FirstWeekday() != time.Sunday {
 		t.Errorf("FirstWeekday = %v", c.FirstWeekday())
 	}
+	if !c.Mouse || config.Default().Mouse {
+		t.Errorf("Mouse = %v, want true here and false by default", c.Mouse)
+	}
 	if c.StaleAfter != config.Default().StaleAfter {
 		t.Errorf("StaleAfter changed to %v", c.StaleAfter)
 	}
```
```diff
--- a/internal/cli/tuideps_test.go
+++ b/internal/cli/tuideps_test.go
@@ -14,10 +14,11 @@ func TestTUIDepsCarryConfig(t *testing.T) {
 	cfg.DefaultCalendar = "Work"
 	cfg.WeekStart = "sunday"
 	cfg.SyncInterval = config.Duration{Duration: 20 * time.Second}
+	cfg.Mouse = true
 
 	d := tuiDeps(&App{Config: cfg, Now: time.Now}, nil)
 
-	if d.DefaultCalendar != "Work" || d.WeekStart != time.Sunday || d.SyncInterval != 20*time.Second {
+	if d.DefaultCalendar != "Work" || d.WeekStart != time.Sunday || d.SyncInterval != 20*time.Second || !d.Mouse {
 		t.Errorf("deps = %+v", d)
 	}
 	if d.Sync == nil || d.Now == nil {
```
2. Run `go test ./internal/config ./internal/cli`: compile failure (`Mouse` doesn't exist).
3. Implement:
```diff
--- a/internal/config/config.go
+++ b/internal/config/config.go
@@ -28,6 +28,7 @@ type Config struct {
 	DefaultCalendar string   `toml:"default_calendar"` // name or ID; "" = primary
 	WeekStart       string   `toml:"week_start"`       // "monday" | "sunday"
 	SendUpdates     string   `toml:"send_updates"`     // "all" | "none": email attendees on changes
+	Mouse           bool     `toml:"mouse"`            // TUI mouse support (hold Shift to select text)
 }
 
 func Default() Config {
```
```diff
--- a/internal/tui/model.go
+++ b/internal/tui/model.go
@@ -27,6 +27,7 @@ type Deps struct {
 	Now             func() time.Time
 	WeekStart       time.Weekday
 	DefaultCalendar string        // config default_calendar: pre-fills the add form
+	Mouse           bool          // config mouse: clicks, wheel, drag-and-drop (Phase 12)
 	SyncInterval    time.Duration // 0 disables periodic sync (tests)
 }
 
@@ -299,7 +300,12 @@ func ago(d time.Duration) string {
 
 // Run starts the TUI full-screen and blocks until the user quits.
 func Run(ctx context.Context, d Deps) error {
-	_, err := tea.NewProgram(New(ctx, d), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
+	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithContext(ctx)}
+	if d.Mouse {
+		// Cell motion: clicks, wheel, and movement while a button is held (dragging).
+		opts = append(opts, tea.WithMouseCellMotion())
+	}
+	_, err := tea.NewProgram(New(ctx, d), opts...).Run()
 	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
 		return nil // Ctrl-C via the context
 	}
```
```diff
--- a/internal/cli/root.go
+++ b/internal/cli/root.go
@@ -139,5 +139,6 @@ func tuiDeps(app *App, svc *service.Service) tui.Deps {
 		WeekStart:       app.Config.FirstWeekday(),
 		SyncInterval:    app.Config.SyncInterval.Duration,
 		DefaultCalendar: app.Config.DefaultCalendar,
+		Mouse:           app.Config.Mouse,
 	}
 }
```
4. Run: pass.

**Commit:** `feat(tui): mouse setting`

---

## Task 12.2: Hit maps and pop-up builder

> ⚠ **Superseded by Task 13.9 (one width measure):** in `modal.go` and `hits_test.go`, every `runewidth.StringWidth(…)` becomes `width(…)`, and the `go-runewidth` import goes.

**Goal:** two small pure helpers everything else uses.
- `hits.go`: a list of rectangles, each with a **target** (a day, an event and which part of it, a list row, a
  button, a form field, or "inside a pop-up"). `at(x, y)` returns the topmost one: a button wins over its pop-up.
- `modal.go`: builds a pop-up line by line and records where its buttons and fields are. `overlay` centers it with
  plain arithmetic. We stop using `lipgloss.Place` for pop-ups because its rounding would have to be copied into the
  hit map, and two copies of layout math drift apart.

**The test that matters:** `TestModalButtonsAreWhereTheyAreDrawn` finds the label **in the rendered text** and
clicks every character of it. A hit-map test that only checks its own numbers proves nothing.

**Files:** create `internal/tui/hits.go`, `internal/tui/modal.go`, `internal/tui/hits_test.go`

**Steps**
1. Test:
```go
package tui

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestHitMapTopmostWins(t *testing.T) {
	var m hitMap
	m.add(0, 0, 10, 5, target{kind: targetModal})
	m.add(2, 1, 4, 1, target{kind: targetKey, key: "d"})

	if got, _ := m.at(3, 1); got.kind != targetKey {
		t.Errorf("at(3,1) = %v, want the button on top", got.kind)
	}
	if got, _ := m.at(8, 3); got.kind != targetModal {
		t.Errorf("at(8,3) = %v, want the pop-up", got.kind)
	}
	if _, ok := m.at(20, 20); ok {
		t.Error("at(20,20) should hit nothing")
	}
}

// find returns the screen position of the first occurrence of s in screen.
func find(t *testing.T, screen, s string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(screen, "\n") {
		if i := strings.Index(line, s); i >= 0 {
			return runewidth.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", s, screen)
	return 0, 0
}

// The real test of a hit map: click where the label is DRAWN and get its target.
func TestModalButtonsAreWhereTheyAreDrawn(t *testing.T) {
	var m modal
	m.text("Delete this event?")
	m.buttons(button{"Delete", "y"}, button{"Cancel", "esc"})
	box, hits := m.render()
	screen, x0, y0 := overlay(60, 12, box)
	hits = hits.offset(x0, y0)

	for label, wantKey := range map[string]string{"[Delete]": "y", "[Cancel]": "esc"} {
		x, y := find(t, screen, label)
		for dx := range len(label) {
			if got, _ := hits.at(x+dx, y); got.kind != targetKey || got.key != wantKey {
				t.Fatalf("click on %s (+%d) = %+v, want key %q", label, dx, got, wantKey)
			}
		}
	}
	x, y := find(t, screen, "Delete this")
	if got, _ := hits.at(x, y); got.kind != targetModal {
		t.Errorf("click on the text = %v, want targetModal", got.kind)
	}
}
```
2. Run: compile failure.
3. `hits.go`:
```go
package tui

import (
	"time"

	"github.com/jabiOP/clindar/internal/model"
)

// A hit map records what was drawn where, so a mouse position can be turned
// back into "this day", "this event's end handle", or "this button". Render
// functions build it while drawing, so the two can never disagree.

type targetKind int

const (
	targetNone  targetKind = iota
	targetDay              // a grid cell
	targetEvent            // an event line in a grid cell
	targetMore             // "+N more" in a grid cell
	targetEntry            // a list row
	targetKey              // a button: clicking it acts like pressing key
	targetField            // a form field
	targetModal            // inside a pop-up, but not on a button or field
)

// part is the piece of a grid event line that was grabbed.
type part int

const (
	partBody  part = iota // move the whole event
	partStart             // ◂ handle: change the first day
	partEnd               // ▸ handle: change the last day
)

type target struct {
	kind  targetKind
	day   time.Time   // targetDay, targetEvent, targetMore: the cell's day
	event model.Event // targetEvent
	part  part        // targetEvent
	entry int         // targetEntry: index into the list's entries
	key   string      // targetKey
	field int         // targetField
}

type hit struct {
	x, y, w, h int
	t          target
}

type hitMap []hit

func (m *hitMap) add(x, y, w, h int, t target) { *m = append(*m, hit{x, y, w, h, t}) }

// at returns the target at (x, y). Later additions are on top: a button wins
// over the pop-up it sits in.
func (m hitMap) at(x, y int) (target, bool) {
	for i := len(m) - 1; i >= 0; i-- {
		h := m[i]
		if x >= h.x && x < h.x+h.w && y >= h.y && y < h.y+h.h {
			return h.t, true
		}
	}
	return target{}, false
}

// offset moves every rectangle, for content drawn at (dx, dy).
func (m hitMap) offset(dx, dy int) hitMap {
	out := make(hitMap, len(m))
	for i, h := range m {
		h.x, h.y = h.x+dx, h.y+dy
		out[i] = h
	}
	return out
}
```
4. `modal.go`:
```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// modal builds a pop-up line by line and remembers where its buttons and
// form fields are, relative to its content.
type modal struct {
	lines []string
	hits  hitMap
}

// button acts as pressing key when clicked, so mouse and keyboard share one code path.
type button struct{ label, key string }

func (m *modal) text(s string) { m.lines = append(m.lines, strings.Split(s, "\n")...) }

// buttons adds one line of "[Label]" buttons, two spaces apart.
func (m *modal) buttons(bs ...button) {
	var parts []string
	x, y := 0, len(m.lines)
	for _, b := range bs {
		label := "[" + b.label + "]"
		w := runewidth.StringWidth(label)
		m.hits.add(x, y, w, 1, target{kind: targetKey, key: b.key})
		parts = append(parts, label)
		x += w + 2
	}
	m.lines = append(m.lines, strings.Join(parts, "  "))
}

// field adds a clickable line (a form field).
func (m *modal) field(line string, index int) {
	m.hits.add(0, len(m.lines), runewidth.StringWidth(line), 1, target{kind: targetField, field: index})
	m.lines = append(m.lines, line)
}

// render draws the box. Hits are relative to the box's top-left corner; the
// whole box is a targetModal, so a click on its border isn't "outside".
func (m modal) render() (string, hitMap) {
	box := modalStyle.Render(strings.Join(m.lines, "\n"))
	all := hitMap{{0, 0, lipgloss.Width(box), lipgloss.Height(box), target{kind: targetModal}}}
	// modalStyle: a 1-cell border, plus 1 cell of padding left and right.
	return box, append(all, m.hits.offset(2, 1)...)
}

// overlay centers box in a width×height area and returns where its top-left
// corner landed. We place it ourselves (not lipgloss.Place) so the hit map
// uses exactly the same arithmetic as the drawing.
func overlay(width, height int, box string) (s string, x, y int) {
	x = max((width-lipgloss.Width(box))/2, 0)
	y = max((height-lipgloss.Height(box))/2, 0)
	lines := strings.Split(box, "\n")
	for i := range lines {
		lines[i] = strings.Repeat(" ", x) + lines[i]
	}
	return strings.Repeat("\n", y) + strings.Join(lines, "\n"), x, y
}
```
5. Run: pass.

**Commit:** `feat(tui): hit maps and pop-up builder`

---

## Task 12.3: Grid and list return hit maps; resize handles

> ⚠ **Superseded by Task 13.9 (one width measure):** in `grid.go` (the handle-width arithmetic), `runewidth.StringWidth(…)` becomes `width(…)`. (`grid_test.go`'s use of `runewidth` disappears in Task 12.7.)

**Goal:** `renderGrid` and `renderList` return `(string, hitMap)`. With `handles` on, an event's line starts with `◂`
on its first day and ends with `▸` on its last day, and each handle has its own hit. `drop` marks the day under
the pointer during a drag with `»`. `eventDays` (first/last local day of an event) is shared with Task 12.4.

**Files:** modify `internal/tui/grid.go`, `internal/tui/list.go`, `internal/tui/model.go` (callers),
`internal/tui/grid_test.go`, `internal/tui/list_test.go`; new golden `testdata/grid_handles.golden`

**Steps**
1. Tests (`find` comes from `hits_test.go`). Note the grid test locates the trip's `▸` as the *last* `▸` on
   its line, because one-day events now have handles too:
```diff
--- a/internal/tui/grid_test.go
+++ b/internal/tui/grid_test.go
@@ -1,9 +1,12 @@
 package tui
 
 import (
+	"strings"
 	"testing"
 	"time"
 
+	"github.com/mattn/go-runewidth"
+
 	"github.com/jabiOP/clindar/internal/model"
 	"github.com/jabiOP/clindar/internal/testutil"
 )
@@ -28,10 +31,64 @@ func TestRenderGridCells(t *testing.T) {
 	}
 	events = append(events, testutil.AllDay("t", "A very long trip name", 2026, time.November, 12, 2))
 
-	got := renderGrid(gridView{
+	got, _ := renderGrid(gridView{
 		start: start, today: time.Date(2026, 11, 11, 0, 0, 0, 0, ny), selected: time.Date(2026, 11, 13, 0, 0, 0, 0, ny),
 		days: model.GroupByDay(events, start, gridDays), width: 70, height: 18, loc: ny,
 	})
 
 	testutil.Golden(t, "grid_cells", got)
 }
+
+// gridWithTrip renders the week of Nov 9 (width 112: 16 columns per day) with a
+// trip Thu 12–Fri 13 and two events on Tue 10, mouse handles on.
+func gridWithTrip(t *testing.T, drop time.Time) (string, hitMap) {
+	t.Helper()
+	ny := testutil.NY(t)
+	start := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
+	events := []model.Event{
+		testutil.Timed("a", "Dentist", time.Date(2026, 11, 10, 9, 0, 0, 0, ny), time.Hour),
+		testutil.Timed("b", "Gym", time.Date(2026, 11, 10, 18, 0, 0, 0, ny), time.Hour),
+		testutil.AllDay("trip", "Trip", 2026, time.November, 12, 2),
+	}
+	return renderGrid(gridView{
+		start: start, today: time.Date(2026, 11, 9, 0, 0, 0, 0, ny), selected: time.Date(2026, 11, 9, 0, 0, 0, 0, ny),
+		days: model.GroupByDay(events, start, gridDays), width: 112, height: 18, loc: ny,
+		handles: true, drop: drop,
+	})
+}
+
+func TestGridHandles(t *testing.T) {
+	got, _ := gridWithTrip(t, time.Time{})
+	testutil.Golden(t, "grid_handles", got)
+}
+
+func TestGridHitMap(t *testing.T) {
+	ny := testutil.NY(t)
+	screen, hits := gridWithTrip(t, time.Time{})
+	thu, fri := time.Date(2026, 11, 12, 0, 0, 0, 0, ny), time.Date(2026, 11, 13, 0, 0, 0, 0, ny)
+
+	// The trip's ◂ is on Thursday, its ▸ on Friday, and its body in between.
+	x, y := find(t, screen, "◂Trip")
+	if got, _ := hits.at(x, y); got.kind != targetEvent || got.part != partStart || !got.day.Equal(thu) || got.event.ID != "trip" {
+		t.Errorf("◂ = %+v", got)
+	}
+	if got, _ := hits.at(x+2, y); got.part != partBody {
+		t.Errorf("body = %+v", got)
+	}
+	line := strings.Split(screen, "\n")[y]
+	x = runewidth.StringWidth(line[:strings.LastIndex(line, "▸")]) // the trip's ▸ is the last one on its line
+	if got, _ := hits.at(x, y); got.part != partEnd || !got.day.Equal(fri) || got.event.ID != "trip" {
+		t.Errorf("▸ = %+v", got)
+	}
+	// The blank line under Gym (cells are 4 lines tall here) is Tuesday itself.
+	x, y = find(t, screen, "09:00 Den")
+	if got, _ := hits.at(x+1, y+2); got.kind != targetDay || !got.day.Equal(time.Date(2026, 11, 10, 0, 0, 0, 0, ny)) {
+		t.Errorf("empty part of Tue = %+v", got)
+	}
+}
+
+func TestGridMarksDropTarget(t *testing.T) {
+	ny := testutil.NY(t)
+	screen, _ := gridWithTrip(t, time.Date(2026, 11, 14, 0, 0, 0, 0, ny))
+	find(t, screen, "»14") // fails the test if the marker is missing
+}
```
```diff
--- a/internal/tui/list_test.go
+++ b/internal/tui/list_test.go
@@ -22,7 +22,7 @@ func TestRenderListScrollsToCursor(t *testing.T) {
 			time.Date(2026, 11, d, 9, 0, 0, 0, ny), time.Hour))
 	}
 
-	got := renderList(listView{days: model.GroupByDay(events, from, 7), cursor: 6, width: 40, height: 5, loc: ny})
+	got, _ := renderList(listView{days: model.GroupByDay(events, from, 7), cursor: 6, width: 40, height: 5, loc: ny})
 
 	testutil.Golden(t, "list_scrolled", got)
 }
@@ -37,7 +37,7 @@ func TestRenderListSelectedRowKeepsCalendarWithColors(t *testing.T) {
 	e := testutil.Timed("e", "Lunch", time.Date(2026, 11, 9, 12, 0, 0, 0, ny), time.Hour)
 	cals := map[string]model.Calendar{testutil.Calendar: {ID: testutil.Calendar, Name: "Me", Color: "#4285f4"}}
 
-	got := renderList(listView{days: model.GroupByDay([]model.Event{e}, from, 1), cursor: 0,
+	got, _ := renderList(listView{days: model.GroupByDay([]model.Event{e}, from, 1), cursor: 0,
 		calendars: cals, width: 60, height: 5, loc: ny})
 
 	plain := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(got, "")
@@ -45,3 +45,23 @@ func TestRenderListSelectedRowKeepsCalendarWithColors(t *testing.T) {
 		t.Errorf("selected row lost its calendar label:\n%s", plain)
 	}
 }
+
+func TestListHitMap(t *testing.T) {
+	ny := testutil.NY(t)
+	from := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
+	var events []model.Event
+	for d := 9; d < 12; d++ {
+		events = append(events, testutil.Timed("e", "Day "+time.Date(2026, 11, d, 0, 0, 0, 0, ny).Format("02"),
+			time.Date(2026, 11, d, 9, 0, 0, 0, ny), time.Hour))
+	}
+	screen, hits := renderList(listView{days: model.GroupByDay(events, from, 3), cursor: 0, width: 40, height: 10, loc: ny})
+
+	x, y := find(t, screen, "Day 11")
+	if got, _ := hits.at(x, y); got.kind != targetEntry || got.entry != 2 {
+		t.Errorf("click on Day 11 = %+v, want entry 2", got)
+	}
+	x, y = find(t, screen, "Wed 11 Nov")
+	if _, ok := hits.at(x, y); ok {
+		t.Error("day headers are not clickable")
+	}
+}
```
2. Run: compile failure.
3. Implement:
```diff
--- a/internal/tui/grid.go
+++ b/internal/tui/grid.go
@@ -6,6 +6,7 @@ import (
 	"time"
 
 	"github.com/charmbracelet/lipgloss"
+	"github.com/mattn/go-runewidth"
 
 	"github.com/jabiOP/clindar/internal/model"
 )
@@ -21,9 +22,11 @@ type gridView struct {
 	calendars       map[string]model.Calendar
 	width, height   int // space available for the grid, including its header
 	loc             *time.Location
+	handles         bool      // mouse on: draw ◂ ▸ resize handles on event lines
+	drop            time.Time // while dragging: the day under the pointer (zero = none)
 }
 
-func renderGrid(g gridView) string {
+func renderGrid(g gridView) (string, hitMap) {
 	colW := max(g.width/7, 8)
 	cellH := max((g.height-2)/4, 2) // 2 header lines
 	var b strings.Builder
@@ -33,23 +36,31 @@ func renderGrid(g gridView) string {
 		names = append(names, fit(g.start.AddDate(0, 0, i).Format("Mon"), colW))
 	}
 	b.WriteString(dimStyle.Render(strings.Join(names, "")) + "\n")
+	var hits hitMap
 	for week := range 4 {
 		var cells []string
 		for dow := range 7 {
-			cells = append(cells, g.cell(g.days[week*7+dow], colW, cellH))
+			cell, cellHits := g.cell(g.days[week*7+dow], colW, cellH)
+			cells = append(cells, cell)
+			hits = append(hits, cellHits.offset(dow*colW, 2+week*cellH)...)
 		}
 		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cells...) + "\n")
 	}
-	return strings.TrimSuffix(b.String(), "\n")
+	return strings.TrimSuffix(b.String(), "\n"), hits
 }
 
-// cell renders one day: a label line, then as many events as fit.
-func (g gridView) cell(day model.Day, w, h int) string {
+// cell renders one day: a label line, then as many events as fit. Hits are
+// relative to the cell's top-left corner.
+func (g gridView) cell(day model.Day, w, h int) (string, hitMap) {
+	var hits hitMap
+	hits.add(0, 0, w, h, target{kind: targetDay, day: day.Date})
 	label := fmt.Sprintf("%2d", day.Date.Day())
 	if day.Date.Day() == 1 {
 		label = day.Date.Format("Jan 2")
 	}
 	switch {
+	case day.Date.Equal(g.drop):
+		label = selectedStyle.Render(fit("»"+label, w-1)) // drop target while dragging
 	case day.Date.Equal(g.selected):
 		label = selectedStyle.Render(fit("▶"+label, w-1))
 	case day.Date.Equal(g.today):
@@ -61,13 +72,16 @@ func (g gridView) cell(day model.Day, w, h int) string {
 	room := h - 1
 	for i, e := range day.Events {
 		if i == room-1 && len(day.Events) > room {
+			hits.add(0, len(lines), w, 1, target{kind: targetMore, day: day.Date})
 			lines = append(lines, dimStyle.Render(fit(fmt.Sprintf("+%d more", len(day.Events)-i), w-1)))
 			break
 		}
 		if i == room {
 			break
 		}
-		lines = append(lines, calendarStyle(g.calendars[e.CalendarID].Color).Render(fit(eventLabel(e, g.loc), w-1)))
+		line, lineHits := g.eventLine(e, day.Date, w-1)
+		hits = append(hits, lineHits.offset(0, len(lines))...)
+		lines = append(lines, line)
 	}
 	for len(lines) < h {
 		lines = append(lines, fit("", w-1))
@@ -76,7 +90,39 @@ func (g gridView) cell(day model.Day, w, h int) string {
 	for i := range lines {
 		lines[i] += " "
 	}
-	return strings.Join(lines, "\n")
+	return strings.Join(lines, "\n"), hits
+}
+
+// eventLine renders one event in a cell w cells wide. With handles on, the
+// event's first day gets ◂ at the start and its last day gets ▸ at the end.
+func (g gridView) eventLine(e model.Event, day time.Time, w int) (string, hitMap) {
+	var hits hitMap
+	hits.add(0, 0, w, 1, target{kind: targetEvent, day: day, event: e, part: partBody})
+	start, end := "", ""
+	if g.handles {
+		first, last := eventDays(e, g.loc)
+		if day.Equal(first) {
+			start = "◂"
+			hits.add(0, 0, 1, 1, target{kind: targetEvent, day: day, event: e, part: partStart})
+		}
+		if day.Equal(last) {
+			end = "▸"
+			hits.add(w-1, 0, 1, 1, target{kind: targetEvent, day: day, event: e, part: partEnd})
+		}
+	}
+	text := start + fit(eventLabel(e, g.loc), w-runewidth.StringWidth(start+end)) + end
+	return calendarStyle(g.calendars[e.CalendarID].Color).Render(text), hits
+}
+
+// eventDays returns the first and last local day an event touches. An end at
+// exactly midnight doesn't touch that day (end times are exclusive).
+func eventDays(e model.Event, loc *time.Location) (first, last time.Time) {
+	s, end := e.Span(loc)
+	first, last = model.StartOfDay(s), model.StartOfDay(s)
+	if end.After(s) {
+		last = model.StartOfDay(end.Add(-time.Nanosecond))
+	}
+	return first, last
 }
 
 // eventLabel is "09:00 Title" for timed events and just the title for all-day.
```
```diff
--- a/internal/tui/list.go
+++ b/internal/tui/list.go
@@ -29,7 +29,7 @@ func listEntries(days []model.Day) []model.Event {
 	return out
 }
 
-func renderList(v listView) string {
+func renderList(v listView) (string, hitMap) {
 	type row struct {
 		text  string
 		entry int // -1 for day headers
@@ -47,7 +47,7 @@ func renderList(v listView) string {
 		}
 	}
 	if len(rows) == 0 {
-		return dimStyle.Render("No events in the next two weeks. Press a to add one.")
+		return dimStyle.Render("No events in the next two weeks. Press a to add one."), nil
 	}
 	// Scroll so the cursor's row is visible.
 	cursorRow := 0
@@ -58,10 +58,14 @@ func renderList(v listView) string {
 	}
 	top := max(0, cursorRow-v.height+2)
 	var b strings.Builder
+	var hits hitMap
 	for i := top; i < len(rows) && i < top+v.height; i++ {
+		if rows[i].entry >= 0 {
+			hits.add(0, i-top, v.width, 1, target{kind: targetEntry, entry: rows[i].entry})
+		}
 		b.WriteString(rows[i].text + "\n")
 	}
-	return strings.TrimSuffix(b.String(), "\n")
+	return strings.TrimSuffix(b.String(), "\n"), hits
 }
 
 // line renders one event row. The selected row gets a visible ▶ marker as well
```
```diff
--- a/internal/tui/model.go
+++ b/internal/tui/model.go
@@ -216,13 +216,13 @@ func (m Model) View() string {
 	var body string
 	switch m.view {
 	case viewGrid:
-		body = renderGrid(gridView{
+		body, _ = renderGrid(gridView{
 			start: m.gridStart, today: m.today, selected: m.selected,
 			days:      model.GroupByDay(m.events, m.gridStart, gridDays),
 			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc(),
 		})
 	case viewList:
-		body = renderList(listView{days: model.GroupByDay(m.events, m.selected, listDays), cursor: m.cursor,
+		body, _ = renderList(listView{days: model.GroupByDay(m.events, m.selected, listDays), cursor: m.cursor,
 			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc()})
 	}
 	if overlay := m.overlay(); overlay != "" {
```
4. `go test ./internal/tui -update -run TestGridHandles`, then check the golden file. It should show `◂Trip` on Thu
   and `Trip …▸` on Fri:
```text
November 2026
Mon             Tue             Wed             Thu             Fri             Sat             Sun             
▶ 9             10              11              12              13              14              15              
                ◂09:00 Dentist▸                 ◂Trip           Trip          ▸                                 
                ◂18:00 Gym    ▸                                                                                 
                                                                                                                
16              17              18              19              20              21              22              
                                                                                                                
                                                                                                                
                                                                                                                
23              24              25              26              27              28              29              
                                                                                                                
                                                                                                                
                                                                                                                
30              Dec 1            2               3               4               5               6              
                                                                                                                
                                                                                                                
                                                                                                                
```
5. `go test ./internal/tui`: pass, and **`git status` shows no change to the older golden files**.

**Commit:** `feat(tui): hit maps for grid and list, resize handles`

---

## Task 12.4: Drop rules

**Goal:** one pure function, `dragChanges`, that turns "grabbed this part of this event on day A, dropped on day B"
into a `service.Changes` (design §6b table):
- **body**: shift by B−A days.
- **▸**: the last day becomes B, never before the first day.
- **◂**: the first day becomes B, never after the last day.

Clock times are always kept. It refuses no-ops (`errNoChange`) and drops that would end before they start
(`errInverted`). `canDrag` refuses read-only calendars and invitations (guests can't move events).
`dragPreview` is the status-bar text while dragging.

**Files:** create `internal/tui/drag_test.go`, `internal/tui/drag.go`

**Steps**
1. Test. Each row is one rule from the design, plus a DST check (09:00 must stay 09:00 across Nov 1):
```go
package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestDragChanges(t *testing.T) {
	ny := testutil.NY(t)
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, ny) }
	at := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, ny) }
	dentist := testutil.Timed("a", "Dentist", at(10, 9), time.Hour)         // Tue 09:00–10:00
	trip := testutil.AllDay("trip", "Trip", 2026, time.November, 13, 2)     // Fri 13–Sat 14
	overnight := testutil.Timed("o", "Overnight", at(13, 20), 38*time.Hour) // Fri 20:00–Sun 10:00
	tests := []struct {
		name               string
		e                  model.Event
		p                  part
		from, to           time.Time
		wantStart, wantEnd time.Time // all-day: local midnights, end exclusive
		wantErr            error
	}{
		{"move one-day event", dentist, partBody, day(10), day(12), at(12, 9), at(12, 10), nil},
		{"move multi-day event by the grabbed day", trip, partBody, day(14), day(16), day(15), day(17), nil},
		{"end handle lengthens", trip, partEnd, day(14), day(16), day(13), day(17), nil},
		{"end handle shortens to one day", trip, partEnd, day(14), day(13), day(13), day(14), nil},
		{"end handle clamps at the first day", trip, partEnd, day(14), day(9), day(13), day(14), nil},
		{"start handle moves the first day", trip, partStart, day(13), day(11), day(11), day(15), nil},
		{"end handle keeps the end's clock time", overnight, partEnd, day(15), day(17), at(13, 20), at(17, 10), nil},
		{"one-day event becomes multi-day", dentist, partEnd, day(10), day(11), at(10, 9), at(11, 10), nil},
		{"start dragged onto last day would invert", overnight, partStart, day(13), day(15), time.Time{}, time.Time{}, errInverted},
		{"drop on the same day", trip, partBody, day(13), day(13), time.Time{}, time.Time{}, errNoChange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := dragChanges(tt.e, tt.p, tt.from, tt.to, ny)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if !ch.Start.Equal(tt.wantStart) || !ch.End.Equal(tt.wantEnd) {
				t.Errorf("got %v → %v, want %v → %v", ch.Start, ch.End, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestDragKeepsClockTimeAcrossDST(t *testing.T) {
	ny := testutil.NY(t)
	e := testutil.Timed("a", "Standup", time.Date(2026, 10, 30, 9, 0, 0, 0, ny), 15*time.Minute) // Fri, EDT

	ch, err := dragChanges(e, partBody, time.Date(2026, 10, 30, 0, 0, 0, 0, ny), time.Date(2026, 11, 2, 0, 0, 0, 0, ny), ny)

	if err != nil || ch.Start.In(ny).Hour() != 9 || ch.Start.In(ny).Day() != 2 {
		t.Errorf("start = %v, %v; want Mon Nov 2 09:00 (DST ended Nov 1)", ch.Start, err)
	}
}

func TestCanDrag(t *testing.T) {
	when := time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)
	mine := model.Calendar{Name: "Me", AccessRole: "owner"}
	invite := testutil.Timed("i", "Their party", when, time.Hour)
	invite.Attendees = []model.Attendee{{Email: testutil.Account, Self: true}, {Email: "host@x.com", Organizer: true}}
	own := testutil.Timed("o", "Mine", when, time.Hour)

	if ok, _ := canDrag(own, mine); !ok {
		t.Error("own event on own calendar should be draggable")
	}
	if ok, why := canDrag(invite, mine); ok || why == "" {
		t.Error("an invite should not be draggable, with a reason")
	}
	if ok, _ := canDrag(own, model.Calendar{Name: "Holidays", AccessRole: "reader"}); ok {
		t.Error("read-only calendar should not be draggable")
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package tui

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
)

var (
	errNoChange = errors.New("dropped where it already is")
	errInverted = errors.New("it would end before it starts")
)

// dragChanges turns a drop into an edit (design: mouse §2). from is the day the
// event was grabbed on, to the day it was dropped on. Clock times are kept;
// only days change.
//
//	body: move the whole event by (to - from) days
//	end:  the last day becomes to (not before the first day)
//	start: the first day becomes to (not after the last day)
func dragChanges(e model.Event, p part, from, to time.Time, loc *time.Location) (service.Changes, error) {
	oldStart, oldEnd := e.Span(loc)
	start, end := oldStart, oldEnd
	first, last := eventDays(e, loc)
	switch p {
	case partBody:
		n := daysBetween(from, to)
		start, end = start.AddDate(0, 0, n), end.AddDate(0, 0, n)
	case partEnd:
		newLast := to
		if newLast.Before(first) {
			newLast = first // clamp: a one-day event, never flipped around
		}
		end = end.AddDate(0, 0, daysBetween(last, newLast))
	case partStart:
		newFirst := to
		if newFirst.After(last) {
			newFirst = last
		}
		start = start.AddDate(0, 0, daysBetween(first, newFirst))
	}
	switch {
	case start.Equal(oldStart) && end.Equal(oldEnd):
		return service.Changes{}, errNoChange
	case !end.After(start): // e.g. Fri 20:00–Sun 10:00 with its start dragged to Sun
		return service.Changes{}, errInverted
	}
	return service.Changes{Start: &start, End: &end}, nil
}

// daysBetween counts calendar days from a to b (both local midnights).
// Rounding absorbs the 23- and 25-hour days around DST changes.
func daysBetween(a, b time.Time) int {
	return int(math.Round(b.Sub(a).Hours() / 24))
}

// canDrag says whether the user may move this event, and if not, why.
// Guests can't change an event's time; only its organizer can.
func canDrag(e model.Event, cal model.Calendar) (bool, string) {
	if !cal.Writable() {
		return false, cal.Name + " is read-only"
	}
	if self := e.SelfAttendee(); self != nil && !self.Organizer {
		return false, "you're a guest: only the organizer can move it"
	}
	return true, ""
}

// dragPreview is the status-bar text while dragging, e.g. `Move "Trip" → Sat 21–Sun 22`.
func dragPreview(e model.Event, p part, ch service.Changes, loc *time.Location) string {
	verb := "Resize"
	if p == partBody {
		verb = "Move"
	}
	moved := e.Clone()
	moved.Start, moved.End = *ch.Start, *ch.End
	first, last := eventDays(moved, loc)
	days := first.Format("Mon 02")
	if !last.Equal(first) {
		days += "–" + last.Format("Mon 02")
	}
	return fmt.Sprintf("%s %q → %s", verb, e.Title, days)
}
```
4. Run: pass.

**Commit:** `feat(tui): drop rules for moving and resizing`

---

## Task 12.5: Wire up the mouse

> ⚠ **Fixed in Task 13.7:** the delete confirmation names its keys (`(y/n)`) with the mouse on, and says "not deleted" when cancelled. Changes `layout.go` (popup) and `nav.go` (`modeConfirm`).
>
> ⚠ **Fixed in Task 13.8:** the details box wraps to the terminal width. Changes `layout.go` (popup, `modeDetail`).
>
> ⚠ **Superseded by Task 13.9 (one width measure):** in `layout.go`'s `statusBar`, `runewidth.StringWidth(label)` becomes `width(label)`.

**Goal:** the interactive part.
- `layout.go`: `View`, pop-ups and status bar, now built with the hit map. This **replaces** `View`,
  `overlay` and `statusBar` in `model.go`.
- `mouse.go`: `handleMouse`. Clicks act on press. A press on a grid event starts a possible drag, and the release
  decides: same day means "click" (open details), another day means "drop". The wheel scrolls a week (grid) or an
  event (list). Dragging onto the header or status bar scrolls the grid.
- Refactors that keyboard users get too:
  - The detail pop-up now shows a *specific* event (`m.detail`, not "the list's cursor"), so it works from the grid.
  - In the detail pop-up, `e`, `d` and `r` act on that event; any other key still closes it.
  - RSVP targets `m.target`.
  - `applyChanges` is shared by the edit form and drops.
- With the mouse on, pop-up choices become buttons (`[This event] [Following] [All events] [Cancel]`, …), form
  fields are clickable, and the status bar ends with `[+ Add] [⟳ Sync] [? Help]`. The help screen lists mouse
  gestures. With the mouse off, the text matches the old keyboard hints.

**Files:** create `internal/tui/layout.go`, `internal/tui/mouse.go`, `internal/tui/mouse_test.go`; modify
`internal/tui/model.go`, `nav.go`, `actions.go`, `form.go`, `keys.go`, `model_test.go`; new golden
`testdata/grid_mouse.golden`

**Steps**
1. Let the fixture take settings (`newFixtureWith`):
```diff
--- a/internal/tui/model_test.go
+++ b/internal/tui/model_test.go
@@ -31,6 +31,12 @@ type fixture struct {
 // newFixture: Thursday Nov 12 2026, a real service over a temp store with a
 // weekly standup, a lunch, and a 2-day trip.
 func newFixture(t *testing.T) *fixture {
+	t.Helper()
+	return newFixtureWith(t, Deps{})
+}
+
+// newFixtureWith is newFixture with extra settings (e.g. Mouse) merged into Deps.
+func newFixtureWith(t *testing.T, extra Deps) *fixture {
 	t.Helper()
 	ny := testutil.NY(t)
 	ctx := context.Background()
@@ -49,7 +55,9 @@ func newFixture(t *testing.T) *fixture {
 	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny)
 	svc := service.New(st, testutil.Account, func() time.Time { return now }, "none")
 	f := &fixture{t: t, svc: svc, ny: ny}
-	m := New(ctx, Deps{Service: svc, Now: func() time.Time { return now }, WeekStart: time.Monday})
+	d := extra
+	d.Service, d.Now, d.WeekStart = svc, func() time.Time { return now }, time.Monday
+	m := New(ctx, d)
 	f.m = f.drain(m, m.Init())
 	f.send(tea.WindowSizeMsg{Width: 84, Height: 20})
 	return f
```
2. Gesture tests. They send real `tea.MouseMsg` press, motion and release messages at positions taken from the hit
   map, then check **outcomes in the store** ("lunch is now on the 14th"):
```go
package tui

import (
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

func TestWheelScrollsGridAWeek(t *testing.T) {
	f := newMouseFixture(t)
	before := f.model().gridStart

	f.send(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})

	if got := f.model().gridStart; !got.Equal(before.AddDate(0, 0, 7)) {
		t.Errorf("gridStart = %v, want a week after %v", got, before)
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
```
3. Run: compile failure.
4. Modify the existing files:
```diff
--- a/internal/tui/model.go
+++ b/internal/tui/model.go
@@ -7,16 +7,13 @@ import (
 	"context"
 	"errors"
 	"fmt"
-	"strings"
 	"time"
 
 	"github.com/charmbracelet/bubbles/cursor"
 	"github.com/charmbracelet/bubbles/textinput"
 	tea "github.com/charmbracelet/bubbletea"
-	"github.com/charmbracelet/lipgloss"
 
 	"github.com/jabiOP/clindar/internal/model"
-	"github.com/jabiOP/clindar/internal/output"
 	"github.com/jabiOP/clindar/internal/service"
 )
 
@@ -71,6 +68,9 @@ type Model struct {
 
 	form      form
 	gotoInput textinput.Model
+	detail    model.Event // the event shown in modeDetail
+	target    model.Event // the event an RSVP (modeRSVP) applies to
+	drag      dragState   // mouse drag in the grid (Phase 12)
 	// afterScope runs the pending action once the user picks a scope.
 	afterScope    func(service.Scope) tea.Cmd
 	allowedScopes string // e.g. "tfa" or "ta"
@@ -200,6 +200,8 @@ func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
 		return m, tea.Batch(m.load(), m.syncCmd())
 	case tea.KeyMsg:
 		return m.handleKey(msg)
+	case tea.MouseMsg:
+		return m.handleMouse(msg)
 	}
 	return m, nil
 }
@@ -211,82 +213,6 @@ func (m Model) entries() []model.Event {
 	return listEntries(model.GroupByDay(m.events, m.selected, listDays))
 }
 
-func (m Model) View() string {
-	bodyH := m.height - 1
-	var body string
-	switch m.view {
-	case viewGrid:
-		body, _ = renderGrid(gridView{
-			start: m.gridStart, today: m.today, selected: m.selected,
-			days:      model.GroupByDay(m.events, m.gridStart, gridDays),
-			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc(),
-		})
-	case viewList:
-		body, _ = renderList(listView{days: model.GroupByDay(m.events, m.selected, listDays), cursor: m.cursor,
-			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc()})
-	}
-	if overlay := m.overlay(); overlay != "" {
-		body = lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, overlay)
-	}
-	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)
-	return body + "\n" + m.statusBar()
-}
-
-func (m Model) overlay() string {
-	switch m.mode {
-	case modeHelp:
-		return modalStyle.Render(helpText())
-	case modeForm:
-		return m.form.View(min(m.width-4, 80))
-	case modeGoto:
-		return modalStyle.Render("Go to date\n\n" + m.gotoInput.View())
-	case modeDetail:
-		var b strings.Builder
-		var cals []model.Calendar
-		for _, c := range m.calendars {
-			cals = append(cals, c)
-		}
-		p, _ := output.NewPrinter(&b, output.Pretty, "", m.loc(), cals)
-		p.Event(m.entries()[m.cursor])
-		return modalStyle.Render(strings.TrimRight(b.String(), "\n"))
-	case modeScope:
-		opts := map[byte]string{'t': "(t)his event", 'f': "(f)ollowing", 'a': "(a)ll events"}
-		var parts []string
-		for i := range len(m.allowedScopes) {
-			parts = append(parts, opts[m.allowedScopes[i]])
-		}
-		return modalStyle.Render("Recurring event. Apply to:\n\n" + strings.Join(parts, "   ") + "\n\nesc cancel")
-	case modeConfirm:
-		return modalStyle.Render("Delete this event? (y/n)")
-	case modeRSVP:
-		return modalStyle.Render("Going? (y)es  (n)o  (m)aybe")
-	}
-	return ""
-}
-
-func (m Model) statusBar() string {
-	parts := []string{}
-	switch {
-	case m.syncing:
-		parts = append(parts, "⟳ syncing")
-	case m.status.LastSync.IsZero():
-		parts = append(parts, "never synced")
-	default:
-		parts = append(parts, "synced "+ago(m.deps.Now().Sub(m.status.LastSync)))
-	}
-	if m.status.Pending > 0 {
-		parts = append(parts, fmt.Sprintf("%d pending", m.status.Pending))
-	}
-	if m.status.Discarded > 0 {
-		parts = append(parts, errorStyle.Render(fmt.Sprintf("%d edit(s) discarded: see clindar log", m.status.Discarded)))
-	}
-	if m.message != "" {
-		parts = append(parts, m.message)
-	}
-	parts = append(parts, "? help")
-	return dimStyle.Render(fit(strings.Join(parts, " · "), m.width))
-}
-
 func ago(d time.Duration) string {
 	switch {
 	case d < time.Minute:
```
```diff
--- a/internal/tui/nav.go
+++ b/internal/tui/nav.go
@@ -27,10 +27,16 @@ func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
 		return m, nil
 	case modeRSVP:
 		return m.rsvpKey(k)
-	case modeHelp, modeDetail:
+	case modeDetail:
+		return m.detailKey(k)
+	case modeHelp:
 		m.mode = modeNormal
 		return m, nil
 	}
+	if m.drag.active && key.Matches(k, keys.Back) {
+		m.drag, m.message = dragState{}, "drag cancelled"
+		return m, nil
+	}
 	m.message = ""
 	switch {
 	case key.Matches(k, keys.Quit):
@@ -104,7 +110,7 @@ func (m Model) listKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
 	case current == nil:
 		return m, nil
 	case key.Matches(k, keys.Open):
-		m.mode = modeDetail
+		m.detail, m.mode = *current, modeDetail
 	case key.Matches(k, keys.Edit):
 		m.form = editEventForm(*current, m.loc())
 		m.mode = modeForm
@@ -112,11 +118,7 @@ func (m Model) listKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
 	case key.Matches(k, keys.Delete):
 		return m.startDelete(*current), nil
 	case key.Matches(k, keys.RSVP):
-		if current.SelfAttendee() == nil {
-			m.message = "you are not invited to this event"
-			return m, nil
-		}
-		m.mode = modeRSVP
+		return m.startRSVP(*current), nil
 	}
 	return m, m.reloadIfNeeded()
 }
```
```diff
--- a/internal/tui/actions.go
+++ b/internal/tui/actions.go
@@ -4,6 +4,7 @@ import (
 	"context"
 	"strings"
 
+	"github.com/charmbracelet/bubbles/key"
 	tea "github.com/charmbracelet/bubbletea"
 
 	"github.com/jabiOP/clindar/internal/model"
@@ -32,7 +33,7 @@ func (m Model) rsvpKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
 		m.mode = modeNormal
 		return m, nil
 	}
-	e := m.entries()[m.cursor]
+	e := m.target
 	id := e.ID
 	m.afterScope = func(sc service.Scope) tea.Cmd {
 		return m.act("replied "+string(resp), func(ctx context.Context, s *service.Service) error {
@@ -88,7 +89,12 @@ func (m Model) submitForm() (tea.Model, tea.Cmd) {
 		m.form.err = err.Error()
 		return m, nil
 	}
-	e := *m.form.editing
+	return m.applyChanges(*m.form.editing, ch)
+}
+
+// applyChanges saves an edit, asking for a scope first if e is part of a
+// series. Both the edit form and a mouse drop end here.
+func (m Model) applyChanges(e model.Event, ch service.Changes) (tea.Model, tea.Cmd) {
 	m.afterScope = func(sc service.Scope) tea.Cmd {
 		return m.act("saved "+e.Title, func(ctx context.Context, s *service.Service) error {
 			_, err := s.Update(ctx, e.ID, ch, sc)
@@ -102,4 +108,29 @@ func (m Model) submitForm() (tea.Model, tea.Cmd) {
 	return m, m.afterScope(service.ScopeNone)
 }
 
+// startRSVP asks how to answer an invitation to e.
+func (m Model) startRSVP(e model.Event) Model {
+	if e.SelfAttendee() == nil {
+		m.mode, m.message = modeNormal, "you are not invited to this event"
+		return m
+	}
+	m.target, m.mode = e, modeRSVP
+	return m
+}
+
+// detailKey: e/d/r act on the event shown; any other key closes the pop-up.
+func (m Model) detailKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
+	switch {
+	case key.Matches(k, keys.Edit):
+		m.form, m.mode = editEventForm(m.detail, m.loc()), modeForm
+	case key.Matches(k, keys.Delete):
+		m = m.startDelete(m.detail)
+	case key.Matches(k, keys.RSVP):
+		m = m.startRSVP(m.detail)
+	default:
+		m.mode = modeNormal
+	}
+	return m, nil
+}
+
 // --- view ---
```
```diff
--- a/internal/tui/form.go
+++ b/internal/tui/form.go
@@ -177,24 +177,37 @@ func splitEmails(s string) []string {
 	return out
 }
 
-func (f form) View(width int) string {
+// view draws the form as a pop-up. Each field line is clickable (it focuses
+// that field); with the mouse on, [Save] and [Cancel] are buttons.
+func (f form) view(width int, mouse bool) modal {
 	title := "New event"
 	if f.editing != nil {
 		title = "Edit event"
 	}
-	var b strings.Builder
-	b.WriteString(headerStyle.Render(title) + "\n\n")
+	var md modal
+	md.text(headerStyle.Render(title))
+	md.text("")
 	for i, in := range f.inputs {
 		label := fit(fieldLabels[i], 10)
 		if i == f.focus {
 			label = selectedStyle.Render(label)
 		}
 		in.Width = max(width-16, 20)
-		b.WriteString(label + "  " + in.View() + "\n")
+		md.field(label+"  "+in.View(), i)
 	}
 	if f.err != "" {
-		b.WriteString("\n" + errorStyle.Render(f.err) + "\n")
+		md.text("")
+		md.text(errorStyle.Render(f.err))
 	}
-	b.WriteString("\n" + dimStyle.Render("tab/↑↓ move · enter save · esc cancel"))
-	return modalStyle.Render(b.String())
+	md.text("")
+	if mouse {
+		md.buttons(button{"Save", "enter"}, button{"Cancel", "esc"})
+	}
+	md.text(dimStyle.Render("tab/↑↓ move · enter save · esc cancel"))
+	return md
+}
+
+// focusField moves the cursor to field i (a mouse click on it).
+func (f form) focusField(i int) form {
+	return f.move(i - f.focus)
 }
```
```diff
--- a/internal/tui/keys.go
+++ b/internal/tui/keys.go
@@ -28,12 +28,19 @@ var keys = struct {
 	Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back / cancel")),
 }
 
-func helpText() string {
+func helpText(mouse bool) string {
 	s := "Keys\n\n"
 	for _, b := range []key.Binding{keys.Left, keys.Right, keys.Up, keys.Down, keys.ScrollUp, keys.ScrollDown,
 		keys.Today, keys.Goto, keys.Toggle, keys.Open, keys.Add, keys.Edit, keys.Delete, keys.RSVP,
 		keys.Sync, keys.Back, keys.Quit} {
 		s += "  " + fit(b.Help().Key, 8) + b.Help().Desc + "\n"
 	}
+	if mouse {
+		s += "\nMouse\n\n" +
+			"  click a day to select it; click it again to open the list\n" +
+			"  click an event to see it; scroll to move a week\n" +
+			"  drag an event to another day; drag ◂ or ▸ to change its first or last day\n" +
+			"  hold Shift (Option on some Macs) to select text\n"
+	}
 	return s + "\nPress any key to close."
 }
```
5. Create `layout.go`:
```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/output"
)

func (m Model) View() string {
	s, _ := m.layout()
	return s
}

// layout draws the screen and returns what is clickable where. View and the
// mouse handler both call it, so a click always matches what was drawn.
func (m Model) layout() (string, hitMap) {
	bodyH := m.height - 1
	var body string
	var hits hitMap
	switch m.view {
	case viewGrid:
		body, hits = renderGrid(gridView{
			start: m.gridStart, today: m.today, selected: m.selected,
			days:      model.GroupByDay(m.events, m.gridStart, gridDays),
			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc(),
			handles: m.deps.Mouse, drop: m.drag.over,
		})
	case viewList:
		body, hits = renderList(listView{days: model.GroupByDay(m.events, m.selected, listDays), cursor: m.cursor,
			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc()})
	}
	if md, ok := m.popup(); ok {
		box, boxHits := md.render()
		var x, y int
		body, x, y = overlay(m.width, bodyH, box)
		hits = boxHits.offset(x, y) // while a pop-up is open, only the pop-up is clickable
	}
	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)
	bar, barHits := m.statusBar()
	if m.mode == modeNormal {
		hits = append(hits, barHits.offset(0, bodyH)...)
	}
	return body + "\n" + bar, hits
}

// popup builds the pop-up for the current mode, if any. With the mouse on,
// choices are buttons that act like their keys; without it, the keyboard
// hints read as before.
func (m Model) popup() (modal, bool) {
	var md modal
	choices := func(plain string, bs ...button) {
		if m.deps.Mouse {
			md.buttons(bs...)
		} else {
			md.text(plain)
		}
	}
	switch m.mode {
	case modeHelp:
		md.text(helpText(m.deps.Mouse))
	case modeForm:
		return m.form.view(min(m.width-4, 80), m.deps.Mouse), true
	case modeGoto:
		md.text("Go to date")
		md.text("")
		md.text(m.gotoInput.View())
		if m.deps.Mouse {
			md.text("")
			md.buttons(button{"Go", "enter"}, button{"Cancel", "esc"})
		}
	case modeDetail:
		var b strings.Builder
		var cals []model.Calendar
		for _, c := range m.calendars {
			cals = append(cals, c)
		}
		p, _ := output.NewPrinter(&b, output.Pretty, "", m.loc(), cals)
		p.Event(m.detail)
		md.text(strings.TrimRight(b.String(), "\n"))
		md.text("")
		choices(dimStyle.Render("e edit · d delete · r RSVP · esc close"),
			button{"Edit", "e"}, button{"Delete", "d"}, button{"RSVP", "r"}, button{"Close", "esc"})
	case modeScope:
		labels := map[byte]button{'t': {"This event", "t"}, 'f': {"Following", "f"}, 'a': {"All events", "a"}}
		plain := map[byte]string{'t': "(t)his event", 'f': "(f)ollowing", 'a': "(a)ll events"}
		var bs []button
		var parts []string
		for i := range len(m.allowedScopes) {
			bs = append(bs, labels[m.allowedScopes[i]])
			parts = append(parts, plain[m.allowedScopes[i]])
		}
		md.text("Recurring event. Apply to:")
		md.text("")
		choices(strings.Join(parts, "   ")+"\n\nesc cancel", append(bs, button{"Cancel", "esc"})...)
	case modeConfirm:
		md.text("Delete this event?")
		choices("(y/n)", button{"Delete", "y"}, button{"Cancel", "n"})
	case modeRSVP:
		md.text("Going?")
		choices("(y)es  (n)o  (m)aybe", button{"Yes", "y"}, button{"No", "n"}, button{"Maybe", "m"}, button{"Cancel", "esc"})
	default:
		return modal{}, false
	}
	return md, true
}

// statusBar shows sync state and messages. With the mouse on, it ends with
// [+ Add] [⟳ Sync] [? Help] buttons; hits are relative to the bar.
func (m Model) statusBar() (string, hitMap) {
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
	if !m.deps.Mouse {
		parts = append(parts, "? help")
		return dimStyle.Render(fit(strings.Join(parts, " · "), m.width)), nil
	}
	var hits hitMap
	var labels []string
	x := m.width
	bs := []button{{"+ Add", "a"}, {"⟳ Sync", "s"}, {"? Help", "?"}}
	for i := len(bs) - 1; i >= 0; i-- { // lay out right to left
		label := "[" + bs[i].label + "]"
		x -= runewidth.StringWidth(label)
		hits.add(x, 0, runewidth.StringWidth(label), 1, target{kind: targetKey, key: bs[i].key})
		labels = append([]string{label}, labels...)
		x--
	}
	info := dimStyle.Render(fit(strings.Join(parts, " · "), max(x, 0)))
	return info + " " + strings.Join(labels, " "), hits
}
```
6. Create `mouse.go`:
```go
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/model"
)

// dragState tracks a drag in the grid: what was grabbed, where, and the day
// currently under the pointer.
type dragState struct {
	active bool
	event  model.Event
	part   part
	from   time.Time // the day the event was grabbed on
	over   time.Time // the day under the pointer now
}

// handleMouse turns mouse messages into the same actions the keys use.
// Clicks act on press, except on grid events: a press there starts a possible
// drag, and the release decides between "click" and "drop".
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.deps.Mouse {
		return m, nil
	}
	_, hits := m.layout()
	t, onSomething := hits.at(msg.X, msg.Y)
	switch {
	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
		return m.wheel(-1)
	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
		return m.wheel(1)
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		return m.press(t, onSomething)
	case msg.Action == tea.MouseActionMotion && m.drag.active:
		return m.dragOver(t, msg.Y)
	case msg.Action == tea.MouseActionRelease && m.drag.active:
		return m.drop()
	}
	return m, nil
}

func (m Model) wheel(dir int) (tea.Model, tea.Cmd) {
	if m.mode != modeNormal {
		return m, nil
	}
	if m.view == viewList {
		m.cursor = min(max(m.cursor+dir, 0), max(len(m.entries())-1, 0))
		return m, nil
	}
	m = m.moveDays(7*dir, true)
	return m, m.reloadIfNeeded()
}

func (m Model) press(t target, onSomething bool) (tea.Model, tea.Cmd) {
	if m.mode != modeNormal {
		switch {
		case t.kind == targetKey:
			return m.handleKey(keyMsg(t.key))
		case t.kind == targetField && m.mode == modeForm:
			m.form = m.form.focusField(t.field)
		case !onSomething && (m.mode == modeDetail || m.mode == modeGoto):
			m.mode = modeNormal // click outside closes
		case m.mode == modeHelp:
			m.mode = modeNormal // any click closes help
		}
		return m, nil
	}
	m.message = ""
	switch t.kind {
	case targetKey: // status bar buttons
		return m.handleKey(keyMsg(t.key))
	case targetDay:
		if t.day.Equal(m.selected) {
			m.view, m.cursor = viewList, 0
			return m, m.reloadIfNeeded()
		}
		m = m.selectDay(t.day)
	case targetMore:
		m = m.selectDay(t.day)
		m.view, m.cursor = viewList, 0
	case targetEvent:
		m = m.selectDay(t.day)
		if ok, why := canDrag(t.event, m.calendars[t.event.CalendarID]); !ok {
			m.detail, m.mode, m.message = t.event, modeDetail, why
			return m, nil
		}
		m.drag = dragState{active: true, event: t.event, part: t.part, from: t.day, over: t.day}
	case targetEntry:
		if t.entry == m.cursor {
			m.detail, m.mode = m.entries()[m.cursor], modeDetail
		} else {
			m.cursor = t.entry
		}
	}
	return m, m.reloadIfNeeded()
}

// dragOver follows the pointer. Dragging onto the grid's header scrolls a
// week back; onto the status bar, a week forward.
func (m Model) dragOver(t target, y int) (tea.Model, tea.Cmd) {
	switch {
	case t.kind == targetDay || t.kind == targetEvent || t.kind == targetMore:
		m.drag.over = t.day
	case y <= 1:
		m = m.moveDays(-7, true)
	case y >= m.height-1:
		m = m.moveDays(7, true)
	}
	m.message = ""
	if ch, err := dragChanges(m.drag.event, m.drag.part, m.drag.from, m.drag.over, m.loc()); err == nil {
		m.message = dragPreview(m.drag.event, m.drag.part, ch, m.loc())
	}
	return m, m.reloadIfNeeded()
}

func (m Model) drop() (tea.Model, tea.Cmd) {
	d := m.drag
	m.drag = dragState{}
	if d.over.Equal(d.from) { // no movement: it was a click
		m.detail, m.mode, m.message = d.event, modeDetail, ""
		return m, nil
	}
	ch, err := dragChanges(d.event, d.part, d.from, d.over, m.loc())
	if err != nil {
		m.message = "not moved: " + err.Error()
		return m, nil
	}
	return m.applyChanges(d.event, ch)
}

func (m Model) selectDay(day time.Time) Model {
	return m.moveDays(daysBetween(m.selected, day), false)
}

// keyMsg turns a button's key name into the key press it stands for.
func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}
```
7. `go test ./internal/tui -update -run TestMouseGridGolden`, then check it: handles on every event line, and the status
   bar ends with the three buttons:
```text
November 2026                                                                                                   
Mon             Tue             Wed             Thu             Fri             Sat             Sun             
 9              10              11              ▶12             13              14              15              
◂09:00 Standup▸                                 ◂09:00 Standup▸                                                 
                                                ◂12:00 Lunch …▸                                                 
                                                                                                                
16              17              18              19              20              21              22              
◂09:00 Standup▸                                 ◂09:00 Standup▸ ◂Trip to Lisbon Trip to Lisbon▸                 
                                                                                                                
                                                                                                                
23              24              25              26              27              28              29              
◂09:00 Standup▸                                 ◂09:00 Standup▸                                                 
                                                                                                                
                                                                                                                
30              Dec 1            2               3               4               5               6              
◂09:00 Standup▸                                 ◂09:00 Standup▸                                                 
                                                                                                                
                                                                                                                
                                                                                                                
never synced                                                                           [+ Add] [⟳ Sync] [? Help]
```
8. `go test -race ./...`: pass. **Check the red step was real:** temporarily make `drop()` return without calling
   `applyChanges`. The three drag tests must fail. Then undo that.

**Commit:** `feat(tui): clicks, wheel, buttons, and drag-and-drop`

---

## Task 12.6: Try it in a real terminal

No code. With `mouse = true` in `config.toml`, using the "clindar-test" calendar:

- [ ] Click days, click a selected day (the list opens), and wheel up and down in grid and list.
- [ ] Click an event, then use [Edit] [Delete] [RSVP] [Close], and click outside to close.
- [ ] Drag a one-off event to another day, then check the web UI.
- [ ] Drag a trip's `▸` two days later and its `◂` one day earlier, then check the web UI.
- [ ] Drag a recurring instance: the scope picker appears and works with clicks.
- [ ] Try to drag an invitation: details open, with a "you're a guest" message.
- [ ] Press Esc mid-drag: nothing changes.
- [ ] Hold Shift (Option in some macOS terminals) and drag: normal text selection works.
- [ ] Set `mouse = false`: no handles, no buttons, and clicks do nothing.
- [ ] Try a narrow terminal (80 columns) and a wide one, and note the readability in `DEVIATIONS.md`.

Add `mouse` to the config table in `docs/cli.md` (Task 11.2).

**Commit:** `docs: mouse support`

---

# Phase 12, part 2: adaptive handles and smarter detail buttons

Two follow-ups from the implementer's notes after Tasks 12.1–12.6, agreed with the product owner:
1. **Handles appear on hover, at the edge.** The `◂` symbol shows only while the pointer is on the leftmost cell of an
   event's first-day line, and `▸` only on the rightmost cell of its last-day line. Every other line keeps its full title.
2. **The detail pop-up hides actions you can't take.**

> ⚠️ **The code in Tasks 12.7–12.9 was written as a plan only; it was not compiled or run** (unlike Tasks 12.1–12.6).
> Treat it as a precise spec. Names match the code from 12.1–12.6. If the compiler disagrees, follow the intent,
> fix it, and note the fix in `DEVIATIONS.md`.

**The key idea:** the **grab zones always exist**, and hover only decides whether the *symbol* is drawn. With the mouse on,
the leftmost cell of an event's first-day line is always the start handle, and the rightmost cell of its last-day
line is always the end handle. So:
- By the time a symbol appears, the pointer is already on it.
- In a terminal that doesn't report hover, grabbing an edge still works; you just don't see the symbol until you drag.
- The symbols of the handle being dragged stay visible for the whole drag.

**Trade-off:** hover needs "all motion" mouse mode (`tea.WithMouseAllMotion()`). The terminal then reports every pointer
move, not only moves while a button is held. Most terminals support it (iTerm2, kitty, WezTerm, Alacritty, GNOME Terminal,
Windows Terminal, and tmux with `set -g mouse on`). macOS Terminal.app is unverified; Task 12.9 checks it.

---

## Task 12.7: Handles on hover

> ⚠ **Superseded by Task 13.9 (one width measure):** in the `eventLine` code below, `runewidth.StringWidth(start+end)` becomes `width(start+end)`.

**Goal:** with mouse on and nothing hovered, the grid looks exactly like the mouse-off grid (apart from the status-bar
buttons). Edges stay grabbable. A symbol appears only on the edge cell under the pointer, or for the handle being dragged.

**Files:** modify `internal/tui/grid.go`, `internal/tui/layout.go`, `internal/tui/model.go`, `internal/tui/mouse.go`,
`internal/tui/grid_test.go`, `internal/tui/mouse_test.go`; golden `grid_handles` becomes `grid_hover`, and
`grid_mouse` is regenerated

**Steps**

1. **Tests first.**

   a. In `grid_test.go`, give `gridWithTrip` a hover argument **before** its drop argument,
      `gridWithTrip(t *testing.T, hover edgeRef, drop time.Time)`, and in its view replace `handles: true` with
      `mouse: true, hover: hover` (keep `drop: drop`). Existing callers pass `edgeRef{}` as the new argument.
      Replace `TestGridHandles` with:
   ```go
   // Hovering the trip's end edge (Friday) draws only that ▸; every other line keeps its full text.
   func TestGridHoverShowsOneHandle(t *testing.T) {
   	trip := testutil.AllDay("trip", "Trip", 2026, time.November, 12, 2) // same IDs as in gridWithTrip
   	got, _ := gridWithTrip(t, edgeOf(trip, partEnd), time.Time{})

   	testutil.Golden(t, "grid_hover", got)
   	if strings.Count(got, "▸") != 1 || strings.Contains(got, "◂") {
   		t.Errorf("want exactly one ▸ and no ◂:\n%s", got)
   	}
   }
   ```
   Review `testdata/grid_hover.golden` after `-update`: `09:00 Dentist` must be **untruncated**, Thursday shows
   `Trip`, and Friday shows `Trip …▸`.

   b. Rewrite `TestGridHitMap`. Nothing is drawn now, so locate edges by **column**, not by symbol. At width 112 each day
   is 16 columns wide, and the content is 15 (the 16th is the gutter):
   ```go
   func TestGridHitMap(t *testing.T) {
   	ny := testutil.NY(t)
   	screen, hits := gridWithTrip(t, edgeRef{}, time.Time{}) // no hover: the zones must exist anyway
   	thu, fri := time.Date(2026, 11, 12, 0, 0, 0, 0, ny), time.Date(2026, 11, 13, 0, 0, 0, 0, ny)
   	_, y := find(t, screen, "Trip") // first occurrence: Thursday's line
   	thuX, friX := 3*16, 4*16

   	if got, _ := hits.at(thuX, y); got.part != partStart || !got.day.Equal(thu) || got.event.ID != "trip" {
   		t.Errorf("Thu left edge = %+v, want the start handle", got)
   	}
   	if got, _ := hits.at(thuX+5, y); got.part != partBody {
   		t.Errorf("Thu middle = %+v, want the body", got)
   	}
   	if got, _ := hits.at(friX+14, y); got.part != partEnd || !got.day.Equal(fri) {
   		t.Errorf("Fri right edge = %+v, want the end handle", got)
   	}
   	if got, _ := hits.at(thuX+14, y); got.part != partBody {
   		t.Errorf("Thu right edge = %+v: not the last day, so no end handle there", got)
   	}
   }
   ```
   Keep the "blank line is Tuesday" check from the old version. `grid_test.go` no longer uses `runewidth` after
   this rewrite, so **remove that import** or the package won't compile. `TestGridMarksDropTarget` only needs the extra
   argument: `gridWithTrip(t, edgeRef{}, time.Date(2026, 11, 14, 0, 0, 0, 0, ny))`.

   c. In `mouse_test.go`, add:
   ```go
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
   ```
   `TestDragEndHandleLengthensTrip` stays **unchanged**. It never hovers, so it now also proves that edges are
   grabbable without hover. Add a comment saying so.
2. Run: compile failure (`edgeRef`, `edgeOf`, `mouse`, `hover` don't exist yet).
3. **`grid.go`**: replace the `handles bool` field and change `eventLine`:
   ```go
   	mouse   bool      // grab zones at event edges (Phase 12)
   	hover   edgeRef   // the edge under the pointer: draw its symbol
   	grabbed edgeRef   // the handle being dragged: draw its symbol
   	drop    time.Time // while dragging: the day under the pointer (zero = none)
   ```
   ```go
   // edgeRef names one end of one event (zero value = none).
   type edgeRef struct {
   	calendarID, eventID string
   	part                part
   }

   func edgeOf(e model.Event, p part) edgeRef { return edgeRef{e.CalendarID, e.ID, p} }

   // eventLine renders one event in a cell w cells wide. With the mouse on, the
   // first-day line's leftmost cell is always the start handle and the last-day
   // line's rightmost cell the end handle; their symbols are drawn only while
   // hovered or dragged.
   func (g gridView) eventLine(e model.Event, day time.Time, w int) (string, hitMap) {
   	var hits hitMap
   	hits.add(0, 0, w, 1, target{kind: targetEvent, day: day, event: e, part: partBody})
   	start, end := "", ""
   	if g.mouse {
   		first, last := eventDays(e, g.loc)
   		if day.Equal(first) {
   			hits.add(0, 0, 1, 1, target{kind: targetEvent, day: day, event: e, part: partStart})
   			if g.showing(e, partStart) {
   				start = "◂"
   			}
   		}
   		if day.Equal(last) {
   			hits.add(w-1, 0, 1, 1, target{kind: targetEvent, day: day, event: e, part: partEnd})
   			if g.showing(e, partEnd) {
   				end = "▸"
   			}
   		}
   	}
   	text := start + fit(eventLabel(e, g.loc), w-runewidth.StringWidth(start+end)) + end
   	return calendarStyle(g.calendars[e.CalendarID].Color).Render(text), hits
   }

   func (g gridView) showing(e model.Event, p part) bool {
   	r := edgeOf(e, p)
   	return r == g.hover || r == g.grabbed
   }
   ```
   (A multi-day event has the same ID in every cell, so hovering its end on Saturday shows `▸` only on Saturday's line.)
4. **`model.go`**: add `hover edgeRef // the handle under the pointer (Phase 12.7)` to `Model`. In `Run`, replace
   `tea.WithMouseCellMotion()` with `tea.WithMouseAllMotion()` and update the comment ("all motion: hover reveals
   handles").
5. **`layout.go`**: in the `gridView{…}` literal, replace `handles: m.deps.Mouse` with:
   ```go
   			mouse: m.deps.Mouse, hover: m.hover, grabbed: m.grabbedEdge(), drop: m.drag.over,
   ```
   and add to `mouse.go`:
   ```go
   // grabbedEdge is the handle being dragged (none while dragging an event's body).
   func (m Model) grabbedEdge() edgeRef {
   	if !m.drag.active || m.drag.part == partBody {
   		return edgeRef{}
   	}
   	return edgeOf(m.drag.event, m.drag.part)
   }
   ```
6. **`mouse.go`**: in `handleMouse`, add this case **before** the `msg.Action == tea.MouseActionMotion && m.drag.active` case:
   ```go
   	case msg.Action == tea.MouseActionMotion && !m.drag.active:
   		m.hover = edgeRef{}
   		if t.kind == targetEvent && t.part != partBody {
   			m.hover = edgeOf(t.event, t.part)
   		}
   		return m, nil
   ```
7. `go test ./internal/tui -update -run 'TestGridHoverShowsOneHandle|TestMouseGridGolden'`, then **review both golden
   files**. `grid_mouse.golden` should now show no `◂`/`▸` at all, with full titles and the status-bar buttons. Delete
   the obsolete `testdata/grid_handles.golden`. Then run `go test -race ./internal/tui` without `-update`.
8. **Check the red step:** temporarily make `showing` always return `true`. `TestHoverOnEdgeRevealsHandle` and
   `TestGridHoverShowsOneHandle` must fail. Then undo that.

**Commit:** `feat(tui): show resize handles only when hovering an event's edge`

---

## Task 12.8: Hide detail actions you can't take

> ⚠ **Fixed in Task 13.3:** RSVP also needs a writable calendar, with its own reason (`whyNoRSVP`).

**Goal:** the detail pop-up only offers actions that can succeed, and the keys `e`/`d`/`r` follow the same rules:
- **[Edit]** / `e` only if the event can be changed: a writable calendar and you're not a guest. This is `canDrag`'s
  rule from Task 12.4: one rule, reused.
- **[Delete]** / `d` only on a writable calendar. Deleting an invitation from your own calendar is allowed.
- **[RSVP]** / `r` only if you're invited.

When a key is refused, the pop-up stays open and the status bar says why. **Hide, don't dim:** a dimmed button would need
its own "clicked but disabled" case. A hidden one isn't in the hit map at all.

**Files:** modify `internal/tui/drag.go`, `internal/tui/layout.go`, `internal/tui/actions.go`; create
`internal/tui/detail_test.go`

**Steps**
1. Tests. `detail_test.go`:
   ```go
   package tui

   import (
   	"strings"
   	"testing"
   	"time"

   	"github.com/jabiOP/clindar/internal/model"
   	"github.com/jabiOP/clindar/internal/testutil"
   )

   // openDetail shows e in the detail pop-up, as a click would.
   func (f *fixture) openDetail(e model.Event) {
   	m := f.model()
   	m.detail, m.mode = e, modeDetail
   	f.m = m
   }

   func TestDetailOfInviteHidesEdit(t *testing.T) {
   	f := newMouseFixture(t)
   	invite := testutil.Timed("inv", "Their party", time.Date(2026, 11, 20, 18, 0, 0, 0, f.ny), time.Hour)
   	invite.Attendees = []model.Attendee{
   		{Email: testutil.Account, Self: true}, {Email: "host@example.com", Organizer: true}}
   	f.openDetail(invite)

   	screen := f.m.View()
   	if strings.Contains(screen, "[Edit]") || !strings.Contains(screen, "[Delete]") || !strings.Contains(screen, "[RSVP]") {
   		t.Errorf("invite: want [Delete] [RSVP] but no [Edit]:\n%s", screen)
   	}
   	f.keys("e")
   	if m := f.model(); m.mode != modeDetail || !strings.Contains(m.message, "guest") {
   		t.Errorf("e on an invite: mode = %v, message = %q; want the pop-up kept open and the reason shown", m.mode, m.message)
   	}
   }

   func TestDetailOfOwnEventHidesRSVP(t *testing.T) {
   	f := newMouseFixture(t)
   	lunch := testutil.Timed("lunch", "Lunch with Sam", time.Date(2026, 11, 12, 12, 0, 0, 0, f.ny), time.Hour)
   	f.openDetail(lunch)

   	screen := f.m.View()
   	if strings.Contains(screen, "[RSVP]") || !strings.Contains(screen, "[Edit]") {
   		t.Errorf("own event: want [Edit] but no [RSVP]:\n%s", screen)
   	}
   }

   func TestDetailOnReadOnlyCalendarOffersOnlyClose(t *testing.T) {
   	f := newMouseFixture(t)
   	hol := testutil.AllDay("thx", "Thanksgiving", 2026, time.November, 26, 1)
   	hol.CalendarID = "holidays" // not in the fixture's calendars: treated as read-only
   	f.openDetail(hol)

   	screen := f.m.View()
   	for _, b := range []string{"[Edit]", "[Delete]", "[RSVP]"} {
   		if strings.Contains(screen, b) {
   			t.Errorf("read-only event shows %s", b)
   		}
   	}
   }
   ```
2. Run: the tests fail on the button checks (they compile already).
3. **`drag.go`**: add next to `canDrag`:
   ```go
   // allowed says which detail actions can succeed for e, and why editing can't.
   type allowed struct {
   	edit, delete, rsvp bool
   	whyNoEdit          string
   }

   func permissions(e model.Event, cal model.Calendar) allowed {
   	edit, why := canDrag(e, cal)
   	return allowed{edit: edit, delete: cal.Writable(), rsvp: e.SelfAttendee() != nil, whyNoEdit: why}
   }
   ```
4. **`layout.go`**, in `popup`, replace the `modeDetail` choices call with:
   ```go
   		can := permissions(m.detail, m.calendars[m.detail.CalendarID])
   		var bs []button
   		var hints []string
   		if can.edit {
   			bs, hints = append(bs, button{"Edit", "e"}), append(hints, "e edit")
   		}
   		if can.delete {
   			bs, hints = append(bs, button{"Delete", "d"}), append(hints, "d delete")
   		}
   		if can.rsvp {
   			bs, hints = append(bs, button{"RSVP", "r"}), append(hints, "r RSVP")
   		}
   		bs, hints = append(bs, button{"Close", "esc"}), append(hints, "esc close")
   		choices(dimStyle.Render(strings.Join(hints, " · ")), bs...)
   ```
5. **`actions.go`**, in `detailKey`, check permissions before acting:
   ```go
   	can := permissions(m.detail, m.calendars[m.detail.CalendarID])
   	switch {
   	case key.Matches(k, keys.Edit) && !can.edit:
   		m.message = "can't edit: " + can.whyNoEdit
   	case key.Matches(k, keys.Delete) && !can.delete:
   		m.message = "can't delete: the calendar is read-only"
   	case key.Matches(k, keys.RSVP) && !can.rsvp:
   		m.message = "you are not invited to this event"
   	case key.Matches(k, keys.Edit):
   		m.form, m.mode = editEventForm(m.detail, m.loc()), modeForm
   	case key.Matches(k, keys.Delete):
   		m = m.startDelete(m.detail)
   	case key.Matches(k, keys.RSVP):
   		m = m.startRSVP(m.detail)
   	default:
   		m.mode = modeNormal
   	}
   	return m, nil
   ```
6. `go test -race ./internal/tui`: pass. Then mouse-off check: `TestDetail…` uses the mouse fixture. Also open an
   invite's details with the mouse off in the real TUI, and check the hint line only lists `r RSVP · esc close`
   (plus `d delete` on a writable calendar).

**Commit:** `feat(tui): only offer detail actions that can succeed`

---

## Task 12.9: Re-check in a real terminal

Same setup as Task 12.6, then:
- [ ] At **80 columns**, titles read normally. No `◂`/`▸` is visible until you point at an event's edge.
- [ ] Hover the left edge of an event's first day: `◂` appears. Hover the right edge of its last day: `▸`. Move away and
      they disappear. Only the line under the pointer changes.
- [ ] Drag a trip's right edge two days later without waiting for the symbol: it still works.
- [ ] **macOS Terminal.app:** does hover reveal the symbols? Record yes or no in `DEVIATIONS.md`. If no, edges must still
      be grabbable (as in the previous check).
- [ ] tmux with `set -g mouse on`: hover works.
- [ ] Open the details of an invitation and of a read-only (e.g. holidays) event: only the possible buttons appear, and
      `e`/`d`/`r` explain refusals in the status bar.

**Commit:** `docs: note terminal hover support`

---

## Task 12.10: Two small fixes from the implementer's notes

> Written as a plan only, not compiled (like Tasks 12.7–12.9).

**1. "Go to date" keeps a stale list cursor (bug).** In the list, after `g` + a date + Enter, `m.cursor` keeps its old
index into the *new* day's entries, so Enter can open the wrong event. `h`/`l` and `t` already reset it to the first
event; `g` must too.

**2. One message for "edit" and "drag".** `canDrag`'s reason says only the organizer can "move" it, but the same reason is
shown when `e` is refused on an invitation (Task 12.8). Use "change", which fits both.

**Files:** modify `internal/tui/nav.go`, `internal/tui/drag.go`; add a test to `internal/tui/model_test.go`

**Steps**
1. Test, in `model_test.go`:
   ```go
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
   ```
   Run: it fails with `cursor = 1`.
2. Fix `gotoKey` in `nav.go`: in the Enter branch, after `m = m.moveDays(…)`, add:
   ```go
   		m.cursor = 0 // like h/l/t: a new day starts at its first event
   ```
3. In `drag.go`, change `canDrag`'s guest message to:
   ```go
   		return false, "you're a guest: only the organizer can change it"
   ```
   The Task 12.8 test only checks for "guest", so it still passes.
4. `go test -race ./internal/tui`: pass.

**Commit:** `fix(tui): reset list cursor after go-to-date; neutral guest message`
