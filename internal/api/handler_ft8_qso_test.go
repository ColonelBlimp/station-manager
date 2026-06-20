package api

import (
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
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
	srv.ft8 = ft8.NewService(types.Ft8Config{Enabled: true}, &logging.Service{}, "")
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
			`{"their_call":"K1ABC","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`, srv.handleFt8QsoStart)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "no_station_callsign" {
			t.Fatalf("status=%d code=%q, want 400 no_station_callsign", w.Code, decodeErrCode(t, w))
		}
	})

	t.Run("malformed slot_utc", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "G0TST")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/start",
			`{"their_call":"K1ABC","their_grid":"FN42","slot_utc":"not-a-time","offset_hz":1500}`, srv.handleFt8QsoStart)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "invalid_field_value" {
			t.Fatalf("status=%d code=%q, want 400 invalid_field_value (body %s)", w.Code, decodeErrCode(t, w), w.Body.String())
		}
		// A stable, slot_utc-specific message — never Go's raw time.Parse text.
		if !strings.Contains(w.Body.String(), "slot_utc") {
			t.Errorf("message should name slot_utc; got %s", w.Body.String())
		}
	})

	t.Run("not armed", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "G0TST")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/start",
			`{"their_call":"K1ABC","their_grid":"FN42","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`, srv.handleFt8QsoStart)
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
			`{"their_call":"K1ABC","their_grid":"FN42","their_snr":-12,"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`, srv.handleFt8QsoWork)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "no_station_callsign" {
			t.Fatalf("status=%d code=%q, want 400 no_station_callsign", w.Code, decodeErrCode(t, w))
		}
	})

	t.Run("malformed slot_utc", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "G0TST")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/work",
			`{"their_call":"K1ABC","their_grid":"FN42","their_snr":-12,"slot_utc":"nope","offset_hz":1500}`, srv.handleFt8QsoWork)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "invalid_field_value" {
			t.Fatalf("status=%d code=%q, want 400 invalid_field_value (body %s)", w.Code, decodeErrCode(t, w), w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "slot_utc") {
			t.Errorf("message should name slot_utc; got %s", w.Body.String())
		}
	})

	t.Run("not armed", func(t *testing.T) {
		srv := ft8QsoTestServer(t, "G0TST")
		w := postFt8Qso(t, srv, "/v1/ft8/qso/work",
			`{"their_call":"K1ABC","their_grid":"FN42","their_snr":-12,"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`, srv.handleFt8QsoWork)
		if w.Code != http.StatusConflict || decodeErrCode(t, w) != "ft8_tx_not_armed" {
			t.Fatalf("status=%d code=%q, want 409 ft8_tx_not_armed (body %s)", w.Code, decodeErrCode(t, w), w.Body.String())
		}
	})
}

// TestHandleFt8Qso_RejectsUnloggableFrequency guards review 2026-06-19 M2: a
// sequenced QSO must not be accepted with a frequency that can't be logged
// (0 / out-of-band) — the contact would be made on air and only then fail
// logging. All three start paths enforce it before committing the sequencer.
func TestHandleFt8Qso_RejectsUnloggableFrequency(t *testing.T) {
	cases := []struct {
		name, path, body string
		h                func(*Server) http.HandlerFunc
	}{
		{"qso/start missing freq", "/v1/ft8/qso/start",
			`{"their_call":"K1ABC","their_grid":"FN42","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500}`,
			func(s *Server) http.HandlerFunc { return s.handleFt8QsoStart }},
		{"qso/start zero freq", "/v1/ft8/qso/start",
			`{"their_call":"K1ABC","their_grid":"FN42","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":0}`,
			func(s *Server) http.HandlerFunc { return s.handleFt8QsoStart }},
		{"qso/start out-of-band freq", "/v1/ft8/qso/start",
			`{"their_call":"K1ABC","their_grid":"FN42","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":9999.0}`,
			func(s *Server) http.HandlerFunc { return s.handleFt8QsoStart }},
		{"cq/start missing freq", "/v1/ft8/cq/start",
			`{"offset_hz":1500}`,
			func(s *Server) http.HandlerFunc { return s.handleFt8CqStart }},
		{"qso/work missing freq", "/v1/ft8/qso/work",
			`{"their_call":"K1ABC","their_grid":"FN42","their_snr":-12,"slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500}`,
			func(s *Server) http.HandlerFunc { return s.handleFt8QsoWork }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := ft8QsoTestServer(t, "G0TST")
			w := postFt8Qso(t, srv, tc.path, tc.body, tc.h(srv))
			if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "ft8_no_frequency" {
				t.Fatalf("status=%d code=%q, want 400 ft8_no_frequency (body %s)", w.Code, decodeErrCode(t, w), w.Body.String())
			}
		})
	}
}

// TestWriteFt8QsoError_Mapping pins the error-classification surface directly
// (review 2026-06-19 L2): each known FT8 sentinel maps to its stable
// status+code, and an unknown error becomes a generic 500 internal_error that
// does NOT leak the raw error text to the client (review 2026-06-19 M2). Driving
// every sentinel through the live service would need a rig, so the mapper is
// exercised here rather than only indirectly via the ft8_tx_not_armed path.
func TestWriteFt8QsoError_Mapping(t *testing.T) {
	srv := testServer(t)
	const op errors.Op = "api.test"

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"not armed", ft8.ErrTxNotArmed, http.StatusConflict, "ft8_tx_not_armed"},
		{"not ready", ft8.ErrTxNotReady, http.StatusServiceUnavailable, "rig_not_ready"},
		{"in progress", ft8.ErrQsoInProgress, http.StatusConflict, "ft8_qso_in_progress"},
		{"no offset", ft8.ErrNoOffset, http.StatusBadRequest, "ft8_no_offset"},
		{"bad message", ft8.ErrTxBadMessage, http.StatusBadRequest, "ft8_tx_bad_message"},
		{"caller mode unsupported", ft8.ErrCallerAnswerModeUnsupported, http.StatusNotImplemented, "ft8_caller_mode_unsupported"},
		{"unknown maps to generic 500", stderrors.New("boom: internal detail"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.writeFt8QsoError(w, op, tc.err)
			if w.Code != tc.wantStatus || decodeErrCode(t, w) != tc.wantCode {
				t.Fatalf("status=%d code=%q, want %d %q (body %s)",
					w.Code, decodeErrCode(t, w), tc.wantStatus, tc.wantCode, w.Body.String())
			}
			if tc.wantCode == "internal_error" && strings.Contains(w.Body.String(), "boom") {
				t.Errorf("generic 500 must not leak raw error text; got %s", w.Body.String())
			}
		})
	}
}

func TestHandleFt8QsoAbandon(t *testing.T) {
	srv := ft8QsoTestServer(t, "G0TST")
	w := postFt8Qso(t, srv, "/v1/ft8/qso/abandon", ``, srv.handleFt8QsoAbandon)
	if w.Code != http.StatusAccepted {
		t.Fatalf("abandon status=%d, want 202", w.Code)
	}
}
