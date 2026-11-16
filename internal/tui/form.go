package tui

import (
	"errors"
	"maps"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/timeparse"
)

// Form field indexes.
const (
	fTitle = iota
	fDate
	fAllDay
	fStart
	fEnd
	fDuration
	fCalendar
	fRepeat
	fColor
	fWhere
	fNotes
	fInvite
	fieldCount
)

var fieldLabels = [fieldCount]string{"Title", "Date", "All-day", "Start", "End", "Duration", "Calendar", "Repeat", "Color", "Where", "Notes", "Invite"}

// colorNames are Google's event colors, in Google's menu order.
var colorNames = []struct{ id, name string }{
	{"11", "Tomato"}, {"4", "Flamingo"}, {"6", "Tangerine"}, {"5", "Banana"}, {"2", "Sage"}, {"10", "Basil"},
	{"7", "Peacock"}, {"9", "Blueberry"}, {"1", "Lavender"}, {"3", "Grape"}, {"8", "Graphite"},
}

// repeatWords are the Repeat picker's choices, the same words as --repeat.
var repeatWords = []string{"none", "daily", "weekdays", "weekly", "monthly", "yearly"}

// form edits a new or existing event. It knows nothing about the service:
// it produces a Draft or Changes and the Model decides what to do with them.
type form struct {
	inputs  [fieldCount]textinput.Model // text fields (picker fields' inputs are unused)
	pickers map[int]picker              // picker fields: fAllDay, fCalendar, fRepeat; copied on write
	locked  map[int]string              // pickers that can't change on edit, with the reason shown
	initial [fieldCount]string          // to detect what an edit changed (pickers: the chosen value)
	focus   int
	editing *model.Event // nil when creating
	errs    map[int]string
	err     string // an error that belongs to no field
	dates   timeparse.Parser
}

// fieldError is an error that belongs to one field.
type fieldError struct {
	field int
	msg   string
}

func (e fieldError) Error() string { return e.msg }

func newForm(editing *model.Event, dates timeparse.Parser) form {
	f := form{editing: editing, dates: dates, pickers: map[int]picker{}, locked: map[int]string{}}
	for i := range f.inputs {
		in := textinput.New()
		in.Prompt = ""
		in.Cursor.SetMode(cursor.CursorStatic) // no blink timers: simpler, and tests stay synchronous
		f.inputs[i] = in
	}
	f.inputs[fTitle].Placeholder = "What"
	f.inputs[fDate].Placeholder = "20/11, fri, or 20/11-23/11 (all-day)"
	f.inputs[fStart].Placeholder = "15:30, 3pm (empty = all day)"
	f.inputs[fEnd].Placeholder = "16:30 (same day; earlier = next day), or 21/11 02:00"
	f.inputs[fDuration].Placeholder = "45m, 1h30m (instead of End)"
	f.inputs[fInvite].Placeholder = "emails, comma-separated"
	f.pickers[fAllDay] = picker{title: "All-day", options: []option{{label: "yes", value: "yes"}, {label: "no", value: "no"}}}
	var rep []option
	for _, w := range repeatWords {
		rep = append(rep, option{label: w, value: w})
	}
	f.pickers[fRepeat] = picker{title: "Repeat", options: rep}
	colors := []option{{label: "Calendar default", value: ""}}
	for _, c := range colorNames {
		colors = append(colors, option{label: c.name, value: c.id})
	}
	f.pickers[fColor] = picker{title: "Color", options: colors}
	f.inputs[fTitle].Focus()
	return f
}

// dateLayout writes a date in the configured order, so the form shows what it reads.
func (f form) dateLayout() string {
	if f.dates.Order == timeparse.MDY {
		return "01/02/2006"
	}
	return "02/01/2006"
}

// newEventForm is the add form: all-day on day, in the default calendar (by
// name or ID, else the primary), choosing among writable calendars only.
func newEventForm(day time.Time, cals []model.Calendar, defaultCal string, dates timeparse.Parser) form {
	f := newForm(nil, dates)
	var opts []option
	chosen := -1
	for _, c := range cals {
		if !c.Writable() {
			continue
		}
		if chosen < 0 && (c.ID == defaultCal || (defaultCal != "" && strings.EqualFold(c.Name, defaultCal))) {
			chosen = len(opts)
		}
		opts = append(opts, option{label: c.Name, value: c.ID, swatch: c.Color})
	}
	if chosen < 0 {
		chosen = 0
		for i, c := range cals {
			if c.Primary && c.Writable() {
				chosen = indexOfCalendar(opts, cals[i].ID)
			}
		}
	}
	if len(opts) == 0 {
		opts = []option{{label: "(no writable calendar)"}}
	}
	f.pickers[fCalendar] = picker{title: "Calendar", options: opts, chosen: max(chosen, 0)}
	f = f.setText(fDate, day.Format(f.dateLayout()))
	return f.remember()
}

