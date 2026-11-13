# Phase 3: Recurrence

Google sends recurring events as a **master** (with `RRULE` lines) plus **exceptions** (changed or cancelled
occurrences). We expand masters into instances **locally**, which is why offline-created series show up
correctly. This package is pure functions, with no I/O.

Read first:
- README primer: "rrule-go" and Google "Key facts" #3.
- RFC 5545 §3.3.10 (RRULE), skimming only: <https://datatracker.ietf.org/doc/html/rfc5545#section-3.3.10>.
  Understand `FREQ`, `BYDAY`, `COUNT`, `UNTIL`, and `EXDATE`.

```bash
go get github.com/teambition/rrule-go@v1.8.2
```

Vocabulary used in the code:
- **slot**: a start time the RRULE generates, whether or not something is there now.
- **instance**: what the user sees at a slot (virtual, or a stored exception).
- **OriginalStart**: the slot an exception replaces, even if the exception moved to another time.

---

## Task 3.1: Parse instance IDs

**Goal:** the inverse of `model.InstanceID` (Task 1.3). Without it, `clindar show <instance-id>` can't find
an occurrence that has no stored exception.

**Files:** modify `internal/model/ids.go` and `internal/model/ids_test.go` (append)

**Steps**
1. Append to `ids_test.go`:
```go
func TestParseInstanceIDRoundTrips(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	for _, tc := range []struct {
		start  time.Time
		allDay bool
	}{
		{time.Date(2026, 11, 16, 9, 0, 0, 0, ny), false},
		{time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), true},
	} {
		id := model.InstanceID("abc", tc.start, tc.allDay)

		series, start, allDay, ok := model.ParseInstanceID(id)

		if !ok || series != "abc" || !start.Equal(tc.start) || allDay != tc.allDay {
			t.Errorf("ParseInstanceID(%q) = %q, %v, %v, %v", id, series, start, allDay, ok)
		}
	}
	for _, id := range []string{"abc", "_20261116", "abc_notadate"} {
		if _, _, _, ok := model.ParseInstanceID(id); ok {
			t.Errorf("ParseInstanceID(%q) ok = true, want false", id)
		}
	}
}
```
2. Run: compile failure.
3. Append to `ids.go`:
```go
// ParseInstanceID is the inverse of InstanceID. ok is false for IDs that
// aren't instance IDs (event IDs never contain "_": base32hex has no underscore).
func ParseInstanceID(id string) (seriesID string, originalStart time.Time, allDay, ok bool) {
	i := strings.LastIndexByte(id, '_')
	if i <= 0 {
		return "", time.Time{}, false, false
	}
	seriesID, stamp := id[:i], id[i+1:]
	if t, err := time.Parse("20060102T150405Z", stamp); err == nil {
		return seriesID, t, false, true
	}
	if t, err := time.Parse("20060102", stamp); err == nil {
		return seriesID, t, true, true
	}
	return "", time.Time{}, false, false
}
```
4. Run: pass.

**Commit:** `feat(model): ParseInstanceID`

---

## Task 3.2: Expand a series

**Goal:** `Expand(master, exceptions, from, to)` returns the instances overlapping the window, with
exceptions applied. It is the core of every calendar view.

Behaviors (each has a test):
- Instances keep their **wall-clock time across DST** (09:00 stays 09:00 in November). This works because
  we expand in the series' own time zone.
- A modified exception replaces its slot, even if it moved outside the window or into it.
- A cancelled exception removes its slot.
- `EXDATE` and `COUNT` are honored.
- All-day series work.

**Files:** create `internal/recur/recur_test.go`, `internal/recur/recur.go`

