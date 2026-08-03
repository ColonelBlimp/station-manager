// Package hamnut implements the lookup.CountryProvider interface
// against the Hamnut /v1/call-signs/prefixes upstream.
//
// Hamnut is the country source-of-truth per ADR 0017 — country / CQ /
// ITU / continent / DXCC fields populated on a QSO come from this
// provider (or the country-table cache it writes through), never from
// callsign-class providers like QRZ. The "QRZ records Malawi for an
// English call" example in ADR 0017's Context is exactly the
// pathology this hamnut-exclusive write rule guards against.
//
// The service is anonymous (no auth, no creds in LookupConfig); only
// URL / UserAgent / HttpTimeoutSec are read.
package hamnut

import (
	"context"
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
	"github.com/ColonelBlimp/station-manager/internal/lookupdef"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// ServiceName is the DI bean ID and the LookupConfig.Name value the
// orchestrator matches on. Mirrors the v1 constant.
const ServiceName = types.HamNutLookupServiceName

// The provider's own default HTTP timeout; the descriptor below is the single
// source for it (ADR 0062).
const defaultHTTPTimeoutSec = 10

// Registering next to the implementation is what makes adding a provider a
// package rather than a sweep across buildEnrichment, config's URL defaults,
// config's credential rules and the SPA's display map (ADR 0062). cmd/smd
// imports this package to trigger it.
//
// NeedsCredentials is FALSE and that is a design fact, not an omission: hamnut
// is anonymous, so the Settings section must not offer it credential boxes and
// the config validator must not demand a login before it can be enabled.
func init() {
	lookupdef.RegisterProvider(lookupdef.ProviderDescriptor{
		Name:              ServiceName,
		DisplayName:       "Hamnut",
		Help:              "Resolves DXCC / CQ / ITU zones from the callsign prefix. Free and anonymous — no credentials needed.",
		Kind:              lookupdef.KindCountry,
		NeedsCredentials:  false,
		DefaultURL:        types.HamNutLookupDefaultURL,
		DefaultTimeoutSec: defaultHTTPTimeoutSec,
	})
	lookup.RegisterCountryProvider(ServiceName, func(logger *logging.Service, cfg *types.LookupConfig, userAgent string) lookup.CountryProvider {
		s := NewService(logger, nil, cfg, nil)
		s.UserAgent = userAgent
		return s
	})
}

// errorBodyLimit bounds how much of a non-2xx response body is read into the
// error message — enough for an upstream error string without slurping a
// misbehaving giant body.
const errorBodyLimit = 512

// successBodyLimit caps a 2xx response body. The upstream is an
// operator-configured/remote URL; a captive portal, misconfigured endpoint, or
// compromised path could otherwise return an unbounded 200 the daemon buffers
// whole (review 2026-06-19 M1). 1 MiB is generous for Hamnut's JSON.
const successBodyLimit = 1 << 20

// Compile-time check that Service satisfies lookup.CountryProvider.
var _ lookup.CountryProvider = (*Service)(nil)

// Service is the hamnut CountryProvider implementation.
//
// Two construction paths:
//   - DI: the iocdi container injects ConfigService + LoggerService
//     via the struct tags; Initialize then resolves the LookupConfig
//     via ConfigService and builds the HTTP client.
//   - Direct: callers (notably tests) supply Config and an optional
//     *http.Client via NewService and skip the ConfigService path.
//
// Initialize is idempotent — safe to call multiple times.
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
}

// NewService constructs a Service with the given dependencies. cfg may
// be nil if ConfigService is set; client may be nil to use the daemon
// default (utils.NewHTTPClient with the config's timeout). The DI
// path uses zero-value initialization + tag injection instead and
// shouldn't call this.
func NewService(logger *logging.Service, cfgSvc *config.Service, cfg *types.LookupConfig, client *http.Client) *Service {
	return &Service{
		LoggerService: logger,
		ConfigService: cfgSvc,
		Config:        cfg,
		client:        client,
	}
}

// Name returns the stable provider identifier — used both as the DI
// bean ID and as the value the SPA receives in the response's
// `countrySource` field.
func (s *Service) Name() string { return ServiceName }

// Initialize wires the Service against its dependencies. Idempotent.
// Returns an error only when the operator's config is broken
// (missing URL, invalid URL, empty UserAgent, non-positive timeout).
//
// When Config.Enabled is false, the HTTP client is intentionally not
// constructed — Lookup short-circuits with a "disabled" sentinel
// result rather than attempting any network call.
//
// The ctx parameter is part of the lookup.CountryProvider interface
// shape (so QRZ's session-key fetch can honour daemon shutdown).
// Hamnut performs no I/O during initialization, so ctx is accepted
// for uniformity but not used here.
func (s *Service) Initialize(_ context.Context) error {
	const op errors.Op = "hamnut.Service.Initialize"
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
				initErr = errors.New(op).WithErr(lerr).WithMsg("loading hamnut config from config service")
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
		if !s.Config.Enabled && s.LoggerService != nil {
			s.LoggerService.InfoWith().Msg("Hamnut lookup is disabled in the config")
		}

		s.isInitialized.Store(true)
	})

	return initErr
}

