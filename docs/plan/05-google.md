# Phase 5: Google adapter

`internal/gcal` is the **only** package that imports `google.golang.org/api/calendar/v3`. It converts
Google types to `model` types and maps HTTP errors to a few sentinel errors that the sync engine reacts to.
`internal/gcal/gcaltest` provides `Fake`, an in-memory Google that Phase 6 and Phase 8 tests use.

Read: README primer "Google Calendar API v3", especially key facts #4–#8.

```bash
go get google.golang.org/api@v0.314.0
```

---

## Task 5.1: Errors and conversion

> ⚠ **Fixed in Task 13.13:** `ToAPI` must also send the *other* kind of start/end fields as nulls (`NullFields`), or switching between all-day and timed reverts.

**Goal:** `FromAPI`/`ToAPI`/`CalendarFromAPI`, and error mapping.

Pay attention to:
- Deleted events in incremental sync arrive **almost empty** (no start/end). `FromAPI` must not fail on them.
- `ToAPI` sets `ForceSendFields` so that clearing a description actually clears it (key fact #7).
- 403 is usually permanent, but Google also uses 403 for rate and quota limits (`rateLimitExceeded`,
  `dailyLimitExceeded`, `quotaExceeded`). Those are transient and must be retried.
- **401 is not permanent.** It means our login is broken, and a re-login fixes it. Treating it as permanent would
  throw away every queued edit on one auth hiccup. It maps to `ErrUnauthorized`, which the engine retries.

**Files:** create `internal/gcal/errors.go`, `internal/gcal/convert_test.go`, `internal/gcal/convert.go`

**Steps**
1. `errors.go` (exercised by Task 5.2's tests):
```go
// Package gcal adapts Google Calendar API v3 to clindar's model. Google types
// never leave this package.
package gcal

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/api/googleapi"
)

// Errors the sync engine reacts to. Anything else is treated as transient
// (network trouble, 5xx, rate limits) and retried later.
var (
	ErrNotFound         = errors.New("not found on Google")      // 404, or 410 for an event
	ErrAlreadyExists    = errors.New("already exists on Google") // 409 on insert
	ErrConflict         = errors.New("changed on Google")        // 412: If-Match failed
	ErrSyncTokenExpired = errors.New("sync token expired")       // 410 on a list call
	ErrPermanent        = errors.New("rejected by Google")       // other 4xx: retrying won't help
	// ErrUnauthorized (401) is NOT permanent: the token may refresh, or the user
	// may log in again. Ops stay queued, and the message says what to do.
	ErrUnauthorized = errors.New("Google rejected our login (run `clindar auth login` if this persists)")
)

func mapErr(err error) error {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return err
	}
	switch code := gerr.Code; {
	case code == http.StatusNotFound || code == http.StatusGone:
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case code == http.StatusConflict:
		return fmt.Errorf("%w: %v", ErrAlreadyExists, err)
	case code == http.StatusPreconditionFailed:
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case code == http.StatusUnauthorized:
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	case code == http.StatusTooManyRequests || isRateLimit(gerr):
		return err
	case code >= 400 && code < 500:
		return fmt.Errorf("%w: %v", ErrPermanent, err)
	}
	return err
}

// Google reports rate and quota limits as 403 with reasons like
// "rateLimitExceeded", "userRateLimitExceeded", "dailyLimitExceeded", "quotaExceeded".
func isRateLimit(gerr *googleapi.Error) bool {
	for _, item := range gerr.Errors {
		reason := strings.ToLower(item.Reason)
		if strings.Contains(reason, "limitexceeded") || strings.Contains(reason, "quotaexceeded") {
			return true
		}
	}
	return false
}

func isGone(err error) bool {
	var gerr *googleapi.Error
	return errors.As(err, &gerr) && gerr.Code == http.StatusGone
}
```
2. Test:
```go
package gcal_test

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/calendar/v3"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
)

func TestFromAPITimedEvent(t *testing.T) {
	ev := &calendar.Event{
		Id: "e1", Status: "confirmed", Summary: "Standup", Etag: `"1"`,
		Start:     &calendar.EventDateTime{DateTime: "2026-11-16T09:00:00-05:00", TimeZone: "America/New_York"},
		End:       &calendar.EventDateTime{DateTime: "2026-11-16T09:15:00-05:00", TimeZone: "America/New_York"},
		Organizer: &calendar.EventOrganizer{Email: "boss@example.com"},
		Attendees: []*calendar.EventAttendee{{Email: "me@example.com", Self: true, ResponseStatus: "accepted"}},
	}

	got, err := gcal.FromAPI("acct", "cal", ev)

	if err != nil {
		t.Fatal(err)
	}
	want := model.Event{
		AccountID: "acct", CalendarID: "cal", ID: "e1", Status: "confirmed", Title: "Standup", ETag: `"1"`,
		Start:    time.Date(2026, 11, 16, 14, 0, 0, 0, time.UTC),
		End:      time.Date(2026, 11, 16, 14, 15, 0, 0, time.UTC),
		TimeZone: "America/New_York", Organizer: "boss@example.com",
		Attendees: []model.Attendee{{Email: "me@example.com", Self: true, Response: model.ResponseAccepted}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mismatch (-want +got):\n%s", diff)
	}
}

func TestFromAPIAllDayAndCancelledInstance(t *testing.T) {
	allDay, err := gcal.FromAPI("a", "c", &calendar.Event{Id: "h", Summary: "Holiday",
		Start: &calendar.EventDateTime{Date: "2026-11-26"}, End: &calendar.EventDateTime{Date: "2026-11-27"}})
	if err != nil || !allDay.AllDay || !allDay.Start.Equal(time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)) ||
		allDay.Status != model.StatusConfirmed {
		t.Errorf("all-day = %+v, %v", allDay, err)
	}

	// Incremental sync sends deleted instances with almost no fields.
	gone, err := gcal.FromAPI("a", "c", &calendar.Event{Id: "s_20261116T140000Z", Status: "cancelled",
		RecurringEventId: "s", OriginalStartTime: &calendar.EventDateTime{DateTime: "2026-11-16T14:00:00Z"}})
	if err != nil || !gone.Cancelled() || gone.SeriesID != "s" || gone.OriginalStart.IsZero() {
		t.Errorf("cancelled instance = %+v, %v", gone, err)
	}
}

func TestToAPIForcesClearableFields(t *testing.T) {
	e := model.Event{ID: "e1", Title: "x", TimeZone: "America/New_York",
		Start: time.Date(2026, 11, 16, 14, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 16, 15, 0, 0, 0, time.UTC)}

	ev := gcal.ToAPI(e)
	body, err := ev.MarshalJSON()

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"description":""`) {
		t.Errorf("empty description must be sent so it can be cleared; body = %s", body)
	}
	if ev.Start.TimeZone != "America/New_York" || ev.Start.DateTime != "2026-11-16T14:00:00Z" {
		t.Errorf("start = %+v", ev.Start)
	}
}

