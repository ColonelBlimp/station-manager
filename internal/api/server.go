package api

import (
	"context"
	stderr "errors"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/frontend"
	"github.com/ColonelBlimp/station-manager/internal/api/httpkit"
	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/manual"
)

// Server holds the HTTP server and its dependencies.
type Server struct {
	httpServer               *http.Server
	listener                 net.Listener
	cfg                      *config.Service
	qso                      *qsoservice.Service
	db                       *sqlite.Service
	logger                   *logging.Service
	hub                      *events.Hub
	enrich                   *lookup.Orchestrator
	mailer                   *email.Service
	bridge                   *bridge.Service
	ft8                      *ft8.Service
	limits                   *loadLimiter
	kit                      *httpkit.Kit
	protocol                 string
	socketPath               string
	defaultPageLimit         int
	maxPageLimit             int
	maxContactHistoryResults int
	daemonVersion            string
	// smcloudRec runs one SM Cloud reconcile pass (POST /v1/smcloud/reconcile).
	// Injected by cmd/smd via SetSmcloudReconcile when an enabled smcloud
	// forwarder exists; nil → the route answers 503.
	smcloudRec SmcloudReconcileFunc
	// restart triggers a graceful daemon restart (POST /v1/restart). Injected by
	// cmd/smd via SetRestart; nil → the route answers 503.
	restart RestartFunc
	// shutdownCh is closed by Shutdown to signal long-lived handlers
	// (the SSE event stream) that they should return promptly. r.Context()
	// alone does NOT fire on http.Server.Shutdown — only on connection
	// close — so without this signal an idle SSE subscriber keeps
	// Shutdown blocked until the configured graceful-shutdown timeout
	// expires and the listener force-closes the connection.
	shutdownCh chan struct{}
	// shutdownOnce guards the shutdownCh close so Shutdown is idempotent —
	// http.Server.Shutdown is itself safe to call repeatedly, but an
	// unguarded close(shutdownCh) panics ("close of closed channel") on a
	// second call (a future test, supervisor retry, or duplicated teardown).
	shutdownOnce sync.Once
	// listenerMu guards listener: it's assigned in ListenAndServe (serve
	// goroutine) and read by StopAccepting (shutdown goroutine).
	listenerMu sync.Mutex
}

