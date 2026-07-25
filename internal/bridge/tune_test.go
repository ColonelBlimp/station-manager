package bridge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// tuneTestService builds an Enabled FTdx10 Service with a fake serial client
// installed as the active client and a known mode/power snapshot, ready for
// StartTune/StopTune white-box tests. tuneMaxDuration is set long so the
// backstop timer never fires mid-test (the auto-off test overrides it).
func tuneTestService(t *testing.T) (*Service, *fakeSerial) {
	t.Helper()
	s := newTestService(t, types.BridgeConfig{
		Enabled: true,
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	})
	f := newFakeSerial()
	s.activeClient = f
	// Identity confirmed in the happy-path helper — StartTune's write gate (H2)
	// refuses to key TX until the rig identifies as the configured driver.
	s.identityConfirmed = true
	s.lastMode = "USB"
	s.lastPower = 100
	s.tuneMaxDuration = time.Hour
	// Small but non-zero settle so the unkey→restore split is exercised (two
	// writes) without slowing the tests (task #270).
	s.tuneRestoreSettle = 5 * time.Millisecond
	return s, f
}

// answerTxStatusQueries models a healthy rig answering the ADR 0051
// read_tx_status query ("TX;") with "RX": a watcher goroutine polls the fake's
// writes and feeds observeTxStatus("0") — exactly what the readLoop would
// deliver on the real TXn; answer. Needed since the restore-gate (2026-07-19
// review P1) made the release paths wait for POSITIVE confirmation before
// restoring mode/power; without an answer they now skip the restore by design.
// Returns a stop func (t.Cleanup-compatible).
func answerTxStatusQueries(s *Service, f *fakeSerial) func() {
	done := make(chan struct{})
	go func() {
		answered := 0
		for {
			select {
			case <-done:
				return
			case <-time.After(2 * time.Millisecond):
			}
			n := 0
			for _, w := range f.recordedWrites() {
				if string(w) == "TX;" {
					n++
				}
			}
			for answered < n {
				s.observeTxStatus("0")
				answered++
			}
		}
	}()
	return func() { close(done) }
}

// lastWrite returns the most recent byte sequence written to the fake as a
// string, or "" if nothing has been written.
func lastWrite(f *fakeSerial) string {
	w := f.recordedWrites()
	if len(w) == 0 {
		return ""
	}
	return string(w[len(w)-1])
}

// awaitTuneState blocks until a tune-state event with the wanted Active value
// arrives on ch, or the timeout elapses. Other event kinds are skipped.
func awaitTuneState(t *testing.T, ch <-chan Event, want bool, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				t.Fatal("event channel closed before tune-state observed")
			}
			if evt.Name != EventTuneState {
				continue
			}
			p, ok := evt.Payload.(TuneStatePayload)
			if !ok {
				t.Fatalf("tune-state payload type = %T, want TuneStatePayload", evt.Payload)
			}
			if p.Active == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for tune-state active=%v", want)
		}
	}
}

func TestTuneSupported(t *testing.T) {
	ftdx10, ok := cat.Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal(`Lookup("yaesu-ftdx10") not found`)
	}
	if !TuneSupported(ftdx10) {
		t.Error("TuneSupported(ftdx10) = false; rigdef has set_mode/set_power/tx_on/tx_off")
	}
	ft710, ok := cat.Lookup("yaesu-ft710")
	if !ok {
		t.Fatal(`Lookup("yaesu-ft710") not found`)
	}
	// The FT-710 carries the full tune command set: set_mode + set_power (write
	// entries added 2026-06-05) and tx_on/tx_off (TX1;/TX0;, added 2026-06-06,
	// not Exposed — tune-controller-only). All four encode, so the dry-run
	// reports true and the SPA shows the Tune button on the FT-710.
	if !TuneSupported(ft710) {
		t.Error("TuneSupported(ft710) = false; rigdef has set_mode/set_power/tx_on/tx_off")
	}
}

// TestTuneSupported_RequiresEncodeableCommands covers review-finding L1: all
// four tune command NAMES present, but set_mode is not Exposed — so it can't
// encode. Name-presence (the old HasCommand check) would advertise tune:true;
// the encodeability dry-run correctly reports false, matching what StartTune
// would do at runtime.
func TestTuneSupported_RequiresEncodeableCommands(t *testing.T) {
	def := cat.RigDefinition{
		ID:         "test-broken-tune",
		Terminator: ";",
		Commands: []cat.Command{
			{Name: tuneModeCommand, Cmd: "MD0%s;", ValueMap: "MAINMODE"}, // present but NOT exposed
			{Name: tunePowerCommand, Cmd: "PC%s;", Pad: 3, Exposed: true},
			{Name: tuneTxOnCommand, Cmd: "TX1;"},
			{Name: tuneTxOffCommand, Cmd: "TX0;"},
		},
		States: []cat.State{
			{Prefix: "MD0", Markers: []cat.Marker{
				{Tag: "MAINMODE", Index: 0, Length: 1, ValueMappings: []cat.ValueMapping{
					{Key: "9", Value: tuneCarrierMode},
				}},
			}},
		},
	}
	// Precondition: every tune command name is present (so the old name-presence
	// check would have returned true).
	for _, name := range []string{tuneModeCommand, tunePowerCommand, tuneTxOnCommand, tuneTxOffCommand} {
		if !cat.HasCommand(def, name) {
			t.Fatalf("precondition: command %q should be present", name)
		}
	}
	if TuneSupported(def) {
		t.Error("TuneSupported = true with an un-exposed set_mode; want false (encodeability, not name-presence)")
	}
}

