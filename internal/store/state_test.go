package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestLastSyncZeroUntilSet(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	if got, err := s.LastSync(ctx, "a"); err != nil || !got.IsZero() {
		t.Fatalf("LastSync = %v, %v; want zero, nil", got, err)
	}
	when := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)

	if err := s.SetLastSync(ctx, "a", when); err != nil {
		t.Fatal(err)
	}

	if got, _ := s.LastSync(ctx, "a"); !got.Equal(when) {
		t.Errorf("LastSync = %v, want %v", got, when)
	}
}

func TestCalendarsTokenAndLastSyncAreIndependent(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	when := time.Unix(1_800_000_000, 0)
	s.SetLastSync(ctx, "a", when)

	s.SetCalendarsToken(ctx, "a", "tok")

	if tok, _ := s.CalendarsToken(ctx, "a"); tok != "tok" {
		t.Errorf("token = %q", tok)
	}
	if got, _ := s.LastSync(ctx, "a"); !got.Equal(when) {
		t.Errorf("LastSync clobbered: %v", got)
	}
}

func TestLogsNewestFirstAndCount(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	base := time.Unix(1_800_000_000, 0)
	for i, msg := range []string{"first", "second"} {
		s.AddLog(ctx, store.LogEntry{Time: base.Add(time.Duration(i) * time.Minute),
			Kind: store.LogDiscardedEdit, AccountID: "a", Message: msg})
	}

	logs, err := s.Logs(ctx, "a", 10)

	if err != nil || len(logs) != 2 || logs[0].Message != "second" {
		t.Fatalf("Logs = %+v, %v", logs, err)
	}
	n, _ := s.CountLogsSince(ctx, "a", store.LogDiscardedEdit, base.Add(30*time.Second))
	if n != 1 {
		t.Errorf("CountLogsSince = %d, want 1", n)
	}
}

func TestResetSyncTokens(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	s.UpsertCalendar(ctx, model.Calendar{AccountID: "a", ID: "c", Name: "C"})
	s.SetCalendarSyncToken(ctx, "a", "c", "events-token")
	s.SetCalendarsToken(ctx, "a", "list-token")

	if err := s.ResetSyncTokens(ctx, "a"); err != nil {
		t.Fatal(err)
	}

	if tok, _ := s.CalendarSyncToken(ctx, "a", "c"); tok != "" {
		t.Errorf("calendar token = %q, want empty", tok)
	}
	if tok, _ := s.CalendarsToken(ctx, "a"); tok != "" {
		t.Errorf("calendar list token = %q, want empty", tok)
	}
}
