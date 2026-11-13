# clindar: Design

> Status: Section 1 (architecture) was reviewed and approved. Sections 2–7 were written during
> planning and capture decisions made in the design conversation. Review them before starting
> Phase 2.

## 0. Decisions log (from the design conversation)

| # | Question | Decision |
|---|----------|----------|
| 1 | How much can the app change? | **Full read/write**: create/edit/delete/move, RSVP, invite attendees |
| 2 | Where does data live? | **Local SQLite cache with background sync**; offline edits queue in an outbox |
| 3 | What runs sync? | **Optional daemon** (`clindar daemon`, polls every ~45s). Without it, the TUI syncs while open and CLI reads sync when the cache is stale |
| 4 | Language / TUI | **Go + Bubble Tea v1 + Lip Gloss + Bubbles** |
| 5 | CLI output | **Human-readable on a TTY**; TSV when piped; `--json` (JSON Lines), `--tsv`, `--format '<go template>'` |
| 6–7 | Integration with the user's GTD (plain-text, one task per line) | **Generic composition only.** The app knows nothing about the task files. Glue lives in shell scripts, documented in `docs/scripting.md` |
| 8 | TUI views (v1) | **Scrollable 4-week grid** and an **agenda list**. Day/week/month views are out of scope |
| 9 | Accounts | **One Google account in v1**, but every table is keyed by `account_id` so multi-account is UX work later, not a migration |
| 10 | OAuth client | **Bring-your-own** client JSON by default, plus build-time `-ldflags` hook for an embedded client |
| 11 | Conflicts | **Google always wins.** On HTTP 412 the local edit is discarded and recorded in a log (`clindar log`, TUI status bar) |
| 12 | Recurring events | **Full parity**: create with common patterns; edit/delete *this / this and following / all* |
| 13 | Notifications | **None in-app.** Expose `clindar next` and `clindar agenda --within 10m` for users to wire into notify-send, status bars, cron |
| 14 | Platforms | **Linux + macOS** (systemd user unit / launchd agent; Secret Service / Keychain) |

Defaults chosen by the assistant (open to change): binary name `clindar` (avoids clashing with
util-linux `cal` and GNU `gcal`), vim-style keys, config at `$XDG_CONFIG_HOME/clindar/config.toml`,
natural-language-ish date input (`tomorrow 14:00`, `fri 2pm`, `2026-11-14 09:30`, `+2h`).

## 1. Architecture (approved)

One Go binary, three faces:

1. `clindar` with no args opens the TUI.
2. `clindar <command>` runs scriptable subcommands (`agenda`, `next`, `show`, `add`, `edit`, `rm`, `rsvp`, `sync`, `log`, `calendars`, `auth …`, `daemon …`).
3. `clindar daemon` is the optional background syncer.

```
┌──────────────┬──────────────┬──────────────┐
│   TUI        │   CLI cmds   │   daemon     │   ← front-ends (internal/tui, internal/cli, internal/daemon)
├──────────────┴──────────────┴──────────────┤
│  Service: queries, edits, recurrence scope  │   ← internal/service (+ internal/recur)
├─────────────────────────────────────────────┤
│  Store (SQLite)     │  Sync engine          │   ← internal/store, internal/syncer
│  events, calendars, │  push: outbox → API   │
│  outbox, sync state │  pull: syncToken      │
├─────────────────────┴───────────────────────┤
│  Google adapter (OAuth, Calendar API v3)    │   ← internal/gcal, internal/auth
└─────────────────────────────────────────────┘
```

**Reads never touch the network.** All front-ends read SQLite at `$XDG_DATA_HOME/clindar/clindar.db`
(WAL mode, so readers and one writer can work at the same time).

**Edits** are written locally first, which is instant, and recorded in the `outbox` table in the
same transaction. The sync engine pushes the outbox and then pulls changes.

**Only one sync at a time** across processes, enforced by `flock` on `$XDG_DATA_HOME/clindar/sync.lock`.
If the daemon holds the lock, the CLI and TUI simply read and do not sync themselves.

## 2. Data model

One domain type, `model.Event`, is used everywhere. It covers single events, recurring
masters, materialized exceptions, and virtual (expanded) instances. Google's own types never leak past
`internal/gcal`.

- **Timed events:** `Start`/`End` are instants; `TimeZone` holds the IANA zone, which recurrence expansion needs.
- **All-day events:** `AllDay=true`; `Start`/`End` are **UTC midnights**, and `End` is **exclusive**
  (a one-day event on Nov 14 has Start=Nov 14 00:00Z, End=Nov 15 00:00Z). Display code re-anchors
  them to local midnight via `Event.Span(loc)`.
- **Recurring master:** `Recurrence` holds Google's lines (`RRULE:…`, `EXDATE…`, `RDATE…`).
- **Exception** (one instance changed or cancelled): `SeriesID` = master ID, `OriginalStart` = the
  slot it replaces, `ID` = Google's instance ID (`<masterID>_<YYYYMMDDTHHMMSSZ>`, or `_<YYYYMMDD>` for all-day).
- **Virtual instance:** produced by expansion and shaped exactly like an exception, but not stored.

**IDs are generated locally** (base32hex, which Google accepts for client-supplied IDs). An event created offline
therefore already has its final ID, and no temp-ID remapping is needed.

