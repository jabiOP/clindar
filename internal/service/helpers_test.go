package service_test

import (
	"context"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

// env is a service over a real temp store, with a fixed clock and two
// calendars: the writable primary, and a read-only, unselected "Holidays".
type env struct {
	t     *testing.T
	ctx   context.Context
	store *store.Store
	svc   *service.Service
	ny    *time.Location
	now   time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := testutil.NewStore(t)
	ny := testutil.NY(t)
	ctx := context.Background()
	for _, c := range []model.Calendar{
		{AccountID: testutil.Account, ID: testutil.Calendar, Name: "Me", Primary: true,
			Selected: true, AccessRole: "owner", TimeZone: "America/New_York"},
		{AccountID: testutil.Account, ID: "holidays", Name: "Holidays", AccessRole: "reader"},
	} {
		if err := st.UpsertCalendar(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny) // Thursday
	svc := service.New(st, testutil.Account, func() time.Time { return now }, "all")
	return &env{t: t, ctx: ctx, store: st, svc: svc, ny: ny, now: now}
}

func (e *env) at(day, hour, min int) time.Time {
	return time.Date(2026, 11, day, hour, min, 0, 0, e.ny)
}

func (e *env) put(ev model.Event) {
	e.t.Helper()
	if err := e.store.PutEvent(e.ctx, ev); err != nil {
		e.t.Fatal(err)
	}
}

// titlesOn returns "15:04 Title" for each event on the given November day.
func (e *env) titlesOn(day int) []string {
	e.t.Helper()
	events, err := e.svc.Agenda(e.ctx, e.at(day, 0, 0), e.at(day+1, 0, 0), service.Filter{})
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, ev := range events {
		out = append(out, ev.Start.In(e.ny).Format("15:04")+" "+ev.Title)
	}
	return out
}

func (e *env) ops() []store.Op {
	e.t.Helper()
	ops, err := e.store.PendingOps(e.ctx, testutil.Account)
	if err != nil {
		e.t.Fatal(err)
	}
	return ops
}

// series stores a weekly Monday 09:00 "Standup" starting Mon Nov 2, with no pending ops.
func (e *env) series() model.Event {
	e.t.Helper()
	m := testutil.Timed("std", "Standup", e.at(2, 9, 0), 15*time.Minute)
	m.Recurrence = []string{"RRULE:FREQ=WEEKLY;BYDAY=MO"}
	m.ETag = `"m1"`
	e.put(m)
	return m
}

func ptr[T any](v T) *T { return &v }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
