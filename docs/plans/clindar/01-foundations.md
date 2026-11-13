# Phase 1: Foundations

At the end of this phase you'll have a Go module, the domain model, date/duration parsing, and
config/paths. Nothing touches the network or disk except config loading.

Throughout the plan, `github.com/yourname/clindar` is the module path. Pick yours in Task 1.1 and
substitute it everywhere (or use `sed` on code you paste).

---

## Task 1.1: Repository skeleton

**Goal:** a buildable, committed Go module.

**Files:** create `go.mod`, `main.go`, `.gitignore` at the repo root (the directory containing `docs/`). The repo and a stub `README.md` already exist.

**Steps**

1. Initialize the module (the git repo already exists):
   ```bash
   go mod init github.com/yourname/clindar
   go mod edit -go=1.26
   ```
   Check the toolchain: `go version` (run inside the repo) must print `go1.26` or newer. The first run may pause
   while Go downloads it (see the README primer). If it fails with a download or `GOTOOLCHAIN` error, stop and
   get Go 1.26 installed. Don't edit the `go` line down.
2. `main.go`:
   ```go
   package main

   import (
   	"fmt"

   	_ "time/tzdata" // embed timezone database so LoadLocation works everywhere
   )

   // version is overwritten at release time with -ldflags "-X main.version=v0.1.0".
   var version = "dev"

   func main() {
   	fmt.Println("clindar", version)
   }
   ```
3. `.gitignore`:
   ```
   /clindar
   /dist/
   *.db
   *.db-wal
   *.db-shm
   client.json
   client_secret*.json
   token.json
   ```
4. Leave `README.md` as is. Real docs come in Phase 11.
5. Verify:
   ```bash
   go build -o clindar . && ./clindar    # prints: clindar dev
   gofmt -l . && go vet ./...
   ```

**Done when:** `./clindar` prints `clindar dev`.

**Commit:** `chore: initialize clindar module`

---

## Task 1.2: Event type and time spans

**Goal:** the single domain type for events, and the one function that decides "does this event touch this
time window?". Every view and query will depend on it, so it needs careful tests.

**Read first:** `00-design.md` §2 (how all-day events are stored).

**Files:**
- Create `internal/model/event.go`
- Create `internal/model/event_test.go`

**Steps**

1. Write the test first. `internal/model/event_test.go`:
   ```go
   package model_test

   import (
   	"testing"
   	"time"
   	_ "time/tzdata"

   	"github.com/yourname/clindar/internal/model"
   )

   func mustLoc(t *testing.T, name string) *time.Location {
   	t.Helper()
   	loc, err := time.LoadLocation(name)
   	if err != nil {
   		t.Fatalf("LoadLocation(%q): %v", name, err)
   	}
   	return loc
   }

   func TestEventOverlaps(t *testing.T) {
   	ny := mustLoc(t, "America/New_York")
   	tokyo := mustLoc(t, "Asia/Tokyo")
   	at := func(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, ny) }
   	utcDay := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }

   	timed := model.Event{Start: at(14, 9), End: at(14, 10)}
   	allDay := model.Event{AllDay: true, Start: utcDay(14), End: utcDay(15)}
   	zeroLen := model.Event{Start: at(14, 9), End: at(14, 9)}

   	tests := []struct {
   		name     string
   		event    model.Event
   		from, to time.Time
   		want     bool
   	}{
   		{"timed inside window", timed, at(14, 0), at(15, 0), true},
   		{"timed ends exactly at window start", timed, at(14, 10), at(14, 12), false},
   		{"timed starts exactly at window end", timed, at(14, 0), at(14, 9), false},
   		{"all-day on its day", allDay, at(14, 0), at(15, 0), true},
   		{"all-day not on previous day", allDay, at(13, 0), at(14, 0), false},
   		{"all-day not on next day", allDay, at(15, 0), at(16, 0), false},
   		// If all-day events were treated as UTC instants, Nov 14 00:00Z–Nov 15 00:00Z would
   		// cover Nov 15 00:00–09:00 in Tokyo. It must not: "Nov 14" means Nov 14 everywhere.
   		{"all-day anchored to window zone",
   			allDay,
   			time.Date(2026, 11, 15, 0, 0, 0, 0, tokyo),
   			time.Date(2026, 11, 15, 6, 0, 0, 0, tokyo),
   			false},
   		{"zero-length event at window start", zeroLen, at(14, 9), at(14, 10), true},
   		{"zero-length event at window end", zeroLen, at(14, 8), at(14, 9), false},
   	}
   	for _, tt := range tests {
   		t.Run(tt.name, func(t *testing.T) {
   			if got := tt.event.Overlaps(tt.from, tt.to); got != tt.want {
   				t.Errorf("Overlaps(%v, %v) = %v, want %v", tt.from, tt.to, got, tt.want)
   			}
   		})
   	}
   }

   func TestEventSpanAllDayUsesLocalMidnight(t *testing.T) {
   	ny := mustLoc(t, "America/New_York")
   	e := model.Event{AllDay: true,
   		Start: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
   		End:   time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)}

   	start, end := e.Span(ny)

   	if want := time.Date(2026, 11, 14, 0, 0, 0, 0, ny); !start.Equal(want) {
   		t.Errorf("start = %v, want %v", start, want)
   	}
   	if want := time.Date(2026, 11, 16, 0, 0, 0, 0, ny); !end.Equal(want) {
   		t.Errorf("end = %v, want %v", end, want)
   	}
   }
   ```
2. Run it: `go test ./internal/model/`. **Expected:** a compile failure (`undefined: model.Event`). That counts as
   your "red" step.