SQLite stores each event as indexed columns (`start_ts`, `end_ts`, `series_id`, `original_ts`,
`is_master`, `status`) plus a JSON blob of the full `model.Event`. Adding a field means changing
the struct and nothing else.

## 3. Sync

Every sync cycle **pushes first, then pulls**.

**Push** sends outbox ops to Google in sequence order. There is at most one pending op per event, because
`Enqueue` coalesces them: insert+patch becomes insert, patch+patch becomes patch, insert+delete becomes nothing, and patch+delete becomes delete.
Each op carries `If-Match: <etag>` when one is known.

| Result | Action |
|---|---|
| 2xx | Store the returned event (new etag) and complete the op |
| 409 on insert | Already created by an earlier interrupted push, so count it as success |
| 404/410 on patch/delete | The event is gone, so drop the op and delete it locally |
| **412** | **Google wins**: drop the op, log `discarded_edit`, re-fetch the event |
| other 4xx | Permanent: drop the op and log an error |
| network/5xx/429 | Transient: keep the op, increment `attempts`, stop pushing this cycle |

If the user edits an event *while* its op is in flight, the op's `version` changes and the
completed push does not delete it. It is rewritten as a patch against the new etag.

**Pull:** `calendarList.list` (with sync token), then `events.list` per calendar with
`syncToken`, `showDeleted=true`, `singleEvents=false`. Masters arrive with their RRULEs, and
recurrence is **expanded locally** (`rrule-go`), so offline-created series display correctly.
Events with a pending outbox op are skipped, because local wins until pushed. A `410 Gone`
clears that calendar's cached events and does a full resync.

**Freshness:** daemon every `sync_interval` (default 45s) with exponential backoff up to 5 minutes when offline.
The TUI syncs on open, on the same interval, and after edits. CLI reads sync first if the last sync is older than
`stale_after` (default 2m), bounded by `sync_timeout` (default 5s). `--offline` skips this.

## 4. Recurrence edits

| Scope | Edit | Delete |
|---|---|---|
| this | Write an exception with the instance's ID; PATCH the instance ID | Store a cancelled exception; DELETE the instance ID |
| following | Truncate the master (`UNTIL` = just before the split; `COUNT` converted) and INSERT a new master from the split point with the changes | Truncate only |
| all | PATCH the master; time changes are applied as a *shift* | DELETE the master |

If "following" is chosen at the first instance, it becomes "all". RSVP supports *this* and *all*.

## 5. CLI surface

```
clindar                                   # TUI
clindar agenda [--today|--days N|--from X --to Y|--within DUR] [-c CAL]... [--json|--tsv|--format T] [--offline]
clindar next   [--json|--format T]        # exit 1 if nothing upcoming
clindar show ID
clindar calendars
clindar add [TITLE|-] --at WHEN [--end WHEN|--dur DUR] [--all-day] [-c CAL] [--where L] [--desc D]
         [--invite EMAIL]... [--repeat daily|weekdays|weekly|monthly|yearly|FREQ=…] [--until DATE|--count N]
clindar edit ID [same flags] [--scope this|following|all]
clindar rm ID [--scope …]
clindar rsvp ID yes|no|maybe [--scope this|all]
clindar sync [--full]      clindar log [-n N]
clindar auth setup FILE | login | status | logout
clindar daemon [--interval D] | daemon install | daemon uninstall
```

- Writes print the event ID on stdout, so `id=$(clindar add …)` works. Human messages go to stderr.
- TSV columns are fixed: `id  start  end  all_day  calendar  title`.
- JSON Lines uses the stable schema in `internal/output`, never Google's raw JSON.
- Exit codes: 0 ok, 1 error or nothing found, 2 usage error.

## 6. TUI

- **4-week grid:** 7 columns × 4 rows. `h/l` move a day, `j/k` move a week, and the window scrolls a week at a
  time when the selection leaves it. `J/K` scroll without moving the selection off-screen. `t` jumps to today,
  `g` goes to a date. Cells show `HH:MM title` lines colored by calendar, with `+N more` on overflow.
- **List:** day-grouped agenda starting at the selected day. `j/k` select an event and `enter` opens its detail.
- **Everywhere:** `tab` toggles views, `a` adds, `e` edits, `d` deletes, `r` RSVPs, `s` syncs now, `?` shows help, `q` quits.
  Recurring edits show a scope picker.
- **Status bar:** last sync age, pending op count, and discarded-edit count.

## 7. Security & config

- OAuth: desktop loopback flow with PKCE, scope `https://www.googleapis.com/auth/calendar`.
- Tokens are stored in the OS keyring (`zalando/go-keyring`), or in a `0600` file in the data dir if no keyring is available.
- Client JSON goes in `$XDG_CONFIG_HOME/clindar/client.json`. `auth.BuiltinClientID`/`BuiltinClientSecret` can be set via `-ldflags`.
- Config (`config.toml`): `sync_interval`, `stale_after`, `sync_timeout`, `default_calendar`,
  `week_start`, `send_updates`. Every key is optional.
- Env overrides: `CLINDAR_CONFIG_DIR`, `CLINDAR_DATA_DIR` (used heavily by tests).

## Out of scope for v1 (YAGNI)

Desktop notifications, day/week/month views, multi-account UI, search, attachments, Meet links,
event colors (calendar colors only), free/busy lookups, Windows, push webhooks.
