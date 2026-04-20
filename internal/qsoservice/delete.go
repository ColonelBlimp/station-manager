package qsoservice

import (
	"context"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
)

// Delete soft-deletes a QSO and atomically enqueues one qso_upload
// row per enabled forwarder whose action_filter includes 'delete'.
//
// Soft-delete + queue inserts share a single transaction under the
// one-fails-all-fail invariant (docs/v2-design/forwarding.md §1):
// either both the QSO is marked deleted AND every matching forwarder
// has a delete row queued, or neither side of the tx persists.
//
// Returns errors.ErrNotFound if the QSO does not exist or is
// already soft-deleted.
func (s *Service) Delete(ctx context.Context, id int64) error {
	const op errors.Op = "qsoservice.Delete"

	if id < 1 {
		return errors.New(op).WithMsgf("QSO ID is invalid: %d", id)
	}

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to begin transaction")
	}
	defer cancel()

	logbookID, err := s.DB.DeleteQsoByIDTx(ctx, tx, id)
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
		if err = s.DB.InsertQsoUploadTx(ctx, tx, id, action.Delete, fwd.Name, fwd.Type); err != nil {
			_ = tx.Rollback()
			return errors.New(op).WithErr(err).WithMsg("failed to insert upload-queue row")
		}
	}

	if err = tx.Commit(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to commit transaction")
	}

	s.Logger.InfoWith().Int64("qso_id", id).Msg("QSO soft-deleted")

	s.Hub.Publish(events.NameQsoDeleted, events.QsoDeletedPayload{
		QsoID:     id,
		LogbookID: logbookID,
	})

	return nil
}
