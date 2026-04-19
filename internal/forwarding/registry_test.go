package forwarding

import (
	"context"
	stderr "errors"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// fakeFwd is a minimal Forwarder for registry tests. Tests use unique
// type names so they don't collide with each other (the registry is a
// package-global singleton).
type fakeFwd struct{ typeName string }

func (f *fakeFwd) Type() string       { return f.typeName }
func (f *fakeFwd) AdifPrefix() string { return "" }
func (f *fakeFwd) Submit(context.Context, types.Qso, Action, string) Result {
	return Result{Outcome: OutcomeSuccess}
}

func TestRegister_And_Build(t *testing.T) {
	Register("registertest-ok", func(fc types.ForwarderConfig) (Forwarder, error) {
		return &fakeFwd{typeName: fc.Type}, nil
	})

	if !IsRegistered("registertest-ok") {
		t.Fatal("IsRegistered returned false after Register")
	}

	fwd, err := Build(types.ForwarderConfig{Name: "x", Type: "registertest-ok"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if fwd.Type() != "registertest-ok" {
		t.Fatalf("Type = %q, want registertest-ok", fwd.Type())
	}
}

func TestBuild_UnknownType(t *testing.T) {
	_, err := Build(types.ForwarderConfig{Name: "x", Type: "does-not-exist-ever"})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "unknown forwarder type") {
		t.Fatalf("error = %q, want 'unknown forwarder type'", err.Error())
	}
}

func TestBuild_ConstructorError(t *testing.T) {
	sentinel := stderr.New("bad credentials")
	Register("registertest-ctor-err", func(fc types.ForwarderConfig) (Forwarder, error) {
		return nil, sentinel
	})

	_, err := Build(types.ForwarderConfig{Name: "x", Type: "registertest-ctor-err"})
	if err == nil {
		t.Fatal("expected error from constructor")
	}
	if !stderr.Is(err, sentinel) {
		t.Fatalf("error does not wrap sentinel: %v", err)
	}
}

func TestRegister_PanicsOnEmptyType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with empty type did not panic")
		}
	}()
	Register("", func(types.ForwarderConfig) (Forwarder, error) { return nil, nil })
}

func TestRegister_PanicsOnNilConstructor(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with nil constructor did not panic")
		}
	}()
	Register("registertest-nil", nil)
}

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	Register("registertest-dup", func(types.ForwarderConfig) (Forwarder, error) {
		return &fakeFwd{typeName: "registertest-dup"}, nil
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("duplicate Register did not panic")
		}
	}()
	Register("registertest-dup", func(types.ForwarderConfig) (Forwarder, error) {
		return &fakeFwd{typeName: "registertest-dup"}, nil
	})
}

func TestIsRegistered_UnknownType(t *testing.T) {
	if IsRegistered("registertest-never-used") {
		t.Fatal("IsRegistered returned true for unregistered type")
	}
}

// ---- RegisterDefaultRetry / DefaultRetryFor (stage 7) ----

func validRetry() types.RetryConfig {
	return types.RetryConfig{
		MaxAttempts:       3,
		InitialBackoffSec: 10,
		MaxBackoffSec:     60,
	}
}

func TestRegisterDefaultRetry_And_Lookup(t *testing.T) {
	RegisterDefaultRetry("retrytest-ok", validRetry())

	got, ok := DefaultRetryFor("retrytest-ok")
	if !ok {
		t.Fatal("DefaultRetryFor returned ok=false for registered type")
	}
	want := validRetry()
	if got != want {
		t.Fatalf("DefaultRetryFor = %+v, want %+v", got, want)
	}
}

func TestDefaultRetryFor_UnknownType(t *testing.T) {
	if _, ok := DefaultRetryFor("retrytest-never-used"); ok {
		t.Fatal("DefaultRetryFor returned ok=true for unregistered type")
	}
}

func TestRegisterDefaultRetry_PanicsOnEmptyType(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("empty type did not panic")
		}
	}()
	RegisterDefaultRetry("", validRetry())
}

func TestRegisterDefaultRetry_PanicsOnDuplicate(t *testing.T) {
	RegisterDefaultRetry("retrytest-dup", validRetry())

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("duplicate did not panic")
		}
	}()
	RegisterDefaultRetry("retrytest-dup", validRetry())
}

func TestRegisterDefaultRetry_PanicsOnInvalidConfig(t *testing.T) {
	cases := []struct {
		name  string
		cfg   types.RetryConfig
		panic string
	}{
		{
			"MaxAttempts < 1",
			types.RetryConfig{MaxAttempts: 0, InitialBackoffSec: 10, MaxBackoffSec: 60},
			"MaxAttempts",
		},
		{
			"InitialBackoff < 1",
			types.RetryConfig{MaxAttempts: 3, InitialBackoffSec: 0, MaxBackoffSec: 60},
			"InitialBackoffSec",
		},
		{
			"MaxBackoff < InitialBackoff",
			types.RetryConfig{MaxAttempts: 3, InitialBackoffSec: 60, MaxBackoffSec: 30},
			"MaxBackoffSec",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("%s: did not panic", tc.name)
				}
			}()
			// Use a unique name each loop so tests don't collide.
			RegisterDefaultRetry("retrytest-bad-"+tc.name, tc.cfg)
		})
	}
}
