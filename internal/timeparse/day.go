package timeparse

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var months = map[string]time.Month{
	"jan": time.January, "january": time.January, "feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March, "apr": time.April, "april": time.April, "may": time.May,
	"jun": time.June, "june": time.June, "jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August, "sep": time.September, "sept": time.September,
	"september": time.September, "oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November, "dec": time.December, "december": time.December,
}

// ParseDay reads a day to go to: any date ParseTime accepts (today, fri, +2w,
// 20/12, 2026-12-24), a month name (the 1st of it, this month included), or a
// day number (the next such day, today included). Times are refused.
func (p Parser) ParseDay(input string, now time.Time) (time.Time, error) {
	s := strings.ToLower(strings.TrimSpace(input))
	today := startOfDay(now)
	if d, ok := p.parseDate(s, now); ok {
		return d, nil
	}
	if m, ok := months[s]; ok {
		y := today.Year()
		if m < today.Month() {
			y++
		}
		return time.Date(y, m, 1, 0, 0, 0, 0, now.Location()), nil
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 31 {
		for i := range 12 { // the next month that has a day n, starting with this one
			first := time.Date(today.Year(), today.Month()+time.Month(i), 1, 0, 0, 0, 0, now.Location())
			if d := first.AddDate(0, 0, n-1); d.Month() == first.Month() && !d.Before(today) {
				return d, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("%q isn't a day (try: fri, 20/12, dec, 15, +2w, 2026-12-24)", input)
}
