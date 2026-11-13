package config_test

import (
	"path/filepath"
	"testing"

	"github.com/jabiOP/clindar/internal/config"
)

func TestDefaultPathsPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("fallback to ~/.config and ~/.local/share", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("XDG_DATA_HOME", "")
		t.Setenv("CLINDAR_CONFIG_DIR", "")
		t.Setenv("CLINDAR_DATA_DIR", "")
		p, err := config.DefaultPaths()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".config", "clindar"); p.ConfigDir != want {
			t.Errorf("ConfigDir = %q, want %q", p.ConfigDir, want)
		}
		if want := filepath.Join(home, ".local", "share", "clindar"); p.DataDir != want {
			t.Errorf("DataDir = %q, want %q", p.DataDir, want)
		}
	})

	t.Run("XDG vars", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/xdg/c")
		t.Setenv("XDG_DATA_HOME", "/xdg/d")
		t.Setenv("CLINDAR_CONFIG_DIR", "")
		t.Setenv("CLINDAR_DATA_DIR", "")
		p, _ := config.DefaultPaths()
		if p.ConfigDir != "/xdg/c/clindar" || p.DataDir != "/xdg/d/clindar" {
			t.Errorf("got %+v", p)
		}
	})

	t.Run("CLINDAR_* overrides win", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/xdg/c")
		t.Setenv("CLINDAR_CONFIG_DIR", "/o/c")
		t.Setenv("CLINDAR_DATA_DIR", "/o/d")
		p, _ := config.DefaultPaths()
		if p.ConfigDir != "/o/c" || p.DataDir != "/o/d" {
			t.Errorf("got %+v", p)
		}
		if p.DBFile() != "/o/d/clindar.db" {
			t.Errorf("DBFile = %q", p.DBFile())
		}
	})
}
