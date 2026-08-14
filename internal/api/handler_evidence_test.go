package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/evidence"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// GET /v1/evidence/status — the §4.1 local honesty surface. Three states the
// operator must be able to tell apart from this one endpoint: no writer
// wired (or capture off) → disabled; a live writer → capturing with real
// usage/counts; and the route always answers, so "capture off" can never be
// confused with "endpoint missing".
func TestHandleEvidenceStatus_DisabledWithoutWriter(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/evidence/status", nil)
	w := httptest.NewRecorder()
	srv.handleEvidenceStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var st evidence.Status
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Enabled || st.State != evidence.StateDisabled {
		t.Fatalf("status = %+v, want disabled", st)
	}
}

func TestHandleEvidenceStatus_LiveWriter(t *testing.T) {
	srv := testServer(t)

	ev := evidence.New(evidence.Config{
		Capture:  true,
		CapBytes: 524288000,
		Path:     filepath.Join(t.TempDir(), "evidence.db"),
	}, logging.Noop())
	if err := ev.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := ev.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(ev.Stop)
	srv.SetEvidence(ev)

	req := httptest.NewRequest(http.MethodGet, "/v1/evidence/status", nil)
	w := httptest.NewRecorder()
	srv.handleEvidenceStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var st evidence.Status
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !st.Enabled || st.State != evidence.StateCapturing {
		t.Fatalf("status = %+v, want enabled/capturing", st)
	}
	if st.CapBytes != 524288000 || st.WatermarkBytes <= 0 || st.UsageBytes == nil || *st.UsageBytes <= 0 {
		t.Fatalf("status sizes = %+v, want real cap/watermark/usage", st)
	}
}
