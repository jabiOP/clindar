package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/timeparse"
)

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		return m.formKey(k)
	case modeGoto:
		return m.gotoKey(k)
	case modeScope:
		return m.scopeKey(k)
	case modeConfirm:
		m.mode = modeNormal
		if k.String() == "y" {
			return m, m.afterScope(service.ScopeNone)
		}
		return m, nil
	case modeRSVP:
		return m.rsvpKey(k)
	case modeDetail:
		return m.detailKey(k)
	case modeHelp:
		m.mode = modeNormal
		return m, nil
	}
	if m.drag.active && key.Matches(k, keys.Back) {
		m.drag, m.message = dragState{}, "drag cancelled"
		return m, nil
	}
	m.message = ""
	switch {
	case key.Matches(k, keys.Quit):
		return m, tea.Quit
	case key.Matches(k, keys.Help):
		m.mode = modeHelp
	case key.Matches(k, keys.Toggle):
		m.view = 1 - m.view
		m.cursor = 0
	case key.Matches(k, keys.Today):
		m.selected, m.gridStart, m.cursor = m.today, weekStart(m.today, m.deps.WeekStart), 0
	case key.Matches(k, keys.Goto):
		m.mode = modeGoto
		m.gotoInput.SetValue("")
		return m, m.gotoInput.Focus()
	case key.Matches(k, keys.Sync):
		m.syncing = m.deps.Sync != nil
		return m, m.syncCmd()
	case key.Matches(k, keys.Add):
		m.form = newEventForm(m.selected, m.deps.DefaultCalendar)
		m.mode = modeForm
		return m, nil
	case key.Matches(k, keys.ScrollDown):
		m = m.moveDays(7, true)
	case key.Matches(k, keys.ScrollUp):
		m = m.moveDays(-7, true)
	default:
		if m.view == viewGrid {
			return m.gridKey(k)
		}
		return m.listKey(k)
	}
	return m, m.reloadIfNeeded()
}

func (m Model) gridKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(k, keys.Left):
		m = m.moveDays(-1, false)
	case key.Matches(k, keys.Right):
		m = m.moveDays(1, false)
	case key.Matches(k, keys.Up):
		m = m.moveDays(-7, false)
	case key.Matches(k, keys.Down):
		m = m.moveDays(7, false)
	case key.Matches(k, keys.Open):
		m.view, m.cursor = viewList, 0
	case key.Matches(k, keys.Edit, keys.Delete, keys.RSVP):
		m.message = "press enter to pick an event in the list"
	}
	return m, m.reloadIfNeeded()
}

func (m Model) listKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := m.entries()
	var current *model.Event
	if m.cursor < len(entries) {
		current = &entries[m.cursor]
	}
	switch {
	case key.Matches(k, keys.Down):
		m.cursor = min(m.cursor+1, max(len(entries)-1, 0))
	case key.Matches(k, keys.Up):
		m.cursor = max(m.cursor-1, 0)
	case key.Matches(k, keys.Left):
		m = m.moveDays(-1, false)
		m.cursor = 0
	case key.Matches(k, keys.Right):
		m = m.moveDays(1, false)
		m.cursor = 0
	case current == nil:
		return m, nil
	case key.Matches(k, keys.Open):
		m.detail, m.mode = *current, modeDetail
	case key.Matches(k, keys.Edit):
		m.form = editEventForm(*current, m.loc())
		m.mode = modeForm
		return m, nil
	case key.Matches(k, keys.Delete):
		return m.startDelete(*current), nil
	case key.Matches(k, keys.RSVP):
		return m.startRSVP(*current), nil
	}
	return m, m.reloadIfNeeded()
}

// moveDays moves the selection; scroll also moves the grid window.
func (m Model) moveDays(n int, scroll bool) Model {
	m.selected = m.selected.AddDate(0, 0, n)
	if scroll {
		m.gridStart = m.gridStart.AddDate(0, 0, n)
	}
	switch end := m.gridStart.AddDate(0, 0, gridDays); {
	case m.selected.Before(m.gridStart):
		m.gridStart = weekStart(m.selected, m.deps.WeekStart)
	case !m.selected.Before(end):
		m.gridStart = weekStart(m.selected, m.deps.WeekStart).AddDate(0, 0, -21)
	}
	return m
}

func (m Model) reloadIfNeeded() tea.Cmd {
	from, to := m.window()
	if from.Before(m.loadedFrom) || to.After(m.loadedTo) || m.loadedFrom.IsZero() {
		return m.load()
	}
	return nil
}

func (m Model) gotoKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
		return m, nil
	case tea.KeyEnter:
		m.mode = modeNormal
		t, _, err := timeparse.ParseTime(m.gotoInput.Value(), m.deps.Now())
		if err != nil {
			m.message = err.Error()
			return m, nil
		}
		m = m.moveDays(int(model.StartOfDay(t).Sub(m.selected).Round(24*time.Hour)/(24*time.Hour)), false)
		return m, m.reloadIfNeeded()
	}
	var cmd tea.Cmd
	m.gotoInput, cmd = m.gotoInput.Update(k)
	return m, cmd
}