3. Implement `internal/model/event.go`:
   ```go
   // Package model holds clindar's domain types. It has no dependencies on storage,
   // network, or UI code; everything else depends on it.
   package model

   import "time"

   // Event status values, as used by Google Calendar.
   const (
   	StatusConfirmed = "confirmed"
   	StatusTentative = "tentative"
   	StatusCancelled = "cancelled"
   )

   // ResponseStatus is an attendee's RSVP.
   type ResponseStatus string

   const (
   	ResponseNeedsAction ResponseStatus = "needsAction"
   	ResponseAccepted    ResponseStatus = "accepted"
   	ResponseDeclined    ResponseStatus = "declined"
   	ResponseTentative   ResponseStatus = "tentative"
   )

   type Attendee struct {
   	Email     string         `json:"email"`
   	Name      string         `json:"name,omitempty"`
   	Response  ResponseStatus `json:"response,omitempty"`
   	Self      bool           `json:"self,omitempty"`
   	Organizer bool           `json:"organizer,omitempty"`
   	Optional  bool           `json:"optional,omitempty"`
   }

   // Event is a single event, a recurring series master, a stored exception,
   // or a virtual instance produced by expanding a series. See design §2.
   //
   // All-day events: AllDay is true, Start/End are UTC midnights, End is exclusive.
   // Use Span/Overlaps rather than reading Start/End directly for display.
   type Event struct {
   	AccountID  string `json:"account_id"`
   	CalendarID string `json:"calendar_id"`
   	ID         string `json:"id"`

   	// SeriesID and OriginalStart are set on exceptions and virtual instances.
   	SeriesID      string    `json:"series_id,omitempty"`
   	OriginalStart time.Time `json:"original_start,omitzero"`

   	Status      string `json:"status"`
   	Title       string `json:"title"`
   	Description string `json:"description,omitempty"`
   	Location    string `json:"location,omitempty"`

   	Start    time.Time `json:"start"`
   	End      time.Time `json:"end"`
   	AllDay   bool      `json:"all_day,omitempty"`
   	TimeZone string    `json:"time_zone,omitempty"` // IANA name, timed events only

   	// Recurrence holds RRULE/EXDATE/RDATE lines; non-empty only on series masters.
   	Recurrence []string   `json:"recurrence,omitempty"`
   	Attendees  []Attendee `json:"attendees,omitempty"`
   	Organizer  string     `json:"organizer,omitempty"` // organizer's email

   	ETag    string    `json:"etag,omitempty"`
   	Updated time.Time `json:"updated,omitzero"`
   	Link    string    `json:"link,omitempty"` // Google web URL
   }

   func (e Event) IsMaster() bool          { return len(e.Recurrence) > 0 }
   func (e Event) IsInstance() bool        { return e.SeriesID != "" }
   func (e Event) Cancelled() bool         { return e.Status == StatusCancelled }
   func (e Event) Duration() time.Duration { return e.End.Sub(e.Start) }

   // Span returns the event's start and end as instants in loc. All-day events
   // are re-anchored to midnight in loc, because "Nov 14" is Nov 14 wherever you are.
   func (e Event) Span(loc *time.Location) (start, end time.Time) {
   	if !e.AllDay {
   		return e.Start.In(loc), e.End.In(loc)
   	}
   	return civilMidnight(e.Start, loc), civilMidnight(e.End, loc)
   }

   // Overlaps reports whether the event intersects the half-open window [from, to).
   // from's location anchors all-day events. Zero-length events count if they
   // start inside the window.
   func (e Event) Overlaps(from, to time.Time) bool {
   	start, end := e.Span(from.Location())
   	if !end.After(start) {
   		return !start.Before(from) && start.Before(to)
   	}
   	return start.Before(to) && end.After(from)
   }

   // civilMidnight takes t's calendar date (in t's own location) and returns
   // midnight of that date in loc.
   func civilMidnight(t time.Time, loc *time.Location) time.Time {
   	y, m, d := t.Date()
   	return time.Date(y, m, d, 0, 0, 0, 0, loc)
   }

   // StartOfDay returns local midnight of the day containing t.
   func StartOfDay(t time.Time) time.Time { return civilMidnight(t, t.Location()) }
   ```
4. Run: `go test ./internal/model/`. All should pass. If "all-day anchored to window zone" fails, re-read `Span`.

**Done when:** tests pass, `go vet` clean.

**Commit:** `feat(model): add Event with span/overlap semantics`

---

## Task 1.3: Event IDs and instance IDs

**Goal:** generate Google-compatible event IDs locally (design §2), and compute the deterministic ID Google
gives to an occurrence of a recurring event.

**Files:** create `internal/model/ids.go`, `internal/model/ids_test.go`

**Steps**

1. Test:
   ```go
   package model_test

   import (
   	"regexp"
   	"testing"
   	"time"

   	"github.com/yourname/clindar/internal/model"
   )

   func TestNewEventIDIsGoogleCompatibleAndUnique(t *testing.T) {
   	valid := regexp.MustCompile(`^[a-v0-9]+$`) // Google: chars a-v and 0-9, length 5–1024
   	seen := map[string]bool{}
   	for range 1000 {
   		id := model.NewEventID()
   		if !valid.MatchString(id) || len(id) < 5 || len(id) > 1024 {
   			t.Fatalf("NewEventID() = %q, not valid base32hex lowercase", id)
   		}
   		if seen[id] {
   			t.Fatalf("NewEventID() returned duplicate %q", id)
   		}
   		seen[id] = true
   	}
   }

   func TestInstanceID(t *testing.T) {
   	ny := mustLoc(t, "America/New_York")
   	tests := []struct {
   		name   string
   		start  time.Time
   		allDay bool
   		want   string
   	}{
   		{"timed uses UTC basic format", time.Date(2026, 11, 16, 9, 0, 0, 0, ny), false, "abc_20261116T140000Z"},
   		{"all-day uses date only", time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), true, "abc_20261116"},
   	}
   	for _, tt := range tests {
   		t.Run(tt.name, func(t *testing.T) {
   			if got := model.InstanceID("abc", tt.start, tt.allDay); got != tt.want {
   				t.Errorf("InstanceID = %q, want %q", got, tt.want)
   			}
   		})
   	}
   }
   ```