func TestAttendeesToAPISendsOnlyAttendees(t *testing.T) {
	e := model.Event{ID: "e1", Title: "Their meeting", Description: "agenda",
		Attendees: []model.Attendee{{Email: "me@example.com", Self: true, Response: model.ResponseAccepted}}}

	body, err := gcal.AttendeesToAPI(e).MarshalJSON()

	if err != nil {
		t.Fatal(err)
	}
	if want := `{"attendees":[{"email":"me@example.com","responseStatus":"accepted"}]}`; string(body) != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestToAPIAllDayUsesDates(t *testing.T) {
	e := model.Event{ID: "e", Title: "x", AllDay: true,
		Start: time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)}

	ev := gcal.ToAPI(e)

	if ev.Start.Date != "2026-11-26" || ev.End.Date != "2026-11-27" || ev.Start.DateTime != "" {
		t.Errorf("start=%+v end=%+v", ev.Start, ev.End)
	}
}
```
3. Run: compile failure.
4. Implement:
```go
package gcal

import (
	"fmt"
	"time"

	"google.golang.org/api/calendar/v3"

	"github.com/jabiOP/clindar/internal/model"
)

// FromAPI converts a Google event. Deleted events in incremental sync often
// carry only id, status and (for instances) recurringEventId/originalStartTime.
func FromAPI(accountID, calendarID string, ev *calendar.Event) (model.Event, error) {
	e := model.Event{
		AccountID: accountID, CalendarID: calendarID, ID: ev.Id,
		SeriesID: ev.RecurringEventId, Status: ev.Status,
		Title: ev.Summary, Description: ev.Description, Location: ev.Location,
		Recurrence: ev.Recurrence, ETag: ev.Etag, Link: ev.HtmlLink,
	}
	if e.Status == "" {
		e.Status = model.StatusConfirmed
	}
	if ev.Organizer != nil {
		e.Organizer = ev.Organizer.Email
	}
	if ev.Updated != "" {
		e.Updated, _ = time.Parse(time.RFC3339, ev.Updated) // informational only
	}
	if ev.OriginalStartTime != nil {
		t, _, err := parseDateTime(ev.OriginalStartTime)
		if err != nil {
			return e, fmt.Errorf("event %s originalStartTime: %w", ev.Id, err)
		}
		e.OriginalStart = t
	}
	if ev.Start == nil || ev.End == nil {
		if e.Cancelled() {
			return e, nil
		}
		return e, fmt.Errorf("event %s has no start/end", ev.Id)
	}
	start, allDay, err := parseDateTime(ev.Start)
	if err != nil {
		return e, fmt.Errorf("event %s start: %w", ev.Id, err)
	}
	end, _, err := parseDateTime(ev.End)
	if err != nil {
		return e, fmt.Errorf("event %s end: %w", ev.Id, err)
	}
	e.Start, e.End, e.AllDay = start, end, allDay
	if !allDay {
		e.TimeZone = ev.Start.TimeZone
	}
	for _, a := range ev.Attendees {
		e.Attendees = append(e.Attendees, model.Attendee{
			Email: a.Email, Name: a.DisplayName, Response: model.ResponseStatus(a.ResponseStatus),
			Self: a.Self, Organizer: a.Organizer, Optional: a.Optional,
		})
	}
	return e, nil
}

