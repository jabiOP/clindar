// Package gcaltest provides Fake, an in-memory stand-in for Google Calendar
// that behaves like the real thing where clindar cares: etags, sync tokens,
// cancelled tombstones, 404/409/410/412, and instance IDs.
package gcaltest

import (
	"context"
	"fmt"
	"maps"
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
	// ColorCalls counts EventColors calls (the palette should be fetched once).
	ColorCalls int
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
	cur.Attendees, cur.ColorID = e.Attendees, e.ColorID
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

// Palette is Google's standard event palette (colorId → background).
var Palette = map[string]string{
	"1": "#a4bdfc", "2": "#7ae7bf", "3": "#dbadff", "4": "#ff887c", "5": "#fbd75b", "6": "#ffb878",
	"7": "#46d6db", "8": "#e1e1e1", "9": "#5484ed", "10": "#51b749", "11": "#dc2127",
}

func (f *Fake) EventColors(ctx context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ColorCalls++
	if f.Fail != nil {
		return nil, f.Fail
	}
	return maps.Clone(Palette), nil
}