2. Run: fails to compile. Good.
3. Implement `internal/model/ids.go`:
   ```go
   package model

   import (
   	"crypto/rand"
   	"encoding/base32"
   	"strings"
   	"time"
   )

   var idEncoding = base32.HexEncoding.WithPadding(base32.NoPadding)

   // NewEventID returns a random 26-char ID using only [a-v0-9], which Google
   // accepts for client-supplied event IDs. Generating IDs locally means an event
   // created offline already has its final ID.
   func NewEventID() string {
   	b := make([]byte, 16)
   	rand.Read(b) // never returns an error since Go 1.24
   	return strings.ToLower(idEncoding.EncodeToString(b))
   }

   // InstanceID is the ID Google assigns to one occurrence of a recurring event.
   func InstanceID(seriesID string, originalStart time.Time, allDay bool) string {
   	if allDay {
   		return seriesID + "_" + originalStart.UTC().Format("20060102")
   	}
   	return seriesID + "_" + originalStart.UTC().Format("20060102T150405Z")
   }
   ```
4. Run tests: pass.

> ⚠️ The instance-ID format is Google behavior we rely on. Task 8.7 includes a manual check against a real
> calendar. If it's ever wrong, this function is the only place to fix.

**Commit:** `feat(model): local event IDs and instance IDs`

---

## Task 1.4: Clone and SelfAttendee

**Goal:** safe copying. `Event` contains slices. Copying the struct (`b := a`) shares the slice's backing array,
so changing `b.Attendees[0]` silently changes `a` as well. This bug is hard to spot, so we write a test that demonstrates it.

**Files:** modify `internal/model/event.go`, `internal/model/event_test.go`

**Steps**

1. Add tests:
   ```go
   func TestCloneDoesNotShareSlices(t *testing.T) {
   	orig := model.Event{
   		Recurrence: []string{"RRULE:FREQ=DAILY"},
   		Attendees:  []model.Attendee{{Email: "me@x.com", Self: true, Response: model.ResponseNeedsAction}},
   	}

   	c := orig.Clone()
   	c.Recurrence[0] = "changed"
   	c.SelfAttendee().Response = model.ResponseAccepted

   	if orig.Recurrence[0] != "RRULE:FREQ=DAILY" {
   		t.Errorf("orig.Recurrence changed to %q", orig.Recurrence[0])
   	}
   	if orig.Attendees[0].Response != model.ResponseNeedsAction {
   		t.Errorf("orig attendee response changed to %q", orig.Attendees[0].Response)
   	}
   }

   func TestSelfAttendeeNilWhenAbsent(t *testing.T) {
   	e := model.Event{Attendees: []model.Attendee{{Email: "other@x.com"}}}
   	if a := e.SelfAttendee(); a != nil {
   		t.Errorf("SelfAttendee() = %+v, want nil", a)
   	}
   }
   ```
2. Run: compile failure.
3. Implement (in `event.go`):
   ```go
   // Clone returns a deep copy. Always Clone before modifying an Event you didn't create.
   func (e Event) Clone() Event {
   	c := e
   	c.Recurrence = append([]string(nil), e.Recurrence...)
   	c.Attendees = append([]Attendee(nil), e.Attendees...)
   	return c
   }

   // SelfAttendee returns a pointer into e.Attendees for the current user, or nil.
   // Note the pointer receiver: the returned pointer modifies e itself.
   func (e *Event) SelfAttendee() *Attendee {
   	for i := range e.Attendees {
   		if e.Attendees[i].Self {
   			return &e.Attendees[i]
   		}
   	}
   	return nil
   }
   ```
   `c.SelfAttendee()` in the test works because `c` is addressable.
4. Run: pass. Note that `append([]T(nil), …)` turns an empty slice into `nil`. That's intended and keeps
   JSON round-trips stable.

**Commit:** `feat(model): deep Clone and SelfAttendee`

---

## Task 1.5: Calendar type, sorting, and grouping by day

**Goal:** the shared helpers every view uses to order events and bucket them into days. CLI pretty output,
the TUI list, and the TUI grid all call these. **Never re-implement grouping elsewhere.**

**Files:** create `internal/model/calendar.go`, `internal/model/days.go`, `internal/model/days_test.go`

**Steps**

1. `internal/model/calendar.go` (plain data, no test needed beyond the one-liner below):
   ```go
   package model

   // Calendar is a calendar the account can see, from Google's calendar list.
   type Calendar struct {
   	AccountID  string `json:"account_id"`
   	ID         string `json:"id"`
   	Name       string `json:"name"`
   	Color      string `json:"color,omitempty"`       // "#rrggbb"
   	AccessRole string `json:"access_role,omitempty"` // owner | writer | reader | freeBusyReader
   	Primary    bool   `json:"primary,omitempty"`
   	Selected   bool   `json:"selected"` // shown by default (Google's "selected" flag)
   	TimeZone   string `json:"time_zone,omitempty"`
   }

   func (c Calendar) Writable() bool { return c.AccessRole == "owner" || c.AccessRole == "writer" }
   ```