func TestResolveTunePower(t *testing.T) {
	cases := []struct {
		in        int
		want      int
		wantClamp bool
	}{
		{0, defaultTunePowerW, false},  // omitted → default 20
		{-5, defaultTunePowerW, false}, // negative → default (config rejects <0 anyway)
		{10, 10, false},                // honoured below ceiling
		{40, 40, false},                // at the ceiling
		{50, maxTunePowerW, true},      // above ceiling → clamped to 40
		{1000, maxTunePowerW, true},    // way above → clamped
	}
	for _, c := range cases {
		got, clamped := resolveTunePower(c.in)
		if got != c.want || clamped != c.wantClamp {
			t.Errorf("resolveTunePower(%d) = (%d,%v), want (%d,%v)", c.in, got, clamped, c.want, c.wantClamp)
		}
	}
}

func TestResolveTuneDuration(t *testing.T) {
	cases := []struct {
		inMs      int
		want      time.Duration
		wantClamp bool
	}{
		{0, defaultTuneDuration, false},  // omitted → default 15s
		{-1, defaultTuneDuration, false}, // negative → default
		{5000, 5 * time.Second, false},   // honoured below ceiling
		{30000, maxTuneDuration, false},  // at the ceiling (30s)
		{45000, maxTuneDuration, true},   // above → clamped to 30s
	}
	for _, c := range cases {
		got, clamped := resolveTuneDuration(c.inMs)
		if got != c.want || clamped != c.wantClamp {
			t.Errorf("resolveTuneDuration(%d) = (%v,%v), want (%v,%v)", c.inMs, got, clamped, c.want, c.wantClamp)
		}
	}
}

func TestResolveTuneRestoreSettle(t *testing.T) {
	cases := []struct {
		inMs      int
		want      time.Duration
		wantClamp bool
	}{
		{0, defaultTuneRestoreSettle, false},  // omitted → default 150ms
		{-1, defaultTuneRestoreSettle, false}, // negative → default
		{300, 300 * time.Millisecond, false},  // honoured below ceiling
		{2000, maxTuneRestoreSettle, false},   // at the ceiling (2s)
		{5000, maxTuneRestoreSettle, true},    // above → clamped to 2s
	}
	for _, c := range cases {
		got, clamped := resolveTuneRestoreSettle(c.inMs)
		if got != c.want || clamped != c.wantClamp {
			t.Errorf("resolveTuneRestoreSettle(%d) = (%v,%v), want (%v,%v)", c.inMs, got, clamped, c.want, c.wantClamp)
		}
	}
}

func TestEncodeTuneOn(t *testing.T) {
	def, _ := cat.Lookup("yaesu-ftdx10")
	s := &Service{tunePowerW: 20}
	on, err := s.encodeTuneOn(def)
	if err != nil {
		t.Fatalf("encodeTuneOn: %v", err)
	}
	// RTTY-U → 9 (MD09;), 20 W → PC020;, key TX1; — mode, power, key in order.
	if got, want := string(on), "MD09;PC020;TX1;"; got != want {
		t.Errorf("encodeTuneOn = %q, want %q", got, want)
	}
}

// The unkey command is sent on its own line (TX0;), separately from the
// restore, so the carrier drops before the TX→RX settle (task #270).
func TestEncodeTuneUnkey(t *testing.T) {
	def, _ := cat.Lookup("yaesu-ftdx10")
	got, err := encodeTuneUnkey(def)
	if err != nil {
		t.Fatalf("encodeTuneUnkey: %v", err)
	}
	if string(got) != "TX0;" {
		t.Errorf("encodeTuneUnkey = %q, want %q", string(got), "TX0;")
	}
}

// A rigdef that can key TX but has no tx_off command must NOT yield an unkey
// line — silently omitting the unkey would strand a keyed carrier (review
// 2026-06-04 H3). encodeTuneUnkey returns an error so StartTune's pre-key gate
// refuses and releaseTune stays armed + loud.
func TestEncodeTuneUnkey_RequiresTxOff(t *testing.T) {
	def := cat.RigDefinition{
		ID:         "test-no-txoff",
		Terminator: ";",
		Commands: []cat.Command{
			{Name: tuneTxOnCommand, Cmd: "TX1;"},
			// deliberately no tx_off
		},
	}
	if _, err := encodeTuneUnkey(def); err == nil {
		t.Fatal("encodeTuneUnkey without tx_off = nil error, want error")
	}
}

