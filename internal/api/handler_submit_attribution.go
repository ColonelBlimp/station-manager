package api

import (
	"net/http"
	"net/url"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// handleGetSubmitAttribution reports the attribution a Phone / CW submit is
// stored with right now (ADR 0085): MY_RIG — the pinned startup rig with its
// per-rig override — and the effective OPERATOR and MY_NAME. With ?operator=X
// it is for a submit carrying OPERATOR X, resolved exactly as the submit
// resolves it (present-but-empty = an empty submitted operator, so the
// default_operator fallback applies); without the parameter, for
// logging_station.operator (the original behaviour). A saved draft records it;
// Restore sends it back as an expectation on POST /v1/qso. Its own read, not a
// /v1/config field: that surface stays narrow (review 2026-06-19 L1).
func (s *Server) handleGetSubmitAttribution(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleGetSubmitAttribution"
	// Parsed strictly: a dropped pair would silently answer for another operator.
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_query_param",
			"the query string could not be parsed", op)
		return
	}
	operator := s.cfg.Snapshot().LoggingStation.Operator
	if query.Has("operator") {
		operator = query.Get("operator")
	}
	// The operator reaches a submit as an ADIF value, which the parser right-trims:
	// resolve the form that will be stored, or a matching expectation would be
	// refused as attribution_changed (Codex review 4a27711e).
	s.writeJSON(w, http.StatusOK, s.qso.LiveAttribution(adif.NormalizeValue(operator)))
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
