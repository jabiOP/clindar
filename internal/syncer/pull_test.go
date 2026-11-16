package syncer_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestPullBringsInRemoteChanges(t *testing.T) {
	w := newWorld(t)
	w.google.Put(testutil.Timed("e1", "Dentist", w.at(16, 9), time.Hour))
	w.sync()
	if got := w.localTitle("e1"); got != "Dentist" {
		t.Fatalf("after first sync: %q", got)
	}

	ev, _ := w.google.Event(testutil.Calendar, "e1")
	ev.Title = "Dentist (moved)"
	w.google.Put(ev)
	w.sync()
	if got := w.localTitle("e1"); got != "Dentist (moved)" {
		t.Errorf("after remote edit: %q", got)
	}

	w.google.Remove(testutil.Calendar, "e1")
	w.sync()
	if got := w.localTitle("e1"); got != "<none>" {
		t.Errorf("after remote delete: %q", got)
	}
}

func TestPullDoesNotOverwriteUnpushedEdit(t *testing.T) {
	w := newWorld(t)
	w.google.Put(testutil.Timed("e1", "Original", w.at(16, 9), time.Hour))
	w.sync()
	w.svc.Update(w.ctx, "e1", service.Changes{Title: ptr("Mine")}, service.ScopeNone)
	remote, _ := w.google.Event(testutil.Calendar, "e1")
	remote.Title = "Theirs"
	w.google.Put(remote)

	if err := w.engine.Pull(w.ctx); err != nil { // pull only, as if an edit landed mid-sync
		t.Fatal(err)
	}

	if got := w.localTitle("e1"); got != "Mine" {
		t.Errorf("local title = %q, want Mine until the push resolves it", got)
	}
}

func TestExpiredSyncTokenResyncs(t *testing.T) {
	w := newWorld(t)
	w.google.Put(testutil.Timed("e1", "Keep", w.at(16, 9), time.Hour))
	w.google.Put(testutil.Timed("e2", "Stale", w.at(16, 11), time.Hour))
	w.sync()
	w.google.Remove(testutil.Calendar, "e2")
	w.google.ExpireTokens = true

	w.sync()

	if w.localTitle("e1") != "Keep" || w.localTitle("e2") != "<none>" {
		t.Errorf("e1=%q e2=%q", w.localTitle("e1"), w.localTitle("e2"))
	}
}

func TestPaletteIsFetchedOnce(t *testing.T) {
	w := newWorld(t)
	w.sync()
	w.sync()

	if w.google.ColorCalls != 1 {
		t.Errorf("EventColors called %d times, want 1 (cached in the store)", w.google.ColorCalls)
	}
}
