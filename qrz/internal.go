package qrz

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
)

// fetchAndSetSessionKey fetches a session key from the configured QRZ endpoint and assigns it to the service instance.
// It validates initialization, builds the request, handles errors, and processes the XML response for the session key.
// Errors returned:
func (s *Service) requestAndSetSessionKey() error {
	const op errors.Op = "qrz.Service.fetchAndSetSessionKey"

	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return errors.New(op).Err(err).Msg("invalid QRZ base URL")
	}

	q := u.Query()
	q.Set("username", s.Config.Username)
	q.Set("password", s.Config.Password)
	q.Set("agent", s.Config.UserAgent)
	u.RawQuery = q.Encode()

	// Build request to set headers (User-Agent is often required)
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		err = errors.New(op).Errorf("Failed to create HTTP GET request: %w", err)
		return err
	}
	req.Header.Set("User-Agent", s.Config.UserAgent)
	req.Header.Set("Accept", "application/xml")

	resp, err := s.client.Do(req)
	if err != nil {
		err = errors.New(op).Errorf("Failed to perform HTTP GET request: %w", err)
		return err
	}
	defer func(Body io.ReadCloser) {
		if e := Body.Close(); e != nil {
			s.LoggerService.ErrorWith().Err(err).Msg("Failed to close response body")
		}
	}(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to read body for context
		b, _ := io.ReadAll(resp.Body)
		err = errors.New(op).Errorf("QRZ.com returned unexpected status %d: %s", resp.StatusCode, string(b))
		return err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		err = errors.New(op).Errorf("Failed to read response body: %w", err)
		return err
	}

	var db Database
	if err = xml.Unmarshal(body, &db); err != nil {
		err = errors.New(op).Errorf("Failed to unmarshal XML: %w", err)
		return err
	}

	// Check for API-level error
	if db.Session.Error != "" {
		err = errors.New(op).Errorf("QRZ.com returned error: %s", db.Session.Error)
		return err
	}
	if db.Session.Key == "" {
		err = errors.New(op).Msg("QRZ.com returned missing session key")
		return err
	}

	// Set the sesion key
	s.sessionKey = db.Session.Key

	return nil
}

func (s *Service) unmarshalResponse(body []byte) (types.ContactedStation, error) {
	const op errors.Op = "qrz.Service.unmarshalResponse"

	var (
		station types.ContactedStation
		db      Database
	)

	if err := xml.Unmarshal(body, &db); err != nil {
		return station, errors.New(op).Err(err).Msg("failed to unmarshal QRZ XML response")
	}

	sessionErr := strings.TrimSpace(db.Session.Error)
	if sessionErr != "" {
		lower := strings.ToLower(sessionErr)
		errBuilder := errors.New(op).Msg(sessionErr)
		if strings.Contains(lower, "not found") {
			errBuilder = errBuilder.Err(errors.ErrNotFound)
		}
		return station, errBuilder
	}

	cs := db.Callsign
	trim := func(v string) string {
		return strings.TrimSpace(v)
	}
	joinParts := func(parts ...string) string {
		var cleaned []string
		for _, part := range parts {
			if part = trim(part); part != "" {
				cleaned = append(cleaned, part)
			}
		}
		return strings.Join(cleaned, ", ")
	}
	buildName := func() string {
		if v := trim(cs.NameFmt); v != "" {
			return v
		}
		var pieces []string
		if v := trim(cs.Fname); v != "" {
			pieces = append(pieces, v)
		}
		if v := trim(cs.Name); v != "" {
			pieces = append(pieces, v)
		}
		if len(pieces) > 0 {
			return strings.Join(pieces, " ")
		}
		return trim(cs.Nickname)
	}

	call := strings.ToUpper(trim(cs.Call))
	if call == "" {
		return station, errors.New(op).Err(errors.ErrNotFound).Msg("callsign not present in QRZ response")
	}

	station.Call = call
	station.Name = buildName()
	station.Address = joinParts(cs.Addr1, joinParts(cs.Addr2, cs.State), cs.Zip, cs.Country)
	station.QTH = joinParts(cs.Addr2, cs.State)
	station.Country = trim(cs.Country)
	station.Gridsquare = strings.ToUpper(trim(cs.Grid))
	station.CQZ = trim(cs.Cqzone)
	station.ITUZ = trim(cs.Ituzone)
	station.DXCC = trim(cs.Dxcc)
	station.Email = trim(cs.Email)
	station.EqCall = strings.ToUpper(trim(cs.PCall))
	station.Web = trim(cs.URL)
	station.Lat = trim(cs.Lat)
	station.Lon = trim(cs.Lon)
	station.ContactedOp = trim(cs.Attn)

	return station, nil
}

func (s *Service) validateConfig(op errors.Op) error {
	if s.Config == nil {
		return errors.New(op).Msg("service config is not set")
	}

	// Don't check the config it the service is not enabled
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

	if len(s.Config.Username) < 3 {
		return errors.New(op).Msg("QRZ.com lookup service username must be at least 3 characters long")
	}

	if len(s.Config.Password) < 5 {
		return errors.New(op).Msg("QRZ.com lookup service password must be at least 5 characters long")
	}

	return nil
}
