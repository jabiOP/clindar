package output_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jabiOP/clindar/internal/gcal/gcaltest"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/output"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii) // no ANSI colors in expected output
	os.Exit(m.Run())
}

var cals = []model.Calendar{{ID: testutil.Calendar, Name: "Me"}}

func sample(t *testing.T) []model.Event {
	ny := testutil.NY(t)
	standup := testutil.Timed("std_20261116T140000Z", "Standup", time.Date(2026, 11, 16, 9, 0, 0, 0, ny), 15*time.Minute)
	standup.SeriesID = "std"
	report := testutil.Timed("r1", "Write\treport", time.Date(2026, 11, 16, 14, 0, 0, 0, ny), time.Hour)
	holiday := testutil.AllDay("h1", "Holiday", 2026, time.November, 17, 1)
	return []model.Event{standup, report, holiday}
}

func render(t *testing.T, mode output.Mode, format string, fn func(p *output.Printer) error) string {
	t.Helper()
	var buf bytes.Buffer
	p, err := output.NewPrinter(&buf, mode, format, testutil.NY(t), cals)
	if err != nil {
		t.Fatal(err)
	}
	if err := fn(p); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestChooseMode(t *testing.T) {
	tests := []struct {
		json, tsv bool
		format    string
		tty       bool
		want      output.Mode
		wantErr   bool
	}{
		{tty: true, want: output.Pretty},
		{tty: false, want: output.TSV},
		{json: true, tty: true, want: output.JSON},
		{format: "{{.Title}}", want: output.Template},
		{json: true, tsv: true, wantErr: true},
	}
	for _, tt := range tests {
		got, err := output.ChooseMode(tt.json, tt.tsv, tt.format, tt.tty)
		if (err != nil) != tt.wantErr || (!tt.wantErr && got != tt.want) {
			t.Errorf("ChooseMode(%+v) = %v, %v", tt, got, err)
		}
	}
}

func TestTSVIsStableAndOneLinePerEvent(t *testing.T) {
	ny := testutil.NY(t)
	got := render(t, output.TSV, "", func(p *output.Printer) error {
		return p.Events(sample(t), time.Date(2026, 11, 16, 0, 0, 0, 0, ny), 7)
	})

	want := "std_20261116T140000Z\t2026-11-16T09:00:00-05:00\t2026-11-16T09:15:00-05:00\t0\tMe\tStandup\n" +
		"r1\t2026-11-16T14:00:00-05:00\t2026-11-16T15:00:00-05:00\t0\tMe\tWrite report\n" +
		"h1\t2026-11-17\t2026-11-18\t1\tMe\tHoliday\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJSONLines(t *testing.T) {
	ny := testutil.NY(t)
	got := render(t, output.JSON, "", func(p *output.Printer) error {
		return p.Events(sample(t)[:1], time.Date(2026, 11, 16, 0, 0, 0, 0, ny), 1)
	})

	var j output.EventJSON
	if err := json.Unmarshal([]byte(got), &j); err != nil {
		t.Fatalf("not valid JSON: %q: %v", got, err)
	}
	if j.ID != "std_20261116T140000Z" || j.Calendar != "Me" || !j.Recurring || j.Start != "2026-11-16T09:00:00-05:00" {
		t.Errorf("got %+v", j)
	}
}

func TestTemplate(t *testing.T) {
	ny := testutil.NY(t)
	got := render(t, output.Template, `{{time "15:04" .Start}} {{.Title}}`, func(p *output.Printer) error {
		return p.Events(sample(t)[:1], time.Date(2026, 11, 16, 0, 0, 0, 0, ny), 1)
	})

	if got != "09:00 Standup\n" {
		t.Errorf("got %q", got)
	}
}

func TestPrettyAgendaGroupsByDay(t *testing.T) {
	ny := testutil.NY(t)
	got := render(t, output.Pretty, "", func(p *output.Printer) error {
		return p.Events(sample(t), time.Date(2026, 11, 16, 0, 0, 0, 0, ny), 3)
	})

	want := "Mon 16 Nov\n" +
		"  09:00–09:15  Standup  ● Me\n" +
		"  14:00–15:00  Write report  ● Me\n" + // tabs/newlines in titles are flattened
		"Tue 17 Nov\n" +
		"  all day      Holiday  ● Me\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestNextIncludesMinutes(t *testing.T) {
	ny := testutil.NY(t)
	e := sample(t)[0]
	now := time.Date(2026, 11, 16, 8, 48, 0, 0, ny)

	pretty := render(t, output.Pretty, "", func(p *output.Printer) error { return p.Next(e, now) })
	jsonOut := render(t, output.JSON, "", func(p *output.Printer) error { return p.Next(e, now) })

	if pretty != "in 12m  09:00–09:15  Standup\n" {
		t.Errorf("pretty = %q", pretty)
	}
	var j output.EventJSON
	json.Unmarshal([]byte(jsonOut), &j)
	if j.StartsInMinutes == nil || *j.StartsInMinutes != 12 {
		t.Errorf("starts_in_minutes = %v", j.StartsInMinutes)
	}
}

func TestPrettyDetailShowsWholeSpanAndPlainRepeats(t *testing.T) {
	ny := testutil.NY(t)
	trip := testutil.AllDay("t", "Trip", 2026, time.November, 18, 5) // Wed 18 – Sun 22
	overnight := testutil.Timed("o", "Night shift", time.Date(2026, 11, 16, 22, 0, 0, 0, ny), 4*time.Hour)
	weekly := sample(t)[0] // an occurrence of a series

	tests := []struct {
		e    model.Event
		want string
	}{
		{trip, "When:      Wed 18 Nov 2026 – Sun 22 Nov 2026  all day\n"},
		{overnight, "When:      Mon 16 Nov 2026 22:00 – Tue 17 Nov 2026 02:00\n"},
		{weekly, "When:      Mon 16 Nov 2026  09:00–09:15\n"},
		{weekly, "Repeats:   yes\n"}, // shared by the CLI and the TUI: no command-line hint
	}
	for _, tt := range tests {
		got := render(t, output.Pretty, "", func(p *output.Printer) error { return p.Event(tt.e) })
		if !strings.Contains(got, tt.want) {
			t.Errorf("%s: want line %q in:\n%s", tt.e.Title, tt.want, got)
		}
	}
}

func TestPrettyDetailWrapsValuesUnderThemselves(t *testing.T) {
	e := sample(t)[0]
	e.Attendees = []model.Attendee{
		{Email: "alice@example.com", Response: model.ResponseAccepted},
		{Email: "bob@example.com", Response: model.ResponseNeedsAction},
		{Email: "carol@example.com", Response: model.ResponseDeclined},
	}
	var buf bytes.Buffer
	p, err := output.NewPrinter(&buf, output.Pretty, "", testutil.NY(t), cals)
	if err != nil {
		t.Fatal(err)
	}
	p.SetWidth(40)

	if err := p.Event(e); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	inGuests := false
	continued := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > 40 {
			t.Errorf("line wider than 40 (%d): %q", w, l)
		}
		switch {
		case strings.HasPrefix(l, "Guests:"):
			inGuests = true
		case inGuests && strings.HasPrefix(l, strings.Repeat(" ", 11)):
			continued++
		default:
			inGuests = false
		}
	}
	if continued == 0 {
		t.Errorf("Guests should wrap onto indented lines:\n%s", buf.String())
	}
}

