package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0010 outcome 11: the session-email mark runs in one explicit transaction per
// attempt, and its failures are classified.

func seedMarkQsos(t *testing.T, svc *Service, calls ...string) []types.Qso {
	t.Helper()
	lbID, err := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}
	out := make([]types.Qso, 0, len(calls))
	for i, call := range calls {
		q := validTestQso(lbID, call, "40m", "SSB", "20261106", "0845")
		q.TimeOn = []string{"0845", "0846", "0847", "0848"}[i%4]
		id, err := svc.InsertQso(q)
		if err != nil {
			t.Fatalf("insert %s: %v", call, err)
		}
		stored, err := svc.FetchQsoByIdWithContext(context.Background(), id)
		if err != nil {
			t.Fatalf("fetch %s: %v", call, err)
		}
		out = append(out, stored)
	}
	return out
}

func setMarkFault(t *testing.T, f func(context.Context, *sql.Tx) error) {
	t.Helper()
	sessionMarkBeforeCommit = f
	t.Cleanup(func() { sessionMarkBeforeCommit = nil })
}

func readBack(t *testing.T, svc *Service, id int64) types.Qso {
	t.Helper()
	q, err := svc.FetchQsoByIDIncludingDeletedWithContext(context.Background(), id)
	if err != nil {
		t.Fatalf("read back %d: %v", id, err)
	}
	return q
}

// Case 12: a failure after the UPDATE and before COMMIT rolls the attempt back:
// nothing is marked, the revision is untouched, and the error is neither an
// uncertain commit nor a rollback failure.
func TestSessionMark_FaultBeforeCommitRollsBack(t *testing.T) {
	svc := testService(t)
	q := seedMarkQsos(t, svc, "M0CMA")[0]
	fault := stderr.New("injected before commit")
	setMarkFault(t, func(context.Context, *sql.Tx) error { return fault })

	stamped, err := svc.MarkSessionEmailedAtRevisionWithContext(context.Background(),
		[]SessionEmailTarget{{ID: q.ID, Revision: q.Revision}}, "20261106")
	if stamped != nil || !stderr.Is(err, fault) {
		t.Fatalf("stamped = %v, err = %v; want nil and the injected fault", stamped, err)
	}
	if stderr.Is(err, ErrSessionMarkUncertain) || stderr.Is(err, ErrSessionRollbackFailed) {
		t.Fatalf("a rolled-back statement failure was misclassified: %v", err)
	}
	got := readBack(t, svc, q.ID)
	if got.SmFwrdByEmailStatus == adif.YesString || got.Revision != q.Revision {
		t.Fatalf("after rollback: status %q revision %d; want unmarked at revision %d",
			got.SmFwrdByEmailStatus, got.Revision, q.Revision)
	}
}

// Case 12a: a test-only deferred foreign-key violation makes COMMIT fail with the
// transaction left open. The mark reports ErrSessionMarkUncertain (not "unmarked"),
// the pinned driver's own rollback leaves nothing persisted, and the connection is
// clean: the next attempt on the same, only connection marks and commits.
func TestSessionMark_DeferredFKCommitFailureIsUncertainAndRecoverable(t *testing.T) {
	svc := testService(t) // shared :memory:, one connection
	q := seedMarkQsos(t, svc, "M0CMA")[0]
	setMarkFault(t, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
			return err
		}
		// Deferred: no error here; the violation is reported at COMMIT.
		_, err := tx.ExecContext(ctx, `UPDATE qso SET logbook_id = 987654321 WHERE id = ?`, q.ID)
		return err
	})

	stamped, err := svc.MarkSessionEmailedAtRevisionWithContext(context.Background(),
		[]SessionEmailTarget{{ID: q.ID, Revision: q.Revision}}, "20261106")
	if stamped != nil || !stderr.Is(err, ErrSessionMarkUncertain) {
		t.Fatalf("stamped = %v, err = %v; want nil and ErrSessionMarkUncertain", stamped, err)
	}
	got := readBack(t, svc, q.ID)
	if got.SmFwrdByEmailStatus == adif.YesString || got.LogbookID != q.LogbookID || got.Revision != q.Revision {
		t.Fatalf("after the failed commit: status %q logbook %d revision %d; want nothing persisted",
			got.SmFwrdByEmailStatus, got.LogbookID, got.Revision)
	}

	sessionMarkBeforeCommit = nil
	stamped, err = svc.MarkSessionEmailedAtRevisionWithContext(context.Background(),
		[]SessionEmailTarget{{ID: q.ID, Revision: got.Revision}}, "20261106")
	if err != nil || len(stamped) != 1 || stamped[0] != q.ID {
		t.Fatalf("next attempt: stamped = %v, err = %v; want [%d] on a clean connection", stamped, err, q.ID)
	}
	if readBack(t, svc, q.ID).SmFwrdByEmailStatus != adif.YesString {
		t.Fatal("next attempt reported marked but the row is not")
	}
}

