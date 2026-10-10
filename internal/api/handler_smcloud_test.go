package api

import (
	"context"
	"encoding/json"
	stderr "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

/*
   POST /v1/smcloud/reconcile, the per-binding aggregate (W-0021 5F.4 commit 2;
   rulings F6, G1–G3).

     AR1  mixed outcomes: 200, set order, each entry exactly one of summary or
          error; a run error's raw text never reaches the wire.
     AR2  every entry a fault (no runnable reconciler): 503 smcloud_unavailable
          with the faults' results beside the envelope.
     AR3  no entries: 503 with "results": [].
     AR4  isolation: both runs start independently (a sequential handler
          deadlocks into the first one's timeout); one deadline is fixed for
          every run when the handler starts; a run that blocks until its
          context ends reads "timed out after 25 s", its peer's summary is
          served, and the blocked run has EXITED before the handler returns.
     AR5  the request cancelled: every run sees its context end and has
          exited before the handler returns; none says "timed out".
     AR6  every runnable reconciler failing is still 200, errors in order.
     AR7  a panicking run becomes the fixed failure; its peer is served.
*/

const (
	arFailed    = "the reconcile pass failed; the daemon log has the cause"
	arTimedOut  = "timed out after 25 s"
	arCancelled = "the request was cancelled before the pass finished"
	arHeld      = "uploads are held until adoption is confirmed for the current station account"
	arUnres     = "the binding cannot be resolved against a station account"
)

type arResult struct {
	ForwarderName string          `json:"forwarder_name"`
	LogbookUUID   string          `json:"logbook_uuid"`
	Summary       json.RawMessage `json:"summary"`
	Error         string          `json:"error"`
}

type arBody struct {
	Code    string     `json:"code"`
	Message string     `json:"message"`
	Results []arResult `json:"results"`
}

func arDo(t *testing.T, srv *Server, ctx context.Context) (*httptest.ResponseRecorder, arBody) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/smcloud/reconcile", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.handleSmcloudReconcile(w, req)
	var b arBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return w, b
}

func arOK(v any) func(context.Context) (any, error) {
	return func(context.Context) (any, error) { return v, nil }
}

func arShortTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := reconcileRunTimeout
	reconcileRunTimeout = d
	t.Cleanup(func() { reconcileRunTimeout = old })
}

// arWant checks names, logbook UUIDs and, per entry, either a summary
// ("" = an error is expected) or the exact error text.
func arWant(t *testing.T, got []arResult, want [][3]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("results = %+v; want %d entries", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.ForwarderName != w[0] || g.LogbookUUID != "lb-"+w[0] {
			t.Fatalf("result %d = %s/%s; want %s/lb-%s (set order)", i, g.ForwarderName, g.LogbookUUID, w[0], w[0])
		}
		hasSummary := len(g.Summary) > 0 && string(g.Summary) != "null"
		if w[1] != "" {
			if !hasSummary || g.Error != "" || !strings.Contains(string(g.Summary), w[1]) {
				t.Fatalf("result %s = summary %s error %q; want only a summary with %s", w[0], g.Summary, g.Error, w[1])
			}
			continue
		}
		if hasSummary || g.Error != w[2] {
			t.Fatalf("result %s = summary %s error %q; want only the error %q", w[0], g.Summary, g.Error, w[2])
		}
	}
}

func entry(name string, run func(context.Context) (any, error), fault string) SmcloudReconcileEntry {
	return SmcloudReconcileEntry{ForwarderName: name, LogbookUUID: "lb-" + name, Run: run, Fault: fault}
}

func TestSmcloudReconcile_AR1_MixedOutcomes(t *testing.T) {
	srv := testServer(t)
	secret := stderr.New(`GET http://user:pw-SECRET@cloud/v1/archives/x/reconcile: token=TOKEN-SECRET HTTP 500 (body: {"detail":"BODY-SECRET"})`)
	srv.SetSmcloudReconcile([]SmcloudReconcileEntry{
		entry("a", arOK(map[string]any{"in_sync": true}), ""),
		entry("b", func(context.Context) (any, error) { return nil, secret }, ""),
		entry("c", nil, arHeld),
		entry("d", nil, arUnres),
	})
	w, b := arDo(t, srv, context.Background())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s; want 200", w.Code, w.Body.String())
	}
	arWant(t, b.Results, [][3]string{{"a", `"in_sync":true`, ""}, {"b", "", arFailed}, {"c", "", arHeld}, {"d", "", arUnres}})
	for _, s := range []string{"SECRET", "pw-", "user:", "cloud/v1"} {
		if strings.Contains(w.Body.String(), s) {
			t.Fatalf("the raw cause reached the wire (%q): %s", s, w.Body.String())
		}
	}
}

func TestSmcloudReconcile_AR2_NoRunnableReconciler(t *testing.T) {
	srv := testServer(t)
	srv.SetSmcloudReconcile([]SmcloudReconcileEntry{entry("c", nil, arHeld), entry("d", nil, arUnres)})
	w, b := arDo(t, srv, context.Background())
	if w.Code != http.StatusServiceUnavailable || b.Code != "smcloud_unavailable" || b.Message == "" {
		t.Fatalf("status = %d body = %s; want 503 smcloud_unavailable", w.Code, w.Body.String())
	}
	arWant(t, b.Results, [][3]string{{"c", "", arHeld}, {"d", "", arUnres}})
}

