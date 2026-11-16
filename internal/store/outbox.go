package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jabiOP/clindar/internal/model"
)

type OpKind string

const (
	OpInsert OpKind = "insert"
	OpPatch  OpKind = "patch"
	OpRSVP   OpKind = "rsvp" // send only the attendee list (your response), unconditionally
	OpDelete OpKind = "delete"
	OpMove   OpKind = "move" // events.move to Destination; payload is the event (for log lines)
)

// Op is one pending change waiting to be pushed to Google.
type Op struct {
	Seq         int64
	AccountID   string
	CalendarID  string
	EventID     string
	Kind        OpKind
	Destination string      // move only: the calendar the event goes to
	Payload     model.Event // the event's full local state, for insert/patch
	IfMatch     string      // etag to send as If-Match; "" = unconditional (a move records it, never sends it)
	SendUpdates string      // "all" | "none"
	Version     int         // bumped whenever a newer edit is coalesced into this op
	Attempts    int
	LastError   string
	CreatedAt   time.Time // informational; set by the caller from its clock
}

var ErrPendingDelete = errors.New("event has a pending delete")

// Enqueue records a local change. There is at most one pending op per event;
// a new change is merged into the existing one (design §3):
//
//	existing  new     result
//	-         any     new op
//	insert    patch   insert (new payload)
//	patch     patch   patch  (new payload, original If-Match)
//	insert    rsvp    insert (new payload: the insert carries the response)
//	patch     rsvp    patch  (new payload: the patch carries the response)
//	rsvp      rsvp    rsvp   (new payload)
//	rsvp      patch   patch  (new payload, the PATCH's If-Match: see below)
//	rsvp      delete  delete (the DELETE's If-Match)
//	insert    delete  delete (cheap 404 if the insert never landed; safe if it was in flight)
//	patch     delete  delete (original If-Match)
//	delete    any     ErrPendingDelete
//	any       insert  error  (IDs are unique; this is a bug)
//	insert    move    insert, re-addressed to the destination (it hasn't reached Google)
//	other     move    a separate move after it (the patch goes to the source calendar)
//	move      any     a separate op (a move is never merged into)
//
// Ops are keyed by (account, calendar, event), and after a move the event's
// later ops are keyed by the destination, so those don't meet the move.
//
// If-Match normally stays the existing op's: it is the etag the user's FIRST
// pending change was based on. The exception is a pending rsvp, which has no
// If-Match on purpose; a patch or delete merged into it must keep its own,
// or it would silently overwrite changes made on Google (design §3).
func (s *Store) Enqueue(ctx context.Context, op Op) error {
	return s.InTx(ctx, func(tx *Store) error {
		existing, err := tx.pendingFor(ctx, op.AccountID, op.CalendarID, op.EventID)
		if errors.Is(err, ErrNotFound) {
			return tx.insertOp(ctx, op)
		}
		if err != nil {
			return err
		}
		switch {
		case existing.Kind == OpDelete:
			return fmt.Errorf("enqueue %s for %s: %w", op.Kind, op.EventID, ErrPendingDelete)
		case op.Kind == OpInsert:
			return fmt.Errorf("enqueue insert for %s: already has a pending %s", op.EventID, existing.Kind)
		}
		switch {
		case op.Kind == OpMove && existing.Kind == OpInsert:
			return tx.readdressInsert(ctx, existing.Seq, op.Destination, op.Payload)
		case op.Kind == OpMove || existing.Kind == OpMove:
			return tx.insertOp(ctx, op)
		}
		kind, ifMatch := existing.Kind, existing.IfMatch // insert/patch already send the attendee list
		if existing.Kind == OpRSVP {
			kind, ifMatch = op.Kind, op.IfMatch
		}
		if op.Kind == OpDelete {
			return tx.rewriteOp(ctx, existing.Seq, OpDelete, model.Event{}, ifMatch, op.SendUpdates)
		}
		return tx.rewriteOp(ctx, existing.Seq, kind, op.Payload, ifMatch, op.SendUpdates)
	})
}

func (s *Store) insertOp(ctx context.Context, op Op) error {
	payload, err := encodePayload(op.Kind, op.Payload)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, `INSERT INTO outbox
		(account_id, calendar_id, event_id, op, destination, payload, if_match, send_updates, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		op.AccountID, op.CalendarID, op.EventID, op.Kind, op.Destination, payload, op.IfMatch,
		sendUpdatesOrNone(op.SendUpdates), op.CreatedAt.Unix())
	return err
}

// readdressInsert points a pending insert at another calendar: moving an event
// that hasn't reached Google yet is just creating it somewhere else.
func (s *Store) readdressInsert(ctx context.Context, seq int64, calendarID string, payload model.Event) error {
	p, err := encodePayload(OpInsert, payload)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, `UPDATE outbox SET calendar_id = ?, payload = ?, version = version + 1
		WHERE seq = ?`, calendarID, p, seq)
	return err
}

func (s *Store) rewriteOp(ctx context.Context, seq int64, kind OpKind, payload model.Event, ifMatch, sendUpdates string) error {
	p, err := encodePayload(kind, payload)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, `UPDATE outbox SET op = ?, payload = ?, if_match = ?, send_updates = ?,
		version = version + 1 WHERE seq = ?`, kind, p, ifMatch, sendUpdatesOrNone(sendUpdates), seq)
	return err
}

func (s *Store) pendingFor(ctx context.Context, accountID, calendarID, eventID string) (Op, error) {
	ops, err := s.queryOps(ctx, `WHERE account_id = ? AND calendar_id = ? AND event_id = ? ORDER BY seq DESC LIMIT 1`,
		accountID, calendarID, eventID)
	if err != nil {
		return Op{}, err
	}
	if len(ops) == 0 {
		return Op{}, ErrNotFound
	}
	return ops[0], nil
}

// PendingOps returns the account's ops in the order they must be pushed.
func (s *Store) PendingOps(ctx context.Context, accountID string) ([]Op, error) {
	return s.queryOps(ctx, `WHERE account_id = ? ORDER BY seq`, accountID)
}

func (s *Store) PendingCount(ctx context.Context, accountID string) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM outbox WHERE account_id = ?`, accountID).Scan(&n)
	return n, err
}