// Lookup performs a country lookup with context.Background(). The
// LookupWithContext form is preferred from the orchestrator since
// SPA-side AbortController cancellation needs ctx propagation.
func (s *Service) Lookup(callsign string) (types.Country, error) {
	return s.LookupWithContext(context.Background(), callsign)
}

// LookupWithContext queries hamnut for the country corresponding to
// the callsign's prefix. Returns:
//
//   - (Country{Name:"Unknown"}, nil) when the provider is disabled —
//     a sentinel the orchestrator distinguishes from a real result.
//   - (Country{}, errors.ErrNotFound) when the upstream returns 404
//     OR a 200 with `{"found": false}` — both mean "hamnut doesn't
//     know this prefix"; the orchestrator writes no row in either
//     case per ADR 0017.
//   - (Country{}, error) for transport / parse / non-2xx-non-404
//     responses. The orchestrator's implicit-fall-through policy
//     (ADR 0017 #7) treats these as "internet down or upstream
//     misbehaving" and falls back to whatever the local country row
//     says.
//   - (Country{...}, nil) on success.
func (s *Service) LookupWithContext(ctx context.Context, callsign string) (types.Country, error) {
	const op errors.Op = "hamnut.Service.LookupWithContext"
	if ctx == nil {
		ctx = context.Background()
	}

	if !s.isInitialized.Load() {
		return types.Country{}, errors.New(op).WithMsg("service is not initialized")
	}
	if s.Config == nil {
		return types.Country{}, errors.New(op).WithMsg("service config is not set")
	}

	callsign = strings.TrimSpace(callsign)
	if !s.Config.Enabled {
		// Sentinel: ADR 0017 #7 implicit-fall-through covers the
		// "hamnut is down" case at the orchestrator. "Disabled" is
		// the operator's deliberate choice — distinguished by the
		// "Unknown" Name so the orchestrator can log/track without
		// treating it as a transport failure.
		return types.Country{Name: "Unknown"}, nil
	}
	if s.client == nil {
		return types.Country{}, errors.New(op).WithMsg("http client is not configured")
	}
	if callsign == "" {
		return types.Country{}, errors.New(op).WithMsg("callsign cannot be empty")
	}

	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return types.Country{}, errors.New(op).WithErr(err).WithMsg("invalid Hamnut base URL")
	}
	q := u.Query()
	q.Set("prefix", callsign)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return types.Country{}, errors.New(op).WithErr(err).WithMsg("failed to create HTTP GET request")
	}
	req.Header.Set("User-Agent", s.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return types.Country{}, errors.New(op).WithErr(err).WithMsg("failed to perform HTTP GET request")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return types.Country{}, errors.New(op).WithErr(errors.ErrNotFound).
			WithMsg("prefix not found by Hamnut")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Drain a small slice of the body for the error message; we
		// don't need the whole thing and reading it all could be
		// expensive on a misbehaving upstream.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return types.Country{}, errors.New(op).
			WithMsgf("hamnut returned status %d: %s", resp.StatusCode, string(b))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, successBodyLimit+1))
	if err != nil {
		return types.Country{}, errors.New(op).WithErr(err).WithMsg("failed to read response body")
	}
	if len(body) > successBodyLimit {
		return types.Country{}, errors.New(op).WithMsgf("hamnut response body exceeds %d-byte limit", successBodyLimit)
	}
	country, err := s.unmarshalResponse(body)
	if err != nil {
		return types.Country{}, err
	}
	return country, nil
}

func (s *Service) validateConfig(op errors.Op) error {
	if s.Config == nil {
		return errors.New(op).WithMsg("service config is not set")
	}
	if !s.Config.Enabled {
		return nil
	}

	s.Config.URL = strings.TrimSpace(s.Config.URL)
	if s.Config.URL == "" {
		return errors.New(op).WithMsg("hamnut URL cannot be empty when enabled")
	}
	u, err := url.Parse(s.Config.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New(op).WithErr(err).WithMsg("hamnut URL is invalid")
	}

	s.UserAgent = strings.TrimSpace(s.UserAgent)
	if s.UserAgent == "" {
		return errors.New(op).WithMsg("hamnut UserAgent cannot be empty when enabled (daemon should have set it from Config.UserAgent at construction)")
	}

	if s.Config.HttpTimeoutSec <= 0 {
		return errors.New(op).WithMsg("hamnut HttpTimeoutSec must be greater than zero")
	}
	return nil
}
