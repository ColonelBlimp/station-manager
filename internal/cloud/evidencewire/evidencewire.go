// Package evidencewire is the §5 evidence-sync wire contract, shared by the
// SMD-side sync client (internal/evidence) and the SMC-side ingest
// (internal/cloud). It is deliberately one package: the content digest is
// identity (§5.2 — digest identity is (tenant, kind, uuid) over versioned
// canonical immutable content, operator ruling 2026-08-10), so a client and
// server that drifted on canonicalization would corrupt data, not merely
// disagree. Stdlib-only so either side can import it freely.
//
// The outcome vocabulary carries all six §5.1 outcomes from day one;
// tombstoned and suppressed are emitted only once §8 deletion/opt-out
// machinery exists. The retention record kind's tag is reserved here and
// joins with the retention slice (§4.1 sequencing amendment, 2026-08-10).
package evidencewire

import "encoding/json"

// Record kinds (§5.1). One envelope, uniform rules for every kind.
const (
	KindObservation  = "observation"
	KindCoverage     = "coverage"
	KindLossInterval = "loss_interval"
	KindProfile      = "profile"
	// KindRetention is RESERVED: the wire tag exists so the retention slice
	// adds a kind, not a protocol. Servers reject it until then.
	KindRetention = "retention"
)

// Per-row outcomes (§5.1). The first four are terminal; the client marks the
// row synced and never re-offers it. RetryableMissingProfile re-offers the
// referenced LOCAL profile even if previously marked synced (selection
// priority, operator ruling 2026-08-10). PermanentReject quarantines
// locally with its reason.
const (
	OutcomeAccepted                = "accepted"
	OutcomeAlreadyPresent          = "already_present"
	OutcomeTombstoned              = "tombstoned"
	OutcomeSuppressed              = "suppressed"
	OutcomeRetryableMissingProfile = "retryable_missing_profile"
	OutcomePermanentReject         = "permanent_reject"
)

// DigestVersion1 is the canonical-content digest version this build writes.
// A change to canonicalization is a NEW version, never a redefinition.
const DigestVersion1 = 1

// Record is one row in a sync batch. Identity is the envelope's
// (kind, uuid) under the authenticated tenant; Payload is the immutable
// content, stored verbatim by SMC (replay-complete, §5.3 amendment).
type Record struct {
	Kind    string          `json:"kind"`
	UUID    string          `json:"uuid"`
	DigestV int             `json:"digest_v"`
	Digest  string          `json:"digest"`
	Payload json.RawMessage `json:"payload"`
}

// PutRequest is the batch envelope for PUT /v1/evidence.
type PutRequest struct {
	Records []Record `json:"records"`
}

// RowOutcome is one row's answer. Reason accompanies permanent_reject only.
type RowOutcome struct {
	UUID    string `json:"uuid"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

// PutResponse carries exactly one outcome per submitted record. A response
// that does not is invalid and consumes no rows (operator ruling
// 2026-08-10).
type PutResponse struct {
	Outcomes []RowOutcome `json:"outcomes"`
}