// The verification read returns current, non-deleted rows by id, across the
// chunk boundary, in one snapshot.
func TestFetchCurrentQsosByID(t *testing.T) {
	svc := testService(t)
	qs := seedMarkQsos(t, svc, "M0CMA", "M0CMB", "M0CMC")
	if _, err := svc.handle.ExecContext(context.Background(),
		`UPDATE qso SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?`, qs[1].ID); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}
	ids := []int64{qs[0].ID, qs[1].ID, qs[2].ID}
	for i := int64(0); i < fetchCurrentChunk+10; i++ {
		ids = append(ids, 1_000_000+i) // missing ids push the read across chunks
	}
	got, err := svc.FetchCurrentQsosByIDWithContext(context.Background(), ids)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (deleted and missing ids absent)", len(got))
	}
	if _, ok := got[qs[1].ID]; ok {
		t.Fatal("a soft-deleted row was returned")
	}
	if got[qs[0].ID].Revision != qs[0].Revision || got[qs[2].ID].UUID != qs[2].UUID {
		t.Fatalf("rows not read faithfully: %+v", got)
	}
}

// Cases 15 and 16, on the production shape (a temporary-file WAL database with a
// real pool): a commit on ANOTHER connection between the verification read and the
// mark makes the compare-and-set miss exactly that row. The marking transaction is
// a writer from its first statement, so the concurrent commit cannot strand it
// (no SQLITE_BUSY_SNAPSHOT): the call succeeds and marks the untouched row.
func TestSessionMark_WALPool_ConcurrentCommitBetweenVerifyAndMark(t *testing.T) {
	svc := concurrentCacheService(t, filepath.Join(t.TempDir(), "mark.db"))
	ctx := context.Background()
	qs := seedMarkQsos(t, svc, "M0CMA", "M0CMB")

	verified, err := svc.FetchCurrentQsosByIDWithContext(ctx, []int64{qs[0].ID, qs[1].ID})
	if err != nil {
		t.Fatalf("verification read: %v", err)
	}

	other, err := svc.handle.Conn(ctx)
	if err != nil {
		t.Fatalf("second connection: %v", err)
	}
	defer func() { _ = other.Close() }()
	if _, err := other.ExecContext(ctx,
		`UPDATE qso SET additional_data = json_set(additional_data, '$.clublog_qso_upload_status', 'Y') WHERE id = ?`,
		qs[0].ID); err != nil {
		t.Fatalf("concurrent commit: %v", err)
	}

	targets := []SessionEmailTarget{
		{ID: qs[0].ID, Revision: verified[qs[0].ID].Revision},
		{ID: qs[1].ID, Revision: verified[qs[1].ID].Revision},
	}
	stamped, err := svc.MarkSessionEmailedAtRevisionWithContext(ctx, targets, "20261106")
	if err != nil {
		t.Fatalf("mark after a concurrent commit failed (stranded?): %v", err)
	}
	if len(stamped) != 1 || stamped[0] != qs[1].ID {
		t.Fatalf("stamped = %v, want only [%d]: the concurrently written row must miss", stamped, qs[1].ID)
	}
	if readBack(t, svc, qs[0].ID).SmFwrdByEmailStatus == adif.YesString {
		t.Fatal("the concurrently written row was marked at a revision nobody verified")
	}
}

