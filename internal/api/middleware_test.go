package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRecoverPanic_CatchesAndReturns500 covers the happy recovery
// path: a handler that panics before writing a response is caught
// by the middleware, the daemon stays alive, and the client sees a
// 500 `internal_error` envelope. The panic value itself must NOT
// leak into the response body.
func TestRecoverPanic_CatchesAndReturns500(t *testing.T) {
	srv := testServer(t)

	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("deliberate test panic: sensitive-internal-secret")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic-test", nil)
	w := httptest.NewRecorder()
	srv.recoverPanic(panicking).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s",
			w.Code, http.StatusInternalServerError, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"code":"internal_error"`) {
		t.Fatalf("body = %q, want internal_error code", body)
	}
	// Panic value must not leak to the client.
	if strings.Contains(body, "sensitive-internal-secret") {
		t.Fatalf("panic value leaked into response body: %s", body)
	}
}

// TestRecoverPanic_NoPanicPassesThrough verifies the no-panic
// fast path: the middleware is a pure pass-through when the
// wrapped handler returns normally.
func TestRecoverPanic_NoPanicPassesThrough(t *testing.T) {
	srv := testServer(t)

	normal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/ok-test", nil)
	w := httptest.NewRecorder()
	srv.recoverPanic(normal).ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTeapot)
	}
	if w.Body.String() != "ok" {
		t.Fatalf("body = %q, want ok", w.Body.String())
	}
}

// TestResponseRecorder_DefaultStatusIs200 verifies the recorder
// reports 200 when the wrapped handler writes a body without an
// explicit WriteHeader call. Mirrors net/http's implicit-200
// behaviour; load-bearing for the access-log line because a 200
// response must show up in the log as 200, not 0.
func TestResponseRecorder_DefaultStatusIs200(t *testing.T) {
	w := httptest.NewRecorder()
	rec := newResponseRecorder(w)
	if _, err := rec.Write([]byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if rec.status != http.StatusOK {
		t.Fatalf("recorded status = %d, want 200", rec.status)
	}
	if rec.bytes != 2 {
		t.Fatalf("recorded bytes = %d, want 2", rec.bytes)
	}
}

// TestResponseRecorder_CapturesExplicitStatus verifies the recorder
// pins the first WriteHeader call. A second WriteHeader (which
// net/http warns about but still no-ops) must not overwrite the
// recorded status — the recorded value should always be the one
// the client actually saw on the wire.
func TestResponseRecorder_CapturesExplicitStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rec := newResponseRecorder(w)
	rec.WriteHeader(http.StatusNotFound)
	rec.WriteHeader(http.StatusInternalServerError) // should be ignored
	if rec.status != http.StatusNotFound {
		t.Fatalf("recorded status = %d, want 404 (first WriteHeader sticks)", rec.status)
	}
}

// TestResponseRecorder_FlushPassesThrough — SSE handlers assert
// http.Flusher on the writer. The recorder must implement Flush so
// the assertion succeeds and the chunked stream actually flushes.
// Verifies via httptest.NewRecorder which itself implements Flusher.
func TestResponseRecorder_FlushPassesThrough(t *testing.T) {
	w := httptest.NewRecorder()
	rec := newResponseRecorder(w)
	if _, ok := any(rec).(http.Flusher); !ok {
		t.Fatalf("responseRecorder does not implement http.Flusher; SSE will break")
	}
	// Calling Flush should be a no-op when the underlying writer
	// implements it (httptest.NewRecorder does); the assertion above
	// is what actually matters for the SSE assertion path.
	rec.Flush()
}

// TestLogRequests_PassesThroughResponse verifies the access-log
// middleware doesn't corrupt the response. The status, body, and
// headers the handler writes must reach the client unchanged; the
// middleware's only side-effect is the log line (not asserted here
// — the logging service has its own tests).
func TestLogRequests_PassesThroughResponse(t *testing.T) {
	srv := testServer(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("pass-through"))
	})

	req := httptest.NewRequest(http.MethodGet, "/log-test", nil)
	w := httptest.NewRecorder()
	srv.logRequests(handler).ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTeapot)
	}
	if w.Header().Get("X-Test") != "yes" {
		t.Fatalf("X-Test header lost in middleware")
	}
	if w.Body.String() != "pass-through" {
		t.Fatalf("body = %q, want pass-through", w.Body.String())
	}
}

// TestClientIP_StripsPort confirms the helper returns just the IP
// address, not the host:port form that RemoteAddr carries.
func TestClientIP_StripsPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	got := clientIP(req)
	if got != "127.0.0.1" {
		t.Fatalf("clientIP = %q, want 127.0.0.1", got)
	}
}

// TestClientIP_HonoursXForwardedForFirstHop covers the LAN
// reverse-proxy case: when X-Forwarded-For is present, the helper
// returns the first hop (the original client's address) rather than
// the proxy's RemoteAddr.
func TestClientIP_HonoursXForwardedForFirstHop(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:443"
	req.Header.Set("X-Forwarded-For", "192.168.1.50, 10.0.0.1")
	got := clientIP(req)
	if got != "192.168.1.50" {
		t.Fatalf("clientIP = %q, want 192.168.1.50 (first XFF hop)", got)
	}
}

// TestResponseRecorder_NoteErrorPropagatesFromWriteError confirms
// the wiring used by the access log: a handler that calls
// s.writeError(w, ...) through the recorder leaves errCode /
// errMessage / errOp populated for logRequests to pick up.
func TestResponseRecorder_NoteErrorPropagatesFromWriteError(t *testing.T) {
	srv := testServer(t)

	w := httptest.NewRecorder()
	rec := newResponseRecorder(w)
	srv.writeError(rec, http.StatusNotFound, "logbook_not_found",
		"logbook does not exist", "api.handleSubmitQso")

	if rec.errCode != "logbook_not_found" {
		t.Fatalf("errCode = %q, want logbook_not_found", rec.errCode)
	}
	if rec.errMessage != "logbook does not exist" {
		t.Fatalf("errMessage = %q, want 'logbook does not exist'", rec.errMessage)
	}
	if rec.errOp != "api.handleSubmitQso" {
		t.Fatalf("errOp = %q, want api.handleSubmitQso", rec.errOp)
	}
	// First call wins — a buggy handler that calls writeError twice
	// must not corrupt the access-log classification with the
	// second envelope's values.
	srv.writeError(rec, http.StatusBadRequest, "second_error", "second", "api.other")
	if rec.errCode != "logbook_not_found" {
		t.Fatalf("errCode after second writeError = %q, want first to win", rec.errCode)
	}
}
