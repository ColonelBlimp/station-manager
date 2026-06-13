package bridge

import (
	"bytes"
	"context"
	stderr "errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/serial"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// errOpenFailed is the synthetic error TestPipeline_OpenFailureExitsCleanly
// returns from a stub openClient to simulate a failed serial.Open.
var errOpenFailed = stderr.New("test: synthetic open failure")

// newPipelineTestService builds a Service wired against a fakeSerial
// for hardware-free pipeline tests. Returns the service and the fake
// so the test can feed lines / inspect writes / Close to simulate
// rig disconnect.
func newPipelineTestService(t *testing.T) (*Service, *fakeSerial) {
	t.Helper()
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	fake := installFakeSerial(s)
	return s, fake
}

// TestPipeline_SendsINIT covers the contract that Start kicks the
// rig into AUTO mode by sending the rigdef's INIT command before
// reading. Without this, modern rigs never push state and the SPA
// would see an empty stream.
func TestPipeline_SendsINIT(t *testing.T) {
	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	// Spin briefly until the pipeline's WriteCommandBytes has fired.
	// The race-detector hammer would catch any leak here; we just
	// need to give the goroutine a moment to do its first write.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if writes := fake.recordedWrites(); len(writes) > 0 {
			// FT-710 INIT is "AI1;" (M3a.3 onwards — see
			// internal/cat/rigs/yaesu-ft710.json). INIT arms AUTO
			// push mode; the identity + state snapshot is fetched
			// separately via READ on each SSE-open.
			if !bytes.Equal(writes[0], []byte("AI1;")) {
				t.Errorf("first write = %q, want %q", writes[0], "AI1;")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("pipeline did not send INIT within 1s")
}

// TestPipeline_DecodesIdentityPush covers the wire path from a rig
// push line through cat.Decode through mapStatusToPayload to the
// hub. The first line a real rig sends after INIT is its ID
// response (e.g. "ID0800" for FT-710); the SPA expects this as the
// initial RigIdentity field.
func TestPipeline_DecodesIdentityPush(t *testing.T) {
	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	// FT-710 ID code 0800 maps to "FT-710" via the rigdef's
	// IDENTITY value-mapping.
	fake.feedLine([]byte("ID0800"))

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed before identity event")
		}
		if evt.Name != EventRigState {
			t.Errorf("evt.Name = %q, want %q", evt.Name, EventRigState)
		}
		p, ok := evt.Payload.(RigStatePayload)
		if !ok {
			t.Fatalf("payload type = %T, want RigStatePayload", evt.Payload)
		}
		if p.RigIdentity != "FT-710" {
			t.Errorf("RigIdentity = %q, want %q", p.RigIdentity, "FT-710")
		}
	case <-time.After(time.Second):
		t.Fatal("no identity event within 1s")
	}
}

// TestClassifyIdentity pins the H2 verification decision exhaustively —
// including the mismatch case, which can't be produced through the shipped
// rigdefs (neither maps an ID code to a model other than its own).
func TestClassifyIdentity(t *testing.T) {
	cases := []struct {
		decoded, model string
		want           identityResult
	}{
		{"FT-710", "FT-710", identityConfirmed},
		{"FTdx10", "FTdx10", identityConfirmed},
		{"", "FT-710", identityUnrecognised},   // unmapped ID code
		{"FTdx10", "FT-710", identityMismatch}, // wired the wrong Yaesu
		{"FT-710", "FTdx10", identityMismatch}, // the other way round
	}
	for _, c := range cases {
		if got := classifyIdentity(c.decoded, c.model); got != c.want {
			t.Errorf("classifyIdentity(%q,%q) = %d, want %d", c.decoded, c.model, got, c.want)
		}
	}
}

