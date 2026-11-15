# Phase 4: Service

`internal/service` is the API that **every** front-end uses (CLI, TUI). Rules:
- **Reads** come from the store, with series expanded via `recur`.
- **Writes** update the store **and** enqueue an outbox op **in one transaction** (`s.save`/`s.remove`).
  No other package writes events. If you find yourself calling `store.PutEvent` from the CLI or TUI, stop.
- The service never talks to Google. Sync (Phase 6) does that later.

Read design §4 (scope table) before Task 4.3.

---

## Task 4.1: Service, calendars, agenda, get, next

**Goal:** the read side. `Agenda` merges one-off events with expanded series and sorts them. `Get` accepts
instance IDs even when no exception is stored. **Orphan instances** (Task 2.4) are *detached*: returned with
`SeriesID` cleared, so the CLI, the TUI, and the edit paths all treat them as one-off events. You can't edit
"all" of a series that isn't in your calendar. `Next` powers status bars: in-progress events count, and
declined and all-day events are skipped.

**Files:**
- Create `internal/service/service.go`, `internal/service/query.go`
- Create `internal/service/helpers_test.go` (shared test environment for this package), `internal/service/query_test.go`

**Steps**
1. Shared test environment. `env` builds a real store with two calendars and a fixed clock. Every
   service test uses it, so each test stays short:
