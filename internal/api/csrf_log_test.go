package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

/*
	CSRF refusal logging (docs/reviews/api-logging-gaps.md A3) — specified
	before the implementation, 2026-08-08 session of 2026-08-07.

	Criterion: when the CSRF guard refuses a mutating request, smd.log carries
	a dedicated Warn record naming the refused destination, and I can tell
	apart the states the static 403 collapsed into one line:

	  - a DNS-rebinding attempt (a foreign Host) from a LAN deployment
	    refusing LEGITIMATE traffic under a wildcard bind (my own LAN IP as
	    the refused Host) — opposite actions: investigate vs fix the bind;
	  - a cross-origin page on a foreign site from a stale bookmark on a
	    different PORT of the same host (origin_host keeps host:port);
	  - both refusal kinds from each other (distinct messages).

	The A3 AMENDMENT is load-bearing and overrides the finding's first
	wording: the Origin header is client-controlled and can carry
	https://user:pass@host — the exact credential-into-a-0644-file shape of
	the 2026-07-25 P1s. Only PARSED fields are logged (url.URL keeps userinfo
	in u.User, never u.Host); a value that does not parse to a host logs the
	FACT of unparseability, never the raw bytes. The Host header gets the
	same treatment: RFC 9110 forbids userinfo in Host, but nothing stops a
	hostile client sending it.

	CL1 — a refused Host is named, sanitised: the LAN-misconfig and rebinding
	      fixtures produce records whose host VALUES differ (the value IS the
	      diagnosis). Warn, with remote/method/path for the join to the
	      access line.
	CL2 — a refused Origin carries origin_scheme + origin_host with the port
	      preserved: stale-bookmark-wrong-port and foreign-site fixtures
	      differ in origin_host; the request's own (allowed) host rides along
	      as what the Origin failed to match.
	CL3 — a userinfo-bearing Origin logs its parsed host only; the credential
	      appears NOWHERE in the log output.
	CL4 — an Origin that does not parse to a host logs
	      origin_unparseable=true and never the raw value.
	CL5 — the Host header, equally client-controlled, sheds userinfo the same
	      way; a value too long to be a real destination (RFC 1035 caps a DNS
	      name at 253 octets) is reported unparseable, not copied into the log.
	CL6 — allowed traffic logs nothing here: a same-origin POST and a safe-
	      method cross-origin GET leave zero CSRF records (a refusal line
	      that also fired on routine traffic would train the operator to
	      ignore it).
*/

const (
	msgHostRefused   = "cross-origin refused: request host not allowed"
	msgOriginRefused = "cross-origin refused: origin not allowed"
)

// csrfCaptureServer is captureServer with a config hook (the CSRF guard's
// behaviour depends on the bind).
func csrfCaptureServer(t *testing.T, mutate func(cfg *config.Config)) (*Server, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	srv := testServerWithLogger(t, mutate, nil, logging.NewForWriter(buf))
	return srv, buf
}

// csrfDo drives one request through the guard, asserting the expected verdict
// so a fixture that stops exercising the refusal path fails loudly.
func csrfDo(t *testing.T, srv *Server, method, host, orig string, wantCode int) {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/restart", nil)
	req.Host = host
	if orig != "" {
		req.Header.Set("Origin", orig)
	}
	w := httptest.NewRecorder()
	srv.requireSameOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, req)
	if w.Code != wantCode {
		t.Fatalf("fixture: %s Host=%q Origin=%q: got %d, want %d (%s)",
			method, host, orig, w.Code, wantCode, w.Body.String())
	}
}

// allMessages returns every record carrying this message.
func allMessages(t *testing.T, buf *bytes.Buffer, message string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, rec := range logRecords(t, buf) {
		if rec["message"] == message {
			out = append(out, rec)
		}
	}
	return out
}

// CL1: the refused Host's value is the diagnosis.
func TestCsrfLog_RefusedHostIsNamed(t *testing.T) {
	srv, buf := csrfCaptureServer(t, func(cfg *config.Config) {
		cfg.SocketPath = "0.0.0.0:8080" // wildcard bind: loopback-only, so LAN Hosts refuse
	})

	csrfDo(t, srv, http.MethodPost, "192.168.1.5:8080", "", http.StatusForbidden)  // LAN misconfig
	csrfDo(t, srv, http.MethodPost, "evil.example:8080", "", http.StatusForbidden) // rebinding

	recs := allMessages(t, buf, msgHostRefused)
	if len(recs) != 2 {
		t.Fatalf("host-refusal records = %d, want 2\n%s", len(recs), buf.String())
	}
	hosts := []string{recs[0]["host"].(string), recs[1]["host"].(string)}
	if hosts[0] != "192.168.1.5:8080" || hosts[1] != "evil.example:8080" {
		t.Errorf("refused hosts = %v — the two confusable states must differ by VALUE "+
			"(fix-the-bind vs investigate)", hosts)
	}
	rec := recs[0]
	if rec["level"] != "warn" {
		t.Errorf("level = %v, want warn — a security refusal at Info interleaved with "+
			"routine traffic reads as routine", rec["level"])
	}
	if rec["method"] != "POST" || rec["path"] != "/v1/restart" {
		t.Errorf("record must join to the access line: method=%v path=%v", rec["method"], rec["path"])
	}
	if remote, _ := rec["remote"].(string); remote == "" {
		t.Error("record carries no remote")
	}
}

