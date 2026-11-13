package recur

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jabiOP/clindar/internal/model"
)

var ErrSplitAtFirst = errors.New("split point is the first instance; edit the whole series instead")

// SplitAt divides a series at the instance originally starting at `at`.
// before keeps the series' instances strictly before `at`; after describes the
// remaining instances, for a new series starting at `at`. COUNT is shared out
// between the two; EXDATE/RDATE lines are copied to both (out-of-range ones are harmless).
func SplitAt(master model.Event, at time.Time) (before, after []string, err error) {
	set, err := ruleSet(master)
	if err != nil {
		return nil, nil, err
	}
	rr := set.GetRRule()
	if rr == nil {
		return nil, nil, fmt.Errorf("series %s has no RRULE", master.ID)
	}
	if !isSlot(set, at) {
		return nil, nil, ErrNoSuchInstance
	}
	// COUNT counts generated slots, including EXDATE'd ones, so count with the bare RRULE.
	used := len(rr.Between(master.Start, at.Add(-time.Second), true))
	if used == 0 {
		return nil, nil, ErrSplitAtFirst
	}
	for _, line := range master.Recurrence {
		body, isRule := strings.CutPrefix(line, "RRULE:")
		if !isRule {
			before = append(before, line)
			after = append(after, line)
			continue
		}
		parts := ruleParts(body)
		beforeRule := withPart(withoutPart(parts, "COUNT", "UNTIL"), "UNTIL", untilValue(at.Add(-time.Second), master.AllDay))
		afterRule := parts
		if c, ok := partValue(parts, "COUNT"); ok {
			total, err := strconv.Atoi(c)
			if err != nil {
				return nil, nil, fmt.Errorf("series %s: bad COUNT %q", master.ID, c)
			}
			afterRule = withPart(withoutPart(parts, "COUNT"), "COUNT", strconv.Itoa(total-used))
		}
		before = append(before, "RRULE:"+strings.Join(beforeRule, ";"))
		after = append(after, "RRULE:"+strings.Join(afterRule, ";"))
	}
	return before, after, nil
}

// untilValue formats t for an UNTIL clause: a UTC timestamp for timed series,
// a plain date for all-day ones (RFC 5545 requires UNTIL to match DTSTART's type).
func untilValue(t time.Time, allDay bool) string {
	if allDay {
		return t.UTC().Format("20060102")
	}
	return t.UTC().Format("20060102T150405Z")
}

func ruleParts(body string) []string { return strings.Split(body, ";") }

func partValue(parts []string, key string) (string, bool) {
	for _, p := range parts {
		if k, v, _ := strings.Cut(p, "="); strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

func withoutPart(parts []string, keys ...string) []string {
	var out []string
	for _, p := range parts {
		k, _, _ := strings.Cut(p, "=")
		drop := false
		for _, key := range keys {
			drop = drop || strings.EqualFold(k, key)
		}
		if !drop {
			out = append(out, p)
		}
	}
	return out
}

func withPart(parts []string, key, value string) []string {
	return append(withoutPart(parts, key), key+"="+value)
}