// The per-operation limit survives a caller's LONGER deadline (W-0010 outcome 11:
// the marking context is an admission budget, so it must not replace the shorter
// per-operation bound the way ensureCtxTimeout would). A shorter caller deadline
// still wins.
func TestWithOperationLimit_KeepsTheEarlierDeadline(t *testing.T) {
	long, cancelLong := context.WithTimeout(context.Background(), time.Hour)
	defer cancelLong()
	ctx, cancel := withOperationLimit(long, 2)
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok || time.Until(dl) > 3*time.Second {
		t.Fatalf("deadline in %v, want the 2 s operation limit despite the 1 h caller budget", time.Until(dl))
	}

	short, cancelShort := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelShort()
	ctx2, cancel2 := withOperationLimit(short, 30)
	defer cancel2()
	if dl2, _ := ctx2.Deadline(); time.Until(dl2) > time.Second {
		t.Fatalf("deadline in %v, want the caller's shorter 100 ms", time.Until(dl2))
	}
}

// The marking UPDATE itself runs under ContextTimeout even inside a longer
// TransactionContextTimeout and a longer caller budget (observed on the real
// marking path, not only the helper).
func TestSessionMark_StatementKeepsItsShorterLimit(t *testing.T) {
	svc := testService(t)
	q := seedMarkQsos(t, svc, "M0CMA")[0]
	cfg := *svc.DatabaseConfig
	cfg.ContextTimeout, cfg.TransactionContextTimeout = 2, 60
	svc.DatabaseConfig = &cfg
	var observed time.Duration
	var had bool
	sessionMarkStatementDeadline = func(dl time.Time, ok bool) { observed, had = time.Until(dl), ok }
	t.Cleanup(func() { sessionMarkStatementDeadline = nil })

	budget, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if _, err := svc.MarkSessionEmailedAtRevisionWithContext(budget,
		[]SessionEmailTarget{{ID: q.ID, Revision: q.Revision}}, "20261106"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if !had || observed > 3*time.Second {
		t.Fatalf("statement deadline in %v (set %v); want the 2 s ContextTimeout, not the 60 s transaction limit or the 1 h budget", observed, had)
	}
}

// When the transaction has already ended (database/sql rolls back itself when the
// transaction's context ends), the explicit rollback returns sql.ErrTxDone. That
// transaction DID roll back: it is not ErrSessionRollbackFailed, so the caller's
// retries are not stopped while its budget remains.
func TestSessionMark_AlreadyRolledBackIsNotARollbackFailure(t *testing.T) {
	t.Run("simulated: transaction ended before the explicit rollback", func(t *testing.T) {
		svc := testService(t)
		q := seedMarkQsos(t, svc, "M0CMA")[0]
		setMarkFault(t, func(_ context.Context, tx *sql.Tx) error {
			if err := tx.Rollback(); err != nil { // stands in for database/sql's own rollback
				t.Fatalf("pre-rollback: %v", err)
			}
			return context.DeadlineExceeded
		})
		_, err := svc.MarkSessionEmailedAtRevisionWithContext(context.Background(),
			[]SessionEmailTarget{{ID: q.ID, Revision: q.Revision}}, "20261106")
		if !stderr.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the cause", err)
		}
		if stderr.Is(err, ErrSessionRollbackFailed) || stderr.Is(err, ErrSessionMarkUncertain) {
			t.Fatalf("an already rolled-back transaction was misclassified: %v", err)
		}
		if readBack(t, svc, q.ID).SmFwrdByEmailStatus == adif.YesString {
			t.Fatal("row marked")
		}
	})
	t.Run("real: the caller's context is cancelled inside the transaction", func(t *testing.T) {
		svc := testService(t)
		q := seedMarkQsos(t, svc, "M0CMA")[0]
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		setMarkFault(t, func(ctx context.Context, _ *sql.Tx) error {
			cancel()
			<-ctx.Done()
			return ctx.Err()
		})
		_, err := svc.MarkSessionEmailedAtRevisionWithContext(ctx,
			[]SessionEmailTarget{{ID: q.ID, Revision: q.Revision}}, "20261106")
		if err == nil {
			t.Fatal("want an error after the cancellation")
		}
		// Whether database/sql's own rollback or the explicit one ran first, the
		// transaction rolled back: never a rollback failure.
		if stderr.Is(err, ErrSessionRollbackFailed) {
			t.Fatalf("cancellation misclassified as a rollback failure: %v", err)
		}
		if readBack(t, svc, q.ID).SmFwrdByEmailStatus == adif.YesString {
			t.Fatal("row marked")
		}
	})
}
