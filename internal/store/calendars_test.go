package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestCalendarsUpsertPreservesSyncTokenAndSortsPrimaryFirst(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	work := model.Calendar{AccountID: "a", ID: "work", Name: "Work", Selected: true}
	primary := model.Calendar{AccountID: "a", ID: "me@x.com", Name: "Me", Primary: true, Selected: true}
	for _, c := range []model.Calendar{work, primary} {
		if err := s.UpsertCalendar(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetCalendarSyncToken(ctx, "a", "work", "tok1"); err != nil {
		t.Fatal(err)
	}

	work.Name = "Work (renamed)"
	if err := s.UpsertCalendar(ctx, work); err != nil {
		t.Fatal(err)
	}

	cals, err := s.Calendars(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 2 || cals[0].ID != "me@x.com" || cals[1].Name != "Work (renamed)" {
		t.Fatalf("Calendars = %+v", cals)
	}
	tok, err := s.CalendarSyncToken(ctx, "a", "work")
	if err != nil || tok != "tok1" {
		t.Errorf("sync token = %q, %v; want tok1 (upsert must not clear it)", tok, err)
	}
}

func TestCalendarSyncTokenUnknownCalendar(t *testing.T) {
	s := testutil.NewStore(t)
	_, err := s.CalendarSyncToken(context.Background(), "a", "nope")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
