package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLimitConcurrent_Rejects503WhenFull fills every slot of the
// concurrent-request semaphore with handlers blocked on a channel,
// then fires one more request and asserts it fails fast with 503
// server_busy rather than blocking.
func TestLimitConcurrent_Rejects503WhenFull(t *testing.T) {
	srv := testServer(t)

	// Override limiter capacity to make the test cheap and deterministic.
	srv.limits = newLoadLimiter(2, 16, 1000, 1000)

	block := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	})
	wrapped := srv.limitConcurrent(handler)

	// Fire N = capacity requests and park them inside the handler.
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, "/something", nil)
			w := httptest.NewRecorder()
			wrapped.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("background request: status = %d, want 200 (body=%s)", w.Code, w.Body.String())
			}
		})
	}

	// Give the goroutines time to acquire.
	// A short poll loop is cheaper than an arbitrary sleep.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(srv.limits.concurrent) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(srv.limits.concurrent) != 2 {
		t.Fatalf("precondition: slots held = %d, want 2", len(srv.limits.concurrent))
	}

	// This one must be refused immediately.
	req := httptest.NewRequest(http.MethodGet, "/something", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("over-budget request: status = %d, want %d (body=%s)",
			w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"server_busy"`) {
		t.Fatalf("body = %q, want server_busy code", w.Body.String())
	}

	close(block)
	wg.Wait()
}

// TestLimitConcurrent_SSEExempt asserts the concurrent cap does NOT
// count /v1/events requests; SSE has its own cap.
func TestLimitConcurrent_SSEExempt(t *testing.T) {
	srv := testServer(t)
	srv.limits = newLoadLimiter(0, 16, 1000, 1000) // cap = 0 refuses everything it touches

	reached := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	wrapped := srv.limitConcurrent(handler)

	req := httptest.NewRequest(http.MethodGet, sseEventsPath, nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if !reached {
		t.Fatalf("SSE request blocked by concurrent cap; should be exempt")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("SSE request: status = %d, want 200", w.Code)
	}
}

// TestLimitEventSubscribers_Rejects503WhenFull fills the subscriber
// cap with blocked handlers and confirms the next subscriber gets 503.
func TestLimitEventSubscribers_Rejects503WhenFull(t *testing.T) {
	srv := testServer(t)
	srv.limits = newLoadLimiter(128, 2, 1000, 1000)

	block := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	})
	wrapped := srv.limitEventSubscribers(handler)

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, sseEventsPath, nil)
			w := httptest.NewRecorder()
			wrapped.ServeHTTP(w, req)
		})
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		srv.limits.subscribersMu.Lock()
		count := srv.limits.subscribers
		srv.limits.subscribersMu.Unlock()
		if count == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	req := httptest.NewRequest(http.MethodGet, sseEventsPath, nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("over-budget subscriber: status = %d, want 503 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"server_busy"`) {
		t.Fatalf("body = %q, want server_busy code", w.Body.String())
	}

	close(block)
	wg.Wait()
}

// TestLimitEventSubscribers_ReleasesOnDisconnect checks that a slot
// is released once the SSE handler returns, so the next subscriber
// can acquire.
func TestLimitEventSubscribers_ReleasesOnDisconnect(t *testing.T) {
	srv := testServer(t)
	srv.limits = newLoadLimiter(128, 1, 1000, 1000)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := srv.limitEventSubscribers(handler)

	for i := range 3 {
		req := httptest.NewRequest(http.MethodGet, sseEventsPath, nil)
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("iteration %d: status = %d, want 200", i, w.Code)
		}
	}
}

// TestLimitSubmitRate_429AfterBurst consumes the entire burst then
// asserts the next submit gets 429 rate_limited.
func TestLimitSubmitRate_429AfterBurst(t *testing.T) {
	srv := testServer(t)
	// Rate = effectively zero, burst = 3. First 3 pass, 4th is refused.
	srv.limits = newLoadLimiter(128, 16, 0, 3)

	reached := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusAccepted)
	})
	wrapped := srv.limitSubmitRate(handler)

	for i := range 3 {
		req := httptest.NewRequest(http.MethodPost, "/v1/qso", nil)
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("burst %d: status = %d, want 202 (body=%s)", i, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/qso", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("over-budget: status = %d, want 429 (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"rate_limited"`) {
		t.Fatalf("body = %q, want rate_limited code", w.Body.String())
	}
	if reached != 3 {
		t.Fatalf("handler reached %d times, want 3", reached)
	}
}

// TestLimitSubmitRate_RefillRestoresTokens advances virtual time via
// the injected clock by directly driving allowSubmit — this test
// pokes the limiter rather than the middleware to keep it fast and
// deterministic (real wall-time refill would require a sleep).
func TestLimitSubmitRate_RefillRestoresTokens(t *testing.T) {
	lim := newLoadLimiter(128, 16, 10, 5) // 10 tokens/sec, burst 5
	base := time.Unix(1_700_000_000, 0)

	// Drain the burst.
	for i := range 5 {
		if !lim.allowSubmit(base) {
			t.Fatalf("drain %d refused; want allowed", i)
		}
	}
	if lim.allowSubmit(base) {
		t.Fatalf("post-drain allowSubmit returned true; want false")
	}

	// Advance half a second → 5 tokens/sec * 0.5 = 2.5 tokens.
	later := base.Add(500 * time.Millisecond)
	// Well, 10/sec * 0.5 = 5 tokens, capped at burst=5.
	for i := range 5 {
		if !lim.allowSubmit(later) {
			t.Fatalf("after refill, request %d refused", i)
		}
	}
	if lim.allowSubmit(later) {
		t.Fatalf("past-refill allowSubmit returned true; want false (bucket should be empty again)")
	}
}
