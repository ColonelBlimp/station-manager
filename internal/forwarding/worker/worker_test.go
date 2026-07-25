package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// randomDedupeKey produces a 64-char hex string that satisfies the
// dedupe_key CHECK constraint. The worker tests don't care about the
// key value, only that inserts don't trip the schema's length check.
func randomDedupeKey(t *testing.T) string {
	t.Helper()
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(buf[:])
}

// testHarness wires up real services over an in-memory sqlite db, plus
// an initialized logger and an event hub. No mocks; per CLAUDE.md this
// is the preferred shape.
type testHarness struct {
	t      *testing.T
	db     *sqlite.Service
	logger *logging.Service
	hub    *events.Hub
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()

	tmp := t.TempDir()
	cfg := config.DefaultConfig(tmp)
	// A temp-FILE DB, not ":memory:": a shared-cache in-memory DB is destroyed the
	// instant its last pooled connection is transiently dropped — which happens
	// under -race when the concurrent worker goroutine and the test both exercise
	// the pool — surfacing an intermittent "no such table: qso_upload" (seen once
	// in CI, 2026-07-25; the service's own DSN comment documents the drop-and-
	// replace). A file DB has no connection-lifetime fragility; t.TempDir() cleans it.
	dbPath := filepath.Join(tmp, "worker.db")
	cfg.Datastore.Path = dbPath

	cfgSvc := config.New(cfg)
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}

	logSvc := &logging.Service{}
	logSvc.ConfigService = cfgSvc
	logSvc.WorkingDir = cfgSvc.WorkingDir()
	if err := logSvc.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}

	dbSvc := &sqlite.Service{}
	dbSvc.ConfigService = cfgSvc
	dbSvc.LoggerService = logSvc
	if err := dbSvc.Initialize(); err != nil {
		t.Fatalf("sqlite init: %v", err)
	}
	dbSvc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      dbPath,
		MaxOpenConns:              1,
		MaxIdleConns:              1,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	if err := dbSvc.Open(); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	if err := dbSvc.Migrate(); err != nil {
		t.Fatalf("sqlite migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = dbSvc.Close()
		_ = logSvc.Close()
	})

	hub := events.NewHub()
	t.Cleanup(func() { hub.Close() })

	return &testHarness{t: t, db: dbSvc, logger: logSvc, hub: hub}
}

// seedLogbookAndQso inserts a logbook and a QSO under it, returning the
// QSO id. The callsign 'G4ABC' matches the station_callsign on the QSO
// so submit-side validation isn't relevant here — we're probing the
// worker, not ingest.
func (h *testHarness) seedLogbookAndQso() int64 {
	h.t.Helper()
	ctx := context.Background()

	lbID, err := h.db.InsertLogbookWithContext(ctx, types.Logbook{
		Name:     "Test Log",
		Callsign: "G4ABC",
	})
	if err != nil {
		h.t.Fatalf("insert logbook: %v", err)
	}

	qso := types.Qso{
		UUID:      utils.NewUUIDv7(),
		LogbookID: lbID,
		ContactedStation: types.ContactedStation{
			Call:    "M0CMC",
			Country: "England",
		},
		QsoDetails: types.QsoDetails{
			Band:    "40m",
			Mode:    "SSB",
			Freq:    "7.050",
			QsoDate: "20250508",
			TimeOn:  "0845",
			TimeOff: "0850",
			RstSent: "59",
			RstRcvd: "59",
		},
		LoggingStation: types.LoggingStation{StationCallsign: "G4ABC"},
		DedupeKey:      randomDedupeKey(h.t),
	}
	qsoID, err := h.db.InsertQsoWithContext(ctx, qso)
	if err != nil {
		h.t.Fatalf("insert qso: %v", err)
	}
	return qsoID
}

// enqueueUpload creates a qso_upload row via the tx-only insert path.
// status defaults to 'pending' and next_attempt_at to now (defaults on
// the insert path), so the row is immediately claimable.
func (h *testHarness) enqueueUpload(qsoID int64, fwdName, fwdType string, act action.Action) {
	h.t.Helper()
	ctx := context.Background()

	tx, cancel, err := h.db.BeginTxContext(ctx)
	if err != nil {
		h.t.Fatalf("begin tx: %v", err)
	}
	defer cancel()

	if err = h.db.InsertQsoUploadTx(ctx, tx, qsoID, act, fwdName, fwdType); err != nil {
		_ = tx.Rollback()
		h.t.Fatalf("insert qso_upload: %v", err)
	}
	if err = tx.Commit(); err != nil {
		h.t.Fatalf("commit: %v", err)
	}
}

