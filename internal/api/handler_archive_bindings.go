package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// handleGetArchiveBindings serves GET /v1/qso-archives/{uuid}/bindings (ADR
// 0082, W-0021 5D): the ACTIVE archive's destination bindings as the
// Forwarding tab renders them — through the archive port, like every archive
// route. Another archive answers 409, an unknown id 404.
func (s *Server) handleGetArchiveBindings(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleGetArchiveBindings"
	if s.archivesUnavailable(w, op) {
		return
	}
	id := r.PathValue("uuid")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_param", "archive id is required", op)
		return
	}
	view, err := s.archives.Bindings(r.Context(), id)
	if err != nil {
		s.writeArchiveError(w, op, err, "bindings_read_failed", "read the archive's bindings failed")
		return
	}
	s.writeJSON(w, http.StatusOK, view)
}

// handlePutArchiveBindings serves PUT /v1/qso-archives/{uuid}/bindings: the
// port validates the whole candidate first, commits every affected row in one
// transaction, and returns the fresh view. Masked-on-GET, merge-on-PUT: a
// blank field keeps the stored value; `credentials_clear` removes named
// fields from a row that ends disabled.
func (s *Server) handlePutArchiveBindings(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handlePutArchiveBindings"
	if s.archivesUnavailable(w, op) {
		return
	}
	id := r.PathValue("uuid")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_param", "archive id is required", op)
		return
	}
	var req types.ArchiveBindingsRequest
	if !s.readJSONBody(w, r, op, &req) {
		return
	}
	view, err := s.archives.ApplyBindings(r.Context(), id, req)
	if err != nil {
		s.writeArchiveError(w, op, err, "bindings_write_failed", "write the archive's bindings failed")
		return
	}
	s.writeJSON(w, http.StatusOK, view)
}
