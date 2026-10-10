package smcloud

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.4 commit 2: one reconciler per binding (rulings G2, G4,
   2026-10-10).

     RL1  every reconcile log line names its binding: run complete, the
          partial-queue-mutation warning, the periodic loop's failure, and
          the cloud-only notice. Several reconcilers now share smd.log.
     RR1  a periodic pass blocked mid-request returns once its context is
          cancelled (the workers node's drain waits on it).
     SN1  two kept name-wire bindings sharing one cloud logbook name: each
          repairs only its own QSOs (its missing rows and its own
          tombstones), and its peer's rows stay untouched cloud-only rows.
*/

// lockedBuf is a log sink safe for a reconciler's goroutine and the test.
type lockedBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// lineWith is the first log line holding msg, or "".
func lineWith(out, msg string) string {
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, msg) {
			return ln
		}
	}
	return ""
}

func TestReconcileBinding_RL1_LogLinesNameTheBinding(t *testing.T) {
	const named = `"forwarder":"smcloud"`

	t.Run("run complete", func(t *testing.T) {
		rec, buf := logReconciler(t, stubCloud(t).URL)
		_, err := rec.RunOnce(context.Background(), TriggerManual)
		require.NoError(t, err)
		ln := lineWith(buf.String(), "smcloud reconcile: run complete")
		require.NotEmpty(t, ln)
		require.Contains(t, ln, named)
	})

	t.Run("partial queue mutation", func(t *testing.T) {
		rec, buf := logReconciler(t, stubCloud(t).URL)
		rec.runOnceOverride = func() (ReconcileSummary, error) {
			return ReconcileSummary{EnqueuedUpserts: 3}, stderrors.New("enqueue deletes: boom")
		}
		_, err := rec.RunOnce(context.Background(), TriggerManual)
		require.Error(t, err)
		ln := lineWith(buf.String(), "run failed after partially mutating the queue")
		require.NotEmpty(t, ln)
		require.Contains(t, ln, named)
	})

	t.Run("periodic failure", func(t *testing.T) {
		failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(failing.Close)
		rec, _ := logReconciler(t, failing.URL)
		buf := &lockedBuf{}
		rec.log = logging.NewForWriter(buf)
		rec.startDelay = 10 * time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); rec.Run(ctx) }()
		require.Eventually(t, func() bool {
			return lineWith(buf.String(), "smcloud reconcile: run failed (next tick retries)") != ""
		}, 5*time.Second, 10*time.Millisecond)
		cancel()
		<-done
		require.Contains(t, lineWith(buf.String(), "smcloud reconcile: run failed (next tick retries)"), named)
	})

	t.Run("cloud-only notice", func(t *testing.T) {
		cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/logbooks":
				fmt.Fprint(w, `{"logbooks":[{"id":7,"name":"main"}]}`)
			case "/v1/logbooks/7/reconcile":
				fmt.Fprint(w, `{"count":1,"hash":"x"}`)
			case "/v1/logbooks/7/manifest":
				fmt.Fprint(w, `{"entries":[{"uuid":"019fd5c5-efcc-7193-be4f-1fee532e0001","modified_at":"2026-10-10T00:00:00Z","revision":1}]}`)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(cloud.Close)
		rec, buf := logReconciler(t, cloud.URL)
		sum, err := rec.RunOnce(context.Background(), TriggerManual)
		require.NoError(t, err)
		require.Equal(t, 1, sum.CloudOnly, "fixture: one cloud-only row")
		ln := lineWith(buf.String(), "cloud holds rows unknown locally")
		require.NotEmpty(t, ln)
		require.Contains(t, ln, named)
	})
}

func TestReconcileBinding_RR1_PeriodicPassCancelledMidRequest(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	cloud := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-r.Context().Done()
	}))
	t.Cleanup(cloud.Close)
	rec, _ := logReconciler(t, cloud.URL)
	rec.log = logging.NewForWriter(&lockedBuf{})
	rec.startDelay = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); rec.Run(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the periodic pass never reached the cloud")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a periodic pass blocked mid-request did not return after cancellation")
	}
}

