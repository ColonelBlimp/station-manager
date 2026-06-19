package utils

import (
	"net/http"
	"testing"
	"time"
)

// NewHTTPClient had 0% direct coverage (review 2026-06-19 L2). Pin the
// timeout-defaulting contract and that the transport knobs are wired.
func TestNewHTTPClient_TimeoutDefaulting(t *testing.T) {
	// A positive timeout is used as-is.
	c := NewHTTPClient(7 * time.Second)
	if c.Timeout != 7*time.Second {
		t.Errorf("Timeout = %v; want 7s", c.Timeout)
	}

	// Zero / negative fall back to the 15s default.
	for _, d := range []time.Duration{0, -1 * time.Second} {
		c := NewHTTPClient(d)
		if c.Timeout != defaultHTTPRequestTimeout {
			t.Errorf("NewHTTPClient(%v).Timeout = %v; want default %v", d, c.Timeout, defaultHTTPRequestTimeout)
		}
	}
}

func TestNewHTTPClient_TransportConfigured(t *testing.T) {
	c := NewHTTPClient(0)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T; want *http.Transport", c.Transport)
	}
	if tr.Proxy == nil {
		t.Error("transport Proxy is nil; want ProxyFromEnvironment")
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 = false; want true")
	}
	if tr.MaxIdleConns != httpMaxIdleConns {
		t.Errorf("MaxIdleConns = %d; want %d", tr.MaxIdleConns, httpMaxIdleConns)
	}
	if tr.TLSHandshakeTimeout != httpTLSHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout = %v; want %v", tr.TLSHandshakeTimeout, httpTLSHandshakeTimeout)
	}
}
