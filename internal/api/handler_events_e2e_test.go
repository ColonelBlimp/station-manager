package api

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// consumeSSE reads frames off r until the caller-provided collect fn
// returns true, or timeout elapses. Returns the accumulated frames so
// assertions can introspect them. Runs in a goroutine-friendly shape:
// the caller passes in a done chan to abort if the rest of the test
// times out from another axis.
func consumeSSE(t *testing.T, r io.Reader, stopAfter int, timeout time.Duration) []parsedFrame {
	t.Helper()
	br := bufio.NewReader(r)
	deadline := time.Now().Add(timeout)

	frames := []parsedFrame{}
	var cur parsedFrame

	for time.Now().Before(deadline) && len(frames) < stopAfter {
		line, err := br.ReadString('\n')
		if err != nil {
			return frames
		}
		line = strings.TrimRight(line, "\n")
		if line == "" {
			if cur.id != "" || cur.name != "" || cur.data != "" {
				frames = append(frames, cur)
				cur = parsedFrame{}
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "id: "):
			cur.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			cur.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		}
	}
	return frames
}

type parsedFrame struct {
	id, name, data string
}

// TestE2E_SSE_InsertFlow proves the full wire:
//
//	POST /v1/qso  →  QSO committed + qso_upload row enqueued
//	  └→ qsoservice emits qso.stored on the hub
//	worker ticks  →  stub Submit returns success
//	  └→ worker emits forward.succeeded on the hub
//	both events arrive on the SSE subscriber
//
// Exercises the whole chain: DI-wired hub shared between ingest and
// worker, both emit sites fire, SSE handler serialises frames in
// id-monotonic order, client receives them in real time.
func TestE2E_SSE_InsertFlow(t *testing.T) {
	h := startE2E(t,
		forwarderCfg("qrz", "stub", true, "insert"),
	)
	defer h.shutdown()

	// Wrap the handler in a live TCP server so a real HTTP client
	// can stream SSE frames. The internal httpServer.Handler is
	// already fully wired (including panic recovery).
	ts := httptest.NewServer(h.srv.httpServer.Handler)
	defer ts.Close()

	// Open the SSE stream before POSTing so we don't miss the
	// qso.stored/forward.succeeded events. Per the client contract
	// documented in the events handler: "open stream first, then
	// fetch state."
	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer resp.Body.Close()

	waitForSubscriberCount(t, h.srv.hub, 1)

	// Submit a QSO via the real HTTP path.
	lbID := createTestLogbook(t, h.srv, "My Log", "G4ABC")
	qsoID, qsoUUID := submitAndGetID(t, h.srv, lbID, testQsoADIF)

	// Wait until the worker has marked the upload row 'uploaded' —
	// guarantees forward.succeeded has been emitted.
	waitForUploads(t, h.srv, qsoUUID, allUploaded)

	// Drain two frames (qso.stored, forward.succeeded). Order is
	// fixed by monotonic hub IDs — qsoservice emits first, then
	// worker after its tick.
	frames := consumeSSE(t, resp.Body, 2, 3*time.Second)
	if len(frames) < 2 {
		t.Fatalf("got %d frames, want >= 2: %+v", len(frames), frames)
	}

	var sawStored, sawSucceeded bool
	for _, f := range frames {
		switch f.name {
		case events.NameQsoStored:
			var p events.QsoStoredPayload
			if err := json.Unmarshal([]byte(f.data), &p); err != nil {
				t.Fatalf("qso.stored payload: %v, raw=%q", err, f.data)
			}
			if p.QsoID != qsoID || p.LogbookID != lbID {
				t.Errorf("qso.stored payload = %+v, want {qso:%d, logbook:%d}", p, qsoID, lbID)
			}
			sawStored = true

		case events.NameForwardSucceeded:
			var p events.ForwardSucceededPayload
			if err := json.Unmarshal([]byte(f.data), &p); err != nil {
				t.Fatalf("forward.succeeded payload: %v, raw=%q", err, f.data)
			}
			if p.QsoID != qsoID {
				t.Errorf("forward.succeeded QsoID = %d, want %d", p.QsoID, qsoID)
			}
			if p.ForwarderName != "qrz" {
				t.Errorf("ForwarderName = %q, want qrz", p.ForwarderName)
			}
			if p.Action != "insert" {
				t.Errorf("Action = %q, want insert", p.Action)
			}
			if p.UpstreamID != "stub-ok" {
				t.Errorf("UpstreamID = %q, want stub-ok", p.UpstreamID)
			}
			if p.Attempts != 1 {
				t.Errorf("Attempts = %d, want 1", p.Attempts)
			}
			sawSucceeded = true
		}
	}
	if !sawStored {
		t.Error("did not receive qso.stored frame")
	}
	if !sawSucceeded {
		t.Error("did not receive forward.succeeded frame")
	}
}

// TestE2E_SSE_FailureFlow proves forward.failed is emitted when the
// stub is configured to always return a terminal outcome. The row
// should transition pending → failed and the subscriber should see
// exactly one forward.failed frame (no forward.succeeded).
func TestE2E_SSE_FailureFlow(t *testing.T) {
	// Stub in always_terminal mode — every Submit is a terminal fail.
	terminalCfg := types.ForwarderConfig{
		Name:            "qrz",
		Type:            "stub",
		Enabled:         true,
		Credentials:     json.RawMessage(`{"mode":"always_terminal"}`),
		ActionFilter:    []string{"insert"},
		TickIntervalSec: 1,
		BatchSize:       1,
	}
	h := startE2E(t, terminalCfg)
	defer h.shutdown()

	ts := httptest.NewServer(h.srv.httpServer.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer resp.Body.Close()

	waitForSubscriberCount(t, h.srv.hub, 1)

	lbID := createTestLogbook(t, h.srv, "My Log", "G4ABC")
	qsoID, qsoUUID := submitAndGetID(t, h.srv, lbID, testQsoADIF)

	// Wait for the row to settle on 'failed'.
	waitForUploads(t, h.srv, qsoUUID, func(rs []types.QsoUpload) bool {
		if len(rs) == 0 {
			return false
		}
		for _, r := range rs {
			if r.Status != "failed" {
				return false
			}
		}
		return true
	})

	frames := consumeSSE(t, resp.Body, 2, 3*time.Second)

	var sawFailed bool
	for _, f := range frames {
		if f.name == events.NameForwardSucceeded {
			t.Errorf("unexpected forward.succeeded in failure flow: %+v", f)
		}
		if f.name == events.NameForwardFailed {
			var p events.ForwardFailedPayload
			if err := json.Unmarshal([]byte(f.data), &p); err != nil {
				t.Fatalf("forward.failed payload: %v, raw=%q", err, f.data)
			}
			if p.QsoID != qsoID || p.ForwarderName != "qrz" || p.Action != "insert" {
				t.Errorf("payload = %+v", p)
			}
			if p.Reason == "" {
				t.Error("Reason is empty; want the terminal error text")
			}
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Fatalf("did not receive forward.failed frame; got %+v", frames)
	}
}
