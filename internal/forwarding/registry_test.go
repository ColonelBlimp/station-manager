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

func (f *fakeFwd) Type() string { return f.typeName }
func (f *fakeFwd) Submit(context.Context, types.Qso, Action) Result {
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
