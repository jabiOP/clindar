package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/quickadd"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/timeparse"
)

// openQuick starts quick add for the given day(s).
func (m Model) openQuick(def quickadd.Defaults) (Model, tea.Cmd) {
	m.quick = textinput.New()
	m.quick.Prompt, m.quick.Placeholder = "", "Dentist fri 15:30 45m · Trip 20/11-23/11 · Standup weekdays 9:00"
	m.quick.Cursor.SetMode(cursor.CursorStatic)
	m.quickDef, m.mode = def, modeQuick
	return m, m.quick.Focus()
}

func (m Model) parseQuick() (quickadd.Result, error) {
	return quickadd.Parse(m.quick.Value(), m.quickDef, m.deps.Now(), m.deps.Dates)
}

func (m Model) quickKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
		return m, nil
	case tea.KeyTab: // the full form, pre-filled
		r, err := m.parseQuick()
		if err != nil {
			r = quickadd.Result{Title: strings.TrimSpace(m.quick.Value()),
				Span: timeparse.Span{Start: m.quickDef.From, End: m.quickDef.To.AddDate(0, 0, 1), AllDay: true}}
		}
		m.form, m.mode = m.formFromQuick(r), modeForm
		return m, nil
	case tea.KeyEnter:
		r, err := m.parseQuick()
		if err != nil {
			return m, nil // the preview line already shows the error
		}
		d := service.Draft{CalendarID: m.deps.DefaultCalendar, Title: r.Title, Start: r.Span.Start,
			End: r.Span.End, AllDay: r.Span.AllDay, Recurrence: r.Recurrence}
		return m, m.act("created "+r.Title, func(ctx context.Context, s *service.Service) error {
			_, err := s.Create(ctx, d)
			return err
		})
	}
	var cmd tea.Cmd
	m.quick, cmd = m.quick.Update(k)
	return m, cmd
}

// quickPopup is quick add's pop-up: the line, then a live preview (or the problem).
func (m Model) quickPopup() modal {
	var md modal
	md.text(headerStyle.Render("New event"))
	md.text("")
	md.text(m.quick.View())
	r, err := m.parseQuick()
	switch {
	case strings.TrimSpace(m.quick.Value()) == "":
		md.text(dimStyle.Render("→ " + quickadd.Result{Span: timeparse.Span{Start: m.quickDef.From,
			End: m.quickDef.To.AddDate(0, 0, 1), AllDay: true}}.Preview() + " · type a title"))
	case err != nil:
		md.text(errorStyle.Render("? " + err.Error()))
	default:
		md.text(dimStyle.Render("→ " + r.Preview()))
	}
	md.text("")
	if m.deps.Mouse {
		md.buttons(button{"Save", "enter"}, button{"More…", "tab"}, button{"Cancel", "esc"})
	}
	md.text(dimStyle.Render("enter save · tab more fields · esc cancel"))
	return md
}

// formFromQuick opens the full add form pre-filled from a quick-add line.
func (m Model) formFromQuick(r quickadd.Result) form {
	f := newEventForm(r.Span.Start, m.calList, m.deps.DefaultCalendar, m.deps.Dates)
	layout := f.dateLayout()
	f = f.setText(fTitle, r.Title)
	if r.Span.AllDay {
		date := r.Span.Start.Format(layout)
		if last := r.Span.End.AddDate(0, 0, -1); last.After(r.Span.Start) {
			date += "-" + last.Format(layout) // the inclusive last day
		}
		f = f.setText(fDate, date).setChoice(fAllDay, "yes")
	} else {
		f = f.setText(fDate, r.Span.Start.Format(layout)).setChoice(fAllDay, "no").
			setText(fStart, r.Span.Start.Format("15:04")).setText(fEnd, r.Span.End.Format("15:04"))
	}
	if w := r.RepeatWord(); w != "" {
		f = f.setChoice(fRepeat, w)
	}
	return f
}
