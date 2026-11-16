package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
)

func (m Model) startDelete(e model.Event) Model {
	id, title := e.ID, e.Title
	m.afterScope = func(sc service.Scope) tea.Cmd {
		return m.act("deleted "+title, func(ctx context.Context, s *service.Service) error {
			return s.Delete(ctx, id, sc)
		})
	}
	if e.IsInstance() {
		m.mode, m.allowedScopes = modeScope, "tfa"
	} else {
		m.mode = modeConfirm
	}
	return m
}

func (m Model) rsvpKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	answers := map[string]model.ResponseStatus{"y": model.ResponseAccepted, "n": model.ResponseDeclined, "m": model.ResponseTentative}
	resp, ok := answers[k.String()]
	if !ok {
		m.mode = modeNormal
		return m, nil
	}
	e := m.target
	id := e.ID
	m.afterScope = func(sc service.Scope) tea.Cmd {
		return m.act("replied "+string(resp), func(ctx context.Context, s *service.Service) error {
			return s.RSVP(ctx, id, resp, sc)
		})
	}
	if e.IsInstance() {
		m.mode, m.allowedScopes = modeScope, "ta"
		return m, nil
	}
	m.mode = modeNormal
	return m, m.afterScope(service.ScopeNone)
}

func (m Model) scopeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	scopes := map[string]service.Scope{"t": service.ScopeThis, "f": service.ScopeFollowing, "a": service.ScopeAll}
	sc, ok := scopes[k.String()]
	m.mode = modeNormal
	if !ok || !strings.Contains(m.allowedScopes, k.String()) {
		return m, nil
	}
	return m, m.afterScope(sc)
}

func (m Model) formKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
		return m, nil
	case tea.KeyEnter:
		return m.submitForm()
	}
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(k)
	return m, cmd
}

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	now := m.deps.Now()
	if m.form.editing == nil {
		d, err := m.form.draft(now, m.deps.Dates)
		if err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		return m, m.act("created "+d.Title, func(ctx context.Context, s *service.Service) error {
			_, err := s.Create(ctx, d)
			return err
		})
	}
	ch, err := m.form.changes(now, m.deps.Dates)
	if err != nil {
		m.form.err = err.Error()
		return m, nil
	}
	return m.applyChanges(*m.form.editing, ch)
}

// applyChanges saves an edit, asking for a scope first if e is part of a
// series. Both the edit form and a mouse drop end here.
func (m Model) applyChanges(e model.Event, ch service.Changes) (tea.Model, tea.Cmd) {
	m.afterScope = func(sc service.Scope) tea.Cmd {
		return m.act("saved "+e.Title, func(ctx context.Context, s *service.Service) error {
			_, err := s.Update(ctx, e.ID, ch, sc)
			return err
		})
	}
	if e.IsInstance() {
		m.mode, m.allowedScopes = modeScope, "tfa"
		return m, nil
	}
	return m, m.afterScope(service.ScopeNone)
}

// startRSVP asks how to answer an invitation to e.
func (m Model) startRSVP(e model.Event) Model {
	if can := permissions(e, m.calendars[e.CalendarID]); !can.rsvp {
		m.mode, m.message = modeNormal, can.whyNoRSVP
		return m
	}
	m.target, m.mode = e, modeRSVP
	return m
}

// detailKey: e/d/r act on the event shown; any other key closes the pop-up.
func (m Model) detailKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(k, keys.Edit, keys.Delete, keys.RSVP) {
		return m.eventAction(k, m.detail)
	}
	m.mode = modeNormal
	return m, nil
}

// eventAction applies e/d/r to e with the same permission rules everywhere
// (details box, event mode); refusals are explained in the status bar.
func (m Model) eventAction(k tea.KeyMsg, e model.Event) (tea.Model, tea.Cmd) {
	can := permissions(e, m.calendars[e.CalendarID])
	switch {
	case key.Matches(k, keys.Edit) && !can.edit:
		m.message = "can't edit: " + can.whyNoEdit
	case key.Matches(k, keys.Delete) && !can.delete:
		m.message = "can't delete: the calendar is read-only"
	case key.Matches(k, keys.RSVP) && !can.rsvp:
		m.message = can.whyNoRSVP
	case key.Matches(k, keys.Edit):
		m.form, m.mode = editEventForm(e, m.loc()), modeForm
	case key.Matches(k, keys.Delete):
		m = m.startDelete(e)
	case key.Matches(k, keys.RSVP):
		m = m.startRSVP(e)
	}
	return m, nil
}

// --- view ---
