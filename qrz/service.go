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

	"github.com/Station-Manager/adif"
	"github.com/Station-Manager/config"
	"github.com/Station-Manager/database/sqlite"
	"github.com/Station-Manager/enums/upload/action"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
)

// ServiceName is the name of the service and is used to look up the service in the container.
const ServiceName = types.QrzForwardingServiceName

// Service represents a core FORWARDING structure facilitating interaction between logging, configuration, database,
// and HTTP services. It allows for dependency injection and manages initialization state effectively.
type Service struct {
	LoggerService   *logging.Service `di.inject:"loggingservice"`
	ConfigService   *config.Service  `di.inject:"configservice"`
	DatabaseService *sqlite.Service  `di.inject:"sqliteservice"`
	Config          *types.ForwarderConfig
	client          *http.Client

	isInitialized atomic.Bool
	initOnce      sync.Once
}

type Response struct {
	Result string
	Reason string
	LogIDS string
	LogID  string
	Count  string
	Data   string
}

// Initialize initializes the Service by injecting dependencies, setting the configuration, and preparing HTTP clients as needed.
func (s *Service) Initialize() error {
	const op errors.Op = "forwarder.qrz.Service.Initialize"
	if s.isInitialized.Load() {
		return nil
	}

	var initErr error
	s.initOnce.Do(func() {
		if s.LoggerService == nil {
			initErr = errors.New(op).Msg("logger service has not been set/injected")
			return
		}

		if s.ConfigService == nil {
			initErr = errors.New(op).Msg("application config has not been set/injected")
			return
		}

		if s.DatabaseService == nil {
			initErr = errors.New(op).Msg("database service has not been set/injected")
			return
		}

		cfg, err := s.ConfigService.ForwarderConfig(ServiceName)
		if err != nil {
			initErr = errors.New(op).Err(err).Msg("getting forwarder config")
			return
		}
		s.Config = &cfg

		if s.Config.Enabled {
			s.client = utils.NewHTTPClient(s.Config.HttpTimeoutSec * time.Second)
		} else {
			s.LoggerService.InfoWith().Msg("QRZ.com QSO forwarding is disabled in the config")
		}

		s.isInitialized.Store(true)
	})

	return initErr
}

// Forward sends a QSO record to the QRZ.com API, handles the response, and updates the local database with the result.
// This method is kept for backward compatibility but now delegates to ForwardNetworkOnly and UpdateDatabase.
func (s *Service) Forward(qso types.Qso, param ...string) error {
	const op errors.Op = "forwarder.qrz.Forward"

	// Perform network operation
	if err := s.ForwardNetworkOnly(qso, param...); err != nil {
		return errors.New(op).Err(err)
	}

	// Perform database update
	if err := s.UpdateDatabase(qso); err != nil {
		return errors.New(op).Err(err).Msg("updating database")
	}

	return nil
}

// ForwardNetworkOnly sends a QSO record to the QRZ.com API and validates the response.
// Database updates are NOT performed - use UpdateDatabase separately for serialized DB writes.
func (s *Service) ForwardNetworkOnly(qso types.Qso, param ...string) error {
	const op errors.Op = "forwarder.qrz.ForwardNetworkOnly"
	if !s.isInitialized.Load() {
		return errors.New(op).Msg("service not initialized")
	}

	qrzOption := ""
	qrzAction := strings.ToUpper(action.Insert.String()) // Default
	if len(param) > 0 {
		switch param[0] {
		case action.Insert.String():
			// Ignore, already set
		case action.Update.String():
			qrzOption = "REPLACE" // QRZ.com doesn't support UPDATE
		default:
			return errors.New(op).Msgf("Internal: unsupported action: %s", param[0])
		}
	}

	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return errors.New(op).Err(err).Msg("invalid QRZ base URL")
	}

	payload, err := adif.ConvertQsoToAdifNoHeader(qso)
	if err != nil {
		return errors.New(op).Err(err).Msg("converting QSO to ADIF")
	}

	form := url.Values{
		"KEY":    {s.Config.APIKey},
		"ACTION": {qrzAction},
		"ADIF":   {payload},
	}

	if qrzOption != "" {
		form.Add("OPTION", qrzOption)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New(op).Err(err).Msg("Failed to create HTTP POST request")
	}

	req.Header.Set("User-Agent", s.Config.UserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return errors.New(op).Err(err).Msg("performing HTTP POST request")
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var body []byte
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return errors.New(op).Err(err).Msg("QRZ.com: reading the body")
	}

	var r *Response
	if r, err = parseResponse(body); err != nil {
		return errors.New(op).Err(err).Msg("QRZ.com: parsing response data")
	}

	if r.Result == "FAIL" || r.Result == "AUTH" {
		return errors.New(op).Msgf("QRZ.com: Action: %s, failed: %s", qrzAction, r.Reason)
	}

	if r.Result == "OK" || r.Result == "REPLACE" {
		s.LoggerService.InfoWith().Str("callsign", qso.Call).Msgf("QRZ: %s successful", qrzAction)
	}

	return nil
}
