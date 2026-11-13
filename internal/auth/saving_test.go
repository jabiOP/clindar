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
