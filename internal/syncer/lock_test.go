package syncer_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jabiOP/clindar/internal/syncer"
)

func TestLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.lock")
	unlock, ok, err := syncer.TryLock(path)
	if err != nil || !ok {
		t.Fatalf("first TryLock = %v, %v", ok, err)
	}

	ran, err := syncer.WithLock(context.Background(), path, func(context.Context) error {
		t.Error("sync ran while lock was held")
		return nil
	})
	if ran || err != nil {
		t.Errorf("WithLock while held = %v, %v; want false, nil", ran, err)
	}

	unlock()
	ran, err = syncer.WithLock(context.Background(), path, func(context.Context) error { return nil })
	if !ran || err != nil {
		t.Errorf("WithLock after unlock = %v, %v; want true, nil", ran, err)
	}
}
