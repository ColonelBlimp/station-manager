package server

// L6 slice A acceptance — a cloud handler panic must surface structured, not as a
// dropped connection, and must be joinable to its access record.
//
// Confusable states (the finding's own): an application panic vs a transport
// disconnect (a client drop doesn't panic — it surfaces on the normal write-error
// path, e.g. the export "aborted mid-stream" line), and a clean 500 vs a response
// truncated by a panic after bytes were already on the wire.
//
// Criteria (observable: the HTTP status a client sees, the response body, the log):
//   A1 — a handler panic yields HTTP 500 (server stays up), and ONE structured
//        "panic in HTTP handler" line carrying request_id, tenant_id, the recovered
//        value, and a stack — never the panic detail in the client body.
//   A2 — wired in the real chain INSIDE accessLog, so the panic line's request_id
//        matches the request's assigned correlation id.
//   A3 — a panic after the response was partially written records
//        response_committed=true and does NOT append a second 500 envelope.
//   A4 — a non-panicking handler is passed through untouched.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func panicTestServer(buf *bytes.Buffer) *Server {
	// Debug level so the chain test can also see /v1/health's access line, which
	// access.go intentionally downgrades to Debug (health/version are polled).
	log := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return New(nil, nil, log, map[string]int64{"tok": 1}, "test-version", 0)
}

// withCorrelation injects the per-request correlation id + authenticated tenant the
// outer middleware would have set, so an isolated recoverPanic test can assert the
// panic line carries them.
func withCorrelation(r *http.Request, id string, tenant int64) *http.Request {
	ctx := context.WithValue(r.Context(), reqInfoKey{}, &reqInfo{id: id, tenant: tenant})
	ctx = context.WithValue(ctx, tenantKey{}, tenant)
	return r.WithContext(ctx)
}

// findLog returns the first JSON log line whose msg == want, or fails.
func findLog(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(buf.String(), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if msg, _ := m["msg"].(string); msg == want {
			return m
		}
	}
	t.Fatalf("no log line with msg %q; log:\n%s", want, buf.String())
	return nil
}

func TestRecoverPanic_CatchesAndReturns500(t *testing.T) {
	var buf bytes.Buffer
	srv := panicTestServer(&buf)

	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("deliberate test panic: sensitive-internal-secret")
	})
	req := withCorrelation(httptest.NewRequest(http.MethodGet, "/panic-test", nil), "rid-catch-000001", 42)
	w := httptest.NewRecorder()

	srv.recoverPanic(panicking).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("body = %q, want internal_error code", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sensitive-internal-secret") {
		t.Fatalf("panic value leaked into client body: %s", w.Body.String())
	}

	m := findLog(t, &buf, "panic in HTTP handler")
	if lvl, _ := m["level"].(string); lvl != "ERROR" {
		t.Errorf("level = %q, want ERROR", lvl)
	}
	if id, _ := m["request_id"].(string); id != "rid-catch-000001" {
		t.Errorf("request_id = %q, want rid-catch-000001", id)
	}
	if tid, _ := m["tenant_id"].(float64); int64(tid) != 42 {
		t.Errorf("tenant_id = %v, want 42", m["tenant_id"])
	}
	if _, ok := m["stack"]; !ok {
		t.Errorf("panic line has no stack field: %v", m)
	}
	if pv, _ := m["panic"].(string); !strings.Contains(pv, "sensitive-internal-secret") {
		t.Errorf("panic value not captured in log: %v", m["panic"])
	}
}

func TestRecoverPanic_NoPanicPassesThrough(t *testing.T) {
	var buf bytes.Buffer
	srv := panicTestServer(&buf)

	normal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	})
	w := httptest.NewRecorder()
	srv.recoverPanic(normal).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if w.Code != http.StatusTeapot || w.Body.String() != "ok" {
		t.Fatalf("passthrough broken: status=%d body=%q", w.Code, w.Body.String())
	}
	if strings.Contains(buf.String(), "panic in HTTP handler") {
		t.Errorf("panic line logged for a non-panicking handler:\n%s", buf.String())
	}
}

func TestRecoverPanic_CommittedResponse_FlaggedAndEnvelopeSkipped(t *testing.T) {
	var buf bytes.Buffer
	srv := panicTestServer(&buf)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("boom after partial write")
	})
	inner := httptest.NewRecorder()
	rec := newAccessRecorder(inner) // mirror the chain: recoverPanic's writer tracks committed
	req := withCorrelation(httptest.NewRequest(http.MethodGet, "/x", nil), "rid-committed-01", 7)

	srv.recoverPanic(h).ServeHTTP(rec, req)

	m := findLog(t, &buf, "panic in HTTP handler")
	if c, _ := m["response_committed"].(bool); !c {
		t.Errorf("response_committed = %v, want true", m["response_committed"])
	}
	// No second envelope appended onto the partial body.
	if inner.Body.String() != "partial" {
		t.Errorf("body = %q, want just \"partial\" (500 envelope must not be appended)", inner.Body.String())
	}
	if strings.Contains(inner.Body.String(), "internal_error") {
		t.Errorf("500 envelope garbled the committed response: %q", inner.Body.String())
	}
}

// A2: full chain. GET /v1/health on a nil-db server is a reliable panic source
// (handleHealth dereferences the nil *sql.DB), so this exercises the real
// accessLog → gzip → recoverPanic path and proves the panic line's request_id is
// the id accessLog assigned to the request.
func TestHandler_RecoversHandlerPanicInChain(t *testing.T) {
	var buf bytes.Buffer
	srv := panicTestServer(&buf)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (panic recovered in chain)", resp.StatusCode)
	}
	panicLine := findLog(t, &buf, "panic in HTTP handler")
	access := findLog(t, &buf, "http request")
	pid, _ := panicLine["request_id"].(string)
	aid, _ := access["request_id"].(string)
	if pid == "" || pid != aid {
		t.Fatalf("panic request_id %q must equal access request_id %q (joinable)", pid, aid)
	}
}
