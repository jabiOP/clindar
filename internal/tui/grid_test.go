package tui

import (
	"testing"
	"time"

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

	got := renderGrid(gridView{
		start: start, today: time.Date(2026, 11, 11, 0, 0, 0, 0, ny), selected: time.Date(2026, 11, 13, 0, 0, 0, 0, ny),
		days: model.GroupByDay(events, start, gridDays), width: 70, height: 18, loc: ny,
	})

	testutil.Golden(t, "grid_cells", got)
}
