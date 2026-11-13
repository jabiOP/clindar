package timeparse

import (
	"fmt"
	"time"
)

// Span is an event's timing as typed by a user: the result of interpreting
// the --at/--end/--dur/--all-day flags (CLI) or the Start/End/Duration fields (TUI).
type Span struct {
	Start, End time.Time
	AllDay     bool
}

// ParseSpan interprets the timing inputs. --end is parsed relative to the
// start ("--at 'fri 14:00' --end 15:30" means Friday 15:30). For all-day
// events --end is the last day, inclusive.
func ParseSpan(now time.Time, at, end, dur string, allDayFlag bool) (Span, error) {
	if at == "" {
		return Span{}, fmt.Errorf("a start time is required")
	}
	if end != "" && dur != "" {
		return Span{}, fmt.Errorf("give an end or a duration, not both")
	}
	start, dateOnly, err := ParseTime(at, now)
	if err != nil {
		return Span{}, fmt.Errorf("start: %w", err)
	}
	t := Span{Start: start, AllDay: allDayFlag || dateOnly}
	if t.AllDay {
		t.Start = startOfDay(start)
	}
	switch {
	case end != "":
		e, _, err := ParseTime(end, t.Start)
		if err != nil {
			return Span{}, fmt.Errorf("end: %w", err)
		}
		if t.AllDay {
			e = startOfDay(e).AddDate(0, 0, 1) // inclusive last day → exclusive end
		}
		t.End = e
	case dur != "":
		d, err := ParseDuration(dur)
		if err != nil {
			return Span{}, fmt.Errorf("duration: %w", err)
		}
		t.End = t.Start.Add(d)
	case t.AllDay:
		t.End = t.Start.AddDate(0, 0, 1)
	default:
		t.End = t.Start.Add(time.Hour)
	}
	if !t.End.After(t.Start) {
		return Span{}, fmt.Errorf("end must be after start")
	}
	return t, nil
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
