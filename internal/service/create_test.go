package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestCreateStoresLocallyAndQueuesInsert(t *testing.T) {
	e := newEnv(t)
	local := time.Date(2026, 11, 16, 14, 0, 0, 0, time.Local) // as parsed from user input

	ev, err := e.svc.Create(e.ctx, service.Draft{Title: " Write report ", Start: local, End: local.Add(time.Hour)})

	if err != nil {
		t.Fatal(err)
	}
	if ev.Title != "Write report" || ev.CalendarID != testutil.Calendar {
		t.Errorf("created %+v", ev)
	}
	if ev.TimeZone != "America/New_York" {
		t.Errorf("TimeZone = %q, want calendar zone (Go's \"Local\" is not an IANA name)", ev.TimeZone)
	}
	ops := e.ops()
	if len(ops) != 1 || ops[0].Kind != store.OpInsert || ops[0].EventID != ev.ID || ops[0].SendUpdates != "none" {
		t.Errorf("ops = %+v, want one insert with send_updates none", ops)
	}
}

func TestCreateWithInvitesSendsUpdates(t *testing.T) {
	e := newEnv(t)

	_, err := e.svc.Create(e.ctx, service.Draft{Title: "Sync", Start: e.at(16, 9, 0), End: e.at(16, 10, 0),
		Invite: []string{"bob@example.com"}})

	if err != nil {
		t.Fatal(err)
	}
	if ops := e.ops(); ops[0].SendUpdates != "all" || len(ops[0].Payload.Attendees) != 1 {
		t.Errorf("op = %+v", ops[0])
	}
}

func TestCreateAllDayStoresUTCMidnights(t *testing.T) {
	e := newEnv(t)

	ev, err := e.svc.Create(e.ctx, service.Draft{Title: "Trip", AllDay: true, Start: e.at(20, 0, 0), End: e.at(22, 0, 0)})

	if err != nil {
		t.Fatal(err)
	}
	if !ev.Start.Equal(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)) || ev.TimeZone != "" {
		t.Errorf("start = %v, tz = %q", ev.Start, ev.TimeZone)
	}
	if got := e.titlesOn(21); len(got) != 1 {
		t.Errorf("trip not on Nov 21: %v", got)
	}
}

func TestCreateRejects(t *testing.T) {
	tests := map[string]struct {
		draft   service.Draft
		wantErr error
	}{
		"empty title":        {service.Draft{Title: " "}, service.ErrInvalidEvent},
		"end before start":   {service.Draft{Title: "x", Start: time.Unix(100, 0), End: time.Unix(50, 0)}, service.ErrInvalidEvent},
		"read-only calendar": {service.Draft{Title: "x", CalendarID: "Holidays"}, service.ErrReadOnly},
		"unknown calendar":   {service.Draft{Title: "x", CalendarID: "nope"}, service.ErrNoCalendar},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)

			_, err := e.svc.Create(e.ctx, tt.draft)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if len(e.ops()) != 0 {
				t.Errorf("a failed create must not queue anything")
			}
		})
	}
}
