package qrz

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

// errorBodyLimit bounds how much of a non-2xx response body is read into the
// error message — enough to capture an upstream error string without slurping
// a misbehaving giant body.
const errorBodyLimit = 512

// successBodyLimit caps a 2xx response body. QRZ's URL is operator-configured
// and the endpoint is remote; a captive portal, misconfigured URL, or
// compromised path could otherwise return an unbounded 200 the daemon buffers
// whole (review 2026-06-19 M1). 1 MiB is generous for QRZ's XML.
const successBodyLimit = 1 << 20

// secureOrLoopbackURL reports whether u is safe to carry QRZ credentials:
// https to any host, or plain http only to a loopback address (so httptest /
// local mocks work, while a real remote URL must be https). Review 2026-06-19 M2.
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

// readLimitedBody reads up to successBodyLimit bytes of a 2xx body, erroring if
// the upstream sent more rather than buffering it unbounded. Shared by the
// session-auth and lookup reads.
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

// scrubURLError redacts the query string from a *url.Error's URL before it enters
// a logged or cached error path. Go's url.Error masks userinfo but NOT query
// params — and QRZ carries the username+password (session request) and the session
// key (lookup) in the query, which would otherwise leak into the daemon log on
// every transport failure. That matters especially now the provider stays in the
// chain on a flaky link: the failure recurs once per enriched callsign, not once
// per boot. Non-url.Error values (and unparseable URLs) pass through unchanged.
func scrubURLError(err error) error {
	var ue *url.Error
	if stderr.As(err, &ue) {
		if u, perr := url.Parse(ue.URL); perr == nil {
			u.RawQuery = ""
			ue.URL = u.String()
		}
	}
	return err
}

// requestAndSetSessionKey fetches a session key from QRZ.com using
// configured username/password and stores it on the Service for
// subsequent lookup calls.
//
// Carries forward v1's logic. Session keys can expire (typically after
// 24 hours of inactivity); LookupWithContext now detects an expired-session
// error (errSessionExpired) and calls this again to re-authenticate, then
// retries the lookup once (review 2026-06-04 M1) — so a long-running daemon
// recovers without a restart. Also called once at startup from Initialize.
//
// ctx is the daemon-lifecycle context (propagated from Initialize).
// Used by http.NewRequestWithContext so daemon shutdown cancels a
// stuck handshake — review M4 (pre-fix this used http.NewRequest
// with no context, and the only timeout was the absolute per-call
// HttpTimeoutSec).
func (s *Service) requestAndSetSessionKey(ctx context.Context) error {
	const op errors.Op = "qrz.Service.requestAndSetSessionKey"
	if ctx == nil {
		ctx = context.Background()
	}

	u, err := url.Parse(s.Config.URL)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("invalid QRZ base URL")
	}
	q := u.Query()
	q.Set("username", s.Config.Username)
	q.Set("password", s.Config.Password)
	q.Set("agent", s.UserAgent)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to create HTTP GET request")
	}
	req.Header.Set("User-Agent", s.UserAgent)
	req.Header.Set("Accept", "application/xml")

	resp, err := s.client.Do(req)
	if err != nil {
		// scrub: the transport *url.Error embeds the full auth URL incl. the password.
		return errors.New(op).WithErr(scrubURLError(err)).WithMsg("failed to perform HTTP GET request")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return errors.New(op).WithMsgf("QRZ.com returned status %d: %s", resp.StatusCode, string(b))
	}
	body, err := readLimitedBody(resp.Body)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to read response body")
	}

	var db Database
	if err := xml.Unmarshal(body, &db); err != nil {
		return errors.New(op).WithErr(err).WithMsg("failed to unmarshal session XML")
	}
	if db.Session.Error != "" {
		return errors.New(op).WithMsgf("QRZ.com returned error: %s", db.Session.Error)
	}
	if db.Session.Key == "" {
		return errors.New(op).WithMsg("QRZ.com returned missing session key")
	}

	s.setSessionKey(db.Session.Key)
	return nil
}

// errSessionExpired marks a QRZ Session.Error indicating the session key is no
// longer valid (expired / invalid / timed out) — distinct from a transport
// failure or a genuine "not found". LookupWithContext re-authenticates once on
// this error and retries the lookup (review 2026-06-04 M1).
var errSessionExpired = stderr.New("qrz: session expired")