2. Test `internal/model/days_test.go`:
   ```go
   package model_test

   import (
   	"testing"
   	"time"

   	"github.com/yourname/clindar/internal/model"
   )

   func TestSortEventsAllDayFirstThenStartThenTitle(t *testing.T) {
   	ny := mustLoc(t, "America/New_York")
   	at := func(h int) time.Time { return time.Date(2026, 11, 14, h, 0, 0, 0, ny) }
   	events := []model.Event{
   		{Title: "B", Start: at(9), End: at(10)},
   		{Title: "A", Start: at(9), End: at(10)},
   		{Title: "early", Start: at(8), End: at(9)},
   		{Title: "holiday", AllDay: true,
   			Start: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
   			End:   time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)},
   	}

   	model.SortEvents(events, ny)

   	var got []string
   	for _, e := range events {
   		got = append(got, e.Title)
   	}
   	want := []string{"holiday", "early", "A", "B"}
   	if len(got) != len(want) {
   		t.Fatalf("got %v, want %v", got, want)
   	}
   	for i := range want {
   		if got[i] != want[i] {
   			t.Fatalf("got %v, want %v", got, want)
   		}
   	}
   }

   func TestGroupByDayPutsMultiDayEventsOnEveryDay(t *testing.T) {
   	ny := mustLoc(t, "America/New_York")
   	from := time.Date(2026, 11, 14, 15, 30, 0, 0, ny) // mid-day: grouping starts at that day's midnight
   	trip := model.Event{Title: "trip", AllDay: true,
   		Start: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
   		End:   time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)} // Sat+Sun
   	lunch := model.Event{Title: "lunch",
   		Start: time.Date(2026, 11, 15, 12, 0, 0, 0, ny),
   		End:   time.Date(2026, 11, 15, 13, 0, 0, 0, ny)}

   	days := model.GroupByDay([]model.Event{trip, lunch}, from, 3)

   	if len(days) != 3 {
   		t.Fatalf("len(days) = %d, want 3", len(days))
   	}
   	wantCounts := []int{1, 2, 0}
   	for i, d := range days {
   		if len(d.Events) != wantCounts[i] {
   			t.Errorf("day %d (%s): %d events, want %d", i, d.Date.Format("Mon 02"), len(d.Events), wantCounts[i])
   		}
   	}
   	if want := time.Date(2026, 11, 14, 0, 0, 0, 0, ny); !days[0].Date.Equal(want) {
   		t.Errorf("days[0].Date = %v, want %v", days[0].Date, want)
   	}
   }
   ```
3. Run: compile failure.
4. Implement `internal/model/days.go`:
   ```go
   package model

   import (
   	"cmp"
   	"slices"
   	"time"
   )

   // SortEvents orders events for display: all-day first, then by start, then title.
   func SortEvents(events []Event, loc *time.Location) {
   	slices.SortStableFunc(events, func(a, b Event) int {
   		if a.AllDay != b.AllDay {
   			if a.AllDay {
   				return -1
   			}
   			return 1
   		}
   		as, _ := a.Span(loc)
   		bs, _ := b.Span(loc)
   		if c := as.Compare(bs); c != 0 {
   			return c
   		}
   		return cmp.Compare(a.Title, b.Title)
   	})
   }

   // Day is one calendar day (local midnight) and the events touching it.
   type Day struct {
   	Date   time.Time
   	Events []Event
   }

   // GroupByDay buckets events into n consecutive days starting with the day
   // containing from (in from's location). Multi-day events appear on each day
   // they touch. Input order is preserved within a day, so sort first.
   func GroupByDay(events []Event, from time.Time, n int) []Day {
   	start := StartOfDay(from)
   	days := make([]Day, n)
   	for i := range days {
   		d := start.AddDate(0, 0, i) // AddDate, not Add(24h): DST days aren't 24h long
   		next := d.AddDate(0, 0, 1)
   		days[i].Date = d
   		for _, e := range events {
   			if e.Overlaps(d, next) {
   				days[i].Events = append(days[i].Events, e)
   			}
   		}
   	}
   	return days
   }
   ```
5. Run: pass.

**Commit:** `feat(model): calendar type, sorting, grouping by day`

---

## Task 1.6: Duration parsing

**Goal:** `--dur 1h30m`, `--dur 2d`, `in 90m`. Go's `time.ParseDuration` doesn't support days or weeks, so we write
our own.

**Files:** create `internal/timeparse/duration.go`, `internal/timeparse/duration_test.go`

**Steps**

1. Test:
   ```go
   package timeparse_test

   import (
   	"testing"
   	"time"

   	"github.com/yourname/clindar/internal/timeparse"
   )

   func TestParseDuration(t *testing.T) {
   	tests := []struct {
   		in      string
   		want    time.Duration
   		wantErr bool
   	}{
   		{in: "30m", want: 30 * time.Minute},
   		{in: "1h30m", want: 90 * time.Minute},
   		{in: "2d", want: 48 * time.Hour},
   		{in: "1w", want: 7 * 24 * time.Hour},
   		{in: " 1H ", want: time.Hour},
   		{in: "", wantErr: true},
   		{in: "h", wantErr: true},
   		{in: "10", wantErr: true}, // unit is required
   		{in: "5s", wantErr: true}, // seconds are not useful for calendars
   		{in: "1h-5m", wantErr: true},
   	}
   	for _, tt := range tests {
   		t.Run(tt.in, func(t *testing.T) {
   			got, err := timeparse.ParseDuration(tt.in)
   			if (err != nil) != tt.wantErr {
   				t.Fatalf("ParseDuration(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
   			}
   			if got != tt.want {
   				t.Errorf("ParseDuration(%q) = %v, want %v", tt.in, got, tt.want)
   			}
   		})
   	}
   }
   ```
