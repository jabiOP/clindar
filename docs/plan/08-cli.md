# Phase 8: Output and CLI

By the end of this phase, `clindar` works end to end against your real Google account. **This is the first
milestone worth demoing.**

Principles (design §5):
- Commands are **thin**: parse flags, call the service, print with `internal/output`. Logic in a command
  file is a smell. Move it down a layer.
- **stdout is for data, stderr is for humans.** `add` prints only the new ID on stdout, so
  `id=$(clindar add …)` works.
- Exit codes: 0 ok, 1 error or nothing found, 2 usage error.
- On a terminal, print pretty output. In a pipe, print TSV. `--json`, `--tsv`, and `--format` force a mode.

```bash
go get github.com/spf13/cobra@v1.10.2 golang.org/x/term@v0.48.0 github.com/charmbracelet/lipgloss@v1.1.0 github.com/muesli/termenv@v0.16.0
```

Cobra basics (10 min): <https://cobra.dev/>. You need `Command{Use, Short, Args, RunE}`, `Flags().StringVar`,
`Flags().Changed`, and `AddCommand`.

---

## Task 8.1: Output formats

**Goal:** one package that knows how events look in each mode. `EventJSON` is a **public contract** for
users' scripts. Fields may be added, but never renamed or repurposed.

Two lessons hide in this task:
- **`len()` counts bytes, not columns.** `"–"` (en dash) is 3 bytes, so padding with `len` misaligns
  columns. Use `utf8.RuneCountInString` or display-width functions. A test catches this.
- Titles can contain tabs and newlines. In TSV that would break the format, so `clean()` flattens them.

Tests set `lipgloss.SetColorProfile(termenv.Ascii)` in `TestMain`, so expected strings contain no color codes.

**Files:** create `internal/output/output_test.go`, `internal/output/output.go`

**Steps**
1. Test:
```go
package output_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
```
2. Run: compile failure.
3. Implement:
```go
// Package output renders events for the CLI: pretty (humans), TSV and JSON
// Lines (scripts), or a user-supplied Go template.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/jabiOP/clindar/internal/model"
)

type Mode int

const (
	Pretty Mode = iota
	TSV
	JSON
	Template
)

// ChooseMode applies the precedence: explicit flag, else pretty on a terminal, TSV in a pipe.
func ChooseMode(jsonFlag, tsvFlag bool, format string, isTTY bool) (Mode, error) {
	n := 0
	for _, set := range []bool{jsonFlag, tsvFlag, format != ""} {
		if set {
			n++
		}
	}
	switch {
	case n > 1:
		return 0, fmt.Errorf("use only one of --json, --tsv, --format")
	case jsonFlag:
		return JSON, nil
	case tsvFlag:
		return TSV, nil
	case format != "":
		return Template, nil
	case isTTY:
		return Pretty, nil
	default:
		return TSV, nil
	}
}

// EventJSON is the stable, documented shape of an event for scripts.
// Never change a field's meaning; only add fields.
type EventJSON struct {
	ID              string         `json:"id"`
	SeriesID        string         `json:"series_id,omitempty"`
	CalendarID      string         `json:"calendar_id"`
	Calendar        string         `json:"calendar"`
	Title           string         `json:"title"`
	Start           string         `json:"start"` // RFC 3339 local time, or YYYY-MM-DD if all_day
	End             string         `json:"end"`   // exclusive
	AllDay          bool           `json:"all_day"`
	Status          string         `json:"status"`
	Response        string         `json:"response,omitempty"` // your RSVP, if invited
	Location        string         `json:"location,omitempty"`
	Description     string         `json:"description,omitempty"`
	Attendees       []AttendeeJSON `json:"attendees,omitempty"`
	Recurring       bool           `json:"recurring"`
	Link            string         `json:"link,omitempty"`
	StartsInMinutes *int           `json:"starts_in_minutes,omitempty"` // only from `clindar next`
}

type AttendeeJSON struct {
	Email    string `json:"email"`
	Name     string `json:"name,omitempty"`
	Response string `json:"response"`
}

type Printer struct {
	w         io.Writer
	mode      Mode
	tmpl      *template.Template
	loc       *time.Location
	calendars map[string]model.Calendar
}

func NewPrinter(w io.Writer, mode Mode, format string, loc *time.Location, cals []model.Calendar) (*Printer, error) {
	p := &Printer{w: w, mode: mode, loc: loc, calendars: map[string]model.Calendar{}}
	for _, c := range cals {
		p.calendars[c.ID] = c
	}
	if mode == Template {
		t, err := template.New("format").Funcs(template.FuncMap{"time": formatTime}).Parse(format)
		if err != nil {
			return nil, fmt.Errorf("--format: %w", err)
		}
		p.tmpl = t
	}
	return p, nil
}

// formatTime is the template function {{time "15:04" .Start}}.
func formatTime(layout, value string) (string, error) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		if t, err = time.Parse(time.DateOnly, value); err != nil {
			return "", err
		}
	}
	return t.Format(layout), nil
}

func (p *Printer) toJSON(e model.Event) EventJSON {
	j := EventJSON{
		ID: e.ID, SeriesID: e.SeriesID, CalendarID: e.CalendarID, Calendar: p.calendars[e.CalendarID].Name,
		Title: e.Title, AllDay: e.AllDay, Status: e.Status, Location: e.Location, Description: e.Description,
		Recurring: e.IsInstance() || e.IsMaster(), Link: e.Link,
	}
	if e.AllDay {
		j.Start, j.End = e.Start.UTC().Format(time.DateOnly), e.End.UTC().Format(time.DateOnly)
	} else {
		j.Start, j.End = e.Start.In(p.loc).Format(time.RFC3339), e.End.In(p.loc).Format(time.RFC3339)
	}
	if self := e.SelfAttendee(); self != nil {
		j.Response = string(self.Response)
	}
	for _, a := range e.Attendees {
		j.Attendees = append(j.Attendees, AttendeeJSON{Email: a.Email, Name: a.Name, Response: string(a.Response)})
	}
	return j
}

// Events prints an agenda. Pretty mode groups by day across [from, from+days);
// the other modes print one line per event.
func (p *Printer) Events(events []model.Event, from time.Time, days int) error {
	if p.mode == Pretty {
		return p.prettyAgenda(events, from.In(p.loc), days)
	}
	for _, e := range events {
		if err := p.line(p.toJSON(e)); err != nil {
			return err
		}
	}
	return nil
}

// Event prints one event in detail (pretty) or as one line.
func (p *Printer) Event(e model.Event) error {
	if p.mode == Pretty {
		return p.prettyDetail(e)
	}
	return p.line(p.toJSON(e))
}

// Next prints the next event with minutes until it starts (negative = started).
func (p *Printer) Next(e model.Event, now time.Time) error {
	mins := int(e.Start.Sub(now).Round(time.Minute).Minutes())
	if p.mode == Pretty {
		_, err := fmt.Fprintf(p.w, "%s  %s  %s\n", relative(mins), p.timeRange(e), e.Title)
		return err
	}
	j := p.toJSON(e)
	j.StartsInMinutes = &mins
	return p.line(j)
}

func relative(mins int) string {
	switch {
	case mins <= 0:
		return "now"
	case mins < 60:
		return fmt.Sprintf("in %dm", mins)
	default:
		return fmt.Sprintf("in %dh%02dm", mins/60, mins%60)
	}
}

func (p *Printer) line(j EventJSON) error {
	switch p.mode {
	case JSON:
		b, err := json.Marshal(j)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(p.w, "%s\n", b)
		return err
	case Template:
		if err := p.tmpl.Execute(p.w, j); err != nil {
			return err
		}
		_, err := io.WriteString(p.w, "\n")
		return err
	default: // TSV: id, start, end, all_day, calendar, title
		allDay := "0"
		if j.AllDay {
			allDay = "1"
		}
		_, err := fmt.Fprintln(p.w, strings.Join([]string{
			j.ID, j.Start, j.End, allDay, clean(j.Calendar), clean(j.Title)}, "\t"))
		return err
	}
}

// clean keeps one event on one line with no stray tabs.
func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

var (
	dayStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle  = lipgloss.NewStyle().Faint(true)
	timeWidth = utf8.RuneCountInString("00:00–00:00") // not len(): "–" is 3 bytes
)

func (p *Printer) timeRange(e model.Event) string {
	if e.AllDay {
		return fmt.Sprintf("%-*s", timeWidth, "all day")
	}
	s, end := e.Span(p.loc)
	return s.Format("15:04") + "–" + end.Format("15:04")
}

func (p *Printer) prettyAgenda(events []model.Event, from time.Time, days int) error {
	empty := true
	for _, day := range model.GroupByDay(events, from, days) {
		if len(day.Events) == 0 {
			continue
		}
		empty = false
		fmt.Fprintln(p.w, dayStyle.Render(day.Date.Format("Mon 02 Jan")))
		for _, e := range day.Events {
			cal := p.calendars[e.CalendarID]
			fmt.Fprintf(p.w, "  %s  %s  %s\n", p.timeRange(e), clean(e.Title),
				dimStyle.Render(lipgloss.NewStyle().Foreground(lipgloss.Color(cal.Color)).Render("●")+" "+cal.Name))
		}
	}
	if empty {
		_, err := fmt.Fprintln(p.w, dimStyle.Render("No events."))
		return err
	}
	return nil
}

func (p *Printer) prettyDetail(e model.Event) error {
	start, _ := e.Span(p.loc)
	row := func(label, value string) {
		if value != "" {
			fmt.Fprintf(p.w, "%-10s %s\n", label+":", value)
		}
	}
	fmt.Fprintln(p.w, dayStyle.Render(e.Title))
	row("When", start.Format("Mon 02 Jan 2006")+"  "+strings.TrimSpace(p.timeRange(e)))
	row("Calendar", p.calendars[e.CalendarID].Name)
	row("Where", e.Location)
	if e.IsInstance() || e.IsMaster() {
		row("Repeats", "yes (edit with --scope this|following|all)")
	}
	var people []string
	for _, a := range e.Attendees {
		people = append(people, fmt.Sprintf("%s (%s)", a.Email, a.Response))
	}
	row("Guests", strings.Join(people, ", "))
	row("ID", e.ID)
	row("Link", e.Link)
	if e.Description != "" {
		fmt.Fprintf(p.w, "\n%s\n", e.Description)
	}
	return nil
}
```
4. Run: pass.

