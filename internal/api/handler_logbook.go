package api

import (
	stderr "errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

func (s *Server) handleListLogbooks(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleListLogbooks"

	logbooks, err := s.db.FetchAllLogbooksWithContext(r.Context())
	if err != nil {
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.writeJSON(w, http.StatusOK, logbooks)
}

func (s *Server) handleGetLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleGetLogbook"

	id, err := parsePathID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	logbook, err := s.db.FetchLogbookByIDWithContext(r.Context(), id)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "logbook not found", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.writeJSON(w, http.StatusOK, logbook)
}

func (s *Server) handleCreateLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleCreateLogbook"

	var req struct {
		Name        string `json:"name"`
		Callsign    string `json:"callsign"`
		Description string `json:"description,omitempty"`
	}

	if !s.readJSONBody(w, r, op, &req) {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Callsign = strings.ToUpper(strings.TrimSpace(req.Callsign))
	req.Description = strings.TrimSpace(req.Description)

	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_field", "name is required", op)
		return
	}
	if req.Callsign == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_field", "callsign is required", op)
		return
	}
	if !isValidCallsign(req.Callsign) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"callsign must be 3-32 characters and contain at least one digit", op)
		return
	}

	id, err := s.db.InsertLogbookWithContext(r.Context(), types.Logbook{
		Name:        req.Name,
		Callsign:    req.Callsign,
		Description: req.Description,
	})
	if err != nil {
		if stderr.Is(err, errors.ErrDuplicateName) {
			s.writeError(w, http.StatusConflict, "duplicate_name", "a logbook with that name already exists", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleUpdateLogbook"

	id, err := parsePathID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	existing, err := s.db.FetchLogbookByIDWithContext(r.Context(), id)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "logbook not found", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	var req struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
	}

	if !s.readJSONBody(w, r, op, &req) {
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value", "name cannot be empty", op)
			return
		}
		existing.Name = name
	}
	if req.Description != nil {
		existing.Description = strings.TrimSpace(*req.Description)
	}

	if err = s.db.UpdateLogbookWithContext(r.Context(), existing); err != nil {
		if stderr.Is(err, errors.ErrDuplicateName) {
			s.writeError(w, http.StatusConflict, "duplicate_name", "a logbook with that name already exists", op)
			return
		}
		// The logbook was active when fetched but a concurrent DELETE
		// soft-deleted it before the update committed (the active-row update
		// matched zero rows → ErrNotFound). That's a 404, not a server fault.
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "logbook not found", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.writeJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleDeleteLogbook"

	id, err := parsePathID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	// Refuse to delete the configured default logbook: doing so leaves
	// default_logbook_id dangling (new submits then resolve a non-existent
	// logbook) with no UI to repair it (review 2026-07-22 #1). The operator must
	// point the default elsewhere first.
	if def := s.cfg.Snapshot().DefaultLogbookID; def == id {
		s.writeError(w, http.StatusConflict, "default_logbook",
			"cannot delete the default logbook; set another default first", op)
		return
	}

	if err = s.db.DeleteLogbookByIDWithContext(r.Context(), id); err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "logbook not found", op)
			return
		}
		if stderr.Is(err, errors.ErrLogbookHasQsos) {
			s.writeError(w, http.StatusConflict, "has_qsos", "cannot delete a logbook that contains QSOs", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parsePathID extracts a named integer path parameter from the request.
func parsePathID(r *http.Request, name string) (int64, error) {
	const op errors.Op = "api.parsePathID"

	raw := r.PathValue(name)
	if raw == "" {
		return 0, errors.New(op).WithMsgf("%s is required", name)
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, errors.New(op).WithMsgf("%s must be a positive integer", name)
	}
	return id, nil
}

// parsePathUUID extracts a named UUIDv7 path parameter from the
// request. Format validation is a quick reject before the DB lookup.
// Used by the QSO endpoints since ADR 0016 — UUID is the canonical
// external identifier.
func parsePathUUID(r *http.Request, name string) (string, error) {
	const op errors.Op = "api.parsePathUUID"

	raw := r.PathValue(name)
	if raw == "" {
		return "", errors.New(op).WithMsgf("%s is required", name)
	}
	if !utils.IsValidUUIDv7(raw) {
		return "", errors.New(op).WithMsgf("%s must be a UUIDv7", name)
	}
	return raw, nil
}
