package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

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
}

func renderGrid(g gridView) string {
	colW := max(g.width/7, 8)
	cellH := max((g.height-2)/4, 2) // 2 header lines
	var b strings.Builder
	b.WriteString(headerStyle.Render(g.selected.Format("January 2006")) + "\n")
	var names []string
	for i := range 7 {
		names = append(names, fit(g.start.AddDate(0, 0, i).Format("Mon"), colW))
	}
	b.WriteString(dimStyle.Render(strings.Join(names, "")) + "\n")
	for week := range 4 {
		var cells []string
		for dow := range 7 {
			cells = append(cells, g.cell(g.days[week*7+dow], colW, cellH))
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cells...) + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// cell renders one day: a label line, then as many events as fit.
func (g gridView) cell(day model.Day, w, h int) string {
	label := fmt.Sprintf("%2d", day.Date.Day())
	if day.Date.Day() == 1 {
		label = day.Date.Format("Jan 2")
	}
	switch {
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
			lines = append(lines, dimStyle.Render(fit(fmt.Sprintf("+%d more", len(day.Events)-i), w-1)))
			break
		}
		if i == room {
			break
		}
		lines = append(lines, calendarStyle(g.calendars[e.CalendarID].Color).Render(fit(eventLabel(e, g.loc), w-1)))
	}
	for len(lines) < h {
		lines = append(lines, fit("", w-1))
	}
	// One space of gutter on the right of every cell.
	for i := range lines {
		lines[i] += " "
	}
	return strings.Join(lines, "\n")
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
