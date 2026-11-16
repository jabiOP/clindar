package syncer_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/testutil"
)

// newMoveWorld has a second calendar, Work, on Google and (after a sync) locally.
func newMoveWorld(t *testing.T) *world {
	w := newWorld(t)
	w.google.AddCalendar(model.Calendar{AccountID: testutil.Account, ID: "work", Name: "Work", AccessRole: "owner", Selected: true})
	return w
}

func (w *world) move(id, to string, sc service.Scope, ch service.Changes) {
	w.t.Helper()
	ch.CalendarID = &to
	if _, err := w.svc.Update(w.ctx, id, ch, sc); err != nil {
		w.t.Fatal(err)
	}
}

// localCopies counts the event's rows in each calendar.
func (w *world) localCopies(id string) map[string]int {
	w.t.Helper()
	out := map[string]int{}
	for _, cal := range []string{testutil.Calendar, "work"} {
		if _, err := w.store.GetEvent(w.ctx, testutil.Account, cal, id); err == nil {
			out[cal]++
		}
	}
	return out
}

func (w *world) onlyIn(id, cal string) {
	w.t.Helper()
	if got := w.localCopies(id); len(got) != 1 || got[cal] != 1 {
		w.t.Errorf("local copies of %s = %v, want one in %s", id, got, cal)
	}
}

func TestMoveReachesGoogleAndKeepsTheID(t *testing.T) {
	w := newMoveWorld(t)
	w.google.Put(testutil.Timed("e1", "Lunch", w.at(16, 12), time.Hour))
	w.sync()
	before, _ := w.store.GetEvent(w.ctx, testutil.Account, testutil.Calendar, "e1")

	w.move("e1", "Work", service.ScopeNone, service.Changes{})
	w.sync()

	if g, ok := w.google.Event("work", "e1"); !ok || g.Cancelled() {
		t.Errorf("Google: e1 not in work: %+v", g)
	}
	if g, _ := w.google.Event(testutil.Calendar, "e1"); !g.Cancelled() {
		t.Errorf("Google: e1 still live in the old calendar")
	}
	w.onlyIn("e1", "work")
	if got, _ := w.store.GetEvent(w.ctx, testutil.Account, "work", "e1"); got.ETag == before.ETag {
		t.Errorf("local etag not refreshed: %s", got.ETag)
	}
	if w.pendingCount() != 0 {
		t.Errorf("pending = %d", w.pendingCount())
	}
	w.sync()
	w.onlyIn("e1", "work")
}

func TestMoveThenEditPushesBothInOrder(t *testing.T) {
	w := newMoveWorld(t)
	w.google.Put(testutil.Timed("e1", "Lunch", w.at(16, 12), time.Hour))
	w.sync()

	w.move("e1", "Work", service.ScopeNone, service.Changes{})
	w.svc.Update(w.ctx, "e1", service.Changes{Title: ptr("Brunch")}, service.ScopeNone)
	w.sync()

	if g, _ := w.google.Event("work", "e1"); g.Title != "Brunch" {
		t.Errorf("Google title = %q, want Brunch (the patch got the move's etag)", g.Title)
	}
	if logs, _ := w.svc.Logs(w.ctx, 10); len(logs) != 0 {
		t.Errorf("logs = %+v, want none", logs)
	}
	w.onlyIn("e1", "work")
}

func TestEditThenMoveDoesNotDuplicate(t *testing.T) {
	w := newMoveWorld(t)
	w.google.Put(testutil.Timed("e1", "Lunch", w.at(16, 12), time.Hour))
	w.sync()

	w.move("e1", "Work", service.ScopeNone, service.Changes{Title: ptr("Brunch")})
	w.sync()

	w.onlyIn("e1", "work")
	if w.localTitle("e1") != "Brunch" {
		t.Errorf("title = %q", w.localTitle("e1"))
	}
	if g, _ := w.google.Event("work", "e1"); g.Title != "Brunch" {
		t.Errorf("Google title = %q", g.Title)
	}
}

func TestMoveOfANewEventIsJustAnInsert(t *testing.T) {
	w := newMoveWorld(t)
	w.sync()
	w.google.Fail = gcal.ErrNotFound // offline-ish: nothing reaches Google
	e, err := w.svc.Create(w.ctx, service.Draft{Title: "Dentist", Start: w.at(17, 9), End: w.at(17, 10)})
	if err != nil {
		t.Fatal(err)
	}
	w.move(e.ID, "Work", service.ScopeNone, service.Changes{})
	w.google.Fail = nil
	w.sync()

	if _, ok := w.google.Event(testutil.Calendar, e.ID); ok {
		t.Error("Google saw the old calendar")
	}
	if _, ok := w.google.Event("work", e.ID); !ok {
		t.Error("not created in work")
	}
	w.onlyIn(e.ID, "work")
}

func TestMovedSeriesTakesItsExceptions(t *testing.T) {
	w := newMoveWorld(t)
	m := testutil.Timed("std", "Standup", w.at(2, 9), 15*time.Minute)
	m.Recurrence = []string{"RRULE:FREQ=DAILY"}
	w.google.Put(m)
	w.sync()
	nov16 := model.InstanceID("std", w.at(16, 9), false)
	w.svc.Update(w.ctx, nov16, service.Changes{Title: ptr("Retro")}, service.ScopeThis)
	w.sync()

	w.move(nov16, "Work", service.ScopeAll, service.Changes{})
	w.sync()
	w.sync()

	w.onlyIn("std", "work")
	w.onlyIn(nov16, "work")
	if w.localTitle(nov16) != "Retro" { // also: Get isn't ambiguous
		t.Errorf("exception title = %q", w.localTitle(nov16))
	}
	if g, ok := w.google.Event("work", nov16); !ok || g.Title != "Retro" {
		t.Errorf("Google exception = %+v", g)
	}
}

func TestFullResyncBeforeTheMoveLands(t *testing.T) {
	w := newMoveWorld(t)
	m := testutil.Timed("std", "Standup", w.at(2, 9), 15*time.Minute)
	m.Recurrence = []string{"RRULE:FREQ=DAILY"}
	w.google.Put(m)
	w.sync()
	nov16 := model.InstanceID("std", w.at(16, 9), false)
	w.svc.Update(w.ctx, nov16, service.Changes{Title: ptr("Retro")}, service.ScopeThis)
	w.sync()

	w.move("std", "Work", service.ScopeNone, service.Changes{})
	w.store.ResetSyncTokens(w.ctx, testutil.Account)
	if err := w.engine.Pull(w.ctx); err != nil { // without pushing first
		t.Fatal(err)
	}

	w.onlyIn("std", "work")
	w.onlyIn(nov16, "work")
	w.sync()
	w.onlyIn("std", "work")
	w.onlyIn(nov16, "work")
}

func TestRefusedMoveGoesBack(t *testing.T) {
	w := newMoveWorld(t)
	w.google.Put(testutil.Timed("e1", "Birthday", w.at(16, 12), time.Hour))
	w.sync()
	w.google.MoveFail = gcal.ErrPermanent

	w.move("e1", "Work", service.ScopeNone, service.Changes{})
	w.sync()

	w.onlyIn("e1", testutil.Calendar)
	if w.pendingCount() != 0 {
		t.Errorf("pending = %d", w.pendingCount())
	}
	logs, _ := w.svc.Logs(w.ctx, 10)
	if len(logs) != 1 || !strings.Contains(logs[0].Message, "couldn't move it") {
		t.Errorf("logs = %+v", logs)
	}
}
