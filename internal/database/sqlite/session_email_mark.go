package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	stderr "errors"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/adapters"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/aarondl/sqlboiler/v4/queries/qm"
)

// SessionEmailTarget names a QSO row to stamp as session-emailed, together with the
// revision whose content the caller verified against the sent attachment (W-0010
// outcome 11; PT-3, W-0008). The stamp matches on BOTH id and revision, so a row
// written after that verification is not marked: a compare-and-set, never a wrong
// mark.
type SessionEmailTarget struct {
	ID       int64
	Revision int64
}

// Classified outcomes of a failed mark. Any other error means the attempt was
// rolled back and committed nothing.
var (
	// ErrSessionMarkUncertain: COMMIT failed. database/sql has already marked the
	// transaction done, so no rollback can be issued from here; the pinned driver
	// (modernc v1.48.1, tx.go Commit) rolls back itself when the failed COMMIT left
	// the transaction open. Whether the attempt committed is NOT confirmed: the
	// caller must neither report these rows as marked nor as definitively unmarked.
	ErrSessionMarkUncertain = stderr.New("session email mark: commit failed; outcome not confirmed")
	// ErrSessionRollbackFailed: a statement of the marking transaction or of the
	// verification read failed, and the explicit rollback failed too, so the
	// connection's state is unknown. A rollback that finds the transaction already
	// ended (sql.ErrTxDone: database/sql rolls back itself when the transaction's
	// context ends) is NOT this: that transaction did roll back.
	ErrSessionRollbackFailed = stderr.New("session email: rollback after a failed statement also failed")
)

// rollbackAfter rolls tx back after cause and classifies the outcome: cause alone
// when the rollback succeeded or the transaction had already ended (sql.ErrTxDone
// after database/sql's own context-driven rollback), ErrSessionRollbackFailed
// joined with both errors otherwise.
func rollbackAfter(tx *sql.Tx, cause error) error {
	rbErr := tx.Rollback()
	if rbErr == nil || stderr.Is(rbErr, sql.ErrTxDone) {
		return cause
	}
	return stderr.Join(ErrSessionRollbackFailed, cause, rbErr)
}

// sessionMarkStatementDeadline is a test-only observation seam, nil in
// production: it receives the deadline of the context the marking UPDATE runs on.
var sessionMarkStatementDeadline func(deadline time.Time, ok bool)

// sessionMarkBeforeCommit is a test-only fault seam, nil in production: it runs
// inside the marking transaction after the RETURNING rows are consumed and
// closed, immediately before COMMIT. A non-nil error is treated like a failed
// statement (rolled back).
var sessionMarkBeforeCommit func(ctx context.Context, tx *sql.Tx) error

// MarkSessionEmailedAtRevisionWithContext stamps the "forwarded by email" ADIF
// fields on the QSO rows that are STILL at the given revisions, in ONE explicit
// transaction (W-0010 outcome 11):
//
//	$.sm_fwrd_by_email_status = "Y"          (adif.YesString)
//	$.sm_fwrd_by_email_date   = stampDate (UTC YYYYMMDD, from the caller)
//
// These keys match the JSON tags on types.Qso, so the next read surfaces the stamp
// as struct fields — no schema column. A row matches only when its id AND revision
// match a target AND it is not soft-deleted; the UPDATE's own trigger then bumps
// the row's revision (ADR 0050), exactly as any other update does.
//
// The UPDATE is the transaction's first statement, so the transaction is a writer
// from its first statement — no read-to-write upgrade, so no stranded snapshot.
// The returned ids are reported only after every RETURNING row was consumed, the
// rows closed, and COMMIT succeeded: what is reported marked is exactly what
// committed. On a statement failure the transaction is rolled back explicitly and
// nothing is marked; see ErrSessionMarkUncertain and ErrSessionRollbackFailed
// for the two classified failures. Stamping a row already marked is harmless —
// json_set overwrites the date. stampDate comes from the CALLER so the date it
// reports onward matches the date stored (codex 2026-08-08 P3).
func (s *Service) MarkSessionEmailedAtRevisionWithContext(ctx context.Context, targets []SessionEmailTarget, stampDate string) ([]int64, error) {
	const op errors.Op = "sqlite.Service.MarkSessionEmailedAtRevisionWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, nil
	}

	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := withOperationLimit(ctx, s.DatabaseConfig.TransactionContextTimeout)
	defer cancel()

	// Carry the whole target set in ONE JSON-array argument, matched through
	// json_each — NOT a generated OR-chain or a multi-row VALUES. At the documented
	// 10,000-target request cap an OR-chain would blow SQLITE_MAX_EXPR_DEPTH (1000)
	// and a multi-row VALUES risks SQLITE_MAX_COMPOUND_SELECT (500); json_each keeps
	// the statement at a constant three bound parameters at any scale. Every
	// id/revision rides inside the JSON (no interpolation of caller data); the two
	// stamp values are static literals.
	pairs := make([][2]int64, len(targets))
	for i, tgt := range targets {
		pairs[i] = [2]int64{tgt.ID, tgt.Revision}
	}
	targetsJSON, err := json.Marshal(pairs)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("marshal stamp targets")
	}

	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		// No transaction exists, so there is nothing to roll back.
		return nil, errors.New(op).WithErr(err).WithMsg("begin session email stamp")
	}
	// The statement keeps its own, possibly shorter, limit inside the
	// transaction's: ContextTimeout for the UPDATE, TransactionContextTimeout for
	// the transaction around it.
	stmtCtx, cancelStmt := withOperationLimit(ctx, s.DatabaseConfig.ContextTimeout)
	stamped, err := stampInTx(stmtCtx, tx, stampDate, string(targetsJSON))
	cancelStmt()
	if err == nil && sessionMarkBeforeCommit != nil {
		err = sessionMarkBeforeCommit(ctx, tx)
	}
	if err != nil {
		return nil, errors.New(op).WithErr(rollbackAfter(tx, err)).WithMsg("stamp session-emailed flag")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.New(op).WithErr(stderr.Join(ErrSessionMarkUncertain, err)).
			WithMsg("commit session email stamp")
	}
	return stamped, nil
}

