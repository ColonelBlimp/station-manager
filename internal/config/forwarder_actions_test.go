package config

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// These cover the per-forwarder supported-action wiring (review root fix):
// an omitted action_filter defaults to the forwarder type's SUPPORTED set,
// and an explicit filter naming an unsupported action is rejected at load.
// Each test registers under a unique type name because the forwarding
// registry is a process-global singleton.

func TestApplyDefaults_ActionFilter_UsesRegisteredSupportedSet(t *testing.T) {
	forwarding.RegisterSupportedActions("cfgacts-limited", []forwarding.Action{action.Insert, action.Delete})

	cfg := &Config{
		Forwarders: []types.ForwarderConfig{
			{Name: "x", Type: "cfgacts-limited"}, // ActionFilter omitted
		},
	}
	applyDefaults(cfg, t.TempDir())

	got := cfg.Forwarders[0].ActionFilter
	want := []string{"insert", "delete"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ActionFilter = %v, want %v (supported set)", got, want)
	}
}

func TestApplyDefaults_ActionFilter_FallsBackToAllThreeWhenUnregistered(t *testing.T) {
	cfg := &Config{
		Forwarders: []types.ForwarderConfig{
			{Name: "x", Type: "cfgacts-unregistered"}, // no supported set registered
		},
	}
	applyDefaults(cfg, t.TempDir())

	got := cfg.Forwarders[0].ActionFilter
	if len(got) != 3 {
		t.Fatalf("ActionFilter = %v, want all three actions for an unregistered type", got)
	}
}

func TestValidateForwarders_RejectsUnsupportedAction(t *testing.T) {
	forwarding.RegisterSupportedActions("cfgacts-noupdate", []forwarding.Action{action.Insert, action.Delete})

	err := validateForwarders([]types.ForwarderConfig{
		{Name: "cl", Type: "cfgacts-noupdate", ActionFilter: []string{"insert", "update"}},
	})
	if err == nil {
		t.Fatal("expected error for unsupported action 'update'")
	}
	if !strings.Contains(err.Error(), "does not support action") {
		t.Fatalf("err = %q, want 'does not support action'", err.Error())
	}
}

func TestValidateForwarders_AllowsSupportedActions(t *testing.T) {
	forwarding.RegisterSupportedActions("cfgacts-okset", []forwarding.Action{action.Insert, action.Delete})

	if err := validateForwarders([]types.ForwarderConfig{
		{Name: "cl", Type: "cfgacts-okset", ActionFilter: []string{"insert", "delete"}},
	}); err != nil {
		t.Fatalf("validateForwarders rejected a supported set: %v", err)
	}
}

func TestValidateForwarders_UnregisteredTypeNotGated(t *testing.T) {
	// A type with no registered supported set isn't validated against one —
	// any parseable action is accepted (historical behaviour preserved).
	if err := validateForwarders([]types.ForwarderConfig{
		{Name: "x", Type: "cfgacts-ungated", ActionFilter: []string{"insert", "update", "delete"}},
	}); err != nil {
		t.Fatalf("validateForwarders gated an unregistered type: %v", err)
	}
}
