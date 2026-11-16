package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jabiOP/clindar/internal/config"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if c != config.Default() {
		t.Errorf("got %+v, want defaults %+v", c, config.Default())
	}
}

func TestLoadOverridesOnlyGivenKeys(t *testing.T) {
	c, err := config.Load(writeFile(t, "sync_interval = \"20s\"\nweek_start = \"sunday\"\nmouse = true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.SyncInterval.Duration != 20*time.Second {
		t.Errorf("SyncInterval = %v", c.SyncInterval)
	}
	if c.FirstWeekday() != time.Sunday {
		t.Errorf("FirstWeekday = %v", c.FirstWeekday())
	}
	if !c.Mouse || config.Default().Mouse {
		t.Errorf("Mouse = %v, want true here and false by default", c.Mouse)
	}
	if c.StaleAfter != config.Default().StaleAfter {
		t.Errorf("StaleAfter changed to %v", c.StaleAfter)
	}
}

func TestLoadRejectsBadInput(t *testing.T) {
	tests := map[string]string{
		"unknown key":        "sync_intervall = \"20s\"\n",
		"bad duration":       "sync_interval = \"soon\"\n",
		"bad week_start":     "week_start = \"wednesday\"\n",
		"bad send_updates":   "send_updates = \"some\"\n",
		"interval too short": "sync_interval = \"1s\"\n",
		"bad date_order":     "date_order = \"ymd\"\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := config.Load(writeFile(t, content))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "config.toml") {
				t.Errorf("error %q should mention the file", err)
			}
		})
	}
}

func TestDateOrder(t *testing.T) {
	if got := config.Default().DateOrder; got != "dmy" {
		t.Errorf("default date_order = %q, want dmy", got)
	}
	c, err := config.Load(writeFile(t, "date_order = \"mdy\"\n"))
	if err != nil || c.DateOrder != "mdy" {
		t.Errorf("Load = %+v, %v; want date_order mdy", c, err)
	}
}
