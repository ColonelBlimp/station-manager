package smcloud

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/cloud/reconcile"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	Reconcile run-complete logging (docs/reviews/api-logging-gaps.md A2,
	smcloud half) — specified before the implementation, 2026-08-08 session of
	2026-08-07.

	Criterion: when the operator presses "check now" (POST
	/v1/smcloud/reconcile), smd.log records the run's outcome — in_sync and the
	counts — and I can tell it apart from (a) a press whose summary existed
	only in the HTTP response the browser then discarded (the old state: the
	on-demand wiring called RunOnce directly and bypassed the periodic loop's
	logSummary, so the press left no record at all), and (b) the hourly
	periodic run (the trigger field).

	RC1 — a successful run emits exactly ONE run-complete record carrying
	      trigger + in_sync + local/cloud counts. Emission lives INSIDE
	      RunOnce, so no trigger path — periodic loop, on-demand endpoint, a
	      future caller — can complete a run without leaving a record.
	RC2 — the record's trigger is the caller's: a manual and a periodic run
	      are distinguishable. (The periodic loop's own call site passes its
	      trigger at reconcile.go Run — not driven here because
	      reconcileStartupDelay is a 2-minute const; RC2 pins the mechanism
	      that call site uses.)
	RC3 — a FAILED run emits no run-complete record: failure reporting stays
	      the caller's (the loop's warn-and-retry, the endpoint's 500 +
	      writeServerError cause). Guards an implementation that logs before
	      it knows the run succeeded.

	The cloud side is a stub httptest server, not the Postgres-gated e2e
	stack: these rules are about the LOG LINE, not the reconcile algebra
	(reconcile_e2e_test.go owns that), and the stub is what makes them run
	everywhere CI runs. The stub answers the summary endpoints with exactly
	the local side's own empty-logbook {count, hash}, which forces the
	deterministic in-sync early return.
*/

// stubCloud serves the two reads an in-sync run performs: logbook resolve and
// the {count, hash} summary, answering with the local empty-logbook values.
func stubCloud(t *testing.T) *httptest.Server {
	t.Helper()
	count, hash := reconcile.Summary(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/logbooks":
			fmt.Fprintf(w, `{"logbooks":[{"id":7,"name":"main"}]}`)
		case "/v1/logbooks/7/reconcile":
			fmt.Fprintf(w, `{"count":%d,"hash":%q}`, count, hash)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// logReconciler builds a Reconciler over a real (empty) local stack and the
// given cloud URL, with its logger swapped for a buffer.
func logReconciler(t *testing.T, cloudURL string) (*Reconciler, *strings.Builder) {
	t.Helper()
	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, cloudURL)
	lbID, err := dbSvc.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	rec, err := NewReconciler(fc, lbID, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)
	var buf strings.Builder
	rec.log = logging.NewForWriter(&buf)
	return rec, &buf
}

const runCompleteMsg = `"message":"smcloud reconcile: run complete"`

// RC1: one successful run, exactly one record, counts on it.
func TestReconcileRunOnce_LogsExactlyOneRunComplete(t *testing.T) {
	rec, buf := logReconciler(t, stubCloud(t).URL)

	sum, err := rec.RunOnce(context.Background(), TriggerManual)
	require.NoError(t, err)
	require.True(t, sum.InSync, "fixture: the stub echoes the local summary, so the run is in sync")

	out := buf.String()
	require.Equal(t, 1, strings.Count(out, runCompleteMsg),
		"exactly one run-complete record — the on-demand summary used to exist only "+
			"in the HTTP response the browser discarded")
	require.Contains(t, out, `"in_sync":true`, "the outcome is the record's point")
	require.Contains(t, out, `"local":0`)
	require.Contains(t, out, `"cloud":0`)
}

// RC2: manual and periodic runs are distinguishable by trigger.
func TestReconcileRunOnce_TriggerDistinguishesManualFromPeriodic(t *testing.T) {
	rec, buf := logReconciler(t, stubCloud(t).URL)
	ctx := context.Background()

	_, err := rec.RunOnce(ctx, TriggerManual)
	require.NoError(t, err)
	_, err = rec.RunOnce(ctx, TriggerPeriodic)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, `"trigger":"manual"`,
		"the operator's press names itself")
	require.Contains(t, out, `"trigger":"periodic"`,
		"the hourly run names itself — a press and the loop firing around the same "+
			"time were indistinguishable")
}

// RC3: a failed run leaves no run-complete record.
func TestReconcileRunOnce_FailedRunLogsNoRunComplete(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(down.Close)
	rec, buf := logReconciler(t, down.URL)

	_, err := rec.RunOnce(context.Background(), TriggerManual)
	require.Error(t, err, "fixture: the cloud answers 500, the run must fail")

	require.NotContains(t, buf.String(), runCompleteMsg,
		"a failed run must not claim completion — the failure is the caller's to "+
			"report (loop warn / endpoint 500)")
}

