package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// POST /v1/ft8/claim (ADR 0080, W-0019 slice 3): the wire shape of the profile
// claim — 200 with the active profile, 400 for an unknown or missing mode, and
// 409 with the distinct code (plus retry_after_ms inside a linger) when the
// other profile has anything live.
func TestFt8Claim_Wire(t *testing.T) {
	ft8Svc := ft8.NewService(types.Ft8Config{Enabled: true}, logging.Noop(), t.TempDir())
	srv := testServerWithFt8(t, nil, ft8Svc)

	t.Run("same profile is 200", func(t *testing.T) {
		w := postFt8Qso(t, srv, "/v1/ft8/claim", `{"mode":"ft8"}`, srv.handleFt8Claim)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, "FT8", body["mode"])
	})

	t.Run("switch to FT4 while idle is 200", func(t *testing.T) {
		w := postFt8Qso(t, srv, "/v1/ft8/claim", `{"mode":"ft4"}`, srv.handleFt8Claim)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, "FT4", body["mode"])
		require.Equal(t, "FT4", ft8Svc.Profile().Name)
	})

	t.Run("unknown mode is 400", func(t *testing.T) {
		w := postFt8Qso(t, srv, "/v1/ft8/claim", `{"mode":"ft9"}`, srv.handleFt8Claim)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), `"ft8_profile_unknown"`)
	})

	t.Run("missing mode is 400", func(t *testing.T) {
		w := postFt8Qso(t, srv, "/v1/ft8/claim", `{}`, srv.handleFt8Claim)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), `"missing_required_field"`)
	})

	t.Run("a subscriber on the other profile is 409 busy", func(t *testing.T) {
		_, unsub := ft8Svc.Subscribe()
		defer unsub()
		w := postFt8Qso(t, srv, "/v1/ft8/claim", `{"mode":"ft8"}`, srv.handleFt8Claim)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, "ft8_profile_busy", body["code"])
		_, hasRetry := body["retry_after_ms"]
		require.False(t, hasRetry, "no linger pending, no retry hint")
		require.Equal(t, "FT4", ft8Svc.Profile().Name, "a refused claim changes nothing")
	})
}

// A refusal inside a pending linger carries retry_after_ms on the wire.
func TestFt8Claim_RefusalCarriesRetryAfter(t *testing.T) {
	srv := testServer(t)
	w := httptest.NewRecorder()
	srv.writeFt8ClaimError(w, "api.test", &ft8.ProfileRefusal{Err: ft8.ErrProfileSessionActive, RetryAfter: 1500 * time.Millisecond})
	require.Equal(t, http.StatusConflict, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "ft8_session_active", body["code"])
	require.Equal(t, float64(1500), body["retry_after_ms"])
}
