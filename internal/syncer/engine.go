// Package syncer moves changes between the local store and Google:
// push the outbox, then pull changes with sync tokens (design §3).
package syncer

import (
	"context"
	"fmt"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

// Remote is what the engine needs from Google. *gcal.Client implements it for
// real; *gcaltest.Fake implements it for tests.
type Remote interface {
	ListCalendars(ctx context.Context, token string) (cals []model.Calendar, deleted []string, next string, err error)
	ListEvents(ctx context.Context, calendarID, token string) ([]model.Event, string, error)
	GetEvent(ctx context.Context, calendarID, eventID string) (model.Event, error)
	InsertEvent(ctx context.Context, calendarID string, e model.Event, sendUpdates string) (model.Event, error)
	PatchEvent(ctx context.Context, calendarID string, e model.Event, ifMatch, sendUpdates string) (model.Event, error)
	RespondEvent(ctx context.Context, calendarID string, e model.Event, sendUpdates string) (model.Event, error)
	DeleteEvent(ctx context.Context, calendarID, eventID, ifMatch, sendUpdates string) error
}

type Engine struct {
	Store     *store.Store
	Remote    Remote
	AccountID string
	Now       func() time.Time
}

// Sync pushes local changes, then pulls remote ones. If the push hits a
// transient error (offline), it stops and returns it; queued ops stay queued.
func (e *Engine) Sync(ctx context.Context) error {
	if err := e.Push(ctx); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	if err := e.Pull(ctx); err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	return e.Store.SetLastSync(ctx, e.AccountID, e.Now())
}
