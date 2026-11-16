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

// Which calendars show is a local choice that overrides Google's "selected".
func TestAgendaFollowsLocalVisibility(t *testing.T) {
	e := newEnv(t)
	e.put(testutil.Timed("lunch", "Lunch", e.at(16, 12, 0), time.Hour))
	hol := testutil.AllDay("thx", "Thanksgiving", 2026, time.November, 26, 1)
	hol.CalendarID = "holidays" // not selected in Google
	e.put(hol)

	if err := e.store.SetCalendarShown(e.ctx, testutil.Account, testutil.Calendar, false); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetCalendarShown(e.ctx, testutil.Account, "holidays", true); err != nil {
		t.Fatal(err)
	}

	if got := e.titlesOn(16); len(got) != 0 {
		t.Errorf("Nov 16 = %v: the primary calendar is hidden locally", got)
	}
	if got := e.titlesOn(26); len(got) != 1 {
		t.Errorf("Nov 26 = %v: Holidays is shown locally, without a -c filter", got)
	}
}
