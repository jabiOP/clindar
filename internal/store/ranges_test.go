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

func TestSinglesInRange(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	ny := testutil.NY(t)
	at := func(day, h int) time.Time { return time.Date(2026, 11, day, h, 0, 0, 0, ny) }

	inside := testutil.Timed("in", "inside", at(14, 9), time.Hour)
	before := testutil.Timed("before", "before", at(13, 9), time.Hour)
	holiday := testutil.AllDay("hol", "holiday", 2026, time.November, 14, 1)
	cancelled := testutil.Timed("x", "cancelled", at(14, 11), time.Hour)
	cancelled.Status = model.StatusCancelled
	// An occurrence you were invited to on its own: its series "s" isn't stored.
	orphan := testutil.Timed("s_1", "orphan instance", at(14, 12), time.Hour)
	orphan.SeriesID = "s"
	master := testutil.Timed("m", "master", at(14, 13), time.Hour)
	master.Recurrence = []string{"RRULE:FREQ=DAILY"}
	attached := testutil.Timed("m_1", "exception of a stored series", at(14, 15), time.Hour)
	attached.SeriesID = "m"
	for _, e := range []model.Event{inside, before, holiday, cancelled, orphan, master, attached} {
		if err := s.PutEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.SinglesInRange(ctx, testutil.Account, at(14, 0), at(15, 0))
	if err != nil {
		t.Fatal(err)
	}

	ids := map[string]bool{}
	for _, e := range got {
		ids[e.ID] = true
	}
	if diff := cmp.Diff(map[string]bool{"in": true, "hol": true, "s_1": true}, ids); diff != "" {
		t.Errorf("ids mismatch (-want +got):\n%s", diff)
	}
}

func TestDeleteSeriesAndExceptionsFrom(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	day := func(d int) time.Time { return time.Date(2026, 11, d, 9, 0, 0, 0, time.UTC) }
	master := testutil.Timed("m", "series", day(2), time.Hour)
	master.Recurrence = []string{"RRULE:FREQ=WEEKLY"}
	early := testutil.Timed("m_early", "early exception", day(9), time.Hour)
	early.SeriesID, early.OriginalStart = "m", day(9)
	late := testutil.Timed("m_late", "late exception", day(23), time.Hour)
	late.SeriesID, late.OriginalStart = "m", day(23)
	for _, e := range []model.Event{master, early, late} {
		if err := s.PutEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.DeleteExceptionsFrom(ctx, testutil.Account, testutil.Calendar, "m", day(16)); err != nil {
		t.Fatal(err)
	}
	exc, _ := s.Exceptions(ctx, testutil.Account, testutil.Calendar, "m")
	if len(exc) != 1 || exc[0].ID != "m_early" {
		t.Fatalf("after DeleteExceptionsFrom: %v, want only m_early", exc)
	}

	if err := s.DeleteSeries(ctx, testutil.Account, testutil.Calendar, "m"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEvent(ctx, testutil.Account, testutil.Calendar, "m"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("master still present: %v", err)
	}
	if exc, _ := s.Exceptions(ctx, testutil.Account, testutil.Calendar, "m"); len(exc) != 0 {
		t.Errorf("exceptions still present: %v", exc)
	}
}
