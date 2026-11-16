# Phase 13: Fixes from testing

Bugs found after Phases 1–12, by a tester using clindar and by the implementer. Each task is **one commit on the
implementation branch**, shown as exact diffs (tests first, then code). They were written and tested there. If you're
building from this plan, apply them in order after Phase 12. Earlier tasks carry a "⚠ Fixed in Task 13.N" note
where they're affected.

A pattern worth noticing: **most of these slipped past the tests because the tests checked a different shape of data
than real use.** Sorted lists with one event per day, emoji-free titles, short links, and calendars you own all hid
bugs. When you write a fixture, ask what real data looks like that it doesn't.

---

## Task 13.1: Sort by day first (Task 1.5)

**Bug:** `SortEvents` put every all-day event before every timed one across the whole list, so `agenda --tsv/--json`
listed Nov 17–20 birthdays before a Nov 16 10:00 meeting. The pretty view hid it, because it groups by day first.
**Fix:** sort by start *day* (in the given zone), then all-day before timed within that day, then start, then title.

**Files:** `internal/model/days.go`, `internal/model/days_test.go`

1. Test:
```diff
--- a/internal/model/days_test.go
+++ b/internal/model/days_test.go
@@ -61,3 +61,21 @@ func TestGroupByDayPutsMultiDayEventsOnEveryDay(t *testing.T) {
 		t.Errorf("days[0].Date = %v, want %v", days[0].Date, want)
 	}
 }
+
+// Scripts read TSV/JSON in list order, so the order must be by day first:
+// an all-day event on Nov 17 must not come before a timed event on Nov 16.
+func TestSortEventsIsByDayFirst(t *testing.T) {
+	ny := mustLoc(t, "America/New_York")
+	events := []model.Event{
+		{Title: "birthday", AllDay: true,
+			Start: time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC),
+			End:   time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)},
+		{Title: "meeting", Start: time.Date(2026, 11, 16, 10, 0, 0, 0, ny), End: time.Date(2026, 11, 16, 11, 0, 0, 0, ny)},
+	}
+
+	model.SortEvents(events, ny)
+
+	if events[0].Title != "meeting" || events[1].Title != "birthday" {
+		t.Errorf("order = %s, %s; want meeting (Nov 16) then birthday (Nov 17)", events[0].Title, events[1].Title)
+	}
+}
```
2. Run: fails.
3. Fix:
```diff
--- a/internal/model/days.go
+++ b/internal/model/days.go
@@ -6,17 +6,22 @@ import (
 	"time"
 )
 
-// SortEvents orders events for display: all-day first, then by start, then title.
+// SortEvents orders events for display: by the day they start (in loc), then
+// all-day before timed within that day, then by start, then title. Day comes
+// first because scripts read TSV/JSON in this order.
 func SortEvents(events []Event, loc *time.Location) {
 	slices.SortStableFunc(events, func(a, b Event) int {
+		as, _ := a.Span(loc)
+		bs, _ := b.Span(loc)
+		if c := StartOfDay(as).Compare(StartOfDay(bs)); c != 0 {
+			return c
+		}
 		if a.AllDay != b.AllDay {
 			if a.AllDay {
 				return -1
 			}
 			return 1
 		}
-		as, _ := a.Span(loc)
-		bs, _ := b.Span(loc)
 		if c := as.Compare(bs); c != 0 {
 			return c
 		}
```
4. `go test ./...`: pass.

**Commit:** `fix(model): sort events by day first, then all-day before timed`

---

## Task 13.2: Each occurrence links to its own date (Task 3.2)

**Bug:** every occurrence of a series reused the series' web link, which opens the *first* occurrence: Nov 18 opened
Nov 4, and a yearly birthday opened 2024. **Fix:** `recur.Instance` rewrites the link. Google's link carries
`eid` = base64("<event id> <calendar>"); swap in the occurrence ID, keep the calendar part exactly, and re-encode with the
same base64 variant. Links in any other shape stay unchanged (never break a working link).

⚠ This relies on Google's link format. **Task 8.7 has a manual check** for it.

**Files:** `internal/recur/recur.go`, `internal/recur/recur_test.go`

1. Test:
```diff
--- a/internal/recur/recur_test.go
+++ b/internal/recur/recur_test.go
@@ -1,6 +1,7 @@
 package recur_test
 
 import (
+	"encoding/base64"
 	"errors"
 	"testing"
 	"time"
@@ -133,3 +134,19 @@ func TestLookup(t *testing.T) {
 		t.Errorf("Lookup(not a slot) err = %v, want ErrNoSuchInstance", err)
 	}
 }
+
+// Google's web link names one event via eid = base64("<event id> <calendar>").
+// An occurrence must link to itself, not to the series (which opens the first one).
+func TestInstanceLinksToItsOwnOccurrence(t *testing.T) {
+	ny := testutil.NY(t)
+	m := weeklyStandup(t, "RRULE:FREQ=WEEKLY;BYDAY=MO")
+	eid := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
+	m.Link = "https://www.google.com/calendar/event?eid=" + eid("std me@example.com")
+
+	inst := recur.Instance(m, time.Date(2026, 11, 9, 9, 0, 0, 0, ny))
+
+	want := "https://www.google.com/calendar/event?eid=" + eid("std_20261109T140000Z me@example.com")
+	if inst.Link != want {
+		t.Errorf("Link = %s, want %s", inst.Link, want)
+	}
+}
```
2. Run: fails.
3. Fix:
```diff
--- a/internal/recur/recur.go
+++ b/internal/recur/recur.go
@@ -3,8 +3,11 @@
 package recur
 
 import (
+	"encoding/base64"
 	"errors"
 	"fmt"
+	"net/url"
+	"strings"
 	"time"
 
 	"github.com/teambition/rrule-go"
@@ -47,9 +50,41 @@ func Instance(master model.Event, originalStart time.Time) model.Event {
 	inst.End = originalStart.Add(master.Duration())
 	inst.Recurrence = nil
 	inst.ETag = ""
+	inst.Link = instanceLink(master.Link, master.ID, inst.ID)
 	return inst
 }
 
+// eidEncodings are the base64 variants Google's eid parameter may use; we
+// re-encode with whichever one decoded it.
+var eidEncodings = []*base64.Encoding{base64.RawURLEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.StdEncoding}
+
+// instanceLink turns a series' web link into one occurrence's. Google's link
+// carries eid = base64("<event id> <calendar>"); we swap the event ID and keep
+// the calendar part exactly as Google wrote it. Links in any other shape are
+// returned unchanged.
+func instanceLink(link, seriesID, instanceID string) string {
+	u, err := url.Parse(link)
+	if err != nil {
+		return link
+	}
+	q := u.Query()
+	eid := q.Get("eid")
+	for _, enc := range eidEncodings {
+		raw, err := enc.DecodeString(eid)
+		if err != nil {
+			continue
+		}
+		rest, ok := strings.CutPrefix(string(raw), seriesID+" ")
+		if !ok {
+			return link
+		}
+		q.Set("eid", enc.EncodeToString([]byte(instanceID+" "+rest)))
+		u.RawQuery = q.Encode()
+		return u.String()
+	}
+	return link
+}
+
 // Expand returns the instances of master overlapping [from, to). A stored
 // exception replaces the instance at its OriginalStart; a cancelled exception
 // removes it. Exceptions whose slot is no longer in the series are ignored.
```
4. Run: pass.

