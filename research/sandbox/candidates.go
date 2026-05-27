package sandbox

import (
	"sort"
)

// FT8 Costas synchronisation array per QEX 2020 paper §2. Three copies
// of this 7-symbol sequence anchor the start, middle, and end of every
// FT8 transmission, at symbol indices 0, 36, and 72 of the 79-symbol
// frame. The matched filter sums power at the 21 expected (frame, bin)
// cells; a high relative score is the signature of an FT8 candidate
// regardless of the message bits in the data symbols.
var costasArray = [7]int{3, 1, 4, 0, 6, 5, 2}

// costasBlockStarts gives the symbol index of each Costas block in
// the 79-symbol message: symbols 0-6, 36-42, 72-78.
var costasBlockStarts = [3]int{0, 36, 72}

// Sandbox spectrogram-to-FT8 scale factors:
//   - framesPerSymbol = 4: each 160 ms FT8 symbol covers four 40 ms
//     spectrogram frames (since nstep = nsps/4).
//   - binsPerTone = 2: each 6.25 Hz FT8 tone step covers two 3.125 Hz
//     spectrogram bins (since nfft = 2*nsps).
//   - costasSpan = (totalSymbols - 1) * framesPerSymbol = 312 frames
//     from symbol 0 start to symbol 78 start. The last Costas block
//     starts at symbol 72, so we only need (72+6)*4 = 312 frames of
//     spectrogram beyond the candidate's dtFrame.
//   - toneSpanBins = 7 * binsPerTone = 14 bins from tone 0 to tone 7.
const (
	totalSymbols     = 79
	framesPerSymbol  = nsps / nstep // = 4
	binsPerTone      = nfft / nsps  // = 2
	costasSpanFrames = (totalSymbols - 1) * framesPerSymbol
	toneSpanBins     = 7 * binsPerTone

	// nominalStartSec is the WSJT-X convention: an "on schedule" FT8
	// transmission starts 0.5 seconds into the 15-second slot. Truth
	// manifests record dt as a signed offset *from this nominal start*,
	// not from the start of the audio buffer, so candidate dt is
	// reported the same way.
	nominalStartSec = 0.5
)

// Candidate is one (freq, dt) hit from the matched filter, optionally
// refined to sub-bin freq and sub-frame dt by RefineCandidate.
//
// FreqHz and DtSec always carry the current best estimate: equal to
// the coarse spectrogram-snapped values until RefineCandidate runs,
// then updated to the channelizer-refined values. The Coarse* fields
// preserve the pre-refinement estimate so callers can measure the
// drift the refinement step applied.
type Candidate struct {
	// FreqHz is the candidate's tone-0 frequency in Hz, per the WSJT-X
	// convention (the FT8 "frequency" is the lowest tone).
	FreqHz float64

	// DtSec is the time of symbol-0-start relative to the start of the
	// supplied audio buffer, in seconds.
	DtSec float64

	// Sync is the Costas matched-filter score at this candidate's 21
	// anchor cells. For coarse candidates, normalised by 21 × global-
	// median spectrogram noise power; for refined candidates, the
	// post-refinement baseband matched-filter score (different scale).
	Sync float64

	// CoarseFreqHz is the spectrogram-snapped freq before refinement.
	CoarseFreqHz float64

	// CoarseDtSec is the spectrogram-snapped dt before refinement.
	CoarseDtSec float64

	// Refined is true once RefineCandidate has run on this candidate.
	Refined bool
}

