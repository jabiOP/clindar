package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jabiOP/clindar/internal/config"
)

func TestDaemonRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var ran []string
	old := runService
	runService = func(command string, _, _ io.Writer) error { ran = append(ran, command); return nil }
	t.Cleanup(func() { runService = old })
	var stderr bytes.Buffer
	app := &App{Paths: config.Paths{ConfigDir: filepath.Join(home, "cfg"), DataDir: filepath.Join(home, "data")},
		Config: config.Default(), Stderr: &stderr, Stdout: io.Discard}

	if code := Run(context.Background(), app, []string{"daemon", "restart"}); code != 0 || len(ran) != 0 ||
		!strings.Contains(stderr.String(), "nothing to restart") {
		t.Fatalf("not installed: exit %d, ran %v, stderr %q", code, ran, stderr.String())
	}

	unit, _ := thisOSUnit(app)
	os.MkdirAll(filepath.Dir(unit.Path), 0o755)
	os.WriteFile(unit.Path, []byte("old file, pointing somewhere else"), 0o644)
	if code := Run(context.Background(), app, []string{"daemon", "restart"}); code != 0 || len(ran) != 1 || ran[0] != unit.Restart {
		t.Fatalf("installed: exit %d, ran %v, stderr %q", code, ran, stderr.String())
	}
	if b, _ := os.ReadFile(unit.Path); !strings.Contains(string(b), filepath.Join(home, "data")) {
		t.Errorf("the service file should be rewritten for this binary and folders:\n%s", b)
	}
}
