package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/testutil"
)

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	for i := range 2 { // second open must not re-run migrations
		s, err := store.Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		s.Close()
	}
}

func TestInTxRollsBackOnError(t *testing.T) {
	s := testutil.NewStore(t)
	ctx := context.Background()
	boom := errors.New("boom")

	err := s.InTx(ctx, func(tx *store.Store) error {
		if err := tx.EnsureAccount(ctx, "a@x.com"); err != nil {
			return err
		}
		return boom
	})

	if !errors.Is(err, boom) {
		t.Fatalf("InTx error = %v, want boom", err)
	}
	ids, err := s.Accounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("accounts = %v, want none (rolled back)", ids)
	}
}
