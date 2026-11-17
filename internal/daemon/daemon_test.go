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
	dirs := daemon.Dirs{Config: "/home/me/cfg", Data: "/home/me/data & more"}
	for _, u := range []daemon.Unit{
		daemon.SystemdUnit("/usr/local/bin/clindar", "/home/me", dirs),
		daemon.LaunchdAgent("/usr/local/bin/clindar", "/Users/me", dirs),
	} {
		if !strings.Contains(u.Content, "/usr/local/bin/clindar") || !strings.Contains(u.Content, "daemon") {
			t.Errorf("%s content doesn't run `clindar daemon`:\n%s", u.Path, u.Content)
		}
		// The service manager doesn't start it with the installing shell's environment:
		// the folders must be in the file, or it may open another (empty) database.
		if !strings.Contains(u.Content, "CLINDAR_CONFIG_DIR") || !strings.Contains(u.Content, "/home/me/cfg") ||
			!strings.Contains(u.Content, "CLINDAR_DATA_DIR") {
			t.Errorf("%s doesn't pin the folders:\n%s", u.Path, u.Content)
		}
	}
	if c := daemon.LaunchdAgent("/x", "/Users/me", dirs).Content; !strings.Contains(c, "/home/me/data &amp; more") {
		t.Errorf("plist text must be XML-escaped:\n%s", c)
	}
	if got := daemon.SystemdUnit("/x", "/home/me", dirs).Path; got != "/home/me/.config/systemd/user/clindar.service" {
		t.Errorf("systemd path = %s", got)
	}
}

// After failures, the first good sync is logged, so the log shows recovery.
func TestLoopLogsRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	results := []error{errors.New("offline"), nil}
	calls := 0
	sync := func(context.Context) error {
		err := results[calls]
		calls++
		if calls == len(results) {
			cancel()
		}
		return err
	}
	var out strings.Builder
	daemon.Loop(ctx, time.Second, time.Minute, sync, (&fakeAfter{}).after, slog.New(slog.NewTextHandler(&out, nil)))

	if !strings.Contains(out.String(), "sync failed") || !strings.Contains(out.String(), "sync recovered") {
		t.Errorf("log:\n%s", out.String())
	}
}
