// Package recur expands recurring series into instances and rewrites
// recurrence rules for "this and following" edits. Pure functions only.
package recur

import (
	"errors"
	"fmt"
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
