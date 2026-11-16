# clindar: Implementation plan

You are building **clindar**, a fast terminal Google Calendar client: a TUI plus Unix-friendly CLI.
Read `00-design.md` first (15 minutes). It explains *what* we're building and *why*.
These files explain *how*, as small tasks.

## How to use this plan

- Work through the phases **in order**. Each phase builds on the previous one.
- Each task has: **Goal**, **Files**, **Steps**, **Done when**, and a **Commit**.
- Steps follow strict TDD:
  1. Write the test.
  2. Run it and **watch it fail for the reason you expect**. If it fails for some other reason (e.g., compile
     error in an unrelated file), fix that first.
  3. Write the minimum code to pass.
  4. Run the whole package's tests.
  5. Commit.
- Code in this plan is meant to be used as-is. If you must deviate (an API changed, a bug in the plan), do it,
  then write one line in `docs/plan/DEVIATIONS.md` saying what and why.
- **Don't add features not in the task.** If you think something is missing, write it in `DEVIATIONS.md` under
  "Ideas" and move on. (YAGNI.)
- **Don't copy-paste logic.** Each piece of logic lives in exactly one place, and the plan says where. If you're about
  to write the same `if` twice, stop and look for the existing helper. (DRY.)

| Phase | File | You'll have at the end |
|---|---|---|
| 1 | `01-foundations.md` | Repo, domain model, date parsing, config/paths |
| 2 | `02-store.md` | SQLite store: calendars, events, outbox, sync state, log |
| 3 | `03-recurrence.md` | RRULE expansion, instance IDs, series splitting, `--repeat` parsing |
| 4 | `04-service.md` | Query + edit API used by every front-end |
| 5 | `05-google.md` | Google adapter + in-memory fake |
| 6 | `06-sync.md` | Push/pull sync engine, cross-process lock |
| 7 | `07-auth.md` | OAuth login, token storage |
| 8 | `08-cli.md` | All CLI commands. **First milestone: real data on screen** |
| 9 | `09-daemon.md` | Background sync + systemd/launchd install |
| 10 | `10-tui.md` | Bubble Tea UI: grid, list, detail, forms |
| 11 | `11-docs-release.md` | User docs, scripting recipes, manual E2E checklist, release build |
| 12 | `12-mouse.md` | Mouse: clicks, wheel (turned off in Task 15.3), buttons, drag to move/resize in the grid (written as diffs on Phase 10 code) |
| 13 | `13-fixes.md` | Fixes from testing (13.1–13.10, 13.13), then two product decisions: left-aligned day numbers, indented wrapped details |
| 14 | `14-ux-round-1.md` | UX round 1: event mode, quick add, pickers, calendar menu, colors, Meet links, day/month dates (spec, not compiled) |
| 15 | `15-after-ux-round-1.md` | Done: one full resync (migration 4), form keys (Enter saves, Space lists, `Ctrl-S`), Esc asks before discarding, wheel off. Esc then Enter discards, literal stdin titles, clearer write warning, [Copy] fix, moving an event to another calendar. To do: Task 15.15's real-Google checks |

## Toolset primer (read once)

### Go

- **Go 1.26+ is required.** Our pinned dependencies (`golang.org/x/oauth2`, `google.golang.org/api`,
  `modernc.org/sqlite`) need it. **Do not downgrade dependencies to fit an older Go.**
  If your installed Go is older (e.g. 1.24), that's usually fine. `go.mod` says `go 1.26`, so the `go` command
  downloads the 1.26 toolchain automatically (`GOTOOLCHAIN=auto`, the default) from `proxy.golang.org` on first
  use, and `go version` *inside the repo* then reports 1.26. If that download is blocked (corporate proxy,
  sandbox, `GOTOOLCHAIN=local`), install Go 1.26 from <https://go.dev/dl/>, or ask for your environment to be
  updated, before starting Task 1.1.
- Layout: everything except `main.go` lives under `internal/`, which other modules cannot import.
  One directory = one package.
- `go test ./...` runs everything. Useful flags:
  - `-run 'TestName/subtest'` runs only matching tests.
  - `-count=1` disables the test cache.
  - `-race` turns on the data race detector. **Run with `-race` before every commit that touches syncer, daemon, or tui.**
  - `-v` prints verbose output.
- `go vet ./...` and `gofmt -l .` must both be clean before every commit. Set your editor to run `gofmt` on save.
- Errors are values. Wrap them with context: `fmt.Errorf("load calendar %s: %w", id, err)`. Check for specific ones with
  `errors.Is(err, store.ErrNotFound)`. Never `panic` for expected failures.