**Commit:** `fix(recur): occurrences link to their own date, not the series`

---

## Task 13.3: No RSVP through a read-only calendar (Tasks 4.4, 12.8)

**Bug:** an invitation seen through someone else's read-only calendar offered [RSVP], and `r` (and `clindar rsvp`)
queued a reply Google rejects. **Fix:** one rule in two places. `service.RSVP` refuses read-only calendars
(`ErrReadOnly`) before queuing anything. The TUI's `permissions` requires a writable calendar for RSVP and gives a reason
(`whyNoRSVP`) that both the list's `r` and the detail pop-up show.

**Files:** `internal/service/delete.go`, `internal/service/delete_test.go`, `internal/tui/actions.go`, `internal/tui/detail_test.go`, `internal/tui/drag.go`

1. Tests:
```diff
--- a/internal/service/delete_test.go
+++ b/internal/service/delete_test.go
@@ -102,3 +102,22 @@ func TestRSVPOnOrphanInstanceNeedsNoScope(t *testing.T) {
 		t.Errorf("RSVP without scope: %v", err)
 	}
 }
+
+// An invitation seen through someone else's read-only calendar can't be answered
+// there: Google would reject the change. Refuse it before queuing anything.
+func TestRSVPOnReadOnlyCalendarIsRefused(t *testing.T) {
+	e := newEnv(t)
+	inv := testutil.Timed("inv", "Their meeting", e.at(20, 10, 0), time.Hour)
+	inv.CalendarID = "holidays" // read-only in newEnv
+	inv.Attendees = []model.Attendee{{Email: testutil.Account, Self: true, Response: model.ResponseNeedsAction}}
+	e.put(inv)
+
+	err := e.svc.RSVP(e.ctx, "inv", model.ResponseAccepted, service.ScopeNone)
+
+	if !errors.Is(err, service.ErrReadOnly) {
+		t.Errorf("err = %v, want ErrReadOnly", err)
+	}
+	if len(e.ops()) != 0 {
+		t.Errorf("a refused RSVP must not queue anything: %+v", e.ops())
+	}
+}
--- a/internal/tui/detail_test.go
+++ b/internal/tui/detail_test.go
@@ -57,3 +57,22 @@ func TestDetailOnReadOnlyCalendarOffersOnlyClose(t *testing.T) {
 		}
 	}
 }
+
+// Invited, but seen through a read-only calendar (someone else's): no [RSVP],
+// and r explains why instead of opening "Going?".
+func TestDetailOfInviteOnReadOnlyCalendarHidesRSVP(t *testing.T) {
+	f := newMouseFixture(t)
+	invite := testutil.Timed("inv", "Their meeting", time.Date(2026, 11, 20, 10, 0, 0, 0, f.ny), time.Hour)
+	invite.CalendarID = "holidays" // not in the fixture's calendars: treated as read-only
+	invite.Attendees = []model.Attendee{
+		{Email: testutil.Account, Self: true}, {Email: "host@example.com", Organizer: true}}
+	f.openDetail(invite)
+
+	if screen := f.m.View(); strings.Contains(screen, "[RSVP]") {
+		t.Errorf("read-only invite shows [RSVP]:\n%s", screen)
+	}
+	f.keys("r")
+	if m := f.model(); m.mode != modeDetail || !strings.Contains(m.message, "read-only") {
+		t.Errorf("r: mode = %v, message = %q; want the pop-up kept open and the reason shown", m.mode, m.message)
+	}
+}
```
2. Run: fail.
3. Fix:
```diff
--- a/internal/service/delete.go
+++ b/internal/service/delete.go
@@ -68,6 +68,11 @@ func (s *Service) RSVP(ctx context.Context, id string, resp model.ResponseStatus
 	if err != nil {
 		return err
 	}
+	// Seen through someone else's read-only calendar, Google rejects the reply:
+	// answer from your own calendar's copy instead.
+	if _, err := s.writableCalendar(ctx, e.CalendarID); err != nil {
+		return err
+	}
 	if e.IsInstance() {
 		switch scope {
 		case ScopeThis:
--- a/internal/tui/actions.go
+++ b/internal/tui/actions.go
@@ -110,8 +110,8 @@ func (m Model) applyChanges(e model.Event, ch service.Changes) (tea.Model, tea.C
 
 // startRSVP asks how to answer an invitation to e.
 func (m Model) startRSVP(e model.Event) Model {
-	if e.SelfAttendee() == nil {
-		m.mode, m.message = modeNormal, "you are not invited to this event"
+	if can := permissions(e, m.calendars[e.CalendarID]); !can.rsvp {
+		m.mode, m.message = modeNormal, can.whyNoRSVP
 		return m
 	}
 	m.target, m.mode = e, modeRSVP
@@ -127,7 +127,7 @@ func (m Model) detailKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
 	case key.Matches(k, keys.Delete) && !can.delete:
 		m.message = "can't delete: the calendar is read-only"
 	case key.Matches(k, keys.RSVP) && !can.rsvp:
-		m.message = "you are not invited to this event"
+		m.message = can.whyNoRSVP
 	case key.Matches(k, keys.Edit):
 		m.form, m.mode = editEventForm(m.detail, m.loc()), modeForm
 	case key.Matches(k, keys.Delete):
--- a/internal/tui/drag.go
+++ b/internal/tui/drag.go
@@ -72,13 +72,23 @@ func canDrag(e model.Event, cal model.Calendar) (bool, string) {
 
 // allowed says which detail actions can succeed for e, and why editing can't.
 type allowed struct {
-	edit, delete, rsvp bool
-	whyNoEdit          string
+	edit, delete, rsvp   bool
+	whyNoEdit, whyNoRSVP string
 }
 
 func permissions(e model.Event, cal model.Calendar) allowed {
 	edit, why := canDrag(e, cal)
-	return allowed{edit: edit, delete: cal.Writable(), rsvp: e.SelfAttendee() != nil, whyNoEdit: why}
+	a := allowed{edit: edit, delete: cal.Writable(), whyNoEdit: why}
+	switch {
+	case e.SelfAttendee() == nil:
+		a.whyNoRSVP = "you are not invited to this event"
+	case !cal.Writable():
+		// Someone else's calendar showing your invitation: Google rejects replies there.
+		a.whyNoRSVP = "can't reply here: " + cal.Name + " is read-only (answer from your own calendar)"
+	default:
+		a.rsvp = true
+	}
+	return a
 }
 
 // dragPreview is the status-bar text while dragging, e.g. `Move "Trip" → Sat 21–Sun 22`.
```
4. Run: pass.

