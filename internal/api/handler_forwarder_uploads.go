package api

import (
	"net/http"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

// maxEnqueueUploadsBatch bounds one manual-backfill request. Generous (an
// operator backfilling a contest could select thousands) but finite, so a
// runaway/garbage body can't queue an unbounded scan.
const maxEnqueueUploadsBatch = 5000

// enqueueUploadsRequest is the POST /v1/forwarder/{name}/uploads body. uuids are
// the already-stored QSOs to queue for upload to the path-named forwarder; force
// re-sends QSOs already uploaded to that destination (default: skip them).
type enqueueUploadsRequest struct {
	UUIDs []string `json:"uuids"`
	Force bool     `json:"force"`
}

// handleEnqueueForwarderUploads serves POST /v1/forwarder/{name}/uploads — the
// logbook SPA's manual backfill (ADR 0039). It queues the selected stored QSOs
// for upload to one ENABLED forwarder; the existing per-destination worker then
// drains them. This is how QSOs logged while a forwarder was disabled, pre-dating
// it, or otherwise never auto-queued reach a destination — operator-driven, one
// destination at a time, never automatic.
//
// Status codes:
//   - 400  malformed body / empty uuids / batch too large
//   - 400  forwarder unknown, disabled, or doesn't forward inserts
//     (forwarder_unavailable — a disabled forwarder has no worker and gets its
//     queue rows discarded at startup, so enqueuing would strand them)
//   - 200  {enqueued, skipped_uploaded, skipped_deleted?, not_found?}
func (s *Server) handleEnqueueForwarderUploads(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleEnqueueForwarderUploads"

	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "invalid_forwarder", "forwarder name is required", op)
		return
	}

	var req enqueueUploadsRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}

	// Deduplicate UUIDs (preserving first-occurrence order): a repeated UUID would
	// otherwise be classified — and counted — twice. The service is idempotent per
	// QSO regardless, but the summary should reflect distinct rows.
	uuids := dedupeStrings(req.UUIDs)
	if len(uuids) == 0 {
		s.writeError(w, http.StatusBadRequest, "missing_required_field", "uuids is required", op)
		return
	}
	if len(uuids) > maxEnqueueUploadsBatch {
		s.writeError(w, http.StatusBadRequest, "batch_too_large",
			"too many uuids in one request", op)
		return
	}

	res, err := s.qso.EnqueueUploads(r.Context(), name, uuids, req.Force, origin.Manual)
	if err != nil {
		if se := qsoservice.IsSubmitError(err); se != nil {
			s.writeError(w, http.StatusBadRequest, se.Code, se.Message, op)
			return
		}
		s.writeServerError(w, op, err, "enqueue_failed", "enqueue upload backfill failed")
		return
	}

	s.writeJSON(w, http.StatusOK, res)
}
