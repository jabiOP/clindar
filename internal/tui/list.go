package tui

import (
	"strings"
	"time"

	"github.com/jabiOP/clindar/internal/model"
)

const listDays = 14

// listView renders the agenda list. entries is the flat, selectable list of
// events (an event spanning days appears once per day, as in GroupByDay).
type listView struct {
	days          []model.Day
	cursor        int
	calendars     map[string]model.Calendar
	width, height int
	loc           *time.Location
	today         time.Time // local midnight: earlier day headers are dimmed
}

func listEntries(days []model.Day) []model.Event {
	var out []model.Event
	for _, d := range days {
		out = append(out, d.Events...)
	}
	return out
}

func renderList(v listView) (string, hitMap) {
	type row struct {
		text  string
		entry int // -1 for day headers
	}
	var rows []row
	n := 0
	for _, d := range v.days {
		if len(d.Events) == 0 {
			continue
		}
		header := headerStyle
		if d.Date.Before(v.today) {
			header = dimStyle // past days are dimmed, like Google
		}
		rows = append(rows, row{header.Render(d.Date.Format("Mon 02 Jan")), -1})
		for _, e := range d.Events {
			rows = append(rows, row{v.line(e, n == v.cursor), n})
			n++
		}
	}
	if len(rows) == 0 {
		return dimStyle.Render("No events in the next two weeks. Press a to add one."), nil
	}
	// Scroll so the cursor's row is visible.
	cursorRow := 0
	for i, r := range rows {
		if r.entry == v.cursor {
			cursorRow = i
		}
	}
	top := max(0, cursorRow-v.height+2)
	var b strings.Builder
	var hits hitMap
	for i := top; i < len(rows) && i < top+v.height; i++ {
		if rows[i].entry >= 0 {
			hits.add(0, i-top, v.width, 1, target{kind: targetEntry, entry: rows[i].entry})
		}
		b.WriteString(rows[i].text + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n"), hits
}

// line renders one event row. The selected row gets a visible ▶ marker as well
// as reverse video (works without color, and in golden files). It is built from
// plain text and styled once: fitting an already-colored string would count the
// escape codes as width and cut off the calendar label.
func (v listView) line(e model.Event, selected bool) string {
	when := "all day    "
	if !e.AllDay {
		s, end := e.Span(v.loc)
		when = s.Format("15:04") + "–" + end.Format("15:04")
	}
	cal := v.calendars[e.CalendarID]
	resp := ""
	if self := e.SelfAttendee(); self != nil && self.Response != model.ResponseAccepted {
		resp = " [" + string(self.Response) + "]"
	}
	marker := "  "
	if selected {
		marker = "▶ "
	}
	label := "● " + cal.Name
	text := fit(marker+when+"  "+strings.Join(strings.Fields(e.Title), " ")+resp,
		max(v.width-width(cal.Name)-4, 10))
	if selected {
		return selectedStyle.Render(text + "  " + label)
	}
	return text + "  " + calendarStyle(cal.Color).Render(label)
}
