package sandbox

import (
	"fmt"
	"strings"
)

// AcceptDecodeOptions tunes the post-decode quality gate. The gate
// runs AFTER BPDecode + Unpack77 succeed; it rejects accept-by-CRC
// codewords that don't have the additional hallmarks of a real
// FT8 signal (tone alignment, plausible SNR, plausible bit-flip
// distance from the channel hard decision).
//
// Two stricter knobs apply only when the decode came through OSD:
// MinNSyncOSD (typically higher than MinNSyncBP) and MinToneAgreeOSD
// (a reconstructed-tone consistency check). BP-only decodes already
// have BP's internal acceptance test (syndrome + CRC + soft-distance);
// OSD enumerates many candidates and is more exposed to CRC lottery
// wins, so it earns extra scrutiny.
type AcceptDecodeOptions struct {
	// MinNSyncBP is the minimum HardSyncScore (0..21) accepted for a
	// BP-path decode. Truth signals on noisy fixtures typically
	// score 13+; setting this to 8 lets BP through anything plausibly
	// aligned while rejecting Costas-pattern noise (random ~5-8).
	MinNSyncBP int

	// MinNSyncOSD is the stricter minimum HardSyncScore for OSD-path
	// decodes. Real OSD decodes on the truth fixtures we have score
	// 13+; setting this to 11 leaves a small margin while rejecting
	// the random-codeword Costas-mismatch artefact.
	MinNSyncOSD int

	// MinToneAgreeOSD is the minimum count (out of 79) of symbol
	// positions where the codeword's reconstructed tone equals the
	// SymbolGrid argmax. A real decode matches ~60-79 positions; a
	// random codeword matches ~10 (one in eight). Threshold 50
	// cleanly separates on the fixtures.
	MinToneAgreeOSD int

	// MinSNR2500DB is the minimum measured SNR (WSJT-X 2500 Hz
	// reference) for either path. Below this, the candidate is
	// rejected as noise. Set to -math.Inf for "no SNR check".
	MinSNR2500DB float64

	// MaxHardErrors caps the Hamming distance between the decoded
	// codeword and the channel's hard decision. A real decode at
	// FT8 threshold corrects 5-15 bits; >36 (about 20% of the 174
	// codeword bits) suggests BP wandered to a far codeword that's
	// unlikely to be the truth even if it passes CRC.
	MaxHardErrors int
}

// DefaultAcceptDecodeOptions returns the baseline tuning, calibrated
// against the 10cq + overlap fixture set:
//
//   - MinNSyncBP = 8 — BP-success truth on every fixture comes in
//     at 13+. A few false positives at 7-8 may slip through; OSD
//     gate plus tone-agreement catches them.
//
//   - MinNSyncOSD = 11 — OSD-success truth on SNR-20dB scored as
//     low as 13. Leaves ~2-point margin.
//
//   - MinToneAgreeOSD = 50 — random codewords agree on ~10/79
//     positions; real decodes ~60+. 50 is comfortably in the gap.
//
//   - MinSNR2500DB = -25.0 — below the documented FT8 operational
//     threshold; anything below is noise.
//
//   - MaxHardErrors = 36 — 20% of 174 bits.
func DefaultAcceptDecodeOptions() AcceptDecodeOptions {
	return AcceptDecodeOptions{
		MinNSyncBP:      8,
		MinNSyncOSD:     11,
		MinToneAgreeOSD: 50,
		MinSNR2500DB:    -25.0,
		MaxHardErrors:   36,
	}
}

// AcceptDecode runs the quality gate. Returns (accepted, reason).
// reason is "" when accepted; a one-line diagnostic when rejected.
//
// Inputs:
//
//   - method: BPResult.DecodeMethod ("BP", "OSD-N", "fail")
//   - nsync: HardSyncScore(grid) — Costas tone-agreement count, 0..21
//   - grid: the SymbolGrid, used for the OSD tone-agreement check
//   - cw: the decoded 174-bit codeword
//   - hardErrors: HammingDistance(cw, sign(llrs))
//   - snrDB: MeasureSNRPerSymbol(...).SNR2500DB
//   - opts: the AcceptDecodeOptions thresholds
func AcceptDecode(
	method string,
	nsync int,
	grid *SymbolGrid,
	cw [LDPCCodewordBits]uint8,
	hardErrors int,
	snrDB float64,
	opts AcceptDecodeOptions,
) (bool, string) {
	if hardErrors < 0 || hardErrors > opts.MaxHardErrors {
		return false, fmt.Sprintf("hard-errors %d > %d", hardErrors, opts.MaxHardErrors)
	}
	if snrDB < opts.MinSNR2500DB {
		return false, fmt.Sprintf("snr %.1f dB < %.1f dB", snrDB, opts.MinSNR2500DB)
	}
	isOSD := strings.HasPrefix(method, "OSD")
	if isOSD {
		if nsync < opts.MinNSyncOSD {
			return false, fmt.Sprintf("OSD nsync %d < %d", nsync, opts.MinNSyncOSD)
		}
		toneAgree := ToneAgreementCount(cw, grid)
		if toneAgree < opts.MinToneAgreeOSD {
			return false, fmt.Sprintf("OSD tone-agree %d/79 < %d", toneAgree, opts.MinToneAgreeOSD)
		}
	} else {
		if nsync < opts.MinNSyncBP {
			return false, fmt.Sprintf("BP nsync %d < %d", nsync, opts.MinNSyncBP)
		}
	}
	return true, ""
}

// ToneAgreementCount returns the count (0..79) of symbol positions
// where the codeword's reconstructed tone equals the SymbolGrid
// argmax. Costas positions are included — the encoder writes literal
// Costas tones [3,1,4,0,6,5,2] so on a true decode they should match
// the argmax just like HardSyncScore measures; data positions
// additionally check the LDPC-encoded data tones against the grid.
func ToneAgreementCount(cw [LDPCCodewordBits]uint8, grid *SymbolGrid) int {
	tones := CodewordToTones(cw)
	n := 0
	for s := 0; s < ft8SymbolCount; s++ {
		max := 0
		maxV := grid.Tones[s][0]
		for m := 1; m < ft8TonesPerSymbol; m++ {
			if grid.Tones[s][m] > maxV {
				maxV = grid.Tones[s][m]
				max = m
			}
		}
		if max == tones[s] {
			n++
		}
	}
	return n
}

// HardErrorsCount returns the Hamming distance between cw and the
// channel's hard decision (sign of LLR per bit). Positive LLR ⇒ bit 0
// by convention; cw[v] != 0 when llrs[v] < 0 is the agreement case.
func HardErrorsCount(cw [LDPCCodewordBits]uint8, llrs [LDPCCodewordBits]float64) int {
	n := 0
	for v := 0; v < LDPCCodewordBits; v++ {
		var hard uint8
		if llrs[v] < 0 {
			hard = 1
		}
		if cw[v] != hard {
			n++
		}
	}
	return n
}