// softDeleteQso soft-deletes a QSO directly via the tx path. Bypasses
// qsoservice.Delete so we don't inadvertently create additional
// qso_upload rows while arranging a test fixture.
func (h *testHarness) softDeleteQso(qsoID int64) {
	h.t.Helper()
	ctx := context.Background()

	tx, cancel, err := h.db.BeginTxContext(ctx)
	if err != nil {
		h.t.Fatalf("begin tx: %v", err)
	}
	defer cancel()

	if _, err = h.db.DeleteQsoByIDTx(ctx, tx, qsoID); err != nil {
		_ = tx.Rollback()
		h.t.Fatalf("soft-delete qso: %v", err)
	}
	if err = tx.Commit(); err != nil {
		h.t.Fatalf("commit: %v", err)
	}
}

// fetchUpload returns the single qso_upload row for qsoID. Fails the
// test if there isn't exactly one.
func (h *testHarness) fetchUpload(qsoID int64) types.QsoUpload {
	h.t.Helper()
	rows, err := h.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		h.t.Fatalf("fetch uploads: %v", err)
	}
	if len(rows) != 1 {
		h.t.Fatalf("fetchUpload: got %d rows, want 1", len(rows))
	}
	return rows[0]
}

// seedSuccessfulInsert arranges the DB state "a prior successful
// insert for (qsoID, forwarderName) with upstream_id=X" so that the
// stage-5 delete-LOGID lookup finds something. Used by delete-path
// tests that otherwise would trip the "no upstream id" terminal
// guard immediately.
func (h *testHarness) seedSuccessfulInsert(qsoID int64, forwarderName, forwarderType, upstreamID string) {
	h.t.Helper()
	h.enqueueUpload(qsoID, forwarderName, forwarderType, action.Insert)

	claimed, err := h.db.ClaimPendingUploadsWithContext(
		context.Background(), forwarderName, 1,
	)
	if err != nil {
		h.t.Fatalf("claim seeded insert: %v", err)
	}
	if len(claimed) != 1 {
		h.t.Fatalf("claim seeded insert: got %d rows, want 1", len(claimed))
	}
	if err := h.db.MarkUploadSuccessWithContext(
		context.Background(), claimed[0].ID, upstreamID,
	); err != nil {
		h.t.Fatalf("mark seeded insert success: %v", err)
	}
}

// fetchUploadByAction returns the qso_upload row for (qsoID, act).
// Fails the test if zero or more-than-one row matches.
func (h *testHarness) fetchUploadByAction(qsoID int64, act action.Action) types.QsoUpload {
	h.t.Helper()
	rows, err := h.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		h.t.Fatalf("fetch uploads: %v", err)
	}
	var match []types.QsoUpload
	for _, r := range rows {
		if r.Action == act.String() {
			match = append(match, r)
		}
	}
	if len(match) != 1 {
		h.t.Fatalf("fetchUploadByAction(%s): got %d rows, want 1", act, len(match))
	}
	return match[0]
}

// buildStub constructs a stub forwarder via the registry, parameterised
// by mode. mode is one of the stub.Mode* constants; flapN is only used
// when mode == ModeFlapN.
func buildStub(t *testing.T, mode string, flapN int64) forwarding.Forwarder {
	t.Helper()
	creds := []byte(`{"mode":"` + mode + `"}`)
	if mode == stub.ModeFlapN {
		creds = []byte(`{"mode":"flap_n","flap_n":` + strconv.FormatInt(flapN, 10) + `}`)
	}
	fwd, err := forwarding.Build(types.ForwarderConfig{
		Name:        "test",
		Type:        stub.Type,
		Credentials: creds,
	})
	if err != nil {
		t.Fatalf("build stub: %v", err)
	}
	return fwd
}

// defaultCfg returns a Worker Config suitable for tests: short tick so
// the initial tick is all we usually need, small batch, and a retry
// policy that's tight enough the retry-budget tests don't wait long.
func defaultCfg(name string) Config {
	return Config{
		Name:  name,
		Tick:  50 * time.Millisecond,
		Batch: 5,
		Retry: types.RetryConfig{
			MaxAttempts:       3,
			InitialBackoffSec: 1,
			MaxBackoffSec:     60,
		},
	}
}

