package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jabiOP/clindar/internal/model"
)

const eventCols = `data`

// PutEvent inserts or replaces an event.
func (s *Store) PutEvent(ctx context.Context, e model.Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	var originalTS int64
	if !e.OriginalStart.IsZero() {
		originalTS = e.OriginalStart.Unix()
	}
	_, err = s.q.ExecContext(ctx, `
		INSERT INTO events (account_id, calendar_id, id, series_id, is_master, status, start_ts, end_ts, original_ts, data)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, calendar_id, id) DO UPDATE SET
			series_id = excluded.series_id, is_master = excluded.is_master, status = excluded.status,
			start_ts = excluded.start_ts, end_ts = excluded.end_ts, original_ts = excluded.original_ts,
			data = excluded.data`,
		e.AccountID, e.CalendarID, e.ID, e.SeriesID, e.IsMaster(), e.Status,
		e.Start.Unix(), e.End.Unix(), originalTS, string(data))
	return err
}

func (s *Store) GetEvent(ctx context.Context, accountID, calendarID, id string) (model.Event, error) {
	row := s.q.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events
		WHERE account_id = ? AND calendar_id = ? AND id = ?`, accountID, calendarID, id)
	return scanEvent(row)
}

// FindEvent looks an event up by ID alone, across calendars. The same event can
// appear in two calendars (e.g. an invite to you and to a shared calendar);
// then it returns ErrAmbiguous.
func (s *Store) FindEvent(ctx context.Context, accountID, id string) (model.Event, error) {
	events, err := s.queryEvents(ctx, `WHERE account_id = ? AND id = ?`, accountID, id)
	if err != nil {
		return model.Event{}, err
	}
	switch len(events) {
	case 0:
		return model.Event{}, ErrNotFound
	case 1:
		return events[0], nil
	default:
		return model.Event{}, fmt.Errorf("event %s: %w", id, ErrAmbiguous)
	}
}

func (s *Store) DeleteEvent(ctx context.Context, accountID, calendarID, id string) error {
	_, err := s.q.ExecContext(ctx,
		`DELETE FROM events WHERE account_id = ? AND calendar_id = ? AND id = ?`, accountID, calendarID, id)
	return err
}

// DeleteSeries deletes a master and all its exceptions.
func (s *Store) DeleteSeries(ctx context.Context, accountID, calendarID, seriesID string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM events
		WHERE account_id = ? AND calendar_id = ? AND (id = ? OR series_id = ?)`,
		accountID, calendarID, seriesID, seriesID)
	return err
}

// DeleteExceptionsFrom deletes a series' exceptions whose original slot is at or after from.
// Used when a series is truncated ("this and following").
func (s *Store) DeleteExceptionsFrom(ctx context.Context, accountID, calendarID, seriesID string, from time.Time) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM events
		WHERE account_id = ? AND calendar_id = ? AND series_id = ? AND original_ts >= ?`,
		accountID, calendarID, seriesID, from.Unix())
	return err
}

func (s *Store) queryEvents(ctx context.Context, where string, args ...any) ([]model.Event, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+eventCols+` FROM events `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scanEvent(row scanner) (model.Event, error) {
	var data string
	if err := row.Scan(&data); err != nil {
		return model.Event{}, notFound(err)
	}
	var e model.Event
	if err := json.Unmarshal([]byte(data), &e); err != nil {
		return model.Event{}, fmt.Errorf("decode event: %w", err)
	}
	return e, nil
}
