package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Acceptance criterion for docs/reviews/forwarding-logging-gaps.md F4.
// Operator's wording, 2026-08-01:
//
//	When forwarding.Build rejects an enabled forwarder during startup, smd.log
//	records an Error identifying the forwarder, type, and fault before startup
//	aborts, without serializing its credentials. Other startup failures do not
//	produce that specific event.
//
// Why this exists: internal/forwarding/registry.go:64 states, as the
// justification for its "a constructor's error MUST NOT embed a credential
// value" rule, that these errors are "logged as a startup fatal by
// spawnForwarderWorkers". They are not — spawnForwarderWorkers only RETURNS the
// error, which reaches main.go:132 and is printed to STDERR. So a credential or
// configuration fault that stops the daemon leaves `smd starting` followed by
// `smd stopped` in the file an operator or remote admin actually reads.
//
// Scope is deliberately narrow (operator, 2026-08-01): the forwarding.Build
// failure ONLY. A run-wide startup-fatal logger has different ownership and
// duplicate-logging questions and is not this task. Missing retry defaults and
// worker.New failures must NOT be folded in — the last test here pins that.
//
// Level is Error, not zerolog Fatal: returning the error must remain responsible
// for orderly deferred cleanup, which Fatal would skip by calling os.Exit.

const msgForwarderBuildFailed = "forwarder build failed"

// credSentinel is a value that appears ONLY inside a forwarder's credentials, so
// its presence anywhere in the log proves the config was serialized wholesale.
const credSentinel = "s3cret-sentinel-do-not-log"

// spawnAndCapture runs spawnForwarderWorkers with a capture logger and returns
// the spawn error plus everything that logger emitted.
//
// It deliberately passes a DIFFERENT logger from the one inside the test deps:
// the database and config services keep theirs, so the buffer holds only what
// spawnForwarderWorkers itself wrote, with no migration noise to filter out.
func spawnAndCapture(t *testing.T, fwds []types.ForwarderConfig) (error, *bytes.Buffer) {
	t.Helper()
	db, _, cfgSvc := newTestDepsWithCfg(t, nil)
	hub := events.NewHub()
	t.Cleanup(func() { hub.Close() })

	buf := &bytes.Buffer{}
	logger := logging.NewForWriter(buf)
	t.Cleanup(func() { _ = logger.Close() })

	qsoSvc := &qsoservice.Service{DB: db, Logger: logger, Config: cfgSvc, Hub: hub}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	err := spawnForwarderWorkers(ctx, &wg, fwds, db, qsoSvc, logger, hub)
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker goroutines did not drain")
	}
	return err, buf
}

func buildLogRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
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

func findBuildFailure(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	for _, rec := range buildLogRecords(t, buf) {
		if rec["message"] == msgForwarderBuildFailed {
			return rec
		}
	}
	return nil
}

// The positive case: a Build rejection is recorded at Error, naming the
// forwarder, its type and the fault — and startup still aborts.
func TestForwarderBuildFailure_IsLoggedAtErrorWithForwarderTypeAndFault(t *testing.T) {
	err, buf := spawnAndCapture(t, []types.ForwarderConfig{{
		Name:        "mystery",
		Type:        "nonexistent-type-xyz",
		Enabled:     true,
		Credentials: json.RawMessage(`{"api_key":"` + credSentinel + `"}`),
	}})

	if err == nil {
		t.Fatal("spawnForwarderWorkers should still abort startup on a Build failure")
	}

	rec := findBuildFailure(t, buf)
	if rec == nil {
		t.Fatalf("no %q record; the cause reaches only stderr\n%s", msgForwarderBuildFailed, buf.String())
	}
	if rec["level"] != "error" {
		t.Errorf("level = %v, want error", rec["level"])
	}
	if rec["forwarder"] != "mystery" {
		t.Errorf("forwarder = %v, want %q", rec["forwarder"], "mystery")
	}
	if rec["type"] != "nonexistent-type-xyz" {
		t.Errorf("type = %v, want %q", rec["type"], "nonexistent-type-xyz")
	}
	// The fault itself must survive, or the record says only that something
	// failed and not what — which is the state this finding is about.
	if fault, _ := rec["error"].(string); strings.TrimSpace(fault) == "" {
		t.Errorf("record carries no fault detail; error = %q", fault)
	}
}