// RC4 (F8): a run that PARTIALLY mutated the queue and then failed records the
// mutation. A run that queued 400 upserts and then failed the delete enqueue used to
// log identically to one that did nothing — the partial mutation is durable queue
// state the "run failed" line alone hides. No natural manifest fixture produces the
// upserts-committed-then-deletes-fail path, so runOnceOverride drives it.
func TestReconcileRunOnce_PartialMutationOnFailureIsLogged(t *testing.T) {
	rec, buf := logReconciler(t, stubCloud(t).URL)
	rec.runOnceOverride = func() (ReconcileSummary, error) {
		return ReconcileSummary{EnqueuedUpserts: 400}, stderrors.New("enqueue deletes: boom")
	}

	_, err := rec.RunOnce(context.Background(), TriggerManual)
	require.Error(t, err)

	out := buf.String()
	require.Contains(t, out, "run failed after partially mutating the queue",
		"a partial mutation must be recorded, else it looks like a run that did nothing (F8)")
	require.Contains(t, out, `"upserts":400`, "the mutation counts are the point")
	require.NotContains(t, out, runCompleteMsg, "a failed run must not claim completion")
}

// L12-C2 (docs/reviews/internal-codebase-logging-gaps.md): the run-complete record must carry
// the full accounting, not just enqueued counts + a bare truncated flag. Operator rulings
// 2026-08-16: discovered = actionable upserts+deletes BEFORE truncation; attempted =
// min(upserts,limit)+min(deletes,limit) (per-list caps, logging-only — behaviour unchanged);
// skipped = attempted-enqueued (idempotently absorbed); deferred = discovered-attempted.
// Log discovered/enqueued/skipped ALWAYS; deferred + limit only when truncated.

// truncateBatch is the per-list cap + its accounting, factored out so the discovered
// (pre-cap) vs attempted (post-cap) capture is unit-testable without a >5000-row cloud run.
func TestTruncateBatch_PerListCapAccounting(t *testing.T) {
	mk := func(n int) []string { return make([]string, n) }
	cases := []struct {
		name                          string
		up, del, limit                int
		wantDiscovered, wantAttempted int
		wantTruncated                 bool
	}{
		{"neither over", 2, 3, 5, 5, 5, false},
		{"upserts over", 6, 3, 5, 9, 8, true},  // 5 (capped) + 3
		{"deletes over", 2, 8, 5, 10, 7, true}, // 2 + 5 (capped)
		{"both over", 6, 7, 5, 13, 10, true},   // 5 + 5 — per-list, NOT min(discovered,limit)=5
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, d, discovered, attempted, truncated := truncateBatch(mk(tc.up), mk(tc.del), tc.limit)
			require.Equal(t, tc.wantDiscovered, discovered, "discovered = pre-cap total")
			require.Equal(t, tc.wantAttempted, attempted, "attempted = per-list post-cap total")
			require.Equal(t, tc.wantTruncated, truncated)
			require.Equal(t, min(tc.up, tc.limit), len(u), "upserts capped per-list")
			require.Equal(t, min(tc.del, tc.limit), len(d), "deletes capped per-list")
		})
	}
}

// The observable: RunOnce (via the override seam, so no cloud math) logs the accounting and
// gates deferred/limit on truncated. A truncated run has skipped>0 AND deferred>0 so both
// paths differ from the trivial case.
func TestReconcileRunOnce_LogsFullAccounting(t *testing.T) {
	t.Run("truncated run carries deferred + limit", func(t *testing.T) {
		rec, buf := logReconciler(t, stubCloud(t).URL)
		rec.runOnceOverride = func() (ReconcileSummary, error) {
			// discovered 8000, attempted 6000 → deferred 2000; enqueued 5800 → skipped 200.
			return ReconcileSummary{
				Discovered: 8000, Attempted: 6000,
				EnqueuedUpserts: 4800, EnqueuedDeletes: 1000, Truncated: true,
			}, nil
		}
		_, err := rec.RunOnce(context.Background(), TriggerManual)
		require.NoError(t, err)

		out := buf.String()
		require.Contains(t, out, `"discovered":8000`)
		require.Contains(t, out, `"enqueued":5800`)
		require.Contains(t, out, `"skipped":200`, "skipped = attempted - enqueued")
		require.Contains(t, out, `"deferred":2000`, "deferred = discovered - attempted")
		require.Contains(t, out, fmt.Sprintf(`"limit":%d`, maxEnqueuePerRun))
	})

	t.Run("non-truncated run omits deferred + limit", func(t *testing.T) {
		rec, buf := logReconciler(t, stubCloud(t).URL)
		rec.runOnceOverride = func() (ReconcileSummary, error) {
			return ReconcileSummary{
				Discovered: 300, Attempted: 300,
				EnqueuedUpserts: 250, EnqueuedDeletes: 50, Truncated: false,
			}, nil
		}
		_, err := rec.RunOnce(context.Background(), TriggerManual)
		require.NoError(t, err)

		out := buf.String()
		require.Contains(t, out, `"discovered":300`)
		require.Contains(t, out, `"enqueued":300`)
		require.Contains(t, out, `"skipped":0`, "skipped is logged even at zero")
		require.NotContains(t, out, `"deferred"`, "deferred is only meaningful when truncated")
		require.NotContains(t, out, `"limit"`, "the cap is only worth logging when it bit")
	})
}
