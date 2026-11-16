package cli_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/model"
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

func TestAddUnderstandsSlashDatesInTheConfiguredOrder(t *testing.T) {
	h := newHarness(t)
	id := strings.TrimSpace(h.mustRun("add", "Dentist", "--at", "20/11 15:30", "--offline"))
	if out := h.mustRun("show", id, "--offline", "--format", "{{.Start}}"); out != "2026-11-20T15:30:00-05:00\n" {
		t.Errorf("day/month: start = %q", out)
	}

	h.app.Config.DateOrder = "mdy"
	id = strings.TrimSpace(h.mustRun("add", "Dentist", "--at", "11/20 15:30", "--offline"))
	if out := h.mustRun("show", id, "--offline", "--format", "{{.Start}}"); out != "2026-11-20T15:30:00-05:00\n" {
		t.Errorf("month/day: start = %q", out)
	}
}

func TestAddOneLine(t *testing.T) {
	h := newHarness(t)

	id := strings.TrimSpace(h.mustRun("add", "Dentist 20/11 15:30 45m", "--offline"))
	if out := h.mustRun("show", id, "--format", "{{.Title}} {{.Start}} {{.End}}"); out != "Dentist 2026-11-20T15:30:00-05:00 2026-11-20T16:15:00-05:00\n" {
		t.Errorf("got %q", out)
	}
	id = strings.TrimSpace(h.mustRun("add", "Call", "mom", "--offline")) // unquoted: all-day today
	if out := h.mustRun("show", id, "--tsv"); !strings.Contains(out, "\t2026-11-12\t2026-11-13\t1\t") {
		t.Errorf("got %q", out)
	}
}

func TestAddOneLineErrorsAreUsageErrors(t *testing.T) {
	h := newHarness(t)
	if _, errOut, code := h.run("add", "Nap 30m"); code != 2 || !strings.Contains(errOut, "start time") {
		t.Errorf("exit %d, %q", code, errOut)
	}
	if _, _, code := h.run("add", "Dentist", "--dur", "45m"); code != 2 {
		t.Errorf("--dur without --at: exit %d, want 2", code)
	}
}

func TestStdinTitleNeedsAt(t *testing.T) {
	h := newHarness(t)
	h.stdin = "Review report daily\n"

	for _, args := range [][]string{{"add", "-", "--offline"}, {"add", "--offline"}} {
		_, stderr, code := h.run(args...)
		if code != 2 || !strings.Contains(stderr, "needs --at") {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
	if out := h.mustRun("agenda", "--offline", "--days", "30", "--tsv"); out != "" {
		t.Errorf("nothing should be created, got %q", out)
	}

	id := strings.TrimSpace(h.mustRun("add", "-", "--at", "tomorrow 9am", "--offline"))
	if out := h.mustRun("show", id, "--format", "{{.Title}}"); out != "Review report daily\n" {
		t.Errorf("title = %q, want it literal", out)
	}
}

func TestWriteWarningSaysWhetherItReachedGoogle(t *testing.T) {
	h := newHarness(t)
	h.google.FailPull = errors.New("boom")
	id := strings.TrimSpace(h.mustRun("add", "Lunch", "--at", "12:00"))
	if _, stderr, _ := h.run("edit", id, "--title", "Brunch"); !strings.Contains(stderr, "sent to Google; refreshing from Google failed") {
		t.Errorf("pull failed after push: stderr %q", stderr)
	}

	h.google.FailPull, h.google.Fail = nil, errors.New("offline")
	if _, stderr, _ := h.run("edit", id, "--title", "Dinner"); !strings.Contains(stderr, "not sent to Google yet") {
		t.Errorf("push failed: stderr %q", stderr)
	}
}

func TestEditMovesToAnotherCalendar(t *testing.T) {
	h := newHarness(t)
	h.google.AddCalendar(model.Calendar{AccountID: testutil.Account, ID: "work", Name: "Work", AccessRole: "owner", Selected: true})
	h.mustRun("sync")
	id := strings.TrimSpace(h.mustRun("add", "Lunch", "--at", "fri 12:00"))

	h.mustRun("edit", id, "-c", "Work")

	if out := h.mustRun("show", id, "--json", "--offline"); !strings.Contains(out, `"calendar_id":"work"`) {
		t.Errorf("show = %s", out)
	}
	if _, ok := h.google.Event("work", id); !ok {
		t.Error("not moved on Google")
	}
}

func TestEditMovingOneOccurrenceSaysWhy(t *testing.T) {
	h := newHarness(t)
	h.google.AddCalendar(model.Calendar{AccountID: testutil.Account, ID: "work", Name: "Work", AccessRole: "owner", Selected: true})
	h.mustRun("sync")
	h.mustRun("add", "Standup", "--at", "mon 9:00", "--dur", "15m", "--repeat", "weekly", "--offline")
	instance := strings.Fields(h.mustRun("agenda", "--from", "2026-11-23", "--days", "1", "--tsv", "--offline"))[0]

	_, stderr, code := h.run("edit", instance, "-c", "Work", "--scope", "this", "--offline")
	if code != 1 || !strings.Contains(stderr, "move the whole series (scope all)") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	h.mustRun("edit", instance, "-c", "Work", "--scope", "all", "--offline")
}
