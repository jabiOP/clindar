package store

import (
	"context"
	"time"

	"github.com/jabiOP/clindar/internal/model"
)

// SinglesInRange returns non-cancelled events overlapping [from, to) that
// aren't produced by expanding a stored series: one-off events, plus
// "orphan" instances. An orphan is an occurrence whose series is not in this
// calendar, which is what you get when someone invites you to just one
// occurrence of their recurring meeting.
func (s *Store) SinglesInRange(ctx context.Context, accountID string, from, to time.Time) ([]model.Event, error) {
	// SQL filters coarsely with a one-day pad (all-day events are stored as UTC
	// midnights, which can be up to ±14h from local midnight). Overlaps is exact.
	const pad = 24 * 60 * 60
	candidates, err := s.queryEvents(ctx, `WHERE account_id = ? AND is_master = 0
		AND status != 'cancelled' AND start_ts < ? AND end_ts > ?
		AND (series_id = '' OR NOT EXISTS (
			SELECT 1 FROM events AS m
			WHERE m.account_id = events.account_id AND m.calendar_id = events.calendar_id AND m.id = events.series_id))`,
		accountID, to.Unix()+pad, from.Unix()-pad)
	if err != nil {
		return nil, err
	}
	var out []model.Event
	for _, e := range candidates {
		if e.Overlaps(from, to) {
			out = append(out, e)
		}
	}
	return out, nil
}

// MastersBefore returns non-cancelled series masters that start before t.
// (A series has no instances before its own start, so later masters can't matter.)
func (s *Store) MastersBefore(ctx context.Context, accountID string, t time.Time) ([]model.Event, error) {
	const pad = 24 * 60 * 60
	return s.queryEvents(ctx, `WHERE account_id = ? AND is_master = 1 AND status != 'cancelled' AND start_ts < ?`,
		accountID, t.Unix()+pad)
}

// Exceptions returns every stored exception (including cancelled ones) of a series.
func (s *Store) Exceptions(ctx context.Context, accountID, calendarID, seriesID string) ([]model.Event, error) {
	return s.queryEvents(ctx, `WHERE account_id = ? AND calendar_id = ? AND series_id = ?`,
		accountID, calendarID, seriesID)
}

// ClearCalendarEvents deletes a calendar's cached events, except ones with
// pending outbox ops (local edits that haven't reached Google yet).
func (s *Store) ClearCalendarEvents(ctx context.Context, accountID, calendarID string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM events
		WHERE account_id = ? AND calendar_id = ?
		AND id NOT IN (SELECT event_id FROM outbox WHERE account_id = ? AND calendar_id = ?)`,
		accountID, calendarID, accountID, calendarID)
	return err
}
