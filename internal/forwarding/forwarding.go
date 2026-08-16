// Package forwarding defines the contract between the daemon's ingest and
// worker layers and the concrete destination plugins (QRZ, ClubLog, LoTW, ...).
//
// A Forwarder implementation pushes QSOs to one specific upstream. The
// worker layer (internal/forwarding/worker) calls Submit and classifies
// the outcome; the forwarder itself has no knowledge of the qso_upload
// row, the retry policy, or SSE emission.
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
	// OutcomeUnreachable — the host could not be reached AT ALL: no
	// response came back (DNS failure, connection refused, TLS handshake
	// failure, timeout, no route). The QSO is fine; only the link is down.
	// The worker retries INDEFINITELY — the row stays pending, backoff
	// saturates at the cap, and it is NEVER promoted to `failed` and NEVER
	// counts against MaxAttempts (ADR 0038). This is what makes
	// offline-first logging durable: a QSO logged during an outage uploads
	// whenever the link returns, an hour or ten days later. Return this
	// only when the HTTP client yielded no response; the moment a response
	// arrives (even an error status) the host is reachable — use
	// OutcomeTransient or OutcomeTerminal instead.
	OutcomeUnreachable Outcome = "unreachable"
	// OutcomeTransient — the host responded but cannot accept right now:
	// rate limits (429), request timeout (408), 5xx, or a mid-transfer
	// transport glitch after headers arrived. Worker re-queues per the
	// BOUNDED retry policy (§5) and promotes the row to `failed` once the
	// budget is exhausted. "Couldn't reach the host at all" is
	// OutcomeUnreachable, not this.
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
//
// Detail is an OPTIONAL machine-readable sub-outcome that distinguishes two
// results sharing one Outcome — e.g. SM Cloud's `stored` (applied) vs
// `cloud_newer_noop` (the cloud already held a newer copy), both Success. The
// worker logs it as `outcome_detail` on the attempt record when set; empty means
// "no finer detail". It never affects the row's lifecycle, only its trace.
type Result struct {
	Outcome    Outcome
	Err        error
	UpstreamID string
	Detail     string
}

// Forwarder is the plugin boundary between the worker layer and a concrete
// destination. Implementations make one synchronous network call per
// Submit and return a Result. They MUST NOT retry internally — retries
// are the worker's responsibility.
type Forwarder interface {
	// Type returns the plugin identifier, matching the "type" field in
	// config.forwarders[] and the key the implementation registered
	// under. Used for logging and for qso_upload.forwarder_type.
	Type() string

	// AdifPrefix returns the ADIF field-name prefix that the worker
	// stamps on the QSO row on successful 'submit' — e.g. "QRZCOM" for
	// QRZ (producing QRZCOM_QSO_UPLOAD_STATUS / QRZCOM_QSO_UPLOAD_DATE),
	// "CLUBLOG" for ClubLog, "LOTW" for LoTW. Return "" for forwarders
	// with no corresponding ADIF slot (custom webhooks, SM-private
	// destinations). When empty, the worker skips the QSO-row stamp
	// and only updates qso_upload.
	AdifPrefix() string

	// Submit attempts to push one QSO + action pair to the upstream
	// service. It MUST respect ctx cancellation.
	//
	// priorUpstreamID is the UpstreamID recorded on a prior successful
	// Submit for the same (QSO, forwarder) pair — populated by the
	// worker only for action=Delete, empty otherwise. Forwarders that
	// need the upstream's record id to issue a 'delete' (e.g. QRZ, which
	// takes LOGIDS) read it from here; forwarders that don't have this requirement
	// should ignore it.
	Submit(ctx context.Context, qso types.Qso, action Action, priorUpstreamID string) Result
}
