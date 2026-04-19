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
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
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
}

// Worker drains the pending qso_upload queue for one destination.
// Safe to run under safego.Go with respawn=true — Run is idempotent on
// restart (orphaned in_progress rows are reset at daemon startup, so a
// panic mid-processRow doesn't leave any hidden state).
type Worker struct {
	cfg    Config
	fwd    forwarding.Forwarder
	db     *sqlite.Service
	logger *logging.Service
}

// New constructs a Worker from its fully resolved Config and
// dependencies. Returns an error if the config is obviously broken —
// missing name, non-positive tick / batch, or incomplete retry bounds.
// Type-specific forwarder validation already happened in the forwarder's
// own constructor.
func New(cfg Config, fwd forwarding.Forwarder, db *sqlite.Service, logger *logging.Service) (*Worker, error) {
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

	return &Worker{cfg: cfg, fwd: fwd, db: db, logger: logger}, nil
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
		w.processRow(ctx, row)
	}
}

// processRow fetches the QSO, dispatches to the forwarder, and persists
// the outcome. Soft-delete handling (§4): insert/update → skip as
// terminal; delete → fetch-including-deleted and forward.
func (w *Worker) processRow(ctx context.Context, row types.QsoUpload) {
	qso, handled := w.fetchQsoForAction(ctx, row)
	if handled {
		return
	}

	act, err := action.Parse(row.Action)
	if err != nil {
		// Row has an action string we don't recognise. Terminal — retrying
		// won't help (the row is bad data on disk). Should be unreachable:
		// ingest only writes known action values.
		w.markFailed(ctx, row, fmt.Sprintf("unknown action %q", row.Action))
		return
	}

	priorUpstreamID, handled := w.resolvePriorUpstreamID(ctx, row, act)
	if handled {
		return
	}

	res := w.fwd.Submit(ctx, qso, act, priorUpstreamID)
	w.persistOutcome(ctx, row, res)
}

// resolvePriorUpstreamID fetches the upstream_id recorded on the prior
// successful insert for delete actions, so the forwarder can identify
// the record to remove upstream. For insert/update it returns an empty
// string without touching the DB.
//
// Returns handled=true when the row has already been resolved (DB
// infra error → transient, no matching insert → terminal) so the
// caller skips the 'submit' step.
func (w *Worker) resolvePriorUpstreamID(
	ctx context.Context, row types.QsoUpload, act action.Action,
) (string, bool) {
	if act != action.Delete {
		return "", false
	}

	upstreamID, err := w.db.FetchInsertUpstreamIDWithContext(
		ctx, row.QsoID, w.cfg.Name,
	)
	if err != nil {
		// Infra error (DB down, ctx cancel). Retry — the row isn't
		// structurally broken, we just couldn't look it up right now.
		w.markTransientInternal(ctx, row, err)
		return "", true
	}
	if upstreamID == "" {
		// No prior successful insert for this (qso, forwarder). The
		// delete can never resolve — mark terminal per the stage-5
		// design decision (rather than looping on an impossible
		// operation).
		w.markFailed(ctx, row,
			"no upstream id for delete — no successful insert found")
		return "", true
	}
	return upstreamID, false
}

// fetchQsoForAction retrieves the QSO for forwarding. Returns handled=true
// when the row has already been resolved (e.g. soft-delete on
// insert/update → marked failed in-band) so the caller can skip the
// 'submit' step. The normal case returns (qso, false).
func (w *Worker) fetchQsoForAction(ctx context.Context, row types.QsoUpload) (types.Qso, bool) {
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
	switch row.Action {
	case action.Insert.String():
		w.markFailed(ctx, row, "qso soft-deleted before insert forwarded")
		return types.Qso{}, true

	case action.Update.String():
		w.markFailed(ctx, row, "qso soft-deleted; delete row supersedes")
		return types.Qso{}, true

	case action.Delete.String():
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
			w.markFailed(ctx, row, "qso not found for delete forwarding")
			return types.Qso{}, true
		}
		w.markTransientInternal(ctx, row, err)
		return types.Qso{}, true
	}

	// Unknown action — marking transient would loop. Terminal is safer.
	w.markFailed(ctx, row, fmt.Sprintf("unknown action %q", row.Action))
	return types.Qso{}, true
}

