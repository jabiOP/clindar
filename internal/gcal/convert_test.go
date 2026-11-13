package gcal_test

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/calendar/v3"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
)

func TestFromAPITimedEvent(t *testing.T) {
	ev := &calendar.Event{
		Id: "e1", Status: "confirmed", Summary: "Standup", Etag: `"1"`,
		Start:     &calendar.EventDateTime{DateTime: "2026-11-16T09:00:00-05:00", TimeZone: "America/New_York"},
		End:       &calendar.EventDateTime{DateTime: "2026-11-16T09:15:00-05:00", TimeZone: "America/New_York"},
		Organizer: &calendar.EventOrganizer{Email: "boss@example.com"},
		Attendees: []*calendar.EventAttendee{{Email: "me@example.com", Self: true, ResponseStatus: "accepted"}},
	}

	got, err := gcal.FromAPI("acct", "cal", ev)

	if err != nil {
		t.Fatal(err)
	}
	want := model.Event{
		AccountID: "acct", CalendarID: "cal", ID: "e1", Status: "confirmed", Title: "Standup", ETag: `"1"`,
		Start:    time.Date(2026, 11, 16, 14, 0, 0, 0, time.UTC),
		End:      time.Date(2026, 11, 16, 14, 15, 0, 0, time.UTC),
		TimeZone: "America/New_York", Organizer: "boss@example.com",
		Attendees: []model.Attendee{{Email: "me@example.com", Self: true, Response: model.ResponseAccepted}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func TestFromAPIAllDayAndCancelledInstance(t *testing.T) {
	allDay, err := gcal.FromAPI("a", "c", &calendar.Event{Id: "h", Summary: "Holiday",
		Start: &calendar.EventDateTime{Date: "2026-11-26"}, End: &calendar.EventDateTime{Date: "2026-11-27"}})
	if err != nil || !allDay.AllDay || !allDay.Start.Equal(time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)) ||
		allDay.Status != model.StatusConfirmed {
		t.Errorf("all-day = %+v, %v", allDay, err)
	}

	// Incremental sync sends deleted instances with almost no fields.
	gone, err := gcal.FromAPI("a", "c", &calendar.Event{Id: "s_20261116T140000Z", Status: "cancelled",
		RecurringEventId: "s", OriginalStartTime: &calendar.EventDateTime{DateTime: "2026-11-16T14:00:00Z"}})
	if err != nil || !gone.Cancelled() || gone.SeriesID != "s" || gone.OriginalStart.IsZero() {
		t.Errorf("cancelled instance = %+v, %v", gone, err)
	}
}

func TestToAPIForcesClearableFields(t *testing.T) {
	e := model.Event{ID: "e1", Title: "x", TimeZone: "America/New_York",
		Start: time.Date(2026, 11, 16, 14, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 16, 15, 0, 0, 0, time.UTC)}

	ev := gcal.ToAPI(e)
	body, err := ev.MarshalJSON()

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"description":""`) {
		t.Errorf("empty description must be sent so it can be cleared; body = %s", body)
	}
	if ev.Start.TimeZone != "America/New_York" || ev.Start.DateTime != "2026-11-16T14:00:00Z" {
		t.Errorf("start = %+v", ev.Start)
	}
}

func TestAttendeesToAPISendsOnlyAttendees(t *testing.T) {
	e := model.Event{ID: "e1", Title: "Their meeting", Description: "agenda",
		Attendees: []model.Attendee{{Email: "me@example.com", Self: true, Response: model.ResponseAccepted}}}

	body, err := gcal.AttendeesToAPI(e).MarshalJSON()

	if err != nil {
		t.Fatal(err)
	}
	if want := `{"attendees":[{"email":"me@example.com","responseStatus":"accepted"}]}`; string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestToAPIAllDayUsesDates(t *testing.T) {
	e := model.Event{ID: "e", Title: "x", AllDay: true,
		Start: time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)}

	ev := gcal.ToAPI(e)

	if ev.Start.Date != "2026-11-26" || ev.End.Date != "2026-11-27" || ev.Start.DateTime != "" {
		t.Errorf("start=%+v end=%+v", ev.Start, ev.End)
	}
}
