package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

// requireSameOrigin rejects a cross-origin drive-by POST while letting the
// same-origin SPA, loopback dev proxy, non-browser clients, and all safe methods
// through (codex 088bdb84 P1).
func TestRequireSameOrigin(t *testing.T) {
	srv := testServer(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := srv.requireSameOrigin(next)

	do := func(method, origin, host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v1/restart", nil)
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	pass := func(name string, w *httptest.ResponseRecorder) {
		if w.Code != http.StatusOK {
			t.Fatalf("%s: want pass-through 200, got %d (%s)", name, w.Code, w.Body.String())
		}
	}
	block := func(name string, w *httptest.ResponseRecorder) {
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "cross_origin") {
			t.Fatalf("%s: want 403 cross_origin, got %d (%s)", name, w.Code, w.Body.String())
		}
	}

	pass("GET cross-origin (safe method)", do(http.MethodGet, "https://evil.example", "127.0.0.1:8080"))
	pass("POST no Origin (curl/CLI)", do(http.MethodPost, "", "127.0.0.1:8080"))
	pass("POST same host:port", do(http.MethodPost, "http://127.0.0.1:8080", "127.0.0.1:8080"))
	pass("POST loopback dev proxy", do(http.MethodPost, "http://localhost:5173", "127.0.0.1:8080"))
	block("POST cross-origin", do(http.MethodPost, "https://evil.example", "127.0.0.1:8080"))
	block("POST malformed Origin", do(http.MethodPost, "not-a-url", "127.0.0.1:8080"))
	// DNS rebinding: the attacker rebinds their own name to us, so Host AND Origin
	// both read evil.example — an Origin==Host check would pass, the allowlist
	// rejects on the disallowed Host (codex 85997b79 P1).
	block("POST DNS-rebinding", do(http.MethodPost, "http://evil.example:8080", "evil.example:8080"))
}

// originAllowed permits loopback (any port) and an EXACT same host:port, but a
// different-port page on the same hostname is rejected (codex e77a573f P2). With no
// trusted host (unix / wildcard tcp) only loopback Origins pass (codex d068ff9c P1).
func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		origin, trusted string
		want            bool
	}{
		{"http://localhost:5173", "127.0.0.1:8080", true},          // dev proxy (loopback, any port)
		{"http://127.0.0.1:8080", "127.0.0.1:8080", true},          // same-origin loopback
		{"http://station.local:8080", "station.local:8080", true},  // non-loopback same host:port
		{"http://station.local:9999", "station.local:8080", false}, // cross-PORT (P2)
		{"https://evil.example", "station.local:8080", false},      // cross-origin
		{"not-a-url", "127.0.0.1:8080", false},                     // malformed / no host
		{"http://localhost:5173", "", true},                        // loopback Origin, no trusted host
		{"https://evil.example", "", false},                        // non-loopback Origin, no trusted host (P1)
	}
	for _, c := range cases {
		if got := originAllowed(c.origin, c.trusted); got != c.want {
			t.Errorf("originAllowed(%q, %q) = %v, want %v", c.origin, c.trusted, got, c.want)
		}
	}
}

// A wildcard bind (0.0.0.0 / :8080) can't identify its external host, so mutations
// are LOOPBACK-ONLY — fail-closed / rebinding-proof (Option A, codex 5664434c P1).
// A LAN deployment must bind a specific IP instead (next test).
func TestRequireSameOrigin_WildcardBindLoopbackOnly(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.SocketPath = "0.0.0.0:8080"
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := srv.requireSameOrigin(next)
	do := func(host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/restart", nil)
		req.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := do("127.0.0.1:8080"); w.Code != http.StatusOK {
		t.Fatalf("wildcard bind, loopback POST: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	if w := do("192.168.1.5:8080"); w.Code != http.StatusForbidden {
		t.Fatalf("wildcard bind, LAN POST: want 403 (fail-closed), got %d", w.Code)
	}
}

// A SPECIFIC-IP bind is how a LAN deployment accepts network browser writes: the
// bound IP is allowed; a rebound attacker name is not.
func TestRequireSameOrigin_SpecificIPBind(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.SocketPath = "192.168.1.5:8080"
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := srv.requireSameOrigin(next)
	do := func(host, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/restart", nil)
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := do("192.168.1.5:8080", ""); w.Code != http.StatusOK {
		t.Fatalf("specific-IP bind, LAN POST on bound IP: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	if w := do("evil.example:8080", "http://evil.example:8080"); w.Code != http.StatusForbidden {
		t.Fatalf("specific-IP bind, rebinding POST: want 403, got %d", w.Code)
	}
}

// A Unix socket: a non-browser client (no Origin) with an arbitrary Host is
// accepted (codex 6525f509 P2), and a loopback-Origin browser passes — but a
// browser reaching the socket through a reverse proxy that forwards a REBOUND
// Host+Origin is rejected, because r.Host is not a trusted comparison basis for a
// non-tcp listener (codex d068ff9c P1).
func TestRequireSameOrigin_UnixSocket(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Server.Protocol = "unix"
		cfg.SocketPath = "/tmp/smd-test.sock"
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := srv.requireSameOrigin(next)
	do := func(host, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/qso", nil)
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	// `curl --unix-socket … http://smd/v1/qso` — arbitrary Host, no Origin.
	if w := do("smd", ""); w.Code != http.StatusOK {
		t.Fatalf("unix socket, no-Origin POST: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	// A loopback browser reaching the socket via a loopback proxy.
	if w := do("smd", "http://localhost:5173"); w.Code != http.StatusOK {
		t.Fatalf("unix socket, loopback-Origin POST: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	// Rebinding through a proxy that forwards the rebound Host+Origin: rejected.
	if w := do("evil.example", "http://evil.example"); w.Code != http.StatusForbidden {
		t.Fatalf("unix socket, rebinding-via-proxy POST: want 403, got %d", w.Code)
	}
}