2. Run: fails to compile.
3. Implement:
   ```go
   // Package timeparse turns the human date/time/duration strings accepted by the
   // CLI and TUI forms into time values. It never calls time.Now: callers pass now.
   package timeparse

   import (
   	"fmt"
   	"strconv"
   	"strings"
   	"time"
   )

   var units = map[byte]time.Duration{
   	'w': 7 * 24 * time.Hour,
   	'd': 24 * time.Hour,
   	'h': time.Hour,
   	'm': time.Minute,
   }

   // ParseDuration parses sequences like "30m", "1h30m", "2d", "1w".
   func ParseDuration(input string) (time.Duration, error) {
   	s := strings.ToLower(strings.TrimSpace(input))
   	if s == "" {
   		return 0, fmt.Errorf("empty duration")
   	}
   	var total time.Duration
   	for s != "" {
   		i := 0
   		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
   			i++
   		}
   		if i == 0 || i == len(s) {
   			return 0, fmt.Errorf("invalid duration %q (try 30m, 1h30m, 2d)", input)
   		}
   		unit, ok := units[s[i]]
   		if !ok {
   			return 0, fmt.Errorf("invalid duration %q: unknown unit %q (use w, d, h, m)", input, s[i])
   		}
   		n, err := strconv.Atoi(s[:i])
   		if err != nil {
   			return 0, fmt.Errorf("invalid duration %q: %w", input, err)
   		}
   		total += time.Duration(n) * unit
   		s = s[i+1:]
   	}
   	return total, nil
   }
   ```
4. Run: pass.

**Commit:** `feat(timeparse): parse durations with days and weeks`

---

## Task 1.7: Date/time parsing

**Goal:** `--at "tomorrow 2pm"`, `--from fri`, the TUI's "Start" field. These are the formats we support, and
they are the documentation for users too:

| Input | Meaning | `dateOnly` |
|---|---|---|
| `now` | now | no |
| `today` / `tomorrow` / `yesterday` | that date | yes |
| `mon` … `sun`, `monday` … | next such day; **today counts** | yes |
| `+3d`, `+1w` | date N days/weeks ahead | yes |
| `2026-11-20` | that date | yes |
| `14:00`, `2pm`, `9:30am` | today at that time | no |
| `<date> <time>` e.g. `fri 2pm`, `2026-11-20 09:15`, `2026-11-20T09:15` | combined | no |
| `in 2h`, `+90m` | now + duration (needs h or m) | no |
| RFC 3339 `2026-11-20T09:15:00Z` | exact instant, shown in local zone | no |

`dateOnly` tells callers (e.g. `add`) that the user gave no time. `add --at fri` then creates an all-day event.

**Files:** create `internal/timeparse/time.go`, `internal/timeparse/time_test.go`

**Steps**

1. Test. Note the fixed `now`: **Thursday** 2026-11-12 10:30 New York. Never use `time.Now()` in tests.
   ```go
   package timeparse_test

   import (
   	"testing"
   	"time"
   	_ "time/tzdata"

   	"github.com/yourname/clindar/internal/timeparse"
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
   ```
