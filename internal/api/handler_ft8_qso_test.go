package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ft8QsoTestServer is testServer with an enabled (keyer-less) FT8 service and a
// station callsign configured. No keyer ⇒ TX never armed ⇒ StartQso maps to
// ft8_tx_not_armed — the deepest handler path reachable without a rig. The armed
// happy path is pinned at the sequencer layer (ft8.TestSequencer_*).
func ft8QsoTestServer(t *testing.T, callsign string) *Server {
	t.Helper()
	srv := testServer(t)
	srv.ft8 = ft8.NewService(types.Ft8Config{Enabled: true}, &logging.Service{})
	if callsign != "" {
		if err := srv.cfg.Update(func(c *config.Config) error {
			c.LoggingStation.StationCallsign = callsign
			return nil
		}); err != nil {
			t.Fatalf("set callsign: %v", err)
		}
	}
	return srv
}

func postFt8Qso(t *testing.T, srv *Server, path, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestHandleFt8QsoStart(t *testing.T) {
	t.Run("bad json", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "G0TST")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/start", `{`, srv.handleFt8QsoStart)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "invalid_json" {
			t.Fatalf("status=%d code=%q, want 400 invalid_json (body %s)", w.Code, decodeErrCode(t, w), w.Body.String())
		}
	})

	t.Run("missing their_call", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "G0TST")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/start", `{"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500}`, srv.handleFt8QsoStart)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "invalid_field_value" {
			t.Fatalf("status=%d code=%q, want 400 invalid_field_value", w.Code, decodeErrCode(t, w))
		}
	})

	t.Run("no station callsign configured", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "") // no callsign
		w := postFt8Qso(t, srv, "/v1/ft8/qso/start",
			`{"their_call":"K1ABC","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500}`, srv.handleFt8QsoStart)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "no_station_callsign" {
			t.Fatalf("status=%d code=%q, want 400 no_station_callsign", w.Code, decodeErrCode(t, w))
		}
	})

	t.Run("not armed", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "G0TST")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/start",
			`{"their_call":"K1ABC","their_grid":"FN42","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500}`, srv.handleFt8QsoStart)
		if w.Code != http.StatusConflict || decodeErrCode(t, w) != "ft8_tx_not_armed" {
			t.Fatalf("status=%d code=%q, want 409 ft8_tx_not_armed (body %s)", w.Code, decodeErrCode(t, w), w.Body.String())
		}
	})
}

func TestHandleFt8QsoWork(t *testing.T) {
	t.Run("missing their_call", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "G0TST")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/work", `{"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500}`, srv.handleFt8QsoWork)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "invalid_field_value" {
			t.Fatalf("status=%d code=%q, want 400 invalid_field_value", w.Code, decodeErrCode(t, w))
		}
	})

	t.Run("no station callsign configured", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/work",
			`{"their_call":"K1ABC","their_grid":"FN42","their_snr":-12,"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500}`, srv.handleFt8QsoWork)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "no_station_callsign" {
			t.Fatalf("status=%d code=%q, want 400 no_station_callsign", w.Code, decodeErrCode(t, w))
		}
	})

	t.Run("not armed", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "G0TST")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/work",
			`{"their_call":"K1ABC","their_grid":"FN42","their_snr":-12,"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500}`, srv.handleFt8QsoWork)
		if w.Code != http.StatusConflict || decodeErrCode(t, w) != "ft8_tx_not_armed" {
			t.Fatalf("status=%d code=%q, want 409 ft8_tx_not_armed (body %s)", w.Code, decodeErrCode(t, w), w.Body.String())
		}
	})
}

func TestHandleFt8QsoAbandon(t *testing.T) {
	srv := ft8QsoTestServer(t, "G0TST")
	w := postFt8Qso(t, srv, "/v1/ft8/qso/abandon", ``, srv.handleFt8QsoAbandon)
	if w.Code != http.StatusAccepted {
		t.Fatalf("abandon status=%d, want 202", w.Code)
	}
}