// encodeTuneRestore is the post-unkey best-effort half: restore power, then
// restore mode (sent after the TX→RX settle, task #270). No tx_off here.
func TestEncodeTuneRestore(t *testing.T) {
	def, _ := cat.Lookup("yaesu-ftdx10")
	cases := []struct {
		mode  string
		power int
		want  string
	}{
		{"USB", 100, "PC100;MD02;"}, // restore power, restore mode
		{"CW-U", 5, "PC005;MD03;"},  // 5 W → PC005;, CW-U → 3
		{"", 0, ""},                 // nothing known to restore → empty
	}
	for _, c := range cases {
		if got := string(encodeTuneRestore(def, c.mode, c.power)); got != c.want {
			t.Errorf("encodeTuneRestore(%q,%d) = %q, want %q", c.mode, c.power, got, c.want)
		}
	}
}

func TestStartTune_Happy(t *testing.T) {
	s, f := tuneTestService(t)
	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	if got, want := lastWrite(f), "MD09;PC020;TX1;"; got != want {
		t.Errorf("tune-on write = %q, want %q", got, want)
	}
	s.mu.Lock()
	active := s.tuneActive
	s.mu.Unlock()
	if !active {
		t.Error("tuneActive = false after StartTune")
	}
	awaitTuneState(t, ch, true, time.Second)
}

func TestStartTune_RefusesUnknownState(t *testing.T) {
	s, f := tuneTestService(t)
	s.lastMode = "" // never observed a mode push
	s.lastPower = 0

	err := s.StartTune(context.Background())
	if !errors.Is(err, ErrTuneStateUnknown) {
		t.Fatalf("StartTune err = %v, want ErrTuneStateUnknown", err)
	}
	if len(f.recordedWrites()) != 0 {
		t.Errorf("a tune-on line was written despite unknown state: %q", lastWrite(f))
	}
}

// TestStartTune_RefusesUnverifiedIdentity covers the H2 gate on the TX path:
// transmitting into a rig whose identity isn't confirmed is the most dangerous
// wrong-rig case, so StartTune must refuse and key nothing.
func TestStartTune_RefusesUnverifiedIdentity(t *testing.T) {
	s, f := tuneTestService(t)
	s.mu.Lock()
	s.identityConfirmed = false
	s.mu.Unlock()

	err := s.StartTune(context.Background())
	if !errors.Is(err, ErrRigIdentityUnverified) {
		t.Fatalf("StartTune with unverified identity = %v, want ErrRigIdentityUnverified", err)
	}
	if n := len(f.recordedWrites()); n != 0 {
		t.Errorf("TX was keyed despite unverified identity (%d writes)", n)
	}
}

func TestStartTune_RefusesNotConnected(t *testing.T) {
	s, _ := tuneTestService(t)
	s.activeClient = nil

	err := s.StartTune(context.Background())
	if !errors.Is(err, ErrRigNotConnected) {
		t.Fatalf("StartTune err = %v, want ErrRigNotConnected", err)
	}
}

// TestStartTune_FailedKeyWriteEntersUncertain (ADR 0051, was ...ArmsStranded):
// a key write can return an error yet have reached the rig (CI-V no-ACK, or a
// watchdog-closed port that flushed the frame first). The tune state rolls
// back BUT the service enters txUncertain and runs the confirmation cycle —
// a positive RX answer clears it, silence escalates to the tx-alarm.
func TestStartTune_FailedKeyWriteEntersUncertain(t *testing.T) {
	s, f := tuneTestService(t)
	_ = f.Close() // key write now returns ErrClosed

	if err := s.StartTune(context.Background()); err == nil {
		t.Fatal("StartTune with a failing key write = nil, want error")
	}

	s.mu.Lock()
	active := s.tuneActive
	uncertain := s.txUncertain
	timer := s.tuneTimer
	s.mu.Unlock()
	if active {
		t.Error("tuneActive = true after a failed key write; want rolled back to false")
	}
	if !uncertain {
		t.Error("txUncertain = false after a failed key write; a possibly-keyed carrier has no confirmation cycle")
	}
	if timer != nil {
		t.Error("tuneTimer not cleared after a failed key write")
	}
}

func TestStartTune_SingleFlight(t *testing.T) {
	s, f := tuneTestService(t)

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("first StartTune: %v", err)
	}
	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("second StartTune (should no-op): %v", err)
	}
	// Exactly one tune-on line — the second call is a single-flight no-op.
	writes := f.recordedWrites()
	if len(writes) != 1 {
		t.Errorf("got %d writes, want 1 (single-flight); writes=%q", len(writes), writes)
	}
}

func TestStopTune_RestoresAndUnkeys(t *testing.T) {
	s, f := tuneTestService(t)
	t.Cleanup(answerTxStatusQueries(s, f)) // healthy rig: confirm-gate passes
	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	awaitTuneState(t, ch, true, time.Second)

	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("StopTune: %v", err)
	}
	// Two separate writes (task #270): the safety-critical unkey on its own,
	// then — after the TX→RX settle — the best-effort power + mode restore.
	// The mode (MD02;) must NOT ride in the same line as the unkey, or the rig
	// drops it during the transition tail.
	writes := f.recordedWrites()
	// tune-on, unkey, the ADR 0051 status query, then the restore.
	if len(writes) < 4 {
		t.Fatalf("got %d writes, want 4 (tune-on, unkey, tx-status query, restore); writes=%q", len(writes), writes)
	}
	if got := string(writes[len(writes)-3]); got != "TX0;" {
		t.Errorf("unkey write = %q, want %q (tx_off alone)", got, "TX0;")
	}
	if got := string(writes[len(writes)-2]); got != "TX;" {
		t.Errorf("post-unkey write = %q, want %q (ADR 0051 confirmation query)", got, "TX;")
	}
	if got := string(writes[len(writes)-1]); got != "PC100;MD02;" {
		t.Errorf("restore write = %q, want %q (power + mode after settle)", got, "PC100;MD02;")
	}
	s.mu.Lock()
	active := s.tuneActive
	s.mu.Unlock()
	if active {
		t.Error("tuneActive = true after StopTune")
	}
	awaitTuneState(t, ch, false, time.Second)
}