// SearchOptions tunes the candidate scanner. Zero-value fields are
// replaced with their defaults — see DefaultSearchOptions for the
// active values.
type SearchOptions struct {
	// MinFreqHz / MaxFreqHz constrain the tone-0 frequency search
	// window. FT8 captures usually run 100 Hz – 3000 Hz. Bins below
	// MinFreqHz / above MaxFreqHz are not scanned.
	MinFreqHz float64
	MaxFreqHz float64

	// MaxDTSec caps the dt search window (symbol-0 start time relative
	// to the audio buffer start). Default 2.0 s; clipped if the
	// spectrogram doesn't extend that far.
	MaxDTSec float64

	// Threshold is the minimum score (relative to global-median noise)
	// for a cell to be considered a candidate.
	Threshold float64

	// MaxResults caps the returned candidate count after NMS.
	MaxResults int

	// NMSFreqHz / NMSDTSec define the suppression neighbourhood: when
	// a cell is accepted, all lower-scoring cells within this
	// freq/time box of it are dropped. Prevents the same signal from
	// claiming multiple adjacent candidates.
	NMSFreqHz float64
	NMSDTSec  float64
}

// DefaultSearchOptions returns the baseline tuning used when zero-value
// fields appear in a SearchOptions struct (or when nil is passed).
//
// Threshold defaults to 3.0: signal-band sum must be ≥3× the median-
// noise sum. On clean synthetic fixtures this fires at scores in the
// 10×–1000× range; on real captures the typical FT8 signal scores
// 5×–50× depending on SNR.
//
// NMSFreqHz = 12.5 Hz suppresses any lower-scoring cell within ±4 bins
// (twice the tone spacing); NMSDTSec = 0.12 s = 3 spectrogram frames
// (one full symbol width minus an epsilon — wider than the worst-case
// mainlobe-leakage ±2-frame twin, narrower than the symbol duration
// so distinct co-channel transmissions don't merge).
func DefaultSearchOptions() SearchOptions {
	return SearchOptions{
		MinFreqHz:  100,
		MaxFreqHz:  3000,
		MaxDTSec:   2.0,
		Threshold:  3.0,
		MaxResults: 50,
		NMSFreqHz:  12.5,
		NMSDTSec:   0.12,
	}
}

// FindCandidates runs the Costas matched filter across spec and
// returns the top-MaxResults candidates above Threshold, after
// non-maximum suppression.
//
// Geometry: each FT8 symbol occupies framesPerSymbol = 4 spectrogram
// frames; each FT8 tone occupies binsPerTone = 2 spectrogram bins.
// The matched filter sums spec[dtFrame + (block + k)*4][freqBin +
// costas[k]*2] across the three Costas blocks (block ∈ {0, 36, 72}
// and k ∈ [0, 7)).
//
// Scoring uses a single spectrogram frame per symbol (the frame at
// the symbol's start). Integrating across all four sub-frames of a
// symbol roughly multiplies signal + noise by the same factor and is
// not adopted here — the per-symbol-start sum is the simpler and
// equally well-conditioned choice.
//
// Returns an empty slice if the spectrogram is too short to contain
// even one Costas window.
func FindCandidates(spec [][]float64, opts SearchOptions) []Candidate {
	opts = applyOptionDefaults(opts)

	if len(spec) <= costasSpanFrames {
		return nil
	}
	halfFFT := len(spec[0])

	// Search range in spectrogram coordinates.
	minBin := int(opts.MinFreqHz * nfft / fs)
	maxBin := int(opts.MaxFreqHz * nfft / fs)
	if minBin < 0 {
		minBin = 0
	}
	if maxBin+toneSpanBins >= halfFFT {
		maxBin = halfFFT - toneSpanBins - 1
	}
	maxDTFrame := int(opts.MaxDTSec * fs / nstep)
	if maxDTFrame+costasSpanFrames >= len(spec) {
		maxDTFrame = len(spec) - costasSpanFrames - 1
	}
	if maxDTFrame < 0 || minBin >= maxBin {
		return nil
	}

	// Global median noise estimate. Sampling every 19th cell over the
	// whole spectrogram keeps the cost negligible (~37k samples for a
	// 372×1920 spec) while giving a stable estimate. The stride 19 is
	// coprime with nfft/2 = 1920 so it doesn't alias to a regular
	// frequency grid.
	median := estimateMedianPower(spec, 19)
	if median <= 0 {
		// Pathological: completely silent spectrogram. Skip rather than
		// divide by zero — there are no candidates to find.
		return nil
	}
	normFactor := float64(len(costasBlockStarts)*len(costasArray)) * median

	// Score every (freqBin, dtFrame) in the search window. We allocate
	// up-front rather than streaming because we need a global sort for
	// NMS — the time savings of streaming are not worth the bookkeeping.
	var raw []Candidate
	for dtFrame := 0; dtFrame <= maxDTFrame; dtFrame++ {
		for freqBin := minBin; freqBin <= maxBin; freqBin++ {
			sum := 0.0
			for _, blockStart := range costasBlockStarts {
				for k := 0; k < 7; k++ {
					symFrame := dtFrame + (blockStart+k)*framesPerSymbol
					toneBin := freqBin + costasArray[k]*binsPerTone
					sum += spec[symFrame][toneBin]
				}
			}
			score := sum / normFactor
			if score >= opts.Threshold {
				freqHz := float64(freqBin) * fs / nfft
				dtSec := float64(dtFrame)*float64(nstep)/fs - nominalStartSec
				raw = append(raw, Candidate{
					FreqHz:       freqHz,
					DtSec:        dtSec,
					Sync:         score,
					CoarseFreqHz: freqHz,
					CoarseDtSec:  dtSec,
				})
			}
		}
	}

	sort.Slice(raw, func(i, j int) bool { return raw[i].Sync > raw[j].Sync })

	return suppressOverlaps(raw, opts.NMSFreqHz, opts.NMSDTSec, opts.MaxResults)
}

