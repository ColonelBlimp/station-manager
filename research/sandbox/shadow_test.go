package sandbox_test

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/research/sandbox"
)

// synthCleanFT8Audio synthesises a unit-amplitude FT8 signal carrying
// "CQ K1JT FN20" at the given carrier frequency. The audio buffer is
// noiseless: only the modulated signal sits in the buffer; everything
// outside the signal window is zero. Returns the buffer and the
// expected decode text.
//
// Used by the shadow-reject tests to drive a clean fixture through
// MultiPassDecodeFull without depending on captures/.
func synthCleanFT8Audio(t *testing.T, carrierHz float64) ([]float32, string) {
	t.Helper()
	const (
		audioRate = 12000.0
		audioLen  = 180000 // 15 s
		dtSec     = 0.0
	)

	payload, err := sandbox.PackType1("CQ", "K1JT", "FN20")
	if err != nil {
		t.Fatalf("PackType1: %v", err)
	}
	info := sandbox.PayloadToInfo91(payload)
	cw := sandbox.EncodeLDPC(info)
	tones := sandbox.CodewordToTones(cw)

	cosSynth, _, _, _ := sandbox.SynthesizeAudio(tones, carrierHz, dtSec, audioRate, audioLen)
	audio := make([]float32, audioLen)
	copy(audio, cosSynth)
	return audio, "CQ K1JT FN20"
}

