package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// option is one choice in a picker.
type option struct {
	label   string
	value   string // what the caller stores: a calendar ID, a repeat word, a color ID…
	swatch  string // optional "#rrggbb", drawn as ● before the label
	checked bool   // checklist mode
}

// picker is the TUI's one choice widget: closed in a form, open as a
// dropdown, or open as a checklist. It only holds state; the caller acts on
// the pickEvent it returns.
type picker struct {
	title     string
	options   []option
	chosen    int // single choice: index into options
	cursor    int // index into visible()
	filter    string
	filtering bool // after "/" (or a typed letter): runes go to the filter
	open      bool
	checklist bool
}

type pickEvent int

const (
	pickNothing pickEvent = iota
	pickChanged           // single: a new choice
	pickToggled           // checklist: an option was ticked or unticked (its index is returned)
	pickClosed            // closed without a change
)

func (p picker) current() option { return p.options[p.chosen] }

// visible lists the indexes of options matching the filter.
func (p picker) visible() []int {
	var out []int
	for i, o := range p.options {
		if strings.Contains(strings.ToLower(o.label), strings.ToLower(p.filter)) {
			out = append(out, i)
		}
	}
	return out
}

// Update handles one key. It returns what happened and, for pickToggled, which option.
func (p picker) Update(k tea.KeyMsg) (picker, pickEvent, int) {
	n := len(p.options)
	if !p.open { // closed, in a form
		switch k.String() {
		case "h", "left", "ctrl+p":
			p.chosen = (p.chosen - 1 + n) % n
			return p, pickChanged, p.chosen
		case "l", "right", "ctrl+n":
			p.chosen = (p.chosen + 1) % n
			return p, pickChanged, p.chosen
		case " ", "enter":
			p.open, p.filter, p.filtering, p.cursor = true, "", false, max(0, indexOf(p.visible(), p.chosen))
		}
		return p, pickNothing, 0
	}
	vis := p.visible()
	switch s := k.String(); {
	case s == "esc" && p.filtering:
		p.filter, p.filtering = "", false
	case s == "esc":
		p.open, p.filter, p.filtering = false, "", false
		return p, pickClosed, 0
	case s == "down" || (s == "j" && !p.filtering):
		p.cursor = min(p.cursor+1, max(len(vis)-1, 0))
	case s == "up" || (s == "k" && !p.filtering):
		p.cursor = max(p.cursor-1, 0)
	case p.checklist && (s == " " || (s == "x" && !p.filtering)) && len(vis) > 0:
		i := vis[p.cursor]
		p.options[i].checked = !p.options[i].checked
		return p, pickToggled, i
	case !p.checklist && s == "enter" && len(vis) > 0:
		p.chosen, p.open, p.filter, p.filtering = vis[p.cursor], false, "", false
		return p, pickChanged, p.chosen
	case s == "backspace" && p.filter != "":
		p.filter = p.filter[:len(p.filter)-1]
		p.cursor = 0
	case s == "/":
		p.filtering = true
	case k.Type == tea.KeyRunes && (p.filtering || !p.checklist):
		p.filtering, p.filter, p.cursor = true, p.filter+string(k.Runes), 0
	}
	return p, pickNothing, 0
}

// pick is a mouse click on option i: choose it (single) or toggle it (checklist).
func (p picker) pick(i int) (picker, pickEvent, int) {
	if p.checklist {
		p.options[i].checked = !p.options[i].checked
		return p, pickToggled, i
	}
	p.chosen, p.open, p.filter, p.filtering = i, false, "", false
	return p, pickChanged, i
}

// inline is the closed picker inside a form field.
func (p picker) inline() string {
	o := p.current()
	return "‹ " + swatch(o.swatch) + o.label + " ›"
}

// view draws the open picker as a pop-up; each row is clickable.
func (p picker) view(w int, mouse bool) modal {
	var md modal
	md.text(headerStyle.Render(p.title))
	if p.filtering {
		md.text("/" + p.filter)
	}
	md.text("")
	lines, idx := p.rows(w)
	for n, line := range lines {
		md.option(line, idx[n])
	}
	md.text("")
	hint := "j/k move · enter choose · / filter · esc close"
	if p.checklist {
		hint = "j/k move · space toggle · / filter · esc close"
	}
	if mouse {
		md.buttons(button{"Close", "esc"})
	}
	md.text(dimStyle.Render(hint))
	return md
}

// rows draws the open picker's visible options, cursor row marked, fitted to
// w-4 cells; idx[n] is the option index of row n (for click targets).
func (p picker) rows(w int) (lines []string, idx []int) {
	for row, i := range p.visible() {
		o := p.options[i]
		mark := "  "
		if row == p.cursor {
			mark = "▶ "
		}
		box := ""
		if p.checklist {
			box = "[ ] "
			if o.checked {
				box = "[x] "
			}
		}
		var line string
		if row == p.cursor {
			// Plain text, styled once: a colored ● would end with a reset code that
			// also cuts the reverse video short (Task 10.2's rule, too).
			dot := ""
			if o.swatch != "" {
				dot = "● "
			}
			line = selectedStyle.Render(fit(mark+box+dot+o.label, max(w-4, 10)))
		} else {
			line = fit(mark+box+swatch(o.swatch)+o.label, max(w-4, 10))
		}
		lines, idx = append(lines, line), append(idx, i)
	}
	return lines, idx
}

// swatch is a colored ● for a "#rrggbb" color, or nothing.
func swatch(color string) string {
	if color == "" {
		return ""
	}
	return calendarStyle(color).Render("●") + " "
}

func indexOf(xs []int, v int) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}
