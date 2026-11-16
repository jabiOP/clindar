package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"

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

func randomString() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
