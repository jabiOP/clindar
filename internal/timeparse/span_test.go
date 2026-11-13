package timeparse_test

import (
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/timeparse"
)

func TestParseSpan(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny)
	d := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, ny) }
	tests := []struct {
		name                string
		at, end, dur        string
		allDay              bool
		wantStart, wantEnd  time.Time
		wantAllDay, wantErr bool
	}{
		{name: "default hour", at: "fri 14:00", wantStart: d(13, 14, 0), wantEnd: d(13, 15, 0)},
		{name: "end is on the start's day", at: "fri 14:00", end: "15:30", wantStart: d(13, 14, 0), wantEnd: d(13, 15, 30)},
		{name: "duration", at: "fri 14:00", dur: "45m", wantStart: d(13, 14, 0), wantEnd: d(13, 14, 45)},
		{name: "date only is all-day", at: "fri", wantStart: d(13, 0, 0), wantEnd: d(14, 0, 0), wantAllDay: true},
		{name: "all-day end inclusive", at: "fri", end: "sun", wantStart: d(13, 0, 0), wantEnd: d(16, 0, 0), wantAllDay: true},
		{name: "end before start", at: "fri 14:00", end: "13:00", wantErr: true},
		{name: "missing at", wantErr: true},
		{name: "end and dur", at: "fri", end: "sat", dur: "1d", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := timeparse.ParseSpan(now, tt.at, tt.end, tt.dur, tt.allDay)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !got.Start.Equal(tt.wantStart) || !got.End.Equal(tt.wantEnd) || got.AllDay != tt.wantAllDay {
				t.Errorf("got %v → %v allDay=%v", got.Start, got.End, got.AllDay)
			}
		})
	}
}
