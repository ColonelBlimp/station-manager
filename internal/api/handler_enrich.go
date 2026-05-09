package api

import (
	"net/http"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
)

// emptyEnrichmentResult returns a Result with empty country / station
// fields and source=none on both layers. Used when the orchestrator
// is nil (lookup pipeline disabled) so the SPA gets a uniform shape
// rather than an HTTP error path it has to special-case.
func emptyEnrichmentResult(callsign string) lookup.Result {
	return lookup.Result{
		Callsign:      callsign,
		CountrySource: lookup.SourceNone,
		StationSource: lookup.SourceNone,
	}
}

// handleEnrichCallsign answers the SPA's "what do we know about this
// callsign?" question, fronting the orchestrator pipeline per ADR 0017.
//
// GET /v1/enrich/callsign?call=<callsign>[&refresh=true]
//
// The orchestrator handles all the heavy lifting — concurrent reads
// against the cache (country table + contacted_station table), cold-
// miss upstream calls (hamnut for country, the callsign-class chain
// for station), filter+merge before write-back, async refresh
// scheduling for stale hits.
//
// refresh=true bypasses the cache: both layers go straight to
// upstream and overwrite the cached rows on success. The operator's
// escape valve for "the cache is wrong" (stale QRZ data, a
// misclassified country prefix, etc.). Anything other than the
// literal string "true" is treated as the default (cache-aware)
// path — strict to keep the contract obvious.
//
// Response is always 200 per ADR 0017 #12: transient upstream
// failures fold into "empty fields, source=none" rather than non-2xx.
// The handler only returns 4xx for malformed input (missing or
// invalid `call` query param) — those are operator/SPA bugs, not
// enrichment failures.
//
// Cancellation: the request's r.Context() is propagated through to
// the orchestrator's upstream calls. SPA-side AbortController firing
// on the next Tab cancels in-flight upstream HTTP calls promptly.
func (s *Server) handleEnrichCallsign(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleEnrichCallsign"

	if s.enrich == nil {
		// Defensive: the daemon constructed without an orchestrator
		// (lookup pipeline disabled in config or all providers
		// disabled). Return 200 with empty result + source=none so
		// the SPA's UX path is unchanged.
		s.writeJSON(w, http.StatusOK, emptyEnrichmentResult(""))
		return
	}

	callsign := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("call")))
	if callsign == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_param",
			"call query parameter is required", op)
		return
	}
	if !isValidCallsign(callsign) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"call must be 3-32 characters and contain at least one digit", op)
		return
	}

	refresh := r.URL.Query().Get("refresh") == "true"

	var result lookup.Result
	if refresh {
		result = s.enrich.EnrichRefresh(r.Context(), callsign)
	} else {
		result = s.enrich.Enrich(r.Context(), callsign)
	}
	s.writeJSON(w, http.StatusOK, result)
}
