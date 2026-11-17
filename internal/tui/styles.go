package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle   = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	todayStyle    = lipgloss.NewStyle().Bold(true).Underline(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	modalStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

// width is the number of terminal cells s takes. Every width in this package
// comes from here, and it is the measure lipgloss itself uses to join and pad
// blocks: if we padded by a different measure (go-runewidth counts "❤️" as 1
// cell, lipgloss and terminals as 2), a grid column would come out one cell
// wider than planned and shift the whole row. Display width, never len():
// "–" is 3 bytes, CJK runes are 2 cells.
func width(s string) int { return lipgloss.Width(s) }

// fit truncates s to w terminal cells (with "…") and pads it to exactly w.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if sw := width(s); sw <= w {
		return s + strings.Repeat(" ", w-sw)
	}
	out := ""
	for _, r := range s { // keep as much as fits in w-1 cells, then "…"
		next := out + string(r)
		if width(next) > w-1 {
			break
		}
		out = next
	}
	out += "…"
	return out + strings.Repeat(" ", max(w-width(out), 0))
}

// focusStyle marks the selected event in its own color: that color as the
// background, with black or white text, whichever reads better on it.
func focusStyle(color string) lipgloss.Style {
	var r, g, b int
	if _, err := fmt.Sscanf(color, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return selectedStyle // no color (or not "#rrggbb"): reverse video
	}
	text := "#ffffff"
	if 299*r+587*g+114*b > 150_000 { // perceived brightness (ITU-R BT.601), out of 255_000
		text = "#000000"
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(color)).Foreground(lipgloss.Color(text)).Bold(true)
}

func calendarStyle(color string) lipgloss.Style {
	if color == "" {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}