- `context.Context` is the first parameter of anything that does I/O. Pass it through and never store it in a struct.
- **Dependencies:** add them with `go get module@version` and finish with `go mod tidy`. **Never `go get`
  `github.com/charmbracelet/x/...` packages directly.** Lip Gloss pins compatible versions, and pulling a
  newer `x/ansi` breaks the build with errors like `ansi.Style has no field or method SlowBlink`.
  Whatever versions Lip Gloss and Bubble Tea pull in themselves (e.g. `x/ansi v0.11.6` as an `// indirect`
  line in `go.mod`) are expected and fine. The rule is only: never `go get` them yourself.
- Timezones: `main.go` and every test package that loads named zones must `import _ "time/tzdata"`,
  so `time.LoadLocation("America/New_York")` works on machines without a zoneinfo database.

### SQLite (`modernc.org/sqlite`)

- Pure Go, so no C compiler or cgo is needed. The driver name is `"sqlite"`.
- We open the database in **WAL** mode: many readers plus one writer, and readers never block. `busy_timeout(5000)` makes a
  writer wait up to 5s for another process's write lock instead of failing. `_txlock=immediate` makes every
  transaction grab the write lock up front. This avoids `SQLITE_BUSY` errors when two processes (daemon and TUI)
  both read and then write.
- Schema versioning uses `PRAGMA user_version` (an integer stored in the DB file header).
- **When `FromAPI` starts reading a new field, append a migration that resets the sync tokens**
  (`UPDATE calendars SET sync_token = ''; UPDATE sync_state SET calendars_token = '';`, as migration 4 does).
  Incremental sync only resends changed events, so without it events cached earlier never get the field.
- SQLite can't alter a `CHECK` constraint or drop most constraints: to change one, rebuild the table
  (`CREATE TABLE x_new …; INSERT INTO x_new SELECT …; DROP TABLE x; ALTER TABLE x_new RENAME TO x;` and recreate its
  indexes), as migration 5 does for the outbox.

### Google Calendar API v3 (read these pages; ~45 min total)

- Concepts: <https://developers.google.com/calendar/api/concepts/events-calendars>
- Recurring events: <https://developers.google.com/calendar/api/concepts/events-calendars#recurring_events>
- Incremental sync (sync tokens, 410): <https://developers.google.com/calendar/api/guides/sync>
- Event resource reference: <https://developers.google.com/calendar/api/v3/reference/events>
- Go client: <https://pkg.go.dev/google.golang.org/api/calendar/v3>

Key facts you'll need:

1. A **calendar list entry** is *your view* of a calendar: color, whether it's shown (`selected`), and your `accessRole`.
2. **All-day events** use `start.date` / `end.date` (`"2026-11-14"`), and `end` is **exclusive**.
   Timed events use `start.dateTime` (RFC 3339) plus an optional `timeZone`.
3. A **recurring event** is a *master* with `recurrence: ["RRULE:FREQ=WEEKLY;BYDAY=MO", "EXDATE;…"]`.
   Changed or cancelled occurrences are separate *exception* events with `recurringEventId` and `originalStartTime`.
   Their ID is `<masterId>_<originalStart in UTC basic format>`. You can PATCH or DELETE an instance ID even if no
   exception exists yet, and Google creates one.
4. **Sync tokens:** a full `events.list` returns `nextSyncToken`. Sending it next time returns only changes,
   including deletions as `status: "cancelled"` (often with *no other fields*). `410 Gone` means the token is
   dead: wipe and resync.
5. **ETags:** every event has an `etag`. Sending `If-Match: <etag>` on PATCH/DELETE makes Google return **412**
   if someone else changed the event first.
6. **Client-chosen IDs:** you may set `id` on insert (chars `a-v0-9`, length 5–1024). Inserting an existing ID
   returns **409**.
7. **Go client gotcha:** zero-value fields (`""`, `false`) are *omitted* from requests. To clear a field (e.g.
   remove a description), list it in `ForceSendFields`.
8. `sendUpdates` (`all` / `none`) controls whether attendees get emails.

### Bubble Tea (TUI), v1

- Docs: <https://github.com/charmbracelet/bubbletea/tree/v1.3.10> (read the README tutorial and the
  `examples/` folder for `textinput` and `list`). Lip Gloss: <https://github.com/charmbracelet/lipgloss/tree/v1.1.0>.
- **We use v1** (`github.com/charmbracelet/bubbletea`). v2 lives at `charm.land/bubbletea/v2` and has a different API.
  If a blog post uses `tea.KeyPressMsg` or `charm.land`, it's v2: ignore it.
- The Elm architecture: a `Model` has `Init() tea.Cmd`, `Update(tea.Msg) (tea.Model, tea.Cmd)`, and `View() string`.
  - **`Update` must never block.** Anything slow (DB, network) goes in a `tea.Cmd`, which is a `func() tea.Msg`
    that Bubble Tea runs in a goroutine. Its result comes back as a message.
  - `View` is a **pure function of the model**. That makes it trivially testable.

### rrule-go

