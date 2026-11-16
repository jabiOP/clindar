package gcal

import (
	"fmt"
	"time"

	"google.golang.org/api/calendar/v3"

	"github.com/jabiOP/clindar/internal/model"
)

// FromAPI converts a Google event. Deleted events in incremental sync often
// carry only id, status and (for instances) recurringEventId/originalStartTime.
func FromAPI(accountID, calendarID string, ev *calendar.Event) (model.Event, error) {
	e := model.Event{
		AccountID: accountID, CalendarID: calendarID, ID: ev.Id,
		SeriesID: ev.RecurringEventId, Status: ev.Status,
		Title: ev.Summary, Description: ev.Description, Location: ev.Location,
		Recurrence: ev.Recurrence, ETag: ev.Etag, Link: ev.HtmlLink, ColorID: ev.ColorId,
	}
	if e.Status == "" {
		e.Status = model.StatusConfirmed
	}
	if ev.Organizer != nil {
		e.Organizer = ev.Organizer.Email
	}
	e.MeetLink = ev.HangoutLink // read-only: ToAPI never sends it, so patches keep the meeting
	if e.MeetLink == "" && ev.ConferenceData != nil {
		for _, ep := range ev.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" {
				e.MeetLink = ep.Uri
				break
			}
		}
	}
	if ev.Updated != "" {
		e.Updated, _ = time.Parse(time.RFC3339, ev.Updated) // informational only
	}
	if ev.OriginalStartTime != nil {
		t, _, err := parseDateTime(ev.OriginalStartTime)
		if err != nil {
			return e, fmt.Errorf("event %s originalStartTime: %w", ev.Id, err)
		}
		e.OriginalStart = t
	}
	if ev.Start == nil || ev.End == nil {
		if e.Cancelled() {
			return e, nil
		}
		return e, fmt.Errorf("event %s has no start/end", ev.Id)
	}
	start, allDay, err := parseDateTime(ev.Start)
	if err != nil {
		return e, fmt.Errorf("event %s start: %w", ev.Id, err)
	}
	end, _, err := parseDateTime(ev.End)
	if err != nil {
		return e, fmt.Errorf("event %s end: %w", ev.Id, err)
	}
	e.Start, e.End, e.AllDay = start, end, allDay
	if !allDay {
		e.TimeZone = ev.Start.TimeZone
	}
	for _, a := range ev.Attendees {
		e.Attendees = append(e.Attendees, model.Attendee{
			Email: a.Email, Name: a.DisplayName, Response: model.ResponseStatus(a.ResponseStatus),
			Self: a.Self, Organizer: a.Organizer, Optional: a.Optional,
		})
	}
	return e, nil
}

// parseDateTime handles both forms: {date: "2026-11-14"} for all-day
// (returned as UTC midnight) and {dateTime: RFC 3339, timeZone: IANA}.
func parseDateTime(dt *calendar.EventDateTime) (time.Time, bool, error) {
	if dt.Date != "" {
		t, err := time.Parse(time.DateOnly, dt.Date)
		return t, true, err
	}
	t, err := time.Parse(time.RFC3339, dt.DateTime)
	if err != nil {
		return time.Time{}, false, err
	}
	if loc, err := time.LoadLocation(dt.TimeZone); dt.TimeZone != "" && err == nil {
		t = t.In(loc)
	}
	return t, false, nil
}

// ToAPI converts the fields clindar edits. It is used for both insert and patch:
// PATCH replaces exactly the fields present, so we always send all of them.
func ToAPI(e model.Event) *calendar.Event {
	ev := &calendar.Event{
		Id: e.ID, Summary: e.Title, Description: e.Description, Location: e.Location,
		Recurrence: e.Recurrence,
		// The Go client omits zero values; without this, clearing a
		// description would silently do nothing.
		ForceSendFields: []string{"Summary", "Description", "Location"},
	}
	// PATCH merges nested objects field by field: sending start {dateTime}
	// onto a stored start {date} keeps the date, and the event silently stays
	// all-day (likewise the other way). So always send the other kind's fields
	// as explicit nulls; on insert they're harmless.
	if e.AllDay {
		ev.Start = &calendar.EventDateTime{Date: e.Start.UTC().Format(time.DateOnly), NullFields: []string{"DateTime", "TimeZone"}}
		ev.End = &calendar.EventDateTime{Date: e.End.UTC().Format(time.DateOnly), NullFields: []string{"DateTime", "TimeZone"}}
	} else {
		ev.Start = &calendar.EventDateTime{DateTime: e.Start.Format(time.RFC3339), TimeZone: e.TimeZone, NullFields: []string{"Date"}}
		ev.End = &calendar.EventDateTime{DateTime: e.End.Format(time.RFC3339), TimeZone: e.TimeZone, NullFields: []string{"Date"}}
	}
	ev.Attendees = AttendeesToAPI(e).Attendees
	ev.ColorId = e.ColorID
	if e.ColorID == "" {
		ev.NullFields = append(ev.NullFields, "ColorId") // back to the calendar's color; omitted fields don't clear
	}
	return ev
}

// AttendeesToAPI builds a patch body containing only the attendee list.
func AttendeesToAPI(e model.Event) *calendar.Event {
	ev := &calendar.Event{}
	for _, a := range e.Attendees {
		ev.Attendees = append(ev.Attendees, &calendar.EventAttendee{
			Email: a.Email, ResponseStatus: string(a.Response), Optional: a.Optional,
		})
	}
	return ev
}

// CalendarFromAPI converts a calendar list entry.
func CalendarFromAPI(accountID string, c *calendar.CalendarListEntry) model.Calendar {
	name := c.SummaryOverride
	if name == "" {
		name = c.Summary
	}
	return model.Calendar{
		AccountID: accountID, ID: c.Id, Name: name, Color: c.BackgroundColor,
		AccessRole: c.AccessRole, Primary: c.Primary, Selected: c.Selected, TimeZone: c.TimeZone,
	}
}
