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
	// BP layer. The LLR-metric (N=1, N=2, N=3, ...) used to produce
	// this decode is recorded separately in LLRMetric.
	DecodeMethod string

	// LLRMetric records which LLR-generation strategy successfully fed
	// BP+OSD to produce this decode. Set to one of the LLRMetric*
	// constants (LLRMetricN1, LLRMetricN2, LLRMetricN3, ...). The
	// cascade tries them in increasing-block-N order; first metric
	// whose LLRs yield a CRC-passing codeword is recorded.
	//
	// Per-metric attribution lets the corpus harness count which
	// metric uniquely recovered which truth — the load-bearing signal
	// for deciding whether to add further metrics (bit-normalized,
	// best-of-N) to the cascade.
	LLRMetric string

	// Pass records which decoder pass produced this record (1 or 2).
	// Multi-pass diagnostics: pass 2 decodes only exist when the
	// subtract-and-redecode loop recovered an overlap.
	Pass int
}

// LLRMetric* constants name the LLR-generation strategies the cascade
// can use. Keep these stable — the corpus harness uses string equality
// to attribute decodes per-metric.
const (
	LLRMetricN1      = "N1"
	LLRMetricN2      = "N2"
	LLRMetricN3      = "N3"
	LLRMetricN1Norm  = "N1Norm"
	LLRMetricBestOfN = "BestOfN"
)

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

	// Gate is the post-decode quality gate (nsync/SNR/tone-
	// agreement/hard-error checks). Applied after BPDecode+Unpack77
	// succeed; CRC-passing decodes that fail the gate are rejected.
	// Defaults from DefaultAcceptDecodeOptions; set field-by-field
	// to override.
	Gate AcceptDecodeOptions

	// BP is the BPDecode tuning, including OSD fallback policy. Set
	// to DefaultBPOptions in the zero-value MultiPassOptions; pass
	// a customised BPOptions when tuning OSD AcceptDistanceRatio,
	// MaxIterations, etc.
	BP BPOptions

	// Search is the candidate-scanner tuning. Defaults from
	// DefaultSearchOptions; bump MaxResults to surface low-score
	// truth candidates that would otherwise be capped out by the
	// default top-50.
	Search SearchOptions

	// UseAsymmetricSlice toggles the channelizer's FT8-tuned asymmetric
	// admittance mode for this decode run. When true, the driver calls
	// Channelizer.SetAsymmetricFT8Slice(true) before the first pass;
	// every Extract during the run admits [-1.5·baud, +8.5·baud) only,
	// rejecting adjacent-channel main lobes outside the tone band.
	// Output rate and ExtractSymbols' 32-samples-per-symbol contract
	// are preserved. Default false = current symmetric behaviour.
	UseAsymmetricSlice bool

	// EnableBestOfN gates the per-bit best-of-{N=1,N=2,N=3} cascade
	// stage (pass 5). Default false: the cascade ends at N1Norm.
	// Session 103 corpus measurement showed BestOfN produces zero
	// matched-truth lift in symmetric mode and only +1 truth in
	// asymmetric — kept as an experimental opt-in rather than the
	// operative default. SoftLLRsBestOfN + sanity tests + attribution
	// plumbing stay in the tree; this flag is the production gate.
	EnableBestOfN bool
}

