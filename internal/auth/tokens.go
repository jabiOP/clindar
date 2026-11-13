package auth

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

var ErrNoToken = errors.New("not logged in: run `clindar auth login`")

// TokenStore persists one OAuth token per account.
type TokenStore interface {
	Load(account string) (*oauth2.Token, error) // ErrNoToken if absent
	Save(account string, tok *oauth2.Token) error
	Delete(account string) error
}

const keyringService = "clindar"

// KeyringStore uses the OS keyring: Secret Service on Linux, Keychain on macOS.
type KeyringStore struct{}

func (KeyringStore) Load(account string) (*oauth2.Token, error) {
	s, err := keyring.Get(keyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNoToken
	}
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	return &tok, json.Unmarshal([]byte(s), &tok)
}

func (KeyringStore) Save(account string, tok *oauth2.Token) error {
	b, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, account, string(b))
}

func (KeyringStore) Delete(account string) error {
	err := keyring.Delete(keyringService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// FileStore keeps tokens in a 0600 JSON file, for machines with no keyring
// (headless servers, minimal window managers).
type FileStore struct{ Path string }

func (f FileStore) read() (map[string]*oauth2.Token, error) {
	all := map[string]*oauth2.Token{}
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return all, nil
	}
	if err != nil {
		return nil, err
	}
	return all, json.Unmarshal(b, &all)
}

func (f FileStore) write(all map[string]*oauth2.Token) error {
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(f.Path, b, 0o600)
}

func (f FileStore) Load(account string) (*oauth2.Token, error) {
	all, err := f.read()
	if err != nil {
		return nil, err
	}
	if tok, ok := all[account]; ok {
		return tok, nil
	}
	return nil, ErrNoToken
}

func (f FileStore) Save(account string, tok *oauth2.Token) error {
	all, err := f.read()
	if err != nil {
		return err
	}
	all[account] = tok
	return f.write(all)
}

func (f FileStore) Delete(account string) error {
	all, err := f.read()
	if err != nil {
		return err
	}
	delete(all, account)
	return f.write(all)
}

// FallbackStore tries the keyring first and falls back to the file.
type FallbackStore struct {
	Primary, Secondary TokenStore
}

func (s FallbackStore) Load(account string) (*oauth2.Token, error) {
	tok, err := s.Primary.Load(account)
	if err == nil {
		return tok, nil
	}
	return s.Secondary.Load(account)
}

func (s FallbackStore) Save(account string, tok *oauth2.Token) error {
	if err := s.Primary.Save(account, tok); err == nil {
		return nil
	}
	return s.Secondary.Save(account, tok)
}

func (s FallbackStore) Delete(account string) error {
	return errors.Join(s.Primary.Delete(account), s.Secondary.Delete(account))
}

// DefaultTokenStore is the keyring with a file fallback.
func DefaultTokenStore(tokenFile string) TokenStore {
	return FallbackStore{Primary: KeyringStore{}, Secondary: FileStore{Path: tokenFile}}
}
