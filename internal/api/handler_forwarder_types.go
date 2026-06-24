package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
)

// handleForwarderTypes serves GET /v1/forwarder-types — the data-driven
// descriptor for every registered forwarder type (display name, supported
// actions, and credential-field shapes). The config SPA's "add forwarder" form
// renders the right credential inputs from this, so adding a new forwarder type
// in Go needs zero SPA changes (ADR 0028 KISS pattern; backlog 2026-06-23).
//
// Read-only, always 200. Only types registered via
// forwarding.RegisterForwarderType appear (the real forwarders compiled into
// this binary); a bare RegisterSupportedActions test type has no descriptor and
// is omitted.
func (s *Server) handleForwarderTypes(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"types": forwarding.ForwarderTypes()})
}
