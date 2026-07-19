package server

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
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
