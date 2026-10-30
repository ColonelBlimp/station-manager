package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// AllocateContestSerialWithContext hands out the sent serial for one draft of
// a contest QSO in one logbook, in one occurrence of the contest — its start
// date, YYYYMMDD (W-0023 weekend slice). Starting a contest QSO
// consumes its serial for good — abandoned, duplicate or unsaved, the number
// is never handed out again, so the sequence can have gaps but never repeats.
// A draft keeps the serial it was given: asking again (a lost response) gets
// the same number back, whatever other drafts took in between. The write is
// durable across a power loss (durableTx) because the number may be on air.
func (s *Service) AllocateContestSerialWithContext(ctx context.Context, logbookID int64, contestID, occurrence, draft string) (int64, error) {
	const op errors.Op = "sqlite.Service.AllocateContestSerialWithContext"
	if err := checkService(op, s); err != nil {
		return 0, err
	}
	contestID = strings.TrimSpace(contestID)
	if contestID == "" || strings.TrimSpace(occurrence) == "" || strings.TrimSpace(draft) == "" {
		return 0, errors.New(op).WithMsg("contest id, occurrence and draft are required")
	}
	var serial int64
	err := s.durableTx(ctx, op, "allocate contest serial", func(ctx context.Context, tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT serial FROM contest_serial_allocation
			WHERE logbook_id = ? AND contest_id = ? AND occurrence = ? AND draft = ?`,
			logbookID, contestID, occurrence, draft).Scan(&serial)
		if !stderr.Is(err, sql.ErrNoRows) {
			return err // the draft's own serial, or a read failure
		}
		return tx.QueryRowContext(ctx, `
			INSERT INTO contest_serial_allocation (logbook_id, contest_id, occurrence, serial, draft)
			SELECT ?, ?, ?, COALESCE(MAX(serial), 0) + 1, ?
			FROM contest_serial_allocation WHERE logbook_id = ? AND contest_id = ? AND occurrence = ?
			RETURNING serial`, logbookID, contestID, occurrence, draft, logbookID, contestID, occurrence).Scan(&serial)
	})
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("allocate contest serial")
	}
	return serial, nil
}