func indexOfCalendar(opts []option, id string) int {
	for i, o := range opts {
		if o.value == id {
			return i
		}
	}
	return 0
}

// editEventForm pre-fills a form from an existing event. Calendar and Repeat
// can't change on an existing event in v1, so they're locked.
func editEventForm(e model.Event, loc *time.Location, cals []model.Calendar, dates timeparse.Parser) form {
	f := newForm(&e, dates)
	calName := e.CalendarID
	for _, c := range cals {
		if c.ID == e.CalendarID {
			calName = c.Name
		}
	}
	f.pickers[fCalendar] = picker{title: "Calendar", options: []option{{label: calName, value: e.CalendarID}}}
	repeat := "none"
	if e.IsInstance() || e.IsMaster() {
		repeat = "repeats"
	}
	f.pickers[fRepeat] = picker{title: "Repeat", options: []option{{label: repeat, value: repeat}}}
	f.locked[fCalendar], f.locked[fRepeat] = "can't change", "can't change"

	start, end := e.Span(loc)
	layout := f.dateLayout()
	f = f.setText(fTitle, e.Title).setText(fWhere, e.Location).setText(fNotes, e.Description).setChoice(fColor, e.ColorID)
	if e.AllDay {
		f = f.setChoice(fAllDay, "yes")
		date := start.Format(layout)
		if last := end.AddDate(0, 0, -1); last.After(start) {
			date += "-" + last.Format(layout)
		}
		f = f.setText(fDate, date)
	} else {
		f = f.setChoice(fAllDay, "no").setText(fDate, start.Format(layout)).setText(fStart, start.Format("15:04"))
		// A bare time means "on the start's day, or the next day if it's not after
		// the start" (span reads it that way), so only longer spans need a date.
		clock := func(t time.Time) int { return t.Hour()*60 + t.Minute() }
		sameDay := model.StartOfDay(end).Equal(model.StartOfDay(start))
		nextDay := model.StartOfDay(end).Equal(model.StartOfDay(start).AddDate(0, 0, 1)) && clock(end) <= clock(start)
		endText := end.Format(layout + " 15:04")
		if sameDay || nextDay {
			endText = end.Format("15:04")
		}
		f = f.setText(fEnd, endText)
	}
	var emails []string
	for _, a := range e.Attendees {
		if !a.Self && !a.Organizer {
			emails = append(emails, a.Email)
		}
	}
	return f.setText(fInvite, strings.Join(emails, ", ")).remember()
}

// remember records the current values as the starting point, so an edit can
// tell what changed.
func (f form) remember() form {
	for i := range f.initial {
		f.initial[i] = f.value(i)
	}
	return f
}

func (f form) setText(i int, v string) form {
	f.inputs[i].SetValue(v)
	return f
}

// setChoice chooses the option with value v in picker i (if it has one).
func (f form) setChoice(i int, v string) form {
	p := f.pickers[i]
	for n, o := range p.options {
		if o.value == v {
			p.chosen = n
		}
	}
	return f.withPicker(i, p)
}

// withPicker stores p as field i's picker, copying the map first: a form is a
// value, and copies of it must not share picker state.
func (f form) withPicker(i int, p picker) form {
	f.pickers = maps.Clone(f.pickers)
	f.pickers[i] = p
	return f
}

func (f form) isPicker(i int) bool { _, ok := f.pickers[i]; return ok }

// pickerOpen reports whether the focused field is a picker with its list open.
func (f form) pickerOpen() bool { p, ok := f.pickers[f.focus]; return ok && p.open }

// pickerKeys are the keys a closed, focused picker takes; the rest navigate.
var pickerKeys = map[string]bool{"h": true, "l": true, "left": true, "right": true, "ctrl+n": true, "ctrl+p": true, " ": true, "enter": true}

