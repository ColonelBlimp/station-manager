package api

// ST-1 (docs/reviews/internal-security-trust-boundary-audit.md) — the DNS-rebinding Host
// allowlist must protect confidential READS too, not just mutations. requireSameOrigin used
// to early-return for GET/HEAD/OPTIONS before hostAllowed, so a rebound page could read the
// loopback API (config, QSO data, SSE, pprof) via GET. Operator rulings 2026-08-16: the Host
// (destination) check runs for EVERY method, before the method switch and BEFORE the Origin
// check (host-first when both are invalid); the Origin (CSRF) check stays unsafe-method-only;
// a foreign Host gets the same static cross_origin / "host not allowed" 403 for any method;
// and the route handler must not run.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

const (
	msgHostNotAllowed = "host not allowed"
	msgOriginRejected = "cross-origin request rejected"
)

// C1: the Host check applies to every method (incl. GET/HEAD/OPTIONS), across loopback,
// specific-IP and wildcard binds. A foreign/rebound Host → 403 and the route does NOT run; an
// allowed Host → the route runs.
func TestRequireSameOrigin_HostCheckAppliesToEveryMethod(t *testing.T) {
	binds := []struct {
		name, socketPath, allowedHost, foreignHost string
	}{
		{"loopback bind", "127.0.0.1:8080", "127.0.0.1:8080", "evil.example:8080"},
		{"specific-IP bind", "192.168.1.5:8080", "192.168.1.5:8080", "evil.example:8080"},
		// Wildcard admits loopback only, so a LAN host is "foreign" (fail-closed).
		{"wildcard bind", "0.0.0.0:8080", "127.0.0.1:8080", "192.168.1.5:8080"},
	}
	methods := []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPost}

	for _, b := range binds {
		for _, m := range methods {
			t.Run(b.name+"/"+m, func(t *testing.T) {
				srv := testServerWithCfg(t, func(cfg *config.Config) { cfg.SocketPath = b.socketPath })

				drive := func(host string) (*httptest.ResponseRecorder, bool) {
					called := false
					next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						called = true
						w.WriteHeader(http.StatusOK)
					})
					req := httptest.NewRequest(m, "/v1/config", nil)
					req.Host = host
					w := httptest.NewRecorder()
					srv.requireSameOrigin(next).ServeHTTP(w, req)
					return w, called
				}

				// Foreign / rebound Host → 403, route NOT invoked, for EVERY method.
				w, called := drive(b.foreignHost)
				if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), msgHostNotAllowed) {
					t.Errorf("foreign Host %q: want 403 %q, got %d (%s)", b.foreignHost, msgHostNotAllowed, w.Code, w.Body.String())
				}
				if called {
					t.Errorf("route handler ran despite a foreign Host (%s %s %q)", b.name, m, b.foreignHost)
				}

				// Allowed Host → the route runs (200). (POST carries no Origin, so it passes.)
				w2, called2 := drive(b.allowedHost)
				if w2.Code != http.StatusOK || !called2 {
					t.Errorf("allowed Host %q: want 200 + handler invoked, got %d called=%v (%s)",
						b.allowedHost, w2.Code, called2, w2.Body.String())
				}
			})
		}
	}
}

// Precedence: when BOTH the Host and the Origin are invalid, reject on HOST first.
func TestRequireSameOrigin_HostRejectedBeforeOrigin(t *testing.T) {
	srv := testServer(t) // loopback bind
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/config", nil)
	req.Host = "evil.example:8080"                          // rebound Host
	req.Header.Set("Origin", "https://also-evil.example")   // and cross-origin
	w := httptest.NewRecorder()
	srv.requireSameOrigin(next).ServeHTTP(w, req)

	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), msgHostNotAllowed) {
		t.Errorf("both invalid: want 403 on HOST (%q), got %d (%s)", msgHostNotAllowed, w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), msgOriginRejected) {
		t.Error("rejected on Origin, want Host-first")
	}
	if called {
		t.Error("route handler ran despite a foreign Host")
	}
}

// Full-chain: a rebound GET reaches the real handler stack and is 403'd before any route runs
// (proves the outer-middleware coverage — no route can bypass it by registration order).
func TestRebindingGet_FullChain_403BeforeRoute(t *testing.T) {
	srv := testServer(t)
	for _, path := range []string{"/v1/config", "/"} { // an API route and a static SPA asset
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "evil.example:8080"
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), msgHostNotAllowed) {
			t.Errorf("rebound GET %s: want 403 %q before the route, got %d (%s)", path, msgHostNotAllowed, w.Code, w.Body.String())
		}
	}
}
