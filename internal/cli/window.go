package cli

import (
	"fmt"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/timeparse"
)

// window is the time range an agenda command covers.
type window struct {
	from, to     time.Time
	startingOnly bool // --within: only events that start in the window
}

// days is how many calendar days the window touches (for pretty grouping).
func (w window) days() int {
	n := 0
	for d := model.StartOfDay(w.from); d.Before(w.to); d = d.AddDate(0, 0, 1) {
		n++
	}
	return n
}

type windowFlags struct {
	from, to, within string
	days             int
	today            bool
}

// agendaWindow turns agenda flags into a window. Default: today + 6 days.
// --to with a date (no time) includes that whole day.
func agendaWindow(now time.Time, f windowFlags, dates timeparse.Parser) (window, error) {
	today := model.StartOfDay(now)
	switch {
	case f.within != "":
		if f.from != "" || f.to != "" || f.today || f.days != 0 {
			return window{}, fmt.Errorf("--within can't be combined with other range flags")
		}
		d, err := timeparse.ParseDuration(f.within)
		if err != nil {
			return window{}, err
		}
		return window{from: now, to: now.Add(d), startingOnly: true}, nil
	case f.today:
		if f.from != "" || f.to != "" || f.days != 0 {
			return window{}, fmt.Errorf("--today can't be combined with other range flags")
		}
		return window{from: today, to: today.AddDate(0, 0, 1)}, nil
	}
	if f.to != "" && f.days != 0 {
		return window{}, fmt.Errorf("use --to or --days, not both")
	}
	from := today
	if f.from != "" {
		t, _, err := dates.ParseTime(f.from, now)
		if err != nil {
			return window{}, fmt.Errorf("--from: %w", err)
		}
		from = t
	}
	if f.to != "" {
		t, dateOnly, err := dates.ParseTime(f.to, now)
		if err != nil {
			return window{}, fmt.Errorf("--to: %w", err)
		}
		if dateOnly {
			t = t.AddDate(0, 0, 1)
		}
		if !t.After(from) {
			return window{}, fmt.Errorf("--to must be after --from")
		}
		return window{from: from, to: t}, nil
	}
	days := f.days
	if days == 0 {
		days = 7
	}
	if days < 0 {
		return window{}, fmt.Errorf("--days must be positive")
	}
	return window{from: from, to: model.StartOfDay(from).AddDate(0, 0, days)}, nil
}