// runUntil runs the worker until match reports true for the single
// qso_upload row of qsoID, then cancels ctx and waits for Run to
// return. Polls at 10 ms against an in-memory DB, so convergence is
// cheap; a deadline guards against bugs that would otherwise hang the
// suite. Use this for positive assertions ("row reaches status X") —
// it's deterministic across test-harness load, unlike a fixed sleep.
func runUntil(t *testing.T, w *Worker, h *testHarness, qsoID int64, match func(types.QsoUpload) bool) types.QsoUpload {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := h.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
		if err == nil && len(rows) == 1 && match(rows[0]) {
			cancel()
			<-done
			return rows[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("runUntil: condition not reached within deadline")
	return types.QsoUpload{}
}

// runFor runs the worker for a bounded duration, then cancels. Use
// this for negative assertions ("after running, the row is still
// pending") where there's no positive transition to wait for.
func runFor(t *testing.T, w *Worker, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	time.Sleep(d)
	cancel()
	<-done
}

// runUntilAction is like runUntil but tolerates multiple qso_upload
// rows per QSO. It waits for a row with the given action to satisfy
// match. Needed for stage-5 delete-path tests where we pre-seed a
// successful insert row so the LOGID lookup succeeds — in those
// tests there are 2 rows (insert + delete).
func runUntilAction(
	t *testing.T, w *Worker, h *testHarness, qsoID int64,
	act action.Action, match func(types.QsoUpload) bool,
) types.QsoUpload {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := h.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
		if err == nil {
			for _, r := range rows {
				if r.Action == act.String() && match(r) {
					cancel()
					<-done
					return r
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatalf("runUntilAction(%s): condition not reached within deadline", act)
	return types.QsoUpload{}
}

// recordingForwarder is a test-only Forwarder that captures the
// arguments each Submit call receives. Lets delete-path tests
// inspect whether the worker actually passed through the expected
// priorUpstreamID rather than an empty string.
type recordingForwarder struct {
	typeName string
	result   forwarding.Result

	mu    sync.Mutex
	calls []recordedCall
}

type recordedCall struct {
	action          forwarding.Action
	priorUpstreamID string
}

func (r *recordingForwarder) Type() string       { return r.typeName }
func (r *recordingForwarder) AdifPrefix() string { return "" }

func (r *recordingForwarder) Submit(
	_ context.Context, _ types.Qso,
	act forwarding.Action, priorUpstreamID string,
) forwarding.Result {
	r.mu.Lock()
	r.calls = append(r.calls, recordedCall{action: act, priorUpstreamID: priorUpstreamID})
	r.mu.Unlock()
	return r.result
}

func (r *recordingForwarder) snapshotCalls() []recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// panickingForwarder is a test-only Forwarder whose Submit always panics —
// simulating a forwarder bug or a runtime fault mid-process. Used to prove the
// worker's per-row recover boundary (review 2026-06-05 M1).
type panickingForwarder struct{ typeName string }

func (p *panickingForwarder) Type() string       { return p.typeName }
func (p *panickingForwarder) AdifPrefix() string { return "" }
func (p *panickingForwarder) Submit(
	context.Context, types.Qso, forwarding.Action, string,
) forwarding.Result {
	panic("boom: forwarder Submit exploded")
}

// =============================================================================
// Constructor validation
// =============================================================================

func TestNew_ValidationRejectsBadCfg(t *testing.T) {
	h := newHarness(t)
	fwd := buildStub(t, stub.ModeAlwaysSuccess, 0)

	cases := []struct {
		name string
		mod  func(*Config)
	}{
		{"empty name", func(c *Config) { c.Name = "" }},
		{"zero tick", func(c *Config) { c.Tick = 0 }},
		{"zero batch", func(c *Config) { c.Batch = 0 }},
		{"zero max attempts", func(c *Config) { c.Retry.MaxAttempts = 0 }},
		{"zero initial backoff", func(c *Config) { c.Retry.InitialBackoffSec = 0 }},
		{"max < initial", func(c *Config) { c.Retry.MaxBackoffSec = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultCfg("test")
			tc.mod(&cfg)
			if _, err := New(cfg, fwd, h.db, h.logger, h.hub); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestNew_NilDeps(t *testing.T) {
	h := newHarness(t)
	fwd := buildStub(t, stub.ModeAlwaysSuccess, 0)
	cfg := defaultCfg("test")

	if _, err := New(cfg, nil, h.db, h.logger, h.hub); err == nil {
		t.Fatal("expected error for nil forwarder")
	}
	if _, err := New(cfg, fwd, nil, h.logger, h.hub); err == nil {
		t.Fatal("expected error for nil db")
	}
	if _, err := New(cfg, fwd, h.db, nil, h.hub); err == nil {
		t.Fatal("expected error for nil logger")
	}
	if _, err := New(cfg, fwd, h.db, h.logger, nil); err == nil {
		t.Fatal("expected error for nil hub")
	}
}

// =============================================================================
// Happy path
// =============================================================================

func TestWorker_InsertSuccessPath(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})
	if row.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", row.Attempts)
	}
	if row.UpstreamID != "stub-ok" {
		t.Fatalf("upstream_id = %q, want stub-ok", row.UpstreamID)
	}
	if row.LastError != "" {
		t.Fatalf("last_error = %q, want empty on success", row.LastError)
	}
}

// =============================================================================
// Panic recovery — a panic mid-row must not strand the claimed row
// =============================================================================

// A forwarder Submit panic must NOT crash the worker loop or leave the
// already-claimed row stuck at in_progress until a daemon restart. The per-row
// recover boundary resets it through the transient path so it's claimable
// again in-process (review 2026-06-05 M1). Without the boundary the panic would
// unwind Run and propagate out of the test goroutine, crashing the suite.
func TestWorker_PanicInSubmit_RowReclaimableWithoutRestart(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), &panickingForwarder{typeName: stub.Type}, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Pending.String() && u.Attempts >= 1
	})
	if row.Status != status.Pending.String() {
		t.Fatalf("status = %q, want pending (row stranded at in_progress?)", row.Status)
	}
	if row.Attempts < 1 {
		t.Fatalf("attempts = %d, want >= 1 (panic should count as an attempt)", row.Attempts)
	}
	if !strings.Contains(row.LastError, "panic") {
		t.Fatalf("last_error = %q, want it to mention the panic", row.LastError)
	}
}

// A non-success forwarder result with a nil Err must still produce a non-empty
// last_error (and forward.failed reason) via the fallback — the Forwarder
// contract requires Err on non-success, but the worker must not trust it blindly
// (review 2026-06-05 L2).
func TestWorker_TerminalWithNilErr_GetsFallbackReason(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	fwd := &recordingForwarder{
		typeName: stub.Type,
		result:   forwarding.Result{Outcome: forwarding.OutcomeTerminal}, // Err left nil
	}
	w, err := New(defaultCfg("stub"), fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Failed.String()
	})
	if row.LastError == "" {
		t.Fatal("last_error empty for a terminal outcome with nil Err; want fallback text")
	}
}

// =============================================================================
// Forwarder-scope isolation — workers claim only their own rows
// =============================================================================

func TestWorker_DoesNotClaimOtherForwardersRows(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	// Queue a row for "other", not for the worker's own name.
	h.enqueueUpload(qsoID, "other", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	// Negative assertion: let the worker tick for a bounded window and
	// confirm it didn't touch somebody else's row.
	runFor(t, w, 200*time.Millisecond)

	row := h.fetchUpload(qsoID)
	if row.Status != status.Pending.String() {
		t.Fatalf("status = %q, want pending (different forwarder should not touch row)", row.Status)
	}
	if row.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0", row.Attempts)
	}
}

// =============================================================================
// Transient → retry with backoff
// =============================================================================

func TestWorker_TransientRetrySchedulesNextAttempt(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysTransient, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	before := time.Now().Unix()
	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Pending.String() && u.Attempts >= 1
	})
	if row.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", row.Attempts)
	}
	if row.NextAttemptAt <= before {
		t.Fatalf("next_attempt_at=%d must be in the future (before=%d)", row.NextAttemptAt, before)
	}
	if row.LastError == "" {
		t.Fatal("last_error should be populated on transient outcome")
	}
}

