package bridge

import (
	"testing"
	"time"
)

// drain collects whatever events are already buffered on ch without blocking,
// up to a short settle window. Used to inspect what subscribe() replayed.
func drain(ch <-chan Event) []Event {
	var out []Event
	deadline := time.After(50 * time.Millisecond)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, evt)
		case <-deadline:
			return out
		}
	}
}

func bridgeErr(code BridgeErrorCode) Event {
	return Event{Name: EventBridgeError, Payload: BridgeErrorPayload{Code: code}}
}

// TestBridgeErrorCode_IsTransient pins the transient/permanent split the hub's
// recovery-clearing relies on (review 2026-06-04 M1). The transient set must
// match the pipeline's exitTransient publishes; everything else (permanent
// config/rigdef faults + advisory identity warnings) stays cached.
func TestBridgeErrorCode_IsTransient(t *testing.T) {
	transient := []BridgeErrorCode{
		BridgeErrCodeSerialOpenFailed,
		BridgeErrCodeInitWriteFailed,
	}
	permanent := []BridgeErrorCode{
		BridgeErrCodeUnknownDriver,
		BridgeErrCodeSerialConfigInvalid,
		BridgeErrCodeMissingInit,
		BridgeErrCodeMissingRead,
		BridgeErrCodeIdentityUnrecognised,
		BridgeErrCodeIdentityMismatch,
	}
	for _, c := range transient {
		if !c.isTransient() {
			t.Errorf("isTransient(%q) = false, want true", c)
		}
	}
	for _, c := range permanent {
		if c.isTransient() {
			t.Errorf("isTransient(%q) = true, want false", c)
		}
	}
}

// TestHub_ClearsTransientBridgeErrorOnRigState covers M1: a transient
// bridge-error cached at first boot (rig off → serial_open_failed) must be
// dropped once the rig recovers and pushes state, so a SPA tab opening after
// recovery does NOT get the stale toast replayed.
func TestHub_ClearsTransientBridgeErrorOnRigState(t *testing.T) {
	h := newHub(nil)

	// First boot: rig off, supervisor publishes a transient open failure to
	// zero subscribers; it caches.
	h.publish(bridgeErr(BridgeErrCodeSerialOpenFailed))

	// A tab opening right now SHOULD still see it (recovery hasn't happened).
	ch, unsub := h.subscribe()
	if got := drain(ch); len(got) != 1 || got[0].Name != EventBridgeError {
		t.Fatalf("pre-recovery replay = %v, want one bridge-error", got)
	}
	unsub()

	// Rig recovers and pushes state — the cached transient error is now stale.
	h.publish(Event{Name: EventRigState, Payload: RigStatePayload{Mode: "USB"}})

	// A tab opening after recovery must get NO replayed bridge-error.
	ch2, unsub2 := h.subscribe()
	defer unsub2()
	for _, evt := range drain(ch2) {
		if evt.Name == EventBridgeError {
			t.Errorf("post-recovery subscriber got a stale bridge-error replay: %+v", evt.Payload)
		}
	}
}

// TestHub_KeepsPermanentBridgeErrorAcrossRigState is the complement: an
// operator-actionable bridge-error (e.g. an identity-unrecognised advisory,
// which keeps reading state so a rig-state follows it) must NOT be cleared by a
// rig-state — it stays cached so every late subscriber still sees it. (A
// definite identity mismatch is also a bridge-error but halts the pipeline, so
// no rig-state follows it; this test models the advisory-then-rig-state flow.)
func TestHub_KeepsPermanentBridgeErrorAcrossRigState(t *testing.T) {
	h := newHub(nil)
	h.publish(bridgeErr(BridgeErrCodeIdentityMismatch))
	h.publish(Event{Name: EventRigState, Payload: RigStatePayload{Mode: "USB"}})

	ch, unsub := h.subscribe()
	defer unsub()
	var sawErr bool
	for _, evt := range drain(ch) {
		if evt.Name == EventBridgeError {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("permanent bridge-error was cleared by a rig-state; want it retained for late subscribers")
	}
}