```go
package service_test

import (
	"context"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

// env is a service over a real temp store, with a fixed clock and two
// calendars: the writable primary, and a read-only, unselected "Holidays".
type env struct {
	t     *testing.T
	ctx   context.Context
	store *store.Store
	svc   *service.Service
	ny    *time.Location
	now   time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := testutil.NewStore(t)
	ny := testutil.NY(t)
	ctx := context.Background()
	for _, c := range []model.Calendar{
		{AccountID: testutil.Account, ID: testutil.Calendar, Name: "Me", Primary: true,
			Selected: true, AccessRole: "owner", TimeZone: "America/New_York"},
		{AccountID: testutil.Account, ID: "holidays", Name: "Holidays", AccessRole: "reader"},
	} {
		if err := st.UpsertCalendar(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny) // Thursday
	svc := service.New(st, testutil.Account, func() time.Time { return now }, "all")
	return &env{t: t, ctx: ctx, store: st, svc: svc, ny: ny, now: now}
}

func (e *env) at(day, hour, min int) time.Time {
	return time.Date(2026, 11, day, hour, min, 0, 0, e.ny)
}

func (e *env) put(ev model.Event) {
	e.t.Helper()
	if err := e.store.PutEvent(e.ctx, ev); err != nil {
		e.t.Fatal(err)
	}
}

// titlesOn returns "15:04 Title" for each event on the given November day.
func (e *env) titlesOn(day int) []string {
	e.t.Helper()
	events, err := e.svc.Agenda(e.ctx, e.at(day, 0, 0), e.at(day+1, 0, 0), service.Filter{})
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, ev := range events {
		out = append(out, ev.Start.In(e.ny).Format("15:04")+" "+ev.Title)
	}
	return out
}

func (e *env) ops() []store.Op {
	e.t.Helper()
	ops, err := e.store.PendingOps(e.ctx, testutil.Account)
	if err != nil {
		e.t.Fatal(err)
	}
	return ops
}

// series stores a weekly Monday 09:00 "Standup" starting Mon Nov 2, with no pending ops.
func (e *env) series() model.Event {
	e.t.Helper()
	m := testutil.Timed("std", "Standup", e.at(2, 9, 0), 15*time.Minute)
	m.Recurrence = []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"}
	m.ETag = `"m1"`
	e.put(m)
	return m
}

func ptr[T any](v T) *T { return &v }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```
2. Tests:
```go
package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestAgendaMergesSinglesAndSeriesSorted(t *testing.T) {
	e := newEnv(t)
	e.series()
	e.put(testutil.Timed("lunch", "Lunch", e.at(16, 12, 0), time.Hour))
	e.put(testutil.Timed("early", "Gym", e.at(16, 7, 0), time.Hour))

	got := e.titlesOn(16)

	want := []string{"07:00 Gym", "09:00 Standup", "12:00 Lunch"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAgendaHidesUnselectedCalendarsUnlessFiltered(t *testing.T) {
	e := newEnv(t)
	hol := testutil.AllDay("thx", "Thanksgiving", 2026, time.November, 26, 1)
	hol.CalendarID = "holidays"
	e.put(hol)

	if got := e.titlesOn(26); len(got) != 0 {
		t.Errorf("default agenda shows %v, want nothing (Holidays not selected)", got)
	}
	got, err := e.svc.Agenda(e.ctx, e.at(26, 0, 0), e.at(27, 0, 0), service.Filter{CalendarIDs: []string{"holidays"}})
	if err != nil || len(got) != 1 {
		t.Errorf("filtered agenda = %v, %v; want Thanksgiving", got, err)
	}
}

func TestGetVirtualInstanceByID(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	got, err := e.svc.Get(e.ctx, id)

	if err != nil || got.ID != id || got.SeriesID != "std" {
		t.Fatalf("Get(%s) = %+v, %v", id, got, err)
	}
	if _, err := e.svc.Get(e.ctx, model.InstanceID("std", e.at(17, 9, 0), false)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get(non-slot) err = %v, want ErrNotFound", err)
	}
}

func TestNext(t *testing.T) {
	e := newEnv(t)
	e.put(testutil.AllDay("hol", "Holiday", 2026, time.November, 12, 1))
	e.put(testutil.Timed("past", "Done", e.at(12, 9, 0), time.Hour))
	declined := testutil.Timed("no", "Declined", e.at(12, 10, 45), time.Hour)
	declined.Attendees = []model.Attendee{{Email: testutil.Account, Self: true, Response: model.ResponseDeclined}}
	e.put(declined)
	e.put(testutil.Timed("running", "In progress", e.at(12, 10, 0), time.Hour)) // now is 10:30
	e.put(testutil.Timed("later", "Later", e.at(12, 15, 0), time.Hour))

	got, ok, err := e.svc.Next(e.ctx)

	if err != nil || !ok || got.ID != "running" {
		t.Errorf("Next = %q, %v, %v; want running", got.ID, ok, err)
	}
}

// Invited to one occurrence of someone else's series: Google gives us the
// instance without its series. It must show up, and behave like a one-off.
// (RSVP without a scope is checked in delete_test.go: RSVP arrives in Task 4.4.)
func TestOrphanInstanceIsShownAndEditableAsOneOff(t *testing.T) {
	e := newEnv(t)
	inv := testutil.Timed("theirs_20261116T150000Z", "Their weekly sync", e.at(16, 10, 0), time.Hour)
	inv.SeriesID, inv.OriginalStart = "theirs", e.at(16, 10, 0)
	inv.Attendees = []model.Attendee{{Email: testutil.Account, Self: true, Response: model.ResponseNeedsAction}}
	e.put(inv)

	if got := e.titlesOn(16); !equalStrings(got, []string{"10:00 Their weekly sync"}) {
		t.Fatalf("Nov 16 = %v", got)
	}
	got, err := e.svc.Get(e.ctx, inv.ID)
	if err != nil || got.IsInstance() {
		t.Fatalf("Get = %+v, %v; want a one-off", got, err)
	}
}
```
3. Run: compile failure.
4. `internal/service/service.go`:
```go
// Package service is the API every front-end (CLI, TUI) uses. Reads come from
// the store; writes update the store AND enqueue an outbox op in one
// transaction. Nothing else may write events.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

var (
	ErrNeedScope    = errors.New("this is a recurring event: choose a scope (this, following, or all)")
	ErrReadOnly     = errors.New("calendar is read-only")
	ErrNotAttendee  = errors.New("you are not an attendee of this event")
	ErrNoCalendar   = errors.New("no such calendar")
	ErrInvalidEvent = errors.New("invalid event")
)

type Service struct {
	store       *store.Store
	account     string
	now         func() time.Time
	sendUpdates string // "all" | "none": used when an event has attendees
}

func New(st *store.Store, accountID string, now func() time.Time, sendUpdates string) *Service {
	return &Service{store: st, account: accountID, now: now, sendUpdates: sendUpdates}
}

func (s *Service) AccountID() string { return s.account }

// Calendars returns the account's calendars, primary first.
func (s *Service) Calendars(ctx context.Context) ([]model.Calendar, error) {
	return s.store.Calendars(ctx, s.account)
}

// ResolveCalendar finds a calendar by exact ID or case-insensitive name.
// "" means the primary calendar.
func (s *Service) ResolveCalendar(ctx context.Context, nameOrID string) (model.Calendar, error) {
	cals, err := s.Calendars(ctx)
	if err != nil {
		return model.Calendar{}, err
	}
	for _, c := range cals {
		if (nameOrID == "" && c.Primary) || c.ID == nameOrID || (nameOrID != "" && strings.EqualFold(c.Name, nameOrID)) {
			return c, nil
		}
	}
	if nameOrID == "" {
		return model.Calendar{}, fmt.Errorf("primary calendar: %w (run clindar sync)", ErrNoCalendar)
	}
	return model.Calendar{}, fmt.Errorf("%q: %w (see clindar calendars)", nameOrID, ErrNoCalendar)
}

// Status summarizes sync health for status bars and the TUI.
type Status struct {
	LastSync  time.Time
	Pending   int
	Discarded int // discarded edits since the `since` passed to Status
}

func (s *Service) Status(ctx context.Context, since time.Time) (Status, error) {
	var st Status
	var err error
	if st.LastSync, err = s.store.LastSync(ctx, s.account); err != nil {
		return st, err
	}
	if st.Pending, err = s.store.PendingCount(ctx, s.account); err != nil {
		return st, err
	}
	st.Discarded, err = s.store.CountLogsSince(ctx, s.account, store.LogDiscardedEdit, since)
	return st, err
}

func (s *Service) Logs(ctx context.Context, limit int) ([]store.LogEntry, error) {
	return s.store.Logs(ctx, s.account, limit)
}
```
5. `internal/service/query.go`:
```go
package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/store"
)

// Filter narrows Agenda. Empty CalendarIDs means "calendars marked selected".
type Filter struct {
	CalendarIDs []string
}

// Agenda returns every visible event overlapping [from, to), recurring
// instances expanded, sorted for display in from's location.
func (s *Service) Agenda(ctx context.Context, from, to time.Time, f Filter) ([]model.Event, error) {
	visible, err := s.visibleCalendars(ctx, f)
	if err != nil {
		return nil, err
	}
	singles, err := s.store.SinglesInRange(ctx, s.account, from, to)
	if err != nil {
		return nil, err
	}
	var out []model.Event
	for _, e := range singles {
		if visible[e.CalendarID] {
			out = append(out, detach(e)) // SinglesInRange only returns orphan instances, never attached ones
		}
	}
	masters, err := s.store.MastersBefore(ctx, s.account, to)
	if err != nil {
		return nil, err
	}
	for _, m := range masters {
		if !visible[m.CalendarID] {
			continue
		}
		instances, err := s.expand(ctx, m, from, to)
		if err != nil {
			return nil, err
		}
		out = append(out, instances...)
	}
	model.SortEvents(out, from.Location())
	return out, nil
}

func (s *Service) expand(ctx context.Context, m model.Event, from, to time.Time) ([]model.Event, error) {
	exceptions, err := s.store.Exceptions(ctx, s.account, m.CalendarID, m.ID)
	if err != nil {
		return nil, err
	}
	instances, err := recur.Expand(m, exceptions, from, to)
	if err != nil {
		// A rule we can't parse shouldn't hide the event entirely: show the
		// master as a one-off rather than failing the whole agenda.
		if m.Overlaps(from, to) {
			return []model.Event{m}, nil
		}
		return nil, nil
	}
	return instances, nil
}

func (s *Service) visibleCalendars(ctx context.Context, f Filter) (map[string]bool, error) {
	cals, err := s.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	visible := map[string]bool{}
	for _, c := range cals {
		if (len(f.CalendarIDs) == 0 && c.Selected) || slices.Contains(f.CalendarIDs, c.ID) {
			visible[c.ID] = true
		}
	}
	return visible, nil
}

// Next returns the first timed event that hasn't ended yet (in progress
// counts), skipping ones you declined. ok is false if nothing in the next 7 days.
func (s *Service) Next(ctx context.Context) (e model.Event, ok bool, err error) {
	now := s.now()
	events, err := s.Agenda(ctx, now, now.AddDate(0, 0, 7), Filter{})
	if err != nil {
		return model.Event{}, false, err
	}
	for _, e := range events {
		if e.AllDay || !e.End.After(now) {
			continue
		}
		if self := e.SelfAttendee(); self != nil && self.Response == model.ResponseDeclined {
			continue
		}
		return e, true, nil
	}
	return model.Event{}, false, nil
}

// Get returns an event by ID: a stored event, or an instance of a series
// (instance IDs work even when no exception is stored).
func (s *Service) Get(ctx context.Context, id string) (model.Event, error) {
	e, err := s.store.FindEvent(ctx, s.account, id)
	if err == nil && !e.Cancelled() {
		return s.detachIfOrphan(ctx, e)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return model.Event{}, err
	}
	seriesID, originalStart, _, ok := model.ParseInstanceID(id)
	if !ok {
		return model.Event{}, fmt.Errorf("event %s: %w", id, store.ErrNotFound)
	}
	master, err := s.store.FindEvent(ctx, s.account, seriesID)
	if err != nil {
		return model.Event{}, fmt.Errorf("event %s: %w", id, err)
	}
	exceptions, err := s.store.Exceptions(ctx, s.account, master.CalendarID, master.ID)
	if err != nil {
		return model.Event{}, err
	}
	inst, err := recur.Lookup(master, exceptions, originalStart)
	if errors.Is(err, recur.ErrNoSuchInstance) {
		return model.Event{}, fmt.Errorf("event %s: %w", id, store.ErrNotFound)
	}
	return inst, err
}

// detach presents an orphan instance (its series isn't in our calendar) as a
// one-off event, so every front-end and every edit path treats it as one:
// no scope prompt, and edits go to its own ID. The stored row is unchanged.
func detach(e model.Event) model.Event {
	e.SeriesID, e.OriginalStart = "", time.Time{}
	return e
}

func (s *Service) detachIfOrphan(ctx context.Context, e model.Event) (model.Event, error) {
	if !e.IsInstance() {
		return e, nil
	}
	_, err := s.store.GetEvent(ctx, s.account, e.CalendarID, e.SeriesID)
	if errors.Is(err, store.ErrNotFound) {
		return detach(e), nil
	}
	return e, err
}
```
6. Run: pass.

