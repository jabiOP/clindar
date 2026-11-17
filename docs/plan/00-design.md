# clindar: Design

> Status: Section 1 (architecture) was reviewed and approved. Sections 2–7 were written during
> planning and capture decisions made in the design conversation. Review them before starting
> Phase 2.

## 0. Decisions log (from the design conversation)

| # | Question | Decision |
|---|----------|----------|
| 1 | How much can the app change? | **Full read/write**: create/edit/delete/move, RSVP, invite attendees |
| 2 | Where does data live? | **Local SQLite cache with background sync**; offline edits queue in an outbox |
| 3 | What runs sync? | **Optional daemon** (`clindar daemon`, polls every ~20s). Without it, the TUI syncs while open and CLI reads sync when the cache is stale |
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

**Orphan instance:** an occurrence whose series isn't in your calendar (someone invited you to one occurrence
of their recurring meeting). It is stored like an exception but shown and edited as a one-off event.

**IDs are generated locally** (base32hex, which Google accepts for client-supplied IDs). An event created offline
therefore already has its final ID, and no temp-ID remapping is needed.

SQLite stores each event as indexed columns (`start_ts`, `end_ts`, `series_id`, `original_ts`,
`is_master`, `status`) plus a JSON blob of the full `model.Event`. Adding a field means changing
the struct and nothing else.

**One event, two calendars.** An event you can see in two calendars (e.g. you invited someone whose calendar is
shared with you) is stored once per calendar under the same ID. Every command and service method takes either a bare
ID or `CALENDAR/ID` (calendar ID or name). A bare ID picks the copy that fits the action: for show/edit/move/delete,
the one you can edit (writable, not an invitation); for RSVP, the one where you're invited. A tie is refused, and the
error names the `CALENDAR/ID`s to use. The TUI always says which copy it means.

## 3. Sync

Every sync cycle **pushes first, then pulls**.