// TestReadLoop_ConfirmsIdentityOnMatch covers the H2 confirm path: when the rig
// identifies as the configured driver, the write gate unlocks (identityOK
// flips true) so SendCommands / StartTune become allowed.
func TestReadLoop_ConfirmsIdentityOnMatch(t *testing.T) {
	s, fake := newPipelineTestService(t) // FT-710
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	if s.identityOK() {
		t.Fatal("identity confirmed before any rig push")
	}
	fake.feedLine([]byte("ID0800")) // → "FT-710" matches the configured model

	deadline := time.Now().Add(time.Second)
	for !s.identityOK() {
		if time.Now().After(deadline) {
			t.Fatal("identity not confirmed within 1s of a matching ID push")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestReadLoop_BlocksOnUnrecognisedIdentity covers the H2 unrecognised path: a
// rig that answers with an ID code the rigdef doesn't map publishes a
// bridge-error and the write gate stays locked (identityOK false), but state
// display keeps working (the pipeline doesn't halt).
func TestReadLoop_BlocksOnUnrecognisedIdentity(t *testing.T) {
	s, fake := newPipelineTestService(t) // FT-710
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	fake.feedLine([]byte("ID9999")) // unmapped → IDENTITY="" → unrecognised

	deadline := time.After(time.Second)
	for {
		select {
		case evt := <-ch:
			if evt.Name != EventBridgeError {
				continue
			}
			p, ok := evt.Payload.(BridgeErrorPayload)
			if !ok {
				t.Fatalf("payload type = %T, want BridgeErrorPayload", evt.Payload)
			}
			if p.Code != BridgeErrCodeIdentityUnrecognised {
				t.Fatalf("bridge-error code = %q, want %q", p.Code, BridgeErrCodeIdentityUnrecognised)
			}
			if s.identityOK() {
				t.Error("identity confirmed despite an unrecognised ID")
			}
			return
		case <-deadline:
			t.Fatal("no identity-unrecognised bridge-error within 1s")
		}
	}
}

// TestPipeline_DecodesFrequencyPush covers the steady-state shape:
// rig dial change → "FA<freq>" push → bridge emits a partial
// rig-state event with VfoA set, every other field zero. The SPA's
// catState merge keeps prior values; this contract only fires for
// what actually changed.
func TestPipeline_DecodesFrequencyPush(t *testing.T) {
	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	fake.feedLine([]byte("FA014250000"))

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed before freq event")
		}
		p := evt.Payload.(RigStatePayload)
		if p.VfoA != 14250000 {
			t.Errorf("VfoA = %d, want 14250000", p.VfoA)
		}
		if p.VfoB != 0 || p.Mode != "" || p.RigIdentity != "" {
			t.Errorf("only VfoA should be set; got %+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("no freq event within 1s")
	}
}

// TestPipeline_DecodesModeAndSplit covers the value-mapped paths
// (mode code → ADIF mode name; split code → bool) so a regression
// in mapStatusToPayload's dispatch surfaces as a test failure
// rather than a silently-empty SPA field.
func TestPipeline_DecodesModeAndSplit(t *testing.T) {
	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	fake.feedLine([]byte("MD02"))  // main mode → "USB"
	fake.feedLine([]byte("ST1"))   // split → ON
	fake.feedLine([]byte("VS1"))   // selected VFO → B
	fake.feedLine([]byte("PC100")) // power → 100W

	got := map[string]bool{}
	deadline := time.After(time.Second)
	for len(got) < 4 {
		select {
		case evt := <-ch:
			p := evt.Payload.(RigStatePayload)
			switch {
			case p.Mode == "USB":
				got["mode"] = true
			case p.SplitOverride != nil && *p.SplitOverride:
				got["split"] = true
			case p.SelectedVfo == "B":
				got["vfo"] = true
			case p.Power == 100:
				got["power"] = true
			}
		case <-deadline:
			t.Fatalf("only saw %v of 4 expected events", got)
		}
	}
}

// TestPipeline_DecodesSplitOff covers the substantive bug from the
// 2026-05-10 internal-bridge-pipeline review (#1): a rig push of
// `ST0` (split OFF) must reach the SPA on the wire, not be silently
// dropped by JSON `omitempty`. With SplitOverride as *bool, the
// payload carries `splitOverride: false` so the SPA's catState merge
// updates correctly. Pre-fix the field collapsed to nothing on the
// wire because `bool false` is `omitempty`-equivalent to "not set".
func TestPipeline_DecodesSplitOff(t *testing.T) {
	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	fake.feedLine([]byte("ST0"))

	select {
	case evt := <-ch:
		p := evt.Payload.(RigStatePayload)
		if p.SplitOverride == nil {
			t.Fatal("SplitOverride is nil; want non-nil pointer carrying false")
		}
		if *p.SplitOverride {
			t.Errorf("SplitOverride = true, want false (rig pushed ST0)")
		}
	case <-time.After(time.Second):
		t.Fatal("no rig-state event within 1s")
	}
}

// TestMapStatusToPayload_Split covers review-finding H1 (2026-06-04): the split
// flag is "engaged unless the rig says OFF". The FTdx10 maps ST 0/1/2 →
// OFF/ON/ON+ ("ON+" = quick split), and the old `EqualFold(v,"ON")` test read
// ON+ as not-split → the SPA would show split off and log the wrong TX/RX freq.
// Tested at the mapStatusToPayload level because it's rigdef-independent (the
// FT-710 used by the pipeline harness only maps ST 0/1, so ON+ can't reach it).
func TestMapStatusToPayload_Split(t *testing.T) {
	cases := []struct {
		split string
		want  bool
	}{
		{"OFF", false},
		{"ON", true},
		{"ON+", true},     // quick split — the H1 regression
		{"ENABLED", true}, // any future non-OFF value is "engaged"
	}
	for _, c := range cases {
		p, ok := mapStatusToPayload(cat.Status{"SPLIT": c.split})
		if !ok {
			t.Errorf("SPLIT=%q: payload not populated", c.split)
			continue
		}
		if p.SplitOverride == nil {
			t.Errorf("SPLIT=%q: SplitOverride is nil; want non-nil *bool", c.split)
			continue
		}
		if *p.SplitOverride != c.want {
			t.Errorf("SPLIT=%q: SplitOverride = %v, want %v", c.split, *p.SplitOverride, c.want)
		}
	}
}

// TestPipeline_LivenessTimeoutEmitsDisconnect covers the 30s passive
// timeout (dialled down for the test). With no lines fed, the
// pipeline's read deadline fires and a single rig-disconnected event
// reaches the hub. ADR 0010 / ADR 0019 contract.
func TestPipeline_LivenessTimeoutEmitsDisconnect(t *testing.T) {
	prev := livenessTimeout
	livenessTimeout = 30 * time.Millisecond
	t.Cleanup(func() { livenessTimeout = prev })

	s, _ := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed before disconnect event")
		}
		if evt.Name != EventRigDisconnected {
			t.Errorf("evt.Name = %q, want %q", evt.Name, EventRigDisconnected)
		}
		p, ok := evt.Payload.(RigDisconnectedPayload)
		if !ok {
			t.Fatalf("payload type = %T, want RigDisconnectedPayload", evt.Payload)
		}
		if p.Code != RigCodeNoData {
			t.Errorf("disconnect Code = %q, want %q", p.Code, RigCodeNoData)
		}
	case <-time.After(time.Second):
		t.Fatal("no disconnect event within 1s")
	}
}

// TestPipeline_DisconnectAnnouncedOnce covers the deduplication rule:
// if data stays silent across multiple consecutive timeout windows,
// only the first window emits a rig-disconnected event. Without
// this, the SPA would see a stream of duplicate toasts.
func TestPipeline_DisconnectAnnouncedOnce(t *testing.T) {
	prev := livenessTimeout
	livenessTimeout = 30 * time.Millisecond
	t.Cleanup(func() { livenessTimeout = prev })

	s, _ := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	// First disconnect arrives within ~one timeout window.
	select {
	case evt := <-ch:
		if evt.Name != EventRigDisconnected {
			t.Fatalf("first event = %q, want %q", evt.Name, EventRigDisconnected)
		}
	case <-time.After(time.Second):
		t.Fatal("no first disconnect within 1s")
	}

	// Wait through several more timeout windows; no further events
	// should arrive.
	select {
	case evt := <-ch:
		t.Errorf("unexpected second event %q after dedup window", evt.Name)
	case <-time.After(livenessTimeout * 4):
		// expected — dedup held
	}
}

// TestPipeline_DataResumesAfterDisconnect covers the recovery shape:
// after a passive disconnect, a fresh rig push resumes rig-state
// events without any explicit "reconnected" signal. The SPA flips
// rigResponding=true on any rig-state arrival per ADR 0009.
func TestPipeline_DataResumesAfterDisconnect(t *testing.T) {
	prev := livenessTimeout
	livenessTimeout = 30 * time.Millisecond
	t.Cleanup(func() { livenessTimeout = prev })

	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	// Drain the first disconnect event.
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no initial disconnect event within 1s")
	}

	// Feed a frequency push; expect a rig-state event.
	fake.feedLine([]byte("FA007074000"))
	select {
	case evt := <-ch:
		if evt.Name != EventRigState {
			t.Errorf("post-recovery event = %q, want %q", evt.Name, EventRigState)
		}
		p := evt.Payload.(RigStatePayload)
		if p.VfoA != 7074000 {
			t.Errorf("VfoA = %d, want 7074000", p.VfoA)
		}
	case <-time.After(time.Second):
		t.Fatal("no rig-state event after data resumed")
	}
}

