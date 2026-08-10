package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/evidence"
)

// SetEvidence injects the evidence writer for GET /v1/evidence/status.
// Called by cmd/smd after New, before Start (the SetTxKeyer wiring shape).
// nil — no writer constructed — reports the same disabled state a
// capture-off service would: to the operator both mean "no evidence is
// being captured", which is the question the endpoint answers.
func (s *Server) SetEvidence(ev *evidence.Service) {
	s.evidence = ev
}

// handleEvidenceStatus serves the §4.1 local honesty surface: capture state
// (disabled / capturing / drop_new), physical usage against cap and
// watermark, observation and unprofiled-observation counts, and dropped
// slots. Read-only; the counts come from the archive itself.
func (s *Server) handleEvidenceStatus(w http.ResponseWriter, _ *http.Request) {
	if s.evidence == nil {
		s.writeJSON(w, http.StatusOK, evidence.Status{State: evidence.StateDisabled})
		return
	}
	s.writeJSON(w, http.StatusOK, s.evidence.Status())
}
