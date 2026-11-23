package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// ft8StateResponse is the answer of GET /v1/ft8/state (W-0027 A3, section I).
// When FT8 is not served it is `{"served": false}` and nothing else. When it is,
// every other field is present, true or false, so that a caller can tell an idle
// answer from one that is missing a field: the fields are pointers for that
// reason only.
type ft8StateResponse struct {
	Served bool `json:"served"`
	// WouldStop is the daemon's own rule for "a stop would stop something". A
	// caller reads this and decides nothing from the parts beside it.
	WouldStop     *bool `json:"would_stop,omitempty"`
	TxArmed       *bool `json:"tx_armed,omitempty"`
	TxInFlight    *bool `json:"tx_in_flight,omitempty"`
	RunActive     *bool `json:"run_active,omitempty"`
	AutoWorkArmed *bool `json:"auto_work_armed,omitempty"`
}

// ft8StateBody is the answer for a served FT8 in state st.
func ft8StateBody(st ft8.OperatingState) ft8StateResponse {
	would := st.WouldStop()
	return ft8StateResponse{
		Served:        true,
		WouldStop:     &would,
		TxArmed:       &st.TxArmed,
		TxInFlight:    &st.TxInFlight,
		RunActive:     &st.RunActive,
		AutoWorkArmed: &st.AutoWorkArmed,
	}
}

// registerFt8State registers the read whether or not FT8 is enabled, so that
// "not served" is an answer and a missing route is never what says FT8 is off
// (the same reason GET /v1/evidence/status is unconditional). It is registered
// here, beside its handler, and not among the FT8 routes that exist only when
// FT8 is enabled.
func (s *Server) registerFt8State(apiMux *routeMux) {
	apiMux.HandleFunc("GET /v1/ft8/state", s.handleFt8State)
}

// handleFt8State answers what FT8 is doing now.
//
// It changes nothing and authorises nothing, so it asks for no current lifetime:
// the desktop app reads it while closing, when it may no longer be current. The
// state is read in one call and no FT8 lock is held while the answer is written.
func (s *Server) handleFt8State(w http.ResponseWriter, _ *http.Request) {
	if !ft8RoutesServed(s.ft8) {
		s.writeJSON(w, http.StatusOK, ft8StateResponse{})
		return
	}
	s.writeJSON(w, http.StatusOK, ft8StateBody(s.ft8.OperatingState()))
}
