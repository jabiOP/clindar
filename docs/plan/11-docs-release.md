# Phase 11: Docs, release, final check

No new behavior. The work here is writing documentation users can follow, a reproducible build, and one full end-to-end pass.

---

## Task 11.1: `docs/setup-google-cloud.md`

**Goal:** a stranger can go from nothing to `clindar auth login` in about 10 minutes. Write it as numbered steps, and
**follow it yourself in a fresh Google Cloud project** while writing. Screens change, so describe each screen by its title, not
by button position.

Content to cover, in order:
1. Go to <https://console.cloud.google.com/>, create a project (e.g. "clindar").
2. "APIs & Services → Library": enable **Google Calendar API**.
3. "Google Auth Platform" (formerly "OAuth consent screen"): set the app name and support email, audience
   **External**, and add **your own Google address as a test user**. Explain that while the app is in "Testing", Google
   may expire refresh tokens after 7 days. The user can fix that by clicking "Publish app". Unverified apps for personal use
   only show a warning screen, which is fine.
4. "Clients → Create client": type **Desktop app**. Download the JSON.
5. `clindar auth setup ~/Downloads/client_secret_XXXX.json`, then `clindar auth login`.
6. Troubleshooting: "access blocked" means you aren't a test user. `redirect_uri_mismatch` means the client type isn't Desktop.
   Workspace accounts may need an admin to allow the app.

**Commit:** `docs: Google Cloud setup guide`

---

## Task 11.2: `docs/cli.md`

**Goal:** a reference for script writers. Generate the per-command flag lists from `clindar <cmd> --help` (paste
them), then add by hand:
- The **time formats** table from Task 1.7, and the span rules from Task 1.10.
- **Output contracts:** the TSV column order, every `EventJSON` field with its meaning (copy the comments
  from `internal/output/output.go`), and the template function `time`.
- **Exit codes:** 0, 1, and 2, and that `next` exits 1 silently.
- **Scopes:** what this, following, and all do (design §4).
- **Config** keys with their defaults (Task 1.9), plus `CLINDAR_CONFIG_DIR` and `CLINDAR_DATA_DIR`.

Keep it accurate: every example in this file must be one you ran.

**Commit:** `docs: CLI reference`

---

## Task 11.3: `docs/scripting.md`, the integration recipes

**Goal:** show how clindar fits a plain-text GTD system (design Q6–7) and the desktop. **Run every recipe**
before committing. Suggested recipes:

```bash
# Time-block a task line from a GTD file
grep -m1 'quarterly report' ~/gtd/next.txt | clindar add - --at "tomorrow 9:00" --dur 2h

# Tag the task line with the event ID, so you can find it later
id=$(grep -m1 'quarterly report' ~/gtd/next.txt | clindar add - --at "tomorrow 9:00" --dur 2h)
sed -i "0,/quarterly report/s//& @cal:$id/" ~/gtd/next.txt

# Today's agenda as task-style lines in a daily note
clindar agenda --today --format '- [ ] {{time "15:04" .Start}} {{.Title}}' >> ~/notes/$(date +%F).md

# Status bar (waybar/polybar/tmux): next event or "free"
clindar next --format '{{.StartsInMinutes}}m {{.Title}}' 2>/dev/null || echo free

# Reminders via cron or a systemd timer every 5 minutes (exactly-once handling is up to you)
clindar agenda --within 10m --format '{{.Title}} at {{time "15:04" .Start}}' |
  while read -r line; do notify-send "Upcoming" "$line"; done

# Pick an event with fzf and open it
clindar agenda --days 14 --tsv | fzf --with-nth=2,6 | cut -f1 | xargs -r clindar show
```

**Commit:** `docs: scripting recipes`

---

## Task 11.4: README and release build

**Goal:** a README that sells the tool in one screen, and a reproducible build.

README sections: what clindar is (2 sentences); install (`go install github.com/jabiOP/clindar@latest`; while the repo is private, users need `GOPRIVATE=github.com/jabiOP/*` and git access to it); first
run (link to the setup guide, then `clindar auth login`, `clindar`); daemon (`clindar daemon install`); links to `docs/cli.md`
and `docs/scripting.md`; a privacy note (data stays in `~/.local/share/clindar` and Google, and tokens live in the keyring).

Release build (document it in the README's "Building" section):
```bash
VERSION=v0.1.0
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o dist/clindar-${target%/*}-${target#*/} .
done
```
`CGO_ENABLED=0` works because every dependency is pure Go (that's why we chose `modernc.org/sqlite`).
One exception: go-keyring on macOS shells out to `/usr/bin/security`, which needs no cgo.

**Commit:** `docs: README and release instructions`

---

## Task 11.5: Final end-to-end checklist

Do this on a clean account directory (`CLINDAR_CONFIG_DIR`/`CLINDAR_DATA_DIR` pointing at new temp dirs) with the
"clindar-test" calendar. Tick every box in `DEVIATIONS.md` or file an issue for it.

- [ ] Setup guide followed from scratch works.
- [ ] `clindar` opens the grid. Today is marked. `J/K` scrolls and `t` returns to today.
- [ ] Add, edit, delete a one-off event in the TUI. The web UI reflects each one within a sync.
- [ ] Create a weekly series. Edit one instance (this), then from the 3rd instance on (following), then all. Delete with each scope.
      The web UI matches after each step.
- [ ] RSVP to an invite (have a second account invite you). The organizer sees the response.
- [ ] **Offline:** disconnect the network, add and edit events (the TUI shows "N pending"), reconnect, and they sync.
- [ ] **Conflict:** edit offline, change the same event in the web UI, and reconnect. The web version wins, and `clindar log` and the TUI
      status bar report the discarded edit.
- [ ] Daemon installed. Kill the network for 5 minutes and check that the logs show backoff, then recovery.
- [ ] `clindar agenda | cat` is TSV, `--json` is valid JSON Lines (`| jq .`), and `clindar next` exits 1 when nothing is upcoming.
- [ ] Every recipe in `docs/scripting.md` works.
- [ ] `go test -race ./... -count=1`, `go vet ./...`, and `gofmt -l .` are all clean.

**Commit:** `chore: v0.1.0` and tag it: `git tag v0.1.0`
