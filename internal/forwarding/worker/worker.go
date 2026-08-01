// Package worker implements the per-forwarder goroutine that drains the
// qso_upload queue for one destination. Each Worker owns its
// destination's rows exclusively (scoped by forwarder_name) and runs a
// ticker + claim + submit + persist-outcome loop.
//
// Design rationale: docs/v2-design/forwarding.md §4 (topology), §5
// (retry policy), §7 (row lifecycle). Launched by cmd/smd's
// spawnForwarderWorkers under safego.Go.
package worker

import (
	"context"
	stderr "errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Config is the per-worker runtime configuration. Resolved by the
// caller (cmd/smd startup) from a types.ForwarderConfig plus the
// forwarder package's own retry defaults, so Retry is always
// fully populated here.
type Config struct {
	// Name is the per-instance forwarder handle that scopes the claim
	// query. Matches types.ForwarderConfig.Name and qso_upload.forwarder_name.
	Name string

	// Tick is the period between claim attempts. Translates the
	// operator's tick_interval_sec into a Duration.
	Tick time.Duration

	// Batch caps the rows claimed per tick. See §4 on the tradeoff —
	// larger batches concentrate network-time variance into a single
	// tick and slow graceful shutdown.
	Batch int

	// Retry bounds the retry loop. All three fields must be populated;
	// the caller is responsible for merging the operator override (if
	// any) with the forwarder package's defaults.
	Retry types.RetryConfig

	// OnQsoStamped, when non-nil, is invoked after a successful post-upload
	// ADIF stamp (markSuccess's stamping branch) with the stamped QSO's row
	// id. The stamp bumps the row's revision, so row-mirror forwarders (SM
	// Cloud) need the row re-enqueued or their copy drifts until the hourly
	// reconcile heals it via a full-manifest diff; cmd/smd wires this to
	// qsoservice.EnqueueStampSync. Best-effort — the hook's outcome never
	// affects the upload row's own lifecycle, and a forwarder that stamps
	// nothing (including the mirror itself) never fires it.
	OnQsoStamped func(ctx context.Context, qsoID int64)
}

// Worker drains the pending qso_upload queue for one destination.
// Safe to run under safego.Go with respawn=true. Each row is processed
// under a per-row recover boundary (processRowSafely): a panic mid-row
// resets THAT row to a retryable state and the worker keeps draining the
// rest of the batch, so a panic no longer strands the already-claimed
// in_progress row until the next daemon restart. (The daemon-startup
// orphan reset remains the backstop for a genuine process crash.)
type Worker struct {
	cfg    Config
	fwd    forwarding.Forwarder
	db     *sqlite.Service
	logger *logging.Service
	hub    *events.Hub
}

// New constructs a Worker from its fully resolved Config and
// dependencies. Returns an error if the config is obviously broken —
// missing name, non-positive tick / batch, or incomplete retry bounds.
// Type-specific forwarder validation already happened in the forwarder's
// own constructor.
func New(cfg Config, fwd forwarding.Forwarder, db *sqlite.Service, logger *logging.Service, hub *events.Hub) (*Worker, error) {
	const op errors.Op = "forwarding/worker.New"

	if cfg.Name == "" {
		return nil, errors.New(op).WithMsg("cfg.Name is empty")
	}
	if cfg.Tick <= 0 {
		return nil, errors.New(op).WithMsgf("cfg.Tick must be > 0, got %s", cfg.Tick)
	}
	if cfg.Batch < 1 {
		return nil, errors.New(op).WithMsgf("cfg.Batch must be >= 1, got %d", cfg.Batch)
	}
	if cfg.Retry.MaxAttempts < 1 {
		return nil, errors.New(op).WithMsgf("cfg.Retry.MaxAttempts must be >= 1, got %d", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.InitialBackoffSec < 1 {
		return nil, errors.New(op).WithMsgf("cfg.Retry.InitialBackoffSec must be >= 1, got %d", cfg.Retry.InitialBackoffSec)
	}
	if cfg.Retry.MaxBackoffSec < cfg.Retry.InitialBackoffSec {
		return nil, errors.New(op).WithMsg("cfg.Retry.MaxBackoffSec must be >= InitialBackoffSec")
	}
	if fwd == nil {
		return nil, errors.New(op).WithMsg("fwd is nil")
	}
	if db == nil {
		return nil, errors.New(op).WithMsg("db is nil")
	}
	if logger == nil {
		return nil, errors.New(op).WithMsg("logger is nil")
	}
	if hub == nil {
		return nil, errors.New(op).WithMsg("hub is nil")
	}

	return &Worker{cfg: cfg, fwd: fwd, db: db, logger: logger, hub: hub}, nil
}

// Name returns the forwarder_name this worker is scoped to.
func (w *Worker) Name() string { return w.cfg.Name }

// Run drives the claim + submit + persist loop until ctx is cancelled.
// One initial tick runs immediately, so rows already pending at startup
// are picked up without waiting a full tick period; subsequent ticks
// fire on the ticker.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.cfg.Tick)
	defer t.Stop()

	w.tickOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tickOnce(ctx)
		}
	}
}

