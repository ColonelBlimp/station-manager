package smcloud

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	stderr "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"

	"github.com/ColonelBlimp/station-manager/internal/securehttp"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// SupportState is what a start learned about the SM Cloud server's support for
// per-logbook identity (W-0021 5F.4, ADR 0092 decision 4, rulings F4, J2, J3).
// Only SupportSupported and SupportUnverified allow a new identity enable.
type SupportState string

const (
	// SupportChecking: the start's check has not finished.
	SupportChecking SupportState = "checking"
	// SupportSupported: the server answered identity_protocol >= 1.
	SupportSupported SupportState = "supported"
	// SupportUnverified: an identified transport failure — no HTTP status was
	// received (DNS failure, connection refused or reset, a timeout before the
	// headers). Uploads stay on the scoped paths and retry.
	SupportUnverified SupportState = "unverified"
	// SupportUnsupported: the server answered without identity_protocol >= 1.
	SupportUnsupported SupportState = "unsupported"
	// SupportError: anything else — any HTTP status but 200, a malformed
	// answer, a TLS verification failure, an account that builds no client, an
	// unknown error. Never read as offline.
	SupportError SupportState = "error"
	// SupportChanged: the saved account names another server than the one
	// checked; a restart checks it.
	SupportChanged SupportState = "changed"
)

// SupportResult is one start's check: the server it asked, normalized, and
// what that server's answer proved.
type SupportResult struct {
	Server string
	State  SupportState
}

// supportTimeout bounds the check (ruling J3: the existing adoption bound). A
// var so tests can shorten it.
var supportTimeout = AdoptionRequestTimeout

// supportDialContext replaces the transport's dialer when set; tests use it to
// produce a DNS failure without external DNS.
var supportDialContext func(ctx context.Context, network, addr string) (net.Conn, error)

// CheckSupport asks the account's server, once, under supportTimeout, whether
// it supports per-logbook identity: GET /v1/version, with no token and no
// redirect followed. A cancelled ctx (drain) is reported as an error and never
// as an allowing state. The returned error never carries the URL.
func CheckSupport(ctx context.Context, account types.ForwarderConfig) (SupportResult, error) {
	server, err := SupportServer(account)
	if err != nil {
		return SupportResult{State: SupportError}, err
	}
	res := SupportResult{Server: server, State: SupportError}
	// The account must build the forwarder that would use the answer.
	if _, err := New(account); err != nil {
		return res, stderr.New("the SM Cloud station account cannot build a client")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if supportDialContext != nil {
		transport.DialContext = supportDialContext
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()

	rctx, cancel := context.WithTimeout(ctx, supportTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, server+"/v1/version", nil)
	if err != nil {
		return res, stderr.New("the version request cannot be built")
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := securehttp.Do(client, req)
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if transportUnavailable(err) {
			res.State = SupportUnverified
		}
		return res, fmt.Errorf("GET /v1/version: no answer: %w", transportCause(rctx, err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return res, fmt.Errorf("GET /v1/version: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		return res, fmt.Errorf("GET /v1/version: HTTP 200, the answer was cut off: %w", transportCause(rctx, err))
	}
	var fields map[string]json.RawMessage
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) || json.Unmarshal(body, &fields) != nil {
		return res, stderr.New("GET /v1/version: the answer is not a JSON object")
	}
	raw, ok := fields["identity_protocol"]
	if !ok {
		res.State = SupportUnsupported
		return res, nil
	}
	var protocol int
	if err := json.Unmarshal(raw, &protocol); err != nil {
		return res, stderr.New("GET /v1/version: identity_protocol is not a number")
	}
	res.State = SupportUnsupported
	if protocol >= 1 {
		res.State = SupportSupported
	}
	return res, nil
}

// transportUnavailable reports the identified transport failures that may be
// shown as unverified (ruling J2): a DNS failure, a refused or reset
// connection, or the bound expiring before any status. Everything else — a
// TLS verification failure, an orderly close with no answer, an unknown
// error — is not.
func transportUnavailable(err error) bool {
	var dnsErr *net.DNSError
	var unknownAuthority x509.UnknownAuthorityError
	var certInvalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var verify *tls.CertificateVerificationError
	switch {
	case stderr.As(err, &verify), stderr.As(err, &unknownAuthority), stderr.As(err, &certInvalid), stderr.As(err, &hostname):
		return false
	case stderr.As(err, &dnsErr):
		return true
	case stderr.Is(err, syscall.ECONNREFUSED), stderr.Is(err, syscall.ECONNRESET):
		return true
	case stderr.Is(err, context.DeadlineExceeded):
		return true
	}
	return false
}

// SupportServer is the normalized server URL a check is tied to: trimmed, with
// no trailing slash. An account without a readable URL has none.
func SupportServer(account types.ForwarderConfig) (string, error) {
	var creds credentials
	if err := json.Unmarshal(account.Credentials, &creds); err != nil {
		return "", stderr.New("the SM Cloud station account's credentials cannot be read")
	}
	server := strings.TrimRight(strings.TrimSpace(creds.URL), "/")
	if server == "" {
		return "", stderr.New("the SM Cloud station account has no URL")
	}
	return server, nil
}

// For is the state an account saved now sees: the result itself while the
// account names the server checked, SupportChanged once it names another. An
// empty server is never a wildcard, and an account whose server cannot be
// read never keeps a result. The token takes no part: the check sends none.
func (r SupportResult) For(account types.ForwarderConfig) SupportState {
	if r.State == SupportChecking {
		return SupportChecking
	}
	server, err := SupportServer(account)
	if err != nil || r.Server == "" || server != r.Server {
		return SupportChanged
	}
	return r.State
}