// applyOptionDefaults replaces zero-value fields with the values from
// DefaultSearchOptions. A caller passing a partly-filled struct keeps
// any non-zero field they set.
func applyOptionDefaults(opts SearchOptions) SearchOptions {
	d := DefaultSearchOptions()
	if opts.MinFreqHz == 0 {
		opts.MinFreqHz = d.MinFreqHz
	}
	if opts.MaxFreqHz == 0 {
		opts.MaxFreqHz = d.MaxFreqHz
	}
	if opts.MaxDTSec == 0 {
		opts.MaxDTSec = d.MaxDTSec
	}
	if opts.Threshold == 0 {
		opts.Threshold = d.Threshold
	}
	if opts.MaxResults == 0 {
		opts.MaxResults = d.MaxResults
	}
	if opts.NMSFreqHz == 0 {
		opts.NMSFreqHz = d.NMSFreqHz
	}
	if opts.NMSDTSec == 0 {
		opts.NMSDTSec = d.NMSDTSec
	}
	return opts
}

// estimateMedianPower returns the median of every stride-th power
// sample across the whole spectrogram. Stride is coprime with the
// bin count to avoid systematic sampling bias.
func estimateMedianPower(spec [][]float64, stride int) float64 {
	if stride < 1 {
		stride = 1
	}
	approxN := (len(spec) * len(spec[0])) / stride
	sample := make([]float64, 0, approxN+1)
	idx := 0
	for _, row := range spec {
		for _, v := range row {
			if idx%stride == 0 {
				sample = append(sample, v)
			}
			idx++
		}
	}
	if len(sample) == 0 {
		return 0
	}
	sort.Float64s(sample)
	return sample[len(sample)/2]
}

// suppressOverlaps walks the score-sorted candidate list and keeps the
// highest-scoring cells whose freq/time neighbourhoods don't overlap
// any already-kept candidate. Linear in the kept list × the candidate
// list; for ≤ 50 results across thousands of candidates this is
// negligible compared to the matched-filter cost.
func suppressOverlaps(cands []Candidate, nmsFreqHz, nmsDTSec float64, maxKeep int) []Candidate {
	if len(cands) == 0 {
		return nil
	}
	kept := make([]Candidate, 0, maxKeep)
candLoop:
	for _, c := range cands {
		for _, k := range kept {
			if absF(c.FreqHz-k.FreqHz) <= nmsFreqHz && absF(c.DtSec-k.DtSec) <= nmsDTSec {
				continue candLoop
			}
		}
		kept = append(kept, c)
		if len(kept) >= maxKeep {
			break
		}
	}
	return kept
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
