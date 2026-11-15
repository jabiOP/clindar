package cli_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/output"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestAgendaShowsSyncedEvents(t *testing.T) {
	h := newHarness(t)
	ny := testutil.NY(t)
	h.google.Put(testutil.Timed("e1", "Dentist", time.Date(2026, 11, 13, 15, 0, 0, 0, ny), 45*time.Minute))
	h.mustRun("sync")

	out := h.mustRun("agenda", "--json")

	var got output.EventJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("agenda --json = %q: %v", out, err)
	}
	if got.ID != "e1" || got.Title != "Dentist" || got.Calendar != "Me" {
		t.Errorf("got %+v", got)
	}
}

func TestAgendaWithinOnlyListsEventsStartingSoon(t *testing.T) {
	h := newHarness(t)
	ny := testutil.NY(t)
	h.google.Put(testutil.Timed("now", "Already started", time.Date(2026, 11, 12, 10, 0, 0, 0, ny), time.Hour))
	h.google.Put(testutil.Timed("soon", "Starts soon", time.Date(2026, 11, 12, 10, 40, 0, 0, ny), time.Hour))
	h.google.Put(testutil.Timed("later", "Later", time.Date(2026, 11, 12, 15, 0, 0, 0, ny), time.Hour))
	h.mustRun("sync")

	out := h.mustRun("agenda", "--within", "15m", "--format", "{{.Title}}")

	if out != "Starts soon\n" {
		t.Errorf("got %q", out)
	}
}

func TestNextWithNothingExits1Silently(t *testing.T) {
	h := newHarness(t)

	out, errOut, code := h.run("next")

	if code != 1 || out != "" || errOut != "" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// show is a read like agenda: it refreshes a stale cache first (design §3),
// and --offline skips that.
func TestShowRefreshesWhenStaleUnlessOffline(t *testing.T) {
	h := newHarness(t)
	h.google.Put(testutil.Timed("e1", "Dentist", time.Date(2026, 11, 13, 15, 0, 0, 0, testutil.NY(t)), time.Hour))
	h.now = h.now.Add(3 * time.Minute) // past stale_after (2m) since the harness's first sync

	if _, _, code := h.run("show", "e1", "--offline"); code != 1 {
		t.Errorf("show --offline: exit %d, want 1 (not synced yet, so not found)", code)
	}
	if out := h.mustRun("show", "e1", "--format", "{{.Title}}"); out != "Dentist\n" {
		t.Errorf("show = %q, want Dentist (fetched by the refresh)", out)
	}
}
