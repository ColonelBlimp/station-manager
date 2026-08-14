package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/lookupdef"
)

// handleLookupTypes serves GET /v1/lookup-types — the data-driven descriptors
// for every registered enrichment provider plus ADR 0068's completion-field
// catalogue.
//
// The Settings → Enrichment section renders from this, so adding a provider in
// Go needs zero SPA change (ADR 0062). It is the direct counterpart of
// /v1/forwarder-types, and exists because the same facts used to be duplicated
// in a hardcoded map in the SPA.
//
// Read-only, always 200. Only providers registered via
// lookupdef.RegisterProvider appear — i.e. those actually compiled into this
// binary and therefore wirable. A provider present in the operator's config but
// missing here is exactly the "unrecognised" case the section renders
// generically, which is what a config from a newer build looks like.
func (s *Server) handleLookupTypes(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"types":             lookupdef.Descriptors(),
		"completion_fields": lookupdef.CompletionFields(),
	})
}
