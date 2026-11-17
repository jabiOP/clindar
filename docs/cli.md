# clindar reference

`clindar` with no arguments opens the calendar (the TUI). Every other command is a plain CLI command for the shell
and scripts. First time? See [setup-google-cloud.md](setup-google-cloud.md).

- [Commands](#commands)
- [Event IDs](#event-ids)
- [Dates and times](#dates-and-times)
- [Output](#output)
- [Exit codes](#exit-codes)
- [Recurring events: scopes](#recurring-events-scopes)
- [Syncing](#syncing)
- [Configuration](#configuration)
- [The TUI](#the-tui)

## Commands

| Command | What it does |
|---|---|
| `clindar agenda` | List events (default: today and the next 6 days) |
| `clindar next` | The current or next timed event; exit status 1 if none in 7 days |
| `clindar show ID` | One event in detail |
| `clindar add …` | Create an event; prints its ID |
| `clindar edit ID …` | Change an event; only the flags you give change |
| `clindar rm ID` | Delete an event |
| `clindar rsvp ID yes\|no\|maybe` | Answer an invitation |
| `clindar calendars` | List calendars (`*` = shown) |
| `clindar sync` | Sync with Google now (`--full` re-downloads everything) |
| `clindar log` | Discarded edits and sync errors, newest first (`-n` entries, default 20) |
| `clindar auth setup FILE` / `login` / `status` / `logout` | Google login (see the setup guide) |
| `clindar daemon` / `daemon install` / `daemon uninstall` | Sync in the background |
| `clindar completion bash\|zsh\|fish` | Shell completion script |

`clindar COMMAND --help` lists every flag. The ones that need explaining:

### agenda

```
--from WHEN      start of the range (default: today)
--to WHEN        end of the range; a date includes that whole day
--days N         number of days from --from (default 7)
--today          only today
--within DUR     only events starting between now and now+DUR (e.g. 15m), for notifications
-c NAME          only this calendar (name or ID, repeatable; default: the calendars shown in clindar)
--tsv | --json | --format TEMPLATE      see Output
```

### add

Two ways to say when:

- **One line, like Google's quick add** (no `--at`): the title, then optional date, time, duration and repeat words
  **at the end** (see [Quick add](#quick-add)). No date means today; no time means all day.
- **Flags** (`--at`): the title is taken literally, and `--at`, `--end`, `--dur`, `--all-day`, `--repeat`, `--until`
  and `--count` decide the timing.

```bash
clindar add "Dentist 20/11 15:30 45m"
clindar add "Standup weekdays 9:00 15m"
clindar add "Trip 27/11-29/11"
clindar add Lunch with Sam --at "thu 13:00" --dur 1h30m --where "Main St" -c Work
echo "Write quarterly report" | clindar add - --at "tomorrow 9:00" --dur 2h
```

A title from **stdin** (`-`, or no title at all) is always literal and needs `--at`: a line from a file must never
be read as dates or repeats.

Other flags: `-c CALENDAR` (default: config `default_calendar`, else your primary calendar), `--where`, `--desc`,
`--invite EMAIL` (repeatable), `--repeat daily|weekdays|weekly|monthly|yearly|FREQ=…` (an RFC 5545 rule),
`--until DATE`, `--count N`.

### edit

Takes the same flags as `add` (`--title` for the title), plus:

- `-c CALENDAR` moves the event to another calendar. You must own it (not an invitation) and both calendars must be
  writable. A recurring event moves as a whole series (`--scope all`): Google can't move one occurrence.
- `--scope this|following|all` for recurring events (see [Scopes](#recurring-events-scopes)).

```bash
clindar edit p7k747j11cdmis22lolr91ec0g --at "fri 16:00"          # move it, keeping its length
clindar edit dj4ua654k5usovm0jind8gtuck_20261120T080000Z --title Retro --scope this
clindar edit u2fmncdfi51263eeovhik143ck -c Work                   # to another calendar
```

`--at` moves the start and keeps the length unless you also give `--end` or `--dur`.

### rm, rsvp

`clindar rm ID` deletes at once (there is no undo). `clindar rsvp ID yes|no|maybe` answers an invitation;
recurring invitations take `--scope this|all`.

## Event IDs

Commands take the `id` printed by `add` and shown by `agenda --tsv`/`--json`. An occurrence of a recurring event
has its own ID: the series ID plus `_` and its start (`dj4u…_20261120T080000Z`, or `_20261120` for all-day series).

An event you can see in **two calendars** (for example you invited someone whose calendar is shared with you) is
stored once per calendar under the same ID. A bare ID picks the copy that fits the action: for `show`, `edit` and
`rm` the one you can edit, for `rsvp` the one where you're invited. To name a copy, write `CALENDAR/ID`, with the
calendar's name or ID:

```bash
clindar show Work/u2fmncdfi51263eeovhik143ck
```

If two copies fit equally, clindar refuses and the error lists the `CALENDAR/ID`s to choose from.

## Dates and times

Wherever a command takes a time (`--at`, `--from`, `--to`, `--until`):

| You write | Means |
|---|---|
| `now` | now |
| `today`, `tomorrow`, `yesterday` | that day |
| `fri`, `friday` | the next such day; today counts |
| `+3d`, `+1w` | that many days or weeks ahead |
| `20/11`, `20/11/2027` | day/month (set `date_order = "mdy"` for month/day) |
| `2026-11-20` | that day |
| `14:00`, `2pm`, `9:30am` | today at that time |
| `fri 2pm`, `20/11 15:30`, `2026-11-20 09:15`, `2026-11-20T09:15` | a day and a time |
| `in 2h`, `+90m` | from now (needs `h` or `m`) |
| `2026-11-20T09:15:00Z` | an exact instant, shown in your time zone |

A day without a time means **all day**. Names are English only.

How `--at`, `--end` and `--dur` combine:

- `--end` is read relative to the start: `--at "fri 14:00" --end 15:30` ends Friday at 15:30. An end before the
  start is refused (a likely typo); for an event past midnight, give the day: `--at "fri 22:00" --end "sat 01:00"`.
  (Quick add's `22:00-01:00` and the TUI form's End field do read that as the next day.)
- For all-day events, `--end` is the **last day, inclusive**: `--at fri --end sun` is Friday to Sunday.
- The default length is 1 hour, or 1 day for all-day events. Give an end or a duration, not both.
- Durations: `45m`, `1h30m`, `2d`.

### Quick add

In `clindar add "…"` without `--at`, and in the TUI's quick add, these words are read **only at the end of the
line**, in any order, each at most once. Reading stops at the first word that isn't one; the rest is the title, so
`Report Q3 2026` keeps its title.

| Word | Examples | Notes |
|---|---|---|
| date | `fri`, `tomorrow`, `20/11`, `2026-11-20`, `+3d` | as in the table above |
| date range | `27/11-29/11`, `fri-sun` | all day; the last day is included |
| time | `13:00`, `9am` | |
| time range | `9:00-10:00`, `22:00-01:00` | an end at or before the start is the next day |
| duration | `45m`, `1h30m`, `2d` | whole days without a time: all day for N days |
| repeat | `daily`, `weekdays`, `weekly`, `monthly`, `yearly` | |

A duration under a day needs a time (`Nap 14:00 30m`, not `Nap 30m`); a date range can't have a time. A title that
*ends* in one of these words is read as that word (`Read the daily` repeats daily); the TUI's live preview shows it
before you save.

## Output

On a terminal, commands print a readable layout:

```
$ clindar agenda --days 4
Tue 17 Nov
  09:00–09:15  Standup  ● Me
Wed 18 Nov
  09:00–09:15  Standup  ● Me
  09:00–11:00  Write quarterly report  ● Me
Thu 19 Nov
  09:00–09:15  Standup  ● Me
  13:00–14:30  Lunch with Sam  ● Work
```

**Piped**, they print TSV, one event per line, so `clindar agenda | …` just works. `--tsv`, `--json` and `--format`
choose explicitly.

**TSV** columns: `id`, `start`, `end`, `all_day` (`1`/`0`), `calendar` (its name), `title`.

```
dj4ua654k5usovm0jind8gtuck_20261120T080000Z	2026-11-20T09:00:00+01:00	2026-11-20T09:15:00+01:00	0	Me	Standup
```

**JSON** (`--json`) is JSON Lines, one object per event:

| Field | Meaning |
|---|---|
| `id` | the event's ID (an occurrence's own ID for recurring events) |
| `series_id` | the series an occurrence belongs to |
| `calendar_id`, `calendar` | the calendar's ID and name |
| `title` | |
| `start`, `end` | RFC 3339 in your time zone, or `YYYY-MM-DD` if `all_day`; `end` is exclusive |
| `all_day` | `true` for all-day events |
| `status` | `confirmed`, `tentative` or `cancelled` |
| `response` | your answer, if you're invited: `needsAction`, `accepted`, `declined`, `tentative` |
| `location`, `description` | |
| `attendees` | `[{email, name, response}]` |
| `recurring` | `true` for occurrences of a series |
| `link` | the event in Google Calendar's web page |
| `starts_in_minutes` | only from `clindar next` |
| `color` | the color it's drawn in (`#rrggbb`): its own, else its calendar's |
| `meet_link` | its Google Meet URL, if it has one |

Empty fields are left out.

**Templates** (`--format`) are Go templates over the JSON fields, in Go's names: `.ID`, `.Title`, `.Start`, `.End`,
`.AllDay`, `.Calendar`, `.StartsInMinutes`, `.MeetLink`, … The function `time` reformats `.Start` or `.End` with a
Go layout:

```bash
clindar agenda --today --format '- [ ] {{time "15:04" .Start}} {{.Title}}'
```

## Exit codes

| Code | Meaning |
|---|---|
| 0 | done |
| 1 | an error (printed on stderr), or `clindar next` with nothing in the next 7 days (silent) |
| 2 | the command was called wrong (bad flag or value); nothing was changed |

## Recurring events: scopes

Editing or deleting one occurrence of a series asks which ones (`--scope` in the CLI, a pop-up in the TUI):

| Scope | Edit | Delete |
|---|---|---|
| `this` | only this occurrence | only this occurrence |
| `following` | this and later ones: the series is split here | this and later ones: the series ends before it |
| `all` | the whole series; time changes shift every occurrence | the whole series |

`following` on the first occurrence means `all`. Answering an invitation supports `this` and `all`. Moving to another
calendar is `all` only.

## Syncing

clindar works from a local copy of your calendars and syncs with Google:

- **Reads** (`agenda`, `next`, `show`) sync first when the last sync is older than `stale_after`, waiting at most
  `sync_timeout`. `--offline` skips that.
- **Writes** (`add`, `edit`, `rm`, `rsvp`) are saved locally at once, then sent. If Google can't be reached, they wait
  and go with the next sync; the warning says whether the change reached Google. `--offline` only saves.
- The **TUI** syncs every `sync_interval`, and within seconds after each change.
- If the event changed on Google before your edit arrived, Google's version wins and `clindar log` says so.

`clindar daemon install` keeps the copy fresh in the background: a systemd user service on Linux
(`~/.config/systemd/user/clindar.service`), a launchd agent on macOS (log in `~/Library/Logs/clindar.log`).
`daemon install` writes the file and prints the command that starts it. After upgrading clindar, restart it so it
runs the new version: `systemctl --user restart clindar` (Linux), or `launchctl unload` then `launchctl load` the
`.plist` it printed (macOS).

## Configuration

`~/.config/clindar/config.toml` (`$XDG_CONFIG_HOME/clindar/` if set). Every key is optional:

| Key | Default | Meaning |
|---|---|---|
| `sync_interval` | `"20s"` | how often the daemon and the TUI sync (at least `10s`) |
| `stale_after` | `"2m"` | CLI reads sync first when the last sync is older than this |
| `sync_timeout` | `"5s"` | how long a CLI command waits for that sync |
| `default_calendar` | your primary calendar | where new events go (name or ID) |
| `week_start` | `"monday"` | or `"sunday"` |
| `send_updates` | `"all"` | email guests about changes, as Google asks on its web page; `"none"` never does |
| `date_order` | `"dmy"` | how `20/11` is read; `"mdy"` reads `11/20` |
| `mouse` | `false` | mouse in the TUI (select text with Option-drag on macOS, Shift-drag elsewhere) |

```toml
default_calendar = "Work"
week_start = "sunday"
mouse = true
```

Files: the config and your OAuth client (`client.json`) in `~/.config/clindar/`; the local copy (`clindar.db`) in
`~/.local/share/clindar/` (`$XDG_DATA_HOME/clindar/`). `CLINDAR_CONFIG_DIR` and `CLINDAR_DATA_DIR` override the two
folders. Your login token is in the system keyring, or in a private file in the data folder without one.

## The TUI

`clindar` opens a 4-week grid; `Tab` switches to an endless list. `?` shows the keys anywhere.

**Moving around**

| Key | |
|---|---|
| `h` `l` / `←` `→` | previous / next day |
| `j` `k` / `↓` `↑` | next / previous week (grid), event (list) |
| `J` `K` | 4 weeks forward / back (grid), a page (list) |
| `t` | today |
| `g` | go to: `fri`, `20/12`, `dec` (its 1st), `15` (the next 15th), `+2w`, `2026-12-24` |
| `Tab` | grid ⇄ list |
| `Enter` | on a grid day: pick its events (event mode: `j`/`k`, `Enter` for details, `Esc` back) |
| `c` | choose which calendars show (only in clindar; Google's own setting is untouched) |
| `s` | sync now |
| `q` | quit |

**Events** (in the list, event mode, or the details box)

| Key | |
|---|---|
| `Enter` | details: `o` opens it in Google, `m` joins its Meet call, `y` copies the Meet link |
| `a` | add (quick add) |
| `e` | edit (the form) |
| `dd` | delete; a one-off goes at once, a recurring one asks the scope |
| `r` | answer an invitation |
| `C` | color: a palette of the calendar's colors (`h`/`j`/`k`/`l`, `Enter`) |

`y` copies through the terminal (OSC 52): it works in iTerm2 (allow it in *Settings → General → Selection*), kitty,
WezTerm and tmux (`set -g set-clipboard on`), not in macOS Terminal.app.

**Quick add** (`a`): type the event as one line (see [Quick add](#quick-add)); the line below shows what will be
created. `Enter` saves, `Tab` opens the full form with it, `Esc` cancels.

**The form**: `Tab`/`Ctrl-J` next field, `Shift-Tab`/`Ctrl-K` previous. On a choice (All-day, Calendar, Repeat,
Color), `h`/`l` change it and `Space` opens its list, where typing filters and `Enter` chooses; the Color list is a
grid grouped by color family. `Enter` saves from any field, `Ctrl-S` too, even with a list open. `Esc` closes an
open list; on a changed form, `Esc` then `Enter` discards and `Esc` `Esc` keeps editing. The Calendar can change on an
existing event when it may move (see `edit -c`).

**Mouse** (`mouse = true`): click a day to select it, click it again to pick its events, double-click it to add an
event; drag across days to add one spanning them. Click an event for its details; drag it to another day, or drag its
`◂`/`▸` edge to change its first or last day. Right-click an event, or press and hold it, for its color palette
(iTerm2 keeps the right button for its own menu). The wheel does nothing.
