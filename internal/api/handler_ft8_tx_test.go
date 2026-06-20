package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ft8TxTestServer is testServer with an enabled (but keyer-less) FT8 service.
// No keyer is wired, so arming maps to ErrTxUnavailable — that covers the
// handler paths reachable without a rig. The armed/ready/in-flight happy paths
// need fakes the Service owns privately and are pinned at the ft8 layer
// (ft8.TestArmTx_*, ft8.TestTransmitNext_*).
func ft8TxTestServer(t *testing.T) *Server {
	t.Helper()
	srv := testServer(t)
	srv.ft8 = ft8.NewService(types.Ft8Config{Enabled: true}, &logging.Service{}, "")
	return srv
}

func postFt8Tx(t *testing.T, srv *Server, path, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestHandleFt8TxArm(t *testing.T) {
	srv := ft8TxTestServer(t)

	t.Run("bad json", func(t *testing.T) {
		w := postFt8Tx(t, srv, "/v1/ft8/tx/arm", `{`, srv.handleFt8TxArm)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "invalid_json" {
			t.Errorf("code = %q, want invalid_json", code)
		}
	})

	t.Run("arm without a keyer is unavailable", func(t *testing.T) {
		w := postFt8Tx(t, srv, "/v1/ft8/tx/arm", `{"armed":true}`, srv.handleFt8TxArm)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "ft8_tx_unavailable" {
			t.Errorf("code = %q, want ft8_tx_unavailable", code)
		}
	})

	t.Run("disarm is an idempotent no-op", func(t *testing.T) {
		w := postFt8Tx(t, srv, "/v1/ft8/tx/arm", `{"armed":false}`, srv.handleFt8TxArm)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202 (body %s)", w.Code, w.Body.String())
		}
	})
}

func TestHandleFt8TxSend(t *testing.T) {
	srv := ft8TxTestServer(t)

	t.Run("bad json", func(t *testing.T) {
		w := postFt8Tx(t, srv, "/v1/ft8/tx/send", `{`, srv.handleFt8TxSend)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "invalid_json" {
			t.Errorf("code = %q, want invalid_json", code)
		}
	})

	t.Run("non-encodable message is a 400", func(t *testing.T) {
		w := postFt8Tx(t, srv, "/v1/ft8/tx/send",
			`{"message":"this is not a standard ft8 message","offset_hz":1500}`, srv.handleFt8TxSend)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "ft8_tx_bad_message" {
			t.Errorf("code = %q, want ft8_tx_bad_message", code)
		}
	})

	t.Run("a valid message while disarmed is a conflict", func(t *testing.T) {
		w := postFt8Tx(t, srv, "/v1/ft8/tx/send",
			`{"message":"CQ G0XYZ IO91","offset_hz":1500}`, srv.handleFt8TxSend)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "ft8_tx_not_armed" {
			t.Errorf("code = %q, want ft8_tx_not_armed", code)
		}
	})
}
