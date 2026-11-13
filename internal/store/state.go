package store

import (
	"context"
	"errors"
	"time"
)

// LastSync returns when the account last completed a full sync (zero if never).
func (s *Store) LastSync(ctx context.Context, accountID string) (time.Time, error) {
	var ts int64
	err := s.q.QueryRowContext(ctx, `SELECT last_success FROM sync_state WHERE account_id = ?`, accountID).Scan(&ts)
	if errors.Is(notFound(err), ErrNotFound) {
		return time.Time{}, nil
	}
	if err != nil || ts == 0 {
		return time.Time{}, err
	}
	return time.Unix(ts, 0), nil
}

func (s *Store) SetLastSync(ctx context.Context, accountID string, t time.Time) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO sync_state (account_id, last_success) VALUES (?, ?)
		ON CONFLICT (account_id) DO UPDATE SET last_success = excluded.last_success`, accountID, t.Unix())
	return err
}

func (s *Store) CalendarsToken(ctx context.Context, accountID string) (string, error) {
	var tok string
	err := s.q.QueryRowContext(ctx, `SELECT calendars_token FROM sync_state WHERE account_id = ?`, accountID).Scan(&tok)
	if errors.Is(notFound(err), ErrNotFound) {
		return "", nil
	}
	return tok, err
}

func (s *Store) SetCalendarsToken(ctx context.Context, accountID, token string) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO sync_state (account_id, calendars_token) VALUES (?, ?)
		ON CONFLICT (account_id) DO UPDATE SET calendars_token = excluded.calendars_token`, accountID, token)
	return err
}

// Log kinds.
const (
	LogDiscardedEdit = "discarded_edit" // a local edit lost to a newer change on Google
	LogError         = "error"          // an op Google rejected permanently
)

type LogEntry struct {
	ID         int64
	Time       time.Time
	Kind       string
	AccountID  string
	CalendarID string
	EventID    string
	Message    string
	Detail     string
}

func (s *Store) AddLog(ctx context.Context, e LogEntry) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO log (ts, kind, account_id, calendar_id, event_id, message, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, e.Time.Unix(), e.Kind, e.AccountID, e.CalendarID, e.EventID, e.Message, e.Detail)
	return err
}

// Logs returns the newest entries first.
func (s *Store) Logs(ctx context.Context, accountID string, limit int) ([]LogEntry, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id, ts, kind, account_id, calendar_id, event_id, message, detail
		FROM log WHERE account_id = ? ORDER BY id DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.Kind, &e.AccountID, &e.CalendarID, &e.EventID, &e.Message, &e.Detail); err != nil {
			return nil, err
		}
		e.Time = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountLogsSince counts entries of kind newer than since (for the TUI status bar).
func (s *Store) CountLogsSince(ctx context.Context, accountID, kind string, since time.Time) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM log WHERE account_id = ? AND kind = ? AND ts >= ?`,
		accountID, kind, since.Unix()).Scan(&n)
	return n, err
}

// ResetSyncTokens forces the next sync to re-download everything.
func (s *Store) ResetSyncTokens(ctx context.Context, accountID string) error {
	return s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.q.ExecContext(ctx, `UPDATE calendars SET sync_token = '' WHERE account_id = ?`, accountID); err != nil {
			return err
		}
		_, err := tx.q.ExecContext(ctx, `UPDATE sync_state SET calendars_token = '' WHERE account_id = ?`, accountID)
		return err
	})
}