**Commit:** `feat(service): agenda, get, next`

---

## Task 4.2: Create

**Goal:** create events and series. Note these details (each has a test):
- Go names the system zone `"Local"`, which isn't an IANA name, so Google can't use it. `zoneName` falls back to the
  calendar's zone.
- All-day events are normalized to UTC midnights (design §2).
- Attendees are notified (`send_updates`) only if the event has attendees.
- A failed create queues nothing.

`create.go` also holds the helpers that Tasks 4.3–4.4 reuse: `save`, `remove`, `validate`, `setInvites`,
`dateOnly`, `zoneName`, and `writableCalendar`. **Reuse them; don't copy them.**

**Files:** create `internal/service/create_test.go`, `internal/service/create.go`

**Steps**
1. Test:
```go
package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestCreateStoresLocallyAndQueuesInsert(t *testing.T) {
	e := newEnv(t)
	local := time.Date(2026, 11, 16, 14, 0, 0, 0, time.Local) // as parsed from user input

	ev, err := e.svc.Create(e.ctx, service.Draft{Title: " Write report ", Start: local, End: local.Add(time.Hour)})

	if err != nil {
		t.Fatal(err)
	}
	if ev.Title != "Write report" || ev.CalendarID != testutil.Calendar {
		t.Errorf("created %+v", ev)
	}
	if ev.TimeZone != "America/New_York" {
		t.Errorf("TimeZone = %q, want calendar zone (Go's \"Local\" is not an IANA name)", ev.TimeZone)
	}
	ops := e.ops()
	if len(ops) != 1 || ops[0].Kind != store.OpInsert || ops[0].EventID != ev.ID || ops[0].SendUpdates != "none" {
		t.Errorf("ops = %+v, want one insert with send_updates none", ops)
	}
}

func TestCreateWithInvitesSendsUpdates(t *testing.T) {
	e := newEnv(t)

	_, err := e.svc.Create(e.ctx, service.Draft{Title: "Sync", Start: e.at(16, 9, 0), End: e.at(16, 10, 0),
		Invite: []string{"bob@example.com"}})

	if err != nil {
		t.Fatal(err)
	}
	if ops := e.ops(); ops[0].SendUpdates != "all" || len(ops[0].Payload.Attendees) != 1 {
		t.Errorf("op = %+v", ops[0])
	}
}

func TestCreateAllDayStoresUTCMidnights(t *testing.T) {
	e := newEnv(t)

	ev, err := e.svc.Create(e.ctx, service.Draft{Title: "Trip", AllDay: true, Start: e.at(20, 0, 0), End: e.at(22, 0, 0)})

	if err != nil {
		t.Fatal(err)
	}
	if !ev.Start.Equal(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)) || ev.TimeZone != "" {
		t.Errorf("start = %v, tz = %q", ev.Start, ev.TimeZone)
	}
	if got := e.titlesOn(21); len(got) != 1 {
		t.Errorf("trip not on Nov 21: %v", got)
	}
}

func TestCreateRejects(t *testing.T) {
	tests := map[string]struct {
		draft   service.Draft
		wantErr error
	}{
		"empty title":        {service.Draft{Title: " "}, service.ErrInvalidEvent},
		"end before start":   {service.Draft{Title: "x", Start: time.Unix(100, 0), End: time.Unix(50, 0)}, service.ErrInvalidEvent},
		"read-only calendar": {service.Draft{Title: "x", CalendarID: "Holidays"}, service.ErrReadOnly},
		"unknown calendar":   {service.Draft{Title: "x", CalendarID: "nope"}, service.ErrNoCalendar},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)

			_, err := e.svc.Create(e.ctx, tt.draft)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if len(e.ops()) != 0 {
				t.Errorf("a failed create must not queue anything")
			}
		})
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

// Scope says which part of a recurring series an edit applies to.
type Scope string

const (
	ScopeNone      Scope = "" // fine for one-off events; an error for instances
	ScopeThis      Scope = "this"
	ScopeFollowing Scope = "following"
	ScopeAll       Scope = "all"
)

func ParseScope(s string) (Scope, error) {
	switch sc := Scope(strings.ToLower(s)); sc {
	case ScopeNone, ScopeThis, ScopeFollowing, ScopeAll:
		return sc, nil
	}
	return "", fmt.Errorf("unknown scope %q (use this, following, or all)", s)
}

// Draft describes a new event. For all-day events only the dates of Start and
// End matter, and End is exclusive (a one-day event ends the next day).
type Draft struct {
	CalendarID  string // name or ID; "" = primary
	Title       string
	Description string
	Location    string
	Start, End  time.Time
	AllDay      bool
	Recurrence  []string // from recur.ParseRepeat
	Invite      []string // attendee emails
}

// Create adds a new event (or series) locally and queues it for Google.
func (s *Service) Create(ctx context.Context, d Draft) (model.Event, error) {
	cal, err := s.writableCalendar(ctx, d.CalendarID)
	if err != nil {
		return model.Event{}, err
	}
	e := model.Event{
		AccountID: s.account, CalendarID: cal.ID, ID: model.NewEventID(),
		Status: model.StatusConfirmed, Title: strings.TrimSpace(d.Title),
		Description: d.Description, Location: d.Location,
		Start: d.Start, End: d.End, AllDay: d.AllDay,
		Recurrence: d.Recurrence, Organizer: s.account,
	}
	if e.AllDay {
		e.Start, e.End = dateOnly(e.Start), dateOnly(e.End)
	} else {
		e.TimeZone = zoneName(d.Start.Location(), cal.TimeZone)
	}
	setInvites(&e, d.Invite)
	if err := validate(e); err != nil {
		return model.Event{}, err
	}
	err = s.store.InTx(ctx, func(tx *store.Store) error { return s.save(ctx, tx, e, store.OpInsert) })
	return e, err
}

// save writes e locally and queues it for Google, in the caller's transaction.
func (s *Service) save(ctx context.Context, tx *store.Store, e model.Event, kind store.OpKind) error {
	if err := tx.PutEvent(ctx, e); err != nil {
		return err
	}
	return tx.Enqueue(ctx, store.Op{
		AccountID: e.AccountID, CalendarID: e.CalendarID, EventID: e.ID,
		Kind: kind, Payload: e, IfMatch: e.ETag, SendUpdates: s.updatesFor(e), CreatedAt: s.now(),
	})
}

// remove queues a delete. The caller has already updated local state.
func (s *Service) remove(ctx context.Context, tx *store.Store, e model.Event) error {
	return tx.Enqueue(ctx, store.Op{
		AccountID: e.AccountID, CalendarID: e.CalendarID, EventID: e.ID,
		Kind: store.OpDelete, IfMatch: e.ETag, SendUpdates: s.updatesFor(e), CreatedAt: s.now(),
	})
}

func (s *Service) updatesFor(e model.Event) string {
	if len(e.Attendees) == 0 {
		return "none"
	}
	return s.sendUpdates
}

func (s *Service) writableCalendar(ctx context.Context, nameOrID string) (model.Calendar, error) {
	cal, err := s.ResolveCalendar(ctx, nameOrID)
	if err != nil {
		return model.Calendar{}, err
	}
	if !cal.Writable() {
		return model.Calendar{}, fmt.Errorf("%s: %w", cal.Name, ErrReadOnly)
	}
	return cal, nil
}

func validate(e model.Event) error {
	switch {
	case e.Title == "":
		return fmt.Errorf("%w: title is empty", ErrInvalidEvent)
	case !e.End.After(e.Start):
		return fmt.Errorf("%w: end (%s) must be after start (%s)", ErrInvalidEvent,
			e.End.Format(time.DateTime), e.Start.Format(time.DateTime))
	}
	return nil
}

// setInvites replaces the attendee list with emails, keeping existing
// attendees' responses (and you, if you're on the list).
func setInvites(e *model.Event, emails []string) {
	old := map[string]model.Attendee{}
	for _, a := range e.Attendees {
		old[strings.ToLower(a.Email)] = a
	}
	var out []model.Attendee
	for _, a := range e.Attendees {
		if a.Self || a.Organizer {
			out = append(out, a)
		}
	}
	for _, email := range emails {
		email = strings.TrimSpace(email)
		key := strings.ToLower(email)
		if a, ok := old[key]; ok && (a.Self || a.Organizer) {
			continue // already kept above
		} else if ok {
			out = append(out, a)
			continue
		}
		out = append(out, model.Attendee{Email: email, Response: model.ResponseNeedsAction})
	}
	e.Attendees = out
}

// dateOnly returns UTC midnight of t's calendar date (all-day storage format).
func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// zoneName returns an IANA zone name for loc. Go names the system zone
// "Local", which Google doesn't understand, so fall back to the calendar's zone.
func zoneName(loc *time.Location, fallback string) string {
	if name := loc.String(); name != "Local" && name != "" {
		return name
	}
	return fallback
}
```
4. Run: pass.