func (f form) Update(msg tea.Msg) (form, tea.Cmd) {
	k, isKey := msg.(tea.KeyMsg)
	if isKey {
		f.errs, f.err = nil, ""
	}
	if p, ok := f.pickers[f.focus]; ok && isKey {
		if _, locked := f.locked[f.focus]; !locked && (p.open || pickerKeys[k.String()]) {
			np, _, _ := p.Update(k)
			return f.withPicker(f.focus, np), nil
		}
	}
	if isKey {
		switch k.String() {
		case "tab", "down", "ctrl+j":
			return f.move(1), nil
		case "shift+tab", "up", "ctrl+k":
			return f.move(-1), nil
		}
	}
	if f.isPicker(f.focus) {
		return f, nil
	}
	wasEmpty := f.value(fStart) == ""
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	if f.focus == fStart && wasEmpty && f.value(fStart) != "" {
		f = f.setChoice(fAllDay, "no") // typing a time makes it a timed event
	}
	return f, cmd
}

func (f form) move(delta int) form {
	f.inputs[f.focus].Blur()
	f.focus = (f.focus + delta + fieldCount) % fieldCount
	f.inputs[f.focus].Focus()
	return f
}

// openPicker opens the focused field's list (a mouse click on a picker field).
func (f form) openPicker() form {
	p, ok := f.pickers[f.focus]
	if _, locked := f.locked[f.focus]; !ok || locked {
		return f
	}
	np, _, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	return f.withPicker(f.focus, np)
}

// pickOption chooses option i in the focused picker (a mouse click on it).
func (f form) pickOption(i int) form {
	p, ok := f.pickers[f.focus]
	if !ok {
		return f
	}
	np, _, _ := p.pick(i)
	return f.withPicker(f.focus, np)
}

func (f form) value(i int) string {
	if p, ok := f.pickers[i]; ok {
		return p.current().value
	}
	return strings.TrimSpace(f.inputs[i].Value())
}

func (f form) changed(i int) bool { return f.value(i) != f.initial[i] }

// span reads Date, All-day, Start, End and Duration. A fieldError says which field is wrong.
func (f form) span(now time.Time) (timeparse.Span, error) {
	dateText := f.value(fDate)
	from, to, isRange := f.dates.ParseDateRange(dateText, now)
	if !isRange {
		d, ok := f.dates.ParseDate(dateText, now)
		if !ok {
			return timeparse.Span{}, fieldError{fDate, "not a date (try 20/11, fri, 20/11-23/11)"}
		}
		from, to = d, d
	}
	if f.value(fAllDay) == "yes" {
		return timeparse.Span{Start: from, End: to.AddDate(0, 0, 1), AllDay: true}, nil
	}
	if isRange {
		return timeparse.Span{}, fieldError{fDate, "a date range is all-day: set All-day to yes, or give one date"}
	}
	h, mi, ok := timeparse.ParseClock(f.value(fStart))
	if !ok {
		return timeparse.Span{}, fieldError{fStart, "not a time (try 15:30 or 3pm), or set All-day to yes"}
	}
	start := time.Date(from.Year(), from.Month(), from.Day(), h, mi, 0, 0, from.Location())
	end, dur := f.value(fEnd), f.value(fDuration)
	switch {
	case end != "" && dur != "":
		return timeparse.Span{}, fieldError{fDuration, "give an end or a duration, not both"}
	case dur != "":
		d, err := timeparse.ParseDuration(dur)
		if err != nil {
			return timeparse.Span{}, fieldError{fDuration, err.Error()}
		}
		return timeparse.Span{Start: start, End: start.Add(d)}, nil
	case end != "":
		e, _, err := f.dates.ParseTime(end, start) // a bare time is on the start's day
		if err != nil {
			return timeparse.Span{}, fieldError{fEnd, "not a time (try 16:30, or 21/11 02:00)"}
		}
		if !e.After(start) {
			e = e.AddDate(0, 0, 1) // 22:00 → 01:00 ends the next day
		}
		return timeparse.Span{Start: start, End: e}, nil
	}
	return timeparse.Span{Start: start, End: start.Add(time.Hour)}, nil
}

