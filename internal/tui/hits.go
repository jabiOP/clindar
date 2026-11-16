package tui

import (
	"time"

	"github.com/jabiOP/clindar/internal/model"
)

// A hit map records what was drawn where, so a mouse position can be turned
// back into "this day", "this event's end handle", or "this button". Render
// functions build it while drawing, so the two can never disagree.

type targetKind int

const (
	targetNone   targetKind = iota
	targetDay               // a grid cell
	targetEvent             // an event line in a grid cell
	targetMore              // "+N more" in a grid cell
	targetEntry             // a list row
	targetKey               // a button: clicking it acts like pressing key
	targetField             // a form field
	targetModal             // inside a pop-up, but not on a button or field
	targetOption            // a row in a picker; field holds the option index
	targetEmpty             // blank space in a grid day, below its events (Task 14.18)
)

// part is the piece of a grid event line that was grabbed.
type part int

const (
	partBody  part = iota // move the whole event
	partStart             // ◂ handle: change the first day
	partEnd               // ▸ handle: change the last day
)

type target struct {
	kind  targetKind
	day   time.Time   // targetDay, targetEvent, targetMore: the cell's day
	event model.Event // targetEvent
	part  part        // targetEvent
	entry int         // targetEntry: index into the list's entries
	key   string      // targetKey
	field int         // targetField
}

type hit struct {
	x, y, w, h int
	t          target
}

type hitMap []hit

func (m *hitMap) add(x, y, w, h int, t target) { *m = append(*m, hit{x, y, w, h, t}) }

// at returns the target at (x, y). Later additions are on top: a button wins
// over the pop-up it sits in.
func (m hitMap) at(x, y int) (target, bool) {
	for i := len(m) - 1; i >= 0; i-- {
		h := m[i]
		if x >= h.x && x < h.x+h.w && y >= h.y && y < h.y+h.h {
			return h.t, true
		}
	}
	return target{}, false
}

// offset moves every rectangle, for content drawn at (dx, dy).
func (m hitMap) offset(dx, dy int) hitMap {
	out := make(hitMap, len(m))
	for i, h := range m {
		h.x, h.y = h.x+dx, h.y+dy
		out[i] = h
	}
	return out
}
