package tools

import "fmt"

// ExitError carries a process exit code and an optional agent next-step hint
// out of a command's Run. Dispatch renders it for humans (Msg + hint on stderr)
// and, under --json, as {error, hint, code}. A Code of 0 is coerced to 1 by
// Dispatch — an ExitError is a failure, and a zero there is a bug.
type ExitError struct {
	Code int
	Msg  string
	Hint string
}

func (e *ExitError) Error() string { return e.Msg }

// Exitf builds an *ExitError with a formatted message.
func Exitf(code int, format string, a ...any) *ExitError {
	return &ExitError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// WithHint attaches an agent next-step hint and returns the same *ExitError.
func (e *ExitError) WithHint(h string) *ExitError { e.Hint = h; return e }