func TestReconcileBinding_SN1_SharedNameRepairsOnlyItsOwnRows(t *testing.T) {
	cloud := newCloudStack(t)
	qsoSvc, dbSvc, logSvc, fc := newLocalStack(t, cloud.URL)
	ctx := context.Background()
	fcA, fcB := fc, fc // both carry the cloud logbook name "main"
	fcA.Name, fcB.Name = "smcloud-a", "smcloud-b"
	lbA, err := dbSvc.InsertLogbook(types.Logbook{Name: "A", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	lbB, err := dbSvc.InsertLogbook(types.Logbook{Name: "B", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	fwdA, err := New(fcA)
	require.NoError(t, err)
	fwdB, err := New(fcB)
	require.NoError(t, err)

	// The shared cloud logbook "main": A's a1 and a3, B's b1 and b2.
	a1 := importQso(t, qsoSvc, lbA, "K1AAA", "120000")
	a2 := importQso(t, qsoSvc, lbA, "W1AW", "120100") // never uploaded
	a3 := importQso(t, qsoSvc, lbA, "G4ABC", "120200")
	b1 := importQso(t, qsoSvc, lbB, "DL9UW", "130000")
	b2 := importQso(t, qsoSvc, lbB, "9A4ZM", "130100")
	for _, u := range []string{a1, a3} {
		drainTo(t, fwdA, dbSvc, u, action.Insert)
	}
	for _, u := range []string{b1, b2} {
		drainTo(t, fwdB, dbSvc, u, action.Insert)
	}
	// a3 deleted locally before either binding is routed: a tombstone the
	// cloud still holds live.
	q3, err := dbSvc.FetchQsoByUUIDWithContext(ctx, a3)
	require.NoError(t, err)
	require.NoError(t, qsoSvc.Delete(ctx, q3, source.Source("test")))
	for _, u := range []string{a1, a2, a3, b1, b2} {
		require.Empty(t, uploadRowsFor(t, dbSvc, u), "fixture: %s starts with no upload row", u)
	}
	qsoSvc.SetDestinationRoutes([]forwarding.BoundForwarder{{LogbookID: lbA, Config: fcA}, {LogbookID: lbB, Config: fcB}})

	recA, err := NewReconciler(fcA, lbA, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)
	recB, err := NewReconciler(fcB, lbB, dbSvc, qsoSvc, logSvc)
	require.NoError(t, err)

	// A repairs its missing a2 and its own tombstone a3; B's rows are cloud-only to it.
	sum, err := recA.RunOnce(ctx, TriggerManual)
	require.NoError(t, err)
	require.Equal(t, 1, sum.EnqueuedUpserts, "%+v", sum)
	require.Equal(t, 1, sum.EnqueuedDeletes, "%+v", sum)
	require.Equal(t, 2, sum.CloudOnly, "B's b1 and b2: %+v", sum)
	require.Equal(t, []string{"smcloud-a"}, uploadRowsFor(t, dbSvc, a2))
	require.Equal(t, []string{"smcloud-a"}, uploadRowsFor(t, dbSvc, a3))
	for _, u := range []string{a1, b1, b2} {
		require.Empty(t, uploadRowsFor(t, dbSvc, u), "A's reconciler queued %s", u)
	}
	drainTo(t, fwdA, dbSvc, a2, action.Insert)
	drainTo(t, fwdA, dbSvc, a3, action.Delete)

	// B, after A's repair drained: its own rows are untouched on the cloud (no
	// repair needed for b1 or b2), only its new b3 is queued, and A's live rows
	// are cloud-only to it.
	b3 := importQso(t, qsoSvc, lbB, "OK1RR", "130200")
	require.Empty(t, uploadRowsFor(t, dbSvc, b3), "fixture: an import queues nothing")
	sum, err = recB.RunOnce(ctx, TriggerManual)
	require.NoError(t, err)
	require.Equal(t, 1, sum.EnqueuedUpserts, "only b3: %+v", sum)
	require.Equal(t, 0, sum.EnqueuedDeletes, "%+v", sum)
	require.Equal(t, []string{"smcloud-b"}, uploadRowsFor(t, dbSvc, b3))
	require.Equal(t, 3, sum.CloudOnly, "A's a1, a2 and its tombstone a3: %+v", sum)
	for _, u := range []string{b1, b2} {
		require.Empty(t, uploadRowsFor(t, dbSvc, u), "B's own %s was disturbed on the cloud and re-queued", u)
	}
	for _, u := range []string{a1, a2} {
		require.NotContains(t, uploadRowsFor(t, dbSvc, u), "smcloud-b", "B's reconciler queued A's %s", u)
	}
}
