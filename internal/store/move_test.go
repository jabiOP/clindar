package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func move(dest string) store.Op {
	o := op(store.OpMove, "m")
	o.Destination = dest
	o.Payload.CalendarID = dest
	return o
}

func enqueue(t *testing.T, s *store.Store, ops ...store.Op) {
	t.Helper()
	for _, o := range ops {
		if err := s.Enqueue(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMoveAfterInsertReaddressesTheInsert(t *testing.T) {
	s := testutil.NewStore(t)
	enqueue(t, s, op(store.OpInsert, "a"), move("other"))

	ops := pending(t, s)

	if len(ops) != 1 || ops[0].Kind != store.OpInsert || ops[0].CalendarID != "other" || ops[0].Payload.CalendarID != "other" {
		t.Errorf("ops = %+v, want one insert into other", ops)
	}
}

func TestMoveAfterPatchStaysSeparate(t *testing.T) {
	s := testutil.NewStore(t)
	later := op(store.OpPatch, "b")
	later.CalendarID = "other" // after the move, ops are keyed by the destination
	enqueue(t, s, op(store.OpPatch, "a"), move("other"), later)

	ops := pending(t, s)

	if len(ops) != 3 || ops[0].Kind != store.OpPatch || ops[0].CalendarID != testutil.Calendar ||
		ops[1].Kind != store.OpMove || ops[1].Destination != "other" ||
		ops[2].Kind != store.OpPatch || ops[2].CalendarID != "other" {
		t.Errorf("ops = %+v, want patch here, move, patch there", ops)
	}
}

func TestMoveAfterDeleteFails(t *testing.T) {
	s := testutil.NewStore(t)
	enqueue(t, s, op(store.OpDelete, ""))
	if err := s.Enqueue(context.Background(), move("other")); !errors.Is(err, store.ErrPendingDelete) {
		t.Errorf("err = %v, want ErrPendingDelete", err)
	}
}

// An insert re-addressed while it was being pushed landed in the old calendar.
func TestReaddressedInsertLandedInTheOldCalendar(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	enqueue(t, s, op(store.OpInsert, "a"))
	inFlight := pending(t, s)[0]
	enqueue(t, s, move("other"))

	superseded, err := s.CompleteOp(ctx, inFlight, `"etag-1"`)

	if err != nil || !superseded {
		t.Fatalf("CompleteOp = %v, %v; want superseded", superseded, err)
	}
	ops := pending(t, s)
	if len(ops) != 2 || ops[0].Kind != store.OpMove || ops[0].CalendarID != testutil.Calendar ||
		ops[0].Destination != "other" || ops[1].Kind != store.OpPatch || ops[1].CalendarID != "other" ||
		ops[1].IfMatch != `"etag-1"` {
		t.Errorf("ops = %+v, want a move to other, then a patch there", ops)
	}
}

func TestHasPendingSeesMovesIntoACalendar(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	enqueue(t, s, move("other"))

	for _, c := range []struct {
		cal       string
		has, move bool
	}{{testutil.Calendar, true, true}, {"other", true, false}, {"third", false, false}} {
		has, _ := s.HasPending(ctx, testutil.Account, c.cal, "e1")
		moving, _ := s.MovePending(ctx, testutil.Account, c.cal, "e1")
		if has != c.has || moving != c.move {
			t.Errorf("%s: HasPending %v MovePending %v, want %v %v", c.cal, has, moving, c.has, c.move)
		}
	}
}

func TestRetargetETagLeavesRSVPsAlone(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	p, r := op(store.OpPatch, "a"), rsvp("b")
	r.EventID = "e2"
	enqueue(t, s, p, r)
	s.RetargetETag(ctx, testutil.Account, testutil.Calendar, "e1", `"new"`)
	s.RetargetETag(ctx, testutil.Account, testutil.Calendar, "e2", `"new"`)

	ops := pending(t, s)

	if ops[0].IfMatch != `"new"` || ops[1].IfMatch != "" {
		t.Errorf("if-match = %q, %q; want the patch retargeted, the rsvp unconditional", ops[0].IfMatch, ops[1].IfMatch)
	}
}

func putSeries(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	ny := testutil.NY(t)
	start := time.Date(2026, 11, 16, 9, 0, 0, 0, ny)
	master := testutil.Timed("s1", "Yoga", start, time.Hour)
	master.Recurrence = []string{"RRULE:FREQ=DAILY;COUNT=5"}
	ex := testutil.Timed(model.InstanceID("s1", start.AddDate(0, 0, 1), false), "Yoga (late)", start.AddDate(0, 0, 1).Add(time.Hour), time.Hour)
	ex.SeriesID, ex.OriginalStart = "s1", start.AddDate(0, 0, 1)
	other := testutil.Timed("e9", "Lunch", start.Add(3*time.Hour), time.Hour)
	for _, e := range []model.Event{master, ex, other} {
		if err := s.PutEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMoveSeriesTakesItsExceptions(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	putSeries(t, s)

	if err := s.MoveSeries(ctx, testutil.Account, testutil.Calendar, "other", "s1"); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"s1", model.InstanceID("s1", time.Date(2026, 11, 17, 9, 0, 0, 0, testutil.NY(t)), false)} {
		e, err := s.GetEvent(ctx, testutil.Account, "other", id)
		if err != nil || e.CalendarID != "other" {
			t.Errorf("%s in other: %+v, %v", id, e.CalendarID, err)
		}
	}
	if _, err := s.GetEvent(ctx, testutil.Account, testutil.Calendar, "e9"); err != nil {
		t.Errorf("unrelated event moved: %v", err)
	}
}

func TestFullResyncKeepsAMovedSeries(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	putSeries(t, s)
	s.MoveSeries(ctx, testutil.Account, testutil.Calendar, "other", "s1")
	m := move("other")
	m.EventID = "s1"
	enqueue(t, s, m)

	if err := s.ClearCalendarEvents(ctx, testutil.Account, "other"); err != nil {
		t.Fatal(err)
	}

	if ex, _ := s.Exceptions(ctx, testutil.Account, "other", "s1"); len(ex) != 1 {
		t.Errorf("exceptions = %d, want the moved one kept", len(ex))
	}
	if _, err := s.GetEvent(ctx, testutil.Account, "other", "s1"); err != nil {
		t.Errorf("master: %v", err)
	}
}
