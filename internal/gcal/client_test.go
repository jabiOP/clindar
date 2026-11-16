package gcal_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// A stalled connection must not hang a sync (manual sync and the daemon have no
// overall deadline): each request gives up after RequestTimeout, as a transient
// error, so queued edits stay queued and are retried.
func TestStalledRequestTimesOut(t *testing.T) {
	old := gcal.RequestTimeout
	gcal.RequestTimeout = 100 * time.Millisecond
	t.Cleanup(func() { gcal.RequestTimeout = old })
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		select { // stall until the client gives up (or 5s, so a failing test can't hang)
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	start := time.Now()

	_, _, err := c.ListEvents(context.Background(), "cal", "")

	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("ListEvents = %v after %v; want an error within about %v", err, time.Since(start), gcal.RequestTimeout)
	}
	for _, sentinel := range []error{gcal.ErrConflict, gcal.ErrNotFound, gcal.ErrPermanent, gcal.ErrUnauthorized} {
		if errors.Is(err, sentinel) {
			t.Errorf("err = %v is %v; a timeout must be transient", err, sentinel)
		}
	}
}

func TestEventColorsReadsThePalette(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/colors" {
			http.Error(w, "unexpected path "+r.URL.Path, 500)
			return
		}
		fmt.Fprint(w, `{"event":{"11":{"background":"#dc2127","foreground":"#1d1d1d"}}}`)
	})

	got, err := c.EventColors(context.Background())

	if err != nil || len(got) != 1 || got["11"] != "#dc2127" {
		t.Errorf("EventColors = %v, %v", got, err)
	}
}

func TestMoveEventPostsToMove(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodPost || r.URL.Path != "/calendars/from/events/e1/move" ||
			q.Get("destination") != "to" || q.Get("sendUpdates") != "all" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), 500)
			return
		}
		fmt.Fprint(w, `{"id":"e1","etag":"\"2\"","summary":"Dentist","start":{"date":"2026-11-14"},"end":{"date":"2026-11-15"}}`)
	})

	got, err := c.MoveEvent(context.Background(), "from", "e1", "to", "all")

	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "e1" || got.CalendarID != "to" || got.ETag != `"2"` {
		t.Errorf("got %+v, want e1 in calendar to with the new etag", got)
	}
}
