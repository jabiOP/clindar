package tui

import (
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
	fStart
	fEnd
	fDuration
	fCalendar
	fWhere
	fNotes
	fRepeat
	fInvite
	fieldCount
)

var fieldLabels = [fieldCount]string{"Title", "Start", "End", "Duration", "Calendar", "Where", "Notes", "Repeat", "Invite"}

// form edits a new or existing event. It knows nothing about the service:
// it produces a Draft or Changes and the Model decides what to do with them.
type form struct {
	inputs  [fieldCount]textinput.Model
	initial [fieldCount]string // to detect which fields an edit changed
	focus   int
	editing *model.Event // nil when creating
	err     string
}

func newForm(values [fieldCount]string, editing *model.Event) form {
	f := form{initial: values, editing: editing}
	for i := range f.inputs {
		in := textinput.New()
		in.Prompt = ""
		in.Cursor.SetMode(cursor.CursorStatic) // no blink timers: simpler, and tests stay synchronous
		in.SetValue(values[i])
		f.inputs[i] = in
	}
	f.inputs[fTitle].Placeholder = "What"
	f.inputs[fStart].Placeholder = "tomorrow 14:00, fri 2pm, 2026-11-20 (date only = all day)"
	f.inputs[fEnd].Placeholder = "15:30 (same day), or last day for all-day"
	f.inputs[fDuration].Placeholder = "45m, 1h30m, 2d"
	f.inputs[fCalendar].Placeholder = "name (empty = primary)"
	f.inputs[fRepeat].Placeholder = "daily, weekdays, weekly, monthly, yearly, FREQ=..."
	f.inputs[fInvite].Placeholder = "emails, comma-separated"
	if editing != nil { // these can't be changed on an existing event in v1
		f.inputs[fCalendar].Placeholder, f.inputs[fRepeat].Placeholder = "(can't change)", "(can't change)"
	}
	f.inputs[fTitle].Focus()
	return f
}

// newEventForm pre-fills a form for adding an event on day, in the user's
// default calendar (the same default the CLI's `add` uses).
func newEventForm(day time.Time, defaultCalendar string) form {
	var v [fieldCount]string
	v[fStart] = day.Format("2006-01-02") + " 09:00"
	v[fCalendar] = defaultCalendar
	return newForm(v, nil)
}

// editEventForm pre-fills a form from an existing event.
func editEventForm(e model.Event, loc *time.Location) form {
	var v [fieldCount]string
	start, end := e.Span(loc)
	v[fTitle], v[fWhere], v[fNotes] = e.Title, e.Location, e.Description
	if e.AllDay {
		v[fStart], v[fEnd] = start.Format("2006-01-02"), end.AddDate(0, 0, -1).Format("2006-01-02")
	} else {
		v[fStart], v[fEnd] = start.Format("2006-01-02 15:04"), end.Format("15:04")
		if !model.StartOfDay(end).Equal(model.StartOfDay(start)) {
			v[fEnd] = end.Format("2006-01-02 15:04")
		}
	}
	var emails []string
	for _, a := range e.Attendees {
		if !a.Self && !a.Organizer {
			emails = append(emails, a.Email)
		}
	}
	v[fInvite] = strings.Join(emails, ", ")
	return newForm(v, &e)
}

func (f form) Update(msg tea.Msg) (form, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "tab", "down", "ctrl+j":
			return f.move(1), nil
		case "shift+tab", "up", "ctrl+k":
			return f.move(-1), nil
		}
	}
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	return f, cmd
}

func (f form) move(delta int) form {
	f.inputs[f.focus].Blur()
	f.focus = (f.focus + delta + fieldCount) % fieldCount
	f.inputs[f.focus].Focus()
	return f
}

func (f form) value(i int) string { return strings.TrimSpace(f.inputs[i].Value()) }
func (f form) changed(i int) bool { return f.value(i) != strings.TrimSpace(f.initial[i]) }

// draft validates the form as a new event.
func (f form) draft(now time.Time, dates timeparse.Parser) (service.Draft, error) {
	span, err := dates.ParseSpan(now, f.value(fStart), f.value(fEnd), f.value(fDuration), false)
	if err != nil {
		return service.Draft{}, err
	}
	rec, err := recur.ParseRepeat(f.value(fRepeat), span.Start, span.AllDay, time.Time{}, 0)
	if err != nil {
		return service.Draft{}, err
	}
	return service.Draft{
		CalendarID: f.value(fCalendar), Title: f.value(fTitle), Location: f.value(fWhere),
		Description: f.value(fNotes), Start: span.Start, End: span.End, AllDay: span.AllDay,
		Recurrence: rec, Invite: splitEmails(f.value(fInvite)),
	}, nil
}

// changes returns only what the user changed in an edit form.
func (f form) changes(now time.Time, dates timeparse.Parser) (service.Changes, error) {
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
	if f.changed(fStart) || f.changed(fEnd) || f.changed(fDuration) {
		end := f.value(fEnd)
		if f.changed(fDuration) && !f.changed(fEnd) {
			end = "" // a new duration replaces the pre-filled end
		}
		span, err := dates.ParseSpan(now, f.value(fStart), end, f.value(fDuration), false)
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
// that field); with the mouse on, [Save] and [Cancel] are buttons.
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
		in.Width = max(width-16, 20)
		md.field(label+"  "+in.View(), i)
	}
	if f.err != "" {
		md.text("")
		md.text(errorStyle.Render(f.err))
	}
	md.text("")
	if mouse {
		md.buttons(button{"Save", "enter"}, button{"Cancel", "esc"})
	}
	md.text(dimStyle.Render("ctrl+j/tab next · ctrl+k/shift+tab previous · enter save · esc cancel"))
	return md
}

// focusField moves the cursor to field i (a mouse click on it).
func (f form) focusField(i int) form {
	return f.move(i - f.focus)
}
