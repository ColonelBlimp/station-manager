package qsoservice

import (
	"context"
	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// defaultImportBatchSize is the number of QSOs written per transaction by the
// bulk importer. Bounded so a batch's WAL footprint (and the cost of the rare
// per-record fallback when a batch write fails) stay reasonable, while still
// collapsing thousands of per-record commits into a handful — the dominant cost
// of the old one-transaction-per-QSO import loop.
const defaultImportBatchSize = 500

// ImportError records one record that couldn't be stored, for the importer's
// "errored records" report. Index is the 0-based position in the input slice.
type ImportError struct {
	Index  int
	Call   string
	Reason string
}

// ImportBatchResult accumulates the outcome of a bulk import.
type ImportBatchResult struct {
	Stored    int
	Duplicate int
	Errors    []ImportError
}

// SubmitImportBatch is the bulk-import write path (cmd/smd import). It is the
// batched counterpart to per-record SubmitImport, built for big logbooks: it
//   - hoists the logbook-callsign lookup OUT of the per-record loop (one query),
//   - writes QSOs in chunked transactions (batchSize per commit) instead of one
//     commit per QSO — the big win, since per-record commits drove the WAL
//     checkpoint churn that made a large import slow,
//   - dedupes the contacted_station cache writes to UNIQUE calls (one upsert per
//     call, not per QSO),
//   - preserves the per-record "errored records" report: a record that fails
//     validation/dedupe is recorded and skipped, and if a batch's DB write fails
//     the batch is rolled back and re-run per-record (via the proven submit path)
//     to isolate the bad record while still storing the good ones.
//
// It shares prepareQso with the live submit path, so validation is identical.
// Unlike submit it does NOT publish a per-QSO QsoStored event — bulk import runs
// with no live SSE subscribers (the daemon is stopped for exclusive DB access),
// so 45k no-op publishes are pointless. onProgress (optional) is called after
// each batch commits with the running totals, for the CLI's progress line.
func (s *Service) SubmitImportBatch(
	ctx context.Context,
	logbookID int64,
	recs []adif.Record,
	forwardTo []string,
	batchSize int,
	onProgress func(done int, r ImportBatchResult),
) (ImportBatchResult, error) {
	const op errors.Op = "qsoservice.SubmitImportBatch"
	if batchSize <= 0 {
		batchSize = defaultImportBatchSize
	}

	logbookCallsign, lberr := s.DB.LogbookCallsignByIDWithContext(ctx, logbookID)
	if lberr != nil {
		if stderr.Is(lberr, errors.ErrNotFound) {
			return ImportBatchResult{}, &SubmitError{Code: "logbook_not_found", Message: "logbook does not exist"}
		}
		return ImportBatchResult{}, errors.New(op).WithErr(lberr).WithMsg("looking up logbook callsign")
	}

	var res ImportBatchResult
	contacts := make(map[string]types.ContactedStation) // unique call → station (last wins)
	forwarders := s.Config.Forwarders()

	for start := 0; start < len(recs); start += batchSize {
		end := start + batchSize
		if end > len(recs) {
			end = len(recs)
		}
		if err := s.importBatch(ctx, logbookID, logbookCallsign, recs[start:end], start,
			forwardTo, forwarders, contacts, &res); err != nil {
			return res, err
		}
		if onProgress != nil {
			onProgress(end, res)
		}
	}

	// contacted_station cache — best-effort (one-fails-all-fail keeps this outside
	// the QSO transactions), deduped to unique calls.
	for _, cs := range contacts {
		if uerr := s.DB.UpsertContactedStationWithContext(ctx, cs); uerr != nil {
			s.Logger.WarnWith().Err(uerr).Str("call", cs.Call).
				Msg("contacted_station upsert failed (best-effort, import)")
		}
	}
	return res, nil
}

// importBatch processes one batch in two phases. Phase 1 validates + dedupes
// with NO open transaction (the reads use the pool — holding a tx open across
// them would consume a second connection and deadlock at MaxOpenConns=1, and a
// pooled read can't see the tx's own uncommitted rows anyway). Committed
// duplicates (prior batches + pre-existing) are caught by the DB lookup; in-batch
// repeats by batchKeys. Phase 2 writes the survivors in a single transaction.
//
// res and contacts are mutated only AFTER the batch commits, so a rollback can't
// double-count: a failed transactional write rolls back and re-runs the WHOLE
// batch per-record via the proven submit path (which rebuilds the batch's tally
// itself). A BeginTx/Commit failure is fatal (the DB is unusable) and aborts.
func (s *Service) importBatch(
	ctx context.Context,
	logbookID int64,
	logbookCallsign string,
	batch []adif.Record,
	baseIndex int,
	forwardTo []string,
	forwarders []types.ForwarderConfig,
	contacts map[string]types.ContactedStation,
	res *ImportBatchResult,
) error {
	const op errors.Op = "qsoservice.SubmitImportBatch"

	// ---- Phase 1: validate + dedupe (no transaction). Tallies stay local until
	// the batch commits.
	type prepared struct {
		qso types.Qso
		idx int
	}
	var toInsert []prepared
	batchKeys := make(map[string]struct{}, len(batch))
	var batchDup int
	var batchErrs []ImportError

	for i, rec := range batch {
		idx := baseIndex + i
		qso, key, perr := s.prepareQso(rec, logbookID, logbookCallsign, false, true)
		if perr != nil {
			batchErrs = append(batchErrs, ImportError{Index: idx, Call: rec.Call, Reason: submitErrReason(perr)})
			continue
		}
		if _, dup := batchKeys[key]; dup {
			batchDup++
			continue
		}
		if _, derr := s.DB.FetchQsoByDedupeKeyWithContext(ctx, logbookID, key); derr == nil {
			batchDup++
			continue
		} else if !stderr.Is(derr, errors.ErrNotFound) {
			batchErrs = append(batchErrs, ImportError{Index: idx, Call: rec.Call, Reason: derr.Error()})
			continue
		}
		batchKeys[key] = struct{}{}
		toInsert = append(toInsert, prepared{qso: qso, idx: idx})
	}

	// ---- Phase 2: write the survivors in ONE transaction.
	if len(toInsert) > 0 {
		tx, cancel, err := s.DB.BeginTxContext(ctx)
		if err != nil {
			return errors.New(op).WithErr(err).WithMsg("failed to begin import batch transaction")
		}
		defer cancel()

		for _, p := range toInsert {
			qsoID, ierr := s.DB.InsertQsoTx(ctx, tx, p.qso)
			if ierr != nil {
				_ = tx.Rollback()
				return s.importBatchFallback(ctx, logbookID, batch, baseIndex, forwardTo, res)
			}
			for _, fwd := range forwarders {
				if !shouldEnqueue(fwd, action.Insert) || !forwarderNamed(forwardTo, fwd.Name) {
					continue
				}
				if uerr := s.DB.InsertQsoUploadTx(ctx, tx, qsoID, action.Insert, fwd.Name, fwd.Type); uerr != nil {
					_ = tx.Rollback()
					return s.importBatchFallback(ctx, logbookID, batch, baseIndex, forwardTo, res)
				}
			}
		}
		if err := tx.Commit(); err != nil {
			return errors.New(op).WithErr(err).WithMsg("failed to commit import batch transaction")
		}
	}

	// Commit succeeded (or nothing to insert) — fold the batch tally into res.
	for _, p := range toInsert {
		contacts[p.qso.ContactedStation.Call] = p.qso.ContactedStation
	}
	res.Stored += len(toInsert)
	res.Duplicate += batchDup
	res.Errors = append(res.Errors, batchErrs...)
	return nil
}

// importBatchFallback re-runs a batch (whose transactional write failed)
// record-by-record through the proven submit path, so one bad record can't sink
// the whole batch: good records store, the offender is reported, duplicates are
// counted. It rebuilds the batch's entire tally on res (the caller folds nothing
// in this case). submit() upserts contacted_station itself, so these calls don't
// need the deduped map. A non-SubmitError (hard DB error) aborts the import.
func (s *Service) importBatchFallback(
	ctx context.Context,
	logbookID int64,
	batch []adif.Record,
	baseIndex int,
	forwardTo []string,
	res *ImportBatchResult,
) error {
	const op errors.Op = "qsoservice.SubmitImportBatch"
	for i, rec := range batch {
		idx := baseIndex + i
		r, err := s.submit(ctx, logbookID, rec, false, true, forwardTo)
		if err != nil {
			if se := IsSubmitError(err); se != nil {
				res.Errors = append(res.Errors, ImportError{Index: idx, Call: rec.Call, Reason: se.Code + ": " + se.Message})
				continue
			}
			return errors.New(op).WithErr(err).WithMsg("import fallback: record store failed")
		}
		switch r.Status {
		case "duplicate":
			res.Duplicate++
		default:
			res.Stored++
		}
	}
	return nil
}

// submitErrReason renders a prepareQso error for the errored-records report:
// the stable "code: message" for a caller-facing SubmitError, else the raw error.
func submitErrReason(err error) string {
	if se := IsSubmitError(err); se != nil {
		return se.Code + ": " + se.Message
	}
	return err.Error()
}
