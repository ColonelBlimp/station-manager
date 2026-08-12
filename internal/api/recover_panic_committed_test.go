package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// A6 — A PANIC AFTER THE RESPONSE IS COMMITTED MUST BE DISTINGUISHABLE FROM A CLEAN 500.
//
// CRITERION (operator, 2026-08-12):
//
//	When a handler panics AFTER it has already written response bytes, the daemon logs
//	the panic AND records that the response was truncated (response_committed=true), so
//	it can be told apart from a clean 500 (envelope delivered) and from a success — the
//	access log's status alone shows whatever was written first, e.g. 200. And it must
//	NOT append a second 500 envelope onto the partial bytes.
//
// recoverPanic sits INSIDE logRequests in production, so its ResponseWriter is the
// shared *responseRecorder; these tests wrap the same way. Reuses credWarnRecords from
// forwarder_creds_preserve_test.go (same package).

func panicLogServer(t *testing.T) (*Server, *strings.Builder) {
	t.Helper()
	buf := &strings.Builder{}
	srv := testServerWithLogger(t, nil, nil, logging.NewForWriter(buf))
	return srv, buf
}

func panicRecord(t *testing.T, buf *strings.Builder) map[string]any {
	t.Helper()
	recs := credWarnRecords(t, buf, "panic in HTTP handler")
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 panic record, got %d; log:\n%s", len(recs), buf.String())
	}
	return recs[0]
}

// A6-committed — A PANIC AFTER A WRITE IS FLAGGED, AND NO ENVELOPE IS APPENDED.
func TestRecoverPanic_CommittedResponse_FlaggedAndEnvelopeSkipped(t *testing.T) {
	srv, buf := panicLogServer(t)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial-body"))
		panic("boom after commit")
	})
	inner := httptest.NewRecorder()
	rec := newResponseRecorder(inner) // mirror logRequests, which recoverPanic sits inside
	srv.recoverPanic(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	r := panicRecord(t, buf)
	c, ok := r["response_committed"].(bool)
	if !ok {
		t.Fatalf("panic record has no response_committed field: %v", r)
	}
	if !c {
		t.Errorf("response_committed = false, want true (a committed response was truncated)")
	}
	// The 500 envelope must NOT be appended onto the partial body.
	if body := inner.Body.String(); body != "partial-body" {
		t.Errorf("body = %q, want just %q (no 500 envelope appended onto a committed response)",
			body, "partial-body")
	}
	// The access-log classification is tagged so a request logged there as status 200
	// still connects to this panic.
	if rec.errCode != "internal_error" {
		t.Errorf("recorder errCode = %q, want internal_error (connect the truncated response to the panic)",
			rec.errCode)
	}
}

// A6-clean — A PANIC BEFORE ANY WRITE IS FLAGGED not-committed AND STILL GETS A 500.
func TestRecoverPanic_CleanPanic_FlaggedNotCommittedEnvelopeWritten(t *testing.T) {
	srv, buf := panicLogServer(t)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom before any write")
	})
	inner := httptest.NewRecorder()
	rec := newResponseRecorder(inner)
	srv.recoverPanic(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	r := panicRecord(t, buf)
	c, ok := r["response_committed"].(bool)
	if !ok {
		t.Fatalf("panic record has no response_committed field: %v", r)
	}
	if c {
		t.Errorf("response_committed = true, want false for a clean panic")
	}
	// A clean panic still delivers the 500 envelope.
	if inner.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", inner.Code)
	}
	if !strings.Contains(inner.Body.String(), "internal_error") {
		t.Errorf("clean panic must still deliver the 500 envelope; body = %q", inner.Body.String())
	}
}

// A6/flush — A FLUSH COMMITS THE RESPONSE (codex ad3fdf1a P1). An SSE-style handler
// that Flush()es (implicit 200 in net/http) then panics is already on the wire, so it
// must be classified committed and NOT get a 500 envelope appended — even though it
// never called WriteHeader or Write. Before the fix, Flush() left wroteHeader false
// and recoverPanic garbled the stream with a 500 envelope.
func TestRecoverPanic_FlushedResponse_TreatedAsCommitted(t *testing.T) {
	srv, buf := panicLogServer(t)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush() // commit via flush only — no WriteHeader/Write
		panic("boom after flush")
	})
	inner := httptest.NewRecorder()
	rec := newResponseRecorder(inner)
	srv.recoverPanic(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	r := panicRecord(t, buf)
	if c, _ := r["response_committed"].(bool); !c {
		t.Errorf("response_committed = %v, want true (a flush commits the response)", r["response_committed"])
	}
	// No 500 envelope may be appended onto the already-flushed stream.
	if strings.Contains(inner.Body.String(), "internal_error") {
		t.Errorf("a 500 envelope was appended onto a flushed (committed) response: %q", inner.Body.String())
	}
	if rec.errCode != "internal_error" {
		t.Errorf("recorder errCode = %q, want internal_error (connect the truncated stream to the panic)", rec.errCode)
	}
}