**Commit:** `feat(output): pretty, TSV, JSON Lines, and template output`

---

## Task 8.2: Agenda time windows

**Goal:** turn `--from/--to/--days/--today/--within` into a time window. It's pure logic, so it gets a table
test. This is a white-box test (`package cli`), because `agendaWindow` is unexported.

**Files:** create `internal/cli/window_test.go`, `internal/cli/window.go`

**Steps**
1. Test:
```go
package cli

import (
	"testing"
	"time"
	_ "time/tzdata"
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
			w, err := agendaWindow(now, tt.f)
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
```
2. Run: compile failure.
3. Implement:
```go
package cli

import (
	"fmt"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/timeparse"
)

// window is the time range an agenda command covers.
type window struct {
	from, to     time.Time
	startingOnly bool // --within: only events that start in the window
}

// days is how many calendar days the window touches (for pretty grouping).
func (w window) days() int {
	n := 0
	for d := model.StartOfDay(w.from); d.Before(w.to); d = d.AddDate(0, 0, 1) {
		n++
	}
	return n
}

type windowFlags struct {
	from, to, within string
	days             int
	today            bool
}

// agendaWindow turns agenda flags into a window. Default: today + 6 days.
// --to with a date (no time) includes that whole day.
func agendaWindow(now time.Time, f windowFlags) (window, error) {
	today := model.StartOfDay(now)
	switch {
	case f.within != "":
		if f.from != "" || f.to != "" || f.today || f.days != 0 {
			return window{}, fmt.Errorf("--within can't be combined with other range flags")
		}
		d, err := timeparse.ParseDuration(f.within)
		if err != nil {
			return window{}, err
		}
		return window{from: now, to: now.Add(d), startingOnly: true}, nil
	case f.today:
		if f.from != "" || f.to != "" || f.days != 0 {
			return window{}, fmt.Errorf("--today can't be combined with other range flags")
		}
		return window{from: today, to: today.AddDate(0, 0, 1)}, nil
	}
	if f.to != "" && f.days != 0 {
		return window{}, fmt.Errorf("use --to or --days, not both")
	}
	from := today
	if f.from != "" {
		t, _, err := timeparse.ParseTime(f.from, now)
		if err != nil {
			return window{}, fmt.Errorf("--from: %w", err)
		}
		from = t
	}
	if f.to != "" {
		t, dateOnly, err := timeparse.ParseTime(f.to, now)
		if err != nil {
			return window{}, fmt.Errorf("--to: %w", err)
		}
		if dateOnly {
			t = t.AddDate(0, 0, 1)
		}
		if !t.After(from) {
			return window{}, fmt.Errorf("--to must be after --from")
		}
		return window{from: from, to: t}, nil
	}
	days := f.days
	if days == 0 {
		days = 7
	}
	if days < 0 {
		return window{}, fmt.Errorf("--days must be positive")
	}
	return window{from: from, to: model.StartOfDay(from).AddDate(0, 0, days)}, nil
}
```
4. Run: pass.

