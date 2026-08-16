// Package securehttp centralises the credential-transport and redirect policy for
// every SM client that carries credentials to an upstream (ST-4,
// docs/reviews/internal-security-trust-boundary-audit.md). Two rules:
//
//   - Transport (ST-4a): a URL may carry credentials only over https, or over plain
//     http to a loopback host (so local mocks/tests keep working). The SM-Cloud
//     LAN-staging acknowledgement (allow_insecure_http) is the only bypass, and it
//     relaxes ONLY the https requirement — never the "must be a real http(s) URL".
//   - Redirects (ST-4b): a credentialed client follows only same-origin redirects.
//
// Errors NEVER echo the URL or any component of it — url.Parse takes everything
// before the first ':' as the scheme, so an operator who omits "https://" can turn
// "user:token@host" into scheme "user" and a pasted token into the scheme outright.
// Same discipline as scrubURLError in internal/lookup/qrz. Callers wrap the returned
// URL-free sentinel with their own field/op context.
package securehttp

import (
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// URL-free sentinels. Callers add field/op context; tests can errors.Is on them.
var (
	// ErrURLUnparseable — not a usable http(s) URL with a host.
	ErrURLUnparseable = errors.New("must be a valid http:// or https:// URL with a host")
	// ErrInsecureTransport — plain http to a non-loopback host without acknowledgement.
	ErrInsecureTransport = errors.New("must use https (plain http is allowed only for a loopback host)")
)

// CheckCredentialedURL reports (via a nil / non-nil error) whether rawURL may carry
// credentials. allowInsecure is the SM-Cloud acknowledgement: it permits plain http
// to ANY host and nothing else — an unparseable or hostless value is still rejected.
// The returned error is URL-free.
func CheckCredentialedURL(rawURL string, allowInsecure bool) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ErrURLUnparseable
	}
	if u.Scheme == "https" {
		return nil
	}
	// http from here.
	if allowInsecure || isLoopbackHost(u.Hostname()) {
		return nil
	}
	return ErrInsecureTransport
}

// IsInsecureRemote reports whether rawURL carries credentials in cleartext to a
// non-loopback host — the case the SM-Cloud acknowledgement exists to permit, and the
// trigger for the standing startup warning (ST-4a).
func IsInsecureRemote(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" && !isLoopbackHost(u.Hostname())
}

// ---- redirect policy (ST-4b) ------------------------------------------------

// URL-free redirect-refusal sentinels. A refused redirect must not leak the target,
// which the standard library's *url.Error wrapper would otherwise embed (incl. its
// query, a credential-bearing component). Callers reach these via Do, not client.Do.
var (
	// ErrRedirectRefused — a redirect left the original request's origin.
	ErrRedirectRefused = errors.New("refused a cross-origin redirect")
	// ErrTooManyRedirects — the same-origin chain exceeded the hop cap.
	ErrTooManyRedirects = errors.New("stopped after too many redirects")
)

// maxRedirects mirrors net/http's default cap. Setting our own CheckRedirect
// REPLACES that cap, so we re-impose it — otherwise a same-origin redirect loop
// would spin until the client timeout.
const maxRedirects = 10

// SameOriginRedirect is an http.Client.CheckRedirect that follows a redirect ONLY
// when its target shares the ORIGINAL request's origin — same scheme, same host
// (case-insensitive), and same effective port (an explicit default port equals the
// implicit one). Relative redirects resolve within that origin and so are allowed.
// Every hop is compared against via[0] (the original request), not the preceding
// hop, so a chain that steps origin -> origin -> foreign is refused at the foreign
// hop. Downgrade (https->http), upgrade, cross-host, cross-port and subdomain
// redirects are all refused. Errors are URL-free (Do replaces the *url.Error wrap).
func SameOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) >= maxRedirects {
		return ErrTooManyRedirects
	}
	if !sameOrigin(via[0].URL, req.URL) {
		return ErrRedirectRefused
	}
	return nil
}

// Harden installs the same-origin redirect policy on c and returns it, preserving
// its Transport, Timeout, and every other field. Use it on any client that carries
// credentials upstream (ST-4b).
func Harden(c *http.Client) *http.Client {
	c.CheckRedirect = SameOriginRedirect
	return c
}

// NewClient returns a credential-safe client: the given timeout plus the same-origin
// redirect policy. A convenience over Harden(&http.Client{Timeout: ...}).
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: SameOriginRedirect}
}

// Do runs client.Do(req) and, on a redirect refusal, replaces the error with a
// URL-free sentinel. net/http wraps a CheckRedirect error in a *url.Error whose
// message embeds the redirect target URL (including its query), so sanitizing only
// the value returned by CheckRedirect is insufficient — the wrapper leaks it. Any
// credentialed client MUST issue requests through Do, never client.Do directly.
func Do(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err == nil {
		return resp, nil
	}
	switch {
	case errors.Is(err, ErrRedirectRefused):
		return nil, ErrRedirectRefused
	case errors.Is(err, ErrTooManyRedirects):
		return nil, ErrTooManyRedirects
	}
	return resp, err
}

// sameOrigin compares scheme + host + effective port. Non-redirect transport errors
// (dial/TLS/timeout) are out of ST-4b scope and pass through Do unchanged.
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

// isLoopbackHost matches an exact "localhost" or a loopback IP literal. netip.ParseAddr
// (unlike net.ParseIP) accepts an IPv6 zone, so ::1%lo classifies as loopback —
// consistent with config.isLoopbackBind (ST-3a). No hostname resolution: a name other
// than localhost is conservatively non-loopback.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return a.IsLoopback()
	}
	return false
}
