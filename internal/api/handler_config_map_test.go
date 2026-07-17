package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestHandleGetConfig_Map: GET serves the map block (contacts-map band-colour
// overrides). Served raw + sparse — stored overrides round-trip; an unset
// block reports the zero value (the SPA applies its built-in palette).
func TestHandleGetConfig_Map(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Map = types.MapConfig{BandColors: map[string]string{"20m": "#22c55e"}}
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.MapDisplay == nil {
		t.Fatal("map block missing from GET")
	}
	if resp.MapDisplay.BandColors["20m"] != "#22c55e" {
		t.Errorf("map.band_colors not round-tripped: %+v", resp.MapDisplay)
	}
}

// TestHandlePutConfig_MapRoundTrip: a PUT persists the block; the response
// (and the stored config) reflect it.
func TestHandlePutConfig_MapRoundTrip(t *testing.T) {
	srv := testServer(t)

	body := `{"map":{"band_colors":{"40m":"#eab308","70cm":"#ec4899"}}}`
	w := putConfigSmtp(t, srv, body) // shared PUT helper (handler_config_smtp_test.go)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Map
	if got.BandColors["40m"] != "#eab308" || got.BandColors["70cm"] != "#ec4899" {
		t.Errorf("map.band_colors not persisted: %+v", got)
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.MapDisplay == nil || resp.MapDisplay.BandColors["40m"] != "#eab308" {
		t.Errorf("map block not in PUT response: %+v", resp.MapDisplay)
	}
}

// TestHandlePutConfig_MapBadValuesReturn400: a non-band key or a non-hex
// colour is rejected by validateMap.
func TestHandlePutConfig_MapBadValuesReturn400(t *testing.T) {
	srv := testServer(t)

	for _, body := range []string{
		`{"map":{"band_colors":{"20M":"#22c55e"}}}`,   // uppercase key
		`{"map":{"band_colors":{"HF":"#22c55e"}}}`,    // not a band token
		`{"map":{"band_colors":{"20m":"green"}}}`,     // not a hex colour
		`{"map":{"band_colors":{"20m":"#22c55e80"}}}`, // 8-digit hex
	} {
		w := putConfigSmtp(t, srv, body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400 (%s)", body, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "invalid_map") {
			t.Errorf("body %s: response %s, want invalid_map code", body, w.Body.String())
		}
	}
}

// TestHandlePutConfig_MapPresenceAware: a PUT that omits `map` leaves the
// stored block untouched; one that carries it replaces the whole block.
func TestHandlePutConfig_MapPresenceAware(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Map = types.MapConfig{BandColors: map[string]string{"20m": "#22c55e"}}
	})

	body := `{"logging_station":{"station_callsign":"M0XYZ"}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := srv.cfg.Snapshot().Map; got.BandColors["20m"] != "#22c55e" {
		t.Errorf("map clobbered by a map-less PUT: %+v", got)
	}

	// Carrying the block replaces it wholesale — dropping an override works.
	w = putConfigSmtp(t, srv, `{"map":{"band_colors":{"40m":"#eab308"}}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	got := srv.cfg.Snapshot().Map
	if _, still := got.BandColors["20m"]; still || got.BandColors["40m"] != "#eab308" {
		t.Errorf("map block not replaced: %+v", got)
	}
}