// persistOutcome translates a forwarder.Result into the appropriate
// queue-row transition. Terminal transitions (uploaded / failed) will
// become SSE emit sites when the event stream lands (§7).
func (w *Worker) persistOutcome(ctx context.Context, row types.QsoUpload, res forwarding.Result) {
	switch res.Outcome {
	case forwarding.OutcomeSuccess:
		w.markSuccess(ctx, row, res.UpstreamID)

	case forwarding.OutcomeTerminal:
		w.markFailed(ctx, row, errText(res.Err))

	case forwarding.OutcomeTransient:
		w.markTransientFromForwarder(ctx, row, res.Err)

	default:
		// Unknown outcome from the forwarder — treat as terminal so we
		// don't spin on it. Forwarder authors should only return the
		// three documented outcomes.
		w.markFailed(ctx, row, fmt.Sprintf("unknown outcome %q: %s", res.Outcome, errText(res.Err)))
	}
}

// markTransientFromForwarder records a transient forwarder outcome,
// promoting to 'failed' when the retry budget is exhausted.
func (w *Worker) markTransientFromForwarder(ctx context.Context, row types.QsoUpload, cause error) {
	nextAttempts := row.Attempts + 1
	if nextAttempts >= int64(w.cfg.Retry.MaxAttempts) {
		w.markFailed(ctx, row, errText(cause))
		return
	}
	delay := computeBackoff(nextAttempts, w.cfg.Retry)
	nextAt := time.Now().Add(delay).Unix()
	w.markTransientRetry(ctx, row, nextAt, errText(cause))
}

// markTransientInternal records a transient outcome caused by an
// internal failure (DB fetch error, etc.) rather than the forwarder.
// Shares the same retry-budget logic as markTransientFromForwarder so
// a chronic internal problem doesn't keep a row cycling forever.
func (w *Worker) markTransientInternal(ctx context.Context, row types.QsoUpload, cause error) {
	nextAttempts := row.Attempts + 1
	if nextAttempts >= int64(w.cfg.Retry.MaxAttempts) {
		w.markFailed(ctx, row, "internal: "+errText(cause))
		return
	}
	delay := computeBackoff(nextAttempts, w.cfg.Retry)
	nextAt := time.Now().Add(delay).Unix()
	w.markTransientRetry(ctx, row, nextAt, "internal: "+errText(cause))
}

func (w *Worker) markTransientRetry(ctx context.Context, row types.QsoUpload, nextAt int64, lastErr string) {
	if err := w.db.MarkUploadTransientRetryWithContext(ctx, row.ID, nextAt, lastErr); err != nil {
		w.logger.ErrorWith().
			Str("forwarder", w.cfg.Name).
			Int64("upload_id", row.ID).
			Err(err).
			Msg("forwarder: mark transient retry failed")
	}
}

func (w *Worker) markFailed(ctx context.Context, row types.QsoUpload, lastErr string) {
	if err := w.db.MarkUploadFailedWithContext(ctx, row.ID, lastErr); err != nil {
		w.logger.ErrorWith().
			Str("forwarder", w.cfg.Name).
			Int64("upload_id", row.ID).
			Err(err).
			Msg("forwarder: mark failed failed")
	}
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
func (w *Worker) markSuccess(ctx context.Context, row types.QsoUpload, upstreamID string) {
	prefix := w.fwd.AdifPrefix()
	if prefix != "" && row.Action != action.Delete.String() {
		if err := w.db.MarkUploadSuccessWithAdifStampWithContext(
			ctx, row.ID, upstreamID, row.QsoID, prefix,
		); err != nil {
			w.logger.ErrorWith().
				Str("forwarder", w.cfg.Name).
				Int64("upload_id", row.ID).
				Int64("qso_id", row.QsoID).
				Str("adif_prefix", prefix).
				Err(err).
				Msg("forwarder: mark success + adif stamp failed")
		}
		return
	}

	if err := w.db.MarkUploadSuccessWithContext(ctx, row.ID, upstreamID); err != nil {
		w.logger.ErrorWith().
			Str("forwarder", w.cfg.Name).
			Int64("upload_id", row.ID).
			Err(err).
			Msg("forwarder: mark success failed")
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
