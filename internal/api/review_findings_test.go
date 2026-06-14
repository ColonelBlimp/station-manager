package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// --- H1: session-email archive filename can't escape the archive dir --------

func TestSafeArchiveFilename(t *testing.T) {
	bad := []string{
		"../../config.json", // traversal
		"../x.adi",          // traversal
		"a/b.adi",           // separator
		`a\b.adi`,           // backslash separator
		"/etc/passwd",       // absolute
		"..",                // dot-dot
		".",                 // dot
	}
	for _, n := range bad {
		if got, err := safeArchiveFilename(n); err == nil {
			t.Errorf("safeArchiveFilename(%q) = %q, nil; want an error", n, got)
		}
	}

	// Valid bare names pass; .adi is appended when missing.
	if got, err := safeArchiveFilename("session-2026.adi"); err != nil || got != "session-2026.adi" {
		t.Errorf("safeArchiveFilename(%q) = %q, %v; want %q, nil", "session-2026.adi", got, err, "session-2026.adi")
	}
	if got, err := safeArchiveFilename("myexport"); err != nil || got != "myexport.adi" {
		t.Errorf("safeArchiveFilename(%q) = %q, %v; want %q, nil", "myexport", got, err, "myexport.adi")
	}
}

// --- L1: session-email uses the shared body helper (413 on oversize) --------

func TestSessionEmail_OversizeBody_Returns413(t *testing.T) {
	srv := testServerWithMailer(t, types.SmtpConfig{
		Enabled: true, Host: "x", Port: 25, From: "f@x", TimeoutSec: 5,
	})
	big := bytes.Repeat([]byte("a"), int(srv.maxBodyBytes)+1024)
	req := httptest.NewRequest(http.MethodPost, "/v1/session/email", bytes.NewReader(big))
	w := httptest.NewRecorder()

	srv.handleSessionEmail(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize body: status = %d, want 413; body=%s", w.Code, w.Body.String())
	}
}

// --- M2: limitConcurrent exempts ALL SSE paths, not just /v1/events ---------

func TestLimitConcurrent_ExemptsAllSSEPaths(t *testing.T) {
	srv := testServer(t)
	// Zero concurrent cap → acquireConcurrent always fails, so a non-exempt
	// path gets 503 and the exemption is observable.
	srv.limits = newLoadLimiter(0, 16, 1000, 1000)

	reached := false
	h := srv.limitConcurrent(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	// A non-SSE path is blocked by the full cap.
	reached = false
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/qso/x", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("non-SSE path: status = %d, want 503", w.Code)
	}
	if reached {
		t.Error("non-SSE path reached the handler despite a full concurrent cap")
	}

	// Every SSE path is exempt and reaches the handler.
	for _, p := range []string{"/v1/events", "/v1/rig/events", "/v1/ft8/events"} {
		reached = false
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if !reached || w.Code != http.StatusOK {
			t.Errorf("SSE path %s blocked by concurrent cap: reached=%v status=%d", p, reached, w.Code)
		}
	}
}

// --- M1: /v1/events bounds the write deadline per write ---------------------

// sseDeadlineRecorder is a ResponseWriter that records every write deadline the
// handler arms (via http.NewResponseController) and is an http.Flusher.
type sseDeadlineRecorder struct {
	mu        sync.Mutex
	header    http.Header
	deadlines []time.Time
}

func newSSEDeadlineRecorder() *sseDeadlineRecorder {
	return &sseDeadlineRecorder{header: make(http.Header)}
}

func (r *sseDeadlineRecorder) Header() http.Header         { return r.header }
func (r *sseDeadlineRecorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *sseDeadlineRecorder) WriteHeader(int)             {}
func (r *sseDeadlineRecorder) Flush()                      {}

func (r *sseDeadlineRecorder) SetWriteDeadline(t time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deadlines = append(r.deadlines, t)
	return nil
}

func (r *sseDeadlineRecorder) armed() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.deadlines...)
}

func TestHandleEvents_BoundsWriteDeadlinePerWrite(t *testing.T) {
	prev := sseWriteTimeout
	sseWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { sseWriteTimeout = prev })

	srv := testServer(t)
	rec := newSSEDeadlineRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		srv.handleEvents(rec, req)
		close(done)
	}()

	// Wait for the handler to subscribe, then publish one event so a frame write
	// (and its deadline arm) happens too.
	deadline := time.Now().Add(time.Second)
	for srv.hub.SubscriberCount() == 0 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("handler did not subscribe within 1s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	srv.hub.Publish("qso.stored", map[string]any{"qso_id": int64(1), "logbook_id": int64(1)})
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	armed := rec.armed()
	if len(armed) == 0 {
		t.Fatal("no write deadline was armed; /v1/events must bound each write")
	}
	for i, d := range armed {
		if d.IsZero() {
			t.Errorf("armed deadline %d is the zero time (infinite) — a wedged write could hang the goroutine", i)
		}
	}
}

// --- L2: Server.Shutdown is idempotent --------------------------------------

func TestShutdown_Idempotent(t *testing.T) {
	srv := testServer(t)
	ctx := context.Background()
	// A second Shutdown must not panic on "close of closed channel".
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}
