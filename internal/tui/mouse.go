package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/model"
)

// dragState tracks a drag in the grid: what was grabbed, where, and the day
// currently under the pointer.
type dragState struct {
	active bool
	event  model.Event
	part   part
	from   time.Time // the day the event was grabbed on
	over   time.Time // the day under the pointer now
}

// handleMouse turns mouse messages into the same actions the keys use.
// Clicks act on press, except on grid events: a press there starts a possible
// drag, and the release decides between "click" and "drop".
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.deps.Mouse {
		return m, nil
	}
	_, hits := m.layout()
	t, onSomething := hits.at(msg.X, msg.Y)
	switch {
	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
		return m.wheel(-1)
	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
		return m.wheel(1)
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		return m.press(t, onSomething)
	case msg.Action == tea.MouseActionMotion && !m.drag.active:
		m.hover = edgeRef{}
		if t.kind == targetEvent && t.part != partBody {
			m.hover = edgeOf(t.event, t.part)
		}
		return m, nil
	case msg.Action == tea.MouseActionMotion && m.drag.active:
		return m.dragOver(t, msg.Y)
	case msg.Action == tea.MouseActionRelease && m.drag.active:
		return m.drop()
	}
	return m, nil
}

const wheelEvery = 100 * time.Millisecond // trackpads send dozens of events per swipe

func (m Model) wheel(dir int) (tea.Model, tea.Cmd) {
	if m.mode != modeNormal {
		return m, nil
	}
	now := m.deps.Now()
	if now.Sub(m.lastWheel) < wheelEvery {
		return m, nil
	}
	m.lastWheel = now
	if m.view == viewList {
		m.cursor = min(max(m.cursor+dir, 0), max(len(m.entries())-1, 0))
		return m, nil
	}
	m = m.moveDays(dir, false)
	return m, m.reloadIfNeeded()
}

func (m Model) press(t target, onSomething bool) (tea.Model, tea.Cmd) {
	if m.mode != modeNormal {
		switch {
		case t.kind == targetKey:
			return m.handleKey(keyMsg(t.key))
		case t.kind == targetField && m.mode == modeForm:
			m.form = m.form.focusField(t.field)
		case !onSomething && (m.mode == modeDetail || m.mode == modeGoto):
			m.mode = modeNormal // click outside closes
		case m.mode == modeHelp:
			m.mode = modeNormal // any click closes help
		}
		return m, nil
	}
	m.message = ""
	switch t.kind {
	case targetKey: // status bar buttons
		return m.handleKey(keyMsg(t.key))
	case targetDay:
		if t.day.Equal(m.selected) {
			m.view, m.cursor = viewList, 0
			return m, m.reloadIfNeeded()
		}
		m = m.selectDay(t.day)
	case targetMore:
		m = m.selectDay(t.day)
		m.view, m.cursor = viewList, 0
	case targetEvent:
		m = m.selectDay(t.day)
		if ok, why := canDrag(t.event, m.calendars[t.event.CalendarID]); !ok {
			if t.part != partBody { // a resize attempt: just say why
				m.message = why
				return m, nil
			}
			m.detail, m.mode, m.message = t.event, modeDetail, why
			return m, nil
		}
		m.drag = dragState{active: true, event: t.event, part: t.part, from: t.day, over: t.day}
	case targetEntry:
		if t.entry == m.cursor {
			m.detail, m.mode = m.entries()[m.cursor], modeDetail
		} else {
			m.cursor = t.entry
		}
	}
	return m, m.reloadIfNeeded()
}

// dragOver follows the pointer. Dragging onto the grid's header scrolls a
// week back; onto the status bar, a week forward.
func (m Model) dragOver(t target, y int) (tea.Model, tea.Cmd) {
	switch {
	case t.kind == targetDay || t.kind == targetEvent || t.kind == targetMore:
		m.drag.over = t.day
	case y <= 1:
		m = m.moveDays(-7, true)
	case y >= m.height-1:
		m = m.moveDays(7, true)
	}
	m.message = ""
	if ch, err := dragChanges(m.drag.event, m.drag.part, m.drag.from, m.drag.over, m.loc()); err == nil {
		m.message = dragPreview(m.drag.event, m.drag.part, ch, m.loc())
	}
	return m, m.reloadIfNeeded()
}

func (m Model) drop() (tea.Model, tea.Cmd) {
	d := m.drag
	m.drag = dragState{}
	if d.over.Equal(d.from) { // no movement
		if d.part == partBody { // a click on the event
			m.detail, m.mode, m.message = d.event, modeDetail, ""
		}
		return m, nil
	}
	ch, err := dragChanges(d.event, d.part, d.from, d.over, m.loc())
	if err != nil {
		m.message = "not moved: " + err.Error()
		return m, nil
	}
	return m.applyChanges(d.event, ch)
}

func (m Model) selectDay(day time.Time) Model {
	return m.moveDays(daysBetween(m.selected, day), false)
}

// keyMsg turns a button's key name into the key press it stands for.
func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// grabbedEdge is the handle being dragged (none while dragging an event's body).
func (m Model) grabbedEdge() edgeRef {
	if !m.drag.active || m.drag.part == partBody {
		return edgeRef{}
	}
	return edgeOf(m.drag.event, m.drag.part)
}
