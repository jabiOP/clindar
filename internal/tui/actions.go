package tui

import (
	"context"
	"strings"

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
	e := m.entries()[m.cursor]
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
		d, err := m.form.draft(now)
		if err != nil {
			m.form.err = err.Error()
			return m, nil
		}
		return m, m.act("created "+d.Title, func(ctx context.Context, s *service.Service) error {
			_, err := s.Create(ctx, d)
			return err
		})
	}
	ch, err := m.form.changes(now)
	if err != nil {
		m.form.err = err.Error()
		return m, nil
	}
	e := *m.form.editing
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

// --- view ---
