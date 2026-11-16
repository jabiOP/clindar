# Phase 15: After UX round 1

Two parts:

- **Tasks 15.1–15.4: done.** Fixes and decisions from using Phase 14, each **one commit on the implementation
  branch** (`f3c8a11`, `2018d48`, `7105aca`, `9ee8872`; merged to `main` at `9ee8872`). They are shown as exact diffs
  (tests first in each file pair). If you're building from this plan, apply them in order after Phase 14. Earlier
  tasks carry a "⚠ Changed in Task 15.N" note where they're affected.
- **Tasks 15.5–15.11: to do.** Moving an event to another calendar (a user decision). These are **written as a plan
  only, not compiled or run**. Names match the code at `9ee8872`. If the compiler disagrees, follow the intent, fix
  it, and log it in `DEVIATIONS.md`. Every task states its **red step**.

**Rules that come out of this phase** (also in `README.md` and design §3/§6d):

- **When `FromAPI` starts reading a new field, append a migration that resets the sync tokens** (like migration 4).
  Incremental sync only resends events that changed, so without it every event cached earlier lacks the field until
  it happens to change on Google. A running daemon keeps its old binary: restart it after upgrading.
- **In the form, Enter always saves**, except inside an open list, where it chooses. Space opens a list. `Ctrl-S`
  and [Save] save from anywhere (with a list open they first take the highlighted option).
- **Esc on a changed form asks "Discard changes? (y/n)".** An untouched form closes at once; an open list closes
  first.
- **The mouse wheel is off.** Even throttled (Task 14.4), trackpad swipes made it unusable.

| Task | What |
|---|---|
| 15.1 | Migration 4: one full resync, so cached events gain colors and Meet links |
| 15.2 | Form: Enter always saves; Space opens lists; `Ctrl-S` and [Save] save from anywhere |
| 15.3 | The mouse wheel is off (reverts Task 14.4) |
| 15.4 | Esc on a changed form asks before discarding |
| 15.5 | Google: `events.move` in the client, `Remote` and the fake |
| 15.6 | Store: the `move` op (migration 5), coalescing, moving rows |
| 15.7 | Sync: pushing a move; pull doesn't undo it |
| 15.8 | Service: `Changes.CalendarID` and the move rule |
| 15.9 | CLI: `clindar edit ID -c NAME` |
| 15.10 | TUI: the Calendar picker unlocks when a move is allowed |
| 15.11 | Checks in a real terminal and against real Google |

---

## Task 15.1: Refetch everything once (after Tasks 14.19, 14.22)

**Bug (user report):** Meet links and event colors didn't show. The events were cached before Tasks 14.19/14.22
taught `FromAPI` to read `colorId` and Meet links, and incremental sync only resends events that changed, so
unchanged events never got the new fields.
**Fix:** migration 4 resets every sync token once; the next sync is a full one. The test is white-box (package
`store`): it rewinds `user_version` to 3 to stand for a database from before the upgrade.

**Files:** `internal/store/store.go`, `internal/store/migrate_internal_test.go` (new)

```diff
--- /dev/null
+++ b/internal/store/migrate_internal_test.go
@@ -0,0 +1,48 @@
+package store
+
+import (
+	"context"
+	"path/filepath"
+	"testing"
+
+	"github.com/jabiOP/clindar/internal/model"
+)
+
+// Events cached before clindar read colors and Meet links (Tasks 14.19, 14.22)
+// lack those fields, and incremental sync only resends events that changed. So
+// the upgrade resets the sync tokens once: the next sync refetches everything.
+func TestUpgradeForcesAFullResync(t *testing.T) {
+	ctx := context.Background()
+	path := filepath.Join(t.TempDir(), "x.db")
+	s, err := Open(path)
+	if err != nil {
+		t.Fatal(err)
+	}
+	s.UpsertCalendar(ctx, model.Calendar{AccountID: "a", ID: "c", Name: "C"})
+	s.SetCalendarSyncToken(ctx, "a", "c", "events-token")
+	s.SetCalendarsToken(ctx, "a", "list-token")
+	if _, err := s.db.ExecContext(ctx, "PRAGMA user_version = 3"); err != nil { // a database from before the upgrade
+		t.Fatal(err)
+	}
+	s.Close()
+
+	s, err = Open(path)
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer s.Close()
+
+	if tok, _ := s.CalendarSyncToken(ctx, "a", "c"); tok != "" {
+		t.Errorf("calendar token = %q, want empty (full resync)", tok)
+	}
+	if tok, _ := s.CalendarsToken(ctx, "a"); tok != "" {
+		t.Errorf("calendar list token = %q, want empty", tok)
+	}
+	s.SetCalendarSyncToken(ctx, "a", "c", "new-token") // only once: later opens keep tokens
+	s.Close()
+	s2, _ := Open(path)
+	defer s2.Close()
+	if tok, _ := s2.CalendarSyncToken(ctx, "a", "c"); tok != "new-token" {
+		t.Errorf("after a second open: token = %q, want it kept", tok)
+	}
+}
--- a/internal/store/store.go
+++ b/internal/store/store.go
@@ -170,4 +170,11 @@ CREATE TABLE event_colors (
 	background TEXT NOT NULL,
 	PRIMARY KEY (account_id, color_id)
 );
+`,
+	// 4: refetch everything once. Events cached before clindar read colorId and
+	// Meet links lack them, and incremental sync only resends changed events.
+	// Append a migration like this whenever FromAPI starts reading a new field.
+	`
+UPDATE calendars SET sync_token = '';
+UPDATE sync_state SET calendars_token = '';
 `}
```

**Done when:** `go test ./internal/store` passes. After upgrading, restart a running daemon (`clindar daemon
install` again, or stop and start it): an old binary keeps syncing with old code.

**Commit:** `fix(store): refetch everything once so cached events gain colors and Meet links`

---

## Task 15.2: Enter always saves the form (Task 14.17)

**Bug (user report):** Enter did two jobs. On a picker field it opened the list instead of saving, and [Save] sent
Enter, so with a list open it chose an option instead of saving.
**Fix:** Enter always saves, except inside an open list, where it chooses. Space opens a list. `Ctrl-S` saves from
anywhere; with a list open it first takes the highlighted option (`chooseHighlighted`). [Save] sends `Ctrl-S`. The
hint lines follow the state (list open or not). `TestRepeatPickerMakesASeries` now opens the list with Space.

**Files:** `internal/tui/actions.go`, `internal/tui/form.go`, `internal/tui/form_test.go`, `internal/tui/mouse.go`

```diff
--- a/internal/tui/actions.go
+++ b/internal/tui/actions.go
@@ -63,7 +63,10 @@ func (m Model) formKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
 	case k.Type == tea.KeyEsc && !m.form.pickerOpen(): // an open list closes first
 		m.mode = modeNormal
 		return m, nil
-	case k.Type == tea.KeyEnter && !m.form.isPicker(m.form.focus): // on a picker, Enter opens/chooses
+	case k.Type == tea.KeyCtrlS: // save from anywhere; an open list's highlighted option is chosen first
+		m.form = m.form.chooseHighlighted()
+		return m.submitForm()
+	case k.Type == tea.KeyEnter && !m.form.pickerOpen(): // inside an open list, Enter chooses instead
 		return m.submitForm()
 	}
 	var cmd tea.Cmd
--- a/internal/tui/form.go
+++ b/internal/tui/form.go
@@ -232,7 +232,8 @@ func (f form) isPicker(i int) bool { _, ok := f.pickers[i]; return ok }
 func (f form) pickerOpen() bool { p, ok := f.pickers[f.focus]; return ok && p.open }
 
 // pickerKeys are the keys a closed, focused picker takes; the rest navigate.