// TestPipeline_TerminalSerialErrorEmitsDisconnect covers the
// "rig went away mid-session" path: the fake's Close() trips
// ErrClosed on the next ReadResponseBytes, which the pipeline
// surfaces as a final rig-disconnected event with the cause in the
// reason string.
func TestPipeline_TerminalSerialErrorEmitsDisconnect(t *testing.T) {
	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	// Wait for the pipeline's INIT write to land before we Close —
	// a loaded CI agent can take longer than a fixed sleep would
	// allow, and Close-before-INIT would surface as a bridge-error
	// (INIT-write failed) rather than the rig-disconnected this
	// test asserts. Poll on recordedWrites mirrors the pattern
	// other pipeline tests use. Per internal-bridge-pipeline.md
	// review #5.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(fake.recordedWrites()) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(fake.recordedWrites()) < 1 {
		t.Fatal("pipeline did not send INIT within 1s")
	}
	if err := fake.Close(); err != nil {
		t.Fatalf("fake.Close: %v", err)
	}

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed before disconnect event")
		}
		if evt.Name != EventRigDisconnected {
			t.Errorf("evt.Name = %q, want %q", evt.Name, EventRigDisconnected)
		}
	case <-time.After(time.Second):
		t.Fatal("no disconnect event within 1s of terminal close")
	}
}

