package errors

import (
	stderr "errors"
	"fmt"
)

type Op string

type Error interface {
	error
}

type DetailedError struct {
	op    Op
	cause error
	msg   string // Human-readable error message
}

// New creates a new error with the given Op and a default message: "Internal system error."
// The Cause is set to nil.
func New(op Op) *DetailedError {
	return &DetailedError{
		op:    op,
		cause: nil,
		msg:   "Internal system error.",
	}
}

// AsDetailedError attempts to cast the given error to a DetailedError instance.
func AsDetailedError(err error) (*DetailedError, bool) {
	var dErr *DetailedError
	if stderr.As(err, &dErr) {
		return dErr, true
	}
	return nil, false
}

// Error implements the error interface.
func (e *DetailedError) Error() string {
	if e == nil {
		return ""
	}
	return e.msg
}

// Op returns the operation identifier associated with this error.
func (e *DetailedError) Op() Op {
	if e == nil {
		return ""
	}
	return e.op
}

// Msg sets the human-readable error message for a DetailedError instance.
func (e *DetailedError) Msg(msg string) *DetailedError {
	if e == nil {
		return nil
	}
	e.msg = msg
	return e
}

func (e *DetailedError) Msgf(format string, a ...any) *DetailedError {
	if e == nil {
		return nil
	}
	e.msg = fmt.Sprintf(format, a...)
	return e
}

// Err sets the cause for a DetailedError instance.
func (e *DetailedError) Err(err error) *DetailedError {
	if e == nil {
		return nil
	}
	e.cause = err
	return e
}

// Errorf formats the error message for a DetailedError instance.
//
// Syntactically the same as fmt.Errorf.
func (e *DetailedError) Errorf(format string, a ...any) *DetailedError {
	if e == nil {
		return nil
	}
	e.cause = fmt.Errorf(format, a...)
	e.msg = e.cause.Error()
	return e
}

// Cause returns the cause of a DetailedError instance.
func (e *DetailedError) Cause() error {
	if e == nil {
		return nil
	}
	if e.cause == nil {
		return nil
	}
	return e.cause
}

// Unwrap provides compatibility with the errors.Unwrap function.
func (e *DetailedError) Unwrap() error {
	return e.cause
}

// Root returns the root cause error in an error chain.
//
// It repeatedly unwraps the provided error using errors.Unwrap until there is
// no further wrapped error. If err is nil, Root returns nil. If the error
// chain contains a cycle (which should be rare), Root will stop at the last
// unique error before the cycle to avoid an infinite loop.
func Root(err error) error {
	if err == nil {
		return nil
	}

	current := err
	visited := map[error]struct{}{}

	for current != nil {
		if _, seen := visited[current]; seen {
			// Cycle detected; return the last error before the cycle.
			return current
		}
		visited[current] = struct{}{}

		next := stderr.Unwrap(current)
		if next == nil {
			return current
		}
		current = next
	}

	return err
}

func PrintChain(err error) {
	for i := 0; err != nil; i++ {
		if d, ok := AsDetailedError(err); ok {
			fmt.Printf("[%d] op=%s msg=%s\n", i, d.Op(), d.Error())
		} else {
			fmt.Printf("[%d] %s\n", i, err)
		}
		err = stderr.Unwrap(err)
	}
}
