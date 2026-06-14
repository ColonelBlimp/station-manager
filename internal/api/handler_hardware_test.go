package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleHardware_OKAndShape verifies GET /v1/hardware always returns 200
// with the documented shape, regardless of what hardware the test host has.
// Audio enumeration may be unavailable (CGO-free build) or empty (headless
// CI) — the contract is that the endpoint still answers 200 with non-null
// arrays and an audio.available flag, so the SPA can render an empty picker
// rather than choke on null.
func TestHandleHardware_OKAndShape(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/hardware", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", w.Code, w.Body.String())
	}

	var resp struct {
		SerialPorts []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"serial_ports"`
		Audio struct {
			Available bool `json:"available"`
			Capture   []struct {
				Name string `json:"name"`
			} `json:"capture"`
			Playback []struct {
				Name string `json:"name"`
			} `json:"playback"`
		} `json:"audio"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%q", err, w.Body.String())
	}

	// Non-null slices are part of the contract (handler seeds []).
	if resp.SerialPorts == nil {
		t.Error("serial_ports is null; want [] (non-nil)")
	}
	if resp.Audio.Capture == nil || resp.Audio.Playback == nil {
		t.Error("audio.capture/playback is null; want [] (non-nil)")
	}
	// Every serial entry must carry both an ID and a friendly label.
	for i, p := range resp.SerialPorts {
		if p.ID == "" || p.Label == "" {
			t.Errorf("serial_ports[%d]: id=%q label=%q, both must be non-empty", i, p.ID, p.Label)
		}
	}
}
