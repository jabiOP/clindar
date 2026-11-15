// Package recur expands recurring series into instances and rewrites
// recurrence rules for "this and following" edits. Pure functions only.
package recur

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/jabiOP/clindar/internal/model"
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
	inst.Link = instanceLink(master.Link, master.ID, inst.ID)
	return inst
}

// eidEncodings are the base64 variants Google's eid parameter may use; we
// re-encode with whichever one decoded it.
var eidEncodings = []*base64.Encoding{base64.RawURLEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.StdEncoding}

// instanceLink turns a series' web link into one occurrence's. Google's link
// carries eid = base64("<event id> <calendar>"), where a series' event id is
// usually its first occurrence's ("<series id>_<date>"); we swap the event ID
// and keep the calendar part exactly as Google wrote it. Links in any other shape are
// returned unchanged.
func instanceLink(link, seriesID, instanceID string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	q := u.Query()
	eid := q.Get("eid")
	for _, enc := range eidEncodings {
		raw, err := enc.DecodeString(eid)
		if err != nil {
			continue
		}
		// The ID part is the series ID, or (as Google writes it for a series)
		// its first occurrence's ID, "<series id>_<date>". Replace it whole.
		id, rest, ok := strings.Cut(string(raw), " ")
		if !ok || (id != seriesID && !strings.HasPrefix(id, seriesID+"_")) {
			return link
		}
		q.Set("eid", enc.EncodeToString([]byte(instanceID+" "+rest)))
		u.RawQuery = q.Encode()
		return u.String()
	}
	return link
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
