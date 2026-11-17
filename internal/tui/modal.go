package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// modal builds a pop-up line by line and remembers where its buttons and
// form fields are, relative to its content.
type modal struct {
	lines []string
	hits  hitMap
}

// button acts as pressing key when clicked, so mouse and keyboard share one code path.
type button struct{ label, key string }

func (m *modal) text(s string) { m.lines = append(m.lines, strings.Split(s, "\n")...) }

// buttons adds one line of "[Label]" buttons, two spaces apart.
func (m *modal) buttons(bs ...button) {
	var parts []string
	x, y := 0, len(m.lines)
	for _, b := range bs {
		label := "[" + b.label + "]"
		w := width(label)
		m.hits.add(x, y, w, 1, target{kind: targetKey, key: b.key})
		parts = append(parts, label)
		x += w + 2
	}
	m.lines = append(m.lines, strings.Join(parts, "  "))
}

// field adds a clickable line (a form field).
func (m *modal) field(line string, index int) {
	m.hits.add(0, len(m.lines), width(line), 1, target{kind: targetField, field: index})
	m.lines = append(m.lines, line)
}

// render draws the box. Hits are relative to the box's top-left corner; the
// whole box is a targetModal, so a click on its border isn't "outside".
func (m modal) render() (string, hitMap) {
	box := modalStyle.Render(strings.Join(m.lines, "\n"))
	all := hitMap{{0, 0, lipgloss.Width(box), lipgloss.Height(box), target{kind: targetModal}}}
	// modalStyle: a 1-cell border, plus 1 cell of padding left and right.
	return box, append(all, m.hits.offset(2, 1)...)
}

// overlay centers box in a width×height area and returns where its top-left
// corner landed. We place it ourselves (not lipgloss.Place) so the hit map
// uses exactly the same arithmetic as the drawing.
func overlay(width, height int, box string) (s string, x, y int) {
	x = max((width-lipgloss.Width(box))/2, 0)
	y = max((height-lipgloss.Height(box))/2, 0)
	lines := strings.Split(box, "\n")
	for i := range lines {
		lines[i] = strings.Repeat(" ", x) + lines[i]
	}
	return strings.Repeat("\n", y) + strings.Join(lines, "\n"), x, y
}

// optionCells adds a grid line whose cells (shifted by indent) are clickable options.
func (m *modal) optionCells(indent int, line string, cells []gridCell) {
	for _, c := range cells {
		m.hits.add(indent+c.x, len(m.lines), c.w, 1, target{kind: targetOption, field: c.i})
	}
	m.lines = append(m.lines, strings.Repeat(" ", indent)+line)
}

// option adds a clickable picker row for option index i.
func (m *modal) option(line string, i int) {
	m.hits.add(0, len(m.lines), width(line), 1, target{kind: targetOption, field: i})
	m.lines = append(m.lines, line)
}
