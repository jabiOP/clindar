package syncer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

// Pull fetches calendar-list changes, then event changes for every calendar.
func (e *Engine) Pull(ctx context.Context) error {
	if err := e.pullPalette(ctx); err != nil {
		return fmt.Errorf("colors: %w", err)
	}
	if err := e.pullCalendars(ctx); err != nil {
		return fmt.Errorf("calendars: %w", err)
	}
	cals, err := e.Store.Calendars(ctx, e.AccountID)
	if err != nil {
		return err
	}
	for _, c := range cals {
		if err := e.pullEvents(ctx, c.ID); err != nil {
			return fmt.Errorf("calendar %q: %w", c.Name, err)
		}
	}
	return nil
}

func (e *Engine) pullCalendars(ctx context.Context) error {
	token, err := e.Store.CalendarsToken(ctx, e.AccountID)
	if err != nil {
		return err
	}
	cals, deleted, next, err := e.Remote.ListCalendars(ctx, token)
	if errors.Is(err, gcal.ErrSyncTokenExpired) {
		token = ""
		cals, deleted, next, err = e.Remote.ListCalendars(ctx, token)
	}
	if err != nil {
		return err
	}
	return e.Store.InTx(ctx, func(tx *store.Store) error {
		if token == "" { // full listing: anything we have that isn't listed is gone
			deleted = append(deleted, missing(ctx, tx, e.AccountID, cals)...)
		}
		for _, c := range cals {
			if err := tx.UpsertCalendar(ctx, c); err != nil {
				return err
			}
		}
		for _, id := range deleted {
			if err := tx.DeleteCalendar(ctx, e.AccountID, id); err != nil {
				return err
			}
		}
		return tx.SetCalendarsToken(ctx, e.AccountID, next)
	})
}

func missing(ctx context.Context, tx *store.Store, accountID string, listed []model.Calendar) []string {
	have, _ := tx.Calendars(ctx, accountID)
	seen := map[string]bool{}
	for _, c := range listed {
		seen[c.ID] = true
	}
	var out []string
	for _, c := range have {
		if !seen[c.ID] {
			out = append(out, c.ID)
		}
	}
	return out
}

func (e *Engine) pullEvents(ctx context.Context, calendarID string) error {
	token, err := e.Store.CalendarSyncToken(ctx, e.AccountID, calendarID)
	if err != nil {
		return err
	}
	events, next, err := e.Remote.ListEvents(ctx, calendarID, token)
	full := token == ""
	if errors.Is(err, gcal.ErrSyncTokenExpired) {
		full = true
		events, next, err = e.Remote.ListEvents(ctx, calendarID, "")
	}
	if err != nil {
		return err
	}
	return e.Store.InTx(ctx, func(tx *store.Store) error {
		if full { // full listing: start over (keeps rows with pending edits)
			if err := tx.ClearCalendarEvents(ctx, e.AccountID, calendarID); err != nil {
				return err
			}
		}
		for _, ev := range events {
			if err := e.apply(ctx, tx, ev); err != nil {
				return err
			}
		}
		return tx.SetCalendarSyncToken(ctx, e.AccountID, calendarID, next)
	})
}

// apply stores one remote change, unless we have an unpushed local edit for
// that event (local wins until pushed; the push then resolves any conflict).
func (e *Engine) apply(ctx context.Context, tx *store.Store, ev model.Event) error {
	pending, err := tx.HasPending(ctx, ev.AccountID, ev.CalendarID, ev.ID)
	if err != nil || pending {
		return err
	}
	switch {
	case !ev.Cancelled():
		return tx.PutEvent(ctx, ev)
	case ev.SeriesID != "":
		return tx.PutEvent(ctx, ev) // cancelled instance: keep as tombstone so Expand hides the slot
	default:
		return tx.DeleteSeries(ctx, ev.AccountID, ev.CalendarID, ev.ID)
	}
}

// pullPalette fetches Google's event color palette once and caches it;
// sync --full (ResetSyncTokens) clears the cache so it's fetched again.
func (e *Engine) pullPalette(ctx context.Context) error {
	have, err := e.Store.EventColors(ctx, e.AccountID)
	if err != nil || len(have) > 0 {
		return err
	}
	palette, err := e.Remote.EventColors(ctx)
	if err != nil {
		return err
	}
	return e.Store.SetEventColors(ctx, e.AccountID, palette)
}