2. Run: compile failure.
3. Implement `internal/timeparse/time.go`:
   ```go
   package timeparse

   import (
   	"fmt"
   	"strconv"
   	"strings"
   	"time"
   )

   var weekdays = map[string]time.Weekday{
   	"sun": time.Sunday, "sunday": time.Sunday,
   	"mon": time.Monday, "monday": time.Monday,
   	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
   	"wed": time.Wednesday, "wednesday": time.Wednesday,
   	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
   	"fri": time.Friday, "friday": time.Friday,
   	"sat": time.Saturday, "saturday": time.Saturday,
   }

   // ParseTime parses input relative to now, returning a time in now's location.
   // dateOnly is true when the input named a day but no time of day.
   func ParseTime(input string, now time.Time) (t time.Time, dateOnly bool, err error) {
   	raw := strings.TrimSpace(input)
   	if raw == "" {
   		return time.Time{}, false, fmt.Errorf("empty time")
   	}
   	if t, err := time.Parse(time.RFC3339, raw); err == nil {
   		return t.In(now.Location()), false, nil
   	}
   	s := strings.ToLower(raw)
   	if s == "now" {
   		return now, false, nil
   	}
   	if rel, ok := strings.CutPrefix(s, "in "); ok {
   		return relative(rel, now, input)
   	}
   	if strings.HasPrefix(s, "+") && strings.ContainsAny(s, "hm") {
   		return relative(s[1:], now, input)
   	}
   	// "2026-11-20t09:15" → "2026-11-20 09:15"
   	if len(s) > 10 && s[4] == '-' && s[10] == 't' {
   		s = s[:10] + " " + s[11:]
   	}

   	fields := strings.Fields(s)
   	switch len(fields) {
   	case 1:
   		if d, ok := parseDate(fields[0], now); ok {
   			return d, true, nil
   		}
   		if h, m, ok := parseClock(fields[0]); ok {
   			return atClock(now, h, m), false, nil
   		}
   	case 2:
   		d, okDate := parseDate(fields[0], now)
   		h, m, okClock := parseClock(fields[1])
   		if okDate && okClock {
   			return atClock(d, h, m), false, nil
   		}
   	}
   	return time.Time{}, false, fmt.Errorf("cannot understand time %q (try: tomorrow 14:00, fri 2pm, 2026-11-20 09:15, in 2h)", input)
   }

   func relative(s string, now time.Time, input string) (time.Time, bool, error) {
   	d, err := ParseDuration(strings.ReplaceAll(s, " ", ""))
   	if err != nil {
   		return time.Time{}, false, fmt.Errorf("cannot understand time %q: %w", input, err)
   	}
   	return now.Add(d), false, nil
   }

   // parseDate understands today/tomorrow/yesterday, weekday names, +Nd/+Nw, and YYYY-MM-DD.
   func parseDate(s string, now time.Time) (time.Time, bool) {
   	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
   	switch s {
   	case "today":
   		return today, true
   	case "tomorrow":
   		return today.AddDate(0, 0, 1), true
   	case "yesterday":
   		return today.AddDate(0, 0, -1), true
   	}
   	if wd, ok := weekdays[s]; ok {
   		ahead := (int(wd) - int(today.Weekday()) + 7) % 7
   		return today.AddDate(0, 0, ahead), true
   	}
   	if len(s) >= 3 && s[0] == '+' {
   		n, err := strconv.Atoi(s[1 : len(s)-1])
   		if err == nil && n >= 0 {
   			switch s[len(s)-1] {
   			case 'd':
   				return today.AddDate(0, 0, n), true
   			case 'w':
   				return today.AddDate(0, 0, 7*n), true
   			}
   		}
   	}
   	if d, err := time.ParseInLocation("2006-01-02", s, now.Location()); err == nil {
   		return d, true
   	}
   	return time.Time{}, false
   }

   // parseClock understands "14:00", "9:30am", "2pm". A bare "14" is rejected as ambiguous.
   func parseClock(s string) (h, m int, ok bool) {
   	am, pm := strings.HasSuffix(s, "am"), strings.HasSuffix(s, "pm")
   	if am || pm {
   		s = s[:len(s)-2]
   	}
   	hs, ms, hasColon := strings.Cut(s, ":")
   	if !hasColon && !am && !pm {
   		return 0, 0, false
   	}
   	h, err := strconv.Atoi(hs)
   	if err != nil || len(hs) == 0 || len(hs) > 2 {
   		return 0, 0, false
   	}
   	if hasColon {
   		if len(ms) != 2 {
   			return 0, 0, false
   		}
   		if m, err = strconv.Atoi(ms); err != nil {
   			return 0, 0, false
   		}
   	}
   	if am || pm {
   		if h < 1 || h > 12 {
   			return 0, 0, false
   		}
   		if h == 12 {
   			h = 0
   		}
   		if pm {
   			h += 12
   		}
   	}
   	if h < 0 || h > 23 || m < 0 || m > 59 {
   		return 0, 0, false
   	}
   	return h, m, true
   }

   func atClock(day time.Time, h, m int) time.Time {
   	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, day.Location())
   }
   ```
4. Run: `go test ./internal/timeparse/ -v`. If a row fails, read its name. The table tells you exactly which
   behavior broke.

**Commit:** `feat(timeparse): human-friendly date/time parsing`

---

## Task 1.8: Paths (XDG + overrides)

**Goal:** one place that knows where files live. Tests will point `CLINDAR_CONFIG_DIR`/`CLINDAR_DATA_DIR` at
`t.TempDir()`, so they never touch your real config.

**Files:** create `internal/config/paths.go`, `internal/config/paths_test.go`

**Steps**

1. Test:
   ```go
   package config_test

   import (
   	"path/filepath"
   	"testing"

   	"github.com/yourname/clindar/internal/config"
   )

   func TestDefaultPathsPrecedence(t *testing.T) {
   	home := t.TempDir()
   	t.Setenv("HOME", home)

   	t.Run("fallback to ~/.config and ~/.local/share", func(t *testing.T) {
   		t.Setenv("XDG_CONFIG_HOME", "")
   		t.Setenv("XDG_DATA_HOME", "")
   		t.Setenv("CLINDAR_CONFIG_DIR", "")
   		t.Setenv("CLINDAR_DATA_DIR", "")
   		p, err := config.DefaultPaths()
   		if err != nil {
   			t.Fatal(err)
   		}
   		if want := filepath.Join(home, ".config", "clindar"); p.ConfigDir != want {
   			t.Errorf("ConfigDir = %q, want %q", p.ConfigDir, want)
   		}
   		if want := filepath.Join(home, ".local", "share", "clindar"); p.DataDir != want {
   			t.Errorf("DataDir = %q, want %q", p.DataDir, want)
   		}
   	})

   	t.Run("XDG vars", func(t *testing.T) {
   		t.Setenv("XDG_CONFIG_HOME", "/xdg/c")
   		t.Setenv("XDG_DATA_HOME", "/xdg/d")
   		t.Setenv("CLINDAR_CONFIG_DIR", "")
   		t.Setenv("CLINDAR_DATA_DIR", "")
   		p, _ := config.DefaultPaths()
   		if p.ConfigDir != "/xdg/c/clindar" || p.DataDir != "/xdg/d/clindar" {
   			t.Errorf("got %+v", p)
   		}
   	})

   	t.Run("CLINDAR_* overrides win", func(t *testing.T) {
   		t.Setenv("XDG_CONFIG_HOME", "/xdg/c")
   		t.Setenv("CLINDAR_CONFIG_DIR", "/o/c")
   		t.Setenv("CLINDAR_DATA_DIR", "/o/d")
   		p, _ := config.DefaultPaths()
   		if p.ConfigDir != "/o/c" || p.DataDir != "/o/d" {
   			t.Errorf("got %+v", p)
   		}
   		if p.DBFile() != "/o/d/clindar.db" {
   			t.Errorf("DBFile = %q", p.DBFile())
   		}
   	})
   }
   ```
