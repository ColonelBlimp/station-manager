package server

import (
	"encoding/json"
	stderr "errors"
	"io"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/cloud/evidencewire"
)

// maxEvidenceBatchRows caps one PUT /v1/evidence envelope. The SMD client
// sends 500-row backlog batches (ratified constant, spot-network §5.1
// amendment); double that is generous headroom, and anything larger is a
// malformed or hostile request, not a bigger backlog.
const maxEvidenceBatchRows = 1000

// handlePutEvidence is the §5 sync ingest: one envelope, per-row outcomes.
// Envelope faults are REQUEST faults (400); row faults answer on their own
// row inside a 200 — turning one bad row into a request failure would block
// its batch-mates, which §5.1's quarantine contract forbids. Row semantics
// (digest identity, missing-profile retry, tenant scoping) live in
// store.UpsertEvidence.
func (s *Server) handlePutEvidence(w http.ResponseWriter, r *http.Request) {
	var req evidencewire.PutRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_body", "body must be JSON: "+err.Error())
		return
	}
	if err := dec.Decode(new(json.RawMessage)); !stderr.Is(err, io.EOF) {
		s.writeError(w, http.StatusBadRequest, "invalid_body", "body must be a single JSON document")
		return
	}
	if len(req.Records) == 0 {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "records must be a non-empty array")
		return
	}
	if len(req.Records) > maxEvidenceBatchRows {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "records exceeds the batch cap")
		return
	}

	outcomes, err := s.store.UpsertEvidence(r.Context(), tenantID(r), req.Records)
	if err != nil {
		s.log.Error("evidence batch failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "storage_error", "evidence batch could not be stored")
		return
	}
	s.writeJSON(w, http.StatusOK, evidencewire.PutResponse{Outcomes: outcomes})
}
