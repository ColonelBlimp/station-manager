package qrz

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// validCreds returns a minimally-valid credentials blob. Tests that
// need to perturb a single field copy this and mutate.
func validCreds(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"callsign": "M0TEST",
		"api_key":  "ABCD-1234-EFGH-5678",
	})
	if err != nil {
		t.Fatalf("marshal creds: %v", err)
	}
	return raw
}

func TestInit_RegistersQrzType(t *testing.T) {
	if !forwarding.IsRegistered(Type) {
		t.Fatalf("qrz type %q not registered via init()", Type)
	}
}

func TestNew_Valid_ReturnsForwarder(t *testing.T) {
	fwd, err := New(types.ForwarderConfig{
		Name: "qrz-test", Type: Type,
		Credentials: validCreds(t),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if fwd.Type() != Type {
		t.Fatalf("Type = %q, want %q", fwd.Type(), Type)
	}
	if fwd.AdifPrefix() != AdifFieldPrefix {
		t.Fatalf("AdifPrefix = %q, want %q", fwd.AdifPrefix(), AdifFieldPrefix)
	}
}

func TestNew_MissingCredentials_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{Name: "x", Type: Type})
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
	if !strings.Contains(err.Error(), "credentials required") {
		t.Fatalf("error = %q, want 'credentials required' substring", err.Error())
	}
}

func TestNew_MalformedCredentials_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{
		Name: "x", Type: Type,
		Credentials: json.RawMessage(`not json`),
	})
	if err == nil {
		t.Fatal("expected error for malformed credentials JSON")
	}
}

func TestNew_MissingCallsign_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{
		Name: "x", Type: Type,
		Credentials: json.RawMessage(`{"api_key":"ABCD-1234"}`),
	})
	if err == nil {
		t.Fatal("expected error for missing callsign")
	}
	if !strings.Contains(err.Error(), "callsign") {
		t.Fatalf("error = %q, want 'callsign' substring", err.Error())
	}
}

func TestNew_MissingApiKey_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{
		Name: "x", Type: Type,
		Credentials: json.RawMessage(`{"callsign":"M0TEST"}`),
	})
	if err == nil {
		t.Fatal("expected error for missing api_key")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("error = %q, want 'api_key' substring", err.Error())
	}
}

func TestNew_EmptyCallsign_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{
		Name: "x", Type: Type,
		Credentials: json.RawMessage(`{"callsign":"","api_key":"ABCD-1234"}`),
	})
	if err == nil {
		t.Fatal("expected error for empty callsign")
	}
}

func TestNew_EmptyApiKey_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{
		Name: "x", Type: Type,
		Credentials: json.RawMessage(`{"callsign":"M0TEST","api_key":""}`),
	})
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
}

func TestNew_ViaRegistryBuild(t *testing.T) {
	// Exercise the init() → forwarding.Build path.
	fwd, err := forwarding.Build(types.ForwarderConfig{
		Name: "qrz-via-build", Type: Type,
		Credentials: validCreds(t),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if fwd.Type() != Type {
		t.Fatalf("Type = %q, want %q", fwd.Type(), Type)
	}
	if fwd.AdifPrefix() != AdifFieldPrefix {
		t.Fatalf("AdifPrefix = %q, want %q", fwd.AdifPrefix(), AdifFieldPrefix)
	}
}

func TestSubmit_CtxCancelled_IsTransient(t *testing.T) {
	fwd, err := New(types.ForwarderConfig{
		Name: "x", Type: Type,
		Credentials: validCreds(t),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := fwd.Submit(ctx, types.Qso{}, "insert", "")
	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on cancelled ctx", res.Outcome)
	}
	if res.Err == nil {
		t.Fatal("Err nil when ctx cancelled")
	}
}

func TestSubmit_Stubbed_ReturnsTerminal(t *testing.T) {
	// Until stage 4 lands the real HTTP call, Submit returns terminal
	// with a "not yet implemented" error. This guard catches anyone
	// who wires up a real QRZ forwarder in config before stage 4.
	fwd, err := New(types.ForwarderConfig{
		Name: "x", Type: Type,
		Credentials: validCreds(t),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res := fwd.Submit(context.Background(), types.Qso{}, "insert", "")
	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal (stub)", res.Outcome)
	}
	if res.Err == nil {
		t.Fatal("Err nil on stubbed Submit")
	}
	if !strings.Contains(res.Err.Error(), "not yet implemented") {
		t.Fatalf("err = %q, want 'not yet implemented' substring", res.Err.Error())
	}
}