// parseDateTime handles both forms: {date: "2026-11-14"} for all-day
// (returned as UTC midnight) and {dateTime: RFC 3339, timeZone: IANA}.
func parseDateTime(dt *calendar.EventDateTime) (time.Time, bool, error) {
	if dt.Date != "" {
		t, err := time.Parse(time.DateOnly, dt.Date)
		return t, true, err
	}
	t, err := time.Parse(time.RFC3339, dt.DateTime)
	if err != nil {
		return time.Time{}, false, err
	}
	if loc, err := time.LoadLocation(dt.TimeZone); dt.TimeZone != "" && err == nil {
		t = t.In(loc)
	}
	return t, false, nil
}

// ToAPI converts the fields clindar edits. It is used for both insert and patch:
// PATCH replaces exactly the fields present, so we always send all of them.
func ToAPI(e model.Event) *calendar.Event {
	ev := &calendar.Event{
		Id: e.ID, Summary: e.Title, Description: e.Description, Location: e.Location,
		Recurrence: e.Recurrence,
		// The Go client omits zero values; without this, clearing a
		// description would silently do nothing.
		ForceSendFields: []string{"Summary", "Description", "Location"},
	}
	if e.AllDay {
		ev.Start = &calendar.EventDateTime{Date: e.Start.UTC().Format(time.DateOnly)}
		ev.End = &calendar.EventDateTime{Date: e.End.UTC().Format(time.DateOnly)}
	} else {
		ev.Start = &calendar.EventDateTime{DateTime: e.Start.Format(time.RFC3339), TimeZone: e.TimeZone}
		ev.End = &calendar.EventDateTime{DateTime: e.End.Format(time.RFC3339), TimeZone: e.TimeZone}
	}
	ev.Attendees = AttendeesToAPI(e).Attendees
	return ev
}

// AttendeesToAPI builds a patch body containing only the attendee list.
func AttendeesToAPI(e model.Event) *calendar.Event {
	ev := &calendar.Event{}
	for _, a := range e.Attendees {
		ev.Attendees = append(ev.Attendees, &calendar.EventAttendee{
			Email: a.Email, ResponseStatus: string(a.Response), Optional: a.Optional,
		})
	}
	return ev
}

