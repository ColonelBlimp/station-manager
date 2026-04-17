package api

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

func (s *Server) handleSubmitQso(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleSubmitQso"

	// ---- Content-Type check ----

	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/x-adif") && !strings.HasPrefix(ct, "text/plain") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/x-adif or text/plain", op)
		return
	}

	// ---- Read body with size limit ----
	lr := http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	defer func() {
		if err := lr.Close(); err != nil {
			s.logger.WarnWith().Err(err).Msg("failed to close request body reader")
		}
	}()

	body, err := io.ReadAll(lr)
	if err != nil {
		if err.Error() == "http: request body too large" {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large",
				"request body exceeds maximum size", op)
			return
		}
		writeError(w, http.StatusBadRequest, "read_error", "failed to read request body", op)
		return
	}

	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_adif", "request body is empty", op)
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
		writeError(w, http.StatusBadRequest, "invalid_adif", msg, op)
		return
	}

	if len(parsed.Records) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_adif", "no QSO records found in ADIF body", op)
		return
	}

	rec := parsed.Records[0]

	// ---- Validate STATION_CALLSIGN ----
	stationCallsign := strings.ToUpper(strings.TrimSpace(rec.StationCallsign))
	if stationCallsign == "" {
		writeError(w, http.StatusBadRequest, "missing_required_field",
			"STATION_CALLSIGN is required", op)
		return
	}

	// ---- Resolve and validate logbook ----
	// The client must provide ?logbook=<id>. The daemon verifies the
	// logbook exists and its callsign matches STATION_CALLSIGN.
	logbookIDStr := r.URL.Query().Get("logbook")
	if logbookIDStr == "" {
		writeError(w, http.StatusBadRequest, "missing_required_param",
			"logbook query parameter is required (e.g. ?logbook=1)", op)
		return
	}

	logbookID, err := strconv.ParseInt(logbookIDStr, 10, 64)
	if err != nil || logbookID < 1 {
		writeError(w, http.StatusBadRequest, "invalid_id",
			"logbook must be a positive integer", op)
		return
	}

	logbook, err := s.db.FetchLogbookByIDWithContext(r.Context(), logbookID)
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			writeError(w, http.StatusNotFound, "logbook_not_found",
				"logbook does not exist", op)
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), op)
		return
	}

	if !strings.EqualFold(logbook.Callsign, stationCallsign) {
		writeError(w, http.StatusBadRequest, "callsign_mismatch",
			"STATION_CALLSIGN does not match the logbook's callsign", op)
		return
	}

	// ---- Submit ----
	force := r.URL.Query().Get("force") == "true"
	result, err := s.qso.Submit(r.Context(), logbookID, rec, force)
	if err != nil {
		if se := qsoservice.IsSubmitError(err); se != nil {
			writeError(w, http.StatusBadRequest, se.Code, se.Message, op)
			return
		}
		writeError(w, http.StatusInternalServerError, "submit_failed", err.Error(), op)
		return
	}

	status := http.StatusCreated
	if result.Status == "duplicate" {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}