// stampInTx runs the guarded UPDATE ... RETURNING and consumes, closes and checks
// its rows. SQLite does not guarantee RETURNING row order, so callers key by id.
func stampInTx(ctx context.Context, tx *sql.Tx, stampDate, targetsJSON string) ([]int64, error) {
	const query = `
UPDATE qso
SET    additional_data = json_set(
           additional_data,
           '$.sm_fwrd_by_email_status', ?,
           '$.sm_fwrd_by_email_date', ?
       )
WHERE  deleted_at IS NULL
  AND  (id, revision) IN (
           SELECT json_extract(value, '$[0]'), json_extract(value, '$[1]')
           FROM   json_each(?)
       )
RETURNING id`

	if sessionMarkStatementDeadline != nil {
		sessionMarkStatementDeadline(ctx.Deadline())
	}
	rows, err := tx.QueryContext(ctx, query, adif.YesString, stampDate, targetsJSON)
	if err != nil {
		return nil, err
	}
	var stamped []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		stamped = append(stamped, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return stamped, nil
}

// withOperationLimit bounds one database operation by its configured limit
// (seconds) EVEN WHEN the caller's context already carries a deadline:
// context.WithTimeout keeps the earlier of the two. ensureCtxTimeout, by
// contrast, returns a context with a deadline as-is, which would let a caller's
// longer budget replace the shorter per-operation limit (W-0010 outcome 11: the
// session-email marking context is an admission budget, not a per-operation one).
// A non-positive limit adds no bound.
func withOperationLimit(ctx context.Context, seconds int) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if seconds <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
}

// fetchCurrentChunk bounds the bound parameters of one IN (...) read.
const fetchCurrentChunk = 500

// FetchCurrentQsosByIDWithContext reads the current, non-deleted rows for ids in
// ONE read transaction, so the set is one snapshot, and finishes that transaction
// before returning (W-0010 outcome 11, the verification read). A soft-deleted or
// missing id is simply absent from the map.
func (s *Service) FetchCurrentQsosByIDWithContext(ctx context.Context, ids []int64) (map[int64]types.Qso, error) {
	const op errors.Op = "sqlite.Service.FetchCurrentQsosByIDWithContext"
	if err := checkService(op, s); err != nil {
		return nil, err
	}
	out := make(map[int64]types.Qso, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return nil, err
	}
	ctx, cancel := withOperationLimit(ctx, s.DatabaseConfig.ContextTimeout)
	defer cancel()

	tx, err := h.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("begin verification read")
	}
	if err := readCurrentInto(ctx, tx, ids, out); err != nil {
		return nil, errors.New(op).WithErr(rollbackAfter(tx, err)).WithMsg("verification read")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("finish verification read")
	}
	return out, nil
}

func readCurrentInto(ctx context.Context, tx *sql.Tx, ids []int64, out map[int64]types.Qso) error {
	for start := 0; start < len(ids); start += fetchCurrentChunk {
		end := min(start+fetchCurrentChunk, len(ids))
		args := make([]any, end-start)
		for i, id := range ids[start:end] {
			args[i] = id
		}
		// models.Qsos adds the soft-delete filter (deleted_at IS NULL).
		rows, err := models.Qsos(qm.WhereIn(models.QsoColumns.ID+" IN ?", args...)).All(ctx, tx)
		if err != nil {
			return err
		}
		for _, m := range rows {
			q, err := adapters.QsoModelToType(m)
			if err != nil {
				return err
			}
			out[q.ID] = q
		}
	}
	return nil
}