// =============================================================================
// Terminal outcome — immediate failed, no retries
// =============================================================================

func TestWorker_TerminalMarksFailedImmediately(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysTerminal, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Failed.String()
	})
	if row.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (terminal goes straight to failed)", row.Attempts)
	}
	if row.LastError == "" {
		t.Fatal("last_error should be populated on terminal outcome")
	}
}

// =============================================================================
// Retry budget exhaustion — transient that hits MaxAttempts → failed
// =============================================================================

func TestWorker_TransientExhaustionBecomesFailed(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	cfg := defaultCfg("stub")
	cfg.Retry.MaxAttempts = 1 // First transient outcome exhausts the budget.
	w, err := New(cfg, buildStub(t, stub.ModeAlwaysTransient, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Failed.String()
	})
	if row.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", row.Attempts)
	}
}

// =============================================================================
// Connectivity outage (OutcomeUnreachable) — retried indefinitely, never failed
// (ADR 0038). Direct contrast with TestWorker_TransientExhaustionBecomesFailed
// above: identical MaxAttempts=1, but an unreachable host stays pending instead
// of being promoted to failed. This is what makes offline-first logging durable.
// =============================================================================

func TestWorker_UnreachableStaysPendingPastBudget(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	cfg := defaultCfg("stub")
	cfg.Retry.MaxAttempts = 1 // A transient outcome would fail here; unreachable must not.
	fwd := &recordingForwarder{
		typeName: stub.Type,
		result: forwarding.Result{
			Outcome: forwarding.OutcomeUnreachable,
			Err:     fmt.Errorf("dial tcp: connect: connection refused"),
		},
	}
	w, err := New(cfg, fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	before := time.Now().Unix()
	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Pending.String() && u.Attempts >= 1
	})
	if row.Status != status.Pending.String() {
		t.Fatalf("status = %q, want pending (unreachable must never become failed)", row.Status)
	}
	if row.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", row.Attempts)
	}
	if row.NextAttemptAt <= before {
		t.Fatalf("next_attempt_at=%d must be scheduled in the future (before=%d)", row.NextAttemptAt, before)
	}
	if row.LastError == "" {
		t.Fatal("last_error should be populated on an unreachable outcome")
	}
}

