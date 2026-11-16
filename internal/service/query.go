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

// Filter narrows Agenda. Empty CalendarIDs means the visible calendars (the local
// choice if any, else Google's "selected").
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
		if (len(f.CalendarIDs) == 0 && c.Visible) || slices.Contains(f.CalendarIDs, c.ID) {
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
