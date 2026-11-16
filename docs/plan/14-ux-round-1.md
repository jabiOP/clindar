# Phase 14: UX round 1

Thirteen improvements from real use, agreed with the product owner (brief "UX round 1"; design `00-design.md` §6c).
**Starting point:** the implementation branch at `8ee3bd4` (Phases 1–13 done).

> ⚠ **This whole phase was written as a plan only: nothing in it was compiled or run.** Code is a precise spec:
> names match the branch at `8ee3bd4`. If the compiler disagrees, follow the intent, fix it, and log it in
> `DEVIATIONS.md`. Every task states its **red step**: make sure the new test fails for the stated reason before you
> write the code.

**Decisions that shape this phase**
- **Vim keys everywhere new** (`h j k l`, `/`, `Esc`); arrows keep working.
- **Dates with slashes are day/month** (`20/11`), with a config key `date_order = "mdy"` to flip it. **English names
  only** (`fri`, `dec`); no Spanish month or weekday names.
- **In go-to, a bare number is a day of the month** (`15` = the next 15th). Months are reached by name (`dec`) or
  with a day (`1/12`).
- **Past days are dimmed with color only:** golden files (colors off) don't change.
- **Two items were out of scope and now aren't:** per-event colors and Meet links (reading them; we never *create*
  Meet links).

**Task order** follows the brief: small items first, then the list and event mode, dates, the shared picker, and
finally the features built on it.

| Task | Item | What |
|---|---|---|
| 14.1 | 13 | Help text: "Option on macOS, Shift elsewhere" |
| 14.2 | 9 | `J`/`K` move 4 weeks |
| 14.3 | 8 | Past days dimmed |
| 14.4 | 7 | Slower mouse wheel (1 day per step, throttled) |
| 14.5 | 3 | Edge grab: no surprise details box |
| 14.6 | 2 | `Ctrl-J`/`Ctrl-K` between form fields |
| 14.7 | 12 | List: endless scrolling, page keys, fills the screen |
| 14.8 | 1 | Event mode in the grid |
| 14.9 | 11 | `D/M` dates and `date_order` |
| 14.10 | 11 | Go-to understands days, months, day numbers; no times |
| 14.11 | 4 | Reusable picker component |
| 14.12 | 4 | Local calendar visibility (store, service, CLI) |
| 14.13 | 4 | Calendar menu in the TUI |
| 14.14 | 6 | Quick-add parser (shared by CLI and TUI) |
| 14.15 | 6 | `clindar add "Dentist 20/11 15:30 45m"` |
| 14.16 | 6 | Quick add in the TUI |
| 14.17 | 6 | Full form: date, all-day, pickers, errors by field |
| 14.18 | 6 | Mouse: click or drag empty space to add |
| 14.19 | 5 | Per-event colors: data (model, Google, store, JSON) |
| 14.20 | 5 | Per-event colors: display and Color picker |
| 14.21 | 10 | Details: no internals, [Open in Google] |
| 14.22 | 10 | Meet links: show, [Join], [Copy] |
| 14.23 | – | Real-terminal and real-Google checks |

**Test helpers added along the way** (in `internal/tui/model_test.go`, Task 14.4): a movable clock (`f.clock`), and
`f.put(e)` to add an event to the fixture's store and reload. Later tasks rely on them.

---

## Task 14.1: Selecting text: "Option on macOS, Shift elsewhere" (item 13)

**Goal:** the help screen, design §6b and the docs say exactly what works: the user confirmed **Option**-drag on macOS.

**Files:** `internal/tui/keys.go`, `internal/tui/keys_test.go` (new)

1. Test:
   ```go
   package tui

   import (
   	"strings"
   	"testing"
   )

   func TestHelpExplainsTextSelection(t *testing.T) {
   	if got := helpText(true); !strings.Contains(got, "Option-drag on macOS, Shift-drag elsewhere") {
   		t.Errorf("help text:\n%s", got)
   	}
   }
   ```
   **Red:** fails (the text says "hold Shift (Option on some Macs)").
2. In `helpText`, replace the line `"  hold Shift (Option on some Macs) to select text\n"` with
   `"  select text: Option-drag on macOS, Shift-drag elsewhere\n"`.
3. If `docs/cli.md` or the README mention Shift, update them the same way. (Design §6b is already updated.)

**Commit:** `docs(tui): say Option on macOS, Shift elsewhere for selecting text`

---

## Task 14.2: `J`/`K` move 4 weeks (item 9)

**Goal:** one press moves a whole screen (the grid shows 4 weeks), like Google's 4-week view. `PgDn`/`PgUp` stay on
the same bindings. (In the list, Task 14.7 gives `J`/`K` a list-specific meaning.)

**Files:** `internal/tui/keys.go`, `internal/tui/nav.go`, `internal/tui/model_test.go`

1. Test (in `model_test.go`):
   ```go
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
   ```
   **Red:** `J` moves only 7 days.
2. `nav.go`: in `handleKey`, `keys.ScrollDown` → `m.moveDays(gridDays, true)`, `keys.ScrollUp` →
   `m.moveDays(-gridDays, true)`.
3. `keys.go`: help texts `"scroll 4 weeks back"` / `"scroll 4 weeks forward"`.
4. Check `TestGridNavigationScrollsWindow` and any golden test still pass (they use `j`, not `J`).

**Commit:** `feat(tui): J/K scroll four weeks`

---

## Task 14.3: Past days dimmed (item 8)

**Goal:** days before today are drawn faint: in the grid, the day label and its events; in the list, the day headers.
With colors off nothing changes, so **no golden file may change**.

**Files:** `internal/tui/grid.go`, `internal/tui/list.go`, `internal/tui/layout.go`, `internal/tui/grid_test.go`,
`internal/tui/list_test.go`

1. Tests. Faint is SGR `2`, so lipgloss emits `\x1b[2m` (alone, or combined like `\x1b[2;38;2;…m`). Test with colors on:
   ```go
   // grid_test.go
   func TestPastDaysAreFaint(t *testing.T) {
   	lipgloss.SetColorProfile(termenv.TrueColor)
   	defer lipgloss.SetColorProfile(termenv.Ascii)
   	ny := testutil.NY(t)
   	start := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
   	today := time.Date(2026, 11, 12, 0, 0, 0, 0, ny)
   	got, _ := renderGrid(gridView{start: start, today: today, selected: today,
   		days: model.GroupByDay(nil, start, gridDays), width: 70, height: 18, loc: ny})

   	if !strings.Contains(got, "\x1b[2m9 ") {
   		t.Error("Mon 9 (past) should be faint")
   	}
   	if strings.Contains(got, "\x1b[2m13 ") {
   		t.Error("Fri 13 (future) must not be faint")
   	}
   }
   ```
   and in `list_test.go` the same idea for a header: a `listView` with `today` set after the first day; the past
   header (`"Mon 09 Nov"`) is preceded by `\x1b[2m` and today's isn't. (If lipgloss combines bold and faint into one
   sequence such as `\x1b[1;2m`, match with a regexp `\x1b\[[0-9;]*\b2\b[0-9;]*m9 ` instead. **Log which form you saw**.)
   **Red:** no faint codes.
2. `grid.go`: in `cell`, compute `past := day.Date.Before(g.today)`. In the `default:` label case use
   `dimStyle.Render(fit(label, w-1))` when `past`. Pass `past` to `eventLine` and render the event with
   `calendarStyle(color).Faint(past).Render(text)`. (Selected, today and drop-target labels keep their styles: they
   win over dimming.)
3. `list.go`: add `today time.Time` to `listView`. The header uses `dimStyle` when `d.Date.Before(v.today)`, otherwise
   `headerStyle`. `layout.go` passes `today: m.today`.
4. `go test ./internal/tui`: pass, and `git status internal/tui/testdata` shows **nothing**.

**Commit:** `feat(tui): dim past days`

---

## Task 14.4: A slower mouse wheel (item 7)

**Goal:** trackpads send dozens of wheel events per swipe. Now:
- **Grid:** one *day* per wheel step. The window scrolls by a week when the selection leaves it (as with `h`/`l`).
- **List:** one event per step.
- **Throttle:** at most one step per **100 ms**; events in between are dropped.

The clock comes from `Deps.Now`, so tests control it.

**Files:** `internal/tui/model.go`, `internal/tui/mouse.go`, `internal/tui/model_test.go`, `internal/tui/mouse_test.go`

1. **Test helpers first** (used by later tasks too). In `model_test.go`:
   - `fixture` gains `clock time.Time` and `st *store.Store`.
   - In `newFixtureWith`, set `f.clock = now` and pass `d.Now = func() time.Time { return f.clock }`, instead of the
     fixed `now`. The service keeps its own fixed clock.
   - Keep `st` in the fixture.
   - Add:
     ```go
     // put stores e and reloads, as if a sync brought it in.
     func (f *fixture) put(e model.Event) {
     	f.t.Helper()
     	if err := f.st.PutEvent(context.Background(), e); err != nil {
     		f.t.Fatal(err)
     	}
     	f.send(f.model().load()())
     }
     ```
2. Tests (in `mouse_test.go`):
   ```go
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
   ```
   Replace `TestWheelScrollsGridAWeek` (it asserted the old behavior). **Red:** the wheel moves 7 days and isn't
   throttled.
3. `model.go`: `Model` gains `lastWheel time.Time`. `mouse.go`:
   ```go
   const wheelEvery = 100 * time.Millisecond // trackpads send dozens of events per swipe

   func (m Model) wheel(dir int) (tea.Model, tea.Cmd) {
   	if m.mode != modeNormal {
   		return m, nil
   	}
   	now := m.deps.Now()
   	if now.Sub(m.lastWheel) < wheelEvery {
   		return m, nil
   	}
   	m.lastWheel = now
   	if m.view == viewList {
   		m.cursor = min(max(m.cursor+dir, 0), max(len(m.entries())-1, 0))
   		return m, nil
   	}
   	m = m.moveDays(dir, false)
   	return m, m.reloadIfNeeded()
   }
   ```
4. `go test -race ./internal/tui`: pass.

**Commit:** `feat(tui): one day per wheel step, throttled`

---

## Task 14.5: Edge grabs never open details (item 3)

**Goal:**
- Grabbing `◂`/`▸` and releasing on the **same day** does nothing. (Today it counts as a click and opens details.)
- Grabbing an edge of an event you **can't change** shows `canDrag`'s reason in the status bar and opens nothing.
- Clicking an event's **body** still opens details.

**Files:** `internal/tui/mouse.go`, `internal/tui/mouse_test.go`

1. Tests:
   ```go
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
   ```
   **Red:** both open the details box.
2. `mouse.go`, in `press`, `case targetEvent`: when `canDrag` refuses, branch on the part:
   ```go
   		if ok, why := canDrag(t.event, m.calendars[t.event.CalendarID]); !ok {
   			if t.part != partBody { // a resize attempt: just say why
   				m.message = why
   				return m, nil
   			}
   			m.detail, m.mode, m.message = t.event, modeDetail, why
   			return m, nil
   		}
   ```
   In `drop`, a same-day release opens details **only for the body**:
   ```go
   	if d.over.Equal(d.from) {
   		if d.part == partBody { // a click on the event
   			m.detail, m.mode, m.message = d.event, modeDetail, ""
   		}
   		return m, nil
   	}
   ```
3. `go test -race ./internal/tui`: pass (`TestClickEventOpensDetailWithButtons` still clicks the body).

**Commit:** `fix(tui): grabbing an edge never opens details`

---

## Task 14.6: `Ctrl-J`/`Ctrl-K` between form fields (item 2)

**Goal:** vim-like next/previous field that doesn't clash with typing. The hint line shows them.

