package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jabiOP/clindar/internal/auth"
)

func TestLoadClient(t *testing.T) {
	dir := t.TempDir()
	if _, err := auth.LoadClient(filepath.Join(dir, "missing.json")); !errors.Is(err, auth.ErrNoClient) {
		t.Errorf("missing file, no builtin: err = %v, want ErrNoClient", err)
	}
	path := filepath.Join(dir, "client.json")
	os.WriteFile(path, []byte(`{"installed":{"client_id":"id123","client_secret":"s",
		"auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token",
		"redirect_uris":["http://localhost"]}}`), 0o600)

	cfg, err := auth.LoadClient(path)

	if err != nil || cfg.ClientID != "id123" || len(cfg.Scopes) != 1 {
		t.Errorf("LoadClient = %+v, %v", cfg, err)
	}
}
