# Phase 6: Sync engine

`internal/syncer` moves changes between the store and Google: **push** the outbox, then **pull** with sync
tokens. Re-read design §3 now, especially the push result table. Each row of it becomes a test here.

Tests in this phase use a **real store + real service + the fake Google** (`world_test.go`), the same wiring as
the app. They read like user stories: "I edit offline, I reconnect, Google has my edit."

---

## Task 6.1: Engine skeleton and pull

**Goal:** the `Remote` interface (both `*gcal.Client` and `*gcaltest.Fake` satisfy it), `Engine.Sync`, and
pulling calendars and events.

Behaviors:
- Calendars and events changed on Google show up locally. Deleted ones disappear, except cancelled
  *instances*, which are kept as tombstones.
- **An event with an unpushed local edit is not overwritten by pull.** Local wins until the push resolves it.
- A 410 (expired token) clears the calendar's cache (keeping rows with pending edits) and does a full resync.

Push isn't implemented yet. Stub it so `Sync` compiles.

**Files:** create `internal/syncer/engine.go`, `internal/syncer/pull.go`, a temporary `internal/syncer/push.go`,
`internal/syncer/world_test.go`, `internal/syncer/pull_test.go`

**Steps**
1. Test harness, `world_test.go`:
```go
package syncer_test

import (
	"context"
	"errors"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/gcal/gcaltest"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/syncer"
	"github.com/jabiOP/clindar/internal/testutil"
)

var errOffline = errors.New("dial tcp: network is unreachable")

// world wires a real store + service to a fake Google, like the app does.
type world struct {
	t      *testing.T
	ctx    context.Context
	store  *store.Store
	svc    *service.Service
	google *gcaltest.Fake
	engine *syncer.Engine
	ny     *time.Location
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ny := testutil.NY(t)
	st := testutil.NewStore(t)
	now := func() time.Time { return time.Date(2026, 11, 12, 10, 30, 0, 0, ny) }
	google := gcaltest.NewFake(model.Calendar{AccountID: testutil.Account, ID: testutil.Calendar,
		Name: "Me", Primary: true, Selected: true, AccessRole: "owner", TimeZone: "America/New_York"})
	return &world{
		t: t, ctx: context.Background(), store: st, ny: ny, google: google,
		svc:    service.New(st, testutil.Account, now, "none"),
		engine: &syncer.Engine{Store: st, Remote: google, AccountID: testutil.Account, Now: now},
	}
}

func (w *world) at(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, w.ny) }

func (w *world) sync() {
	w.t.Helper()
	if err := w.engine.Sync(w.ctx); err != nil {
		w.t.Fatalf("Sync: %v", err)
	}
}

func (w *world) localTitle(id string) string {
	w.t.Helper()
	e, err := w.svc.Get(w.ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return "<none>"
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return e.Title
}

func (w *world) pendingCount() int {
	w.t.Helper()
	n, err := w.store.PendingCount(w.ctx, testutil.Account)
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

func ptr[T any](v T) *T { return &v }
```
2. Tests, `pull_test.go`:
```go
package syncer_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestPullBringsInRemoteChanges(t *testing.T) {
	w := newWorld(t)
	w.google.Put(testutil.Timed("e1", "Dentist", w.at(16, 9), time.Hour))
	w.sync()
	if got := w.localTitle("e1"); got != "Dentist" {
		t.Fatalf("after first sync: %q", got)
	}

	ev, _ := w.google.Event(testutil.Calendar, "e1")
	ev.Title = "Dentist (moved)"
	w.google.Put(ev)
	w.sync()
	if got := w.localTitle("e1"); got != "Dentist (moved)" {
		t.Errorf("after remote edit: %q", got)
	}

	w.google.Remove(testutil.Calendar, "e1")
	w.sync()
	if got := w.localTitle("e1"); got != "<none>" {
		t.Errorf("after remote delete: %q", got)
	}
}

func TestPullDoesNotOverwriteUnpushedEdit(t *testing.T) {
	w := newWorld(t)
	w.google.Put(testutil.Timed("e1", "Original", w.at(16, 9), time.Hour))
	w.sync()
	w.svc.Update(w.ctx, "e1", service.Changes{Title: ptr("Mine")}, service.ScopeNone)
	remote, _ := w.google.Event(testutil.Calendar, "e1")
	remote.Title = "Theirs"
	w.google.Put(remote)

	if err := w.engine.Pull(w.ctx); err != nil { // pull only, as if an edit landed mid-sync
		t.Fatal(err)
	}

	if got := w.localTitle("e1"); got != "Mine" {
		t.Errorf("local title = %q, want Mine until the push resolves it", got)
	}
}

func TestExpiredSyncTokenResyncs(t *testing.T) {
	w := newWorld(t)
	w.google.Put(testutil.Timed("e1", "Keep", w.at(16, 9), time.Hour))
	w.google.Put(testutil.Timed("e2", "Stale", w.at(16, 11), time.Hour))
	w.sync()
	w.google.Remove(testutil.Calendar, "e2")
	w.google.ExpireTokens = true

	w.sync()

	if w.localTitle("e1") != "Keep" || w.localTitle("e2") != "<none>" {
		t.Errorf("e1=%q e2=%q", w.localTitle("e1"), w.localTitle("e2"))
	}
}
```
3. Run: compile failure.
4. `engine.go`:
```go
// Package syncer moves changes between the local store and Google:
// push the outbox, then pull changes with sync tokens (design §3).
package syncer

import (
	"context"
	"fmt"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

// Remote is what the engine needs from Google. *gcal.Client implements it for
// real; *gcaltest.Fake implements it for tests.
type Remote interface {
	ListCalendars(ctx context.Context, token string) (cals []model.Calendar, deleted []string, next string, err error)
	ListEvents(ctx context.Context, calendarID, token string) ([]model.Event, string, error)
	GetEvent(ctx context.Context, calendarID, eventID string) (model.Event, error)
	InsertEvent(ctx context.Context, calendarID string, e model.Event, sendUpdates string) (model.Event, error)
	PatchEvent(ctx context.Context, calendarID string, e model.Event, ifMatch, sendUpdates string) (model.Event, error)
	RespondEvent(ctx context.Context, calendarID string, e model.Event, sendUpdates string) (model.Event, error)
	DeleteEvent(ctx context.Context, calendarID, eventID, ifMatch, sendUpdates string) error
}

type Engine struct {
	Store     *store.Store
	Remote    Remote
	AccountID string
	Now       func() time.Time
}

// Sync pushes local changes, then pulls remote ones. If the push hits a
// transient error (offline), it stops and returns it; queued ops stay queued.
func (e *Engine) Sync(ctx context.Context) error {
	if err := e.Push(ctx); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	if err := e.Pull(ctx); err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	return e.Store.SetLastSync(ctx, e.AccountID, e.Now())
}
```
5. `pull.go`:
```go
package syncer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

// Pull fetches calendar-list changes, then event changes for every calendar.
func (e *Engine) Pull(ctx context.Context) error {
	if err := e.pullCalendars(ctx); err != nil {
		return fmt.Errorf("calendars: %w", err)
	}
	cals, err := e.Store.Calendars(ctx, e.AccountID)
	if err != nil {
		return err
	}
	for _, c := range cals {
		if err := e.pullEvents(ctx, c.ID); err != nil {
			return fmt.Errorf("calendar %q: %w", c.Name, err)
		}
	}
	return nil
}

func (e *Engine) pullCalendars(ctx context.Context) error {
	token, err := e.Store.CalendarsToken(ctx, e.AccountID)
	if err != nil {
		return err
	}
	cals, deleted, next, err := e.Remote.ListCalendars(ctx, token)
	if errors.Is(err, gcal.ErrSyncTokenExpired) {
		token = ""
		cals, deleted, next, err = e.Remote.ListCalendars(ctx, token)
	}
	if err != nil {
		return err
	}
	return e.Store.InTx(ctx, func(tx *store.Store) error {
		if token == "" { // full listing: anything we have that isn't listed is gone
			deleted = append(deleted, missing(ctx, tx, e.AccountID, cals)...)
		}
		for _, c := range cals {
			if err := tx.UpsertCalendar(ctx, c); err != nil {
				return err
			}
		}
		for _, id := range deleted {
			if err := tx.DeleteCalendar(ctx, e.AccountID, id); err != nil {
				return err
			}
		}
		return tx.SetCalendarsToken(ctx, e.AccountID, next)
	})
}

func missing(ctx context.Context, tx *store.Store, accountID string, listed []model.Calendar) []string {
	have, _ := tx.Calendars(ctx, accountID)
	seen := map[string]bool{}
	for _, c := range listed {
		seen[c.ID] = true
	}
	var out []string
	for _, c := range have {
		if !seen[c.ID] {
			out = append(out, c.ID)
		}
	}
	return out
}

func (e *Engine) pullEvents(ctx context.Context, calendarID string) error {
	token, err := e.Store.CalendarSyncToken(ctx, e.AccountID, calendarID)
	if err != nil {
		return err
	}
	events, next, err := e.Remote.ListEvents(ctx, calendarID, token)
	full := token == ""
	if errors.Is(err, gcal.ErrSyncTokenExpired) {
		full = true
		events, next, err = e.Remote.ListEvents(ctx, calendarID, "")
	}
	if err != nil {
		return err
	}
	return e.Store.InTx(ctx, func(tx *store.Store) error {
		if full { // full listing: start over (keeps rows with pending edits)
			if err := tx.ClearCalendarEvents(ctx, e.AccountID, calendarID); err != nil {
				return err
			}
		}
		for _, ev := range events {
			if err := e.apply(ctx, tx, ev); err != nil {
				return err
			}
		}
		return tx.SetCalendarSyncToken(ctx, e.AccountID, calendarID, next)
	})
}

// apply stores one remote change, unless we have an unpushed local edit for
// that event (local wins until pushed; the push then resolves any conflict).
func (e *Engine) apply(ctx context.Context, tx *store.Store, ev model.Event) error {
	pending, err := tx.HasPending(ctx, ev.AccountID, ev.CalendarID, ev.ID)
	if err != nil || pending {
		return err
	}
	switch {
	case !ev.Cancelled():
		return tx.PutEvent(ctx, ev)
	case ev.SeriesID != "":
		return tx.PutEvent(ctx, ev) // cancelled instance: keep as tombstone so Expand hides the slot
	default:
		return tx.DeleteSeries(ctx, ev.AccountID, ev.CalendarID, ev.ID)
	}
}
```
6. Temporary `push.go` (replaced in Task 6.2):
```go
package syncer

import "context"

// Push is implemented in Task 6.2.
func (e *Engine) Push(ctx context.Context) error { return nil }
```
7. Run `go test -race ./internal/syncer/`: pass.

