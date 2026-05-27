package sandbox

import (
	"math"
)

// DecodeRecord is one accepted FT8 decode from the multi-pass pipeline.
type DecodeRecord struct {
	// FreqHz is the refined tone-0 audio frequency.
	FreqHz float64

	// DtSec is the refined slot-relative time (WSJT-X nominal-start
	// convention: 0 = on schedule).
	DtSec float64

	// Text is the unpacked human-readable message.
	Text string

	// Codeword is the 174-bit LDPC codeword. Kept so multi-pass
	// subtraction can re-encode the tone sequence without re-running
	// EncodeLDPC.
	Codeword [LDPCCodewordBits]uint8

	// DecodeMethod is "BP" / "OSD-N" / "fail" — propagated from the
	// BP layer.
	DecodeMethod string

	// Pass records which decoder pass produced this record (1 or 2).
	// Multi-pass diagnostics: pass 2 decodes only exist when the
	// subtract-and-redecode loop recovered an overlap.
	Pass int
}

// MultiPassOptions tunes the multi-pass loop. Zero-value falls back
// to defaults.
type MultiPassOptions struct {
	// MaxPasses is the maximum number of passes (1 or 2 typically).
	// Setting it to 1 disables the subtract+redecode loop — useful
	// for A/B comparisons.
	MaxPasses int

	// FreqMergeHz / DtMergeSec define when two decodes are
	// considered duplicates and the second is dropped. Pass-1
	// decodes are kept; pass-2 candidates inside the merge window
	// of any pass-1 decode are suppressed.
	FreqMergeHz float64
	DtMergeSec  float64

	// AudioRate is the audio buffer's sample rate. Default 12000 Hz
	// matches the FT8 convention.
	AudioRate float64
}

// DefaultMultiPassOptions returns the baseline tuning: 2 passes,
// ±5 Hz × ±0.5 s merge window (loose enough to absorb refinement
// jitter, tight enough that two genuinely different signals don't
// collide).
func DefaultMultiPassOptions() MultiPassOptions {
	return MultiPassOptions{
		MaxPasses:   2,
		FreqMergeHz: 5.0,
		DtMergeSec:  0.5,
		AudioRate:   audioRateHz,
	}
}

// MultiPassDecode runs the full decoder up to MaxPasses times,
// subtracting each pass's valid decodes from the audio buffer before
// the next pass. Returns the deduplicated list of accepted decodes.
//
// Pass 1: standard pipeline on raw audio.
// Pass 2: re-Prepare the channelizer on the residual audio (the
// channelizer's cached 192k FFT must reflect what's left after
// subtraction), re-Spectrogram, re-FindCandidates, decode, merge.
//
// The returned slice is in pass-order: pass-1 decodes first (in
// candidate-score order), then pass-2 decodes that survived dedup.
func MultiPassDecode(audio []float32, opts MultiPassOptions) []DecodeRecord {
	opts = applyMultiPassDefaults(opts)

	// Mutable audio buffer; subtraction happens in place between
	// passes (or rather, the residual is the new working buffer).
	working := make([]float32, len(audio))
	copy(working, audio)

	ch, err := NewChannelizer()
	if err != nil {
		return nil
	}
	defer ch.Close()
	rOpts := DefaultRefineOptions()
	bpOpts := DefaultBPOptions()
	findOpts := DefaultSearchOptions()

	var accepted []DecodeRecord
	for pass := 1; pass <= opts.MaxPasses; pass++ {
		if err := ch.Prepare(working); err != nil {
			break
		}
		spec := Spectrogram(working)
		if spec == nil {
			break
		}
		cands := FindCandidates(spec, findOpts)

		passDecodes := make([]DecodeRecord, 0, len(cands))
		for _, c := range cands {
			r, err := RefineCandidate(ch, c, rOpts)
			if err != nil {
				continue
			}
			grid, err := ExtractSymbols(ch, r)
			if err != nil {
				continue
			}
			llrs := SoftLLRs(grid)
			br := BPDecode(llrs, bpOpts)
			if !br.OK {
				continue
			}
			var payload [LDPCPayloadBits]uint8
			copy(payload[:], br.Message91[:LDPCPayloadBits])
			ur := Unpack77(payload)
			if !ur.OK {
				continue
			}
			passDecodes = append(passDecodes, DecodeRecord{
				FreqHz:       r.FreqHz,
				DtSec:        r.DtSec,
				Text:         ur.Text,
				Codeword:     br.Codeword,
				DecodeMethod: br.DecodeMethod,
				Pass:         pass,
			})
		}

		// Dedup pass-N decodes against everything already accepted
		// (pass 1 stays as-is; pass 2 keeps only non-dupes).
		for _, d := range passDecodes {
			if !isDuplicate(d, accepted, opts.FreqMergeHz, opts.DtMergeSec) {
				accepted = append(accepted, d)
			}
		}

		// If this isn't the last pass, subtract all pass-N decodes
		// from the working audio for the next pass.
		if pass < opts.MaxPasses {
			for _, d := range passDecodes {
				working = subtractDecodeFromAudio(working, d, opts.AudioRate)
			}
		}
	}
	return accepted
}

// subtractDecodeFromAudio synthesises the decode's audio-rate signal
// at its refined (freqHz, dtSec) and removes it from audio via the
// per-symbol LSQ fit. Returns the new (residual) buffer. Pure: the
// input audio slice is not modified in place.
func subtractDecodeFromAudio(audio []float32, d DecodeRecord, audioRate float64) []float32 {
	tones := CodewordToTones(d.Codeword)
	cosSynth, sinSynth, signalStart, signalLen := SynthesizeAudio(
		tones, d.FreqHz, d.DtSec, audioRate, len(audio),
	)
	sps := int(math.Round(audioRate * ft8SymbolPeriod))
	return FitAndSubtractAudio(audio, cosSynth, sinSynth, signalStart, signalLen, sps)
}

// isDuplicate returns true if d falls within the (freq, dt) merge
// window of any record already in accepted with the same text. Pure
// (freq, dt) overlap alone isn't enough — two distinct signals could
// reasonably sit at the same approximate freq/dt across passes if
// subtraction was imperfect and pass-2 found something different.
// Text equality is the load-bearing dedup signal: identical message
// at a nearby (freq, dt) is the actual duplicate case.
func isDuplicate(d DecodeRecord, accepted []DecodeRecord, freqTol, dtTol float64) bool {
	for _, a := range accepted {
		if a.Text != d.Text {
			continue
		}
		if math.Abs(a.FreqHz-d.FreqHz) <= freqTol &&
			math.Abs(a.DtSec-d.DtSec) <= dtTol {
			return true
		}
	}
	return false
}

func applyMultiPassDefaults(opts MultiPassOptions) MultiPassOptions {
	d := DefaultMultiPassOptions()
	if opts.MaxPasses == 0 {
		opts.MaxPasses = d.MaxPasses
	}
	if opts.FreqMergeHz == 0 {
		opts.FreqMergeHz = d.FreqMergeHz
	}
	if opts.DtMergeSec == 0 {
		opts.DtMergeSec = d.DtMergeSec
	}
	if opts.AudioRate == 0 {
		opts.AudioRate = d.AudioRate
	}
	return opts
}