func TestJSONHasTheEffectiveColor(t *testing.T) {
	ny := testutil.NY(t)
	own := sample(t)[1]
	own.ColorID = "11"
	plain := sample(t)[2]
	colored := []model.Calendar{{ID: testutil.Calendar, Name: "Me", Color: "#4285f4"}}
	var buf bytes.Buffer
	p, err := output.NewPrinter(&buf, output.JSON, "", ny, colored)
	if err != nil {
		t.Fatal(err)
	}
	p.SetPalette(gcaltest.Palette)

	if err := p.Events([]model.Event{own, plain}, time.Date(2026, 11, 16, 0, 0, 0, 0, ny), 2); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var a, b output.EventJSON
	json.Unmarshal([]byte(lines[0]), &a)
	json.Unmarshal([]byte(lines[1]), &b)
	if a.Color != "#d50000" || b.Color != "#4285f4" {
		t.Errorf("colors = %q, %q; want the event's own #d50000, then the calendar's #4285f4", a.Color, b.Color)
	}
}

func TestOmitInternalsDropsIDAndLink(t *testing.T) {
	e := sample(t)[1]
	e.Link = "https://www.google.com/calendar/event?eid=abc"
	for _, omit := range []bool{false, true} {
		var buf bytes.Buffer
		p, err := output.NewPrinter(&buf, output.Pretty, "", testutil.NY(t), cals)
		if err != nil {
			t.Fatal(err)
		}
		if omit {
			p.OmitInternals()
		}
		p.Event(e)
		has := strings.Contains(buf.String(), "ID:") || strings.Contains(buf.String(), "Link:")
		if has == omit {
			t.Errorf("OmitInternals=%v: ID/Link shown = %v:\n%s", omit, has, buf.String())
		}
	}
}

func TestMeetLinkInDetailAndJSON(t *testing.T) {
	e := sample(t)[1]
	e.MeetLink = "https://meet.google.com/abc-defg-hij"
	pretty := render(t, output.Pretty, "", func(p *output.Printer) error { return p.Event(e) })
	jsonOut := render(t, output.JSON, "", func(p *output.Printer) error { return p.Event(e) })

	if !strings.Contains(pretty, "Meet:") || !strings.Contains(pretty, e.MeetLink) {
		t.Errorf("pretty detail:\n%s", pretty)
	}
	if !strings.Contains(jsonOut, `"meet_link":"https://meet.google.com/abc-defg-hij"`) {
		t.Errorf("json: %s", jsonOut)
	}
}