// CalendarFromAPI converts a calendar list entry.
func CalendarFromAPI(accountID string, c *calendar.CalendarListEntry) model.Calendar {
	name := c.SummaryOverride
	if name == "" {
		name = c.Summary
	}
	return model.Calendar{
		AccountID: accountID, ID: c.Id, Name: name, Color: c.BackgroundColor,
		AccessRole: c.AccessRole, Primary: c.Primary, Selected: c.Selected, TimeZone: c.TimeZone,
	}
}
```
5. Run: pass.

**Commit:** `feat(gcal): convert Google events and map errors`

---

## Task 5.2: Client

> ⚠ **Fixed in Task 13.4:** `NewClient` must give the HTTP client a per-request timeout.

> ⚠ **Extended in Task 15.9:** `MoveEvent` (`events.move`), also in `syncer.Remote`.

**Goal:** a thin wrapper over the Google client: paging, sync tokens, `If-Match`, `sendUpdates`.
`RespondEvent` (RSVP) patches **only** the attendee list (`AttendeesToAPI`) and never sends If-Match. On
events you don't own, Google won't let you change the title, so we don't send it.

**How it's tested:** `httptest.NewServer` plays Google, and `option.WithEndpoint` points the real client
library at it. That tests *our use of the library* (paths, headers, paging, error mapping) without the
network. Note that we never stub the library itself.

**Files:** create `internal/gcal/client_test.go`, `internal/gcal/client.go`

**Steps**
1. Test:
```go
package gcal_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/api/option"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
)

// newClient points a real gcal.Client at a fake HTTP server. This tests our
// use of the Google client library (paths, headers, paging, errors) without
// the network.
func newClient(t *testing.T, handler http.HandlerFunc) *gcal.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := gcal.NewClient(context.Background(), "acct", srv.Client(), option.WithEndpoint(srv.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestListEventsFollowsPagesAndReturnsSyncToken(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calendars/cal/events" {
			http.Error(w, "unexpected path "+r.URL.Path, 500)
			return
		}
		if r.URL.Query().Get("showDeleted") != "true" || r.URL.Query().Get("syncToken") != "old" {
			http.Error(w, "missing params: "+r.URL.RawQuery, 500)
			return
		}
		switch r.URL.Query().Get("pageToken") {
		case "":
			fmt.Fprint(w, `{"items":[{"id":"a","status":"cancelled"}],"nextPageToken":"p2"}`)
		case "p2":
			fmt.Fprint(w, `{"items":[{"id":"b","summary":"B","start":{"date":"2026-11-14"},"end":{"date":"2026-11-15"}}],"nextSyncToken":"new"}`)
		}
	})

	events, token, err := c.ListEvents(context.Background(), "cal", "old")

	if err != nil {
		t.Fatal(err)
	}
	if token != "new" || len(events) != 2 || events[0].ID != "a" || events[1].Title != "B" {
		t.Errorf("token=%q events=%+v", token, events)
	}
}

func TestListEventsGoneMeansTokenExpired(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":410,"message":"Sync token is no longer valid"}}`, http.StatusGone)
	})

	_, _, err := c.ListEvents(context.Background(), "cal", "old")

	if !errors.Is(err, gcal.ErrSyncTokenExpired) {
		t.Errorf("err = %v, want ErrSyncTokenExpired", err)
	}
}

