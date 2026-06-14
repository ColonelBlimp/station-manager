package api

import (
	"net/http"
	"strconv"
	"strings"

	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

func (s *Server) handleGetQso(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleGetQso"

	uuid, err := parsePathUUID(r, "uuid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_uuid", err.Error(), op)
		return
	}

	qso, err := s.db.FetchQsoByUUIDWithContext(r.Context(), uuid)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "QSO not found", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	s.writeJSON(w, http.StatusOK, qso)
}

func (s *Server) handleDeleteQso(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleDeleteQso"

	uuid, err := parsePathUUID(r, "uuid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_uuid", err.Error(), op)
		return
	}

	// Resolve UUID → row to get the local PK; the qsoservice.Delete
	// path is keyed on int ID for the DB-side soft-delete + upload-
	// queue tx. Surface separation per ADR 0016: external API speaks
	// UUID, internal storage retains the int PK.
	qso, err := s.db.FetchQsoByUUIDWithContext(r.Context(), uuid)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "QSO not found", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	if err = s.qso.Delete(r.Context(), qso, source.API); err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "QSO not found", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateQso(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleUpdateQso"

	uuid, err := parsePathUUID(r, "uuid")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_uuid", err.Error(), op)
		return
	}

	existing, err := s.db.FetchQsoByUUIDWithContext(r.Context(), uuid)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "QSO not found", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	body, ok := s.readBody(w, r, op)
	if !ok {
		return
	}
	if len(body) == 0 {
		body = []byte("{}")
	}

	updated, err := s.qso.Update(r.Context(), existing, body, source.API)
	if err != nil {
		if se := qsoservice.IsSubmitError(err); se != nil {
			status := http.StatusBadRequest
			if se.Code == "duplicate_key" {
				status = http.StatusConflict
			}
			s.writeError(w, status, se.Code, se.Message, op)
			return
		}
		// The QSO was active when we fetched it but a concurrent DELETE
		// soft-deleted it before the update committed (the active-row UPDATE
		// matched zero rows → ErrNotFound). That's a 404, not a server fault.
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "not_found", "QSO not found", op)
			return
		}
		s.writeServerError(w, op, err, "update_failed", "QSO update failed")
		return
	}

	s.writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleSubmitQso(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleSubmitQso"

	// ---- Content-Type check ----
	// Empty Content-Type is treated as ADIF: ADIF doesn't have a
	// universally-deployed media type and operators using `curl
	// --data-binary @file.adi` without --header end up with no CT.
	// Documented in api.md §3 (Content negotiation) — both
	// application/x-adif and text/plain are accepted, plus the empty
	// fallthrough.
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/x-adif") && !strings.HasPrefix(ct, "text/plain") {
		s.writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/x-adif or text/plain", op)
		return
	}

	// ---- Read body with size limit ----
	body, ok := s.readBody(w, r, op)
	if !ok {
		return
	}

	if len(body) == 0 {
		s.writeError(w, http.StatusBadRequest, "invalid_adif", "request body is empty", op)
		return
	}

	// ---- Parse ADIF ----
	parsed, err := adif.Parse(body)
	if err != nil {
		de, ok := errors.AsDetailedError(err)
		msg := "failed to parse ADIF"
		if ok {
			msg = de.Error()
		}
		s.writeError(w, http.StatusBadRequest, "invalid_adif", msg, op)
		return
	}

	if len(parsed.Records) == 0 {
		s.writeError(w, http.StatusBadRequest, "invalid_adif", "no QSO records found in ADIF body", op)
		return
	}

	// Single-record contract: POST /v1/qso accepts exactly one QSO. A
	// multi-record body is almost always a client bug (e.g. an
	// importer accidentally hitting the single-submit endpoint instead
	// of looping). Rejecting loud is better than silently dropping
	// records 2..N, and bounds the parser-allocation cost flagged in
	// the M5 finding from the 2026-05-02 review (the body itself is
	// already capped by MaxBodyBytes; this caps records-per-body).
	if len(parsed.Records) > 1 {
		s.writeError(w, http.StatusBadRequest, "too_many_records",
			"POST /v1/qso accepts exactly one QSO record per request; use cmd/importer for bulk", op)
		return
	}

	rec := parsed.Records[0]

	// ---- Validate STATION_CALLSIGN ----
	stationCallsign := strings.ToUpper(strings.TrimSpace(rec.StationCallsign))
	if stationCallsign == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_field",
			"STATION_CALLSIGN is required", op)
		return
	}
	if !isValidCallsign(stationCallsign) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			"STATION_CALLSIGN must be 3-32 characters and contain at least one digit", op)
		return
	}

	// ---- Resolve and validate logbook ----
	// The client must provide ?logbook=<id>. The daemon verifies the
	// logbook exists and its callsign matches STATION_CALLSIGN.
	logbookIDStr := r.URL.Query().Get("logbook")
	if logbookIDStr == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_param",
			"logbook query parameter is required (e.g. ?logbook=1)", op)
		return
	}

	logbookID, err := strconv.ParseInt(logbookIDStr, 10, 64)
	if err != nil || logbookID < 1 {
		s.writeError(w, http.StatusBadRequest, "invalid_id",
			"logbook must be a positive integer", op)
		return
	}

	// We only need the logbook's callsign (to match against
	// STATION_CALLSIGN). The dedicated SELECT-callsign helper avoids a
	// full-row scan + adapter call on the submit hot path.
	logbookCallsign, err := s.db.LogbookCallsignByIDWithContext(r.Context(), logbookID)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.writeError(w, http.StatusNotFound, "logbook_not_found",
				"logbook does not exist", op)
			return
		}
		s.writeServerError(w, op, err, "db_error", "database operation failed")
		return
	}

	if !strings.EqualFold(logbookCallsign, stationCallsign) {
		s.writeError(w, http.StatusBadRequest, "callsign_mismatch",
			"STATION_CALLSIGN does not match the logbook's callsign", op)
		return
	}

	// ---- Submit ----
	// `?force` accepts the standard truthy set ("1"/"true"/"True"/"TRUE",
	// etc.) via strconv.ParseBool. An unrecognised value is rejected
	// rather than silently falling through to dedupe-checked behaviour:
	// the contest operator typing this in the heat of a pile-up wants
	// either "force" or "tell me you didn't recognise it," not "I
	// silently ignored your typo and lost the dup." Empty/missing
	// param keeps the dedupe-checked default.
	force := false
	if raw := r.URL.Query().Get("force"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid_query_param",
				"force must be a boolean (1/0/true/false)", op)
			return
		}
		force = v
	}
	result, err := s.qso.Submit(r.Context(), logbookID, rec, force)
	if err != nil {
		if se := qsoservice.IsSubmitError(err); se != nil {
			s.writeError(w, http.StatusBadRequest, se.Code, se.Message, op)
			return
		}
		s.writeServerError(w, op, err, "submit_failed", "QSO submit failed")
		return
	}

	status := http.StatusCreated
	if result.Status == "duplicate" {
		status = http.StatusOK
	}
	s.writeJSON(w, status, result)
}
