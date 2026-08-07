package bridge

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestResolveTimeouts checks the sparse-but-served resolution: a zero config
// yields the built-in defaults, an operator value is preserved, and the
// civ-read-gap ceiling is applied — the same resolution Service.New uses, so
// the value the /v1/config GET serves matches what a running Service uses.
func TestResolveTimeouts(t *testing.T) {
	// Empty in → all defaults out.
	got := ResolveTimeouts(types.BridgeTimeoutsConfig{})
	want := types.BridgeTimeoutsConfig{
		LivenessMs:             5000,
		BackoffInitialMs:       1000,
		BackoffMaxMs:           30000,
		SteadyStateThresholdMs: 10000,
		WriteWatchdogMs:        2000,
		CivReadGapMs:           50,
		CivAckMs:               500,
		CivPollIntervalMs:      1000,
		CivPollQuietMs:         250,
		Ft8MeterPollIntervalMs: 250,
		Ft8MeterPollTimeoutMs:  100,
	}
	if got != want {
		t.Fatalf("ResolveTimeouts(zero) = %+v, want %+v", got, want)
	}

	// Operator value preserved; civ_read_gap above the 2s ceiling is clamped.
	got = ResolveTimeouts(types.BridgeTimeoutsConfig{LivenessMs: 8000, CivReadGapMs: 9999})
	if got.LivenessMs != 8000 {
		t.Errorf("LivenessMs = %d, want 8000 (operator value preserved)", got.LivenessMs)
	}
	if got.CivReadGapMs != 2000 {
		t.Errorf("CivReadGapMs = %d, want 2000 (clamped to civReadGapMax)", got.CivReadGapMs)
	}

	// Backoff initial ≤ max on the EFFECTIVE values (50e35d review P2): a max set
	// with the initial omitted must serve a clamped initial, not the raw 1s
	// default — otherwise /v1/config reports 1000/50 while the supervisor (via New)
	// runs 50/50. This is exactly the clamp New applies, now shared through
	// resolveBackoff so the served view can never disagree with the running service.
	got = ResolveTimeouts(types.BridgeTimeoutsConfig{BackoffMaxMs: 50})
	if got.BackoffInitialMs != 50 {
		t.Errorf("BackoffInitialMs = %d with max=50 and initial unset, want 50 (clamped) — "+
			"the served effective initial must not exceed the effective max", got.BackoffInitialMs)
	}
	if got.BackoffMaxMs != 50 {
		t.Errorf("BackoffMaxMs = %d, want 50 (operator value preserved)", got.BackoffMaxMs)
	}
}

// TestResolveTune checks tune resolution: defaults where zero, clamped to the
// hard safety ceilings (power 40, duration 30000, settle 2000).
func TestResolveTune(t *testing.T) {
	if got := ResolveTune(types.BridgeTuneConfig{}, ""); got != (types.BridgeTuneConfig{
		PowerW: 20, MaxDurationMs: 15000, RestoreSettleMs: 150,
	}) {
		t.Fatalf("ResolveTune(zero) = %+v, want {20 15000 150}", got)
	}

	// Over-ceiling values clamp to the non-overridable safety limits.
	got := ResolveTune(types.BridgeTuneConfig{PowerW: 100, MaxDurationMs: 60000, RestoreSettleMs: 5000}, "")
	if got.PowerW != 40 || got.MaxDurationMs != 30000 || got.RestoreSettleMs != 2000 {
		t.Errorf("ResolveTune(over-ceiling) = %+v, want {40 30000 2000}", got)
	}

	// Def-floor fallback (2026-07-19 review P3): a power below the active
	// rigdef's set_power minimum (FTdx10 PC floor = 5 W) is not encodable, so
	// the served effective value falls back to the default instead of
	// advertising a config every StartTune would fail on.
	got = ResolveTune(types.BridgeTuneConfig{PowerW: 1}, "yaesu-ftdx10")
	if got.PowerW != 20 {
		t.Errorf("ResolveTune(1 W, ftdx10) PowerW = %d, want 20 (def-floor fallback)", got.PowerW)
	}
	// In-range values pass through unchanged with the def present.
	got = ResolveTune(types.BridgeTuneConfig{PowerW: 10}, "yaesu-ftdx10")
	if got.PowerW != 10 {
		t.Errorf("ResolveTune(10 W, ftdx10) PowerW = %d, want 10", got.PowerW)
	}
}
