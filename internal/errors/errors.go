package errors

import (
	stderr "errors"
	"fmt"
)

type Op string

type DetailedError struct {
	op    Op
	cause error
	msg   string // Human-readable error message
}

// New creates a new error with the given Op. The message and cause are
// empty by default; callers attach them via the builder methods (.Msg,
// .Msgf, .Err). An error that has only an op set will format as the op
// string; if neither op nor msg nor cause is set, Error() returns "".
func New(op Op) *DetailedError {
	return &DetailedError{
		op: op,
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
//
// Returns a colon-separated representation of the error frame and its
// causal chain: "<op>: <msg>: <cause.Error()>". If a component is empty
// or nil it is omitted. Examples:
//
//	New("pkg.Func")                          -> "pkg.Func"
//	New("pkg.Func").Msg("x")                 -> "pkg.Func: x"
//	New("pkg.Func").Err(inner)               -> "pkg.Func: inner.Error()"
//	New("pkg.Func").Err(inner).Msg("x")      -> "pkg.Func: x: inner.Error()"
//
// This matches the stdlib convention for wrapped errors (fmt.Errorf with
// %w produces a similar chained string). Callers that need only the
// message set on this specific frame — without the op prefix or the
// recursive cause — should use LocalMsg() instead.
func (e *DetailedError) Error() string {
	if e == nil {
		return ""
	}
	var local string
	switch {
	case e.op != "" && e.msg != "":
		local = string(e.op) + ": " + e.msg
	case e.op != "":
		local = string(e.op)
	case e.msg != "":
		local = e.msg
	}
	if e.cause == nil {
		return local
	}
	if local == "" {
		return e.cause.Error()
	}
	return local + ": " + e.cause.Error()
}

// Op returns the operation identifier associated with this error.
func (e *DetailedError) Op() Op {
	if e == nil {
		return ""
	}
	return e.op
}

// WithMsg sets the human-readable message on a DetailedError and returns
// the builder for further chaining. Nil-safe: a nil receiver returns nil.
func (e *DetailedError) WithMsg(msg string) *DetailedError {
	if e == nil {
		return nil
	}
	e.msg = msg
	return e
}

// WithMsgf sets the human-readable message via fmt.Sprintf formatting and
// returns the builder for further chaining. Nil-safe: a nil receiver
// returns nil.
func (e *DetailedError) WithMsgf(format string, a ...any) *DetailedError {
	if e == nil {
		return nil
	}
	e.msg = fmt.Sprintf(format, a...)
	return e
}

// WithErr sets the cause error on a DetailedError and returns the builder
// for further chaining. Nil-safe: a nil receiver returns nil.
//
// The previous Errorf method — which combined fmt.Errorf and message-setting
// into a single call — has been removed because its dual-role behavior
// silently clobbered previously-set causes and was a footgun. Callers that
// need its behavior should use explicit composition:
//
//	errors.New(op).WithErr(fmt.Errorf("context: %w", err))
//	errors.New(op).WithMsgf("context: %v", err)
//
// depending on whether the intent is to wrap the error as a cause or to
// produce a formatted message only. See docs/reviews/internal-errors.md
// finding 4.4 for the background.
func (e *DetailedError) WithErr(err error) *DetailedError {
	if e == nil {
		return nil
	}
	e.cause = err
	return e
}

// Unwrap provides compatibility with the errors.Unwrap function.
func (e *DetailedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Root returns the root cause error in an error chain.
//
// It repeatedly unwraps the provided error using errors.Unwrap until there is
// no further wrapped error. If err is nil, Root returns nil. Root is
// depth-limited to 100 unwrap steps as a guard against pathological chains
// (cycles, or honestly broken wrappers that return themselves); if the limit
// is hit, the current error at that depth is returned and traversal stops.
//
// The depth limit replaces an earlier map-based cycle detection that used
// error values as map keys. That approach was unsafe for error types whose
// concrete type contains non-comparable fields (slices, maps, functions) —
// inserting such a value into a map panics at runtime. A simple depth limit
// is panic-safe, cheap, and precise enough for practical purposes: real
// error chains are rarely more than a handful of frames deep, and anything
// approaching 100 is either a bug or an adversarial input that we're happy
// to truncate.
func Root(err error) error {
	if err == nil {
		return nil
	}
	const maxDepth = 100
	current := err
	for range maxDepth {
		next := stderr.Unwrap(current)
		if next == nil {
			return current
		}
		current = next
	}
	return current
}
