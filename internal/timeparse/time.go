package timeparse

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

// ParseTime parses input relative to now, returning a time in now's location.
// dateOnly is true when the input named a day but no time of day.
func ParseTime(input string, now time.Time) (t time.Time, dateOnly bool, err error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return time.Time{}, false, fmt.Errorf("empty time")
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.In(now.Location()), false, nil
	}
	s := strings.ToLower(raw)
	if s == "now" {
		return now, false, nil
	}
	if rel, ok := strings.CutPrefix(s, "in "); ok {
		return relative(rel, now, input)
	}
	if strings.HasPrefix(s, "+") && strings.ContainsAny(s, "hm") {
		return relative(s[1:], now, input)
	}
	// "2026-11-20t09:15" → "2026-11-20 09:15"
	if len(s) > 10 && s[4] == '-' && s[10] == 't' {
		s = s[:10] + " " + s[11:]
	}

	fields := strings.Fields(s)
	switch len(fields) {
	case 1:
		if d, ok := parseDate(fields[0], now); ok {
			return d, true, nil
		}
		if h, m, ok := parseClock(fields[0]); ok {
			return atClock(now, h, m), false, nil
		}
	case 2:
		d, okDate := parseDate(fields[0], now)
		h, m, okClock := parseClock(fields[1])
		if okDate && okClock {
			return atClock(d, h, m), false, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("cannot understand time %q (try: tomorrow 14:00, fri 2pm, 2026-11-20 09:15, in 2h)", input)
}

func relative(s string, now time.Time, input string) (time.Time, bool, error) {
	d, err := ParseDuration(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("cannot understand time %q: %w", input, err)
	}
	return now.Add(d), false, nil
}

// parseDate understands today/tomorrow/yesterday, weekday names, +Nd/+Nw, and YYYY-MM-DD.
func parseDate(s string, now time.Time) (time.Time, bool) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch s {
	case "today":
		return today, true
	case "tomorrow":
		return today.AddDate(0, 0, 1), true
	case "yesterday":
		return today.AddDate(0, 0, -1), true
	}
	if wd, ok := weekdays[s]; ok {
		ahead := (int(wd) - int(today.Weekday()) + 7) % 7
		return today.AddDate(0, 0, ahead), true
	}
	if len(s) >= 3 && s[0] == '+' {
		n, err := strconv.Atoi(s[1 : len(s)-1])
		if err == nil && n >= 0 {
			switch s[len(s)-1] {
			case 'd':
				return today.AddDate(0, 0, n), true
			case 'w':
				return today.AddDate(0, 0, 7*n), true
			}
		}
	}
	if d, err := time.ParseInLocation("2006-01-02", s, now.Location()); err == nil {
		return d, true
	}
	return time.Time{}, false
}

// parseClock understands "14:00", "9:30am", "2pm". A bare "14" is rejected as ambiguous.
func parseClock(s string) (h, m int, ok bool) {
	am, pm := strings.HasSuffix(s, "am"), strings.HasSuffix(s, "pm")
	if am || pm {
		s = s[:len(s)-2]
	}
	hs, ms, hasColon := strings.Cut(s, ":")
	if !hasColon && !am && !pm {
		return 0, 0, false
	}
	h, err := strconv.Atoi(hs)
	if err != nil || len(hs) == 0 || len(hs) > 2 {
		return 0, 0, false
	}
	if hasColon {
		if len(ms) != 2 {
			return 0, 0, false
		}
		if m, err = strconv.Atoi(ms); err != nil {
			return 0, 0, false
		}
	}
	if am || pm {
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		if h == 12 {
			h = 0
		}
		if pm {
			h += 12
		}
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func atClock(day time.Time, h, m int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, day.Location())
}