// tickOnce claims up to cfg.Batch rows and processes each one.
// A single failed claim is logged, and the tick is abandoned; the next
// tick will try again. Claim failures in this daemon's single-writer
// sqlite should be rare outside of disk issues.
func (w *Worker) tickOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	rows, err := w.db.ClaimPendingUploadsWithContext(ctx, w.cfg.Name, w.cfg.Batch)
	if err != nil {
		// Suppress the common ctx-cancellation case — shutdown is noise,
		// not an operational failure worth an error log.
		if ctx.Err() == nil {
			w.logger.ErrorWith().
				Str("forwarder", w.cfg.Name).
				Err(err).
				Msg("forwarder: claim failed")
		}
		return
	}
	if len(rows) == 0 {
		return
	}

	w.logger.DebugWith().
		Str("forwarder", w.cfg.Name).
		Int("count", len(rows)).
		Msg("forwarder: rows claimed")

	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		w.processRowSafely(ctx, row)
	}
}

// processRowSafely runs processRow under a per-row recover boundary. A panic
// while processing one row (a forwarder bug, a malformed QSO, a runtime fault)
// is recovered here so the worker keeps draining the rest of the batch AND the
// already-claimed in_progress row is reset to a retryable state — rather than
// unwinding the whole Run loop and stranding that row at in_progress until the
// next daemon restart (review 2026-06-05 M1). The reset routes through
// markTransientInternal, so it respects the retry budget + backoff and emits
// the usual events; a row that panics on every attempt exhausts its budget and
// is marked failed rather than looping forever.
func (w *Worker) processRowSafely(ctx context.Context, row types.QsoUpload) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		w.logger.ErrorWith().
			Str("forwarder", w.cfg.Name).
			Int64("upload_id", row.ID).
			Int64("qso_id", row.QsoID).
			Str("action", row.Action).
			Str("panic", fmt.Sprintf("%v", r)).
			Str("stack", string(debug.Stack())).
			Msg("forwarder: panic processing row; resetting to retry")
		// During shutdown, skip the DB write — the row stays in_progress and
		// the next startup's orphan reset reclaims it (the crash-recovery path
		// still applies when we're actually stopping).
		if ctx.Err() != nil {
			return
		}
		w.markTransientInternal(ctx, row, fmt.Errorf("panic: %v", r))
	}()
	w.processRow(ctx, row)
}

