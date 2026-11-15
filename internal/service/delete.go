package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/store"
)

// Delete removes an event. For instances of a series, scope is required.
func (s *Service) Delete(ctx context.Context, id string, scope Scope) error {
	e, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.writableCalendar(ctx, e.CalendarID); err != nil {
		return err
	}
	if !e.IsInstance() {
		return s.store.InTx(ctx, func(tx *store.Store) error {
			if err := tx.DeleteSeries(ctx, s.account, e.CalendarID, e.ID); err != nil { // also fine for one-offs
				return err
			}
			return s.remove(ctx, tx, e)
		})
	}
	master, err := s.store.GetEvent(ctx, s.account, e.CalendarID, e.SeriesID)
	if err != nil {
		return err
	}
	switch scope {
	case ScopeThis:
		e.Status = model.StatusCancelled // kept as a tombstone so Expand hides the slot
		return s.store.InTx(ctx, func(tx *store.Store) error {
			if err := tx.PutEvent(ctx, e); err != nil {
				return err
			}
			return s.remove(ctx, tx, e)
		})
	case ScopeFollowing:
		before, _, err := recur.SplitAt(master, e.OriginalStart)
		if errors.Is(err, recur.ErrSplitAtFirst) {
			return s.Delete(ctx, master.ID, ScopeNone)
		}
		if err != nil {
			return err
		}
		master.Recurrence = before
		return s.store.InTx(ctx, func(tx *store.Store) error {
			if err := tx.DeleteExceptionsFrom(ctx, s.account, master.CalendarID, master.ID, e.OriginalStart); err != nil {
				return err
			}
			return s.save(ctx, tx, master, store.OpPatch)
		})
	case ScopeAll:
		return s.Delete(ctx, master.ID, ScopeNone)
	default:
		return ErrNeedScope
	}
}

// RSVP sets your response. Scope "following" is not supported.
func (s *Service) RSVP(ctx context.Context, id string, resp model.ResponseStatus, scope Scope) error {
	e, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	// Seen through someone else's read-only calendar, Google rejects the reply:
	// answer from your own calendar's copy instead.
	if _, err := s.writableCalendar(ctx, e.CalendarID); err != nil {
		return err
	}
	if e.IsInstance() {
		switch scope {
		case ScopeThis:
		case ScopeAll:
			if e, err = s.store.GetEvent(ctx, s.account, e.CalendarID, e.SeriesID); err != nil {
				return err
			}
		case ScopeFollowing:
			return fmt.Errorf("RSVP supports scope this or all, not following")
		default:
			return ErrNeedScope
		}
	}
	self := e.SelfAttendee()
	if self == nil {
		return ErrNotAttendee
	}
	self.Response = resp
	return s.store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.PutEvent(ctx, e); err != nil {
			return err
		}
		// Unconditional (no If-Match): an RSVP must not fail just because the
		// organizer edited the title meanwhile. Only the attendee list is sent.
		return tx.Enqueue(ctx, store.Op{
			AccountID: e.AccountID, CalendarID: e.CalendarID, EventID: e.ID,
			Kind: store.OpRSVP, Payload: e, SendUpdates: s.updatesFor(e), CreatedAt: s.now(),
		})
	})
}
