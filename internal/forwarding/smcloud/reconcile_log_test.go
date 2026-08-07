package smcloud

import (
	"context"
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