// CL2: the refused Origin keeps scheme and host:port.
func TestCsrfLog_RefusedOriginCarriesSchemeAndHostPort(t *testing.T) {
	srv, buf := csrfCaptureServer(t, func(cfg *config.Config) {
		cfg.SocketPath = "station.local:8080" // specific bind so a non-loopback Host is allowed
	})

	csrfDo(t, srv, http.MethodPost, "station.local:8080", "http://station.local:9999", http.StatusForbidden) // stale bookmark, wrong port
	csrfDo(t, srv, http.MethodPost, "station.local:8080", "https://evil.example", http.StatusForbidden)      // foreign site

	recs := allMessages(t, buf, msgOriginRefused)
	if len(recs) != 2 {
		t.Fatalf("origin-refusal records = %d, want 2\n%s", len(recs), buf.String())
	}
	if got := recs[0]["origin_host"]; got != "station.local:9999" {
		t.Errorf("origin_host = %v, want station.local:9999 — the PORT is what separates "+
			"a stale bookmark from a foreign page", got)
	}
	if got := recs[1]["origin_host"]; got != "evil.example" {
		t.Errorf("origin_host = %v, want evil.example", got)
	}
	if recs[0]["origin_scheme"] != "http" || recs[1]["origin_scheme"] != "https" {
		t.Errorf("origin_scheme = %v / %v, want http / https",
			recs[0]["origin_scheme"], recs[1]["origin_scheme"])
	}
	if got := recs[0]["host"]; got != "station.local:8080" {
		t.Errorf("host = %v, want station.local:8080 — what the Origin failed to match", got)
	}
}

// CL3: a credential in the Origin never reaches the 0644 log.
func TestCsrfLog_CredentialBearingOriginNeverLogsTheCredential(t *testing.T) {
	srv, buf := csrfCaptureServer(t, nil)

	csrfDo(t, srv, http.MethodPost, "127.0.0.1:8080",
		"https://user:hunter2secret@evil.example", http.StatusForbidden)

	rec := findMessage(t, buf, msgOriginRefused)
	if rec == nil {
		t.Fatalf("no origin-refusal record\n%s", buf.String())
	}
	if got := rec["origin_host"]; got != "evil.example" {
		t.Errorf("origin_host = %v, want evil.example (the parsed host, credential shed)", got)
	}
	if strings.Contains(buf.String(), "hunter2secret") {
		t.Fatalf("the credential reached the log — the 2026-07-25 P1 shape\n%s", buf.String())
	}
}

// CL4: an unparseable Origin logs the fact, not the bytes.
func TestCsrfLog_UnparseableOriginLogsTheFactNotTheValue(t *testing.T) {
	srv, buf := csrfCaptureServer(t, nil)

	csrfDo(t, srv, http.MethodPost, "127.0.0.1:8080", "not-a-url", http.StatusForbidden)

	rec := findMessage(t, buf, msgOriginRefused)
	if rec == nil {
		t.Fatalf("no origin-refusal record\n%s", buf.String())
	}
	if rec["origin_unparseable"] != true {
		t.Errorf("origin_unparseable = %v, want true", rec["origin_unparseable"])
	}
	if strings.Contains(buf.String(), "not-a-url") {
		t.Errorf("the raw unparseable value reached the log\n%s", buf.String())
	}
}

// CL5: the Host header gets the same sanitisation as the Origin.
func TestCsrfLog_HostShedsUserinfo(t *testing.T) {
	srv, buf := csrfCaptureServer(t, nil)

	csrfDo(t, srv, http.MethodPost, "user:secretpw@evil.example", "", http.StatusForbidden)

	rec := findMessage(t, buf, msgHostRefused)
	if rec == nil {
		t.Fatalf("no host-refusal record\n%s", buf.String())
	}
	if got := rec["host"]; got != "evil.example" {
		t.Errorf("host = %v, want evil.example (userinfo shed by the parse)", got)
	}
	if strings.Contains(buf.String(), "secretpw") {
		t.Fatalf("credential-shaped Host content reached the log\n%s", buf.String())
	}
}

func TestCsrfLog_OverlongHostIsUnparseable(t *testing.T) {
	srv, buf := csrfCaptureServer(t, nil)

	long := strings.Repeat("a", 300)
	csrfDo(t, srv, http.MethodPost, long+":8080", "", http.StatusForbidden)

	rec := findMessage(t, buf, msgHostRefused)
	if rec == nil {
		t.Fatalf("no host-refusal record\n%s", buf.String())
	}
	if rec["host_unparseable"] != true {
		t.Errorf("host_unparseable = %v, want true — 300 octets is not a DNS name "+
			"(RFC 1035 caps at 253)", rec["host_unparseable"])
	}
	if strings.Contains(buf.String(), long) {
		t.Errorf("the oversized value was copied into the log")
	}
}

// CL6: allowed traffic leaves zero CSRF records.
func TestCsrfLog_AllowedRequestsLogNothing(t *testing.T) {
	srv, buf := csrfCaptureServer(t, nil)

	csrfDo(t, srv, http.MethodPost, "127.0.0.1:8080", "http://127.0.0.1:8080", http.StatusOK) // same-origin
	csrfDo(t, srv, http.MethodGet, "127.0.0.1:8080", "https://evil.example", http.StatusOK)   // safe method

	if n := countMessage(t, buf, msgHostRefused) + countMessage(t, buf, msgOriginRefused); n != 0 {
		t.Errorf("CSRF records = %d on allowed traffic, want 0 — a refusal line that "+
			"fires on routine traffic trains the operator to ignore it\n%s", n, buf.String())
	}
}