**Commit:** `fix: refuse RSVP on read-only calendars (service and TUI)`

---

## Task 13.4: Time out stalled requests (Task 5.2)

**Bug:** one `clindar sync` took 49.7s (others about 2s). Manual sync and the daemon had no deadline, so a stalled
connection hung them. **Fix:** `gcal.NewClient` gives the HTTP client a **per-request** limit, `gcal.RequestTimeout`
(30s), unless the caller already set one. It **copies** the client rather than changing the caller's. A timeout is an
ordinary transient error: edits stay queued and the daemon backs off. There is no cap on a whole sync, because a first sync of
many calendars can legitimately take long.

Side note from the tester: "warning: sync failed … context deadline exceeded" after an edit that did reach Google is the
CLI's 5s `sync_timeout` expiring during the *pull*, after the push succeeded. That's expected; better wording is an idea
for later.

**Files:** `internal/gcal/client.go`, `internal/gcal/client_test.go`

1. Test (before the fix it took the stalling server's full 5s):
```diff
--- a/internal/gcal/client_test.go
+++ b/internal/gcal/client_test.go
@@ -7,6 +7,7 @@ import (
 	"net/http"
 	"net/http/httptest"
 	"testing"
+	"time"
 
 	"google.golang.org/api/option"
 
@@ -107,3 +108,30 @@ func TestPatchSendsIfMatchAndMapsErrors(t *testing.T) {
 		})
 	}
 }
+
+// A stalled connection must not hang a sync (manual sync and the daemon have no
+// overall deadline): each request gives up after RequestTimeout, as a transient
+// error, so queued edits stay queued and are retried.
+func TestStalledRequestTimesOut(t *testing.T) {
+	old := gcal.RequestTimeout
+	gcal.RequestTimeout = 100 * time.Millisecond
+	t.Cleanup(func() { gcal.RequestTimeout = old })
+	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
+		select { // stall until the client gives up (or 5s, so a failing test can't hang)
+		case <-r.Context().Done():
+		case <-time.After(5 * time.Second):
+		}
+	})
+	start := time.Now()
+
+	_, _, err := c.ListEvents(context.Background(), "cal", "")
+
+	if err == nil || time.Since(start) > 2*time.Second {
+		t.Fatalf("ListEvents = %v after %v; want an error within about %v", err, time.Since(start), gcal.RequestTimeout)
+	}
+	for _, sentinel := range []error{gcal.ErrConflict, gcal.ErrNotFound, gcal.ErrPermanent, gcal.ErrUnauthorized} {
+		if errors.Is(err, sentinel) {
+			t.Errorf("err = %v is %v; a timeout must be transient", err, sentinel)
+		}
+	}
+}
```
2. Run: fails.
3. Fix:
```diff
--- a/internal/gcal/client.go
+++ b/internal/gcal/client.go
@@ -3,6 +3,7 @@ package gcal
 import (
 	"context"
 	"net/http"
+	"time"
 
 	"google.golang.org/api/calendar/v3"
 	"google.golang.org/api/option"
@@ -16,9 +17,20 @@ type Client struct {
 	account string
 }
 
+// RequestTimeout bounds every request to Google. Without it a stalled
+// connection hangs a sync for as long as it stalls (manual sync and the daemon
+// have no overall deadline). A timeout is a transient error: queued edits stay
+// queued and are retried. Tests lower it.
+var RequestTimeout = 30 * time.Second
+
 // NewClient wraps an authorized HTTP client (see package auth). Extra options
 // let tests point it at an httptest server with option.WithEndpoint.
 func NewClient(ctx context.Context, accountID string, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
+	if hc.Timeout == 0 {
+		limited := *hc // a copy: don't change the caller's client
+		limited.Timeout = RequestTimeout
+		hc = &limited
+	}
 	svc, err := calendar.NewService(ctx, append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)...)
 	if err != nil {
 		return nil, err
```
4. Run: pass.

**Commit:** `fix(gcal): time out stalled requests instead of hanging sync`

---

## Task 13.5: Details show the whole span; no CLI hint in the TUI (Task 8.1)

**Bug:** "When" showed only the first day of a multi-day event ("Wed 18 Nov 2026 all day" for Nov 18–22).
"Repeats" said "yes (edit with --scope …)", a command-line hint that also appeared inside the TUI. **Fix:** show first
*and* last day (`Wed 18 Nov 2026 – Sun 22 Nov 2026  all day`, or `Mon 16 Nov 2026 22:00 – Tue 17 Nov 2026 02:00`), and
"Repeats: yes". The CLI's "choose a scope" error already explains the flag.

**Files:** `internal/output/output.go`, `internal/output/output_test.go`

1. Test:
```diff
--- a/internal/output/output_test.go
+++ b/internal/output/output_test.go
@@ -4,6 +4,7 @@ import (
 	"bytes"
 	"encoding/json"
 	"os"
+	"strings"
 	"testing"
 	"time"
 	_ "time/tzdata"
@@ -140,3 +141,26 @@ func TestNextIncludesMinutes(t *testing.T) {
 		t.Errorf("starts_in_minutes = %v", j.StartsInMinutes)
 	}
 }
+
+func TestPrettyDetailShowsWholeSpanAndPlainRepeats(t *testing.T) {
+	ny := testutil.NY(t)
+	trip := testutil.AllDay("t", "Trip", 2026, time.November, 18, 5) // Wed 18 – Sun 22
+	overnight := testutil.Timed("o", "Night shift", time.Date(2026, 11, 16, 22, 0, 0, 0, ny), 4*time.Hour)
+	weekly := sample(t)[0] // an occurrence of a series
+
+	tests := []struct {
+		e    model.Event
+		want string
+	}{
+		{trip, "When:      Wed 18 Nov 2026 – Sun 22 Nov 2026  all day\n"},
+		{overnight, "When:      Mon 16 Nov 2026 22:00 – Tue 17 Nov 2026 02:00\n"},
+		{weekly, "When:      Mon 16 Nov 2026  09:00–09:15\n"},
+		{weekly, "Repeats:   yes\n"}, // shared by the CLI and the TUI: no command-line hint
+	}
+	for _, tt := range tests {
+		got := render(t, output.Pretty, "", func(p *output.Printer) error { return p.Event(tt.e) })
+		if !strings.Contains(got, tt.want) {
+			t.Errorf("%s: want line %q in:\n%s", tt.e.Title, tt.want, got)
+		}
+	}
+}
```
2. Run: fails.
3. Fix:
```diff
--- a/internal/output/output.go
+++ b/internal/output/output.go
@@ -242,18 +242,17 @@ func (p *Printer) prettyAgenda(events []model.Event, from time.Time, days int) e
 }
 
 func (p *Printer) prettyDetail(e model.Event) error {
-	start, _ := e.Span(p.loc)
 	row := func(label, value string) {
 		if value != "" {
 			fmt.Fprintf(p.w, "%-10s %s\n", label+":", value)
 		}
 	}
 	fmt.Fprintln(p.w, dayStyle.Render(e.Title))
-	row("When", start.Format("Mon 02 Jan 2006")+"  "+strings.TrimSpace(p.timeRange(e)))
+	row("When", p.when(e))
 	row("Calendar", p.calendars[e.CalendarID].Name)
 	row("Where", e.Location)
 	if e.IsInstance() || e.IsMaster() {
-		row("Repeats", "yes (edit with --scope this|following|all)")
+		row("Repeats", "yes") // shared by the CLI and the TUI, so no command-line hint here
 	}
 	var people []string
 	for _, a := range e.Attendees {
@@ -267,3 +266,22 @@ func (p *Printer) prettyDetail(e model.Event) error {
 	}
 	return nil
 }
+
+// when describes an event's whole span: one date for a one-day event, first and
+// last date for a longer one (all-day ends are exclusive, so the last day is
+// the day before End).
+func (p *Printer) when(e model.Event) string {
+	const day = "Mon 02 Jan 2006"
+	start, end := e.Span(p.loc)
+	if e.AllDay {
+		last := end.AddDate(0, 0, -1)
+		if !last.After(start) {
+			return start.Format(day) + "  all day"
+		}
+		return start.Format(day) + " – " + last.Format(day) + "  all day"
+	}
+	if end.After(start) && !model.StartOfDay(end.Add(-time.Nanosecond)).Equal(model.StartOfDay(start)) {
+		return start.Format(day+" 15:04") + " – " + end.Format(day+" 15:04")
+	}
+	return start.Format(day) + "  " + p.timeRange(e)
+}
```
4. Run: pass.

**Commit:** `fix(output): show a multi-day event's whole span; no CLI hint in Repeats`

---

## Task 13.6: `show` refreshes a stale cache and takes `--offline` (Task 8.4)

**Bug:** `show` rejected `--offline`, unlike `agenda` and `next`, and never refreshed a stale cache, although design §3
says every CLI read does. **Fix:** `RefreshIfStale` plus the `--offline` flag, like the other read commands.

**Files:** `internal/cli/read.go`, `internal/cli/read_test.go`

1. Test:
```diff
--- a/internal/cli/read_test.go
+++ b/internal/cli/read_test.go
@@ -50,3 +50,18 @@ func TestNextWithNothingExits1Silently(t *testing.T) {
 		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
 	}
 }
+
+// show is a read like agenda: it refreshes a stale cache first (design §3),
+// and --offline skips that.
+func TestShowRefreshesWhenStaleUnlessOffline(t *testing.T) {
+	h := newHarness(t)
+	h.google.Put(testutil.Timed("e1", "Dentist", time.Date(2026, 11, 13, 15, 0, 0, 0, testutil.NY(t)), time.Hour))
+	h.now = h.now.Add(3 * time.Minute) // past stale_after (2m) since the harness's first sync
+
+	if _, _, code := h.run("show", "e1", "--offline"); code != 1 {
+		t.Errorf("show --offline: exit %d, want 1 (not synced yet, so not found)", code)
+	}
+	if out := h.mustRun("show", "e1", "--format", "{{.Title}}"); out != "Dentist\n" {
+		t.Errorf("show = %q, want Dentist (fetched by the refresh)", out)
+	}
+}
```
2. Run: fails.
3. Fix:
```diff
--- a/internal/cli/read.go
+++ b/internal/cli/read.go
@@ -121,12 +121,14 @@ func newNextCmd(app *App) *cobra.Command {
 
 func newShowCmd(app *App) *cobra.Command {
 	var of outputFlags
+	var offline bool
 	cmd := &cobra.Command{
 		Use:   "show ID",
 		Short: "Show one event in detail",
 		Args:  exactArgs(1),
 		RunE: func(cmd *cobra.Command, args []string) error {
 			ctx := cmd.Context()
+			app.RefreshIfStale(ctx, offline)
 			svc, err := app.Service(ctx)
 			if err != nil {
 				return err
@@ -143,6 +145,7 @@ func newShowCmd(app *App) *cobra.Command {
 		},
 	}
 	addOutputFlags(cmd, &of)
+	addOfflineFlag(cmd, &offline)
 	return cmd
 }
 
```
4. Run: pass.

**Commit:** `fix(cli): show refreshes a stale cache and accepts --offline`

---

## Task 13.7: Delete confirmation names its keys; says when cancelled (Task 12.5)

**Bug:** with the mouse on, "Delete this event?" showed [Delete] [Cancel] but not the keys, and Enter cancelled
silently. **Fix:** "Delete this event? (y/n)" with the mouse on as well. Any key other than `y` cancels and says "not
deleted". Enter still cancels: for a delete, cancelling is the safe default.

**Files:** `internal/tui/layout.go`, `internal/tui/mouse_test.go`, `internal/tui/nav.go`

1. Test:
```diff
--- a/internal/tui/mouse_test.go
+++ b/internal/tui/mouse_test.go
@@ -232,3 +232,22 @@ func TestHoverOnEdgeRevealsHandle(t *testing.T) {
 		t.Error("moving off the edge should hide ▸")
 	}
 }
+
+// With the mouse on, the delete confirmation still names its keys, and a key
+// other than y cancels visibly rather than silently.
+func TestDeleteConfirmShowsKeysAndSaysWhenCancelled(t *testing.T) {
+	f := newMouseFixture(t)
+	f.keys("tab", "j", "d") // list from Thu 12: Standup, then Lunch (a one-off)
+
+	if screen := f.m.View(); !strings.Contains(screen, "(y/n)") || !strings.Contains(screen, "[Delete]") {
+		t.Fatalf("confirm pop-up should show (y/n) and the buttons:\n%s", screen)
+	}
+	f.keys("enter")
+
+	if m := f.model(); m.mode != modeNormal || !strings.Contains(m.message, "not deleted") {
+		t.Errorf("mode = %v, message = %q; want back to normal with \"not deleted\"", m.mode, m.message)
+	}
+	if got := f.titlesOn(12); !slices.Contains(got, "Lunch with Sam") {
+		t.Errorf("Nov 12 = %v: lunch must still be there", got)
+	}
+}
```
2. Run: fails.
3. Fix:
```diff
--- a/internal/tui/layout.go
+++ b/internal/tui/layout.go
@@ -110,8 +110,13 @@ func (m Model) popup() (modal, bool) {
 		md.text("")
 		choices(strings.Join(parts, "   ")+"\n\nesc cancel", append(bs, button{"Cancel", "esc"})...)
 	case modeConfirm:
-		md.text("Delete this event?")
-		choices("(y/n)", button{"Delete", "y"}, button{"Cancel", "n"})
+		if m.deps.Mouse { // the buttons don't name their keys, so the question does
+			md.text("Delete this event? (y/n)")
+			md.buttons(button{"Delete", "y"}, button{"Cancel", "n"})
+		} else {
+			md.text("Delete this event?")
+			md.text("(y/n)")
+		}
 	case modeRSVP:
 		md.text("Going?")
 		choices("(y)es  (n)o  (m)aybe", button{"Yes", "y"}, button{"No", "n"}, button{"Maybe", "m"}, button{"Cancel", "esc"})
--- a/internal/tui/nav.go
+++ b/internal/tui/nav.go
@@ -24,6 +24,7 @@ func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
 		if k.String() == "y" {
 			return m, m.afterScope(service.ScopeNone)
 		}
+		m.message = "not deleted" // any other key cancels: say so
 		return m, nil
 	case modeRSVP:
 		return m.rsvpKey(k)
```
4. Run: pass.

**Commit:** `fix(tui): delete confirm names its keys with the mouse on; say when cancelled`

---

## Task 13.8: Wrap the details box to the terminal (Task 12.5)

**Bug:** a long Link, Guests or description line made the details box wider than the terminal and cut off its right
border. **Fix:** wrap (not truncate, so no link or guest is lost) the detail text to the screen width minus the
box's border and padding, using lipgloss's ANSI-aware width and wrap, since the title is already bold.

**Files:** `internal/tui/detail_test.go`, `internal/tui/layout.go`

1. Test:
```diff
--- a/internal/tui/detail_test.go
+++ b/internal/tui/detail_test.go
@@ -5,6 +5,8 @@ import (
 	"testing"
 	"time"
 
+	"github.com/charmbracelet/lipgloss"
+
 	"github.com/jabiOP/clindar/internal/model"
 	"github.com/jabiOP/clindar/internal/testutil"
 )
@@ -76,3 +78,24 @@ func TestDetailOfInviteOnReadOnlyCalendarHidesRSVP(t *testing.T) {
 		t.Errorf("r: mode = %v, message = %q; want the pop-up kept open and the reason shown", m.mode, m.message)
 	}
 }
+
+// A long link, guest list or description must wrap inside the box, not push its
+// right border off the screen.
+func TestDetailFitsTheTerminal(t *testing.T) {
+	f := newFixture(t) // 84 columns
+	e := testutil.Timed("long", "Planning", time.Date(2026, 11, 13, 10, 0, 0, 0, f.ny), time.Hour)
+	e.Link = "https://www.google.com/calendar/event?eid=" + strings.Repeat("x", 120)
+	e.Description = strings.Repeat("word ", 40)
+	f.openDetail(e)
+
+	screen := f.m.View()
+
+	for i, line := range strings.Split(screen, "\n") {
+		if w := lipgloss.Width(line); w > 84 {
+			t.Errorf("line %d is %d columns wide (terminal: 84): %q", i, w, line)
+		}
+	}
+	if strings.Count(screen, "╮") != 1 || strings.Count(screen, "╯") != 1 {
+		t.Errorf("box borders missing:\n%s", screen)
+	}
+}
```
2. Run: fails.
3. Fix:
```diff
--- a/internal/tui/layout.go
+++ b/internal/tui/layout.go
@@ -81,7 +81,14 @@ func (m Model) popup() (modal, bool) {
 		}
 		p, _ := output.NewPrinter(&b, output.Pretty, "", m.loc(), cals)
 		p.Event(m.detail)
-		md.text(strings.TrimRight(b.String(), "\n"))
+		text := strings.TrimRight(b.String(), "\n")
+		// Wrap long lines (link, guests, description) to fit the screen; the box
+		// adds a border and 1 cell of padding on each side. lipgloss measures and
+		// wraps ANSI-aware, so the bold title is safe.
+		if maxW := max(m.width-4, 20); lipgloss.Width(text) > maxW {
+			text = lipgloss.NewStyle().Width(maxW).Render(text)
+		}
+		md.text(text)
 		md.text("")
 		can := permissions(m.detail, m.calendars[m.detail.CalendarID])
 		var bs []button
```
4. Run: pass.

**Commit:** `fix(tui): wrap the details box to fit the terminal`

---

## Task 13.9: One width measure for the whole TUI (Tasks 10.1, 10.2, 12.2, 12.3)

**Bug:** in the grid, day numbers after "Dec 1" sat one column right of their events. Emoji with a variation selector
(❤️ ☀️ ✈️) are 1 cell in `go-runewidth` but 2 in lipgloss and in terminals. `fit` padded by runewidth while
`lipgloss.JoinHorizontal` joined by lipgloss's measure, so a column came out one cell too wide and shifted the row.
**Fix:** one helper, `width()` = `lipgloss.Width`, for every measurement in the TUI, with `fit` rebuilt on top of it.
This **replaces** the Phase 12 rule "widths come from plain text (`runewidth`)". The idea is the same, with one
measure: the one lipgloss pads with. `go-runewidth` becomes an indirect dependency only. Golden files are unchanged.

**The lesson:** two measures of the same thing will eventually disagree. When a library lays text out for you, measure
with *its* ruler.

**Files:** `go.mod`, `internal/tui/grid.go`, `internal/tui/grid_test.go`, `internal/tui/hits_test.go`, `internal/tui/layout.go`, `internal/tui/list.go`, `internal/tui/modal.go`, `internal/tui/styles.go`

1. Test (a "❤️" title made a 113-column line in a 112-column grid):
```diff
--- a/internal/tui/grid_test.go
+++ b/internal/tui/grid_test.go
@@ -5,6 +5,8 @@ import (
 	"testing"
 	"time"
 
+	"github.com/charmbracelet/lipgloss"
+
 	"github.com/jabiOP/clindar/internal/model"
 	"github.com/jabiOP/clindar/internal/testutil"
 )
@@ -97,3 +99,23 @@ func TestGridMarksDropTarget(t *testing.T) {
 	screen, _ := gridWithTrip(t, edgeRef{}, time.Date(2026, 11, 14, 0, 0, 0, 0, ny))
 	find(t, screen, "»14") // fails the test if the marker is missing
 }
+
+// Emoji with a variation selector (❤️ ☀️ ✈️) are 2 columns in lipgloss and in
+// terminals but 1 in go-runewidth. If fit pads by a different measure than
+// lipgloss joins columns with, that day's column grows and shifts the row.
+func TestGridColumnsStayAlignedWithEmoji(t *testing.T) {
+	ny := testutil.NY(t)
+	start := time.Date(2026, 11, 30, 0, 0, 0, 0, ny)
+	events := []model.Event{testutil.Timed("e", "❤️ Ana's birthday", time.Date(2026, 12, 1, 9, 0, 0, 0, ny), time.Hour)}
+
+	got, _ := renderGrid(gridView{
+		start: start, today: start, selected: start,
+		days: model.GroupByDay(events, start, gridDays), width: 112, height: 18, loc: ny,
+	})
+
+	for i, line := range strings.Split(got, "\n")[2:] { // skip the month and weekday headers
+		if w := lipgloss.Width(line); w != 112 {
+			t.Errorf("grid line %d is %d columns, want 112: %q", i+2, w, line)
+		}
+	}
+}
--- a/internal/tui/hits_test.go
+++ b/internal/tui/hits_test.go
@@ -3,8 +3,6 @@ package tui
 import (
 	"strings"
 	"testing"
-
-	"github.com/mattn/go-runewidth"
 )
 
 func TestHitMapTopmostWins(t *testing.T) {
@@ -28,7 +26,7 @@ func find(t *testing.T, screen, s string) (x, y int) {
 	t.Helper()
 	for y, line := range strings.Split(screen, "\n") {
 		if i := strings.Index(line, s); i >= 0 {
-			return runewidth.StringWidth(line[:i]), y
+			return width(line[:i]), y
 		}
 	}
 	t.Fatalf("%q not on screen:\n%s", s, screen)
```
2. Run: fails.
3. Fix:
```diff
--- a/go.mod
+++ b/go.mod
@@ -8,7 +8,6 @@ require (
 	github.com/charmbracelet/bubbletea v1.3.10
 	github.com/charmbracelet/lipgloss v1.1.0
 	github.com/google/go-cmp v0.7.0
-	github.com/mattn/go-runewidth v0.0.31
 	github.com/muesli/termenv v0.16.0
 	github.com/spf13/cobra v1.10.2
 	github.com/teambition/rrule-go v1.8.2
@@ -48,6 +47,7 @@ require (
 	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
 	github.com/mattn/go-isatty v0.0.24 // indirect
 	github.com/mattn/go-localereader v0.0.1 // indirect
+	github.com/mattn/go-runewidth v0.0.31 // indirect
 	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
 	github.com/muesli/cancelreader v0.2.2 // indirect
 	github.com/ncruces/go-strftime v1.1.0 // indirect
--- a/internal/tui/grid.go
+++ b/internal/tui/grid.go
@@ -6,7 +6,6 @@ import (
 	"time"
 
 	"github.com/charmbracelet/lipgloss"
-	"github.com/mattn/go-runewidth"
 
 	"github.com/jabiOP/clindar/internal/model"
 )
@@ -126,7 +125,7 @@ func (g gridView) eventLine(e model.Event, day time.Time, w int) (string, hitMap
 			}
 		}
 	}
-	text := start + fit(eventLabel(e, g.loc), w-runewidth.StringWidth(start+end)) + end
+	text := start + fit(eventLabel(e, g.loc), w-width(start+end)) + end
 	return calendarStyle(g.calendars[e.CalendarID].Color).Render(text), hits
 }
 
--- a/internal/tui/layout.go
+++ b/internal/tui/layout.go
@@ -5,7 +5,6 @@ import (
 	"strings"
 
 	"github.com/charmbracelet/lipgloss"
-	"github.com/mattn/go-runewidth"
 
 	"github.com/jabiOP/clindar/internal/model"
 	"github.com/jabiOP/clindar/internal/output"
@@ -164,8 +163,8 @@ func (m Model) statusBar() (string, hitMap) {
 	bs := []button{{"+ Add", "a"}, {"⟳ Sync", "s"}, {"? Help", "?"}}
 	for i := len(bs) - 1; i >= 0; i-- { // lay out right to left
 		label := "[" + bs[i].label + "]"
-		x -= runewidth.StringWidth(label)
-		hits.add(x, 0, runewidth.StringWidth(label), 1, target{kind: targetKey, key: bs[i].key})
+		x -= width(label)
+		hits.add(x, 0, width(label), 1, target{kind: targetKey, key: bs[i].key})
 		labels = append([]string{label}, labels...)
 		x--
 	}
--- a/internal/tui/list.go
+++ b/internal/tui/list.go
@@ -4,8 +4,6 @@ import (
 	"strings"
 	"time"
 
-	"github.com/mattn/go-runewidth"
-
 	"github.com/jabiOP/clindar/internal/model"
 )
 
@@ -89,7 +87,7 @@ func (v listView) line(e model.Event, selected bool) string {
 	}
 	label := "● " + cal.Name
 	text := fit(marker+when+"  "+strings.Join(strings.Fields(e.Title), " ")+resp,
-		max(v.width-runewidth.StringWidth(cal.Name)-4, 10))
+		max(v.width-width(cal.Name)-4, 10))
 	if selected {
 		return selectedStyle.Render(text + "  " + label)
 	}
--- a/internal/tui/modal.go
+++ b/internal/tui/modal.go
@@ -4,7 +4,6 @@ import (
 	"strings"
 
 	"github.com/charmbracelet/lipgloss"
-	"github.com/mattn/go-runewidth"
 )
 
 // modal builds a pop-up line by line and remembers where its buttons and
@@ -25,7 +24,7 @@ func (m *modal) buttons(bs ...button) {
 	x, y := 0, len(m.lines)
 	for _, b := range bs {
 		label := "[" + b.label + "]"
-		w := runewidth.StringWidth(label)
+		w := width(label)
 		m.hits.add(x, y, w, 1, target{kind: targetKey, key: b.key})
 		parts = append(parts, label)
 		x += w + 2
@@ -35,7 +34,7 @@ func (m *modal) buttons(bs ...button) {
 
 // field adds a clickable line (a form field).
 func (m *modal) field(line string, index int) {
-	m.hits.add(0, len(m.lines), runewidth.StringWidth(line), 1, target{kind: targetField, field: index})
+	m.hits.add(0, len(m.lines), width(line), 1, target{kind: targetField, field: index})
 	m.lines = append(m.lines, line)
 }
 
--- a/internal/tui/styles.go
+++ b/internal/tui/styles.go
@@ -1,8 +1,9 @@
 package tui
 
 import (
+	"strings"
+
 	"github.com/charmbracelet/lipgloss"
-	"github.com/mattn/go-runewidth"
 )
 
 var (
@@ -14,13 +15,32 @@ var (
 	modalStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
 )
 
+// width is the number of terminal cells s takes. Every width in this package
+// comes from here, and it is the measure lipgloss itself uses to join and pad
+// blocks: if we padded by a different measure (go-runewidth counts "❤️" as 1
+// cell, lipgloss and terminals as 2), a grid column would come out one cell
+// wider than planned and shift the whole row. Display width, never len():
+// "–" is 3 bytes, CJK runes are 2 cells.
+func width(s string) int { return lipgloss.Width(s) }
+
 // fit truncates s to w terminal cells (with "…") and pads it to exactly w.
-// Uses display width, not len(): "–" is 3 bytes, CJK runes are 2 cells.
 func fit(s string, w int) string {
 	if w <= 0 {
 		return ""
 	}
-	return runewidth.FillRight(runewidth.Truncate(s, w, "…"), w)
+	if sw := width(s); sw <= w {
+		return s + strings.Repeat(" ", w-sw)
+	}
+	out := ""
+	for _, r := range s { // keep as much as fits in w-1 cells, then "…"
+		next := out + string(r)
+		if width(next) > w-1 {
+			break
+		}
+		out = next
+	}
+	out += "…"
+	return out + strings.Repeat(" ", max(w-width(out), 0))
 }
 
 func calendarStyle(color string) lipgloss.Style {
```
4. `go mod tidy && go test -race ./...`: pass, and no golden file changes.

**Commit:** `fix(tui): measure text with lipgloss's width so emoji don't shift grid columns`

---

## Task 13.10: Occurrence links: handle Google's real link shape (Task 13.2)

**Bug (found by the real-Google check in Task 8.7):** Task 13.2's fix never fired on real data. A series' link doesn't
name the bare series: its `eid` decodes to the **first occurrence's** ID, `"<series id>_<date> <calendar>"` (e.g.
`j87pvipe…_20241117 8j2n…@g`). `instanceLink` only accepted `"<series id> <calendar>"`, so it returned every real link
unchanged. **Fix:** accept the series ID with or without a `_<date>` suffix, and replace the whole ID.

