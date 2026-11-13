// Package testutil holds helpers shared by tests in several packages.
// Only _test.go files may import it.
package testutil

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
)

// NewStore opens a fresh database in a temp dir, closed automatically.
func NewStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// NY is the zone most tests use. Loading it needs time/tzdata (imported by testutil's users or main).
func NY(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}
	return loc
}

// IDs used by fixtures.
const (
	Account  = "me@example.com"
	Calendar = "me@example.com"
)

// Timed returns a confirmed one-off event in the fixture account/calendar.
func Timed(id, title string, start time.Time, dur time.Duration) model.Event {
	return model.Event{
		AccountID: Account, CalendarID: Calendar, ID: id,
		Status: model.StatusConfirmed, Title: title,
		Start: start, End: start.Add(dur), TimeZone: start.Location().String(),
	}
}

// AllDay returns a confirmed all-day event covering `days` days from y-m-d.
func AllDay(id, title string, y int, m time.Month, d, days int) model.Event {
	start := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return model.Event{
		AccountID: Account, CalendarID: Calendar, ID: id,
		Status: model.StatusConfirmed, Title: title, AllDay: true,
		Start: start, End: start.AddDate(0, 0, days),
	}
}
