// Package forwarding defines the contract between the daemon's ingest and
// worker layers and the concrete destination plugins (QRZ, ClubLog, LoTW, ...).
//
// A Forwarder implementation pushes QSOs to one specific upstream. The
// worker layer (internal/forwarding/worker, landing in stage 8) calls
// Submit and classifies the outcome; the forwarder itself has no knowledge
// of the qso_upload row, the retry policy, or SSE emission.
//
// See docs/v2-design/forwarding.md §3 for the design rationale.
package forwarding

import (
	"context"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Action mirrors the upload queue's action enum — one of
// action.Insert / action.Update / action.Delete. Re-exported as an alias
// so forwarder packages don't need to import enums/upload/action directly.
type Action = action.Action

// Outcome classifies the result of a Submit call. The forwarder — not the
// worker — is responsible for distinguishing "try again later" from
// "don't bother retrying" since only it knows the quirks of its upstream.
type Outcome string

const (
	// OutcomeSuccess — upstream accepted.
	OutcomeSuccess Outcome = "success"
	// OutcomeTransient — try again later. Network errors, rate limits,
	// temporary upstream outages. Worker re-queues per retry policy (§5).
	OutcomeTransient Outcome = "transient"
	// OutcomeTerminal — upstream definitively rejected and retrying will
	// not help. Malformed data, revoked credentials, dedupe rejection,
	// etc. Worker marks the row `failed` immediately.
	OutcomeTerminal Outcome = "terminal"
)

// Result is what Forwarder.Submit returns. Err is set when Outcome is not
// Success; it is stored in qso_upload.last_error for diagnostics.
// UpstreamID is optional and, when populated on success, is stored in
// qso_upload.upstream_id, so the later operator UI can link out to the remote
// record.
type Result struct {
	Outcome    Outcome
	Err        error
	UpstreamID string
}

// Forwarder is the plugin boundary between the worker layer and a concrete
// destination. Implementations make one synchronous network call per
// Submit and return a Result. They MUST NOT retry internally — retries
// are the worker's job.
type Forwarder interface {
	// Type returns the plugin identifier, matching the "type" field in
	// config.forwarders[] and the key the implementation registered
	// under. Used for logging and for qso_upload.forwarder_type.
	Type() string

	// Submit attempts to push one QSO + action pair to the upstream
	// service. It MUST respect ctx cancellation.
	Submit(ctx context.Context, qso types.Qso, action Action) Result
}