// TestReleaseTune_ConcurrentStopsReleaseOnce (review 2026-06-16 #3): two stops
// racing (operator stop vs the auto-off backstop, or a double-click) must release
// exactly once. keyMu serialises the release bodies and the first clears
// tuneActive, so the second observes it inactive and no-ops — exactly one tx_off
// reaches the wire, never two (which could fire a stale unkey/restore over a
// later transmission).
func TestReleaseTune_ConcurrentStopsReleaseOnce(t *testing.T) {
	s, f := tuneTestService(t)
	t.Cleanup(answerTxStatusQueries(s, f)) // healthy rig: confirm-gate passes
	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.StopTune(context.Background()) }()
	}
	wg.Wait()

	s.mu.Lock()
	active := s.tuneActive
	s.mu.Unlock()
	if active {
		t.Error("tuneActive should be false after concurrent stops")
	}
	n := 0
	for _, w := range f.recordedWrites() {
		if string(w) == "TX0;" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("tx_off written %d times, want exactly 1 (releases serialised)", n)
	}
}

// TestKeyRelease_ConcurrentStartStopNoDeadlock hammers StartTune/StopTune from
// several goroutines to exercise keyMu under the race detector (no deadlock, no
// data race). A final stop must leave the carrier definitively down.
func TestKeyRelease_ConcurrentStartStopNoDeadlock(t *testing.T) {
	s, f := tuneTestService(t)
	t.Cleanup(answerTxStatusQueries(s, f)) // healthy rig: confirm-gate passes
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_ = s.StartTune(context.Background())
			} else {
				_ = s.StopTune(context.Background())
			}
		}(i)
	}
	wg.Wait()
	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("final StopTune: %v", err)
	}
	s.mu.Lock()
	active := s.tuneActive
	s.mu.Unlock()
	if active {
		t.Error("tuneActive should be false after a final StopTune")
	}
}

func TestStopTune_IdempotentWhenIdle(t *testing.T) {
	s, f := tuneTestService(t)
	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("StopTune (idle): %v", err)
	}
	if len(f.recordedWrites()) != 0 {
		t.Errorf("StopTune wrote %q while idle; want no-op", lastWrite(f))
	}
}

// TestStopTune_CtxCancelDuringSettleStillRestores is the F3 regression: the
// post-unkey mode/power restore is DETACHED from the caller's ctx, so a cancelled
// request — e.g. the browser disconnecting during the settle — must NOT skip the
// restore and strand the rig in RTTY / tune-power. Both writes go out: the unkey
// (TX0;) and, after the settle, the restore (PC…;MD…;). (Before F3 the restore was
// skipped on a cancelled ctx, task #270 — that was the bug.)
func TestStopTune_CtxCancelDuringSettleStillRestores(t *testing.T) {
	s, f := tuneTestService(t)
	s.tuneRestoreSettle = 5 * time.Millisecond
	t.Cleanup(answerTxStatusQueries(s, f)) // healthy rig: confirm-gate passes
	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	awaitTuneState(t, ch, true, time.Second)

	// An already-cancelled context stands in for a request that died before the
	// restore ran. Under the old behaviour the settle bailed on ctx.Done and
	// skipped the restore; now step 2 is detached and completes regardless.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.StopTune(ctx); err != nil {
		t.Fatalf("StopTune: %v", err)
	}

	writes := f.recordedWrites()
	if len(writes) < 3 {
		t.Fatalf("want unkey + query + restore writes, got %d: %q", len(writes), writes)
	}
	if got := string(writes[len(writes)-3]); got != "TX0;" {
		t.Errorf("unkey write = %q, want %q", got, "TX0;")
	}
	if got := string(writes[len(writes)-1]); got != "PC100;MD02;" {
		t.Errorf("restore skipped on a cancelled ctx = %q, want %q (F3: restore is detached)", got, "PC100;MD02;")
	}

	s.mu.Lock()
	active := s.tuneActive
	s.mu.Unlock()
	if active {
		t.Error("tuneActive = true after stop; carrier wrongly reported up")
	}
	awaitTuneState(t, ch, false, time.Second)
}