// processRow fetches the QSO, dispatches to the forwarder, and persists
// the outcome. Soft-delete handling (§4): insert/update → skip as
// terminal; delete → fetch-including-deleted and forward.
func (w *Worker) processRow(ctx context.Context, row types.QsoUpload) {
	// Parse once at the top; the typed value threads through both
	// fetchQsoForAction's soft-delete switch and resolvePriorUpstreamID's
	// delete-only branch. This also resolves "unknown action" here,
	// so fetchQsoForAction doesn't need to handle it again.
	act, err := action.Parse(row.Action)
	if err != nil {
		// Row has an action string we don't recognise. Terminal — retrying
		// won't help (the row is bad data on disk). Should be unreachable:
		// ingest only writes known action values. Structured warn so an
		// operator notices if the unreachable becomes reachable.
		w.logger.WarnWith().
			Str("forwarder", w.cfg.Name).
			Int64("upload_id", row.ID).
			Str("action", row.Action).
			Msg("forwarder: row carries unknown action string")
		_ = w.markFailed(ctx, row, fmt.Sprintf("unknown action %q", row.Action))
		return
	}

	qso, handled := w.fetchQsoForAction(ctx, row, act)
	if handled {
		return
	}

	priorUpstreamID, handled := w.resolvePriorUpstreamID(ctx, row, act)
	if handled {
		return
	}

	call := qso.ContactedStation.Call

	// DEBUG, not Info: this is the prospective breadcrumb for an attempt that may
	// never return (a hung upstream, a daemon killed mid-upload). It is worth
	// having, but not at the default level — with the outcome record below it made
	// this package 41% of every line in smd.log while saying nothing the outcome
	// does not. Demoted rather than deleted: deleting saves nothing further at the
	// default level and loses the trace entirely when it is wanted.
	w.logger.DebugWith().
		Str("forwarder", w.cfg.Name).
		Int64("qso_id", row.QsoID).
		Str("action", row.Action).
		Str("call", call).
		Msg("forwarding: submit")

	// Timed around Forwarder.Submit ALONE. The queue write and the stamp-sync hook
	// that follow are deliberately outside it: the one question this number exists
	// to answer is whether the UPSTREAM is slow, and folding local work in would
	// make a slow disk look like a slow API. (It identifies a slow upstream; it
	// does not positively identify a slow local write — that needs a second
	// duration this does not add.)
	start := time.Now()
	res := w.fwd.Submit(ctx, qso, act, priorUpstreamID)
	submitDur := time.Since(start)

	w.persistOutcome(ctx, row, call, res, submitDur)
}

// resolvePriorUpstreamID fetches the upstream_id recorded on the prior
// successful upstream-creating action (insert OR update) for delete actions,
// so a forwarder that identifies the remote record by id (QRZ's LOGIDS) can
// issue the delete. For insert/update it returns an empty string without
// touching the DB.
//
// An EMPTY result is passed through, NOT failed: some upstreams (ClubLog)
// delete by QSO fields and never stored a record id, so a missing id is
// normal for them. Whether a missing id is fatal is the forwarder's call —
// its Submit returns Terminal when it needs an id it didn't get (e.g.
// qrz.buildForm). The worker no longer gates on presence here; doing so
// made id-less-delete forwarders unreachable (review finding, High).
//
// Returns handled=true only when a DB infra error was already resolved
// (transient retry), so the caller skips the 'submit' step. The DB lookup
// error is the only worker-level outcome — everything else, empty id
// included, flows to Submit.
func (w *Worker) resolvePriorUpstreamID(
	ctx context.Context, row types.QsoUpload, act action.Action,
) (string, bool) {
	if act != action.Delete {
		return "", false
	}

	upstreamID, err := w.db.FetchPriorUpstreamIDWithContext(
		ctx, row.QsoID, w.cfg.Name,
	)
	if err != nil {
		// Infra error (DB down, ctx cancel). Retry — the row isn't
		// structurally broken, we just couldn't look it up right now.
		w.markTransientInternal(ctx, row, err)
		return "", true
	}
	// Empty id is intentionally passed through to Submit — the forwarder
	// decides whether it can proceed without one. See the doc comment.
	return upstreamID, false
}

