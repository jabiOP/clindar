# Phase 2: SQLite store

The store is the **only** package that writes SQL. Everything is keyed by `account_id` (design §0, Q9).
Read `00-design.md` §2–3 and the SQLite section of the README primer before starting.

**Dependencies for this phase:**
```bash
go get modernc.org/sqlite@v1.62.0 github.com/google/go-cmp@v0.7.0
```

**Design notes you need:**
- Events are stored as **indexed columns + a JSON blob** of `model.Event`. The columns exist only for
  `WHERE` clauses. The blob is the truth. Adding a field to `model.Event` needs no migration.
- `querier` is an interface satisfied by both `*sql.DB` and `*sql.Tx`. That lets every method run inside or
  outside a transaction with no duplicate code (`InTx`).
- Tests use a **real SQLite file in `t.TempDir()`**. It's fast (milliseconds) and tests the real SQL. Never mock the database.

---

## Task 2.1: Open, migrate, transactions, accounts, test helpers

**Goal:** open/create the DB with the right pragmas, run migrations once, run work in a transaction, and
record accounts. Also add the shared test helpers that every later phase uses.

**Files:**
- Create `internal/testutil/testutil.go` (shared fixtures; only `_test.go` files may import it)
- Create `internal/store/store.go`, `internal/store/helpers.go`, `internal/store/accounts.go`
- Create `internal/store/store_test.go`

**Steps**

1. Write the test helpers. `internal/testutil/testutil.go`:
```go
// Package testutil holds helpers shared by tests in several packages.
// Only _test.go files may import it.
package testutil

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/store"
)

// NewStore opens a fresh database in a temp dir, closed automatically.
func NewStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// NY is the zone most tests use. Loading it needs time/tzdata (imported by testutil's users or main).
func NY(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	return loc
}

// IDs used by fixtures.
const (
	Account  = "me@example.com"
	Calendar = "me@example.com"
)

// Timed returns a confirmed one-off event in the fixture account/calendar.
func Timed(id, title string, start time.Time, dur time.Duration) model.Event {
	return model.Event{
		AccountID: Account, CalendarID: Calendar, ID: id,
		Status: model.StatusConfirmed, Title: title,
		Start: start, End: start.Add(dur), TimeZone: start.Location().String(),
	}
}

// AllDay returns a confirmed all-day event covering `days` days from y-m-d.
func AllDay(id, title string, y int, m time.Month, d, days int) model.Event {
	start := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return model.Event{
		AccountID: Account, CalendarID: Calendar, ID: id,
		Status: model.StatusConfirmed, Title: title, AllDay: true,
		Start: start, End: start.AddDate(0, 0, days),
	}
}
```
2. Write the tests. `internal/store/store_test.go`:
```go
package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/yourname/clindar/internal/store"
	"github.com/yourname/clindar/internal/testutil"
)

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	for i := range 2 { // second open must not re-run migrations
		s, err := store.Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		s.Close()
	}
}

func TestInTxRollsBackOnError(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	boom := errors.New("boom")

	err := s.InTx(ctx, func(tx *store.Store) error {
		if err := tx.EnsureAccount(ctx, "a@x.com"); err != nil {
			return err
		}
		return boom
	})

	if !errors.Is(err, boom) {
		t.Fatalf("InTx error = %v, want boom", err)
	}
	ids, err := s.Accounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("accounts = %v, want none (rolled back)", ids)
	}
}
```
   `TestInTxRollsBackOnError` matters. Without transactions, "write the event, then queue it for Google" could
   half-happen: the event shows locally but never syncs.
3. Run `go test ./internal/store/`. It should fail to compile.
4. Implement `internal/store/store.go`. Read the comments: the pragmas are explained in the README primer.
```go
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
`}
```
5. `internal/store/helpers.go`:
```go
package store

import (
	"cmp"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/yourname/clindar/internal/model"
)

