package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestRenderListScrollsToCursor(t *testing.T) {
	ny := testutil.NY(t)
	from := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
	var events []model.Event
	for d := 9; d < 16; d++ {
		events = append(events, testutil.Timed("e", "Day "+time.Date(2026, 11, d, 0, 0, 0, 0, ny).Format("02"),
			time.Date(2026, 11, d, 9, 0, 0, 0, ny), time.Hour))
	}

	got, _ := renderList(listView{days: model.GroupByDay(events, from, 7), cursor: 6, width: 40, height: 5, loc: ny})

	testutil.Golden(t, "list_scrolled", got)
}

// With colors on, the selected row must keep its calendar label. (Golden files
// run without colors, so they can't catch escape codes being counted as width.)
func TestRenderListSelectedRowKeepsCalendarWithColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	ny := testutil.NY(t)
	from := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
	e := testutil.Timed("e", "Lunch", time.Date(2026, 11, 9, 12, 0, 0, 0, ny), time.Hour)
	cals := map[string]model.Calendar{testutil.Calendar: {ID: testutil.Calendar, Name: "Me", Color: "#4285f4"}}

	got, _ := renderList(listView{days: model.GroupByDay([]model.Event{e}, from, 1), cursor: 0,
		calendars: cals, width: 60, height: 5, loc: ny})

	plain := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(got, "")
	if !strings.Contains(plain, "▶") || !strings.Contains(plain, "● Me") {
		t.Errorf("selected row lost its calendar label:\n%s", plain)
	}
}

func TestListHitMap(t *testing.T) {
	ny := testutil.NY(t)
	from := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
	var events []model.Event
	for d := 9; d < 12; d++ {
		events = append(events, testutil.Timed("e", "Day "+time.Date(2026, 11, d, 0, 0, 0, 0, ny).Format("02"),
			time.Date(2026, 11, d, 9, 0, 0, 0, ny), time.Hour))
	}
	screen, hits := renderList(listView{days: model.GroupByDay(events, from, 3), cursor: 0, width: 40, height: 10, loc: ny})

	x, y := find(t, screen, "Day 11")
	if got, _ := hits.at(x, y); got.kind != targetEntry || got.entry != 2 {
		t.Errorf("click on Day 11 = %+v, want entry 2", got)
	}
	x, y = find(t, screen, "Wed 11 Nov")
	if _, ok := hits.at(x, y); ok {
		t.Error("day headers are not clickable")
	}
}

func TestPastDayHeadersAreFaint(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	ny := testutil.NY(t)
	from := time.Date(2026, 11, 9, 0, 0, 0, 0, ny)
	today := time.Date(2026, 11, 10, 0, 0, 0, 0, ny)
	events := []model.Event{
		testutil.Timed("a", "Past", time.Date(2026, 11, 9, 9, 0, 0, 0, ny), time.Hour),
		testutil.Timed("b", "Today", time.Date(2026, 11, 10, 9, 0, 0, 0, ny), time.Hour),
	}

	got, _ := renderList(listView{days: model.GroupByDay(events, from, 2), cursor: 1, width: 60, height: 10, loc: ny, today: today})

	if !strings.Contains(got, "\x1b[2mMon 09 Nov") {
		t.Errorf("the past day's header should be faint: %q", got)
	}
	if strings.Contains(got, "\x1b[2mTue 10 Nov") {
		t.Errorf("today's header must not be faint: %q", got)
	}
}
