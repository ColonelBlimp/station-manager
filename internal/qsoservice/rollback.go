package qsoservice

import (
	"database/sql"

	"github.com/ColonelBlimp/station-manager/internal/database/txutil"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// rollbackTx rolls tx back and CLASSIFIES the result, reusing txutil.Rollback so every
// transaction owner shares one policy (EH-6): a clean rollback and a benign
// sql.ErrTxDone (the tx already completed — after an in-flight insert failure that
// means the driver already rolled back) are CONFIRMED and return nil; any other
// rollback error is UNVERIFIED and is returned so the caller can decide.
//
// This package owns one-fails-all-fail for QSO writes — a QSO row and its upload-queue
// rows are atomic — so a rollback that itself fails is precisely the case where that
// promise may not have held, and these call sites are the only place that can observe
// it (Q6). An unverified rollback is warn-logged here (tagged with the triggering
// operation) and returned; the caller preserves the triggering error alongside it and
// must not take another mutation path on the same rows. A confirmed rollback is silent.
func (s *Service) rollbackTx(tx *sql.Tx, op errors.Op) error {
	var rbErr error
	txutil.Rollback(tx, &rbErr) // nil on clean / benign sql.ErrTxDone; joins the cause otherwise
	if rbErr != nil {
		s.Logger.WarnWith().
			Str("op", string(op)).
			Str("error", rbErr.Error()).
			Msg("transaction rollback failed after an error; write atomicity is unverified")
	}
	return rbErr
}
