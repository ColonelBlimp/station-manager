package qsoservice

import (
	"context"
	stderr "errors"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
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
	// SkippedNoHistory carries UUIDs refused because the destination accepts
	// RETRIES of previously queued live uploads only (forwarding
	// NoBulkBackfill types — ClubLog's realtime.php rule): a QSO with no
	// queue history for this forwarder was never a live upload, so sending it
	// now would be catch-up backfill, which belongs to an ADIF upload on the
	// destination's website.
	SkippedNoHistory []string `json:"skipped_no_history,omitempty"`
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
//     only genuine gaps instead of re-sending. "Already uploaded" is keyed on the
//     destination's ADIF stamp prefix (AdifPrefixForType + HasUploadStamp), NOT on
//     the queue row: the stamp is written into the QSO's additional_data atomically
//     with a successful upload and survives the disable-discard, so it is the
//     durable per-TYPE "done" signal (consistent with the missing_from filter and
//     the SPA's "uploaded" colour). Consequence of keying on type, not name: two
//     configured forwarders of the SAME type (e.g. qrz-primary + qrz-backup) share
//     one stamp, so a backfill to the second skips what is already on the first.
//   - Soft-deleted and unknown QSOs are reported, never fatal — one bad UUID in a
//     large selection must not fail the rest (per-QSO best-effort).
//
// The actual inserts run in a single transaction (one-shot re-arm via the
// InsertQsoUploadTx UPSERT). A genuine DB failure rolls the whole batch back and
// surfaces as an error; the read-side classification has already happened, so a
// retry is safe and idempotent.
// org names what is asking — manual backfill or a reconcile repair. It is a
// PARAMETER rather than something inferred here because both callers share this
// function and `force` does not separate them (docs/reviews/forwarding-logging-gaps.md F1).
func (s *Service) EnqueueUploads(ctx context.Context, forwarderName string, uuids []string, force bool, org origin.Origin) (EnqueueResult, error) {
	const op errors.Op = "qsoservice.EnqueueUploads"

	fwd, ok := s.findEnabledInsertForwarder(forwarderName)
	if !ok {
		return EnqueueResult{}, &SubmitError{
			Code:    "forwarder_unavailable",
			Message: "forwarder is unknown, disabled, or does not forward new QSOs",
		}
	}

	// Some upstreams forbid catch-up batches on the realtime endpoint the
	// forwarder uses (ClubLog's realtime.php rule — batch catch-up there gets
	// the application API key blocked). RETRYING a previously queued live
	// upload is legitimate realtime usage, though (2026-07-19 review #2 — this
	// endpoint is how a 403-era Terminal row gets re-armed after the operator
	// fixes credentials and restarts), so the distinction is per row via queue
	// history below: an UNFINISHED insert row → retry allowed; anything else →
	// backfill, refused into SkippedNoHistory.
	//
	// KNOWN LIMITATION (review round 2 #2, accepted): queue rows are working
	// state, not durable provenance — disabling the forwarder purges its
	// non-uploaded rows at the next daemon startup (ADR 0039 stop+drop), after
	// which a genuinely-failed live upload classifies as no-history here. The
	// degraded path is the destination's blessed one anyway (ADIF export
	// uploaded on its website), so no durable marker is kept; the in-app fix
	// for every bulk case is the putlogs.php route (docs/backlog.md).
	retryOnly := forwarding.NoBulkBackfill(fwd.Type)
	if retryOnly && force {
		// force means "re-send even though already uploaded" — for a
		// retry-only destination that is by definition the prohibited
		// catch-up batch (up to a full selection of already-delivered QSOs
		// re-hitting the realtime endpoint), so it is refused outright
		// rather than silently narrowed (review round 2 #1).
		return EnqueueResult{}, &SubmitError{
			Code: "force_unsupported",
			Message: "this destination accepts retries of failed uploads only; force re-send " +
				"is not available — for anything already uploaded, use an ADIF export on the " +
				"destination's website",
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
				} else if stderr.Is(derr, errors.ErrNotFound) {
					res.NotFound = append(res.NotFound, uuid)
				} else {
					// A non-ErrNotFound error on the tombstone probe is an infra fault,
					// not "unknown UUID": bucketing it as NotFound would return HTTP
					// success while silently skipping a deleted QSO the reconciler meant
					// to repair. Propagate it (2026-07-21 review finding 6).
					return EnqueueResult{}, errors.New(op).WithErr(derr).WithMsg("tombstone probe by uuid")
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

		// Retry-only destinations: allow the enqueue only when this QSO has an
		// UNFINISHED insert row for THIS forwarder — a live upload that never
		// succeeded (failed/pending/in_progress). An uploaded row is NOT retry
		// provenance (re-sending delivered QSOs is the prohibited catch-up
		// shape even one at a time — review round 2 #1), and a delete row says
		// nothing about insert history.
		if retryOnly {
			hist, herr := s.DB.FetchUploadsByQsoIDWithContext(ctx, qso.ID)
			if herr != nil {
				return EnqueueResult{}, errors.New(op).WithErr(herr).WithMsg("check upload queue history")
			}
			retryable := false
			for _, h := range hist {
				if strings.EqualFold(h.ForwarderName, fwd.Name) &&
					h.Action == action.Insert.String() &&
					h.Status != status.Uploaded.String() {
					retryable = true
					break
				}
			}
			if !retryable {
				res.SkippedNoHistory = append(res.SkippedNoHistory, uuid)
				continue
			}
		}

		enqueueIDs = append(enqueueIDs, qso.ID)
	}

	if len(enqueueIDs) == 0 {
		// Every successful return logs — THIS one especially (logging-gaps Q2):
		// a selection in which every QSO was refused is the pure ClubLog-
		// compliance case (skipped_no_history honours the 2026-07-19
		// realtime.php grant condition), and it used to write nothing at all —
		// all-refused and never-invoked were the same silence.
		s.logEnqueueResult(fwd.Name, fwd.Type, len(uuids), force, org, res)
		return res, nil
	}

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return EnqueueResult{}, errors.New(op).WithErr(err).WithMsg("begin transaction")
	}
	defer cancel()

	for _, qsoID := range enqueueIDs {
		if err = s.DB.InsertQsoUploadTx(ctx, tx, qsoID, action.Insert, fwd.Name, fwd.Type, org); err != nil {
			_ = tx.Rollback()
			return EnqueueResult{}, errors.New(op).WithErr(err).WithMsg("insert upload-queue row")
		}
	}
	if err = tx.Commit(); err != nil {
		return EnqueueResult{}, errors.New(op).WithErr(err).WithMsg("commit transaction")
	}

	res.Enqueued = len(enqueueIDs)
	s.logEnqueueResult(fwd.Name, fwd.Type, len(uuids), force, org, res)
	return res, nil
}

// logEnqueueResult writes the backfill outcome line (logging-gaps Q2): the
// requested selection size plus ALL FIVE outcome counts — {enqueued:12} and
// {enqueued:12, skipped_no_history:300} used to be the same line, and the
// refusals SM makes to honour its written ClubLog commitment existed only in a
// browser response. Lengths only: the UUID lists belong to the response (and
// Debug if anywhere), not Info. The origin field answers who asked — the
// manual-backfill handler and the reconciler's heal path both land here, and
// the message deliberately claims no attribution the field could contradict
// (its first wording said "manual" and mislabelled every reconcile heal).
func (s *Service) logEnqueueResult(fwdName, fwdType string, requested int, force bool, org origin.Origin, res EnqueueResult) {
	s.Logger.InfoWith().
		Str("forwarder", fwdName).
		Str("type", fwdType).
		Str("origin", org.String()).
		Int("requested", requested).
		Int("enqueued", res.Enqueued).
		Int("skipped_uploaded", res.SkippedUploaded).
		Int("skipped_deleted", len(res.SkippedDeleted)).
		Int("not_found", len(res.NotFound)).
		Int("skipped_no_history", len(res.SkippedNoHistory)).
		Bool("force", force).
		Msg("upload backfill result")
}

// findEnabledInsertForwarder resolves a forwarder by name (case-insensitive) and
// returns it only if it is enabled and forwards inserts — the eligibility gate
// for a manual backfill (mirrors the submit-path shouldEnqueue check).
func (s *Service) findEnabledInsertForwarder(name string) (types.ForwarderConfig, bool) {
	return s.findEnabledForwarderFor(name, action.Insert)
}

// findEnabledForwarderFor resolves a forwarder by name (case-insensitive) and
// returns it only if it is enabled and its action_filter covers act.
func (s *Service) findEnabledForwarderFor(name string, act action.Action) (types.ForwarderConfig, bool) {
	name = strings.TrimSpace(name)
	for _, fc := range s.Config.Forwarders() {
		if strings.EqualFold(fc.Name, name) && shouldEnqueue(fc, act) {
			return fc, true
		}
	}
	return types.ForwarderConfig{}, false
}

// EnqueueDeleteResult summarises an EnqueueDeleteUploads call. SkippedLive
// carries UUIDs that turned out NOT to be soft-deleted — a delete row for a
// live QSO would wrongly remove it upstream, so they are refused per-row.
type EnqueueDeleteResult struct {
	Enqueued    int      `json:"enqueued"`
	SkippedLive []string `json:"skipped_live,omitempty"`
	NotFound    []string `json:"not_found,omitempty"`
}

// EnqueueDeleteUploads queues delete-action upload rows for already
// SOFT-DELETED QSOs (by UUID) to one enabled forwarder whose action_filter
// covers delete. The normal delete path enqueues at delete time (DeleteQso);
// this is the repair path for a delete the destination missed — the SM Cloud
// reconciler (ADR 0040 S4) uses it to push tombstones the cloud still shows
// as live. Per-row best-effort like EnqueueUploads: a live or unknown UUID is
// reported, never fatal.
// org names what is asking — see EnqueueUploads.
func (s *Service) EnqueueDeleteUploads(ctx context.Context, forwarderName string, uuids []string, org origin.Origin) (EnqueueDeleteResult, error) {
	const op errors.Op = "qsoservice.EnqueueDeleteUploads"

	fwd, ok := s.findEnabledForwarderFor(forwarderName, action.Delete)
	if !ok {
		return EnqueueDeleteResult{}, &SubmitError{
			Code:    "forwarder_unavailable",
			Message: "forwarder is unknown, disabled, or does not forward deletes",
		}
	}

	var res EnqueueDeleteResult
	enqueueIDs := make([]int64, 0, len(uuids))
	for _, raw := range uuids {
		uuid := strings.TrimSpace(raw)
		if uuid == "" || !utils.IsValidUUIDv7(uuid) {
			res.NotFound = append(res.NotFound, raw)
			continue
		}
		// A delete row must reference a soft-deleted QSO: the live-only fetch
		// succeeding means the row is NOT deleted → refuse it.
		if _, err := s.DB.FetchQsoByUUIDWithContext(ctx, uuid); err == nil {
			res.SkippedLive = append(res.SkippedLive, uuid)
			continue
		} else if !stderr.Is(err, errors.ErrNotFound) {
			// Same class as EnqueueUploads' probe (2026-07-21 review finding 6): a
			// non-ErrNotFound fault must NOT fall through to the including-deleted
			// fetch below — if the live-only fetch faulted on a QSO that is actually
			// live, the fall-through would find it and enqueue a DELETE against a live
			// row. Propagate the fault instead.
			return EnqueueDeleteResult{}, errors.New(op).WithErr(err).WithMsg("live-only fetch by uuid")
		}
		qso, err := s.DB.FetchQsoByUUIDIncludingDeletedWithContext(ctx, uuid)
		if err != nil {
			if stderr.Is(err, errors.ErrNotFound) {
				res.NotFound = append(res.NotFound, uuid)
				continue
			}
			return EnqueueDeleteResult{}, errors.New(op).WithErr(err).WithMsg("fetch QSO by uuid")
		}
		enqueueIDs = append(enqueueIDs, qso.ID)
	}

	if len(enqueueIDs) == 0 {
		// Same rule as the insert path (logging-gaps Q2): the zero-enqueue
		// return logs too, or a repair that repaired nothing is confusable
		// with one never attempted.
		s.logEnqueueDeleteResult(fwd.Name, fwd.Type, len(uuids), org, res)
		return res, nil
	}

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return EnqueueDeleteResult{}, errors.New(op).WithErr(err).WithMsg("begin transaction")
	}
	defer cancel()
	for _, qsoID := range enqueueIDs {
		if err = s.DB.InsertQsoUploadTx(ctx, tx, qsoID, action.Delete, fwd.Name, fwd.Type, org); err != nil {
			_ = tx.Rollback()
			return EnqueueDeleteResult{}, errors.New(op).WithErr(err).WithMsg("insert delete upload-queue row")
		}
	}
	if err = tx.Commit(); err != nil {
		return EnqueueDeleteResult{}, errors.New(op).WithErr(err).WithMsg("commit transaction")
	}
	res.Enqueued = len(enqueueIDs)
	s.logEnqueueDeleteResult(fwd.Name, fwd.Type, len(uuids), org, res)
	return res, nil
}

// logEnqueueDeleteResult is the delete-repair sibling of logEnqueueResult
// (logging-gaps Q2): every return path logs, all outcome counts carried —
// NotFound was omitted even when the old line did fire. Lengths only. Origin
// on the line, attribution out of the message, same as the insert path (the
// first wording hardcoded "(reconcile repair)").
func (s *Service) logEnqueueDeleteResult(fwdName, fwdType string, requested int, org origin.Origin, res EnqueueDeleteResult) {
	s.Logger.InfoWith().
		Str("forwarder", fwdName).
		Str("type", fwdType).
		Str("origin", org.String()).
		Int("requested", requested).
		Int("enqueued", res.Enqueued).
		Int("skipped_live", len(res.SkippedLive)).
		Int("not_found", len(res.NotFound)).
		Msg("delete upload backfill result")
}