**Commit:** `feat(cli): agenda time windows`

---

## Task 8.3: App wiring, root command, sync, log, auth, main

**Goal:** the skeleton every command hangs off, plus the commands needed to log in and sync.

- `App` holds dependencies. `NewApp` wires the real ones. Tests build an `App` with temp dirs, a fixed clock,
  buffers, and the fake Google (`Remote` is a func field for exactly this reason).
- `Run(ctx, app, args)` is used by both `Main` and the tests, so tests exercise the real command tree, flag
  parsing, and exit codes.
- `RefreshIfStale` (reads) and `PushAfterWrite` (writes) sync on demand when the daemon isn't running.
  They warn on failure instead of failing: **local data always works**.

**Files:**
- Create `internal/cli/errors.go`, `internal/cli/app.go`, `internal/cli/root.go`, `internal/cli/sync.go`, `internal/cli/authcmd.go`
- Replace `main.go`
- Create `internal/cli/harness_test.go`

**Steps**
1. Test harness (and the not-logged-in test). Read `newHarness` carefully. It's how every CLI test runs
   commands in-process:
```go
package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/cli"
	"github.com/jabiOP/clindar/internal/config"
	"github.com/jabiOP/clindar/internal/gcal/gcaltest"
	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/syncer"
	"github.com/jabiOP/clindar/internal/testutil"
)

// harness runs clindar commands in-process against temp dirs and a fake Google.
type harness struct {
	t      *testing.T
	app    *cli.App
	google *gcaltest.Fake
	now    time.Time
	stdin  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ny := testutil.NY(t)
	h := &harness{t: t, now: time.Date(2026, 11, 12, 10, 30, 0, 0, ny)} // Thursday
	h.google = gcaltest.NewFake(model.Calendar{AccountID: testutil.Account, ID: testutil.Calendar,
		Name: "Me", Primary: true, Selected: true, AccessRole: "owner", TimeZone: "America/New_York"})
	dir := t.TempDir()
	h.app = &cli.App{
		Paths:  config.Paths{ConfigDir: dir + "/config", DataDir: dir + "/data"},
		Config: config.Default(),
		Now:    func() time.Time { return h.now },
		Remote: func(context.Context, string) (syncer.Remote, error) { return h.google, nil },
	}
	t.Cleanup(func() { h.app.Close() })
	st, err := h.app.Store()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureAccount(context.Background(), testutil.Account); err != nil { // as `auth login` would
		t.Fatal(err)
	}
	h.mustRun("sync") // and so would its first sync
	return h
}

// run executes a command line and returns stdout, stderr, and the exit code.
func (h *harness) run(args ...string) (string, string, int) {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	h.app.Stdin, h.app.Stdout, h.app.Stderr = strings.NewReader(h.stdin), &stdout, &stderr
	code := cli.Run(context.Background(), h.app, args)
	return stdout.String(), stderr.String(), code
}

// mustRun fails the test unless the command exits 0.
func (h *harness) mustRun(args ...string) string {
	h.t.Helper()
	out, errOut, code := h.run(args...)
	if code != 0 {
		h.t.Fatalf("clindar %s: exit %d\nstderr: %s", strings.Join(args, " "), code, errOut)
	}
	return out
}

func TestNotLoggedIn(t *testing.T) {
	h := newHarness(t)
	st, _ := h.app.Store()
	st.DeleteAccount(context.Background(), testutil.Account)

	_, stderr, code := h.run("sync")

	if code != 1 || !strings.Contains(stderr, "clindar auth login") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}
```
2. `errors.go`:
```go
package cli

import "errors"

// Exit codes (documented in docs/cli.md).
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// usageError marks mistakes in how the command was called (exit code 2).
type usageError struct{ error }

func (e usageError) Unwrap() error { return e.error }

// silentError exits non-zero without printing (e.g. `clindar next` with nothing next).
type silentError struct{ code int }

func (e silentError) Error() string { return "" }

func exitCode(err error) int {
	var se silentError
	var ue usageError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &se):
		return se.code
	case errors.As(err, &ue):
		return exitUsage
	default:
		return exitError
	}
}
```
3. `app.go`:
```go
// Package cli implements the clindar command line with cobra. Commands are thin:
// parse flags, call the service, print with the output package.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jabiOP/clindar/internal/config"
	"github.com/jabiOP/clindar/internal/output"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/syncer"
)

var ErrNotLoggedIn = errors.New("not logged in: run `clindar auth login` (first time? see `clindar auth setup --help`)")

// App holds everything commands need. Main builds the real one; tests build
// one with temp dirs, a fixed clock, buffers, and a fake Remote.
type App struct {
	Paths  config.Paths
	Config config.Config
	Now    func() time.Time
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	IsTTY  bool // stdout is a terminal
	// Remote builds the Google client for an account.
	Remote func(ctx context.Context, accountID string) (syncer.Remote, error)

	store *store.Store
}

// Store opens the database on first use.
func (a *App) Store() (*store.Store, error) {
	if a.store != nil {
		return a.store, nil
	}
	if err := a.Paths.EnsureDirs(); err != nil {
		return nil, err
	}
	st, err := store.Open(a.Paths.DBFile())
	if err != nil {
		return nil, err
	}
	a.store = st
	return st, nil
}

func (a *App) Close() error {
	if a.store == nil {
		return nil
	}
	return a.store.Close()
}

// Account returns the logged-in account (v1 supports exactly one).
func (a *App) Account(ctx context.Context) (string, error) {
	st, err := a.Store()
	if err != nil {
		return "", err
	}
	ids, err := st.Accounts(ctx)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", ErrNotLoggedIn
	}
	return ids[0], nil
}

func (a *App) Service(ctx context.Context) (*service.Service, error) {
	account, err := a.Account(ctx)
	if err != nil {
		return nil, err
	}
	return service.New(a.store, account, a.Now, a.Config.SendUpdates), nil
}

func (a *App) Engine(ctx context.Context) (*syncer.Engine, error) {
	account, err := a.Account(ctx)
	if err != nil {
		return nil, err
	}
	remote, err := a.Remote(ctx, account)
	if err != nil {
		return nil, err
	}
	return &syncer.Engine{Store: a.store, Remote: remote, AccountID: account, Now: a.Now}, nil
}

// SyncNow runs one sync unless another process (the daemon) is already
// syncing, in which case ran is false and that's fine.
func (a *App) SyncNow(ctx context.Context) (ran bool, err error) {
	engine, err := a.Engine(ctx)
	if err != nil {
		return false, err
	}
	return syncer.WithLock(ctx, a.Paths.LockFile(), engine.Sync)
}

// syncQuietly syncs within the configured timeout, printing a warning
// instead of failing: reads and writes always work from the local store.
func (a *App) syncQuietly(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, a.Config.SyncTimeout.Duration)
	defer cancel()
	if _, err := a.SyncNow(ctx); err != nil {
		fmt.Fprintf(a.Stderr, "clindar: warning: sync failed, using local data: %v\n", err)
	}
}

// RefreshIfStale is called by read commands: sync first if the last sync is
// older than stale_after, unless --offline.
func (a *App) RefreshIfStale(ctx context.Context, offline bool) {
	if offline {
		return
	}
	account, err := a.Account(ctx)
	if err != nil {
		return // the command itself will report this
	}
	last, err := a.store.LastSync(ctx, account)
	if err == nil && a.Now().Sub(last) < a.Config.StaleAfter.Duration {
		return
	}
	a.syncQuietly(ctx)
}

// PushAfterWrite is called by write commands so edits reach Google promptly.
func (a *App) PushAfterWrite(ctx context.Context, offline bool) {
	if !offline {
		a.syncQuietly(ctx)
	}
}

// outputFlags are shared by every command that prints events.
type outputFlags struct {
	json, tsv bool
	format    string
}

func (a *App) Printer(ctx context.Context, svc *service.Service, f outputFlags) (*output.Printer, error) {
	mode, err := output.ChooseMode(f.json, f.tsv, f.format, a.IsTTY)
	if err != nil {
		return nil, usageError{err}
	}
	cals, err := svc.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	return output.NewPrinter(a.Stdout, mode, f.format, a.Now().Location(), cals)
}
```
4. `root.go`. **Temporarily remove** the commands you haven't written yet from `root.AddCommand(...)`
   (agenda, next, show, calendars, add, edit, rm, rsvp). Add each back in Tasks 8.4–8.5.