// TestPipeline_UnknownDriverExitsCleanly covers the misconfigured-
// driver path: cat.Lookup returns ok=false, the pipeline logs +
// publishes bridge-error and exits without panicking. Stop still
// works.
func TestPipeline_UnknownDriverExitsCleanly(t *testing.T) {
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "no-such-rig"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	installFakeSerial(s)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestPipeline_OpenFailureExitsCleanly covers the port-not-available
// path: openClient returns an error (operator typo'd the port,
// permission denied, etc.). Pipeline logs and exits; Service.Stop
// still works.
func TestPipeline_OpenFailureExitsCleanly(t *testing.T) {
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	s.openClient = func(_ serial.Config) (serial.Client, error) {
		return nil, errOpenFailed
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestPipeline_TriggerBootstrap_WritesReadCommand covers the
// bootstrap-on-SSE-open contract: TriggerBootstrap writes the
// rigdef's READ command via the active client. Wire shape is
// "ID;FA;FB;ST;VS;MD0;MD1;PC;" for both Yaesu rigdefs.
//
// As of 2026-05-16, runPipeline ALSO sends READ at startup right
// after INIT (so supervisor-driven recovery pulls a fresh snapshot
// without waiting for an SSE-open). That makes writes[0]=INIT and
// writes[1]=READ before TriggerBootstrap fires; the per-bootstrap
// READ becomes writes[2]. This test asserts that the TriggerBootstrap
// call genuinely causes the additional READ — not that READ merely
// appears somewhere in the write log.
func TestPipeline_TriggerBootstrap_WritesReadCommand(t *testing.T) {
	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	// Wait for runPipeline's INIT + post-INIT READ to land. Without
	// this gate, the test could fire TriggerBootstrap before
	// runPipeline finishes its pair of writes, making the
	// "TriggerBootstrap added a new write" assertion meaningless.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(fake.recordedWrites()) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(fake.recordedWrites()) < 2 {
		t.Fatalf("runPipeline did not write INIT + READ within 1s; got %d writes", len(fake.recordedWrites()))
	}

	if err := s.TriggerBootstrap(context.Background()); err != nil {
		t.Fatalf("TriggerBootstrap: %v", err)
	}

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(fake.recordedWrites()) >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	writes := fake.recordedWrites()
	if len(writes) < 3 {
		t.Fatalf("expected 3 writes (INIT + startup READ + bootstrap READ), got %d", len(writes))
	}
	if !bytes.Equal(writes[2], []byte("ID;FA;FB;ST;VS;MD0;MD1;PC;")) {
		t.Errorf("TriggerBootstrap write = %q, want %q", writes[2], "ID;FA;FB;ST;VS;MD0;MD1;PC;")
	}
}

// TestPipeline_TriggerBootstrap_NoOpWhenPipelineNotRunning covers
// the safe-by-design contract: calling TriggerBootstrap on a Service
// whose pipeline hasn't started (or has exited) returns nil silently
// rather than panicking on a nil client.
func TestPipeline_TriggerBootstrap_NoOpWhenPipelineNotRunning(t *testing.T) {
	s := New(types.BridgeConfig{Enabled: false}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	if err := s.TriggerBootstrap(context.Background()); err != nil {
		t.Errorf("TriggerBootstrap on disabled bridge returned err: %v", err)
	}
}

// TestPipeline_BridgeError_UnknownDriver covers the operator-typo
// path: bridge.cat.driver names a rig that's not in the embedded
// rigdb. Pipeline publishes a bridge-error event with the bad
// driver name in the message and exits cleanly. Subscribe-after-
// Start works because the hub caches the bridge-error and replays
// it to the late subscriber.
func TestPipeline_BridgeError_UnknownDriver(t *testing.T) {
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-no-such-rig"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	installFakeSerial(s)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed before bridge-error")
		}
		if evt.Name != EventBridgeError {
			t.Errorf("evt.Name = %q, want %q", evt.Name, EventBridgeError)
		}
		p, ok := evt.Payload.(BridgeErrorPayload)
		if !ok {
			t.Fatalf("payload type = %T, want BridgeErrorPayload", evt.Payload)
		}
		if p.Code != BridgeErrCodeUnknownDriver {
			t.Errorf("Code = %q, want %q", p.Code, BridgeErrCodeUnknownDriver)
		}
		if p.Details["driver"] != "yaesu-no-such-rig" {
			t.Errorf("Details[driver] = %q, want %q", p.Details["driver"], "yaesu-no-such-rig")
		}
	case <-time.After(time.Second):
		t.Fatal("no bridge-error event within 1s")
	}
}