**Commit:** `feat(service): create events`

---

## Task 4.3: Update (with scopes)

**Goal:** edit one-off events, and edit instances with scope `this`, `following`, or `all` (design §4).

- `Changes` uses pointers: `nil` means "don't touch". Setting `Start` without `End` **moves** the event and
  keeps its duration.
- `all` applies the change to the master as a **shift**. Moving one Monday instance from 09:00 to 10:30
  moves every instance by +90m.
- `following` = `recur.SplitAt` + patch the old master + insert a new master. At the first instance it
  becomes `all`.
- An instance with no scope returns `ErrNeedScope`. The CLI turns that into a helpful message.

**Files:** create `internal/service/update_test.go`, `internal/service/update.go`

**Steps**
1. Test:
```go
package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestUpdateOneOffMovesKeepingDuration(t *testing.T) {
	e := newEnv(t)
	ev := testutil.Timed("e1", "Dentist", e.at(16, 9, 0), 45*time.Minute)
	ev.ETag = `"v1"`
	e.put(ev)

	got, err := e.svc.Update(e.ctx, "e1", service.Changes{Start: ptr(e.at(17, 14, 0))}, service.ScopeNone)

	if err != nil {
		t.Fatal(err)
	}
	if !got.End.Equal(e.at(17, 14, 45)) {
		t.Errorf("End = %v, want 14:45 (duration kept)", got.End)
	}
	ops := e.ops()
	if len(ops) != 1 || ops[0].Kind != store.OpPatch || ops[0].IfMatch != `"v1"` {
		t.Errorf("ops = %+v, want one patch with If-Match \"v1\"", ops)
	}
}

func TestUpdateInstanceRequiresScope(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	_, err := e.svc.Update(e.ctx, id, service.Changes{Title: ptr("x")}, service.ScopeNone)

	if !errors.Is(err, service.ErrNeedScope) {
		t.Errorf("err = %v, want ErrNeedScope", err)
	}
}

func TestUpdateThisInstanceOnly(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	if _, err := e.svc.Update(e.ctx, id, service.Changes{Title: ptr("Retro")}, service.ScopeThis); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(16); !equalStrings(got, []string{"09:00 Retro"}) {
		t.Errorf("Nov 16 = %v", got)
	}
	if got := e.titlesOn(23); !equalStrings(got, []string{"09:00 Standup"}) {
		t.Errorf("Nov 23 = %v (other instances must be unchanged)", got)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].EventID != id || ops[0].IfMatch != "" {
		t.Errorf("ops = %+v, want one unconditional patch of the instance", ops)
	}
}

func TestUpdateAllShiftsSeries(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	// Move this instance 09:00 → 10:30, applied to all: every instance shifts by 90m.
	if _, err := e.svc.Update(e.ctx, id, service.Changes{Start: ptr(e.at(16, 10, 30))}, service.ScopeAll); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(9); !equalStrings(got, []string{"10:30 Standup"}) {
		t.Errorf("Nov 9 = %v", got)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].EventID != "std" || ops[0].IfMatch != `"m1"` {
		t.Errorf("ops = %+v, want one patch of the master", ops)
	}
}

func TestUpdateFollowingSplitsSeries(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	next, err := e.svc.Update(e.ctx, id, service.Changes{Title: ptr("Standup v2")}, service.ScopeFollowing)

	if err != nil {
		t.Fatal(err)
	}
	if got := e.titlesOn(9); !equalStrings(got, []string{"09:00 Standup"}) {
		t.Errorf("Nov 9 = %v", got)
	}
	for _, day := range []int{16, 23, 30} {
		if got := e.titlesOn(day); !equalStrings(got, []string{"09:00 Standup v2"}) {
			t.Errorf("Nov %d = %v", day, got)
		}
	}
	ops := e.ops()
	if len(ops) != 2 || ops[0].Kind != store.OpPatch || ops[0].EventID != "std" ||
		ops[1].Kind != store.OpInsert || ops[1].EventID != next.ID {
		t.Errorf("ops = %+v, want patch(master) then insert(new series)", ops)
	}
}

func TestUpdateFollowingAtFirstInstanceUpdatesAll(t *testing.T) {
	e := newEnv(t)
	e.series()
	first := model.InstanceID("std", e.at(2, 9, 0), false)

	if _, err := e.svc.Update(e.ctx, first, service.Changes{Title: ptr("New")}, service.ScopeFollowing); err != nil {
		t.Fatal(err)
	}

	if ops := e.ops(); len(ops) != 1 || ops[0].EventID != "std" {
		t.Errorf("ops = %+v, want a single patch of the master", ops)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/store"
)

// Changes lists fields to modify; nil means "leave as is". Setting Start
// without End moves the event, keeping its duration.
type Changes struct {
	Title, Description, Location *string
	Start, End                   *time.Time
	AllDay                       *bool
	Invite                       *[]string // replaces the attendee list
}

func (c Changes) withoutTiming() Changes {
	c.Start, c.End, c.AllDay = nil, nil, nil
	return c
}

func (c Changes) apply(e *model.Event) error {
	if c.Title != nil {
		e.Title = strings.TrimSpace(*c.Title)
	}
	if c.Description != nil {
		e.Description = *c.Description
	}
	if c.Location != nil {
		e.Location = *c.Location
	}
	if c.Invite != nil {
		setInvites(e, *c.Invite)
	}
	duration := e.Duration()
	if c.AllDay != nil && *c.AllDay != e.AllDay {
		if c.Start == nil {
			return fmt.Errorf("%w: switching all-day on or off needs a new start time", ErrInvalidEvent)
		}
		e.AllDay = *c.AllDay
		duration = time.Hour
		if e.AllDay {
			duration = 24 * time.Hour
		}
	}
	if c.Start != nil {
		e.Start = *c.Start
		e.End = e.Start.Add(duration)
	}
	if c.End != nil {
		e.End = *c.End
	}
	if e.AllDay {
		e.Start, e.End, e.TimeZone = dateOnly(e.Start), dateOnly(e.End), ""
	} else if c.Start != nil {
		e.TimeZone = zoneName(c.Start.Location(), e.TimeZone)
	}
	return validate(*e)
}

// Update changes an event. For instances of a series, scope is required.
func (s *Service) Update(ctx context.Context, id string, ch Changes, scope Scope) (model.Event, error) {
	e, err := s.Get(ctx, id)
	if err != nil {
		return model.Event{}, err
	}
	if _, err := s.writableCalendar(ctx, e.CalendarID); err != nil {
		return model.Event{}, err
	}
	if !e.IsInstance() { // one-off event, or a master addressed directly
		if err := ch.apply(&e); err != nil {
			return model.Event{}, err
		}
		return e, s.store.InTx(ctx, func(tx *store.Store) error { return s.save(ctx, tx, e, store.OpPatch) })
	}
	switch scope {
	case ScopeThis:
		if err := ch.apply(&e); err != nil {
			return model.Event{}, err
		}
		return e, s.store.InTx(ctx, func(tx *store.Store) error { return s.save(ctx, tx, e, store.OpPatch) })
	case ScopeAll:
		return s.updateAll(ctx, e, ch)
	case ScopeFollowing:
		return s.updateFollowing(ctx, e, ch)
	default:
		return model.Event{}, ErrNeedScope
	}
}

// updateAll applies ch to the series master. Time changes made relative to
// the instance are applied to the master as a shift.
func (s *Service) updateAll(ctx context.Context, inst model.Event, ch Changes) (model.Event, error) {
	master, err := s.store.GetEvent(ctx, s.account, inst.CalendarID, inst.SeriesID)
	if err != nil {
		return model.Event{}, err
	}
	moved := inst.Clone()
	if err := ch.apply(&moved); err != nil {
		return model.Event{}, err
	}
	if err := ch.withoutTiming().apply(&master); err != nil {
		return model.Event{}, err
	}
	master.AllDay = moved.AllDay
	master.Start = master.Start.Add(moved.Start.Sub(inst.Start))
	master.End = master.Start.Add(moved.Duration())
	master.TimeZone = moved.TimeZone
	if master.AllDay {
		master.Start, master.End = dateOnly(master.Start), dateOnly(master.End)
	}
	return master, s.store.InTx(ctx, func(tx *store.Store) error { return s.save(ctx, tx, master, store.OpPatch) })
}

// updateFollowing ends the series before inst and starts a new series at inst
// with ch applied (design §4).
func (s *Service) updateFollowing(ctx context.Context, inst model.Event, ch Changes) (model.Event, error) {
	master, err := s.store.GetEvent(ctx, s.account, inst.CalendarID, inst.SeriesID)
	if err != nil {
		return model.Event{}, err
	}
	before, after, err := recur.SplitAt(master, inst.OriginalStart)
	if errors.Is(err, recur.ErrSplitAtFirst) {
		return s.updateAll(ctx, inst, ch)
	}
	if err != nil {
		return model.Event{}, err
	}
	next := recur.Instance(master, inst.OriginalStart) // the original slot, not a moved exception
	if err := ch.apply(&next); err != nil {
		return model.Event{}, err
	}
	next.ID, next.SeriesID, next.OriginalStart = model.NewEventID(), "", time.Time{}
	next.Recurrence = after
	master.Recurrence = before

	return next, s.store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.DeleteExceptionsFrom(ctx, s.account, master.CalendarID, master.ID, inst.OriginalStart); err != nil {
			return err
		}
		if err := s.save(ctx, tx, master, store.OpPatch); err != nil {
			return err
		}
		return s.save(ctx, tx, next, store.OpInsert)
	})
}
```
4. Run: pass.