**Note:** in a text input, `Ctrl-K` normally deletes to the end of the line (bubbles' default). The form now claims
it for "previous field". `Ctrl-J` is the line-feed byte (0x0A); Enter is 0x0D, so they don't collide in normal
terminals. Task 14.23 checks this in a real one.

**Files:** `internal/tui/form.go`, `internal/tui/actions_test.go`

1. Test:
   ```go
   func TestFormCtrlJKMoveBetweenFields(t *testing.T) {
   	f := newFixture(t)
   	f.keys("a") // the form, until Task 14.16 (which updates this line to f.openFullForm())

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
   ```
   **Red:** focus doesn't move.
2. `form.go`, `Update`: add `"ctrl+j"` to the next-field case and `"ctrl+k"` to the previous-field case. Hint line:
   `"ctrl+j/tab next · ctrl+k/shift+tab previous · enter save · esc cancel"`.

**Commit:** `feat(tui): ctrl+j/ctrl+k move between form fields`

---

## Task 14.7: The list scrolls on, pages, and fills the screen (item 12)

**Why it felt "too short":** the list showed exactly 14 days from the selected day and skipped empty days. With an
ordinary calendar that's fewer rows than the screen has. The fixture's 14 days give 13 rows on a 19-row screen. The
height calculation (`bodyH = height − 1`) is fine; the fixed window was the problem.

**Goal:**
- The list starts at the selected day with **4 weeks** loaded.
- It **extends by 2 weeks** when the cursor gets within 3 entries of the end.
- It **prepends 2 weeks** when you press `k`/`↑` on the first entry. The cursor then lands on the entry just before
  the old first one.
- After every load it keeps extending until it has **two screenfuls of rows** (one visible, one ready for `J`), or
  covers 2 years.
- `J`/`K` (and `PgDn`/`PgUp`) **page** the cursor by a screenful in the list. In the grid they keep moving 4 weeks.

**Files:** `internal/tui/model.go`, `internal/tui/nav.go`, `internal/tui/list.go`, `internal/tui/layout.go`,
`internal/tui/mouse.go`, `internal/tui/model_test.go`

1. Tests (`model_test.go`):
   ```go
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
   ```
   **Red:** 13 rows, the cursor stops at the end, `k` does nothing at the top, and `J` moves the selection 28 days
   instead of paging.
2. `model.go`: replace the list's dependence on `m.selected` with its own range:
   ```go
   	listFrom   time.Time // first day of the list
   	listSpan   int       // days loaded in the list
   	listAnchor *entryRef // after prepending: the entry the cursor must land just before
   ```
   ```go
   const (
   	listInitial = 28      // days the list starts with
   	listStep    = 14      // days added when scrolling past either end
   	listMax     = 2 * 366 // never load more than this
   )

   // entryRef identifies a list entry across reloads (multi-day events appear once per day).
   type entryRef struct {
   	calendarID, eventID string
   	day                 time.Time
   }

   func (m Model) listDays() []model.Day { return model.GroupByDay(m.events, m.listFrom, m.listSpan) }

   // entries is the list view's selectable events.
   func (m Model) entries() []model.Event { return listEntries(m.listDays()) }

   func (m Model) entryRefs() []entryRef {
   	var out []entryRef
   	for _, d := range m.listDays() {
   		for _, e := range d.Events {
   			out = append(out, entryRef{e.CalendarID, e.ID, d.Date})
   		}
   	}
   	return out
   }

   // listRows is how many lines the list draws (day headers + entries).
   func (m Model) listRows() int {
   	rows := 0
   	for _, d := range m.listDays() {
   		if len(d.Events) > 0 {
   			rows += 1 + len(d.Events)
   		}
   	}
   	return rows
   }

   // openList shows the list starting at day.
   func (m Model) openList(day time.Time) Model {
   	m.view, m.listFrom, m.listSpan, m.cursor = viewList, day, listInitial, 0
   	return m
   }
   ```
   `window()` must also cover `[listFrom, listFrom+listSpan)` (take the min of the starts and the max of the ends).
   Every place that switches to the list now calls `openList`: `Tab` in `handleKey` (`openList(m.selected)` when going to
   the list), and `targetMore` in `mouse.go`. (Task 14.8 changes `Enter` and day clicks.) In the list, `t`, `h` and `l` change
   `m.selected` and then `openList(m.selected)`.
3. `nav.go`, in `listKey`:
   ```go
   	case key.Matches(k, keys.Down):
   		m.cursor = min(m.cursor+1, max(len(entries)-1, 0))
   		return m.extendListIfNeeded()
   	case key.Matches(k, keys.Up) && m.cursor == 0:
   		return m.prependList()
   	case key.Matches(k, keys.Up):
   		m.cursor--
   ```
   and add:
   ```go
   // extendListIfNeeded loads 2 more weeks when the cursor is near the end.
   func (m Model) extendListIfNeeded() (tea.Model, tea.Cmd) {
   	if m.cursor >= len(m.entries())-3 && m.listSpan < listMax {
   		m.listSpan += listStep
   		return m, m.reloadIfNeeded()
   	}
   	return m, nil
   }

   // prependList loads the 2 weeks before the list; after the load, the cursor
   // lands on the entry just before the current first one.
   func (m Model) prependList() (tea.Model, tea.Cmd) {
   	if refs := m.entryRefs(); len(refs) > 0 {
   		m.listAnchor = &refs[0]
   	}
   	m.listFrom, m.listSpan = m.listFrom.AddDate(0, 0, -listStep), m.listSpan+listStep
   	return m, m.reloadIfNeeded()
   }
   ```
   `J`/`K` in the list (in `handleKey`, check `m.view == viewList` before the grid's `moveDays(±gridDays)`):
   ```go
   		page := max(1, m.height-3)
   		m.cursor = min(max(m.cursor+dir*page, 0), max(len(m.entries())-1, 0))
   		return m.extendListIfNeeded()
   ```
4. `model.go`, in `Update`'s `loadedMsg` case, after storing the events:
   ```go
   		if m.view == viewList {
   			if m.listAnchor != nil { // just prepended: land before the old first entry
   				if i := slices.Index(m.entryRefs(), *m.listAnchor); i > 0 {
   					m.cursor = i - 1
   				}
   				m.listAnchor = nil
   			}
   			if m.listRows() < 2*(m.height-1) && m.listSpan < listMax { // a screenful, plus one ahead for J
   				m.listSpan += listStep
   				return m, m.load()
   			}
   		}
   ```
   (`entryRef` has only comparable fields, so `slices.Index` works.)
5. `layout.go`: `renderList(listView{days: m.listDays(), …})`. In `list.go`, the empty message becomes
   `"No events. Press a to add one."`. The mouse wheel in the list calls `extendListIfNeeded` after moving.
6. `go test -race ./internal/tui`, then **review `testdata/list.golden`**: it should now fill all 19 rows (regenerate it
   with `-update`). `list_scrolled.golden` doesn't change.

**Commit:** `feat(tui): endless list that fills the screen; J/K page`

---

## Task 14.8: Event mode in the grid (item 1)

**Goal:** act on events without leaving the grid.
- **`Enter`** on a day enters **event mode** for that day. Its events become selectable, with `▶` plus reverse video.
- **`j`/`k`** (and `↓`/`↑`) move between them. If the cell shows "+N more", the cell scrolls: it shows a window of
  events around the cursor, and its last line becomes `↑a ↓b` (how many are hidden above and below).
- **`Enter`** opens details, and **`e`/`d`/`r`** act directly, through the **same permission rules** as the details box (one
  function, `eventAction`).
- **`Esc`** goes back to moving between days. Any other key (`h`, `l`, `J`, `t`, `g`, `Tab`, `a`, …) leaves event mode
  and does its usual job.
- **`Esc` in the list** returns to the grid. `Tab` still toggles.
- Mouse: clicking the **already selected** day, or its `+N more`, also enters event mode (it used to open the list).

**Files:** `internal/tui/model.go`, `internal/tui/nav.go`, `internal/tui/actions.go`, `internal/tui/grid.go`,
`internal/tui/layout.go`, `internal/tui/mouse.go`, `internal/tui/model_test.go`, `internal/tui/mouse_test.go`

1. Tests (`model_test.go`). The fixture's Thu Nov 12 has Standup 09:00 and Lunch 12:00:
   ```go
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
   ```
   In `mouse_test.go`, `TestClickSelectsDayThenOpensList` becomes `TestClickSelectedDayStartsEventMode`: the second click
   leaves `view == viewGrid` with `dayMode == true`. **Red:** all fail (Enter opens the list).
2. `model.go`: `Model` gains `dayMode bool` and `dayCursor int`, plus:
   ```go
   // dayEvents are the selected day's events, in display order.
   func (m Model) dayEvents() []model.Event { return model.GroupByDay(m.events, m.selected, 1)[0].Events }
   ```
3. `actions.go`: extract `detailKey`'s e/d/r handling into one function both places use:
   ```go
   // eventAction applies e/d/r to e with the same permission rules everywhere
   // (details box, event mode); refusals are explained in the status bar.
   func (m Model) eventAction(k tea.KeyMsg, e model.Event) (tea.Model, tea.Cmd) {
   	can := permissions(e, m.calendars[e.CalendarID])
   	switch {
   	case key.Matches(k, keys.Edit) && !can.edit:
   		m.message = "can't edit: " + can.whyNoEdit
   	case key.Matches(k, keys.Delete) && !can.delete:
   		m.message = "can't delete: the calendar is read-only"
   	case key.Matches(k, keys.RSVP) && !can.rsvp:
   		m.message = can.whyNoRSVP
   	case key.Matches(k, keys.Edit):
   		m.form, m.mode = editEventForm(e, m.loc()), modeForm
   	case key.Matches(k, keys.Delete):
   		m = m.startDelete(e)
   	case key.Matches(k, keys.RSVP):
   		m = m.startRSVP(e)
   	}
   	return m, nil
   }
   ```
   `detailKey` becomes: if `k` matches Edit/Delete/RSVP → `return m.eventAction(k, m.detail)`; otherwise close.
4. `nav.go`. In `handleKey`, right after the drag-cancel check:
   `if m.view == viewGrid && m.dayMode { return m.dayKey(k) }`. Then:
   ```go
   func (m Model) dayKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
   	events := m.dayEvents()
   	if len(events) == 0 {
   		m.dayMode = false
   		return m.handleKey(k)
   	}
   	m.dayCursor = min(m.dayCursor, len(events)-1)
   	switch {
   	case key.Matches(k, keys.Back):
   		m.dayMode, m.message = false, ""
   	case key.Matches(k, keys.Down):
   		m.dayCursor = min(m.dayCursor+1, len(events)-1)
   	case key.Matches(k, keys.Up):
   		m.dayCursor = max(m.dayCursor-1, 0)
   	case key.Matches(k, keys.Open):
   		m.detail, m.mode = events[m.dayCursor], modeDetail
   	case key.Matches(k, keys.Edit, keys.Delete, keys.RSVP):
   		return m.eventAction(k, events[m.dayCursor])
   	default: // any other key leaves event mode and does its usual job
   		m.dayMode = false
   		return m.handleKey(k)
   	}
   	return m, nil
   }
   ```
   In `gridKey`, `keys.Open` now enters event mode:
   ```go
   	case key.Matches(k, keys.Open):
   		if len(m.dayEvents()) == 0 {
   			m.message = "no events on this day · a to add"
   			return m, nil
   		}
   		m.dayMode, m.dayCursor = true, 0
   		m.message = "j/k pick · enter open · e/d/r act · esc back"
   ```
   and the e/d/r message becomes `"press enter to pick an event"`. In `listKey`, add `case key.Matches(k, keys.Back):
   m.view = viewGrid`.
5. `grid.go`: `gridView` gains `focus int // event mode: index of the focused event on the selected day; -1 = none`.
   In `cell`, when `g.focus >= 0 && day.Date.Equal(g.selected)`:
   - If all events fit (`len(events) <= room`), draw them as usual, marking index `focus`.
   - Otherwise show a window of `visible := room-1` events starting at
     `offset := min(max(focus-visible+1, 0), len(events)-visible)`, marking `focus`, and then a dim last line
     `fmt.Sprintf("↑%d ↓%d", offset, len(events)-offset-visible)`.
   
   `eventLine` takes a `focused bool`. When it's true, the text is `"▶" + fit(label, w-1-width(start+end))` (plus
   handles, if any), rendered with `selectedStyle` instead of the calendar color. Hits are unchanged. Outside event mode, the
   cell renders exactly as before, so **golden files must not change** (`layout.go` passes `focus: -1` unless `m.dayMode`, in
   which case `focus: m.dayCursor`).
6. `mouse.go`, `press`: `case targetDay` on the selected day → `m.dayMode, m.dayCursor = true, 0` (not the list). `case
   targetMore` → select the day, then the same. Any other `targetDay` click, and every drag, sets `m.dayMode = false`.
7. `go test -race ./internal/tui`: pass, with no golden changes.

**Commit:** `feat(tui): event mode in the grid (enter, j/k, e/d/r, esc)`

---

## Task 14.9: Day/month dates and `date_order` (item 11)

**Goal:** `D/M`, `DD/MM` and `DD/MM/YYYY` work wherever dates are typed: the CLI's `--at`, `--from`, `--to` and `--until`,
the TUI form, quick add, and go-to.
- **Without a year:** the next time that day and month come round, **today included** (on Nov 12, `12/11` is today and
  `11/11` is next year).
- **With a year:** exactly that date, and only 4-digit years (`1/2/26` is ambiguous, so it's an error).
- Impossible dates (`31/11`) are errors.
- Config `date_order = "dmy" | "mdy"` (default `"dmy"`) flips the order. ISO `YYYY-MM-DD` is unchanged.

**Design:** `timeparse` gets a `Parser` value carrying the order. Its **zero value is DMY**, and the existing package-level
functions become `Parser{}.…`, so every current caller keeps compiling. Callers that know the config pass a configured
`Parser`. `config` stays free of internal imports: it stores the string, and `cli` turns it into a `Parser`.

**Files:** `internal/timeparse/time.go`, `internal/timeparse/span.go`, `internal/timeparse/time_test.go`,
`internal/config/config.go`, `internal/config/config_test.go`, `internal/cli/app.go`, `internal/cli/window.go`,
`internal/cli/write.go`, `internal/cli/root.go` (`tuiDeps`), `internal/cli/*_test.go`, `internal/tui/model.go`
(`Deps.Dates`), `internal/tui/form.go`

1. Tests. `time_test.go` (same `now`: Thu 2026-11-12 10:30 New York):
   ```go
   func TestParseTimeSlashDates(t *testing.T) {
   	ny, _ := time.LoadLocation("America/New_York")
   	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny)
   	d := func(y, mo, day, h, mi int) time.Time { return time.Date(y, time.Month(mo), day, h, mi, 0, 0, ny) }
   	dmy, mdy := timeparse.Parser{}, timeparse.Parser{Order: timeparse.MDY}
   	tests := []struct {
   		p       timeparse.Parser
   		in      string
   		want    time.Time
   		wantErr bool
   	}{
   		{p: dmy, in: "20/11", want: d(2026, 11, 20, 0, 0)},
   		{p: dmy, in: "12/11", want: d(2026, 11, 12, 0, 0)}, // today counts
   		{p: dmy, in: "11/11", want: d(2027, 11, 11, 0, 0)}, // past: next year
   		{p: dmy, in: "5/1", want: d(2027, 1, 5, 0, 0)},
   		{p: dmy, in: "20/11/2027", want: d(2027, 11, 20, 0, 0)},
   		{p: dmy, in: "20/11 15:30", want: d(2026, 11, 20, 15, 30)},
   		{p: dmy, in: "29/2", want: d(2028, 2, 29, 0, 0)}, // next leap day
   		{p: mdy, in: "11/20", want: d(2026, 11, 20, 0, 0)},
   		{p: mdy, in: "11/20 2pm", want: d(2026, 11, 20, 14, 0)},
   		{p: dmy, in: "31/11", wantErr: true},
   		{p: dmy, in: "13/13", wantErr: true},
   		{p: dmy, in: "1/2/26", wantErr: true}, // 2-digit years are ambiguous
   		{p: mdy, in: "20/11", wantErr: true},
   	}
   	for _, tt := range tests {
   		t.Run(tt.in, func(t *testing.T) {
   			got, _, err := tt.p.ParseTime(tt.in, now)
   			if (err != nil) != tt.wantErr {
   				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
   			}
   			if !tt.wantErr && !got.Equal(tt.want) {
   				t.Errorf("got %v, want %v", got, tt.want)
   			}
   		})
   	}
   }
   ```
   `config_test.go`: the default is `"dmy"`, `date_order = "mdy"` loads, and `date_order = "ymd"` is an error naming the
   file (add a row to `TestLoadRejectsBadInput`).
   CLI (`write_test.go`): `add Dentist --at "20/11 15:30" --offline`, then `show ID --format '{{.Start}}'` prints
   `2026-11-20T15:30:00-05:00`. Then the same with `h.app.Config.DateOrder = "mdy"` and `--at "11/20 15:30"`.
   **Red:** slash dates are errors and `DateOrder` doesn't exist.
2. `timeparse/time.go`:
   ```go
   // DateOrder says how slash dates are read.
   type DateOrder int

   const (
   	DMY DateOrder = iota // 20/11 is 20 November (the default)
   	MDY                  // 11/20 is 20 November
   )

   // Parser reads user-typed dates. Its zero value reads day/month, so
   // Parser{} is the default everywhere a config isn't at hand.
   type Parser struct{ Order DateOrder }

   // ParseTime is Parser{}.ParseTime, kept for callers without a config.
   func ParseTime(input string, now time.Time) (time.Time, bool, error) { return Parser{}.ParseTime(input, now) }
   ```
   Move the old body of `ParseTime` into `func (p Parser) ParseTime(…)` and make `parseDate` a method (`p.parseDate`) that
   tries `p.slashDate` after the ISO form:
   ```go
   // slashDate reads D/M, DD/MM or DD/MM/YYYY (M/D/YYYY with MDY). Without a
   // year it is the next time that day comes round, today included.
   func (p Parser) slashDate(s string, today time.Time) (time.Time, bool) {
   	parts := strings.Split(s, "/")
   	if len(parts) < 2 || len(parts) > 3 {
   		return time.Time{}, false
   	}
   	var nums [3]int
   	for i, part := range parts {
   		n, err := strconv.Atoi(part)
   		if err != nil || n <= 0 {
   			return time.Time{}, false
   		}
   		nums[i] = n
   	}
   	day, month := nums[0], nums[1]
   	if p.Order == MDY {
   		day, month = month, day
   	}
   	if month > 12 || day > 31 {
   		return time.Time{}, false
   	}
   	at := func(y int) (time.Time, bool) {
   		d := time.Date(y, time.Month(month), day, 0, 0, 0, 0, today.Location())
   		return d, d.Day() == day // false for 31/11: Go rolls it over to 1 Dec
   	}
   	if len(parts) == 3 {
   		if len(parts[2]) != 4 {
   			return time.Time{}, false // 2-digit years are ambiguous
   		}
   		return at(nums[2])
   	}
   	for y := today.Year(); y <= today.Year()+8; y++ { // 8 years always reach the next 29 February
   		if d, ok := at(y); ok && !d.Before(today) {
   			return d, true
   		}
   	}
   	return time.Time{}, false
   }
   ```
   (Add `"strconv"` if it isn't imported.) `span.go`: likewise `func (p Parser) ParseSpan(…)`, using `p.ParseTime`, with the
   package-level `ParseSpan` delegating to `Parser{}`.
3. `config.go`: `DateOrder string \`toml:"date_order"\` // "dmy" (20/11) | "mdy" (11/20)`, default `"dmy"`, validated like
   `week_start`. `cli/app.go`:
   ```go
   // Dates is the date parser the user configured.
   func (a *App) Dates() timeparse.Parser {
   	if a.Config.DateOrder == "mdy" {
   		return timeparse.Parser{Order: timeparse.MDY}
   	}
   	return timeparse.Parser{}
   }
   ```
   Use `app.Dates()` for every parse in `window.go` (give `agendaWindow` a `timeparse.Parser` parameter; tests pass
   `timeparse.Parser{}`) and in `write.go` (`ParseSpan`, `--until`, `changesFrom`). `tui.Deps` gains `Dates
   timeparse.Parser`; `tuiDeps` sets it (add it to `TestTUIDepsCarryConfig`), and `form.draft`/`form.changes` take it.
4. Docs: in `docs/plan/01-foundations.md` the Task 1.7 table already points here. Add `20/11`, `20/11/2027` and
   `20/11 15:30` to the time-format table in `docs/cli.md`, plus the `date_order` key.
5. `go test -race ./...`: pass.

**Commit:** `feat(timeparse): day/month dates with a date_order setting`

---

## Task 14.10: Go-to picks days: day numbers, months, no times (item 11)

**Goal:** `g` understands what people type to jump to a day:
- Everything that's a date elsewhere: `today`, `fri`, `+2w`, `20/12`, `2026-12-24`.
- **A month name** (`dec`, `december`): the 1st of that month, this month included (in November, `nov` is Nov 1
  and `oct` is next October).
- **A bare number 1–31:** the next day with that number, today included. Months that don't have it are skipped (on
  Nov 12, `31` is Dec 31).

Times of day (`14:00`, `in 2h`, `+90m`, `now`) are refused, because they make no sense for picking a day.

**Files:** `internal/timeparse/day.go` (new), `internal/timeparse/day_test.go` (new), `internal/tui/nav.go`,
`internal/tui/model.go` (placeholder), `internal/tui/model_test.go`

1. Tests (`day_test.go`):
   ```go
   func TestParseDay(t *testing.T) {
   	ny, _ := time.LoadLocation("America/New_York")
   	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny) // Thursday
   	d := func(y, mo, day int) time.Time { return time.Date(y, time.Month(mo), day, 0, 0, 0, 0, ny) }
   	tests := []struct {
   		in      string
   		want    time.Time
   		wantErr bool
   	}{
   		{in: "fri", want: d(2026, 11, 13)},
   		{in: "today", want: d(2026, 11, 12)},
   		{in: "+2w", want: d(2026, 11, 26)},
   		{in: "20/12", want: d(2026, 12, 20)},
   		{in: "2026-12-24", want: d(2026, 12, 24)},
   		{in: "dec", want: d(2026, 12, 1)},
   		{in: "December", want: d(2026, 12, 1)},
   		{in: "nov", want: d(2026, 11, 1)}, // this month counts
   		{in: "oct", want: d(2027, 10, 1)},
   		{in: "15", want: d(2026, 11, 15)},
   		{in: "12", want: d(2026, 11, 12)}, // today counts
   		{in: "5", want: d(2026, 12, 5)},
   		{in: "31", want: d(2026, 12, 31)}, // November has no 31st
   		{in: "14:00", wantErr: true},
   		{in: "in 2h", wantErr: true},
   		{in: "+90m", wantErr: true},
   		{in: "now", wantErr: true},
   		{in: "0", wantErr: true},
   		{in: "32", wantErr: true},
   	}
   	for _, tt := range tests {
   		t.Run(tt.in, func(t *testing.T) {
   			got, err := timeparse.Parser{}.ParseDay(tt.in, now)
   			if (err != nil) != tt.wantErr {
   				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
   			}
   			if !tt.wantErr && !got.Equal(tt.want) {
   				t.Errorf("got %v, want %v", got, tt.want)
   			}
   		})
   	}
   }
   ```
   TUI (`model_test.go`): `g`, `15`, Enter selects Nov 15. `g`, `14:00`, Enter leaves the selection unchanged, with a
   message containing "isn't a day". **Red:** `ParseDay` doesn't exist.
2. `day.go`:
   ```go
   package timeparse

   import (
   	"fmt"
   	"strconv"
   	"strings"
   	"time"
   )

   var months = map[string]time.Month{
   	"jan": time.January, "january": time.January, "feb": time.February, "february": time.February,
   	"mar": time.March, "march": time.March, "apr": time.April, "april": time.April, "may": time.May,
   	"jun": time.June, "june": time.June, "jul": time.July, "july": time.July,
   	"aug": time.August, "august": time.August, "sep": time.September, "sept": time.September,
   	"september": time.September, "oct": time.October, "october": time.October,
   	"nov": time.November, "november": time.November, "dec": time.December, "december": time.December,
   }

   // ParseDay reads a day to go to: any date ParseTime accepts (today, fri, +2w,
   // 20/12, 2026-12-24), a month name (the 1st of it, this month included), or a
   // day number (the next such day, today included). Times are refused.
   func (p Parser) ParseDay(input string, now time.Time) (time.Time, error) {
   	s := strings.ToLower(strings.TrimSpace(input))
   	today := startOfDay(now)
   	if d, ok := p.parseDate(s, now); ok {
   		return d, nil
   	}
   	if m, ok := months[s]; ok {
   		y := today.Year()
   		if m < today.Month() {
   			y++
   		}
   		return time.Date(y, m, 1, 0, 0, 0, 0, now.Location()), nil
   	}
   	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 31 {
   		for i := range 12 { // the next month that has a day n, starting with this one
   			first := time.Date(today.Year(), today.Month()+time.Month(i), 1, 0, 0, 0, 0, now.Location())
   			if d := first.AddDate(0, 0, n-1); d.Month() == first.Month() && !d.Before(today) {
   				return d, nil
   			}
   		}
   	}
   	return time.Time{}, fmt.Errorf("%q isn't a day (try: fri, 20/12, dec, 15, +2w, 2026-12-24)", input)
   }
   ```
3. `nav.go`, `gotoKey`: replace `timeparse.ParseTime(...)` with `m.deps.Dates.ParseDay(m.gotoInput.Value(),
   m.deps.Now())` (it returns the day directly). The go-to input's placeholder in `New` becomes
   `"fri, +2w, 20/12, dec, 15"`.
4. Docs: in `docs/cli.md`, describe go-to's inputs (TUI section).
5. `go test -race ./...`: pass.

**Commit:** `feat(tui): go-to understands day numbers and months, refuses times`

---

## Task 14.11: One reusable picker (item 4)

**Goal:** a single widget for every "choose one of these" and "tick these" in the TUI. It's used for the calendar menu
(14.13), and for the form's Calendar, All-day and Repeat fields (14.17) and Color field (14.20). It has three faces:

| Face | Keys |
|---|---|
| **Closed**, inside a form: shows the current choice as `‹ Work ›` | `h`/`l`, `←`/`→`, `Ctrl-P`/`Ctrl-N` cycle; `Space`/`Enter` open |
| **Dropdown** | `j`/`k`, `↓`/`↑` move; typing filters (or `/` first, so `j`/`k` can be typed); `Enter` chooses; `Esc` closes |
| **Checklist** (calendar menu) | `j`/`k`, `↓`/`↑` move; `Space`/`x` toggle; `/` filters; `Esc` closes |

The picker is pure state plus a view, with no service calls. It reports what happened, and the caller acts on it.

**Files:** `internal/tui/picker.go` (new), `internal/tui/picker_test.go` (new), `internal/tui/hits.go`,
`internal/tui/modal.go`

1. Tests (`picker_test.go`):
   ```go
   package tui

   import (
   	"testing"

   	tea "github.com/charmbracelet/bubbletea"
   )

   func abc() picker {
   	return picker{title: "Calendar", options: []option{{label: "Me", value: "me"}, {label: "Work", value: "w"}, {label: "Home", value: "h"}}}
   }

   func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

   func TestPickerCyclesWhenClosed(t *testing.T) {
   	p := abc()
   	p, ev, _ := p.Update(runes("l"))
   	if ev != pickChanged || p.current().value != "w" {
   		t.Fatalf("l: %v %q", ev, p.current().value)
   	}
   	p, _, _ = p.Update(runes("h"))
   	p, _, _ = p.Update(runes("h")) // wraps from the first to the last
   	if p.current().value != "h" {
   		t.Errorf("h h: %q, want the last option", p.current().value)
   	}
   }

   func TestPickerDropdownFiltersAndChooses(t *testing.T) {
   	p := abc()
   	p, _, _ = p.Update(runes(" ")) // open
   	p, _, _ = p.Update(runes("wo"))
   	if vis := p.visible(); len(vis) != 1 || p.options[vis[0]].label != "Work" {
   		t.Fatalf("visible = %v", vis)
   	}
   	p, ev, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
   	if ev != pickChanged || p.open || p.current().label != "Work" {
   		t.Errorf("enter: %v open=%v %q", ev, p.open, p.current().label)
   	}
   }

   func TestPickerEscKeepsTheChoice(t *testing.T) {
   	p := abc()
   	p, _, _ = p.Update(runes(" "))
   	p, _, _ = p.Update(runes("j"))
   	p, ev, _ := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
   	if ev != pickClosed || p.current().value != "me" {
   		t.Errorf("esc: %v %q", ev, p.current().value)
   	}
   }

   func TestChecklistToggles(t *testing.T) {
   	p := abc()
   	p.checklist, p.open = true, true
   	p, _, _ = p.Update(runes("j"))
   	p, ev, i := p.Update(runes(" "))
   	if ev != pickToggled || i != 1 || !p.options[1].checked {
   		t.Errorf("space: %v %d %v", ev, i, p.options[1].checked)
   	}
   }

   func TestPickerRowsAreClickable(t *testing.T) {
   	p := abc()
   	p.open = true
   	box, hits := p.view(40, true).render()
   	x, y := find(t, box, "Home")
   	if got, _ := hits.at(x, y); got.kind != targetOption || got.field != 2 {
   		t.Errorf("click on Home = %+v", got)
   	}
   }
   ```
   **Red:** none of this exists.
2. `hits.go`: add `targetOption // a row in a picker; field holds the option index`. `modal.go`: add
   ```go
   // option adds a clickable picker row for option index i.
   func (m *modal) option(line string, i int) {
   	m.hits.add(0, len(m.lines), width(line), 1, target{kind: targetOption, field: i})
   	m.lines = append(m.lines, line)
   }
   ```
3. `picker.go`:
   ```go
   package tui

   import (
   	"strings"

   	tea "github.com/charmbracelet/bubbletea"
   )

   // option is one choice in a picker.
   type option struct {
   	label   string
   	value   string // what the caller stores: a calendar ID, a repeat word, a color ID…
   	swatch  string // optional "#rrggbb", drawn as ● before the label
   	checked bool   // checklist mode
   }

   // picker is the TUI's one choice widget: closed in a form, open as a
   // dropdown, or open as a checklist. It only holds state; the caller acts on
   // the pickEvent it returns.
   type picker struct {
   	title     string
   	options   []option
   	chosen    int // single choice: index into options
   	cursor    int // index into visible()
   	filter    string
   	filtering bool // after "/" (or a typed letter): runes go to the filter
   	open      bool
   	checklist bool
   }

   type pickEvent int

   const (
   	pickNothing pickEvent = iota
   	pickChanged           // single: a new choice
   	pickToggled           // checklist: an option was ticked or unticked (its index is returned)
   	pickClosed            // closed without a change
   )

   func (p picker) current() option { return p.options[p.chosen] }

   // visible lists the indexes of options matching the filter.
   func (p picker) visible() []int {
   	var out []int
   	for i, o := range p.options {
   		if strings.Contains(strings.ToLower(o.label), strings.ToLower(p.filter)) {
   			out = append(out, i)
   		}
   	}
   	return out
   }

   // Update handles one key. It returns what happened and, for pickToggled, which option.
   func (p picker) Update(k tea.KeyMsg) (picker, pickEvent, int) {
   	n := len(p.options)
   	if !p.open { // closed, in a form
   		switch k.String() {
   		case "h", "left", "ctrl+p":
   			p.chosen = (p.chosen - 1 + n) % n
   			return p, pickChanged, p.chosen
   		case "l", "right", "ctrl+n":
   			p.chosen = (p.chosen + 1) % n
   			return p, pickChanged, p.chosen
   		case " ", "enter":
   			p.open, p.filter, p.filtering, p.cursor = true, "", false, max(0, indexOf(p.visible(), p.chosen))
   		}
   		return p, pickNothing, 0
   	}
   	vis := p.visible()
   	switch s := k.String(); {
   	case s == "esc" && p.filtering:
   		p.filter, p.filtering = "", false
   	case s == "esc":
   		p.open, p.filter, p.filtering = false, "", false
   		return p, pickClosed, 0
   	case s == "down" || (s == "j" && !p.filtering):
   		p.cursor = min(p.cursor+1, max(len(vis)-1, 0))
   	case s == "up" || (s == "k" && !p.filtering):
   		p.cursor = max(p.cursor-1, 0)
   	case p.checklist && (s == " " || (s == "x" && !p.filtering)) && len(vis) > 0:
   		i := vis[p.cursor]
   		p.options[i].checked = !p.options[i].checked
   		return p, pickToggled, i
   	case !p.checklist && s == "enter" && len(vis) > 0:
   		p.chosen, p.open, p.filter, p.filtering = vis[p.cursor], false, "", false
   		return p, pickChanged, p.chosen
   	case s == "backspace" && p.filter != "":
   		p.filter = p.filter[:len(p.filter)-1]
   		p.cursor = 0
   	case s == "/":
   		p.filtering = true
   	case k.Type == tea.KeyRunes && (p.filtering || !p.checklist):
   		p.filtering, p.filter, p.cursor = true, p.filter+string(k.Runes), 0
   	}
   	return p, pickNothing, 0
   }

   // pick is a mouse click on option i: choose it (single) or toggle it (checklist).
   func (p picker) pick(i int) (picker, pickEvent, int) {
   	if p.checklist {
   		p.options[i].checked = !p.options[i].checked
   		return p, pickToggled, i
   	}
   	p.chosen, p.open, p.filter, p.filtering = i, false, "", false
   	return p, pickChanged, i
   }

   // inline is the closed picker inside a form field.
   func (p picker) inline() string {
   	o := p.current()
   	return "‹ " + swatch(o.swatch) + o.label + " ›"
   }

   // view draws the open picker as a pop-up; each row is clickable.
   func (p picker) view(w int, mouse bool) modal {
   	var md modal
   	md.text(headerStyle.Render(p.title))
   	if p.filtering {
   		md.text("/" + p.filter)
   	}
   	md.text("")
   	for row, i := range p.visible() {
   		o := p.options[i]
   		mark := "  "
   		if row == p.cursor {
   			mark = "▶ "
   		}
   		box := ""
   		if p.checklist {
   			box = "[ ] "
   			if o.checked {
   				box = "[x] "
   			}
   		}
   		line := fit(mark+box+swatch(o.swatch)+o.label, max(w-4, 10))
   		if row == p.cursor {
   			line = selectedStyle.Render(line)
   		}
   		md.option(line, i)
   	}
   	md.text("")
   	hint := "j/k move · enter choose · / filter · esc close"
   	if p.checklist {
   		hint = "j/k move · space toggle · / filter · esc close"
   	}
   	if mouse {
   		md.buttons(button{"Close", "esc"})
   	}
   	md.text(dimStyle.Render(hint))
   	return md
   }

   // swatch is a colored ● for a "#rrggbb" color, or nothing.
   func swatch(color string) string {
   	if color == "" {
   		return ""
   	}
   	return calendarStyle(color).Render("●") + " "
   }

   func indexOf(xs []int, v int) int {
   	for i, x := range xs {
   		if x == v {
   			return i
   		}
   	}
   	return -1
   }
   ```
   (The selected row is fitted *before* styling: Task 10.2's rule. The swatch is colored, so the line is fitted as a whole. If
   lipgloss pads a colored `●` wrongly, build the row from plain text and color only the swatch afterwards, then log it.)
4. `go test ./internal/tui -run 'Picker|Checklist'`: pass.

**Commit:** `feat(tui): reusable picker (cycle, dropdown, checklist)`

---

## Task 14.12: Local calendar visibility (item 4)

**Goal:** which calendars show in clindar is a **local** choice that overrides Google's `selected` flag and is **never
written to Google**. The TUI menu (14.13) sets it. The CLI's default `agenda` (no `-c`) follows it automatically,
because it goes through `service.Agenda`'s filter.

**Rule:** visible = the local choice, if there is one; otherwise Google's `selected`.

**Files:** `internal/store/store.go` (migration 2), `internal/store/calendars.go`, `internal/store/accounts.go`,
`internal/store/calendars_test.go`, `internal/model/calendar.go`, `internal/service/service.go`,
`internal/service/query.go`, `internal/service/query_test.go`, `internal/cli/read.go`

1. Tests. `calendars_test.go`:
   ```go
   func TestLocalVisibilityOverridesGoogle(t *testing.T) {
   	s := testutil.NewStore(t)
   	ctx := context.Background()
   	for _, c := range []model.Calendar{
   		{AccountID: "a", ID: "on", Name: "On", Selected: true},
   		{AccountID: "a", ID: "off", Name: "Off", Selected: false},
   		{AccountID: "a", ID: "plain", Name: "Plain", Selected: true},
   	} {
   		s.UpsertCalendar(ctx, c)
   	}
   	s.SetCalendarShown(ctx, "a", "on", false)
   	s.SetCalendarShown(ctx, "a", "off", true)
   	s.UpsertCalendar(ctx, model.Calendar{AccountID: "a", ID: "on", Name: "On", Selected: true}) // a sync must not reset it

   	cals, _ := s.Calendars(ctx, "a")
   	got := map[string]bool{}
   	for _, c := range cals {
   		got[c.ID] = c.Visible
   	}
   	want := map[string]bool{"on": false, "off": true, "plain": true}
   	if diff := cmp.Diff(want, got); diff != "" {
   		t.Errorf("visible (-want +got):\n%s", diff)
   	}
   }
   ```
   `query_test.go`: `TestAgendaFollowsLocalVisibility`. Hide the primary calendar locally with
   `e.store.SetCalendarShown(e.ctx, testutil.Account, testutil.Calendar, false)`: Lunch disappears from `titlesOn`. Show
   "holidays" locally: Thanksgiving appears without a `-c` filter. **Red:** `SetCalendarShown`/`Visible` don't exist.
2. `store.go`: **append** migration 2 (never edit migration 1, which users' databases already ran):
   ```go
   `
   CREATE TABLE calendar_prefs (
   	account_id  TEXT NOT NULL,
   	calendar_id TEXT NOT NULL,
   	shown       INTEGER NOT NULL, -- local choice; overrides Google's "selected"
   	PRIMARY KEY (account_id, calendar_id)
   );
   `,
   ```
3. `model/calendar.go`: `Visible bool \`json:"visible"\` // shown in clindar: the local choice if any, else Selected`.
   `calendars.go`: `Calendars` reads with a join and computes `Visible`:
   ```go
   	rows, err := s.q.QueryContext(ctx, `SELECT c.data, p.shown FROM calendars AS c
   		LEFT JOIN calendar_prefs AS p ON p.account_id = c.account_id AND p.calendar_id = c.id
   		WHERE c.account_id = ?`, accountID)
   	…
   		var data string
   		var shown sql.NullInt64
   		if err := rows.Scan(&data, &shown); err != nil { … }
   		…
   		c.Visible = c.Selected
   		if shown.Valid {
   			c.Visible = shown.Int64 == 1
   		}
   ```
   plus:
   ```go
   // SetCalendarShown records the local choice to show or hide a calendar.
   func (s *Store) SetCalendarShown(ctx context.Context, accountID, calendarID string, shown bool) error {
   	_, err := s.q.ExecContext(ctx, `INSERT INTO calendar_prefs (account_id, calendar_id, shown) VALUES (?, ?, ?)
   		ON CONFLICT (account_id, calendar_id) DO UPDATE SET shown = excluded.shown`, accountID, calendarID, shown)
   	return err
   }
   ```
   `DeleteCalendar` also deletes the calendar's pref. `DeleteAccount`'s table list gains `"calendar_prefs"`.
4. `service`: `visibleCalendars` uses `c.Visible` instead of `c.Selected`, and add
   `func (s *Service) SetCalendarShown(ctx context.Context, calendarID string, shown bool) error`. `cli/read.go`: the
   `calendars` command marks `*` from `Visible`. Its `Short` help says "* = shown (change in the TUI with c)".
5. `go test -race ./...`: pass, including `TestOpenIsIdempotent` (it now runs migration 2 once).

(There's no CLI command to change visibility in this round. If you want one, `clindar calendars show|hide NAME` is
a small follow-up; note it in "Ideas".)

**Commit:** `feat: local calendar visibility, overriding Google's selected flag`

---

## Task 14.13: The calendar menu (item 4)

**Goal:** `c` (and a `[Calendars]` status-bar button with the mouse on) opens a checklist: one row per calendar, with
a checkbox, a color swatch and the name. `j`/`k` move, `Space`/`x` toggle (saved at once via Task 14.12), and `Esc` closes.
A click on a row toggles it.

**Files:** `internal/tui/keys.go`, `internal/tui/model.go`, `internal/tui/nav.go`, `internal/tui/layout.go`,
`internal/tui/mouse.go`, `internal/tui/model_test.go`

1. Tests:
   ```go
   func TestCalendarMenuHidesACalendar(t *testing.T) {
   	f := newFixture(t)
   	f.keys("c")
   	if m := f.model(); m.mode != modeCalendars || !strings.Contains(f.m.View(), "[x]") {
   		t.Fatalf("mode = %v:\n%s", m.mode, f.m.View())
   	}

   	f.keys(" ") // untick "Me", the only calendar

   	if got := f.titlesOn(12); len(got) != 0 {
   		t.Errorf("Nov 12 = %v: a hidden calendar's events must disappear", got)
   	}
   	if f.model().mode != modeCalendars {
   		t.Error("toggling must keep the menu open")
   	}
   	f.keys("esc")
   	if strings.Contains(f.m.View(), "Lunch") {
   		t.Error("the grid still shows the hidden calendar's events")
   	}
   }
   ```
   In `mouse_test.go`, clicking the `[Calendars]` button opens the menu, and clicking a row toggles it. **Red:** `c` does nothing.
2. `keys.go`: `Calendars: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "choose calendars"))`. Add it to
   `helpText`'s list.
3. `model.go`:
   - `mode` gains `modeCalendars`.
   - `Model` gains `calMenu picker` and `calList []model.Calendar`. Fill `calList` in the `loadedMsg` handler, in the
     store's order (primary first).
   - A message for toggles that **doesn't close** the menu (unlike `actionDoneMsg`):
   ```go
   type calShownMsg struct{ err error }
   ```
   handled as: on error, set `m.message`; in both cases, `return m, m.load()`.
4. `nav.go`. In `handleKey`'s normal branch:
   ```go
   	case key.Matches(k, keys.Calendars):
   		m.calMenu = picker{title: "Calendars", checklist: true, open: true}
   		for _, c := range m.calList {
   			m.calMenu.options = append(m.calMenu.options, option{label: c.Name, value: c.ID, swatch: c.Color, checked: c.Visible})
   		}
   		m.mode = modeCalendars
   ```
   and a mode case `case modeCalendars: return m.calMenuKey(k)`:
   ```go
   func (m Model) calMenuKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
   	var ev pickEvent
   	var i int
   	m.calMenu, ev, i = m.calMenu.Update(k)
   	return m.calMenuEvent(ev, i)
   }

   // calMenuEvent saves a toggle at once, or closes the menu.
   func (m Model) calMenuEvent(ev pickEvent, i int) (tea.Model, tea.Cmd) {
   	switch ev {
   	case pickClosed:
   		m.mode = modeNormal
   	case pickToggled:
   		o := m.calMenu.options[i]
   		svc, ctx := m.deps.Service, m.ctx
   		return m, func() tea.Msg { return calShownMsg{svc.SetCalendarShown(ctx, o.value, o.checked)} }
   	}
   	return m, nil
   }
   ```
5. `layout.go`: `popup` shows `m.calMenu.view(min(m.width-4, 50), m.deps.Mouse)` in `modeCalendars`. In `statusBar`, add
   `{"Calendars", "c"}` before `{"⟳ Sync", "s"}`. `mouse.go`, `press`: in `modeCalendars`, a `targetOption` →
   `m.calMenu, ev, i = m.calMenu.pick(t.field)` then `m.calMenuEvent(ev, i)`.
6. `go test -race ./internal/tui`, then **review `testdata/grid_mouse.golden`**: the status bar gains `[Calendars]`.

**Commit:** `feat(tui): calendar menu to choose which calendars show`

---

## Task 14.14: The quick-add parser (item 6)

**Goal:** one function reads a Google-style line into an event. The CLI (14.15) and the TUI (14.16, 14.18) both use it, so they
always agree.

**The rule:** recognized tokens may appear **only at the end** of the line, in any order, each **at most once**.
Scanning goes **from the end** and stops at the first word that isn't one; everything before it is the title. So
`Report Q3 2026` keeps its title, because `2026` alone isn't a token.

| Token | Examples | Notes |
|---|---|---|
| date | `fri`, `tomorrow`, `20/11`, `2026-11-20`, `+3d` | the same words as `--at` (Task 14.9) |
| date range | `20/11-23/11`, `fri-sun` | all-day; the last day is inclusive |
| time | `13:00`, `9am` | |
| time range | `9:00-10:00`, `22:00-01:00` | an end ≤ start crosses midnight |
| duration | `45m`, `1h30m`, `2d` | whole days with no time → all-day for N days |
| repeat | `daily`, `weekdays`, `weekly`, `monthly`, `yearly` | the same words as `--repeat` |

**No date** means the default day or days: the selected day in the TUI (or the span dragged with the mouse), or today in
the CLI. **No time** means all-day. A sub-day duration with no time is an error ("a duration needs a start time"). A
date range with a time is an error ("a date range is all-day").

A known edge: a title that *ends* in a token word is read as a token (`Read the daily` repeats daily). The live
preview (14.16) makes this visible before saving.

**Files:** `internal/quickadd/quickadd.go` (new), `internal/quickadd/quickadd_test.go` (new),
`internal/timeparse/time.go` (exported helpers)

1. `timeparse`: export what quick add needs, so nothing is parsed twice:
   ```go
   // ParseDate reads a date token (no time): today, fri, +3d, 20/11, 2026-11-20.
   func (p Parser) ParseDate(s string, now time.Time) (time.Time, bool) {
   	return p.parseDate(strings.ToLower(s), now)
   }

   // ParseClock reads a time of day: 14:00, 9:30am, 2pm.
   func ParseClock(s string) (h, m int, ok bool) { return parseClock(strings.ToLower(s)) }

   // ParseDateRange reads "<date>-<date>" (e.g. 20/11-23/11, fri-sun, ISO too).
   // to is the inclusive last day; ok is false if s isn't a range.
   func (p Parser) ParseDateRange(s string, now time.Time) (from, to time.Time, ok bool) {
   	for i := range len(s) { // try every "-": ISO dates contain dashes too
   		if s[i] != '-' {
   			continue
   		}
   		a, okA := p.ParseDate(s[:i], now)
   		b, okB := p.ParseDate(s[i+1:], now)
   		if okA && okB && !b.Before(a) {
   			return a, b, true
   		}
   	}
   	return time.Time{}, time.Time{}, false
   }
   ```
2. Tests (`quickadd_test.go`). `now` is Thu 2026-11-12 10:30 New York; the default day is Nov 12:
   ```go
   package quickadd_test

   import (
   	"testing"
   	"time"
   	_ "time/tzdata"

   	"github.com/jabiOP/clindar/internal/quickadd"
   	"github.com/jabiOP/clindar/internal/timeparse"
   )

   func TestParse(t *testing.T) {
   	ny, _ := time.LoadLocation("America/New_York")
   	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny)
   	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, ny) }
   	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, ny) }
   	today := quickadd.Defaults{From: day(12), To: day(12)}
   	tests := []struct {
   		line               string
   		def                quickadd.Defaults
   		title              string
   		start, end         time.Time
   		allDay             bool
   		rule               string // first recurrence line, "" = none
   		wantErr            bool
   	}{
   		{line: "Event 9:00-10:00", def: today, title: "Event", start: at(12, 9, 0), end: at(12, 10, 0)},
   		{line: "Lunch with Sam fri 13:00", def: today, title: "Lunch with Sam", start: at(13, 13, 0), end: at(13, 14, 0)},
   		{line: "Dentist 20/11 15:30 45m", def: today, title: "Dentist", start: at(20, 15, 30), end: at(20, 16, 15)},
   		{line: "Trip 20/11-23/11", def: today, title: "Trip", start: day(20), end: day(24), allDay: true},
   		{line: "Standup weekdays 9:00 15m", def: today, title: "Standup", start: at(12, 9, 0), end: at(12, 9, 15),
   			rule: "RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
   		{line: "Call mom", def: today, title: "Call mom", start: day(12), end: day(13), allDay: true},
   		{line: "Report Q3 2026", def: today, title: "Report Q3 2026", start: day(12), end: day(13), allDay: true},
   		{line: "Gym tomorrow 7am weekly", def: today, title: "Gym", start: at(13, 7, 0), end: at(13, 8, 0),
   			rule: "RRULE:FREQ=WEEKLY;BYDAY=FR"},
   		{line: "Party 22:00-01:00", def: today, title: "Party", start: at(12, 22, 0), end: at(13, 1, 0)},
   		{line: "Off 2d", def: today, title: "Off", start: day(12), end: day(14), allDay: true},
   		{line: "Conference", def: quickadd.Defaults{From: day(17), To: day(19)}, title: "Conference",
   			start: day(17), end: day(20), allDay: true}, // a span dragged with the mouse
   		{line: "9:00", def: today, wantErr: true},                   // no title
   		{line: "Nap 30m", def: today, wantErr: true},                // a duration needs a start time
   		{line: "Trip 20/11-23/11 9:00", def: today, wantErr: true},  // a date range is all-day
   		{line: "Thing 9:00-10:00 30m", def: today, wantErr: true},   // an end and a duration
   	}
   	for _, tt := range tests {
   		t.Run(tt.line, func(t *testing.T) {
   			r, err := quickadd.Parse(tt.line, tt.def, now, timeparse.Parser{})
   			if (err != nil) != tt.wantErr {
   				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
   			}
   			if tt.wantErr {
   				return
   			}
   			if r.Title != tt.title || !r.Span.Start.Equal(tt.start) || !r.Span.End.Equal(tt.end) || r.Span.AllDay != tt.allDay {
   				t.Errorf("got %q %v→%v allDay=%v", r.Title, r.Span.Start, r.Span.End, r.Span.AllDay)
   			}
   			if got := first(r.Recurrence); got != tt.rule {
   				t.Errorf("rule = %q, want %q", got, tt.rule)
   			}
   		})
   	}
   }

   func first(xs []string) string {
   	if len(xs) == 0 {
   		return ""
   	}
   	return xs[0]
   }

   func TestPreview(t *testing.T) {
   	ny, _ := time.LoadLocation("America/New_York")
   	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny)
   	def := quickadd.Defaults{From: time.Date(2026, 11, 12, 0, 0, 0, 0, ny), To: time.Date(2026, 11, 12, 0, 0, 0, 0, ny)}
   	for line, want := range map[string]string{
   		"Standup weekdays 9:00 15m": "Thu 12 Nov, 09:00–09:15, every weekday",
   		"Trip 20/11-23/11":          "Fri 20 Nov – Mon 23 Nov, all day",
   		"Party 22:00-01:00":         "Thu 12 Nov, 22:00–01:00 (next day)",
   	} {
   		r, err := quickadd.Parse(line, def, now, timeparse.Parser{})
   		if err != nil || r.Preview() != want {
   			t.Errorf("%q: preview %q, %v; want %q", line, r.Preview(), err, want)
   		}
   	}
   }
   ```
   **Red:** the package doesn't exist.
3. `quickadd.go`:
   ```go
   // Package quickadd reads Google-style one-line events such as
   // "Dentist 20/11 15:30 45m". The CLI and the TUI share it, so they agree.
   package quickadd

   import (
   	"errors"
   	"strings"
   	"time"

   	"github.com/jabiOP/clindar/internal/recur"
   	"github.com/jabiOP/clindar/internal/timeparse"
   )

   // Defaults are the days used when the line names none: the TUI's selected day
   // (or a span dragged with the mouse), or today in the CLI. To may equal From.
   type Defaults struct{ From, To time.Time }

   type Result struct {
   	Title      string
   	Span       timeparse.Span
   	Recurrence []string
   	repeat     string // the repeat word, for Preview
   }

   var repeatWords = map[string]string{
   	"daily": "every day", "weekdays": "every weekday", "weekly": "every week",
   	"monthly": "every month", "yearly": "every year",
   }

   type clock struct{ h, m int }

   // Parse reads "<title> <tokens>" (see the table in the plan, Task 14.14).
   func Parse(line string, def Defaults, now time.Time, p timeparse.Parser) (Result, error) {
   	words := strings.Fields(line)
   	var (
   		from, to          time.Time
   		haveDate          bool
   		haveRange         bool
   		start, end        *clock
   		dur               time.Duration
   		repeat            string
   	)
   	i := len(words)
   scan:
   	for ; i > 0; i-- {
   		w := strings.ToLower(words[i-1])
   		switch {
   		case repeat == "" && repeatWords[w] != "":
   			repeat = w
   		case dur == 0 && isDuration(w):
   			dur, _ = timeparse.ParseDuration(w)
   		case start == nil && clockRange(w) != nil:
   			r := clockRange(w)
   			start, end = &r[0], &r[1]
   		case start == nil && isClock(w):
   			h, m, _ := timeparse.ParseClock(w)
   			start = &clock{h, m}
   		case !haveDate && !haveRange && isRange(p, w, now):
   			from, to, _ = p.ParseDateRange(w, now)
   			haveRange = true
   		case !haveDate && !haveRange && isDate(p, w, now):
   			from, _ = p.ParseDate(w, now)
   			to, haveDate = from, true
   		default:
   			break scan
   		}
   	}
   	r := Result{Title: strings.Join(words[:i], " "), repeat: repeat}
   	if r.Title == "" {
   		return r, errors.New("no title: start with the event's name, e.g. \"Dentist 20/11 15:30\"")
   	}
   	if !haveDate && !haveRange {
   		from, to = def.From, def.To
   	}
   	at := func(c clock) time.Time { return time.Date(from.Year(), from.Month(), from.Day(), c.h, c.m, 0, 0, from.Location()) }
   	switch {
   	case start != nil && haveRange:
   		return r, errors.New("a date range is all-day: drop the time, or give one date")
   	case start != nil && end != nil && dur > 0:
   		return r, errors.New("give an end time or a duration, not both")
   	case start != nil:
   		s := at(*start)
   		e := s.Add(time.Hour)
   		if end != nil {
   			if e = at(*end); !e.After(s) {
   				e = e.AddDate(0, 0, 1) // 22:00-01:00 ends the next day
   			}
   		}
   		if dur > 0 {
   			e = s.Add(dur)
   		}
   		r.Span = timeparse.Span{Start: s, End: e}
   	case dur > 0 && dur%(24*time.Hour) == 0:
   		r.Span = timeparse.Span{Start: day(from), End: day(from).AddDate(0, 0, int(dur/(24*time.Hour))), AllDay: true}
   	case dur > 0:
   		return r, errors.New("a duration needs a start time, e.g. \"Nap 14:00 30m\"")
   	default:
   		r.Span = timeparse.Span{Start: day(from), End: day(to).AddDate(0, 0, 1), AllDay: true}
   	}
   	if repeat != "" {
   		rec, err := recur.ParseRepeat(repeat, r.Span.Start, r.Span.AllDay, time.Time{}, 0)
   		if err != nil {
   			return r, err
   		}
   		r.Recurrence = rec
   	}
   	return r, nil
   }

   // Preview describes the event, e.g. "Fri 20 Nov, 09:00–10:00, every weekday".
   func (r Result) Preview() string {
   	s := r.Span
   	var out string
   	switch {
   	case s.AllDay && s.End.After(s.Start.AddDate(0, 0, 1)):
   		out = s.Start.Format("Mon 02 Jan") + " – " + s.End.AddDate(0, 0, -1).Format("Mon 02 Jan") + ", all day"
   	case s.AllDay:
   		out = s.Start.Format("Mon 02 Jan") + ", all day"
   	default:
   		out = s.Start.Format("Mon 02 Jan, 15:04") + "–" + s.End.Format("15:04")
   		if day(s.End) != day(s.Start) && !s.End.Equal(day(s.End)) {
   			out += " (next day)"
   		}
   	}
   	if r.repeat != "" {
   		out += ", " + repeatWords[r.repeat]
   	}
   	return out
   }

   func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()) }

   func isDuration(w string) bool { _, err := timeparse.ParseDuration(w); return err == nil }
   func isClock(w string) bool    { _, _, ok := timeparse.ParseClock(w); return ok }

   func isDate(p timeparse.Parser, w string, now time.Time) bool { _, ok := p.ParseDate(w, now); return ok }
   func isRange(p timeparse.Parser, w string, now time.Time) bool {
   	_, _, ok := p.ParseDateRange(w, now)
   	return ok
   }

   // clockRange reads "9:00-10:00"; nil if w isn't one.
   func clockRange(w string) []clock {
   	a, b, ok := strings.Cut(w, "-")
   	if !ok {
   		return nil
   	}
   	h1, m1, ok1 := timeparse.ParseClock(a)
   	h2, m2, ok2 := timeparse.ParseClock(b)
   	if !ok1 || !ok2 {
   		return nil
   	}
   	return []clock{{h1, m1}, {h2, m2}}
   }
   ```
   (Run `gofmt`: the `var (…)` block alignment above is approximate.)
4. `go test ./internal/quickadd ./internal/timeparse`: pass.

**Commit:** `feat(quickadd): Google-style one-line event parser`

---

## Task 14.15: Quick add in the CLI (item 6)

**Goal:** `clindar add "Dentist 20/11 15:30 45m"` with no `--at` uses the quick-add parser, defaulting to today. With
`--at`, the title stays literal and the flags decide everything, as before. Unquoted works too, because the
arguments are joined. `--end`/`--dur`/`--all-day`/`--repeat`/`--until`/`--count` need `--at`; without it they're a usage
error pointing at the one-line form.

Behavior change: `clindar add Dentist` without `--at` used to be a usage error. It's now an all-day event today. Update
`docs/cli.md`.

**Files:** `internal/cli/write.go`, `internal/cli/write_test.go`

1. Tests:
   ```go
   func TestAddOneLine(t *testing.T) {
   	h := newHarness(t)

   	id := strings.TrimSpace(h.mustRun("add", "Dentist 20/11 15:30 45m", "--offline"))
   	if out := h.mustRun("show", id, "--format", "{{.Title}} {{.Start}} {{.End}}"); out != "Dentist 2026-11-20T15:30:00-05:00 2026-11-20T16:15:00-05:00\n" {
   		t.Errorf("got %q", out)
   	}
   	id = strings.TrimSpace(h.mustRun("add", "Call", "mom", "--offline")) // unquoted: all-day today
   	if out := h.mustRun("show", id, "--tsv"); !strings.Contains(out, "\t2026-11-12\t2026-11-13\t1\t") {
   		t.Errorf("got %q", out)
   	}
   }

   func TestAddOneLineErrorsAreUsageErrors(t *testing.T) {
   	h := newHarness(t)
   	if _, errOut, code := h.run("add", "Nap 30m"); code != 2 || !strings.Contains(errOut, "start time") {
   		t.Errorf("exit %d, %q", code, errOut)
   	}
   	if _, _, code := h.run("add", "Dentist", "--dur", "45m"); code != 2 {
   		t.Errorf("--dur without --at: exit %d, want 2", code)
   	}
   }
   ```
   **Red:** without `--at`, `add` fails with "a start time is required".
2. `write.go`, in `add`'s `RunE`, after reading the title. If `f.at == ""`:
   ```go
   		if f.at == "" {
   			if f.end != "" || f.dur != "" || f.allDay || f.repeat != "" || f.until != "" || f.count > 0 {
   				return usageError{fmt.Errorf("--end/--dur/--all-day/--repeat need --at; or write it all on one line: clindar add \"Dentist 20/11 15:30 45m\"")}
   			}
   			today := model.StartOfDay(now)
   			r, err := quickadd.Parse(title, quickadd.Defaults{From: today, To: today}, now, app.Dates())
   			if err != nil {
   				return usageError{err}
   			}
   			title, t, rec = r.Title, r.Span, r.Recurrence
   		} else {
   			// the existing --at path: ParseSpan, --until, ParseRepeat
   		}
   ```
   (Restructure the existing code so `title`, `t` (the span) and `rec` are declared before the branch.) Update `add`'s
   `Use`, `Long` and `Example` to show the one-line form first.
3. `go test -race ./internal/cli`: pass (the existing `--at` tests are unchanged).

**Commit:** `feat(cli): one-line quick add when --at is omitted`

---

## Task 14.16: Quick add in the TUI (item 6)

**Goal:** `a` (and `[+ Add]`) opens **quick add**: one text line for the selected day, with a **live preview** below it
that shows the interpretation, or what's wrong, as you type.
- `Enter` saves, in the default calendar.
- `Tab` (or `[More…]`) opens the full form, pre-filled from what's typed so far.
- `Esc` cancels.

**Files:** `internal/tui/quick.go` (new), `internal/tui/model.go`, `internal/tui/nav.go`, `internal/tui/layout.go`,
`internal/tui/mouse.go`, `internal/tui/actions_test.go`, `internal/tui/mouse_test.go`

1. Tests (`actions_test.go`). Add a helper the form tests use from now on:
   ```go
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
   ```
   **Update existing tests** that used `a` to reach the form: `TestFormShowsParseErrors` and `TestFormCtrlJKMoveBetweenFields`
   use `f.openFullForm()`. In `TestStatusBarAndFormButtons`, `[+ Add]` now opens quick add; click `[More…]` (key `tab`) to
   get to the form. `TestAddEventThroughForm` still passes: it types "Dentist" and Enter, which is now an all-day quick add
   on Nov 13. **Red:** `modeQuick` doesn't exist.
2. `model.go`: `mode` gains `modeQuick`. `Model` gains `quick textinput.Model` (static cursor, like the others) and `quickDef
   quickadd.Defaults`.
3. `quick.go`:
   ```go
   package tui

   // openQuick starts quick add for the given day(s).
   func (m Model) openQuick(def quickadd.Defaults) (Model, tea.Cmd) {
   	m.quick = textinput.New()
   	m.quick.Prompt, m.quick.Placeholder = "", "Dentist fri 15:30 45m · Trip 20/11-23/11 · Standup weekdays 9:00"
   	m.quick.Cursor.SetMode(cursor.CursorStatic)
   	m.quickDef, m.mode = def, modeQuick
   	return m, m.quick.Focus()
   }

   func (m Model) parseQuick() (quickadd.Result, error) {
   	return quickadd.Parse(m.quick.Value(), m.quickDef, m.deps.Now(), m.deps.Dates)
   }

   func (m Model) quickKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
   	switch k.Type {
   	case tea.KeyEsc:
   		m.mode = modeNormal
   		return m, nil
   	case tea.KeyTab: // the full form, pre-filled
   		r, err := m.parseQuick()
   		if err != nil {
   			r = quickadd.Result{Title: strings.TrimSpace(m.quick.Value()),
   				Span: timeparse.Span{Start: m.quickDef.From, End: m.quickDef.To.AddDate(0, 0, 1), AllDay: true}}
   		}
   		m.form, m.mode = m.formFromQuick(r), modeForm
   		return m, nil
   	case tea.KeyEnter:
   		r, err := m.parseQuick()
   		if err != nil {
   			return m, nil // the preview line already shows the error
   		}
   		d := service.Draft{CalendarID: m.deps.DefaultCalendar, Title: r.Title, Start: r.Span.Start,
   			End: r.Span.End, AllDay: r.Span.AllDay, Recurrence: r.Recurrence}
   		return m, m.act("created "+r.Title, func(ctx context.Context, s *service.Service) error {
   			_, err := s.Create(ctx, d)
   			return err
   		})
   	}
   	var cmd tea.Cmd
   	m.quick, cmd = m.quick.Update(k)
   	return m, cmd
   }

   // quickPopup is quick add's pop-up: the line, then a live preview (or the problem).
   func (m Model) quickPopup() modal {
   	var md modal
   	md.text(headerStyle.Render("New event"))
   	md.text("")
   	md.text(m.quick.View())
   	r, err := m.parseQuick()
   	switch {
   	case strings.TrimSpace(m.quick.Value()) == "":
   		md.text(dimStyle.Render("→ " + quickadd.Result{Span: timeparse.Span{Start: m.quickDef.From,
   			End: m.quickDef.To.AddDate(0, 0, 1), AllDay: true}}.Preview() + " · type a title"))
   	case err != nil:
   		md.text(errorStyle.Render("? " + err.Error()))
   	default:
   		md.text(dimStyle.Render("→ " + r.Preview()))
   	}
   	md.text("")
   	if m.deps.Mouse {
   		md.buttons(button{"Save", "enter"}, button{"More…", "tab"}, button{"Cancel", "esc"})
   	}
   	md.text(dimStyle.Render("enter save · tab more fields · esc cancel"))
   	return md
   }
   ```
   `formFromQuick(r)` fills the **current** form's fields from `r`: Title; for timed events, Start as
   `"2006-01-02 15:04"` and End as `"15:04"`; for all-day events, Start as the first day and End as the inclusive last day;
   Repeat as `r`'s repeat word. (Task 14.17 rewrites it for the new form.) Add an exported
   `func (r Result) RepeatWord() string { return r.repeat }` to `quickadd` for this.
4. Wiring:
   - `nav.go`: `keys.Add` → `m.openQuick(quickadd.Defaults{From: m.selected, To: m.selected})`.
   - `handleKey`: `case modeQuick: return m.quickKey(k)`.
   - `layout.go`: `popup` returns `m.quickPopup()` in `modeQuick`.
   - `mouse.go`: `keyMsg` maps `"tab"` to `tea.KeyMsg{Type: tea.KeyTab}`.
5. `go test -race ./internal/tui`: pass.

**Commit:** `feat(tui): quick add with live preview`

---

## Task 14.17: The full form (item 6)

**Goal:** the form becomes:

| # | Field | Kind | Notes |
|---|---|---|---|
| 1 | Title | text | |
| 2 | Date | text | `20/11` (one day) or `20/11-23/11` (a range, all-day only) |
| 3 | All-day | picker | yes / no |
| 4 | Start | text | a time: `15:30`, `3pm`. **Typing one switches All-day to no** |
| 5 | End | text | a time (same day; ≤ start = the next day), or `date time` |
| 6 | Duration | text | instead of End |
| 7 | Calendar | picker | **writable** calendars only; on edit, locked ("can't change") |
| 8 | Repeat | picker | none, daily, weekdays, weekly, monthly, yearly; on edit, locked |
| 9 | Where | text | |
| 10 | Notes | text | |
| 11 | Invite | text | comma-separated emails |

(Task 14.20 adds **Color**, a picker, after Repeat.)

- **New events default to all-day on the selected day**, with Start empty (the form no longer pre-fills 09:00).
- **Errors show under the field they belong to**, and the cursor jumps there.
- On a picker field: `h`/`l` (and `←`/`→`, `Ctrl-N`/`Ctrl-P`) cycle, and `Space`/`Enter` open its dropdown. On a text field,
  `Enter` saves.
- With the mouse on, clicking a picker field opens its dropdown, and clicking an option chooses it.

This is a larger task: commit after each numbered step that compiles.

**Files:** `internal/tui/form.go`, `internal/tui/form_test.go` (new), `internal/tui/actions.go` (`formKey`),
`internal/tui/quick.go` (`formFromQuick`), `internal/tui/mouse.go`, `internal/tui/nav.go`/`actions.go` (callers of
`newEventForm`/`editEventForm`)

1. Tests (`form_test.go`):
   ```go
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
   ```
   with a helper in `form_test.go`:
   ```go
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
   ```
   **Red:** `fDate`, `fAllDay`, `form.pickers` don't exist.
2. `form.go`, the structure:
   ```go
   const (
   	fTitle = iota
   	fDate
   	fAllDay
   	fStart
   	fEnd
   	fDuration
   	fCalendar
   	fRepeat
   	fWhere
   	fNotes
   	fInvite
   	fieldCount
   )

   var fieldLabels = [fieldCount]string{"Title", "Date", "All-day", "Start", "End", "Duration", "Calendar", "Repeat", "Where", "Notes", "Invite"}

   type form struct {
   	inputs  [fieldCount]textinput.Model // text fields
   	pickers map[int]*picker             // picker fields: fAllDay, fCalendar, fRepeat (and fColor, Task 14.20)
   	locked  map[int]string              // pickers that can't change on edit, with the reason shown
   	initial [fieldCount]string          // to detect what an edit changed (pickers: the chosen value)
   	focus   int
   	editing *model.Event
   	errs    map[int]string // per-field errors, shown under the field
   	dates   timeparse.Parser
   }

   // fieldError is an error that belongs to one field.
   type fieldError struct {
   	field int
   	msg   string
   }

   func (e fieldError) Error() string { return e.msg }
   ```
   `value(i)` returns the text for text fields and `pickers[i].current().value` for pickers. `changed(i)` compares with
   `initial[i]`.
3. Constructors:
   - `newEventForm(day, cals []model.Calendar, defaultCal string, p timeparse.Parser)`:
     - Date: the day in the configured order (`"02/01/2006"` for DMY, `"01/02/2006"` for MDY).
     - All-day: `yes`. Start empty.
     - Calendar: the writable calendars, in store order, pre-selecting `defaultCal` by name or ID, else the primary.
     - Repeat: `none`, `daily`, `weekdays`, `weekly`, `monthly`, `yearly`.
   - `editEventForm(e, loc, cals, p)`:
     - Date: the event's first day, or `first-last` for multi-day all-day events.
     - All-day: from the event. Start and End: clocks.
     - Calendar and Repeat: `locked` ("can't change"), as before.
   - `formFromQuick` (in `quick.go`) builds on `newEventForm` and sets the fields from the quick-add result.
   
   Callers pass `m.calList` and `m.deps.Dates`.
4. `Update`: if the focused field is a picker, it gets the key first:
   - If it's open, everything goes to `picker.Update`.
   - If closed, `h`, `l`, `left`, `right`, `ctrl+n`, `ctrl+p`, `space` and `enter` go to it.
   - Locked pickers ignore everything except navigation.
   
   Navigation (`tab`, `shift+tab`, `ctrl+j`, `ctrl+k`, `up`/`down` when no picker is open) works as before. When **Start**
   changes from empty to non-empty, set All-day to `no`. In `actions.go`, `formKey` only calls `submitForm` on Enter
   when the focused field is **not** a picker (`!m.form.isPicker(m.form.focus)`).
5. Timing, in one function:
   ```go
   // span reads Date, All-day, Start, End and Duration. A fieldError says which field is wrong.
   func (f form) span(now time.Time) (timeparse.Span, error) {
   	dateText := f.value(fDate)
   	from, to, isRange := f.dates.ParseDateRange(dateText, now)
   	if !isRange {
   		d, ok := f.dates.ParseDate(dateText, now)
   		if !ok {
   			return timeparse.Span{}, fieldError{fDate, "not a date (try 20/11, fri, 20/11-23/11)"}
   		}
   		from, to = d, d
   	}
   	if f.value(fAllDay) == "yes" {
   		return timeparse.Span{Start: from, End: to.AddDate(0, 0, 1), AllDay: true}, nil
   	}
   	if isRange {
   		return timeparse.Span{}, fieldError{fDate, "a date range is all-day: set All-day to yes, or give one date"}
   	}
   	h, mi, ok := timeparse.ParseClock(f.value(fStart))
   	if !ok {
   		return timeparse.Span{}, fieldError{fStart, "not a time (try 15:30 or 3pm), or set All-day to yes"}
   	}
   	start := time.Date(from.Year(), from.Month(), from.Day(), h, mi, 0, 0, from.Location())
   	end, dur := f.value(fEnd), f.value(fDuration)
   	switch {
   	case end != "" && dur != "":
   		return timeparse.Span{}, fieldError{fDuration, "give an end or a duration, not both"}
   	case dur != "":
   		d, err := timeparse.ParseDuration(dur)
   		if err != nil {
   			return timeparse.Span{}, fieldError{fDuration, err.Error()}
   		}
   		return timeparse.Span{Start: start, End: start.Add(d)}, nil
   	case end != "":
   		e, _, err := f.dates.ParseTime(end, start) // a bare time is on the start's day
   		if err != nil {
   			return timeparse.Span{}, fieldError{fEnd, "not a time (try 16:30, or 21/11 02:00)"}
   		}
   		if !e.After(start) {
   			e = e.AddDate(0, 0, 1) // 22:00 → 01:00 ends the next day
   		}
   		return timeparse.Span{Start: start, End: e}, nil
   	}
   	return timeparse.Span{Start: start, End: start.Add(time.Hour)}, nil
   }
   ```
   `draft` and `changes` use it (timing changes when any of Date, All-day, Start, End or Duration changed). Repeat: if not
   `none`, `recur.ParseRepeat(word, span.Start, span.AllDay, time.Time{}, 0)`. Calendar: the picker's value (an ID).
   Errors that aren't `fieldError`s (e.g. from the service) still go in the status bar via `actionDoneMsg`.
6. `submitForm`: on a `fieldError`, set `m.form.errs = map[int]string{fe.field: fe.msg}`, move focus there, and stay in the
   form. Clear `errs` on any later key.
7. `view`:
   - Each field line is `label + "  " + (input.View() | picker.inline() | "‹ … › (can't change)")`.
   - Under a field with an error, add an indented `errorStyle` line (12 spaces, under the value).
   - When the focused picker is **open**, draw its options right under the field line with `md.option`, so the dropdown is
     inline and clickable.
   - Hint: `"ctrl+j/tab next · ctrl+k previous · h/l change · space list · enter save · esc cancel"`.
8. `mouse.go`, `press` in `modeForm`:
   - A `targetField` on a picker field focuses it **and opens** it.
   - A `targetOption` calls `m.form.pickers[m.form.focus].pick(t.field)`.
9. `go test -race ./internal/tui`: pass. Then **look at it** at 80 columns: open quick add, `tab`, walk through all fields,
   and open each dropdown.

**Commit:** `feat(tui): full form with date, all-day, pickers, and errors by field`

---

## Task 14.18: Mouse: click or drag empty space to add (item 6)

**Goal:** Google-style creating with the mouse.
- **Click empty space** in a grid day (below its events, not on the day number or an event) opens quick add for that
  day.
- **Drag across empty space** in several days opens quick add with an **all-day span** from the first to the last day
  dragged over. While dragging, the span is marked `»` (the existing drop-target marking, now for a range).
- Clicking or dragging an **event** keeps its behavior (details, move, resize). Clicking the **day number** keeps
  selecting the day (and, on the selected day, enters event mode: Task 14.8).

**Files:** `internal/tui/hits.go`, `internal/tui/grid.go`, `internal/tui/layout.go`, `internal/tui/mouse.go`,
`internal/tui/model.go`, `internal/tui/mouse_test.go`

1. Tests (`mouse_test.go`). In the fixture, Tue 17 has no events, and Thu 19 has one standup with blank lines below it:
   ```go
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
   ```
   **Red:** there's no `targetEmpty`.
2. `hits.go`: `targetEmpty // blank space in a grid day, below its events`. In `grid.go`'s `cell`, the padding loop adds a
   hit per blank line: `hits.add(0, len(lines), w, 1, target{kind: targetEmpty, day: day.Date})`, before appending the line.
   (The whole-cell `targetDay` stays underneath, so the label row still selects the day.)
3. The drop marking becomes a range: `gridView.drop` → `dropFrom, dropTo time.Time`. A day is marked `»` when it's within
   them. Event drags pass the same day for both. `layout.go` passes the active drag's range (event drag:
   `m.drag.over` twice; span drag: the ordered `from`/`over`).
4. `model.go`: `Model` gains:
   ```go
   	span spanDrag // mouse drag across empty days (Task 14.18)
   ```
   ```go
   type spanDrag struct {
   	active     bool
   	from, over time.Time
   }
   ```
   `mouse.go`:
   - **`press`, `case targetEmpty`:** `m = m.selectDay(t.day)`, `m.dayMode = false`,
     `m.span = spanDrag{true, t.day, t.day}`.
   - **`handleMouse`:**
     - Motion while `m.span.active` → `m.span.over = t.day` for day-like targets (`targetDay`, `targetEmpty`,
       `targetEvent`, `targetMore`), with the same edge scrolling as `dragOver`. **Put this case before the hover case**
       (`msg.Action == tea.MouseActionMotion && !m.drag.active`). That case matches every motion without an event drag,
       so it would otherwise swallow span-drag motion.
     - Release while `m.span.active`:
       ```go
       	s := m.span
       	m.span = spanDrag{}
       	from, to := s.from, s.over
       	if to.Before(from) {
       		from, to = to, from
       	}
       	return m.openQuick(quickadd.Defaults{From: from, To: to})
       ```
     - `Esc` during a span drag cancels it, like an event drag.
5. `go test -race ./internal/tui`: pass, and no golden file changes (nothing is dragged in them).

**Commit:** `feat(tui): click or drag empty space to add an event`

---

## Task 14.19: Per-event colors: the data (item 5)

**Goal:** an event's own color (Google's `colorId`) goes through the whole data path:
- **Model:** `Event.ColorID`, stored in the existing JSON blob, so no migration is needed for events.
- **Google adapter:** read it, write it, and **clear** it.
- **Palette:** Google's event palette comes from `colors.get`. It's fetched once and cached in the store (migration 3); `sync
  --full` refreshes it.
- **Fake:** the fake Google supports colors and the palette, with its own test.
- **JSON output:** a new field, `color`. It's the event's **effective** color as `#rrggbb`: its own color if it has one,
  otherwise its calendar's.

**Clearing needs care:** this is the same family of gotcha as Task 13.13. An empty `ColorId` is omitted by the Go
client, so "back to the calendar's color" would silently do nothing. `ToAPI` sends `colorId: null` via `NullFields`
when `ColorID` is empty. Task 14.23 checks against real Google that null clears it. If it doesn't, try
`ForceSendFields` with `""` and log the result.

**Files:** `internal/model/event.go`, `internal/model/calendar.go`, `internal/model/*_test.go`, `internal/gcal/convert.go`,
`internal/gcal/client.go`, `internal/gcal/*_test.go`, `internal/gcal/gcaltest/fake.go`, `internal/gcal/gcaltest/fake_test.go`,
`internal/syncer/engine.go`, `internal/syncer/pull.go`, `internal/syncer/pull_test.go`, `internal/store/store.go`
(migration 3), `internal/store/state.go`, `internal/store/state_test.go`, `internal/service/service.go`,
`internal/output/output.go`, `internal/output/output_test.go`, `internal/cli/app.go`

1. **Tests**, one per layer:
   - `model`: `TestEffectiveColor`, a table of: own color in the palette → the palette's hex; no own color → the calendar's
     color; an own color missing from the palette → the calendar's color.
   - `gcal/convert_test.go`: `FromAPI` reads `ColorId: "11"`. `ToAPI` with `ColorID: "11"` sends `"colorId":"11"`. `ToAPI`
     with `ColorID: ""` sends `"colorId":null` (check the marshalled body, as `TestToAPIClearsTheOtherKindOfStartAndEnd` does).
   - `gcal/client_test.go`: `EventColors` reads `/colors` →
     `{"event":{"11":{"background":"#dc2127","foreground":"#1d1d1d"}}}` into `{"11": "#dc2127"}`.
   - `gcaltest/fake_test.go`: `EventColors` returns `gcaltest.Palette`. A patch with `ColorID: ""` clears a stored color.
   - `store/state_test.go`: `SetEventColors` then `EventColors` round-trips. `ResetSyncTokens` empties the palette.
   - `syncer/pull_test.go`: two syncs call the fake's `EventColors` **once** (add a call counter `ColorCalls` to the fake).
   - `output/output_test.go`: with `SetPalette(gcaltest.Palette)`, an event with `ColorID: "11"` has `"color":"#dc2127"`
     in `--json`. One without has the calendar's color.
   
   **Red:** none of these fields or methods exist.
2. `model`:
   ```go
   	ColorID string `json:"color_id,omitempty"` // Google's event colorId; "" = the calendar's color
   ```
   ```go
   // EffectiveColor is the color an event is drawn in: its own color from the
   // palette (colorId → "#rrggbb") if it has one, else its calendar's.
   func EffectiveColor(e Event, cal Calendar, palette map[string]string) string {
   	if c, ok := palette[e.ColorID]; ok && e.ColorID != "" {
   		return c
   	}
   	return cal.Color
   }
   ```
3. `gcal`. In `FromAPI`: `e.ColorID = ev.ColorId`. In `ToAPI`:
   ```go
   	ev.ColorId = e.ColorID
   	if e.ColorID == "" {
   		ev.NullFields = append(ev.NullFields, "ColorId") // back to the calendar's color; omitted fields don't clear
   	}
   ```
   `client.go`:
   ```go
   // EventColors returns Google's event palette: colorId → background "#rrggbb".
   func (c *Client) EventColors(ctx context.Context) (map[string]string, error) {
   	res, err := c.svc.Colors.Get().Context(ctx).Do()
   	if err != nil {
   		return nil, mapErr(err)
   	}
   	out := map[string]string{}
   	for id, def := range res.Event {
   		out[id] = def.Background
   	}
   	return out, nil
   }
   ```
   `syncer.Remote` gains `EventColors(ctx) (map[string]string, error)`.
4. Fake: `PatchEvent` copies `ColorID` like the other editable fields, and:
   ```go
   // Palette is Google's standard event palette (colorId → background).
   var Palette = map[string]string{
   	"1": "#a4bdfc", "2": "#7ae7bf", "3": "#dbadff", "4": "#ff887c", "5": "#fbd75b", "6": "#ffb878",
   	"7": "#46d6db", "8": "#e1e1e1", "9": "#5484ed", "10": "#51b749", "11": "#dc2127",
   }

   func (f *Fake) EventColors(ctx context.Context) (map[string]string, error) {
   	f.mu.Lock()
   	defer f.mu.Unlock()
   	f.ColorCalls++
   	if f.Fail != nil {
   		return nil, f.Fail
   	}
   	return maps.Clone(Palette), nil
   }
   ```
5. Store: **append** migration 3:
   ```sql
   CREATE TABLE event_colors (
   	account_id TEXT NOT NULL,
   	color_id   TEXT NOT NULL,
   	background TEXT NOT NULL,
   	PRIMARY KEY (account_id, color_id)
   );
   ```
   `state.go` gains `EventColors(ctx, accountID) (map[string]string, error)` and `SetEventColors(ctx, accountID,
   map[string]string) error` (delete then insert, in one `InTx`). `ResetSyncTokens` also deletes the account's
   `event_colors`, and `DeleteAccount`'s table list gains it.
6. Sync: at the start of `Pull`, if `Store.EventColors` is empty, fetch `Remote.EventColors` and store it. A failure here
   is returned like any pull error.
7. `service.EventColors(ctx)` passes through. `output`: `Printer.SetPalette(map[string]string)`. `EventJSON` gains
   ```go
   	Color string `json:"color,omitempty"` // effective color: the event's own, else its calendar's ("#rrggbb")
   ```
   set with `model.EffectiveColor`. `cli/app.go`'s `Printer` loads the palette from the service and calls `SetPalette`.
8. `go test -race ./...`: pass.

**Commit:** `feat: per-event colors in the model, Google adapter, store and JSON`

---

## Task 14.20: Per-event colors: display and the Color picker (item 5)

**Goal:**
- Events with their own color are drawn in it in the grid and the list (the list's `●` dot). Otherwise they keep the
  calendar's color.
- The form gets a **Color** picker after Repeat: "Calendar default" plus Google's 11 colors, by Google's names, each with a
  swatch. "Calendar default" clears the color (Task 14.19). Color **can** change on edit.

**Files:** `internal/tui/model.go`, `internal/tui/grid.go`, `internal/tui/list.go`, `internal/tui/layout.go`,
`internal/tui/form.go`, `internal/tui/grid_test.go`, `internal/tui/form_test.go`, `internal/service/create.go`,
`internal/service/update.go`, `internal/service/update_test.go`

1. Tests:
   - `grid_test.go`, with colors on (`termenv.TrueColor`): an event with `ColorID: "11"` and the palette passed in renders
     with `38;2;220;33;39` (#dc2127). The same event without `ColorID`, on a calendar colored `#4285f4`, renders with
     `38;2;66;133;244`.
   - `form_test.go`: open the full form, title "Dinner", `f.focus(fColor)`, `l` (→ Tomato, the first color), save. The created
     event has `ColorID == "11"`. Then edit it, Color `h` (→ Calendar default), save. The event's `ColorID == ""`, and the
     queued op's payload has `ColorID == ""`.
   - `service/update_test.go`: `Changes{ColorID: ptr("4")}` updates the event and queues a patch.
   
   **Red:** no `fColor`, no palette in views, no `ColorID` in `Changes`.
2. `service`: `Draft.ColorID string` (copied in `Create`). `Changes.ColorID *string`, applied in `apply` like `Location`.
3. TUI:
   - `loadedMsg` carries the palette (`svc.EventColors`), stored in `Model.palette`.
   - `gridView` and `listView` gain `palette map[string]string`. `eventLine` and the list's dot use
     `model.EffectiveColor(e, cal, palette)`.
   - The detail printer gets `SetPalette` too.
4. Form: add `fColor` after `fRepeat` in the field constants and `fieldLabels`. Its picker options, in Google's menu order:
   ```go
   var colorNames = []struct{ id, name string }{
   	{"11", "Tomato"}, {"4", "Flamingo"}, {"6", "Tangerine"}, {"5", "Banana"}, {"2", "Sage"}, {"10", "Basil"},
   	{"7", "Peacock"}, {"9", "Blueberry"}, {"1", "Lavender"}, {"3", "Grape"}, {"8", "Graphite"},
   }
   ```
   preceded by `option{label: "Calendar default", value: ""}`. Swatches come from the palette. The value goes to
   `Draft.ColorID`, or to `Changes.ColorID` when it changed.
5. `go test -race ./...`: pass, with no golden changes (colors off).

**Commit:** `feat(tui): draw events in their own color; Color picker in the form`

---

## Task 14.21: Details: no internals; [Open in Google] (item 10)

**Goal:**
- The TUI's details box drops the **ID** and raw **Link** lines. (`clindar show` keeps the ID, which scripts use.)
- **[Open in Google]** and the `o` key open the event's web page.
- The browser opener moves from `auth` to a neutral package, `internal/browser`, so the TUI doesn't import `auth`.

**Files:** `internal/browser/browser.go` (new), `internal/auth/login.go`, `internal/cli/authcmd.go`, `internal/output/output.go`,
`internal/output/output_test.go`, `internal/tui/model.go`, `internal/tui/layout.go`, `internal/tui/actions.go`,
`internal/tui/detail_test.go`, `internal/cli/root.go` (`tuiDeps`), `internal/cli/tuideps_test.go`

1. Tests:
   - `output_test.go`: after `p.OmitInternals()`, the pretty detail has no `"ID:"` and no `"Link:"`. Without it, both are there.
   - `detail_test.go`:
     ```go
     func TestOpenInGoogle(t *testing.T) {
     	var opened string
     	f := newFixtureWith(t, Deps{OpenURL: func(u string) error { opened = u; return nil }})
     	e := testutil.Timed("ev", "Review", time.Date(2026, 11, 12, 15, 0, 0, 0, f.ny), time.Hour)
     	e.Link = "https://www.google.com/calendar/event?eid=abc"
     	f.put(e)
     	f.openDetail(e)

     	if s := f.m.View(); strings.Contains(s, "ID:") || strings.Contains(s, "Link:") {
     		t.Errorf("internals in the details box:\n%s", s)
     	}
     	f.keys("o")

     	if opened != e.Link || !strings.Contains(f.model().message, "browser") {
     		t.Errorf("opened %q, message %q", opened, f.model().message)
     	}
     }
     ```
   - `tuideps_test.go`: `d.OpenURL != nil`.
   
   **Red:** `OmitInternals`/`Deps.OpenURL` don't exist.
2. `internal/browser/browser.go`: move `OpenBrowser` from `auth` here as `browser.Open(url string) error` (same body).
   `authcmd.go` calls `browser.Open`. Delete `auth.OpenBrowser`.
3. `output`: `func (p *Printer) OmitInternals() { p.omitInternals = true }`. `prettyDetail` skips the ID and Link rows when
   it's set.
4. TUI:
   - `Deps.OpenURL func(string) error` (`tuiDeps`: `browser.Open`).
   - A message for one-off results that only update the status bar:
     ```go
     type infoMsg struct{ text string }
     ```
     `Update` sets `m.message = msg.text`.
   - In `layout.go`'s detail pop-up, call `p.OmitInternals()`. When `m.detail.Link != ""`, add `button{"Open in Google",
     "o"}` (and `"o open in Google"` in the keyboard hint).
   - `detailKey`: `o` with a link →
     ```go
     		open, link := m.deps.OpenURL, m.detail.Link
     		return m, func() tea.Msg {
     			if err := open(link); err != nil {
     				return infoMsg{"couldn't open the browser: " + err.Error()}
     			}
     			return infoMsg{"opened in your browser"}
     		}
     ```
5. `go test -race ./...`: pass.

**Commit:** `feat(tui): details without internals; open in Google`

---

## Task 14.22: Meet links: show, [Join], [Copy] (item 10)

**Goal:** clindar **reads** Meet links; it never creates them.
- `Event.MeetLink` comes from `hangoutLink`, or else the `conferenceData` entry point of type `video`.
- The details box (TUI and `clindar show`) gets a **Meet** line. The JSON output gets `meet_link`.
- In the TUI, **[Join]** (`m`) opens the link, and **[Copy]** (`y`, like vim's yank) copies it to the clipboard via **OSC 52**.

Patches never send `conferenceData`, so Google keeps the meeting attached.

**OSC 52** is an escape sequence that asks the *terminal* to set the clipboard. It works over SSH and in most terminals
(iTerm2, kitty, WezTerm, Alacritty, Windows Terminal, and tmux with `set -g set-clipboard on`). **macOS Terminal.app doesn't
support it.** There, [Copy] does nothing visible, but the Meet line is still on screen to select with Option-drag. Task 14.23
checks this.

**Files:** `internal/model/event.go`, `internal/gcal/convert.go`, `internal/gcal/convert_test.go`, `internal/output/output.go`,
`internal/output/output_test.go`, `internal/clipboard/clipboard.go` (new), `internal/tui/model.go`, `internal/tui/layout.go`,
`internal/tui/actions.go`, `internal/tui/detail_test.go`, `internal/cli/root.go`, `internal/cli/tuideps_test.go`

```bash
go get github.com/aymanbagabas/go-osc52/v2@v2.0.1   # already in go.sum via termenv; check the version there and use it
```

1. Tests:
   - `convert_test.go`: `HangoutLink: "https://meet.google.com/abc-defg-hij"` → `MeetLink`. Without it,
     `ConferenceData{EntryPoints: [{EntryPointType: "phone", Uri: "tel:…"}, {EntryPointType: "video", Uri:
     "https://meet.google.com/xyz"}]}` → the video URI. `ToAPI` doesn't send `conferenceData`.
   - `output_test.go`: pretty detail has `"Meet:"`; JSON has `"meet_link"`.
   - `detail_test.go`:
     ```go
     func TestMeetJoinAndCopy(t *testing.T) {
     	var opened, copied string
     	f := newFixtureWith(t, Deps{
     		OpenURL: func(u string) error { opened = u; return nil },
     		Copy:    func(s string) error { copied = s; return nil },
     	})
     	e := testutil.Timed("m", "Sync call", time.Date(2026, 11, 12, 16, 0, 0, 0, f.ny), 30*time.Minute)
     	e.MeetLink = "https://meet.google.com/abc-defg-hij"
     	f.put(e)
     	f.openDetail(e)

     	f.keys("y")
     	f.keys("m")

     	if copied != e.MeetLink || opened != e.MeetLink {
     		t.Errorf("copied %q, opened %q", copied, opened)
     	}
     }

     func TestNoMeetButtonsWithoutAMeetLink(t *testing.T) {
     	f := newMouseFixture(t)
     	f.openDetail(testutil.Timed("lunch", "Lunch with Sam", time.Date(2026, 11, 12, 12, 0, 0, 0, f.ny), time.Hour))
     	if s := f.m.View(); strings.Contains(s, "[Join]") || strings.Contains(s, "[Copy]") {
     		t.Errorf("Meet buttons without a Meet link:\n%s", s)
     	}
     }
     ```
   
   **Red:** `MeetLink`/`Deps.Copy` don't exist.
2. `model`: `MeetLink string \`json:"meet_link,omitempty"\` // Google Meet URL (read-only)`. `gcal.FromAPI`:
   ```go
   	e.MeetLink = ev.HangoutLink
   	if e.MeetLink == "" && ev.ConferenceData != nil {
   		for _, ep := range ev.ConferenceData.EntryPoints {
   			if ep.EntryPointType == "video" {
   				e.MeetLink = ep.Uri
   				break
   			}
   		}
   	}
   ```
3. `output`: a `row("Meet", e.MeetLink)` after Where. `EventJSON.MeetLink string \`json:"meet_link,omitempty"\``.
4. `internal/clipboard/clipboard.go`:
   ```go
   // Package clipboard copies text through the terminal (OSC 52): it works over
   // SSH and in most terminals, but not in macOS Terminal.app.
   package clipboard

   import (
   	"os"
   	"strings"

   	"github.com/aymanbagabas/go-osc52/v2"
   )

   // Copy asks the terminal to put text on the clipboard. Inside tmux or screen
   // the sequence is wrapped so it reaches the outer terminal.
   func Copy(text string) error {
   	seq := osc52.New(text)
   	switch {
   	case os.Getenv("TMUX") != "":
   		seq = seq.Tmux()
   	case strings.HasPrefix(os.Getenv("TERM"), "screen"):
   		seq = seq.Screen()
   	}
   	_, err := seq.WriteTo(os.Stderr) // the TUI owns stdout; stderr reaches the same terminal
   	return err
   }
   ```
   (Check go-osc52 v2's API names, `New`/`Tmux`/`Screen`/`WriteTo`, on pkg.go.dev for the version in `go.sum`.)
5. TUI:
   - `Deps.Copy func(string) error` (`tuiDeps`: `clipboard.Copy`; add it to `TestTUIDepsCarryConfig`).
   - In the detail pop-up, when `m.detail.MeetLink != ""`, add `button{"Join", "m"}` and `button{"Copy", "y"}` (and
     `"m join · y copy link"` in the hint).
   - `detailKey`: `m` → `OpenURL(MeetLink)`; `y` → `Copy(MeetLink)`. Both use `infoMsg` ("Meet link copied" / "opened in your
     browser" / the error), as in Task 14.21.
6. `go test -race ./...`: pass.

**Commit:** `feat: Meet links in details, with join and copy`

---

## Task 14.23: Checks in a real terminal and against real Google

Record every result in `DEVIATIONS.md`. Fix anything that fails **and** its test or fake, as before.

**Real terminal** (with `mouse = true` where relevant):
- [ ] Help says "Option-drag on macOS, Shift-drag elsewhere", and Option-drag selects text on the user's Mac.
- [ ] `J`/`K` move 4 weeks in the grid and page in the list.
- [ ] Past days look dimmed (and today and future days don't).
- [ ] A trackpad swipe moves a few days, not weeks. A single wheel notch moves one day.
- [ ] Grabbing an edge and releasing in place does nothing. On an invitation, the status bar says why.
- [ ] `Ctrl-J`/`Ctrl-K` move between form fields. **Check that `Ctrl-J` isn't read as Enter** (it would save the form).
- [ ] The list fills the screen, scrolls past both ends, and `Esc` returns to the grid.
- [ ] Event mode: `Enter`, `j`/`k` through a day with "+N more", `Enter`/`e`/`d`/`r`, `Esc`.
- [ ] Typed dates: `20/11`, `20/11 15:30` in quick add and the form. Go-to with `dec`, `15`, `20/12`, and a refused `14:00`.
- [ ] The calendar menu (`c` and `[Calendars]`): toggling hides and shows events at once. `clindar agenda` (no `-c`) follows it.
- [ ] Quick add: all six examples from the brief. Check the live preview for each, then the result.
- [ ] The full form: the defaults, typing a time making it timed, the pickers (keys and mouse), and an error under its field.
- [ ] Click empty space to add one day; drag across three days to add a span.
- [ ] Event colors show in the grid and the list.
- [ ] [Open in Google] opens the event's page. [Join] opens Meet. [Copy] works in iTerm2 (or kitty/WezTerm), in **tmux**
      (with `set -g set-clipboard on`), and **not** in macOS Terminal.app. That last one is expected; note it.

**Real Google** (on the "clindar-test" calendar; add these rows to Task 8.7's table):
- [ ] Hiding a calendar in clindar does **not** change Google's "selected" checkbox in the web UI.
- [ ] Set an event's color in clindar; the web UI shows it. Set it back to "Calendar default"; the web UI clears it
      (Task 14.19's `NullFields`).
- [ ] An event with Google Meet (created in the web UI) shows its link. Editing its title in clindar keeps the Meet link.
- [ ] A recurring event made with quick add (`Standup weekdays 9:00 15m`) matches in the web UI.

**Commit:** `docs: UX round 1 checks`