-var pickerKeys = map[string]bool{"h": true, "l": true, "left": true, "right": true, "ctrl+n": true, "ctrl+p": true, " ": true, "enter": true}
+// Enter isn't one of them: in the form, Enter always saves (Space opens a list).
+var pickerKeys = map[string]bool{"h": true, "l": true, "left": true, "right": true, "ctrl+n": true, "ctrl+p": true, " ": true}
 
 func (f form) Update(msg tea.Msg) (form, tea.Cmd) {
 	k, isKey := msg.(tea.KeyMsg)
@@ -453,11 +454,16 @@ func (f form) view(width int, mouse bool) modal {
 	}
 	md.text("")
 	if mouse {
-		md.buttons(button{"Save", "enter"}, button{"Cancel", "esc"})
+		md.buttons(button{"Save", "ctrl+s"}, button{"Cancel", "esc"}) // ctrl+s saves even with a list open
 	}
 	// Two lines: one would be wider than an 80-column terminal.
-	md.text(dimStyle.Render("ctrl+j/tab next · ctrl+k previous · h/l change"))
-	md.text(dimStyle.Render("space list · enter save · esc cancel"))
+	if p, ok := f.pickers[f.focus]; ok && p.open {
+		md.text(dimStyle.Render("in the list: j/k move · enter choose · esc close the list"))
+		md.text(dimStyle.Render("ctrl+s save"))
+	} else {
+		md.text(dimStyle.Render("ctrl+j/tab next · ctrl+k previous · h/l change · space list"))
+		md.text(dimStyle.Render("enter or ctrl+s save · esc cancel"))
+	}
 	return md
 }
 
@@ -487,3 +493,13 @@ func (f form) withPalette(palette map[string]string) form {
 	}
 	return f.withPicker(fColor, p)
 }
+
+// chooseHighlighted closes an open list on its highlighted option, as Enter in
+// the list would: saving with a list open keeps what the user was pointing at.
+func (f form) chooseHighlighted() form {
+	if !f.pickerOpen() {
+		return f
+	}
+	np, _, _ := f.pickers[f.focus].Update(tea.KeyMsg{Type: tea.KeyEnter})
+	return f.withPicker(f.focus, np)
+}
--- a/internal/tui/form_test.go
+++ b/internal/tui/form_test.go
@@ -67,8 +67,8 @@ func TestRepeatPickerMakesASeries(t *testing.T) {
 	f.keys("Yoga")
 	f.focus(fRepeat)
 	f.keys("l", "l", "l") // none → daily → weekdays → weekly
-	f.keys("enter")       // on a picker: opens the dropdown…
-	f.keys("enter")       // …and chooses "weekly" again; then save from a text field:
+	f.keys(" ")           // space opens the list…
+	f.keys("enter")       // …and enter chooses "weekly" again (inside a list, enter chooses); then save:
 	f.focus(fTitle)
 	f.keys("enter")
 
@@ -174,3 +174,41 @@ func TestColorPickerSetsAndClearsTheColor(t *testing.T) {
 		}
 	}
 }
+
+// Enter saves the form even when the cursor is on a (closed) picker; Space opens it.
+func TestEnterOnAPickerSaves(t *testing.T) {
+	f := newFixture(t)
+	f.openFullForm()
+	f.keys("Yoga")
+	f.focus(fRepeat)
+	f.keys("l", "enter") // none → daily, then save
+
+	if m := f.model(); m.mode != modeNormal {
+		t.Fatalf("mode = %v, want the form saved and closed", m.mode)
+	}
+	if got := f.titlesOn(13); !slices.Contains(got, "Yoga") {
+		t.Errorf("Nov 13 = %v, want a daily Yoga", got)
+	}
+}
+
+// [Save] (ctrl+s) saves even while a picker's list is open, keeping the highlighted choice.
+func TestSaveButtonWithAListOpen(t *testing.T) {
+	f := newMouseFixture(t)
+	f.click(f.button("a"))
+	f.click(f.button("tab"))
+	f.keys("Yoga")
+	f.focus(fRepeat)
+	f.keys(" ", "j") // open the list, highlight "daily"
+	if !f.model().form.pickerOpen() {
+		t.Fatal("space should open the Repeat list")
+	}
+
+	f.click(f.button("ctrl+s"))
+
+	if m := f.model(); m.mode != modeNormal {
+		t.Fatalf("mode = %v, want saved", m.mode)
+	}
+	if got := f.titlesOn(13); !slices.Contains(got, "Yoga") {
+		t.Errorf("Nov 13 = %v, want a daily Yoga", got)
+	}
+}
--- a/internal/tui/mouse.go
+++ b/internal/tui/mouse.go
@@ -201,6 +201,8 @@ func keyMsg(k string) tea.KeyMsg {
 		return tea.KeyMsg{Type: tea.KeyEsc}
 	case "tab":
 		return tea.KeyMsg{Type: tea.KeyTab}
+	case "ctrl+s":
+		return tea.KeyMsg{Type: tea.KeyCtrlS}
 	}
 	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
 }
```

**Commit:** `fix(tui): enter always saves the form; space opens lists; ctrl+s and [Save] save from anywhere`

---

## Task 15.3: The mouse wheel is off (reverts Task 14.4)

**User decision:** even at one day per step and one step per 100 ms, trackpad swipes made the wheel unusable. Wheel
events are now ignored in the grid and the list. `wheel()`, `wheelEvery` and `Model.lastWheel` are gone.
`TestWheelDoesNothing` replaces `TestWheelMovesOneDayAndIsThrottled`. (The `wheelDown` test helper stays: the new
test uses it.)

**Files:** `internal/tui/model.go`, `internal/tui/mouse.go`, `internal/tui/mouse_test.go`

```diff
--- a/internal/tui/model.go
+++ b/internal/tui/model.go
@@ -30,7 +30,7 @@ type Deps struct {
 	Dates           timeparse.Parser   // config date_order: how typed dates are read
 	OpenURL         func(string) error // opens a link in the browser (Task 14.21); nil in most tests
 	Copy            func(string) error // puts text on the clipboard (Task 14.22); nil in most tests
-	Mouse           bool               // config mouse: clicks, wheel, drag-and-drop (Phase 12)
+	Mouse           bool               // config mouse: clicks, drag-and-drop, hover (Phase 12; the wheel is off)
 	SyncInterval    time.Duration      // 0 disables periodic sync (tests)
 }
 
