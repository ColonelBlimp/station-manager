package bridge

// ServiceName is the DI bean ID for the bridge service.
const ServiceName = "bridgeservice"

// EventName identifies the kind of event flowing from the bridge to
// SSE subscribers. Three values per ADR 0010, no others — extending
// this set is a wire-protocol change that requires updating the SPA's
// EventSource consumer in `frontend/logging/src/lib/states/bridge.svelte.ts`
// and revising ADR 0010.
type EventName string

const (
	// EventRigState carries CAT-relevant rig fields the SPA's
	// catState mirrors. On the initial event after a new SSE
	// connection, the bridge emits a full snapshot (built from a
	// CAT poll command — ADR 0019, no persistent cache); on
	// subsequent events the bridge forwards whatever fields the
	// rig push produced. The SPA's merge logic is identical for
	// both cases (field-by-field overwrite into catState).
	EventRigState EventName = "rig-state"

	// EventRigDisconnected fires when the bridge concludes the rig
	// is no longer alive — CAT identity check fails at startup, no
	// data has flowed for the timeout window (ADR 0010 passive
	// liveness), or the serial port returns EIO/closed. Payload is
	// {Reason string} for the SPA to surface as a toast.
	EventRigDisconnected EventName = "rig-disconnected"

	// EventBridgeError surfaces operator-actionable bridge-side
	// errors (port permission denied, rig identification failed,
	// baud-rate mismatch, etc.). NOT used for transient retries or
	// per-frame protocol hiccups. Payload is {Message string}.
	EventBridgeError EventName = "bridge-error"
)

// Event is one bridge → SSE-subscriber message. Name is the SSE
// event-type field; Payload is JSON-marshalled by the handler into
// the SSE data: line.
//
// Payload is intentionally untyped (any) at this layer — each
// EventName has its own conventional payload shape (see RigStatePayload,
// RigDisconnectedPayload, BridgeErrorPayload below). The handler
// marshals whatever the publisher provides; consumers branch on the
// SSE event-type field, then unmarshal into the matching shape.
type Event struct {
	Name    EventName
	Payload any
}

// RigStatePayload is the shape under EventRigState. Fields mirror
// catState's CAT-relevant subset per ADR 0010. Any field omitted
// from a given event leaves the SPA's catState entry untouched
// (Svelte 5 $state proxy retains prior values on partial assignment).
//
// Frequency in Hz; mode/subMode follow ADIF naming; power in raw rig
// watts (the SPA applies the amp multiplier when it derives
// effectivePower per ADR 0009).
//
// SplitOverride is *bool rather than bool so the wire can distinguish
// "rig pushed split=OFF" (`splitOverride: false`) from "rig didn't
// push split this frame" (field omitted). The plain-bool form
// collapses the OFF case into the omitted case via `omitempty`,
// which would silently drop a legitimate state change. Other fields
// (VfoA, VfoB, Power) stay non-pointer because 0 is never a
// legitimate rig value for them — `omitempty` correctly treats 0 as
// "not pushed."
type RigStatePayload struct {
	RigIdentity   string `json:"rigIdentity,omitempty"`
	VfoA          int64  `json:"vfoA,omitempty"`
	VfoB          int64  `json:"vfoB,omitempty"`
	Mode          string `json:"mode,omitempty"`
	SubMode       string `json:"subMode,omitempty"`
	SelectedVfo   string `json:"selectedVfo,omitempty"`
	SplitOverride *bool  `json:"splitOverride,omitempty"`
	Power         int    `json:"power,omitempty"`
}

// RigDisconnectedPayload carries the human-readable reason the bridge
// concluded the rig is gone. SPA toasts it via ADR 0008.
type RigDisconnectedPayload struct {
	Reason string `json:"reason"`
}

// BridgeErrorPayload carries an operator-actionable error message.
// SPA toasts it via ADR 0008.
type BridgeErrorPayload struct {
	Message string `json:"message"`
}
