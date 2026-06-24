package forwarding

import (
	"context"
	stderr "errors"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
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

// ---- RegisterSupportedActions / SupportedActionsFor ----

func TestRegisterSupportedActions_And_Lookup(t *testing.T) {
	RegisterSupportedActions("actstest-ok", []Action{action.Insert, action.Delete})

	got, ok := SupportedActionsFor("actstest-ok")
	if !ok {
		t.Fatal("SupportedActionsFor returned ok=false for registered type")
	}
	want := []Action{action.Insert, action.Delete}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SupportedActionsFor = %v, want %v", got, want)
	}

	// Returned slice is a copy — mutating it must not corrupt the registry.
	got[0] = action.Update
	again, _ := SupportedActionsFor("actstest-ok")
	if again[0] != action.Insert {
		t.Fatalf("registry mutated via returned slice: got %v", again)
	}
}

func TestSupportedActionsFor_UnknownType(t *testing.T) {
	if _, ok := SupportedActionsFor("actstest-never-used"); ok {
		t.Fatal("SupportedActionsFor returned ok=true for unregistered type")
	}
}

func TestRegisterSupportedActions_Panics(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"empty type", func() { RegisterSupportedActions("", []Action{action.Insert}) }},
		{"empty set", func() { RegisterSupportedActions("actstest-empty", nil) }},
		{"unknown action", func() { RegisterSupportedActions("actstest-unknown", []Action{"hovercraft"}) }},
		{"duplicate action", func() {
			RegisterSupportedActions("actstest-dupact", []Action{action.Insert, action.Insert})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("%s: did not panic", tc.name)
				}
			}()
			tc.fn()
		})
	}
}

func TestRegisterSupportedActions_PanicsOnDuplicateRegistration(t *testing.T) {
	RegisterSupportedActions("actstest-dupreg", []Action{action.Insert})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("duplicate registration did not panic")
		}
	}()
	RegisterSupportedActions("actstest-dupreg", []Action{action.Insert})
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

// ---- RegisterForwarderType / ForwarderTypes ----

func TestRegisterForwarderType_And_ForwarderTypes(t *testing.T) {
	RegisterForwarderType("fwdtype-ok", "OK Forwarder",
		[]Action{action.Insert, action.Delete},
		[]CredentialField{{Key: "api", Label: "API", Kind: "password", Help: "h"}})

	// The supported-action set is recorded too (delegated to the shared path), so
	// the action_filter defaulting / validation keep working for this type.
	got, ok := SupportedActionsFor("fwdtype-ok")
	if !ok || len(got) != 2 {
		t.Fatalf("SupportedActionsFor(fwdtype-ok) = %v,%v; want a 2-action set", got, ok)
	}

	types := ForwarderTypes()
	for i := 1; i < len(types); i++ {
		if types[i-1].Type > types[i].Type {
			t.Fatalf("ForwarderTypes not sorted by type: %q before %q", types[i-1].Type, types[i].Type)
		}
	}
	var d *TypeDescriptor
	for i := range types {
		if types[i].Type == "fwdtype-ok" {
			d = &types[i]
		}
	}
	if d == nil {
		t.Fatal("ForwarderTypes did not include fwdtype-ok")
	}
	if d.DisplayName != "OK Forwarder" {
		t.Fatalf("DisplayName = %q, want OK Forwarder", d.DisplayName)
	}
	if len(d.SupportedActions) != 2 {
		t.Fatalf("SupportedActions = %v, want 2", d.SupportedActions)
	}
	if len(d.CredentialFields) != 1 || d.CredentialFields[0].Key != "api" ||
		d.CredentialFields[0].Kind != "password" {
		t.Fatalf("CredentialFields = %+v, want one password field 'api'", d.CredentialFields)
	}

	// Returned slices are copies — mutating them can't corrupt the registry.
	d.CredentialFields[0].Key = "mutated"
	if again := ForwarderTypes(); func() bool {
		for _, x := range again {
			if x.Type == "fwdtype-ok" {
				return x.CredentialFields[0].Key != "api"
			}
		}
		return true
	}() {
		t.Fatal("ForwarderTypes did not return a defensive copy of credential fields")
	}
}

func TestRegisterForwarderType_Panics(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"empty display name", func() {
			RegisterForwarderType("ftpanic-nodisp", "", []Action{action.Insert}, nil)
		}},
		{"bad credential kind", func() {
			RegisterForwarderType("ftpanic-kind", "X", []Action{action.Insert},
				[]CredentialField{{Key: "k", Label: "K", Kind: "secret"}})
		}},
		{"empty credential key", func() {
			RegisterForwarderType("ftpanic-key", "X", []Action{action.Insert},
				[]CredentialField{{Key: "", Label: "K", Kind: "text"}})
		}},
		{"duplicate credential key", func() {
			RegisterForwarderType("ftpanic-dupkey", "X", []Action{action.Insert},
				[]CredentialField{{Key: "k", Label: "A", Kind: "text"}, {Key: "k", Label: "B", Kind: "text"}})
		}},
		{"empty type (delegated)", func() {
			RegisterForwarderType("", "X", []Action{action.Insert}, nil)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s: expected panic", c.name)
				}
			}()
			c.fn()
		})
	}
}
