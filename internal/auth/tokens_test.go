package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"

	"github.com/jabiOP/clindar/internal/auth"
)

func TestFileStoreRoundTripAndPermissions(t *testing.T) {
	fs := auth.FileStore{Path: filepath.Join(t.TempDir(), "sub", "token.json")}
	if _, err := fs.Load("a"); !errors.Is(err, auth.ErrNoToken) {
		t.Fatalf("Load before Save: %v, want ErrNoToken", err)
	}

	if err := fs.Save("a", &oauth2.Token{AccessToken: "x", RefreshToken: "r"}); err != nil {
		t.Fatal(err)
	}

	tok, err := fs.Load("a")
	if err != nil || tok.RefreshToken != "r" {
		t.Errorf("Load = %+v, %v", tok, err)
	}
	info, _ := os.Stat(fs.Path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}
	fs.Delete("a")
	if _, err := fs.Load("a"); !errors.Is(err, auth.ErrNoToken) {
		t.Errorf("Load after Delete: %v", err)
	}
}

type brokenStore struct{}

func (brokenStore) Load(string) (*oauth2.Token, error) { return nil, errors.New("no keyring") }
func (brokenStore) Save(string, *oauth2.Token) error   { return errors.New("no keyring") }
func (brokenStore) Delete(string) error                { return nil }

func TestFallbackStoreUsesFileWhenKeyringFails(t *testing.T) {
	file := auth.FileStore{Path: filepath.Join(t.TempDir(), "token.json")}
	s := auth.FallbackStore{Primary: brokenStore{}, Secondary: file}

	if err := s.Save("a", &oauth2.Token{AccessToken: "x"}); err != nil {
		t.Fatal(err)
	}

	if tok, err := s.Load("a"); err != nil || tok.AccessToken != "x" {
		t.Errorf("Load = %+v, %v", tok, err)
	}
}
