package tui

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
)

var (
	errNoChange = errors.New("dropped where it already is")
	errInverted = errors.New("it would end before it starts")
)

// dragChanges turns a drop into an edit (design: mouse §2). from is the day the
// event was grabbed on, to the day it was dropped on. Clock times are kept;
// only days change.
//
//	body: move the whole event by (to - from) days
//	end:  the last day becomes to (not before the first day)
//	start: the first day becomes to (not after the last day)
func dragChanges(e model.Event, p part, from, to time.Time, loc *time.Location) (service.Changes, error) {
	oldStart, oldEnd := e.Span(loc)
	start, end := oldStart, oldEnd
	first, last := eventDays(e, loc)
	switch p {
	case partBody:
		n := daysBetween(from, to)
		start, end = start.AddDate(0, 0, n), end.AddDate(0, 0, n)
	case partEnd:
		newLast := to
		if newLast.Before(first) {
			newLast = first // clamp: a one-day event, never flipped around
		}
		end = end.AddDate(0, 0, daysBetween(last, newLast))
	case partStart:
		newFirst := to
		if newFirst.After(last) {
			newFirst = last
		}
		start = start.AddDate(0, 0, daysBetween(first, newFirst))
	}
	switch {
	case start.Equal(oldStart) && end.Equal(oldEnd):
		return service.Changes{}, errNoChange
	case !end.After(start): // e.g. Fri 20:00–Sun 10:00 with its start dragged to Sun
		return service.Changes{}, errInverted
	}
	return service.Changes{Start: &start, End: &end}, nil
}

// daysBetween counts calendar days from a to b (both local midnights).
// Rounding absorbs the 23- and 25-hour days around DST changes.
func daysBetween(a, b time.Time) int {
	return int(math.Round(b.Sub(a).Hours() / 24))
}

// canDrag says whether the user may move this event, and if not, why.
// Guests can't change an event's time; only its organizer can.
func canDrag(e model.Event, cal model.Calendar) (bool, string) {
	if !cal.Writable() {
		return false, cal.Name + " is read-only"
	}
	if self := e.SelfAttendee(); self != nil && !self.Organizer {
		return false, "you're a guest: only the organizer can move it"
	}
	return true, ""
}

// allowed says which detail actions can succeed for e, and why editing can't.
type allowed struct {
	edit, delete, rsvp bool
	whyNoEdit          string
}

func permissions(e model.Event, cal model.Calendar) allowed {
	edit, why := canDrag(e, cal)
	return allowed{edit: edit, delete: cal.Writable(), rsvp: e.SelfAttendee() != nil, whyNoEdit: why}
}

// dragPreview is the status-bar text while dragging, e.g. `Move "Trip" → Sat 21–Sun 22`.
func dragPreview(e model.Event, p part, ch service.Changes, loc *time.Location) string {
	verb := "Resize"
	if p == partBody {
		verb = "Move"
	}
	moved := e.Clone()
	moved.Start, moved.End = *ch.Start, *ch.End
	first, last := eventDays(moved, loc)
	days := first.Format("Mon 02")
	if !last.Equal(first) {
		days += "–" + last.Format("Mon 02")
	}
	return fmt.Sprintf("%s %q → %s", verb, e.Title, days)
}