2. Run: compile failure.
3. Implement:
   ```go
   // Package config resolves file locations and loads the user's config file.
   package config

   import (
   	"fmt"
   	"os"
   	"path/filepath"
   )

   // Paths are the two directories clindar uses. We follow XDG on macOS too:
   // CLI users expect ~/.config, and it keeps one code path.
   type Paths struct {
   	ConfigDir string // config.toml, client.json
   	DataDir   string // clindar.db, sync.lock, token.json fallback
   }

   func DefaultPaths() (Paths, error) {
   	home, err := os.UserHomeDir()
   	if err != nil {
   		return Paths{}, fmt.Errorf("find home directory: %w", err)
   	}
   	return Paths{
   		ConfigDir: resolve("CLINDAR_CONFIG_DIR", "XDG_CONFIG_HOME", filepath.Join(home, ".config")),
   		DataDir:   resolve("CLINDAR_DATA_DIR", "XDG_DATA_HOME", filepath.Join(home, ".local", "share")),
   	}, nil
   }

   func resolve(override, xdgVar, fallbackBase string) string {
   	if v := os.Getenv(override); v != "" {
   		return v
   	}
   	if v := os.Getenv(xdgVar); v != "" {
   		return filepath.Join(v, "clindar")
   	}
   	return filepath.Join(fallbackBase, "clindar")
   }

   func (p Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "config.toml") }
   func (p Paths) ClientFile() string { return filepath.Join(p.ConfigDir, "client.json") }
   func (p Paths) DBFile() string     { return filepath.Join(p.DataDir, "clindar.db") }
   func (p Paths) LockFile() string   { return filepath.Join(p.DataDir, "sync.lock") }
   func (p Paths) TokenFile() string  { return filepath.Join(p.DataDir, "token.json") }

   // EnsureDirs creates both directories, private to the user.
   func (p Paths) EnsureDirs() error {
   	for _, d := range []string{p.ConfigDir, p.DataDir} {
   		if err := os.MkdirAll(d, 0o700); err != nil {
   			return fmt.Errorf("create %s: %w", d, err)
   		}
   	}
   	return nil
   }
   ```
4. Run: pass.

**Commit:** `feat(config): XDG paths with CLINDAR_* overrides`

---

## Task 1.9: Config file

**Goal:** load `config.toml`. Every key is optional, a missing file means defaults, and an unknown key is an
error, so typos don't silently do nothing.

**Files:** create `internal/config/config.go`, `internal/config/config_test.go`. Dependency:
```bash
go get github.com/BurntSushi/toml@v1.6.0
```

**Steps**

1. Test:
   ```go
   package config_test

   import (
   	"os"
   	"path/filepath"
   	"strings"
   	"testing"
   	"time"

   	"github.com/yourname/clindar/internal/config"
   )

   func writeFile(t *testing.T, content string) string {
   	t.Helper()
   	path := filepath.Join(t.TempDir(), "config.toml")
   	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
   		t.Fatal(err)
   	}
   	return path
   }

   func TestLoadMissingFileGivesDefaults(t *testing.T) {
   	c, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
   	if err != nil {
   		t.Fatal(err)
   	}
   	if c != config.Default() {
   		t.Errorf("got %+v, want defaults %+v", c, config.Default())
   	}
   }

   func TestLoadOverridesOnlyGivenKeys(t *testing.T) {
   	c, err := config.Load(writeFile(t, "sync_interval = \"20s\"\nweek_start = \"sunday\"\n"))
   	if err != nil {
   		t.Fatal(err)
   	}
   	if c.SyncInterval.Duration != 20*time.Second {
   		t.Errorf("SyncInterval = %v", c.SyncInterval)
   	}
   	if c.FirstWeekday() != time.Sunday {
   		t.Errorf("FirstWeekday = %v", c.FirstWeekday())
   	}
   	if c.StaleAfter != config.Default().StaleAfter {
   		t.Errorf("StaleAfter changed to %v", c.StaleAfter)
   	}
   }

   func TestLoadRejectsBadInput(t *testing.T) {
   	tests := map[string]string{
   		"unknown key":        "sync_intervall = \"20s\"\n",
   		"bad duration":       "sync_interval = \"soon\"\n",
   		"bad week_start":     "week_start = \"wednesday\"\n",
   		"bad send_updates":   "send_updates = \"some\"\n",
   		"interval too short": "sync_interval = \"1s\"\n",
   	}
   	for name, content := range tests {
   		t.Run(name, func(t *testing.T) {
   			_, err := config.Load(writeFile(t, content))
   			if err == nil {
   				t.Fatal("expected error")
   			}
   			if !strings.Contains(err.Error(), "config.toml") {
   				t.Errorf("error %q should mention the file", err)
   			}
   		})
   	}
   }
   ```
