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
// A committed tx makes Rollback return sql.ErrTxDone — a deterministic real failure,
// no fake needed.

func TestRollbackTx_FailedRollbackLogsWarn(t *testing.T) {
	s := newTestService(t)
	buf := logbuf(s)
	ctx := context.Background()

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, tx.Commit()) // now done: a subsequent Rollback returns sql.ErrTxDone

	s.rollbackTx(tx, errors.Op("qsoservice.testRollback"))

	out := buf.String()
	require.Contains(t, out, `"level":"warn"`,
		"a rollback that itself fails is the only observable evidence the atomic write may not have held")
	require.Contains(t, out, "rollback failed", "the message names the failed cleanup")
	require.Contains(t, out, "qsoservice.testRollback", "and the operation it was rolling back")
}

func TestRollbackTx_CleanRollbackIsSilent(t *testing.T) {
	s := newTestService(t)
	buf := logbuf(s)
	ctx := context.Background()

	tx, cancel, err := s.DB.BeginTxContext(ctx)
	require.NoError(t, err)
	defer cancel()

	s.rollbackTx(tx, errors.Op("qsoservice.testRollback")) // fresh tx → clean rollback

	require.NotContains(t, buf.String(), "rollback failed",
		"a clean rollback must stay silent — only a FAILED rollback is worth a line")
}
