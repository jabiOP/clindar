package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/store"
)

// Changes lists fields to modify; nil means "leave as is". Setting Start
// without End moves the event, keeping its duration.
type Changes struct {
	Title, Description, Location *string
	Start, End                   *time.Time
	AllDay                       *bool
	Invite                       *[]string // replaces the attendee list
}

func (c Changes) withoutTiming() Changes {
	c.Start, c.End, c.AllDay = nil, nil, nil
	return c
}

func (c Changes) apply(e *model.Event) error {
	if c.Title != nil {
		e.Title = strings.TrimSpace(*c.Title)
	}
	if c.Description != nil {
		e.Description = *c.Description
	}
	if c.Location != nil {
		e.Location = *c.Location
	}
	if c.Invite != nil {
		setInvites(e, *c.Invite)
	}
	duration := e.Duration()
	if c.AllDay != nil && *c.AllDay != e.AllDay {
		if c.Start == nil {
			return fmt.Errorf("%w: switching all-day on or off needs a new start time", ErrInvalidEvent)
		}
		e.AllDay = *c.AllDay
		duration = time.Hour
		if e.AllDay {
			duration = 24 * time.Hour
		}
	}
	if c.Start != nil {
		e.Start = *c.Start
		e.End = e.Start.Add(duration)
	}
	if c.End != nil {
		e.End = *c.End
	}
	if e.AllDay {
		e.Start, e.End, e.TimeZone = dateOnly(e.Start), dateOnly(e.End), ""
	} else if c.Start != nil {
		e.TimeZone = zoneName(c.Start.Location(), e.TimeZone)
	}
	return validate(*e)
}

// Update changes an event. For instances of a series, scope is required.
func (s *Service) Update(ctx context.Context, id string, ch Changes, scope Scope) (model.Event, error) {
	e, err := s.Get(ctx, id)
	if err != nil {
		return model.Event{}, err
	}
	if _, err := s.writableCalendar(ctx, e.CalendarID); err != nil {
		return model.Event{}, err
	}
	if !e.IsInstance() { // one-off event, or a master addressed directly
		if err := ch.apply(&e); err != nil {
			return model.Event{}, err
		}
		return e, s.store.InTx(ctx, func(tx *store.Store) error { return s.save(ctx, tx, e, store.OpPatch) })
	}
	switch scope {
	case ScopeThis:
		if err := ch.apply(&e); err != nil {
			return model.Event{}, err
		}
		return e, s.store.InTx(ctx, func(tx *store.Store) error { return s.save(ctx, tx, e, store.OpPatch) })
	case ScopeAll:
		return s.updateAll(ctx, e, ch)
	case ScopeFollowing:
		return s.updateFollowing(ctx, e, ch)
	default:
		return model.Event{}, ErrNeedScope
	}
}

// updateAll applies ch to the series master. Time changes made relative to
// the instance are applied to the master as a shift.
func (s *Service) updateAll(ctx context.Context, inst model.Event, ch Changes) (model.Event, error) {
	master, err := s.store.GetEvent(ctx, s.account, inst.CalendarID, inst.SeriesID)
	if err != nil {
		return model.Event{}, err
	}
	moved := inst.Clone()
	if err := ch.apply(&moved); err != nil {
		return model.Event{}, err
	}
	if err := ch.withoutTiming().apply(&master); err != nil {
		return model.Event{}, err
	}
	master.AllDay = moved.AllDay
	master.Start = master.Start.Add(moved.Start.Sub(inst.Start))
	master.End = master.Start.Add(moved.Duration())
	master.TimeZone = moved.TimeZone
	if master.AllDay {
		master.Start, master.End = dateOnly(master.Start), dateOnly(master.End)
	}
	return master, s.store.InTx(ctx, func(tx *store.Store) error { return s.save(ctx, tx, master, store.OpPatch) })
}

// updateFollowing ends the series before inst and starts a new series at inst
// with ch applied (design §4).
func (s *Service) updateFollowing(ctx context.Context, inst model.Event, ch Changes) (model.Event, error) {
	master, err := s.store.GetEvent(ctx, s.account, inst.CalendarID, inst.SeriesID)
	if err != nil {
		return model.Event{}, err
	}
	before, after, err := recur.SplitAt(master, inst.OriginalStart)
	if errors.Is(err, recur.ErrSplitAtFirst) {
		return s.updateAll(ctx, inst, ch)
	}
	if err != nil {
		return model.Event{}, err
	}
	next := recur.Instance(master, inst.OriginalStart) // the original slot, not a moved exception
	if err := ch.apply(&next); err != nil {
		return model.Event{}, err
	}
	next.ID, next.SeriesID, next.OriginalStart = model.NewEventID(), "", time.Time{}
	next.Recurrence = after
	master.Recurrence = before

	return next, s.store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.DeleteExceptionsFrom(ctx, s.account, master.CalendarID, master.ID, inst.OriginalStart); err != nil {
			return err
		}
		if err := s.save(ctx, tx, master, store.OpPatch); err != nil {
			return err
		}
		return s.save(ctx, tx, next, store.OpInsert)
	})
}
