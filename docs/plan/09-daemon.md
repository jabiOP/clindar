# Phase 9: Daemon

`clindar daemon` syncs every `sync_interval` (45s by default) so the cache stays fresh for status bars and scripts
(design Q3). It's **optional**: without it, the CLI and TUI sync on their own.

---

## Task 9.1: The loop

**Goal:** sync, wait, repeat. On failure, back off by doubling the wait, capped at 5 minutes, so a laptop that's offline doesn't
spin. Reset the wait on success. Stop promptly when the context is cancelled (SIGTERM from systemd/launchd).

**Testing time without sleeping:** the loop takes `after func(time.Duration) <-chan time.Time`.
Production passes `time.After`. The test passes a fake that records the requested wait and fires at
once. The test then asserts the *sequence of waits*, which is exactly the backoff behavior.

**A real bug this test found:** when both `ctx.Done()` and the timer are ready, Go's `select` picks one **at
random**, so the loop could run one extra sync after cancellation. That's why the loop checks
`ctx.Err()` at the top. Run the test with `-count=20` to see that it's stable.

**Files:** create `internal/daemon/daemon_test.go`, `internal/daemon/daemon.go`

**Steps**
1. Test:
```go
package daemon_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/daemon"
)

// fakeAfter records requested waits and fires immediately, so the test never sleeps.
type fakeAfter struct{ waits []time.Duration }

func (f *fakeAfter) after(d time.Duration) <-chan time.Time {
	f.waits = append(f.waits, d)
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

func TestLoopBacksOffOnErrorsAndResets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	results := []error{nil, errors.New("offline"), errors.New("offline"), errors.New("offline"), nil}
	calls := 0
	sync := func(context.Context) error {
		err := results[calls]
		calls++
		if calls == len(results) {
			cancel()
		}
		return err
	}
	fa := &fakeAfter{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	daemon.Loop(ctx, 45*time.Second, 2*time.Minute, sync, fa.after, log)

	// ok → 45s; three failures double up to the 2m cap; success resets to 45s.
	want := []time.Duration{45 * time.Second, 90 * time.Second, 2 * time.Minute, 2 * time.Minute, 45 * time.Second}
	if len(fa.waits) != len(want) {
		t.Fatalf("waits = %v, want %v", fa.waits, want)
	}
	for i := range want {
		if fa.waits[i] != want[i] {
			t.Errorf("waits = %v, want %v", fa.waits, want)
			break
		}
	}
}
```
2. Run: compile failure.
3. Implement:
```go
// Package daemon runs sync on a timer and installs itself as a user service.
package daemon

import (
	"context"
	"log/slog"
	"time"
)

// Loop calls sync every interval until ctx is cancelled. After a failure it
// waits longer each time (doubling, capped at maxBackoff) so an offline
// laptop doesn't spin; the first success resets the wait.
//
// after is time.After in production; tests pass a fake to avoid sleeping.
func Loop(ctx context.Context, interval, maxBackoff time.Duration, sync func(context.Context) error,
	after func(time.Duration) <-chan time.Time, log *slog.Logger) {
	wait := interval
	// Check ctx first: when both channels in the select below are ready, Go
	// picks one at random, so the select alone can't guarantee we stop.
	for ctx.Err() == nil {
		if err := sync(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			wait = min(wait*2, maxBackoff)
			log.Warn("sync failed", "err", err, "retry_in", wait)
		} else {
			wait = interval
		}
		select {
		case <-ctx.Done():
			return
		case <-after(wait):
		}
	}
}
```
4. `go test -race -count=20 ./internal/daemon/`: pass every time.

**Commit:** `feat(daemon): sync loop with backoff`

---

## Task 9.2: Service files

**Goal:** `clindar daemon install` writes a systemd **user** unit on Linux or a launchd agent on macOS, then
**prints** the command to start it. We don't run `systemctl`/`launchctl` ourselves: the user stays in control,
and the code stays simple.

References: `man systemd.service`, `man systemd.unit` (user units live in `~/.config/systemd/user/`);
`man launchd.plist`.

**Files:** create `internal/daemon/install.go`; extend `internal/daemon/daemon_test.go`