// The credential clause. The risk is an implementation that logs the whole
// ForwarderConfig (.Interface("config", fc)) rather than named fields — which
// would satisfy every assertion above while writing the operator's API key into
// a 0644 file. The sentinel appears ONLY in Credentials, so its presence
// anywhere in the buffer is proof of wholesale serialization.
func TestForwarderBuildFailure_DoesNotSerializeCredentials(t *testing.T) {
	_, buf := spawnAndCapture(t, []types.ForwarderConfig{{
		Name:        "mystery",
		Type:        "nonexistent-type-xyz",
		Enabled:     true,
		Credentials: json.RawMessage(`{"api_key":"` + credSentinel + `"}`),
	}})

	if strings.Contains(buf.String(), credSentinel) {
		t.Fatalf("credential value leaked into the log — the config was serialized wholesale\n%s", buf.String())
	}
	// Guard against the test passing because nothing was logged at all: the
	// sentinel-absence assertion is only meaningful once the record exists.
	if findBuildFailure(t, buf) == nil {
		t.Fatalf("no %q record, so the no-credentials assertion proves nothing", msgForwarderBuildFailed)
	}
}

// The negative clause — "other startup failures do not produce that specific
// event". Without this, an implementation that logged the same message from the
// shared error path would pass every test above while over-claiming: a missing
// retry default is not a Build rejection, and folding the two together is
// exactly what the narrow scope forbids.
func TestForwarderBuildFailure_NotEmittedForOtherStartupFailures(t *testing.T) {
	err, buf := spawnAndCapture(t, []types.ForwarderConfig{{
		Name:        "no-retry-config",
		Type:        testNoRetryDefaultType,
		Enabled:     true,
		Credentials: stubCreds(t),
	}})

	if err == nil {
		t.Fatal("a missing retry default should still abort startup")
	}
	if rec := findBuildFailure(t, buf); rec != nil {
		t.Errorf("a missing retry default emitted the Build-failure event: %v", rec)
	}
}

// F15 — the worker-started record carries the EFFECTIVE retry policy (max attempts +
// backoff bounds), so later retry behaviour is reconstructable from the log alone: a
// type's registered DefaultRetry need not appear in config.json.
func TestForwarderWorkerStarted_LogsRetryPolicy(t *testing.T) {
	err, buf := spawnAndCapture(t, []types.ForwarderConfig{{
		Name:            "stub-retry",
		Type:            stub.Type,
		Enabled:         true,
		Credentials:     stubCreds(t),
		TickIntervalSec: 1,
		BatchSize:       1,
		Retry:           &types.RetryConfig{MaxAttempts: 7, InitialBackoffSec: 2, MaxBackoffSec: 300},
	}})
	if err != nil {
		t.Fatalf("happy-path spawn: %v", err)
	}

	var rec map[string]any
	for _, r := range buildLogRecords(t, buf) {
		if r["message"] == "forwarder worker started" {
			rec = r
			break
		}
	}
	if rec == nil {
		t.Fatalf("no 'forwarder worker started' record\n%s", buf.String())
	}
	for k, want := range map[string]float64{
		"retry_max_attempts":        7,
		"retry_initial_backoff_sec": 2,
		"retry_max_backoff_sec":     300,
	} {
		if got, _ := rec[k].(float64); got != want {
			t.Errorf("%s = %v, want %v (the retry policy must be reconstructable, F15)", k, rec[k], want)
		}
	}
}

// A disabled forwarder is skipped before Build runs, so it must produce neither
// an error nor the event.
func TestForwarderBuildFailure_SilentWhenNothingFailsToBuild(t *testing.T) {
	err, buf := spawnAndCapture(t, []types.ForwarderConfig{{
		Name:    "mystery",
		Type:    "nonexistent-type-xyz",
		Enabled: false,
	}})

	if err != nil {
		t.Fatalf("a disabled forwarder should not abort startup: %v", err)
	}
	if rec := findBuildFailure(t, buf); rec != nil {
		t.Errorf("Build-failure event emitted for a forwarder that was never built: %v", rec)
	}
}
