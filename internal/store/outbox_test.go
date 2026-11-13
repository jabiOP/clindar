package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func op(kind store.OpKind, title string) store.Op {
	return store.Op{
		AccountID: testutil.Account, CalendarID: testutil.Calendar, EventID: "e1",
		Kind: kind, Payload: model.Event{ID: "e1", Title: title}, IfMatch: `"etag-` + title + `"`,
	}
}

// rsvp is how the service queues RSVPs: deliberately without If-Match.
func rsvp(title string) store.Op {
	o := op(store.OpRSVP, title)
	o.IfMatch = ""
	return o
}

func pending(t *testing.T, s *store.Store) []store.Op {
	t.Helper()
	ops, err := s.PendingOps(context.Background(), testutil.Account)
	if err != nil {
		t.Fatal(err)
	}
	return ops
}

func TestEnqueueCoalescing(t *testing.T) {
	tests := []struct {
		name        string
		ops         []store.Op
		wantKind    store.OpKind // "" = no pending op
		wantTitle   string
		wantIfMatch string
	}{
		{"single insert", []store.Op{op(store.OpInsert, "a")}, store.OpInsert, "a", `"etag-a"`},
		{"insert then patch stays insert", []store.Op{op(store.OpInsert, "a"), op(store.OpPatch, "b")}, store.OpInsert, "b", `"etag-a"`},
		{"patch then patch keeps first If-Match", []store.Op{op(store.OpPatch, "a"), op(store.OpPatch, "b")}, store.OpPatch, "b", `"etag-a"`},
		{"insert then delete becomes delete", []store.Op{op(store.OpInsert, "a"), op(store.OpDelete, "")}, store.OpDelete, "", `"etag-a"`},
		{"patch then delete becomes delete", []store.Op{op(store.OpPatch, "a"), op(store.OpDelete, "")}, store.OpDelete, "", `"etag-a"`},
		{"patch then rsvp stays patch", []store.Op{op(store.OpPatch, "a"), rsvp("b")}, store.OpPatch, "b", `"etag-a"`},
		{"rsvp then rsvp stays unconditional", []store.Op{rsvp("a"), rsvp("b")}, store.OpRSVP, "b", ""},
		// A pending RSVP has no If-Match; an edit merged into it must keep its own,
		// or it would overwrite the organizer's changes instead of conflicting.
		{"rsvp then patch takes the patch's If-Match", []store.Op{rsvp("a"), op(store.OpPatch, "b")}, store.OpPatch, "b", `"etag-b"`},
		{"rsvp then delete takes the delete's If-Match", []store.Op{rsvp("a"), op(store.OpDelete, "")}, store.OpDelete, "", `"etag-"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testutil.NewStore(t)
			for _, o := range tt.ops {
				if err := s.Enqueue(context.Background(), o); err != nil {
					t.Fatal(err)
				}
			}

			ops := pending(t, s)

			if len(ops) != 1 {
				t.Fatalf("got %d pending ops, want 1: %+v", len(ops), ops)
			}
			if ops[0].Kind != tt.wantKind || ops[0].Payload.Title != tt.wantTitle || ops[0].IfMatch != tt.wantIfMatch {
				t.Errorf("got kind=%s title=%q ifMatch=%s, want %s %q %s",
					ops[0].Kind, ops[0].Payload.Title, ops[0].IfMatch, tt.wantKind, tt.wantTitle, tt.wantIfMatch)
			}
		})
	}
}

func TestEnqueueAfterDeleteFails(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	if err := s.Enqueue(ctx, op(store.OpDelete, "")); err != nil {
		t.Fatal(err)
	}

	err := s.Enqueue(ctx, op(store.OpPatch, "late"))

	if !errors.Is(err, store.ErrPendingDelete) {
		t.Errorf("err = %v, want ErrPendingDelete", err)
	}
}

func TestOpsForDifferentEventsKeepOrder(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	a, b := op(store.OpPatch, "a"), op(store.OpInsert, "b")
	b.EventID = "e2"
	for _, o := range []store.Op{a, b} {
		if err := s.Enqueue(ctx, o); err != nil {
			t.Fatal(err)
		}
	}

	ops := pending(t, s)

	if len(ops) != 2 || ops[0].EventID != "e1" || ops[1].EventID != "e2" {
		t.Errorf("ops = %+v, want e1 then e2", ops)
	}
}

func TestCompleteOpRemovesIt(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	s.Enqueue(ctx, op(store.OpPatch, "a"))
	inFlight := pending(t, s)[0]

	superseded, err := s.CompleteOp(ctx, inFlight, `"new"`)

	if err != nil || superseded {
		t.Fatalf("CompleteOp = %v, %v; want false, nil", superseded, err)
	}
	if ops := pending(t, s); len(ops) != 0 {
		t.Errorf("still pending: %+v", ops)
	}
}

// The user edits an event while its insert is being pushed. The newer edit must
// survive, and become a patch against the etag Google just returned.
func TestCompleteOpWhenEditedInFlight(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	s.Enqueue(ctx, op(store.OpInsert, "a"))
	inFlight := pending(t, s)[0]
	s.Enqueue(ctx, op(store.OpPatch, "b")) // arrives during the push

	superseded, err := s.CompleteOp(ctx, inFlight, `"server-etag"`)

	if err != nil || !superseded {
		t.Fatalf("CompleteOp = %v, %v; want true, nil", superseded, err)
	}
	ops := pending(t, s)
	if len(ops) != 1 {
		t.Fatalf("pending = %+v, want 1 op", ops)
	}
	if ops[0].Kind != store.OpPatch || ops[0].Payload.Title != "b" || ops[0].IfMatch != `"server-etag"` {
		t.Errorf("got %s %q %s, want patch \"b\" \"server-etag\"", ops[0].Kind, ops[0].Payload.Title, ops[0].IfMatch)
	}
}

func TestFailOpCountsAttempts(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	s.Enqueue(ctx, op(store.OpPatch, "a"))
	seq := pending(t, s)[0].Seq

	s.FailOp(ctx, seq, "timeout")
	s.FailOp(ctx, seq, "timeout again")

	got := pending(t, s)[0]
	if got.Attempts != 2 || got.LastError != "timeout again" {
		t.Errorf("attempts=%d lastError=%q", got.Attempts, got.LastError)
	}
}
