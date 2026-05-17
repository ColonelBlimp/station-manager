package qsoservice

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// shouldEnqueue's contract per ADR 0022:
//   - presence in config.json (i.e. reaching this function at all) is
//     what gates enqueue
//   - Enabled is a worker-lifecycle signal, NOT an enqueue gate
//   - action_filter is the only field shouldEnqueue itself filters on
//
// These tests defend the ADR-0022 semantics against any future
// well-meaning "but disabled forwarders shouldn't enqueue, right?"
// regression.

func TestShouldEnqueue_EnabledForwarderWithMatchingAction(t *testing.T) {
	fc := types.ForwarderConfig{
		Enabled:      true,
		ActionFilter: []string{"insert", "update"},
	}
	if !shouldEnqueue(fc, action.Insert) {
		t.Error("enabled forwarder with matching action: want true, got false")
	}
}

func TestShouldEnqueue_DisabledForwarderStillEnqueues(t *testing.T) {
	// The load-bearing case per ADR 0022 — disabled forwarders MUST
	// still enqueue so rows accumulate for the worker to drain after
	// re-enable + restart. Pre-ADR-0022 this returned false and was
	// the root cause of the 3B8IDX dogfood incident.
	fc := types.ForwarderConfig{
		Enabled:      false,
		ActionFilter: []string{"insert", "update", "delete"},
	}
	for _, act := range []action.Action{action.Insert, action.Update, action.Delete} {
		if !shouldEnqueue(fc, act) {
			t.Errorf("disabled forwarder, action=%s: want true (rows accumulate), got false", act)
		}
	}
}

func TestShouldEnqueue_ActionFilterExcludes(t *testing.T) {
	// action_filter is the only field shouldEnqueue itself filters on.
	// A forwarder that doesn't list the action in its filter must not
	// enqueue, regardless of Enabled.
	cases := []struct {
		name    string
		enabled bool
	}{
		{"enabled", true},
		{"disabled", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fc := types.ForwarderConfig{
				Enabled:      c.enabled,
				ActionFilter: []string{"insert"}, // no "delete"
			}
			if shouldEnqueue(fc, action.Delete) {
				t.Error("action_filter excludes 'delete': want false, got true")
			}
		})
	}
}

func TestShouldEnqueue_EmptyActionFilter(t *testing.T) {
	// Empty action_filter means "this destination cares about no
	// actions" — nothing should enqueue. Distinct from "missing config
	// entry" (handled by the caller's range loop, not by this function).
	fc := types.ForwarderConfig{
		Enabled:      true,
		ActionFilter: []string{},
	}
	if shouldEnqueue(fc, action.Insert) {
		t.Error("empty action_filter: want false, got true")
	}
}