// draft validates the form as a new event.
func (f form) draft(now time.Time) (service.Draft, error) {
	span, err := f.span(now)
	if err != nil {
		return service.Draft{}, err
	}
	var rec []string
	if w := f.value(fRepeat); w != "none" {
		if rec, err = recur.ParseRepeat(w, span.Start, span.AllDay, time.Time{}, 0); err != nil {
			return service.Draft{}, fieldError{fRepeat, err.Error()}
		}
	}
	return service.Draft{
		CalendarID: f.value(fCalendar), Title: f.value(fTitle), Location: f.value(fWhere),
		Description: f.value(fNotes), Start: span.Start, End: span.End, AllDay: span.AllDay,
		Recurrence: rec, Invite: splitEmails(f.value(fInvite)), ColorID: f.value(fColor),
	}, nil
}

// changes returns only what the user changed in an edit form.
func (f form) changes(now time.Time) (service.Changes, error) {
	var ch service.Changes
	str := func(i int) *string { v := f.value(i); return &v }
	if f.changed(fTitle) {
		ch.Title = str(fTitle)
	}
	if f.changed(fWhere) {
		ch.Location = str(fWhere)
	}
	if f.changed(fNotes) {
		ch.Description = str(fNotes)
	}
	if f.changed(fInvite) {
		emails := splitEmails(f.value(fInvite))
		ch.Invite = &emails
	}
	if f.changed(fColor) {
		ch.ColorID = str(fColor)
	}
	if f.changed(fDate) || f.changed(fAllDay) || f.changed(fStart) || f.changed(fEnd) || f.changed(fDuration) {
		g := f
		if g.changed(fDuration) && !g.changed(fEnd) {
			g = g.setText(fEnd, "") // a new duration replaces the pre-filled end
		}
		span, err := g.span(now)
		if err != nil {
			return ch, err
		}
		ch.Start, ch.End, ch.AllDay = &span.Start, &span.End, &span.AllDay
	}
	return ch, nil
}

func splitEmails(s string) []string {
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// view draws the form as a pop-up. Each field line is clickable (it focuses
// that field, and opens a picker's list); with the mouse on, [Save] and
// [Cancel] are buttons. Errors show under their field.
func (f form) view(width int, mouse bool) modal {
	title := "New event"
	if f.editing != nil {
		title = "Edit event"
	}
	var md modal
	md.text(headerStyle.Render(title))
	md.text("")
	for i, in := range f.inputs {
		label := fit(fieldLabels[i], 10)
		if i == f.focus {
			label = selectedStyle.Render(label)
		}
		var value string
		p, isPicker := f.pickers[i]
		switch {
		case isPicker && f.locked[i] != "":
			value = dimStyle.Render("‹ " + p.current().label + " › (" + f.locked[i] + ")")
		case isPicker:
			value = p.inline()
		default:
			in.Width = max(width-16, 20)
			value = in.View()
		}
		md.field(label+"  "+value, i)
		if msg := f.errs[i]; msg != "" {
			md.text(strings.Repeat(" ", 12) + errorStyle.Render(msg))
		}
		if isPicker && p.open && i == f.focus { // the dropdown, inline and clickable
			lines, idx := p.rows(width - 12)
			for n, line := range lines {
				md.option(strings.Repeat(" ", 12)+line, idx[n])
			}
		}
	}
	if f.err != "" {
		md.text("")
		md.text(errorStyle.Render(f.err))
	}
	md.text("")
	if mouse {
		md.buttons(button{"Save", "enter"}, button{"Cancel", "esc"})
	}
	// Two lines: one would be wider than an 80-column terminal.
	md.text(dimStyle.Render("ctrl+j/tab next · ctrl+k previous · h/l change"))
	md.text(dimStyle.Render("space list · enter save · esc cancel"))
	return md
}

// focusField moves the cursor to field i (a mouse click on it).
func (f form) focusField(i int) form {
	return f.move(i - f.focus)
}

// showError puts a fieldError under its field and moves the cursor there;
// any other error goes on a line of its own.
func (f form) showError(err error) form {
	var fe fieldError
	if errors.As(err, &fe) {
		f.errs = map[int]string{fe.field: fe.msg}
		return f.focusField(fe.field)
	}
	f.err = err.Error()
	return f
}

// withPalette gives the Color picker its swatches (Google's event palette).
func (f form) withPalette(palette map[string]string) form {
	p := f.pickers[fColor]
	p.options = append([]option(nil), p.options...)
	for i := range p.options {
		p.options[i].swatch = palette[p.options[i].value]
	}
	return f.withPicker(fColor, p)
}
