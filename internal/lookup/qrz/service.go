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
	client        *http.Client

	isInitialized atomic.Bool
	initOnce      sync.Once

	sessionKey string
}

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
				// Soft-disable on session-fetch failure (network
				// timeout, DNS failure, QRZ.com itself down, or
				// invalid credentials). The "Enrichment never blocks
				// logging" invariant says external-service failures
				// must not prevent the operator from logging — and
				// blocking daemon startup is an even stronger break
				// of that rule than blocking a single Lookup. So we
				// log loudly, flip Enabled=false (Lookup short-
				// circuits cleanly), and return nil so the daemon
				// continues starting. cmd/smd checks Config.Enabled
				// after Initialize and skips the provider when
				// false. Operator sees the warning in the log; if
				// it's credentials they fix config.json; if it's
				// network they wait for it to come back (a future
				// retry-on-first-lookup or periodic-reconnect path
				// can revive the provider without a daemon restart,
				// but that's a separate piece of work).
				s.Config.Enabled = false
				s.LoggerService.WarnWith().
					Err(err).
					Msg("QRZ session key fetch failed; service disabled (operator can still log QSOs; check credentials or network)")
				return
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

	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("invalid QRZ base URL")
	}
	q := u.Query()
	q.Set("s", s.sessionKey)
	q.Set("callsign", callsign)
	q.Set("agent", s.Config.UserAgent)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("failed to create HTTP GET request")
	}
	req.Header.Set("User-Agent", s.Config.UserAgent)
	req.Header.Set("Accept", "application/xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("failed to perform HTTP GET request")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return types.ContactedStation{}, errors.New(op).
			WithMsgf("QRZ.com returned status %d: %s", resp.StatusCode, string(b))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("failed to read response body")
	}

	station, err := s.unmarshalResponse(body)
	if err != nil {
		// ErrNotFound bubbles up unchanged so the chain runner can
		// branch — distinct from transport/parse failures, which the
		// orchestrator also treats as "try next" but logs differently.
		if stderr.Is(err, errors.ErrNotFound) {
			return types.ContactedStation{}, err
		}
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("failed to unmarshal response body")
	}

	return station, nil
}
