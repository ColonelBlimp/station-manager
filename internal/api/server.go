package api

import (
	"context"
	stderr "errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

// Server holds the HTTP server and its dependencies.
type Server struct {
	httpServer   *http.Server
	listener     net.Listener
	qso          *qsoservice.Service
	db           *sqlite.Service
	logger       *logging.Service
	maxBodyBytes int64
	protocol     string
}

// New constructs a Server from the resolved services and config.
func New(cfg config.Config, qso *qsoservice.Service, db *sqlite.Service, logger *logging.Service) *Server {
	s := &Server{
		qso:          qso,
		db:           db,
		logger:       logger,
		maxBodyBytes: cfg.Server.MaxBodyBytes,
		protocol:     cfg.Server.Protocol,
	}

	mux := http.NewServeMux()

	// QSO
	mux.HandleFunc("POST /v1/qso", s.handleSubmitQso)
	mux.HandleFunc("GET /v1/qso/{id}", s.handleGetQso)

	// Logbook CRUD
	mux.HandleFunc("GET /v1/logbook", s.handleListLogbooks)
	mux.HandleFunc("GET /v1/logbook/{id}", s.handleGetLogbook)
	mux.HandleFunc("POST /v1/logbook", s.handleCreateLogbook)
	mux.HandleFunc("PATCH /v1/logbook/{id}", s.handleUpdateLogbook)
	mux.HandleFunc("DELETE /v1/logbook/{id}", s.handleDeleteLogbook)

	// Operational
	mux.HandleFunc("GET /v1/healthz", s.handleHealthz)

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeoutSec) * time.Second,
	}

	return s
}

// ListenAndServe binds the listener and starts serving.
func (s *Server) ListenAndServe(socketPath string) error {
	const op errors.Op = "api.ListenAndServe"

	// For Unix sockets, remove stale socket file if it exists.
	if s.protocol == "unix" {
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			return errors.New(op).WithErr(err).WithMsg("removing stale socket")
		}
	}

	ln, err := net.Listen(s.protocol, socketPath)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("binding %s listener on %s", s.protocol, socketPath)
	}
	s.listener = ln

	s.logger.InfoWith().Str("protocol", s.protocol).Str("address", socketPath).Msg("HTTP server listening")

	if err = s.httpServer.Serve(ln); err != nil && !stderr.Is(err, http.ErrServerClosed) {
		return errors.New(op).WithErr(err)
	}

	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
