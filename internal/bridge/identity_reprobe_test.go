package bridge

// L5 acceptance — a rig left alive-but-unwritable must surface, not hide in Debug.
//
// The identity re-probe (readLoop, pipeline.go ~792) re-issues the READ while the
// rig is talking but its identity is unconfirmed. If that WRITE keeps failing,
// identity can never confirm and EVERY operator write (set-freq, mode, tune) stays
// blocked — yet the failure was logged only at Debug, invisible in a default
// deployment. Confusable states: identity still pending normally (a probe interval
// or two) vs identity stranded by persistent write failure.
//
// Criterion G6 (observable in smd.log): after identityReprobeFailThreshold (3,
// OPEN-3) CONSECUTIVE re-probe write failures, one Warn at the default level
// carrying the write cause; a landed write between failures resets the streak, so
// intermittent churn never trips it.

import (
	"context"
	stderr "errors"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
)

// --- the threshold rule (deterministic) ---------------------------------------

func TestPromoteReprobeFailure_PromotesOnceAtThreshold(t *testing.T) {
	if identityReprobeFailThreshold != 3 {
		t.Fatalf("identityReprobeFailThreshold = %d, want 3 (OPEN-3)", identityReprobeFailThreshold)
	}
	for _, tc := range []struct {
		streak int
		want   bool
	}{
		{1, false}, {2, false}, {3, true}, {4, false}, {5, false},
	} {
		if got := promoteReprobeFailure(tc.streak); got != tc.want {
			t.Errorf("promoteReprobeFailure(%d) = %v, want %v (promote exactly on the crossing)", tc.streak, got, tc.want)
		}
	}
}

// reprobeReadLoopHarness starts readLoop directly over a fresh fakeSerial whose
// writes fail, and paces identity re-probes with unparsed keep-alive frames. It
// returns the fake, log buffer, and a stop func. livenessTimeout is long so the
// rig stays "alive" and the NO-DATA re-probe (a different path, B9) never fires —
// only the identity re-probe under test does.
func reprobeReadLoopHarness(t *testing.T) (*fakeSerial, *syncBuf, func()) {
	t.Helper()
	s, buf := newIdentityLogTestService(t, "yaesu-ft710")
	s.livenessTimeout = 5 * time.Second

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

	stop := func() { cancel(); <-done }
	return fake, buf, stop
}

// pumpUntil feeds unparsed keep-alive frames (each paces one identity re-probe
// while identity stays unconfirmed) until want warn lines appear, or fails.
func pumpUntil(t *testing.T, fake *fakeSerial, buf *syncBuf, sub string, want int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for countLines(buf, sub) < want {
		if time.Now().After(deadline) {
			t.Fatalf("%q reached %d lines, want %d; log:\n%s", sub, countLines(buf, sub), want, buf.String())
		}
		fake.feedLine([]byte("SM0100")) // no FT-710 State → ErrNoMatch, keeps identity unconfirmed
		time.Sleep(8 * time.Millisecond)
	}
}

func TestReadLoop_IdentityReprobe_SustainedWriteFailure_PromotesToWarnOnce(t *testing.T) {
	prev := identityReprobeInterval
	identityReprobeInterval = 5 * time.Millisecond
	t.Cleanup(func() { identityReprobeInterval = prev })

	fake, buf, stop := reprobeReadLoopHarness(t)
	defer stop()
	fake.setWriteErr(stderr.New("write: i/o error")) // every re-probe write fails

	pumpUntil(t, fake, buf, "rig writes remain blocked", 1)
	// Keep failing — the promotion must stay a single line, not one per retry.
	for i := 0; i < 5; i++ {
		fake.feedLine([]byte("SM0100"))
		time.Sleep(8 * time.Millisecond)
	}
	stop()

	recs := matching(t, buf, "rig writes remain blocked")
	if len(recs) != 1 {
		t.Fatalf("sustained-failure warn lines = %d, want exactly 1; log:\n%s", len(recs), buf.String())
	}
	if lvl, _ := recs[0]["level"].(string); lvl != "warn" {
		t.Errorf("level = %q, want warn (default-visible, not Debug)", lvl)
	}
	if !strings.Contains(buf.String(), "write: i/o error") {
		t.Errorf("sustained-failure line missing the write cause; log:\n%s", buf.String())
	}
}

// A landed write between failures must reset the streak: two separate sustained
// episodes (fail → recover → fail) produce TWO warns. Without the reset the streak
// would only ever grow and `== threshold` would fire at most once for all time.
func TestReadLoop_IdentityReprobe_SuccessfulWriteResetsStreak(t *testing.T) {
	prev := identityReprobeInterval
	identityReprobeInterval = 5 * time.Millisecond
	t.Cleanup(func() { identityReprobeInterval = prev })

	fake, buf, stop := reprobeReadLoopHarness(t)
	defer stop()

	fake.setWriteErr(stderr.New("write: i/o error"))
	pumpUntil(t, fake, buf, "rig writes remain blocked", 1) // first episode warns

	// Writes land again (rig still sends no ID, so identity stays unconfirmed and
	// the re-probe keeps firing) → the next re-probe resets the streak to 0.
	fake.setWriteErr(nil)
	for i := 0; i < 4; i++ {
		fake.feedLine([]byte("SM0100"))
		time.Sleep(8 * time.Millisecond)
	}

	// Fail again → a fresh run of 3 must be required to warn a SECOND time.
	fake.setWriteErr(stderr.New("write: i/o error"))
	pumpUntil(t, fake, buf, "rig writes remain blocked", 2)
	stop()

	if recs := matching(t, buf, "rig writes remain blocked"); len(recs) != 2 {
		t.Fatalf("sustained-failure warn lines = %d, want 2 (streak reset between episodes); log:\n%s", len(recs), buf.String())
	}
}
