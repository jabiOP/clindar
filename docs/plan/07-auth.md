# Phase 7: Authentication

`internal/auth` handles the OAuth client credentials, the browser login, and token storage and refresh.

Background (read, ~20 min):
- OAuth for desktop apps: <https://developers.google.com/identity/protocols/oauth2/native-app>
  Focus on the "loopback IP address" redirect and PKCE.
- `golang.org/x/oauth2` package docs: `Config`, `Token`, `TokenSource`, `GenerateVerifier`.

Key facts:
- The user creates their own "Desktop app" OAuth client (design §0, Q10; `docs/setup-google-cloud.md` in
  Phase 11). For desktop clients, Google says the "secret" is not actually confidential.
- We request `access_type=offline` + `prompt=consent` so Google returns a **refresh token**.
- Access tokens expire after about an hour. `oauth2` refreshes them automatically, and we save the refreshed token.

```bash
go get golang.org/x/oauth2@v0.38.0 github.com/zalando/go-keyring@v0.2.8
```

---

## Task 7.1: Token stores

**Goal:** persist tokens in the OS keyring (Secret Service on Linux, Keychain on macOS). Fall back to a
`0600` file when there's no keyring, which is common on headless boxes and minimal window managers.

**Files:** create `internal/auth/tokens_test.go`, `internal/auth/tokens.go`

We don't unit-test `KeyringStore`, because it would touch your real keyring. It's covered by the manual check in Task 8.7.

**Steps**
1. Test:
```go
package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"

	"github.com/yourname/clindar/internal/auth"
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
```
2. Run: compile failure.
3. Implement `tokens.go`:
```go
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
```
4. Run: pass.

**Commit:** `feat(auth): keyring and file token stores`

---

## Task 7.2: OAuth client config

**Goal:** load the user's `client.json`, falling back to a build-time embedded client (`-ldflags`), with
an error that tells the user what to do.

**Files:** create `internal/auth/client_test.go`, `internal/auth/client.go`

**Steps**
1. Test:
```go
package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yourname/clindar/internal/auth"
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
```
   Run: compile failure.
2. Implement:
```go
// Package auth handles OAuth: the client credentials, the browser login flow,
// and storing/refreshing tokens.
package auth

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
)

// Set at build time for an official build, e.g.
//
//	go build -ldflags "-X github.com/yourname/clindar/internal/auth.BuiltinClientID=... -X ...BuiltinClientSecret=..."
//
// For a "Desktop app" OAuth client the secret is not confidential (Google says so).
var (
	BuiltinClientID     string
	BuiltinClientSecret string
)

var ErrNoClient = errors.New("no OAuth client configured: run `clindar auth setup path/to/client_secret.json` (see docs/setup-google-cloud.md)")

// LoadClient reads the user's client JSON (downloaded from Google Cloud
// Console). If the file doesn't exist, it falls back to the built-in client.
func LoadClient(path string) (*oauth2.Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if BuiltinClientID == "" {
			return nil, ErrNoClient
		}
		return &oauth2.Config{
			ClientID: BuiltinClientID, ClientSecret: BuiltinClientSecret,
			Endpoint: google.Endpoint, Scopes: []string{calendar.CalendarScope},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, err := google.ConfigFromJSON(data, calendar.CalendarScope)
	if err != nil {
		return nil, fmt.Errorf("%s: %w (expected the JSON for a \"Desktop app\" OAuth client)", path, err)
	}
	return cfg, nil
}
```
3. Run `go test ./internal/auth/`: pass.

**Commit:** `feat(auth): load OAuth client`

---

## Task 7.3: Browser login (loopback + PKCE)

**Goal:** `Login` listens on `127.0.0.1:<random port>`, opens the browser at Google's consent page, receives
the code at the redirect, checks `state`, and exchanges the code with the PKCE verifier.

**How it's tested:** the test plays **both sides**. An `httptest` server is Google's token endpoint, and the
injected `openBrowser` func plays the user's browser by requesting the redirect URL. That's why
`openBrowser` is a parameter: dependency injection makes the untestable testable.

**Files:** create `internal/auth/login_test.go`, `internal/auth/login.go`

**Steps**
1. Test:
```go
package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/yourname/clindar/internal/auth"
)

// TestLoginFlow plays both Google (token endpoint) and the user's browser.
func TestLoginFlow(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("code") != "the-code" || r.Form.Get("code_verifier") == "" {
			http.Error(w, "bad exchange: "+r.Form.Encode(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access", "refresh_token": "refresh", "token_type": "Bearer", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	cfg := &oauth2.Config{ClientID: "id", Endpoint: oauth2.Endpoint{
		AuthURL: "https://accounts.example/auth", TokenURL: tokenSrv.URL}}

	browser := func(authURL string) error {
		u, _ := url.Parse(authURL)
		q := u.Query()
		if q.Get("code_challenge") == "" || q.Get("access_type") != "offline" {
			t.Errorf("auth URL missing PKCE/offline: %s", authURL)
		}
		// The "user" approves: Google redirects the browser back to us.
		go http.Get(q.Get("redirect_uri") + "?code=the-code&state=" + q.Get("state"))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tok, err := auth.Login(ctx, cfg, browser)

	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "access" || tok.RefreshToken != "refresh" {
		t.Errorf("token = %+v", tok)
	}
}
```
   Run: compile failure.