func TestPatchSendsIfMatchAndMapsErrors(t *testing.T) {
	tests := []struct {
		status  int
		body    string
		wantErr error // nil = plain transient error
	}{
		{http.StatusPreconditionFailed, `{"error":{"code":412,"message":"Precondition Failed"}}`, gcal.ErrConflict},
		{http.StatusNotFound, `{"error":{"code":404,"message":"Not Found"}}`, gcal.ErrNotFound},
		{http.StatusBadRequest, `{"error":{"code":400,"message":"Bad Request"}}`, gcal.ErrPermanent},
		{http.StatusForbidden, `{"error":{"code":403,"message":"Rate","errors":[{"reason":"rateLimitExceeded"}]}}`, nil},
		{http.StatusUnauthorized, `{"error":{"code":401,"message":"Invalid Credentials"}}`, gcal.ErrUnauthorized},
		{http.StatusServiceUnavailable, `{"error":{"code":503,"message":"Unavailable"}}`, nil},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			var gotIfMatch string
			c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotIfMatch = r.Header.Get("If-Match")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			})

			_, err := c.PatchEvent(context.Background(), "cal", model.Event{ID: "e1", Title: "x"}, `"etag1"`, "none")

			if gotIfMatch != `"etag1"` {
				t.Errorf("If-Match = %q", gotIfMatch)
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, sentinel := range []error{gcal.ErrConflict, gcal.ErrNotFound, gcal.ErrPermanent, gcal.ErrUnauthorized} {
				if errors.Is(err, sentinel) != (sentinel == tt.wantErr) {
					t.Errorf("errors.Is(%v, %v) = %v", err, sentinel, !(sentinel == tt.wantErr))
				}
			}
		})
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package gcal

import (
	"context"
	"net/http"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"github.com/jabiOP/clindar/internal/model"
)

// Client talks to Google Calendar for one account. It satisfies syncer.Remote.
type Client struct {
	svc     *calendar.Service
	account string
}

// NewClient wraps an authorized HTTP client (see package auth). Extra options
// let tests point it at an httptest server with option.WithEndpoint.
func NewClient(ctx context.Context, accountID string, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	svc, err := calendar.NewService(ctx, append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)...)
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc, account: accountID}, nil
}

// PrimaryCalendarID returns the primary calendar's ID, which is the account's email.
func (c *Client) PrimaryCalendarID(ctx context.Context) (string, error) {
	cal, err := c.svc.CalendarList.Get("primary").Context(ctx).Do()
	if err != nil {
		return "", mapErr(err)
	}
	return cal.Id, nil
}

// ListCalendars returns calendars changed since token ("" = all), the IDs of
// deleted ones, and the token for next time.
func (c *Client) ListCalendars(ctx context.Context, token string) (cals []model.Calendar, deleted []string, next string, err error) {
	call := c.svc.CalendarList.List().ShowDeleted(true).MaxResults(250).Context(ctx)
	if token != "" {
		call.SyncToken(token)
	}
	for page := ""; ; {
		if page != "" {
			call.PageToken(page)
		}
		res, err := call.Do()
		if isGone(err) {
			return nil, nil, "", ErrSyncTokenExpired
		}
		if err != nil {
			return nil, nil, "", mapErr(err)
		}
		for _, item := range res.Items {
			if item.Deleted {
				deleted = append(deleted, item.Id)
				continue
			}
			cals = append(cals, CalendarFromAPI(c.account, item))
		}
		if page = res.NextPageToken; page == "" {
			return cals, deleted, res.NextSyncToken, nil
		}
	}
}

// ListEvents returns events changed since token ("" = everything), including
// cancelled ones, as series masters + exceptions (not expanded).
func (c *Client) ListEvents(ctx context.Context, calendarID, token string) ([]model.Event, string, error) {
	call := c.svc.Events.List(calendarID).ShowDeleted(true).MaxResults(2500).Context(ctx)
	if token != "" {
		call.SyncToken(token)
	}
	var out []model.Event
	for page := ""; ; {
		if page != "" {
			call.PageToken(page)
		}
		res, err := call.Do()
		if isGone(err) {
			return nil, "", ErrSyncTokenExpired
		}
		if err != nil {
			return nil, "", mapErr(err)
		}
		for _, item := range res.Items {
			e, err := FromAPI(c.account, calendarID, item)
			if err != nil {
				return nil, "", err
			}
			out = append(out, e)
		}
		if page = res.NextPageToken; page == "" {
			return out, res.NextSyncToken, nil
		}
	}
}

func (c *Client) GetEvent(ctx context.Context, calendarID, eventID string) (model.Event, error) {
	ev, err := c.svc.Events.Get(calendarID, eventID).Context(ctx).Do()
	if err != nil {
		return model.Event{}, mapErr(err)
	}
	return FromAPI(c.account, calendarID, ev)
}

