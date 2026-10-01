package api

import (
	"net/http"
	"net/url"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// handleGetSubmitAttribution reports the attribution a Phone / CW submit is
// stored with right now (ADR 0085): MY_RIG — the pinned startup rig with its
// per-rig override — and the effective OPERATOR and MY_NAME for the
// logging_station.operator the SPA sends. A saved draft records it; Restore
// sends it back as an expectation on POST /v1/qso. Its own read, not a
// /v1/config field: that surface stays narrow (review 2026-06-19 L1).
func (s *Server) handleGetSubmitAttribution(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.qso.LiveAttribution(s.cfg.Snapshot().LoggingStation.Operator))
}

// expectedAttribution reads POST /v1/qso's optional expect_my_rig /
// expect_operator / expect_my_name. All three or none: a partial set is an
// error (ok=false), never a partial check. A present-but-empty parameter is a
// known-empty value.
func expectedAttribution(q url.Values) (expect *types.SubmitAttribution, ok bool) {
	keys := [...]string{"expect_my_rig", "expect_operator", "expect_my_name"}
	present := 0
	for _, k := range keys {
		if q.Has(k) {
			present++
		}
	}
	switch present {
	case 0:
		return nil, true
	case len(keys):
		return &types.SubmitAttribution{
			MyRig:    q.Get("expect_my_rig"),
			Operator: q.Get("expect_operator"),
			MyName:   q.Get("expect_my_name"),
		}, true
	default:
		return nil, false
	}
}
