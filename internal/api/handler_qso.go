package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

func (s *Server) handleSubmitQso(w http.ResponseWriter, r *http.Request) {
	const op = "api.v1.qso.submit"

	// ---- Content-Type check ----

	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/x-adif") && !strings.HasPrefix(ct, "text/plain") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/x-adif or text/plain", op)
		return
	}

	// ---- Read body with size limit ----

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.maxBodyBytes))
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

	// ---- Resolve logbook ----
	// For milestone 1: use ?logbook=<id> query param, or look up by
	// STATION_CALLSIGN, or auto-create. Simplest: require the callsign
	// to match an existing logbook, or create one.

	rec := parsed.Records[0]
	logbookID, err := s.resolveLogbook(r.Context(), rec.StationCallsign)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "logbook_error", err.Error(), op)
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
