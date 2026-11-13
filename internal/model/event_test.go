package model_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/model"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func TestEventOverlaps(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	tokyo := mustLoc(t, "Asia/Tokyo")
	at := func(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, ny) }
	utcDay := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }

	timed := model.Event{Start: at(14, 9), End: at(14, 10)}
	allDay := model.Event{AllDay: true, Start: utcDay(14), End: utcDay(15)}
	zeroLen := model.Event{Start: at(14, 9), End: at(14, 9)}

	tests := []struct {
		name     string
		event    model.Event
		from, to time.Time
		want     bool
	}{
		{"timed inside window", timed, at(14, 0), at(15, 0), true},
		{"timed ends exactly at window start", timed, at(14, 10), at(14, 12), false},
		{"timed starts exactly at window end", timed, at(14, 0), at(14, 9), false},
		{"all-day on its day", allDay, at(14, 0), at(15, 0), true},
		{"all-day not on previous day", allDay, at(13, 0), at(14, 0), false},
		{"all-day not on next day", allDay, at(15, 0), at(16, 0), false},
		// If all-day events were treated as UTC instants, Nov 14 00:00Z–Nov 15 00:00Z would
		// cover Nov 15 00:00–09:00 in Tokyo. It must not: "Nov 14" means Nov 14 everywhere.
		{"all-day anchored to window zone",
			allDay,
			time.Date(2026, 11, 15, 0, 0, 0, 0, tokyo),
			time.Date(2026, 11, 15, 6, 0, 0, 0, tokyo),
			false},
		{"zero-length event at window start", zeroLen, at(14, 9), at(14, 10), true},
		{"zero-length event at window end", zeroLen, at(14, 8), at(14, 9), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.Overlaps(tt.from, tt.to); got != tt.want {
				t.Errorf("Overlaps(%v, %v) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestEventSpanAllDayUsesLocalMidnight(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	e := model.Event{AllDay: true,
		Start: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)}

	start, end := e.Span(ny)

	if want := time.Date(2026, 11, 14, 0, 0, 0, 0, ny); !start.Equal(want) {
		t.Errorf("start = %v, want %v", start, want)
	}
	if want := time.Date(2026, 11, 16, 0, 0, 0, 0, ny); !end.Equal(want) {
		t.Errorf("end = %v, want %v", end, want)
	}
}

func TestCloneDoesNotShareSlices(t *testing.T) {
	orig := model.Event{
		Recurrence: []string{"RRULE:FREQ=DAILY"},
		Attendees:  []model.Attendee{{Email: "me@x.com", Self: true, Response: model.ResponseNeedsAction}},
	}

	c := orig.Clone()
	c.Recurrence[0] = "changed"
	c.SelfAttendee().Response = model.ResponseAccepted

	if orig.Recurrence[0] != "RRULE:FREQ=DAILY" {
		t.Errorf("orig.Recurrence changed to %q", orig.Recurrence[0])
	}
	if orig.Attendees[0].Response != model.ResponseNeedsAction {
		t.Errorf("orig attendee response changed to %q", orig.Attendees[0].Response)
	}
}

func TestSelfAttendeeNilWhenAbsent(t *testing.T) {
	e := model.Event{Attendees: []model.Attendee{{Email: "other@x.com"}}}
	if a := e.SelfAttendee(); a != nil {
		t.Errorf("SelfAttendee() = %+v, want nil", a)
	}
}
