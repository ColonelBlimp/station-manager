// Package qrzcq implements callsign enrichment through QRZCQ.com's XML API.
//
// QRZCQ uses a session-key protocol: authenticate once with the account
// username/password, reuse the returned key for callsign GETs, and acquire a
// new key only when the server reports expiry. The provider is registered into
// Station Manager's priority-ordered callsign chain and is seeded disabled, so
// adding it to a build never sends credentials or changes enrichment until the
// operator explicitly enables it.
package qrzcq

import (
	"context"
	stderr "errors"
	"net/http"
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

const (
	ServiceName = types.QRZCQLookupServiceName

	DefaultURL     = types.QRZCQLookupDefaultURL
	DefaultViewURL = types.QRZCQLookupDefaultViewURL

	DefaultHTTPTimeoutSec = 10
	minUsernameLen        = 3
	minPasswordLen        = 5
)

func init() {
	lookupdef.RegisterProvider(lookupdef.ProviderDescriptor{
		Name:              ServiceName,
		DisplayName:       "QRZCQ.com",
		Help:              "Fills name, grid and address from QRZCQ. Needs a premium QRZCQ account with XML access.",
		Kind:              lookupdef.KindCallsign,
		NeedsCredentials:  true,
		MinUsernameLen:    minUsernameLen,
		MinPasswordLen:    minPasswordLen,
		DefaultURL:        DefaultURL,
		DefaultViewURL:    DefaultViewURL,
		DefaultTimeoutSec: DefaultHTTPTimeoutSec,
	})
	lookup.RegisterCallsignProvider(ServiceName, func(
		logger *logging.Service,
		cfg *types.LookupConfig,
		userAgent string,
	) lookup.CallsignProvider {
		s := NewService(logger, nil, cfg, nil)
		s.UserAgent = userAgent
		return s
	})
}

var _ lookup.CallsignProvider = (*Service)(nil)

// Service implements the QRZCQ XML callsign provider.
type Service struct {
	ConfigService *config.Service  `di.inject:"configservice"`
	LoggerService *logging.Service `di.inject:"loggingservice"`
	Config        *types.LookupConfig
	UserAgent     string
	client        *http.Client

	initMu        sync.Mutex
	isInitialized atomic.Bool

	sessionMu  sync.Mutex
	sessionKey string

	authMu          sync.Mutex
	lastAuthAttempt time.Time
	lastAuthErr     error
}

// sessionRetryCooldown prevents an outage or bad login from turning a burst
// of callsign enrichments into a burst of authentication requests.
var sessionRetryCooldown = 30 * time.Second

var (
	errSessionExpired = stderr.New("qrzcq: session expired")
	errAuthInProgress = stderr.New("qrzcq: session key acquisition in progress")
)

func NewService(
	logger *logging.Service,
	cfgSvc *config.Service,
	cfg *types.LookupConfig,
	client *http.Client,
) *Service {
	return &Service{
		LoggerService: logger,
		ConfigService: cfgSvc,
		Config:        cfg,
		client:        client,
	}
}

func (s *Service) Name() string { return ServiceName }

// Initialize validates configuration and attempts the initial login. A valid
// configuration with a temporarily unavailable upstream still initializes:
// the provider remains enabled and lazily retries authentication on a later
// lookup, allowing the rest of the enrichment chain to fail soft meanwhile.
func (s *Service) Initialize(ctx context.Context) error {
	const op errors.Op = "qrzcq.Service.Initialize"
	if s.isInitialized.Load() {
		return nil
	}
	s.initMu.Lock()
	defer s.initMu.Unlock()
	if s.isInitialized.Load() {
		return nil
	}
	if s.LoggerService == nil {
		return errors.New(op).WithMsg("logger service has not been set/injected")
	}
	if s.Config == nil {
		if s.ConfigService == nil {
			return errors.New(op).WithMsg("application config has not been set/injected")
		}
		cfg, err := s.ConfigService.LookupServiceConfig(ServiceName)
		if err != nil {
			return errors.New(op).WithErr(err).WithMsg("loading QRZCQ config from config service")
		}
		s.Config = &cfg
	}
	if err := s.validateConfig(op); err != nil {
		return err
	}
	if !s.Config.Enabled {
		s.LoggerService.InfoWith().Msg("QRZCQ.com XML lookup is disabled in the config")
		s.isInitialized.Store(true)
		return nil
	}
	if s.client == nil {
		s.client = utils.NewHTTPClient(time.Duration(s.Config.HttpTimeoutSec) * time.Second)
	}
	if err := s.requestAndSetSessionKey(ctx); err != nil {
		s.LoggerService.WarnWith().Err(err).
			Msg("QRZCQ session key fetch failed at startup; will retry lazily on lookups")
	}
	s.isInitialized.Store(true)
	return nil
}

func (s *Service) Lookup(callsign string) (types.ContactedStation, error) {
	return s.LookupWithContext(context.Background(), callsign)
}

func (s *Service) LookupWithContext(ctx context.Context, callsign string) (types.ContactedStation, error) {
	const op errors.Op = "qrzcq.Service.LookupWithContext"
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.isInitialized.Load() {
		return types.ContactedStation{}, errors.New(op).WithMsg("service is not initialized")
	}
	if s.Config == nil {
		return types.ContactedStation{}, errors.New(op).WithMsg("service config is not set")
	}
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if !s.Config.Enabled {
		return types.ContactedStation{Call: callsign}, nil
	}
	if callsign == "" {
		return types.ContactedStation{}, errors.New(op).WithMsg("callsign cannot be empty")
	}
	if s.client == nil {
		return types.ContactedStation{}, errors.New(op).WithMsg("http client is not configured")
	}
	if err := s.ensureSessionKey(); err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).
			WithMsg("no QRZCQ session key (will retry)")
	}

	keyBefore := s.getSessionKey()
	station, err := s.lookupOnce(ctx, callsign)
	if err != nil && stderr.Is(err, errSessionExpired) {
		s.clearSessionKeyIf(keyBefore)
		if authErr := s.ensureSessionKey(); authErr != nil {
			s.LoggerService.WarnWith().Err(authErr).
				Msg("QRZCQ session re-auth after expiry unavailable (will retry)")
			return types.ContactedStation{}, errors.New(op).WithErr(err).
				WithMsg("session expired and re-auth unavailable")
		}
		s.LoggerService.InfoWith().Msg("QRZCQ session re-authenticated after expiry; retrying lookup")
		station, err = s.lookupOnce(ctx, callsign)
	}
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			return types.ContactedStation{}, err
		}
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("lookup failed")
	}
	return station, nil
}

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

func (s *Service) clearSessionKeyIf(expected string) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.sessionKey == expected {
		s.sessionKey = ""
	}
}

func (s *Service) ensureSessionKey() error {
	if s.getSessionKey() != "" {
		return nil
	}
	if !s.authMu.TryLock() {
		return errAuthInProgress
	}
	defer s.authMu.Unlock()
	if s.getSessionKey() != "" {
		return nil
	}
	if s.lastAuthErr != nil && !s.lastAuthAttempt.IsZero() &&
		time.Since(s.lastAuthAttempt) < sessionRetryCooldown {
		return s.lastAuthErr
	}
	err := s.requestAndSetSessionKey(context.Background())
	s.lastAuthErr = err
	s.lastAuthAttempt = time.Now()
	return err
}
