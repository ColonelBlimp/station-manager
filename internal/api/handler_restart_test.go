package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// POST /v1/restart — 503 until cmd/smd wires the trigger; 202 + fires the trigger
// when wired and not transmitting. The 409-while-transmitting branch is covered by
// bridge.TestTxActive plus the handler's TxActive guard.
func TestHandleRestart(t *testing.T) {
	srv := testServer(t)

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/restart", nil)
		w := httptest.NewRecorder()
		srv.handleRestart(w, req)
		return w
	}

	// Not wired → 503 restart_unavailable.
	if w := do(); w.Code != http.StatusServiceUnavailable ||
		!strings.Contains(w.Body.String(), "restart_unavailable") {
		t.Fatalf("unwired: status = %d body = %s", w.Code, w.Body.String())
	}

	// Wired + not transmitting → 202 and the trigger fires.
	called := false
	srv.SetRestart(func() { called = true })
	w := do()
	if w.Code != http.StatusAccepted {
		t.Fatalf("wired: status = %d body = %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("restart trigger was not called on a 202")
	}
}
