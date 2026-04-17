package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func (s *Server) handleListLogbooks(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleListLogbooks"

	logbooks, err := s.db.FetchAllLogbooksWithContext(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}

	writeJSON(w, http.StatusOK, logbooks)
}

func (s *Server) handleGetLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleGetLogbook"

	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	logbook, err := s.db.FetchLogbookByIDWithContext(r.Context(), id)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "logbook not found", op)
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}

	writeJSON(w, http.StatusOK, logbook)
}

func (s *Server) handleCreateLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleCreateLogbook"

	var req struct {
		Name        string `json:"name"`
		Callsign    string `json:"callsign"`
		Description string `json:"description,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "failed to parse request body", op)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Callsign = strings.ToUpper(strings.TrimSpace(req.Callsign))
	req.Description = strings.TrimSpace(req.Description)

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "missing_required_field", "name is required", op)
		return
	}
	if req.Callsign == "" {
		writeError(w, http.StatusBadRequest, "missing_required_field", "callsign is required", op)
		return
	}

	id, err := s.db.InsertLogbookWithContext(r.Context(), types.Logbook{
		Name:        req.Name,
		Callsign:    req.Callsign,
		Description: req.Description,
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			writeError(w, http.StatusConflict, "duplicate_name", "a logbook with that name already exists", op)
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleUpdateLogbook"

	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	existing, err := s.db.FetchLogbookByIDWithContext(r.Context(), id)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "logbook not found", op)
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}

	var req struct {
		Name        *string `json:"name,omitempty"`
		Callsign    *string `json:"callsign,omitempty"`
		Description *string `json:"description,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "failed to parse request body", op)
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "invalid_field_value", "name cannot be empty", op)
			return
		}
		existing.Name = name
	}
	if req.Callsign != nil {
		callsign := strings.ToUpper(strings.TrimSpace(*req.Callsign))
		if callsign == "" {
			writeError(w, http.StatusBadRequest, "invalid_field_value", "callsign cannot be empty", op)
			return
		}
		existing.Callsign = callsign
	}
	if req.Description != nil {
		existing.Description = strings.TrimSpace(*req.Description)
	}

	if err := s.db.UpdateLogbookWithContext(r.Context(), existing); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			writeError(w, http.StatusConflict, "duplicate_name", "a logbook with that name already exists", op)
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleDeleteLogbook"

	id, err := parsePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	if err := s.db.DeleteLogbookByIDWithContext(r.Context(), id); err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "logbook not found", op)
			return
		}
		if strings.Contains(err.Error(), "FOREIGN KEY constraint") || strings.Contains(err.Error(), "RESTRICT") {
			writeError(w, http.StatusConflict, "has_qsos", "cannot delete a logbook that contains QSOs", op)
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
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