```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jabiOP/clindar/internal/auth"
	"github.com/jabiOP/clindar/internal/config"
	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/syncer"
)

// Version is set by main.
var Version = "dev"

// Main is the real entry point: wires the App and runs the command line.
func Main() int {
	app, err := NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clindar:", err)
		return exitError
	}
	defer app.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, app, os.Args[1:])
}

// Run executes one command line against app and returns the exit code.
// Main and the tests both go through here.
func Run(ctx context.Context, app *App, args []string) int {
	root := NewRootCmd(app)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil && err.Error() != "" {
		fmt.Fprintln(app.Stderr, "clindar:", err)
	}
	return exitCode(err)
}

// NewApp builds the App used in production.
func NewApp() (*App, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		return nil, err
	}
	return &App{
		Paths: paths, Config: cfg, Now: time.Now,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		IsTTY: term.IsTerminal(int(os.Stdout.Fd())),
		Remote: func(ctx context.Context, account string) (syncer.Remote, error) {
			oauthCfg, err := auth.LoadClient(paths.ClientFile())
			if err != nil {
				return nil, err
			}
			hc, err := auth.HTTPClient(ctx, oauthCfg, auth.DefaultTokenStore(paths.TokenFile()), account)
			if err != nil {
				return nil, err
			}
			return gcal.NewClient(ctx, account, hc)
		},
	}, nil
}

// NewRootCmd builds the command tree. Tests call it with their own App.
func NewRootCmd(app *App) *cobra.Command {
	root := &cobra.Command{
		Use:           "clindar",
		Short:         "A fast terminal client for Google Calendar",
		Version:       Version,
		SilenceUsage:  true, // don't dump usage on runtime errors
		SilenceErrors: true, // Main prints errors once
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help() // Phase 10 replaces this with the TUI
		},
	}
	root.SetIn(app.Stdin)
	root.SetOut(app.Stdout)
	root.SetErr(app.Stderr)
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return usageError{err} })
	root.AddCommand(
		newAgendaCmd(app), newNextCmd(app), newShowCmd(app), newCalendarsCmd(app),
		newAddCmd(app), newEditCmd(app), newRmCmd(app), newRSVPCmd(app),
		newSyncCmd(app), newLogCmd(app), newAuthCmd(app),
	)
	return root
}

func addOutputFlags(cmd *cobra.Command, f *outputFlags) {
	cmd.Flags().BoolVar(&f.json, "json", false, "print JSON Lines (one event per line)")
	cmd.Flags().BoolVar(&f.tsv, "tsv", false, "print tab-separated: id start end all_day calendar title")
	cmd.Flags().StringVar(&f.format, "format", "", `print with a Go template, e.g. '{{time "15:04" .Start}} {{.Title}}'`)
}

func addOfflineFlag(cmd *cobra.Command, offline *bool) {
	cmd.Flags().BoolVar(offline, "offline", false, "don't sync with Google; use local data only")
}

// exactArgs is cobra.ExactArgs, but reports a usage error (exit 2).
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}
```
5. `sync.go`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSyncCmd(app *App) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync with Google now",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			account, err := app.Account(ctx)
			if err != nil {
				return err
			}
			if full {
				if err := app.store.ResetSyncTokens(ctx, account); err != nil {
					return err
				}
			}
			ran, err := app.SyncNow(ctx)
			if err != nil {
				return err
			}
			if !ran {
				fmt.Fprintln(app.Stderr, "clindar: another sync is running (the daemon?); it will pick up your changes")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "re-download everything (fixes a confused cache)")
	return cmd
}

func newLogCmd(app *App) *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show discarded edits and sync errors, newest first",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			entries, err := svc.Logs(cmd.Context(), n)
			if err != nil {
				return err
			}
			for _, e := range entries {
				fmt.Fprintf(app.Stdout, "%s  %-14s  %s\n", e.Time.In(app.Now().Location()).Format("2006-01-02 15:04"), e.Kind, e.Message)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "lines", "n", 20, "number of entries")
	return cmd
}
```
6. `authcmd.go`. `auth login` is thin glue over Phase 7 and is verified manually in step 9:
```go
package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jabiOP/clindar/internal/auth"
	"github.com/jabiOP/clindar/internal/gcal"
)

func newAuthCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage your Google login"}
	cmd.AddCommand(newAuthSetupCmd(app), newAuthLoginCmd(app), newAuthStatusCmd(app), newAuthLogoutCmd(app))
	return cmd
}

func newAuthSetupCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "setup CLIENT_SECRET.json",
		Short: "Install your Google OAuth client (see docs/setup-google-cloud.md)",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := auth.LoadClient(args[0]); err != nil { // validate before copying
				return err
			}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			if err := app.Paths.EnsureDirs(); err != nil {
				return err
			}
			if err := os.WriteFile(app.Paths.ClientFile(), data, 0o600); err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "Saved to %s. Next: clindar auth login\n", app.Paths.ClientFile())
			return nil
		},
	}
}

func newAuthLoginCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to Google in your browser, then do a first sync",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := auth.LoadClient(app.Paths.ClientFile())
			if err != nil {
				return err
			}
			st, err := app.Store()
			if err != nil {
				return err
			}
			existing, err := app.Account(ctx)
			if err != nil && !errors.Is(err, ErrNotLoggedIn) {
				return err
			}
			tok, err := auth.Login(ctx, cfg, func(url string) error {
				fmt.Fprintf(app.Stderr, "Opening your browser. If it doesn't open, visit:\n\n  %s\n\n", url)
				_ = auth.OpenBrowser(url) // the printed URL is the fallback
				return nil
			})
			if err != nil {
				return err
			}
			client, err := gcal.NewClient(ctx, "", cfg.Client(ctx, tok))
			if err != nil {
				return err
			}
			email, err := client.PrimaryCalendarID(ctx) // the primary calendar's ID is your email
			if err != nil {
				return err
			}
			if existing != "" && existing != email {
				return fmt.Errorf("already logged in as %s; run `clindar auth logout` first (one account in this version)", existing)
			}
			if err := auth.DefaultTokenStore(app.Paths.TokenFile()).Save(email, tok); err != nil {
				return err
			}
			if err := st.EnsureAccount(ctx, email); err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "Logged in as %s. Syncing...\n", email)
			if _, err := app.SyncNow(ctx); err != nil {
				return err
			}
			fmt.Fprintln(app.Stderr, "Done. Try: clindar agenda")
			return nil
		},
	}
}

func newAuthStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show login and sync status",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			st, err := svc.Status(cmd.Context(), time.Time{})
			if err != nil {
				return err
			}
			last := "never"
			if !st.LastSync.IsZero() {
				last = app.Now().Sub(st.LastSync).Round(time.Second).String() + " ago"
			}
			fmt.Fprintf(app.Stdout, "account:   %s\nlast sync: %s\npending:   %d change(s)\ndiscarded: %d edit(s) (see clindar log)\n",
				svc.AccountID(), last, st.Pending, st.Discarded)
			return nil
		},
	}
}

func newAuthLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget your login and delete the local cache (unsynced changes are lost)",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			account, err := app.Account(ctx)
			if err != nil {
				return err
			}
			if err := auth.DefaultTokenStore(app.Paths.TokenFile()).Delete(account); err != nil {
				return err
			}
			if err := app.store.DeleteAccount(ctx, account); err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "Logged out %s.\n", account)
			return nil
		},
	}
}
```
7. `main.go`:
```go
package main

import (
	"os"
	_ "time/tzdata" // embed timezone database so LoadLocation works everywhere

	"github.com/jabiOP/clindar/internal/cli"
)

// version is overwritten at release time with -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	cli.Version = version
	os.Exit(cli.Main())
}
```
8. `go mod tidy && go test ./internal/cli/`: pass.
9. **Milestone: log in for real.** Follow `docs/setup-google-cloud.md` if Phase 11 is done. Otherwise use this summary:
   create a Google Cloud project, enable the "Google Calendar API", configure the OAuth consent screen
   (External, add yourself as a test user), and create an OAuth client ID of type **Desktop app**. Download the JSON. Then run:
   ```bash
   go build -o clindar . && export CLINDAR_CONFIG_DIR=/tmp/clindar-dev/config CLINDAR_DATA_DIR=/tmp/clindar-dev/data
   ./clindar auth setup ~/Downloads/client_secret_*.json
   ./clindar auth login
   ./clindar auth status
   ```
   The `CLINDAR_*` variables keep dev data away from your real config.

**Commit:** `feat(cli): app wiring, auth, sync, log commands`

---

## Task 8.4: Read commands (agenda, next, show, calendars)

**Goal:** the scripting surface. `next` exits 1 **silently** when there's nothing upcoming, so status-bar scripts
can write `clindar next --format '{{.Title}}' || echo free`. `agenda --within 10m` lists only events *starting* in
the window, which is the right input for notification scripts.

**Files:** create `internal/cli/read_test.go`, `internal/cli/read.go`; add the four commands to `root.AddCommand`

**Steps**
1. Test:
```go
package cli_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/output"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestAgendaShowsSyncedEvents(t *testing.T) {
	h := newHarness(t)
	ny := testutil.NY(t)
	h.google.Put(testutil.Timed("e1", "Dentist", time.Date(2026, 11, 13, 15, 0, 0, 0, ny), 45*time.Minute))
	h.mustRun("sync")

	out := h.mustRun("agenda", "--json")

	var got output.EventJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("agenda --json = %q: %v", out, err)
	}
	if got.ID != "e1" || got.Title != "Dentist" || got.Calendar != "Me" {
		t.Errorf("got %+v", got)
	}
}

func TestAgendaWithinOnlyListsEventsStartingSoon(t *testing.T) {
	h := newHarness(t)
	ny := testutil.NY(t)
	h.google.Put(testutil.Timed("now", "Already started", time.Date(2026, 11, 12, 10, 0, 0, 0, ny), time.Hour))
	h.google.Put(testutil.Timed("soon", "Starts soon", time.Date(2026, 11, 12, 10, 40, 0, 0, ny), time.Hour))
	h.google.Put(testutil.Timed("later", "Later", time.Date(2026, 11, 12, 15, 0, 0, 0, ny), time.Hour))
	h.mustRun("sync")

	out := h.mustRun("agenda", "--within", "15m", "--format", "{{.Title}}")

	if out != "Starts soon\n" {
		t.Errorf("got %q", out)
	}
}

func TestNextWithNothingExits1Silently(t *testing.T) {
	h := newHarness(t)

	out, errOut, code := h.run("next")

	if code != 1 || out != "" || errOut != "" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
)