// TestMultiPassDecodeFull_CleanFixtureProducesDecode is the
// smoke-test: a clean synthetic FT8 signal end-to-end through
// MultiPassDecodeFull must surface in .Decodes with the expected
// text. Shadow rejects on a clean fixture may or may not be empty —
// the candidate scanner can surface sidelobes that pass BP/OSD via
// CRC lottery — so we don't assert on their count, only that the
// real decode landed.
func TestMultiPassDecodeFull_CleanFixtureProducesDecode(t *testing.T) {
	audio, wantText := synthCleanFT8Audio(t, 1500.0)
	opts := sandbox.DefaultMultiPassOptions()

	res := sandbox.MultiPassDecodeFull(audio, opts, nil)

	if len(res.Decodes) == 0 {
		t.Fatalf("MultiPassDecodeFull: 0 decodes on clean fixture; want >= 1 carrying %q", wantText)
	}
	found := false
	for _, d := range res.Decodes {
		if strings.TrimSpace(d.Text) == wantText {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("clean fixture decoded but %q not surfaced. Got: %v", wantText, decodeTexts(res.Decodes))
	}
}

// TestMultiPassDecodeFull_StrictGateRejectsToShadow proves the
// shadow-reject capture path. An impossible BP nsync threshold
// (22 > the 21 maximum) forces the gate to reject *every* BP-path
// decode regardless of signal quality, so a clean fixture's real
// decode shows up in .ShadowRejects instead of .Decodes.
//
// Asserts every field on the resulting ShadowReject is populated
// with a sensible value — the audit diagnostic is only useful if
// every field carries signal.
func TestMultiPassDecodeFull_StrictGateRejectsToShadow(t *testing.T) {
	audio, wantText := synthCleanFT8Audio(t, 1500.0)
	opts := sandbox.DefaultMultiPassOptions()
	// HardSyncScore tops out at 21 (3 Costas blocks × 7 symbols).
	// Setting MinNSyncBP to 22 makes any BP-path decode unconditionally
	// reject with "BP nsync N < 22". Pure CRC-lottery and OSD-path
	// decodes also fail the parallel MinNSyncOSD raised to 22.
	opts.Gate.MinNSyncBP = 22
	opts.Gate.MinNSyncOSD = 22

	res := sandbox.MultiPassDecodeFull(audio, opts, nil)

	if len(res.Decodes) != 0 {
		t.Errorf("strict gate should reject every decode; got %d in .Decodes: %v",
			len(res.Decodes), decodeTexts(res.Decodes))
	}
	if len(res.ShadowRejects) == 0 {
		t.Fatalf("strict gate rejected all decodes but .ShadowRejects is empty — capture path broken")
	}

	// Find the shadow reject carrying our truth text and verify
	// all diagnostic fields are populated.
	var truth *sandbox.ShadowReject
	for i := range res.ShadowRejects {
		if strings.TrimSpace(res.ShadowRejects[i].Text) == wantText {
			truth = &res.ShadowRejects[i]
			break
		}
	}
	if truth == nil {
		t.Fatalf("no shadow-reject carries truth text %q. Got: %v",
			wantText, shadowTexts(res.ShadowRejects))
	}

	// Reason must be non-empty and reference the nsync gate the test
	// rigged. AcceptDecode currently emits "BP nsync N < 22" or
	// "OSD nsync N < 22 (metric=...)"; either is a valid hit.
	if truth.Reason == "" {
		t.Errorf("ShadowReject.Reason: empty; want a non-empty diagnostic string")
	}
	if !strings.Contains(truth.Reason, "nsync") {
		t.Errorf("ShadowReject.Reason = %q; want a string mentioning nsync (the gate that fired)", truth.Reason)
	}
	if truth.NSync < 0 || truth.NSync > 21 {
		t.Errorf("ShadowReject.NSync = %d; want 0..21", truth.NSync)
	}
	if truth.ToneAgree < 0 || truth.ToneAgree > 79 {
		t.Errorf("ShadowReject.ToneAgree = %d; want 0..79", truth.ToneAgree)
	}
	// Clean fixture should have high tone agreement (>50) on the
	// truth-carrying candidate, even though the gate rejected it.
	if truth.ToneAgree < 50 {
		t.Errorf("ShadowReject.ToneAgree = %d on clean fixture; want >= 50 (truth-carrying candidate should match most tones)",
			truth.ToneAgree)
	}
	if truth.HardErrors < 0 || truth.HardErrors > sandbox.LDPCCodewordBits {
		t.Errorf("ShadowReject.HardErrors = %d; want 0..%d", truth.HardErrors, sandbox.LDPCCodewordBits)
	}
	if truth.Method != "BP" && !strings.HasPrefix(truth.Method, "OSD") {
		t.Errorf("ShadowReject.Method = %q; want \"BP\" or \"OSD-N\"", truth.Method)
	}
	switch truth.LLRMetric {
	case sandbox.LLRMetricN1, sandbox.LLRMetricN2, sandbox.LLRMetricN3,
		sandbox.LLRMetricN1Norm, sandbox.LLRMetricBestOfN:
	default:
		t.Errorf("ShadowReject.LLRMetric = %q; want one of the LLRMetric* constants", truth.LLRMetric)
	}
	if truth.FreqHz < 1400 || truth.FreqHz > 1600 {
		t.Errorf("ShadowReject.FreqHz = %f; truth at 1500 Hz, want ±100 Hz refined", truth.FreqHz)
	}
	if truth.Pass < 1 || truth.Pass > opts.MaxPasses {
		t.Errorf("ShadowReject.Pass = %d; want 1..%d", truth.Pass, opts.MaxPasses)
	}
	// Codeword: just verify it's non-zero — encode produces non-zero
	// codewords for any non-trivial message.
	nonZero := 0
	for _, b := range truth.Codeword {
		if b != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Errorf("ShadowReject.Codeword is all-zero; expected the encoded codeword")
	}
}

// TestMultiPassDecodeFull_BackwardsCompat pins the contract that
// MultiPassDecodeWithHashes returns the same .Decodes as
// MultiPassDecodeFull on the same input. Production callers that
// don't consume the shadow channel must see no behaviour change.
func TestMultiPassDecodeFull_BackwardsCompat(t *testing.T) {
	audio, _ := synthCleanFT8Audio(t, 1500.0)
	opts := sandbox.DefaultMultiPassOptions()

	viaWrapper := sandbox.MultiPassDecodeWithHashes(audio, opts, nil)
	viaFull := sandbox.MultiPassDecodeFull(audio, opts, nil)

	if len(viaWrapper) != len(viaFull.Decodes) {
		t.Fatalf("decode count diverged: WithHashes=%d, Full.Decodes=%d",
			len(viaWrapper), len(viaFull.Decodes))
	}
	for i := range viaWrapper {
		if viaWrapper[i].Text != viaFull.Decodes[i].Text {
			t.Errorf("decode[%d] text diverged: WithHashes=%q, Full=%q",
				i, viaWrapper[i].Text, viaFull.Decodes[i].Text)
		}
		if viaWrapper[i].FreqHz != viaFull.Decodes[i].FreqHz {
			t.Errorf("decode[%d] FreqHz diverged: WithHashes=%f, Full=%f",
				i, viaWrapper[i].FreqHz, viaFull.Decodes[i].FreqHz)
		}
	}
}

func decodeTexts(ds []sandbox.DecodeRecord) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Text
	}
	return out
}

func shadowTexts(rs []sandbox.ShadowReject) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Text
	}
	return out
}
