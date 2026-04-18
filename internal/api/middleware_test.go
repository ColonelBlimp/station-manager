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
