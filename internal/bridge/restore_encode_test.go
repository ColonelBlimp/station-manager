package bridge

// H1 (docs/reviews/internal-codebase-logging-gaps.md) — restore ENCODE failures must not be
// silently skipped. Both the tune restore (encodeTuneRestore) and the FT8 mode restore
// dropped a component whose CAT command failed to encode with no trace, so the write-failure
// logging could never fire. Operator rulings 2026-08-16:
//   1. tune: ONE aggregated Warn per restore, with restore_power_error and/or
//      restore_mode_error (omit the field for a component that encoded OK), phase=encode to
//      distinguish it from a write failure.
//   2. invalidate the tune snapshot if EITHER component fails to encode, but still write every
//      component that DID encode — internal-confidence change only, stop/restore sequencing
//      unchanged.
//   3. FT8 is log-only: one Warn with the encode cause, after PTT-down confirmation.
//   All logging is strictly downstream of confirmed unkey/PTT-down.

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Ruling 1 + 2 core, unit level: encodeTuneRestore returns per-component encode errors AND
// still encodes the component that succeeded (so a mode that won't encode does not lose the
// power restore). It is a pure package func, exactly as the finding notes.
func TestEncodeTuneRestore_ReturnsFailuresAndStillEncodesOKComponents(t *testing.T) {
	def, ok := cat.Lookup("yaesu-ftdx10")
	if !ok {
		t.Fatal("yaesu-ftdx10 rigdef missing")
	}

	// Mode won't encode (not in the value_map); 50 W will.
	line, powerErr, modeErr := encodeTuneRestore(def, "ZZ-NOT-A-MODE", 50)
	if powerErr != nil {
		t.Errorf("powerErr = %v, want nil (50 W encodes)", powerErr)
	}
	if modeErr == nil {
		t.Error("modeErr = nil, want an error for an unencodable mode")
	}
	if len(line) == 0 {
		t.Error("line empty — the OK power component must still be encoded (ruling 2)")
	}

	// Both encode: no errors, both components on the line.
	line2, pErr2, mErr2 := encodeTuneRestore(def, "USB", 50)
	if pErr2 != nil || mErr2 != nil {
		t.Errorf("all-ok: unexpected errors power=%v mode=%v", pErr2, mErr2)
	}
	if len(line2) == 0 {
		t.Error("all-ok: line empty, want both components encoded")
	}

	// Nothing to restore: empty line, no errors.
	line3, pErr3, mErr3 := encodeTuneRestore(def, "", 0)
	if len(line3) != 0 || pErr3 != nil || mErr3 != nil {
		t.Errorf("nothing to restore: line=%d power=%v mode=%v", len(line3), pErr3, mErr3)
	}
}

// C1 (tune, integration): a restore MODE that won't encode produces exactly ONE Warn after
// the carrier is down — phase=encode, restore_mode_error present, restore_power_error ABSENT
// (power encoded) — the snapshot is invalidated, and the power that DID encode is still
// written (ruling 2). Fixture forces the defensive path with an unencodable pre-tune mode.
func TestTuneRestore_ModeEncodeFailure_WarnsAndInvalidatesSnapshotButWritesPower(t *testing.T) {
	s, f, buf := tuneLoggedService(t)
	t.Cleanup(answerTxStatusQueries(s, f)) // healthy rig: the confirm-gate passes
	s.lastMode = "ZZ-NOT-A-MODE"           // restore mode won't encode; power (100) will

	if err := s.StartTune(context.Background()); err != nil {
		t.Fatalf("StartTune: %v", err)
	}
	if err := s.StopTune(context.Background()); err != nil {
		t.Fatalf("StopTune: %v", err)
	}

	recs := matching(t, buf, "tune restore encode failed")
	if len(recs) != 1 {
		t.Fatalf("encode-failure warnings = %d, want 1\n%s", len(recs), buf.String())
	}
	r := recs[0]
	if r["level"] != "warn" {
		t.Errorf("level = %v, want warn", r["level"])
	}
	if r["phase"] != "encode" {
		t.Errorf("phase = %v, want encode (distinct from a write failure)", r["phase"])
	}
	if _, ok := r["restore_mode_error"]; !ok {
		t.Errorf("want restore_mode_error present: %v", r)
	}
	if _, ok := r["restore_power_error"]; ok {
		t.Errorf("power encoded — restore_power_error must be omitted: %v", r)
	}

	// Ruling 2: the snapshot is invalidated (it now lies), and the OK power was still written.
	s.mu.Lock()
	mode := s.lastMode
	s.mu.Unlock()
	if mode != "" {
		t.Errorf("snapshot not invalidated after encode failure: lastMode=%q", mode)
	}
	var sawPower bool
	for _, w := range f.recordedWrites() {
		if string(w) == "PC100;" {
			sawPower = true
		}
	}
	if !sawPower {
		t.Errorf("the power component that encoded must still be written (ruling 2); writes=%q", f.recordedWrites())
	}
}

// C2 (ft8, integration): an FT8 restore mode that won't encode produces one Warn after
// PTT-down confirmation — phase=encode — instead of being silently skipped. Log-only.
func TestFt8ModeRestore_EncodeFailure_Warns(t *testing.T) {
	buf := &syncBuf{}
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, logging.NewForWriter(buf))
	f := newFakeSerial()
	s.activeClient = f
	s.identityConfirmed = true
	s.tuneRestoreSettle = 0
	s.lastMode = "ZZ-NOT-A-MODE" // captured as the ft8 restore mode at key time; won't encode
	t.Cleanup(answerTxStatusQueries(s, f))

	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); err != nil {
		t.Fatalf("KeyFt8Tx: %v", err)
	}
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}

	recs := matching(t, buf, "ft8 mode restore encode failed")
	if len(recs) != 1 {
		t.Fatalf("ft8 encode-failure warnings = %d, want 1\n%s", len(recs), buf.String())
	}
	if recs[0]["phase"] != "encode" {
		t.Errorf("phase = %v, want encode", recs[0]["phase"])
	}
}
