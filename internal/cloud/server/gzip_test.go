package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	stderrors "errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// flushFailWriter is a ResponseWriter whose body Write always fails, so the deferred
// gz.Close() (which flushes the buffer + writes the gzip footer) errors — the C2
// truncation case.
type flushFailWriter struct{ header http.Header }

func (f *flushFailWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}
func (f *flushFailWriter) WriteHeader(int)           {}
func (f *flushFailWriter) Write([]byte) (int, error) { return 0, stderrors.New("connection reset") }

// TestGzip_FlushFailureIsLogged pins C2: a gzip flush that fails at Close means the
// client got a TRUNCATED body, yet handlers log the response as served before this
// deferred flush runs. The failure must be recorded, else a broken download and a
// clean one are the same log.
func TestGzip_FlushFailureIsLogged(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	h := gzipMiddleware(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":"a body that must flush through gzip.Close"}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/export", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	h.ServeHTTP(&flushFailWriter{}, req)

	out := logBuf.String()
	if !strings.Contains(out, "gzip response flush failed") {
		t.Fatalf("a failed gzip flush must be logged (C2); log:\n%s", out)
	}
	if !strings.Contains(out, "/v1/export") {
		t.Errorf("the flush-failure log must name the path; log:\n%s", out)
	}
}

// versionServer builds a Server sufficient for the /v1/version route — no
// store or DB, so the gzip negotiation tests run without Postgres.
func versionServer() *Server {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(nil, nil, log, map[string]int64{"tok": 1}, "test-version", 0)
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

// TestNegotiateEncoding pins the Accept-Encoding negotiation (2026-07-19
// reviews #3, round 2 #3): q-values, case-insensitivity, wildcard, explicit
// refusal, and the everything-refused → 406 tri-state.
func TestNegotiateEncoding(t *testing.T) {
	cases := []struct {
		header string
		want   contentEncoding
	}{
		{"gzip", encGzip},
		{"GZIP", encGzip},
		{"x-gzip", encGzip},
		{"gzip, deflate, br", encGzip},
		{"gzip;q=0.8", encGzip},
		{"gzip;q=0", encIdentity},      // gzip refused; identity default-acceptable
		{"gzip; q=0.000", encIdentity}, // refused, spaced param
		{"*", encGzip},                 // wildcard accepts anything
		{"deflate, *;q=0.1", encGzip},  // gzip via wildcard
		{"identity", encIdentity},
		{"deflate, br", encIdentity},
		{"", encIdentity},                // absent header → identity
		{"gzip;q=0, *;q=1", encIdentity}, // explicit gzip refusal beats the wildcard; identity via *
		{"identity;q=0, gzip", encGzip},  // identity refused but gzip accepted
		// Relative preference, not just refusal (review 2026-07-20 #4): the
		// higher effective weight wins when both codings are acceptable.
		{"gzip;q=0.1, identity;q=1", encIdentity},
		{"identity;q=1, gzip;q=0.1", encIdentity}, // order-independent
		{"gzip;q=1, identity;q=0.5", encGzip},
		{"gzip;q=0.5, *;q=1", encIdentity},      // wildcard weights identity above explicit gzip
		{"identity;q=0.2, gzip;q=0.2", encGzip}, // tie → server prefers gzip
		// Everything this server can produce refused → 406.
		{"gzip;q=0, identity;q=0", encNotAcceptable},
		{"identity;q=0", encNotAcceptable},                  // identity refused, gzip never accepted
		{"*;q=0", encNotAcceptable},                         // wildcard refuses ALL codings incl. identity
		{"gzip;q=0, identity;q=0, *;q=1", encNotAcceptable}, // explicit refusals beat the wildcard
	}
	for _, c := range cases {
		if got := negotiateEncoding(c.header); got != c.want {
			t.Errorf("negotiateEncoding(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}

// TestGzip_AllRefusedGets406: a request refusing every producible coding gets
// 406 (with Vary), not a body in a refused encoding.
func TestGzip_AllRefusedGets406(t *testing.T) {
	ts := httptest.NewServer(versionServer().Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/version", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0, identity;q=0")
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/version: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("status = %d, want 406", resp.StatusCode)
	}
	if v := resp.Header.Get("Vary"); !strings.Contains(v, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding on the 406 path", v)
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
	h := gzipMiddleware(slog.New(slog.NewTextHandler(io.Discard, nil)),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
