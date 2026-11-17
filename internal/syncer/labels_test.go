package syncer_test

import (
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/testutil"
)

func (w *world) calendar() model.Calendar {
	w.t.Helper()
	cals, err := w.svc.Calendars(w.ctx)
	if err != nil || len(cals) == 0 {
		w.t.Fatalf("calendars: %v %v", cals, err)
	}
	return cals[0]
}

func TestLabelsAreSyncedAndSet(t *testing.T) {
	w := newWorld(t)
	w.google.SetLabels(testutil.Calendar, []model.Label{{ID: "l1", Name: "Cherry", Color: "#d81b60"}})
	w.google.Put(testutil.Timed("e1", "Lunch", w.at(16, 12), time.Hour))
	w.sync()

	if got := w.calendar().Labels; len(got) != 1 || got[0].Name != "Cherry" {
		t.Fatalf("labels = %+v", got)
	}
	w.svc.Update(w.ctx, "e1", service.Changes{LabelID: ptr("l1"), ColorID: ptr("")}, service.ScopeNone)
	w.sync()

	if g, _ := w.google.Event(testutil.Calendar, "e1"); g.LabelID != "l1" {
		t.Errorf("Google label = %q, want l1", g.LabelID)
	}
	w.svc.Update(w.ctx, "e1", service.Changes{LabelID: ptr(""), ColorID: ptr("")}, service.ScopeNone)
	w.sync()
	if g, _ := w.google.Event(testutil.Calendar, "e1"); g.LabelID != "" {
		t.Errorf("Google label = %q, want it removed", g.LabelID)
	}
}

// A label added on the web doesn't change the calendar list; an event using
// it makes clindar fetch the labels again.
func TestAnUnknownLabelRefetchesTheLabels(t *testing.T) {
	w := newWorld(t)
	w.sync()
	w.google.SetLabels(testutil.Calendar, []model.Label{{ID: "new", Name: "Mint", Color: "#3eb489"}})
	e := testutil.Timed("e1", "Lunch", w.at(16, 12), time.Hour)
	e.LabelID = "new"
	w.google.Put(e)
	w.sync()

	if l, ok := w.calendar().Label("new"); !ok || l.Color != "#3eb489" {
		t.Errorf("label new = %+v, %v", l, ok)
	}
}
