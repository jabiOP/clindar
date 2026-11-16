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

func TestFakeColors(t *testing.T) {
	f := newFake()
	ctx := context.Background()
	if p, err := f.EventColors(ctx); err != nil || p["11"] != gcaltest.Palette["11"] || len(p) != len(gcaltest.Palette) {
		t.Errorf("EventColors = %v, %v", p, err)
	}
	e := f.Put(model.Event{CalendarID: "c", ID: "a", Title: "A", ColorID: "11", Start: at(1), End: at(1)})
	e.ColorID = ""

	got, err := f.PatchEvent(ctx, "c", e, "", "none")

	if err != nil || got.ColorID != "" {
		t.Errorf("after patching ColorID \"\": %q, %v; want the color cleared", got.ColorID, err)
	}
}