// fetchQsoForAction retrieves the QSO for forwarding. Returns handled=true
// when the row has already been resolved (e.g. soft-delete on
// insert/update → marked failed in-band) so the caller can skip the
// 'submit' step. The normal case returns (qso, false).
//
// act is already parsed by processRow — every action reaching here
// is one of Insert/Update/Delete, so no unknown-action branch is
// needed.
func (w *Worker) fetchQsoForAction(ctx context.Context, row types.QsoUpload, act action.Action) (types.Qso, bool) {
	qso, err := w.db.FetchQsoByIdWithContext(ctx, row.QsoID)
	if err == nil {
		return qso, false
	}
	if !stderr.Is(err, errors.ErrNotFound) {
		// Infrastructure error (DB down, ctx cancel during a query, etc.).
		// Treat as transient so the row is requeued rather than stranded
		// in_progress.
		w.markTransientInternal(ctx, row, err)
		return types.Qso{}, true
	}

	// QSO absent or soft-deleted. §4 semantics:
	switch act {
	case action.Insert:
		_ = w.markFailed(ctx, row, "qso soft-deleted before insert forwarded")
		return types.Qso{}, true

	case action.Update:
		_ = w.markFailed(ctx, row, "qso soft-deleted; delete row supersedes")
		return types.Qso{}, true

	case action.Delete:
		// Delete must still forward — the upstream needs telling. Fetch
		// including soft-deleted rows so the forwarder has CALL/DATE/TIME
		// to identify the record to remove upstream.
		qso, err = w.db.FetchQsoByIDIncludingDeletedWithContext(ctx, row.QsoID)
		if err == nil {
			return qso, false
		}
		if stderr.Is(err, errors.ErrNotFound) {
			// Not even a soft-deleted row exists. The qso_upload row points
			// at a nonexistent QSO — ingest should never produce this, so
			// mark terminal and move on.
			_ = w.markFailed(ctx, row, "qso not found for delete forwarding")
			return types.Qso{}, true
		}
		w.markTransientInternal(ctx, row, err)
		return types.Qso{}, true
	}

	// action.Parse accepts only insert/update/delete, so an unknown act
	// reaching the switch is unreachable in practice. Belt-and-braces:
	// terminal so we don't loop. Structured warn so an operator
	// notices if the unreachable becomes reachable.
	w.logger.WarnWith().
		Str("forwarder", w.cfg.Name).
		Int64("upload_id", row.ID).
		Str("action", act.String()).
		Msg("forwarder: switch reached unreachable action")
	_ = w.markFailed(ctx, row, fmt.Sprintf("unreachable action %q", act))
	return types.Qso{}, true
}

// persistOutcome translates a forwarder.Result into the appropriate
// queue-row transition. Terminal transitions (uploaded / failed)
// publish the corresponding forward.* event on the hub after the
// DB write succeeds — markSuccess and markFailed own that emit.
//
// call is the contacted-station callsign for log lines; persistOutcome
// itself doesn't otherwise touch the QSO row.
func (w *Worker) persistOutcome(
	ctx context.Context, row types.QsoUpload, call string, res forwarding.Result, submitDur time.Duration,
) {
	// Both halves of the result, resolved before anything is written to the log.
	//
	// The ORDER is the fix: the success line used to be written BEFORE
	// markSuccess ran, so an upstream-accepted row whose queue write then failed —
	// or was re-armed by a concurrent edit and will therefore be SENT AGAIN — was
	// indistinguishable in the log from a completed upload. Persist first, then
	// report what actually happened to both the upstream and the local row.
	var (
		outcome = string(res.Outcome)
		disp    disposition
		cause   error
		attempt = &attemptFields{}
		stamped bool
	)

	switch res.Outcome {
	case forwarding.OutcomeSuccess:
		attempt.upstreamID = res.UpstreamID
		disp, stamped = w.markSuccess(ctx, row, res.UpstreamID)

	case forwarding.OutcomeTerminal:
		// The Forwarder contract sets Result.Err on a non-success outcome, but
		// don't trust it blindly — a nil here would store an empty last_error
		// and emit an empty forward.failed reason (review 2026-06-05 L2).
		cause = nonNilErr(res.Err, "forwarder reported terminal outcome without an error")
		disp = w.markFailed(ctx, row, cause.Error())

	case forwarding.OutcomeTransient:
		cause = nonNilErr(res.Err, "forwarder reported transient outcome without an error")
		outcome, disp = w.markTransientFromForwarder(ctx, row, cause, attempt)

	case forwarding.OutcomeUnreachable:
		cause = nonNilErr(res.Err, "forwarder reported unreachable outcome without an error")
		disp = w.markUnreachable(ctx, row, cause, attempt)

	default:
		// Unknown outcome from the forwarder — treat as terminal so we
		// don't spin on it. Forwarder authors should only return the
		// three documented outcomes. Structured warn so a misbehaving
		// forwarder (typically a bug in a new plugin) surfaces in logs
		// rather than just in last_error text.
		w.logger.WarnWith().
			Str("forwarder", w.cfg.Name).
			Int64("upload_id", row.ID).
			Str("outcome", string(res.Outcome)).
			Msg("forwarder: returned unrecognised Outcome")
		cause = nonNilErr(res.Err, "forwarder reported an unrecognised outcome")
		disp = w.markFailed(ctx, row, fmt.Sprintf("unknown outcome %q: %s", res.Outcome, errText(res.Err)))
	}

	w.logAttempt(row, call, outcome, disp, cause, submitDur, attempt)

	// Strictly AFTER the record. The stamp bumped the QSO's revision, so the
	// row-mirror forwarder(s) must be told or their copy drifts until the hourly
	// reconcile heals it with a full-manifest diff. But the hook is best-effort and
	// fallible, and the upload has already persisted — so its trace is written
	// first, and a hook that blocks or panics can no longer erase it.
	if stamped && w.cfg.OnQsoStamped != nil {
		w.cfg.OnQsoStamped(ctx, row.QsoID)
	}
}

