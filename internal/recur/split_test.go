package recur_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestSplitAt(t *testing.T) {
	ny := testutil.NY(t)
	split := time.Date(2026, 11, 16, 9, 0, 0, 0, ny) // third Monday, 14:00Z
	tests := []struct {
		name       string
		recurrence []string
		wantBefore []string
		wantAfter  []string
	}{
		{"open-ended",
			[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO"},
			[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO;UNTIL=20261116T135959Z"},
			[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO"}},
		{"count is shared out, exdates copied",
			[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=5", "EXDATE;TZID=America/New_York:20261109T090000"},
			[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO;UNTIL=20261116T135959Z", "EXDATE;TZID=America/New_York:20261109T090000"},
			[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=3", "EXDATE;TZID=America/New_York:20261109T090000"}},
		{"existing until kept on the new series",
			[]string{"RRULE:FREQ=WEEKLY;UNTIL=20261231T000000Z;BYDAY=MO"},
			[]string{"RRULE:FREQ=WEEKLY;BYDAY=MO;UNTIL=20261116T135959Z"},
			[]string{"RRULE:FREQ=WEEKLY;UNTIL=20261231T000000Z;BYDAY=MO"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := weeklyStandup(t, "")
			m.Recurrence = tt.recurrence

			before, after, err := recur.SplitAt(m, split)

			if err != nil {
				t.Fatal(err)
			}
			if !equal(before, tt.wantBefore) {
				t.Errorf("before = %v\nwant     %v", before, tt.wantBefore)
			}
			if !equal(after, tt.wantAfter) {
				t.Errorf("after = %v\nwant    %v", after, tt.wantAfter)
			}
		})
	}
}

func TestSplitAtAllDayUsesDateUntil(t *testing.T) {
	m := testutil.AllDay("d", "Daily", 2026, time.November, 1, 1)
	m.Recurrence = []string{"RRULE:FREQ=DAILY"}

	before, _, err := recur.SplitAt(m, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))

	if err != nil || before[0] != "RRULE:FREQ=DAILY;UNTIL=20261104" {
		t.Errorf("before = %v, err = %v", before, err)
	}
}

func TestSplitAtErrors(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO")

	if _, _, err := recur.SplitAt(m, m.Start); !errors.Is(err, recur.ErrSplitAtFirst) {
		t.Errorf("split at first: err = %v, want ErrSplitAtFirst", err)
	}
	notSlot := time.Date(2026, 11, 17, 9, 0, 0, 0, ny) // a Tuesday
	if _, _, err := recur.SplitAt(m, notSlot); !errors.Is(err, recur.ErrNoSuchInstance) {
		t.Errorf("split at non-slot: err = %v, want ErrNoSuchInstance", err)
	}
}
