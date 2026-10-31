package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
)

// AllocateContestSerialWithContext hands out the sent serial for one draft of
// a contest QSO in one logbook, in one occurrence of the contest — its start
// date, YYYYMMDD (W-0023 weekend slice). Starting a contest QSO
// consumes its serial for good — abandoned, duplicate or unsaved, the number
// is never handed out again, so the sequence can have gaps but never repeats.
// A draft keeps the serial it was given: asking again (a lost response) gets
// the same number back, whatever other drafts took in between. A new serial
// goes above both the ledger and any Sent # already saved for the occurrence. The write is
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
		// The next serial goes above every serial of this occurrence already on
		// air: those allocated, and any Sent # saved on a QSO — typed by hand as
		// an override or after a failed allocation — which the ledger never saw.
		return tx.QueryRowContext(ctx, `
			INSERT INTO contest_serial_allocation (logbook_id, contest_id, occurrence, serial, draft)
			SELECT ?1, ?2, ?3, MAX(
				(SELECT COALESCE(MAX(serial), 0) FROM contest_serial_allocation
					WHERE logbook_id = ?1 AND contest_id = ?2 AND occurrence = ?3),
				(SELECT COALESCE(MAX(CAST(json_extract(additional_data, '$.stx') AS INTEGER)), 0) FROM qso
					WHERE logbook_id = ?1
					AND json_extract(additional_data, '$.contest_id') = ?2
					AND json_extract(additional_data, '$.app_sm_contest_start') = ?3)
			) + 1, ?4
			RETURNING serial`, logbookID, contestID, occurrence, draft).Scan(&serial)
	})
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("allocate contest serial")
	}
	return serial, nil
}

// IsContestOccurrenceDuplicateWithContext answers "have I worked this call on
// this band in this contest occurrence already?" (W-0023 weekend slice). Only
// the occurrence's own contacts count — CONTEST_ID and APP_SM_CONTEST_START
// both match — so a contact before the contest, or in another edition, is not
// a duplicate. Band only: the UK/EI contests are one mode each. Deleted
// contacts do not count (the models' default filter). A duplicate is reported,
// never refused: the sponsor keeps duplicates in the log (ruling A).
func (s *Service) IsContestOccurrenceDuplicateWithContext(ctx context.Context, logbookID int64, callsign, band, contestID, occurrence string) (bool, error) {
	const op errors.Op = "sqlite.Service.IsContestOccurrenceDuplicateWithContext"
	if err := checkService(op, s); err != nil {
		return false, err
	}
	callsign, band = strings.TrimSpace(callsign), strings.TrimSpace(band)
	contestID, occurrence = strings.TrimSpace(contestID), strings.TrimSpace(occurrence)
	if logbookID < 1 || callsign == "" || band == "" || contestID == "" || occurrence == "" {
		return false, errors.New(op).WithMsg("logbook, callsign, band, contest and occurrence are required")
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return false, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	exists, err := models.Qsos(
		models.QsoWhere.Call.EQ(callsign),
		models.QsoWhere.Band.EQ(band),
		models.QsoWhere.LogbookID.EQ(logbookID),
		qm.Where("json_extract(additional_data, '$.contest_id') = ?", contestID),
		qm.Where("json_extract(additional_data, '$.app_sm_contest_start') = ?", occurrence),
	).Exists(ctx, h)
	if err != nil {
		return false, errors.New(op).WithErr(err)
	}
	return exists, nil
}
