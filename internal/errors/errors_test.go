package errors

import (
	stderr "errors"
	"fmt"
	"strings"
	"testing"
)

// ---- Construction and basic builder behavior ---------------------------------

func TestNew_BasicConstruction(t *testing.T) {
	e := New("pkg.Func")
	if e == nil {
		t.Fatal("New returned nil")
	}
	if got := e.Op(); got != "pkg.Func" {
		t.Fatalf("Op() = %q, want %q", got, "pkg.Func")
	}
	// Unset fields default to zero values.
	if got := e.Unwrap(); got != nil {
		t.Fatalf("Unwrap() on fresh error = %v, want nil", got)
	}
}

func TestNew_EmptyOp(t *testing.T) {
	// An empty op is allowed (not validated) but should be reflected in Error().
	e := New("")
	if e == nil {
		t.Fatal("New(\"\") returned nil")
	}
	if got := e.Error(); got != "" {
		t.Fatalf("Error() on empty error = %q, want empty string", got)
	}
}

// ---- WithMsg / WithMsgf ------------------------------------------------------

func TestWithMsg_SetsMessage(t *testing.T) {
	e := New("pkg.Func").WithMsg("something went wrong")
	if got, want := e.Error(), "pkg.Func: something went wrong"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestWithMsgf_FormatsMessage(t *testing.T) {
	e := New("pkg.Func").WithMsgf("value %d out of range [%d, %d]", 42, 0, 10)
	want := "pkg.Func: value 42 out of range [0, 10]"
	if got := e.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestWithMsg_OverwritesPreviousMessage(t *testing.T) {
	e := New("pkg.Func").WithMsg("first").WithMsg("second")
	if !strings.Contains(e.Error(), "second") {
		t.Fatalf("Error() = %q, expected to contain 'second'", e.Error())
	}
	if strings.Contains(e.Error(), "first") {
		t.Fatalf("Error() = %q, expected NOT to contain 'first'", e.Error())
	}
}

// ---- WithErr -----------------------------------------------------------------

func TestWithErr_SetsCause(t *testing.T) {
	inner := stderr.New("inner")
	e := New("pkg.Func").WithErr(inner)
	if got := e.Unwrap(); got != inner {
		t.Fatalf("Unwrap() = %v, want %v", got, inner)
	}
}

func TestWithErr_IncludedInErrorString(t *testing.T) {
	inner := stderr.New("db connection refused")
	e := New("pkg.Func").WithErr(inner)
	if got, want := e.Error(), "pkg.Func: db connection refused"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestWithErr_WithBothMsgAndCause(t *testing.T) {
	inner := stderr.New("connection refused")
	e := New("pkg.Func").WithErr(inner).WithMsg("failed to open db")
	want := "pkg.Func: failed to open db: connection refused"
	if got := e.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// ---- Error() format across all combinations ---------------------------------

func TestError_OpOnly(t *testing.T) {
	e := New("pkg.Func")
	if got, want := e.Error(), "pkg.Func"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestError_OpAndMsg(t *testing.T) {
	e := New("pkg.Func").WithMsg("boom")
	if got, want := e.Error(), "pkg.Func: boom"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestError_OpAndCause(t *testing.T) {
	e := New("pkg.Func").WithErr(stderr.New("cause"))
	if got, want := e.Error(), "pkg.Func: cause"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestError_OpMsgAndCause(t *testing.T) {
	e := New("pkg.Func").WithErr(stderr.New("cause")).WithMsg("msg")
	if got, want := e.Error(), "pkg.Func: msg: cause"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestError_MsgOnlyNoOp(t *testing.T) {
	// Edge case: empty op, message set. Shouldn't happen in practice but
	// verify the formatter degrades gracefully.
	e := New("").WithMsg("bare message")
	if got, want := e.Error(), "bare message"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestError_CauseOnlyNoOpNoMsg(t *testing.T) {
	e := New("").WithErr(stderr.New("bare cause"))
	if got, want := e.Error(), "bare cause"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestError_NestedDetailedErrors(t *testing.T) {
	inner := New("inner.Op").WithMsg("inner msg")
	middle := New("middle.Op").WithErr(inner).WithMsg("middle msg")
	outer := New("outer.Op").WithErr(middle).WithMsg("outer msg")

	want := "outer.Op: outer msg: middle.Op: middle msg: inner.Op: inner msg"
	if got := outer.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// ---- Nil-safety -------------------------------------------------------------

func TestNilReceiver_AllMethodsSafe(t *testing.T) {
	var e *DetailedError

	// None of these should panic on a nil receiver. We don't care about
	// return values; we care about not crashing.
	_ = e.Error()
	_ = e.Op()
	_ = e.Unwrap()
	_ = e.WithMsg("x")
	_ = e.WithMsgf("x %d", 1)
	_ = e.WithErr(stderr.New("x"))

	// Chained calls on a nil receiver should also not panic.
	result := e.WithMsg("a").WithErr(stderr.New("b")).WithMsgf("c %d", 1)
	if result != nil {
		t.Fatalf("chained nil receiver should return nil, got %v", result)
	}

	// Error() on nil should return empty string.
	if got := e.Error(); got != "" {
		t.Fatalf("nil.Error() = %q, want empty string", got)
	}

	// Op() on nil should return empty Op.
	if got := e.Op(); got != "" {
		t.Fatalf("nil.Op() = %q, want empty string", got)
	}
}

// ---- AsDetailedError --------------------------------------------------------

func TestAsDetailedError_Positive(t *testing.T) {
	original := New("pkg.Func").WithMsg("boom")
	got, ok := AsDetailedError(error(original))
	if !ok {
		t.Fatal("AsDetailedError returned ok=false, want true")
	}
	if got != original {
		t.Fatalf("AsDetailedError returned %v, want %v", got, original)
	}
}

func TestAsDetailedError_Negative(t *testing.T) {
	plain := stderr.New("not a detailed error")
	got, ok := AsDetailedError(plain)
	if ok {
		t.Fatal("AsDetailedError(plain) returned ok=true, want false")
	}
	if got != nil {
		t.Fatalf("AsDetailedError(plain) returned %v, want nil", got)
	}
}

func TestAsDetailedError_WrappedViaFmt(t *testing.T) {
	inner := New("inner.Op").WithMsg("inner")
	wrapped := fmt.Errorf("outer context: %w", inner)
	got, ok := AsDetailedError(wrapped)
	if !ok {
		t.Fatal("AsDetailedError(wrapped) returned ok=false, want true")
	}
	if got != inner {
		t.Fatalf("AsDetailedError unwrapped to %v, want %v", got, inner)
	}
}

func TestAsDetailedError_Nil(t *testing.T) {
	got, ok := AsDetailedError(nil)
	if ok {
		t.Fatal("AsDetailedError(nil) returned ok=true, want false")
	}
	if got != nil {
		t.Fatalf("AsDetailedError(nil) returned %v, want nil", got)
	}
}

// ---- Unwrap interop with stdlib errors.Is / errors.As ----------------------

func TestErrorsIs_SentinelThroughDetailedErrorChain(t *testing.T) {
	// Wrapping ErrNotFound in a DetailedError should still allow errors.Is
	// to match the sentinel through the chain.
	wrapped := New("pkg.Func").WithErr(ErrNotFound).WithMsg("lookup failed")
	if !stderr.Is(wrapped, ErrNotFound) {
		t.Fatalf("errors.Is(wrapped, ErrNotFound) = false, want true")
	}
}

func TestErrorsIs_StdErrorThroughDetailedErrorChain(t *testing.T) {
	sentinel := stderr.New("custom sentinel")
	wrapped := New("pkg.Func").WithErr(sentinel).WithMsg("wrapping context")
	if !stderr.Is(wrapped, sentinel) {
		t.Fatalf("errors.Is(wrapped, sentinel) = false, want true")
	}
}

func TestErrorsAs_ExtractsDetailedError(t *testing.T) {
	original := New("pkg.Func").WithMsg("extract me")
	wrapped := fmt.Errorf("outer: %w", original)

	var extracted *DetailedError
	if !stderr.As(wrapped, &extracted) {
		t.Fatal("errors.As(wrapped, &dErr) = false, want true")
	}
	if extracted != original {
		t.Fatalf("extracted = %v, want %v", extracted, original)
	}
	if extracted.Op() != "pkg.Func" {
		t.Fatalf("extracted.Op() = %q, want %q", extracted.Op(), "pkg.Func")
	}
}

// ---- Unwrap chain walking --------------------------------------------------

func TestUnwrap_DetailedErrorChain(t *testing.T) {
	leaf := stderr.New("leaf")
	mid := New("mid.Op").WithErr(leaf).WithMsg("mid")
	top := New("top.Op").WithErr(mid).WithMsg("top")

	if got := stderr.Unwrap(top); got != mid {
		t.Fatalf("Unwrap(top) = %v, want mid", got)
	}
	if got := stderr.Unwrap(mid); got != leaf {
		t.Fatalf("Unwrap(mid) = %v, want leaf", got)
	}
	if got := stderr.Unwrap(leaf); got != nil {
		t.Fatalf("Unwrap(leaf) = %v, want nil", got)
	}
}

// ---- Root -------------------------------------------------------------------

func TestRoot_Nil(t *testing.T) {
	if got := Root(nil); got != nil {
		t.Fatalf("Root(nil) = %v, want nil", got)
	}
}

func TestRoot_SingleError(t *testing.T) {
	e := New("pkg.Func").WithMsg("single")
	if got := Root(e); got != e {
		t.Fatalf("Root(e) = %v, want %v", got, e)
	}
}

func TestRoot_SimpleChain(t *testing.T) {
	leaf := stderr.New("root cause")
	wrapped := fmt.Errorf("mid: %w", leaf)
	top := New("top.Op").WithErr(wrapped).WithMsg("top")

	if got := Root(top); got != leaf {
		t.Fatalf("Root(top) = %v, want %v", got, leaf)
	}
}

func TestRoot_DetailedErrorChain(t *testing.T) {
	root := New("deep.Op").WithMsg("root cause")
	mid := New("mid.Op").WithErr(root)
	top := New("top.Op").WithErr(mid)

	got := Root(top)
	if got != root {
		t.Fatalf("Root(top) = %v, want %v", got, root)
	}
	if dErr, ok := AsDetailedError(got); !ok {
		t.Fatalf("Root(top) is not DetailedError: %T", got)
	} else if dErr.Op() != "deep.Op" {
		t.Fatalf("root Op() = %q, want %q", dErr.Op(), "deep.Op")
	}
}

func TestRoot_DepthLimit(t *testing.T) {
	// Build a chain of more than maxDepth (100) wrappers to verify the
	// depth limit kicks in and Root terminates without running indefinitely.
	// We use a simple custom error type that unwraps to the next layer.
	current := error(stderr.New("deepest"))
	for i := range 200 {
		current = fmt.Errorf("layer %d: %w", i, current)
	}
	// Root should return something (not panic, not hang). It won't be the
	// true root — it'll be whatever was at depth 100 — which is exactly
	// the documented truncation behavior.
	got := Root(current)
	if got == nil {
		t.Fatal("Root(deep chain) returned nil, want non-nil")
	}
}

// ---- Unhashable-error-in-chain safety --------------------------------------

// unhashableError has a non-comparable underlying type (a slice field),
// which means it cannot be used as a map key. The previous Root()
// implementation used a map keyed by error values, which would panic when
// an unhashableError was inserted. The current depth-limited implementation
// should handle it without panic.
type unhashableError struct {
	items []string
}

func (e unhashableError) Error() string {
	return strings.Join(e.items, ", ")
}

func TestRoot_UnhashableErrorInChain(t *testing.T) {
	leaf := unhashableError{items: []string{"a", "b", "c"}}
	wrapped := New("mid.Op").WithErr(leaf)
	top := New("top.Op").WithErr(wrapped)

	// This should not panic.
	got := Root(top)
	if got == nil {
		t.Fatal("Root returned nil with unhashable leaf, want non-nil")
	}
	// Root should unwrap through the DetailedErrors to the unhashable leaf.
	if _, ok := got.(unhashableError); !ok {
		t.Fatalf("Root = %T, want unhashableError", got)
	}
}

// ---- ErrNotFound sentinel --------------------------------------------------

func TestErrNotFound_IsSentinel(t *testing.T) {
	// Direct comparison.
	if !stderr.Is(ErrNotFound, ErrNotFound) {
		t.Fatal("errors.Is(ErrNotFound, ErrNotFound) = false, want true")
	}
}

func TestErrNotFound_ThroughWrapping(t *testing.T) {
	wrapped := New("lookup.Call").WithErr(ErrNotFound).WithMsg("no such callsign")
	if !stderr.Is(wrapped, ErrNotFound) {
		t.Fatal("errors.Is(wrapped, ErrNotFound) = false, want true")
	}
}

func TestErrNotFound_UnwrappedInRoot(t *testing.T) {
	wrapped := New("lookup.Call").WithErr(ErrNotFound).WithMsg("no such callsign")
	if got := Root(wrapped); got != ErrNotFound {
		t.Fatalf("Root(wrapped) = %v, want ErrNotFound", got)
	}
}
