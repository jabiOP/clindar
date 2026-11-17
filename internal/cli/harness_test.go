package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/cli"
	"github.com/jabiOP/clindar/internal/config"
	"github.com/jabiOP/clindar/internal/gcal/gcaltest"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/syncer"
	"github.com/jabiOP/clindar/internal/testutil"
)

// harness runs clindar commands in-process against temp dirs and a fake Google.
type harness struct {
	t      *testing.T
	app    *cli.App
	google *gcaltest.Fake
	now    time.Time
	stdin  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ny := testutil.NY(t)
	h := &harness{t: t, now: time.Date(2026, 11, 12, 10, 30, 0, 0, ny)} // Thursday
	h.google = gcaltest.NewFake(model.Calendar{AccountID: testutil.Account, ID: testutil.Calendar,
		Name: "Me", Primary: true, Selected: true, AccessRole: "owner", TimeZone: "America/New_York"})
	dir := t.TempDir()
	h.app = &cli.App{
		Paths:  config.Paths{ConfigDir: dir + "/config", DataDir: dir + "/data"},
		Config: config.Default(),
		Now:    func() time.Time { return h.now },
		Remote: func(context.Context, string) (syncer.Remote, error) { return h.google, nil },
	}
	t.Cleanup(func() { h.app.Close() })
	st, err := h.app.Store()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureAccount(context.Background(), testutil.Account); err != nil { // as `auth login` would
		t.Fatal(err)
	}
	h.mustRun("sync") // and so would its first sync
	return h
}

// run executes a command line and returns stdout, stderr, and the exit code.
func (h *harness) run(args ...string) (string, string, int) {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	h.app.Stdin, h.app.Stdout, h.app.Stderr = strings.NewReader(h.stdin), &stdout, &stderr
	code := cli.Run(context.Background(), h.app, args)
	return stdout.String(), stderr.String(), code
}

// mustRun fails the test unless the command exits 0.
func (h *harness) mustRun(args ...string) string {
	h.t.Helper()
	out, errOut, code := h.run(args...)
	if code != 0 {
		h.t.Fatalf("clindar %s: exit %d\nstderr: %s", strings.Join(args, " "), code, errOut)
	}
	return out
}

func TestNotLoggedIn(t *testing.T) {
	h := newHarness(t)
	st, _ := h.app.Store()
	st.DeleteAccount(context.Background(), testutil.Account)

	_, stderr, code := h.run("sync")

	if code != 1 || !strings.Contains(stderr, "clindar auth login") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

// A daemon started before login (or on the wrong folders) waits and says
// which folder it looked in, instead of exiting and being restarted at once.
func TestDaemonNotLoggedInWaits(t *testing.T) {
	h := newHarness(t)
	st, _ := h.app.Store()
	st.DeleteAccount(context.Background(), testutil.Account)
	var stderr bytes.Buffer
	h.app.Stderr = &stderr
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	code := cli.Run(ctx, h.app, []string{"daemon", "--interval", "10s"})

	if code != 0 || !strings.Contains(stderr.String(), "not logged in") || !strings.Contains(stderr.String(), h.app.Paths.DataDir) {
		t.Errorf("exit %d, stderr:\n%s", code, stderr.String())
	}
}
