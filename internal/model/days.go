package model

import (
	"cmp"
	"slices"
	"time"
)

// SortEvents orders events for display: by the day they start (in loc), then
// all-day before timed within that day, then by start, then title. Day comes
// first because scripts read TSV/JSON in this order.
func SortEvents(events []Event, loc *time.Location) {
	slices.SortStableFunc(events, func(a, b Event) int {
		as, _ := a.Span(loc)
		bs, _ := b.Span(loc)
		if c := StartOfDay(as).Compare(StartOfDay(bs)); c != 0 {
			return c
		}
		if a.AllDay != b.AllDay {
			if a.AllDay {
				return -1
			}
			return 1
		}
		if c := as.Compare(bs); c != 0 {
			return c
		}
		return cmp.Compare(a.Title, b.Title)
	})
}

// Day is one calendar day (local midnight) and the events touching it.
type Day struct {
	Date   time.Time
	Events []Event
}

// GroupByDay buckets events into n consecutive days starting with the day
// containing from (in from's location). Multi-day events appear on each day
// they touch. Input order is preserved within a day, so sort first.
func GroupByDay(events []Event, from time.Time, n int) []Day {
	start := StartOfDay(from)
	days := make([]Day, n)
	for i := range days {
		d := start.AddDate(0, 0, i) // AddDate, not Add(24h): DST days aren't 24h long
		next := d.AddDate(0, 0, 1)
		days[i].Date = d
		for _, e := range events {
			if e.Overlaps(d, next) {
				days[i].Events = append(days[i].Events, e)
			}
		}
	}
	return days
}