// TestStopTune_UnconfirmedSkipsRestore pins the 2026-07-19 review P1 gate:
// when the rig never answers the TX-status query after the unkey, the release
// must NOT write the power/mode restore — a rig that missed TX0 but still
// receives frames would get PC full power while KEYED (the amp-damage
// scenario). The alarm stands, uncertainty is retained, and the carrier is
// still reported down (the ADR 0051 UI/uncertainty split).
func TestStopTune_UnconfirmedSkipsRestore(t *testing.T) {
	prev := txConfirmTimeout
	txConfirmTimeout = 30 * time.Millisecond
	defer func() { txConfirmTimeout = prev }()

	s, f := tuneTestService(t)
	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("StopTune: %v", err)
	}

	// The unkey and the status query went out; the restore must NOT have.
	if w := lastWrite(f); w != "TX;" {
		t.Fatalf("last write = %q, want the TX; query (restore must be skipped while unconfirmed)", w)
	}
	for _, w := range f.recordedWrites() {
		if string(w) == "PC100;MD02;" {
			t.Fatal("power/mode restore written without positive RX confirmation")
		}
	}

	s.mu.Lock()
	alarmed, uncertain, active := s.txAlarmActive, s.txUncertain, s.tuneActive
	s.mu.Unlock()
	if !alarmed || !uncertain {
		t.Fatalf("alarmed=%v uncertain=%v after unconfirmed release, want both true", alarmed, uncertain)
	}
	if active {
		t.Error("tuneActive should clear even on the skip path (UI reports probably-down)")
	}
}

// TestStopTune_StillKeyedAnswerSkipsRestore: the rig ANSWERS the query with
// "1" (CAT TX still keyed — the unkey definitively failed). Same gate, faster
// resolution: the restore is skipped and the still-keyed alarm stands.
func TestStopTune_StillKeyedAnswerSkipsRestore(t *testing.T) {
	s, f := tuneTestService(t)
	// Answer every status query with "still keyed".
	done := make(chan struct{})
	defer close(done)
	go func() {
		answered := 0
		for {
			select {
			case <-done:
				return
			case <-time.After(2 * time.Millisecond):
			}
			n := 0
			for _, w := range f.recordedWrites() {
				if string(w) == "TX;" {
					n++
				}
			}
			for answered < n {
				s.observeTxStatus("1")
				answered++
			}
		}
	}()

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("StopTune: %v", err)
	}
	for _, w := range f.recordedWrites() {
		if string(w) == "PC100;MD02;" {
			t.Fatal("restore written although the rig answered TX1 (still keyed)")
		}
	}
	s.mu.Lock()
	alarmed := s.txAlarmActive
	s.mu.Unlock()
	if !alarmed {
		t.Fatal("tx-alarm must stand after a still-keyed answer")
	}
}

func TestTuneAutoOff(t *testing.T) {
	s, f := tuneTestService(t)
	s.tuneMaxDuration = 30 * time.Millisecond
	t.Cleanup(answerTxStatusQueries(s, f)) // healthy rig: confirm-gate passes
	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	// The hard backstop must drop the carrier on its own.
	awaitTuneState(t, ch, false, 2*time.Second)
	s.mu.Lock()
	active := s.tuneActive
	s.mu.Unlock()
	if active {
		t.Error("tuneActive = true after auto-off")
	}
	// Auto-off uses the same release: unkey alone, the ADR 0051 status query,
	// then the restore.
	writes := f.recordedWrites()
	if len(writes) < 4 {
		t.Fatalf("got %d writes, want 4 (tune-on, unkey, query, restore); writes=%q", len(writes), writes)
	}
	if got := string(writes[len(writes)-3]); got != "TX0;" {
		t.Errorf("auto-off unkey write = %q, want %q", got, "TX0;")
	}
	if got := string(writes[len(writes)-1]); got != "PC100;MD02;" {
		t.Errorf("auto-off restore write = %q, want %q", got, "PC100;MD02;")
	}
}

func TestClearTuneOnDisconnect(t *testing.T) {
	s, _ := tuneTestService(t)
	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	awaitTuneState(t, ch, true, time.Second)

	s.clearTuneOnDisconnect()

	s.mu.Lock()
	active := s.tuneActive
	mode := s.lastMode
	power := s.lastPower
	s.mu.Unlock()
	if active {
		t.Error("tuneActive = true after clearTuneOnDisconnect")
	}
	// Stale snapshot must be forgotten so a previous-session value can't seed
	// a later restore.
	if mode != "" || power != 0 {
		t.Errorf("snapshot not cleared: mode=%q power=%d", mode, power)
	}
	awaitTuneState(t, ch, false, time.Second)
}

func TestCaptureTuneSnapshot_FrozenDuringTune(t *testing.T) {
	s, _ := tuneTestService(t)

	// Active tune: the rig's own RTTY/tune-power pushes must NOT overwrite the
	// restore snapshot.
	s.tuneActive = true
	s.captureTuneSnapshot(RigStatePayload{Mode: "RTTY-U", Power: 20})
	s.mu.Lock()
	mode, power := s.lastMode, s.lastPower
	s.mu.Unlock()
	if mode != "USB" || power != 100 {
		t.Errorf("snapshot mutated during tune: mode=%q power=%d, want USB/100", mode, power)
	}

	// Idle: pushes update the snapshot normally.
	s.tuneActive = false
	s.captureTuneSnapshot(RigStatePayload{Mode: "CW-U", Power: 50})
	s.mu.Lock()
	mode, power = s.lastMode, s.lastPower
	s.mu.Unlock()
	if mode != "CW-U" || power != 50 {
		t.Errorf("snapshot not updated while idle: mode=%q power=%d, want CW-U/50", mode, power)
	}
}

