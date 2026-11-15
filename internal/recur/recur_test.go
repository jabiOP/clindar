package recur_test

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/testutil"
)

// weeklyStandup: Mondays and Wednesdays 09:00–09:15 New York, starting Mon Nov 2 2026.
func weeklyStandup(t *testing.T, rule string) model.Event {
	t.Helper()
	ny := testutil.NY(t)
	m := testutil.Timed("std", "Standup", time.Date(2026, 11, 2, 9, 0, 0, 0, ny), 15*time.Minute)
	m.Recurrence = []string{rule}
	return m
}

func starts(events []model.Event, loc *time.Location) []string {
	var out []string
	model.SortEvents(events, loc)
	for _, e := range events {
		out = append(out, e.Start.In(loc).Format("Jan 02 15:04")+" "+e.Title)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestExpandWithinWindow(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO,WE")

	got, err := recur.Expand(m, nil, time.Date(2026, 11, 9, 0, 0, 0, 0, ny), time.Date(2026, 11, 16, 0, 0, 0, 0, ny))

	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Nov 09 09:00 Standup", "Nov 11 09:00 Standup"}
	if g := starts(got, ny); !equal(g, want) {
		t.Errorf("got %v, want %v", g, want)
	}
	if got[0].ID != "std_20261109T140000Z" || got[0].SeriesID != "std" || got[0].IsMaster() {
		t.Errorf("instance identity wrong: %+v", got[0])
	}
}

func TestExpandKeepsWallClockAcrossDST(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO")
	m.Start = time.Date(2026, 10, 26, 9, 0, 0, 0, ny) // before DST ends on Nov 1
	m.End = m.Start.Add(15 * time.Minute)

	got, _ := recur.Expand(m, nil, time.Date(2026, 11, 2, 0, 0, 0, 0, ny), time.Date(2026, 11, 3, 0, 0, 0, 0, ny))

	if len(got) != 1 || got[0].Start.In(ny).Hour() != 9 {
		t.Errorf("got %v, want one instance at 09:00 local", starts(got, ny))
	}
}

func TestExpandAppliesExceptions(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO,WE")
	mon := time.Date(2026, 11, 9, 9, 0, 0, 0, ny)
	wed := time.Date(2026, 11, 11, 9, 0, 0, 0, ny)

	moved := recur.Instance(m, mon)
	moved.Title = "Standup (moved)"
	moved.Start = time.Date(2026, 11, 10, 10, 0, 0, 0, ny) // moved to Tuesday 10:00
	moved.End = moved.Start.Add(15 * time.Minute)
	cancelled := recur.Instance(m, wed)
	cancelled.Status = model.StatusCancelled

	got, _ := recur.Expand(m, []model.Event{moved, cancelled},
		time.Date(2026, 11, 9, 0, 0, 0, 0, ny), time.Date(2026, 11, 16, 0, 0, 0, 0, ny))

	want := []string{"Nov 10 10:00 Standup (moved)"}
	if g := starts(got, ny); !equal(g, want) {
		t.Errorf("got %v, want %v", g, want)
	}
}

func TestExpandHonoursExdateAndCount(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=3")
	m.Recurrence = append(m.Recurrence, "EXDATE;TZID=America/New_York:20261109T090000")

	got, _ := recur.Expand(m, nil, time.Date(2026, 11, 1, 0, 0, 0, 0, ny), time.Date(2027, 1, 1, 0, 0, 0, 0, ny))

	want := []string{"Nov 02 09:00 Standup", "Nov 16 09:00 Standup"}
	if g := starts(got, ny); !equal(g, want) {
		t.Errorf("got %v, want %v", g, want)
	}
}

func TestExpandAllDaySeries(t *testing.T) {
	ny := testutil.NY(t)
	m := testutil.AllDay("bday", "Birthday", 2020, time.November, 14, 1)
	m.Recurrence = []string{"RRULE:FREQ=YEARLY"}

	got, _ := recur.Expand(m, nil, time.Date(2026, 11, 14, 0, 0, 0, 0, ny), time.Date(2026, 11, 15, 0, 0, 0, 0, ny))

	if len(got) != 1 || got[0].ID != "bday_20261114" || !got[0].AllDay {
		t.Errorf("got %+v", got)
	}
}

func TestLookup(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO,WE")
	mon := time.Date(2026, 11, 9, 9, 0, 0, 0, ny)

	inst, err := recur.Lookup(m, nil, mon)
	if err != nil || inst.ID != "std_20261109T140000Z" {
		t.Errorf("Lookup(valid slot) = %v, %v", inst.ID, err)
	}
	_, err = recur.Lookup(m, nil, mon.Add(time.Hour))
	if !errors.Is(err, recur.ErrNoSuchInstance) {
		t.Errorf("Lookup(not a slot) err = %v, want ErrNoSuchInstance", err)
	}
}

// Google's web link names one event via eid = base64("<event id> <calendar>").
// An occurrence must link to itself, not to the series (which opens the first one).
func TestInstanceLinksToItsOwnOccurrence(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO")
	eid := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	m.Link = "https://www.google.com/calendar/event?eid=" + eid("std me@example.com")

	inst := recur.Instance(m, time.Date(2026, 11, 9, 9, 0, 0, 0, ny))

	want := "https://www.google.com/calendar/event?eid=" + eid("std_20261109T140000Z me@example.com")
	if inst.Link != want {
		t.Errorf("Link = %s, want %s", inst.Link, want)
	}
}
