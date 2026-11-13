package recur_test

import (
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestParseRepeat(t *testing.T) {
	ny := testutil.NY(t)
	fri := time.Date(2026, 11, 13, 9, 0, 0, 0, ny)
	tests := []struct {
		name    string
		spec    string
		until   time.Time
		count   int
		allDay  bool
		want    string // "" = no recurrence
		wantErr bool
	}{
		{name: "none", spec: ""},
		{name: "daily", spec: "daily", want: "RRULE:FREQ=DAILY"},
		{name: "weekly uses start weekday", spec: "weekly", want: "RRULE:FREQ=WEEKLY;BYDAY=FR"},
		{name: "weekdays", spec: "Weekdays", want: "RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{name: "count", spec: "daily", count: 5, want: "RRULE:FREQ=DAILY;COUNT=5"},
		{name: "until includes whole day", spec: "daily", until: time.Date(2026, 11, 20, 0, 0, 0, 0, ny),
			want: "RRULE:FREQ=DAILY;UNTIL=20261121T045959Z"},
		{name: "until all-day", spec: "daily", allDay: true, until: time.Date(2026, 11, 20, 0, 0, 0, 0, ny),
			want: "RRULE:FREQ=DAILY;UNTIL=20261120"},
		{name: "raw rule", spec: "freq=monthly;bymonthday=1", want: "RRULE:FREQ=MONTHLY;BYMONTHDAY=1"},
		{name: "raw with prefix", spec: "RRULE:FREQ=YEARLY", want: "RRULE:FREQ=YEARLY"},
		{name: "unknown word", spec: "fortnightly", wantErr: true},
		{name: "invalid raw", spec: "FREQ=SOMETIMES", wantErr: true},
		{name: "until and count", spec: "daily", count: 2, until: fri, wantErr: true},
		{name: "count without repeat", spec: "", count: 2, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := recur.ParseRepeat(tt.spec, fri, tt.allDay, tt.until, tt.count)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.want == "" {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("got %v, want [%s]", got, tt.want)
			}
		})
	}
}