// TestWorker_UnreachableRetriesAcrossCycles proves the retry is genuinely
// indefinite, not just one extra attempt: with MaxAttempts=1 the row is claimed
// and submitted repeatedly (attempts climbs past the budget) yet never lands in
// failed. Uses the minimum 1s backoff, so it waits a real second — skipped in
// -short to keep the fast CI lane quick.
func TestWorker_UnreachableRetriesAcrossCycles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping backoff-timed multi-cycle test in -short")
	}
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	cfg := defaultCfg("stub")
	cfg.Retry.MaxAttempts = 1
	cfg.Retry.InitialBackoffSec = 1 // Minimum; second claim lands ~1s after the first.
	fwd := &recordingForwarder{
		typeName: stub.Type,
		result: forwarding.Result{
			Outcome: forwarding.OutcomeUnreachable,
			Err:     fmt.Errorf("dial tcp: i/o timeout"),
		},
	}
	w, err := New(cfg, fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		// Attempts climbing to >=2 with MaxAttempts=1 proves the budget no
		// longer gates this class; status must stay pending throughout.
		return u.Status == status.Pending.String() && u.Attempts >= 2
	})
	if row.Status != status.Pending.String() {
		t.Fatalf("status = %q, want pending after multiple cycles", row.Status)
	}
}

// =============================================================================
// Soft-delete handling — §4
// =============================================================================

func TestWorker_SoftDeletedQso_InsertMarkedFailed(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
	h.softDeleteQso(qsoID)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Failed.String()
	})
	if row.LastError == "" {
		t.Fatal("last_error should describe the soft-delete skip")
	}
}

func TestWorker_SoftDeletedQso_UpdateMarkedFailed(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Update)
	h.softDeleteQso(qsoID)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Failed.String()
	})
}

func TestWorker_SoftDeletedQso_DeleteStillForwards(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	// Stage 5: delete requires a prior successful insert row (so the
	// worker's LOGID lookup has something to return). Seed one, then
	// enqueue the delete + soft-delete the QSO.
	h.seedSuccessfulInsert(qsoID, "stub", stub.Type, "stub-prior")
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Delete)
	h.softDeleteQso(qsoID)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	row := runUntilAction(t, w, h, qsoID, action.Delete, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})
	if row.UpstreamID != "stub-ok" {
		t.Fatalf("upstream_id = %q, want stub-ok", row.UpstreamID)
	}
}

// =============================================================================
// Stage 6: ADIF-stamp dispatch
// =============================================================================

// stampingForwarder is a minimal Forwarder with a non-empty
// AdifPrefix, letting the worker exercise the
// MarkUploadSuccessWithAdifStamp dispatch path. Returns configurable
// outcomes per call.
type stampingForwarder struct {
	typeName string
	prefix   string
	result   forwarding.Result
}

func (s *stampingForwarder) Type() string       { return s.typeName }
func (s *stampingForwarder) AdifPrefix() string { return s.prefix }
func (s *stampingForwarder) Submit(
	_ context.Context, _ types.Qso,
	_ forwarding.Action, _ string,
) forwarding.Result {
	return s.result
}

