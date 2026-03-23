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

	"github.com/Station-Manager/config"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
)

const (
	ServiceName = types.HamNutLookupServiceName
)

type Service struct {
	ConfigService *config.Service  `di.inject:"configservice"`
	LoggerService *logging.Service `di.inject:"loggingservice"`
	Config        *types.LookupConfig
	client        *http.Client

	isInitialized atomic.Bool
	initOnce      sync.Once
}

// NewService returns a Hamnut lookup service with the provided dependencies. The
// lookup.ConfigService is optional if you supply Config directly. The client can
// be overridden for testing; otherwise it will be created during Initialize.
func NewService(logger *logging.Service, cfgSvc *config.Service, cfg *types.LookupConfig, client *http.Client) *Service {
	return &Service{
		LoggerService: logger,
		ConfigService: cfgSvc,
		Config:        cfg,
		client:        client,
	}
}

// Initialize initializes the Service instance by setting up required dependencies and configurations.
func (s *Service) Initialize() error {
	const op errors.Op = "hamnut.Service.Initialize"
	if s.isInitialized.Load() {
		return nil
	}

	var initErr error
	s.initOnce.Do(func() {
		if s.LoggerService == nil {
			initErr = errors.New(op).Msg("logger service has not been set/injected")
			return
		}

		if s.Config == nil {
			if s.ConfigService == nil {
				initErr = errors.New(op).Msg("application config has not been set/injected")
				return
			}

			cfg, err := s.ConfigService.LookupServiceConfig(ServiceName)
			if err != nil {
				initErr = errors.New(op).Err(err).Msg("getting lookup service config")
				return
			}
			s.Config = &cfg
		}

		if err := s.validateConfig(op); err != nil {
			initErr = err
			return
		}

		if s.client == nil {
			if s.Config.Enabled {
				s.client = utils.NewHTTPClient(s.Config.HttpTimeoutSec * time.Second)
			} else {
				s.LoggerService.InfoWith().Msg("Hamnut callsign/prefix lookup is disabled in the config")
			}
		}

		s.isInitialized.Store(true)
	})

	return initErr
}

// Lookup performs a country lookup using a callsign and returns the corresponding country information or an error.
func (s *Service) Lookup(callsign string) (types.Country, error) {
	return s.LookupWithContext(context.Background(), callsign)
}

// LookupWithContext performs a country lookup using the supplied context so callers
// can enforce cancellation and deadlines.
func (s *Service) LookupWithContext(ctx context.Context, callsign string) (types.Country, error) {
	const op errors.Op = "hamnut.Service.LookupWithContext"
	if ctx == nil {
		ctx = context.Background()
	}

	emptyRetVal := types.Country{}

	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg("service is not initialized")
	}
	if s.Config == nil {
		return emptyRetVal, errors.New(op).Msg("service config is not set")
	}

	callsign = strings.TrimSpace(callsign)

	// This check is here because if the client is disabled, the HTTP client will not be initialized
	if !s.Config.Enabled {
		if s.LoggerService != nil {
			s.LoggerService.InfoWith().Msg("Hamnut callsign/prefix lookup is disabled in the config")
		}
		return types.Country{Name: "Unknown"}, nil
	}

	if s.client == nil {
		return emptyRetVal, errors.New(op).Msg("http client is not configured")
	}

	if callsign == "" {
		return emptyRetVal, errors.New(op).Msg("callsign cannot be empty")
	}

	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return emptyRetVal, errors.New(op).Err(err).Msg("invalid Hamnut base URL")
	}
	q := u.Query()
	q.Set("prefix", callsign)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return emptyRetVal, errors.New(op).Err(err).Msg("Failed to create HTTP GET request")
	}

	req.Header.Set("User-Agent", s.Config.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return emptyRetVal, errors.New(op).Err(err).Msg("Failed to perform HTTP GET request")
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return emptyRetVal, errors.New(op).Err(errors.ErrNotFound).Msg("Prefix not found by Hamnut")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return emptyRetVal, errors.New(op).Errorf("Service returned unexpected status %d: %s", resp.StatusCode, string(b))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return emptyRetVal, errors.New(op).Errorf("Failed to read response body: %w", err)
	}

	country, err := s.unmarshalResponse(body)
	if err != nil {
		return emptyRetVal, errors.New(op).Err(err).Msg("Failed to unmarshal response body")
	}

	return country, nil
}

func (s *Service) validateConfig(op errors.Op) error {
	if s.Config == nil {
		return errors.New(op).Msg("service config is not set")
	}

	if !s.Config.Enabled {
		return nil
	}

	s.Config.URL = strings.TrimSpace(s.Config.URL)
	if s.Config.URL == "" {
		return errors.New(op).Msg("lookup service URL cannot be empty")
	}

	u, err := url.Parse(s.Config.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New(op).Err(err).Msg("lookup service URL is invalid")
	}

	s.Config.UserAgent = strings.TrimSpace(s.Config.UserAgent)
	if s.Config.UserAgent == "" {
		return errors.New(op).Msg("lookup service user agent cannot be empty")
	}

	if s.Config.HttpTimeoutSec <= 0 {
		return errors.New(op).Msg("lookup service timeout must be greater than zero")
	}

	return nil
}
