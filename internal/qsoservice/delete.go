package qsoservice

import (
	"context"
	"encoding/json"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Delete soft-deletes a QSO and atomically enqueues one qso_upload
// row per CONFIGURED forwarder whose action_filter includes 'delete'
// (ADR 0022: gated by config presence + action_filter, not the Enabled
// flag), plus a qso_history audit row carrying the pre-delete snapshot
// (ADR 0016 prep #2).
//
// All three writes share a single transaction under the
// one-fails-all-fail invariant (docs/v2-design/forwarding.md §1):
// either the QSO is marked deleted AND every matching forwarder has
// a delete row queued AND the audit row is appended, or none of it
// persists.
//
// existing is the pre-delete snapshot — already fetched by the
// handler to resolve UUID→ID and verify the row exists. Reusing it
// here avoids a second round-trip and guarantees the audit row's
// before_image matches what the operator was looking at when they
// triggered the delete.
//
// src identifies which subsystem of the daemon initiated the delete
// (recorded on the audit row).
//
// Returns errors.ErrNotFound if the QSO does not exist or is
// already soft-deleted by the time the tx runs.
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

	logbookID, err := s.DB.DeleteQsoByIDTx(ctx, tx, existing.ID)
	if err != nil {
		_ = tx.Rollback()
		// Pass through ErrNotFound as-is so callers (handler layer) can
		// translate it to 404 via stderr.Is.
		return err
	}

	for _, fwd := range s.Config.Forwarders() {
		if !shouldEnqueue(fwd, action.Delete) {
			continue
		}
		if err = s.DB.InsertQsoUploadTx(ctx, tx, existing.ID, action.Delete, fwd.Name, fwd.Type); err != nil {
			_ = tx.Rollback()
			return errors.New(op).WithErr(err).WithMsg("failed to insert upload-queue row")
		}
	}

	beforeImage, err := json.Marshal(existing)
	if err != nil {
		_ = tx.Rollback()
		return errors.New(op).WithErr(err).WithMsg("failed to marshal pre-delete snapshot")
	}
	if err = s.DB.InsertQsoHistoryTx(ctx, tx, existing.UUID, action.Delete, src, beforeImage); err != nil {
		_ = tx.Rollback()
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
		Msg("QSO soft-deleted")

	s.Hub.Publish(events.NameQsoDeleted, events.QsoDeletedPayload{
		QsoID:     existing.ID,
		LogbookID: logbookID,
	})

	return nil
}
