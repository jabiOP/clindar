// Package model holds clindar's domain types. It has no dependencies on storage,
// network, or UI code; everything else depends on it.
package model

import "time"

// Event status values, as used by Google Calendar.
const (
	StatusConfirmed = "confirmed"
	StatusTentative = "tentative"
	StatusCancelled = "cancelled"
)

// ResponseStatus is an attendee's RSVP.
type ResponseStatus string

const (
	ResponseNeedsAction ResponseStatus = "needsAction"
	ResponseAccepted    ResponseStatus = "accepted"
	ResponseDeclined    ResponseStatus = "declined"
	ResponseTentative   ResponseStatus = "tentative"
)

type Attendee struct {
	Email     string         `json:"email"`
	Name      string         `json:"name,omitempty"`
	Response  ResponseStatus `json:"response,omitempty"`
	Self      bool           `json:"self,omitempty"`
	Organizer bool           `json:"organizer,omitempty"`
	Optional  bool           `json:"optional,omitempty"`
}

// Event is a single event, a recurring series master, a stored exception,
// or a virtual instance produced by expanding a series. See design §2.
//
// All-day events: AllDay is true, Start/End are UTC midnights, End is exclusive.
// Use Span/Overlaps rather than reading Start/End directly for display.
type Event struct {
	AccountID  string `json:"account_id"`
	CalendarID string `json:"calendar_id"`
	ID         string `json:"id"`

	// SeriesID and OriginalStart are set on exceptions and virtual instances.
	SeriesID      string    `json:"series_id,omitempty"`
	OriginalStart time.Time `json:"original_start,omitzero"`

	Status      string `json:"status"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`

	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	AllDay   bool      `json:"all_day,omitempty"`
	TimeZone string    `json:"time_zone,omitempty"` // IANA name, timed events only

	// Recurrence holds RRULE/EXDATE/RDATE lines; non-empty only on series masters.
	Recurrence []string   `json:"recurrence,omitempty"`
	Attendees  []Attendee `json:"attendees,omitempty"`
	Organizer  string     `json:"organizer,omitempty"` // organizer's email

	ETag    string    `json:"etag,omitempty"`
	Updated time.Time `json:"updated,omitzero"`
	Link    string    `json:"link,omitempty"`     // Google web URL
	ColorID string    `json:"color_id,omitempty"` // Google's event colorId; "" = the calendar's color
	// LabelID is the event's color label (Google's eventLabelId: an entry in
	// its calendar's Labels); it supersedes ColorID. LabelChanged marks a local
	// edit that set or cleared it, so the change is sent the label way.
	LabelID      string `json:"label_id,omitempty"`
	LabelChanged bool   `json:"label_changed,omitempty"`
	MeetLink     string `json:"meet_link,omitempty"` // Google Meet URL (read-only: never sent back)
}

func (e Event) IsMaster() bool          { return len(e.Recurrence) > 0 }
func (e Event) IsInstance() bool        { return e.SeriesID != "" }
func (e Event) Cancelled() bool         { return e.Status == StatusCancelled }
func (e Event) Duration() time.Duration { return e.End.Sub(e.Start) }

// Span returns the event's start and end as instants in loc. All-day events
// are re-anchored to midnight in loc, because "Nov 14" is Nov 14 wherever you are.
func (e Event) Span(loc *time.Location) (start, end time.Time) {
	if !e.AllDay {
		return e.Start.In(loc), e.End.In(loc)
	}
	return civilMidnight(e.Start, loc), civilMidnight(e.End, loc)
}

// Overlaps reports whether the event intersects the half-open window [from, to).
// from's location anchors all-day events. Zero-length events count if they
// start inside the window.
func (e Event) Overlaps(from, to time.Time) bool {
	start, end := e.Span(from.Location())
	if !end.After(start) {
		return !start.Before(from) && start.Before(to)
	}
	return start.Before(to) && end.After(from)
}

// civilMidnight takes t's calendar date (in t's own location) and returns
// midnight of that date in loc.
func civilMidnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// StartOfDay returns local midnight of the day containing t.
func StartOfDay(t time.Time) time.Time { return civilMidnight(t, t.Location()) }

// Clone returns a deep copy. Always Clone before modifying an Event you didn't create.
func (e Event) Clone() Event {
	c := e
	c.Recurrence = append([]string(nil), e.Recurrence...)
	c.Attendees = append([]Attendee(nil), e.Attendees...)
	return c
}

// SelfAttendee returns a pointer into e.Attendees for the current user, or nil.
// Note the pointer receiver: the returned pointer modifies e itself.
func (e *Event) SelfAttendee() *Attendee {
	for i := range e.Attendees {
		if e.Attendees[i].Self {
			return &e.Attendees[i]
		}
	}
	return nil
}

// IsInvitation reports whether you're a guest: on the attendee list but not
// the organizer. Guests can't change the event, only answer it.
func (e Event) IsInvitation() bool {
	self := e.SelfAttendee()
	return self != nil && !self.Organizer
}
