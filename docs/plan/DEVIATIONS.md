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
- **Tester bug 1 (Task 1.5):** `model.SortEvents` put every all-day event before every timed one across the whole
  list, so `agenda` TSV/JSON listed Nov 17–20 birthdays before a Nov 16 10:00 meeting. (The pretty view hid it by
  grouping by day first.) It now sorts by start day in the given zone first, then all-day before timed within the
  day. Test: `TestSortEventsIsByDayFirst`.
- **Tester bug 2 (Task 3.2):** every occurrence of a series reused the series' web link, which opens the first
  occurrence (Nov 18 opened Nov 4; a yearly birthday opened 2024). `recur.Instance` now rewrites the link: Google's
  link has `eid` = base64("<event id> <calendar>"), so it decodes the series' eid, swaps in the occurrence ID, keeps
  the calendar part exactly as Google wrote it, and re-encodes with the same base64 variant. Links in any other
  shape stay unchanged. Test: `TestInstanceLinksToItsOwnOccurrence`. **Needs a real-Google check (add to Task
  8.7):** open a non-first occurrence's `Link` from `clindar show <occurrence id> --json` and confirm it opens that
  date.
- **Tester bug 3 (Tasks 4.4, 12.8):** an invitation shown through someone else's read-only calendar offered [RSVP],
  and `r` (and the CLI's `rsvp`) queued a reply Google would reject. `service.RSVP` now refuses read-only calendars
  with `ErrReadOnly` before queuing anything, and the TUI's `permissions` requires a writable calendar for RSVP,
  with a reason (`whyNoRSVP`) that `startRSVP` (list `r`) and `detailKey` both show: one rule, as for edit. Tests:
  `TestRSVPOnReadOnlyCalendarIsRefused`, `TestDetailOfInviteOnReadOnlyCalendarHidesRSVP`.
- **Tester bug 4 (Task 5.2):** one `clindar sync` took 49.7s (others ~2s): manual sync and the daemon have no
  deadline, so a stalled connection could hang them for as long as it stalls. `gcal.NewClient` now gives the HTTP
  client a per-request limit, `gcal.RequestTimeout` (30s), unless the caller already set one; it copies the client
  rather than changing it. A timeout is an ordinary transient error: queued edits stay queued and the daemon backs
  off. No overall cap on a whole sync, since a first sync of many calendars can legitimately take long. Test:
  `TestStalledRequestTimesOut` (it took the stalling server's full 5s before the fix). The tester's other message,
  "warning: sync failed … context deadline exceeded" after an edit that did reach Google, is the 5s `sync_timeout`
  of the CLI's after-write sync expiring during the pull, after the push succeeded: expected, though the wording
  could say "push done, refresh timed out".
- **Tester notes (Task 8.1, shown in `clindar show` and the TUI details):** "When" showed only the first day of a
  multi-day event ("Wed 18 Nov 2026 all day" for Nov 18–22); it now shows first and last day (`Wed 18 Nov 2026 – Sun
  22 Nov 2026  all day`, or `Mon 16 Nov 2026 22:00 – Tue 17 Nov 2026 02:00` for timed events crossing midnight).
  "Repeats" said "yes (edit with --scope this|following|all)", a command-line hint that also appeared inside the
  TUI; it now says "yes" (the CLI's "choose a scope" error already explains the flag). Test:
  `TestPrettyDetailShowsWholeSpanAndPlainRepeats`.
- **Tester note (Task 12.5):** with the mouse on, "Delete this event?" showed [Delete] [Cancel] but not its keys,
  and Enter cancelled silently. The question now reads "Delete this event? (y/n)" when the mouse is on (mouse-off
  text unchanged), and any key other than `y` cancels with "not deleted" in the status bar. Enter still cancels: for
  a delete, cancel is the safe default. Test: `TestDeleteConfirmShowsKeysAndSaysWhenCancelled`.
- **Tester note (Task 12.5):** the details box grew wider than the terminal when the Link, Guests or description
  line was long, cutting off its right border. The detail text is now wrapped to the screen width minus the box's
  border and padding when it doesn't fit (wrapped, not truncated, so no link or guest is lost). It uses lipgloss's
  ANSI-aware width and wrap, since the title is already bold. Test: `TestDetailFitsTheTerminal`.
- **Tester note (Task 8.4):** `show` rejected `--offline`, unlike `agenda` and `next`. It also never refreshed a
  stale cache, although design §3 says every CLI read syncs first when the last sync is older than `stale_after`.
  `show` now calls `RefreshIfStale` and takes `--offline`, like the other read commands. Test:
  `TestShowRefreshesWhenStaleUnlessOffline`.
- **Tester note (Tasks 10.1–10.2, 12.2–12.3):** in the grid, the day numbers after "Dec 1" sat one column right of
  their events. Cause: emoji with a variation selector (❤️ ☀️ ✈️) are 1 cell in `go-runewidth` but 2 in lipgloss and
  in terminals; `fit` padded by runewidth while `lipgloss.JoinHorizontal` joined by lipgloss's measure, so that
  day's column came out 1 cell too wide and shifted the row. The TUI now measures everything with one helper,
  `width()` = `lipgloss.Width` (plain text still; it is the measure lipgloss pads with), and `fit` is rewritten on
  top of it. This replaces Phase 12's rule "widths come from plain text (`runewidth`)": same idea, one measure.
  `go-runewidth` is now only an indirect dependency (same version). Test: `TestGridColumnsStayAlignedWithEmoji` (a
  "❤️" title made a 113-column line in a 112-column grid). All golden files unchanged.
- **Tester re-test, Task 13.2 was incomplete:** real series links don't name the bare series: their `eid` decodes to
  the *first occurrence's* ID, `"<series id>_<date> <calendar>"` (e.g. `j87pvipe…_20241117 8j2n…@g`). `instanceLink`
  only accepted `"<series id> <calendar>"`, so it returned every real link unchanged. It now accepts the series ID
  with or without a `_<date>` suffix and replaces the whole ID. My first test passed only because its made-up link
  had no date. Test: `TestInstanceLinkReplacesADatedSeriesLink` (built from the tester's real link shape); the old
  test still passes.
- **Task 8.7 "each occurrence's link opens its own date" (tester, after 3c8897b):** every recurring event in the next
  7 days now carries its own occurrence ID in its link (e.g. `…_20261117` for a yearly birthday that used to name
  2024, `…_20261118T110000Z` for a weekly meeting that used to name Nov 4), and these are the IDs Google itself
  uses (section C matched them exactly). Checked by decoding the links; opening one in a browser is the last step.
- **User bug (Task 5.1): switching an event between all-day and timed reverted.** Google's PATCH merges nested
  objects field by field, so sending `start: {dateTime, timeZone}` onto a stored `start: {date}` kept the date, and
  the event stayed all-day (and the reverse kept a stale `dateTime`); clindar then stored Google's copy, so the edit
  looked undone. Moving the day or hour worked because it doesn't change which fields exist. `ToAPI` now sends the
  other kind's fields as explicit nulls (`NullFields`: `Date` for timed, `DateTime`/`TimeZone` for all-day),
  harmless on insert. The fake replaces start/end wholesale, so it can't model this; the test checks the request
  body instead: `TestToAPIClearsTheOtherKindOfStartAndEnd`. **Needs a real-Google check (add to Task 8.7):** turn an
  all-day event into a timed one and back, `clindar sync` after each, and the web UI keeps the change.
- **Task 14.3:** lipgloss emitted faint as the plain `\x1b[2m` for both the grid label and the list header (not
  combined with bold), so the literal-string tests work. The list test, described but not written out in the plan,
  is `TestPastDayHeadersAreFaint`; `renderList` returns `(string, hitMap)` since Task 12.3. To get the stated red
  step (no faint codes) instead of a compile error, the `today` field was added to `listView` before the test ran.
- **Task 14.7, two gaps in the spec filled:** (1) `window()` includes the list's range only while the list is
  showing (`m.view == viewList`); otherwise a list grown to two years, or one never opened (zero `listFrom`), would
  be loaded behind the grid. (2) Go-to (`g`) inside the list now calls `openList` for the new day, like `t`/`h`/`l`;
  since the list no longer follows `m.selected` by itself, it would otherwise stay where it was. The removed
  `listDays` constant became the `listDays()` method, as specified.
- **Task 14.8, three adjustments:** (1) `gridView` gets `dayMode bool` plus `focus int` instead of `focus int` with
  -1 = none: every test that builds a `gridView` directly leaves `focus` at 0, which would mark the selected day's
  first event and change the golden files. (2) Clicking the already-selected day goes through the same
  `startDayMode()` as Enter, so on a day with no events it says "no events on this day · a to add" instead of
  entering an empty event mode. The rewritten mouse test therefore clicks Mon 16 (a standup) and a new
  `TestClickSelectedEmptyDaySaysSo` covers Tue 17. (3) `TestEventModeMovesAndOpens` and
  `TestEventModeActsLikeTheDetailsBox` already passed before the change, because Enter used to open the list and the
  same keys there reached the same result; the other tests gave the red step.
- **Task 14.9:** compiled as specified apart from imports. Go-to already parses through `m.deps.Dates`, ahead of
  Task 14.10. Step 4's `docs/cli.md` doesn't exist yet (Phase 11, Task 11.2), so `20/11`, `20/11/2027`, `20/11
  15:30` and the `date_order` key must go into its time-format and config tables when it is written.
- **Task 14.10:** compiled and passed as written. The TUI check described in step 1 is
  `TestGotoDayNumberAndRefusedTime`. Like 14.9, the go-to description belongs in `docs/cli.md` (TUI section) when
  Task 11.2 writes it.
- **Task 14.11:** compiled and passed as written. One rendering fix, as the plan anticipated: widths were right, but
  on the cursor row the colored swatch's own reset code (`\x1b[0m` after the `●`) also ended the row's reverse
  video, so the highlight stopped after the dot. The cursor row is now built from plain text (an uncolored `● `) and
  styled once with `selectedStyle`; other rows keep the colored swatch.
- **Task 14.13:** compiled as written. To get the stated red step (`c` does nothing) instead of a compile error, the
  `modeCalendars` constant was added before the tests ran. The mouse test described in step 1 is
  `TestCalendarsButtonOpensMenuAndRowClickToggles`. `grid_mouse.golden` changed only in its status bar, which gained
  `[Calendars]`.
- **Task 14.14:** compiled and passed as written (after `gofmt`). New package `internal/quickadd` (→ `timeparse`,
  `recur`): the README's "Final package map" and its dependency-direction list should gain it.
- **Task 14.15:** compiled and passed as written. Worth noting in `docs/cli.md` and `docs/scripting.md`: a title
  read from stdin (`clindar add -`) with no `--at` also goes through quick add, so a task line ending in a token
  word (`… daily`, `… fri`) gets parsed. The GTD recipes pass `--at`, which keeps the title literal; the docs should
  say so.
- **Task 14.16:** compiled after adding `quick.go`'s imports (the plan showed `package tui` without them).
  `formFromQuick` is written from the description: for all-day events the End field gets the inclusive last day only
  when the event spans more than one day. Since `a` opens quick add now, `newEventForm` is used only by
  `TestAddFormUsesDefaultCalendar`; quick add's Enter and `formFromQuick` both use `Deps.DefaultCalendar`.
- **Task 14.17, adjustments:** (1) `form.pickers` is `map[int]picker` (values), not `map[int]*picker`, and every
  change copies the map first (`withPicker`): Bubble Tea models are values, and with pointers every copy of a form
  would share and mutate the same pickers. (2) On edit, a changed Duration with an unchanged (pre-filled) End drops
  the End, as the old form did, so changing the duration doesn't fail with "an end or a duration, not both". (3) The
  edit form's End is a bare time when the event ends the same day, or the next day at or before its start time (span
  reads a bare time that way); longer spans get the full date. (4) `draft`/`changes` read the parser from the form
  (`f.dates`) instead of a parameter. (5) The open dropdown's rows come from a new `picker.rows`, shared with the
  pop-up view. (6) The hint is two lines: as one line (~86 cells) it pushed the box past an 80-column terminal, seen
  in the step-9 look. (7) `formKey`: Esc closes an open list before the form. (8) Updated two older tests to the new
  form: `TestFormShowsParseErrors` now expects the Date field's error, and `TestAddFormUsesDefaultCalendar` uses the
  new `newEventForm` signature. Step 9 checked in tmux at 80 columns: quick add → tab → walk the fields → open
  Repeat → choose weekly → save, and `agenda` shows the weekly series. `Ctrl-J`/`Ctrl-K` worked there (a Task 14.23
  item).
- **Task 14.18:** compiled as written after adding `spanDrag` (in `mouse.go`, next to `dragState`) and the imports.
  To get a behavioral red step, the `targetEmpty` constant was added before the tests ran. One older test changed by
  design: `TestGridHitMap` (Task 12.3) asserted that a blank line under a day's events is `targetDay`; it is now
  `targetEmpty`, and the test also checks that the day number is still `targetDay`. Checked in tmux: dragging blank
  space from Tue 17 to Thu 19 opens quick add with "Tue 17 Nov – Thu 19 Nov, all day".
- **Task 14.19:** compiled as specified. The per-layer tests, described but not written out in the plan, are
  `TestEffectiveColor` (model), `TestEventColorRoundTrip` (convert), `TestEventColorsReadsThePalette` (client),
  `TestFakeColors`, `TestEventColorsRoundTripAndReset` (store), `TestPaletteIsFetchedOnce` (syncer) and
  `TestJSONHasTheEffectiveColor` (output). `pullPalette` runs first in `Pull`, so offline the pull fails at
  "colors:" rather than "calendars:"; it is the same transient error either way. **Needs a real-Google check (Task
  14.23):** setting a color, then "Calendar default", clears `colorId`.
- **Task 14.20:** two adjustments. (1) The grid color test can't hard-code `38;2;220;33;39` / `38;2;66;133;244`:
  lipgloss rounds when converting hex (#4285f4 → `38;2;65;133;243`), so `TestEventsDrawInTheirOwnColor` compares
  against the escape code lipgloss itself emits for each hex; drawing in the calendar color makes it fail, as
  required. (2) The form gets the palette through a `withPalette` method at its three call sites, rather than
  through yet another constructor parameter. The form and service tests described in the plan are
  `TestColorPickerSetsAndClearsTheColor` and `TestUpdateColor`.

## Ideas
- **Grid "offset after Dec 1" (tester, both rounds) is not a width bug.** Single-digit day numbers are right-aligned
  to two characters (`fmt.Sprintf("%2d")` in `gridView.cell`, Task 10.1), so " 2" puts its digit one column right
  of the events below it. "Dec 1" and two-digit days have no leading space, which is why it looks like it starts
  after Dec 1. The tester's positions match exactly (Dec 1 at 20; digits of " 2", " 3" at 41, 61). Every grid line
  measures exactly the grid width with colors, emoji and selection (checked at 140 columns). If left-aligned
  numbers are preferred, it's a one-character change (`%2d` → `%d`) plus regenerating the grid golden files.
- The details box now wraps long lines, but a wrapped Guests or Link line continues at the box's left edge instead
  of under its value (tester). Wrapping each `Label: value` row's value separately, indented 11 columns, would fix it.
- `clindar calendars show|hide NAME`: there's no CLI command to change calendar visibility yet (Task 14.12 adds it to
  the store, service and TUI only); a small follow-up.