// isSessionExpiredError reports whether a lowercased QRZ Session.Error string
// indicates an expired/invalid session key. QRZ phrases this variously
// ("Session Timeout", "Invalid session key", "Session does not exist", "Not
// logged in"); match the stable substrings.
func isSessionExpiredError(lower string) bool {
	return strings.Contains(lower, "session timeout") ||
		strings.Contains(lower, "invalid session") ||
		strings.Contains(lower, "session does not exist") ||
		strings.Contains(lower, "not logged in") ||
		strings.Contains(lower, "session expired")
}

// unmarshalResponse decodes the QRZ XML body into a
// types.ContactedStation. Country / CQ / ITU / DXCC fields are
// populated faithfully from the upstream wire shape — the orchestrator's
// FilterToCallsignFields strips them at the merge boundary per
// ADR 0017 #2, so leaving them populated here keeps the parser close
// to the upstream contract and centralises the country-strip rule.
//
// Returns errors.ErrNotFound when QRZ reports the callsign is not in
// its database (Session.Error contains "not found", case-insensitive)
// or when the response has no Call element. The chain runner branches
// on ErrNotFound to proceed to the next provider per ADR 0017 #8.
func (s *Service) unmarshalResponse(body []byte) (types.ContactedStation, error) {
	const op errors.Op = "qrz.Service.unmarshalResponse"

	var (
		station types.ContactedStation
		db      Database
	)
	if err := xml.Unmarshal(body, &db); err != nil {
		return station, errors.New(op).WithErr(err).WithMsg("failed to unmarshal QRZ XML response")
	}

	if sessionErr := strings.TrimSpace(db.Session.Error); sessionErr != "" {
		lower := strings.ToLower(sessionErr)
		if strings.Contains(lower, "not found") {
			return station, errors.New(op).WithErr(errors.ErrNotFound).WithMsg(sessionErr)
		}
		if isSessionExpiredError(lower) {
			return station, errors.New(op).WithErr(errSessionExpired).WithMsg(sessionErr)
		}
		return station, errors.New(op).WithMsg(sessionErr)
	}

	cs := db.Callsign
	trim := strings.TrimSpace
	joinParts := func(parts ...string) string {
		var cleaned []string
		for _, p := range parts {
			if p = trim(p); p != "" {
				cleaned = append(cleaned, p)
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
		return station, errors.New(op).WithErr(errors.ErrNotFound).
			WithMsg("callsign not present in QRZ response")
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

// validateConfig enforces the operator-config contract for QRZ. URL,
// UserAgent, HttpTimeoutSec required; username/password also required
// because QRZ.com auth is mandatory (anonymous lookups not supported
// upstream).
func (s *Service) validateConfig(op errors.Op) error {
	if s.Config == nil {
		return errors.New(op).WithMsg("service config is not set")
	}
	if !s.Config.Enabled {
		return nil
	}

	s.Config.URL = strings.TrimSpace(s.Config.URL)
	if s.Config.URL == "" {
		return errors.New(op).WithMsg("QRZ URL cannot be empty when enabled")
	}
	u, err := url.Parse(s.Config.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New(op).WithErr(err).WithMsg("QRZ URL is invalid")
	}
	// QRZ session auth puts username + password in the request URL, so refuse to
	// send them over plain http to a remote host — https required (http allowed
	// only for loopback, for local mocks). Review 2026-06-19 M2.
	if !secureOrLoopbackURL(u) {
		return errors.New(op).WithMsg("QRZ URL must use https (http allowed only for loopback) — credentials travel in the request URL")
	}

	s.UserAgent = strings.TrimSpace(s.UserAgent)
	if s.UserAgent == "" {
		return errors.New(op).WithMsg("QRZ UserAgent cannot be empty when enabled (daemon should have set it from Config.UserAgent at construction)")
	}
	if s.Config.HttpTimeoutSec <= 0 {
		return errors.New(op).WithMsg("QRZ HttpTimeoutSec must be greater than zero")
	}
	if len(s.Config.Username) < 3 {
		return errors.New(op).WithMsg("QRZ username must be at least 3 characters")
	}
	if len(s.Config.Password) < 5 {
		return errors.New(op).WithMsg("QRZ password must be at least 5 characters")
	}
	return nil
}
