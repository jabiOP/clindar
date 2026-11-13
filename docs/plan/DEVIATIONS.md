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

## Ideas