// TestHub_CachesBridgeErrorForLateSubscriber pins the cache contract
// from review #2: startup-time bridge-errors (unknown driver, port
// permission, etc.) fire while no SSE client is connected, but a
// SPA tab opening later still receives the cached error as its
// first event so the operator gets the toast.
//
// Eager-start: pipeline goroutine spawns at Service.Start, runs
// cat.Lookup, fails, calls publishBridgeError, exits. Late
// subscribe replays the cached event.
func TestHub_CachesBridgeErrorForLateSubscriber(t *testing.T) {
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "unknown-on-purpose"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	installFakeSerial(s)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	// Give the eager-spawn goroutine time to fail and publish the
	// bridge-error before we subscribe. Without this sleep the
	// race is undefined; with it, we deterministically test the
	// late-subscribe-sees-cached-error path.
	time.Sleep(50 * time.Millisecond)

	ch, unsub := s.Subscribe()
	defer unsub()

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed before cached bridge-error")
		}
		if evt.Name != EventBridgeError {
			t.Errorf("evt.Name = %q, want %q", evt.Name, EventBridgeError)
		}
	case <-time.After(time.Second):
		t.Fatal("late subscriber did not see cached bridge-error within 1s")
	}

	// Second late subscriber should also see the cached error —
	// the cache isn't consumed by Subscribe.
	ch2, unsub2 := s.Subscribe()
	defer unsub2()

	select {
	case evt, ok := <-ch2:
		if !ok {
			t.Fatal("second subscriber channel closed before cached bridge-error")
		}
		if evt.Name != EventBridgeError {
			t.Errorf("second subscriber evt.Name = %q, want %q", evt.Name, EventBridgeError)
		}
	case <-time.After(time.Second):
		t.Fatal("second late subscriber did not see cached bridge-error within 1s")
	}
}