**Steps**
1. Test:
```go
package recur_test

import (
	"errors"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/yourname/clindar/internal/model"
	"github.com/yourname/clindar/internal/recur"
	"github.com/yourname/clindar/internal/testutil"
)

// weeklyStandup: Mondays and Wednesdays 09:00–09:15 New York, starting Mon Nov 2 2026.
func weeklyStandup(t *testing.T, rule string) model.Event {
	t.Helper()
	ny := testutil.NY(t)
	m := testutil.Timed("std", "Standup", time.Date(2026, 11, 2, 9, 0, 0, 0, ny), 15*time.Minute)
	m.Recurrence = []string{rule}
	return m
}

func starts(events []model.Event, loc *time.Location) []string {
	var out []string
	model.SortEvents(events, loc)
	for _, e := range events {
		out = append(out, e.Start.In(loc).Format("Jan 02 15:04")+" "+e.Title)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestExpandWithinWindow(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO,WE")

	got, err := recur.Expand(m, nil, time.Date(2026, 11, 9, 0, 0, 0, 0, ny), time.Date(2026, 11, 16, 0, 0, 0, 0, ny))

	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Nov 09 09:00 Standup", "Nov 11 09:00 Standup"}
	if g := starts(got, ny); !equal(g, want) {
		t.Errorf("got %v, want %v", g, want)
	}
	if got[0].ID != "std_20261109T140000Z" || got[0].SeriesID != "std" || got[0].IsMaster() {
		t.Errorf("instance identity wrong: %+v", got[0])
	}
}

func TestExpandKeepsWallClockAcrossDST(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO")
	m.Start = time.Date(2026, 10, 26, 9, 0, 0, 0, ny) // before DST ends on Nov 1
	m.End = m.Start.Add(15 * time.Minute)

	got, _ := recur.Expand(m, nil, time.Date(2026, 11, 2, 0, 0, 0, 0, ny), time.Date(2026, 11, 3, 0, 0, 0, 0, ny))

	if len(got) != 1 || got[0].Start.In(ny).Hour() != 9 {
		t.Errorf("got %v, want one instance at 09:00 local", starts(got, ny))
	}
}

func TestExpandAppliesExceptions(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO,WE")
	mon := time.Date(2026, 11, 9, 9, 0, 0, 0, ny)
	wed := time.Date(2026, 11, 11, 9, 0, 0, 0, ny)

	moved := recur.Instance(m, mon)
	moved.Title = "Standup (moved)"
	moved.Start = time.Date(2026, 11, 10, 10, 0, 0, 0, ny) // moved to Tuesday 10:00
	moved.End = moved.Start.Add(15 * time.Minute)
	cancelled := recur.Instance(m, wed)
	cancelled.Status = model.StatusCancelled

	got, _ := recur.Expand(m, []model.Event{moved, cancelled},
		time.Date(2026, 11, 9, 0, 0, 0, 0, ny), time.Date(2026, 11, 16, 0, 0, 0, 0, ny))

	want := []string{"Nov 10 10:00 Standup (moved)"}
	if g := starts(got, ny); !equal(g, want) {
		t.Errorf("got %v, want %v", g, want)
	}
}

func TestExpandHonoursExdateAndCount(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=3")
	m.Recurrence = append(m.Recurrence, "EXDATE;TZID=America/New_York:20261109T090000")

	got, _ := recur.Expand(m, nil, time.Date(2026, 11, 1, 0, 0, 0, 0, ny), time.Date(2027, 1, 1, 0, 0, 0, 0, ny))

	want := []string{"Nov 02 09:00 Standup", "Nov 16 09:00 Standup"}
	if g := starts(got, ny); !equal(g, want) {
		t.Errorf("got %v, want %v", g, want)
	}
}

func TestExpandAllDaySeries(t *testing.T) {
	ny := testutil.NY(t)
	m := testutil.AllDay("bday", "Birthday", 2020, time.November, 14, 1)
	m.Recurrence = []string{"RRULE:FREQ=YEARLY"}

	got, _ := recur.Expand(m, nil, time.Date(2026, 11, 14, 0, 0, 0, 0, ny), time.Date(2026, 11, 15, 0, 0, 0, 0, ny))

	if len(got) != 1 || got[0].ID != "bday_20261114" || !got[0].AllDay {
		t.Errorf("got %+v", got)
	}
}

func TestLookup(t *testing.T) {
	ny := testutil.NY(t)
	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO,WE")
	mon := time.Date(2026, 11, 9, 9, 0, 0, 0, ny)

	inst, err := recur.Lookup(m, nil, mon)
	if err != nil || inst.ID != "std_20261109T140000Z" {
		t.Errorf("Lookup(valid slot) = %v, %v", inst.ID, err)
	}
	_, err = recur.Lookup(m, nil, mon.Add(time.Hour))
	if !errors.Is(err, recur.ErrNoSuchInstance) {
		t.Errorf("Lookup(not a slot) err = %v, want ErrNoSuchInstance", err)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
// Package recur expands recurring series into instances and rewrites
// recurrence rules for "this and following" edits. Pure functions only.
package recur

import (
	"errors"
	"fmt"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/yourname/clindar/internal/model"
)

var ErrNoSuchInstance = errors.New("no such instance in series")

// location returns the zone the series' wall-clock times are defined in.
func location(m model.Event) *time.Location {
	if m.AllDay {
		return time.UTC // all-day events are stored as UTC midnights
	}
	if m.TimeZone != "" {
		if loc, err := time.LoadLocation(m.TimeZone); err == nil {
			return loc
		}
	}
	return m.Start.Location()
}

func ruleSet(m model.Event) (*rrule.Set, error) {
	loc := location(m)
	set, err := rrule.StrSliceToRRuleSetInLoc(m.Recurrence, loc)
	if err != nil {
		return nil, fmt.Errorf("series %s: parse recurrence %q: %w", m.ID, m.Recurrence, err)
	}
	set.DTStart(m.Start.In(loc))
	return set, nil
}

// Instance builds the virtual instance of master at originalStart.
func Instance(master model.Event, originalStart time.Time) model.Event {
	inst := master.Clone()
	inst.ID = model.InstanceID(master.ID, originalStart, master.AllDay)
	inst.SeriesID = master.ID
	inst.OriginalStart = originalStart
	inst.Start = originalStart
	inst.End = originalStart.Add(master.Duration())
	inst.Recurrence = nil
	inst.ETag = ""
	return inst
}

// Expand returns the instances of master overlapping [from, to). A stored
// exception replaces the instance at its OriginalStart; a cancelled exception
// removes it. Exceptions whose slot is no longer in the series are ignored.
func Expand(master model.Event, exceptions []model.Event, from, to time.Time) ([]model.Event, error) {
	set, err := ruleSet(master)
	if err != nil {
		return nil, err
	}
	replaced := map[int64]bool{}
	var out []model.Event
	for _, ex := range exceptions {
		if !isSlot(set, ex.OriginalStart) {
			continue
		}
		replaced[ex.OriginalStart.Unix()] = true
		if !ex.Cancelled() && ex.Overlaps(from, to) {
			out = append(out, ex)
		}
	}
	// Search wider than the window: an instance that started before `from` may
	// still be running, and all-day instances shift by up to a day across zones.
	slots := set.Between(from.Add(-master.Duration()-24*time.Hour), to.Add(24*time.Hour), true)
	for _, slot := range slots {
		if replaced[slot.Unix()] {
			continue
		}
		if inst := Instance(master, slot); inst.Overlaps(from, to) {
			out = append(out, inst)
		}
	}
	return out, nil
}

// Lookup returns the instance of master at originalStart: the stored exception
// if there is one, else the virtual instance. Cancelled or non-existent slots
// return ErrNoSuchInstance.
func Lookup(master model.Event, exceptions []model.Event, originalStart time.Time) (model.Event, error) {
	for _, ex := range exceptions {
		if ex.OriginalStart.Equal(originalStart) {
			if ex.Cancelled() {
				return model.Event{}, ErrNoSuchInstance
			}
			return ex, nil
		}
	}
	set, err := ruleSet(master)
	if err != nil {
		return model.Event{}, err
	}
	if !isSlot(set, originalStart) {
		return model.Event{}, ErrNoSuchInstance
	}
	return Instance(master, originalStart), nil
}

func isSlot(set *rrule.Set, t time.Time) bool {
	return len(set.Between(t, t, true)) == 1
}
```
4. Run: pass. If the DST test fails, you're probably expanding in UTC. Look at `location()`.

