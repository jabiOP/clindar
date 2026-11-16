package quickadd_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/quickadd"
	"github.com/jabiOP/clindar/internal/timeparse"
)

func TestParse(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny)
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, ny) }
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, ny) }
	today := quickadd.Defaults{From: day(12), To: day(12)}
	tests := []struct {
		line       string
		def        quickadd.Defaults
		title      string
		start, end time.Time
		allDay     bool
		rule       string // first recurrence line, "" = none
		wantErr    bool
	}{
		{line: "Event 9:00-10:00", def: today, title: "Event", start: at(12, 9, 0), end: at(12, 10, 0)},
		{line: "Lunch with Sam fri 13:00", def: today, title: "Lunch with Sam", start: at(13, 13, 0), end: at(13, 14, 0)},
		{line: "Dentist 20/11 15:30 45m", def: today, title: "Dentist", start: at(20, 15, 30), end: at(20, 16, 15)},
		{line: "Trip 20/11-23/11", def: today, title: "Trip", start: day(20), end: day(24), allDay: true},
		{line: "Standup weekdays 9:00 15m", def: today, title: "Standup", start: at(12, 9, 0), end: at(12, 9, 15),
			rule: "RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
		{line: "Call mom", def: today, title: "Call mom", start: day(12), end: day(13), allDay: true},
		{line: "Report Q3 2026", def: today, title: "Report Q3 2026", start: day(12), end: day(13), allDay: true},
		{line: "Gym tomorrow 7am weekly", def: today, title: "Gym", start: at(13, 7, 0), end: at(13, 8, 0),
			rule: "RRULE:FREQ=WEEKLY;BYDAY=FR"},
		{line: "Party 22:00-01:00", def: today, title: "Party", start: at(12, 22, 0), end: at(13, 1, 0)},
		{line: "Off 2d", def: today, title: "Off", start: day(12), end: day(14), allDay: true},
		{line: "Conference", def: quickadd.Defaults{From: day(17), To: day(19)}, title: "Conference",
			start: day(17), end: day(20), allDay: true}, // a span dragged with the mouse
		{line: "9:00", def: today, wantErr: true},                  // no title
		{line: "Nap 30m", def: today, wantErr: true},               // a duration needs a start time
		{line: "Trip 20/11-23/11 9:00", def: today, wantErr: true}, // a date range is all-day
		{line: "Thing 9:00-10:00 30m", def: today, wantErr: true},  // an end and a duration
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			r, err := quickadd.Parse(tt.line, tt.def, now, timeparse.Parser{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if r.Title != tt.title || !r.Span.Start.Equal(tt.start) || !r.Span.End.Equal(tt.end) || r.Span.AllDay != tt.allDay {
				t.Errorf("got %q %v→%v allDay=%v", r.Title, r.Span.Start, r.Span.End, r.Span.AllDay)
			}
			if got := first(r.Recurrence); got != tt.rule {
				t.Errorf("rule = %q, want %q", got, tt.rule)
			}
		})
	}
}

func first(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

func TestPreview(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 11, 12, 10, 30, 0, 0, ny)
	def := quickadd.Defaults{From: time.Date(2026, 11, 12, 0, 0, 0, 0, ny), To: time.Date(2026, 11, 12, 0, 0, 0, 0, ny)}
	for line, want := range map[string]string{
		"Standup weekdays 9:00 15m": "Thu 12 Nov, 09:00–09:15, every weekday",
		"Trip 20/11-23/11":          "Fri 20 Nov – Mon 23 Nov, all day",
		"Party 22:00-01:00":         "Thu 12 Nov, 22:00–01:00 (next day)",
	} {
		r, err := quickadd.Parse(line, def, now, timeparse.Parser{})
		if err != nil || r.Preview() != want {
			t.Errorf("%q: preview %q, %v; want %q", line, r.Preview(), err, want)
		}
	}
}