**Commit:** `feat(syncer): pull calendars and events with sync tokens`

---

## Task 6.2: Push

**Goal:** send outbox ops in order and handle every outcome in the design §3 table.

Behaviors (each has a test):
- A created event reaches Google, and the local copy gets Google's etag.
- **Conflict (412): Google wins.** Local shows Google's version, and a `discarded_edit` log entry says why.
- **Offline:** the op stays queued with `attempts` incremented, and the next sync delivers it. Push stops at the
  first transient error, because later ops may depend on earlier ones (a split is patch-then-insert).
- Instance edits and deletes reach Google as exceptions.
- **An edit made during a push isn't lost.** This exercises `CompleteOp`'s superseded path (Task 2.5) end to end.
- **A 401 keeps everything queued.** One login problem must never discard edits.
- **The queue can't jam.** If an edit is rejected *and* Google won't even let us read the event back (403, e.g. we
  lost access to the calendar), the op is dropped and logged anyway. The next pull fixes local state.
  Otherwise that op would block every later one forever.
- **RSVPs survive organizer edits.** The organizer renames the event while our acceptance is queued. After sync,
  Google has *their* title and *our* response.

**Files:** replace `internal/syncer/push.go`; create `internal/syncer/push_test.go`

**Steps**
1. Tests:
```go
package syncer_test

import (
	"errors"
	"fmt"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestPushCreatesOnGoogleAndStoresEtag(t *testing.T) {
	w := newWorld(t)
	w.sync() // learn calendars
	ev, err := w.svc.Create(w.ctx, service.Draft{Title: "Write report", Start: w.at(16, 14), End: w.at(16, 15)})
	if err != nil {
		t.Fatal(err)
	}

	w.sync()

	remote, ok := w.google.Event(testutil.Calendar, ev.ID)
	if !ok || remote.Title != "Write report" {
		t.Fatalf("Google has %+v, %v", remote, ok)
	}
	local, _ := w.svc.Get(w.ctx, ev.ID)
	if local.ETag != remote.ETag {
		t.Errorf("local etag %q, want Google's %q", local.ETag, remote.ETag)
	}
	if n := w.pendingCount(); n != 0 {
		t.Errorf("%d ops still pending", n)
	}
}

func TestConflictGoogleWinsAndIsLogged(t *testing.T) {
	w := newWorld(t)
	w.google.Put(testutil.Timed("e1", "Original", w.at(16, 9), time.Hour))
	w.sync()
	if _, err := w.svc.Update(w.ctx, "e1", service.Changes{Title: ptr("Mine")}, service.ScopeNone); err != nil {
		t.Fatal(err)
	}
	remote, _ := w.google.Event(testutil.Calendar, "e1")
	remote.Title = "Theirs"
	w.google.Put(remote) // someone else edits first: etag changes

	w.sync()

	if got, _ := w.google.Event(testutil.Calendar, "e1"); got.Title != "Theirs" {
		t.Errorf("Google title = %q, want Theirs (Google wins)", got.Title)
	}
	if got := w.localTitle("e1"); got != "Theirs" {
		t.Errorf("local title = %q, want Theirs", got)
	}
	logs, _ := w.store.Logs(w.ctx, testutil.Account, 10)
	if len(logs) != 1 || logs[0].Kind != store.LogDiscardedEdit {
		t.Errorf("logs = %+v, want one discarded_edit", logs)
	}
}

func TestOfflineKeepsOpsAndRetries(t *testing.T) {
	w := newWorld(t)
	w.sync()
	ev, _ := w.svc.Create(w.ctx, service.Draft{Title: "Offline idea", Start: w.at(16, 14), End: w.at(16, 15)})
	w.google.Fail = errOffline

	err := w.engine.Sync(w.ctx)

	if !errors.Is(err, errOffline) {
		t.Fatalf("Sync err = %v, want offline error", err)
	}
	ops, _ := w.store.PendingOps(w.ctx, testutil.Account)
	if len(ops) != 1 || ops[0].Attempts != 1 {
		t.Fatalf("ops = %+v, want 1 op with 1 attempt", ops)
	}

	w.google.Fail = nil
	w.sync()

	if _, ok := w.google.Event(testutil.Calendar, ev.ID); !ok {
		t.Error("event never reached Google")
	}
}

func TestInstanceEditsReachGoogle(t *testing.T) {
	w := newWorld(t)
	m := testutil.Timed("std", "Standup", w.at(2, 9), 15*time.Minute)
	m.Recurrence = []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"}
	w.google.Put(m)
	w.sync()
	nov16 := model.InstanceID("std", w.at(16, 9), false)
	nov23 := model.InstanceID("std", w.at(23, 9), false)

	w.svc.Update(w.ctx, nov16, service.Changes{Title: ptr("Retro")}, service.ScopeThis)
	w.svc.Delete(w.ctx, nov23, service.ScopeThis)
	w.sync()

	if ex, ok := w.google.Event(testutil.Calendar, nov16); !ok || ex.Title != "Retro" {
		t.Errorf("Google exception for Nov 16 = %+v, %v", ex, ok)
	}
	if ex, ok := w.google.Event(testutil.Calendar, nov23); !ok || !ex.Cancelled() {
		t.Errorf("Google exception for Nov 23 = %+v, %v (want cancelled)", ex, ok)
	}
	if w.localTitle(nov16) != "Retro" || w.localTitle(nov23) != "<none>" {
		t.Errorf("local: nov16=%q nov23=%q", w.localTitle(nov16), w.localTitle(nov23))
	}
}

// The user edits an event while its insert is being pushed. Their newer edit
// must not be lost, and must reach Google on the next sync.
func TestEditDuringPushIsNotLost(t *testing.T) {
	w := newWorld(t)
	w.sync()
	ev, _ := w.svc.Create(w.ctx, service.Draft{Title: "v1", Start: w.at(16, 14), End: w.at(16, 15)})
	w.google.BeforeWrite = func() {
		w.google.BeforeWrite = nil
		if _, err := w.svc.Update(w.ctx, ev.ID, service.Changes{Title: ptr("v2")}, service.ScopeNone); err != nil {
			t.Error(err)
		}
	}

	w.sync() // pushes v1; v2 arrives mid-flight
	if got := w.localTitle(ev.ID); got != "v2" {
		t.Fatalf("local title after first sync = %q, want v2", got)
	}
	w.sync() // pushes v2 as a patch

	if got, _ := w.google.Event(testutil.Calendar, ev.ID); got.Title != "v2" {
		t.Errorf("Google title = %q, want v2", got.Title)
	}
	if n := w.pendingCount(); n != 0 {
		t.Errorf("%d ops still pending", n)
	}
}

// A rejected edit whose event we can't even read back (403) must not block
// every later edit forever.
func TestUnreadableEventDoesNotJamTheQueue(t *testing.T) {
	w := newWorld(t)
	w.google.Put(testutil.Timed("e1", "Original", w.at(16, 9), time.Hour))
	w.sync()
	w.svc.Update(w.ctx, "e1", service.Changes{Title: ptr("Mine")}, service.ScopeNone)
	remote, _ := w.google.Event(testutil.Calendar, "e1")
	w.google.Put(remote) // new etag: our patch will get 412
	w.google.FailGet = fmt.Errorf("%w: 403 forbidden", gcal.ErrPermanent)
	later, _ := w.svc.Create(w.ctx, service.Draft{Title: "Later edit", Start: w.at(17, 9), End: w.at(17, 10)})

	if err := w.engine.Push(w.ctx); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if _, ok := w.google.Event(testutil.Calendar, later.ID); !ok {
		t.Error("the op behind the rejected one never reached Google")
	}
	if n := w.pendingCount(); n != 0 {
		t.Errorf("%d ops still pending", n)
	}
}

func TestUnauthorizedKeepsOpsQueued(t *testing.T) {
	w := newWorld(t)
	w.sync()
	w.svc.Create(w.ctx, service.Draft{Title: "x", Start: w.at(16, 9), End: w.at(16, 10)})
	w.google.Fail = fmt.Errorf("%w: 401", gcal.ErrUnauthorized)

	err := w.engine.Sync(w.ctx)

	if !errors.Is(err, gcal.ErrUnauthorized) {
		t.Errorf("Sync err = %v, want ErrUnauthorized", err)
	}
	if n := w.pendingCount(); n != 1 {
		t.Errorf("pending = %d, want 1: a login problem must not discard edits", n)
	}
}

// RSVPs go out as attendee-only patches without If-Match: the organizer
// editing the title meanwhile must neither block our reply nor be overwritten.
func TestRSVPSurvivesOrganizerEdits(t *testing.T) {
	w := newWorld(t)
	inv := testutil.Timed("inv", "Party", w.at(20, 18), 2*time.Hour)
	inv.Attendees = []model.Attendee{
		{Email: "host@example.com", Organizer: true, Response: model.ResponseAccepted},
		{Email: testutil.Account, Self: true, Response: model.ResponseNeedsAction},
	}
	w.google.Put(inv)
	w.sync()
	if err := w.svc.RSVP(w.ctx, "inv", model.ResponseAccepted, service.ScopeNone); err != nil {
		t.Fatal(err)
	}
	remote, _ := w.google.Event(testutil.Calendar, "inv")
	remote.Title = "Party (moved to the roof)"
	w.google.Put(remote)

	w.sync()

	got, _ := w.google.Event(testutil.Calendar, "inv")
	if got.Title != "Party (moved to the roof)" {
		t.Errorf("title = %q: the RSVP overwrote the organizer's edit", got.Title)
	}
	if got.SelfAttendee() == nil || got.Attendees[1].Response != model.ResponseAccepted {
		t.Errorf("attendees = %+v, want our acceptance", got.Attendees)
	}
}

// RSVP, then edit the same event before syncing, while the organizer changes
// it on Google. Our edit must still conflict (Google wins), not overwrite them.
func TestEditAfterRSVPStillConflicts(t *testing.T) {
	w := newWorld(t)
	ev := testutil.Timed("inv", "Party", w.at(20, 18), 2*time.Hour)
	ev.Attendees = []model.Attendee{{Email: testutil.Account, Self: true, Response: model.ResponseNeedsAction}}
	w.google.Put(ev)
	w.sync()
	w.svc.RSVP(w.ctx, "inv", model.ResponseAccepted, service.ScopeNone)
	if _, err := w.svc.Update(w.ctx, "inv", service.Changes{Title: ptr("Mine")}, service.ScopeNone); err != nil {
		t.Fatal(err)
	}
	remote, _ := w.google.Event(testutil.Calendar, "inv")
	remote.Title = "Theirs"
	w.google.Put(remote)

	w.sync()

	if got, _ := w.google.Event(testutil.Calendar, "inv"); got.Title != "Theirs" {
		t.Errorf("Google title = %q, want Theirs (our edit must conflict, not overwrite)", got.Title)
	}
	logs, _ := w.store.Logs(w.ctx, testutil.Account, 10)
	if len(logs) != 1 || logs[0].Kind != store.LogDiscardedEdit {
		t.Errorf("logs = %+v, want one discarded_edit", logs)
	}
}
```
2. Run: these fail against the stub (not a compile failure this time). **Check that they fail for the
   right reason:** "event never reached Google", etc.