- Docs: <https://pkg.go.dev/github.com/teambition/rrule-go@v1.8.2>.
- We use `rrule.StrSliceToRRuleSetInLoc(lines, loc)` then `set.DTStart(start)` then `set.Between(from, to, true)`.
- **Never call `set.All()`.** An infinite series never returns.

## Testing guide (read this. Really.)

The goal of a test is to **fail when behavior breaks and pass otherwise**. Tests that break on harmless
refactors are a cost, not a safety net.

1. **Test behavior through the public API of the package.** Call `store.EventsInRange`, not the private SQL helper.
   Tests live in the same directory. Prefer `package foo_test` (black-box) unless you truly need internals.
2. **One behavior per test (or per table row).** Name it after the behavior:
   `TestEnqueue_PatchAfterInsertStaysInsert`, not `TestEnqueue2`.
3. **Arrange / Act / Assert.** Separate the three with blank lines. If "arrange" is 40 lines, build a helper or
   fixture (see `internal/testutil`).
4. **Table-driven tests** when the same behavior varies by input (parsers, formatters):
   ```go
   tests := []struct{ name, in string; want time.Time; wantErr bool }{ ... }
   for _, tt := range tests {
       t.Run(tt.name, func(t *testing.T) { ... })
   }
   ```
5. **Deterministic time.** Code never calls `time.Now()` directly. It takes a `now func() time.Time`.
   Tests use a fixed instant. Flaky tests usually come from the clock or from goroutine timing.
6. **Real over fake over mock.**
   - Things we own and are fast (SQLite in `t.TempDir()`, pure functions) are tested *for real*.
   - The network boundary (Google) gets a **fake**: `internal/gcal/gcaltest.Fake`, a small in-memory
     implementation that behaves like Google (etags, sync tokens, 409/412/410). Fakes give realistic tests.
   - Avoid mocks that assert "method X was called with Y". They test implementation, not behavior.
     Assert on outcomes instead (what's in the store, what the fake server now holds).
7. **Assert precisely, with helpful failure messages:** `t.Fatalf("Start = %v, want %v", got, want)`.
   Use `t.Fatalf` when continuing makes no sense. Use `t.Errorf` to report several independent problems.
8. **Golden files** for big text output (TUI renders, help text): the expected output lives in `testdata/*.golden`.
   `go test ./internal/tui -update` rewrites them. **Always review the diff** of a golden update before committing.
9. **Helpers call `t.Helper()`**, so failures point at the caller's line.
10. A bug report becomes a failing test first, then the fix.

We use only the standard `testing` package plus `github.com/google/go-cmp/cmp` for struct diffs
(`if diff := cmp.Diff(want, got); diff != "" { t.Errorf("mismatch (-want +got):\n%s", diff) }`).

## Commit conventions

- Commit after every task, or more often. Small commits are easy to review and revert.
- Format: `type(scope): summary` where type ∈ `feat|fix|test|refactor|docs|chore`. Example:
  `feat(store): coalesce outbox ops per event`.
- Before committing, run:
  ```bash
  gofmt -l . && go vet ./... && go test ./...
  ```
  `gofmt -l` must print nothing.
- Never commit `client.json`, tokens, or `*.db` files. `.gitignore` covers them. Check `git status` anyway.

## Final package map

```
main.go                       entry point → cli.Main()
internal/
  model/        Event, Calendar, Attendee, IDs, GroupByDay, SortEvents      (no deps)
  timeparse/    "tomorrow 2pm" → time.Time; "1h30m"/"2d" → Duration         (no deps)
  quickadd/     "Dentist 20/11 15:30 45m" → title + span + repeat (Phase 14)
  config/       Paths (XDG), Config (TOML)
  store/        SQLite persistence; outbox coalescing
  recur/        expand series, instance lookup, split, --repeat parsing
  service/      Agenda/Get/Create/Update/Delete/RSVP: the only write path
  gcal/         Google adapter (convert + Client); gcal/gcaltest: Fake
  syncer/       Engine (push/pull), file lock
  auth/         OAuth loopback+PKCE, token stores
  browser/      open a URL in the user's browser (Phase 14)
  clipboard/    copy text via the terminal, OSC 52 (Phase 14)
  output/       pretty / TSV / JSON / template printers
  cli/          cobra commands, App wiring
  daemon/       loop + backoff, systemd/launchd files
  tui/          Bubble Tea app
  testutil/     fixtures, golden helper, fixed clock
```

Dependency direction (enforced in code review):

- `model` and `timeparse` depend on nothing internal.
- `store` → model; `recur` → model; `service` → store, recur, model; `quickadd` → timeparse, recur.
- `gcal` → model; `syncer` → store, gcal, model.
- `output` → model; `cli`, `tui`, `daemon` → anything above.
- **Nothing imports `cli` or `tui`.** If you need something from them, it belongs lower down.