// HasPending reports whether a local op still owns this event in this
// calendar: one addressed to it, or a move into it that hasn't landed.
func (s *Store) HasPending(ctx context.Context, accountID, calendarID, eventID string) (bool, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM outbox WHERE account_id = ? AND event_id = ?
		AND (calendar_id = ? OR destination = ?)`, accountID, eventID, calendarID, calendarID).Scan(&n)
	return n > 0, err
}

// MovePending reports whether a move of this event (or series) out of this
// calendar hasn't landed yet.
func (s *Store) MovePending(ctx context.Context, accountID, calendarID, eventID string) (bool, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM outbox WHERE account_id = ? AND calendar_id = ?
		AND event_id = ? AND op = 'move'`, accountID, calendarID, eventID).Scan(&n)
	return n > 0, err
}

// RetargetETag points the pending ops for an event in a calendar at etag.
// After a move lands, ops queued behind it were based on the content the
// move kept. RSVPs (no If-Match on purpose) are left alone.
func (s *Store) RetargetETag(ctx context.Context, accountID, calendarID, eventID, etag string) error {
	_, err := s.q.ExecContext(ctx, `UPDATE outbox SET if_match = ? WHERE account_id = ? AND calendar_id = ?
		AND event_id = ? AND if_match != ''`, etag, accountID, calendarID, eventID)
	return err
}

// CompleteOp removes an op after Google accepted it. If the user changed the
// event while the op was in flight (version moved on), the op is kept but
// retargeted at Google's new etag (and an insert becomes a patch), and
// superseded is true: the caller must NOT overwrite the local event.
func (s *Store) CompleteOp(ctx context.Context, op Op, newETag string) (superseded bool, err error) {
	err = s.InTx(ctx, func(tx *Store) error {
		res, err := tx.q.ExecContext(ctx, `DELETE FROM outbox WHERE seq = ? AND version = ?`, op.Seq, op.Version)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return nil
		}
		superseded = true
		now, err := tx.queryOps(ctx, `WHERE seq = ?`, op.Seq)
		if err != nil || len(now) == 0 {
			return err
		}
		if op.Kind == OpInsert && now[0].CalendarID != op.CalendarID {
			// Re-addressed while in flight: it landed in op's calendar. The row
			// becomes a move from there (same seq: first), and a copy as a patch
			// for the destination carries the latest content.
			if _, err := tx.q.ExecContext(ctx, `INSERT INTO outbox
				(account_id, calendar_id, event_id, op, payload, if_match, send_updates, created_at)
				SELECT account_id, calendar_id, event_id, 'patch', payload, ?, send_updates, created_at
				FROM outbox WHERE seq = ?`, newETag, op.Seq); err != nil {
				return err
			}
			_, err = tx.q.ExecContext(ctx, `UPDATE outbox SET op = 'move', destination = calendar_id,
				calendar_id = ?, if_match = ? WHERE seq = ?`, op.CalendarID, newETag, op.Seq)
			return err
		}
		_, err = tx.q.ExecContext(ctx, `UPDATE outbox SET if_match = ?,
			op = CASE op WHEN 'insert' THEN 'patch' ELSE op END WHERE seq = ?`, newETag, op.Seq)
		return err
	})
	return superseded, err
}

// DropOp removes an op that can never succeed (conflict or permanent error).
func (s *Store) DropOp(ctx context.Context, seq int64) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM outbox WHERE seq = ?`, seq)
	return err
}

// FailOp records a transient failure; the op will be retried next sync.
func (s *Store) FailOp(ctx context.Context, seq int64, msg string) error {
	_, err := s.q.ExecContext(ctx, `UPDATE outbox SET attempts = attempts + 1, last_error = ? WHERE seq = ?`, msg, seq)
	return err
}

func (s *Store) queryOps(ctx context.Context, where string, args ...any) ([]Op, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT seq, account_id, calendar_id, event_id, op, destination, payload,
		if_match, send_updates, version, attempts, last_error FROM outbox `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ops []Op
	for rows.Next() {
		var op Op
		var payload string
		if err := rows.Scan(&op.Seq, &op.AccountID, &op.CalendarID, &op.EventID, &op.Kind, &op.Destination, &payload,
			&op.IfMatch, &op.SendUpdates, &op.Version, &op.Attempts, &op.LastError); err != nil {
			return nil, err
		}
		if payload != "" {
			if err := json.Unmarshal([]byte(payload), &op.Payload); err != nil {
				return nil, fmt.Errorf("decode outbox payload: %w", err)
			}
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

func encodePayload(kind OpKind, e model.Event) (string, error) {
	if kind == OpDelete {
		return "", nil
	}
	b, err := json.Marshal(e)
	return string(b), err
}

func sendUpdatesOrNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
