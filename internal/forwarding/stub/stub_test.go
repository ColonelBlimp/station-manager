package stub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// newFwd builds a stub with the given credentials object. Pass nil for
// "no credentials blob at all" (exercises the default-mode path).
func newFwd(t *testing.T, creds map[string]any) forwarding.Forwarder {
	t.Helper()
	fc := types.ForwarderConfig{Name: "stub-test", Type: Type}
	if creds != nil {
		raw, err := json.Marshal(creds)
		if err != nil {
			t.Fatalf("marshal creds: %v", err)
		}
		fc.Credentials = raw
	}
	fwd, err := New(fc)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return fwd
}

func TestInit_RegistersStubType(t *testing.T) {
	if !forwarding.IsRegistered(Type) {
		t.Fatalf("stub type %q not registered via init()", Type)
	}
}

func TestInit_RegistersDefaultRetry(t *testing.T) {
	got, ok := forwarding.DefaultRetryFor(Type)
	if !ok {
		t.Fatalf("stub type %q has no default retry registered via init()", Type)
	}
	if got != DefaultRetry {
		t.Fatalf("registered default = %+v, want %+v (should match exported DefaultRetry)",
			got, DefaultRetry)
	}
}

func TestNew_DefaultMode_IsAlwaysSuccess(t *testing.T) {
	fwd := newFwd(t, nil)
	if fwd.Type() != Type {
		t.Fatalf("Type = %q, want %q", fwd.Type(), Type)
	}
	res := fwd.Submit(context.Background(), types.Qso{}, "insert", "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success (default mode)", res.Outcome)
	}
}

func TestNew_EmptyModeDefaultsToSuccess(t *testing.T) {
	// Credentials blob present but mode field empty — should still default.
	fwd := newFwd(t, map[string]any{"mode": ""})
	res := fwd.Submit(context.Background(), types.Qso{}, "insert", "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", res.Outcome)
	}
}

func TestNew_UnknownMode_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{
		Name: "x", Type: Type,
		Credentials: json.RawMessage(`{"mode":"hovercraft"}`),
	})
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !strings.Contains(err.Error(), "unknown mode") {
		t.Fatalf("error = %q, want 'unknown mode' substring", err.Error())
	}
}

func TestNew_FlapN_WithoutCount_Errors(t *testing.T) {
	_, err := New(types.ForwarderConfig{
		Name: "x", Type: Type,
		Credentials: json.RawMessage(`{"mode":"flap_n"}`),
	})
	if err == nil {
		t.Fatal("expected error for flap_n with zero count")
	}
	if !strings.Contains(err.Error(), "flap_n must be") {
		t.Fatalf("error = %q, want flap_n guard", err.Error())
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

func TestSubmit_AlwaysSuccess(t *testing.T) {
	fwd := newFwd(t, map[string]any{"mode": ModeAlwaysSuccess})

	for i := 1; i <= 3; i++ {
		res := fwd.Submit(context.Background(), types.Qso{}, "insert", "")
		if res.Outcome != forwarding.OutcomeSuccess {
			t.Fatalf("iter %d: outcome = %q, want success", i, res.Outcome)
		}
		if res.UpstreamID == "" {
			t.Fatalf("iter %d: UpstreamID empty on success", i)
		}
		if res.Err != nil {
			t.Fatalf("iter %d: Err = %v on success, want nil", i, res.Err)
		}
	}

	if got := fwd.(*Forwarder).Calls(); got != 3 {
		t.Fatalf("Calls() = %d, want 3", got)
	}
}

func TestSubmit_AlwaysTransient(t *testing.T) {
	fwd := newFwd(t, map[string]any{"mode": ModeAlwaysTransient})
	res := fwd.Submit(context.Background(), types.Qso{}, "insert", "")

	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient", res.Outcome)
	}
	if res.Err == nil {
		t.Fatal("Err nil on transient outcome")
	}
}

func TestSubmit_AlwaysTerminal(t *testing.T) {
	fwd := newFwd(t, map[string]any{"mode": ModeAlwaysTerminal})
	res := fwd.Submit(context.Background(), types.Qso{}, "insert", "")

	if res.Outcome != forwarding.OutcomeTerminal {
		t.Fatalf("outcome = %q, want terminal", res.Outcome)
	}
	if res.Err == nil {
		t.Fatal("Err nil on terminal outcome")
	}
}

func TestSubmit_FlapN_TransientThenSuccess(t *testing.T) {
	fwd := newFwd(t, map[string]any{
		"mode":   ModeFlapN,
		"flap_n": 3,
	})

	// First 3 calls → transient.
	for i := 1; i <= 3; i++ {
		res := fwd.Submit(context.Background(), types.Qso{}, "insert", "")
		if res.Outcome != forwarding.OutcomeTransient {
			t.Fatalf("call %d: outcome = %q, want transient", i, res.Outcome)
		}
	}

	// 4th call → success (past the flap window).
	res := fwd.Submit(context.Background(), types.Qso{}, "insert", "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("call 4: outcome = %q, want success after flap", res.Outcome)
	}

	// 5th call → still success.
	res = fwd.Submit(context.Background(), types.Qso{}, "insert", "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("call 5: outcome = %q, want success after flap", res.Outcome)
	}
}

func TestSubmit_CtxCancelled_DoesNotConsumeCallSlot(t *testing.T) {
	fwd := newFwd(t, map[string]any{"mode": ModeAlwaysSuccess})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := fwd.Submit(ctx, types.Qso{}, "insert", "")
	if res.Outcome != forwarding.OutcomeTransient {
		t.Fatalf("outcome = %q, want transient on cancelled ctx", res.Outcome)
	}
	if res.Err == nil {
		t.Fatal("Err nil when ctx cancelled")
	}

	// Counter should still be 0 — ctx-cancel short-circuits before the
	// counter bumps, so test assertions stay clean.
	if got := fwd.(*Forwarder).Calls(); got != 0 {
		t.Fatalf("Calls() = %d, want 0 after ctx-cancelled Submit", got)
	}
}

func TestNew_ViaRegistryBuild(t *testing.T) {
	// Exercise the init() → forwarding.Build path, not just direct New().
	fwd, err := forwarding.Build(types.ForwarderConfig{
		Name: "stub-via-build", Type: Type,
		Credentials: json.RawMessage(`{"mode":"always_success"}`),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if fwd.Type() != Type {
		t.Fatalf("Type = %q, want %q", fwd.Type(), Type)
	}
}