**Commit:** `feat(recur): expand series with exceptions`

---

## Task 3.3: Split a series ("this and following")

**Goal:** `SplitAt(master, at)` returns the recurrence lines for the old series, ending just before `at`,
and for a new series starting at `at`. The service (Phase 4) uses it to implement "edit/delete this and
following".

Subtleties covered by tests:
- `COUNT=5` split after 2 used slots gives the new series `COUNT=3`. COUNT counts *generated* slots,
  **including EXDATE'd ones**, so we count with the bare RRULE, not the set.
- `UNTIL` must be the same type as DTSTART: a UTC timestamp for timed series, a date for all-day series.
- Splitting at the first instance is meaningless. Return `ErrSplitAtFirst`, and the caller treats it as "all".

We edit the RRULE as a `;`-separated list of `KEY=VALUE` parts instead of round-tripping through rrule-go.
Round-tripping reorders parts and turns date UNTILs into timestamps, which Google rejects for all-day events.

**Files:** create `internal/recur/split_test.go`, `internal/recur/split.go`

**Steps**
1. Test:
```go
package recur_test

import (
	"errors"
	"testing"
	"time"

	"github.com/yourname/clindar/internal/recur"
	"github.com/yourname/clindar/internal/testutil"
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
```
2. Run: compile failure.
3. Implement:
```go
package recur

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yourname/clindar/internal/model"
)

var ErrSplitAtFirst = errors.New("split point is the first instance; edit the whole series instead")

// SplitAt divides a series at the instance originally starting at `at`.
// before keeps the series' instances strictly before `at`; after describes the
// remaining instances, for a new series starting at `at`. COUNT is shared out
// between the two; EXDATE/RDATE lines are copied to both (out-of-range ones are harmless).
func SplitAt(master model.Event, at time.Time) (before, after []string, err error) {
	set, err := ruleSet(master)
	if err != nil {
		return nil, nil, err
	}
	rr := set.GetRRule()
	if rr == nil {
		return nil, nil, fmt.Errorf("series %s has no RRULE", master.ID)
	}
	if !isSlot(set, at) {
		return nil, nil, ErrNoSuchInstance
	}
	// COUNT counts generated slots, including EXDATE'd ones, so count with the bare RRULE.
	used := len(rr.Between(master.Start, at.Add(-time.Second), true))
	if used == 0 {
		return nil, nil, ErrSplitAtFirst
	}
	for _, line := range master.Recurrence {
		body, isRule := strings.CutPrefix(line, "RRULE:")
		if !isRule {
			before = append(before, line)
			after = append(after, line)
			continue
		}
		parts := ruleParts(body)
		beforeRule := withPart(withoutPart(parts, "COUNT", "UNTIL"), "UNTIL", untilValue(at.Add(-time.Second), master.AllDay))
		afterRule := parts
		if c, ok := partValue(parts, "COUNT"); ok {
			total, err := strconv.Atoi(c)
			if err != nil {
				return nil, nil, fmt.Errorf("series %s: bad COUNT %q", master.ID, c)
			}
			afterRule = withPart(withoutPart(parts, "COUNT"), "COUNT", strconv.Itoa(total-used))
		}
		before = append(before, "RRULE:"+strings.Join(beforeRule, ";"))
		after = append(after, "RRULE:"+strings.Join(afterRule, ";"))
	}
	return before, after, nil
}

// untilValue formats t for an UNTIL clause: a UTC timestamp for timed series,
// a plain date for all-day ones (RFC 5545 requires UNTIL to match DTSTART's type).
func untilValue(t time.Time, allDay bool) string {
	if allDay {
		return t.UTC().Format("20060102")
	}
	return t.UTC().Format("20060102T150405Z")
}

func ruleParts(body string) []string { return strings.Split(body, ";") }

func partValue(parts []string, key string) (string, bool) {
	for _, p := range parts {
		if k, v, _ := strings.Cut(p, "="); strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

func withoutPart(parts []string, keys ...string) []string {
	var out []string
	for _, p := range parts {
		k, _, _ := strings.Cut(p, "=")
		drop := false
		for _, key := range keys {
			drop = drop || strings.EqualFold(k, key)
		}
		if !drop {
			out = append(out, p)
		}
	}
	return out
}

func withPart(parts []string, key, value string) []string {
	return append(withoutPart(parts, key), key+"="+value)
}
```
4. Run: pass.