func newAgendaCmd(app *App) *cobra.Command {
	var wf windowFlags
	var of outputFlags
	var calendars []string
	var offline bool
	cmd := &cobra.Command{
		Use:   "agenda",
		Short: "List events (default: today and the next 6 days)",
		Example: `  clindar agenda --today
  clindar agenda --from mon --days 5 --json
  clindar agenda --within 15m --format '{{.Title}}'   # for notifications`,
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w, err := agendaWindow(app.Now(), wf)
			if err != nil {
				return usageError{err}
			}
			app.RefreshIfStale(ctx, offline)
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			filter, err := calendarFilter(cmd, svc, calendars)
			if err != nil {
				return err
			}
			events, err := svc.Agenda(ctx, w.from, w.to, filter)
			if err != nil {
				return err
			}
			if w.startingOnly {
				events = startingIn(events, w)
			}
			p, err := app.Printer(ctx, svc, of)
			if err != nil {
				return err
			}
			return p.Events(events, w.from, w.days())
		},
	}
	cmd.Flags().StringVar(&wf.from, "from", "", "start of range (default: today)")
	cmd.Flags().StringVar(&wf.to, "to", "", "end of range; a date includes that whole day")
	cmd.Flags().IntVar(&wf.days, "days", 0, "number of days from --from (default 7)")
	cmd.Flags().BoolVar(&wf.today, "today", false, "only today")
	cmd.Flags().StringVar(&wf.within, "within", "", "only events starting between now and now+DURATION")
	cmd.Flags().StringArrayVarP(&calendars, "calendar", "c", nil, "calendar name or ID (repeatable; default: calendars shown in Google)")
	addOutputFlags(cmd, &of)
	addOfflineFlag(cmd, &offline)
	return cmd
}

func calendarFilter(cmd *cobra.Command, svc *service.Service, names []string) (service.Filter, error) {
	var f service.Filter
	for _, n := range names {
		c, err := svc.ResolveCalendar(cmd.Context(), n)
		if err != nil {
			return f, err
		}
		f.CalendarIDs = append(f.CalendarIDs, c.ID)
	}
	return f, nil
}

func startingIn(events []model.Event, w window) []model.Event {
	var out []model.Event
	for _, e := range events {
		if !e.AllDay && !e.Start.Before(w.from) && e.Start.Before(w.to) {
			out = append(out, e)
		}
	}
	return out
}

func newNextCmd(app *App) *cobra.Command {
	var of outputFlags
	var offline bool
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Show the current or next timed event (exit status 1 if none in 7 days)",
		Example: `  clindar next                       # in 12m  09:00–09:15  Standup
  clindar next --format '{{.Title}}'  # for a status bar`,
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			app.RefreshIfStale(ctx, offline)
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			e, ok, err := svc.Next(ctx)
			if err != nil {
				return err
			}
			if !ok {
				return silentError{code: exitError}
			}
			p, err := app.Printer(ctx, svc, of)
			if err != nil {
				return err
			}
			return p.Next(e, app.Now())
		},
	}
	addOutputFlags(cmd, &of)
	addOfflineFlag(cmd, &offline)
	return cmd
}

func newShowCmd(app *App) *cobra.Command {
	var of outputFlags
	cmd := &cobra.Command{
		Use:   "show ID",
		Short: "Show one event in detail",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			e, err := svc.Get(ctx, args[0])
			if err != nil {
				return err
			}
			p, err := app.Printer(ctx, svc, of)
			if err != nil {
				return err
			}
			return p.Event(e)
		},
	}
	addOutputFlags(cmd, &of)
	return cmd
}

func newCalendarsCmd(app *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "calendars",
		Short: "List calendars (* = shown by default)",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			cals, err := svc.Calendars(cmd.Context())
			if err != nil {
				return err
			}
			for _, c := range cals {
				if asJSON {
					b, _ := json.Marshal(c)
					fmt.Fprintf(app.Stdout, "%s\n", b)
					continue
				}
				mark := " "
				if c.Selected {
					mark = "*"
				}
				fmt.Fprintf(app.Stdout, "%s %-24s %-8s %s\n", mark, c.Name, c.AccessRole, c.ID)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON Lines")
	return cmd
}
```
4. Run: pass. Then try it against your real data: `./clindar agenda`, `./clindar agenda | cat` (TSV), `./clindar next`.

**Commit:** `feat(cli): agenda, next, show, calendars`

---

## Task 8.5: Write commands (add, edit, rm, rsvp)

**Goal:** create and change events from the shell.

- Titles can come from **stdin** (`clindar add - …`, or no title), which is how the GTD integration pipes a task line
  in (design Q6–7).
- `edit` changes **only the flags you pass**. It uses `cmd.Flags().Changed` and converts them to
  `service.Changes`. `--at fri` on a timed event keeps the time of day.
- Writes print the ID on stdout, then `PushAfterWrite`.

**Files:** create `internal/cli/write_test.go`, `internal/cli/write.go`; add the four commands to `root.AddCommand`

**Steps**
1. Test:
```go
package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/output"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestAddThenAgendaJSON(t *testing.T) {
	h := newHarness(t)

	id := strings.TrimSpace(h.mustRun("add", "Write", "report", "--at", "fri 14:00", "--dur", "90m"))
	out := h.mustRun("agenda", "--from", "fri", "--days", "1", "--json")

	var got output.EventJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("agenda output %q: %v", out, err)
	}
	if got.ID != id || got.Title != "Write report" || got.Start != "2026-11-13T14:00:00-05:00" || got.End != "2026-11-13T15:30:00-05:00" {
		t.Errorf("got %+v", got)
	}
	if _, ok := h.google.Event(testutil.Calendar, id); !ok {
		t.Error("add should push to Google right away")
	}
}

func TestAddReadsTitleFromStdin(t *testing.T) {
	h := newHarness(t)
	h.stdin = "call the bank  @phone\n(second line ignored)\n"

	id := strings.TrimSpace(h.mustRun("add", "-", "--at", "tomorrow 9am", "--offline"))

	out := h.mustRun("show", id, "--format", "{{.Title}}")
	if out != "call the bank  @phone\n" {
		t.Errorf("title = %q", out)
	}
}

func TestAddDateOnlyIsAllDay(t *testing.T) {
	h := newHarness(t)

	id := strings.TrimSpace(h.mustRun("add", "Conference", "--at", "2026-11-20", "--end", "2026-11-21", "--offline"))

	out := h.mustRun("show", id, "--tsv")
	if want := id + "\t2026-11-20\t2026-11-22\t1\tMe\tConference\n"; out != want {
		t.Errorf("got %q, want %q (end is exclusive)", out, want)
	}
}

