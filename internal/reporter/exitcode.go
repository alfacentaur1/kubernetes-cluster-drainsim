package reporter

import "errors"

// Exit codes returned by drainsim.
const (
	ExitOK          = 0 // the check passed
	ExitCheckFailed = 1 // the check failed
	ExitError       = 2 // the tool could not run
)

// CheckFailedError means the tool ran correctly but the check did not pass,
// for example a drain would violate a PodDisruptionBudget.
type CheckFailedError struct {
	// Reason explains why the check failed.
	Reason string
}

// Error returns the reason the check failed.
func (e *CheckFailedError) Error() string {
	return e.Reason
}

// ExitCode maps an error to a process exit code: ExitOK for nil,
// ExitCheckFailed for a CheckFailedError (also when wrapped), and ExitError
// for anything else.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var cf *CheckFailedError
	if errors.As(err, &cf) {
		return ExitCheckFailed
	}
	return ExitError
}
