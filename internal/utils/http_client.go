package utils

import (
	"net"
	"net/http"
	"time"
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
	reqTimeout := 15 * time.Second
	if httpTimeout > 0 {
		reqTimeout = httpTimeout
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Timeout:   reqTimeout,
		Transport: transport,
	}
}
