package syncer_test

import (
	"context"
	"errors"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/gcal/gcaltest"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/syncer"
	"github.com/jabiOP/clindar/internal/testutil"
)

var errOffline = errors.New("dial tcp: network is unreachable")

// world wires a real store + service to a fake Google, like the app does.
type world struct {
	t      *testing.T
	ctx    context.Context
	store  *store.Store
	svc    *service.Service
	google *gcaltest.Fake
	engine *syncer.Engine
	ny     *time.Location
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ny := testutil.NY(t)
	st := testutil.NewStore(t)
	now := func() time.Time { return time.Date(2026, 11, 12, 10, 30, 0, 0, ny) }
	google := gcaltest.NewFake(model.Calendar{AccountID: testutil.Account, ID: testutil.Calendar,
		Name: "Me", Primary: true, Selected: true, AccessRole: "owner", TimeZone: "America/New_York"})
	return &world{
		t: t, ctx: context.Background(), store: st, ny: ny, google: google,
		svc:    service.New(st, testutil.Account, now, "none"),
		engine: &syncer.Engine{Store: st, Remote: google, AccountID: testutil.Account, Now: now},
	}
}

func (w *world) at(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, w.ny) }

func (w *world) sync() {
	w.t.Helper()
	if err := w.engine.Sync(w.ctx); err != nil {
		w.t.Fatalf("Sync: %v", err)
	}
}

func (w *world) localTitle(id string) string {
	w.t.Helper()
	e, err := w.svc.Get(w.ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return "<none>"
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return e.Title
}

func (w *world) pendingCount() int {
	w.t.Helper()
	n, err := w.store.PendingCount(w.ctx, testutil.Account)
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

func ptr[T any](v T) *T { return &v }
