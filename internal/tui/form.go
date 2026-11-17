package tui

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
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
	inputs         [fieldCount]textinput.Model // text fields (picker fields' inputs are unused)
	pickers        map[int]picker              // picker fields: fAllDay, fCalendar, fRepeat; copied on write
	locked         map[int]string              // pickers that can't change on edit, with the reason shown
	initial        [fieldCount]string          // to detect what an edit changed (pickers: the chosen value)
	focus          int
	editing        *model.Event // nil when creating
	errs           map[int]string
	err            string // an error that belongs to no field
	dates          timeparse.Parser
	confirmDiscard bool              // Esc with unsaved changes asked "Discard changes? (y/n)"
	cals           []model.Calendar  // for the Color picker: the chosen calendar's labels
	palette        map[string]string // Google's classic event colors (colorId → "#rrggbb")
}

// labelPrefix marks a Color option that is a label, not a classic colorId.
const labelPrefix = "label:"

// colorOptions are the Color picker's choices for calendar cal: its labels
// (Google's color menu: 24 defaults and any custom colors), or the 11 classic
// colors for a calendar without labels.
func colorOptions(cal model.Calendar, palette map[string]string) []option {
	opts := []option{{label: "Calendar default", value: ""}}
	for _, l := range cal.Labels {
		opts = append(opts, option{label: l.DisplayName(), value: labelPrefix + l.ID, swatch: l.Color})
	}
	if len(cal.Labels) == 0 {
		for _, c := range colorNames {
			opts = append(opts, option{label: c.name, value: c.id, swatch: palette[c.id]})
		}
	}
	for i := range opts[1:] { // "Calendar default" keeps group 0, on its own row
		opts[i+1].group = 1 + colorFamily(opts[i+1].swatch)
	}
	slices.SortStableFunc(opts[1:], func(a, b option) int {
		if a.group != b.group {
			return a.group - b.group
		}
		return cmp.Compare(lightness(b.swatch), lightness(a.swatch)) // light to dark
	})
	return opts
}

// Color families, in the order the grid shows them.
const (
	famRed = iota
	famOrange
	famYellow
	famGreen
	famTeal
	famBlue
	famPurple
	famPink
	famBrown
	famGrey
)

// colorFamily groups a "#rrggbb" color with similar ones, by hue; washed-out
// colors are browns (warm and dark) or greys. Unknown colors are grey.
func colorFamily(hex string) int {
	h, s, l, ok := hsl(hex)
	switch {
	case !ok || s < 0.15:
		return famGrey
	case s < 0.35 && l < 0.5 && h < 60:
		return famBrown
	case h < 12 || h >= 345:
		return famRed
	case h < 40:
		return famOrange
	case h < 70:
		return famYellow
	case h < 160:
		return famGreen
	case h < 190:
		return famTeal
	case h < 250:
		return famBlue
	case h < 290:
		return famPurple
	}
	return famPink
}

func lightness(hex string) float64 { _, _, l, _ := hsl(hex); return l }

// hsl converts "#rrggbb" to hue (0–360), saturation and lightness (0–1).
func hsl(hex string) (h, s, l float64, ok bool) {
	var r8, g8, b8 int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r8, &g8, &b8); err != nil {
		return 0, 0, 0, false
	}
	r, g, b := float64(r8)/255, float64(g8)/255, float64(b8)/255
	hi, lo := max(r, g, b), min(r, g, b)
	l = (hi + lo) / 2
	d := hi - lo
	if d == 0 {
		return 0, 0, l, true
	}
	s = d / (1 - math.Abs(2*l-1))
	switch hi {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, l, true
}

// colorValue is the Color option that stands for e's color.
func colorValue(e model.Event) string {
	if e.LabelID != "" {
		return labelPrefix + e.LabelID
	}
	return e.ColorID
}

// splitColor turns a Color option back into a colorId or a label ID.
func splitColor(v string) (colorID, labelID string) {
	if id, ok := strings.CutPrefix(v, labelPrefix); ok {
		return "", id
	}
	return v, ""
}