func (c *Client) InsertEvent(ctx context.Context, calendarID string, e model.Event, sendUpdates string) (model.Event, error) {
	ev, err := c.svc.Events.Insert(calendarID, ToAPI(e)).SendUpdates(sendUpdates).Context(ctx).Do()
	if err != nil {
		return model.Event{}, mapErr(err)
	}
	return FromAPI(c.account, calendarID, ev)
}

func (c *Client) PatchEvent(ctx context.Context, calendarID string, e model.Event, ifMatch, sendUpdates string) (model.Event, error) {
	call := c.svc.Events.Patch(calendarID, e.ID, ToAPI(e)).SendUpdates(sendUpdates).Context(ctx)
	if ifMatch != "" {
		call.Header().Set("If-Match", ifMatch)
	}
	ev, err := call.Do()
	if err != nil {
		return model.Event{}, mapErr(err)
	}
	return FromAPI(c.account, calendarID, ev)
}

// RespondEvent sends only the attendee list, which is how you RSVP to an
// event you don't own. It is deliberately unconditional (no If-Match).
func (c *Client) RespondEvent(ctx context.Context, calendarID string, e model.Event, sendUpdates string) (model.Event, error) {
	ev, err := c.svc.Events.Patch(calendarID, e.ID, AttendeesToAPI(e)).SendUpdates(sendUpdates).Context(ctx).Do()
	if err != nil {
		return model.Event{}, mapErr(err)
	}
	return FromAPI(c.account, calendarID, ev)
}

func (c *Client) DeleteEvent(ctx context.Context, calendarID, eventID, ifMatch, sendUpdates string) error {
	call := c.svc.Events.Delete(calendarID, eventID).SendUpdates(sendUpdates).Context(ctx)
	if ifMatch != "" {
		call.Header().Set("If-Match", ifMatch)
	}
	if err := call.Do(); err != nil {
		return mapErr(err)
	}
	return nil
}
```
4. Run: pass.

**Commit:** `feat(gcal): Calendar API client`

---

## Task 5.3: The fake Google

> ⚠ **Extended in Task 15.9:** the fake's `MoveEvent` keeps IDs, gives new etags, and leaves cancelled tombstones in the source calendar.

**Goal:** `gcaltest.Fake`, an in-memory implementation of the same methods as `Client`, behaving like
Google where we care: etags, sync tokens (a change counter), cancelled tombstones, 404/409/410/412, and
instance IDs that create exceptions on write. It also has hooks: `Fail` simulates being offline, `FailGet` makes only `GetEvent` fail, `ExpireTokens`
forces a 410, and `BeforeWrite` lets a test edit locally while a push is "in flight".

Why a fake and not mocks: sync tests can then assert **outcomes** ("Google now has the event, the outbox is
empty") instead of call sequences. Because so many tests trust the fake, **the fake has its own tests**.

**Files:** create `internal/gcal/gcaltest/fake_test.go`, `internal/gcal/gcaltest/fake.go`

**Steps**
1. Test:
```go
package gcaltest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/gcal/gcaltest"
	"github.com/jabiOP/clindar/internal/model"
)

// The fake is test infrastructure that many tests trust, so it gets tests too:
// if it drifts from Google's behavior, every test built on it lies.

func newFake() *gcaltest.Fake { return gcaltest.NewFake(model.Calendar{ID: "c", Name: "C"}) }

func at(day int) time.Time { return time.Date(2026, 11, day, 14, 0, 0, 0, time.UTC) }

func TestFakeSyncTokensReturnOnlyChanges(t *testing.T) {
	f := newFake()
	ctx := context.Background()
	f.Put(model.Event{CalendarID: "c", ID: "a", Title: "A", Start: at(1), End: at(1)})
	all, token, _ := f.ListEvents(ctx, "c", "")

	f.Put(model.Event{CalendarID: "c", ID: "b", Title: "B", Start: at(2), End: at(2)})
	changed, _, _ := f.ListEvents(ctx, "c", token)

	if len(all) != 1 || len(changed) != 1 || changed[0].ID != "b" {
		t.Errorf("full = %v, changes = %v", all, changed)
	}
}

func TestFakeRejectsStaleETag(t *testing.T) {
	f := newFake()
	ctx := context.Background()
	old := f.Put(model.Event{CalendarID: "c", ID: "a", Title: "A", Start: at(1), End: at(1)})
	f.Put(old) // someone else saves: new etag

	_, err := f.PatchEvent(ctx, "c", old, old.ETag, "none")

	if !errors.Is(err, gcal.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict", err)
	}
}

