package server

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// versionServer builds a Server sufficient for the /v1/version route — no
// store or DB, so the gzip negotiation tests run without Postgres.
func versionServer() *Server {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(nil, nil, log, map[string]int64{"tok": 1}, "test-version")
}

func TestGzip_CompressesWhenAccepted(t *testing.T) {
	ts := httptest.NewServer(versionServer().Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/version", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	// DisableCompression stops the transport's transparent decompression so the
	// test can see the wire encoding.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/version: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if ce := resp.Header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", ce)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decompressed body is not JSON: %v (%q)", err, body)
	}
}

func TestGzip_IdentityWhenNotAccepted(t *testing.T) {
	ts := httptest.NewServer(versionServer().Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/version", nil)
	req.Header.Set("Accept-Encoding", "identity")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/version: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding = %q, want identity (none)", ce)
	}
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("plain body is not JSON: %v", err)
	}
}

// TestAcceptsGzip pins the Accept-Encoding negotiation (2026-07-19 review #3):
// q-values, case-insensitivity, wildcard, and explicit refusal.
func TestAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"gzip", true},
		{"GZIP", true},
		{"x-gzip", true},
		{"gzip, deflate, br", true},
		{"gzip;q=0.8", true},
		{"gzip;q=0", false},        // explicitly refused
		{"gzip; q=0.000", false},   // refused, spaced param
		{"*", true},                // wildcard accepts anything
		{"*;q=0", false},           // wildcard refusal
		{"deflate, *;q=0.1", true}, // gzip via wildcard
		{"identity", false},
		{"deflate, br", false},
		{"", false},
		{"gzip;q=0, *;q=1", false}, // explicit gzip entry beats the wildcard
	}
	for _, c := range cases {
		if got := acceptsGzip(c.header); got != c.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}

// TestGzip_RefusedQValueGetsIdentity: a client that says gzip;q=0 has refused
// gzip — the middleware must serve identity.
func TestGzip_RefusedQValueGetsIdentity(t *testing.T) {
	ts := httptest.NewServer(versionServer().Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/version", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/version: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding = %q, want identity for a refused q=0", ce)
	}
	// Vary must be present on identity responses too — a cache that stores
	// this body must not serve it to a gzip-accepting request unkeyed.
	if v := resp.Header.Get("Vary"); !strings.Contains(v, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding on the identity path", v)
	}
}

// TestGzip_PreservesResponseController pins the Unwrap chain (2026-07-19
// review #1): handleExport extends its write deadline via
// http.ResponseController, which reaches the connection by walking Unwrap();
// without gzipResponseWriter.Unwrap the extension fails and every gzip-accepting
// export (i.e. every default Go client) falls back to the server-wide 2-minute
// WriteTimeout — truncating a slow full-logbook restore mid-JSON.
func TestGzip_PreservesResponseController(t *testing.T) {
	var rcErr error
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rcErr = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute))
		w.WriteHeader(http.StatusOK)
	}))
	ts := httptest.NewServer(h)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if rcErr != nil {
		t.Fatalf("SetWriteDeadline through the gzip wrapper = %v, want nil (Unwrap chain broken)", rcErr)
	}
}

// TestGzip_DefaultGoClientTransparent proves the daemon-side contract: a stock
// Go client (the reconciler's shape) advertises gzip and decompresses
// transparently — zero client change needed.
func TestGzip_DefaultGoClientTransparent(t *testing.T) {
	ts := httptest.NewServer(versionServer().Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/version")
	if err != nil {
		t.Fatalf("GET /v1/version: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("transparent gzip round-trip failed: %v", err)
	}
	if v["version"] != "test-version" {
		t.Fatalf("version = %v, want test-version", v["version"])
	}
}
