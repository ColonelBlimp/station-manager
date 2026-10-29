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

	s.notifyArchiveSummary()
	s.writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleUpdateLogbook"

	id, err := parsePathID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}

	// Early existence check before parsing the body (retained). The row's VALUES are
	// deliberately not reused for the write — a stale full-row DTO is exactly what let
	// overlapping partial PATCHes clobber each other (PT-4); the field-level update
	// below writes only the supplied columns.
	if _, err := s.db.FetchLogbookByIDWithContext(r.Context(), id); err != nil {
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

	// Normalize the supplied fields and pass only present members through — so a
	// concurrent patch to a DIFFERENT field is never overwritten by this request's
	// stale snapshot of it (PT-4).
	var name, description *string
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			s.writeError(w, http.StatusBadRequest, "invalid_field_value", "name cannot be empty", op)
			return
		}
		name = &n
	}
	if req.Description != nil {
		d := strings.TrimSpace(*req.Description)
		description = &d
	}

	updated, err := s.db.UpdateLogbookFieldsWithContext(r.Context(), id, name, description)
	if err != nil {
		if stderr.Is(err, errors.ErrDuplicateName) {
			s.writeError(w, http.StatusConflict, "duplicate_name", "a logbook with that name already exists", op)
			return
		}
		// A concurrent DELETE soft-deleted the row before the write committed (the
		// active-row update matched zero rows → ErrNotFound). That's a 404, not a fault.
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "logbook not found", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.notifyArchiveSummary()
	// Respond with the COMMITTED row, not the stale pre-update object.
	s.writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteLogbook(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleDeleteLogbook"

	id, err := parsePathID(r, "id")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_id", err.Error(), op)
		return
	}
	// Under the single flight a default change and an archive activation share
	// (ADR 0097 §2 step 1), so a delete never races a change of the default.
	if s.defaultChanger != nil {
		if err := s.defaultChanger.WithLogbookChange(func() error {
			s.deleteLogbook(w, r, op, id)
			return nil
		}); err != nil {
			s.writeArchiveError(w, op, err, "db_error", "database operation failed")
		}
		return
	}
	s.deleteLogbook(w, r, op, id)
}

// deleteLogbook is the delete itself, with its refusals.
func (s *Server) deleteLogbook(w http.ResponseWriter, r *http.Request, op errors.Op, id int64) {
	// Refuse to delete the configured default logbook: doing so leaves
	// default_logbook_id dangling (new submits then resolve a non-existent
	// logbook) with no UI to repair it (review 2026-07-22 #1). The operator must
	// point the default elsewhere first.
	snap := s.cfg.Snapshot()
	if snap.DefaultLogbookID == id {
		s.writeError(w, http.StatusConflict, "default_logbook",
			"cannot delete the default logbook; set another default first", op)
		return
	}
	// Nor the target of a default change that is pending or whose removal is not
	// yet confirmed on disk (ADR 0097): config holds the record exactly while it
	// is either, and a later start could still apply it.
	if c := snap.DefaultLogbookChange; c != nil {
		lb, err := s.db.FetchLogbookByIDWithContext(r.Context(), id)
		if err != nil && !stderr.Is(err, errors.ErrNotFound) {
			s.writeServerError(w, op, err, "db_error", "database operation failed")
			return
		}
		if err == nil && lb.UUID != "" && lb.UUID == c.LogbookUUID {
			s.writeError(w, http.StatusConflict, "default_change_target",
				"cannot delete the target of a default logbook change that is pending or not yet confirmed", op)
			return
		}
	}

	if err := s.db.DeleteLogbookByIDWithContext(r.Context(), id); err != nil {
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

	s.notifyArchiveSummary()
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
