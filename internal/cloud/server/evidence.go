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

	tenant := tenantID(r)
	outcomes, err := s.store.UpsertEvidence(r.Context(), tenant, req.Records)
	if err != nil {
		s.log.Error("evidence batch failed", "tenant_id", tenant, "rows", len(req.Records), "request_id", requestID(r), "err", err)
		s.writeError(w, http.StatusInternalServerError, "storage_error", "evidence batch could not be stored")
		return
	}
	// A batch with rejects / tombstones / already-present / missing-profiles returns
	// 200 exactly like a fully-accepted one, so without this line the server (and the
	// proxy in front of it) cannot tell "all stored" from "some quarantined" — and the
	// whole point of the evidence pipeline is knowing what the far end did with each
	// row (C1). One Info line per batch, the outcome breakdown by count.
	var by [6]int
	for _, o := range outcomes {
		switch o.Outcome {
		case evidencewire.OutcomeAccepted:
			by[0]++
		case evidencewire.OutcomeAlreadyPresent:
			by[1]++
		case evidencewire.OutcomeTombstoned:
			by[2]++
		case evidencewire.OutcomeSuppressed:
			by[3]++
		case evidencewire.OutcomeRetryableMissingProfile:
			by[4]++
		case evidencewire.OutcomePermanentReject:
			by[5]++
		}
	}
	s.log.Info("evidence batch stored",
		"tenant_id", tenant, "rows", len(req.Records),
		"accepted", by[0], "already_present", by[1], "tombstoned", by[2],
		"suppressed", by[3], "retryable_missing_profile", by[4], "permanent_reject", by[5])
	s.writeJSON(w, http.StatusOK, evidencewire.PutResponse{Outcomes: outcomes})
}
