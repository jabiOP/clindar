// Package store persists calendars, events, the outbox, and sync state in SQLite.
// It is the only package that writes SQL.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

var (
	ErrNotFound  = errors.New("not found")
	ErrAmbiguous = errors.New("ambiguous: matches more than one event")
)

// querier is satisfied by both *sql.DB and *sql.Tx, so every method works
// the same inside or outside a transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db *sql.DB // nil when this Store is bound to a transaction
	q  querier
}

// Open opens (creating if needed) the database at path and migrates it.
func Open(path string) (*Store, error) {
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return &Store{db: db, q: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// InTx runs fn inside one transaction. If fn returns an error, nothing it did
// is saved. Calling InTx on a Store already inside a transaction joins it.
func (s *Store) InTx(ctx context.Context, fn func(tx *Store) error) error {
	if s.db == nil {
		return fn(s)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&Store{q: tx}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		// PRAGMA can't take a bound parameter, hence Sprintf. i is our own int, not user input.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// migrations[i] upgrades the schema from version i to i+1.
// NEVER edit a migration that has been released: append a new one.
var migrations = []string{`
CREATE TABLE accounts (
	id TEXT PRIMARY KEY
);
CREATE TABLE calendars (
	account_id  TEXT NOT NULL,
	id          TEXT NOT NULL,
	data        TEXT NOT NULL,              -- JSON model.Calendar
	sync_token  TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (account_id, id)
);
CREATE TABLE events (
	account_id  TEXT NOT NULL,
	calendar_id TEXT NOT NULL,
	id          TEXT NOT NULL,
	series_id   TEXT NOT NULL DEFAULT '',
	is_master   INTEGER NOT NULL,
	status      TEXT NOT NULL,
	start_ts    INTEGER NOT NULL,           -- unix seconds
	end_ts      INTEGER NOT NULL,
	original_ts INTEGER NOT NULL DEFAULT 0, -- exceptions: unix seconds of OriginalStart
	data        TEXT NOT NULL,              -- JSON model.Event
	PRIMARY KEY (account_id, calendar_id, id)
);
CREATE INDEX events_by_start  ON events (account_id, start_ts);
CREATE INDEX events_by_series ON events (account_id, calendar_id, series_id);
CREATE INDEX events_by_id     ON events (account_id, id);
CREATE TABLE outbox (
	seq          INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id   TEXT NOT NULL,
	calendar_id  TEXT NOT NULL,
	event_id     TEXT NOT NULL,
	op           TEXT NOT NULL CHECK (op IN ('insert', 'patch', 'rsvp', 'delete')),
	payload      TEXT NOT NULL DEFAULT '',  -- JSON model.Event for insert/patch
	if_match     TEXT NOT NULL DEFAULT '',
	send_updates TEXT NOT NULL DEFAULT 'none',
	version      INTEGER NOT NULL DEFAULT 1, -- bumped each time the op is coalesced
	attempts     INTEGER NOT NULL DEFAULT 0,
	last_error   TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL
);
CREATE INDEX outbox_by_event ON outbox (account_id, calendar_id, event_id);
CREATE TABLE sync_state (
	account_id      TEXT PRIMARY KEY,
	calendars_token TEXT NOT NULL DEFAULT '',
	last_success    INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE log (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	ts          INTEGER NOT NULL,
	kind        TEXT NOT NULL,
	account_id  TEXT NOT NULL DEFAULT '',
	calendar_id TEXT NOT NULL DEFAULT '',
	event_id    TEXT NOT NULL DEFAULT '',
	message     TEXT NOT NULL,
	detail      TEXT NOT NULL DEFAULT ''
);
`,
	// 2: local calendar visibility (Task 14.12)
	`
CREATE TABLE calendar_prefs (
	account_id  TEXT NOT NULL,
	calendar_id TEXT NOT NULL,
	shown       INTEGER NOT NULL, -- local choice; overrides Google's "selected"
	PRIMARY KEY (account_id, calendar_id)
);
`}
