package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestDragChanges(t *testing.T) {
	ny := testutil.NY(t)
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, ny) }
	at := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, ny) }
	dentist := testutil.Timed("a", "Dentist", at(10, 9), time.Hour)         // Tue 09:00–10:00
	trip := testutil.AllDay("trip", "Trip", 2026, time.November, 13, 2)     // Fri 13–Sat 14
	overnight := testutil.Timed("o", "Overnight", at(13, 20), 38*time.Hour) // Fri 20:00–Sun 10:00
	tests := []struct {
		name               string
		e                  model.Event
		p                  part
		from, to           time.Time
		wantStart, wantEnd time.Time // all-day: local midnights, end exclusive
		wantErr            error
	}{
		{"move one-day event", dentist, partBody, day(10), day(12), at(12, 9), at(12, 10), nil},
		{"move multi-day event by the grabbed day", trip, partBody, day(14), day(16), day(15), day(17), nil},
		{"end handle lengthens", trip, partEnd, day(14), day(16), day(13), day(17), nil},
		{"end handle shortens to one day", trip, partEnd, day(14), day(13), day(13), day(14), nil},
		{"end handle clamps at the first day", trip, partEnd, day(14), day(9), day(13), day(14), nil},
		{"start handle moves the first day", trip, partStart, day(13), day(11), day(11), day(15), nil},
		{"end handle keeps the end's clock time", overnight, partEnd, day(15), day(17), at(13, 20), at(17, 10), nil},
		{"one-day event becomes multi-day", dentist, partEnd, day(10), day(11), at(10, 9), at(11, 10), nil},
		{"start dragged onto last day would invert", overnight, partStart, day(13), day(15), time.Time{}, time.Time{}, errInverted},
		{"drop on the same day", trip, partBody, day(13), day(13), time.Time{}, time.Time{}, errNoChange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := dragChanges(tt.e, tt.p, tt.from, tt.to, ny)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if !ch.Start.Equal(tt.wantStart) || !ch.End.Equal(tt.wantEnd) {
				t.Errorf("got %v → %v, want %v → %v", ch.Start, ch.End, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestDragKeepsClockTimeAcrossDST(t *testing.T) {
	ny := testutil.NY(t)
	e := testutil.Timed("a", "Standup", time.Date(2026, 10, 30, 9, 0, 0, 0, ny), 15*time.Minute) // Fri, EDT

	ch, err := dragChanges(e, partBody, time.Date(2026, 10, 30, 0, 0, 0, 0, ny), time.Date(2026, 11, 2, 0, 0, 0, 0, ny), ny)

	if err != nil || ch.Start.In(ny).Hour() != 9 || ch.Start.In(ny).Day() != 2 {
		t.Errorf("start = %v, %v; want Mon Nov 2 09:00 (DST ended Nov 1)", ch.Start, err)
	}
}

func TestCanDrag(t *testing.T) {
	when := time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)
	mine := model.Calendar{Name: "Me", AccessRole: "owner"}
	invite := testutil.Timed("i", "Their party", when, time.Hour)
	invite.Attendees = []model.Attendee{{Email: testutil.Account, Self: true}, {Email: "host@x.com", Organizer: true}}
	own := testutil.Timed("o", "Mine", when, time.Hour)

	if ok, _ := canDrag(own, mine); !ok {
		t.Error("own event on own calendar should be draggable")
	}
	if ok, why := canDrag(invite, mine); ok || why == "" {
		t.Error("an invite should not be draggable, with a reason")
	}
	if ok, _ := canDrag(own, model.Calendar{Name: "Holidays", AccessRole: "reader"}); ok {
		t.Error("read-only calendar should not be draggable")
	}
}
