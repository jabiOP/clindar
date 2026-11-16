package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jabiOP/clindar/internal/model"
)

// Events cached before clindar read colors and Meet links (Tasks 14.19, 14.22)
// lack those fields, and incremental sync only resends events that changed. So
// the upgrade resets the sync tokens once: the next sync refetches everything.
func TestUpgradeForcesAFullResync(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "x.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.UpsertCalendar(ctx, model.Calendar{AccountID: "a", ID: "c", Name: "C"})
	s.SetCalendarSyncToken(ctx, "a", "c", "events-token")
	s.SetCalendarsToken(ctx, "a", "list-token")
	if _, err := s.db.ExecContext(ctx, "PRAGMA user_version = 3"); err != nil { // a database from before the upgrade
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if tok, _ := s.CalendarSyncToken(ctx, "a", "c"); tok != "" {
		t.Errorf("calendar token = %q, want empty (full resync)", tok)
	}
	if tok, _ := s.CalendarsToken(ctx, "a"); tok != "" {
		t.Errorf("calendar list token = %q, want empty", tok)
	}
	s.SetCalendarSyncToken(ctx, "a", "c", "new-token") // only once: later opens keep tokens
	s.Close()
	s2, _ := Open(path)
	defer s2.Close()
	if tok, _ := s2.CalendarSyncToken(ctx, "a", "c"); tok != "new-token" {
		t.Errorf("after a second open: token = %q, want it kept", tok)
	}
}