func TestWorker_InsertSuccess_WithAdifPrefix_StampsQsoRow(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "qrz", "qrz", action.Insert)

	fwd := &stampingForwarder{
		typeName: "qrz",
		prefix:   "QRZCOM",
		result: forwarding.Result{
			Outcome: forwarding.OutcomeSuccess, UpstreamID: "logid-1001",
		},
	}
	w, err := New(defaultCfg("qrz"), fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})

	// QSO row must now carry the QRZCOM upload-status stamp.
	qso, err := h.db.FetchQsoByIdWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch qso: %v", err)
	}
	if qso.QrzComUploadStatus != "Y" {
		t.Fatalf("QrzComUploadStatus = %q, want Y", qso.QrzComUploadStatus)
	}
	if qso.QrzComUploadDate == "" {
		t.Fatal("QrzComUploadDate empty — worker didn't dispatch to stamp path")
	}
}

func TestWorker_DeleteSuccess_WithAdifPrefix_DoesNotStamp(t *testing.T) {
	// Delete action must not stamp even when the forwarder has an
	// ADIF prefix — stamping "uploaded" on a soft-deleted QSO is
	// nonsensical.
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	// Stage 5: delete needs a prior successful insert row for the
	// LOGID lookup. Use the plain MarkUploadSuccess path so no stamp
	// leaks from the seeding step.
	h.enqueueUpload(qsoID, "qrz", "qrz", action.Insert)
	claimed, _ := h.db.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	_ = h.db.MarkUploadSuccessWithContext(context.Background(), claimed[0].ID, "prior-logid")

	h.enqueueUpload(qsoID, "qrz", "qrz", action.Delete)
	h.softDeleteQso(qsoID)

	fwd := &stampingForwarder{
		typeName: "qrz",
		prefix:   "QRZCOM",
		result:   forwarding.Result{Outcome: forwarding.OutcomeSuccess},
	}
	w, err := New(defaultCfg("qrz"), fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	runUntilAction(t, w, h, qsoID, action.Delete, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})

	// Re-fetch the soft-deleted QSO (including-deleted variant).
	qso, err := h.db.FetchQsoByIDIncludingDeletedWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch qso incl deleted: %v", err)
	}
	if qso.QrzComUploadStatus != "" {
		t.Fatalf("QrzComUploadStatus = %q, want empty (delete must not stamp)", qso.QrzComUploadStatus)
	}
}

func TestWorker_InsertSuccess_EmptyPrefix_DoesNotStamp(t *testing.T) {
	// Forwarder with AdifPrefix="" (stub, custom webhooks) must go
	// through the plain MarkUploadSuccess path. This regression
	// guard ensures the dispatch doesn't accidentally write empty
	// JSON paths when the prefix is empty.
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})

	qso, _ := h.db.FetchQsoByIdWithContext(context.Background(), qsoID)
	if qso.QrzComUploadStatus != "" {
		t.Fatalf("QrzComUploadStatus = %q, want empty (empty-prefix must not stamp)", qso.QrzComUploadStatus)
	}
}

func TestWorker_StampSuccess_FiresOnQsoStamped(t *testing.T) {
	// A successful stamping upload bumps the QSO row's revision, so the
	// OnQsoStamped hook must fire (cmd/smd wires it to the row-mirror
	// re-enqueue — without it the SM Cloud copy drifts until reconcile).
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "qrz", "qrz", action.Insert)

	var stamped []int64
	cfg := defaultCfg("qrz")
	cfg.OnQsoStamped = func(_ context.Context, id int64) { stamped = append(stamped, id) }

	fwd := &stampingForwarder{
		typeName: "qrz",
		prefix:   "QRZCOM",
		result:   forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "logid-1002"},
	}
	w, err := New(cfg, fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	// runUntil waits for Run to return, which happens-before this read.
	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})

	if len(stamped) != 1 || stamped[0] != qsoID {
		t.Fatalf("OnQsoStamped calls = %v, want exactly [%d]", stamped, qsoID)
	}
}

func TestWorker_EmptyPrefix_DoesNotFireOnQsoStamped(t *testing.T) {
	// A non-stamping forwarder (the mirror itself, custom webhooks) never
	// bumps the row, so the hook must stay silent — this is also the
	// re-enqueue-loop guard: smcloud's own success must not re-enqueue.
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	fired := 0
	cfg := defaultCfg("stub")
	cfg.OnQsoStamped = func(_ context.Context, _ int64) { fired++ }

	w, err := New(cfg, buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})

	if fired != 0 {
		t.Fatalf("OnQsoStamped fired %d times for a non-stamping forwarder, want 0", fired)
	}
}

// =============================================================================
// Stage 5: delete action + priorUpstreamID lookup
// =============================================================================

