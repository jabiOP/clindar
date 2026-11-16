// Package quickadd reads Google-style one-line events such as
// "Dentist 20/11 15:30 45m". The CLI and the TUI share it, so they agree.
package quickadd

import (
	"errors"
	"strings"
	"time"

	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/timeparse"
)

// Defaults are the days used when the line names none: the TUI's selected day
// (or a span dragged with the mouse), or today in the CLI. To may equal From.
type Defaults struct{ From, To time.Time }

type Result struct {
	Title      string
	Span       timeparse.Span
	Recurrence []string
	repeat     string // the repeat word, for Preview
}

var repeatWords = map[string]string{
	"daily": "every day", "weekdays": "every weekday", "weekly": "every week",
	"monthly": "every month", "yearly": "every year",
}

type clock struct{ h, m int }

// Parse reads "<title> <tokens>" (see the table in the plan, Task 14.14).
func Parse(line string, def Defaults, now time.Time, p timeparse.Parser) (Result, error) {
	words := strings.Fields(line)
	var (
		from, to   time.Time
		haveDate   bool
		haveRange  bool
		start, end *clock
		dur        time.Duration
		repeat     string
	)
	i := len(words)
scan:
	for ; i > 0; i-- {
		w := strings.ToLower(words[i-1])
		switch {
		case repeat == "" && repeatWords[w] != "":
			repeat = w
		case dur == 0 && isDuration(w):
			dur, _ = timeparse.ParseDuration(w)
		case start == nil && clockRange(w) != nil:
			r := clockRange(w)
			start, end = &r[0], &r[1]
		case start == nil && isClock(w):
			h, m, _ := timeparse.ParseClock(w)
			start = &clock{h, m}
		case !haveDate && !haveRange && isRange(p, w, now):
			from, to, _ = p.ParseDateRange(w, now)
			haveRange = true
		case !haveDate && !haveRange && isDate(p, w, now):
			from, _ = p.ParseDate(w, now)
			to, haveDate = from, true
		default:
			break scan
		}
	}
	r := Result{Title: strings.Join(words[:i], " "), repeat: repeat}
	if r.Title == "" {
		return r, errors.New("no title: start with the event's name, e.g. \"Dentist 20/11 15:30\"")
	}
	if !haveDate && !haveRange {
		from, to = def.From, def.To
	}
	at := func(c clock) time.Time {
		return time.Date(from.Year(), from.Month(), from.Day(), c.h, c.m, 0, 0, from.Location())
	}
	switch {
	case start != nil && haveRange:
		return r, errors.New("a date range is all-day: drop the time, or give one date")
	case start != nil && end != nil && dur > 0:
		return r, errors.New("give an end time or a duration, not both")
	case start != nil:
		s := at(*start)
		e := s.Add(time.Hour)
		if end != nil {
			if e = at(*end); !e.After(s) {
				e = e.AddDate(0, 0, 1) // 22:00-01:00 ends the next day
			}
		}
		if dur > 0 {
			e = s.Add(dur)
		}
		r.Span = timeparse.Span{Start: s, End: e}
	case dur > 0 && dur%(24*time.Hour) == 0:
		r.Span = timeparse.Span{Start: day(from), End: day(from).AddDate(0, 0, int(dur/(24*time.Hour))), AllDay: true}
	case dur > 0:
		return r, errors.New("a duration needs a start time, e.g. \"Nap 14:00 30m\"")
	default:
		r.Span = timeparse.Span{Start: day(from), End: day(to).AddDate(0, 0, 1), AllDay: true}
	}
	if repeat != "" {
		rec, err := recur.ParseRepeat(repeat, r.Span.Start, r.Span.AllDay, time.Time{}, 0)
		if err != nil {
			return r, err
		}
		r.Recurrence = rec
	}
	return r, nil
}

// RepeatWord is the repeat word the line ended with ("weekdays"), or "".
func (r Result) RepeatWord() string { return r.repeat }

// Preview describes the event, e.g. "Fri 20 Nov, 09:00–10:00, every weekday".
func (r Result) Preview() string {
	s := r.Span
	var out string
	switch {
	case s.AllDay && s.End.After(s.Start.AddDate(0, 0, 1)):
		out = s.Start.Format("Mon 02 Jan") + " – " + s.End.AddDate(0, 0, -1).Format("Mon 02 Jan") + ", all day"
	case s.AllDay:
		out = s.Start.Format("Mon 02 Jan") + ", all day"
	default:
		out = s.Start.Format("Mon 02 Jan, 15:04") + "–" + s.End.Format("15:04")
		if day(s.End) != day(s.Start) && !s.End.Equal(day(s.End)) {
			out += " (next day)"
		}
	}
	if r.repeat != "" {
		out += ", " + repeatWords[r.repeat]
	}
	return out
}

func day(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func isDuration(w string) bool { _, err := timeparse.ParseDuration(w); return err == nil }
func isClock(w string) bool    { _, _, ok := timeparse.ParseClock(w); return ok }

func isDate(p timeparse.Parser, w string, now time.Time) bool {
	_, ok := p.ParseDate(w, now)
	return ok
}
func isRange(p timeparse.Parser, w string, now time.Time) bool {
	_, _, ok := p.ParseDateRange(w, now)
	return ok
}

// clockRange reads "9:00-10:00"; nil if w isn't one.
func clockRange(w string) []clock {
	a, b, ok := strings.Cut(w, "-")
	if !ok {
		return nil
	}
	h1, m1, ok1 := timeparse.ParseClock(a)
	h2, m2, ok2 := timeparse.ParseClock(b)
	if !ok1 || !ok2 {
		return nil
	}
	return []clock{{h1, m1}, {h2, m2}}
}
