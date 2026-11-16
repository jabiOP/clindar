package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

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
// trip Thu 12–Fri 13 and two events on Tue 10, mouse on.
func gridWithTrip(t *testing.T, hover edgeRef, drop time.Time) (string, hitMap) {
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
		mouse: true, hover: hover, drop: drop,
	})
}

// Hovering the trip's end edge (Friday) draws only that ▸; every other line keeps its full text.
func TestGridHoverShowsOneHandle(t *testing.T) {
	trip := testutil.AllDay("trip", "Trip", 2026, time.November, 12, 2) // same IDs as in gridWithTrip
	got, _ := gridWithTrip(t, edgeOf(trip, partEnd), time.Time{})

	testutil.Golden(t, "grid_hover", got)
	if strings.Count(got, "▸") != 1 || strings.Contains(got, "◂") {
		t.Errorf("want exactly one ▸ and no ◂:\n%s", got)
	}
}

func TestGridHitMap(t *testing.T) {
	ny := testutil.NY(t)
	screen, hits := gridWithTrip(t, edgeRef{}, time.Time{}) // no hover: the zones must exist anyway
	thu, fri := time.Date(2026, 11, 12, 0, 0, 0, 0, ny), time.Date(2026, 11, 13, 0, 0, 0, 0, ny)
	_, y := find(t, screen, "Trip") // first occurrence: Thursday's line
	thuX, friX := 3*16, 4*16

	if got, _ := hits.at(thuX, y); got.part != partStart || !got.day.Equal(thu) || got.event.ID != "trip" {
		t.Errorf("Thu left edge = %+v, want the start handle", got)
	}
	if got, _ := hits.at(thuX+5, y); got.part != partBody {
		t.Errorf("Thu middle = %+v, want the body", got)
	}
	if got, _ := hits.at(friX+14, y); got.part != partEnd || !got.day.Equal(fri) {
		t.Errorf("Fri right edge = %+v, want the end handle", got)
	}
	if got, _ := hits.at(thuX+14, y); got.part != partBody {
		t.Errorf("Thu right edge = %+v: not the last day, so no end handle there", got)
	}
	// The blank line under Gym (cells are 4 lines tall here) is Tuesday itself.
	x, y := find(t, screen, "09:00 Den")
	if got, _ := hits.at(x+1, y+2); got.kind != targetDay || !got.day.Equal(time.Date(2026, 11, 10, 0, 0, 0, 0, ny)) {
		t.Errorf("empty part of Tue = %+v", got)
	}
}

func TestGridMarksDropTarget(t *testing.T) {
	ny := testutil.NY(t)
	screen, _ := gridWithTrip(t, edgeRef{}, time.Date(2026, 11, 14, 0, 0, 0, 0, ny))
	find(t, screen, "»14") // fails the test if the marker is missing
}

// Emoji with a variation selector (❤️ ☀️ ✈️) are 2 columns in lipgloss and in
// terminals but 1 in go-runewidth. If fit pads by a different measure than
// lipgloss joins columns with, that day's column grows and shifts the row.
func TestGridColumnsStayAlignedWithEmoji(t *testing.T) {
	ny := testutil.NY(t)
	start := time.Date(2026, 11, 30, 0, 0, 0, 0, ny)
	events := []model.Event{testutil.Timed("e", "❤️ Ana's birthday", time.Date(2026, 12, 1, 9, 0, 0, 0, ny), time.Hour)}

	got, _ := renderGrid(gridView{
		start: start, today: start, selected: start,
		days: model.GroupByDay(events, start, gridDays), width: 112, height: 18, loc: ny,
	})

	for i, line := range strings.Split(got, "\n")[2:] { // skip the month and weekday headers
		if w := lipgloss.Width(line); w != 112 {
			t.Errorf("grid line %d is %d columns, want 112: %q", i+2, w, line)
		}
	}
}

// Day numbers start where their events start (" 2" would put the digit one column right).
func TestDayNumbersAreLeftAligned(t *testing.T) {
	ny := testutil.NY(t)
	start := time.Date(2026, 11, 30, 0, 0, 0, 0, ny) // Mon Nov 30
	later := time.Date(2026, 12, 25, 0, 0, 0, 0, ny) // keeps the today/selected markers off the first row
	got, _ := renderGrid(gridView{start: start, today: later, selected: later,
		days: model.GroupByDay(nil, start, gridDays), width: 70, height: 18, loc: ny})

	row := strings.Split(got, "\n")[2] // title, weekday names, then the first week's day numbers
	if !strings.HasPrefix(row, "30        Dec 1     2         3 ") {
		t.Errorf("first week's labels = %q", row)
	}
}
