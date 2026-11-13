package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/jabiOP/clindar/internal/model"
)

const gridDays = 28

// gridView is everything renderGrid needs. Keeping rendering a pure function
// of plain data makes it easy to golden-test.
type gridView struct {
	start           time.Time   // first cell (local midnight, a week start)
	today, selected time.Time   // local midnights
	days            []model.Day // gridDays days from start
	calendars       map[string]model.Calendar
	width, height   int // space available for the grid, including its header
	loc             *time.Location
	handles         bool      // mouse on: draw ◂ ▸ resize handles on event lines
	drop            time.Time // while dragging: the day under the pointer (zero = none)
}

func renderGrid(g gridView) (string, hitMap) {
	colW := max(g.width/7, 8)
	cellH := max((g.height-2)/4, 2) // 2 header lines
	var b strings.Builder
	b.WriteString(headerStyle.Render(g.selected.Format("January 2006")) + "\n")
	var names []string
	for i := range 7 {
		names = append(names, fit(g.start.AddDate(0, 0, i).Format("Mon"), colW))
	}
	b.WriteString(dimStyle.Render(strings.Join(names, "")) + "\n")
	var hits hitMap
	for week := range 4 {
		var cells []string
		for dow := range 7 {
			cell, cellHits := g.cell(g.days[week*7+dow], colW, cellH)
			cells = append(cells, cell)
			hits = append(hits, cellHits.offset(dow*colW, 2+week*cellH)...)
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cells...) + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n"), hits
}

// cell renders one day: a label line, then as many events as fit. Hits are
// relative to the cell's top-left corner.
func (g gridView) cell(day model.Day, w, h int) (string, hitMap) {
	var hits hitMap
	hits.add(0, 0, w, h, target{kind: targetDay, day: day.Date})
	label := fmt.Sprintf("%2d", day.Date.Day())
	if day.Date.Day() == 1 {
		label = day.Date.Format("Jan 2")
	}
	switch {
	case day.Date.Equal(g.drop):
		label = selectedStyle.Render(fit("»"+label, w-1)) // drop target while dragging
	case day.Date.Equal(g.selected):
		label = selectedStyle.Render(fit("▶"+label, w-1))
	case day.Date.Equal(g.today):
		label = todayStyle.Render(fit(label+" today", w-1))
	default:
		label = fit(label, w-1)
	}
	lines := []string{label}
	room := h - 1
	for i, e := range day.Events {
		if i == room-1 && len(day.Events) > room {
			hits.add(0, len(lines), w, 1, target{kind: targetMore, day: day.Date})
			lines = append(lines, dimStyle.Render(fit(fmt.Sprintf("+%d more", len(day.Events)-i), w-1)))
			break
		}
		if i == room {
			break
		}
		line, lineHits := g.eventLine(e, day.Date, w-1)
		hits = append(hits, lineHits.offset(0, len(lines))...)
		lines = append(lines, line)
	}
	for len(lines) < h {
		lines = append(lines, fit("", w-1))
	}
	// One space of gutter on the right of every cell.
	for i := range lines {
		lines[i] += " "
	}
	return strings.Join(lines, "\n"), hits
}

// eventLine renders one event in a cell w cells wide. With handles on, the
// event's first day gets ◂ at the start and its last day gets ▸ at the end.
func (g gridView) eventLine(e model.Event, day time.Time, w int) (string, hitMap) {
	var hits hitMap
	hits.add(0, 0, w, 1, target{kind: targetEvent, day: day, event: e, part: partBody})
	start, end := "", ""
	if g.handles {
		first, last := eventDays(e, g.loc)
		if day.Equal(first) {
			start = "◂"
			hits.add(0, 0, 1, 1, target{kind: targetEvent, day: day, event: e, part: partStart})
		}
		if day.Equal(last) {
			end = "▸"
			hits.add(w-1, 0, 1, 1, target{kind: targetEvent, day: day, event: e, part: partEnd})
		}
	}
	text := start + fit(eventLabel(e, g.loc), w-runewidth.StringWidth(start+end)) + end
	return calendarStyle(g.calendars[e.CalendarID].Color).Render(text), hits
}

// eventDays returns the first and last local day an event touches. An end at
// exactly midnight doesn't touch that day (end times are exclusive).
func eventDays(e model.Event, loc *time.Location) (first, last time.Time) {
	s, end := e.Span(loc)
	first, last = model.StartOfDay(s), model.StartOfDay(s)
	if end.After(s) {
		last = model.StartOfDay(end.Add(-time.Nanosecond))
	}
	return first, last
}

// eventLabel is "09:00 Title" for timed events and just the title for all-day.
func eventLabel(e model.Event, loc *time.Location) string {
	title := strings.Join(strings.Fields(e.Title), " ")
	if e.AllDay {
		return title
	}
	start, _ := e.Span(loc)
	return start.Format("15:04") + " " + title
}

// weekStart returns the first day of t's week.
func weekStart(t time.Time, first time.Weekday) time.Time {
	d := model.StartOfDay(t)
	back := (int(d.Weekday()) - int(first) + 7) % 7
	return d.AddDate(0, 0, -back)
}
