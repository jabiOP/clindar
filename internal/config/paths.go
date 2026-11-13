// Package config resolves file locations and loads the user's config file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths are the two directories clindar uses. We follow XDG on macOS too:
// CLI users expect ~/.config, and it keeps one code path.
type Paths struct {
	ConfigDir string // config.toml, client.json
	DataDir   string // clindar.db, sync.lock, token.json fallback
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("find home directory: %w", err)
	}
	return Paths{
		ConfigDir: resolve("CLINDAR_CONFIG_DIR", "XDG_CONFIG_HOME", filepath.Join(home, ".config")),
		DataDir:   resolve("CLINDAR_DATA_DIR", "XDG_DATA_HOME", filepath.Join(home, ".local", "share")),
	}, nil
}

func resolve(override, xdgVar, fallbackBase string) string {
	if v := os.Getenv(override); v != "" {
		return v
	}
	if v := os.Getenv(xdgVar); v != "" {
		return filepath.Join(v, "clindar")
	}
	return filepath.Join(fallbackBase, "clindar")
}

func (p Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "config.toml") }
func (p Paths) ClientFile() string { return filepath.Join(p.ConfigDir, "client.json") }
func (p Paths) DBFile() string     { return filepath.Join(p.DataDir, "clindar.db") }
func (p Paths) LockFile() string   { return filepath.Join(p.DataDir, "sync.lock") }
func (p Paths) TokenFile() string  { return filepath.Join(p.DataDir, "token.json") }

// EnsureDirs creates both directories, private to the user.
func (p Paths) EnsureDirs() error {
	for _, d := range []string{p.ConfigDir, p.DataDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}