func TestFakeInstanceWriteCreatesException(t *testing.T) {
	f := newFake()
	ctx := context.Background()
	f.Put(model.Event{CalendarID: "c", ID: "s", Title: "Series", Start: at(2), End: at(2).Add(time.Hour),
		Recurrence: []string{"RRULE:FREQ=WEEKLY"}})
	id := model.InstanceID("s", at(9), false)

	got, err := f.PatchEvent(ctx, "c", model.Event{ID: id, Title: "Moved", Start: at(10), End: at(10)}, "", "none")

	if err != nil || got.SeriesID != "s" || !got.OriginalStart.Equal(at(9)) || got.Title != "Moved" {
		t.Errorf("exception = %+v, %v", got, err)
	}
}
```
2. Run: compile failure.
3. Implement:
```go
// Package gcaltest provides Fake, an in-memory stand-in for Google Calendar
// that behaves like the real thing where clindar cares: etags, sync tokens,
// cancelled tombstones, 404/409/410/412, and instance IDs.
package gcaltest

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/model"
)

type Fake struct {
	mu        sync.Mutex
	calendars []model.Calendar
	events    map[string]map[string]model.Event // calendarID → eventID → event
	changedAt map[string]int                    // calendarID/eventID → change number
	clock     int                               // increments on every change
	etag      int

	// Fail, if set, is returned by every call (simulates being offline).
	Fail error
	// FailGet, if set, is returned by GetEvent only (e.g. lost access to a calendar).
	FailGet error
	// ExpireTokens makes the next list call with a token fail with ErrSyncTokenExpired.
	ExpireTokens bool
	// BeforeWrite, if set, runs at the start of every insert/patch/delete
	// (lets tests change local state while a push is "in flight").
	BeforeWrite func()
}

func NewFake(cals ...model.Calendar) *Fake {
	f := &Fake{events: map[string]map[string]model.Event{}, changedAt: map[string]int{}, calendars: cals}
	for _, c := range cals {
		f.events[c.ID] = map[string]model.Event{}
	}
	return f
}

// --- helpers for tests: simulate changes made on another device ---

// Put stores e as if edited elsewhere, assigning a fresh etag. Returns the stored event.
func (f *Fake) Put(e model.Event) model.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.put(e)
}

// Remove deletes an event as Google does: it becomes a cancelled tombstone.
func (f *Fake) Remove(calendarID, eventID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.events[calendarID][eventID]
	e.Status = model.StatusCancelled
	f.put(e)
}

// Event returns the server's copy of an event.
func (f *Fake) Event(calendarID, eventID string) (model.Event, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.events[calendarID][eventID]
	return e, ok
}

func (f *Fake) put(e model.Event) model.Event {
	f.etag++
	f.clock++
	e.ETag = fmt.Sprintf(`"%d"`, f.etag)
	if e.Status == "" {
		e.Status = model.StatusConfirmed
	}
	f.events[e.CalendarID][e.ID] = e
	f.changedAt[e.CalendarID+"/"+e.ID] = f.clock
	return e
}

// --- syncer.Remote implementation ---

func (f *Fake) ListCalendars(ctx context.Context, token string) ([]model.Calendar, []string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Fail != nil {
		return nil, nil, "", f.Fail
	}
	return append([]model.Calendar(nil), f.calendars...), nil, "cal-token", nil
}

// ListEvents uses the change counter as the sync token.
func (f *Fake) ListEvents(ctx context.Context, calendarID, token string) ([]model.Event, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Fail != nil {
		return nil, "", f.Fail
	}
	since := 0
	if token != "" {
		if f.ExpireTokens {
			f.ExpireTokens = false
			return nil, "", gcal.ErrSyncTokenExpired
		}
		since, _ = strconv.Atoi(token)
	}
	var out []model.Event
	for id, e := range f.events[calendarID] {
		if f.changedAt[calendarID+"/"+id] > since {
			out = append(out, e)
		}
	}
	return out, strconv.Itoa(f.clock), nil
}

