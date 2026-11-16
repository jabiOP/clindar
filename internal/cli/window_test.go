package cli

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/timeparse"
)

func TestAgendaWindow(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny) // Thursday
	d := func(day, h int) time.Time { return time.Date(2026, 11, day, h, 0, 0, 0, ny) }
	tests := []struct {
		name     string
		f        windowFlags
		from, to time.Time
		days     int
		wantErr  bool
	}{
		{name: "default is a week from today", from: d(12, 0), to: d(19, 0), days: 7},
		{name: "today", f: windowFlags{today: true}, from: d(12, 0), to: d(13, 0), days: 1},
		{name: "from+days", f: windowFlags{from: "mon", days: 5}, from: d(16, 0), to: d(21, 0), days: 5},
		{name: "to date is inclusive", f: windowFlags{from: "fri", to: "sun"}, from: d(13, 0), to: d(16, 0), days: 3},
		{name: "within", f: windowFlags{within: "90m"}, from: now, to: now.Add(90 * time.Minute), days: 1},
		{name: "to before from", f: windowFlags{from: "fri", to: "thu"}, wantErr: true},
		{name: "within with today", f: windowFlags{within: "1h", today: true}, wantErr: true},
		{name: "to and days", f: windowFlags{to: "fri", days: 2}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := agendaWindow(now, tt.f, timeparse.Parser{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !w.from.Equal(tt.from) || !w.to.Equal(tt.to) || w.days() != tt.days {
				t.Errorf("got [%v, %v) %d days; want [%v, %v) %d days", w.from, w.to, w.days(), tt.from, tt.to, tt.days)
			}
		})
	}
}