**Why the first test passed:** its made-up link had no date. A fixture invented from the docs, rather than copied
from real data, passed while the real data failed. The new test is built from the tester's real link shape, and the old
one still passes.

**Files:** `internal/recur/recur.go`, `internal/recur/recur_test.go`

1. Test:
```diff
--- a/internal/recur/recur_test.go
+++ b/internal/recur/recur_test.go
@@ -150,3 +150,19 @@ func TestInstanceLinksToItsOwnOccurrence(t *testing.T) {
 		t.Errorf("Link = %s, want %s", inst.Link, want)
 	}
 }
+
+// Real series links name their FIRST occurrence ("<id>_<date> <calendar>"), not
+// the bare series ID (tester's finding: the yearly birthday opened 2024's).
+func TestInstanceLinkReplacesADatedSeriesLink(t *testing.T) {
+	m := testutil.AllDay("j87pvipe", "pavlo bday", 2024, time.November, 17, 1)
+	m.Recurrence = []string{"RRULE:FREQ=YEARLY"}
+	eid := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
+	m.Link = "https://www.google.com/calendar/event?eid=" + eid("j87pvipe_20241117 8j2n@g")
+
+	inst := recur.Instance(m, time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC))
+
+	want := "https://www.google.com/calendar/event?eid=" + eid("j87pvipe_20261117 8j2n@g")
+	if inst.Link != want {
+		t.Errorf("Link = %s, want %s", inst.Link, want)
+	}
+}
```
2. Run: fails (the dated link comes back unchanged).
3. Fix:
```diff
--- a/internal/recur/recur.go
+++ b/internal/recur/recur.go
@@ -59,8 +59,9 @@ func Instance(master model.Event, originalStart time.Time) model.Event {
 var eidEncodings = []*base64.Encoding{base64.RawURLEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.StdEncoding}
 
 // instanceLink turns a series' web link into one occurrence's. Google's link
-// carries eid = base64("<event id> <calendar>"); we swap the event ID and keep
-// the calendar part exactly as Google wrote it. Links in any other shape are
+// carries eid = base64("<event id> <calendar>"), where a series' event id is
+// usually its first occurrence's ("<series id>_<date>"); we swap the event ID
+// and keep the calendar part exactly as Google wrote it. Links in any other shape are
 // returned unchanged.
 func instanceLink(link, seriesID, instanceID string) string {
 	u, err := url.Parse(link)
@@ -74,8 +75,10 @@ func instanceLink(link, seriesID, instanceID string) string {
 		if err != nil {
 			continue
 		}
-		rest, ok := strings.CutPrefix(string(raw), seriesID+" ")
-		if !ok {
+		// The ID part is the series ID, or (as Google writes it for a series)
+		// its first occurrence's ID, "<series id>_<date>". Replace it whole.
+		id, rest, ok := strings.Cut(string(raw), " ")
+		if !ok || (id != seriesID && !strings.HasPrefix(id, seriesID+"_")) {
 			return link
 		}
 		q.Set("eid", enc.EncodeToString([]byte(instanceID+" "+rest)))
```
4. Run: pass.
5. **Real check (Task 8.7):** `clindar show <occurrence id> --json` for a non-first occurrence, then open its `link` in a
   browser: Google must show that date.

