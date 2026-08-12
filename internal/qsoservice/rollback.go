package qsoservice

import (
	"database/sql"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// rollbackTx rolls tx back and logs a Warn if the ROLLBACK ITSELF fails, tagged
// with the operation that triggered it. This package owns one-fails-all-fail for
// QSO writes — a QSO row and its upload-queue rows are atomic — so a rollback that
// fails is precisely the case where that promise may not have held, and these call
// sites are the only place that can observe it (Q6). The triggering operation's own
// error is returned by the caller and logged at ERR separately; a clean rollback is
// silent.
func (s *Service) rollbackTx(tx *sql.Tx, op errors.Op) {
	if err := tx.Rollback(); err != nil {
		s.Logger.WarnWith().
			Str("op", string(op)).
			Str("error", err.Error()).
			Msg("transaction rollback failed after an error; write atomicity is unverified")
	}
}
