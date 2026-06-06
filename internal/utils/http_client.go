package utils

import (
	"net"
	"net/http"
	"time"
)

// Outbound HTTP client defaults — conservative for a daemon making many small
// requests to a few upstreams over a flaky link (ADR 0017 prefers a fast
// fall-through to the local DB over a hung request). These are deliberately
// code constants, not config: the per-request deadline IS operator-tunable via
// each provider's HttpTimeoutSec (passed as httpTimeout); the transport pool /
// handshake knobs are not the sort of thing an operator tunes.
const (
	defaultHTTPRequestTimeout = 15 * time.Second
	httpDialTimeout           = 5 * time.Second
	httpKeepAlive             = 30 * time.Second
	httpMaxIdleConns          = 100
	httpMaxIdleConnsPerHost   = 10
	httpIdleConnTimeout       = 90 * time.Second
	httpTLSHandshakeTimeout   = 5 * time.Second
	httpExpectContinueTimeout = 1 * time.Second
)

// NewHTTPClient returns a *http.Client configured with conservative
// defaults appropriate for the daemon's outbound requests (lookup
// providers, forwarders, etc.).
//
// httpTimeout is the hard deadline for the whole request — including
// connect, TLS, headers, body. Pass <= 0 to use the default of 15s.
//
// Transport defaults are picked for a daemon that runs many small
// requests against a small set of upstreams: keep-alives + HTTP/2 so
// repeat hits to the same host are cheap, modest idle pool sizes so
// memory stays bounded, short connect/handshake timeouts so a flaky
// upstream fails fast (relevant under ADR 0017's "implicit fall-
// through" failure mode — the daemon would rather quickly fall
// through to the local DB than hang the request).
func NewHTTPClient(httpTimeout time.Duration) *http.Client {
	reqTimeout := defaultHTTPRequestTimeout
	if httpTimeout > 0 {
		reqTimeout = httpTimeout
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   httpDialTimeout,
			KeepAlive: httpKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          httpMaxIdleConns,
		MaxIdleConnsPerHost:   httpMaxIdleConnsPerHost,
		IdleConnTimeout:       httpIdleConnTimeout,
		TLSHandshakeTimeout:   httpTLSHandshakeTimeout,
		ExpectContinueTimeout: httpExpectContinueTimeout,
	}

	return &http.Client{
		Timeout:   reqTimeout,
		Transport: transport,
	}
}
