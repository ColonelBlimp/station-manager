package qsoservice

import (
	"context"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// EnqueueStampSync re-enqueues QSOs (by row id) to every enabled ROW-MIRROR
// forwarder (forwarding.IsRowMirror — the SM Cloud backup) after an
// out-of-band row write. The two such writes are the post-upload ADIF stamp
// (worker markSuccess) and the session-email stamp: both bump the row's
// revision AFTER the mirror already received it, and without this re-enqueue
// the mirror's copy stays one revision behind until the hourly reconcile
// heals it via a full-manifest diff — an O(logbook-size) download per drifted
// hour, per client (docs/backlog.md "smcloud stamp-drift"). Riding the normal
// worker queue instead costs ~2 KB once per stamp and keeps reconcile on its
// cheap hash-only path.
//
// Best-effort by contract: callers invoke it AFTER their stamp has committed
// and only log a failure — a stamp must never be rolled back or retried
// because the mirror enqueue failed; the reconciler remains the backstop for
// any missed row. Rows are enqueued blind (no live-check): a row soft-deleted
// between stamp and enqueue surfaces as the worker's established
// "soft-deleted; delete row supersedes" terminal outcome, which is correct —
// the delete-action row carries the removal.
//
// Returns the number of queue rows written. No enabled mirror configured is
// the common case (smcloud not set up) and returns (0, nil) without touching
// the DB.
func (s *Service) EnqueueStampSync(ctx context.Context, qsoIDs []int64) (int, error) {
	const op errors.Op = "qsoservice.EnqueueStampSync"

	if len(qsoIDs) == 0 {
		return 0, nil
	}
	// Routing is per QSO (ADR 0082 part 6): each row goes to the enabled
	// row-mirror bindings of ITS logbook, so the logbook of every id is read first.
	logbooks, err := s.DB.LogbookIDsByQsoIDsWithContext(ctx, qsoIDs)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("resolve logbooks of the stamped rows")
	}
	type target struct {
		qsoID int64
		fc    types.ForwarderConfig
	}
	var targets []target
	for _, qsoID := range qsoIDs {
		lb, ok := logbooks[qsoID]
		if !ok {
			continue // the row is gone; nothing to mirror
		}
		for _, fc := range s.routesFor(lb) {
			if forwarding.IsRowMirror(fc.Type) && shouldEnqueue(fc, action.Update) {
				targets = append(targets, target{qsoID: qsoID, fc: fc})
			}
		}
	}
	if len(targets) == 0 {
		return 0, nil
	}

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("begin transaction")
	}
	defer cancel()

	n := 0
	for _, tg := range targets {
		if err = s.DB.InsertQsoUploadTx(ctx, tx, tg.qsoID, action.Update, tg.fc.Name, tg.fc.Type, origin.StampSync); err != nil {
			s.rollbackTx(tx, op)
			return 0, errors.New(op).WithErr(err).WithMsg("insert mirror upload-queue row")
		}
		n++
	}
	if err = tx.Commit(); err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("commit transaction")
	}

	// Info, not Debug (Q4): this is the built half of the smcloud
	// stamp-drift/reconcile-bandwidth item, and its failure mode is silent
	// non-firing that returns reconcile to the expensive full-manifest path. Its
	// volume is bounded by stamp events (a handful of rows a day), not traffic, so
	// it belongs at the default level where "is the cheap path holding?" is answerable.
	s.Logger.InfoWith().
		Int("queued", n).
		Int("qsos", len(qsoIDs)).
		Msg("stamp sync: re-enqueued revision-bumped rows to mirror forwarder(s)")
	return n, nil
}
