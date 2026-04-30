package api

import (
	"context"
	stderr "errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ColonelBlimp/station-manager/frontend"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

// Server holds the HTTP server and its dependencies.
type Server struct {
	httpServer               *http.Server
	listener                 net.Listener
	qso                      *qsoservice.Service
	db                       *sqlite.Service
	logger                   *logging.Service
	hub                      *events.Hub
	limits                   *loadLimiter
	maxBodyBytes             int64
	protocol                 string
	socketPath               string
	defaultPageLimit         int
	maxPageLimit             int
	maxContactHistoryResults int
	daemonVersion            string
}

// New constructs a Server from the resolved services and config. The
// daemonVersion string is served by /v1/version; "dev" is the usual
// placeholder when no build-time version is injected.
func New(cfg config.Config, daemonVersion string, qso *qsoservice.Service, db *sqlite.Service, logger *logging.Service, hub *events.Hub) *Server {
	s := &Server{
		qso:                      qso,
		db:                       db,
		logger:                   logger,
		hub:                      hub,
		limits:                   newLoadLimiter(cfg.Server.MaxConcurrentRequests, cfg.Server.MaxEventSubscribers, cfg.Server.SubmitRatePerSec, cfg.Server.SubmitRateBurst),
		maxBodyBytes:             cfg.Server.MaxBodyBytes,
		protocol:                 cfg.Server.Protocol,
		defaultPageLimit:         cfg.Server.DefaultPageLimit,
		maxPageLimit:             cfg.Server.MaxPageLimit,
		maxContactHistoryResults: cfg.Server.MaxContactHistoryResults,
		daemonVersion:            daemonVersion,
	}

	mux := http.NewServeMux()

	// QSO — POST /v1/qso carries the hottest per-endpoint cap (token
	// bucket). See docs/v2-design/api.md §6 for the threat model.
	mux.Handle("POST /v1/qso", s.limitSubmitRate(http.HandlerFunc(s.handleSubmitQso)))
	mux.HandleFunc("GET /v1/qso/{id}", s.handleGetQso)
	mux.HandleFunc("PATCH /v1/qso/{id}", s.handleUpdateQso)
	mux.HandleFunc("DELETE /v1/qso/{id}", s.handleDeleteQso)
	mux.HandleFunc("GET /v1/qso/{id}/uploads", s.handleListQsoUploads)

	// Logbook CRUD
	mux.HandleFunc("GET /v1/logbook", s.handleListLogbooks)
	mux.HandleFunc("GET /v1/logbook/{id}", s.handleGetLogbook)
	mux.HandleFunc("POST /v1/logbook", s.handleCreateLogbook)
	mux.HandleFunc("PATCH /v1/logbook/{id}", s.handleUpdateLogbook)
	mux.HandleFunc("DELETE /v1/logbook/{id}", s.handleDeleteLogbook)
	mux.HandleFunc("GET /v1/logbook/{id}/qso", s.handleListQsoByLogbook)

	// Contest
	mux.HandleFunc("GET /v1/contest-dupe", s.handleContestDupe)

	// QSO draft support
	mux.HandleFunc("GET /v1/contact-history", s.handleContactHistory)

	// Event stream (SSE firehose — see docs/v2-design/api.md §4.5).
	// Wrapped with its own subscriber cap (NOT counted against the
	// general concurrent-request limit since SSE connections are
	// long-lived by design).
	mux.Handle("GET /v1/events", s.limitEventSubscribers(http.HandlerFunc(s.handleEvents)))

	// Operational
	mux.HandleFunc("GET /v1/healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/version", s.handleVersion)

	// SPA — served at the root. Anything not matched by /v1/* falls
	// through to the SPA's index.html so client-side routing handles
	// /log, /logbook, /config, etc. Conditional on TCP + opt-in so a
	// Unix-socket headless deployment stays a supported shape (browsers
	// can only reach TCP listeners). See docs/v2-design/frontend-spa.md
	// and docs/v2-design/topology.md.
	if cfg.Server.Protocol == "tcp" && cfg.Server.ServeSPA != nil && *cfg.Server.ServeSPA {
		mux.Handle("GET /", spaHandler(frontend.LoggingFS()))
	}

	s.httpServer = &http.Server{
		Handler:      s.limitConcurrent(s.recoverPanic(mux)),
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
	s.socketPath = socketPath // cached for Shutdown's socket-file cleanup

	s.logger.InfoWith().Str("protocol", s.protocol).Str("address", socketPath).Msg("HTTP server listening")

	if err = s.httpServer.Serve(ln); err != nil && !stderr.Is(err, http.ErrServerClosed) {
		return errors.New(op).WithErr(err)
	}

	return nil
}

// Shutdown gracefully shuts down the HTTP server. On Unix-socket
// deployments the socket file is best-effort removed afterwards so
// operators grepping /tmp for daemon state don't see a stale file
// between runs. The next startup's pre-bind cleanup also handles this,
// but removing on exit keeps the filesystem honest.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.httpServer.Shutdown(ctx)
	if s.protocol == "unix" && s.socketPath != "" {
		// Ignore the remove error: the kernel may have already unlinked
		// the file when the listener closed, and we're on the way out.
		_ = os.Remove(s.socketPath)
	}
	return err
}
