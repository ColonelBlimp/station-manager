// Package qrz implements lookup.CallsignProvider against the QRZ.com
// XML API.
//
// QRZ.com is a CallsignProvider per ADR 0017 — it supplies operator-
// shaped fields (name, QTH, gridsquare, license class, etc.) by
// callsign. Country / CQ / ITU / continent / DXCC fields populated on
// the returned struct are filtered out at the orchestrator's merge
// boundary via lookup.FilterToCallsignFields; this package leaves
// them populated so the upstream parse stays faithful, and the
// single enforcement point lives next to the orchestrator.
//
// Auth: session-key model. Initialize fetches a session key with the
// configured username/password; Lookup uses it. Session keys can
// expire (typically after 24h of inactivity) — a separate package,
// `internal/forwarding/qrz`, handles QSO uploads to a similar API
// surface; the two are deliberately distinct because they have
// different lifecycle and failure modes (uploads retry; lookups do
// not).
package qrz

import (
	"context"
	stderr "errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// ServiceName is the DI bean ID and the LookupConfig.Name value the
// orchestrator matches on.
const ServiceName = types.QRZLookupServiceName

// Compile-time check that Service satisfies lookup.CallsignProvider.
var _ lookup.CallsignProvider = (*Service)(nil)

// Service is the QRZ.com CallsignProvider implementation.
//
// Construction paths mirror the hamnut provider — DI via tag
// injection or direct construction via NewService.
type Service struct {
	ConfigService *config.Service  `di.inject:"configservice"`
	LoggerService *logging.Service `di.inject:"loggingservice"`
	Config        *types.LookupConfig
	// UserAgent is the per-request User-Agent header value. Sourced
	// from the daemon's global Config.UserAgent at construction time
	// (see cmd/smd/main.go). Fails Initialize loudly when empty if
	// Config.Enabled is true.
	UserAgent string
	client    *http.Client

	isInitialized atomic.Bool
	initOnce      sync.Once

	// sessionMu guards sessionKey, written by Initialize and by the
	// mid-session re-auth path (review 2026-06-04 M1) and read by every lookup.
	sessionMu  sync.Mutex
	sessionKey string

	// authMu single-flights lazy session-key (re)acquisition (ensureSessionKey) so
	// a burst of concurrent lookups triggers at most one QRZ login at a time.
	// lastAuth* (guarded by authMu) drive the retry cooldown.
	authMu          sync.Mutex
	lastAuthAttempt time.Time
	lastAuthErr     error
}

// sessionRetryCooldown bounds how often a keyless service re-attempts the QRZ
// session-key login (ensureSessionKey): a persistent failure (prolonged outage,
// bad credentials) then retries at most once per cooldown instead of once per
// lookup — quick enough to recover names within a slot or two of a flaky link
// returning, sparse enough not to hammer QRZ. A var so tests can shorten it.
var sessionRetryCooldown = 30 * time.Second

// NewService constructs a Service with the given dependencies. Used
// by tests that need a custom *http.Client pointing at httptest;
// production wiring uses zero-value initialization + DI tag
// injection and should not call this.
func NewService(logger *logging.Service, cfgSvc *config.Service, cfg *types.LookupConfig, client *http.Client) *Service {
	return &Service{
		LoggerService: logger,
		ConfigService: cfgSvc,
		Config:        cfg,
		client:        client,
	}
}

// Name returns the stable provider identifier.
func (s *Service) Name() string { return ServiceName }

// Initialize wires the Service against its dependencies and fetches
// the session key. Idempotent. If session-key fetch fails the service
// is marked disabled (any subsequent Lookup short-circuits) — that
// matches v1's "any error here and we should disable the service"
// behaviour and matches ADR 0017's implicit-fall-through model: if
// QRZ auth is broken at startup, the chain runner skips QRZ.
//
// ctx is the daemon-lifecycle context — propagated into the
// session-key HTTP call so daemon shutdown can interrupt a stuck
// TLS handshake or hung response read (review M4). The HTTP
// client's per-request timeout (HttpTimeoutSec) still bounds the
// call in absolute terms; ctx provides the orthogonal cooperative-
// cancellation channel that the operator-triggered shutdown path
// needs.
func (s *Service) Initialize(ctx context.Context) error {
	const op errors.Op = "qrz.Service.Initialize"
	if s.isInitialized.Load() {
		return nil
	}

	var initErr error
	s.initOnce.Do(func() {
		if s.LoggerService == nil {
			initErr = errors.New(op).WithMsg("logger service has not been set/injected")
			return
		}

		if s.Config == nil {
			if s.ConfigService == nil {
				initErr = errors.New(op).WithMsg("application config has not been set/injected")
				return
			}
			cfg, lerr := s.ConfigService.LookupServiceConfig(ServiceName)
			if lerr != nil {
				initErr = errors.New(op).WithErr(lerr).WithMsg("loading QRZ config from config service")
				return
			}
			s.Config = &cfg
		}

		if err := s.validateConfig(op); err != nil {
			initErr = err
			return
		}

		if s.client == nil && s.Config.Enabled {
			s.client = utils.NewHTTPClient(time.Duration(s.Config.HttpTimeoutSec) * time.Second)
		}

		if s.Config.Enabled {
			if err := s.requestAndSetSessionKey(ctx); err != nil {
				// Do NOT disable on a startup session-key failure. On a flaky /
				// bandwidth-contended link — the target operating environment
				// (7Q8AC, and the author's own Malawi station) — one boot-time
				// timeout must not kill enrichment for the whole run: that turned a
				// single blip into hours of nameless QSOs with no recovery short of a
				// daemon restart (found on-air 2026-07-04). Instead we stay Enabled
				// with no key; lookups lazily re-fetch it (ensureSessionKey, cooldown-
				// bounded), so QRZ revives on its own once the link recovers. Until
				// then enrichment degrades to the other providers (country still
				// resolves) — the "enrichment never blocks logging" invariant holds.
				// Note: leaving lastAuthAttempt zero means the FIRST lookup retries
				// immediately (fast recovery) rather than waiting out a cooldown.
				s.LoggerService.WarnWith().
					Err(err).
					Msg("QRZ session key fetch failed at startup; will retry lazily on lookups (names degrade to other providers until the link recovers)")
			}
		} else {
			s.LoggerService.InfoWith().Msg("QRZ.com lookup is disabled in the config")
		}

		s.isInitialized.Store(true)
	})

	return initErr
}

// Lookup performs a callsign lookup with context.Background().
func (s *Service) Lookup(callsign string) (types.ContactedStation, error) {
	return s.LookupWithContext(context.Background(), callsign)
}

// LookupWithContext queries QRZ.com for the callsign. Returns:
//
//   - (ContactedStation{Call:callsign}, nil) when the provider is
//     disabled — sentinel matching v1's contract; the orchestrator
//     normally skips disabled providers entirely so this is just a
//     defensive corner case.
//   - (ContactedStation{}, errors.ErrNotFound) when QRZ reports the
//     callsign is not in its database. The chain runner branches on
//     ErrNotFound to proceed to the next provider per ADR 0017 #8.
//   - (ContactedStation{}, error) for transport / parse / non-2xx /
//     auth-style errors. The orchestrator's implicit-fall-through
//     (ADR 0017 #7) treats these as "QRZ unreachable" and proceeds
//     to the next chain provider.
//   - (ContactedStation{...}, nil) on success. Country / CQ / ITU /
//     DXCC fields may be populated; the orchestrator's
//     FilterToCallsignFields strips them at the merge boundary per
//     ADR 0017 #2.
func (s *Service) LookupWithContext(ctx context.Context, callsign string) (types.ContactedStation, error) {
	const op errors.Op = "qrz.Service.LookupWithContext"
	if ctx == nil {
		ctx = context.Background()
	}

	if !s.isInitialized.Load() {
		return types.ContactedStation{}, errors.New(op).WithMsg("service is not initialized")
	}
	if s.Config == nil {
		return types.ContactedStation{}, errors.New(op).WithMsg("service config is not set")
	}

	callsign = strings.TrimSpace(callsign)
	if !s.Config.Enabled {
		// Sentinel matches v1's "disabled returns empty-but-with-call"
		// contract. Orchestrator should normally skip disabled
		// providers from the chain entirely (Enabled flag on the
		// LookupConfig); this is the defensive fallback.
		return types.ContactedStation{Call: callsign}, nil
	}
	if s.client == nil {
		return types.ContactedStation{}, errors.New(op).WithMsg("http client is not configured")
	}
	if callsign == "" {
		return types.ContactedStation{}, errors.New(op).WithMsg("callsign cannot be empty")
	}

	// Lazily (re)acquire the session key if we don't have one — recovers a service
	// that started keyless after a boot-time login failure, without a restart. On a
	// still-failing link this returns fail-soft (the orchestrator falls through to
	// the next provider), never blocking the log.
	if err := s.ensureSessionKey(ctx); err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("no QRZ session key (will retry)")
	}

	station, err := s.lookupOnce(ctx, callsign)
	if err != nil && stderr.Is(err, errSessionExpired) {
		// The session key expired mid-session. Re-authenticate once and retry
		// the lookup once (review 2026-06-04 M1). No loop: a second expiry
		// means credentials / QRZ are genuinely broken, not a stale key.
		if rerr := s.requestAndSetSessionKey(ctx); rerr != nil {
			s.LoggerService.WarnWith().Err(rerr).Msg("QRZ session re-auth failed after expiry")
			return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("session expired and re-auth failed")
		}
		s.LoggerService.InfoWith().Msg("QRZ session re-authenticated after expiry; retrying lookup")
		station, err = s.lookupOnce(ctx, callsign)
	}
	if err != nil {
		// ErrNotFound bubbles up unchanged so the chain runner can branch —
		// distinct from transport / parse / session failures, which the
		// orchestrator also treats as "try next" but logs differently.
		if stderr.Is(err, errors.ErrNotFound) {
			return types.ContactedStation{}, err
		}
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("lookup failed")
	}
	return station, nil
}