func TestEditRecurringNeedsScope(t *testing.T) {
	h := newHarness(t)
	h.mustRun("add", "Standup", "--at", "mon 9:00", "--dur", "15m", "--repeat", "weekly", "--offline")
	instance := strings.Fields(h.mustRun("agenda", "--from", "2026-11-23", "--days", "1", "--tsv"))[0]

	_, stderr, code := h.run("edit", instance, "--title", "Retro", "--offline")
	if code != 1 || !strings.Contains(stderr, "choose a scope") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}

	h.mustRun("edit", instance, "--title", "Retro", "--scope", "this", "--offline")
	if out := h.mustRun("agenda", "--from", "2026-11-23", "--days", "1", "--format", "{{.Title}}"); out != "Retro\n" {
		t.Errorf("after edit: %q", out)
	}
}

func TestNextShowsMinutesUntilAddedEvent(t *testing.T) {
	h := newHarness(t)
	h.mustRun("add", "Lunch", "--at", "12:00", "--offline")

	out, _, code := h.run("next", "--offline", "--format", "{{.StartsInMinutes}} {{.Title}}")
	if code != 0 || out != "90 Lunch\n" {
		t.Errorf("exit %d, out %q", code, out)
	}
}

func TestUsageErrorsExit2(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"agenda", "--json", "--tsv"},
		{"agenda", "--bogus"},
		{"add", "x", "--at", "someday"},
		{"rsvp", "id", "perhaps"},
	} {
		if _, _, code := h.run(args...); code != 2 {
			t.Errorf("clindar %v: exit %d, want 2", args, code)
		}
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package cli

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/timeparse"
)

// eventFlags are shared by add and edit.
type eventFlags struct {
	at, end, dur     string
	allDay           bool
	calendar         string
	where, desc      string
	invite           []string
	repeat, until    string
	count            int
	offline          bool
	scope, titleFlag string
}

func addEventFlags(cmd *cobra.Command, f *eventFlags) {
	cmd.Flags().StringVar(&f.at, "at", "", `start: "tomorrow 14:00", "fri 2pm", "2026-11-20 09:15"; a date alone means all-day`)
	cmd.Flags().StringVar(&f.end, "end", "", "end time (relative to the start day), or last day for all-day events")
	cmd.Flags().StringVar(&f.dur, "dur", "", "duration, e.g. 30m, 1h30m, 2d (default 1h, or 1 day)")
	cmd.Flags().BoolVar(&f.allDay, "all-day", false, "make it an all-day event")
	cmd.Flags().StringVar(&f.where, "where", "", "location")
	cmd.Flags().StringVar(&f.desc, "desc", "", "description")
	cmd.Flags().StringArrayVar(&f.invite, "invite", nil, "attendee email (repeatable)")
	addOfflineFlag(cmd, &f.offline)
}

func newAddCmd(app *App) *cobra.Command {
	var f eventFlags
	cmd := &cobra.Command{
		Use:   "add [TITLE... | -]",
		Short: "Create an event; prints its ID",
		Long:  "Create an event. With no TITLE or with -, the title is the first line of stdin.",
		Example: `  clindar add Dentist --at "tue 15:00" --dur 45m --where "Main St"
  grep 'report' ~/gtd/next.txt | clindar add - --at "tomorrow 9am" --dur 2h
  clindar add Standup --at "mon 9:00" --dur 15m --repeat weekdays`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			title, err := titleFrom(args, app)
			if err != nil {
				return usageError{err}
			}
			now := app.Now()
			t, err := timeparse.ParseSpan(now, f.at, f.end, f.dur, f.allDay)
			if err != nil {
				return usageError{err}
			}
			var until time.Time
			if f.until != "" {
				if until, _, err = timeparse.ParseTime(f.until, now); err != nil {
					return usageError{fmt.Errorf("--until: %w", err)}
				}
			}
			rec, err := recur.ParseRepeat(f.repeat, t.Start, t.AllDay, until, f.count)
			if err != nil {
				return usageError{err}
			}
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			e, err := svc.Create(ctx, service.Draft{
				CalendarID: firstNonEmpty(f.calendar, app.Config.DefaultCalendar), Title: title,
				Description: f.desc, Location: f.where, Start: t.Start, End: t.End, AllDay: t.AllDay,
				Recurrence: rec, Invite: f.invite,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(app.Stdout, e.ID)
			app.PushAfterWrite(ctx, f.offline)
			return nil
		},
	}
	addEventFlags(cmd, &f)
	cmd.Flags().StringVarP(&f.calendar, "calendar", "c", "", "calendar name or ID (default: config default_calendar, else primary)")
	cmd.Flags().StringVar(&f.repeat, "repeat", "", "daily | weekdays | weekly | monthly | yearly | FREQ=... (RFC 5545)")
	cmd.Flags().StringVar(&f.until, "until", "", "last date of a repeating event")
	cmd.Flags().IntVar(&f.count, "count", 0, "number of occurrences of a repeating event")
	return cmd
}

func titleFrom(args []string, app *App) (string, error) {
	if len(args) > 0 && !(len(args) == 1 && args[0] == "-") {
		return strings.Join(args, " "), nil
	}
	line, err := bufio.NewReader(app.Stdin).ReadString('\n')
	if line = strings.TrimSpace(line); line == "" {
		return "", fmt.Errorf("no title: give one as arguments or on stdin (%v)", err)
	}
	return line, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func addScopeFlag(cmd *cobra.Command, scope *string, allowed string) {
	cmd.Flags().StringVar(scope, "scope", "", "for recurring events: "+allowed)
}

func newEditCmd(app *App) *cobra.Command {
	var f eventFlags
	cmd := &cobra.Command{
		Use:   "edit ID",
		Short: "Change an event; only the flags you give are changed",
		Example: `  clindar edit abc123 --at "thu 16:00"            # move, keeping duration
  clindar edit abc123_20261116T140000Z --title Retro --scope this`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			scope, err := service.ParseScope(f.scope)
			if err != nil {
				return usageError{err}
			}
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			e, err := svc.Get(ctx, args[0])
			if err != nil {
				return err
			}
			ch, err := changesFrom(cmd, f, e, app.Now())
			if err != nil {
				return usageError{err}
			}
			updated, err := svc.Update(ctx, args[0], ch, scope)
			if err != nil {
				return err
			}
			fmt.Fprintln(app.Stdout, updated.ID)
			app.PushAfterWrite(ctx, f.offline)
			return nil
		},
	}
	addEventFlags(cmd, &f)
	cmd.Flags().StringVar(&f.titleFlag, "title", "", "new title")
	addScopeFlag(cmd, &f.scope, "this | following | all")
	return cmd
}

// changesFrom builds service.Changes from the flags the user actually set.
func changesFrom(cmd *cobra.Command, f eventFlags, e model.Event, now time.Time) (service.Changes, error) {
	var ch service.Changes
	set := cmd.Flags().Changed
	if set("title") {
		ch.Title = &f.titleFlag
	}
	if set("where") {
		ch.Location = &f.where
	}
	if set("desc") {
		ch.Description = &f.desc
	}
	if set("invite") {
		ch.Invite = &f.invite
	}
	allDay := e.AllDay
	if set("all-day") {
		allDay = f.allDay
		ch.AllDay = &allDay
	}
	start := e.Start
	if set("at") {
		t, dateOnly, err := timeparse.ParseTime(f.at, now)
		if err != nil {
			return ch, fmt.Errorf("--at: %w", err)
		}
		if dateOnly && !allDay { // "--at fri" on a timed event: same time, new day
			clock := e.Start.In(t.Location())
			t = time.Date(t.Year(), t.Month(), t.Day(), clock.Hour(), clock.Minute(), 0, 0, t.Location())
		}
		start = t
		ch.Start = &start
	}
	if set("end") && set("dur") {
		return ch, fmt.Errorf("use --end or --dur, not both")
	}
	if set("end") {
		end, _, err := timeparse.ParseTime(f.end, start)
		if err != nil {
			return ch, fmt.Errorf("--end: %w", err)
		}
		if allDay {
			end = model.StartOfDay(end).AddDate(0, 0, 1)
		}
		ch.End = &end
	}
	if set("dur") {
		d, err := timeparse.ParseDuration(f.dur)
		if err != nil {
			return ch, fmt.Errorf("--dur: %w", err)
		}
		end := start.Add(d)
		ch.End = &end
	}
	return ch, nil
}

func newRmCmd(app *App) *cobra.Command {
	var scope string
	var offline bool
	cmd := &cobra.Command{
		Use:   "rm ID",
		Short: "Delete an event",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := service.ParseScope(scope)
			if err != nil {
				return usageError{err}
			}
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			if err := svc.Delete(cmd.Context(), args[0], sc); err != nil {
				return err
			}
			app.PushAfterWrite(cmd.Context(), offline)
			return nil
		},
	}
	addScopeFlag(cmd, &scope, "this | following | all")
	addOfflineFlag(cmd, &offline)
	return cmd
}