func (f *Fake) GetEvent(ctx context.Context, calendarID, eventID string) (model.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Fail != nil {
		return model.Event{}, f.Fail
	}
	if f.FailGet != nil {
		return model.Event{}, f.FailGet
	}
	e, ok := f.events[calendarID][eventID]
	if !ok || e.Cancelled() {
		return model.Event{}, gcal.ErrNotFound
	}
	return e, nil
}

func (f *Fake) InsertEvent(ctx context.Context, calendarID string, e model.Event, sendUpdates string) (model.Event, error) {
	if err := f.beforeWrite(); err != nil {
		return model.Event{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.events[calendarID][e.ID]; exists {
		return model.Event{}, gcal.ErrAlreadyExists
	}
	e.CalendarID = calendarID
	return f.put(e), nil
}

func (f *Fake) PatchEvent(ctx context.Context, calendarID string, e model.Event, ifMatch, sendUpdates string) (model.Event, error) {
	if err := f.beforeWrite(); err != nil {
		return model.Event{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, err := f.current(calendarID, e.ID, ifMatch)
	if err != nil {
		return model.Event{}, err
	}
	cur.Title, cur.Description, cur.Location = e.Title, e.Description, e.Location
	cur.Start, cur.End, cur.AllDay, cur.TimeZone = e.Start, e.End, e.AllDay, e.TimeZone
	cur.Attendees = e.Attendees
	if e.Recurrence != nil {
		cur.Recurrence = e.Recurrence
	}
	return f.put(cur), nil
}

// RespondEvent changes only the attendee list.
func (f *Fake) RespondEvent(ctx context.Context, calendarID string, e model.Event, sendUpdates string) (model.Event, error) {
	if err := f.beforeWrite(); err != nil {
		return model.Event{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, err := f.current(calendarID, e.ID, "")
	if err != nil {
		return model.Event{}, err
	}
	cur.Attendees = e.Attendees
	return f.put(cur), nil
}

func (f *Fake) DeleteEvent(ctx context.Context, calendarID, eventID, ifMatch, sendUpdates string) error {
	if err := f.beforeWrite(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, err := f.current(calendarID, eventID, ifMatch)
	if err != nil {
		return err
	}
	cur.Status = model.StatusCancelled
	f.put(cur)
	if cur.IsMaster() { // deleting a series deletes its exceptions too
		for _, ex := range f.events[calendarID] {
			if ex.SeriesID == cur.ID {
				ex.Status = model.StatusCancelled
				f.put(ex)
			}
		}
	}
	return nil
}

// current returns the live event for a write, creating an exception on the fly
// when an instance ID of an existing series is addressed (as Google does).
func (f *Fake) current(calendarID, eventID, ifMatch string) (model.Event, error) {
	if f.Fail != nil {
		return model.Event{}, f.Fail
	}
	cur, ok := f.events[calendarID][eventID]
	if !ok {
		seriesID, orig, _, isInstance := model.ParseInstanceID(eventID)
		master, hasMaster := f.events[calendarID][seriesID]
		if !isInstance || !hasMaster || master.Cancelled() {
			return model.Event{}, gcal.ErrNotFound
		}
		cur = master.Clone()
		cur.ID, cur.SeriesID, cur.OriginalStart, cur.Recurrence, cur.ETag = eventID, seriesID, orig, nil, ""
		cur.Start, cur.End = orig, orig.Add(master.Duration())
		return cur, nil
	}
	if cur.Cancelled() {
		return model.Event{}, gcal.ErrNotFound
	}
	if ifMatch != "" && ifMatch != cur.ETag {
		return model.Event{}, gcal.ErrConflict
	}
	return cur, nil
}

func (f *Fake) beforeWrite() error {
	if f.BeforeWrite != nil {
		f.BeforeWrite()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Fail
}
```
4. Run `go test -race ./internal/gcal/...`: pass.

**Rule:** if you ever find Google behaving differently from the fake (Task 8.7 checks a few things), fix the
fake **and** add a fake test for that behavior.

**Commit:** `test(gcal): in-memory fake Google`

---

## Phase 5 checkpoint
`gofmt -l . && go vet ./... && go test -race ./... -count=1`
