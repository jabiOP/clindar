// Package output renders events for the CLI: pretty (humans), TSV and JSON
// Lines (scripts), or a user-supplied Go template.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/jabiOP/clindar/internal/model"
)

type Mode int

const (
	Pretty Mode = iota
	TSV
	JSON
	Template
)

// ChooseMode applies the precedence: explicit flag, else pretty on a terminal, TSV in a pipe.
func ChooseMode(jsonFlag, tsvFlag bool, format string, isTTY bool) (Mode, error) {
	n := 0
	for _, set := range []bool{jsonFlag, tsvFlag, format != ""} {
		if set {
			n++
		}
	}
	switch {
	case n > 1:
		return 0, fmt.Errorf("use only one of --json, --tsv, --format")
	case jsonFlag:
		return JSON, nil
	case tsvFlag:
		return TSV, nil
	case format != "":
		return Template, nil
	case isTTY:
		return Pretty, nil
	default:
		return TSV, nil
	}
}

// EventJSON is the stable, documented shape of an event for scripts.
// Never change a field's meaning; only add fields.
type EventJSON struct {
	ID              string         `json:"id"`
	SeriesID        string         `json:"series_id,omitempty"`
	CalendarID      string         `json:"calendar_id"`
	Calendar        string         `json:"calendar"`
	Title           string         `json:"title"`
	Start           string         `json:"start"` // RFC 3339 local time, or YYYY-MM-DD if all_day
	End             string         `json:"end"`   // exclusive
	AllDay          bool           `json:"all_day"`
	Status          string         `json:"status"`
	Response        string         `json:"response,omitempty"` // your RSVP, if invited
	Location        string         `json:"location,omitempty"`
	Description     string         `json:"description,omitempty"`
	Attendees       []AttendeeJSON `json:"attendees,omitempty"`
	Recurring       bool           `json:"recurring"`
	Link            string         `json:"link,omitempty"`
	StartsInMinutes *int           `json:"starts_in_minutes,omitempty"` // only from `clindar next`
	Color           string         `json:"color,omitempty"`             // effective color: the event's own, else its calendar's ("#rrggbb")
}

type AttendeeJSON struct {
	Email    string `json:"email"`
	Name     string `json:"name,omitempty"`
	Response string `json:"response"`
}

type Printer struct {
	w             io.Writer
	mode          Mode
	tmpl          *template.Template
	loc           *time.Location
	calendars     map[string]model.Calendar
	width         int               // pretty details wrap to this many cells; 0 = don't wrap
	palette       map[string]string // event colorId → "#rrggbb"
	omitInternals bool              // pretty details: no ID or Link rows (the TUI)
}

// SetWidth makes pretty details wrap to w cells: each "Label: value" row wraps its
// value under itself. 0 (the default, used by the CLI) means don't wrap.
func (p *Printer) SetWidth(w int) { p.width = w }

// OmitInternals drops the ID and raw Link rows from pretty details: the TUI
// shows a button to open the event instead. clindar show keeps them (scripts use the ID).
func (p *Printer) OmitInternals() { p.omitInternals = true }

// SetPalette gives the printer Google's event palette, so JSON can report each
// event's effective color.
func (p *Printer) SetPalette(palette map[string]string) { p.palette = palette }

const labelColumn = 11 // "%-10s ": a 10-cell label plus one space

// wrap splits s into lines of at most w cells (ANSI-aware). It returns s
// unchanged when wrapping is off or s already fits.
func (p *Printer) wrap(s string, w int) []string {
	if p.width <= 0 || w <= 0 || lipgloss.Width(s) <= w {
		return []string{s}
	}
	lines := strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ") // Render pads every line to w
	}
	return lines
}

func NewPrinter(w io.Writer, mode Mode, format string, loc *time.Location, cals []model.Calendar) (*Printer, error) {
	p := &Printer{w: w, mode: mode, loc: loc, calendars: map[string]model.Calendar{}}
	for _, c := range cals {
		p.calendars[c.ID] = c
	}
	if mode == Template {
		t, err := template.New("format").Funcs(template.FuncMap{"time": formatTime}).Parse(format)
		if err != nil {
			return nil, fmt.Errorf("--format: %w", err)
		}
		p.tmpl = t
	}
	return p, nil
}

// formatTime is the template function {{time "15:04" .Start}}.
func formatTime(layout, value string) (string, error) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		if t, err = time.Parse(time.DateOnly, value); err != nil {
			return "", err
		}
	}
	return t.Format(layout), nil
}

func (p *Printer) toJSON(e model.Event) EventJSON {
	j := EventJSON{
		ID: e.ID, SeriesID: e.SeriesID, CalendarID: e.CalendarID, Calendar: p.calendars[e.CalendarID].Name,
		Title: e.Title, AllDay: e.AllDay, Status: e.Status, Location: e.Location, Description: e.Description,
		Recurring: e.IsInstance() || e.IsMaster(), Link: e.Link,
		Color: model.EffectiveColor(e, p.calendars[e.CalendarID], p.palette),
	}
	if e.AllDay {
		j.Start, j.End = e.Start.UTC().Format(time.DateOnly), e.End.UTC().Format(time.DateOnly)
	} else {
		j.Start, j.End = e.Start.In(p.loc).Format(time.RFC3339), e.End.In(p.loc).Format(time.RFC3339)
	}
	if self := e.SelfAttendee(); self != nil {
		j.Response = string(self.Response)
	}
	for _, a := range e.Attendees {
		j.Attendees = append(j.Attendees, AttendeeJSON{Email: a.Email, Name: a.Name, Response: string(a.Response)})
	}
	return j
}

