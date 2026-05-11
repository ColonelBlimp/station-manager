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

// RigDisconnectedCode names a category of "the rig stopped talking"
// the daemon publishes. The SPA's i18n catalogue keys the localized
// toast template off this code and substitutes the per-instance
// values from Details (e.g. {"error": "i/o timeout"}).
//
// Daemon log lines stay in technical English (operator-debugging
// audience); operator-facing toast wording lives in the SPA
// catalogue (operator-using-the-app audience). Two different
// audiences, two different ownership lanes — keeps both layers
// independently tunable, and gives the SPA a clean i18n hook for
// future localizations (Tumbuka, Chichewa).
type RigDisconnectedCode string

const (
	// RigCodeNoData — pipeline read timed out (rig went quiet for
	// the liveness window; typically the rig is powered off or
	// disconnected mid-flight).
	RigCodeNoData RigDisconnectedCode = "rig_no_data"
	// RigCodeSerialError — terminal serial-port error (ErrClosed,
	// EIO, cable yank). Details: {"error": "<go err string>"}.
	RigCodeSerialError RigDisconnectedCode = "serial_port_error"
)

// BridgeErrorCode names a category of "the bridge can't start /
// can't talk to this rig" condition. Same i18n flow as
// RigDisconnectedCode: SPA catalogue keys off the code, substitutes
// per-instance values from Details.
type BridgeErrorCode string

const (
	// BridgeErrCodeUnknownDriver — `bridge.cat.driver` doesn't match
	// any rigdef. Details: {"driver": "<configured value>"}.
	BridgeErrCodeUnknownDriver BridgeErrorCode = "unknown_driver"
	// BridgeErrCodeSerialConfigInvalid — rigdef serial-config block
	// couldn't be translated to a serial.Config. Details:
	// {"error": "<reason>"}.
	BridgeErrCodeSerialConfigInvalid BridgeErrorCode = "serial_config_invalid"
	// BridgeErrCodeMissingInit — rigdef has no INIT command, so we
	// can't arm AUTO push state. Details: {"driver": "<id>"}.
	BridgeErrCodeMissingInit BridgeErrorCode = "missing_init_command"
	// BridgeErrCodeMissingRead — rigdef has no READ command, so we
	// can't bootstrap on SSE-open. Details: {"driver": "<id>"}.
	BridgeErrCodeMissingRead BridgeErrorCode = "missing_read_command"
	// BridgeErrCodeSerialOpenFailed — serial.Open returned an error
	// (permission, device-not-found, etc.). Details: {"port", "error"}.
	BridgeErrCodeSerialOpenFailed BridgeErrorCode = "serial_open_failed"
	// BridgeErrCodeInitWriteFailed — INIT byte-write failed. Details:
	// {"driver", "error"}.
	BridgeErrCodeInitWriteFailed BridgeErrorCode = "init_write_failed"
	// BridgeErrCodeIdentityUnrecognised — rig responded with an ID
	// code the configured rigdef doesn't map. Details: {"driver"}.
	BridgeErrCodeIdentityUnrecognised BridgeErrorCode = "identity_unrecognised"
	// BridgeErrCodeIdentityMismatch — rig identified as a different
	// model than the configured driver expects. Details: {"driver",
	// "expected", "actual"}.
	BridgeErrCodeIdentityMismatch BridgeErrorCode = "identity_mismatch"
)

// RigDisconnectedPayload carries the disconnect category + key/value
// substitution details. The SPA looks up `bridge.disconnected.<Code>`
// in its i18n catalogue and substitutes named placeholders from
// Details (e.g. template "Lost the serial connection ({error})" +
// details {"error": "i/o timeout"} → "Lost the serial connection
// (i/o timeout)").
//
// Wire shape evolved (was {Reason string}) so the SPA can own
// operator-facing wording centrally + future-proof i18n. Coordinated
// upgrade with the SPA's bridge.svelte.ts consumer.
type RigDisconnectedPayload struct {
	Code    RigDisconnectedCode `json:"code"`
	Details map[string]string   `json:"details,omitempty"`
}

// BridgeErrorPayload carries an operator-actionable error code +
// substitution details. Same i18n flow as RigDisconnectedPayload.
type BridgeErrorPayload struct {
	Code    BridgeErrorCode   `json:"code"`
	Details map[string]string `json:"details,omitempty"`
}