var responses = map[string]model.ResponseStatus{
	"yes": model.ResponseAccepted, "no": model.ResponseDeclined, "maybe": model.ResponseTentative,
}

func newRSVPCmd(app *App) *cobra.Command {
	var scope string
	var offline bool
	cmd := &cobra.Command{
		Use:   "rsvp ID yes|no|maybe",
		Short: "Respond to an invitation",
		Args:  exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, ok := responses[strings.ToLower(args[1])]
			if !ok {
				return usageError{fmt.Errorf("answer must be yes, no, or maybe")}
			}
			sc, err := service.ParseScope(scope)
			if err != nil {
				return usageError{err}
			}
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			if err := svc.RSVP(cmd.Context(), args[0], resp, sc); err != nil {
				return err
			}
			app.PushAfterWrite(cmd.Context(), offline)
			return nil
		},
	}
	addScopeFlag(cmd, &scope, "this | all")
	addOfflineFlag(cmd, &offline)
	return cmd
}
```
4. Run `go test -race ./...`: pass.

**Commit:** `feat(cli): add, edit, rm, rsvp`

---

## Task 8.6: Try it like a user

No code here. Spend 20 minutes using `clindar` against a **throwaway test calendar** (create one in Google
Calendar called "clindar-test"; then `-c clindar-test` on writes). Keep `calendar.google.com` open side by side.

1. `./clindar add "Plan test" --at "tomorrow 10:00" --dur 30m -c clindar-test`, then check the web UI.
2. Edit it in the web UI, then run `./clindar agenda --from tomorrow --days 1`. The change should show.
3. `./clindar add "Weekly" --at "mon 9:00" --dur 15m --repeat weekly -c clindar-test`, then edit one instance with `--scope this`,
   and another with `--scope following`. Compare with the web UI.
4. Write anything that surprised you in `DEVIATIONS.md`.

---

## Task 8.7: Verify our assumptions about Google

Tests use the fake, so they prove our logic, **not** Google's behavior. Check each assumption once by hand and
record the result in `DEVIATIONS.md`. If one is wrong, fix the code **and the fake** (Task 5.3 rule).

| Assumption | How to check |
|---|---|
| Instance IDs are `<id>_<YYYYMMDDTHHMMSSZ>` (or `_<YYYYMMDD>` for all-day series) | `./clindar agenda --json` on a recurring event, then compare the `id` with the URL of that instance in the web UI's event details, or use the API Explorer "events.instances" |
| PATCH/DELETE on an instance ID with no exception works | `clindar edit <instance> --scope this` on an untouched instance |
| Client-chosen IDs are accepted on insert | `clindar add` succeeds and the web UI shows the event |
| 412 on a stale `If-Match` | Edit an event offline (`--offline`), change it in the web UI, `clindar sync`, then `clindar log` should show a discarded edit |
| Clearing a field works (`ForceSendFields`) | `clindar edit ID --desc ""` removes the description |
| Cancelled instances arrive with `originalStartTime` in incremental sync | Delete one instance in the web UI, `clindar sync`, and check that the slot disappears in `clindar agenda` |
| RSVP as a non-organizer works and changes nothing else | Have a second account invite you; `clindar rsvp ID yes`; the organizer sees "accepted" and the title/description are untouched. Also try it after the organizer edits the title. |
| Single-occurrence invites show up | Have a second account invite you to **one** occurrence of their recurring meeting; `clindar sync`; it appears in `clindar agenda` as a normal event |
| A 401 keeps edits queued | Revoke clindar at <https://myaccount.google.com/permissions>, make an edit, `clindar sync` should fail with the "log in" message, `clindar auth status` shows the pending change, `clindar auth login` then delivers it |
| Keyring works | `secret-tool search service clindar` (Linux) or Keychain Access (macOS) shows the token |
| Token refresh | Leave it for over an hour, then `clindar sync` still works without logging in |