// attemptFields carries the per-outcome extras onto the attempt record without
// widening every mark* signature: retry bookkeeping for the transient paths, the
// upstream id for success. Zero values are omitted from the record.
type attemptFields struct {
	upstreamID string
	attempts   int64
	delay      time.Duration
}

// disposition is what happened to the QUEUE ROW locally, independent of what the
// upstream said. The two are orthogonal and both are needed: an upload the
// upstream accepted can still be re-armed or fail to persist, and reporting only
// the upstream half is what made a will-be-sent-again row look completed.
type disposition string

const (
	// dispPersisted — the queue transition committed.
	dispPersisted disposition = "persisted"
	// dispPersistFailed — the upstream call happened, the local write did not.
	dispPersistFailed disposition = "persist_failed"
	// dispRearmed — a concurrent operator edit re-armed the claimed row, so the
	// transition never committed and the row WILL be submitted again. Not a
	// failure, and deliberately not conflated with one.
	dispRearmed disposition = "rearmed"
)

// logAttempt emits the single default-visible attempt-result record, after the
// local disposition is known.
//
// Severity is chosen from BOTH halves: a persistence failure is an Error however
// the upstream answered, an exhausted budget or a terminal rejection is a Warn,
// and an ordinary success or scheduled retry is Info. Reporting everything at one
// level would bury the outcomes that need attention among the ~98% that do not.
func (w *Worker) logAttempt(
	row types.QsoUpload, call, outcome string, disp disposition,
	cause error, submitDur time.Duration, extra *attemptFields,
) {
	var ev logging.LogEvent
	switch {
	case disp == dispPersistFailed:
		ev = w.logger.ErrorWith()
	case outcome == outcomeExhausted || outcome == string(forwarding.OutcomeTerminal):
		ev = w.logger.WarnWith()
	default:
		ev = w.logger.InfoWith()
	}

	ev = ev.
		Str("forwarder", w.cfg.Name).
		Int64("qso_id", row.QsoID).
		Str("action", row.Action).
		Str("call", call).
		Str("outcome", outcome).
		Str("disposition", string(disp)).
		// What CAUSED this row to be queued. Without it the two highest-volume
		// lines in the daemon cannot explain their own volume: live logging, a
		// backfill, a stamp-sync mirror and a reconcile repair are otherwise
		// identical records (docs/reviews/forwarding-logging-gaps.md F1).
		Str("origin", row.Origin).
		Int64("submit_duration_ms", submitDur.Milliseconds())

	if extra != nil {
		if extra.upstreamID != "" {
			ev = ev.Str("upstream_id", extra.upstreamID)
		}
		if extra.attempts > 0 {
			ev = ev.Int64("attempts", extra.attempts)
		}
		if extra.delay > 0 {
			ev = ev.Dur("retry_in", extra.delay)
		}
	}
	if cause != nil {
		ev = ev.Err(cause)
	}
	ev.Msg("forwarding: attempt")
}

// outcomeExhausted is the attempt-record outcome for a transient failure that
// consumed the last retry. Distinct from `terminal` (an upstream rejection) and
// from `transient` (one that will be retried) because the operator action
// differs: exhausted means SM gave up on a row it could have kept trying.
const outcomeExhausted = "exhausted"

