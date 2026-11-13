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
