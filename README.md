# clindar

A fast terminal client for Google Calendar: a calendar you drive with the keyboard (a scrollable 4-week grid and an
endless agenda list), and a Unix-friendly CLI for scripts, plain-text task lists and status bars. Edits work offline
and sync in the background.

```
$ clindar agenda --days 2
Tue 17 Nov
  09:00–09:15  Standup  ● Me
Wed 18 Nov
  09:00–09:15  Standup  ● Me
  09:00–11:00  Write quarterly report  ● Me
```

## Install

Needs Go 1.26 or newer.

```bash
go install github.com/jabiOP/clindar@latest
```

While the repository is private, Go needs to know it's private, and git needs access to it:

```bash
GOPRIVATE=github.com/jabiOP/* go install github.com/jabiOP/clindar@latest
```

Linux and macOS are supported.

From a clone of the repository, `make install` builds and installs it, and restarts the daemon (if you installed
one) so it runs the new version. To update: `git pull && make install`.

Go installs programs in `~/go/bin` (or `$GOBIN`); if `clindar` isn't found afterwards, add that folder to your PATH:
`echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc` (or `~/.bashrc`), then open a new terminal.

## First run

1. Create your Google OAuth client (about 10 minutes, once): [docs/setup-google-cloud.md](docs/setup-google-cloud.md).
2. `clindar auth setup ~/Downloads/client_secret_*.json`
3. `clindar auth login` (opens your browser, then does a first sync)
4. `clindar` opens the calendar; `?` shows the keys.

To keep your calendars fresh in the background, and edits flowing to Google even when no clindar is open:

```bash
clindar daemon install
```

It writes a systemd user service (Linux) or a launchd agent (macOS) and prints the command that starts it.

## Documentation

- [docs/cli.md](docs/cli.md): every command, date format, output field, config key and TUI key.
- [docs/scripting.md](docs/scripting.md): recipes for task lists, daily notes, status bars, reminders and fzf.
- [docs/plan/](docs/plan/README.md): how clindar was designed and built.

## Privacy

Your calendar data stays on your computer (`~/.local/share/clindar/`) and in your Google account; clindar talks to
nothing else. Your login token is kept in the system keyring (or a private file in that folder where there is none),
and the OAuth client is your own.

## Building

```bash
make install   # go install ., then restart the daemon on the new version
make test      # go vet and go test -race
```

Release binaries for Linux and macOS, on any machine (every dependency is pure Go, so no C compiler is needed):

```bash
VERSION=v0.1.0
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o dist/clindar-${target%/*}-${target#*/} .
done
```

`clindar --version` prints the version.
