package store_test

import (
	"context"
	"errors"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestPutGetRoundTripsEveryField(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	ny := testutil.NY(t)
	want := testutil.Timed("e1", "Standup", time.Date(2026, 11, 16, 9, 0, 0, 0, ny), 15*time.Minute)
	want.Description = "daily"
	want.Location = "Room 1"
	want.Attendees = []model.Attendee{{Email: "me@example.com", Self: true, Response: model.ResponseAccepted}}
	want.ETag = `"123"`

	if err := s.PutEvent(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEvent(ctx, testutil.Account, testutil.Calendar, "e1")
	if err != nil {
		t.Fatal(err)
	}

	// cmp uses time.Time's Equal method, so the lost *Location pointer doesn't matter.
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("round trip mismatch (-want +got):\n%s", diff)
	}
}

func TestGetEventMissing(t *testing.T) {
	s := testutil.NewStore(t)
	_, err := s.GetEvent(context.Background(), "a", "c", "nope")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFindEventAmbiguousAcrossCalendars(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	e := testutil.Timed("shared", "Party", time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC), time.Hour)
	other := e
	other.CalendarID = "family"
	for _, ev := range []model.Event{e, other} {
		if err := s.PutEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	_, err := s.FindEvent(ctx, testutil.Account, "shared")

	if !errors.Is(err, store.ErrAmbiguous) {
		t.Errorf("err = %v, want ErrAmbiguous", err)
	}
}