// lookupOnce performs a single QRZ query with the current session key and
// decodes the response. ErrNotFound, errSessionExpired, and transport errors
// all bubble (wrapped) so LookupWithContext can branch — notably re-auth on
// errSessionExpired.
func (s *Service) lookupOnce(ctx context.Context, callsign string) (types.ContactedStation, error) {
	const op errors.Op = "qrz.Service.lookupOnce"

	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("invalid QRZ base URL")
	}
	q := u.Query()
	q.Set("s", s.getSessionKey())
	q.Set("callsign", callsign)
	q.Set("agent", s.UserAgent)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("failed to create HTTP GET request")
	}
	req.Header.Set("User-Agent", s.UserAgent)
	req.Header.Set("Accept", "application/xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("failed to perform HTTP GET request")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return types.ContactedStation{}, errors.New(op).
			WithMsgf("QRZ.com returned status %d: %s", resp.StatusCode, string(b))
	}

	body, err := readLimitedBody(resp.Body)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("failed to read response body")
	}

	return s.unmarshalResponse(body)
}

// getSessionKey / setSessionKey guard sessionKey for the concurrent read
// (every lookup) vs write (Initialize + mid-session re-auth) — see sessionMu
// (review 2026-06-04 M1).
func (s *Service) getSessionKey() string {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	return s.sessionKey
}

func (s *Service) setSessionKey(key string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	s.sessionKey = key
}

// ensureSessionKey makes sure a session key is available before a lookup, lazily
// (re)fetching it when absent — the recovery path for a service that started with
// no key (a boot-time login failure on a flaky link) so it revives without a daemon
// restart. Bounded by sessionRetryCooldown so a persistent failure retries at most
// once per cooldown, not once per lookup. authMu single-flights the fetch: a burst
// of concurrent lookups makes at most one login attempt; the rest see the fresh key
// or the cooldown. A non-empty key short-circuits — including one that later proves
// expired, which LookupWithContext handles via its own re-auth path.
func (s *Service) ensureSessionKey(ctx context.Context) error {
	if s.getSessionKey() != "" {
		return nil
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	// A concurrent lookup may have acquired a key while we waited on authMu.
	if s.getSessionKey() != "" {
		return nil
	}
	if !s.lastAuthAttempt.IsZero() && time.Since(s.lastAuthAttempt) < sessionRetryCooldown {
		return s.lastAuthErr // recent failure — don't hammer QRZ; wait out the cooldown
	}
	s.lastAuthAttempt = time.Now()
	s.lastAuthErr = s.requestAndSetSessionKey(ctx)
	return s.lastAuthErr
}