// dialHz round-trips CurrentDialMHz back to integer Hz for exact comparison
// (avoids float-equality fuzz on values like 21.074).
func dialHz(mhz float64) int64 { return int64(mhz*1_000_000 + 0.5) }

// TestCurrentDialMHz resolves the selected VFO's frequency for FT8 logging, merging
// partial rig-state pushes — the source of truth is the rig (the bridge), not the
// SPA's stale start-time snapshot.
func TestCurrentDialMHz(t *testing.T) {
	s, _ := tuneTestService(t)

	// Unknown before any frequency is decoded — the FT8 sink must fall back, never
	// log a fabricated 0 or a placeholder.
	if _, ok := s.CurrentDialMHz(); ok {
		t.Fatal("CurrentDialMHz should be unknown before any frequency is decoded")
	}

	// Yaesu reports FA/FB/VS separately. VFO-A alone is not authoritative:
	// the rig may actually be operating on B and its SELECT reply is still in
	// flight.
	s.captureDialFreq(RigStatePayload{VfoA: 21_074_000})
	if _, ok := s.CurrentDialMHz(); ok {
		t.Fatal("VFO-A frequency without a fresh selection must remain unknown")
	}
	s.captureDialFreq(RigStatePayload{SelectedVfo: "A"})
	if mhz, ok := s.CurrentDialMHz(); !ok || dialHz(mhz) != 21_074_000 {
		t.Fatalf("VFO-A dial = %v ok=%v, want 21.074 true", mhz, ok)
	}

	// Partial merge: a SelectedVfo=B push (no freq) then a VfoB push → selected freq
	// follows VFO-B, while the earlier VFO-A value is retained.
	s.captureDialFreq(RigStatePayload{SelectedVfo: "B"})
	s.captureDialFreq(RigStatePayload{VfoB: 14_139_500})
	if mhz, ok := s.CurrentDialMHz(); !ok || dialHz(mhz) != 14_139_500 {
		t.Fatalf("selected VFO-B dial = %v ok=%v, want 14.1395 true", mhz, ok)
	}
	s.captureDialFreq(RigStatePayload{SelectedVfo: "A"})
	if mhz, ok := s.CurrentDialMHz(); !ok || dialHz(mhz) != 21_074_000 {
		t.Fatalf("back to VFO-A dial = %v ok=%v, want 21.074 true", mhz, ok)
	}
}

// TestCurrentDialMHz_SelectedVfoUnknown pins the per-VFO knownness rule
// (2026-07-19 review P2): the SELECTED VFO must itself have been decoded —
// falling back to the other VFO's frequency would log an FT8 QSO on the wrong
// band, which is worse than reporting unknown.
func TestCurrentDialMHz_SelectedVfoUnknown(t *testing.T) {
	s, _ := tuneTestService(t)

	// A known, B selected but never decoded → unknown (NOT VFO-A's value).
	s.captureDialFreq(RigStatePayload{VfoA: 14_074_000, SelectedVfo: "B"})
	if mhz, ok := s.CurrentDialMHz(); ok {
		t.Fatalf("CurrentDialMHz = (%v, true) with selected VFO-B undecoded; want ok=false", mhz)
	}

	// B arrives → the selected frequency is B's.
	s.captureDialFreq(RigStatePayload{VfoB: 7_074_000})
	if mhz, ok := s.CurrentDialMHz(); !ok || dialHz(mhz) != 7_074_000 {
		t.Fatalf("selected VFO-B dial = %v ok=%v, want 7.074 true", mhz, ok)
	}
}

// A passive CAT recovery marks the connection live on its first rig reply, but
// a Yaesu READ snapshot returns FA, FB, and VS as separate frames. Until VS
// arrives, neither frequency may be exposed as the selected operating dial.
func TestCurrentDialMHz_PassiveRecoveryWaitsForSelectedVFO(t *testing.T) {
	s, _ := tuneTestService(t)
	s.captureDialFreq(RigStatePayload{
		VfoA:        14_074_000,
		VfoB:        7_074_000,
		SelectedVfo: "B",
	})
	if mhz, ok := s.CurrentDialMHz(); !ok || dialHz(mhz) != 7_074_000 {
		t.Fatalf("precondition dial = %v ok=%v, want selected VFO-B", mhz, ok)
	}

	// Passive liveness loss invalidates the snapshot; the first recovered frame
	// makes CAT writable again before the later SELECT reply arrives.
	s.noDataStrikes.Store(noDataStrikeLimit)
	s.mu.Lock()
	s.clearRigSnapshotLocked()
	s.mu.Unlock()
	s.noDataStrikes.Store(0)

	s.captureDialFreq(RigStatePayload{VfoA: 21_074_000})
	if mhz, ok := s.CurrentDialMHz(); ok {
		t.Fatalf("recovered VFO-A before SELECT = %v,true; want unknown", mhz)
	}
	s.captureDialFreq(RigStatePayload{VfoB: 18_100_000})
	if mhz, ok := s.CurrentDialMHz(); ok {
		t.Fatalf("both recovered frequencies before SELECT = %v,true; want unknown", mhz)
	}
	s.captureDialFreq(RigStatePayload{SelectedVfo: "B"})
	if mhz, ok := s.CurrentDialMHz(); !ok || dialHz(mhz) != 18_100_000 {
		t.Fatalf("dial after recovered SELECT=B = %v ok=%v, want 18.1 true", mhz, ok)
	}
}

