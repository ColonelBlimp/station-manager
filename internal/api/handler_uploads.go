package api

import (
	stderr "errors"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// uploadsResponse wraps the qso_upload slice in an envelope so the
// response shape matches other list-style endpoints and leaves room
// for future metadata (paging, etc.) without a breaking change. Rows
// are ordered by (forwarder_name, action) as returned by the DB layer.
type uploadsResponse struct {
	Items []types.QsoUpload `json:"items"`
}

// handleListQsoUploads serves GET /v1/qso/{uuid}/uploads — the pull
// counterpart to the SSE forward.* events. Clients that can't hold an
// SSE connection (or want a cold-start snapshot) poll this endpoint
// to see every forwarder's current status for a single QSO.
//
// Behaviour:
//   - 400 on malformed uuid
//   - 404 only if the QSO uuid has never existed in any state (never
//     inserted, or hard-purged by some future admin op)
//   - 200 with {"items": [...]} for live AND soft-deleted QSOs; the
//     delete-action row is legitimate forwarding work and its status
//     must remain observable to clients until it reaches a terminal
//     state.
func (s *Server) handleListQsoUploads(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleListQsoUploads"

	uuid, err := parsePathUUID(r, "uuid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_uuid", err.Error(), op)
		return
	}

	// Existence probe that includes soft-deleted rows — we want to
	// return 404 only for genuinely unknown UUIDs, not for QSOs the
	// operator has just deleted. The non-deleted-only fetch would hide
	// soft-deleted rows, turning a legitimate "show me the
	// delete-forward status" query into a 404. The probe also
	// resolves the local int PK, which the upload-queue join uses.
	qso, err := s.db.FetchQsoByUUIDIncludingDeletedWithContext(r.Context(), uuid)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "QSO not found", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	rows, err := s.db.FetchUploadsByQsoIDWithContext(r.Context(), qso.ID)
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	// Nil-to-empty normalisation: the DB method returns a zero-length
	// slice when there are no rows, but the JSON marshaller would turn
	// a literal nil into "null". Force an empty array for a consistent
	// client contract.
	if rows == nil {
		rows = []types.QsoUpload{}
	}

	s.writeJSON(w, http.StatusOK, uploadsResponse{Items: rows})
}