**Push** sends outbox ops to Google in sequence order. There is at most one pending op per event, because
`Enqueue` coalesces them: insert+patch becomes insert, patch+patch becomes patch, insert+delete becomes nothing, and patch+delete becomes delete.
Each op carries `If-Match: <etag>` when one is known. (Since Phase 15 the rule is "one pending op per event *and
calendar*": a move adds a second one; see **Move** below.)

| Result | Action |
|---|---|
| 2xx | Store the returned event (new etag) and complete the op |
| 409 on insert | Already created by an earlier interrupted push, so count it as success |
| 404/410 on patch/delete | The event is gone, so drop the op and delete it locally |
| **412** | **Google wins**: drop the op, log `discarded_edit`, re-fetch the event |
| other 4xx | Permanent: drop the op and log an error |
| 401 | Login problem: transient. Keep everything queued, and tell the user to `auth login` if it persists |
| network/5xx/429, 403 rate/quota limits | Transient: keep the op, increment `attempts`, stop pushing this cycle |

When an op is dropped (412/404/permanent), the engine re-fetches the event to restore Google's version. If that
re-fetch is itself refused (403), the op is still dropped, so one broken event can't block the queue.

**Move** (Phase 15) is a separate op kind: `events.move` from the source calendar (which keys the op) to a
`destination`. Google keeps the ID and gives a new etag; the old calendar then lists the event as cancelled and the
new one as new. Locally the rows change calendar at once, in the transaction that queues the op. A move is never
merged into another op: an edit made before it stays on the source calendar and is pushed first; an edit made after
it is keyed by the destination and pushed after (with the move's new etag). A move after an unpushed insert just
re-addresses the insert. Until the move lands, the source calendar's pull skips the event and its exceptions, and a
full resync of the destination keeps them.

**RSVP** is a separate op kind: it patches only the attendee list and sends no If-Match, so it can't conflict
with (or overwrite) the organizer's edits.

If the user edits an event *while* its op is in flight, the op's `version` changes and the
completed push does not delete it. It is rewritten as a patch against the new etag.

**Pull:** `calendarList.list` (with sync token), then `events.list` per calendar with
`syncToken`, `showDeleted=true`, `singleEvents=false`. Masters arrive with their RRULEs, and
recurrence is **expanded locally** (`rrule-go`), so offline-created series display correctly.
Events with a pending outbox op are skipped, because local wins until pushed. A `410 Gone`
clears that calendar's cached events and does a full resync.

**New fields need a resync.** Incremental sync only resends events that changed. So whenever `FromAPI` starts
reading a field it ignored before, a migration resets the sync tokens once (migration 4 did it for `colorId` and Meet
links); otherwise events cached earlier never get the field.

**Freshness:** daemon every `sync_interval` (default 20s) with exponential backoff up to 5 minutes when offline.
After a local change, a sync that leaves it pending (the daemon held the sync lock) is retried: the TUI every 3s, a
CLI write by waiting for the lock within `sync_timeout`.
The TUI syncs on open, on the same interval, and after edits. CLI reads sync first if the last sync is older than
`stale_after` (default 2m), bounded by `sync_timeout` (default 5s). `--offline` skips this.

## 4. Recurrence edits

| Scope | Edit | Delete |
|---|---|---|
| this | Write an exception with the instance's ID; PATCH the instance ID | Store a cancelled exception; DELETE the instance ID |
| following | Truncate the master (`UNTIL` = just before the split; `COUNT` converted) and INSERT a new master from the split point with the changes | Truncate only |
| all | PATCH the master; time changes are applied as a *shift* | DELETE the master |

If "following" is chosen at the first instance, it becomes "all". RSVP supports *this* and *all*.
Orphan instances (§2) have no series here, so they take no scope.

**Changing the calendar** (Phase 15) moves a one-off or a **whole series** (scope *all* only, exceptions included).
One occurrence can't move on its own: Google refuses it, so neither *this* nor *following* is offered, and an orphan
instance can't move either. You must own the event (not an invitation), and both calendars must be writable.

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

## 6b. Mouse (Phase 12)

Off by default (`mouse = true` in `config.toml`). While on, clindar captures the mouse; select text with Option-drag in iTerm2, fn-drag in macOS Terminal, Shift-drag elsewhere.

- **Clicks:** click a day to select it, and click the selected day again to open the list there. Click an event to open
  its details, with [Edit] [Delete] [RSVP] [Close]. In the list, click to select and click again to open. Click
  `+N more` to open the list at that day. *(The wheel was here, then slowed in §6c, then turned off in §6d.)*
- **Buttons:** every pop-up choice is clickable (scope, delete confirm, RSVP, form [Save] [Cancel]). Form fields are
  clickable. The status bar has [+ Add] [⟳ Sync] [? Help]. Click outside the detail, help or go-to pop-ups to close them.
- **Drag (grid only):** the leftmost cell of an event's first-day line is its start handle; the rightmost cell of its
  last-day line is its end handle. The `◂`/`▸` symbols are drawn only while the pointer is on that cell (hover,
  needs "all motion" mouse mode) or while that handle is being dragged, so titles stay full at any width. The zones
  work even in terminals without hover reporting.

  | Grab | Drop on day D | Result |
  |---|---|---|
  | body | any day | move the whole event by D − grabbed day; clock times and length kept |
  | `▸` | D ≥ first day | last day becomes D (end clock time kept) |
  | `◂` | D ≤ last day | first day becomes D (start clock time kept) |

  Handles clamp at a one-day event instead of flipping it. A drop that would end before it starts is refused.
  The target day is marked `»` and the status bar previews the result. Esc cancels. Recurring instances get the
  scope picker. Read-only calendars and invitations can't be dragged (clicking opens details with the reason).
- **Detail pop-up offers only possible actions:** [Edit] needs a writable calendar and not being a guest; [Delete] a
  writable calendar; [RSVP] an invitation. The keys `e`/`d`/`r` follow the same rules and explain refusals.
- **No double-click, right-click or undo** (see Out of scope).

## 6c. UX round 1 (Phase 14)

Decisions from real use (brief "UX round 1"):

- **Keys:** vim-like everywhere new; arrows keep working. `J`/`K` move 4 weeks in the grid and page in the list.
  `Ctrl-J`/`Ctrl-K` move between form fields.
- **Event mode:** `Enter` on a grid day makes its events selectable (`j`/`k`, `Enter` details, `e`/`d`/`r` with the
  details box's permission rules, `Esc` back). The list stays on `Tab`; `Esc` in the list returns to the grid.
- **List:** endless in both directions; loads ahead so a screen and a page are always ready.
- **Past days** are dimmed with color only (no change with colors off).
- **Mouse:** grabbing an edge never opens details. Click a day to select it, double-click to add (§6e); drag across empty days to add an
  all-day span. *(The wheel was slowed here to one day or event per step, at most one per 100 ms; §6d turned it off.)*
- **Dates:** slashes are day/month (`date_order = "mdy"` flips it); English names only. Go-to takes days only: a bare
  number is the next such day of the month, a month name is its 1st (this month included); times are refused.
- **Adding:** quick add is the default, a Google-style line whose date/time/range/duration/repeat tokens come at the
  end (`internal/quickadd`, shared by CLI and TUI), with a live preview. The full form has Date + All-day + Start +
  End/Duration, pickers for Calendar (writable only), Repeat and Color, and errors under their field. New events default
  to all-day on the selected day.
- **Calendars:** which calendars show is a local setting (`c` menu), overriding Google's `selected`, never written to
  Google; the CLI's default agenda follows it.
- **One picker component** for every choice (calendar menu, form pickers).
- **Per-event colors:** Google's `colorId` and palette (cached); drawn in grid and list; settable in the form; JSON field
  `color` (effective color).
- **Details:** no ID or raw link in the TUI; [Open in Google] (`o`); Meet links shown, with [Join] (`m`) and [Copy] (`y`,
  OSC 52; not in macOS Terminal.app). JSON field `meet_link`.

## 6d. After UX round 1 (Phase 15)

- **Form keys:** `Enter` always saves, except inside an open list, where it chooses. `Space` opens a list. `Ctrl-S`
  and [Save] save from anywhere (with a list open, they first take the highlighted option).
- **Esc on a changed form** asks before discarding: `Esc` then `Enter` discards; `Esc` `Esc` (or any other key)
  keeps editing. The hint line says so. An untouched form closes at once; an open list closes first. [Cancel] is Esc.
- **Stdin titles are literal:** `clindar add -` needs `--at`; only a title typed on the command line is read like
  quick add.
- **After a CLI write**, a failed sync says whether the change reached Google ("sent to Google; refreshing timed
  out") or not ("saved here; not sent to Google yet").
- **The mouse wheel is off.** Trackpad swipes made it unusable even throttled. Keys scroll; clicks and drags stay.
- **Changing an event's calendar** (§4): the edit form's Calendar picker is unlocked when the move is allowed and lists
  writable calendars only; otherwise it's locked with the reason. On an occurrence, saving a new calendar offers only
  "all" and says why. The CLI: `clindar edit ID -c NAME`. Dragging never changes the calendar. Guests are told as
  for any edit (`send_updates`), which is what Google Calendar's own "send update emails?" question does.

## 6e. UX round 2

- **Event colors are Google's labels** (since Google's June 2026 change): each calendar has up to 200 color labels
  (the 24 defaults plus custom ones), read with `calendars.get` (`labelProperties.eventLabels`) and refetched when an
  event uses an unknown one. An event's `eventLabelId` supersedes its classic `colorId`. The Color picker offers the
  chosen calendar's labels as a grid, one row per color family (reds … greys, light to dark), h/j/k/l to move,
  typing filters; default colors show Google's names (the API gives them none); a calendar without labels offers the 11 classic colors.
  Writes that set or clear a label use `eventLabelVersion=1` and always send `eventLabelId`; others stay on
  `colorId`. Colors are drawn as the API reports them (no remapping).
- **`dd` deletes** (list, event mode, details), at once for a one-off, like vim; `d` then another key does nothing.
  A click on [Delete] still asks "(y/n)" (no undo).
- **The selected event keeps its color:** highlighted with its color as background and black or white text.
- **Mouse:** one click on a day selects it; a double click (within 400 ms) adds an event there; dragging across days
  still adds a span.
- **Quick color palette:** right-click an event (grid or list), press and hold it (0.5 s, for terminals like iTerm2
  that keep the right button for their own menu), [Color] in its details, or `C` in the list, event mode or details: a
  grid of the calendar's colors as dots, 9 per row in hue order, plus "Calendar default", like Google Calendar's
  right-click menu. A click or Enter applies the color at once (recurring events ask for a scope).
- **`?` anywhere** shows help; closing it returns to the details box or event mode it was opened from.

## 7. Security & config

- OAuth: desktop loopback flow with PKCE, scope `https://www.googleapis.com/auth/calendar`.
- Tokens are stored in the OS keyring (`zalando/go-keyring`), or in a `0600` file in the data dir if no keyring is available.
- The TUI add form and `clindar add` both default to `default_calendar`.
- Client JSON goes in `$XDG_CONFIG_HOME/clindar/client.json`. `auth.BuiltinClientID`/`BuiltinClientSecret` can be set via `-ldflags`.
- Config (`config.toml`): `sync_interval`, `stale_after`, `sync_timeout`, `default_calendar`,
  `week_start`, `send_updates`, `mouse`, `date_order`. Every key is optional.
- Env overrides: `CLINDAR_CONFIG_DIR`, `CLINDAR_DATA_DIR` (used heavily by tests).

## Out of scope for v1 (YAGNI)

Undo (even for drags; a single-step undo of the last drag is the natural first version later),
desktop notifications, day/week/month views, multi-account UI, search, attachments, **creating** Meet links
(clindar shows existing ones), free/busy lookups, Windows, push webhooks, Spanish (or any non-English) date words,
a CLI command to change calendar visibility.

*Moved into scope in UX round 1 (§6c): per-event colors, and reading Meet links.*
