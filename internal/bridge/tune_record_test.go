package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// tuneLoggedService is tuneTestService with a buffer-backed logger so the L4 tune
// start/stop records can be asserted.
func tuneLoggedService(t *testing.T) (*Service, *fakeSerial, *syncBuf) {
	t.Helper()
	buf := &syncBuf{}
	s := New(types.BridgeConfig{
		Enabled: true,
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, logging.NewForWriter(buf))
	f := newFakeSerial()
	s.activeClient = f
	s.identityConfirmed = true
	s.lastMode = "USB"
	s.lastPower = 100
	s.tuneMaxDuration = time.Hour
	s.tuneRestoreSettle = 5 * time.Millisecond
	return s, f, buf
}

// L4: the normal operator tune START now leaves a durable record (it published SSE
// only before), carrying reason, carrier power, mode, and the auto-off ceiling.
func TestTuneRecord_StartLogged(t *testing.T) {
	s, _, buf := tuneLoggedService(t)

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}

	recs := matching(t, buf, "tune carrier started")
	if len(recs) != 1 {
		t.Fatalf("want one start record, got %d: %q", len(recs), buf.String())
	}
	r := recs[0]
	if r["reason"] != "operator" || r["mode"] != tuneCarrierMode {
		t.Errorf("start record reason/mode wrong: %v", r)
	}
	if r["power_w"] != float64(s.tunePowerW) {
		t.Errorf("power_w = %v, want %d", r["power_w"], s.tunePowerW)
	}
	if r["auto_off_seconds"] != float64(3600) {
		t.Errorf("auto_off_seconds = %v, want 3600", r["auto_off_seconds"])
	}
}

// L4 confusable state: an operator stop is distinguishable from auto-off/disconnect by
// reason, and carries the actual keyed duration — the normal path that logged nothing.
func TestTuneRecord_OperatorStopLogged(t *testing.T) {
	s, f, buf := tuneLoggedService(t)
	t.Cleanup(answerTxStatusQueries(s, f)) // healthy rig: confirm-gate passes

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	// Deterministic duration: backdate the start so duration_ms has a known floor.
	s.mu.Lock()
	s.tuneStart = time.Now().Add(-3 * time.Second)
	s.mu.Unlock()

	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("StopTune: %v", err)
	}

	recs := matching(t, buf, "tune carrier stopped")
	if len(recs) != 1 {
		t.Fatalf("want one stop record, got %d: %q", len(recs), buf.String())
	}
	r := recs[0]
	if r["reason"] != "operator" || r["mode"] != tuneCarrierMode {
		t.Errorf("stop record reason/mode wrong: %v", r)
	}
	if d, _ := r["duration_ms"].(float64); d < 3000 {
		t.Errorf("duration_ms = %v, want >= 3000", r["duration_ms"])
	}
}

// L4: the auto-off backstop's stop reads as the SAME uniform record (reason=auto-off),
// replacing the old free-text "auto-off fired" line.
func TestTuneRecord_AutoOffStopLogged(t *testing.T) {
	s, f, buf := tuneLoggedService(t)
	t.Cleanup(answerTxStatusQueries(s, f))

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	s.mu.Lock()
	gen := s.tuneGen
	s.mu.Unlock()
	s.tuneAutoOff(gen)

	recs := matching(t, buf, "tune carrier stopped")
	if len(recs) != 1 || recs[0]["reason"] != "auto-off" {
		t.Fatalf("want one auto-off stop record, got: %q", buf.String())
	}
	if len(matching(t, buf, "auto-off fired")) != 0 {
		t.Errorf("the legacy free-text auto-off message must be gone: %q", buf.String())
	}
}

// L4: a rig disconnect mid-tune reads as the same uniform stop record
// (reason=disconnect), so all three teardown paths are queryable alike.
func TestTuneRecord_DisconnectStopLogged(t *testing.T) {
	s, _, buf := tuneLoggedService(t)

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	s.clearTuneOnDisconnect()

	recs := matching(t, buf, "tune carrier stopped")
	if len(recs) != 1 || recs[0]["reason"] != "disconnect" {
		t.Fatalf("want one disconnect stop record, got: %q", buf.String())
	}
}
