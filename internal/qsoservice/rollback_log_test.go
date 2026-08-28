package qsoservice

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Q6 — A FAILED ROLLBACK IS THE ONLY EVIDENCE ONE-FAILS-ALL-FAIL MAY NOT HAVE HELD.
//
// CRITERION (qsoservice-logging-gaps Q6): 14 sites discarded `tx.Rollback()`'s error.
// This package owns the invariant that a QSO row and its upload-queue rows are atomic;
// a rollback that itself fails is precisely the case where that promise may not have
// held, and these sites are the only place that can observe it. rollbackTx now warns
// on a non-nil rollback error; a clean rollback stays silent.
//
// After PT-5, rollbackTx CLASSIFIES its result (reusing txutil.Rollback): a clean
// rollback and a benign sql.ErrTxDone are CONFIRMED (nil, silent); only a genuine
// rollback failure is UNVERIFIED (returned + warn-logged). A genuine failure needs a
// controllable driver, so it is proven in the sqlmock import test
// (TestSubmitImportBatch_UncertainRollbackAbortsWithoutFallback); the two cases below
// pin the confirmed side that a real in-memory DB can produce.

// A committed (or already auto-rolled-back) transaction makes Rollback return
// sql.ErrTxDone. That is BENIGN — a confirmed completion, not an unverified failure —
// so rollbackTx returns nil and stays silent.
func TestRollbackTx_BenignErrTxDoneIsConfirmedAndSilent(t *testing.T) {
	s := newTestService(t)
	buf := logbuf(s)
	ctx := context.Background()

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, tx.Commit()) // a subsequent Rollback returns sql.ErrTxDone

	require.NoError(t, s.rollbackTx(tx, errors.Op("qsoservice.testRollback")),
		"sql.ErrTxDone is a benign, confirmed rollback — not an unverified failure")
	require.NotContains(t, buf.String(), "rollback failed",
		"a benign ErrTxDone must not warn about unverified atomicity")
}

func TestRollbackTx_CleanRollbackIsConfirmedAndSilent(t *testing.T) {
	s := newTestService(t)
	buf := logbuf(s)
	ctx := context.Background()

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	require.NoError(t, err)
	defer cancel()

	require.NoError(t, s.rollbackTx(tx, errors.Op("qsoservice.testRollback")), // fresh tx → clean
		"a clean rollback is confirmed → nil")
	require.NotContains(t, buf.String(), "rollback failed",
		"a clean rollback must stay silent — only an UNVERIFIED rollback is worth a line")
}
