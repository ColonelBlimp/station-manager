package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleRigs_OKAndShape verifies GET /v1/rigs returns 200 with the editor
// surface: the configured rigs (overrides intact) and the embedded rigdef
// catalogue (defaults). The catalogue is the deterministic part — the embedded
// rigdefs ship with the binary — so it carries the substantive assertions.
func TestHandleRigs_OKAndShape(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/rigs", nil)
	req.Host = "127.0.0.1:8080" // loopback: the Host allowlist now covers safe methods too (ST-1)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", w.Code, w.Body.String())
	}

	type catEntry struct {
		ID           string              `json:"id"`
		Name         string              `json:"name"`
		Ft8Mode      string              `json:"ft8_mode"`
		RigModes     []string            `json:"rig_modes"`
		ModeMappings map[string]struct{} `json:"mode_mappings"`
		Serial       struct {
			BaudRate int `json:"baud_rate"`
		} `json:"serial"`
	}
	var resp struct {
		DefaultRigID int64 `json:"default_rig_id"`
		Rigs         []struct {
			ID    int64  `json:"id"`
			Model string `json:"model"`
		} `json:"rigs"`
		Catalogue []catEntry `json:"catalogue"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%q", err, w.Body.String())
	}

	if resp.Rigs == nil {
		t.Error("rigs is null; want [] (non-nil)")
	}
	if len(resp.Catalogue) == 0 {
		t.Fatal("catalogue is empty; the embedded rigdefs should always be present")
	}

	// The FTdx10 rigdef must be in the catalogue with its known defaults.
	var ftdx10 *catEntry
	for i := range resp.Catalogue {
		if resp.Catalogue[i].ID == "yaesu-ftdx10" {
			ftdx10 = &resp.Catalogue[i]
		}
	}
	if ftdx10 == nil {
		t.Fatal("catalogue missing yaesu-ftdx10")
	}
	if ftdx10.Ft8Mode != "DATA-U" {
		t.Errorf("ftdx10 ft8_mode = %q, want DATA-U", ftdx10.Ft8Mode)
	}
	if len(ftdx10.RigModes) == 0 {
		t.Error("ftdx10 rig_modes is empty")
	}
	if len(ftdx10.ModeMappings) == 0 {
		t.Error("ftdx10 mode_mappings is empty")
	}
	if ftdx10.Serial.BaudRate <= 0 {
		t.Error("ftdx10 serial.baud_rate is zero; rigdef serial defaults should be present")
	}
	if ftdx10.Name == "" {
		t.Error("ftdx10 name is empty (the SPA derives default MY_RIG from it)")
	}

	// The CAT command/state tables must not leak into the editor payload.
	body := w.Body.String()
	if strings.Contains(body, `"commands"`) || strings.Contains(body, `"states"`) {
		t.Error("payload leaked CAT command/state tables; the editor projection should omit them")
	}
}
