package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestLimit_OverLimitGets503 saturates a max=2 limiter with two parked
// requests and verifies the third is rejected immediately with 503 +
// Retry-After, then that the parked requests complete untouched.
func TestLimit_OverLimitGets503(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	h := limitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}), 2)
	ts := httptest.NewServer(h)
	defer ts.Close()

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(ts.URL)
			if err != nil {
				t.Errorf("parked request: %v", err)
				return
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("parked request status = %d, want 200", resp.StatusCode)
			}
		}()
	}
	// Both slots held before the over-limit probe, or the probe could win a
	// slot and the test would flake.
	<-entered
	<-entered

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("over-limit request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("over-limit status = %d, want 503", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("over-limit response missing Retry-After")
	}
	var envelope struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code != "overloaded" {
		t.Errorf("over-limit body = %q, want code overloaded (err %v)", body, err)
	}

	close(release)
	wg.Wait()
}

// TestLimit_SlotReleased proves a completed request frees its slot: with
// max=1, two sequential requests both succeed.
func TestLimit_SlotReleased(t *testing.T) {
	h := limitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), 1)
	ts := httptest.NewServer(h)
	defer ts.Close()

	for i := range 2 {
		resp, err := http.Get(ts.URL)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200 (slot not released?)", i, resp.StatusCode)
		}
	}
}

// TestLimit_DefaultApplied pins the <=0 → defaultMaxConcurrent fallback at
// the Server level, so a zero-value construction still gets a bounded chain.
func TestLimit_DefaultApplied(t *testing.T) {
	s := versionServer()
	if s.maxConcurrent != 0 {
		t.Fatalf("fixture maxConcurrent = %d, want 0 (testing the fallback)", s.maxConcurrent)
	}
	// The fallback lives in limitMiddleware; a request through the full
	// handler chain succeeding proves the limiter installed with a usable cap
	// rather than a zero-capacity semaphore rejecting everything.
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/version")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// TestExport_GateOverLimitGets503 pins the export concurrency gate (review
// 2026-07-20 #1): with every export slot taken, a further authenticated export
// is refused immediately with 503 + Retry-After — it must NOT reach the store,
// where it would pin one of the 5 pool connections for up to 15 minutes and,
// at 5 concurrent, starve health/uploads/reconcile. Runs without a database
// precisely because the gate must reject before any store access.
func TestExport_GateOverLimitGets503(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(nil, nil, quiet, map[string]int64{"tok-export-gate": 7}, "test", 0)
	for i := 0; i < maxConcurrentExports; i++ {
		srv.exportSlots <- struct{}{} // saturate the gate
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/export", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer tok-export-gate")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra != exportRetryAfterSeconds {
		t.Errorf("Retry-After = %q, want %q", ra, exportRetryAfterSeconds)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "overloaded" {
		t.Errorf("code = %q, want overloaded", body.Code)
	}

	// Draining one slot frees the gate for the next export (the deferred
	// release in handleExport is exercised by every PG-backed export test).
	<-srv.exportSlots
}
