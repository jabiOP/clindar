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

func withWork(e *env) *env {
	e.t.Helper()
	if err := e.store.UpsertCalendar(e.ctx, model.Calendar{AccountID: testutil.Account, ID: "work", Name: "Work",
		AccessRole: "writer", Selected: true}); err != nil {
		e.t.Fatal(err)
	}
	return e
}

func TestMoveOneOff(t *testing.T) {
	e := withWork(newEnv(t))
	ev := testutil.Timed("lunch", "Lunch", e.at(16, 12, 0), time.Hour)
	ev.ETag = `"1"`
	e.put(ev)

	if _, err := e.svc.Update(e.ctx, "lunch", service.Changes{CalendarID: ptr("Work")}, service.ScopeNone); err != nil {
		t.Fatal(err)
	}

	ops := e.ops()
	if len(ops) != 1 || ops[0].Kind != store.OpMove || ops[0].CalendarID != testutil.Calendar ||
		ops[0].Destination != "work" || ops[0].SendUpdates != "none" {
		t.Errorf("ops = %+v, want one move to work (no guests: no emails)", ops)
	}
	if got, _ := e.svc.Get(e.ctx, "lunch"); got.CalendarID != "work" {
		t.Errorf("calendar = %q, want work", got.CalendarID)
	}
	if got := e.titlesOn(16); !equalStrings(got, []string{"12:00 Lunch"}) {
		t.Errorf("Nov 16 = %v, want it once", got)
	}
}

func TestMoveTellsGuestsLikeAnyEdit(t *testing.T) {
	e := withWork(newEnv(t))
	ev := testutil.Timed("lunch", "Lunch", e.at(16, 12, 0), time.Hour)
	ev.Attendees = []model.Attendee{{Email: "sam@example.com"}}
	e.put(ev)

	e.svc.Update(e.ctx, "lunch", service.Changes{CalendarID: ptr("work")}, service.ScopeNone)

	if ops := e.ops(); len(ops) != 1 || ops[0].SendUpdates != "all" {
		t.Errorf("ops = %+v, want send_updates (all) for an event with guests", ops)
	}
}

func TestMoveWithOtherChanges(t *testing.T) {
	e := withWork(newEnv(t))
	e.put(testutil.Timed("lunch", "Lunch", e.at(16, 12, 0), time.Hour))

	if _, err := e.svc.Update(e.ctx, "lunch", service.Changes{Title: ptr("Brunch"), CalendarID: ptr("Work")}, service.ScopeNone); err != nil {
		t.Fatal(err)
	}

	ops := e.ops()
	if len(ops) != 2 || ops[0].Kind != store.OpPatch || ops[0].CalendarID != testutil.Calendar ||
		ops[0].Payload.Title != "Brunch" || ops[1].Kind != store.OpMove {
		t.Errorf("ops = %+v, want the patch here, then the move", ops)
	}
}

func TestMoveSeriesNeedsScopeAll(t *testing.T) {
	e := withWork(newEnv(t))
	e.series()
	inst := model.InstanceID("std", e.at(16, 9, 0), false)
	ex, _ := e.svc.Get(e.ctx, inst)
	ex.Title = "Standup (moved)"
	e.put(ex) // a stored exception

	for _, sc := range []service.Scope{service.ScopeThis, service.ScopeFollowing, service.ScopeNone} {
		if _, err := e.svc.Update(e.ctx, inst, service.Changes{CalendarID: ptr("Work")}, sc); !errors.Is(err, service.ErrMoveOccurrence) {
			t.Errorf("scope %v: err = %v, want ErrMoveOccurrence", sc, err)
		}
	}
	if len(e.ops()) != 0 {
		t.Fatalf("refusals queued %+v", e.ops())
	}

	if _, err := e.svc.Update(e.ctx, inst, service.Changes{CalendarID: ptr("Work")}, service.ScopeAll); err != nil {
		t.Fatal(err)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].EventID != "std" || ops[0].Kind != store.OpMove {
		t.Errorf("ops = %+v, want the series moved", ops)
	}
	if exs, _ := e.store.Exceptions(e.ctx, testutil.Account, "work", "std"); len(exs) != 1 {
		t.Errorf("exceptions in work = %d, want 1", len(exs))
	}
}

func TestMoveRefusals(t *testing.T) {
	e := withWork(newEnv(t))
	e.put(testutil.Timed("lunch", "Lunch", e.at(16, 12, 0), time.Hour))
	inv := testutil.Timed("inv", "Their meeting", e.at(17, 10, 0), time.Hour)
	inv.Attendees = []model.Attendee{{Email: testutil.Account, Self: true}, {Email: "boss@example.com", Organizer: true}}
	e.put(inv)
	orphan := testutil.Timed("theirs_20261116T150000Z", "Their weekly", e.at(16, 10, 0), time.Hour)
	orphan.SeriesID, orphan.OriginalStart = "theirs", e.at(16, 10, 0)
	e.put(orphan)

	for _, c := range []struct {
		id, to string
		want   error
	}{
		{"lunch", "Holidays", service.ErrReadOnly},
		{"lunch", "nope", service.ErrNoCalendar},
		{"inv", "Work", service.ErrInvitation},
		{orphan.ID, "Work", service.ErrMoveOccurrence},
	} {
		if _, err := e.svc.Update(e.ctx, c.id, service.Changes{CalendarID: ptr(c.to)}, service.ScopeNone); !errors.Is(err, c.want) {
			t.Errorf("%s → %s: err = %v, want %v", c.id, c.to, err, c.want)
		}
	}
	if len(e.ops()) != 0 {
		t.Errorf("refusals queued %+v", e.ops())
	}
}

func TestMoveToTheSameCalendarIsJustAnEdit(t *testing.T) {
	e := newEnv(t)
	e.put(testutil.Timed("lunch", "Lunch", e.at(16, 12, 0), time.Hour))

	e.svc.Update(e.ctx, "lunch", service.Changes{Title: ptr("Brunch"), CalendarID: ptr("Me")}, service.ScopeNone)

	if ops := e.ops(); len(ops) != 1 || ops[0].Kind != store.OpPatch {
		t.Errorf("ops = %+v, want one patch", ops)
	}
}