// New constructs a Server from the resolved services and config. The
// daemonVersion string is served by /v1/version; "dev" is the usual
// placeholder when no build-time version is injected.
//
// cfgSvc is the live, mutex-guarded config.Service that GET/PUT
// /v1/config reads from and writes to. Distinct from the cfg snapshot
// used to materialise startup-time tunables (timeouts, limits,
// protocol) because those don't change at runtime; the config-update
// endpoint only touches operator-relevant fields (logging_station,
// default_*_id) which startup doesn't bake into Server fields.
func New(cfg config.Config, daemonVersion string, cfgSvc *config.Service, qso *qsoservice.Service, db *sqlite.Service, logger *logging.Service, hub *events.Hub, enrich *lookup.Orchestrator, mailer *email.Service, br *bridge.Service, ft8Svc *ft8.Service) *Server {
	s := &Server{
		cfg:                      cfgSvc,
		qso:                      qso,
		db:                       db,
		logger:                   logger,
		hub:                      hub,
		enrich:                   enrich,
		mailer:                   mailer,
		bridge:                   br,
		ft8:                      ft8Svc,
		limits:                   newLoadLimiter(cfg.Server.MaxConcurrentRequests, cfg.Server.MaxEventSubscribers, cfg.Server.SubmitRatePerSec, cfg.Server.SubmitRateBurst),
		kit:                      httpkit.New(logger, cfg.Server.MaxBodyBytes),
		protocol:                 cfg.Server.Protocol,
		defaultPageLimit:         cfg.Server.DefaultPageLimit,
		maxPageLimit:             cfg.Server.MaxPageLimit,
		maxContactHistoryResults: cfg.Server.MaxContactHistoryResults,
		daemonVersion:            daemonVersion,
		shutdownCh:               make(chan struct{}),
	}

	mux := http.NewServeMux()

	// QSO — POST /v1/qso carries the hottest per-endpoint cap (token
	// bucket). See docs/v2-design/api.md §6 for the threat model.
	mux.Handle("POST /v1/qso", s.limitSubmitRate(http.HandlerFunc(s.handleSubmitQso)))
	mux.HandleFunc("GET /v1/qso/{uuid}", s.handleGetQso)
	mux.HandleFunc("PATCH /v1/qso/{uuid}", s.handleUpdateQso)
	mux.HandleFunc("DELETE /v1/qso/{uuid}", s.handleDeleteQso)
	mux.HandleFunc("GET /v1/qso/{uuid}/uploads", s.handleListQsoUploads)

	// Manual upload backfill (ADR 0039) — the logbook SPA queues selected
	// stored QSOs for upload to one enabled forwarder. See
	// handler_forwarder_uploads.go.
	mux.HandleFunc("POST /v1/forwarder/{name}/uploads", s.handleEnqueueForwarderUploads)

	// SM Cloud on-demand reconcile (ADR 0040 S4) — one detect+heal pass now.
	// 503 until cmd/smd wires an enabled smcloud forwarder's reconciler.
	mux.HandleFunc("POST /v1/smcloud/reconcile", s.handleSmcloudReconcile)
	mux.HandleFunc("POST /v1/restart", s.handleRestart)

	// Logbook CRUD
	mux.HandleFunc("GET /v1/logbook", s.handleListLogbooks)
	mux.HandleFunc("GET /v1/logbook/{id}", s.handleGetLogbook)
	mux.HandleFunc("POST /v1/logbook", s.handleCreateLogbook)
	mux.HandleFunc("PATCH /v1/logbook/{id}", s.handleUpdateLogbook)
	mux.HandleFunc("DELETE /v1/logbook/{id}", s.handleDeleteLogbook)
	mux.HandleFunc("GET /v1/logbook/{id}/qso", s.handleListQsoByLogbook)
	mux.HandleFunc("GET /v1/logbook/{id}/count", s.handleLogbookCount)

	// Contest
	mux.HandleFunc("GET /v1/contest-dupe", s.handleContestDupe)

	// QSO draft support
	mux.HandleFunc("GET /v1/contact-history", s.handleContactHistory)

	// Enrichment pipeline (ADR 0017). Always-200; SPA fires this on
	// Tab-out from the Callsign field.
	mux.HandleFunc("GET /v1/enrich/callsign", s.handleEnrichCallsign)

	// Session ADIF email — SessionPanel "send" button. Daemon owns
	// SMTP creds; SPA owns the session list. Body shape and contract
	// in handler_session_email.go.
	mux.HandleFunc("POST /v1/session/email", s.handleSessionEmail)
	mux.HandleFunc("POST /v1/session/export", s.handleSessionExport)

	// Event stream (SSE firehose — see docs/v2-design/api.md §4.5).
	// Wrapped with its own subscriber cap (NOT counted against the
	// general concurrent-request limit since SSE connections are
	// long-lived by design).
	mux.Handle("GET /v1/events", s.limitEventSubscribers(http.HandlerFunc(s.handleEvents)))

	// Config — operator's writable subset (logging_station,
	// default_*_id) plus joined display details for the default
	// logbook/rig. See docs/v2-design/api.md.
	mux.HandleFunc("GET /v1/config", s.handleGetConfig)
	mux.HandleFunc("PUT /v1/config", s.handlePutConfig)

	// Hardware enumeration — serial ports + audio devices for the config
	// SPA's rig-profile pickers (friendly labels, no hand-typed ids).
	mux.HandleFunc("GET /v1/hardware", s.handleHardware)

	// Rig-profiles editor surface — configured rigs (overrides) + the rigdef
	// catalogue (defaults) for the config SPA's default-vs-override editing.
	mux.HandleFunc("GET /v1/rigs", s.handleRigs)

	// Data-driven forwarder-type descriptors for the config SPA's Forwarding tab
	// (display name, supported actions, credential field shapes).
	mux.HandleFunc("GET /v1/forwarder-types", s.handleForwarderTypes)

	// Operational
	mux.HandleFunc("GET /v1/healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/version", s.handleVersion)

	// Rig SSE — opt-in via cfg.Bridge.Enabled (ADR 0013 + ADR 0019).
	// When the bridge subsystem is disabled (master smd / headless
	// deployments without a rig) the route is not registered, so
	// clients see 404 → SPA fallthrough rather than a stalled
	// EventSource. The bridge.Service.HTTPHandler closure subscribes
	// to the bridge's internal hub for each new SSE connection;
	// disconnect-safety + multi-subscriber fan-out are handled there.
	//
	// Wrapped with limitEventSubscribers — same cap (MaxEventSubscribers,
	// default 16) as /v1/events. Without this, a misbehaving client
	// could open unlimited rig SSE connections; limitConcurrent doesn't
	// apply to SSE by design (long-lived). The two SSE endpoints share
	// the cap because both consume the same "long-lived subscriber
	// slot" resource.
	if br != nil && br.Enabled() {
		mux.Handle("GET /v1/rig/events", s.limitEventSubscribers(br.HTTPHandler(s.shutdownCh)))
		// Inbound command path (ADR 0026) — same enablement gate as the
		// SSE route. A normal request (not long-lived), so the global
		// limitConcurrent middleware covers it; no per-route SSE cap.
		mux.HandleFunc("POST /v1/rig/command", s.handleRigCommand)
		// Tune-carrier control (ADR 0027) — the first TX endpoint. Same
		// enablement gate; a normal (not long-lived) request, so the global
		// limitConcurrent middleware covers it. The daemon owns the
		// guaranteed stop, so this can't strand a carrier.
		mux.HandleFunc("POST /v1/rig/tune", s.handleRigTune)
	}

	// FT8 occupancy SSE — opt-in via ft8.enabled (ADR 0029 step a). When the
	// FT8 subsystem is disabled the route is not registered (404 → SPA
	// fallthrough), same shape as the rig SSE. The ft8.Service.HTTPHandler
	// closure subscribes to the subsystem's occupancy hub per connection, with
	// a one-slot replay cache for late subscribers; disconnect-safety +
	// multi-subscriber fan-out are handled there. Shares the SSE subscriber cap
	// (limitEventSubscribers) with the other two long-lived streams. Live
	// occupancy needs a CGO capture build; on the static build the subsystem is
	// idle and the stream simply carries keepalives until a slot is processed.
	if ft8Svc != nil && ft8Svc.Enabled() {
		mux.Handle("GET /v1/ft8/events", s.limitEventSubscribers(ft8Svc.HTTPHandler(s.shutdownCh)))
		// FT8 transmit (ADR 0030 step e1) — arm/disarm the TX path and queue a
		// message on the next slot. Same enablement gate as the SSE route;
		// normal (not long-lived) requests, so the global limitConcurrent
		// middleware covers them. The daemon owns the guaranteed stop, and
		// arming is refused unless a rig is connected — neither can strand RF.
		mux.HandleFunc("POST /v1/ft8/tx/arm", s.handleFt8TxArm)
		mux.HandleFunc("POST /v1/ft8/tx/send", s.handleFt8TxSend)
		// Manual sequencer (ADR 0031 step e3) — start/abandon an answer-a-CQ
		// exchange the daemon then auto-advances. Same enablement gate.
		mux.HandleFunc("POST /v1/ft8/qso/start", s.handleFt8QsoStart)
		// Work a station calling US, picked from the pile-up (ADR 0033 "work a
		// caller") — a caller-style exchange against the chosen station; idle on
		// completion. Abandon uses the shared qso/abandon route.
		mux.HandleFunc("POST /v1/ft8/qso/work", s.handleFt8QsoWork)
		mux.HandleFunc("POST /v1/ft8/qso/path", s.handleFt8QsoPath)
		mux.HandleFunc("POST /v1/ft8/qso/abandon", s.handleFt8QsoAbandon)
		mux.HandleFunc("POST /v1/ft8/qso/skip", s.handleFt8QsoSkip)
		// Caller-side sequencer (ADR 0033) — start a Call-CQ session that works the
		// stations that answer (auto_first / operator_pick per ft8.tx.caller_answer_mode);
		// abandon uses the shared qso/abandon route. Same enablement gate.
		mux.HandleFunc("POST /v1/ft8/cq/start", s.handleFt8CqStart)
	}

	// pprof — opt-in via cfg.Server.EnableProfiling. Off by default
	// because pprof exposes goroutine state, heap dumps, allocation
	// profiles, and a CPU-profile endpoint that can be used as a DoS
	// vector (`/debug/pprof/profile?seconds=N` blocks for the full
	// duration). Operators flip it on for stress / soak tests then
	// flip it back off. Lives outside /v1/* — it's a development
	// affordance, not a stable API contract, and isn't documented in
	// api.md. The handler registrations mirror what
	// `net/http/pprof.init()` does on http.DefaultServeMux, but
	// targeted at our mux so DefaultServeMux stays untouched.
	//
	// Method-specificity (GET on every entry, POST extra on symbol)
	// is load-bearing under Go 1.22 ServeMux conflict detection: the
	// SPA registers as `GET /`, which would clash with method-less
	// `/debug/pprof/` (the latter matches every method, neither is a
	// strict subset of the other → panic at register time). Making
	// pprof routes GET-only puts them inside `GET /`'s method-set,
	// resolving the conflict. POST /debug/pprof/symbol is registered
	// alongside GET because `go tool pprof`'s batched-address-
	// resolution flow uses POST.
	if cfg.Server.EnableProfiling {
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("POST /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
		logger.WarnWith().Msg("server: pprof endpoints mounted at /debug/pprof/* — disable in production")
	}

	// SPA — served at the root. Anything not matched by /v1/* falls
	// through to the SPA's index.html so client-side routing handles
	// /log, /logbook, /config, etc. Conditional on TCP + opt-in so a
	// Unix-socket headless deployment stays a supported shape (browsers
	// can only reach TCP listeners). See docs/v2-design/frontend-spa.md
	// and docs/v2-design/topology.md.
	if cfg.Server.Protocol == "tcp" && cfg.Server.ServeSPA != nil && *cfg.Server.ServeSPA {
		// Config SPA at the /config/ sub-path. StripPrefix turns
		// "/config/assets/index.js" into "/assets/index.js" so the shared
		// spaHandler resolves it (and unknown sub-routes) against the
		// config dist/. Registered as a subtree pattern, so ServeMux
		// 301-redirects a bare "/config" to "/config/". This more-specific
		// pattern takes priority over the "/" catch-all below.
		mux.Handle("GET /config/", http.StripPrefix("/config", spaHandler(frontend.ConfigFS())))
		// Logbook SPA at the /logbook/ sub-path — same StripPrefix +
		// subtree-redirect rationale as the config SPA above.
		mux.Handle("GET /logbook/", http.StripPrefix("/logbook", spaHandler(frontend.LogbookFS())))
		// Consolidated app SPA (ADR 0044) at the /app/ sub-path — the
		// full-replacement operator client, dogfoodable on the embedded daemon
		// alongside the shipping SPAs during the consolidation. Same StripPrefix
		// + subtree-redirect rationale as the config SPA above.
		mux.Handle("GET /app/", http.StripPrefix("/app", spaHandler(frontend.AppFS())))
		// Operator manual at /manual/ — a static, zero-JS Hugo page (ADR 0036),
		// NOT an SPA, so it uses manualHandler (plain file server, real 404s) not
		// spaHandler. Same StripPrefix + subtree-redirect rationale as the SPAs.
		mux.Handle("GET /manual/", http.StripPrefix("/manual", manualHandler(manual.FS())))
		// The root redirects to the app SPA (ADR 0044). The legacy logging SPA that
		// served "/" as a catch-all was retired 2026-07-21. This is an EXACT-match
		// "/{$}" pattern, not a catch-all, so there is no longer a root SPA fallthrough:
		// any GET path matching no route (incl. /debug/pprof/* when profiling is off)
		// is a clean 404 rather than SPA HTML.
		mux.Handle("GET /{$}", http.RedirectHandler("/app/", http.StatusFound))
	}

	// Middleware chain — outermost first:
	//   logRequests       — access log; observes every completion (incl.
	//                       503 from limitConcurrent and 500 from recoverPanic)
	//   limitConcurrent   — non-SSE concurrent-request cap
	//   recoverPanic      — panic safety net + structured panic log
	//   mux               — per-route handlers (with their own per-route
	//                       middleware like limitSubmitRate / limitEventSubscribers)
	s.httpServer = &http.Server{
		Handler: s.logRequests(s.limitConcurrent(s.recoverPanic(s.requireSameOrigin(mux)))),
		// ReadHeaderTimeout caps the pre-handler request-header read
		// independently of ReadTimeout (which budgets headers + body
		// together and is operator-tunable up to longer values for
		// slow clients). A short fixed cap here closes a
		// slowloris-style slow-headers DoS surface — a malicious or
		// buggy client holding the connection open in the headers
		// phase consumes a TCP socket but no MaxConcurrentRequests
		// slot until the handler runs, so without this cap the
		// concurrent-request limit doesn't protect the listener
		// itself. Review finding M3 (2026-05-07).
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeoutSec) * time.Second,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout:      time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
		IdleTimeout:       time.Duration(cfg.Server.IdleTimeoutSec) * time.Second,
	}

	return s
}