**Steps**
1. Add `TestUnitsRunTheGivenBinary` (and the `strings` import). The full file is now:
```go
package daemon_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/daemon"
)

// fakeAfter records requested waits and fires immediately, so the test never sleeps.
type fakeAfter struct{ waits []time.Duration }

func (f *fakeAfter) after(d time.Duration) <-chan time.Time {
	f.waits = append(f.waits, d)
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

func TestLoopBacksOffOnErrorsAndResets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	results := []error{nil, errors.New("offline"), errors.New("offline"), errors.New("offline"), nil}
	calls := 0
	sync := func(context.Context) error {
		err := results[calls]
		calls++
		if calls == len(results) {
			cancel()
		}
		return err
	}
	fa := &fakeAfter{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	daemon.Loop(ctx, 45*time.Second, 2*time.Minute, sync, fa.after, log)

	// ok → 45s; three failures double up to the 2m cap; success resets to 45s.
	want := []time.Duration{45 * time.Second, 90 * time.Second, 2 * time.Minute, 2 * time.Minute, 45 * time.Second}
	if len(fa.waits) != len(want) {
		t.Fatalf("waits = %v, want %v", fa.waits, want)
	}
	for i := range want {
		if fa.waits[i] != want[i] {
			t.Errorf("waits = %v, want %v", fa.waits, want)
			break
		}
	}
}

func TestUnitsRunTheGivenBinary(t *testing.T) {
	for _, u := range []daemon.Unit{
		daemon.SystemdUnit("/usr/local/bin/clindar", "/home/me"),
		daemon.LaunchdAgent("/usr/local/bin/clindar", "/Users/me"),
	} {
		if !strings.Contains(u.Content, "/usr/local/bin/clindar") || !strings.Contains(u.Content, "daemon") {
			t.Errorf("%s content doesn't run `clindar daemon`:\n%s", u.Path, u.Content)
		}
	}
	if got := daemon.SystemdUnit("/x", "/home/me").Path; got != "/home/me/.config/systemd/user/clindar.service" {
		t.Errorf("systemd path = %s", got)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const label = "com.github.jabiop.clindar"

// Unit is a service definition for the current OS.
type Unit struct {
	Path    string // where the file goes
	Content string
	Enable  string // command the user runs to start it
	Disable string // command to stop it before removing the file
}

// ForThisOS returns the systemd user unit (Linux) or launchd agent (macOS)
// that runs `exe daemon`.
func ForThisOS(exe, home string) (Unit, error) {
	switch runtime.GOOS {
	case "linux":
		return SystemdUnit(exe, home), nil
	case "darwin":
		return LaunchdAgent(exe, home), nil
	}
	return Unit{}, fmt.Errorf("daemon install is not supported on %s; run `clindar daemon` yourself", runtime.GOOS)
}

func SystemdUnit(exe, home string) Unit {
	return Unit{
		Path: filepath.Join(home, ".config", "systemd", "user", "clindar.service"),
		Content: fmt.Sprintf(`[Unit]
Description=clindar Google Calendar sync
After=network-online.target

[Service]
ExecStart=%s daemon
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
`, exe),
		Enable:  "systemctl --user daemon-reload && systemctl --user enable --now clindar.service",
		Disable: "systemctl --user disable --now clindar.service",
	}
}

func LaunchdAgent(exe, home string) Unit {
	path := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	logFile := filepath.Join(home, "Library", "Logs", "clindar.log")
	return Unit{
		Path: path,
		Content: fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key><array><string>%s</string><string>daemon</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, label, exe, logFile),
		Enable:  "launchctl load -w " + path,
		Disable: "launchctl unload -w " + path,
	}
}

// Write creates the unit file (and its directory).
func (u Unit) Write() error {
	if err := os.MkdirAll(filepath.Dir(u.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(u.Path, []byte(u.Content), 0o644)
}
```
4. `go test ./internal/daemon/`: all pass.

**Commit:** `feat(daemon): systemd and launchd service files`

---

## Task 9.3: `clindar daemon` command

**Goal:** wire it up. Each tick uses `syncer.WithLock`, so a sync started from the TUI or CLI never collides
with the daemon's.

**Files:** create `internal/cli/daemoncmd.go`; add `newDaemonCmd(app)` to `root.AddCommand`

**Steps**
1. Implement:
```go
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jabiOP/clindar/internal/daemon"
	"github.com/jabiOP/clindar/internal/syncer"
)

func newDaemonCmd(app *App) *cobra.Command {
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Keep the local cache in sync in the background",
		Long: `Runs until stopped, syncing every --interval (config: sync_interval).
Usually started by your service manager: see 'clindar daemon install'.`,
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			engine, err := app.Engine(ctx)
			if err != nil {
				return err
			}
			if interval == 0 {
				interval = app.Config.SyncInterval.Duration
			}
			log := slog.New(slog.NewTextHandler(app.Stderr, nil))
			log.Info("clindar daemon started", "account", engine.AccountID, "interval", interval)
			daemon.Loop(ctx, interval, 5*time.Minute, func(ctx context.Context) error {
				ran, err := syncer.WithLock(ctx, app.Paths.LockFile(), engine.Sync)
				if !ran && err == nil {
					log.Debug("skipped: another sync is running")
				}
				return err
			}, time.After, log)
			return nil
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 0, "time between syncs (default from config, 45s)")
	cmd.AddCommand(newDaemonInstallCmd(app), newDaemonUninstallCmd(app))
	return cmd
}

func newDaemonInstallCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install a systemd user service (Linux) or launchd agent (macOS)",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			unit, err := thisOSUnit()
			if err != nil {
				return err
			}
			if err := unit.Write(); err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "Wrote %s\nStart it now with:\n\n  %s\n", unit.Path, unit.Enable)
			return nil
		},
	}
}

func newDaemonUninstallCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the service file written by 'daemon install'",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			unit, err := thisOSUnit()
			if err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "First stop it with:\n\n  %s\n\n", unit.Disable)
			if err := os.Remove(unit.Path); err != nil && !os.IsNotExist(err) {
				return err
			}
			fmt.Fprintf(app.Stderr, "Removed %s\n", unit.Path)
			return nil
		},
	}
}

func thisOSUnit() (daemon.Unit, error) {
	exe, err := os.Executable()
	if err != nil {
		return daemon.Unit{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return daemon.Unit{}, err
	}
	return daemon.ForThisOS(exe, home)
}
```
2. Build and try it:
   ```bash
   go build -o clindar . && ./clindar daemon --interval 15s    # watch the logs; Ctrl-C stops it cleanly
   ```
   While it runs, change an event in the web UI. Within 15s, `./clindar agenda --offline` should show the change.
3. Install for real: `./clindar daemon install`, then run the printed command, then `systemctl --user status clindar`.
   For the installed service, put the binary somewhere stable first (`go install .` puts it in `~/go/bin`).

**Commit:** `feat(cli): daemon command and install`
