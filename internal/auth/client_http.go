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
