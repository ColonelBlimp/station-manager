package qsoservice

import (
	"context"
	stderr "errors"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// EnqueueResult summarises a manual backfill enqueue (ADR 0039 — the logbook
// SPA's "upload to X" action). Every input UUID lands in exactly one bucket. The
// counts drive the operator's confirmation toast; the small exceptional lists
// (deleted / unknown) let the SPA point at the rows it couldn't act on.
type EnqueueResult struct {
	Enqueued        int      `json:"enqueued"`                  // queued (or re-armed) for upload
	SkippedUploaded int      `json:"skipped_uploaded"`          // already on this destination; not re-sent (no force)
	SkippedDeleted  []string `json:"skipped_deleted,omitempty"` // soft-deleted — not backfilled
	NotFound        []string `json:"not_found,omitempty"`       // unknown or malformed UUID
}

// EnqueueUploads queues already-stored QSOs (by UUID) for upload to one ENABLED
// forwarder — the manual backfill path of ADR 0039. It is how QSOs that were
// logged while a forwarder was disabled, pre-dated it, or were never auto-queued
// reach a destination; the operator selects rows in the logbook SPA and pushes
// them to one destination at a time.
//
// Policy:
//   - The destination must be a configured, ENABLED forwarder whose action_filter
//     covers insert. A disabled forwarder has no worker AND has its queue rows
//     discarded at startup (ADR 0039), so enqueuing to one would strand the rows —
//     reject up front (forwarder_unavailable).
//   - Already-uploaded QSOs are skipped unless force, so a repeat backfill fills
//     only genuine gaps instead of re-sending. "Already uploaded" = an existing
//     insert-upload row for this destination at status=uploaded; that row is
//     written atomically with the ADIF stamp and is preserved across the disable
//     discard, so it is the durable per-destination "done" signal. Keying the
//     check on forwarder_name (not a per-type ADIF prefix) keeps it N-agnostic —
//     a new forwarder type needs no change here.
//   - Soft-deleted and unknown QSOs are reported, never fatal — one bad UUID in a
//     large selection must not fail the rest (per-QSO best-effort).
//
// The actual inserts run in a single transaction (one-shot re-arm via the
// InsertQsoUploadTx UPSERT). A genuine DB failure rolls the whole batch back and
// surfaces as an error; the read-side classification has already happened, so a
// retry is safe and idempotent.
func (s *Service) EnqueueUploads(ctx context.Context, forwarderName string, uuids []string, force bool) (EnqueueResult, error) {
	const op errors.Op = "qsoservice.EnqueueUploads"

	fwd, ok := s.findEnabledInsertForwarder(forwarderName)
	if !ok {
		return EnqueueResult{}, &SubmitError{
			Code:    "forwarder_unavailable",
			Message: "forwarder is unknown, disabled, or does not forward new QSOs",
		}
	}

	// The destination's ADIF stamp prefix is the durable "already uploaded?"
	// signal (consistent with the missing_from filter + the SPA colour). A type
	// that stamps nothing (e.g. a custom webhook) has no such signal — there's
	// nothing to skip on, so every selected QSO is enqueued regardless of force.
	stampPrefix, hasStamp := forwarding.AdifPrefixForType(fwd.Type)

	var res EnqueueResult
	enqueueIDs := make([]int64, 0, len(uuids))

	for _, raw := range uuids {
		uuid := strings.TrimSpace(raw)
		if uuid == "" || !utils.IsValidUUIDv7(uuid) {
			res.NotFound = append(res.NotFound, raw)
			continue
		}

		qso, err := s.DB.FetchQsoByUUIDWithContext(ctx, uuid)
		if err != nil {
			if stderr.Is(err, errors.ErrNotFound) {
				// Non-deleted fetch missed: tell a soft-deleted QSO (don't
				// backfill-insert a deleted contact) apart from a genuinely
				// unknown UUID via the including-deleted probe.
				if _, derr := s.DB.FetchQsoByUUIDIncludingDeletedWithContext(ctx, uuid); derr == nil {
					res.SkippedDeleted = append(res.SkippedDeleted, uuid)
				} else {
					res.NotFound = append(res.NotFound, uuid)
				}
				continue
			}
			return EnqueueResult{}, errors.New(op).WithErr(err).WithMsg("fetch QSO by uuid")
		}

		if !force && hasStamp {
			already, aerr := s.DB.HasUploadStampWithContext(ctx, qso.ID, stampPrefix)
			if aerr != nil {
				return EnqueueResult{}, errors.New(op).WithErr(aerr).WithMsg("check existing upload stamp")
			}
			if already {
				res.SkippedUploaded++
				continue
			}
		}

		enqueueIDs = append(enqueueIDs, qso.ID)
	}

	if len(enqueueIDs) == 0 {
		return res, nil
	}

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return EnqueueResult{}, errors.New(op).WithErr(err).WithMsg("begin transaction")
	}
	defer cancel()

	for _, qsoID := range enqueueIDs {
		if err = s.DB.InsertQsoUploadTx(ctx, tx, qsoID, action.Insert, fwd.Name, fwd.Type); err != nil {
			_ = tx.Rollback()
			return EnqueueResult{}, errors.New(op).WithErr(err).WithMsg("insert upload-queue row")
		}
	}
	if err = tx.Commit(); err != nil {
		return EnqueueResult{}, errors.New(op).WithErr(err).WithMsg("commit transaction")
	}

	res.Enqueued = len(enqueueIDs)

	s.Logger.InfoWith().
		Str("forwarder", fwd.Name).
		Str("type", fwd.Type).
		Int("enqueued", res.Enqueued).
		Int("skipped_uploaded", res.SkippedUploaded).
		Bool("force", force).
		Msg("manual upload backfill enqueued")

	return res, nil
}

// findEnabledInsertForwarder resolves a forwarder by name (case-insensitive) and
// returns it only if it is enabled and forwards inserts — the eligibility gate
// for a manual backfill (mirrors the submit-path shouldEnqueue check).
func (s *Service) findEnabledInsertForwarder(name string) (types.ForwarderConfig, bool) {
	name = strings.TrimSpace(name)
	for _, fc := range s.Config.Forwarders() {
		if strings.EqualFold(fc.Name, name) && shouldEnqueue(fc, action.Insert) {
			return fc, true
		}
	}
	return types.ForwarderConfig{}, false
}