// TestHub_CachesRigDisconnectedForLateSubscriber pins the parallel
// cache contract for rig-disconnected: when the pipeline's
// announcedDisconnect dedup fires the event exactly once per
// silent-window, every later subscriber should still see the toast.
// Without the cache the second SPA tab gets no notification of an
// off rig.
//
// Drives the hub directly (rather than the pipeline) so the test
// stays focused on the cache semantics and doesn't depend on the
// 30s liveness timeout firing.
func TestHub_CachesRigDisconnectedForLateSubscriber(t *testing.T) {
	h := newHub()
	defer h.close()

	disconnect := Event{Name: EventRigDisconnected, Payload: RigDisconnectedPayload{Code: RigCodeNoData}}
	h.publish(disconnect)

	// First late subscriber sees the cached event as its first frame.
	ch1, unsub1 := h.subscribe()
	defer unsub1()
	select {
	case evt, ok := <-ch1:
		if !ok {
			t.Fatal("first subscriber channel closed before cached rig-disconnected")
		}
		if evt.Name != EventRigDisconnected {
			t.Errorf("evt.Name = %q, want %q", evt.Name, EventRigDisconnected)
		}
	case <-time.After(time.Second):
		t.Fatal("first late subscriber did not see cached rig-disconnected within 1s")
	}

	// Second late subscriber also sees it — cache is not consumed.
	ch2, unsub2 := h.subscribe()
	defer unsub2()
	select {
	case evt, ok := <-ch2:
		if !ok {
			t.Fatal("second subscriber channel closed before cached rig-disconnected")
		}
		if evt.Name != EventRigDisconnected {
			t.Errorf("second subscriber evt.Name = %q, want %q", evt.Name, EventRigDisconnected)
		}
	case <-time.After(time.Second):
		t.Fatal("second late subscriber did not see cached rig-disconnected within 1s")
	}
}

// TestHub_ClearsRigDisconnectedCacheOnRigState pins the auto-recovery
// invariant: when the rig starts pushing data again, EventRigState
// publication clears the cached disconnect so subscribers that connect
// AFTER recovery don't see a stale toast for a rig that is fine.
//
// Distinct from bridge-error (which is never cleared on the principle
// that operator-actionable errors should stay observable) because
// rig-disconnected describes transient state that the
// implicit-reconnect flow (ADR 0009) naturally resolves.
func TestHub_ClearsRigDisconnectedCacheOnRigState(t *testing.T) {
	h := newHub()
	defer h.close()

	// Disconnect, then a rig push (rig came back online).
	h.publish(Event{Name: EventRigDisconnected, Payload: RigDisconnectedPayload{Code: RigCodeNoData}})
	h.publish(Event{Name: EventRigState, Payload: RigStatePayload{VfoA: 14_250_000}})

	// A subscriber connecting now should NOT see the stale
	// disconnect — the rig is back. The subscriber channel should
	// stay quiet (no replay).
	ch, unsub := h.subscribe()
	defer unsub()
	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed unexpectedly")
		}
		t.Fatalf("expected no cached event after recovery, got %q", evt.Name)
	case <-time.After(50 * time.Millisecond):
		// expected — no replay; rig is fine, no toast for late subscriber.
	}
}

// TestPipeline_BridgeError_OpenFailure covers the port-not-available
// path (operator typo'd the device, permission denied, etc.):
// openClient returns an error → bridge-error event with the cause →
// pipeline exits cleanly.
func TestPipeline_BridgeError_OpenFailure(t *testing.T) {
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "/dev/no-such-port"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	s.openClient = func(_ serial.Config) (serial.Client, error) {
		return nil, errOpenFailed
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("subscriber channel closed before bridge-error")
		}
		if evt.Name != EventBridgeError {
			t.Errorf("evt.Name = %q, want %q", evt.Name, EventBridgeError)
		}
		p, ok := evt.Payload.(BridgeErrorPayload)
		if !ok {
			t.Fatalf("payload type = %T, want BridgeErrorPayload", evt.Payload)
		}
		if p.Code != BridgeErrCodeSerialOpenFailed {
			t.Errorf("Code = %q, want %q", p.Code, BridgeErrCodeSerialOpenFailed)
		}
		if p.Details["port"] != "/dev/no-such-port" {
			t.Errorf("Details[port] = %q, want it to mention the bad port", p.Details["port"])
		}
	case <-time.After(time.Second):
		t.Fatal("no bridge-error event within 1s")
	}
}

// TestPipeline_IdentityVerified_NoErrorOnMatch covers the happy path:
// rig ID 0800 maps to "FT-710" via the FT-710 rigdef, which matches
// def.Model. No bridge-error fires; the IDENTITY arrives as a
// normal rig-state event.
func TestPipeline_IdentityVerified_NoErrorOnMatch(t *testing.T) {
	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	fake.feedLine([]byte("ID0800")) // FT-710's known ID

	// First event should be the rig-state with the identity. No
	// bridge-error should follow.
	select {
	case evt := <-ch:
		if evt.Name != EventRigState {
			t.Errorf("first event = %q, want %q", evt.Name, EventRigState)
		}
	case <-time.After(time.Second):
		t.Fatal("no event within 1s")
	}

	// Drain a short window to confirm no bridge-error follows.
	select {
	case evt := <-ch:
		if evt.Name == EventBridgeError {
			t.Errorf("unexpected bridge-error after identity match: %+v", evt.Payload)
		}
	case <-time.After(100 * time.Millisecond):
		// expected — no follow-up event
	}
}

