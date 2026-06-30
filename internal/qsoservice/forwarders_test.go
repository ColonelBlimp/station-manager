package qsoservice

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// shouldEnqueue's contract per ADR 0039 (supersedes ADR 0022's enqueue rule):
//   - `enabled` GATES enqueue — a disabled forwarder enqueues nothing (no more
//     "suspended" queued-but-not-uploaded state; ADR 0038's forever-retry
//     removed its only use case)
//   - action_filter further restricts which actions an enabled forwarder takes
//
// These tests defend the ADR-0039 semantics. The earlier 3B8IDX incident
// (disabled forwarder enqueued nothing, surprising on re-enable) is no longer a
// bug: under 0039 that's intended — re-uploading what was logged while disabled
// is a deliberate logbook-SPA backfill, not an automatic drain.

func TestShouldEnqueue_EnabledForwarderWithMatchingAction(t *testing.T) {
	fc := types.ForwarderConfig{
		Enabled:      true,
		ActionFilter: []string{"insert", "update"},
	}
	if !shouldEnqueue(fc, action.Insert) {
		t.Error("enabled forwarder with matching action: want true, got false")
	}
}

func TestShouldEnqueue_DisabledForwarderDoesNotEnqueue(t *testing.T) {
	// The load-bearing case per ADR 0039 — a disabled forwarder enqueues
	// nothing, even for actions its filter would otherwise accept. (Startup
	// reconciliation additionally discards any queued rows it still holds.)
	fc := types.ForwarderConfig{
		Enabled:      false,
		ActionFilter: []string{"insert", "update", "delete"},
	}
	for _, act := range []action.Action{action.Insert, action.Update, action.Delete} {
		if shouldEnqueue(fc, act) {
			t.Errorf("disabled forwarder, action=%s: want false (enabled gates enqueue), got true", act)
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
