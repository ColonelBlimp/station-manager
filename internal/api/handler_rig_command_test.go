package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// rigTestServer is testServer with an FTdx10 bridge injected but NOT started —
// so the configured rigdef (set_freq/set_mode/PLAYBACK) is resolvable yet
// there is no active serial client. That covers every handler path except the
// 202 happy path, whose encode-and-write half is pinned at the bridge layer
// (bridge.TestSendCommand); the 202 path needs a live serial client, which the
// bridge owns privately.
func rigTestServer(t *testing.T) *Server {
	t.Helper()
	srv := testServer(t)
	srv.bridge = bridge.New(types.BridgeConfig{
		Enabled: true,
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, &logging.Service{})
	return srv
}

func postRigCommand(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/rig/command", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRigCommand(w, req)
	return w
}

func TestHandleRigCommand_Validation(t *testing.T) {
	srv := rigTestServer(t)
	cases := []struct {
		name     string
		body     string
		wantHTTP int
		wantCode string // ErrorResponse.Code
	}{
		{"bad json", `{`, http.StatusBadRequest, "invalid_json"},
		{"missing op", `{"value":"14074000"}`, http.StatusBadRequest, "missing_required_param"},
		{"non-scalar value", `{"op":"set_freq","value":{"x":1}}`, http.StatusBadRequest, "invalid_field_value"},
		{"unknown op", `{"op":"frobnicate","value":"1"}`, http.StatusBadRequest, "rig_unsupported_command"},
		{"not exposed", `{"op":"PLAYBACK","value":"5"}`, http.StatusBadRequest, "rig_unsupported_command"},
		{"unmapped mode", `{"op":"set_mode","value":"NOPE"}`, http.StatusBadRequest, "rig_invalid_value"},
		{"missing value for value-bearing op", `{"op":"set_freq"}`, http.StatusBadRequest, "rig_invalid_value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postRigCommand(t, srv, tc.body)
			if w.Code != tc.wantHTTP {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.wantHTTP, w.Body.String())
			}
			var resp ErrorResponse
			if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
				t.Fatalf("decode error body: %v (%s)", err, w.Body.String())
			}
			if resp.Code != tc.wantCode {
				t.Errorf("error code = %q, want %q", resp.Code, tc.wantCode)
			}
		})
	}
}

// TestHandleRigCommand_NoRig covers well-formed commands (a numeric set_freq
// and a string set_mode) reaching SendCommand and failing with 503 because no
// rig is connected — proving the scalar conversion (number→string) and the
// not-connected mapping. With an active client these would 202; that half is
// covered by bridge.TestSendCommand.
func TestHandleRigCommand_NoRig(t *testing.T) {
	srv := rigTestServer(t)
	cases := []struct{ name, body string }{
		{"set_freq numeric", `{"op":"set_freq","value":14074000}`},
		{"set_mode string", `{"op":"set_mode","value":"DATA-U"}`},
		{"band_up valueless (no value field)", `{"op":"band_up"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postRigCommand(t, srv, tc.body)
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503 (body %s)", w.Code, w.Body.String())
			}
			var resp ErrorResponse
			if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if resp.Code != "rig_not_connected" {
				t.Errorf("error code = %q, want rig_not_connected", resp.Code)
			}
		})
	}
}

func decodeErrCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp ErrorResponse
	if err := unmarshalJSON(w.Body.String(), &resp); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, w.Body.String())
	}
	return resp.Code
}

// TestHandleRigCommand_Batch covers the {commands:[...]} path: a well-formed
// batch reaches SendCommands (503 here, no client), a bad op rejects the whole
// batch, mixing {op,value} with {commands} is ambiguous, and an empty batch is
// treated as a missing command.
func TestHandleRigCommand_Batch(t *testing.T) {
	srv := rigTestServer(t)

	t.Run("valid batch reaches the rig", func(t *testing.T) {
		w := postRigCommand(t, srv, `{"commands":[{"op":"set_freq","value":14074000},{"op":"set_mode","value":"DATA-U"}]}`)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "rig_not_connected" {
			t.Errorf("code = %q, want rig_not_connected", code)
		}
	})

	t.Run("bad op rejects whole batch", func(t *testing.T) {
		w := postRigCommand(t, srv, `{"commands":[{"op":"set_freq","value":14074000},{"op":"frobnicate","value":"x"}]}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "rig_unsupported_command" {
			t.Errorf("code = %q, want rig_unsupported_command", code)
		}
	})

	t.Run("op and commands together is ambiguous", func(t *testing.T) {
		w := postRigCommand(t, srv, `{"op":"set_freq","value":1,"commands":[{"op":"set_mode","value":"USB"}]}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "invalid_field_value" {
			t.Errorf("code = %q, want invalid_field_value", code)
		}
	})

	t.Run("empty batch is a missing command", func(t *testing.T) {
		w := postRigCommand(t, srv, `{"commands":[]}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "missing_required_param" {
			t.Errorf("code = %q, want missing_required_param", code)
		}
	})

	t.Run("batch over the size cap is rejected", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`{"commands":[`)
		for i := 0; i <= maxRigCommandBatch; i++ { // one over the cap
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`{"op":"band_up"}`)
		}
		b.WriteString(`]}`)
		w := postRigCommand(t, srv, b.String())
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "invalid_field_value" {
			t.Errorf("code = %q, want invalid_field_value", code)
		}
	})
}