2. Run: compile failure.
3. Implement:
   ```go
   package config

   import (
   	"errors"
   	"fmt"
   	"io/fs"
   	"time"

   	"github.com/BurntSushi/toml"
   )

   // Duration lets TOML values like "45s" decode into a time.Duration.
   type Duration struct{ time.Duration }

   func (d *Duration) UnmarshalText(b []byte) error {
   	v, err := time.ParseDuration(string(b))
   	if err != nil {
   		return err
   	}
   	d.Duration = v
   	return nil
   }

   type Config struct {
   	SyncInterval    Duration `toml:"sync_interval"`    // daemon/TUI poll period
   	StaleAfter      Duration `toml:"stale_after"`      // CLI reads sync first if older than this
   	SyncTimeout     Duration `toml:"sync_timeout"`     // max time a CLI read waits for that sync
   	DefaultCalendar string   `toml:"default_calendar"` // name or ID; "" = primary
   	WeekStart       string   `toml:"week_start"`       // "monday" | "sunday"
   	SendUpdates     string   `toml:"send_updates"`     // "all" | "none": email attendees on changes
   }

   func Default() Config {
   	return Config{
   		SyncInterval: Duration{45 * time.Second},
   		StaleAfter:   Duration{2 * time.Minute},
   		SyncTimeout:  Duration{5 * time.Second},
   		WeekStart:    "monday",
   		SendUpdates:  "all",
   	}
   }

   // Load reads path over the defaults. A missing file is not an error.
   func Load(path string) (Config, error) {
   	c := Default()
   	md, err := toml.DecodeFile(path, &c)
   	if errors.Is(err, fs.ErrNotExist) {
   		return Default(), nil
   	}
   	if err != nil {
   		return Config{}, fmt.Errorf("%s: %w", path, err)
   	}
   	if und := md.Undecoded(); len(und) > 0 {
   		return Config{}, fmt.Errorf("%s: unknown key %q", path, und[0].String())
   	}
   	if err := c.validate(); err != nil {
   		return Config{}, fmt.Errorf("%s: %w", path, err)
   	}
   	return c, nil
   }

   func (c Config) validate() error {
   	if c.WeekStart != "monday" && c.WeekStart != "sunday" {
   		return fmt.Errorf("week_start must be \"monday\" or \"sunday\", got %q", c.WeekStart)
   	}
   	if c.SendUpdates != "all" && c.SendUpdates != "none" {
   		return fmt.Errorf("send_updates must be \"all\" or \"none\", got %q", c.SendUpdates)
   	}
   	if c.SyncInterval.Duration < 10*time.Second {
   		return fmt.Errorf("sync_interval must be at least 10s (Google rate limits), got %v", c.SyncInterval)
   	}
   	return nil
   }

   func (c Config) FirstWeekday() time.Weekday {
   	if c.WeekStart == "sunday" {
   		return time.Sunday
   	}
   	return time.Monday
   }
   ```
4. `go mod tidy`, then run tests: pass.

**Commit:** `feat(config): TOML config with defaults and validation`

---

## Task 1.10: Event spans (start + end/duration)

**Goal:** one function that turns "start / end / duration" input into an event's timing. The CLI's
`--at/--end/--dur/--all-day` flags and the TUI form's Start/End/Duration fields both call it. (DRY: when
someone asks "what does `--end 15:30` mean?", there is exactly one answer, in one place.)

Rules:
- `end` is parsed **relative to the start**, so `--at "fri 14:00" --end 15:30` means Friday 15:30.
- A start with no time ("fri") makes the event **all-day**.
- For all-day events, `end` is the **last day, inclusive** (humans say "Fri to Sun"). We convert it to
  Google's exclusive end.
- The defaults are 1 hour, or 1 day for all-day events. You can't give both an end and a duration.

**Files:** create `internal/timeparse/span_test.go`, `internal/timeparse/span.go`

**Steps**

1. Test:
```go
package timeparse_test

import (
	"testing"
	"time"

	"github.com/yourname/clindar/internal/timeparse"
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
```
2. Run `go test ./internal/timeparse/`. It should fail to compile.
3. Implement:
```go
package timeparse

import (
	"fmt"
	"time"
)

// Span is an event's timing as typed by a user: the result of interpreting
// the --at/--end/--dur/--all-day flags (CLI) or the Start/End/Duration fields (TUI).
type Span struct {
	Start, End time.Time
	AllDay     bool
}

// ParseSpan interprets the timing inputs. --end is parsed relative to the
// start ("--at 'fri 14:00' --end 15:30" means Friday 15:30). For all-day
// events --end is the last day, inclusive.
func ParseSpan(now time.Time, at, end, dur string, allDayFlag bool) (Span, error) {
	if at == "" {
		return Span{}, fmt.Errorf("a start time is required")
	}
	if end != "" && dur != "" {
		return Span{}, fmt.Errorf("give an end or a duration, not both")
	}
	start, dateOnly, err := ParseTime(at, now)
	if err != nil {
		return Span{}, fmt.Errorf("start: %w", err)
	}
	t := Span{Start: start, AllDay: allDayFlag || dateOnly}
	if t.AllDay {
		t.Start = startOfDay(start)
	}
	switch {
	case end != "":
		e, _, err := ParseTime(end, t.Start)
		if err != nil {
			return Span{}, fmt.Errorf("end: %w", err)
		}
		if t.AllDay {
			e = startOfDay(e).AddDate(0, 0, 1) // inclusive last day → exclusive end
		}
		t.End = e
	case dur != "":
		d, err := ParseDuration(dur)
		if err != nil {
			return Span{}, fmt.Errorf("duration: %w", err)
		}
		t.End = t.Start.Add(d)
	case t.AllDay:
		t.End = t.Start.AddDate(0, 0, 1)
	default:
		t.End = t.Start.Add(time.Hour)
	}
	if !t.End.After(t.Start) {
		return Span{}, fmt.Errorf("end must be after start")
	}
	return t, nil
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
```
4. Run: pass.

**Commit:** `feat(timeparse): ParseSpan for start/end/duration input`

---

## Phase 1 checkpoint

```bash
gofmt -l . && go vet ./... && go test ./... -count=1
```
All green. `git log --oneline` shows ~9 small commits.
