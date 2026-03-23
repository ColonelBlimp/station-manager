package utils

import (
	"net"
	"net/http"
	"time"
)

func NewHTTPClient(httpTimeout time.Duration) *http.Client {
	// Default total request timeout
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
		// DisableCompression: false,
	}

	return &http.Client{
		Timeout:   reqTimeout, // hard deadline for the whole request
		Transport: transport,
		// CheckRedirect: func(req *http.Request, via []*http.Request) error {
		//     if len(via) >= 10 { return http.ErrUseLastResponse }
		//     return nil
		// },
	}
}
