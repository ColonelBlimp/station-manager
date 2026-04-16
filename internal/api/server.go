package api

import (
	"context"
	stderr "errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Server holds the HTTP server and its dependencies.
type Server struct {
	httpServer   *http.Server
	listener     net.Listener
	qso          *qsoservice.Service
	db           *sqlite.Service
	logger       *logging.Service
	maxBodyBytes int64
}

// New constructs a Server from the resolved services and config.
func New(cfg config.Config, qso *qsoservice.Service, db *sqlite.Service, logger *logging.Service) *Server {
	s := &Server{
		qso:          qso,
		db:           db,
		logger:       logger,
		maxBodyBytes: cfg.Server.MaxBodyBytes,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/qso", s.handleSubmitQso)
	mux.HandleFunc("GET /v1/healthz", s.handleHealthz)

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeoutSec) * time.Second,
	}

	return s
}

// ListenAndServe binds the Unix domain socket and starts serving.
func (s *Server) ListenAndServe(socketPath string) error {
	// Remove stale socket file if it exists.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale socket: %w", err)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("binding unix socket %s: %w", socketPath, err)
	}
	s.listener = ln

	s.logger.InfoWith().Str("socket", socketPath).Msg("HTTP server listening")

	if err := s.httpServer.Serve(ln); err != nil && !stderr.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// resolveLogbook finds or creates a logbook for the given station callsign.
// For milestone 1 this is a simple lookup-or-create by callsign.
func (s *Server) resolveLogbook(ctx context.Context, stationCallsign string) (int64, error) {
	callsign := strings.ToUpper(strings.TrimSpace(stationCallsign))
	if callsign == "" {
		return 0, fmt.Errorf("station callsign is empty")
	}

	// Try to find an existing logbook with this callsign.
	logbooks, err := s.db.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetching logbooks: %w", err)
	}

	for _, lb := range logbooks {
		if strings.EqualFold(lb.Callsign, callsign) {
			return lb.ID, nil
		}
	}

	// No match — auto-create a logbook with the callsign as its name.
	newID, err := s.db.InsertLogbookWithContext(ctx, types.Logbook{
		Name:     callsign,
		Callsign: callsign,
	})
	if err != nil {
		return 0, errors.New("api.resolveLogbook").WithErr(err).WithMsg("failed to create logbook")
	}

	s.logger.InfoWith().Str("callsign", callsign).Int64("logbook_id", newID).Msg("auto-created logbook")
	return newID, nil
}
