package server

import "net/http"

// defaultMaxConcurrent is the in-flight request cap applied when the caller
// passes New maxConcurrent <= 0. ~3× the bounded DB pool (5 conns in
// cmd/smcloud): enough that pool-waiters plus the odd long export never
// starve legitimate traffic, small enough that a request flood is bounded at
// a handful of goroutines instead of one per connection.
const defaultMaxConcurrent = 16

// retryAfterSeconds is the Retry-After hint on a 503. Bursts drain within a
// worker tick or two; anything longer means saturation a client can't help by
// hammering.
const retryAfterSeconds = "2"

// limitMiddleware bounds in-flight requests with a semaphore (pre-Phase-2
// gate, decided 2026-07-18). The DB pool protects Postgres but not the
// process — without this, excess requests (including unauthenticated
// /v1/health hits) pile up as handler goroutines waiting for pool
// connections. Over-limit requests are rejected immediately with 503 +
// Retry-After rather than queued.
//
// Scope (2026-07-19 review #1): this bounds HANDLER concurrency only — it
// runs after net/http has already accepted the connection, spawned its
// goroutine, and parsed headers, so it cannot bound a connection or
// slow-header flood by itself. cmd/smcloud pairs it with an accept-time
// connection cap (netutil.LimitListener, 4× this cap); the two together are
// the process bound — connections at accept, request work here. Per-IP
// fairness deliberately lives at the reverse proxy (which sees real client
// IPs — this binary would only ever see the proxy's).
func limitMiddleware(next http.Handler, max int) http.Handler {
	if max <= 0 {
		max = defaultMaxConcurrent
	}
	slots := make(chan struct{}, max)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", retryAfterSeconds)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"overloaded","message":"too many concurrent requests; retry shortly"}`))
		}
	})
}
