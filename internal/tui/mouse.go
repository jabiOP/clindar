package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/quickadd"
)

// dragState tracks a drag in the grid: what was grabbed, where, and the day
// currently under the pointer.
// spanDrag is a drag across empty grid days: it creates an all-day event over them.
type spanDrag struct {
	active     bool
	from, over time.Time
}

type dragState struct {
	active bool
	event  model.Event
	part   part
	from   time.Time // the day the event was grabbed on
	over   time.Time // the day under the pointer now
	at     time.Time // when it was grabbed: a long press opens the color palette
}

// longPress is how long a press on an event, released in place, must last to
// open its color palette instead of its details (for terminals, like iTerm2,
// that keep the right button for their own menu).
const longPress = 500 * time.Millisecond

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
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		return m, nil // the wheel is off: trackpad swipes made it unusable, even throttled
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		return m.press(t, onSomething)
	case msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress && m.mode == modeNormal:
		switch t.kind { // right-click an event: its quick color palette, as in Google Calendar
		case targetEvent:
			return m.startColor(t.event), nil
		case targetEntry:
			if t.entry < len(m.entries()) {
				return m.startColor(m.entries()[t.entry]), nil
			}
		}
		return m, nil
	case msg.Action == tea.MouseActionMotion && m.span.active: // before hover, which takes every other motion
		switch {
		case t.kind == targetDay || t.kind == targetEmpty || t.kind == targetEvent || t.kind == targetMore:
			m.span.over = t.day
		case msg.Y <= 1:
			m = m.moveDays(-7, true)
		case msg.Y >= m.height-1:
			m = m.moveDays(7, true)
		}
		return m, m.reloadIfNeeded()
	case msg.Action == tea.MouseActionRelease && m.span.active:
		s := m.span
		m.span = spanDrag{}
		if s.over.Equal(s.from) { // a plain click: the day is selected, nothing more
			return m, nil
		}
		from, to := s.from, s.over
		if to.Before(from) {
			from, to = to, from
		}
		return m.openQuick(quickadd.Defaults{From: from, To: to})
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

func (m Model) press(t target, onSomething bool) (tea.Model, tea.Cmd) {
	if m.mode != modeNormal {
		switch {
		case t.kind == targetKey && t.key == "delete" && m.mode == modeDetail: // [Delete]: a click asks first
			return m.startDelete(m.detail, true)
		case t.kind == targetKey:
			return m.handleKey(keyMsg(t.key))
		case t.kind == targetField && m.mode == modeForm:
			m.form = m.form.focusField(t.field).openPicker() // a picker field opens its list
		case t.kind == targetOption && m.mode == modeForm:
			m.form = m.form.pickOption(t.field)
		case t.kind == targetOption && m.mode == modeColor: // a click on a dot applies it
			np, ev, _ := m.colorPick.pick(t.field)
			m.colorPick = np
			return m.colorEvent(ev)
		case !onSomething && m.mode == modeColor:
			m.mode = modeNormal // click outside closes
			return m, nil
		case t.kind == targetOption && m.mode == modeCalendars:
			var ev pickEvent
			var i int
			m.calMenu, ev, i = m.calMenu.pick(t.field)
			return m.calMenuEvent(ev, i)
		case !onSomething && (m.mode == modeDetail || m.mode == modeGoto):
			m.mode = modeNormal // click outside closes
		case m.mode == modeHelp:
			m.mode = m.helpFrom // any click closes help
		}
		return m, nil
	}
	m.message = ""
	if t.kind == targetDay || t.kind == targetEmpty { // a double click on a day adds an event there
		now := m.deps.Now()
		if m.lastClick.day.Equal(t.day) && now.Sub(m.lastClick.at) < doubleClick {
			m.lastClick, m.dayMode, m.span = clickRef{}, false, spanDrag{}
			return m.openQuick(quickadd.Defaults{From: t.day, To: t.day})
		}
		m.lastClick = clickRef{day: t.day, at: now}
	}
	switch t.kind {
	case targetKey: // status bar buttons
		return m.handleKey(keyMsg(t.key))
	case targetDay:
		if t.day.Equal(m.selected) {
			return m.startDayMode(), nil // like Enter
		}
		m = m.selectDay(t.day)
		m.dayMode = false
	case targetEmpty: // click selects; dragging across days adds an all-day span
		m = m.selectDay(t.day)
		m.dayMode = false
		m.span = spanDrag{true, t.day, t.day}
		return m, m.reloadIfNeeded()
	case targetMore:
		m = m.selectDay(t.day)
		m = m.startDayMode()
	case targetEvent:
		m = m.selectDay(t.day)
		m.dayMode = false
		if ok, why := canDrag(t.event, m.calendars[t.event.CalendarID]); !ok {
			if t.part != partBody { // a resize attempt: just say why
				m.message = why
				return m, nil
			}
			m.detail, m.mode, m.message = t.event, modeDetail, why
			return m, nil
		}
		m.drag = dragState{active: true, event: t.event, part: t.part, from: t.day, over: t.day, at: m.deps.Now()}
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
		switch {
		case d.part == partBody && m.deps.Now().Sub(d.at) >= longPress: // a long press: its colors
			return m.startColor(d.event), nil
		case d.part == partBody: // a click on the event
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

// doubleClick is the longest gap between two clicks on a day that adds an event.
const doubleClick = 400 * time.Millisecond

// clickRef is the last click on a day, to tell a double click.
type clickRef struct {
	day, at time.Time
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
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
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
