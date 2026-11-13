package timeparse_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/timeparse"
)

func TestParseTime(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny) // a Thursday
	d := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, ny) }

	tests := []struct {
		in           string
		want         time.Time
		wantDateOnly bool
		wantErr      bool
	}{
		{in: "now", want: now},
		{in: "today", want: d(12, 0, 0), wantDateOnly: true},
		{in: "Tomorrow", want: d(13, 0, 0), wantDateOnly: true},
		{in: "yesterday", want: d(11, 0, 0), wantDateOnly: true},
		{in: "thu", want: d(12, 0, 0), wantDateOnly: true}, // today counts
		{in: "fri", want: d(13, 0, 0), wantDateOnly: true},
		{in: "wednesday", want: d(18, 0, 0), wantDateOnly: true},
		{in: "+3d", want: d(15, 0, 0), wantDateOnly: true},
		{in: "+1w", want: d(19, 0, 0), wantDateOnly: true},
		{in: "2026-11-20", want: d(20, 0, 0), wantDateOnly: true},
		{in: "14:00", want: d(12, 14, 0)},
		{in: "2pm", want: d(12, 14, 0)},
		{in: "9:30am", want: d(12, 9, 30)},
		{in: "12am", want: d(12, 0, 0)},
		{in: "12pm", want: d(12, 12, 0)},
		{in: "tomorrow 9am", want: d(13, 9, 0)},
		{in: "fri 14:30", want: d(13, 14, 30)},
		{in: "2026-11-20 09:15", want: d(20, 9, 15)},
		{in: "2026-11-20T09:15", want: d(20, 9, 15)},
		{in: "2026-11-20T14:15:00Z", want: d(20, 9, 15)},
		{in: "in 2h", want: d(12, 12, 30)},
		{in: "+90m", want: d(12, 12, 0)},
		{in: "", wantErr: true},
		{in: "blah", wantErr: true},
		{in: "25:00", wantErr: true},
		{in: "13pm", wantErr: true},
		{in: "fri 99", wantErr: true},
		{in: "14", wantErr: true}, // ambiguous: require "14:00" or "2pm"
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, dateOnly, err := timeparse.ParseTime(tt.in, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTime(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("ParseTime(%q) = %v, want %v", tt.in, got, tt.want)
			}
			if dateOnly != tt.wantDateOnly {
				t.Errorf("ParseTime(%q) dateOnly = %v, want %v", tt.in, dateOnly, tt.wantDateOnly)
			}
			if got.Location() != ny {
				t.Errorf("ParseTime(%q) location = %v, want now's location", tt.in, got.Location())
			}
		})
	}
}
