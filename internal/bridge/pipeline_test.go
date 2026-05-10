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
		Serial:  types.BridgeSerialConfig{Port: "fake", Baud: 38400},
		Cat:     types.BridgeCatConfig{Driver: "yaesu-ft710"},
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
			// FT-710 INIT is "AI1;ID;" — serial.Port appends the
			// configured delimiter (';') if missing; the rigdef
			// already terminates with ';' so the command lands
			// verbatim.
			if !bytes.Equal(writes[0], []byte("AI1;ID;")) {
				t.Errorf("first write = %q, want %q", writes[0], "AI1;ID;")
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
			case p.SplitOverride:
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
		if p.Reason == "" {
			t.Error("disconnect Reason is empty; want a human-readable string")
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

	// Give the pipeline a moment to enter ReadResponseBytes (it has
	// to send INIT first, which races against Close on a busy CI).
	time.Sleep(20 * time.Millisecond)
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
// driver path: cat.Lookup returns ok=false, the pipeline logs and
// exits without panicking. Stop still works.
func TestPipeline_UnknownDriverExitsCleanly(t *testing.T) {
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  types.BridgeSerialConfig{Port: "fake", Baud: 38400},
		Cat:     types.BridgeCatConfig{Driver: "no-such-rig"},
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
		Serial:  types.BridgeSerialConfig{Port: "fake", Baud: 38400},
		Cat:     types.BridgeCatConfig{Driver: "yaesu-ft710"},
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
		Baud: 38400,
	}, def.Serial)
	if err != nil {
		t.Fatalf("buildSerialConfig: %v", err)
	}
	if cfg.PortName != "/dev/ttyUSB0" {
		t.Errorf("PortName = %q, want %q", cfg.PortName, "/dev/ttyUSB0")
	}
	if cfg.BaudRate != 38400 {
		t.Errorf("BaudRate = %d, want 38400", cfg.BaudRate)
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
