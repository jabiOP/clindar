package gcal

import (
	"context"
	"net/http"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"github.com/jabiOP/clindar/internal/model"
)

// Client talks to Google Calendar for one account. It satisfies syncer.Remote.
type Client struct {
	svc     *calendar.Service
	account string
}

// RequestTimeout bounds every request to Google. Without it a stalled
// connection hangs a sync for as long as it stalls (manual sync and the daemon
// have no overall deadline). A timeout is a transient error: queued edits stay
// queued and are retried. Tests lower it.
var RequestTimeout = 30 * time.Second

// NewClient wraps an authorized HTTP client (see package auth). Extra options
// let tests point it at an httptest server with option.WithEndpoint.
func NewClient(ctx context.Context, accountID string, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	if hc.Timeout == 0 {
		limited := *hc // a copy: don't change the caller's client
		limited.Timeout = RequestTimeout
		hc = &limited
	}
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

// EventColors returns Google's event palette: colorId → background "#rrggbb".
func (c *Client) EventColors(ctx context.Context) (map[string]string, error) {
	res, err := c.svc.Colors.Get().Context(ctx).Do()
	if err != nil {
		return nil, mapErr(err)
	}
	out := map[string]string{}
	for id, def := range res.Event {
		out[id] = def.Background
	}
	return out, nil
}
