package cli

import "errors"

// Process exit codes. They are part of the CLI's contract with scripts, so they
// are documented in `dorgu health --help` and must not be renumbered casually.
const (
	// ExitOK means the command did what it was asked.
	ExitOK = 0

	// ExitError means the command could not run: an unreachable cluster, a
	// missing kubeconfig, no kubectl, a failed API call.
	ExitError = 1

	// ExitCritical means the check ran and found active critical incidents.
	// Only returned when the caller opts in with --exit-code.
	ExitCritical = 2

	// ExitUnknown means the check ran but could not see enough to judge health,
	// for example when the incident CRDs cannot be read. Only returned with
	// --exit-code. Reporting a clean bill of health from a partial read is the
	// failure mode this exists to avoid.
	ExitUnknown = 3
)

// ExitError carries an explicit process exit code out of a command, so a
// monitoring script can tell "healthy" from "on fire" from "cannot see the
// cluster" (F-04). Without one, every failure looked identical: exit 1, or worse,
// exit 0.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitCodeError) Unwrap() error { return e.err }

// withExitCode wraps err so Execute's caller exits with the given code. A nil
// err yields a silent non-zero exit, for a command that has already printed its
// own formatted message.
func withExitCode(code int, err error) error {
	if err == nil {
		err = errSilent
	}
	return &exitCodeError{code: code, err: err}
}

// ExitCode maps an error returned by Execute onto a process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var coded *exitCodeError
	if errors.As(err, &coded) {
		return coded.code
	}
	return ExitError
}
