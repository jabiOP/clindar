// Package daemon runs sync on a timer and installs itself as a user service.
package daemon

import (
	"context"
	"log/slog"
	"time"
)

// Loop calls sync every interval until ctx is cancelled. After a failure it
// waits longer each time (doubling, capped at maxBackoff) so an offline
// laptop doesn't spin; the first success resets the wait.
//
// after is time.After in production; tests pass a fake to avoid sleeping.
func Loop(ctx context.Context, interval, maxBackoff time.Duration, sync func(context.Context) error,
	after func(time.Duration) <-chan time.Time, log *slog.Logger) {
	wait := interval
	// Check ctx first: when both channels in the select below are ready, Go
	// picks one at random, so the select alone can't guarantee we stop.
	for ctx.Err() == nil {
		if err := sync(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			wait = min(wait*2, maxBackoff)
			log.Warn("sync failed", "err", err, "retry_in", wait)
		} else {
			wait = interval
		}
		select {
		case <-ctx.Done():
			return
		case <-after(wait):
		}
	}
}
