package cli

import "fmt"

// ExitError is an error that carries the exit status the program is to end
// with.
//
// main exits 1 for any error a command returns, which a command whose answer
// is in its status cannot use: diff exits 1 for two files that differ, and 2
// for a file it could not read, as diff(1) does, and a script told "1" cannot
// tell a difference from a failure. Err may be nil for a status that has
// nothing to report, the differences having been printed already.
type ExitError struct {
	// Code is the exit status.
	Code int
	// Err is what to print on standard error, or nil to print nothing.
	Err error
}

// Error returns the text of the wrapped error, or the status when there is
// none.
func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *ExitError) Unwrap() error { return e.Err }
