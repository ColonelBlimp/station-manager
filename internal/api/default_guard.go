package api

import (
	"context"
	stderr "errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// defaultLogbookHeader names the logbook a tab treats as the default (ADR 0097
// §4, W-0024 slice 4). A live request carries it beside X-SM-Archive-Id, so a
// tab still showing the default it booted with cannot log or start FT8 on
// another one after a default change applied at a restart. The tab takes it
// from its established station context, the same one that names ?logbook=.
const defaultLogbookHeader = "X-SM-Default-Logbook"

// liveSubmitKey carries requireLiveSubmit's classification to the submit
// handler, which admits a live submission through the live-entry gate.
type liveSubmitKey struct{}

// effectiveDefault resolves the default this generation serves. uuid is ""
// when no logbook is the default (unconfigured, or the pointer names none);
// err is a datastore failure, never a proof of anything.
func (s *Server) effectiveDefault(ctx context.Context) (id int64, uuid string, err error) {
	id = s.cfg.Snapshot().DefaultLogbookID
	if id < 1 {
		return id, "", nil
	}
	lb, err := s.db.FetchLogbookByIDWithContext(ctx, id)
	if stderr.Is(err, errors.ErrNotFound) {
		return id, "", nil
	}
	if err != nil {
		return id, "", err
	}
	return id, lb.UUID, nil
}

// readDefaultHeader returns the asserted default, or writes the refusal: a
// missing header (a tab served by an older page) or a malformed or repeated one.
func (s *Server) readDefaultHeader(w http.ResponseWriter, r *http.Request, op errors.Op) (string, bool) {
	values := r.Header.Values(defaultLogbookHeader)
	if len(values) == 0 {
		s.writeError(w, http.StatusBadRequest, "missing_required_field",
			defaultLogbookHeader+" header is required: reload the page so it can confirm its default logbook", op)
		return "", false
	}
	if len(values) > 1 || !utils.IsValidUUIDv7(values[0]) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value",
			defaultLogbookHeader+" must be one logbook UUID", op)
		return "", false
	}
	return values[0], true
}

func (s *Server) writeDefaultMismatch(w http.ResponseWriter, op errors.Op) {
	s.writeError(w, http.StatusConflict, "default_logbook_mismatch",
		"this page shows a different default logbook from the one the daemon now serves; nothing was done — the page must reload", op)
}

// checkDefault enforces the header on an FT8 request that admits a session or
// transmission: FT8 pins the daemon's default, so any other assertion means the
// contact would go somewhere the page does not show. No effective default
// matches nothing (fail closed).
func (s *Server) checkDefault(w http.ResponseWriter, r *http.Request, op errors.Op) bool {
	asserted, ok := s.readDefaultHeader(w, r, op)
	if !ok {
		return false
	}
	id, effective, err := s.effectiveDefault(r.Context())
	if err != nil {
		s.writeStationIdentityUnavailable(w, op, err, id)
		return false
	}
	if effective == "" || !strings.EqualFold(asserted, effective) {
		s.writeDefaultMismatch(w, op)
		return false
	}
	return true
}

// requireDefault guards an FT8 route whose every request admits a session or
// transmission. Inside requireArchive, before the handler.
func (s *Server) requireDefault(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.checkDefault(w, r, "api.requireDefault") {
			h.ServeHTTP(w, r)
		}
	})
}

// requireLiveSubmit classifies POST /v1/qso by the tab's own assertion: naming
// the logbook it asserts is the default is a live submission, refused unless
// that is the effective default and never retargeted; naming any other logbook
// is a deliberate operation on it. It runs before the rate limiter, so a refused
// request spends no submit token. A query the handler will refuse anyway passes
// through to that refusal.
func (s *Server) requireLiveSubmit(h http.Handler) http.Handler {
	const op errors.Op = "api.requireLiveSubmit"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asserted, ok := s.readDefaultHeader(w, r, op)
		if !ok {
			return
		}
		query, qerr := url.ParseQuery(r.URL.RawQuery)
		target, perr := strconv.ParseInt(query.Get("logbook"), 10, 64)
		if qerr != nil || perr != nil || target < 1 {
			h.ServeHTTP(w, r)
			return
		}
		id, effective, err := s.effectiveDefault(r.Context())
		if err != nil {
			s.writeStationIdentityUnavailable(w, op, err, id)
			return
		}
		targetUUID := effective
		if target != id {
			lb, err := s.db.FetchLogbookByIDWithContext(r.Context(), target)
			switch {
			case stderr.Is(err, errors.ErrNotFound):
				// Deliberate: it names nothing the tab asserts. The handler
				// reports the missing logbook.
				h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), liveSubmitKey{}, false)))
				return
			case err != nil:
				s.writeStationIdentityUnavailable(w, op, err, target)
				return
			}
			targetUUID = lb.UUID
		}
		live := strings.EqualFold(targetUUID, asserted)
		if live && (effective == "" || !strings.EqualFold(asserted, effective)) {
			s.writeDefaultMismatch(w, op)
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), liveSubmitKey{}, live)))
	})
}

// submitClassifiedLive reports requireLiveSubmit's classification; ok is false when the
// route was served without it.
func submitClassifiedLive(r *http.Request) (live, ok bool) {
	live, ok = r.Context().Value(liveSubmitKey{}).(bool)
	return live, ok
}
