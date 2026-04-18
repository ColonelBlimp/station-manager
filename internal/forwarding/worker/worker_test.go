package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
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
// an initialized logger. No mocks; per CLAUDE.md this is the preferred
// shape.
type testHarness struct {
	t      *testing.T
	db     *sqlite.Service
	logger *logging.Service
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()

	cfg := config.DefaultConfig(t.TempDir())
	cfg.Datastore.Path = ":memory:"

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
		Path:                      ":memory:",
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

	return &testHarness{t: t, db: dbSvc, logger: logSvc}
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

	if err = h.db.DeleteQsoByIDTx(ctx, tx, qsoID); err != nil {
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

// buildStub constructs a stub forwarder via the registry, parameterised
// by mode. mode is one of the stub.Mode* constants; flapN is only used
// when mode == ModeFlapN.
func buildStub(t *testing.T, mode string, flapN int64) forwarding.Forwarder {
	t.Helper()
	creds := []byte(`{"mode":"` + mode + `"}`)
	if mode == stub.ModeFlapN {
		creds = []byte(`{"mode":"flap_n","flap_n":` + itoa(flapN) + `}`)
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

func itoa(n int64) string {
	// Tiny helper so the test file doesn't pull strconv just for this.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
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

// runOnce spins up the worker just long enough for tickOnce to drain
// what's claimable, then cancels and waits for Run to return. Avoids
// racing on goroutine scheduling at test shutdown.
func runOnce(t *testing.T, w *Worker) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	// The initial tick runs synchronously at the top of Run; give it a
	// moment to finish before cancelling. 100ms is generous for the
	// in-memory DB and the stub's no-op Submit.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done
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
			if _, err := New(cfg, fwd, h.db, h.logger); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestNew_NilDeps(t *testing.T) {
	h := newHarness(t)
	fwd := buildStub(t, stub.ModeAlwaysSuccess, 0)
	cfg := defaultCfg("test")

	if _, err := New(cfg, nil, h.db, h.logger); err == nil {
		t.Fatal("expected error for nil forwarder")
	}
	if _, err := New(cfg, fwd, nil, h.logger); err == nil {
		t.Fatal("expected error for nil db")
	}
	if _, err := New(cfg, fwd, h.db, nil); err == nil {
		t.Fatal("expected error for nil logger")
	}
}

// =============================================================================
// Happy path
// =============================================================================

func TestWorker_InsertSuccessPath(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	runOnce(t, w)

	row := h.fetchUpload(qsoID)
	if row.Status != status.Uploaded.String() {
		t.Fatalf("status = %q, want uploaded", row.Status)
	}
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
// Forwarder-scope isolation — workers claim only their own rows
// =============================================================================

func TestWorker_DoesNotClaimOtherForwardersRows(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	// Queue a row for "other", not for the worker's own name.
	h.enqueueUpload(qsoID, "other", stub.Type, action.Insert)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runOnce(t, w)

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

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysTransient, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	before := time.Now().Unix()
	runOnce(t, w)

	row := h.fetchUpload(qsoID)
	if row.Status != status.Pending.String() {
		t.Fatalf("status = %q, want pending (backoff reschedule)", row.Status)
	}
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

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysTerminal, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runOnce(t, w)

	row := h.fetchUpload(qsoID)
	if row.Status != status.Failed.String() {
		t.Fatalf("status = %q, want failed", row.Status)
	}
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
	w, err := New(cfg, buildStub(t, stub.ModeAlwaysTransient, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runOnce(t, w)

	row := h.fetchUpload(qsoID)
	if row.Status != status.Failed.String() {
		t.Fatalf("status = %q, want failed (MaxAttempts=1 exhausted on first try)", row.Status)
	}
	if row.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", row.Attempts)
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

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runOnce(t, w)

	row := h.fetchUpload(qsoID)
	if row.Status != status.Failed.String() {
		t.Fatalf("status = %q, want failed (soft-deleted before insert)", row.Status)
	}
	if row.LastError == "" {
		t.Fatal("last_error should describe the soft-delete skip")
	}
}

func TestWorker_SoftDeletedQso_UpdateMarkedFailed(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Update)
	h.softDeleteQso(qsoID)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runOnce(t, w)

	row := h.fetchUpload(qsoID)
	if row.Status != status.Failed.String() {
		t.Fatalf("status = %q, want failed (soft-deleted update superseded by delete row)", row.Status)
	}
}

func TestWorker_SoftDeletedQso_DeleteStillForwards(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Delete)
	h.softDeleteQso(qsoID)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runOnce(t, w)

	row := h.fetchUpload(qsoID)
	if row.Status != status.Uploaded.String() {
		t.Fatalf("status = %q, want uploaded (delete must still forward)", row.Status)
	}
	if row.UpstreamID != "stub-ok" {
		t.Fatalf("upstream_id = %q, want stub-ok", row.UpstreamID)
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
	w, err := New(cfg, buildStub(t, stub.ModeAlwaysTransient, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	runOnce(t, w)

	beforeAttempts := h.fetchUpload(qsoID).Attempts // expect 1 after the first run

	// Second pass with a success stub — should NOT claim the row because
	// next_attempt_at is still in the future.
	w2, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker 2: %v", err)
	}
	runOnce(t, w2)

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

	// flap_n=2 → first two submits transient, third succeeds. One Worker
	// tick processes one row only once, so we run three cycles to reach
	// the success. Between cycles the next_attempt_at clamp would keep
	// us waiting — set initial_backoff_sec to 1 s but reset the row's
	// next_attempt_at to now after each transient to exercise the full
	// state machine without sleeping through real backoff.
	stubFwd := buildStub(t, stub.ModeFlapN, 2)
	cfg := defaultCfg("stub")
	// Headroom for the test's reset hack: each real tick costs 1 stub
	// call, but the reset also bumps attempts once, so attempts grows
	// 2× as fast as real retries would. Pick a MaxAttempts that doesn't
	// exhaust before the third real submit.
	cfg.Retry.MaxAttempts = 20
	// Long ticker so only the initial tickOnce fires during runOnce's
	// 100 ms window. Each runOnce is then exactly one submit attempt,
	// keeping the attempts arithmetic deterministic.
	cfg.Tick = 10 * time.Second
	w, err := New(cfg, stubFwd, h.db, h.logger)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	// The number of runOnce cycles isn't exactly flap_n+1 because the
	// test's "normalise next_attempt_at" reset also calls
	// MarkUploadTransientRetry, which bumps attempts. That inflation is
	// bounded — each real tick costs 1 stub call regardless of how
	// many times we re-queued the row — so a small headroom over
	// flap_n+1 is enough to observe the eventual success. We don't
	// assert an exact attempts count here; the point is that a
	// flapping destination does succeed within a bounded number of
	// cycles.
	const maxCycles = 6
	for cycle := 0; cycle < maxCycles; cycle++ {
		runOnce(t, w)
		row := h.fetchUpload(qsoID)
		if row.Status == status.Uploaded.String() {
			return
		}
		// Not yet succeeded — normalise next_attempt_at so the next tick
		// can claim it again without waiting real wall time.
		if err := h.db.MarkUploadTransientRetryWithContext(
			context.Background(), row.ID, time.Now().Unix(), row.LastError,
		); err != nil {
			t.Fatalf("reset next_attempt_at: %v", err)
		}
	}

	t.Fatalf("flap_n=2 did not succeed within %d cycles", maxCycles)
}

// =============================================================================
// ctx cancellation ends Run
// =============================================================================

func TestWorker_RunReturnsWhenCtxCancelled(t *testing.T) {
	h := newHarness(t)

	w, err := New(defaultCfg("stub"), buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger)
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
