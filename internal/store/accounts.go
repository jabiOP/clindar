package store

import (
	"context"
)

// EnsureAccount records an account ID (the user's email). Safe to call repeatedly.
func (s *Store) EnsureAccount(ctx context.Context, id string) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO accounts (id) VALUES (?) ON CONFLICT DO NOTHING`, id)
	return err
}

func (s *Store) Accounts(ctx context.Context) ([]string, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id FROM accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DeleteAccount removes an account and everything belonging to it.
func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	return s.InTx(ctx, func(tx *Store) error {
		for _, table := range []string{"events", "calendars", "calendar_prefs", "event_colors", "outbox", "sync_state", "log"} {
			if _, err := tx.q.ExecContext(ctx, `DELETE FROM `+table+` WHERE account_id = ?`, id); err != nil {
				return err
			}
		}
		_, err := tx.q.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
		return err
	})
}