**Commit:** `fix(recur): rewrite occurrence links whose eid already names a dated occurrence`

---

# Product decisions after testing

> ⚠ **Tasks 13.11–13.12 are written as a plan only; they were not compiled or run.** Treat them as precise specs.
> If the compiler disagrees, follow the intent and note the fix in `DEVIATIONS.md`.

## Task 13.11: Left-align day numbers in the grid (Task 10.1)

**Decision:** day numbers start exactly where their events start. Until now single-digit days were right-aligned to two
characters (`" 2"`), so their digit sat one column right of the events below. "Dec 1" and two-digit days had no
leading space, which is why the tester saw "an offset that starts after Dec 1". It was never a width bug; every grid
line measured exactly right.

```
before                                   after
30        Dec 1      2         3         30        Dec 1     2         3
09:00 Gym           09:00 Dentist        09:00 Gym           09:00 Dentist
```

**Files:** modify `internal/tui/grid.go`, `internal/tui/grid_test.go`; regenerate the grid golden files

1. Test, in `grid_test.go`:
   ```go
   // Day numbers start where their events start (" 2" would put the digit one column right).
   func TestDayNumbersAreLeftAligned(t *testing.T) {
   	ny := testutil.NY(t)
   	start := time.Date(2026, 11, 30, 0, 0, 0, 0, ny) // Mon Nov 30
   	later := time.Date(2026, 12, 25, 0, 0, 0, 0, ny) // keeps the today/selected markers off the first row
   	got, _ := renderGrid(gridView{start: start, today: later, selected: later,
   		days: model.GroupByDay(nil, start, gridDays), width: 70, height: 18, loc: ny})

   	row := strings.Split(got, "\n")[2] // title, weekday names, then the first week's day numbers
   	if !strings.HasPrefix(row, "30        Dec 1     2         3 ") {
   		t.Errorf("first week's labels = %q", row)
   	}
   }
   ```
   (Width 70 gives 10 columns per day: 9 of content plus 1 of gutter.) Run: fails, with `" 2"` and `" 3"`.
