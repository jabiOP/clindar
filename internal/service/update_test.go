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

func TestUpdateOneOffMovesKeepingDuration(t *testing.T) {
	e := newEnv(t)
	ev := testutil.Timed("e1", "Dentist", e.at(16, 9, 0), 45*time.Minute)
	ev.ETag = `"v1"`
	e.put(ev)

	got, err := e.svc.Update(e.ctx, "e1", service.Changes{Start: ptr(e.at(17, 14, 0))}, service.ScopeNone)

	if err != nil {
		t.Fatal(err)
	}
	if !got.End.Equal(e.at(17, 14, 45)) {
		t.Errorf("End = %v, want 14:45 (duration kept)", got.End)
	}
	ops := e.ops()
	if len(ops) != 1 || ops[0].Kind != store.OpPatch || ops[0].IfMatch != `"v1"` {
		t.Errorf("ops = %+v, want one patch with If-Match \"v1\"", ops)
	}
}

func TestUpdateInstanceRequiresScope(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	_, err := e.svc.Update(e.ctx, id, service.Changes{Title: ptr("x")}, service.ScopeNone)

	if !errors.Is(err, service.ErrNeedScope) {
		t.Errorf("err = %v, want ErrNeedScope", err)
	}
}

func TestUpdateThisInstanceOnly(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	if _, err := e.svc.Update(e.ctx, id, service.Changes{Title: ptr("Retro")}, service.ScopeThis); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(16); !equalStrings(got, []string{"09:00 Retro"}) {
		t.Errorf("Nov 16 = %v", got)
	}
	if got := e.titlesOn(23); !equalStrings(got, []string{"09:00 Standup"}) {
		t.Errorf("Nov 23 = %v (other instances must be unchanged)", got)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].EventID != id || ops[0].IfMatch != "" {
		t.Errorf("ops = %+v, want one unconditional patch of the instance", ops)
	}
}

func TestUpdateAllShiftsSeries(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	// Move this instance 09:00 → 10:30, applied to all: every instance shifts by 90m.
	if _, err := e.svc.Update(e.ctx, id, service.Changes{Start: ptr(e.at(16, 10, 30))}, service.ScopeAll); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(9); !equalStrings(got, []string{"10:30 Standup"}) {
		t.Errorf("Nov 9 = %v", got)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].EventID != "std" || ops[0].IfMatch != `"m1"` {
		t.Errorf("ops = %+v, want one patch of the master", ops)
	}
}

func TestUpdateFollowingSplitsSeries(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	next, err := e.svc.Update(e.ctx, id, service.Changes{Title: ptr("Standup v2")}, service.ScopeFollowing)

	if err != nil {
		t.Fatal(err)
	}
	if got := e.titlesOn(9); !equalStrings(got, []string{"09:00 Standup"}) {
		t.Errorf("Nov 9 = %v", got)
	}
	for _, day := range []int{16, 23, 30} {
		if got := e.titlesOn(day); !equalStrings(got, []string{"09:00 Standup v2"}) {
			t.Errorf("Nov %d = %v", day, got)
		}
	}
	ops := e.ops()
	if len(ops) != 2 || ops[0].Kind != store.OpPatch || ops[0].EventID != "std" ||
		ops[1].Kind != store.OpInsert || ops[1].EventID != next.ID {
		t.Errorf("ops = %+v, want patch(master) then insert(new series)", ops)
	}
}

func TestUpdateFollowingAtFirstInstanceUpdatesAll(t *testing.T) {
	e := newEnv(t)
	e.series()
	first := model.InstanceID("std", e.at(2, 9, 0), false)

	if _, err := e.svc.Update(e.ctx, first, service.Changes{Title: ptr("New")}, service.ScopeFollowing); err != nil {
		t.Fatal(err)
	}

	if ops := e.ops(); len(ops) != 1 || ops[0].EventID != "std" {
		t.Errorf("ops = %+v, want a single patch of the master", ops)
	}
}

func TestUpdateColor(t *testing.T) {
	e := newEnv(t)
	e.put(testutil.Timed("e1", "Dinner", e.at(16, 19, 0), time.Hour))

	got, err := e.svc.Update(e.ctx, "e1", service.Changes{ColorID: ptr("4")}, service.ScopeNone)

	if err != nil || got.ColorID != "4" {
		t.Fatalf("Update = %+v, %v", got.ColorID, err)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].Kind != store.OpPatch || ops[0].Payload.ColorID != "4" {
		t.Errorf("ops = %+v, want one patch carrying ColorID 4", ops)
	}
}
