package cli

import (
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/config"
	"github.com/jabiOP/clindar/internal/timeparse"
)

// Every TUI-relevant config setting must reach the TUI. (A form test alone
// can't catch a setting that's never passed in.)
func TestTUIDepsCarryConfig(t *testing.T) {
	cfg := config.Default()
	cfg.DefaultCalendar = "Work"
	cfg.WeekStart = "sunday"
	cfg.SyncInterval = config.Duration{Duration: 20 * time.Second}
	cfg.Mouse = true
	cfg.DateOrder = "mdy"

	d := tuiDeps(&App{Config: cfg, Now: time.Now}, nil)

	if d.DefaultCalendar != "Work" || d.WeekStart != time.Sunday || d.SyncInterval != 20*time.Second || !d.Mouse || d.Dates.Order != timeparse.MDY {
		t.Errorf("deps = %+v", d)
	}
	if d.Sync == nil || d.Now == nil || d.OpenURL == nil {
		t.Error("Sync, Now and OpenURL must be set")
	}
}