3. Implement `push.go`:
```go
package syncer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

// Push sends pending ops in order. It stops at the first transient error,
// because later ops may depend on earlier ones (e.g. a series split).
func (e *Engine) Push(ctx context.Context) error {
	ops, err := e.Store.PendingOps(ctx, e.AccountID)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if err := e.pushOne(ctx, op); err != nil {
			if ferr := e.Store.FailOp(ctx, op.Seq, err.Error()); ferr != nil {
				return errors.Join(err, ferr)
			}
			return err
		}
	}
	return nil
}

// pushOne returns an error only for transient failures. Permanent outcomes
// (conflict, gone, rejected) are resolved here and return nil.
func (e *Engine) pushOne(ctx context.Context, op store.Op) error {
	var got model.Event
	var err error
	switch op.Kind {
	case store.OpInsert:
		got, err = e.Remote.InsertEvent(ctx, op.CalendarID, op.Payload, op.SendUpdates)
		if errors.Is(err, gcal.ErrAlreadyExists) { // an earlier push landed but we never heard back
			got, err = e.Remote.GetEvent(ctx, op.CalendarID, op.EventID)
		}
	case store.OpPatch:
		got, err = e.Remote.PatchEvent(ctx, op.CalendarID, op.Payload, op.IfMatch, op.SendUpdates)
	case store.OpRSVP:
		got, err = e.Remote.RespondEvent(ctx, op.CalendarID, op.Payload, op.SendUpdates)
	case store.OpDelete:
		err = e.Remote.DeleteEvent(ctx, op.CalendarID, op.EventID, op.IfMatch, op.SendUpdates)
		if errors.Is(err, gcal.ErrNotFound) {
			err = nil // already gone: that's what we wanted
		}
	}
	switch {
	case err == nil:
		return e.complete(ctx, op, got)
	case errors.Is(err, gcal.ErrConflict):
		return e.discard(ctx, op, store.LogDiscardedEdit,
			fmt.Sprintf("%q: your edit was discarded because the event changed on Google first", title(op)), err)
	case errors.Is(err, gcal.ErrNotFound):
		return e.discard(ctx, op, store.LogError,
			fmt.Sprintf("%q: no longer exists on Google; your edit was dropped", title(op)), err)
	case errors.Is(err, gcal.ErrPermanent):
		return e.discard(ctx, op, store.LogError,
			fmt.Sprintf("%q: Google rejected the change", title(op)), err)
	default:
		return err
	}
}

func (e *Engine) complete(ctx context.Context, op store.Op, got model.Event) error {
	return e.Store.InTx(ctx, func(tx *store.Store) error {
		superseded, err := tx.CompleteOp(ctx, op, got.ETag)
		if err != nil || superseded || op.Kind == store.OpDelete {
			return err // a newer local edit (or a delete) owns the local row
		}
		return tx.PutEvent(ctx, got) // Google's normalized copy, with the new etag
	})
}

// discard drops an op that can never succeed, logs why, and restores the
// event from Google ("Google wins").
func (e *Engine) discard(ctx context.Context, op store.Op, kind, msg string, cause error) error {
	remote, err := e.Remote.GetEvent(ctx, op.CalendarID, op.EventID)
	gone := errors.Is(err, gcal.ErrNotFound)
	// If Google refuses even to show us the event (e.g. 403: access to the
	// calendar was lost), give up on restoring it: drop the op anyway so the
	// queue keeps moving. The next pull fixes local state (a lost calendar
	// disappears from the calendar list, taking its events with it).
	unreadable := errors.Is(err, gcal.ErrPermanent)
	if err != nil && !gone && !unreadable {
		return err // transient: retry the whole op later
	}
	return e.Store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.DropOp(ctx, op.Seq); err != nil {
			return err
		}
		if err := tx.AddLog(ctx, store.LogEntry{Time: e.Now(), Kind: kind, AccountID: op.AccountID,
			CalendarID: op.CalendarID, EventID: op.EventID, Message: msg, Detail: cause.Error()}); err != nil {
			return err
		}
		switch {
		case gone:
			return tx.DeleteSeries(ctx, op.AccountID, op.CalendarID, op.EventID)
		case unreadable:
			return nil
		}
		return tx.PutEvent(ctx, remote)
	})
}

func title(op store.Op) string {
	if op.Payload.Title != "" {
		return op.Payload.Title
	}
	return op.EventID
}
```
4. Run `go test -race ./internal/syncer/`: pass.

