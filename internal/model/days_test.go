package model_test

import (
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
)

func TestSortEventsAllDayFirstThenStartThenTitle(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	at := func(h int) time.Time { return time.Date(2026, 11, 14, h, 0, 0, 0, ny) }
	events := []model.Event{
		{Title: "B", Start: at(9), End: at(10)},
		{Title: "A", Start: at(9), End: at(10)},
		{Title: "early", Start: at(8), End: at(9)},
		{Title: "holiday", AllDay: true,
			Start: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)},
	}

	model.SortEvents(events, ny)

	var got []string
	for _, e := range events {
		got = append(got, e.Title)
	}
	want := []string{"holiday", "early", "A", "B"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestGroupByDayPutsMultiDayEventsOnEveryDay(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	from := time.Date(2026, 11, 14, 15, 30, 0, 0, ny) // mid-day: grouping starts at that day's midnight
	trip := model.Event{Title: "trip", AllDay: true,
		Start: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)} // Sat+Sun
	lunch := model.Event{Title: "lunch",
		Start: time.Date(2026, 11, 15, 12, 0, 0, 0, ny),
		End:   time.Date(2026, 11, 15, 13, 0, 0, 0, ny)}

	days := model.GroupByDay([]model.Event{trip, lunch}, from, 3)

	if len(days) != 3 {
		t.Fatalf("len(days) = %d, want 3", len(days))
	}
	wantCounts := []int{1, 2, 0}
	for i, d := range days {
		if len(d.Events) != wantCounts[i] {
			t.Errorf("day %d (%s): %d events, want %d", i, d.Date.Format("Mon 02"), len(d.Events), wantCounts[i])
		}
	}
	if want := time.Date(2026, 11, 14, 0, 0, 0, 0, ny); !days[0].Date.Equal(want) {
		t.Errorf("days[0].Date = %v, want %v", days[0].Date, want)
	}
}