// TestWorker_Delete_NoPriorInsert_IdLessForwarderReaches is the integration
// guard for the High review finding: a forwarder that deletes by QSO fields
// (ClubLog-shaped — no stored upstream id) must still have its delete
// dispatched even with no prior insert row. Previously the worker
// short-circuited an empty upstream id to `failed` before Submit ran, which
// made such deletes unreachable. The worker now passes the empty id through
// and lets the forwarder decide. Package-level Submit tests can't catch this
// because they call Submit directly, bypassing resolvePriorUpstreamID.
func TestWorker_Delete_NoPriorInsert_IdLessForwarderReaches(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	// No prior insert row seeded — the lookup comes back empty.
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Delete)

	// An id-less forwarder that succeeds regardless of priorUpstreamID.
	fwd := &recordingForwarder{
		typeName: stub.Type,
		result:   forwarding.Result{Outcome: forwarding.OutcomeSuccess},
	}
	w, err := New(defaultCfg("stub"), fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	runUntilAction(t, w, h, qsoID, action.Delete, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})

	calls := fwd.snapshotCalls()
	if len(calls) != 1 {
		t.Fatalf("forwarder Submit called %d times, want 1 (worker must dispatch the delete)", len(calls))
	}
	if calls[0].action != action.Delete {
		t.Fatalf("Submit action = %q, want delete", calls[0].action)
	}
	if calls[0].priorUpstreamID != "" {
		t.Fatalf("priorUpstreamID = %q, want empty (no prior insert)", calls[0].priorUpstreamID)
	}
}

// TestWorker_Delete_NoPriorInsert_IdRequiringForwarderFails proves the other
// half of the contract: a forwarder that NEEDS the upstream id (QRZ-shaped)
// still ends the row `failed` when there's no prior insert — but the failure
// is now the forwarder's terminal Result, decided after Submit is called with
// the empty id, not a worker-side short-circuit.
func TestWorker_Delete_NoPriorInsert_IdRequiringForwarderFails(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Delete)

	// Forwarder that rejects a missing id terminally (stands in for QRZ's
	// buildForm empty-priorUpstreamID guard).
	fwd := &recordingForwarder{
		typeName: stub.Type,
		result: forwarding.Result{
			Outcome: forwarding.OutcomeTerminal,
			Err:     fmt.Errorf("delete requires non-empty priorUpstreamID"),
		},
	}
	w, err := New(defaultCfg("stub"), fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Failed.String()
	})
	if !strings.Contains(row.LastError, "priorUpstreamID") {
		t.Fatalf("last_error = %q, want forwarder's terminal reason", row.LastError)
	}
	calls := fwd.snapshotCalls()
	if len(calls) != 1 {
		t.Fatalf("forwarder Submit called %d times, want 1 (worker must dispatch, forwarder decides)", len(calls))
	}
	if calls[0].priorUpstreamID != "" {
		t.Fatalf("priorUpstreamID = %q, want empty", calls[0].priorUpstreamID)
	}
}

func TestWorker_Delete_WithPriorInsert_PassesUpstreamID(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.seedSuccessfulInsert(qsoID, "stub", stub.Type, "logid-7777")
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Delete)

	// Recording forwarder so we can assert the priorUpstreamID we
	// received was the one from the seeded insert row.
	fwd := &recordingForwarder{
		typeName: stub.Type,
		result:   forwarding.Result{Outcome: forwarding.OutcomeSuccess},
	}
	w, err := New(defaultCfg("stub"), fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	runUntilAction(t, w, h, qsoID, action.Delete, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})

	calls := fwd.snapshotCalls()
	if len(calls) != 1 {
		t.Fatalf("forwarder Submit called %d times, want exactly 1", len(calls))
	}
	if calls[0].action != action.Delete {
		t.Fatalf("Submit action = %q, want delete", calls[0].action)
	}
	if calls[0].priorUpstreamID != "logid-7777" {
		t.Fatalf("Submit priorUpstreamID = %q, want 'logid-7777'", calls[0].priorUpstreamID)
	}
}

func TestWorker_InsertAndUpdate_DoNotTriggerLookup(t *testing.T) {
	// Sanity check: for insert and update actions, the worker must
	// NOT consult the upstream_id lookup and must pass empty
	// priorUpstreamID through to the forwarder. Regression guard
	// against accidentally running the delete-path lookup for
	// non-delete actions.
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	fwd := &recordingForwarder{
		typeName: stub.Type,
		result:   forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "new-id"},
	}
	w, err := New(defaultCfg("stub"), fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String()
	})

	calls := fwd.snapshotCalls()
	if len(calls) != 1 {
		t.Fatalf("Submit called %d times, want 1", len(calls))
	}
	if calls[0].action != action.Insert {
		t.Fatalf("Submit action = %q, want insert", calls[0].action)
	}
	if calls[0].priorUpstreamID != "" {
		t.Fatalf("Submit priorUpstreamID = %q, want empty for insert", calls[0].priorUpstreamID)
	}
}

