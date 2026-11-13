// Package timeparse turns the human date/time/duration strings accepted by the
// CLI and TUI forms into time values. It never calls time.Now: callers pass now.
package timeparse

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var units = map[byte]time.Duration{
	'w': 7 * 24 * time.Hour,
	'd': 24 * time.Hour,
	'h': time.Hour,
	'm': time.Minute,
}

// ParseDuration parses sequences like "30m", "1h30m", "2d", "1w".
func ParseDuration(input string) (time.Duration, error) {
	s := strings.ToLower(strings.TrimSpace(input))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	var total time.Duration
	for s != "" {
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 || i == len(s) {
			return 0, fmt.Errorf("invalid duration %q (try 30m, 1h30m, 2d)", input)
		}
		unit, ok := units[s[i]]
		if !ok {
			return 0, fmt.Errorf("invalid duration %q: unknown unit %q (use w, d, h, m)", input, s[i])
		}
		n, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", input, err)
		}
		total += time.Duration(n) * unit
		s = s[i+1:]
	}
	return total, nil
}
