# Deviations from the plan

One line per deviation: what changed, and why.

- **Task 4.1:** `TestOrphanInstanceIsShownAndEditableAsOneOff` calls `svc.RSVP` and `service.ScopeNone`, which
  only exist after Tasks 4.4 and 4.2, so the 4.1 test file can't compile. The RSVP check moved to Task 4.4 as
  `TestRSVPOnOrphanInstanceNeedsNoScope` in `delete_test.go`; 4.1 keeps "shown, and fetched as a one-off".
- **Task 9.1:** `daemon_test.go` includes `TestUnitsRunTheGivenBinary`, which uses `daemon.Unit`/`SystemdUnit`/
  `LaunchdAgent` from Task 9.2, so the 9.1 test file can't compile and the Loop test can't run. The units test is
  added in Task 9.2 instead (with the `strings` import it needs).
- **Task 10.2 (found in 10.4's terminal walk-through):** on a color terminal the list's selected row lost its
  `● Calendar` label, and the cut could land inside an escape code. `renderList` ran `fit()` on an already-colored
  line, and `runewidth` counts escape codes as width. `listView.line` now builds the selected row from plain text and
  styles it once; it also measures the calendar name with `runewidth.StringWidth` instead of `len()`. Test:
  `TestRenderListSelectedRowKeepsCalendarWithColors`. Golden files are unchanged.
- **Task 12.6 (terminal walk-through, tmux sending real SGR mouse events, local database, no Google):** passed:
  click day / click selected day opens the list; click event opens details with [Edit] [Delete] [RSVP] [Close];
  click outside closes; drag a one-off to another day (clock time kept); drag a trip's `▸` two days later and `◂`
  one day earlier; drag a recurring instance shows the scope picker and `[This event]` moves only that instance;
  Esc mid-drag changes nothing ("drag cancelled"); dragging a read-only calendar's event opens details with
  "Holidays is read-only"; `mouse = false` shows no handles or buttons and ignores clicks. **Not checked here:**
  the web UI after each drag (needs Google), Shift-to-select (a terminal-emulator feature), and adding `mouse` to
  `docs/cli.md` (that file comes with Task 11.2, not written yet). **Readability:** at 80 columns (11 per day)
  one-day titles shrink to `◂09:00 S…▸`, still usable to find and drag events but not to read them; at 112
  columns titles of ~8 characters fit (`◂09:00 Standup▸`). As the plan predicted.
- **Task 12.7 (uncompiled spec):** after rewriting `TestGridHitMap` (edges found by column, not by symbol),
  `grid_test.go` no longer uses `runewidth`; its import had to go, or the package doesn't compile. Everything else
  compiled as written.
- **Task 12.9 (terminal re-check, same tmux setup as 12.6: mouse events injected as SGR sequences, including
  button-less motion for hover):** passed: at 80 columns no `◂`/`▸` shows until the pointer is on an edge, and titles
  use the whole cell (`09:00 Sta…`); hovering a first day's left edge shows `◂`, a last day's right edge shows `▸`,
  moving away hides it, and a middle day's right edge shows nothing; dragging a trip's right edge two days later
  without hovering first works; an invitation's details offer `[Delete] [RSVP] [Close]` and a read-only holiday's
  only `[Close]`, with `e`/`d`/`r` explained in the status bar (also checked with the mouse off: the hint line lists
  only the possible keys). **Not checked:** whether real terminals report hover. Injecting the escape sequences tests
  clindar's handling of all-motion events, not whether macOS Terminal.app, or tmux with `set -g mouse on`, actually
  sends them. That needs a person with a mouse.

## Ideas
- After `g` (go to date) in the list view, the cursor keeps its old index instead of going to the first event of the
  new day, so Enter can open the wrong event. `gotoKey` could reset `m.cursor = 0`, as `h`/`l` already do.
- Pressing `e` on an invitation's details says "can't edit: you're a guest: only the organizer can **move** it":
  `whyNoEdit` reuses `canDrag`'s wording. "change" would fit both uses.