// notFound converts sql.ErrNoRows into our ErrNotFound so callers never import database/sql.
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func sortCalendars(cals []model.Calendar) {
	slices.SortFunc(cals, func(a, b model.Calendar) int {
		if a.Primary != b.Primary {
			if a.Primary {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}
```
6. `internal/store/accounts.go`:
```go
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
		for _, table := range []string{"events", "calendars", "outbox", "sync_state", "log"} {
			if _, err := tx.q.ExecContext(ctx, `DELETE FROM `+table+` WHERE account_id = ?`, id); err != nil {
				return err
			}
		}
		_, err := tx.q.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
		return err
	})
}
```
7. `go mod tidy && go test ./internal/store/`: pass.

**Gotcha:** never edit a migration string after it has shipped. Users' databases already ran it.
Append a new migration instead.

**Commit:** `feat(store): open/migrate SQLite, transactions, accounts`

---

## Task 2.2: Calendars

**Goal:** store calendar-list entries. The upsert must **keep** the calendar's sync token. If it reset the
token, every calendar-list refresh would trigger a full re-download of that calendar's events.

**Files:** create `internal/store/calendars_test.go`, `internal/store/calendars.go`

**Steps**
1. Test:
```go
package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/store"
	"github.com/yourname/clindar/internal/testutil"
)

