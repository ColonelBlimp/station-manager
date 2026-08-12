package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// accessLine finds the parsed "http request" access record for the given path.
func accessLine(t *testing.T, buf *bytes.Buffer, path string) map[string]any {
	t.Helper()
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(l), &rec) != nil {
			continue
		}
		if rec["msg"] == "http request" && rec["path"] == path {
			return rec
		}
	}
	t.Fatalf("no access line for %q in:\n%s", path, buf.String())
	return nil
}

func authedGet(t *testing.T, url, token, xrid string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if xrid != "" {
		req.Header.Set("X-Request-Id", xrid)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// C4 — an authenticated request produces one access line carrying the correlation id,
// tenant, method, path, and status; the same id is echoed in the response header. This
// is what direct-LAN deployments lack entirely and what ties a Caddy entry to an app
// error.
func TestAccessLog_AuthenticatedRequestCarriesCorrelation(t *testing.T) {
	var buf bytes.Buffer
	ts, _, tenant := testServerLogged(t, slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	resp := authedGet(t, ts.URL+"/v1/logbooks", testToken, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	rid := resp.Header.Get("X-Request-Id")
	_ = resp.Body.Close()
	if !requestIDPattern.MatchString(rid) {
		t.Fatalf("response X-Request-Id = %q, want a bounded id", rid)
	}

	rec := accessLine(t, &buf, "/v1/logbooks")
	if rec["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", rec["level"])
	}
	if rec["method"] != "GET" {
		t.Errorf("method = %v, want GET", rec["method"])
	}
	if s, _ := rec["status"].(float64); int(s) != 200 {
		t.Errorf("status = %v, want 200", rec["status"])
	}
	if rec["request_id"] != rid {
		t.Errorf("access-line request_id %v != response header %q — they must match", rec["request_id"], rid)
	}
	if id, _ := rec["tenant_id"].(float64); int64(id) != tenant {
		t.Errorf("tenant_id = %v, want %d (the authenticated tenant)", rec["tenant_id"], tenant)
	}
}

// A valid incoming X-Request-Id (Caddy's) is honored, so the Caddy entry and the app
// log share one id.
func TestAccessLog_HonorsValidIncomingRequestID(t *testing.T) {
	var buf bytes.Buffer
	ts, _, _ := testServerLogged(t, slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	const given = "caddy-abc123-def456-req" // 23 chars, bounded format

	resp := authedGet(t, ts.URL+"/v1/logbooks", testToken, given)
	_ = resp.Body.Close()
	if got := resp.Header.Get("X-Request-Id"); got != given {
		t.Errorf("response X-Request-Id = %q, want the honored incoming %q", got, given)
	}
	if rec := accessLine(t, &buf, "/v1/logbooks"); rec["request_id"] != given {
		t.Errorf("access-line request_id = %v, want the honored %q", rec["request_id"], given)
	}
}

// An untrusted / malformed X-Request-Id is discarded and a fresh bounded id minted.
func TestAccessLog_RejectsInvalidRequestID(t *testing.T) {
	var buf bytes.Buffer
	ts, _, _ := testServerLogged(t, slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	const bad = "bad id with spaces!" // spaces + '!' → invalid

	resp := authedGet(t, ts.URL+"/v1/logbooks", testToken, bad)
	got := resp.Header.Get("X-Request-Id")
	_ = resp.Body.Close()
	if got == bad {
		t.Errorf("a malformed X-Request-Id was echoed back verbatim: %q", got)
	}
	if !requestIDPattern.MatchString(got) {
		t.Errorf("generated id %q is not bounded", got)
	}
}

// /v1/health is unauthenticated + polled frequently, so its access line stays at Debug.
func TestAccessLog_HealthLogsAtDebug(t *testing.T) {
	var buf bytes.Buffer
	ts, _, _ := testServerLogged(t, slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	resp := authedGet(t, ts.URL+"/v1/health", "", "")
	_ = resp.Body.Close()

	if rec := accessLine(t, &buf, "/v1/health"); rec["level"] != "DEBUG" {
		t.Errorf("/v1/health access level = %v, want DEBUG (frequent poll must not flood the default log)", rec["level"])
	}
}
