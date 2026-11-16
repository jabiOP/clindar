package service_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

// You invited sam, and sam's calendar is shared with you: the event is
// stored twice under one ID, once per calendar.
func sharedCopies(t *testing.T) *env {
	e := newEnv(t)
	if err := e.store.UpsertCalendar(e.ctx, model.Calendar{AccountID: testutil.Account, ID: "sam@x", Name: "Sam",
		AccessRole: "writer", Selected: true}); err != nil {
		t.Fatal(err)
	}
	mine := testutil.Timed("shared", "Review", e.at(16, 10, 0), time.Hour)
	mine.Attendees = []model.Attendee{{Email: testutil.Account, Self: true, Organizer: true}, {Email: "sam@x"}}
	theirs := mine.Clone()
	theirs.CalendarID = "sam@x"
	theirs.Attendees = []model.Attendee{{Email: testutil.Account, Organizer: true}, {Email: "sam@x", Self: true}}
	e.put(mine)
	e.put(theirs)
	return e
}

func TestBareIDPicksTheCopyForTheAction(t *testing.T) {
	e := sharedCopies(t)

	if got, err := e.svc.Get(e.ctx, "shared"); err != nil || got.CalendarID != testutil.Calendar {
		t.Errorf("Get = %q, %v; want your own (editable) copy", got.CalendarID, err)
	}
	if _, err := e.svc.Update(e.ctx, "shared", service.Changes{Title: ptr("Review v2")}, service.ScopeNone); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RSVP(e.ctx, "shared", model.ResponseAccepted, service.ScopeNone); err != nil {
		t.Fatal(err)
	}
	ops := e.ops()
	if len(ops) != 2 || ops[0].CalendarID != testutil.Calendar || ops[1].Kind != store.OpRSVP || ops[1].CalendarID != "sam@x" {
		t.Errorf("ops = %+v, want the edit on yours and the reply on sam's", ops)
	}
}

func TestRefNamesTheCopy(t *testing.T) {
	e := sharedCopies(t)
	for _, ref := range []string{service.Ref("sam@x", "shared"), "Sam/shared"} {
		if got, err := e.svc.Get(e.ctx, ref); err != nil || got.CalendarID != "sam@x" {
			t.Errorf("%s: %q, %v", ref, got.CalendarID, err)
		}
	}
}

func TestEqualCopiesAreAmbiguousAndSayWhich(t *testing.T) {
	e := newEnv(t)
	e.store.UpsertCalendar(e.ctx, model.Calendar{AccountID: testutil.Account, ID: "team", Name: "Team", AccessRole: "writer"})
	ev := testutil.Timed("dup", "Offsite", e.at(16, 10, 0), time.Hour)
	e.put(ev)
	ev.CalendarID = "team"
	e.put(ev)

	_, err := e.svc.Get(e.ctx, "dup")

	if !errors.Is(err, store.ErrAmbiguous) || !strings.Contains(err.Error(), "team/dup") {
		t.Errorf("err = %v, want ambiguous, naming team/dup", err)
	}
}
