package recur

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

var weekdayCodes = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// ParseRepeat turns a --repeat value into recurrence lines for a series
// starting at start. spec is daily | weekdays | weekly | monthly | yearly, or a
// raw rule ("FREQ=WEEKLY;BYDAY=MO,TH", optionally prefixed "RRULE:").
// until (zero = none) and count (0 = none) bound the series; not both.
func ParseRepeat(spec string, start time.Time, allDay bool, until time.Time, count int) ([]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		if !until.IsZero() || count > 0 {
			return nil, fmt.Errorf("--until/--count need --repeat")
		}
		return nil, nil
	}
	var parts []string
	switch strings.ToLower(spec) {
	case "daily":
		parts = []string{"FREQ=DAILY"}
	case "weekdays":
		parts = []string{"FREQ=WEEKLY", "BYDAY=MO,TU,WE,TH,FR"}
	case "weekly":
		parts = []string{"FREQ=WEEKLY", "BYDAY=" + weekdayCodes[start.Weekday()]}
	case "monthly":
		parts = []string{"FREQ=MONTHLY"}
	case "yearly":
		parts = []string{"FREQ=YEARLY"}
	default:
		body := strings.TrimPrefix(strings.ToUpper(spec), "RRULE:")
		if !strings.HasPrefix(body, "FREQ=") {
			return nil, fmt.Errorf("unknown repeat %q (use daily, weekdays, weekly, monthly, yearly, or FREQ=...)", spec)
		}
		parts = ruleParts(body)
	}
	switch {
	case !until.IsZero() && count > 0:
		return nil, fmt.Errorf("use --until or --count, not both")
	case !until.IsZero():
		// Include the whole "until" day.
		endOfDay := time.Date(until.Year(), until.Month(), until.Day(), 23, 59, 59, 0, until.Location())
		if allDay {
			endOfDay = time.Date(until.Year(), until.Month(), until.Day(), 0, 0, 0, 0, time.UTC)
		}
		parts = withPart(parts, "UNTIL", untilValue(endOfDay, allDay))
	case count > 0:
		parts = withPart(parts, "COUNT", strconv.Itoa(count))
	}
	body := strings.Join(parts, ";")
	if _, err := rrule.StrToROption(body); err != nil {
		return nil, fmt.Errorf("invalid repeat rule %q: %w", body, err)
	}
	return []string{"RRULE:" + body}, nil
}
