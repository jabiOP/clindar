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
