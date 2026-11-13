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

	"github.com/jabiOP/clindar/internal/auth"
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