// Events prints an agenda. Pretty mode groups by day across [from, from+days);
// the other modes print one line per event.
func (p *Printer) Events(events []model.Event, from time.Time, days int) error {
	if p.mode == Pretty {
		return p.prettyAgenda(events, from.In(p.loc), days)
	}
	for _, e := range events {
		if err := p.line(p.toJSON(e)); err != nil {
			return err
		}
	}
	return nil
}

// Event prints one event in detail (pretty) or as one line.
func (p *Printer) Event(e model.Event) error {
	if p.mode == Pretty {
		return p.prettyDetail(e)
	}
	return p.line(p.toJSON(e))
}

// Next prints the next event with minutes until it starts (negative = started).
func (p *Printer) Next(e model.Event, now time.Time) error {
	mins := int(e.Start.Sub(now).Round(time.Minute).Minutes())
	if p.mode == Pretty {
		_, err := fmt.Fprintf(p.w, "%s  %s  %s\n", relative(mins), p.timeRange(e), e.Title)
		return err
	}
	j := p.toJSON(e)
	j.StartsInMinutes = &mins
	return p.line(j)
}

func relative(mins int) string {
	switch {
	case mins <= 0:
		return "now"
	case mins < 60:
		return fmt.Sprintf("in %dm", mins)
	default:
		return fmt.Sprintf("in %dh%02dm", mins/60, mins%60)
	}
}

func (p *Printer) line(j EventJSON) error {
	switch p.mode {
	case JSON:
		b, err := json.Marshal(j)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(p.w, "%s\n", b)
		return err
	case Template:
		if err := p.tmpl.Execute(p.w, j); err != nil {
			return err
		}
		_, err := io.WriteString(p.w, "\n")
		return err
	default: // TSV: id, start, end, all_day, calendar, title
		allDay := "0"
		if j.AllDay {
			allDay = "1"
		}
		_, err := fmt.Fprintln(p.w, strings.Join([]string{
			j.ID, j.Start, j.End, allDay, clean(j.Calendar), clean(j.Title)}, "\t"))
		return err
	}
}

// clean keeps one event on one line with no stray tabs.
func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

var (
	dayStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle  = lipgloss.NewStyle().Faint(true)
	timeWidth = utf8.RuneCountInString("00:00–00:00") // not len(): "–" is 3 bytes
)

func (p *Printer) timeRange(e model.Event) string {
	if e.AllDay {
		return fmt.Sprintf("%-*s", timeWidth, "all day")
	}
	s, end := e.Span(p.loc)
	return s.Format("15:04") + "–" + end.Format("15:04")
}

func (p *Printer) prettyAgenda(events []model.Event, from time.Time, days int) error {
	empty := true
	for _, day := range model.GroupByDay(events, from, days) {
		if len(day.Events) == 0 {
			continue
		}
		empty = false
		fmt.Fprintln(p.w, dayStyle.Render(day.Date.Format("Mon 02 Jan")))
		for _, e := range day.Events {
			cal := p.calendars[e.CalendarID]
			fmt.Fprintf(p.w, "  %s  %s  %s\n", p.timeRange(e), clean(e.Title),
				dimStyle.Render(lipgloss.NewStyle().Foreground(lipgloss.Color(cal.Color)).Render("●")+" "+cal.Name))
		}
	}
	if empty {
		_, err := fmt.Fprintln(p.w, dimStyle.Render("No events."))
		return err
	}
	return nil
}

func (p *Printer) prettyDetail(e model.Event) error {
	row := func(label, value string) {
		if value == "" {
			return
		}
		lines := p.wrap(value, p.width-labelColumn)
		fmt.Fprintf(p.w, "%-10s %s\n", label+":", lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(p.w, "%s%s\n", strings.Repeat(" ", labelColumn), l)
		}
	}
	for _, l := range p.wrap(dayStyle.Render(e.Title), p.width) {
		fmt.Fprintln(p.w, l)
	}
	row("When", p.when(e))
	row("Calendar", p.calendars[e.CalendarID].Name)
	row("Where", e.Location)
	if e.IsInstance() || e.IsMaster() {
		row("Repeats", "yes") // shared by the CLI and the TUI, so no command-line hint here
	}
	var people []string
	for _, a := range e.Attendees {
		people = append(people, fmt.Sprintf("%s (%s)", a.Email, a.Response))
	}
	row("Guests", strings.Join(people, ", "))
	if !p.omitInternals {
		row("ID", e.ID)
		row("Link", e.Link)
	}
	if e.Description != "" {
		fmt.Fprintln(p.w)
		for _, l := range p.wrap(e.Description, p.width) {
			fmt.Fprintln(p.w, l)
		}
	}
	return nil
}

// when describes an event's whole span: one date for a one-day event, first and
// last date for a longer one (all-day ends are exclusive, so the last day is
// the day before End).
func (p *Printer) when(e model.Event) string {
	const day = "Mon 02 Jan 2006"
	start, end := e.Span(p.loc)
	if e.AllDay {
		last := end.AddDate(0, 0, -1)
		if !last.After(start) {
			return start.Format(day) + "  all day"
		}
		return start.Format(day) + " – " + last.Format(day) + "  all day"
	}
	if end.After(start) && !model.StartOfDay(end.Add(-time.Nanosecond)).Equal(model.StartOfDay(start)) {
		return start.Format(day+" 15:04") + " – " + end.Format(day+" 15:04")
	}
	return start.Format(day) + "  " + p.timeRange(e)
}
