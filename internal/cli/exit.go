package cli

import "errors"

// ErrPending marks a command that kept local work but could not finish a
// remote operation such as a push or PR update. Callers can distinguish this
// from a hard failure by inspecting ExitCode.
var ErrPending = errors.New("publish is pending")

const (
	ExitOK      = 0
	ExitFailure = 1
	ExitPending = 2
)

// ExitCode maps a command result to the process exit status. A pending publish
// is deliberately not success: scripts must be able to retry it.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrPending):
		return ExitPending
	default:
		return ExitFailure
	}
}
