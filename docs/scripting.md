# Scripting with clindar

clindar's commands print TSV when piped, take titles from stdin, and exit non-zero when there's nothing to show, so
they fit plain-text task lists, status bars and notifications. Field names and formats are in [cli.md](cli.md#output).

## Time-block a task from a plain-text list

Put the first line matching "quarterly report" in your calendar tomorrow at 9:00 for two hours:

```bash
grep -m1 'quarterly report' ~/gtd/next.txt | clindar add - --at "tomorrow 9:00" --dur 2h
```

A title read from stdin is always taken literally (a task ending in "daily" or "fri" stays as written), which is why
`--at` is required here.

## Tag the task with its event

`add` prints the new event's ID; keep it in the task line to find the event later:

```bash
id=$(grep -m1 'quarterly report' ~/gtd/next.txt | clindar add - --at "tomorrow 9:00" --dur 2h)
sed -i "0,/quarterly report/s//& @cal:$id/" ~/gtd/next.txt
```

```bash
grep -o '@cal:[a-v0-9_]*' ~/gtd/next.txt | cut -d: -f2 | xargs -r -n1 clindar show
```

(GNU sed. On macOS use `sed -i ''`, and replace `0,/…/` with a plain `/…/`, which tags every matching line.)

## Today's agenda in a daily note

```bash
clindar agenda --today --format '- [ ] {{time "15:04" .Start}} {{.Title}}' >> ~/notes/$(date +%F).md
```

```
- [ ] 09:00 Standup
```

## Status bar: the next event, or "free"

For waybar, polybar, i3blocks or tmux's `status-right`:

```bash
clindar next --format '{{.StartsInMinutes}}m {{.Title}}' 2>/dev/null || echo free
```

`next` exits 1 without printing when nothing is coming up in the next 7 days. A status bar refreshes often, so
consider `--offline` there and let the daemon keep the copy fresh (`clindar daemon install`).

## Reminders

Run this every 5 minutes from cron or a systemd timer to be told about events starting in the next 10 minutes
(it tells you on every run until they start: remembering what you were already told is up to you):

```bash
clindar agenda --within 10m --format '{{.Title}} at {{time "15:04" .Start}}' |
  while read -r line; do notify-send "Upcoming" "$line"; done
```

On macOS, replace `notify-send "Upcoming" "$line"` with
`osascript -e "display notification \"$line\" with title \"Upcoming\""`.

## Pick an event with fzf

```bash
clindar agenda --days 14 --tsv | fzf --with-nth=2,6 | cut -f1 | xargs -r clindar show
```

The same with `edit` or `rm` instead of `show` acts on the picked event.

## JSON

`--json` prints one object per line, ready for `jq`:

```bash
clindar agenda --days 7 --json | jq -r 'select(.meet_link) | "\(.start) \(.title) \(.meet_link)"'
```
