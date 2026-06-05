package bridge

import (
	"context"
	"errors"
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
		Cat:     types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	})
	f := newFakeSerial()
	s.activeClient = f
	s.lastMode = "USB"
	s.lastPower = 100
	s.tuneMaxDuration = time.Hour
	return s, f
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
	if TuneSupported(ft710) {
		t.Error("TuneSupported(ft710) = true; rigdef has no tune commands")
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

func TestEncodeTuneOff(t *testing.T) {
	def, _ := cat.Lookup("yaesu-ftdx10")
	cases := []struct {
		mode  string
		power int
		want  string
	}{
		{"USB", 100, "TX0;PC100;MD02;"}, // unkey, restore power, restore mode
		{"CW-U", 5, "TX0;PC005;MD03;"},  // 5 W → PC005;, CW-U → 3
		{"", 0, "TX0;"},                 // unknown restore → at least unkey
	}
	for _, c := range cases {
		got, err := encodeTuneOff(def, c.mode, c.power)
		if err != nil {
			t.Errorf("encodeTuneOff(%q,%d) unexpected error: %v", c.mode, c.power, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("encodeTuneOff(%q,%d) = %q, want %q", c.mode, c.power, string(got), c.want)
		}
	}
}

// A rigdef that can key TX but has no tx_off command must NOT yield a tune-off
// line — silently omitting the unkey would strand a keyed carrier while the
// release path reports it down (review 2026-06-04 H3). encodeTuneOff returns an
// error instead so releaseTune stays armed and loud.
func TestEncodeTuneOff_RequiresTxOff(t *testing.T) {
	def := cat.RigDefinition{
		ID:         "test-no-txoff",
		Terminator: ";",
		Commands: []cat.Command{
			{Name: tuneTxOnCommand, Cmd: "TX1;"},
			// deliberately no tx_off
		},
	}
	got, err := encodeTuneOff(def, "USB", 100)
	if err == nil {
		t.Fatalf("encodeTuneOff without tx_off = %q, want error", string(got))
	}
	if got != nil {
		t.Errorf("encodeTuneOff error path returned bytes %q, want nil", string(got))
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

func TestStartTune_RefusesNotConnected(t *testing.T) {
	s, _ := tuneTestService(t)
	s.activeClient = nil

	err := s.StartTune(context.Background())
	if !errors.Is(err, ErrRigNotConnected) {
		t.Fatalf("StartTune err = %v, want ErrRigNotConnected", err)
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
	ch, unsub := s.Subscribe()
	defer unsub()

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	awaitTuneState(t, ch, true, time.Second)

	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("StopTune: %v", err)
	}
	// Restore to the pre-tune snapshot (USB / 100 W): unkey, power, mode.
	if got, want := lastWrite(f), "TX0;PC100;MD02;"; got != want {
		t.Errorf("tune-off write = %q, want %q", got, want)
	}
	s.mu.Lock()
	active := s.tuneActive
	s.mu.Unlock()
	if active {
		t.Error("tuneActive = true after StopTune")
	}
	awaitTuneState(t, ch, false, time.Second)
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

func TestTuneAutoOff(t *testing.T) {
	s, f := tuneTestService(t)
	s.tuneMaxDuration = 30 * time.Millisecond
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
	if got, want := lastWrite(f), "TX0;PC100;MD02;"; got != want {
		t.Errorf("auto-off write = %q, want %q (unkey + restore)", got, want)
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