func TestSmcloudReconcile_AR3_NoBindings(t *testing.T) {
	for name, set := range map[string]bool{"never wired": false, "wired empty": true} {
		t.Run(name, func(t *testing.T) {
			srv := testServer(t)
			if set {
				srv.SetSmcloudReconcile(nil)
			}
			w, b := arDo(t, srv, context.Background())
			if w.Code != http.StatusServiceUnavailable || b.Code != "smcloud_unavailable" {
				t.Fatalf("status = %d body = %s; want 503 smcloud_unavailable", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), `"results":[]`) {
				t.Fatalf("body = %s; want \"results\": [] beside the envelope", w.Body.String())
			}
		})
	}
}

func TestSmcloudReconcile_AR4_SlowBindingIsolation(t *testing.T) {
	t.Run("both start independently", func(t *testing.T) {
		arShortTimeout(t, 2*time.Second)
		srv := testServer(t)
		released := make(chan struct{})
		srv.SetSmcloudReconcile([]SmcloudReconcileEntry{
			entry("a", func(ctx context.Context) (any, error) {
				select {
				case <-released:
					return map[string]any{"run": "a"}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}, ""),
			entry("b", func(context.Context) (any, error) {
				close(released)
				return map[string]any{"run": "b"}, nil
			}, ""),
		})
		w, b := arDo(t, srv, context.Background())
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
		}
		arWant(t, b.Results, [][3]string{{"a", `"run":"a"`, ""}, {"b", `"run":"b"`, ""}})
	})

	t.Run("a run blocked until its deadline", func(t *testing.T) {
		arShortTimeout(t, 300*time.Millisecond)
		srv := testServer(t)
		var exited atomic.Bool
		var mu sync.Mutex
		var deadlines []time.Time
		record := func(ctx context.Context) {
			dl, ok := ctx.Deadline()
			if !ok {
				t.Error("a run's context has no deadline")
			}
			mu.Lock()
			deadlines = append(deadlines, dl)
			mu.Unlock()
		}
		srv.SetSmcloudReconcile([]SmcloudReconcileEntry{
			entry("a", func(ctx context.Context) (any, error) {
				defer exited.Store(true)
				record(ctx)
				<-ctx.Done()
				time.Sleep(50 * time.Millisecond) // a run that winds down after its context ends
				return nil, ctx.Err()
			}, ""),
			entry("b", func(ctx context.Context) (any, error) {
				time.Sleep(100 * time.Millisecond) // scheduled late
				record(ctx)
				return map[string]any{"run": "b"}, nil
			}, ""),
		})
		start := time.Now()
		w, b := arDo(t, srv, context.Background())
		if !exited.Load() {
			t.Fatal("the handler returned before the blocked run exited")
		}
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
		}
		arWant(t, b.Results, [][3]string{{"a", "", arTimedOut}, {"b", `"run":"b"`, ""}})
		mu.Lock()
		defer mu.Unlock()
		if len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) {
			t.Fatalf("deadlines = %v; want one deadline shared by every run", deadlines)
		}
		// Fixed when the handler starts, not when a run is scheduled: run b
		// records 100 ms in, so a per-run deadline would be 100 ms later.
		if off := deadlines[0].Sub(start); off < 300*time.Millisecond || off > 350*time.Millisecond {
			t.Fatalf("deadline is %v after the request; want the 300 ms budget from the handler's start", off)
		}
	})
}

func TestSmcloudReconcile_AR5_RequestCancelled(t *testing.T) {
	srv := testServer(t)
	var started sync.WaitGroup
	started.Add(2)
	var exited atomic.Int32
	block := func(ctx context.Context) (any, error) {
		defer exited.Add(1)
		started.Done()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	srv.SetSmcloudReconcile([]SmcloudReconcileEntry{entry("a", block, ""), entry("b", block, "")})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { started.Wait(); cancel() }()
	done := make(chan struct{})
	var b arBody
	go func() { defer close(done); _, b = arDo(t, srv, ctx) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler did not return after the request was cancelled")
	}
	if n := exited.Load(); n != 2 {
		t.Fatalf("%d of 2 runs exited before the handler returned", n)
	}
	arWant(t, b.Results, [][3]string{{"a", "", arCancelled}, {"b", "", arCancelled}})
}

func TestSmcloudReconcile_AR6_AllRunsFail(t *testing.T) {
	srv := testServer(t)
	fail := func(context.Context) (any, error) { return nil, stderr.New("cloud unreachable") }
	srv.SetSmcloudReconcile([]SmcloudReconcileEntry{entry("a", fail, ""), entry("b", fail, ""), entry("c", nil, arHeld)})
	w, b := arDo(t, srv, context.Background())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s; want 200: a runnable reconciler failing is not unavailability", w.Code, w.Body.String())
	}
	arWant(t, b.Results, [][3]string{{"a", "", arFailed}, {"b", "", arFailed}, {"c", "", arHeld}})
}

func TestSmcloudReconcile_AR7_PanicIsolated(t *testing.T) {
	srv := testServer(t)
	srv.SetSmcloudReconcile([]SmcloudReconcileEntry{
		entry("a", func(context.Context) (any, error) { panic("reconcile exploded (test)") }, ""),
		entry("b", arOK(map[string]any{"run": "b"}), ""),
	})
	w, b := arDo(t, srv, context.Background())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	arWant(t, b.Results, [][3]string{{"a", "", arFailed}, {"b", `"run":"b"`, ""}})
	if strings.Contains(w.Body.String(), "exploded") {
		t.Fatalf("the panic value reached the wire: %s", w.Body.String())
	}
}