// The IC-7300 rigdef has no SELECT state: its operating frequency is modelled
// as VFO-A. Explicit-selection knownness must not make that dial permanently
// unknown after a passive snapshot reset.
func TestCurrentDialMHz_ImplicitVFOARecoversWithoutSelect(t *testing.T) {
	s, fake := newCIVPipelineTestService(t)
	s.mu.Lock()
	s.activeClient = fake
	s.identityConfirmed = true
	s.clearRigSnapshotLocked()
	s.mu.Unlock()

	s.captureDialFreq(RigStatePayload{VfoA: 18_100_000})
	if mhz, ok := s.CurrentDialMHz(); !ok || dialHz(mhz) != 18_100_000 {
		t.Fatalf("implicit VFO-A dial after recovery = %v ok=%v, want 18.1 true", mhz, ok)
	}
}

// TestClearTuneOnDisconnect_ForgetsDial: a frequency from a previous rig session must
// not seed a logged QSO after reconnect.
func TestClearTuneOnDisconnect_ForgetsDial(t *testing.T) {
	s, _ := tuneTestService(t)
	s.captureDialFreq(RigStatePayload{VfoA: 21_074_000, SelectedVfo: "A"})
	if _, ok := s.CurrentDialMHz(); !ok {
		t.Fatal("precondition: dial should be known after a frequency push")
	}
	s.clearTuneOnDisconnect()
	if _, ok := s.CurrentDialMHz(); ok {
		t.Fatal("dial must be forgotten on disconnect")
	}
}

// TestUnkeyOnTeardown guards F1 (review 2026-07-02): the daemon's OWN shutdown
// (ctx cancel) mid-tune / mid-FT8-TX must unkey the rig before the port closes —
// a healthy CAT-keyed rig otherwise keeps transmitting after the daemon exits.
func TestUnkeyOnTeardown(t *testing.T) {
	def, ok := cat.Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal("yaesu-ftdx10 rigdef not found")
	}

	t.Run("writes tx_off when a tune is active", func(t *testing.T) {
		s, f := tuneTestService(t)
		s.mu.Lock()
		s.tuneActive = true
		s.mu.Unlock()
		before := len(f.recordedWrites())
		s.unkeyOnTeardown(def, f, false)
		sawOff := false
		for _, w := range f.recordedWrites()[before:] {
			if string(w) == "TX0;" {
				sawOff = true
			}
		}
		if !sawOff {
			t.Fatalf("teardown must write tx_off (TX0;) while a tune is keyed; new writes = %v",
				f.recordedWrites()[before:])
		}
	})

	t.Run("writes tx_off when FT8 TX is active", func(t *testing.T) {
		s, f := tuneTestService(t)
		s.mu.Lock()
		s.ft8TxActive = true
		s.mu.Unlock()
		s.unkeyOnTeardown(def, f, false)
		if w := lastWrite(f); w != "TX0;" {
			t.Fatalf("teardown must unkey an active FT8 TX; last write = %q, want TX0;", w)
		}
	})

	t.Run("no write when PTT isn't up", func(t *testing.T) {
		s, f := tuneTestService(t)
		before := len(f.recordedWrites())
		s.unkeyOnTeardown(def, f, false)
		if n := len(f.recordedWrites()); n != before {
			t.Fatalf("no unkey write expected when not keyed; got %d new writes", n-before)
		}
	})
}

