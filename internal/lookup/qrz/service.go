package qrz

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	stderr "errors"

	"github.com/Station-Manager/config"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
)

const (
	ServiceName = types.QrzLookupServiceName
)

type Service struct {
	ConfigService *config.Service  `di.inject:"configservice"`
	LoggerService *logging.Service `di.inject:"loggingservice"`
	Config        *types.LookupConfig
	client        *http.Client

	isInitialized atomic.Bool
	initOnce      sync.Once

	sessionKey string
}

// Initialize initializes the Service instance by setting up required dependencies and configurations.
func (s *Service) Initialize() error {
	const op errors.Op = "qrz.Service.Initialize"
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
				if err := s.requestAndSetSessionKey(); err != nil {
					// Any error here and we should disable the service
					s.Config.Enabled = false
					initErr = err
					return
				}
			} else {
				s.LoggerService.InfoWith().Msg("QRZ.com callsign lookup is disabled in the config")
			}
		}

		s.isInitialized.Store(true)
	})

	return initErr
}

// Lookup retrieves information about a contacted station by its callsign.
// It uses the default context and returns the station details or an error.
func (s *Service) Lookup(callsign string) (types.ContactedStation, error) {
	if !s.Config.Enabled {
		s.LoggerService.InfoWith().Msg("QRZ.com lookup not enabled in the config.")
		// If not enabled, just return an empty station object - NO ERROR
		return types.ContactedStation{}, nil
	}
	return s.LookupWithContext(context.Background(), callsign)
}

// LookupWithContext retrieves information about a contacted station based on the provided callsign and context.
// It validates the service's initialization state, builds the request, and processes the response or returns an error.
// Returns a ContactedStation object with details or an error if the retrieval fails.
func (s *Service) LookupWithContext(ctx context.Context, callsign string) (types.ContactedStation, error) {
	const op errors.Op = "qrz.service.LookupWithContext"
	if ctx == nil {
		ctx = context.Background()
	}

	emptyRetVal := types.ContactedStation{}
	if !s.isInitialized.Load() {
		return emptyRetVal, errors.New(op).Msg("service is not initialized")
	}
	if s.Config == nil {
		return emptyRetVal, errors.New(op).Msg("service config is not set")
	}

	callsign = strings.TrimSpace(callsign)

	// This check is here because if the client is disabled, the HTTP client will not be initialized
	if !s.Config.Enabled {
		s.LoggerService.InfoWith().Msg("QRZ.com callsign lookup is disabled in the config")
		return types.ContactedStation{Call: callsign}, nil
	}

	if s.client == nil {
		return emptyRetVal, errors.New(op).Msg("http client is not configured")
	}

	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return emptyRetVal, errors.New(op).Err(err).Msg("invalid QRZ base URL")
	}

	q := u.Query()
	q.Set("s", s.sessionKey)
	q.Set("callsign", callsign)
	q.Set("agent", s.Config.UserAgent)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return emptyRetVal, errors.New(op).Err(err).Msg("Failed to create HTTP GET request")
	}

	req.Header.Set("User-Agent", s.Config.UserAgent)
	req.Header.Set("Accept", "application/xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return emptyRetVal, errors.New(op).Err(err).Msg("Failed to perform HTTP GET request")
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return emptyRetVal, errors.New(op).Errorf("Service returned unexpected status %d: %s", resp.StatusCode, string(b))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return emptyRetVal, errors.New(op).Errorf("Failed to read response body: %w", err)
	}

	station, err := s.unmarshalResponse(body)
	//TODO: handle 'Not found'
	if err != nil {
		if stderr.Is(err, errors.ErrNotFound) {
			s.LoggerService.InfoWith().Str("callsign", callsign).Msg("Callsign not found in QRZ.com database")
			return types.ContactedStation{Call: callsign}, nil
		}
		return emptyRetVal, errors.New(op).Err(err).Msg("Failed to unmarshal response body")
	}

	return station, nil
}