// =============================================================================
// next_attempt_at gating — future rows aren't claimed
// =============================================================================

func TestWorker_DoesNotClaimFutureRows(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	// Arrange the row into a state with next_attempt_at in the future:
	// a transient outcome with max_attempts=10 bumps next_attempt_at out
	// by one InitialBackoffSec.
	cfg := defaultCfg("stub")
	cfg.Retry.InitialBackoffSec = 60 // 60 s into the future
	cfg.Retry.MaxBackoffSec = 60
	w, err := New(cfg, buildStub(t, stub.ModeAlwaysTransient, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	// Wait for the first transient outcome to push next_attempt_at 60 s
	// into the future.
	runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Pending.String() && u.Attempts >= 1
	})

	beforeAttempts := h.fetchUpload(qsoID).Attempts

	// Second worker (success stub) must NOT re-claim the row, because
	// next_attempt_at is still 60 s out. Let it tick for a bounded
	// window and confirm no transition happened.
	w2, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker 2: %v", err)
	}
	runFor(t, w2, 200*time.Millisecond)

	row := h.fetchUpload(qsoID)
	if row.Status != status.Pending.String() {
		t.Fatalf("status = %q, want still pending (next_attempt_at gating)", row.Status)
	}
	if row.Attempts != beforeAttempts {
		t.Fatalf("attempts = %d, want unchanged %d (row should not have been re-claimed)", row.Attempts, beforeAttempts)
	}
}

// =============================================================================
// flap_n — transient until budget allows the eventual success
// =============================================================================

func TestWorker_FlapN_EventuallySucceeds(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	// flap_n=2: first two submits transient, third succeeds. Flat
	// 1 s backoff (MaxBackoffSec=1 clamps the exponential growth) means
	// the row becomes re-claimable ~1 s after each transient outcome.
	// The worker's 50 ms tick makes the re-claim prompt, so end-to-end
	// convergence is roughly 2 × 1 s + jitter, well under the
	// deadline we extend below.
	stubFwd := buildStub(t, stub.ModeFlapN, 2)
	cfg := defaultCfg("stub")
	cfg.Retry.MaxAttempts = 5
	cfg.Retry.InitialBackoffSec = 1
	cfg.Retry.MaxBackoffSec = 1
	w, err := New(cfg, stubFwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Convergence needs ~2 s of real wall-clock because each retry
	// waits out the backoff; give a generous deadline.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		row := h.fetchUpload(qsoID)
		if row.Status == status.Uploaded.String() {
			if row.Attempts != 3 {
				t.Fatalf("attempts = %d, want 3 (flap_n=2 + success)", row.Attempts)
			}
			cancel()
			<-done
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("flap_n=2 did not succeed within deadline")
}

// =============================================================================
// ctx cancellation ends Run
// =============================================================================

func TestWorker_RunReturnsWhenCtxCancelled(t *testing.T) {
	h := newHarness(t)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}
}

// TestMarkSuccess_ReArmed_NoTerminalEvent pins review 2026-07-20
// internal/forwarding #4: when a concurrent operator edit re-arms a claimed row
// (in_progress → pending) mid-send, the worker's completion is a no-op — it must
// NOT publish a terminal forward.succeeded event (the row is still pending and
// will be re-forwarded), and the re-armed row must survive.
func TestMarkSuccess_ReArmed_NoTerminalEvent(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	// Worker claims the row → in_progress.
	claimed, err := h.db.ClaimPendingUploadsWithContext(context.Background(), "stub", 5)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v (%d rows)", err, len(claimed))
	}

	// Operator edit re-arms the same triple → back to pending.
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	sub, unsub := h.hub.Subscribe()
	defer unsub()

	// Stale completion of the claimed (now re-armed) row.
	w.markSuccess(context.Background(), claimed[0], "logid-stale")

	select {
	case ev := <-sub:
		t.Fatalf("published %q on a re-armed no-op completion; want no event", ev.Name)
	case <-time.After(50 * time.Millisecond):
	}
	if got := h.fetchUpload(qsoID); got.Status != status.Pending.String() {
		t.Fatalf("status = %q, want pending (re-arm must survive)", got.Status)
	}
}