// ListenAndServe binds the listener and starts serving.
func (s *Server) ListenAndServe(socketPath string) error {
	const op errors.Op = "api.ListenAndServe"

	// For Unix sockets, remove a stale socket file before binding — but only if
	// the existing path is actually a socket. A typo'd socket_path aimed at a
	// regular file the daemon can delete (config.json, an ADIF export, another
	// user file) would otherwise be unlinked on startup, which is destructive
	// since the same config can hold secrets. lstat first; refuse to start if the
	// path exists as anything other than a socket (review 2026-06-19 H1).
	if s.protocol == "unix" {
		fi, statErr := os.Lstat(socketPath)
		switch {
		case statErr != nil && !os.IsNotExist(statErr):
			return errors.New(op).WithErr(statErr).WithMsgf("inspecting socket path %s", socketPath)
		case statErr == nil && fi.Mode()&os.ModeSocket == 0:
			return errors.New(op).WithMsgf(
				"socket_path %s exists and is not a socket (refusing to remove it); "+
					"point socket_path at a free path or remove the file by hand", socketPath)
		case statErr == nil:
			if err := os.Remove(socketPath); err != nil {
				return errors.New(op).WithErr(err).WithMsg("removing stale socket")
			}
		}
	}

	ln, err := net.Listen(s.protocol, socketPath)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsgf("binding %s listener on %s", s.protocol, socketPath)
	}
	s.listenerMu.Lock()
	s.listener = ln
	s.listenerMu.Unlock()
	s.socketPath = socketPath // cached for Shutdown's socket-file cleanup

	s.logger.InfoWith().Str("protocol", s.protocol).Str("address", socketPath).Msg("HTTP server listening")

	// http.ErrServerClosed is the clean-stop signal from Shutdown; net.ErrClosed
	// is the clean-stop signal from StopAccepting closing the listener directly.
	if err = s.httpServer.Serve(ln); err != nil &&
		!stderr.Is(err, http.ErrServerClosed) && !stderr.Is(err, net.ErrClosed) {
		return errors.New(op).WithErr(err)
	}

	return nil
}

