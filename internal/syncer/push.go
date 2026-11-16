package syncer

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

// Push sends pending ops in order. It stops at the first transient error,
// because later ops may depend on earlier ones (e.g. a series split).
func (e *Engine) Push(ctx context.Context) error {
	ops, err := e.Store.PendingOps(ctx, e.AccountID)
	if err != nil {
		return err
	}
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		if err := e.pushOne(ctx, op); err != nil {
			if ferr := e.Store.FailOp(ctx, op.Seq, err.Error()); ferr != nil {
				return errors.Join(err, ferr)
			}
			return err
		}
		if op.Kind == store.OpMove { // ops behind a move were retargeted: read them again
			if ops, err = e.Store.PendingOps(ctx, e.AccountID); err != nil {
				return err
			}
			ops = slices.DeleteFunc(ops, func(o store.Op) bool { return o.Seq <= op.Seq })
			i = -1
		}
	}
	return nil
}

// pushOne returns an error only for transient failures. Permanent outcomes
// (conflict, gone, rejected) are resolved here and return nil.
func (e *Engine) pushOne(ctx context.Context, op store.Op) error {
	var got model.Event
	var err error
	switch op.Kind {
	case store.OpInsert:
		got, err = e.Remote.InsertEvent(ctx, op.CalendarID, op.Payload, op.SendUpdates)
		if errors.Is(err, gcal.ErrAlreadyExists) { // an earlier push landed but we never heard back
			got, err = e.Remote.GetEvent(ctx, op.CalendarID, op.EventID)
		}
	case store.OpPatch:
		got, err = e.Remote.PatchEvent(ctx, op.CalendarID, op.Payload, op.IfMatch, op.SendUpdates)
	case store.OpRSVP:
		got, err = e.Remote.RespondEvent(ctx, op.CalendarID, op.Payload, op.SendUpdates)
	case store.OpDelete:
		err = e.Remote.DeleteEvent(ctx, op.CalendarID, op.EventID, op.IfMatch, op.SendUpdates)
		if errors.Is(err, gcal.ErrNotFound) {
			err = nil // already gone: that's what we wanted
		}
	case store.OpMove:
		got, err = e.Remote.MoveEvent(ctx, op.CalendarID, op.EventID, op.Destination, op.SendUpdates)
		if errors.Is(err, gcal.ErrNotFound) { // an earlier push may have landed without our hearing back
			if there, gerr := e.Remote.GetEvent(ctx, op.Destination, op.EventID); gerr == nil {
				got, err = there, nil
			}
		}
	}
	switch {
	case err == nil:
		return e.complete(ctx, op, got)
	case op.Kind == store.OpMove && (errors.Is(err, gcal.ErrPermanent) || errors.Is(err, gcal.ErrNotFound)):
		return e.discard(ctx, op, store.LogError,
			fmt.Sprintf("%q: couldn't move it to another calendar; it stays where it was", title(op)), err)
	case errors.Is(err, gcal.ErrConflict):
		return e.discard(ctx, op, store.LogDiscardedEdit,
			fmt.Sprintf("%q: your edit was discarded because the event changed on Google first", title(op)), err)
	case errors.Is(err, gcal.ErrNotFound):
		return e.discard(ctx, op, store.LogError,
			fmt.Sprintf("%q: no longer exists on Google; your edit was dropped", title(op)), err)
	case errors.Is(err, gcal.ErrPermanent):
		return e.discard(ctx, op, store.LogError,
			fmt.Sprintf("%q: Google rejected the change", title(op)), err)
	default:
		return err
	}
}

func (e *Engine) complete(ctx context.Context, op store.Op, got model.Event) error {
	return e.Store.InTx(ctx, func(tx *store.Store) error {
		superseded, err := tx.CompleteOp(ctx, op, got.ETag)
		if err != nil || superseded || op.Kind == store.OpDelete {
			return err // a newer local edit (or a delete) owns the local row
		}
		if op.Kind == store.OpMove { // edits queued behind the move now need its etag
			if err := tx.RetargetETag(ctx, op.AccountID, op.Destination, op.EventID, got.ETag); err != nil {
				return err
			}
		}
		if owned, err := tx.HasPending(ctx, got.AccountID, got.CalendarID, got.ID); err != nil || owned {
			return err // a later op (e.g. a move after this patch) owns the local row
		}
		return tx.PutEvent(ctx, got) // Google's normalized copy, with the new etag
	})
}

// discard drops an op that can never succeed, logs why, and restores the
// event from Google ("Google wins").
func (e *Engine) discard(ctx context.Context, op store.Op, kind, msg string, cause error) error {
	remote, err := e.Remote.GetEvent(ctx, op.CalendarID, op.EventID)
	gone := errors.Is(err, gcal.ErrNotFound)
	// If Google refuses even to show us the event (e.g. 403: access to the
	// calendar was lost), give up on restoring it: drop the op anyway so the
	// queue keeps moving. The next pull fixes local state (a lost calendar
	// disappears from the calendar list, taking its events with it).
	unreadable := errors.Is(err, gcal.ErrPermanent)
	if err != nil && !gone && !unreadable {
		return err // transient: retry the whole op later
	}
	return e.Store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.DropOp(ctx, op.Seq); err != nil {
			return err
		}
		if op.Kind == store.OpMove { // the rows moved locally: put them back where Google has them
			if err := tx.MoveSeries(ctx, op.AccountID, op.Destination, op.CalendarID, op.EventID); err != nil {
				return err
			}
		}
		if err := tx.AddLog(ctx, store.LogEntry{Time: e.Now(), Kind: kind, AccountID: op.AccountID,
			CalendarID: op.CalendarID, EventID: op.EventID, Message: msg, Detail: cause.Error()}); err != nil {
			return err
		}
		switch {
		case gone:
			return tx.DeleteSeries(ctx, op.AccountID, op.CalendarID, op.EventID)
		case unreadable:
			return nil
		}
		return tx.PutEvent(ctx, remote)
	})
}

func title(op store.Op) string {
	if op.Payload.Title != "" {
		return op.Payload.Title
	}
	return op.EventID
}
