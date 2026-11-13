package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jabiOP/clindar/internal/model"
)

// UpsertCalendar inserts or updates a calendar, preserving its sync token.
func (s *Store) UpsertCalendar(ctx context.Context, c model.Calendar) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, `
		INSERT INTO calendars (account_id, id, data) VALUES (?, ?, ?)
		ON CONFLICT (account_id, id) DO UPDATE SET data = excluded.data`,
		c.AccountID, c.ID, string(data))
	return err
}

// DeleteCalendar removes a calendar and its cached events.
func (s *Store) DeleteCalendar(ctx context.Context, accountID, calendarID string) error {
	return s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.q.ExecContext(ctx,
			`DELETE FROM events WHERE account_id = ? AND calendar_id = ?`, accountID, calendarID); err != nil {
			return err
		}
		_, err := tx.q.ExecContext(ctx,
			`DELETE FROM calendars WHERE account_id = ? AND id = ?`, accountID, calendarID)
		return err
	})
}

// Calendars returns the account's calendars, primary first, then by name.
func (s *Store) Calendars(ctx context.Context, accountID string) ([]model.Calendar, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT data FROM calendars WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cals []model.Calendar
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var c model.Calendar
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return nil, fmt.Errorf("decode calendar: %w", err)
		}
		cals = append(cals, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortCalendars(cals)
	return cals, nil
}

func (s *Store) CalendarSyncToken(ctx context.Context, accountID, calendarID string) (string, error) {
	var tok string
	err := s.q.QueryRowContext(ctx,
		`SELECT sync_token FROM calendars WHERE account_id = ? AND id = ?`, accountID, calendarID).Scan(&tok)
	return tok, notFound(err)
}

func (s *Store) SetCalendarSyncToken(ctx context.Context, accountID, calendarID, token string) error {
	_, err := s.q.ExecContext(ctx,
		`UPDATE calendars SET sync_token = ? WHERE account_id = ? AND id = ?`, token, accountID, calendarID)
	return err
}
