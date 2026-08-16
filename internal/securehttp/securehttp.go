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
	"net/netip"
	"net/url"
	"strings"
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
