package api

import (
	"bytes"
	"encoding/json"
	stderrs "errors"
	"io"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// recordNotificationRequest is the browser's allowlisted, typed body for a
// durable operator notification (W-0001 / ADR 0076). Only export.adif_failed is
// wired: the client supplies the bounded kind-specific fields (count, outcome);
// the daemon stamps category, severity, occurrence time, and build. Count is a
// pointer so a missing value is distinguishable from a supplied 0.
type recordNotificationRequest struct {
	Kind    string `json:"kind"`
	Count   *int   `json:"count"`
	Outcome string `json:"outcome"`
}

// handleRecordNotification records a browser-originated durable notification
// (W-0001 / ADR 0076). It decodes STRICTLY — the shared readJSONBody accepts
// unknown fields, so this handler reads the body with the size cap and then
// json-decodes with DisallowUnknownFields, rejecting any smuggled key (message,
// reason, code, …). It allowlists the kind and outcome, requires a positive
// integral count (non-integral and overflow are rejected by the int decode), and
// builds the canonical detail server-side — the client's bytes are never stored.
// Category, severity, occurrence time, and build are all stamped daemon-side.
//
// The count is deliberately NOT capped at the export endpoint's 10,000 limit: a
// 10,001-QSO request may be precisely the invalid export failure being recorded.
func (s *Server) handleRecordNotification(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleRecordNotification"

	body, ok := s.kit.ReadBody(w, r, op)
	if !ok {
		return
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var req recordNotificationRequest
	if err := dec.Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "failed to parse request body", op)
		return
	}
	// Reject any trailing tokens. dec.More() only reports another array/object
	// ELEMENT, so a stray delimiter (e.g. a trailing ']') slips past it; a second
	// decode that must reach io.EOF rejects every trailing byte instead.
	if err := dec.Decode(new(json.RawMessage)); !stderrs.Is(err, io.EOF) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "unexpected trailing data after JSON body", op)
		return
	}

	if req.Kind != "export.adif_failed" {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "unsupported notification kind", op)
		return
	}
	if req.Count == nil || *req.Count < 1 {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "count must be a positive integer", op)
		return
	}
	switch req.Outcome {
	case "no_qsos", "invalid", "server", "network":
	default:
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "unsupported export outcome", op)
		return
	}

	detail, _ := json.Marshal(struct {
		Count   int    `json:"count"`
		Outcome string `json:"outcome"`
	}{Count: *req.Count, Outcome: req.Outcome})

	if err := s.db.RecordOperatorEvent(r.Context(), sqlite.OperatorEventInput{
		Category: "notification",
		Kind:     "export.adif_failed",
		Severity: "error",
		Build:    s.daemonVersion,
		Detail:   detail,
	}); err != nil {
		s.writeServerError(w, op, err, "record_failed", "failed to record notification")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
