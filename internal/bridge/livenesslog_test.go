package bridge

// B8 / B9 / B11 (internal/bridge logging audit) — three teardown/liveness paths
// in pipeline.go that decided NOT to act and left no trace in smd.log:
//
//   - B8  client.Close()'s error was discarded (`_ = client.Close()`). A port
//     that didn't close is why the supervisor's next reopen fails busy, and the
//     cause was nowhere.
//   - B9  the no-data re-probe's WRITE failure returned only an exit class; its
//     werr reached neither log nor SPA (which renders the code, not
//     details.error), while the sibling terminal-READ failure logged its cause.
//   - B11 the quiet-rig announce and the recovery reset were SSE-only, so an
//     outage at T1 and its recovery at T2 could not be joined in the log.
//
// ACCEPTANCE (all assert on the emitted record, not on mechanism internals):
//   - B8  -> one Warn "serial port close failed" carrying the close error.
//   - B9  -> one Warn "re-probe write failed" carrying the write error, and the
//     pipeline tears down (exitTransient).
//   - B11 -> one Warn "rig went quiet" with a strike count when the rig falls
//     silent, and one Info "liveness restored" with the strike count reached
//     when data resumes.

import (
	"context"
	stderr "errors"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// countLines is a JSON-free line counter safe to call from a waitFor poll while
// the read goroutine is still writing (matching()/records() parse and would
// t.Fatalf on a torn line — logging.Service writes whole lines, but the poll
// must not depend on that).
func countLines(buf *syncBuf, sub string) int {
	n := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, sub) {
			n++
		}
	}
	return n
}

// TestRunPipeline_SerialCloseError_LogsWarn pins B8: a port that fails to close
// on teardown leaves a Warn carrying the cause, so a subsequent busy reopen is
// traceable to it.
func TestRunPipeline_SerialCloseError_LogsWarn(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")
	fake := installFakeSerial(s)
	fake.closeErr = stderr.New("port still busy")

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait until the pipeline has opened and written INIT, so the teardown path
	// (and its client.Close) will actually run on Stop.
	waitFor(t, func() bool { return len(fake.recordedWrites()) > 0 }, "pipeline did not send INIT")

	// Stop cancels ctx and waits for runSupervisor to return, which is AFTER
	// runPipeline's deferred teardown (and Close) has run — so the buffer is
	// final once Stop returns, no sleep needed.
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	recs := matching(t, buf, "serial port close failed")
	if len(recs) != 1 {
		t.Fatalf("close-failed warn lines = %d, want 1; log:\n%s", len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "warn" {
		t.Errorf("level = %q, want warn", lvl)
	}
	if !strings.Contains(buf.String(), "port still busy") {
		t.Errorf("close-failed line missing the close cause; log:\n%s", buf.String())
	}
}

// TestReadLoop_ReprobeWriteFailure_LogsWarnAndTearsDown pins B9: a re-probe WRITE
// failure logs its cause (distinct from the terminal-READ failure) and returns
// exitTransient so the supervisor reopens.
func TestReadLoop_ReprobeWriteFailure_LogsWarnAndTearsDown(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")
	s.livenessTimeout = 20 * time.Millisecond

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

	fake := newFakeSerial()
	fake.writeErr = stderr.New("write: i/o error") // every write fails → first re-probe write fails

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exitCh := make(chan pipelineExitClass, 1)
	go func() { exitCh <- s.readLoop(ctx, fake, def, initBytes, readBytes) }()

	select {
	case exit := <-exitCh:
		if exit != exitTransient {
			t.Fatalf("readLoop exit = %d, want exitTransient(%d)", exit, exitTransient)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not tear down on re-probe write failure")
	}

	recs := matching(t, buf, "re-probe write failed")
	if len(recs) != 1 {
		t.Fatalf("re-probe-write-failed warn lines = %d, want 1; log:\n%s", len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "warn" {
		t.Errorf("level = %q, want warn", lvl)
	}
	if !strings.Contains(buf.String(), "write: i/o error") {
		t.Errorf("re-probe line missing the write cause; log:\n%s", buf.String())
	}
}

// TestReadLoop_LivenessTransitions_LogLostAndRestored pins B11: the quiet→alive
// pair each leave one durable line carrying the strike count, so an outage and
// its recovery are joinable in smd.log.
func TestReadLoop_LivenessTransitions_LogLostAndRestored(t *testing.T) {
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")
	s.livenessTimeout = 20 * time.Millisecond

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

	fake := newFakeSerial()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = s.readLoop(ctx, fake, def, initBytes, readBytes) }()

	// The rig is silent → liveness expires → "went quiet".
	waitFor(t, func() bool { return countLines(buf, "rig went quiet") >= 1 }, "no went-quiet line")
	// Data resumes with a valid rig frame → "liveness restored".
	if !fake.feedLine([]byte("FA014250000")) {
		t.Fatal("feedLine rejected")
	}
	waitFor(t, func() bool { return countLines(buf, "liveness restored") >= 1 }, "no restored line")

	// Freeze the buffer before parsing: with no more frames the loop would time
	// out again and append a second "went quiet".
	cancel()
	<-done

	lost := matching(t, buf, "rig went quiet")
	if len(lost) < 1 {
		t.Fatalf("went-quiet lines = %d, want >= 1; log:\n%s", len(lost), buf.String())
	}
	if lvl, _ := lost[0]["level"].(string); lvl != "warn" {
		t.Errorf("went-quiet level = %q, want warn", lvl)
	}
	if _, ok := lost[0]["strikes"]; !ok {
		t.Errorf("went-quiet line has no strikes field: %v", lost[0])
	}

	restored := matching(t, buf, "liveness restored")
	if len(restored) != 1 {
		t.Fatalf("restored lines = %d, want exactly 1 (one recovery edge); log:\n%s", len(restored), buf.String())
	}
	if lvl, _ := restored[0]["level"].(string); lvl != "info" {
		t.Errorf("restored level = %q, want info", lvl)
	}
	if _, ok := restored[0]["strikes"]; !ok {
		t.Errorf("restored line has no strikes field: %v", restored[0])
	}
}
