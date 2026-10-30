package api

import (
	"context"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// DefaultChanger is the default-logbook change port (ADR 0097), implemented by
// internal/archive and injected by cmd/smd. Every refusal carries
// RequestCode(), mapped like the archive routes'.
type DefaultChanger interface {
	RequestDefaultChange(ctx context.Context, req types.DefaultChangeRequest) (types.DefaultChangeAccepted, error)
	CancelDefaultChange(ctx context.Context, id string) (types.DefaultChangeStatus, error)
	ConfirmDefaultChange(ctx context.Context, id string) (types.DefaultChangeStatus, error)
	RetryDefaultCleanup(ctx context.Context, id string) (types.DefaultChangeStatus, error)
	DefaultChangeStatus(ctx context.Context) (types.DefaultChangeStatus, error)
	WithLogbookChange(fn func() error) error
}

// SetDefaultChanger injects the port (cmd/smd).
func (s *Server) SetDefaultChanger(c DefaultChanger) { s.defaultChanger = c }

// SetLiveEntryGate injects the live-entry gate POST /v1/qso admits through.
func (s *Server) SetLiveEntryGate(g *LiveEntryGate) { s.liveEntry = g }

// DefaultChangeRoutesReleased registers the /v1/default-logbook routes. It
// stayed false until W-0024 slices 4 (the stale-tab guard) and 5 (the
// confirmation and recovery) landed — hiding the button would not have made an
// exposed mutation safe — and is released by slice 5c. Read when New builds
// the routes; tests may set it before.
var DefaultChangeRoutesReleased = true

// registerDefaultChangeRoutes registers the default logbook's change at an
// attended restart (ADR 0097), only when released.
func (s *Server) registerDefaultChangeRoutes(apiMux *http.ServeMux) {
	if !DefaultChangeRoutesReleased {
		return
	}
	apiMux.HandleFunc("GET /v1/default-logbook", s.handleDefaultChangeStatus)
	apiMux.Handle("POST /v1/default-logbook", s.requireArchive(http.HandlerFunc(s.handleRequestDefaultChange)))
	apiMux.Handle("POST /v1/default-logbook/cancel", s.requireArchive(http.HandlerFunc(s.handleCancelDefaultChange)))
	apiMux.Handle("POST /v1/default-logbook/confirm", s.requireArchive(http.HandlerFunc(s.handleConfirmDefaultChange)))
	apiMux.Handle("POST /v1/default-logbook/retry-cleanup", s.requireArchive(http.HandlerFunc(s.handleRetryDefaultCleanup)))
}

func (s *Server) defaultChangeUnavailable(w http.ResponseWriter, op errors.Op) bool {
	if s.defaultChanger != nil {
		return false
	}
	s.writeError(w, http.StatusServiceUnavailable, "default_change_unavailable", "this daemon has no default-change port wired", op)
	return true
}

func (s *Server) handleDefaultChangeStatus(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleDefaultChangeStatus"
	if s.defaultChangeUnavailable(w, op) {
		return
	}
	out, err := s.defaultChanger.DefaultChangeStatus(r.Context())
	if err != nil {
		s.writeArchiveError(w, op, err, "default_change_status_failed", "the default change could not be read")
		return
	}
	s.writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRequestDefaultChange(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleRequestDefaultChange"
	if s.defaultChangeUnavailable(w, op) {
		return
	}
	var req types.DefaultChangeRequest
	if !s.readCommandJSON(w, r, op, &req) {
		return
	}
	out, err := s.defaultChanger.RequestDefaultChange(r.Context(), req)
	if err != nil {
		s.writeArchiveError(w, op, err, "default_change_failed", "the default change could not be requested")
		return
	}
	s.writeJSON(w, http.StatusAccepted, out)
}

func (s *Server) handleCancelDefaultChange(w http.ResponseWriter, r *http.Request) {
	s.defaultChangeAction(w, r, "api.handleCancelDefaultChange", func(c DefaultChanger) func(context.Context, string) (types.DefaultChangeStatus, error) {
		return c.CancelDefaultChange
	})
}

func (s *Server) handleConfirmDefaultChange(w http.ResponseWriter, r *http.Request) {
	s.defaultChangeAction(w, r, "api.handleConfirmDefaultChange", func(c DefaultChanger) func(context.Context, string) (types.DefaultChangeStatus, error) {
		return c.ConfirmDefaultChange
	})
}

func (s *Server) handleRetryDefaultCleanup(w http.ResponseWriter, r *http.Request) {
	s.defaultChangeAction(w, r, "api.handleRetryDefaultCleanup", func(c DefaultChanger) func(context.Context, string) (types.DefaultChangeStatus, error) {
		return c.RetryDefaultCleanup
	})
}

// defaultChangeAction is a Cancel, Confirm or Retry cleanup naming its request.
func (s *Server) defaultChangeAction(w http.ResponseWriter, r *http.Request, op errors.Op,
	pick func(DefaultChanger) func(context.Context, string) (types.DefaultChangeStatus, error)) {
	if s.defaultChangeUnavailable(w, op) {
		return
	}
	var req types.DefaultChangeAction
	if !s.readCommandJSON(w, r, op, &req) {
		return
	}
	out, err := pick(s.defaultChanger)(r.Context(), req.RequestID)
	if err != nil {
		s.writeArchiveError(w, op, err, "default_change_failed", "the default change action failed")
		return
	}
	s.writeJSON(w, http.StatusOK, out)
}
