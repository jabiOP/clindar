package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/output"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestAddThenAgendaJSON(t *testing.T) {
	h := newHarness(t)

	id := strings.TrimSpace(h.mustRun("add", "Write", "report", "--at", "fri 14:00", "--dur", "90m"))
	out := h.mustRun("agenda", "--from", "fri", "--days", "1", "--json")

	var got output.EventJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("agenda output %q: %v", out, err)
	}
	if got.ID != id || got.Title != "Write report" || got.Start != "2026-11-13T14:00:00-05:00" || got.End != "2026-11-13T15:30:00-05:00" {
		t.Errorf("got %+v", got)
	}
	if _, ok := h.google.Event(testutil.Calendar, id); !ok {
		t.Error("add should push to Google right away")
	}
}

func TestAddReadsTitleFromStdin(t *testing.T) {
	h := newHarness(t)
	h.stdin = "call the bank  @phone\n(second line ignored)\n"

	id := strings.TrimSpace(h.mustRun("add", "-", "--at", "tomorrow 9am", "--offline"))

	out := h.mustRun("show", id, "--format", "{{.Title}}")
	if out != "call the bank  @phone\n" {
		t.Errorf("title = %q", out)
	}
}

func TestAddDateOnlyIsAllDay(t *testing.T) {
	h := newHarness(t)

	id := strings.TrimSpace(h.mustRun("add", "Conference", "--at", "2026-11-20", "--end", "2026-11-21", "--offline"))

	out := h.mustRun("show", id, "--tsv")
	if want := id + "\t2026-11-20\t2026-11-22\t1\tMe\tConference\n"; out != want {
		t.Errorf("got %q, want %q (end is exclusive)", out, want)
	}
}

func TestEditRecurringNeedsScope(t *testing.T) {
	h := newHarness(t)
	h.mustRun("add", "Standup", "--at", "mon 9:00", "--dur", "15m", "--repeat", "weekly", "--offline")
	instance := strings.Fields(h.mustRun("agenda", "--from", "2026-11-23", "--days", "1", "--tsv"))[0]

	_, stderr, code := h.run("edit", instance, "--title", "Retro", "--offline")
	if code != 1 || !strings.Contains(stderr, "choose a scope") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}

	h.mustRun("edit", instance, "--title", "Retro", "--scope", "this", "--offline")
	if out := h.mustRun("agenda", "--from", "2026-11-23", "--days", "1", "--format", "{{.Title}}"); out != "Retro\n" {
		t.Errorf("after edit: %q", out)
	}
}

func TestNextShowsMinutesUntilAddedEvent(t *testing.T) {
	h := newHarness(t)
	h.mustRun("add", "Lunch", "--at", "12:00", "--offline")

	out, _, code := h.run("next", "--offline", "--format", "{{.StartsInMinutes}} {{.Title}}")
	if code != 0 || out != "90 Lunch\n" {
		t.Errorf("exit %d, out %q", code, out)
	}
}

func TestUsageErrorsExit2(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"agenda", "--json", "--tsv"},
		{"agenda", "--bogus"},
		{"add", "x", "--at", "someday"},
		{"rsvp", "id", "perhaps"},
	} {
		if _, _, code := h.run(args...); code != 2 {
			t.Errorf("clindar %v: exit %d, want 2", args, code)
		}
	}
}