// markTransientFromForwarder records a transient forwarder outcome, promoting to
// 'failed' when the retry budget is exhausted. Returns the attempt-record outcome
// (transient vs exhausted — the operator action differs) and the local
// disposition; the caller emits the single record.
func (w *Worker) markTransientFromForwarder(
	ctx context.Context, row types.QsoUpload, cause error, extra *attemptFields,
) (string, disposition) {
	nextAttempts := row.Attempts + 1
	extra.attempts = nextAttempts
	if nextAttempts >= int64(w.cfg.Retry.MaxAttempts) {
		return outcomeExhausted, w.markFailed(ctx, row, errText(cause))
	}
	delay := computeBackoff(nextAttempts, w.cfg.Retry)
	extra.delay = delay
	nextAt := time.Now().Add(delay).Unix()
	return string(forwarding.OutcomeTransient), w.markTransientRetry(ctx, row, nextAt, errText(cause))
}

// markUnreachable records a connectivity failure: the upstream host could
// not be reached at all (the forwarder returned OutcomeUnreachable). Unlike
// a transient outcome it does NOT consume the retry budget and is NEVER
// promoted to `failed` — the row goes back to `pending` and is retried
// indefinitely, with backoff saturating at MaxBackoffSec, so a QSO logged
// during an outage uploads whenever the link returns (ADR 0038). attempts
// still increments (useful diagnostics, and it drives backoff toward the
// cap) but never triggers give-up. `failed` stays reserved for host-up
// rejections, which keeps it a clean "needs operator attention" signal.
func (w *Worker) markUnreachable(
	ctx context.Context, row types.QsoUpload, cause error, extra *attemptFields,
) disposition {
	nextAttempts := row.Attempts + 1
	delay := computeBackoff(nextAttempts, w.cfg.Retry)
	extra.attempts, extra.delay = nextAttempts, delay
	nextAt := time.Now().Add(delay).Unix()
	return w.markTransientRetry(ctx, row, nextAt, errText(cause))
}

// markTransientInternal records a transient outcome caused by an
// internal failure (DB fetch error, etc.) rather than the forwarder.
// Shares the same retry-budget logic as markTransientFromForwarder so
// a chronic internal problem doesn't keep a row cycling forever.
func (w *Worker) markTransientInternal(ctx context.Context, row types.QsoUpload, cause error) {
	nextAttempts := row.Attempts + 1
	if nextAttempts >= int64(w.cfg.Retry.MaxAttempts) {
		// Disposition discarded: this path emits no attempt record (Forwarder.Submit
		// was never reached). Its own missing logging is forwarding F5, not this diff.
		_ = w.markFailed(ctx, row, "internal: "+errText(cause))
		return
	}
	delay := computeBackoff(nextAttempts, w.cfg.Retry)
	nextAt := time.Now().Add(delay).Unix()
	_ = w.markTransientRetry(ctx, row, nextAt, "internal: "+errText(cause))
}

// reArmed reports whether a completion write was a no-op because a concurrent
// operator edit re-armed the claimed row (in_progress → pending). It is not a
// failure: the caller must NOT publish a terminal event or fire the stamp hook,
// because the transition never committed (review 2026-07-20 internal/forwarding
// #4). Logged at debug for traceability, not as an error.
func (w *Worker) reArmed(err error, uploadID int64, completion string) bool {
	if !stderr.Is(err, errors.ErrUploadReArmed) {
		return false
	}
	w.logger.DebugWith().
		Str("forwarder", w.cfg.Name).
		Int64("upload_id", uploadID).
		Str("completion", completion).
		Msg("forwarder: completion skipped — row re-armed by a concurrent edit")
	return true
}

func (w *Worker) markTransientRetry(
	ctx context.Context, row types.QsoUpload, nextAt int64, lastErr string,
) disposition {
	err := w.db.MarkUploadTransientRetryWithContext(ctx, row.ID, nextAt, lastErr)
	if w.reArmed(err, row.ID, "transient-retry") {
		return dispRearmed
	}
	if err != nil {
		w.logger.ErrorWith().
			Str("forwarder", w.cfg.Name).
			Int64("upload_id", row.ID).
			Err(err).
			Msg("forwarder: mark transient retry failed")
		return dispPersistFailed
	}
	return dispPersisted
}