// TestDefensiveUnkeyAndTeardownAlarm covers the ADR 0051 replacements for the
// stranded-flag machinery: a teardown that cannot unkey a keyed rig raises the
// persistent tx-alarm (the SPA holds the banner across the restart), and every
// identity-confirmed connection fires ONE unconditional defensive tx_off —
// stateless, so it needs no memory of how the prior life died.
func TestDefensiveUnkeyAndTeardownAlarm(t *testing.T) {
	def, ok := cat.Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal("yaesu-ftdx10 rigdef not found")
	}

	t.Run("teardown raises the tx-alarm when the unkey write fails (dead port)", func(t *testing.T) {
		s, f := tuneTestService(t)
		s.mu.Lock()
		s.tuneActive = true
		s.mu.Unlock()
		_ = f.Close() // dead port: WriteCommandBytes now returns ErrClosed
		s.unkeyOnTeardown(def, f, false)
		s.mu.Lock()
		alarmed := s.txAlarmActive
		uncertain := s.txUncertain
		s.mu.Unlock()
		if !alarmed || !uncertain {
			t.Fatalf("failed teardown unkey: alarmed=%v uncertain=%v, want both true (rig may still be keyed)", alarmed, uncertain)
		}
	})

	t.Run("a successful teardown unkey does not alarm", func(t *testing.T) {
		s, f := tuneTestService(t)
		s.mu.Lock()
		s.ft8TxActive = true
		s.mu.Unlock()
		s.unkeyOnTeardown(def, f, false) // healthy shutdown, healthy port → write succeeds
		s.mu.Lock()
		alarmed := s.txAlarmActive
		uncertain := s.txUncertain
		s.mu.Unlock()
		if alarmed {
			t.Fatal("a successful teardown unkey must not raise the alarm")
		}
		if !uncertain {
			t.Fatal("teardown write-accepted is NOT confirmed — uncertainty must be retained (8bd88c1b review)")
		}
	})

	t.Run("a FAULT-shaped teardown alarms even when the write is accepted", func(t *testing.T) {
		// 2026-07-19 review P1: an error-shaped pipeline exit means the link
		// just proved untrustworthy — a write the OS accepted may never have
		// reached the rig (the 2026-07-18 stalled-endpoint incident), and the
		// supervisor may never reconnect to run the defensive recovery. The
		// quiet-uncertain trade applies only to healthy shutdowns.
		s, f := tuneTestService(t)
		s.mu.Lock()
		s.tuneActive = true
		s.mu.Unlock()
		s.unkeyOnTeardown(def, f, true) // healthy port, but fault-shaped exit
		if w := lastWrite(f); w != "TX0;" {
			t.Fatalf("fault teardown must still write the unkey; last write = %q", w)
		}
		s.mu.Lock()
		alarmed := s.txAlarmActive
		uncertain := s.txUncertain
		s.mu.Unlock()
		if !alarmed || !uncertain {
			t.Fatalf("fault teardown with accepted write: alarmed=%v uncertain=%v, want both true", alarmed, uncertain)
		}
	})

	t.Run("defensive recovery fires unconditionally and demands confirmation", func(t *testing.T) {
		// 8bd88c1b review: a FRESH service (no seeded alarm/uncertainty —
		// the real restart shape) must still run the full cycle: TX0 +
		// status query out, TX blocked until the rig answers RX.
		s, f := tuneTestService(t)
		s.beginDefensiveRecovery(def, f)
		// The wire phase runs on a goroutine (the readLoop must stay free to
		// deliver CI-V ACKs) — poll for both frames.
		sawBoth := func() bool {
			var sawTxOff, sawQuery bool
			for _, w := range f.recordedWrites() {
				switch string(w) {
				case "TX0;":
					sawTxOff = true
				case "TX;":
					sawQuery = true
				}
			}
			return sawTxOff && sawQuery
		}
		deadline := time.Now().Add(time.Second)
		for !sawBoth() {
			if time.Now().After(deadline) {
				t.Fatalf("recovery writes = %q, want TX0; AND the TX; query", f.recordedWrites())
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !s.TxUncertain() {
			t.Fatal("recovery must hold TX blocked until the rig confirms")
		}
		if !s.identityOK() {
			t.Fatal("recovery must have unlocked identity")
		}
		s.observeTxStatus("0")
		if s.TxUncertain() {
			t.Fatal("a positive RX answer must clear the recovery uncertainty")
		}
	})

	t.Run("no key can race into the recovery window", func(t *testing.T) {
		// 8bd88c1b review: uncertainty is set BEFORE identity unlocks, so a
		// key attempted mid-recovery is refused — the old busy-skip (which
		// could silently drop the recovery) is unreachable by ordering.
		s, f := tuneTestService(t)
		s.setIdentityConfirmed(false)
		s.lastMode = "USB"
		s.lastPower = 35
		s.beginDefensiveRecovery(def, f)
		// Recovery is in flight (unconfirmed): a key must be refused. The
		// pre-block is synchronous, so this refusal is deterministic.
		if err := s.StartTune(context.Background()); !errors.Is(err, ErrTxUncertain) {
			t.Fatalf("StartTune during recovery = %v, want ErrTxUncertain", err)
		}
		// Wait for the async wire phase (its beginTxConfirm re-arms
		// uncertainty) BEFORE answering, or the answer can race the query.
		deadline := time.Now().Add(time.Second)
		for {
			ws := f.recordedWrites()
			if len(ws) > 0 && string(ws[len(ws)-1]) == "TX;" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("status query never written; writes=%q", ws)
			}
			time.Sleep(5 * time.Millisecond)
		}
		s.observeTxStatus("0")
		if err := s.StartTune(context.Background()); err != nil {
			t.Fatalf("StartTune after confirmation: %v", err)
		}
	})

	t.Run("a standing alarm clears via the confirmation cycle after the defensive unkey", func(t *testing.T) {
		s, f := tuneTestService(t)
		s.raiseTxAlarm(TxAlarmTeardownUnconfirmed) // prior life left the alarm up
		s.beginDefensiveRecovery(def, f)           // writes TX0 + starts confirmation
		deadline := time.Now().Add(time.Second)
		for {
			ws := f.recordedWrites()
			if len(ws) > 0 && string(ws[len(ws)-1]) == "TX;" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("status query never written; writes=%q", ws)
			}
			time.Sleep(5 * time.Millisecond)
		}
		s.observeTxStatus("0")
		s.mu.Lock()
		alarmed := s.txAlarmActive
		uncertain := s.txUncertain
		s.mu.Unlock()
		if alarmed || uncertain {
			t.Fatalf("after a positive RX answer: alarmed=%v uncertain=%v, want both false", alarmed, uncertain)
		}
	})
}