**Commit:** `feat(service): update with this/following/all scopes`

---

## Task 4.4: Delete and RSVP

> ⚠ **Fixed in Task 13.3:** `RSVP` must refuse events on read-only calendars.

**Goal:**
- **Delete `this`** keeps a *cancelled tombstone* row, so `Expand` hides the slot, and queues a DELETE of the
  instance ID.
- **Delete `following`** truncates the series.
- **Delete `all`** deletes the master.
- **RSVP** changes only *your* attendee entry (`SelfAttendee`, Task 1.4). On events you weren't invited to,
  it returns `ErrNotAttendee`. It queues an `rsvp` op **without If-Match** rather than a patch. The organizer
  may edit the event at any time, and that must not make your reply fail (Task 6.2).

**Files:** create `internal/service/delete_test.go`, `internal/service/delete.go`

**Steps**
1. Test:
```go
package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestDeleteThisInstance(t *testing.T) {
	e := newEnv(t)
	e.series()
	id := model.InstanceID("std", e.at(16, 9, 0), false)

	if err := e.svc.Delete(e.ctx, id, service.ScopeThis); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(16); len(got) != 0 {
		t.Errorf("Nov 16 = %v, want empty", got)
	}
	if got := e.titlesOn(23); len(got) != 1 {
		t.Errorf("Nov 23 = %v, want standup", got)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].Kind != store.OpDelete || ops[0].EventID != id {
		t.Errorf("ops = %+v", ops)
	}
}

func TestDeleteFollowingTruncates(t *testing.T) {
	e := newEnv(t)
	e.series()

	if err := e.svc.Delete(e.ctx, model.InstanceID("std", e.at(16, 9, 0), false), service.ScopeFollowing); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(9); len(got) != 1 {
		t.Errorf("Nov 9 = %v, want standup", got)
	}
	if got := e.titlesOn(16); len(got) != 0 {
		t.Errorf("Nov 16 = %v, want empty", got)
	}
}

func TestDeleteAllRemovesSeriesAndQueuesMasterDelete(t *testing.T) {
	e := newEnv(t)
	e.series()

	if err := e.svc.Delete(e.ctx, model.InstanceID("std", e.at(16, 9, 0), false), service.ScopeAll); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(9); len(got) != 0 {
		t.Errorf("Nov 9 = %v, want empty", got)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].Kind != store.OpDelete || ops[0].EventID != "std" || ops[0].IfMatch != `"m1"` {
		t.Errorf("ops = %+v", ops)
	}
}

func TestRSVP(t *testing.T) {
	e := newEnv(t)
	inv := testutil.Timed("inv", "Party", e.at(20, 18, 0), 2*time.Hour)
	inv.Attendees = []model.Attendee{
		{Email: "host@example.com", Organizer: true, Response: model.ResponseAccepted},
		{Email: testutil.Account, Self: true, Response: model.ResponseNeedsAction},
	}
	e.put(inv)
	e.put(testutil.Timed("mine", "Solo", e.at(20, 9, 0), time.Hour))

	if err := e.svc.RSVP(e.ctx, "inv", model.ResponseAccepted, service.ScopeNone); err != nil {
		t.Fatal(err)
	}

	got, _ := e.svc.Get(e.ctx, "inv")
	if got.SelfAttendee().Response != model.ResponseAccepted || got.Attendees[0].Response != model.ResponseAccepted {
		t.Errorf("attendees = %+v", got.Attendees)
	}
	if ops := e.ops(); len(ops) != 1 || ops[0].Kind != store.OpRSVP || ops[0].IfMatch != "" || ops[0].SendUpdates != "all" {
		t.Errorf("ops = %+v, want one unconditional rsvp op that notifies the organizer", ops)
	}
	if err := e.svc.RSVP(e.ctx, "mine", model.ResponseAccepted, service.ScopeNone); !errors.Is(err, service.ErrNotAttendee) {
		t.Errorf("RSVP on own event: err = %v, want ErrNotAttendee", err)
	}
}

// Moved here from Task 4.1 (see DEVIATIONS.md): an orphan instance is presented
// as a one-off, so RSVP needs no scope.
func TestRSVPOnOrphanInstanceNeedsNoScope(t *testing.T) {
	e := newEnv(t)
	inv := testutil.Timed("theirs_20261116T150000Z", "Their weekly sync", e.at(16, 10, 0), time.Hour)
	inv.SeriesID, inv.OriginalStart = "theirs", e.at(16, 10, 0)
	inv.Attendees = []model.Attendee{{Email: testutil.Account, Self: true, Response: model.ResponseNeedsAction}}
	e.put(inv)

	if err := e.svc.RSVP(e.ctx, inv.ID, model.ResponseAccepted, service.ScopeNone); err != nil {
		t.Errorf("RSVP without scope: %v", err)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
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
```
4. Run: pass.

**Commit:** `feat(service): delete with scopes, RSVP`

---

## Phase 4 checkpoint
`gofmt -l . && go vet ./... && go test ./... -count=1`. You can now create, edit, and expand events entirely
offline. Nothing talks to Google yet.