// DefaultMultiPassOptions returns the baseline tuning: 2 passes,
// ±5 Hz × ±0.5 s merge window, default quality gate, default BP+OSD,
// default candidate scanner.
func DefaultMultiPassOptions() MultiPassOptions {
	return MultiPassOptions{
		MaxPasses:   2,
		FreqMergeHz: 5.0,
		DtMergeSec:  0.5,
		AudioRate:   audioRateHz,
		Gate:        DefaultAcceptDecodeOptions(),
		BP:          DefaultBPOptions(),
		Search:      DefaultSearchOptions(),
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
//
// Hash table: callers needing Type 4 hash resolution or h22 lookups
// in Type 1 should use MultiPassDecodeWithHashes and pass a
// persistent *CallsignHashTable.
func MultiPassDecode(audio []float32, opts MultiPassOptions) []DecodeRecord {
	return MultiPassDecodeWithHashes(audio, opts, nil)
}

// MultiPassDecodeWithHashes runs MultiPassDecode with the supplied
// hash table available for Type 4 / hashed-Type-1 resolution. The
// table accumulates: every successfully-decoded callsign (standard
// or nonstandard) is registered for future references — both within
// the same call (Type 1 decoded first can populate a hash that
// Type 4 looks up later in the same slot) and across calls (the
// next slot's table starts with everything we've ever decoded).
//
// nil ht is equivalent to MultiPassDecode (no hash resolution; Type
// 4 messages surface their h12 as a "<...N>" placeholder).
func MultiPassDecodeWithHashes(audio []float32, opts MultiPassOptions, ht *CallsignHashTable) []DecodeRecord {
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
	ch.SetAsymmetricFT8Slice(opts.UseAsymmetricSlice)
	rOpts := DefaultRefineOptions()
	bpOpts := opts.BP
	// Zero-value BP defaults to DefaultBPOptions (defensive: callers
	// that constructed MultiPassOptions{} directly without using
	// DefaultMultiPassOptions still get a working decoder).
	if bpOpts.MaxIterations == 0 {
		bpOpts = DefaultBPOptions()
	}
	findOpts := opts.Search
	if findOpts.MaxResults == 0 {
		findOpts = DefaultSearchOptions()
	}

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
			// Cascade through LLR-generation variants in increasing-
			// model-complexity order. BPDecode internally falls back to
			// OSD on BP failure, so each !br.OK here means BP+OSD
			// failed on that LLR set. First metric whose LLRs decode to
			// a CRC-pass wins; we record which in llrMetric so the
			// corpus harness can attribute lifts.
			//
			//   1. N=1     — single-symbol max-log demap (QEX § 6)
			//   2. N=2     — 2-symbol block detection, 0.32 s coherence
			//   3. N=3     — 3-symbol block detection, 0.48 s coherence
			//   4. N1Norm  — per-symbol noise-normalized N=1 (BP scale
			//                invariance failure mode; see
			//                qex-derivation.md § 8). Last in cascade
			//                because it shares N=1's coherence model;
			//                higher-N block detection takes precedence
			//                when N=1 fails. Empirically measured.
			//   5. BestOfN — per-bit max-|LLR| selection across
			//                {N=1, N=2, N=3}. Produces an LLR vector
			//                that no single source can produce. Last
			//                in cascade because it costs 3 metric
			//                generations per invocation. See
			//                qex-derivation.md § 9.
			llrs := SoftLLRs(grid)
			br := BPDecode(llrs, bpOpts)
			llrMetric := LLRMetricN1
			if !br.OK {
				llrs2 := SoftLLRsN2(grid)
				br2 := BPDecode(llrs2, bpOpts)
				if br2.OK {
					br = br2
					llrs = llrs2
					llrMetric = LLRMetricN2
				} else {
					llrs3 := SoftLLRsN3(grid)
					br3 := BPDecode(llrs3, bpOpts)
					if br3.OK {
						br = br3
						llrs = llrs3
						llrMetric = LLRMetricN3
					} else {
						llrs4 := SoftLLRsN1BitNormalized(grid)
						br4 := BPDecode(llrs4, bpOpts)
						if br4.OK {
							br = br4
							llrs = llrs4
							llrMetric = LLRMetricN1Norm
						} else if opts.EnableBestOfN {
							llrs5 := SoftLLRsBestOfN(grid)
							br5 := BPDecode(llrs5, bpOpts)
							if !br5.OK {
								continue
							}
							br = br5
							llrs = llrs5
							llrMetric = LLRMetricBestOfN
						} else {
							continue
						}
					}
				}
			}
			var payload [LDPCPayloadBits]uint8
			copy(payload[:], br.Message91[:LDPCPayloadBits])
			ur := Unpack77WithHashes(payload, ht)
			if !ur.OK {
				continue
			}
			// Post-decode quality gate. Reject CRC-passing codewords
			// that fail the nsync / tone-agreement / SNR / hard-error
			// checks — these are the OSD CRC-lottery and tone-aliased-
			// Costas-hit cases that have a valid LDPC+CRC but don't
			// actually correspond to an FT8 signal in the audio.
			//
			// hardErrs uses whichever LLR set produced the accepted
			// codeword; the gate's MaxHardErrors threshold was tuned
			// against N=1 LLRs but the metric remains directly
			// interpretable for N=2 (count of sign-disagreement bits
			// between channel LLRs and recovered codeword).
			nsync := HardSyncScore(grid)
			hardErrs := HardErrorsCount(br.Codeword, llrs)
			snr := measureCandidateSNR(ch, r, br.Codeword)
			if ok, _ := AcceptDecode(
				br.DecodeMethod, llrMetric, nsync, grid, br.Codeword,
				hardErrs, snr, opts.Gate,
			); !ok {
				continue
			}
			passDecodes = append(passDecodes, DecodeRecord{
				FreqHz:       r.FreqHz,
				DtSec:        r.DtSec,
				Text:         ur.Text,
				Codeword:     br.Codeword,
				DecodeMethod: br.DecodeMethod,
				LLRMetric:    llrMetric,
				Pass:         pass,
			})
			registerCallsigns(ht, ur.Text)
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

// registerCallsigns walks the decoded message text and registers any
// callsign-shaped tokens in the hash table. Generous on what counts:
// space-separated tokens with at least one digit (the FT8 callsign
// shape requirement) get registered. Spurious additions are
// inexpensive (the hash table just maps hashes to strings) and
// harmless on lookup — a real h12 in a future decode will index a
// legitimately-decoded callsign with extremely high probability.
//
// Skips the "<...N>" hash placeholder (which itself contains digits)
// because that's a marker for unresolved hashes, not a callsign.
//
// Skips "CQ", "DE", "QRZ", "RRR", "RR73", "73", "R", and pure
// numeric reports ("+05", "-12") that lack the call-shape signature.
func registerCallsigns(ht *CallsignHashTable, text string) {
	if ht == nil || text == "" {
		return
	}
	for _, tok := range stringFields(text) {
		if !looksLikeCallsign(tok) {
			continue
		}
		ht.Add(tok)
	}
}

// stringFields is strings.Fields without an extra import — splits on
// whitespace.
func stringFields(s string) []string {
	var out []string
	start := -1
	for i, c := range s {
		if c == ' ' || c == '\t' {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// looksLikeCallsign filters tokens to plausible callsign shapes:
// contains at least one letter AND at least one digit; not a known
// non-callsign keyword; not a "<...>" placeholder.
func looksLikeCallsign(tok string) bool {
	if tok == "" || tok[0] == '<' {
		return false
	}
	switch tok {
	case "CQ", "DE", "QRZ", "R", "RRR", "RR73", "73":
		return false
	}
	hasLetter, hasDigit := false, false
	for _, c := range tok {
		switch {
		case c >= 'A' && c <= 'Z':
			hasLetter = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

// measureCandidateSNR runs the M2 per-symbol fit path on the candidate
// to produce its 2500 Hz-bandwidth SNR estimate. Reuses the channelizer
// at 100 Hz BW (tight isolation that excludes ±100 Hz neighbours on
// dense fixtures).
func measureCandidateSNR(ch *Channelizer, r Candidate, cw [LDPCCodewordBits]uint8) float64 {
	const snrBandwidthHz = 100.0
	tones := CodewordToTones(cw)
	ref := SynthesizeBaseband(tones, snrBandwidthHz)
	bb, err := ch.Extract(r.FreqHz, snrBandwidthHz)
	if err != nil {
		return -1000 // sentinel: very negative SNR (will fail any gate)
	}
	startSample := int((r.DtSec + nominalStartSec) * snrBandwidthHz)
	endSample := startSample + len(ref)
	if startSample < 0 || endSample > len(bb) {
		return -1000
	}
	sps := int(snrBandwidthHz * ft8SymbolPeriod)
	m := MeasureSNRPerSymbol(bb[startSample:endSample], ref, sps, snrBandwidthHz)
	return m.SNR2500DB
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
