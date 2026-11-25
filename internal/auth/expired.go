package auth

import (
	"errors"

	"golang.org/x/oauth2"
)

// LoginExpired reports whether err comes from Google refusing the stored
// refresh token (expired or revoked), so the user must log in again. The
// HTTP client wraps this in a *url.Error, so it can look like a network
// failure; check this first.
func LoginExpired(err error) bool {
	var re *oauth2.RetrieveError
	return errors.As(err, &re) && re.ErrorCode == "invalid_grant"
}

// LoginExpiredHint is what to tell the user when LoginExpired is true.
const LoginExpiredHint = "your Google login expired or was revoked: run `clindar auth login`"
