// Package candidates is the research-stage candidate finder. Find
// takes a 15-second 12 kHz mono FT8 audio buffer and returns detected
// sync candidates. The implementation is the subject of the new
// approach we're exploring; the CLI at research/cmd/find-candidates
// wires the harness around it.
//
// Imports stdlib + internal/audio only — by rule the research tree
// must not depend on internal/ft8/*. FT8 protocol constants are
// re-declared in this file.
package candidates

import (
	"math"
	"sort"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// FT8 protocol parameters per the QEX 2020 paper §4. Replicated here
// because the research tree is firewalled from internal/ft8/*.
const (
	fs    = 12000.0   // sample rate
	nsps  = 1920      // samples per symbol (160 ms at fs)
	nn    = 79        // total channel symbols per transmission
	nmax  = 180000    // samples per full 15-second FT8 slot (= 15 × fs)
	baud  = fs / nsps // 6.25 Hz — symbol rate and 8-FSK tone spacing
	nfft1 = 2 * nsps  // 3840-point spectrogram FFT → 3.125 Hz bins (freq oversampled 2×)
	nstep = nsps / 4  // 480-sample column step → 40 ms (time oversampled 4×)

	// Costas-array layout per QEX paper §4 — three blocks of seven
	// known-tone symbols at fixed channel-symbol positions 0..6,
	// 36..42, 72..78.
	numCostasBlocks       = 3
	costasSymbolsPerBlock = 7  // symbols per Costas block (== len(icos7))
	costasBlockStride     = 36 // channel-symbol stride between block starts

	// ft8ToneCount is the 8-FSK alphabet size (tones 0..7). Costas
	// anchors use only tones 0..6 (the icos7 alphabet); tone 7
	// appears only in data symbols, so at any Costas anchor position
	// tone 7's bin is guaranteed-silent of in-pattern energy — useful
	// for the matched filter's noise reference.
	ft8ToneCount = 8

	// Derived from the spectrogram shape: how many time-steps span
	// one symbol, and how many freq bins span one tone.
	stepsPerSymbol = nsps / nstep // 4
	freqOversample = nfft1 / nsps // 2

	// maxToneIdx is the largest FT8 tone index the matched filter
	// touches. Used by Find to keep centreBin + freqOversample*maxToneIdx
	// inside the spectrogram and by costasScore's bounds guard.
	maxToneIdx = ft8ToneCount - 1 // 7

	// expectedInSamples is the total in-pattern (centre, time-step)
	// cell count when every Costas anchor sits inside the spectrogram:
	// 3 blocks × 7 anchors × 4 time-steps each.
	expectedInSamples = numCostasBlocks * costasSymbolsPerBlock * stepsPerSymbol // 84

	// minInSamples is the floor under which costasScore refuses to
	// trust the in-pattern / out-of-pattern ratio — partial-block
	// matches near the slot edges can pump out high scores from very
	// few in-bounds cells. 50% of the full anchor grid is a
	// conservative cut that still admits this package's worst-case
	// in-search-range coverage (dtSteps = ±50 → 56 samples = 67%).
	minInSamples = expectedInSamples / 2 // 42
)

// icos7 is the FT8 7-tone Costas synchronisation pattern. The same
// sequence appears three times in each transmission: channel symbols
// 0-6, 36-42, 72-78.
var icos7 = [7]uint8{3, 1, 4, 0, 6, 5, 2}

// Candidate is one detected FT8 sync hit: where in the slot (Freq,
// DT), how strong (Score, the stage-1 matched-filter ratio), and
// — when stage-2 verification has run — the Costas-pattern
// verification record.
type Candidate struct {
	// Freq is the candidate centre frequency in Hz. Quantised to the
	// spectrogram bin spacing (fs / nfft1 = 3.125 Hz).
	Freq float64

	// DT is the time offset in seconds relative to the nominal 0.5 s
	// TX start, in the physical TX frame — i.e. the stage-1 raw DT
	// has had dtPhysicalOffsetSec added so a physically-on-time
	// signal reports DT = 0. Negative = early arrival, positive = late.
	DT float64

	// Score is the stage-1 matched-filter SNR ratio: mean in-pattern
	// Costas tone power / mean out-of-pattern tone power. Used as
	// the final tie-break in the stage-2 ranking.
	Score float64

	// Verify, when non-nil, carries the stage-2 Costas-pattern
	// verification record for this candidate. Find populates this
	// for every returned candidate.
	Verify *CostasVerify
}

// rawCandidate is the internal pipeline candidate used inside Find.
// It carries the spectrogram-grid coordinates (centreBin, rawDtSteps)
// as integers so dedup, NMS, and any other adjacency check work in
// exact integer space — no float-precision noise at the suppression
// boundary the way `math.Abs(Freq[i]-Freq[j]) <= 6.25` was hitting.
// At the API edge rawCandidate is converted to Candidate by
// multiplying out to physical (Freq, DT).
type rawCandidate struct {
	centreBin  int           // spectrogram freq bin index
	rawDtSteps int           // matched-filter raw dt step (pre-physical correction)
	score      float64       // stage-1 matched-filter ratio
	verify     *CostasVerify // populated by stage-2 verifier
}

// Stage-1 generator settings: a very low sanity floor and a generous
// topK cap. The threshold is just above the matched-filter noise
// floor (ratio = 1.0 means signal energy equals out-of-pattern
// energy); anything below is uncorrelated with the Costas pattern by
// construction. stage1TopK caps how many candidates feed into the
// stage-2 verifier — sized loose enough that all in-band signals
// down to the FT8 sensitivity floor pass through.
//
// Why 10000: real captures with HF noise produce ~12,000 stage-1
// candidates at s1 >= 1.5; the weakest reals in our 6-slot corpus
// rank in the 7,000-10,000 range. Empirically (perf round 1
// 2026-05-24) every step DOWN in topK costs real-capture recall —
// 7,500 lost 3 reals, 5,000 lost 6. Keep at 10,000 and chase
// speed via kernel optimisation (CGO + SIMD) rather than fewer
// verifies. Verify itself is the precision lever, so admitting
// more is the safe direction.
const (
	stage1ScoreThreshold = 1.0   // matched-filter ratio floor (sanity only)
	stage1TopK           = 10000 // verifier accepts at most this many per slot
)

// Stage-2 gate: categorical-only. WinsTotal + WinsPerBlock catch
// pattern-incoherent junk; the GeoContrast metric is retained as a
// RANKING signal (see the sort key below) rather than as a hard
// floor. The previous hard floor at GeoContrast >= 0.85 was
// non-monotonic with SNR: it rejected real -22 dB signals (geo
// ~0.81) while admitting strong random aliases at clean (geo
// 1.24). A floor that drifts the wrong way across operating
// conditions is worse than no floor — rely on ranking + topK to
// surface real signals and let downstream demod/LDPC reject any
// FPs that survive into the output tail.
const (
	sanityWinsTotal    = 8 // minimum WinsTotal out of 21 anchors
	sanityWinsPerBlock = 1 // minimum WinsBlock[b] out of 7 anchors each
)

// Final cap after ranking + NMS.
const stage2MaxResults = 100

// Gates parameterises the stage-1 and stage-2 gate floors. Find()
// uses DefaultGates which mirrors the production values; FindWithGates
// lets sweep harnesses drive these floors externally without
// touching production code. Adoption of any non-default Gates is
// gated on text-level decode measurement (matched up, extras stable
// or down, malformed == 0) — see research/cmd/sweep-gates.
//
// **Status (Session 92, 2026-05-25)**: every cell of the full
// (stage1 × wins × perBlock) cube measured identical to the default
// on real captures (matched=96, extras=2, malformed=0, CRC=98).
// Relaxation admits more candidates but no extra CRC passes — the
// finder-recall misses are decoder-bound, not gate-bound. Keep
// production at DefaultGates until the demod or LDPC paths change.
type Gates struct {
	// Stage1Threshold is the matched-filter SNR ratio floor at stage 1.
	// Candidates with score below this are discarded before stage-2
	// verification runs. Production default 1.0.
	Stage1Threshold float64

	// SanityWinsTotal is the stage-2 floor for total Costas-anchor wins
	// (out of 21). Candidates below this fail the gate. Production
	// default 8.
	SanityWinsTotal int

	// SanityWinsPerBlock is the stage-2 per-Costas-block wins floor
	// (out of 7 each). A candidate must hit this floor in every
	// accessible block. Production default 1.
	SanityWinsPerBlock int

	// Stage2MaxResults caps the number of candidates Find returns
	// after ranking + NMS. Production default 100. The cap-sweep
	// harness raises this to test whether the binding constraint
	// on parity is "rank > 100 real signals are dropped" — a
	// different question from gate relaxation. Set to 0 to use
	// the package default (matches DefaultGates behaviour).
	Stage2MaxResults int

	// RefinementMode controls how refineCandidate interacts with the
	// stage-2 categorical gate. Default behaviour preserved as the
	// zero value (RefinementDefault).
	//
	// Background: the stage-2 gate filters candidates pre-refinement
	// on WinsTotal + per-block wins. refineCandidate then optimises
	// GeoContrast in a 3×3 + 3×3 grid around each survivor. Per the
	// Session-96 measurement (`TestRefinementGate_DoesNotInvalidate`
	// in refinement_gate_test.go), 54.6% of returned candidates have
	// refined positions where the (newly recomputed) Verify no longer
	// passes the gate — refinement drifts to higher-GeoContrast but
	// lower-WinsTotal positions for marginal candidates.
	//
	// Three modes:
	//   - RefinementDefault: GeoContrast-only optimisation; can drift
	//     off-gate. Original Session-94 behaviour, kept for A/B.
	//   - RefinementGateAware: refine only within positions that still
	//     pass the gate; fall back to the pre-refine position if no
	//     gate-passing neighbour improves GeoContrast. Preserves the
	//     invariant that every returned candidate passes the gate.
	//   - RefinementStrictDrop: current refinement, then drop candidates
	//     whose refined Verify fails the gate. Filtering policy change,
	//     not just an invariant repair.
	RefinementMode RefinementMode
}

// RefinementMode is the enum for Gates.RefinementMode. See the field's
// doc-comment for the per-mode semantics and Session-96 measurement
// context.
type RefinementMode int

const (
	// RefinementDefault is the original GeoContrast-only refinement
	// (zero value, no change in default behaviour).
	RefinementDefault RefinementMode = iota
	// RefinementGateAware refines only within gate-passing positions.
	RefinementGateAware
	// RefinementStrictDrop drops candidates whose refined Verify fails the gate.
	RefinementStrictDrop
)

// passesCostasGate reports whether `v` satisfies the categorical gate
// at `gates` (WinsTotal floor + per-block wins floor with accessible-
// block exemption). Extracted from FindWithGates 2026-05-26 so the
// pre- and post-refinement gate-check paths cannot drift apart.
//
// The accessible-block exemption (per-block floor applies only to
// blocks where at least one anchor was accessible) is load-bearing
// for slot-edge candidates and must not be omitted.
func passesCostasGate(v *CostasVerify, gates Gates) bool {
	if v == nil {
		return false
	}
	if v.WinsTotal < gates.SanityWinsTotal {
		return false
	}
	for b := 0; b < numCostasBlocks; b++ {
		if v.AccessibleBlock[b] == 0 {
			continue
		}
		if v.WinsBlock[b] < gates.SanityWinsPerBlock {
			return false
		}
	}
	return true
}

// DefaultGates mirrors the production gate values. Find() uses this.
//
// Session 96 (2026-05-26): RefinementMode kept at RefinementDefault
// (zero value, GeoContrast-only refinement) after operator-directed
// A/B sweep. RefinementGateAware was prototyped and measured: +1
// matched / +3 extras at the production config (2-iter coherent-
// adaptive + LLR-softening) but -1 matched / -1 extras at the bare
// decode-eval baseline. Net trade-off config-dependent; the
// empirically-cleaner default was kept. RefinementGateAware and
// RefinementStrictDrop are exposed as A/B options via the gates
// field for future measurement runs.
var DefaultGates = Gates{
	Stage1Threshold:    stage1ScoreThreshold,
	SanityWinsTotal:    sanityWinsTotal,
	SanityWinsPerBlock: sanityWinsPerBlock,
	Stage2MaxResults:   stage2MaxResults,
}

// Find returns verified sync candidates detected in a 15-second FT8
// audio slot. Input must be 12 kHz mono float32 samples covering
// the full slot (180,000 samples). Every returned candidate has a
// non-nil Verify field.
//
// Pipeline:
//
//  1. Spectrogram (rectangular window, 3840-point real FFT, 480-sample step).
//  2. Stage-1 sweep over (centreBin, dtSteps): pooled-power Costas
//     matched-filter score from costasScore. NO non-max suppression
//     at this stage — sympathetic peaks survive into stage 2.
//  3. DT correction: each stage-1 DT is shifted by dtPhysicalOffsetSec
//     so downstream coordinates are in the physical TX frame.
//  4. Top-stage1TopK by stage-1 score cap going into stage 2.
//  5. Stage-2 verification: per-symbol DTFT (Goertzel-batched 8-tone)
//     at each Costas anchor → win counts + log contrast.
//  6. Categorical gate: WinsTotal ≥ sanityWinsTotal AND every
//     WinsBlock[b] ≥ sanityWinsPerBlock. GeoContrast steers the
//     ranking, not the gate.
//  7. Ranked tie-break: GeoContrast desc → WinsTotal desc →
//     MinBlockContrast desc → Stage1Score desc.
//  8. Final non-max suppression in integer (centreBin, rawDtSteps) space.
//  9. Local refinement of survivors via refineCandidate (sub-bin freq
//     and sub-step DT).
//
// 10. Cap to stage2MaxResults.
func Find(samples []float32) []Candidate {
	return FindWithGates(samples, DefaultGates)
}

// FindWithGates is the gate-parameterised variant of Find. The pipeline
// is identical except the three gate floors come from the supplied
// Gates rather than the package defaults. Used by sweep harnesses;
// production callers should use Find() directly.
func FindWithGates(samples []float32, gates Gates) []Candidate {
	// Require the documented 15-second slot. The stage-2 verifier
	// indexes audio up to txStart + nn*nsps where txStart = 6000 +
	// dt*fs, so any caller passing a buffer shorter than nmax would
	// see every candidate silently rejected inside verifyCostas
	// (and Find return an empty list with no diagnostic). Enforce
	// the documented contract instead.
	if len(samples) < nmax {
		return nil
	}
	spec := spectrogram(samples)
	if len(spec) == 0 {
		return nil
	}

	const (
		freqLowHz       = 200.0
		freqHighHz      = 2900.0
		searchHalfSpanS = 2.5

		df    = fs / nfft1          // 3.125 Hz per freq bin
		tstep = float64(nstep) / fs // 0.04 s per time column
	)

	nominalStartStep := int(math.Floor(0.5 / tstep))

	// Compute raw dt-step search bounds so the PHYSICAL DT range is
	// symmetric at ±searchHalfSpanS. The matched-filter peak sits
	// dtPhysicalOffsetSteps steps below physical zero (see
	// verify.go for the structural reason), so the raw loop bounds
	// must shift by that amount or the physical range becomes
	// asymmetric (e.g. [-1.88, +2.12] s for the previous symmetric
	// raw [-50, +50] loop).
	halfSpanSteps := int(math.Round(searchHalfSpanS / tstep))
	dtStepsMin := -halfSpanSteps - dtPhysicalOffsetSteps // raw step at physical -searchHalfSpanS
	dtStepsMax := halfSpanSteps - dtPhysicalOffsetSteps  // raw step at physical +searchHalfSpanS

	halfFFT := len(spec[0])
	binLow := int(math.Round(freqLowHz / df))
	if binLow < 0 {
		binLow = 0
	}
	binHigh := int(math.Round(freqHighHz / df))
	if binHigh+freqOversample*maxToneIdx >= halfFFT {
		binHigh = halfFFT - freqOversample*maxToneIdx - 1
	}
	if binHigh < binLow {
		return nil
	}

	// ---- Stage 1: spectrogram sweep, no NMS. ----
	var stage1 []rawCandidate
	for centreBin := binLow; centreBin <= binHigh; centreBin++ {
		for dtSteps := dtStepsMin; dtSteps <= dtStepsMax; dtSteps++ {
			score := costasScore(spec, centreBin, dtSteps, nominalStartStep)
			if score >= gates.Stage1Threshold {
				stage1 = append(stage1, rawCandidate{
					centreBin:  centreBin,
					rawDtSteps: dtSteps,
					score:      score,
				})
			}
		}
	}
	if len(stage1) == 0 {
		return nil
	}

	// Cap stage-1 set going into verification, descending by stage-1 score.
	sort.Slice(stage1, func(i, j int) bool {
		return stage1[i].score > stage1[j].score
	})
	if len(stage1) > stage1TopK {
		stage1 = stage1[:stage1TopK]
	}

	// ---- Stage 2: per-anchor Costas verification. ----
	verified := make([]rawCandidate, 0, len(stage1))
	for _, c := range stage1 {
		freq := float64(c.centreBin) * df
		physicalDT := float64(c.rawDtSteps)*tstep + dtPhysicalOffsetSec
		v := verifyCostas(samples, freq, physicalDT, c.score)
		// Categorical gate only. Pattern-incoherent junk gets cut
		// here; GeoContrast steers the ranking (sort key below)
		// rather than acting as a hard floor.
		if !passesCostasGate(&v, gates) {
			continue
		}
		vCopy := v
		c.verify = &vCopy
		verified = append(verified, c)
	}
	if len(verified) == 0 {
		return nil
	}

	// ---- Rank by verifier metrics. ----
	sort.Slice(verified, func(i, j int) bool {
		a, b := verified[i].verify, verified[j].verify
		if a.GeoContrast != b.GeoContrast {
			return a.GeoContrast > b.GeoContrast
		}
		if a.WinsTotal != b.WinsTotal {
			return a.WinsTotal > b.WinsTotal
		}
		if a.MinBlockContrast != b.MinBlockContrast {
			return a.MinBlockContrast > b.MinBlockContrast
		}
		return a.Stage1Score > b.Stage1Score
	})

	// ---- Final NMS in INTEGER bin/step space. ----
	//
	// Comparing centreBin and rawDtSteps as integers removes the
	// float-boundary precision issue that earlier blocked the
	// 2-step suppression window. Keeping the time suppression at 3
	// steps for now (this commit isolates the precision fix; width
	// retuning is a separate concern). The 2-bin freq window
	// dedupes adjacent-bin grid aliases of the same strong signal
	// — its sidelobes leak ±2 bins under the rectangular window
	// and show high gate metrics at those neighbours.
	//
	// Session 92 (2026-05-25) tested ±1 bin to see if it would
	// recover two suspected PASS_GATE-pair misses (PE1NPS / HG60IPA
	// at 2253 Hz vs OM3DX at 2259 Hz, two truth bins apart). It did
	// not: the suppressor sits 1 bin from the target's best
	// in-tolerance lattice point, which is itself a sidelobe of
	// the OM3DX signal — the weak signals (PE1NPS / HG60IPA)
	// genuinely have wins < 8 at their own bin (721), so the
	// problem is WINS_LOW at the real-signal bin, not over-wide
	// NMS. Test result: matched flat 96→96, text-extra +1, CRC +1
	// (extra near-bin candidate decoded to garbage). Reverted.
	const (
		freqSuppressBins  = 2 // ±2 spectrogram bins (= 6.25 Hz)
		timeSuppressSteps = 3 // ±3 spectrogram steps (= 120 ms)
	)
	keep := make([]bool, len(verified))
	for i := range keep {
		keep[i] = true
	}
	for i := 0; i < len(verified); i++ {
		if !keep[i] {
			continue
		}
		for j := i + 1; j < len(verified); j++ {
			if !keep[j] {
				continue
			}
			dbin := verified[i].centreBin - verified[j].centreBin
			if dbin < 0 {
				dbin = -dbin
			}
			dstep := verified[i].rawDtSteps - verified[j].rawDtSteps
			if dstep < 0 {
				dstep = -dstep
			}
			if dbin <= freqSuppressBins && dstep <= timeSuppressSteps {
				keep[j] = false
			}
		}
	}

	// ---- Refine survivors locally; convert to public type; cap. ----
	//
	// Each post-NMS survivor is the spectrogram-quantised position of
	// a candidate. Stage-2 verifier was evaluated at that quantised
	// coordinate, but the actual signal in real captures sits at a
	// sub-bin/sub-step offset. refineCandidate walks a small (df,
	// ddt) grid around the supplied position and returns the
	// physical (freq, dt) where GeoContrast peaks, along with the
	// updated Verify record at that position.
	//
	// For our synthetic on-grid fixtures the peak is at the supplied
	// position already, so refinement is a no-op for them. The
	// per-survivor cost (~17 verify calls) is fine: refinement runs
	// only on at most gates.Stage2MaxResults candidates, post-gate,
	// post-NMS.
	maxResults := gates.Stage2MaxResults
	if maxResults <= 0 {
		maxResults = stage2MaxResults
	}
	out := make([]Candidate, 0, maxResults)
	for i, c := range verified {
		if !keep[i] {
			continue
		}
		initialFreq := float64(c.centreBin) * df
		initialDT := float64(c.rawDtSteps)*tstep + dtPhysicalOffsetSec
		refFreq, refDT, refV := refineCandidate(samples, initialFreq, initialDT, c.score, gates)

		// RefinementStrictDrop policy: post-refinement gate filter.
		// If the refined Verify no longer satisfies the gate that
		// admitted this candidate pre-refinement, drop it entirely.
		// Distinct from RefinementGateAware (which keeps the candidate
		// but rolls back to the gate-passing initial position when no
		// refined neighbour improves under gate). Per Session-96
		// 2026-05-26 design discussion.
		if gates.RefinementMode == RefinementStrictDrop && !passesCostasGate(&refV, gates) {
			continue
		}

		refVCopy := refV
		out = append(out, Candidate{
			Freq:   refFreq,
			DT:     refDT,
			Score:  c.score,
			Verify: &refVCopy,
		})
		if len(out) >= maxResults {
			break
		}
	}
	return out
}

// spectrogram returns spec[t][f] = |X[t][f]|² for time columns at
// nstep-sample intervals and frequency bins [0, nfft1/2). The
// Nyquist bin (X[nfft1/2]) is intentionally dropped — for our FT8
// use case the candidate sweep tops out at 2900 Hz (bin ~928 of
// 1920) so the Nyquist bin at 6000 Hz carries no signal of interest.
// Matches the NH1 = NFFT1/2 convention used by SM's dsp.Sync.
//
// Input contract: callers must supply at least nfft1 samples.
// `candidates.Find` already pre-validates len(samples) >= nn*nsps
// before calling, so nCols ≥ 1 is guaranteed in production use; the
// nil return below is a defensive fallback for direct callers.
//
// Windowing: rectangular (no window). Sidelobe leakage at -13 dB
// from the strongest bin is acceptable for FT8 Costas matched-filter
// scoring because the other six Costas tones live two bins away and
// don't sit inside the first sidelobe. If false positives from
// sidelobe leakage ever become an issue, swap in a Hann/Hamming
// window here.
//
// Allocation shape: spec rows back onto one contiguous slice, so the
// 367 × 1920 = ~706k float64 cells live in a single heap object
// instead of 367 separate ones — better cache locality, fewer GC
// roots. The per-call plan.FFT(chunk) still allocates internally
// (two slices per call; see audio.RealPlan); we don't fix that here.
func spectrogram(samples []float32) [][]float64 {
	plan := audio.NewRealPlan(nfft1)
	halfFFT := nfft1 / 2

	nCols := 0
	for s := 0; s+nfft1 <= len(samples); s += nstep {
		nCols++
	}
	if nCols == 0 {
		return nil
	}

	backing := make([]float64, nCols*halfFFT)
	spec := make([][]float64, nCols)
	chunk := make([]float32, nfft1)
	// chunk is reused across all nCols calls. Safe because each
	// iteration's copy() fills exactly nfft1 samples — no stale
	// tail leaks into a later FFT. If we ever allow partial
	// zero-padded final frames, clear chunk's tail before that call.
	for t := 0; t < nCols; t++ {
		copy(chunk, samples[t*nstep:t*nstep+nfft1])
		X := plan.FFT(chunk)
		row := backing[t*halfFFT : (t+1)*halfFFT]
		for f := 0; f < halfFFT; f++ {
			re, im := real(X[f]), imag(X[f])
			row[f] = re*re + im*im
		}
		spec[t] = row
	}
	return spec
}

// costasScore returns the matched-filter SNR ratio for one candidate
// at (centreBin, dtSteps): power averaged across the 21 in-bounds
// Costas anchor (symbol, tone) positions, divided by power averaged
// across the corresponding out-of-pattern (symbol, other-tone)
// positions at the same time slots. The "other" tones span all 8
// FT8 8-FSK alphabet tones except the anchor's expected one — at
// Costas anchor positions tones outside the icos7 set (in particular
// tone 7) are guaranteed no in-pattern energy, giving a cleaner
// noise reference than restricting to icos7 alone.
//
// **Input contract.** spec must be a rectangular grid — every row
// the same length, len ≥ centreBin + freqOversample*maxToneIdx + 1.
// centreBin must be ≥ 0. Find enforces both via its binLow/binHigh
// bracketing AND its sole producer (spectrogram in this file)
// which writes uniform-width rows by construction. The width check
// below validates spec[0] only — it is consistency with the
// spectrogram producer, NOT a defensive guard against ragged input
// from other callers. A ragged spec would still panic on a
// shorter-than-spec[0] row.
//
// **Slot-edge handling.** Anchor cells whose time slot falls outside
// the spectrogram are skipped — graceful degradation at the slot
// edges. Below the minInSamples coverage floor the function returns
// 0 to prevent a tiny number of in-bounds cells from producing a
// high-but-meaningless ratio.
func costasScore(spec [][]float64, centreBin, dtSteps, nominalStartStep int) float64 {
	if len(spec) == 0 || len(spec[0]) == 0 {
		return 0
	}
	if centreBin < 0 {
		return 0
	}
	if centreBin+freqOversample*maxToneIdx >= len(spec[0]) {
		return 0
	}

	nCols := len(spec)
	var inSum, noiseSum float64
	var inCount, noiseCount int

	for block := 0; block < numCostasBlocks; block++ {
		blockStartSym := block * costasBlockStride
		for sym := 0; sym < costasSymbolsPerBlock; sym++ {
			channelSym := blockStartSym + sym
			tStart := dtSteps + nominalStartStep + channelSym*stepsPerSymbol
			expectedTone := int(icos7[sym])
			inBin := centreBin + freqOversample*expectedTone

			for step := 0; step < stepsPerSymbol; step++ {
				t := tStart + step
				if t < 0 || t >= nCols {
					continue
				}
				row := spec[t]
				inSum += row[inBin]
				inCount++
				for k := 0; k < ft8ToneCount; k++ {
					if k == expectedTone {
						continue
					}
					otherBin := centreBin + freqOversample*k
					noiseSum += row[otherBin]
					noiseCount++
				}
			}
		}
	}

	if inCount < minInSamples || noiseCount == 0 {
		return 0
	}
	inMean := inSum / float64(inCount)
	noiseMean := noiseSum / float64(noiseCount)
	if noiseMean <= 0 {
		return 0
	}
	return inMean / noiseMean
}
