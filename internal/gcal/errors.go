// Package gcal adapts Google Calendar API v3 to clindar's model. Google types
// never leave this package.
package gcal

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/api/googleapi"
)

// Errors the sync engine reacts to. Anything else is treated as transient
// (network trouble, 5xx, rate limits) and retried later.
var (
	ErrNotFound         = errors.New("not found on Google")      // 404, or 410 for an event
	ErrAlreadyExists    = errors.New("already exists on Google") // 409 on insert
	ErrConflict         = errors.New("changed on Google")        // 412: If-Match failed
	ErrSyncTokenExpired = errors.New("sync token expired")       // 410 on a list call
	ErrPermanent        = errors.New("rejected by Google")       // other 4xx: retrying won't help
	// ErrUnauthorized (401) is NOT permanent: the token may refresh, or the user
	// may log in again. Ops stay queued, and the message says what to do.
	ErrUnauthorized = errors.New("Google rejected our login (run `clindar auth login` if this persists)")
)

func mapErr(err error) error {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return err
	}
	switch code := gerr.Code; {
	case code == http.StatusNotFound || code == http.StatusGone:
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case code == http.StatusConflict:
		return fmt.Errorf("%w: %v", ErrAlreadyExists, err)
	case code == http.StatusPreconditionFailed:
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case code == http.StatusUnauthorized:
		return fmt.Errorf("%w: %v", ErrUnauthorized, err)
	case code == http.StatusTooManyRequests || isRateLimit(gerr):
		return err
	case code >= 400 && code < 500:
		return fmt.Errorf("%w: %v", ErrPermanent, err)
	}
	return err
}

// Google reports rate and quota limits as 403 with reasons like
// "rateLimitExceeded", "userRateLimitExceeded", "dailyLimitExceeded", "quotaExceeded".
func isRateLimit(gerr *googleapi.Error) bool {
	for _, item := range gerr.Errors {
		reason := strings.ToLower(item.Reason)
		if strings.Contains(reason, "limitexceeded") || strings.Contains(reason, "quotaexceeded") {
			return true
		}
	}
	return false
}

func isGone(err error) bool {
	var gerr *googleapi.Error
	return errors.As(err, &gerr) && gerr.Code == http.StatusGone
}
