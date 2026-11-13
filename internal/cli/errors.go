package cli

import "errors"

// Exit codes (documented in docs/cli.md).
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// usageError marks mistakes in how the command was called (exit code 2).
type usageError struct{ error }

func (e usageError) Unwrap() error { return e.error }

// silentError exits non-zero without printing (e.g. `clindar next` with nothing next).
type silentError struct{ code int }

func (e silentError) Error() string { return "" }

func exitCode(err error) int {
	var se silentError
	var ue usageError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &se):
		return se.code
	case errors.As(err, &ue):
		return exitUsage
	default:
		return exitError
	}
}
