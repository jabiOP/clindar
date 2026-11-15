package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestDeleteThisInstance(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	if err := e.svc.Delete(e.ctx, id, service.ScopeThis); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(16); len(got) != 0 {
		t.Errorf("Nov 16 = %v, want empty", got)
	}
	if got := e.titlesOn(23); len(got) != 1 {
		t.Errorf("Nov 23 = %v, want standup", got)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].Kind != store.OpDelete || ops[0].EventID != id {
		t.Errorf("ops = %+v", ops)
	}
}

func TestDeleteFollowingTruncates(t *testing.T) {
	e := newEnv(t)
	e.series()

	if err := e.svc.Delete(e.ctx, model.InstanceID("std", e.at(16, 9, 0), false), service.ScopeFollowing); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(9); len(got) != 1 {
		t.Errorf("Nov 9 = %v, want standup", got)
	}
	if got := e.titlesOn(16); len(got) != 0 {
		t.Errorf("Nov 16 = %v, want empty", got)
	}
}

func TestDeleteAllRemovesSeriesAndQueuesMasterDelete(t *testing.T) {
	e := newEnv(t)
	e.series()

	if err := e.svc.Delete(e.ctx, model.InstanceID("std", e.at(16, 9, 0), false), service.ScopeAll); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(9); len(got) != 0 {
		t.Errorf("Nov 9 = %v, want empty", got)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].Kind != store.OpDelete || ops[0].EventID != "std" || ops[0].IfMatch != `"m1"` {
		t.Errorf("ops = %+v", ops)
	}
}

func TestRSVP(t *testing.T) {
	e := newEnv(t)
	inv := testutil.Timed("inv", "Party", e.at(20, 18, 0), 2*time.Hour)
	inv.Attendees = []model.Attendee{
		{Email: "host@example.com", Organizer: true, Response: model.ResponseAccepted},
		{Email: testutil.Account, Self: true, Response: model.ResponseNeedsAction},
	}
	e.put(inv)
	e.put(testutil.Timed("mine", "Solo", e.at(20, 9, 0), time.Hour))

	if err := e.svc.RSVP(e.ctx, "inv", model.ResponseAccepted, service.ScopeNone); err != nil {
		t.Fatal(err)
	}

	got, _ := e.svc.Get(e.ctx, "inv")
	if got.SelfAttendee().Response != model.ResponseAccepted || got.Attendees[0].Response != model.ResponseAccepted {
		t.Errorf("attendees = %+v", got.Attendees)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].Kind != store.OpRSVP || ops[0].IfMatch != "" || ops[0].SendUpdates != "all" {
		t.Errorf("ops = %+v, want one unconditional rsvp op that notifies the organizer", ops)
	}
	if err := e.svc.RSVP(e.ctx, "mine", model.ResponseAccepted, service.ScopeNone); !errors.Is(err, service.ErrNotAttendee) {
		t.Errorf("RSVP on own event: err = %v, want ErrNotAttendee", err)
	}
}

// Moved here from Task 4.1 (see DEVIATIONS.md): an orphan instance is presented
// as a one-off, so RSVP needs no scope.
func TestRSVPOnOrphanInstanceNeedsNoScope(t *testing.T) {
	e := newEnv(t)
	inv := testutil.Timed("theirs_20261116T150000Z", "Their weekly sync", e.at(16, 10, 0), time.Hour)
	inv.SeriesID, inv.OriginalStart = "theirs", e.at(16, 10, 0)
	inv.Attendees = []model.Attendee{{Email: testutil.Account, Self: true, Response: model.ResponseNeedsAction}}
	e.put(inv)

	if err := e.svc.RSVP(e.ctx, inv.ID, model.ResponseAccepted, service.ScopeNone); err != nil {
		t.Errorf("RSVP without scope: %v", err)
	}
}

// An invitation seen through someone else's read-only calendar can't be answered
// there: Google would reject the change. Refuse it before queuing anything.
func TestRSVPOnReadOnlyCalendarIsRefused(t *testing.T) {
	e := newEnv(t)
	inv := testutil.Timed("inv", "Their meeting", e.at(20, 10, 0), time.Hour)
	inv.CalendarID = "holidays" // read-only in newEnv
	inv.Attendees = []model.Attendee{{Email: testutil.Account, Self: true, Response: model.ResponseNeedsAction}}
	e.put(inv)

	err := e.svc.RSVP(e.ctx, "inv", model.ResponseAccepted, service.ScopeNone)

	if !errors.Is(err, service.ErrReadOnly) {
		t.Errorf("err = %v, want ErrReadOnly", err)
	}
	if len(e.ops()) != 0 {
		t.Errorf("a refused RSVP must not queue anything: %+v", e.ops())
	}
}