@@ -81,7 +81,6 @@ type Model struct {
 	drag      dragState   // mouse drag in the grid (Phase 12)
 	span      spanDrag    // mouse drag across empty days (Task 14.18)
 	hover     edgeRef     // the handle under the pointer (Phase 12.7)
-	lastWheel time.Time   // last wheel step taken (throttle, Task 14.4)
 
 	dayMode   bool // event mode: j/k pick an event on the selected grid day (Task 14.8)
 	dayCursor int  // event mode: index into dayEvents()
@@ -326,7 +325,7 @@ func ago(d time.Duration) string {
 func Run(ctx context.Context, d Deps) error {
 	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithContext(ctx)}
 	if d.Mouse {
-		// All motion: clicks, wheel, dragging, and hover (reveals resize handles).
+		// All motion: clicks, dragging, and hover (reveals resize handles).
 		opts = append(opts, tea.WithMouseAllMotion())
 	}
 	_, err := tea.NewProgram(New(ctx, d), opts...).Run()
--- a/internal/tui/mouse.go
+++ b/internal/tui/mouse.go
@@ -35,10 +35,8 @@ func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
 	_, hits := m.layout()
 	t, onSomething := hits.at(msg.X, msg.Y)
 	switch {
-	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
-		return m.wheel(-1)
-	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
-		return m.wheel(1)
+	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
+		return m, nil // the wheel is off: trackpad swipes made it unusable, even throttled
 	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
 		return m.press(t, onSomething)
 	case msg.Action == tea.MouseActionMotion && m.span.active: // before hover, which takes every other motion
@@ -73,25 +71,6 @@ func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
 	return m, nil
 }
 
-const wheelEvery = 100 * time.Millisecond // trackpads send dozens of events per swipe
-
-func (m Model) wheel(dir int) (tea.Model, tea.Cmd) {
-	if m.mode != modeNormal {
-		return m, nil
-	}
-	now := m.deps.Now()
-	if now.Sub(m.lastWheel) < wheelEvery {
-		return m, nil
-	}
-	m.lastWheel = now
-	if m.view == viewList {
-		m.cursor = min(max(m.cursor+dir, 0), max(len(m.entries())-1, 0))
-		return m.extendListIfNeeded()
-	}
-	m = m.moveDays(dir, false)
-	return m, m.reloadIfNeeded()
-}
-
 func (m Model) press(t target, onSomething bool) (tea.Model, tea.Cmd) {
 	if m.mode != modeNormal {
 		switch {
--- a/internal/tui/mouse_test.go
+++ b/internal/tui/mouse_test.go
@@ -97,20 +97,20 @@ func (f *fixture) wheelDown() {
 	f.send(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
 }
 
-func TestWheelMovesOneDayAndIsThrottled(t *testing.T) {
+// The wheel is off: trackpad swipes made it unusable even throttled (user, after Task 14.4).
+func TestWheelDoesNothing(t *testing.T) {
 	f := newMouseFixture(t)
-	sel := f.model().selected
+	before := f.model()
 
-	f.wheelDown()
-	f.wheelDown() // same instant: dropped
-	if got := f.model().selected; !got.Equal(sel.AddDate(0, 0, 1)) {
-		t.Fatalf("selected = %v, want one day later", got)
+	for range 5 {
+		f.wheelDown()
+		f.clock = f.clock.Add(time.Second)
 	}
-	f.clock = f.clock.Add(150 * time.Millisecond)
+	f.keys("tab") // the list too
 	f.wheelDown()
 
-	if got := f.model().selected; !got.Equal(sel.AddDate(0, 0, 2)) {
-		t.Errorf("selected = %v, want two days later", got)
+	if m := f.model(); !m.selected.Equal(before.selected) || !m.gridStart.Equal(before.gridStart) || m.cursor != 0 {
+		t.Errorf("selected %v gridStart %v cursor %d: the wheel must not move anything", m.selected, m.gridStart, m.cursor)
 	}
 }
 
```

**Commit:** `feat(tui): turn the mouse wheel off`

---

## Task 15.4: Esc asks before discarding a changed form

**User decision.** Esc used to discard the form at once, which isn't what a vim user expects. Of the options
(vim-style normal/insert modes, confirm-discard, Esc saves) the user chose **confirm-discard**:

- Esc on an untouched form closes it.
- With any change it shows **"Discard changes? (y/n)"** in the hint area. `y` closes without saving; any other key
  keeps editing and isn't typed into a field.
- An open list still closes first. [Cancel] sends Esc, so it behaves the same.
- A form opened from quick add (Tab) counts as changed: its values differ from an empty form.

`form.dirty()` reuses `changed(i)`, the same comparison an edit uses to build `Changes`.

**Files:** `internal/tui/actions.go`, `internal/tui/form.go`, `internal/tui/form_test.go`

```diff
--- a/internal/tui/actions.go
+++ b/internal/tui/actions.go
@@ -59,8 +59,19 @@ func (m Model) scopeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
 }
 
 func (m Model) formKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
+	if m.form.confirmDiscard { // "Discard changes? (y/n)": y closes, anything else keeps editing
+		m.form.confirmDiscard = false
+		if k.String() == "y" {
+			m.mode = modeNormal
+		}
+		return m, nil
+	}
 	switch {
 	case k.Type == tea.KeyEsc && !m.form.pickerOpen(): // an open list closes first
+		if m.form.dirty() {
+			m.form.confirmDiscard = true
+			return m, nil
+		}
 		m.mode = modeNormal
 		return m, nil
 	case k.Type == tea.KeyCtrlS: // save from anywhere; an open list's highlighted option is chosen first
--- a/internal/tui/form.go
+++ b/internal/tui/form.go
@@ -47,15 +47,16 @@ var repeatWords = []string{"none", "daily", "weekdays", "weekly", "monthly", "ye
 // form edits a new or existing event. It knows nothing about the service:
 // it produces a Draft or Changes and the Model decides what to do with them.
 type form struct {
-	inputs  [fieldCount]textinput.Model // text fields (picker fields' inputs are unused)
-	pickers map[int]picker              // picker fields: fAllDay, fCalendar, fRepeat; copied on write
-	locked  map[int]string              // pickers that can't change on edit, with the reason shown
-	initial [fieldCount]string          // to detect what an edit changed (pickers: the chosen value)
-	focus   int
-	editing *model.Event // nil when creating
-	errs    map[int]string
-	err     string // an error that belongs to no field
-	dates   timeparse.Parser
+	inputs         [fieldCount]textinput.Model // text fields (picker fields' inputs are unused)
+	pickers        map[int]picker              // picker fields: fAllDay, fCalendar, fRepeat; copied on write
+	locked         map[int]string              // pickers that can't change on edit, with the reason shown
+	initial        [fieldCount]string          // to detect what an edit changed (pickers: the chosen value)
+	focus          int
+	editing        *model.Event // nil when creating
+	errs           map[int]string
+	err            string // an error that belongs to no field
+	dates          timeparse.Parser
+	confirmDiscard bool // Esc with unsaved changes asked "Discard changes? (y/n)"
 }
 
 // fieldError is an error that belongs to one field.
@@ -457,7 +458,9 @@ func (f form) view(width int, mouse bool) modal {
 		md.buttons(button{"Save", "ctrl+s"}, button{"Cancel", "esc"}) // ctrl+s saves even with a list open
 	}
 	// Two lines: one would be wider than an 80-column terminal.
-	if p, ok := f.pickers[f.focus]; ok && p.open {
+	if f.confirmDiscard {
+		md.text(errorStyle.Render("Discard changes? (y/n)"))
+	} else if p, ok := f.pickers[f.focus]; ok && p.open {
 		md.text(dimStyle.Render("in the list: j/k move · enter choose · esc close the list"))
 		md.text(dimStyle.Render("ctrl+s save"))
 	} else {
@@ -503,3 +506,13 @@ func (f form) chooseHighlighted() form {
 	np, _, _ := f.pickers[f.focus].Update(tea.KeyMsg{Type: tea.KeyEnter})
 	return f.withPicker(f.focus, np)
 }
+
+// dirty reports whether anything differs from how the form opened.
+func (f form) dirty() bool {
+	for i := range fieldCount {
+		if f.changed(i) {
+			return true
+		}
+	}
+	return false
+}
--- a/internal/tui/form_test.go
+++ b/internal/tui/form_test.go
@@ -212,3 +212,33 @@ func TestSaveButtonWithAListOpen(t *testing.T) {
 		t.Errorf("Nov 13 = %v, want a daily Yoga", got)
 	}
 }
+
+func TestEscOnAnUntouchedFormCloses(t *testing.T) {
+	f := newFixture(t)
+	f.keys("enter", "j", "e") // edit Lunch, change nothing
+	f.keys("esc")
+	if m := f.model(); m.mode != modeNormal {
+		t.Errorf("mode = %v, want closed", m.mode)
+	}
+}
+
+func TestEscWithChangesAsksBeforeDiscarding(t *testing.T) {
+	f := newFixture(t)
+	f.openFullForm()
+	f.keys("Dentist", "esc")
+	if m := f.model(); m.mode != modeForm || !strings.Contains(f.m.View(), "Discard changes? (y/n)") {
+		t.Fatalf("mode = %v; want the form, asking:\n%s", m.mode, f.m.View())
+	}
+
+	f.keys("n") // keep editing: nothing lost, and "n" isn't typed into the title
+	if m := f.model(); m.mode != modeForm || m.form.value(fTitle) != "Dentist" {
+		t.Fatalf("after n: mode = %v, title = %q", m.mode, m.form.value(fTitle))
+	}
+	f.keys("esc", "y")
+	if m := f.model(); m.mode != modeNormal {
+		t.Errorf("after esc y: mode = %v, want closed", m.mode)
+	}
+	if got := f.titlesOn(12); slices.Contains(got, "Dentist") {
+		t.Error("discarding must not save")
+	}
+}
```

**Commit:** `feat(tui): esc asks before discarding a changed form`

---

# Moving an event to another calendar (Tasks 15.5–15.11)

> ⚠ **Tasks 15.5–15.11 are written as a plan only; they were not compiled or run.** Treat the code as a precise
> spec. If the compiler disagrees, follow the intent and note the fix in `DEVIATIONS.md`.

**User decision.** You can change the Calendar of an existing event when **all** of these hold:

- you own it: it is **not an invitation** (the same "can edit" rule as Task 12.8: no self attendee who isn't the
  organizer);
- **both calendars are writable**;
- it's a **one-off event or a whole series**.

**A single occurrence can't move on its own: Google doesn't allow it** (`events.move` refuses instances). The form
says so and offers to move the whole series, through the scope picker with only "all". An orphan instance (Task 4.1:
shown as a one-off because its series isn't in our calendar) is still an occurrence on Google, so it can't move either.

**How Google does it.** `POST calendars/{from}/events/{id}/move?destination={to}` (`events.move`). The event keeps
its ID and comes back with a new etag. A series moves with its exceptions. Afterwards, `events.list` on the old
calendar returns the event (and each exception) as **cancelled**, and on the new calendar as **new**. Moving changes
the event's organizer to the destination calendar; it takes no `If-Match`. We send `sendUpdates=none`: nothing about
the meeting changes for guests (Task 15.11 checks that they aren't re-invited).

**Design in one paragraph.** A move is its own outbox op kind, `move`, addressed like every op to the calendar the
request goes to (the **source**), with a new `destination` column. Locally the move happens at once: the event's rows
(master and exceptions) change calendar in the same transaction that queues the op. Because ops are keyed by
(account, calendar, event), an edit made *before* the move stays a separate op on the source calendar and is pushed
first; an edit made *after* is keyed by the destination and pushed after. Until the move lands, pull must neither
bring Google's copy back into the source calendar nor wipe the moved rows from the destination.

**Known limit (accept it, log it in `DEVIATIONS.md` if you see it):** when a whole series moves, Google gives each
exception a new etag too. An edit to *one occurrence* queued while the series move is still pending carries the old
etag, so it comes back 412 and is discarded with the usual log line. The next pull refreshes the etags. The master's
own pending edits are retargeted (Task 15.7), so whole-event edits are safe.

---

## Task 15.5: Google: `events.move`

**Goal:** `MoveEvent` in `gcal.Client`, in the `syncer.Remote` interface, and in the fake, which models what pull sees
afterwards.

**Files:** `internal/gcal/client.go`, `internal/gcal/client_test.go`, `internal/syncer/engine.go`,
`internal/gcal/gcaltest/fake.go`, `internal/gcal/gcaltest/fake_test.go`

1. Client test (`client_test.go`), with the existing `newClient` helper:
   ```go
   func TestMoveEventPostsToMove(t *testing.T) {
   	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
   		q := r.URL.Query()
   		if r.Method != http.MethodPost || r.URL.Path != "/calendars/from/events/e1/move" ||
   			q.Get("destination") != "to" || q.Get("sendUpdates") != "none" {
   			http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), 500)
   			return
   		}
   		fmt.Fprint(w, `{"id":"e1","etag":"\"2\"","summary":"Dentist","start":{"date":"2026-11-14"},"end":{"date":"2026-11-15"}}`)
   	})

   	got, err := c.MoveEvent(context.Background(), "from", "e1", "to", "none")

   	if err != nil {
   		t.Fatal(err)
   	}
   	if got.ID != "e1" || got.CalendarID != "to" || got.ETag != `"2"` {
   		t.Errorf("got %+v, want e1 in calendar to with the new etag", got)
   	}
   }
   ```
   **Red:** doesn't compile (`MoveEvent` undefined). That is the expected failure here; the behavior is checked once it compiles.
2. `client.go`:
   ```go
   // MoveEvent moves an event (or a whole series) to another calendar. Google
   // keeps the ID and returns the event with a new etag. It takes no If-Match.
   func (c *Client) MoveEvent(ctx context.Context, calendarID, eventID, destination, sendUpdates string) (model.Event, error) {
   	ev, err := c.svc.Events.Move(calendarID, eventID, destination).SendUpdates(sendUpdates).Context(ctx).Do()
   	if err != nil {
   		return model.Event{}, mapErr(err)
   	}
   	return FromAPI(c.account, destination, ev)
   }
   ```
3. `engine.go`: add to `Remote`, after `DeleteEvent`:
   ```go
   	MoveEvent(ctx context.Context, calendarID, eventID, destination, sendUpdates string) (model.Event, error)
   ```
4. Fake tests (`fake_test.go`). `newFake()` has one calendar, `c`; these need a second one:
   ```go
   func TestFakeMoveKeepsIDAndLeavesTombstones(t *testing.T) {
   	ctx := context.Background()
   	f := gcaltest.NewFake(model.Calendar{ID: "a", Name: "A"}, model.Calendar{ID: "b", Name: "B"})
   	master := f.Put(model.Event{CalendarID: "a", ID: "s1", Title: "Yoga", Start: at(2), End: at(2).Add(time.Hour),
   		Recurrence: []string{"RRULE:FREQ=DAILY;COUNT=5"}})
   	f.PatchEvent(ctx, "a", model.Event{ID: model.InstanceID("s1", at(3), false), Title: "Yoga (late)",
   		Start: at(3).Add(time.Hour), End: at(3).Add(2 * time.Hour)}, "", "none") // an exception
   	_, tokenA, _ := f.ListEvents(ctx, "a", "")

   	moved, err := f.MoveEvent(ctx, "a", "s1", "b", "none")

   	if err != nil {
   		t.Fatal(err)
   	}
   	if moved.ID != "s1" || moved.CalendarID != "b" || moved.ETag == master.ETag {
   		t.Errorf("moved = %+v: want the same ID, calendar b, a new etag", moved)
   	}
   	inA, _, _ := f.ListEvents(ctx, "a", tokenA)
   	if len(inA) != 2 || !inA[0].Cancelled() || !inA[1].Cancelled() {
   		t.Errorf("calendar a's changes = %+v, want the master and the exception cancelled", inA)
   	}
   	inB, _, _ := f.ListEvents(ctx, "b", "")
   	if len(inB) != 2 {
   		t.Errorf("calendar b = %+v, want the master and its exception", inB)
   	}
   }

   func TestFakeRefusesToMoveAnOccurrence(t *testing.T) {
   	f := gcaltest.NewFake(model.Calendar{ID: "a", Name: "A"}, model.Calendar{ID: "b", Name: "B"})
   	f.Put(model.Event{CalendarID: "a", ID: "s1", Title: "Yoga", Start: at(2), End: at(2).Add(time.Hour),
   		Recurrence: []string{"RRULE:FREQ=DAILY;COUNT=5"}})

   	_, err := f.MoveEvent(context.Background(), "a", model.InstanceID("s1", at(3), false), "b", "none")

   	if !errors.Is(err, gcal.ErrPermanent) {
   		t.Errorf("err = %v, want ErrPermanent (Google refuses to move one occurrence)", err)
   	}
   }
   ```
   **Red:** `MoveEvent` undefined on `*Fake`; once it exists as a stub returning `gcal.ErrNotFound`, both fail on
   their assertions.
5. `fake.go`:
   ```go
   // MoveEvent moves an event, or a series with its exceptions, as Google does:
   // same IDs and new etags in the destination; cancelled tombstones in the
   // source, which is what a later events.list there returns. Occurrences
   // (instance IDs, or stored exceptions) can't move on their own.
   func (f *Fake) MoveEvent(ctx context.Context, calendarID, eventID, destination, sendUpdates string) (model.Event, error) {
   	if err := f.beforeWrite(); err != nil {
   		return model.Event{}, err
   	}
   	f.mu.Lock()
   	defer f.mu.Unlock()
   	cur, ok := f.events[calendarID][eventID]
   	if _, _, _, isInstance := model.ParseInstanceID(eventID); isInstance || cur.SeriesID != "" {
   		return model.Event{}, fmt.Errorf("%w: cannot move one occurrence of a series", gcal.ErrPermanent)
   	}
   	if _, known := f.events[destination]; !ok || cur.Cancelled() || !known {
   		return model.Event{}, gcal.ErrNotFound
   	}
   	group := []model.Event{cur}
   	for _, ex := range f.events[calendarID] {
   		if ex.SeriesID == cur.ID {
   			group = append(group, ex)
   		}
   	}
   	var moved model.Event
   	for i, e := range group {
   		gone := e
   		gone.Status = model.StatusCancelled
   		f.put(gone)
   		e.CalendarID, e.Organizer = destination, destination
   		if got := f.put(e); i == 0 {
   			moved = got
   		}
   	}
   	return moved, nil
   }
   ```
   `put` keys by `e.CalendarID`, so the tombstone stays in the source and the copy lands in the destination.
6. `go test ./internal/gcal/... ./internal/syncer` (the syncer package must still compile against `Remote`).

**Done when:** both packages pass; `go vet ./...` is clean.

**Commit:** `feat(gcal): move an event to another calendar`

---

## Task 15.6: Store: the `move` op

**Goal:** the outbox can hold a move, coalescing knows about it, and the store can move an event's rows.

**Files:** `internal/store/store.go`, `internal/store/outbox.go`, `internal/store/events.go`,
`internal/store/ranges.go`, `internal/store/outbox_test.go`, `internal/store/events_test.go`,
`internal/store/migrate_internal_test.go`

### Migration 5

SQLite can't change a `CHECK` constraint, so the outbox table is rebuilt. Rows keep their `seq` (push order).
Renaming the new table also carries its `AUTOINCREMENT` counter over, so new ops still come after old ones.

```go
	// 5: the "move" op (Task 15.6). SQLite can't alter a CHECK, so the outbox
	// is rebuilt; rows keep their seq, which is the push order.
	`
CREATE TABLE outbox_new (
	seq          INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id   TEXT NOT NULL,
	calendar_id  TEXT NOT NULL,              -- the calendar the request goes to (a move's source)
	event_id     TEXT NOT NULL,
	op           TEXT NOT NULL CHECK (op IN ('insert', 'patch', 'rsvp', 'delete', 'move')),
	destination  TEXT NOT NULL DEFAULT '',   -- move only: the calendar the event goes to
	payload      TEXT NOT NULL DEFAULT '',   -- JSON model.Event (not for delete)
	if_match     TEXT NOT NULL DEFAULT '',
	send_updates TEXT NOT NULL DEFAULT 'none',
	version      INTEGER NOT NULL DEFAULT 1,
	attempts     INTEGER NOT NULL DEFAULT 0,
	last_error   TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL
);
INSERT INTO outbox_new (seq, account_id, calendar_id, event_id, op, payload, if_match, send_updates,
	version, attempts, last_error, created_at)
	SELECT seq, account_id, calendar_id, event_id, op, payload, if_match, send_updates,
	version, attempts, last_error, created_at FROM outbox;
DROP TABLE outbox;
ALTER TABLE outbox_new RENAME TO outbox;
CREATE INDEX outbox_by_event ON outbox (account_id, calendar_id, event_id);
CREATE INDEX outbox_by_destination ON outbox (account_id, destination, event_id);
`
```

No resync migration is needed: `FromAPI` reads nothing new.

### The op

```go
	OpMove   OpKind = "move" // events.move to Op.Destination; the payload is the event (for log messages)
```
`Op` gains `Destination string // move only: the calendar the event goes to`. `insertOp`, `queryOps` (and the
`SELECT` column list) carry it. `encodePayload` keeps a payload for moves (only deletes have none).

For a move, `Op.IfMatch` holds the etag the event had when it was moved. **Push doesn't send it** (`events.move` takes
no `If-Match`); Task 15.7 uses it only to recognize later ops based on that etag. (It records, it doesn't guard.)

### Coalescing

Add rows to `Enqueue`'s table and code:

```
//	-         move    move
//	insert    move    insert, re-addressed to the destination (payload too): it hasn't reached Google yet
//	patch     move    patch, then a separate move (never merged: the patch goes to the source calendar)
//	rsvp      move    rsvp, then a separate move
//	move      any     a separate op (a move is never merged into; later ops for the
//	                  destination are keyed by it anyway)
//	delete    move    ErrPendingDelete
```

In code, right after the `ErrPendingDelete` / duplicate-insert checks:

```go
		switch {
		case op.Kind == OpMove && existing.Kind == OpInsert:
			return tx.readdressInsert(ctx, existing.Seq, op.Destination, op.Payload)
		case op.Kind == OpMove || existing.Kind == OpMove:
			return tx.insertOp(ctx, op)
		}
```
with
```go
// readdressInsert points a pending insert at another calendar: moving an event
// that hasn't reached Google yet is just creating it somewhere else.
func (s *Store) readdressInsert(ctx context.Context, seq int64, calendarID string, payload model.Event) error {
	p, err := encodePayload(OpInsert, payload)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, `UPDATE outbox SET calendar_id = ?, payload = ?, version = version + 1
		WHERE seq = ?`, calendarID, p, seq)
	return err
}
```

**An insert in flight when it was re-addressed.** `CompleteOp` already handles "edited while in flight" by turning
the insert into a patch. If the stored row now names a *different calendar* than the op that was pushed, the insert
landed in the old calendar. Then the stored row becomes a move from the pushed op's calendar (same `seq`, so it goes
first), and a new patch for the destination carries the latest payload:

```go
		superseded = true
		rows, err := tx.queryOps(ctx, `WHERE seq = ?`, op.Seq)
		if err != nil || len(rows) == 0 {
			return err
		}
		if op.Kind == OpInsert && rows[0].CalendarID != op.CalendarID { // re-addressed while in flight
			// A copy as a patch for the destination (new seq: after the move)…
			if _, err := tx.q.ExecContext(ctx, `INSERT INTO outbox
				(account_id, calendar_id, event_id, op, payload, if_match, send_updates, created_at)
				SELECT account_id, calendar_id, event_id, 'patch', payload, ?, send_updates, created_at
				FROM outbox WHERE seq = ?`, newETag, op.Seq); err != nil {
				return err
			}
			// …and the row itself becomes the move from where the insert landed (same seq: first).
			_, err = tx.q.ExecContext(ctx, `UPDATE outbox SET op = 'move', destination = calendar_id,
				calendar_id = ?, if_match = ? WHERE seq = ?`, op.CalendarID, newETag, op.Seq)
			return err
		}
		_, err = tx.q.ExecContext(ctx, `UPDATE outbox SET if_match = ?, … (unchanged)
```

### Who owns a local row

```go
// HasPending reports whether a local op still owns this event in this
// calendar: one addressed to it, or a move into it that hasn't landed.
func (s *Store) HasPending(ctx context.Context, accountID, calendarID, eventID string) (bool, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM outbox WHERE account_id = ? AND event_id = ?
		AND (calendar_id = ? OR destination = ?)`, accountID, eventID, calendarID, calendarID).Scan(&n)
	return n > 0, err
}

// MovePending reports whether a move of this event (or series) out of this
// calendar hasn't landed yet. Pull uses it for the series' exceptions.
func (s *Store) MovePending(ctx context.Context, accountID, calendarID, eventID string) (bool, error)
```

`ClearCalendarEvents` (full resync) keeps rows owned by a pending op the same way, **and the exceptions of a pending
series**, or a full resync of the destination would wipe a moved series before Google has it:

```go
	_, err := s.q.ExecContext(ctx, `DELETE FROM events
		WHERE account_id = ? AND calendar_id = ?
		AND id NOT IN (SELECT event_id FROM outbox WHERE account_id = ? AND (calendar_id = ? OR destination = ?))
		AND series_id NOT IN (SELECT event_id FROM outbox WHERE account_id = ? AND (calendar_id = ? OR destination = ?))`,
		accountID, calendarID, accountID, calendarID, calendarID, accountID, calendarID, calendarID)
```

### Moving rows and retargeting etags

```go
// MoveSeries moves an event, or a series master with all its exceptions, to
// another calendar. IDs stay the same, as on Google. A stale copy already in
// the destination is replaced.
func (s *Store) MoveSeries(ctx context.Context, accountID, from, to, id string) error {
	_, err := s.q.ExecContext(ctx, `UPDATE OR REPLACE events
		SET calendar_id = ?, data = json_set(data, '$.calendar_id', ?)
		WHERE account_id = ? AND calendar_id = ? AND (id = ? OR series_id = ?)`, to, to, accountID, from, id, id)
	return err
}

// RetargetETag points the pending ops for an event in a calendar at Google's
// new etag. After a move lands, edits queued behind it were based on the
// content the move kept, so the move's etag is the right one for them.
// RSVPs (no If-Match on purpose) are left alone.
func (s *Store) RetargetETag(ctx context.Context, accountID, calendarID, eventID, etag string) error
```

(`json_set` is in modernc's SQLite. If it ever isn't, read the rows, set `CalendarID`, `DeleteSeries` and
`PutEvent` them, all in the caller's transaction.)

### Tests

1. `outbox_test.go`, as table rows or small tests in the style of `TestEnqueueCoalescing` (`op()` builds e1 in the
   test calendar; add a `move(dest)` helper that sets `Kind: OpMove, Destination: dest`):
   - `TestMoveAfterInsertReaddressesTheInsert`: insert, then move to `other` → one op, an insert, `CalendarID ==
     "other"`, payload's `CalendarID == "other"`.
   - `TestMoveAfterPatchStaysSeparate`: patch, then move → two ops in that order: patch on the test calendar, move with
     `Destination == "other"`. Then a patch keyed by `other` → three ops (a different key, so it stays its own op).
   - `TestMoveAfterDeleteFails`: `ErrPendingDelete`.
   - `TestReaddressedInsertLandedInTheOldCalendar`: insert (seq 1) taken for push; meanwhile a move re-addresses it;
     `CompleteOp(original, "etag-1")` → superseded, and the ops are now: a move from the test calendar to `other`
     (seq 1, `IfMatch "etag-1"`), then a patch for `other`.
   - `TestHasPendingSeesMovesIntoACalendar`: after a move to `other`, `HasPending(other, e1)` and `HasPending(test, e1)`
     are both true; `MovePending(test, e1)` true, `MovePending(other, e1)` false.
   - `TestRetargetETagLeavesRSVPsAlone`.
2. `events_test.go`: `TestMoveSeriesTakesItsExceptions`: a master `s1`, an exception of it and an unrelated event
   in the test calendar; `MoveSeries(…, "other", "s1")` → master and exception found in `other` (by `GetEvent`, with
   `CalendarID == "other"` inside the JSON too), the unrelated event untouched. And
   `TestFullResyncKeepsAMovedSeries`: after `MoveSeries` plus a queued move, `ClearCalendarEvents(other)` keeps both
   rows.
3. `migrate_internal_test.go`: `TestMigration5KeepsQueuedOps`: open a raw `database/sql` connection to a new file,
   run `migrations[:4]` and set `user_version = 4` (a database from before), insert one outbox row with `seq = 7`,
   close; `Open` the file → `PendingOps` returns it with `seq == 7`, and enqueuing a move succeeds with a higher seq.

**Red:** compile errors first (`OpMove`, `Destination`, `MoveSeries`…); with stubs in place, the coalescing tests fail
because a move merges like a patch, and the migration test fails on the CHECK constraint.

**Done when:** `go test -race ./internal/store` passes.

**Commit:** `feat(store): a move op, and moving an event's rows to another calendar`

---

## Task 15.7: Sync: pushing a move, and pull doesn't undo it

**Goal:** push sends moves; completing or discarding one leaves exactly one local copy in the right calendar; and no
pull, incremental or full, before or after the push, brings the event back into the old calendar or loses it from
the new one.

**Files:** `internal/syncer/push.go`, `internal/syncer/pull.go`, `internal/syncer/move_test.go` (new), `internal/gcal/gcaltest/fake.go`,
`internal/syncer/world_test.go`

### Push

In `pushOne`:
```go
	case store.OpMove:
		got, err = e.Remote.MoveEvent(ctx, op.CalendarID, op.EventID, op.Destination, op.SendUpdates)
		if errors.Is(err, gcal.ErrNotFound) { // an earlier push may have landed without our hearing back
			if there, gerr := e.Remote.GetEvent(ctx, op.Destination, op.EventID); gerr == nil {
				got, err = there, nil
			}
		}
```

`complete` gains two rules. The second is general: **a later op for the same event owns the local row**, so a patch
pushed before a pending move must not write the event back into the old calendar.
```go
func (e *Engine) complete(ctx context.Context, op store.Op, got model.Event) error {
	return e.Store.InTx(ctx, func(tx *store.Store) error {
		superseded, err := tx.CompleteOp(ctx, op, got.ETag)
		if err != nil || superseded || op.Kind == store.OpDelete {
			return err // a newer local edit (or a delete) owns the local row
		}
		if op.Kind == store.OpMove { // edits queued behind the move now need its etag
			if err := tx.RetargetETag(ctx, op.AccountID, op.Destination, op.EventID, got.ETag); err != nil {
				return err
			}
		}
		if owned, err := tx.HasPending(ctx, got.AccountID, got.CalendarID, got.ID); err != nil || owned {
			return err // e.g. this patch has a move queued behind it, or this move a patch
		}
		return tx.PutEvent(ctx, got) // Google's normalized copy, with the new etag
	})
}
```

`discard` (Google refused the move, e.g. a birthday or working-location event, which `events.move` rejects): put the
rows back first, then restore as usual. Inside its transaction, before the `switch`:
```go
		if op.Kind == store.OpMove { // the rows moved locally; put them back where Google has them
			if err := tx.MoveSeries(ctx, op.AccountID, op.Destination, op.CalendarID, op.EventID); err != nil {
				return err
			}
		}
```
and the message for a move: `fmt.Sprintf("%q: couldn't move it to another calendar; it stays where it was", title(op))`.
(Edits queued behind it, keyed by the destination, then come back 404 and are dropped with their own log line.
That's acceptable: Google refused the move, and the log says so.)

### Pull

1. In `apply`, an exception of a series that is being moved out of this calendar is skipped, like the master (whose
   pending move `HasPending` already sees). Otherwise a full resync of the old calendar before the push lands would
   store the series' exceptions there, without their master: they'd show as orphan one-offs.
   ```go
	pending, err := tx.HasPending(ctx, ev.AccountID, ev.CalendarID, ev.ID)
	if err == nil && !pending && ev.SeriesID != "" {
		pending, err = tx.MovePending(ctx, ev.AccountID, ev.CalendarID, ev.SeriesID)
	}
	if err != nil || pending {
		return err
	}
   ```
2. In `pullEvents`, **apply cancelled series masters last**. After a series moves, the old calendar returns the master
   *and* each exception as cancelled. A cancelled exception is stored as a tombstone (it hides its slot), so one
   arriving after its master's `DeleteSeries` would stay behind in the old calendar, and `FindEvent` on its ID would
   then be ambiguous. Sorting doesn't change anything else: masters and exceptions are independent rows otherwise.
   ```go
	// Cancelled masters last: DeleteSeries then also removes tombstones of
	// their exceptions that came in the same listing (a series moved away).
	slices.SortStableFunc(events, func(a, b model.Event) int {
		return cmp.Compare(cancelledMaster(a), cancelledMaster(b))
	})
   ```
   with `func cancelledMaster(e model.Event) int` returning 1 for `e.Cancelled() && e.SeriesID == ""`, else 0.

### Tests (`move_test.go`, with `world`)

`newWorld` gets a second writable calendar on both sides: add `{ID: "work", Name: "Work", AccessRole: "owner"}` to
the fake and upsert it into the store (or a `w.addCalendar` helper). Then:

- `TestMoveReachesGoogleAndKeepsTheID`: create a one-off, sync, `svc.Update(id, Changes{CalendarID: ptr("Work")},
  ScopeNone)`, sync → the fake has it in `work`, cancelled in the test calendar; locally exactly one row, in `work`,
  with Google's new etag; `pendingCount() == 0`.
- `TestMoveThenEditPushesBothInOrder`: synced event; move to Work, then change the title (keyed by `work`); sync →
  Google's copy in `work` has the new title (the patch got the move's etag: no 412, nothing in `clindar log`).
- `TestEditThenMoveDoesNotDuplicate`: synced event; change the title, then move; sync → one local row, in `work`,
  with the new title; nothing left in the test calendar locally.
- `TestMoveOfANewEventIsJustAnInsert`: create offline (`google.Fail` set), move to Work, clear `Fail`, sync →
  Google never saw the test calendar (`google.Event(testCal, id)` absent), and the event is in `work`.
- `TestMovedSeriesTakesItsExceptions`: a synced daily series with one edited occurrence; move with `ScopeAll`;
  sync → both in `work` on Google and locally; the occurrence still shows its edit; nothing (not even a tombstone)
  left in the test calendar locally; `svc.Get(exceptionID)` isn't ambiguous.
- `TestFullResyncBeforeTheMoveLands`: move with `google.Fail` set (push fails); clear `Fail`, but expire tokens
  for the pull only: call `w.store.ResetSyncTokens` (what `sync --full` uses), then `w.engine.Pull` directly,
  **without** pushing → the event is still shown once, in `work` (the old calendar's copy is skipped,
  the destination's row survives `ClearCalendarEvents`). Then `w.sync()` → still once, in `work`.
- `TestRefusedMoveGoesBack`: give the fake a `MoveFail error` field, returned only by `MoveEvent` (like `FailGet`),
  set to `gcal.ErrPermanent` → after sync the event is back in the test calendar locally, the op is gone, and
  `clindar log` (`svc.Logs`) has "couldn't move it".

**Red:** with Task 15.6 done but this task's code missing, `pushOne` ignores `OpMove` (nil error, empty event):
`complete` stores an empty event and the first test fails on the fake's state. Then each rule above makes its test
pass.

**Done when:** `go test -race ./internal/syncer` passes.

**Commit:** `feat(syncer): push moves; pull keeps a moved event where it went`

---

## Task 15.8: Service: `Changes.CalendarID`

**Goal:** `Update` moves an event when `Changes.CalendarID` names another calendar, with the move rule in **one
place** that the TUI also uses.

**Files:** `internal/model/event.go`, `internal/model/event_test.go`, `internal/service/service.go`,
`internal/service/update.go`, `internal/service/move_test.go` (new), `internal/tui/drag.go`

1. `model`: the "invitation" half of the edit rule moves out of `tui.canDrag` so the service can use it:
   ```go
   // IsInvitation reports whether you're a guest: you're on the attendee list
   // but not the organizer. Guests can't change the event, only answer it.
   func (e Event) IsInvitation() bool {
   	self := e.SelfAttendee()
   	return self != nil && !self.Organizer
   }
   ```
   `tui.canDrag` uses it (`if e.IsInvitation() {`), same message. Test: `TestIsInvitation` (no attendees: false;
   self organizer: false; self guest: true).
2. `service.go` errors:
   ```go
	ErrInvitation     = errors.New("this is an invitation: only the organizer can change it")
	ErrMoveOccurrence = errors.New("one occurrence can't move to another calendar: move the whole series (scope all)")
   ```
3. `update.go`:
   ```go
   type Changes struct {
   	...
   	CalendarID *string // name or ID; moves the event (a whole series, for instances: scope all)
   }

   // CanMove says whether e may move out of its calendar (from): you own it, the
   // calendar is writable, and it's a one-off or a whole series. The destination
   // must be writable too; Update checks that. The TUI uses this to unlock its
   // Calendar picker, so the rule lives only here.
   func CanMove(e model.Event, from model.Calendar) error {
   	_, _, _, occurrence := model.ParseInstanceID(e.ID)
   	switch {
   	case !from.Writable():
   		return fmt.Errorf("%s: %w", from.Name, ErrReadOnly)
   	case e.IsInvitation():
   		return ErrInvitation
   	case occurrence && !e.IsInstance(): // an orphan occurrence, shown as a one-off (Task 4.1)
   		return ErrMoveOccurrence
   	}
   	return nil
   }
   ```
   In `Update`, after the source-calendar check and before anything else:
   ```go
	if ch.CalendarID != nil {
		to, err := s.writableCalendar(ctx, *ch.CalendarID)
		if err != nil {
			return model.Event{}, err
		}
		rest := ch
		rest.CalendarID = nil
		if to.ID != e.CalendarID {
			return s.move(ctx, e, to.ID, rest, scope)
		}
		ch = rest // "move" to where it already is: just the other changes
	}
   ```
   and
   ```go
   // move applies the other changes first (an op on the old calendar), then moves
   // the event, or its whole series, in the same transaction. The patch is pushed
   // before the move, each to the right calendar (Task 15.6).
   func (s *Service) move(ctx context.Context, e model.Event, to string, rest Changes, scope Scope) (model.Event, error) {
   	from, err := s.ResolveCalendar(ctx, e.CalendarID)
   	if err != nil {
   		return model.Event{}, err
   	}
   	if err := CanMove(e, from); err != nil {
   		return model.Event{}, err
   	}
   	id := e.ID
   	if e.IsInstance() {
   		if scope != ScopeAll {
   			return model.Event{}, ErrMoveOccurrence
   		}
   		id = e.SeriesID
   	}
   	var moved model.Event
   	err = s.store.InTx(ctx, func(tx *store.Store) error {
   		in := s.on(tx) // the same service, inside this transaction (InTx nests)
   		if rest != (Changes{}) {
   			if _, err := in.Update(ctx, e.ID, rest, scope); err != nil {
   				return err
   			}
   		}
   		cur, err := tx.GetEvent(ctx, s.account, e.CalendarID, id) // after the edit: the payload is for log lines
   		if err != nil {
   			return err
   		}
   		if err := tx.MoveSeries(ctx, s.account, e.CalendarID, to, id); err != nil {
   			return err
   		}
   		moved = cur
   		moved.CalendarID = to
   		return tx.Enqueue(ctx, store.Op{AccountID: s.account, CalendarID: e.CalendarID, EventID: id,
   			Kind: store.OpMove, Destination: to, Payload: moved, IfMatch: cur.ETag, SendUpdates: "none",
   			CreatedAt: s.now()})
   	})
   	return moved, err
   }

   func (s *Service) on(tx *store.Store) *Service { c := *s; c.store = tx; return &c }
   ```
   `Changes` holds pointers and a `*[]string`, so `rest != (Changes{})` is a valid comparison (all fields
   comparable). For "following" with a calendar change, `ErrMoveOccurrence` stops it before any edit is made.
4. Tests (`move_test.go`, with `newEnv`; add a writable `{ID: "work", Name: "Work", AccessRole: "writer"}` calendar
   in the test or in `newEnv`):
   - `TestMoveOneOff`: one queued op of kind move, source the test calendar, destination `work`, `SendUpdates ==
     "none"`; `svc.Get(id).CalendarID == "work"`; `titlesOn` still shows it once.
   - `TestMoveWithOtherChanges`: title and calendar in one `Update` → ops: patch on the test calendar (new title),
     then the move.
   - `TestMoveSeriesNeedsScopeAll`: an instance with `ScopeThis`, `ScopeFollowing` and `ScopeNone` →
     `ErrMoveOccurrence`, nothing queued; with `ScopeAll` → the master and its exceptions are in `work`.
   - `TestMoveRefusals`: to `Holidays` (reader) → `ErrReadOnly`; an invitation (self attendee, not organizer) →
     `ErrInvitation`; an orphan instance → `ErrMoveOccurrence`; an unknown calendar → `ErrNoCalendar`. None queues
     anything.
   - `TestMoveToTheSameCalendarIsJustAnEdit`: `CalendarID: ptr("Me")` plus a title → one patch, no move.

**Red:** compile error on `CalendarID`; with the field added but ignored, `TestMoveOneOff` fails (no op queued).

**Done when:** `go test -race ./internal/service ./internal/model ./internal/tui` passes.

**Commit:** `feat(service): move an event, or a whole series, to another calendar`

---

## Task 15.9: CLI: `clindar edit ID -c NAME`

**Goal:** `edit` takes `-c/--calendar` like `add`, and moves the event.

**Files:** `internal/cli/write.go`, `internal/cli/write_test.go`

1. Test (`write_test.go`, with the harness; the harness's store needs a second writable calendar, "Work"):
   `TestEditMovesToAnotherCalendar`: `edit ID -c Work --offline` prints the ID, and `show ID --json --offline` has
   `"calendar_id": "work"`. And `TestEditMovingOneOccurrenceSaysWhy`: `edit <instance> -c Work --scope this` exits
   non-zero with "move the whole series (scope all)" on stderr; with `--scope all` it succeeds.
   **Red:** `unknown shorthand flag: 'c'`.
2. `newEditCmd`:
   ```go
	cmd.Flags().StringVarP(&f.calendar, "calendar", "c", "",
		"move to this calendar (name or ID); a recurring event moves as a whole series (--scope all)")
   ```
   and in `changesFrom`:
   ```go
	if set("calendar") {
		ch.CalendarID = &f.calendar
	}
   ```
   Update the `Example` with `clindar edit abc123 -c Work                     # move to another calendar`.
3. The error mapping in `errors.go` already prints service errors as they are; check that `ErrInvitation` and
   `ErrMoveOccurrence` exit with the same code as `ErrReadOnly` (a refused edit), and map them if not.

**Done when:** `go test ./internal/cli` passes. `docs/cli.md` (Task 11.2) must list `-c` under `edit`.

**Commit:** `feat(cli): edit -c moves an event to another calendar`

---

## Task 15.10: TUI: the Calendar picker unlocks when a move is allowed

**Goal:** in the edit form, the Calendar picker works when `service.CanMove` allows it, listing **writable calendars
only**; otherwise it stays locked and shows the reason. Saving a calendar change on an occurrence of a series opens
the scope picker with **only "all"** and a line explaining why.

**Files:** `internal/tui/form.go`, `internal/tui/actions.go`, `internal/tui/layout.go`, `internal/tui/model.go`,
`internal/tui/form_test.go`

1. `editEventForm` (it already receives `cals`):
   ```go
	var from model.Calendar
	for _, c := range cals {
		if c.ID == e.CalendarID {
			from = c
		}
	}
	if err := service.CanMove(e, from); err != nil {
		f.pickers[fCalendar] = picker{title: "Calendar", options: []option{{label: from.Name, value: e.CalendarID}}}
		f.locked[fCalendar] = lockReason(err) // "invitation", "occurrence of a series", "read-only"
	} else {
		f = f.withCalendars(cals, e.CalendarID) // writable only, current one chosen
	}
   ```
   The writable-calendar options are built today inside `newEventForm`; move that loop into `withCalendars(cals,
   chosenID)` and call it from both (one place). The source calendar is always writable here (else editing is
   refused before the form opens), so it is always among the options. `lockReason` turns the three errors into the
   short text shown after the value: `can't move: an invitation`, `can't move: one occurrence`, `can't move:
   read-only`. Repeat stays locked as before.
2. `changes()`: `if f.changed(fCalendar) { ch.CalendarID = str(fCalendar) }` (the option's value is the calendar ID).
3. `applyChanges`: for an instance, a calendar change allows only "all":
   ```go
	if e.IsInstance() {
		m.mode, m.allowedScopes, m.scopeNote = modeScope, "tfa", ""
		if ch.CalendarID != nil {
			m.allowedScopes = "a"
			m.scopeNote = "One occurrence can't move to another calendar. Move the whole series?"
		}
		return m, nil
	}
   ```
   `Model` gains `scopeNote string`; the scope pop-up (`layout.go`, `modeScope`) shows it above the buttons when set.
   Everything else about the scope picker (keys, Esc cancelling the save) stays as it is.
4. Tests (`form_test.go`; the fixture needs a second writable calendar, "Work", and the existing read-only one):
   - `TestEditFormListsWritableCalendars`: edit a one-off; focus Calendar; Space → the options are Me and Work, not
     Holidays.
   - `TestEditFormMovesAnEvent`: choose Work, Enter → `f.model()` back to normal; the event's calendar is `work`
     (through `svc.Get`); one move op queued.
   - `TestEditFormLocksCalendarOnAnInvitation`: the picker shows "can't move: an invitation" and `l`/Space don't change it.
   - `TestMovingAnOccurrenceOffersOnlyTheWholeSeries`: edit an occurrence, choose Work, Enter → `modeScope` with
     `allowedScopes == "a"` and the note in `View()`; `a` → the master is in `work`.
5. Drag and drop doesn't change calendars (the grid has no calendar axis). Nothing to do; it's noted in design §6d.

**Red:** the first two tests fail (the picker is locked with "can't change").

**Done when:** `go test -race ./internal/tui` passes; golden files unchanged (the form isn't in them).

**Commit:** `feat(tui): change an event's calendar in the edit form`

---

## Task 15.11: Checks in a real terminal and against real Google

Record every result in `DEVIATIONS.md`. Fix anything that fails **and** its test or fake, as before.

**Real terminal:**
- [ ] The form: Enter saves from any field, including a closed picker; Space opens a list and Enter there chooses;
      `Ctrl-S` and [Save] save with a list open and keep the highlighted option.
- [ ] Esc on an untouched form closes it. After typing, Esc asks "Discard changes? (y/n)"; `n` keeps editing (and
      isn't typed), `y` closes without saving. [Cancel] does the same. From quick add + Tab, Esc asks.
- [ ] The mouse wheel and a trackpad swipe do nothing in the grid and the list.
- [ ] The edit form's Calendar picker: unlocked on your own event (writable calendars only), locked on an invitation
      with the reason; on an occurrence, saving a new calendar offers only [All events] with the note.

**Real Google** (on the "clindar-test" calendar plus a second calendar you own; add these rows to Task 8.7's table):
- [ ] After upgrading past migration 4, the first sync is a full one: existing events show their colors and Meet links.
- [ ] A one-off event moved with `clindar edit ID -c Other` keeps its ID (`clindar show ID --json`; the web UI shows
      it in Other), and `clindar sync` twice leaves exactly one copy.
- [ ] A series with one edited occurrence moves with `--scope all`; the web UI shows the series and the edited
      occurrence in Other.
- [ ] Guests aren't re-invited: move an event with a guest (a second account); the guest gets no new invitation email
      and their copy still works (this is `sendUpdates=none`; if their copy breaks, try `all` and record it).
- [ ] An event with a Meet link keeps it after the move.
- [ ] Moving one occurrence is refused with the "whole series" message (CLI) or the "all"-only picker (TUI).
- [ ] What Google does with a birthday or working-location event on your primary calendar: expect the move refused,
      reverted, and logged ("couldn't move it").

**Commit:** `docs: checks for moving events and the form keys`
