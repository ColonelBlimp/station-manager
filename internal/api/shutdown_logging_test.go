package api

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// Acceptance criteria for the HTTP-lifecycle logging gaps
// (docs/reviews/api-logging-gaps.md A1 and A9). Operator's wording, 2026-08-01:
//
//	A1. When controlled HTTP shutdown begins, smd.log records exactly one
//	    drain-started event. After http.Server.Shutdown returns successfully, it
//	    records exactly one completion event. No opening event means this path
//	    never began; opening without completion means it began but did not
//	    complete.
//
//	A9. When net/http writes a server diagnostic through ErrorLog, smd.log
//	    receives a Warn event containing that diagnostic. In the absence of such
//	    a diagnostic, that event is absent.
//
// Two judgements are baked in deliberately, both the operator's (2026-08-01):
//
//   - A1's criterion is NOT "a crash leaves neither line". A crash during
//     shutdown may leave only the opening line, or may land after Shutdown
//     returned. The pair proves the HTTP lifecycle TRANSITION, not that the
//     process survived — so the distinguishable middle state is
//     opened-without-completion, and that is what these tests pin.
//   - The shutdown TRIGGER (signal / restart / server error) is deliberately not
//     carried here. cmd/smd already logs it immediately before calling
//     StopAccepting (main.go:943), so pushing trigger semantics into internal/api
//     would widen this package's interface for a fact that is already recorded
//     upstream.
//
// Records are parsed as zerolog JSON and matched on the `message` field. A
// substring search over the raw buffer would also match a record that merely
// mentions the text in an unrelated field, which would pass against an
// implementation that logged the right words in the wrong place.
//
// The repeated-call cases are load-bearing rather than defensive: StopAccepting
// and Shutdown are both documented idempotent, so an implementation that logs on
// method ENTRY satisfies "records an event" while emitting duplicate lifecycle
// markers — and a duplicate opening marker destroys the very
// opened-without-completion signal the criterion rests on.

const (
	msgDrainStarted    = "HTTP server draining"
	msgShutdownDone    = "HTTP server shutdown complete"
	msgHTTPServerError = "http server error"
)

// captureServer builds a test Server whose logger writes into the returned
// buffer, so assertions can be made on emitted records.
func captureServer(t *testing.T) (*Server, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	srv := testServerWithLogger(t, nil, nil, logging.NewForWriter(buf))
	return srv, buf
}

// logRecords parses the buffer as newline-delimited zerolog JSON.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		out = append(out, rec)
	}
	return out
}

// countMessage returns how many records carry exactly this `message` value.
func countMessage(t *testing.T, buf *bytes.Buffer, message string) int {
	t.Helper()
	n := 0
	for _, rec := range logRecords(t, buf) {
		if rec["message"] == message {
			n++
		}
	}
	return n
}

// findMessage returns the first record with this `message`, or nil.
func findMessage(t *testing.T, buf *bytes.Buffer, message string) map[string]any {
	t.Helper()
	for _, rec := range logRecords(t, buf) {
		if rec["message"] == message {
			return rec
		}
	}
	return nil
}

// A1, first half: beginning the drain emits exactly one opening event.
func TestShutdownLogging_StopAcceptingRecordsExactlyOneDrainStarted(t *testing.T) {
	srv, buf := captureServer(t)

	srv.StopAccepting()

	if got := countMessage(t, buf, msgDrainStarted); got != 1 {
		t.Fatalf("drain-started events = %d, want exactly 1\n%s", got, buf.String())
	}
}

// A1, second half: a completed Shutdown emits exactly one completion event, and
// the pair appears in order.
func TestShutdownLogging_ShutdownRecordsExactlyOneCompletion(t *testing.T) {
	srv, buf := captureServer(t)

	srv.StopAccepting()
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if got := countMessage(t, buf, msgDrainStarted); got != 1 {
		t.Errorf("drain-started events = %d, want exactly 1", got)
	}
	if got := countMessage(t, buf, msgShutdownDone); got != 1 {
		t.Errorf("completion events = %d, want exactly 1", got)
	}

	// Order matters: the criterion reads the pair as a transition.
	var sawOpen bool
	for _, rec := range logRecords(t, buf) {
		switch rec["message"] {
		case msgDrainStarted:
			sawOpen = true
		case msgShutdownDone:
			if !sawOpen {
				t.Fatalf("completion event preceded the drain-started event\n%s", buf.String())
			}
		}
	}
}

// A1's distinguishable middle state: drain begun, shutdown not completed. This
// is the state the criterion exists to make readable, so it is asserted
// directly rather than inferred from the two tests above.
func TestShutdownLogging_DrainWithoutShutdownIsDistinguishable(t *testing.T) {
	srv, buf := captureServer(t)

	srv.StopAccepting() // and deliberately no Shutdown

	if got := countMessage(t, buf, msgDrainStarted); got != 1 {
		t.Errorf("drain-started events = %d, want exactly 1", got)
	}
	if got := countMessage(t, buf, msgShutdownDone); got != 0 {
		t.Errorf("completion events = %d, want 0 — shutdown never completed", got)
	}
}

// A1, idempotence. Both methods are documented idempotent; logging on method
// entry would emit a marker per call and destroy the exactly-one guarantee the
// criterion rests on.
func TestShutdownLogging_RepeatedCallsDoNotDuplicateMarkers(t *testing.T) {
	srv, buf := captureServer(t)

	srv.StopAccepting()
	srv.StopAccepting()
	srv.StopAccepting()
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}

	if got := countMessage(t, buf, msgDrainStarted); got != 1 {
		t.Errorf("drain-started events = %d after 3 StopAccepting calls, want exactly 1", got)
	}
	if got := countMessage(t, buf, msgShutdownDone); got != 1 {
		t.Errorf("completion events = %d after 2 Shutdown calls, want exactly 1", got)
	}
}

// A9: net/http's own diagnostics must reach smd.log. The server's ErrorLog is
// the only channel Go offers for them, and it is currently unset — so they go to
// the standard logger (stderr/journal) instead of the file an operator reads.
func TestHTTPServerErrorLog_IsWiredToTheStructuredLogger(t *testing.T) {
	srv, buf := captureServer(t)

	if srv.httpServer == nil {
		t.Fatal("httpServer is nil; New should have constructed it")
	}
	if srv.httpServer.ErrorLog == nil {
		t.Fatal("http.Server.ErrorLog is nil — net/http diagnostics bypass smd.log")
	}

	srv.httpServer.ErrorLog.Print("simulated transport diagnostic")

	rec := findMessage(t, buf, msgHTTPServerError)
	if rec == nil {
		t.Fatalf("no %q record after writing through ErrorLog\n%s", msgHTTPServerError, buf.String())
	}
	if rec["level"] != "warn" {
		t.Errorf("level = %v, want warn — these are transport abnormalities, not access events", rec["level"])
	}
	// The diagnostic itself must survive, or the wiring records only that
	// something happened and not what.
	detail, _ := rec["error"].(string)
	if !strings.Contains(detail, "simulated transport diagnostic") {
		t.Errorf("record does not carry the diagnostic text; error = %q", detail)
	}
}

// A9's negative half — "in the absence of such a diagnostic, that event is
// absent". Without this, an implementation that emitted the event
// unconditionally would pass the test above.
func TestHTTPServerErrorLog_SilentWhenNoDiagnostic(t *testing.T) {
	_, buf := captureServer(t)

	if got := countMessage(t, buf, msgHTTPServerError); got != 0 {
		t.Errorf("%q events = %d with no diagnostic written, want 0", msgHTTPServerError, got)
	}
}
