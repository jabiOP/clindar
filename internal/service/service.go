// Package service is the API every front-end (CLI, TUI) uses. Reads come from
// the store; writes update the store AND enqueue an outbox op in one
// transaction. Nothing else may write events.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

var (
	ErrNeedScope    = errors.New("this is a recurring event: choose a scope (this, following, or all)")
	ErrReadOnly     = errors.New("calendar is read-only")
	ErrNotAttendee  = errors.New("you are not an attendee of this event")
	ErrNoCalendar   = errors.New("no such calendar")
	ErrInvalidEvent = errors.New("invalid event")

	ErrInvitation     = errors.New("this is an invitation: only the organizer can change it")
	ErrMoveOccurrence = errors.New("one occurrence can't move to another calendar: move the whole series (scope all)")
)

type Service struct {
	store       *store.Store
	account     string
	now         func() time.Time
	sendUpdates string // "all" | "none": used when an event has attendees
}

func New(st *store.Store, accountID string, now func() time.Time, sendUpdates string) *Service {
	return &Service{store: st, account: accountID, now: now, sendUpdates: sendUpdates}
}

func (s *Service) AccountID() string { return s.account }

// Calendars returns the account's calendars, primary first.
func (s *Service) Calendars(ctx context.Context) ([]model.Calendar, error) {
	return s.store.Calendars(ctx, s.account)
}

// ResolveCalendar finds a calendar by exact ID or case-insensitive name.
// "" means the primary calendar.
func (s *Service) ResolveCalendar(ctx context.Context, nameOrID string) (model.Calendar, error) {
	cals, err := s.Calendars(ctx)
	if err != nil {
		return model.Calendar{}, err
	}
	for _, c := range cals {
		if (nameOrID == "" && c.Primary) || c.ID == nameOrID || (nameOrID != "" && strings.EqualFold(c.Name, nameOrID)) {
			return c, nil
		}
	}
	if nameOrID == "" {
		return model.Calendar{}, fmt.Errorf("primary calendar: %w (run clindar sync)", ErrNoCalendar)
	}
	return model.Calendar{}, fmt.Errorf("%q: %w (see clindar calendars)", nameOrID, ErrNoCalendar)
}

// Status summarizes sync health for status bars and the TUI.
type Status struct {
	LastSync  time.Time
	Pending   int
	Discarded int // discarded edits since the `since` passed to Status
}

func (s *Service) Status(ctx context.Context, since time.Time) (Status, error) {
	var st Status
	var err error
	if st.LastSync, err = s.store.LastSync(ctx, s.account); err != nil {
		return st, err
	}
	if st.Pending, err = s.store.PendingCount(ctx, s.account); err != nil {
		return st, err
	}
	st.Discarded, err = s.store.CountLogsSince(ctx, s.account, store.LogDiscardedEdit, since)
	return st, err
}

// EventColors returns Google's event palette (colorId → "#rrggbb"), as cached by sync.
func (s *Service) EventColors(ctx context.Context) (map[string]string, error) {
	return s.store.EventColors(ctx, s.account)
}

// SetCalendarShown shows or hides a calendar in clindar. It is a local choice:
// it overrides Google's "selected" flag and is never sent to Google.
func (s *Service) SetCalendarShown(ctx context.Context, calendarID string, shown bool) error {
	return s.store.SetCalendarShown(ctx, s.account, calendarID, shown)
}

func (s *Service) Logs(ctx context.Context, limit int) ([]store.LogEntry, error) {
	return s.store.Logs(ctx, s.account, limit)
}
