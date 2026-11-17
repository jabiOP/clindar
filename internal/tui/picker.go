package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// option is one choice in a picker.
type option struct {
	label   string
	value   string // what the caller stores: a calendar ID, a repeat word, a color ID…
	swatch  string // optional "#rrggbb", drawn as ● before the label
	checked bool   // checklist mode
	group   int    // grid mode: options of one group share a row (e.g. a color family)
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
	grid      bool // open as a grid: one row per group; h/l along a row, j/k between rows
	dots      bool // grid of bare color dots, 9 per row, groups flowing on (the quick palette)
}

// columns is how many options a grid row holds.
func (p picker) columns() int {
	if p.dots {
		return 9
	}
	return gridColumns
}

// rowGroup is what starts a new grid row when it changes. Dots flow on, except
// that an option without a color (e.g. "Calendar default") gets its own row.
func (p picker) rowGroup(o option) int {
	if p.dots {
		return min(o.group, 1)
	}
	return o.group
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
	if p.grid && !p.filtering {
		switch k.String() {
		case "h", "left":
			p.cursor = max(p.cursor-1, 0)
			return p, pickNothing, 0
		case "l", "right":
			p.cursor = min(p.cursor+1, max(len(vis)-1, 0))
			return p, pickNothing, 0
		case "j", "down":
			p.cursor = p.gridStep(1)
			return p, pickNothing, 0
		case "k", "up":
			p.cursor = p.gridStep(-1)
			return p, pickNothing, 0
		}
	}
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
	hint := "j/k move · enter choose · / filter · esc close"
	if p.grid {
		lines, cells := p.gridRows(w)
		for n, line := range lines {
			md.optionCells(0, line, cells[n])
		}
		if vis := p.visible(); p.dots && p.cursor < len(vis) { // dots have no names: say which one
			md.text("")
			md.text(p.options[vis[p.cursor]].label)
		}
		hint = "h/j/k/l move · enter choose · esc close"
	} else {
		lines, idx := p.rows(w)
		for n, line := range lines {
			md.option(line, idx[n])
		}
	}
	md.text("")
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
// pickerWindow is how many options an open list shows at once; it scrolls
// with the cursor (a calendar can have 200+ colors). Typing filters it.
const pickerWindow = 10

func (p picker) rows(w int) (lines []string, idx []int) {
	vis := p.visible()
	first := min(max(p.cursor-pickerWindow/2, 0), max(len(vis)-pickerWindow, 0))
	for row, i := range vis {
		if row < first || row >= first+pickerWindow {
			continue
		}
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

// gridColumns is how many options a grid row holds; a longer group wraps.
// Three fit names like "Cherry blossom" in an 80-column terminal.
const gridColumns = 3

// gridLayout splits the visible options into rows: a new row for each group,
// and when a row is full. Values are positions in visible().
func (p picker) gridLayout(cols int) [][]int {
	vis := p.visible()
	var rows [][]int
	for pos, i := range vis {
		n := len(rows)
		if n == 0 || len(rows[n-1]) == cols || p.rowGroup(p.options[vis[rows[n-1][0]]]) != p.rowGroup(p.options[i]) {
			rows = append(rows, nil)
			n++
		}
		rows[n-1] = append(rows[n-1], pos)
	}
	return rows
}

// gridStep moves the cursor dir rows down (or up), keeping its column.
func (p picker) gridStep(dir int) int {
	rows := p.gridLayout(p.columns())
	for r, row := range rows {
		for c, pos := range row {
			if pos != p.cursor {
				continue
			}
			to := min(max(r+dir, 0), len(rows)-1)
			return rows[to][min(c, len(rows[to])-1)]
		}
	}
	return p.cursor
}

// gridCell is one clickable option in a grid line.
type gridCell struct{ x, w, i int }

// gridRows draws the open grid (a window of rows around the cursor) for w cells.
func (p picker) gridRows(w int) (lines []string, cells [][]gridCell) {
	cols := p.columns()
	vis := p.visible()
	rows := p.gridLayout(cols)
	cur := 0
	for r, row := range rows {
		if slices.Contains(row, p.cursor) {
			cur = r
		}
	}
	const window = 10
	first := min(max(cur-window/2, 0), max(len(rows)-window, 0))
	last := min(first+window, len(rows))
	cellW := (w - 4) / cols
	if p.dots {
		cellW = 4
	}
	more := func(s string) { lines, cells = append(lines, dimStyle.Render("  "+s)), append(cells, nil) }
	if first > 0 {
		more("↑ more")
	}
	for r := first; r < last; r++ {
		var line string
		var cs []gridCell
		for c, pos := range rows[r] {
			o := p.options[vis[pos]]
			text := fit("  "+swatch(o.swatch)+o.label, cellW)
			if p.dots && o.swatch != "" {
				text = dotCell(o.swatch, pos == p.cursor, vis[pos] == p.chosen)
			} else if pos == p.cursor {
				dot := ""
				if o.swatch != "" {
					dot = "● "
				}
				text = selectedStyle.Render(fit("▶ "+dot+o.label, cellW))
			}
			if p.dots && o.swatch == "" { // a whole row, like Google's "Default" button
				text = fit("  "+o.label, cols*cellW)
				if pos == p.cursor {
					text = selectedStyle.Render(fit("▶ "+o.label, cols*cellW))
				}
				cs = append(cs, gridCell{x: 0, w: cols * cellW, i: vis[pos]})
				line += text
				continue
			}
			cs = append(cs, gridCell{x: c * cellW, w: cellW, i: vis[pos]})
			line += text
		}
		lines, cells = append(lines, line), append(cells, cs)
	}
	if last < len(rows) {
		more("↓ more")
	}
	return lines, cells
}

// dotCell is one 4-cell dot of the quick palette: "[●]" under the cursor,
// "◉" for the current color.
func dotCell(color string, cursor, chosen bool) string {
	dot := "●"
	if chosen {
		dot = "◉"
	}
	dot = calendarStyle(color).Render(dot)
	if cursor {
		return "[" + dot + "] "
	}
	return " " + dot + "  "
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
