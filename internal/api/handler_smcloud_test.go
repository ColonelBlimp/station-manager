package api

import (
	"context"
	"encoding/json"
	stderr "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// POST /v1/smcloud/reconcile — 503 until cmd/smd wires a reconciler; runs the
// injected pass and returns its summary once wired; a failed run is a 500.
func TestHandleSmcloudReconcile(t *testing.T) {
	srv := testServer(t)

	do := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/smcloud/reconcile", nil)
		w := httptest.NewRecorder()
		srv.handleSmcloudReconcile(w, req)
		return w
	}

	// Not wired → 503 smcloud_unavailable.
	if w := do(); w.Code != http.StatusServiceUnavailable ||
		!strings.Contains(w.Body.String(), "smcloud_unavailable") {
		t.Fatalf("unwired: status = %d body = %s", w.Code, w.Body.String())
	}

	// Wired → the pass runs and its summary is served.
	srv.SetSmcloudReconcile(func(ctx context.Context) (any, error) {
		return map[string]any{"in_sync": true, "local_count": 42}, nil
	})
	w := do()
	if w.Code != http.StatusOK {
		t.Fatalf("wired: status = %d body = %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["in_sync"] != true || out["local_count"] != float64(42) {
		t.Fatalf("summary not served: %v", out)
	}

	// A failing run is a 500 with the error envelope.
	srv.SetSmcloudReconcile(func(ctx context.Context) (any, error) {
		return nil, stderr.New("cloud unreachable")
	})
	if w := do(); w.Code != http.StatusInternalServerError ||
		!strings.Contains(w.Body.String(), "reconcile_failed") {
		t.Fatalf("failing run: status = %d body = %s", w.Code, w.Body.String())
	}
}
