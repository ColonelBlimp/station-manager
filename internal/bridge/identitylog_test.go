package bridge

// B10 (internal/bridge logging audit) — the identity paths log NOTHING.
//
// Two paths in readLoop's identity block decide NOT to trust the rig and, until
// this change, left no trace in smd.log:
//
//   - identityMismatch: the rig reports a *known* model different from the
//     configured bridge.cat.driver. readLoop publishes an SSE bridge-error and
//     returns exitPermanent; runSupervisor's `case exitPermanent: return` is a
//     BARE return, so the bridge dies for the whole process lifetime with not
//     one line in smd.log — indistinguishable from a clean shutdown, and the
//     ONLY permanent-halt site that didn't log (the four in runPipeline all
//     ErrorWith first). It fires on a wrong bridge.cat.driver — a first-run
//     mistake, on a deployment where the person reading the log is not the
//     operator.
//   - identityUnrecognised: the rig answers with an ID code the rigdef doesn't
//     map. Identity is never confirmed, so every operator write path (command /
//     tune / FT8 key) stays blocked indefinitely while state display keeps
//     working — a degraded-but-running state that was announced on the SSE wire
//     only, never in the log.
//
// ACCEPTANCE (operator-observable, both assert on the emitted log record, not on
// fields the mechanism happens to carry):
//   - mismatch  -> exactly one Error line, message contains "identity mismatch",
//     carrying driver/expected/actual, emitted BEFORE readLoop returns
//     exitPermanent.
//   - unrecognised -> exactly one Warn line PER PIPELINE INSTANCE, message
//     contains "identity unrecognised", carrying driver — the once-per-instance
//     cadence matters because an unrecognised rig chatters frames and a
//     per-frame line would flood the log.
//
// Mismatch is unreachable through the shipped rigdefs (each IDENTITY value_map
// maps only its own model, so classifyIdentity yields only ""/confirmed), so the
// mismatch test drives readLoop directly with def.Model set to a model the
// decoder will not produce — the exact state a future multi-model rigdef reaches
// when a sibling model answers. The unrecognised test drives the full pipeline
// (Start + feedLine), which reaches its branch today.

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func newIdentityLogTestService(t *testing.T, driver string) (*Service, *syncBuf) {
	t.Helper()
	buf := &syncBuf{}
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: driver},
	}, logging.NewForWriter(buf))
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return s, buf
}

// TestReadLoop_IdentityMismatch_LogsErrorBeforeHalt pins the headline half of
// B10: the permanent halt on a wrong-rig mismatch leaves an Error line naming
// driver/expected/actual, so a clean shutdown and a mismatch-halt are no longer
// the same silence.
func TestReadLoop_IdentityMismatch_LogsErrorBeforeHalt(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")

	def, ok := cat.Lookup("yaesu-ft710")
	if !ok {
		t.Fatal("rigdef yaesu-ft710 not found")
	}
	initBytes, err := cat.Encode(def, initCommandName)
	if err != nil {
		t.Fatalf("encode INIT: %v", err)
	}
	readBytes, err := cat.Encode(def, readCommandName)
	if err != nil {
		t.Fatalf("encode READ: %v", err)
	}
	wantDriver := def.ID
	// The ft710 decoder maps ID0800 -> "FT-710"; expecting an FTdx10 forces the
	// classifyIdentity mismatch branch deterministically (see file header).
	def.Model = "FTdx10"

	fake := newFakeSerial()
	if !fake.feedLine([]byte("ID0800")) { // decodes IDENTITY="FT-710"
		t.Fatal("feedLine rejected")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exitCh := make(chan pipelineExitClass, 1)
	go func() { exitCh <- s.readLoop(ctx, fake, def, initBytes, readBytes) }()

	select {
	case exit := <-exitCh:
		if exit != exitPermanent {
			t.Fatalf("readLoop exit = %d, want exitPermanent(%d)", exit, exitPermanent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not halt on identity mismatch")
	}

	recs := matching(t, buf, "identity mismatch")
	if len(recs) != 1 {
		t.Fatalf("identity-mismatch log lines = %d, want exactly 1; log:\n%s", len(recs), buf.String())
	}
	rec := recs[0]
	if lvl, _ := rec["level"].(string); lvl != "error" {
		t.Errorf("mismatch log level = %q, want error", lvl)
	}
	if got, _ := rec["driver"].(string); got != wantDriver {
		t.Errorf("driver = %q, want %q", got, wantDriver)
	}
	if got, _ := rec["expected"].(string); got != "FTdx10" {
		t.Errorf("expected = %q, want FTdx10", got)
	}
	if got, _ := rec["actual"].(string); got != "FT-710" {
		t.Errorf("actual = %q, want FT-710", got)
	}
}

// TestReadLoop_UnrecognisedIdentity_LogsWarnOncePerInstance pins the reachable-
// today half of B10: an unmapped ID leaves one Warn line (not silence, not a
// per-frame flood), and identity stays unconfirmed so writes remain blocked.
func TestReadLoop_UnrecognisedIdentity_LogsWarnOncePerInstance(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")
	fake := installFakeSerial(s)

	def, ok := cat.Lookup("yaesu-ft710")
	if !ok {
		t.Fatal("rigdef yaesu-ft710 not found")
	}
	wantDriver := def.ID

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	// Three unmapped IDs then one recognisable freq push. A single read
	// goroutine drains the fake FIFO, so when the freq rig-state event arrives
	// all three unrecognised frames have been processed and the warn count is
	// final — no sleep, no race on the once-per-instance latch.
	for i := 0; i < 3; i++ {
		if !fake.feedLine([]byte("ID9999")) {
			t.Fatal("feedLine rejected")
		}
	}
	if !fake.feedLine([]byte("FA014250000")) {
		t.Fatal("feedLine rejected")
	}

	deadline := time.After(2 * time.Second)
	drained := false
	for !drained {
		select {
		case evt := <-ch:
			if evt.Name != EventRigState {
				continue
			}
			if p, ok := evt.Payload.(RigStatePayload); ok && p.VfoA == 14250000 {
				drained = true
			}
		case <-deadline:
			t.Fatal("freq rig-state never arrived; unrecognised frames not drained")
		}
	}

	recs := matching(t, buf, "identity unrecognised")
	if len(recs) != 1 {
		t.Fatalf("identity-unrecognised warn lines = %d, want exactly 1 per instance; log:\n%s", len(recs), buf.String())
	}
	rec := recs[0]
	if lvl, _ := rec["level"].(string); lvl != "warn" {
		t.Errorf("level = %q, want warn", lvl)
	}
	if got, _ := rec["driver"].(string); got != wantDriver {
		t.Errorf("driver = %q, want %q", got, wantDriver)
	}
	if s.identityOK() {
		t.Error("identity confirmed despite an unrecognised ID")
	}
}
