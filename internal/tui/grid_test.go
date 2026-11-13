package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestWeekStart(t *testing.T) {
	ny := testutil.NY(t)
	thu := time.Date(2026, 11, 12, 15, 0, 0, 0, ny)
	if got := weekStart(thu, time.Monday); !got.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, ny)) {
		t.Errorf("monday start = %v", got)
	}
	if got := weekStart(thu, time.Sunday); !got.Equal(time.Date(2026, 11, 8, 0, 0, 0, 0, ny)) {
		t.Errorf("sunday start = %v", got)
	}
}

func TestRenderGridCells(t *testing.T) {
	ny := testutil.NY(t)
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
	var events []model.Event
	for h := 8; h < 13; h++ { // five events on Tuesday: more than a cell can show
		events = append(events, testutil.Timed("e", "Meeting", time.Date(2026, 11, 10, h, 0, 0, 0, ny), time.Hour))
	}
	events = append(events, testutil.AllDay("t", "A very long trip name", 2026, time.November, 12, 2))

	got, _ := renderGrid(gridView{
		start: start, today: time.Date(2026, 11, 11, 0, 0, 0, 0, ny), selected: time.Date(2026, 11, 13, 0, 0, 0, 0, ny),
		days: model.GroupByDay(events, start, gridDays), width: 70, height: 18, loc: ny,
	})

	testutil.Golden(t, "grid_cells", got)
}

// gridWithTrip renders the week of Nov 9 (width 112: 16 columns per day) with a
// trip Thu 12–Fri 13 and two events on Tue 10, mouse handles on.
func gridWithTrip(t *testing.T, drop time.Time) (string, hitMap) {
	t.Helper()
	ny := testutil.NY(t)
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
	events := []model.Event{
		testutil.Timed("a", "Dentist", time.Date(2026, 11, 10, 9, 0, 0, 0, ny), time.Hour),
		testutil.Timed("b", "Gym", time.Date(2026, 11, 10, 18, 0, 0, 0, ny), time.Hour),
		testutil.AllDay("trip", "Trip", 2026, time.November, 12, 2),
	}
	return renderGrid(gridView{
		start: start, today: time.Date(2026, 11, 9, 0, 0, 0, 0, ny), selected: time.Date(2026, 11, 9, 0, 0, 0, 0, ny),
		days: model.GroupByDay(events, start, gridDays), width: 112, height: 18, loc: ny,
		handles: true, drop: drop,
	})
}

func TestGridHandles(t *testing.T) {
	got, _ := gridWithTrip(t, time.Time{})
	testutil.Golden(t, "grid_handles", got)
}

func TestGridHitMap(t *testing.T) {
	ny := testutil.NY(t)
	screen, hits := gridWithTrip(t, time.Time{})
	thu, fri := time.Date(2026, 11, 12, 0, 0, 0, 0, ny), time.Date(2026, 11, 13, 0, 0, 0, 0, ny)

	// The trip's ◂ is on Thursday, its ▸ on Friday, and its body in between.
	x, y := find(t, screen, "◂Trip")
	if got, _ := hits.at(x, y); got.kind != targetEvent || got.part != partStart || !got.day.Equal(thu) || got.event.ID != "trip" {
		t.Errorf("◂ = %+v", got)
	}
	if got, _ := hits.at(x+2, y); got.part != partBody {
		t.Errorf("body = %+v", got)
	}
	line := strings.Split(screen, "\n")[y]
	x = runewidth.StringWidth(line[:strings.LastIndex(line, "▸")]) // the trip's ▸ is the last one on its line
	if got, _ := hits.at(x, y); got.part != partEnd || !got.day.Equal(fri) || got.event.ID != "trip" {
		t.Errorf("▸ = %+v", got)
	}
	// The blank line under Gym (cells are 4 lines tall here) is Tuesday itself.
	x, y = find(t, screen, "09:00 Den")
	if got, _ := hits.at(x+1, y+2); got.kind != targetDay || !got.day.Equal(time.Date(2026, 11, 10, 0, 0, 0, 0, ny)) {
		t.Errorf("empty part of Tue = %+v", got)
	}
}

func TestGridMarksDropTarget(t *testing.T) {
	ny := testutil.NY(t)
	screen, _ := gridWithTrip(t, time.Date(2026, 11, 14, 0, 0, 0, 0, ny))
	find(t, screen, "»14") // fails the test if the marker is missing
}