func (w *Worker) markFailed(ctx context.Context, row types.QsoUpload, lastErr string) disposition {
	err := w.db.MarkUploadFailedWithContext(ctx, row.ID, lastErr)
	if w.reArmed(err, row.ID, "failed") {
		return dispRearmed
	}
	if err != nil {
		w.logger.ErrorWith().
			Str("forwarder", w.cfg.Name).
			Int64("upload_id", row.ID).
			Err(err).
			Msg("forwarder: mark failed failed")
		return dispPersistFailed
	}
	w.hub.Publish(events.NameForwardFailed, events.ForwardFailedPayload{
		QsoID:         row.QsoID,
		ForwarderName: w.cfg.Name,
		Action:        row.Action,
		Attempts:      int(row.Attempts) + 1,
		Reason:        lastErr,
	})
	return dispPersisted
}

// markSuccess persists a success outcome, dispatching between the
// plain and ADIF-stamping variants based on the forwarder's
// declarative metadata and the row's action.
//
// Rules (stage 6):
//   - delete → plain variant always; a soft-deleted local QSO should
//     not be stamped as "uploaded".
//   - forwarder has no ADIF prefix (stub, custom webhooks) → plain
//     variant; there is no ADIF slot to stamp.
//   - otherwise → MarkUploadSuccessWithAdifStamp, which updates
//     qso_upload AND the QSO row's <PREFIX>_QSO_UPLOAD_{STATUS,DATE}
//     in one transaction, honouring the one-fails-all-fail invariant.
//
// The bool reports whether an ADIF stamp COMMITTED, so the caller can fire the
// best-effort stamp-sync hook AFTER the attempt record is written. The hook must
// not run inside here: it is fallible (it can block or panic), and with the
// attempt record now emitted after markSuccess returns, a hook failure would
// erase the only trace of an upload that already persisted — which is exactly
// what the pre-restructure ordering could not do (clean-room review of f31738bc).
func (w *Worker) markSuccess(ctx context.Context, row types.QsoUpload, upstreamID string) (disposition, bool) {
	stamped := false
	prefix := w.fwd.AdifPrefix()
	if prefix != "" && row.Action != action.Delete.String() {
		err := w.db.MarkUploadSuccessWithAdifStampWithContext(
			ctx, row.ID, upstreamID, row.QsoID, prefix,
		)
		if w.reArmed(err, row.ID, "success+stamp") {
			return dispRearmed, false // no stamp committed → no mirror hook, no succeeded event
		}
		if err != nil {
			w.logger.ErrorWith().
				Str("forwarder", w.cfg.Name).
				Int64("upload_id", row.ID).
				Int64("qso_id", row.QsoID).
				Str("adif_prefix", prefix).
				Err(err).
				Msg("forwarder: mark success + adif stamp failed")
			return dispPersistFailed, false
		}
		// The stamp bumped the QSO row's revision, so the row-mirror forwarder(s)
		// need telling — but that call is the caller's to make, after logging.
		stamped = true
	} else {
		err := w.db.MarkUploadSuccessWithContext(ctx, row.ID, upstreamID)
		if w.reArmed(err, row.ID, "success") {
			return dispRearmed, false
		}
		if err != nil {
			w.logger.ErrorWith().
				Str("forwarder", w.cfg.Name).
				Int64("upload_id", row.ID).
				Err(err).
				Msg("forwarder: mark success failed")
			return dispPersistFailed, false
		}
	}

	w.hub.Publish(events.NameForwardSucceeded, events.ForwardSucceededPayload{
		QsoID:         row.QsoID,
		ForwarderName: w.cfg.Name,
		Action:        row.Action,
		UpstreamID:    upstreamID,
		Attempts:      int(row.Attempts) + 1,
	})
	return dispPersisted, stamped
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// nonNilErr guarantees a non-nil error for a non-success forwarder outcome.
// The Forwarder contract sets Result.Err whenever Outcome != Success, but a
// buggy or future forwarder could break that; without this the row's
// last_error and the forward.failed SSE reason would be empty, silently hiding
// the failure (review 2026-06-05 L2).
func nonNilErr(err error, fallback string) error {
	if err == nil {
		return stderr.New(fallback)
	}
	return err
}