**Commit:** `feat(recur): split series for this-and-following edits`

---

## Task 3.4: `--repeat` shorthands

**Goal:** turn `daily | weekdays | weekly | monthly | yearly | FREQ=...` plus `--until`/`--count` into a
validated RRULE line. The CLI and the TUI form both use it.

**Files:** create `internal/recur/repeat_test.go`, `internal/recur/repeat.go`

**Steps**
1. Test:
```go
package recur_test

import (
	"testing"
	"time"

	"github.com/yourname/clindar/internal/recur"
	"github.com/yourname/clindar/internal/testutil"
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
```
2. Run: compile failure.
3. Implement:
```go
package recur

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

var weekdayCodes = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// ParseRepeat turns a --repeat value into recurrence lines for a series
// starting at start. spec is daily | weekdays | weekly | monthly | yearly, or a
// raw rule ("FREQ=WEEKLY;BYDAY=MO,TH", optionally prefixed "RRULE:").
// until (zero = none) and count (0 = none) bound the series; not both.
func ParseRepeat(spec string, start time.Time, allDay bool, until time.Time, count int) ([]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		if !until.IsZero() || count > 0 {
			return nil, fmt.Errorf("--until/--count need --repeat")
		}
		return nil, nil
	}
	var parts []string
	switch strings.ToLower(spec) {
	case "daily":
		parts = []string{"FREQ=DAILY"}
	case "weekdays":
		parts = []string{"FREQ=WEEKLY", "BYDAY=MO,TU,WE,TH,FR"}
	case "weekly":
		parts = []string{"FREQ=WEEKLY", "BYDAY=" + weekdayCodes[start.Weekday()]}
	case "monthly":
		parts = []string{"FREQ=MONTHLY"}
	case "yearly":
		parts = []string{"FREQ=YEARLY"}
	default:
		body := strings.TrimPrefix(strings.ToUpper(spec), "RRULE:")
		if !strings.HasPrefix(body, "FREQ=") {
			return nil, fmt.Errorf("unknown repeat %q (use daily, weekdays, weekly, monthly, yearly, or FREQ=...)", spec)
		}
		parts = ruleParts(body)
	}
	switch {
	case !until.IsZero() && count > 0:
		return nil, fmt.Errorf("use --until or --count, not both")
	case !until.IsZero():
		// Include the whole "until" day.
		endOfDay := time.Date(until.Year(), until.Month(), until.Day(), 23, 59, 59, 0, until.Location())
		if allDay {
			endOfDay = time.Date(until.Year(), until.Month(), until.Day(), 0, 0, 0, 0, time.UTC)
		}
		parts = withPart(parts, "UNTIL", untilValue(endOfDay, allDay))
	case count > 0:
		parts = withPart(parts, "COUNT", strconv.Itoa(count))
	}
	body := strings.Join(parts, ";")
	if _, err := rrule.StrToROption(body); err != nil {
		return nil, fmt.Errorf("invalid repeat rule %q: %w", body, err)
	}
	return []string{"RRULE:" + body}, nil
}
```
4. Run: pass.

**Commit:** `feat(recur): parse --repeat shorthands`

---

## Phase 3 checkpoint
`gofmt -l . && go vet ./... && go test ./... -count=1`