**Commit:** `feat(syncer): push outbox with conflict handling`

---

## Task 6.3: Cross-process lock

**Goal:** only one sync at a time across *processes* (daemon, TUI, and CLI). We use `flock(2)`, which works on
Linux and macOS. The OS releases it if the process dies, so a crash can't leave a stale lock behind.
`WithLock` returns `ran=false` (not an error) when someone else is syncing. That's the normal case when the
daemon is running.

**Files:** create `internal/syncer/lock_test.go`, `internal/syncer/lock.go`

**Steps**
1. Test:
```go
package syncer_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jabiOP/clindar/internal/syncer"
)

func TestLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.lock")
	unlock, ok, err := syncer.TryLock(path)
	if err != nil || !ok {
		t.Fatalf("first TryLock = %v, %v", ok, err)
	}

	ran, err := syncer.WithLock(context.Background(), path, func(context.Context) error {
		t.Error("sync ran while lock was held")
		return nil
	})
	if ran || err != nil {
		t.Errorf("WithLock while held = %v, %v; want false, nil", ran, err)
	}

	unlock()
	ran, err = syncer.WithLock(context.Background(), path, func(context.Context) error { return nil })
	if !ran || err != nil {
		t.Errorf("WithLock after unlock = %v, %v; want true, nil", ran, err)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package syncer

import (
	"context"
	"errors"
	"os"
	"syscall"
)

// TryLock takes an exclusive, non-blocking flock on path (works on Linux and
// macOS). ok is false if another process (or another open of the file) holds it.
// The OS releases the lock automatically if the process dies.
func TryLock(path string) (unlock func(), ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true, nil
}

// WithLock runs sync only if nobody else is syncing. ran reports whether it did.
func WithLock(ctx context.Context, lockPath string, sync func(context.Context) error) (ran bool, err error) {
	unlock, ok, err := TryLock(lockPath)
	if err != nil || !ok {
		return false, err
	}
	defer unlock()
	return true, sync(ctx)
}
```
4. Run: pass.

**Commit:** `feat(syncer): flock-based sync lock`

---

## Phase 6 checkpoint
`gofmt -l . && go vet ./... && go test -race ./... -count=1`. The whole offline-edit pipeline now works
against the fake.