// TestPipeline_IdentityVerified_ErrorOnUnrecognised covers the
// operator-wired-wrong-driver path: rig responds with an ID code
// the configured rigdef doesn't have a value mapping for. The
// IDENTITY tag decodes to empty string (cat decoder's
// no-match-in-mapping behaviour) and the bridge fires a bridge-error
// telling the operator their bridge.cat.driver is wrong.
func TestPipeline_IdentityVerified_ErrorOnUnrecognised(t *testing.T) {
	s, fake := newPipelineTestService(t) // configured for yaesu-ft710
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	// Feed an ID code that's NOT in FT-710's value mapping
	// (FT-710's rigdef knows only 0800 → "FT-710").
	fake.feedLine([]byte("ID9999"))

	// Expect a bridge-error within the read loop's first iteration.
	// The rig-state event for the (empty) IDENTITY is suppressed
	// because mapStatusToPayload skips empty values, so we don't
	// see a redundant rig-state alongside the bridge-error.
	deadline := time.After(time.Second)
	for {
		select {
		case evt := <-ch:
			if evt.Name == EventBridgeError {
				p := evt.Payload.(BridgeErrorPayload)
				if p.Code != BridgeErrCodeIdentityUnrecognised {
					t.Errorf("Code = %q, want %q", p.Code, BridgeErrCodeIdentityUnrecognised)
				}
				return
			}
		case <-deadline:
			t.Fatal("no bridge-error within 1s")
		}
	}
}

// TestPipeline_IdentityVerified_OnceOnly covers the dedup rule:
// repeated IDENTITY pushes (e.g. multiple bootstrap polls) only
// produce one bridge-error if there's a problem. Otherwise the
// SPA would be flooded.
func TestPipeline_IdentityVerified_OnceOnly(t *testing.T) {
	s, fake := newPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	ch, unsub := s.Subscribe()
	defer unsub()

	fake.feedLine([]byte("ID9999")) // first unrecognised
	fake.feedLine([]byte("ID9999")) // second — should NOT fire again
	fake.feedLine([]byte("ID9999")) // third — same

	bridgeErrors := 0
	deadline := time.After(300 * time.Millisecond)
loop:
	for {
		select {
		case evt := <-ch:
			if evt.Name == EventBridgeError {
				bridgeErrors++
			}
		case <-deadline:
			break loop
		}
	}
	if bridgeErrors != 1 {
		t.Errorf("got %d bridge-error events, want exactly 1 (identity-verify dedup)", bridgeErrors)
	}
}

// TestBuildSerialConfig_FromYaesuRigDef walks the rigdef → serial
// translation for the FT-710 (the canonical reference rig). Pins the
// expected enums + delimiter so a future rigdef edit that forgets to
// re-run the build would surface as a test failure.
func TestBuildSerialConfig_FromYaesuRigDef(t *testing.T) {
	def, ok := cat.Lookup("yaesu-ft710")
	if !ok {
		t.Fatal("yaesu-ft710 rigdef not found")
	}
	cfg, err := buildSerialConfig(types.BridgeSerialConfig{
		Port: "/dev/ttyUSB0",
	}, def.Serial)
	if err != nil {
		t.Fatalf("buildSerialConfig: %v", err)
	}
	if cfg.PortName != "/dev/ttyUSB0" {
		t.Errorf("PortName = %q, want %q", cfg.PortName, "/dev/ttyUSB0")
	}
	if cfg.BaudRate != def.Serial.BaudRate {
		t.Errorf("BaudRate = %d, want %d (from rigdef)", cfg.BaudRate, def.Serial.BaudRate)
	}
	if cfg.DataBits != 8 {
		t.Errorf("DataBits = %d, want 8", cfg.DataBits)
	}
	if cfg.LineDelimiter != ';' {
		t.Errorf("LineDelimiter = %q, want %q", cfg.LineDelimiter, ';')
	}
	if cfg.ReadTimeoutMS != def.Serial.ReadTimeoutMS {
		t.Errorf("ReadTimeoutMS = %d, want %d", cfg.ReadTimeoutMS, def.Serial.ReadTimeoutMS)
	}
}