// withColors rebuilds the Color picker for the chosen calendar, keeping the
// current choice when that calendar offers it.
func (f form) withColors() form {
	var cal model.Calendar
	for _, c := range f.cals {
		if c.ID == f.value(fCalendar) {
			cal = c
		}
	}
	cur := f.value(fColor)
	p := picker{title: "Color", options: colorOptions(cal, f.palette), grid: true}
	for i, o := range p.options {
		if o.value == cur {
			p.chosen = i
		}
	}
	f.pickers = maps.Clone(f.pickers)
	f.pickers[fColor] = p
	return f
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
	f.pickers[fColor] = picker{title: "Color", options: colorOptions(model.Calendar{}, nil), grid: true}
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
	opts := writableCalendars(cals)
	chosen := -1
	for i, o := range opts {
		if chosen < 0 && (o.value == defaultCal || (defaultCal != "" && strings.EqualFold(o.label, defaultCal))) {
			chosen = i
		}
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
	f.cals = cals
	f = f.withColors()
	f = f.setText(fDate, day.Format(f.dateLayout()))
	return f.remember()
}

// writableCalendars are the Calendar picker's options: only calendars you can write to.
func writableCalendars(cals []model.Calendar) []option {
	var opts []option
	for _, c := range cals {
		if c.Writable() {
			opts = append(opts, option{label: c.Name, value: c.ID, swatch: c.Color})
		}
	}
	return opts
}

func indexOfCalendar(opts []option, id string) int {
	for i, o := range opts {
		if o.value == id {
			return i
		}
	}
	return 0
}

// editEventForm pre-fills a form from an existing event. Calendar is unlocked
// when the event may move (service.CanMove); Repeat can't change on edit.
func editEventForm(e model.Event, loc *time.Location, cals []model.Calendar, dates timeparse.Parser) form {
	f := newForm(&e, dates)
	from := model.Calendar{ID: e.CalendarID, Name: e.CalendarID}
	for _, c := range cals {
		if c.ID == e.CalendarID {
			from = c
		}
	}
	if err := service.CanMove(e, from); err != nil {
		f.pickers[fCalendar] = picker{title: "Calendar", options: []option{{label: from.Name, value: e.CalendarID}}}
		f.locked[fCalendar] = whyNoMove(err)
	} else {
		opts := writableCalendars(cals)
		f.pickers[fCalendar] = picker{title: "Calendar", options: opts, chosen: indexOfCalendar(opts, e.CalendarID)}
	}
	repeat := "none"
	if e.IsInstance() || e.IsMaster() {
		repeat = "repeats"
	}
	f.pickers[fRepeat] = picker{title: "Repeat", options: []option{{label: repeat, value: repeat}}}
	f.locked[fRepeat] = "can't change"
	f.cals = cals
	f = f.withColors()

	start, end := e.Span(loc)
	layout := f.dateLayout()
	f = f.setText(fTitle, e.Title).setText(fWhere, e.Location).setText(fNotes, e.Description).setChoice(fColor, colorValue(e))
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

// whyNoMove is the short reason shown after a locked Calendar picker.
func whyNoMove(err error) string {
	switch {
	case errors.Is(err, service.ErrInvitation):
		return "can't move: an invitation"
	case errors.Is(err, service.ErrMoveOccurrence):
		return "can't move: one occurrence"
	default:
		return "can't move: read-only"
	}
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
	if i == fCalendar { // each calendar has its own colors
		f = f.withColors()
	}
	return f
}

func (f form) isPicker(i int) bool { _, ok := f.pickers[i]; return ok }

// pickerOpen reports whether the focused field is a picker with its list open.
func (f form) pickerOpen() bool { p, ok := f.pickers[f.focus]; return ok && p.open }

// pickerKeys are the keys a closed, focused picker takes; the rest navigate.
// Enter isn't one of them: in the form, Enter always saves (Space opens a list).
var pickerKeys = map[string]bool{"h": true, "l": true, "left": true, "right": true, "ctrl+n": true, "ctrl+p": true, " ": true}

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
	colorID, labelID := splitColor(f.value(fColor))
	return service.Draft{
		CalendarID: f.value(fCalendar), Title: f.value(fTitle), Location: f.value(fWhere),
		Description: f.value(fNotes), Start: span.Start, End: span.End, AllDay: span.AllDay,
		Recurrence: rec, Invite: splitEmails(f.value(fInvite)), ColorID: colorID, LabelID: labelID,
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
	if f.changed(fColor) { // one replaces the other: a label, a classic color, or neither
		colorID, labelID := splitColor(f.value(fColor))
		ch.ColorID, ch.LabelID = &colorID, &labelID
	}
	if f.changed(fCalendar) {
		ch.CalendarID = str(fCalendar)
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
		if isPicker && p.open && i == f.focus && p.grid { // the color grid, inline and clickable
			lines, cells := p.gridRows(width - 12)
			for n, line := range lines {
				md.optionCells(12, line, cells[n])
			}
		} else if isPicker && p.open && i == f.focus { // the dropdown, inline and clickable
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
		md.buttons(button{"Save", "ctrl+s"}, button{"Cancel", "esc"}) // ctrl+s saves even with a list open
	}
	// Two lines: one would be wider than an 80-column terminal.
	if f.confirmDiscard {
		md.text(errorStyle.Render("Discard changes? enter discard · esc keep editing"))
	} else if p, ok := f.pickers[f.focus]; ok && p.open {
		move := "j/k move"
		if p.grid {
			move = "h/j/k/l move · type to filter"
		}
		md.text(dimStyle.Render("in the list: " + move + " · enter choose · esc close the list"))
		md.text(dimStyle.Render("ctrl+s save"))
	} else {
		md.text(dimStyle.Render("ctrl+j/tab next · ctrl+k previous · h/l change · space list"))
		md.text(dimStyle.Render("enter or ctrl+s save · esc then enter discard"))
	}
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

// withPalette gives the classic colors their swatches (Google's event palette).
func (f form) withPalette(palette map[string]string) form {
	f.palette = palette
	return f.withColors()
}

// chooseHighlighted closes an open list on its highlighted option, as Enter in
// the list would: saving with a list open keeps what the user was pointing at.
func (f form) chooseHighlighted() form {
	if !f.pickerOpen() {
		return f
	}
	np, _, _ := f.pickers[f.focus].Update(tea.KeyMsg{Type: tea.KeyEnter})
	return f.withPicker(f.focus, np)
}

// dirty reports whether anything differs from how the form opened.
func (f form) dirty() bool {
	for i := range fieldCount {
		if f.changed(i) {
			return true
		}
	}
	return false
}