// StopAccepting closes the network listener, releasing the bound address
// IMMEDIATELY without draining in-flight connections. Call it at the very start
// of shutdown so the port frees before the (possibly multi-second) subsystem
// teardown — otherwise the listener stays bound until Shutdown runs last, and a
// replacement process racing the old one's teardown fails to bind ("address
// already in use"). Active connections are drained later by Shutdown; the Serve
// loop's resulting net.ErrClosed is treated as a clean stop. Safe if never bound,
// and idempotent (a second close is a harmless error we ignore).
func (s *Server) StopAccepting() {
	s.listenerMu.Lock()
	ln := s.listener
	s.listenerMu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

// Shutdown gracefully shuts down the HTTP server.
//
// shutdownCh is closed first so long-lived handlers (the SSE event
// stream) can observe the shutdown signal and return promptly;
// without this, http.Server.Shutdown would block on idle SSE
// connections until ctx expired and the listener force-closed them,
// turning every shutdown into a full graceful-timeout wait. Closing
// shutdownCh is a no-op for short-lived request handlers — they
// don't watch it.
//
// On Unix-socket deployments the socket file is best-effort removed
// afterwards so operators grepping /tmp for daemon state don't see a
// stale file between runs. The next startup's pre-bind cleanup also
// handles this, but removing on exit keeps the filesystem honest.
func (s *Server) Shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() { close(s.shutdownCh) })
	err := s.httpServer.Shutdown(ctx)
	if s.protocol == "unix" && s.socketPath != "" {
		// Ignore the remove error: the kernel may have already unlinked
		// the file when the listener closed, and we're on the way out.
		_ = os.Remove(s.socketPath)
	}
	return err
}