2. Implement:
```go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"

	"golang.org/x/oauth2"
)

// Login runs the OAuth "installed app" flow: listen on a random loopback
// port, send the user's browser to Google, and receive the code at
// http://127.0.0.1:PORT/. PKCE protects the code exchange.
// openBrowser is injected so tests can drive the flow without a browser.
func Login(ctx context.Context, cfg *oauth2.Config, openBrowser func(url string) error) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	c := *cfg // don't mutate the caller's config
	c.RedirectURL = fmt.Sprintf("http://%s/", ln.Addr())

	state := randomString()
	verifier := oauth2.GenerateVerifier()
	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return // ignore stray requests (e.g. /favicon.ico)
		case q.Get("error") != "":
			fmt.Fprintln(w, "Login failed. You can close this tab.")
			results <- result{err: fmt.Errorf("google: %s", q.Get("error"))}
		default:
			fmt.Fprintln(w, "clindar is now logged in. You can close this tab.")
			results <- result{code: q.Get("code")}
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	url := c.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier))
	if err := openBrowser(url); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-results:
		if res.err != nil {
			return nil, res.err
		}
		return c.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	}
}

// OpenBrowser tries the platform's opener; the URL is printed too in case it fails.
func OpenBrowser(url string) error {
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	return exec.Command(cmd, url).Start()
}

func randomString() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```
3. Run `go test -race ./internal/auth/`: pass.

**Commit:** `feat(auth): loopback OAuth login with PKCE`

---

## Task 7.4: Authorized HTTP client that saves refreshed tokens

**Goal:** an `*http.Client` that adds the bearer token, refreshes it when expired, and **saves** the refreshed
token. Otherwise every process start would refresh again, and a revoked refresh token would never be noticed.

This test is in package `auth` (white-box), because `savingSource` is unexported. That's fine for a small
internal piece.

**Files:** create `internal/auth/saving_test.go`, `internal/auth/client_http.go`

**Steps**
1. Test:
```go
package auth

import (
	"testing"

	"golang.org/x/oauth2"
)

type memStore map[string]*oauth2.Token

func (m memStore) Load(a string) (*oauth2.Token, error) { return m[a], nil }
func (m memStore) Save(a string, t *oauth2.Token) error { m[a] = t; return nil }
func (m memStore) Delete(a string) error                { delete(m, a); return nil }

type staticSource struct{ tok *oauth2.Token }

func (s staticSource) Token() (*oauth2.Token, error) { return s.tok, nil }

func TestSavingSourcePersistsRefreshedTokens(t *testing.T) {
	store := memStore{}
	old := &oauth2.Token{AccessToken: "old"}
	src := &savingSource{base: staticSource{&oauth2.Token{AccessToken: "new"}}, store: store, account: "a", last: old}

	if _, err := src.Token(); err != nil {
		t.Fatal(err)
	}

	if store["a"] == nil || store["a"].AccessToken != "new" {
		t.Errorf("stored = %+v, want the refreshed token", store["a"])
	}
}
```
2. Run: compile failure.
3. Implement:
```go
package auth

import (
	"context"
	"net/http"
	"sync"

	"golang.org/x/oauth2"
)

// HTTPClient returns a client that adds the account's token to requests and
// refreshes it when needed, saving refreshed tokens back to the store.
func HTTPClient(ctx context.Context, cfg *oauth2.Config, store TokenStore, account string) (*http.Client, error) {
	tok, err := store.Load(account)
	if err != nil {
		return nil, err
	}
	src := &savingSource{base: cfg.TokenSource(ctx, tok), store: store, account: account, last: tok}
	return oauth2.NewClient(ctx, oauth2.ReuseTokenSource(tok, src)), nil
}

// savingSource persists a token whenever the refresh produces a new one.
type savingSource struct {
	mu      sync.Mutex
	base    oauth2.TokenSource
	store   TokenStore
	account string
	last    *oauth2.Token
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	if s.last == nil || tok.AccessToken != s.last.AccessToken {
		s.last = tok
		_ = s.store.Save(s.account, tok) // best effort: a failed save only costs a refresh next time
	}
	return tok, nil
}
```
4. Run: pass.

**Commit:** `feat(auth): HTTP client that persists refreshed tokens`
