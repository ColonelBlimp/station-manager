package qsoservice

import (
	"context"
	"encoding/json"
	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Delete soft-deletes a QSO and atomically enqueues one qso_upload
// row per ENABLED forwarder whose action_filter includes 'delete'
// (ADR 0039: `enabled` gates enqueue), plus a qso_history audit row
// carrying the pre-delete snapshot
// (ADR 0016 prep #2).
//
// All three writes share a single transaction under the
// one-fails-all-fail invariant (docs/v2-design/forwarding.md §1):
// either the QSO is marked deleted AND every matching forwarder has
// a delete row queued AND the audit row is appended, or none of it
// persists.
//
// existing is the snapshot the handler already fetched to resolve
// UUID→ID; its revision is the optimistic-concurrency expectation
// (revision CAS, PT-2). The audit row's before_image is NOT this
// snapshot but the authoritative pre-delete image DeleteQsoByIDTx
// reads inside the transaction, so a concurrent edit committed after
// this fetch can never leave a stale before_image in the append-only
// chain.
//
// src identifies which subsystem of the daemon initiated the delete
// (recorded on the audit row).
//
// Returns errors.ErrNotFound if the QSO is missing or already
// soft-deleted, and a *SubmitError{Code:"delete_conflict"} if the row
// is still live at a different revision (the handler maps it to 409).
func (s *Service) Delete(ctx context.Context, existing types.Qso, src source.Source) error {
	const op errors.Op = "qsoservice.Delete"

	if existing.ID < 1 {
		return errors.New(op).WithMsgf("QSO ID is invalid: %d", existing.ID)
	}
	if existing.UUID == "" {
		return errors.New(op).WithMsg("QSO UUID is empty")
	}

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to begin transaction")
	}
	defer cancel()

	preimage, logbookID, err := s.DB.DeleteQsoByIDTx(ctx, tx, existing.ID, existing.Revision)
	if err != nil {
		s.rollbackTx(tx, op)
		// A still-live revision mismatch is a caller-facing conflict, parallel to
		// the edit path's edit_conflict: the QSO changed after this request fetched
		// it, so the delete refuses rather than removing a newer state and appending
		// a stale before-image to the audit chain (PT-2). ErrNotFound passes through
		// for the handler's 404 (missing / already-tombstoned).
		if stderr.Is(err, errors.ErrStaleRevision) {
			return &SubmitError{
				Code:    "delete_conflict",
				Message: "the QSO changed while this delete was in flight — reload it and retry",
			}
		}
		return err
	}

	// Destinations this delete was queued to — recorded on "QSO soft-deleted" for the
	// same reason as submit's "QSO stored" (Q5). Non-nil so an empty fan-out logs [].
	forwardedTo := make([]string, 0, len(s.Config.Forwarders()))
	for _, fwd := range s.Config.Forwarders() {
		if !shouldEnqueue(fwd, action.Delete) {
			continue
		}
		if err = s.DB.InsertQsoUploadTx(ctx, tx, existing.ID, action.Delete, fwd.Name, fwd.Type, origin.Edit); err != nil {
			s.rollbackTx(tx, op)
			return errors.New(op).WithErr(err).WithMsg("failed to insert upload-queue row")
		}
		forwardedTo = append(forwardedTo, fwd.Name)
	}

	// The before_image is the AUTHORITATIVE in-transaction pre-delete image, not
	// the caller's snapshot — the last live state, guaranteed current by the
	// revision guard above (PT-2).
	beforeImage, err := json.Marshal(preimage)
	if err != nil {
		s.rollbackTx(tx, op)
		return errors.New(op).WithErr(err).WithMsg("failed to marshal pre-delete image")
	}
	if err = s.DB.InsertQsoHistoryTx(ctx, tx, existing.UUID, action.Delete, src, beforeImage); err != nil {
		s.rollbackTx(tx, op)
		return errors.New(op).WithErr(err).WithMsg("failed to insert qso_history row")
	}

	if err = tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to commit transaction")
	}

	s.Logger.InfoWith().
		Int64("qso_id", existing.ID).
		Str("call", existing.ContactedStation.Call).
		Str("qso_date", existing.QsoDetails.QsoDate).
		Str("time_on", existing.QsoDetails.TimeOn).
		Strs("forwarded_to", forwardedTo).
		Msg("QSO soft-deleted")

	s.Hub.Publish(events.NameQsoDeleted, events.QsoDeletedPayload{
		QsoUUID:   existing.UUID,
		QsoID:     existing.ID,
		LogbookID: logbookID,
	})

	return nil
}