2. In `gridView.cell` (`grid.go`), change
   ```go
   	label := fmt.Sprintf("%2d", day.Date.Day())
   ```
   to
   ```go
   	label := strconv.Itoa(day.Date.Day()) // left-aligned: the digit starts where the events start
   ```
   and add the `strconv` import (`fmt` is still used for "+N more").
3. `go test ./internal/tui -update`, then **review the diff of every grid golden file** (`grid`, `grid_cells`,
   `grid_hover`, `grid_mouse`): only day-number lines may change, each losing a leading space before a single digit.
   `git diff --stat internal/tui/testdata` should list only grid files. Then `go test -race ./internal/tui`.

**Commit:** `feat(tui): left-align day numbers in the grid`

---

## Task 13.12: Wrapped detail lines continue under their value (Task 13.8)

**Decision (tester's idea):** when a long Guests or Link value wraps in the details box, the continuation lines start
under the value, not at the box's left edge:

```
before                                    after
Guests:    alice@example.com (accepted),  Guests:    alice@example.com (accepted),
bob@example.com (needsAction)                        bob@example.com (needsAction)
```

**Design:** wrapping belongs where the rows are built, in `output.prettyDetail`, not in the TUI. Afterwards the TUI
can't tell a label from a value. So the `Printer` gets an optional width:
- **0 (default, the CLI):** no wrapping; the terminal wraps, as today.
- **>0 (the TUI):** each row's value wraps to `width − 11` and its continuation lines are indented 11 cells, the label
  column. The title and description wrap to the full width.

The TUI drops its own whole-text wrapping (Task 13.8's `lipgloss.NewStyle().Width(maxW)` block) and calls `SetWidth`.

**Files:** modify `internal/output/output.go`, `internal/output/output_test.go`, `internal/tui/layout.go`

1. Test, in `output_test.go`:
   ```go
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
   ```
   (Add the `strings` import if `output_test.go` doesn't have it. `lipgloss` is already imported for `TestMain`.)
   Run: compile failure (`SetWidth` doesn't exist).
2. In `output.go`, add a `width int` field to `Printer`, then:
   ```go
   // SetWidth makes pretty details wrap to w cells: each "Label: value" row wraps its
   // value under itself. 0 (the default, used by the CLI) means don't wrap.
   func (p *Printer) SetWidth(w int) { p.width = w }

   const labelColumn = 11 // "%-10s ": a 10-cell label plus one space

   // wrap splits s into lines of at most w cells (ANSI-aware). It returns s
   // unchanged when wrapping is off or s already fits.
   func (p *Printer) wrap(s string, w int) []string {
   	if p.width <= 0 || w <= 0 || lipgloss.Width(s) <= w {
   		return []string{s}
   	}
   	lines := strings.Split(lipgloss.NewStyle().Width(w).Render(s), "\n")
   	for i := range lines {
   		lines[i] = strings.TrimRight(lines[i], " ") // Render pads every line to w
   	}
   	return lines
   }
   ```
   and in `prettyDetail`:
   ```go
   	row := func(label, value string) {
   		if value == "" {
   			return
   		}
   		lines := p.wrap(value, p.width-labelColumn)
   		fmt.Fprintf(p.w, "%-10s %s\n", label+":", lines[0])
   		for _, l := range lines[1:] {
   			fmt.Fprintf(p.w, "%s%s\n", strings.Repeat(" ", labelColumn), l)
   		}
   	}
   	for _, l := range p.wrap(dayStyle.Render(e.Title), p.width) {
   		fmt.Fprintln(p.w, l)
   	}
   ```
   replacing the old `row` and the title's `Fprintln`. For the description:
   ```go
   	if e.Description != "" {
   		fmt.Fprintln(p.w)
   		for _, l := range p.wrap(e.Description, p.width) {
   			fmt.Fprintln(p.w, l)
   		}
   	}
   ```
3. In `layout.go` (`popup`, `modeDetail`), delete Task 13.8's wrapping block (`if maxW := …; lipgloss.Width(text) > maxW
   { … }`) and, right after `output.NewPrinter(…)`, add:
   ```go
   		p.SetWidth(max(m.width-4, 20)) // the box adds a border and 1 cell of padding per side
   ```
4. `go test -race ./...`: pass. That includes Task 13.8's `TestDetailFitsTheTerminal` (the box still fits) and the
   existing output tests (width 0 is unchanged). **Red-step check:** make `row` print continuation lines without the
   indent. The new test must fail. Then undo that.
5. Look at it: in the TUI at 80 columns, open an event with several guests and a long link. Each wrapped line should
   start under its value.

**Commit:** `feat(output): wrap detail values under themselves`
