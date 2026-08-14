package qrzcq

import (
	"context"
	"encoding/xml"
	stderr "errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const (
	errorBodyLimit   = 512
	successBodyLimit = 1 << 20
)

func secureOrLoopbackURL(u *url.URL) bool {
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func readLimitedBody(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, successBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > successBodyLimit {
		return nil, stderr.New("response body exceeds size limit")
	}
	return body, nil
}

// scrubURLError removes query parameters from transport errors before they
// reach logging. QRZCQ authentication carries the password in the query and
// lookups carry the session key there; net/url otherwise includes both in an
// error's URL.
func scrubURLError(err error) error {
	for current := err; current != nil; current = stderr.Unwrap(current) {
		if ue, ok := current.(*url.Error); ok {
			if u, parseErr := url.Parse(ue.URL); parseErr == nil {
				u.RawQuery = ""
				ue.URL = u.String()
			}
		}
	}
	return err
}

func (s *Service) requestAndSetSessionKey(ctx context.Context) error {
	const op errors.Op = "qrzcq.Service.requestAndSetSessionKey"
	if ctx == nil {
		ctx = context.Background()
	}
	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("invalid QRZCQ base URL")
	}
	q := u.Query()
	q.Set("username", s.Config.Username)
	q.Set("password", s.Config.Password)
	q.Set("agent", s.UserAgent)
	u.RawQuery = q.Encode()

	body, err := s.getXML(ctx, u)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("QRZCQ session request failed")
	}
	var db Database
	if err := xml.Unmarshal(body, &db); err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to unmarshal QRZCQ session XML")
	}
	if sessionErr := strings.TrimSpace(db.Session.Error); sessionErr != "" {
		return errors.New(op).WithMsgf("QRZCQ returned error: %s",
			redactSecrets(sessionErr, s.Config.Password))
	}
	key := strings.TrimSpace(db.Session.Key)
	if key == "" {
		return errors.New(op).WithMsg("QRZCQ returned missing session key")
	}
	s.setSessionKey(key)
	return nil
}

func (s *Service) lookupOnce(ctx context.Context, callsign string) (types.ContactedStation, error) {
	const op errors.Op = "qrzcq.Service.lookupOnce"
	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("invalid QRZCQ base URL")
	}
	q := u.Query()
	q.Set("s", s.getSessionKey())
	q.Set("callsign", callsign)
	q.Set("agent", s.UserAgent)
	u.RawQuery = q.Encode()

	body, err := s.getXML(ctx, u)
	if err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).WithMsg("QRZCQ lookup request failed")
	}
	return unmarshalResponse(body, s.getSessionKey())
}

func (s *Service) getXML(ctx context.Context, u *url.URL) ([]byte, error) {
	const op errors.Op = "qrzcq.Service.getXML"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("failed to create HTTP GET request")
	}
	req.Header.Set("User-Agent", s.UserAgent)
	req.Header.Set("Accept", "application/xml")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errors.New(op).WithErr(scrubURLError(err)).WithMsg("failed to perform HTTP GET request")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Drain a bounded amount to let the transport reuse its connection, but do
		// not echo a proxy/server body: it could reflect the credential-bearing URL.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, errorBodyLimit))
		return nil, errors.New(op).WithMsgf("QRZCQ returned HTTP %d", resp.StatusCode)
	}
	body, err := readLimitedBody(resp.Body)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("failed to read response body")
	}
	return body, nil
}

func unmarshalResponse(body []byte, sessionKey string) (types.ContactedStation, error) {
	const op errors.Op = "qrzcq.unmarshalResponse"
	var db Database
	if err := xml.Unmarshal(body, &db); err != nil {
		return types.ContactedStation{}, errors.New(op).WithErr(err).
			WithMsg("failed to unmarshal QRZCQ XML response")
	}
	if sessionErr := strings.TrimSpace(db.Session.Error); sessionErr != "" {
		lower := strings.ToLower(sessionErr)
		safeSessionErr := redactSecrets(sessionErr, sessionKey)
		switch {
		case strings.Contains(lower, "not found"):
			return types.ContactedStation{}, errors.New(op).WithErr(errors.ErrNotFound).WithMsg(safeSessionErr)
		case isSessionExpiredError(lower):
			return types.ContactedStation{}, errors.New(op).WithErr(errSessionExpired).WithMsg(safeSessionErr)
		default:
			return types.ContactedStation{}, errors.New(op).WithMsg(safeSessionErr)
		}
	}

	trim := strings.TrimSpace
	cs := db.Callsign
	call := strings.ToUpper(trim(cs.Call))
	if call == "" {
		return types.ContactedStation{}, errors.New(op).WithErr(errors.ErrNotFound).
			WithMsg("callsign not present in QRZCQ response")
	}
	qth := trim(cs.QTH)
	if qth == "" {
		qth = joinParts(cs.City, cs.State)
	}
	return types.ContactedStation{
		Call:       call,
		Name:       trim(cs.Name),
		QTH:        qth,
		Address:    joinParts(cs.Address, cs.City, cs.State, cs.Zip, cs.Country),
		Cont:       strings.ToUpper(trim(cs.Continent)),
		Country:    trim(cs.Country),
		Gridsquare: strings.ToUpper(trim(cs.Locator)),
		Lat:        trim(cs.Latitude),
		Lon:        trim(cs.Longitude),
		Web:        trim(cs.Website),
		DXCC:       trim(cs.DXCC),
		ITUZ:       trim(cs.ITU),
		CQZ:        trim(cs.CQ),
		Iota:       strings.ToUpper(trim(cs.IOTA)),
	}, nil
}

func redactSecrets(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func joinParts(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, ", ")
}

func isSessionExpiredError(lower string) bool {
	return strings.Contains(lower, "session timeout") ||
		strings.Contains(lower, "invalid session") ||
		strings.Contains(lower, "session does not exist") ||
		strings.Contains(lower, "not logged in") ||
		strings.Contains(lower, "session expired")
}

func (s *Service) validateConfig(op errors.Op) error {
	if s.Config == nil {
		return errors.New(op).WithMsg("service config is not set")
	}
	if !s.Config.Enabled {
		return nil
	}
	s.Config.URL = strings.TrimSpace(s.Config.URL)
	u, err := url.Parse(s.Config.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New(op).WithErr(err).WithMsg("QRZCQ URL is invalid")
	}
	if !secureOrLoopbackURL(u) {
		return errors.New(op).WithMsg(
			"QRZCQ URL must use https (http allowed only for loopback) — credentials travel in the request URL")
	}
	s.UserAgent = strings.TrimSpace(s.UserAgent)
	if s.UserAgent == "" {
		return errors.New(op).WithMsg("QRZCQ UserAgent cannot be empty when enabled")
	}
	if s.Config.HttpTimeoutSec <= 0 {
		return errors.New(op).WithMsg("QRZCQ HttpTimeoutSec must be greater than zero")
	}
	s.Config.Username = strings.TrimSpace(s.Config.Username)
	if len(s.Config.Username) < minUsernameLen {
		return errors.New(op).WithMsgf("QRZCQ username must be at least %d characters", minUsernameLen)
	}
	if len(s.Config.Password) < minPasswordLen {
		return errors.New(op).WithMsgf("QRZCQ password must be at least %d characters", minPasswordLen)
	}
	return nil
}
