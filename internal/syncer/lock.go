package syncer

import (
	"context"
	"errors"
	"os"
	"syscall"
)

// TryLock takes an exclusive, non-blocking flock on path (works on Linux and
// macOS). ok is false if another process (or another open of the file) holds it.
// The OS releases the lock automatically if the process dies.
func TryLock(path string) (unlock func(), ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true, nil
}

// WithLock runs sync only if nobody else is syncing. ran reports whether it did.
func WithLock(ctx context.Context, lockPath string, sync func(context.Context) error) (ran bool, err error) {
	unlock, ok, err := TryLock(lockPath)
	if err != nil || !ok {
		return false, err
	}
	defer unlock()
	return true, sync(ctx)
}
