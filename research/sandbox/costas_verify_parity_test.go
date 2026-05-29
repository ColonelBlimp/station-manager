package sandbox_test

// **Scope.** This is a RESEARCH AUDIT, not a shipping clean-room
// contract. It compares the sandbox's independently-derived
// VerifyCostasAt with the `research/candidates` package's
// verifier on the same audio + coordinate. Both implementations were
// independently derived from QEX § 4 + Goertzel 1958, so they should
// produce the same numeric outputs — and on the strict corpus they
// agree bit-exactly (1e-9 relative).
//
// **Boundary caveat.** A shipped clean-room MIT FT8 library that
// uses this verifier MUST justify its provenance from QEX / Goertzel
// + its own tests alone — importing `research/candidates` (or any
// other library whose clean-room status isn't independently
// verified) into a production build would blur the boundary. This
// test is fine in the research tree because the research tree is
// firewalled from `internal/ft8/`; do not promote it into the
// production package's test suite without first auditing the
// `candidates` provenance and either copying the verifier into
// production or replacing this test with a spec-vector test
// (synthesised Costas patterns + analytically-known outputs).
//
// If this test ever starts failing it means one of the two
// implementations drifted from the QEX-described idea, not that they
// disagree about it — both should be brought back into spec, the
// failing one corrected against the paper.

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/candidates"
	"github.com/ColonelBlimp/station-manager/research/sandbox"
)

func TestStage2VerifierParityWithCandidates(t *testing.T) {
	wavPath := filepath.Join("..", "..", "captures", "20m_slot1.wav")
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Skipf("read %q: %v (corpus not present)", wavPath, err)
	}
	if data.SampleRate != 12000 || data.Channels != 1 {
		t.Fatalf("%q: sample rate %d Hz, %d channels — want 12000 mono", wavPath, data.SampleRate, data.Channels)
	}

	spec := sandbox.Spectrogram(data.Samples)
	cands := sandbox.FindCandidates(spec, sandbox.DefaultSearchOptions())
	if len(cands) == 0 {
		t.Fatalf("no candidates produced by sandbox finder on %q", wavPath)
	}

	const winsRatioTol = 0.0 // categorical: must agree exactly
	const contrastRelTol = 1e-9

	var mismatches int
	for i, c := range cands {
		sb := sandbox.VerifyCostasAt(data.Samples, c.FreqHz, c.DtSec, c.Sync)
		cv := candidates.VerifyCostas(data.Samples, c.FreqHz, c.DtSec, c.Sync)
		if sb.WinsTotal != cv.WinsTotal {
			mismatches++
			t.Errorf("cand %d (%.1f Hz, %+.3fs): WinsTotal sandbox=%d candidates=%d", i, c.FreqHz, c.DtSec, sb.WinsTotal, cv.WinsTotal)
		}
		if !relClose(sb.GeoContrast, cv.GeoContrast, contrastRelTol) {
			mismatches++
			t.Errorf("cand %d (%.1f Hz, %+.3fs): GeoContrast sandbox=%.6g candidates=%.6g", i, c.FreqHz, c.DtSec, sb.GeoContrast, cv.GeoContrast)
		}
		if !relClose(sb.MinBlockContrast, cv.MinBlockContrast, contrastRelTol) {
			mismatches++
			t.Errorf("cand %d (%.1f Hz, %+.3fs): MinBlockContrast sandbox=%.6g candidates=%.6g", i, c.FreqHz, c.DtSec, sb.MinBlockContrast, cv.MinBlockContrast)
		}
		if mismatches > 5 {
			t.Fatalf("aborting after %d mismatches", mismatches)
		}
		_ = winsRatioTol
	}
	t.Logf("verified %d candidates: WinsTotal exact match, GeoContrast / MinBlockContrast match within %g relative", len(cands), contrastRelTol)
}

func relClose(a, b, tol float64) bool {
	if a == b {
		return true
	}
	denom := math.Max(math.Abs(a), math.Abs(b))
	if denom == 0 {
		return true
	}
	return math.Abs(a-b)/denom <= tol
}