func TestCalendarsUpsertPreservesSyncTokenAndSortsPrimaryFirst(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	work := model.Calendar{AccountID: "a", ID: "work", Name: "Work", Selected: true}
	primary := model.Calendar{AccountID: "a", ID: "me@x.com", Name: "Me", Primary: true, Selected: true}
	for _, c := range []model.Calendar{work, primary} {
		if err := s.UpsertCalendar(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetCalendarSyncToken(ctx, "a", "work", "tok1"); err != nil {
		t.Fatal(err)
	}

	work.Name = "Work (renamed)"
	if err := s.UpsertCalendar(ctx, work); err != nil {
		t.Fatal(err)
	}

	cals, err := s.Calendars(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 2 || cals[0].ID != "me@x.com" || cals[1].Name != "Work (renamed)" {
		t.Fatalf("Calendars = %+v", cals)
	}
	tok, err := s.CalendarSyncToken(ctx, "a", "work")
	if err != nil || tok != "tok1" {
		t.Errorf("sync token = %q, %v; want tok1 (upsert must not clear it)", tok, err)
	}
}

func TestCalendarSyncTokenUnknownCalendar(t *testing.T) {
	s := testutil.NewStore(t)
	_, err := s.CalendarSyncToken(context.Background(), "a", "nope")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yourname/clindar/internal/model"
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
```
4. Run: pass.

**Commit:** `feat(store): calendars with sync tokens`

---

## Task 2.3: Events: put, get, find, delete

**Goal:** basic event persistence. Note `FindEvent`: CLI users type an ID without a calendar. The same ID
can legitimately exist in two calendars, so we return `ErrAmbiguous` rather than guess.

**Files:** create `internal/store/events_test.go`, `internal/store/events.go`

**Steps**
1. Test. `TestPutGetRoundTripsEveryField` is the important one: if a field isn't serialized, it fails.
```go
package store_test

import (
	"context"
	"errors"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/store"
	"github.com/yourname/clindar/internal/testutil"
)

func TestPutGetRoundTripsEveryField(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	ny := testutil.NY(t)
	want := testutil.Timed("e1", "Standup", time.Date(2026, 11, 16, 9, 0, 0, 0, ny), 15*time.Minute)
	want.Description = "daily"
	want.Location = "Room 1"
	want.Attendees = []model.Attendee{{Email: "me@example.com", Self: true, Response: model.ResponseAccepted}}
	want.ETag = `"123"`

	if err := s.PutEvent(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEvent(ctx, testutil.Account, testutil.Calendar, "e1")
	if err != nil {
		t.Fatal(err)
	}

	// cmp uses time.Time's Equal method, so the lost *Location pointer doesn't matter.
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("round trip mismatch (-want +got):\n%s", diff)
	}
}

func TestGetEventMissing(t *testing.T) {
	s := testutil.NewStore(t)
	_, err := s.GetEvent(context.Background(), "a", "c", "nope")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFindEventAmbiguousAcrossCalendars(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	e := testutil.Timed("shared", "Party", time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC), time.Hour)
	other := e
	other.CalendarID = "family"
	for _, ev := range []model.Event{e, other} {
		if err := s.PutEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	_, err := s.FindEvent(ctx, testutil.Account, "shared")

	if !errors.Is(err, store.ErrAmbiguous) {
		t.Errorf("err = %v, want ErrAmbiguous", err)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yourname/clindar/internal/model"
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
```
4. Run: pass.

**Commit:** `feat(store): event CRUD with JSON payload`

---

## Task 2.4: Range queries

**Goal:** the queries the service layer uses to build an agenda:
- `SinglesInRange` returns one-off events overlapping a window.
- `MastersBefore` returns recurring series that could have instances in a window.
- `Exceptions` returns a series' modified or cancelled instances.

Recurrence **expansion is not done here** (Phase 3 does it). SQL can't evaluate RRULEs.

`SinglesInRange` also returns **orphan instances**. If someone invites you to just one occurrence of their recurring
meeting, Google gives you that occurrence (with `recurringEventId`) but **not** its series. Expansion can never
produce it, so this query must, or the invite would be invisible. The test has one orphan and one exception of a
stored series, and only the orphan may come back.

The other subtle bit: all-day events are stored as UTC midnights, but the window is in local time. SQL
filters with a one-day pad, and `Event.Overlaps` (Task 1.2) makes the exact decision. **Don't
reimplement overlap logic in SQL.**

**Files:** create `internal/store/ranges_test.go`, `internal/store/ranges.go`

**Steps**
1. Test:
```go
package store_test

import (
	"context"
	"errors"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/store"
	"github.com/yourname/clindar/internal/testutil"
)

func TestSinglesInRange(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	ny := testutil.NY(t)
	at := func(day, h int) time.Time { return time.Date(2026, 11, day, h, 0, 0, 0, ny) }

	inside := testutil.Timed("in", "inside", at(14, 9), time.Hour)
	before := testutil.Timed("before", "before", at(13, 9), time.Hour)
	holiday := testutil.AllDay("hol", "holiday", 2026, time.November, 14, 1)
	cancelled := testutil.Timed("x", "cancelled", at(14, 11), time.Hour)
	cancelled.Status = model.StatusCancelled
	// An occurrence you were invited to on its own: its series "s" isn't stored.
	orphan := testutil.Timed("s_1", "orphan instance", at(14, 12), time.Hour)
	orphan.SeriesID = "s"
	master := testutil.Timed("m", "master", at(14, 13), time.Hour)
	master.Recurrence = []string{"RRULE:FREQ=DAILY"}
	attached := testutil.Timed("m_1", "exception of a stored series", at(14, 15), time.Hour)
	attached.SeriesID = "m"
	for _, e := range []model.Event{inside, before, holiday, cancelled, orphan, master, attached} {
		if err := s.PutEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.SinglesInRange(ctx, testutil.Account, at(14, 0), at(15, 0))
	if err != nil {
		t.Fatal(err)
	}

	ids := map[string]bool{}
	for _, e := range got {
		ids[e.ID] = true
	}
	if diff := cmp.Diff(map[string]bool{"in": true, "hol": true, "s_1": true}, ids); diff != "" {
		t.Errorf("ids mismatch (-want +got):\n%s", diff)
	}
}

func TestDeleteSeriesAndExceptionsFrom(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	day := func(d int) time.Time { return time.Date(2026, 11, d, 9, 0, 0, 0, time.UTC) }
	master := testutil.Timed("m", "series", day(2), time.Hour)
	master.Recurrence = []string{"RRULE:FREQ=WEEKLY"}
	early := testutil.Timed("m_early", "early exception", day(9), time.Hour)
	early.SeriesID, early.OriginalStart = "m", day(9)
	late := testutil.Timed("m_late", "late exception", day(23), time.Hour)
	late.SeriesID, late.OriginalStart = "m", day(23)
	for _, e := range []model.Event{master, early, late} {
		if err := s.PutEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.DeleteExceptionsFrom(ctx, testutil.Account, testutil.Calendar, "m", day(16)); err != nil {
		t.Fatal(err)
	}
	exc, _ := s.Exceptions(ctx, testutil.Account, testutil.Calendar, "m")
	if len(exc) != 1 || exc[0].ID != "m_early" {
		t.Fatalf("after DeleteExceptionsFrom: %v, want only m_early", exc)
	}

	if err := s.DeleteSeries(ctx, testutil.Account, testutil.Calendar, "m"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEvent(ctx, testutil.Account, testutil.Calendar, "m"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("master still present: %v", err)
	}
	if exc, _ := s.Exceptions(ctx, testutil.Account, testutil.Calendar, "m"); len(exc) != 0 {
		t.Errorf("exceptions still present: %v", exc)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package store

import (
	"context"
	"time"

	"github.com/yourname/clindar/internal/model"
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
```
4. Run: pass.

**Commit:** `feat(store): range, series, and exception queries`

---

## Task 2.5: The outbox

**Goal:** the queue of local changes waiting to reach Google. This is the heart of offline editing, so take
your time. Read design §3 first.

Three ideas:
1. **Coalescing.** There is at most one pending op per event. A new edit is merged into the existing op (see the
   table in the `Enqueue` doc comment). This keeps pushes cheap. It also avoids a real bug: two separate
   patches would both carry the *old* etag, and the second would fail with 412.
2. **Versioned completion.** The push engine reads op v1 and sends it to Google. Meanwhile the user edits again, which
   coalesces into the same row as v2. When Google answers, `CompleteOp(op v1)` must **not** delete the row.
   Instead it keeps the newer edit and points it at the etag Google just returned. `TestCompleteOpWhenEditedInFlight` covers exactly this.
3. **RSVP is its own kind of op** (`rsvp`). It sends only the attendee list, without If-Match (Task 6.2 explains
   why). When it meets an insert or patch for the same event, it merges into them, since they carry the
   attendee list anyway. **If-Match rule:** a merged op normally keeps the existing op's If-Match, which is the etag the
   user's *first* pending change was based on. A pending RSVP has none, though, so a patch or delete merged
   into it keeps **its own** If-Match. Otherwise it would silently overwrite the organizer's changes instead
   of conflicting. Note that the test's `rsvp()` helper has no If-Match, exactly like the real ones.

`Op.CreatedAt` is set by the caller from *its* clock. The store never calls `time.Now()` (README testing rule 5).

**Files:** create `internal/store/outbox_test.go`, `internal/store/outbox.go`

**Steps**
1. Test. Read every row of `TestEnqueueCoalescing` and convince yourself the expected result is right
   before writing code.
```go
package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/store"
	"github.com/yourname/clindar/internal/testutil"
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
```
2. Run: compile failure.
3. Implement:
```go
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yourname/clindar/internal/model"
)

type OpKind string

const (
	OpInsert OpKind = "insert"
	OpPatch  OpKind = "patch"
	OpRSVP   OpKind = "rsvp" // send only the attendee list (your response), unconditionally
	OpDelete OpKind = "delete"
)

// Op is one pending change waiting to be pushed to Google.
type Op struct {
	Seq         int64
	AccountID   string
	CalendarID  string
	EventID     string
	Kind        OpKind
	Payload     model.Event // the event's full local state, for insert/patch
	IfMatch     string      // etag to send as If-Match; "" = unconditional
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
		(account_id, calendar_id, event_id, op, payload, if_match, send_updates, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		op.AccountID, op.CalendarID, op.EventID, op.Kind, payload, op.IfMatch, sendUpdatesOrNone(op.SendUpdates),
		op.CreatedAt.Unix())
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

func (s *Store) HasPending(ctx context.Context, accountID, calendarID, eventID string) (bool, error) {
	_, err := s.pendingFor(ctx, accountID, calendarID, eventID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
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
	rows, err := s.q.QueryContext(ctx, `SELECT seq, account_id, calendar_id, event_id, op, payload,
		if_match, send_updates, version, attempts, last_error FROM outbox `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ops []Op
	for rows.Next() {
		var op Op
		var payload string
		if err := rows.Scan(&op.Seq, &op.AccountID, &op.CalendarID, &op.EventID, &op.Kind, &payload,
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
```
4. Run: pass. Then run `go test -race ./internal/store/`.

**Commit:** `feat(store): outbox with per-event coalescing`

---

## Task 2.6: Sync state and the log

**Goal:** remember when we last synced (CLI staleness check, TUI status bar), store the calendar-list sync
token, and keep a log of discarded edits and errors (`clindar log`). Also `ResetSyncTokens` for `clindar sync --full`.

**Files:** create `internal/store/state_test.go`, `internal/store/state.go`

**Steps**
1. Test:
```go
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/store"
	"github.com/yourname/clindar/internal/testutil"
)

func TestLastSyncZeroUntilSet(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	if got, err := s.LastSync(ctx, "a"); err != nil || !got.IsZero() {
		t.Fatalf("LastSync = %v, %v; want zero, nil", got, err)
	}
	when := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)

	if err := s.SetLastSync(ctx, "a", when); err != nil {
		t.Fatal(err)
	}

	if got, _ := s.LastSync(ctx, "a"); !got.Equal(when) {
		t.Errorf("LastSync = %v, want %v", got, when)
	}
}

func TestCalendarsTokenAndLastSyncAreIndependent(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	when := time.Unix(1_800_000_000, 0)
	s.SetLastSync(ctx, "a", when)

	s.SetCalendarsToken(ctx, "a", "tok")

	if tok, _ := s.CalendarsToken(ctx, "a"); tok != "tok" {
		t.Errorf("token = %q", tok)
	}
	if got, _ := s.LastSync(ctx, "a"); !got.Equal(when) {
		t.Errorf("LastSync clobbered: %v", got)
	}
}

func TestLogsNewestFirstAndCount(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	base := time.Unix(1_800_000_000, 0)
	for i, msg := range []string{"first", "second"} {
		s.AddLog(ctx, store.LogEntry{Time: base.Add(time.Duration(i) * time.Minute),
			Kind: store.LogDiscardedEdit, AccountID: "a", Message: msg})
	}

	logs, err := s.Logs(ctx, "a", 10)

	if err != nil || len(logs) != 2 || logs[0].Message != "second" {
		t.Fatalf("Logs = %+v, %v", logs, err)
	}
	n, _ := s.CountLogsSince(ctx, "a", store.LogDiscardedEdit, base.Add(30*time.Second))
	if n != 1 {
		t.Errorf("CountLogsSince = %d, want 1", n)
	}
}

func TestResetSyncTokens(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	s.UpsertCalendar(ctx, model.Calendar{AccountID: "a", ID: "c", Name: "C"})
	s.SetCalendarSyncToken(ctx, "a", "c", "events-token")
	s.SetCalendarsToken(ctx, "a", "list-token")

	if err := s.ResetSyncTokens(ctx, "a"); err != nil {
		t.Fatal(err)
	}

	if tok, _ := s.CalendarSyncToken(ctx, "a", "c"); tok != "" {
		t.Errorf("calendar token = %q, want empty", tok)
	}
	if tok, _ := s.CalendarsToken(ctx, "a"); tok != "" {
		t.Errorf("calendar list token = %q, want empty", tok)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
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
```
4. Run: pass.

**Commit:** `feat(store): sync state and log`

---

## Phase 2 checkpoint
```bash
gofmt -l . && go vet ./... && go test -race ./... -count=1
```
