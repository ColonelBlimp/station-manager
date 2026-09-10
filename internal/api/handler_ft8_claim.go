package api

import (
	stderr "errors"
	"net/http"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// ft8ClaimRequest is the POST /v1/ft8/claim body (ADR 0080): the FT view names
// the profile it is about to subscribe on.
type ft8ClaimRequest struct {
	Mode string `json:"mode"`
}

// ft8ClaimResponse is the 200 body: the active profile after the claim.
type ft8ClaimResponse struct {
	Mode string `json:"mode"`
}

// ft8ClaimRefusal is the 409 body: the standard error envelope plus, when the
// cause is the linger of a session the operator just left, how long it has to
// wind down — so the view can re-claim on time instead of polling.
type ft8ClaimRefusal struct {
	Code         string    `json:"code"`
	Message      string    `json:"message"`
	Op           errors.Op `json:"op,omitempty"`
	RetryAfterMs int64     `json:"retry_after_ms,omitempty"`
}

// handleFt8Claim selects the FT-family profile for the capture sessions that
// follow (ADR 0080, W-0019 slice 3). The view calls it BEFORE opening
// /v1/ft8/events because a native EventSource cannot surface a refusal code.
// The same profile is always a 200; a different one is refused with a distinct
// code while a subscriber holds a capture, a transmission is in flight, a
// session is active or TX is armed — nothing changes on a refusal.
func (s *Server) handleFt8Claim(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleFt8Claim"

	var req ft8ClaimRequest
	if !s.readCommandJSON(w, r, op, &req) {
		return
	}
	if req.Mode == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_field", "mode is required", op)
		return
	}
	p, err := s.ft8.ClaimProfile(req.Mode)
	if err != nil {
		s.writeFt8ClaimError(w, op, err)
		return
	}
	s.writeJSON(w, http.StatusOK, ft8ClaimResponse{Mode: p.Name})
}

// writeFt8ClaimError maps the claim's sentinels to HTTP status + codes. Unknown
// is a client error; every other refusal is a conflict with live state and
// carries retry_after_ms when a capture linger is pending.
func (s *Server) writeFt8ClaimError(w http.ResponseWriter, op errors.Op, err error) {
	if stderr.Is(err, ft8.ErrProfileUnknown) {
		s.writeError(w, http.StatusBadRequest, "ft8_profile_unknown", "mode must be ft8 or ft4", op)
		return
	}
	code, msg := "", ""
	switch {
	case stderr.Is(err, ft8.ErrProfileBusy):
		code, msg = "ft8_profile_busy", "another subscriber holds a capture on the other profile"
	case stderr.Is(err, ft8.ErrProfileTxInFlight):
		code, msg = "ft8_tx_in_flight", "a transmission is in flight; the profile changes only between sessions"
	case stderr.Is(err, ft8.ErrProfileSessionActive):
		code, msg = "ft8_session_active", "a sequenced session is active; the profile changes only between sessions"
	case stderr.Is(err, ft8.ErrProfileTxArmed):
		code, msg = "ft8_tx_armed", "FT8 transmit is armed; disarm before changing profile"
	default:
		s.writeServerError(w, op, err, "internal_error", "profile claim failed")
		return
	}
	body := ft8ClaimRefusal{Code: code, Message: msg, Op: op}
	var refusal *ft8.ProfileRefusal
	if stderr.As(err, &refusal) && refusal.RetryAfter > 0 {
		// Every positive duration serialises as at least 1 ms: a sub-millisecond
		// remainder at the advertised deadline must not read as "no hint".
		body.RetryAfterMs = max(int64(1), (refusal.RetryAfter + time.Millisecond - 1).Milliseconds())
	}
	s.writeJSON(w, http.StatusConflict, body)
}
