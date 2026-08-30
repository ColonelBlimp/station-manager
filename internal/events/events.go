package events

// ServiceName is the DI bean ID under which the daemon's *Hub is
// registered (via iocdi.RegisterInstance). Consumers inject the
// hub with `di.inject:"eventhub"`.
const ServiceName = "eventhub"

// Event names. Use these constants when calling Hub.Publish so the
// emit sites and the SSE handler agree on the wire vocabulary.
// Vocabulary is settled in docs/v2-design/api.md §4.5.
const (
	NameQsoStored        = "qso.stored"
	NameQsoUpdated       = "qso.updated"
	NameQsoDeleted       = "qso.deleted"
	NameForwardSucceeded = "forward.succeeded"
	NameForwardFailed    = "forward.failed"
)

// Event is the envelope delivered to every subscriber.
//
// ID is a per-hub monotonic counter assigned at Publish time. It is
// useful for debug-tracing and client-side dedup, but is NOT a
// resume cursor — the hub keeps no backlog, so a client cannot
// "rewind" to a past ID.
//
// Payload is an event-name-specific struct (QsoStoredPayload,
// ForwardSucceededPayload, etc.). The SSE handler marshals it to
// JSON; in-process consumers can type-assert on it directly.
type Event struct {
	ID      int64
	Name    string
	Payload any
}

// QsoStoredPayload is the payload for NameQsoStored.
// Minimal shape — clients re-query for QSO details if they need them.
//
// QsoUUID is the canonical QSO identifier (AW-1). QsoID is the daemon-local row PK,
// DEPRECATED and retained only through v2.0.0-alpha.2 for one release; it is removed in
// v2.0.0-alpha.3. New consumers key on qso_uuid. LogbookID stays numeric (the public
// logbook key).
type QsoStoredPayload struct {
	QsoUUID   string `json:"qso_uuid"`
	QsoID     int64  `json:"qso_id"` // DEPRECATED (removed v2.0.0-alpha.3); use qso_uuid
	LogbookID int64  `json:"logbook_id"`
}

// QsoUpdatedPayload is the payload for NameQsoUpdated. See QsoStoredPayload for the
// qso_uuid/qso_id deprecation contract.
type QsoUpdatedPayload struct {
	QsoUUID   string `json:"qso_uuid"`
	QsoID     int64  `json:"qso_id"` // DEPRECATED (removed v2.0.0-alpha.3); use qso_uuid
	LogbookID int64  `json:"logbook_id"`
}

// QsoDeletedPayload is the payload for NameQsoDeleted. See QsoStoredPayload for the
// qso_uuid/qso_id deprecation contract.
type QsoDeletedPayload struct {
	QsoUUID   string `json:"qso_uuid"`
	QsoID     int64  `json:"qso_id"` // DEPRECATED (removed v2.0.0-alpha.3); use qso_uuid
	LogbookID int64  `json:"logbook_id"`
}

// ForwardSucceededPayload is the payload for NameForwardSucceeded,
// emitted when the worker marks a qso_upload row as succeeded. Action
// is "insert", "update", or "delete" (from internal/enums/upload/action).
// UpstreamID is the remote service's identifier for the stored record
// (e.g. QRZ LOGID). Empty for forwarders that don't produce one.
type ForwardSucceededPayload struct {
	QsoUUID       string `json:"qso_uuid"`
	QsoID         int64  `json:"qso_id"` // DEPRECATED (removed v2.0.0-alpha.3); use qso_uuid
	ForwarderName string `json:"forwarder_name"`
	Action        string `json:"action"`
	UpstreamID    string `json:"upstream_id,omitempty"`
	Attempts      int    `json:"attempts"`
}

// ForwardFailedPayload is the payload for NameForwardFailed, emitted
// when the worker gives up on a qso_upload row (terminal outcome or
// retries exhausted). Reason is a short human-readable summary; the
// full error history lives in qso_upload.last_error.
type ForwardFailedPayload struct {
	QsoUUID       string `json:"qso_uuid"`
	QsoID         int64  `json:"qso_id"` // DEPRECATED (removed v2.0.0-alpha.3); use qso_uuid
	ForwarderName string `json:"forwarder_name"`
	Action        string `json:"action"`
	Attempts      int    `json:"attempts"`
	Reason        string `json:"reason"`
}
