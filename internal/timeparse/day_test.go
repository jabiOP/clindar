package timeparse_test

import (
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/timeparse"
)

func TestParseDay(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny) // Thursday
	d := func(y, mo, day int) time.Time { return time.Date(y, time.Month(mo), day, 0, 0, 0, 0, ny) }
	tests := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{in: "fri", want: d(2026, 11, 13)},
		{in: "today", want: d(2026, 11, 12)},
		{in: "+2w", want: d(2026, 11, 26)},
		{in: "20/12", want: d(2026, 12, 20)},
		{in: "2026-12-24", want: d(2026, 12, 24)},
		{in: "dec", want: d(2026, 12, 1)},
		{in: "December", want: d(2026, 12, 1)},
		{in: "nov", want: d(2026, 11, 1)}, // this month counts
		{in: "oct", want: d(2027, 10, 1)},
		{in: "15", want: d(2026, 11, 15)},
		{in: "12", want: d(2026, 11, 12)}, // today counts
		{in: "5", want: d(2026, 12, 5)},
		{in: "31", want: d(2026, 12, 31)}, // November has no 31st
		{in: "14:00", wantErr: true},
		{in: "in 2h", wantErr: true},
		{in: "+90m", wantErr: true},
		{in: "now", wantErr: true},
		{in: "0", wantErr: true},
		{in: "32", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := timeparse.Parser{}.ParseDay(tt.in, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
