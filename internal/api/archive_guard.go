package api

import (
	"net/http"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// archiveIDHeader names the archive a browser tab has proven it is bound to
// (ADR 0094 update, 2026-10-23). A request that would act on an archive carries
// it so a daemon serving a different archive refuses before any side effect:
// a response's provenance cannot stop an action reaching the wrong archive, and
// ADR 0087 keeps that prevention requirement. UUIDs do not make a QSO route
// safe on their own — `smd import` preserves supplied UUIDs, so two archives can
// hold the same one.
const archiveIDHeader = "X-SM-Archive-Id"

// SetServedArchive records the archive this daemon generation has open, read
// from the file's own identity. Called by cmd/smd after New, before Start.
func (s *Server) SetServedArchive(id string) {
	s.servedArchiveID = id
}

// checkArchive enforces the header against the served archive and writes the
// refusal when it fails. required=false lets a request without the header
// through (plain readers of GET /v1/qso/{uuid}); a supplied header is always
// enforced. An unset served archive refuses every supplied header: nothing can
// be proven to match it.
func (s *Server) checkArchive(w http.ResponseWriter, r *http.Request, op errors.Op, required bool) bool {
	values := r.Header.Values(archiveIDHeader)
	if len(values) == 0 {
		if !required {
			return true
		}
		s.writeError(w, http.StatusBadRequest, "missing_required_field",
			archiveIDHeader+" header is required: reload the page so it can confirm its archive", op)
		return false
	}
	if len(values) > 1 || !utils.IsValidUUIDv7(values[0]) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			archiveIDHeader+" must be one archive UUID", op)
		return false
	}
	if s.servedArchiveID == "" || !strings.EqualFold(values[0], s.servedArchiveID) {
		s.writeError(w, http.StatusConflict, "archive_mismatch",
			"this page is bound to a different archive from the one the daemon now serves; nothing was done — the page must reload", op)
		return false
	}
	return true
}

// requireArchive guards a route whose every request acts on the archive or
// admits transmission. It runs before the handler, so before any side effect.
func (s *Server) requireArchive(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.checkArchive(w, r, "api.requireArchive", true) {
			h.ServeHTTP(w, r)
		}
	})
}

// archiveIfSupplied guards a read that plain readers may make without the
// header; when a tab supplies one, it must match.
func (s *Server) archiveIfSupplied(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.checkArchive(w, r, "api.archiveIfSupplied", false) {
			h.ServeHTTP(w, r)
		}
	})
}
