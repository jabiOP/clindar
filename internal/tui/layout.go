package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/output"
)

func (m Model) View() string {
	s, _ := m.layout()
	return s
}

// layout draws the screen and returns what is clickable where. View and the
// mouse handler both call it, so a click always matches what was drawn.
func (m Model) layout() (string, hitMap) {
	bodyH := m.height - 1
	var body string
	var hits hitMap
	switch m.view {
	case viewGrid:
		body, hits = renderGrid(gridView{
			start: m.gridStart, today: m.today, selected: m.selected,
			days:      model.GroupByDay(m.events, m.gridStart, gridDays),
			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc(),
			handles: m.deps.Mouse, drop: m.drag.over,
		})
	case viewList:
		body, hits = renderList(listView{days: model.GroupByDay(m.events, m.selected, listDays), cursor: m.cursor,
			calendars: m.calendars, width: m.width, height: bodyH, loc: m.loc()})
	}
	if md, ok := m.popup(); ok {
		box, boxHits := md.render()
		var x, y int
		body, x, y = overlay(m.width, bodyH, box)
		hits = boxHits.offset(x, y) // while a pop-up is open, only the pop-up is clickable
	}
	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)
	bar, barHits := m.statusBar()
	if m.mode == modeNormal {
		hits = append(hits, barHits.offset(0, bodyH)...)
	}
	return body + "\n" + bar, hits
}

// popup builds the pop-up for the current mode, if any. With the mouse on,
// choices are buttons that act like their keys; without it, the keyboard
// hints read as before.
func (m Model) popup() (modal, bool) {
	var md modal
	choices := func(plain string, bs ...button) {
		if m.deps.Mouse {
			md.buttons(bs...)
		} else {
			md.text(plain)
		}
	}
	switch m.mode {
	case modeHelp:
		md.text(helpText(m.deps.Mouse))
	case modeForm:
		return m.form.view(min(m.width-4, 80), m.deps.Mouse), true
	case modeGoto:
		md.text("Go to date")
		md.text("")
		md.text(m.gotoInput.View())
		if m.deps.Mouse {
			md.text("")
			md.buttons(button{"Go", "enter"}, button{"Cancel", "esc"})
		}
	case modeDetail:
		var b strings.Builder
		var cals []model.Calendar
		for _, c := range m.calendars {
			cals = append(cals, c)
		}
		p, _ := output.NewPrinter(&b, output.Pretty, "", m.loc(), cals)
		p.Event(m.detail)
		md.text(strings.TrimRight(b.String(), "\n"))
		md.text("")
		choices(dimStyle.Render("e edit · d delete · r RSVP · esc close"),
			button{"Edit", "e"}, button{"Delete", "d"}, button{"RSVP", "r"}, button{"Close", "esc"})
	case modeScope:
		labels := map[byte]button{'t': {"This event", "t"}, 'f': {"Following", "f"}, 'a': {"All events", "a"}}
		plain := map[byte]string{'t': "(t)his event", 'f': "(f)ollowing", 'a': "(a)ll events"}
		var bs []button
		var parts []string
		for i := range len(m.allowedScopes) {
			bs = append(bs, labels[m.allowedScopes[i]])
			parts = append(parts, plain[m.allowedScopes[i]])
		}
		md.text("Recurring event. Apply to:")
		md.text("")
		choices(strings.Join(parts, "   ")+"\n\nesc cancel", append(bs, button{"Cancel", "esc"})...)
	case modeConfirm:
		md.text("Delete this event?")
		choices("(y/n)", button{"Delete", "y"}, button{"Cancel", "n"})
	case modeRSVP:
		md.text("Going?")
		choices("(y)es  (n)o  (m)aybe", button{"Yes", "y"}, button{"No", "n"}, button{"Maybe", "m"}, button{"Cancel", "esc"})
	default:
		return modal{}, false
	}
	return md, true
}

// statusBar shows sync state and messages. With the mouse on, it ends with
// [+ Add] [⟳ Sync] [? Help] buttons; hits are relative to the bar.
func (m Model) statusBar() (string, hitMap) {
	parts := []string{}
	switch {
	case m.syncing:
		parts = append(parts, "⟳ syncing")
	case m.status.LastSync.IsZero():
		parts = append(parts, "never synced")
	default:
		parts = append(parts, "synced "+ago(m.deps.Now().Sub(m.status.LastSync)))
	}
	if m.status.Pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", m.status.Pending))
	}
	if m.status.Discarded > 0 {
		parts = append(parts, errorStyle.Render(fmt.Sprintf("%d edit(s) discarded: see clindar log", m.status.Discarded)))
	}
	if m.message != "" {
		parts = append(parts, m.message)
	}
	if !m.deps.Mouse {
		parts = append(parts, "? help")
		return dimStyle.Render(fit(strings.Join(parts, " · "), m.width)), nil
	}
	var hits hitMap
	var labels []string
	x := m.width
	bs := []button{{"+ Add", "a"}, {"⟳ Sync", "s"}, {"? Help", "?"}}
	for i := len(bs) - 1; i >= 0; i-- { // lay out right to left
		label := "[" + bs[i].label + "]"
		x -= runewidth.StringWidth(label)
		hits.add(x, 0, runewidth.StringWidth(label), 1, target{kind: targetKey, key: bs[i].key})
		labels = append([]string{label}, labels...)
		x--
	}
	info := dimStyle.Render(fit(strings.Join(parts, " · "), max(x, 0)))
	return info + " " + strings.Join(labels, " "), hits
}
