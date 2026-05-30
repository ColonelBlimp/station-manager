package sandbox

import (
	"math"
	"sort"
	"strings"
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

	// Unresolved is true when this decode carried an unresolvable
	// hashed callsign and was emitted with a "<...>" placeholder under
	// MultiPassOptions.EmitUnresolvedHashes. The codeword is valid and
	// the decode passed the gate; only the hashed callsign couldn't be
	// rendered. False for fully-resolved decodes.
	Unresolved bool
}

// LLRMetric* constants name the LLR-generation strategies the cascade
// can use. Keep these stable — the corpus harness uses string equality
// to attribute decodes per-metric.
const (
	LLRMetricN1      = "N1"
	LLRMetricN2      = "N2"
	LLRMetricN3      = "N3"
	LLRMetricN1Norm  = "N1Norm"
	LLRMetricN1Sep   = "N1Sep"
	LLRMetricBestOfN = "BestOfN"
	LLRMetricAPCQ    = "APCQ"
	LLRMetricAP3     = "AP3"
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

	// EnableAPCQ gates the AP-CQ a priori decoding stage that runs
	// after every other cascade pass has failed. Implements the QEX
	// § 7 AP2 specialisation: hypothesise the candidate as a Type-1
	// CQ message, inject ±APCQMag priors at the 34 codeword positions
	// pinned by that hypothesis (c28_1=CQ, p1=p2=r1=0, i3=1), and
	// re-run BP+OSD on the augmented LLR vector. Only fires when
	// standard cascade has failed — the AP path can only help, never
	// substitute for a working channel decode.
	//
	// Default false because AP-CQ is plausibly a CRC-lottery source
	// when the candidate isn't actually a CQ message — the 33-bit
	// pin constrains BP to a small subspace, raising the random-
	// codeword-passes-CRC odds. Promote after measurement.
	EnableAPCQ bool

	// APCQMag overrides the AP-CQ pinning magnitude (default 0 = use
	// the package default apCQMagnitude). Larger values dominate the
	// channel more aggressively; smaller values let BP override more
	// easily when the candidate is not actually a CQ message.
	APCQMag float64

	// EnableAP3 gates the AP3 a priori decoding stage that runs after
	// AP-CQ has failed. AP3 enumerates (c28_1, c28_2) hypothesis
	// pairs drawn from the running CallsignHashTable: c28_1 from the
	// CQ family ∪ hash callsigns, c28_2 from hash callsigns. Each
	// hypothesis pins 62 codeword bits (vs AP-CQ's 34) via
	// BPDecodeWithPin and the ap3PinMask.
	//
	// Per-candidate cost is O(K²) BP runs where K = AP3MaxCallsigns.
	// Default false: AP3 is the most expensive cascade stage and
	// produces no work on an empty hash table. The hash-feeding loop
	// gives AP3 its leverage — pass-1 decodes populate the table,
	// pass-2 candidates inherit a populated hash.
	EnableAP3 bool

	// AP3Mag overrides the AP3 per-bit pinning magnitude (default 0
	// = use apCQMagnitude). Tunable via the corpus harness.
	AP3Mag float64

	// AP3MaxCallsigns caps the number of hash-table callsigns AP3
	// considers as c28_1 / c28_2 candidates. 0 falls back to a
	// sensible default (8). Hash-table snapshots are unordered, so
	// the K chosen are arbitrary; future refinement could LRU-rank
	// or score by candidate freq proximity.
	//
	// Worst-case hypothesis count per failed candidate is
	// (CQ-family-size + K) × K — at K=8 with 4 CQ-family entries
	// that's 96 BP runs.
	AP3MaxCallsigns int

	// MagnitudeLLR selects the QEX § 6 spec-aligned demap domain.
	// Default false = power-domain (existing sandbox behaviour,
	// LLR = max{|C|²}_{x=0} − max{|C|²}_{x=1}). True = magnitude
	// domain (paper-prescribed, LLR = max{|C|}_{x=0} − max{|C|}_{x=1}),
	// applied uniformly to N=1, N=2, N=3, N1Norm, and BestOfN.
	//
	// Power vs magnitude is NOT a constant-factor transform on the
	// demap *difference*: L_pow = (a+b)·L_mag where a,b are the two
	// max-magnitudes; (a+b) varies per symbol with signal+noise, so
	// BP's global noise normalisation cannot absorb it. See
	// qex-derivation.md § 3.1.1 for the math.
	//
	// A/B mode added 2026-05-29; not yet a strict-mode default.
	MagnitudeLLR bool

	// Stage2Mode selects the post-NMS Costas verifier behaviour:
	// Stage2Off (default) skips it entirely, Stage2Observe runs it
	// without filtering, Stage2Filter drops sub-threshold
	// candidates, Stage2Rerank re-sorts by metric. The verifier
	// runs between FindCandidates and RefineCandidate, on every
	// pass independently. See costas_verify.go for the algorithm.
	Stage2Mode Stage2Mode

	// Stage2Metric selects which discriminator drives Stage2Filter
	// / Stage2Rerank. Default Stage2MetricMinBlock; per the
	// 2026-05-29 audit, MinBlockContrast is the cleanest single-
	// metric separator at the near-truth median.
	Stage2Metric Stage2Metric

	// Stage2Threshold is the minimum metric value a candidate must
	// reach to survive Stage2Filter. Ignored for Stage2Off /
	// Stage2Observe / Stage2Rerank. Has no default — Stage2Filter
	// without a threshold passes everything (functionally observe).
	Stage2Threshold float64

	// TraceCandidates gates the per-candidate decoder trace emitted
	// via MultiPassResult.Traces. Pure observe instrumentation: no
	// effect on decode outcomes; just records what each candidate
	// did inside BP/OSD/unpack/gate. Cost is per-candidate audio +
	// grid Goertzel sweeps (~few ms each on a real audio buffer) +
	// memory for the per-attempt BP results — fine for research
	// runs, not for production. See research/sandbox/trace.go.
	TraceCandidates bool

	// OSDDisableForN1 gates OSD fallback specifically on the N1
	// cascade stage. When true, the N1 BP attempt runs with OSD
	// disabled (BP-only), while N2/N3/N1Norm/BestOfN/AP forms keep
	// the normally-configured OSD. Implementation of the Session-107
	// "OSD only for deeper metrics" experiment: the trace showed N1
	// produces 67% of accepted false-positive extras AND most of
	// them came via OSD-2 rescue, so isolating N1-OSD lets us
	// measure whether N1+OSD is the dominant false-positive engine
	// while preserving deeper-metric recoveries.
	OSDDisableForN1 bool

	// SepKappa enables the N1Sep cascade stage (SoftLLRsN1SepWeighted):
	// N1Norm plus a per-symbol winner-vs-runner-up separation confidence
	// weight w_s = min(1, sep_s/SepKappa), sep_s = (√top1−√top2)/σ̂_s.
	// SepKappa ≤ 0 (default) skips the stage entirely, so the baseline
	// cascade and corpus numbers are preserved exactly — flip it on to
	// A/B N1Norm against N1Norm+sepWeight. Smaller kappa down-weights
	// ambiguous (near-tied top-tone) symbols more aggressively; the
	// operating point is corpus-calibrated, not a spec constant. Targets
	// the Session-107 unpack_fail / wrong-codeword bucket where a strong
	// runner-up tone gives N1Norm false confidence. See
	// SoftLLRsN1SepWeighted + qex-derivation.md § 8.4.
	SepKappa float64

	// EmitUnresolvedHashes lets the decode loop accept a CRC-valid,
	// structurally-valid decode whose only defect is an unresolvable
	// hashed callsign (UnpackResult.Unresolved) — emitting it with the
	// "<...>" placeholder rather than discarding it as unpack_fail.
	// This matches jt9's behaviour (and the jt9-oracle truth manifests,
	// which carry "<...>" for the same case). The post-decode gate
	// still runs; only genuinely undecodable payloads (unsupported i3,
	// reserved token, bad grid) remain dropped. Default false preserves
	// the existing baseline exactly. Targets the Session-108 finding
	// that the unpack_fail bucket is dominated by correct codewords
	// blocked solely on hash resolution, not wrong-codeword commits.
	EmitUnresolvedHashes bool
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
// persistent *CallsignHashTable. Callers needing the shadow-reject
// audit channel should use MultiPassDecodeFull and consult the
// returned ShadowRejects slice.
func MultiPassDecode(audio []float32, opts MultiPassOptions) []DecodeRecord {
	return MultiPassDecodeFull(audio, opts, nil).Decodes
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
//
// Returns only the accepted decodes. Callers needing the
// shadow-reject diagnostic channel should use MultiPassDecodeFull
// instead.
func MultiPassDecodeWithHashes(audio []float32, opts MultiPassOptions, ht *CallsignHashTable) []DecodeRecord {
	return MultiPassDecodeFull(audio, opts, ht).Decodes
}

// MultiPassDecodeFull is the rich-return entry point that exposes
// both the accepted decodes and the shadow-reject channel.
//
// A shadow reject is a candidate whose codeword passed BPDecode +
// CRC + Unpack77 (i.e. surfaced legal text) but failed the
// post-decode quality gate (AcceptDecode). The shadow-reject channel
// is the operator-requested audit instrument introduced 2026-05-29
// to answer "are oracle misses gate-rejects or true decode
// failures?" — without re-running the pipeline.
//
// MultiPassDecodeWithHashes and MultiPassDecode are thin wrappers
// that return only the .Decodes slice for production callers that
// don't consume the audit channel.
func MultiPassDecodeFull(audio []float32, opts MultiPassOptions, ht *CallsignHashTable) MultiPassResult {
	opts = applyMultiPassDefaults(opts)

	// Mutable audio buffer; subtraction happens in place between
	// passes (or rather, the residual is the new working buffer).
	working := make([]float32, len(audio))
	copy(working, audio)

	ch, err := NewChannelizer()
	if err != nil {
		return MultiPassResult{}
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
	var rejects []ShadowReject
	var traces []CandidateTrace
	for pass := 1; pass <= opts.MaxPasses; pass++ {
		if err := ch.Prepare(working); err != nil {
			break
		}
		spec := Spectrogram(working)
		if spec == nil {
			break
		}
		cands := FindCandidates(spec, findOpts)
		cands = applyStage2(working, cands, opts)

		passDecodes := make([]DecodeRecord, 0, len(cands))
		for _, c := range cands {
			// Per-candidate trace scaffold (emit gated on TraceCandidates).
			// Populated incrementally as the candidate moves through the
			// pipeline; appended to result.Traces before each continue /
			// at the accept path. The audio-side Stage2 contrast is
			// computed once on the working (pass-residual) buffer so the
			// trace reflects what BP actually saw on this pass.
			var ctrace CandidateTrace
			if opts.TraceCandidates {
				ctrace.Pass = pass
				ctrace.FreqHz = c.FreqHz
				ctrace.DtSec = c.DtSec
				ctrace.AudioGeo = VerifyCostasAt(working, c.FreqHz, c.DtSec, c.Sync).GeoContrast
			}

			r, err := RefineCandidate(ch, c, rOpts)
			if err != nil {
				if opts.TraceCandidates {
					ctrace.Outcome = "refine_fail"
					result := MultiPassResult{}
					_ = result
					// emit in place
					// (collected into a slice declared above the pass loop)
					traceEmit(&traces, ctrace)
				}
				continue
			}
			// Update freq/dt to the refined coords for the trace record.
			if opts.TraceCandidates {
				ctrace.FreqHz = r.FreqHz
				ctrace.DtSec = r.DtSec
			}
			grid, err := ExtractSymbols(ch, r)
			if err != nil {
				if opts.TraceCandidates {
					ctrace.Outcome = "extract_fail"
					traceEmit(&traces, ctrace)
				}
				continue
			}
			if opts.TraceCandidates {
				ctrace.GridGeo = VerifyCostasGrid(grid).GeoContrast
				ctrace.SepNumNearTied, ctrace.SepMin, ctrace.SepMedian = gridSepSummary(grid)
			}
			var traceTarget *[]TraceAttempt
			if opts.TraceCandidates {
				traceTarget = &ctrace.Attempts
			}
			co, ok := runCascade(grid, opts, bpOpts, ht, traceTarget)
			if !ok {
				if opts.TraceCandidates {
					ctrace.Outcome = "cascade_fail"
					traceEmit(&traces, ctrace)
				}
				continue
			}
			br := co.BR
			llrs := co.LLRs
			llrMetric := co.Metric
			var payload [LDPCPayloadBits]uint8
			copy(payload[:], br.Message91[:LDPCPayloadBits])
			ur := Unpack77WithHashes(payload, ht)
			// Emit a displayable-but-unresolved decode (hashed callsign
			// rendered "<...>") only under EmitUnresolvedHashes; the gate
			// below still runs on it. Genuinely undecodable payloads
			// (ur.OK false and ur.Unresolved false) always drop here.
			emitUnresolved := opts.EmitUnresolvedHashes && ur.Unresolved
			if !ur.OK && !emitUnresolved {
				if opts.TraceCandidates {
					ctrace.Outcome = "unpack_fail"
					ctrace.UnpackDetail = ur.Detail
					traceEmit(&traces, ctrace)
				}
				continue
			}
			// Backfill text into the winning attempt for trace clarity.
			if opts.TraceCandidates && len(ctrace.Attempts) > 0 {
				ctrace.Attempts[len(ctrace.Attempts)-1].Text = ur.Text
				ctrace.Attempts[len(ctrace.Attempts)-1].TextOK = ur.OK
			}
			// AP-form correctness guard: OSD-2 can flip MRB bits even
			// with priors pinned (the OSD pin shipped Session 104 now
			// holds AP-pinned bits, but the guard remains as a defence
			// in depth — a wrongly-hypothesised AP can still yield
			// CRC-valid codewords that BP converged to with weak channel
			// agreement). co.TextGuard is non-empty when the winning
			// stage was an AP form; its value is the expected text
			// prefix (e.g. "CQ" for AP-CQ, "K1JT W1ABC" for AP3 with
			// standard-call c28_1).
			if co.TextGuard != "" && !strings.HasPrefix(ur.Text, co.TextGuard) {
				if opts.TraceCandidates {
					ctrace.Outcome = "ap_guard_fail"
					traceEmit(&traces, ctrace)
				}
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
			// Compute tone-agreement eagerly. AcceptDecode only consults
			// it on the OSD path, but the shadow-reject audit needs it
			// uniformly across BP and OSD so the corpus harness can
			// compare distributions. The cost is 79 comparisons per
			// candidate — negligible next to BP+OSD.
			toneAgree := ToneAgreementCount(br.Codeword, grid)
			if opts.TraceCandidates {
				ctrace.NSync = nsync
				ctrace.ToneAgree = toneAgree
				ctrace.HardErrors = hardErrs
				ctrace.SNR2500DB = snr
				ctrace.I3 = ur.I3
			}
			if ok, reason := AcceptDecode(
				br.DecodeMethod, llrMetric, nsync, grid, br.Codeword,
				hardErrs, snr, opts.Gate,
			); !ok {
				rejects = append(rejects, ShadowReject{
					Reason:     reason,
					NSync:      nsync,
					ToneAgree:  toneAgree,
					SNR2500DB:  snr,
					HardErrors: hardErrs,
					Method:     br.DecodeMethod,
					LLRMetric:  llrMetric,
					FreqHz:     r.FreqHz,
					DtSec:      r.DtSec,
					Text:       ur.Text,
					Codeword:   br.Codeword,
					Pass:       pass,
				})
				if opts.TraceCandidates {
					ctrace.Outcome = "gate_reject:" + reason
					ctrace.GateReason = reason
					traceEmit(&traces, ctrace)
				}
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
				Unresolved:   emitUnresolved,
			})
			if opts.TraceCandidates {
				if emitUnresolved {
					ctrace.Outcome = "accepted_unresolved"
					ctrace.UnpackDetail = ur.Detail
				} else {
					ctrace.Outcome = "accepted"
				}
				traceEmit(&traces, ctrace)
			}
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
	return MultiPassResult{Decodes: accepted, ShadowRejects: rejects, Traces: traces}
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

// traceEmit appends one CandidateTrace to the per-pass trace
// accumulator. Defensive nil-guard so callers can pass a nil slice
// pointer without ceremony (the trace path is opt-in; production
// invocations should never hit this code).
func traceEmit(traces *[]CandidateTrace, t CandidateTrace) {
	if traces == nil {
		return
	}
	*traces = append(*traces, t)
}

// ApplyStage2 is the public entry point to the Stage2 Costas-verifier
// gate so research tooling (e.g. the miss funnel) can reproduce the
// same post-NMS transformation MultiPassDecodeFull applies internally.
// Behaviour is identical to the internal call in MultiPassDecodeFull.
func ApplyStage2(samples []float32, cands []Candidate, opts MultiPassOptions) []Candidate {
	return applyStage2(samples, cands, opts)
}

// applyStage2 runs the Stage2 Costas verifier on the post-NMS
// candidate list and returns the candidate list to feed the refine
// loop. Behaviour depends on opts.Stage2Mode:
//
//   - Stage2Off: pass-through; no verifier call. Zero cost.
//   - Stage2Observe: verifier runs on every candidate (effect
//     visible only through downstream side instrumentation), but
//     the candidate list is returned unchanged.
//   - Stage2Filter: candidates with metric below opts.Stage2Threshold
//     are dropped. Surviving order matches the input order (Sync-
//     descending as produced by FindCandidates).
//   - Stage2Rerank: full list is re-sorted descending by the
//     configured metric. No candidate is dropped; the downstream
//     MaxResults cap (if present) then admits top-K by Stage2.
//
// The verifier reads the supplied audio (the working buffer at the
// current pass), so multi-pass subtraction is naturally honoured —
// pass-2 verifies against the residual after pass-1 subtraction.
func applyStage2(samples []float32, cands []Candidate, opts MultiPassOptions) []Candidate {
	if opts.Stage2Mode == Stage2Off || len(cands) == 0 {
		return cands
	}
	metrics := make([]float64, len(cands))
	for i, c := range cands {
		v := VerifyCostasAt(samples, c.FreqHz, c.DtSec, c.Sync)
		metrics[i] = opts.Stage2Metric.extract(v)
	}
	switch opts.Stage2Mode {
	case Stage2Filter:
		out := cands[:0]
		for i, c := range cands {
			if metrics[i] >= opts.Stage2Threshold {
				out = append(out, c)
			}
		}
		return out
	case Stage2Rerank:
		idx := make([]int, len(cands))
		for i := range idx {
			idx[i] = i
		}
		sort.Slice(idx, func(i, j int) bool {
			return metrics[idx[i]] > metrics[idx[j]]
		})
		out := make([]Candidate, len(cands))
		for i, j := range idx {
			out[i] = cands[j]
		}
		return out
	}
	return cands
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

// cascadeOutcome bundles the result of a successful cascade stage.
// Metric records which LLR-generation strategy (or AP form) produced
// the decode; TextGuard, when non-empty, is the expected prefix of
// the unpacked text — used by the outer loop to reject AP candidates
// whose decoded text diverges from the hypothesis.
type cascadeOutcome struct {
	BR        BPResult
	LLRs      [FT8CodewordBits]float64
	Metric    string
	TextGuard string
}

// runCascade is the per-candidate LLR-generation cascade. Each stage
// produces a 174-bit LLR vector; the first one whose BP+OSD decodes
// to a CRC-passing codeword wins. Stages in order:
//
//  1. N=1     — single-symbol max-log demap (QEX § 6)
//  2. N=2     — 2-symbol block detection, 0.32 s coherence (QEX § 6)
//  3. N=3     — 3-symbol block detection, 0.48 s coherence (QEX § 6)
//  4. N1Norm  — per-symbol noise-normalized N=1 (qex-derivation.md § 8)
//     4b. N1Sep  — N1Norm + winner-vs-runner-up separation weight, opt-in
//     via SepKappa > 0 (qex-derivation.md § 8.4)
//  5. BestOfN — per-bit max-|LLR| selection, opt-in via EnableBestOfN
//     (qex-derivation.md § 9)
//  6. AP-CQ   — c28_1 ∈ {bare CQ, CQ DX, CQ COTA, CQ POTA}, 34 pinned
//     bits; opt-in via EnableAPCQ (qex-derivation.md § 10)
//  7. AP3     — (c28_1, c28_2) drawn from CallsignHashTable + CQ family;
//     62 pinned bits; opt-in via EnableAP3
//
// Stages 1-5 (incl. N1Sep) use unpinned BP. Stages 6-7 use BPDecodeWithPin so OSD's
// MRB bit-flip search cannot undo the priors.
//
// Returns (outcome, true) on first success; (zero, false) when all
// stages fail.
//
// When trace is non-nil, every metric attempted is appended to the
// trace BEFORE the success-return short-circuits. Behaviour is
// otherwise identical to the non-traced path.
func runCascade(
	grid *SymbolGrid,
	opts MultiPassOptions,
	bpOpts BPOptions,
	ht *CallsignHashTable,
	trace *[]TraceAttempt,
) (cascadeOutcome, bool) {
	mm := opts.MagnitudeLLR
	// N1 stage: optionally disable OSD for this stage only. The rest
	// of the cascade keeps the normally-configured OSD via bpOpts.
	n1Opts := bpOpts
	if opts.OSDDisableForN1 {
		n1Opts.OSD.Enable = false
	}
	llrs := SoftLLRs(grid, mm)
	br := BPDecode(llrs, n1Opts)
	if trace != nil {
		*trace = append(*trace, TraceAttempt{Metric: LLRMetricN1, BR: br, MeanAbsLLR: meanAbsLLR(llrs)})
	}
	if br.OK {
		return cascadeOutcome{BR: br, LLRs: llrs, Metric: LLRMetricN1}, true
	}

	llrs = SoftLLRsN2(grid, mm)
	br = BPDecode(llrs, bpOpts)
	if trace != nil {
		*trace = append(*trace, TraceAttempt{Metric: LLRMetricN2, BR: br, MeanAbsLLR: meanAbsLLR(llrs)})
	}
	if br.OK {
		return cascadeOutcome{BR: br, LLRs: llrs, Metric: LLRMetricN2}, true
	}

	llrs = SoftLLRsN3(grid, mm)
	br = BPDecode(llrs, bpOpts)
	if trace != nil {
		*trace = append(*trace, TraceAttempt{Metric: LLRMetricN3, BR: br, MeanAbsLLR: meanAbsLLR(llrs)})
	}
	if br.OK {
		return cascadeOutcome{BR: br, LLRs: llrs, Metric: LLRMetricN3}, true
	}

	llrs = SoftLLRsN1BitNormalized(grid, mm)
	br = BPDecode(llrs, bpOpts)
	if trace != nil {
		*trace = append(*trace, TraceAttempt{Metric: LLRMetricN1Norm, BR: br, MeanAbsLLR: meanAbsLLR(llrs)})
	}
	if br.OK {
		return cascadeOutcome{BR: br, LLRs: llrs, Metric: LLRMetricN1Norm}, true
	}

	if opts.SepKappa > 0 {
		llrs = SoftLLRsN1SepWeighted(grid, mm, opts.SepKappa)
		br = BPDecode(llrs, bpOpts)
		if trace != nil {
			*trace = append(*trace, TraceAttempt{Metric: LLRMetricN1Sep, BR: br, MeanAbsLLR: meanAbsLLR(llrs)})
		}
		if br.OK {
			return cascadeOutcome{BR: br, LLRs: llrs, Metric: LLRMetricN1Sep}, true
		}
	}

	if opts.EnableBestOfN {
		llrs = SoftLLRsBestOfN(grid, mm)
		br = BPDecode(llrs, bpOpts)
		if trace != nil {
			*trace = append(*trace, TraceAttempt{Metric: LLRMetricBestOfN, BR: br, MeanAbsLLR: meanAbsLLR(llrs)})
		}
		if br.OK {
			return cascadeOutcome{BR: br, LLRs: llrs, Metric: LLRMetricBestOfN}, true
		}
	}

	if opts.EnableAPCQ {
		pin := apCQPinMask()
		for _, c28v := range apCQValueOrder {
			l := softLLRsAPCQWithMag(grid, opts.APCQMag, c28v, mm)
			b := BPDecodeWithPin(l, bpOpts, &pin)
			if trace != nil {
				*trace = append(*trace, TraceAttempt{Metric: LLRMetricAPCQ, BR: b, MeanAbsLLR: meanAbsLLR(l)})
			}
			if b.OK {
				return cascadeOutcome{BR: b, LLRs: l, Metric: LLRMetricAPCQ, TextGuard: "CQ"}, true
			}
			_ = c28v
		}
	}

	if opts.EnableAP3 {
		pairs := enumerateAP3HypothesisPairs(ht, opts.AP3MaxCallsigns)
		if len(pairs) > 0 {
			pin := ap3PinMask()
			for _, p := range pairs {
				l := softLLRsAP3WithMag(grid, opts.AP3Mag, p.c28_1, p.c28_2, mm)
				b := BPDecodeWithPin(l, bpOpts, &pin)
				if trace != nil {
					*trace = append(*trace, TraceAttempt{Metric: LLRMetricAP3, BR: b, MeanAbsLLR: meanAbsLLR(l)})
				}
				if !b.OK {
					continue
				}
				guard := p.call1 + " " + p.call2
				if p.c28_1 < ntokens {
					// c28_1 was a CQ-family token; the unpacked text
					// starts with "CQ" (possibly "CQ XX" for modifier
					// forms). The text guard tightens to "CQ" — the
					// specific modifier isn't part of the hypothesised
					// pair.
					guard = "CQ"
				}
				return cascadeOutcome{BR: b, LLRs: l, Metric: LLRMetricAP3, TextGuard: guard}, true
			}
		}
	}

	return cascadeOutcome{}, false
}

// ap3HypothesisPair carries one (c28_1, c28_2) AP3 hypothesis with
// the originating callsign strings preserved for the text guard.
type ap3HypothesisPair struct {
	c28_1, c28_2 uint32
	call1, call2 string
}

// enumerateAP3HypothesisPairs builds the AP3 hypothesis list from the
// hash table. c28_1 candidates: bare "CQ" + up to maxK callsigns from
// the table. c28_2 candidates: the same callsign set (CQ never
// appears as an addressee/callee). Self-pairs (c1 == c2) are skipped
// — no station addresses itself.
//
// maxK <= 0 falls back to 8. Per-candidate AP3 cost is
// O((1 + min(maxK, N)) × min(maxK, N)) BP runs where N is the live
// hash size; the cap protects against AP3 dominating runtime when
// the table is large.
func enumerateAP3HypothesisPairs(ht *CallsignHashTable, maxK int) []ap3HypothesisPair {
	if ht == nil {
		return nil
	}
	if maxK <= 0 {
		maxK = 8
	}
	calls := ht.Callsigns()
	if len(calls) == 0 {
		return nil
	}
	if len(calls) > maxK {
		calls = calls[:maxK]
	}

	// Build packed (c28, callsign) sides. Drop entries whose callsign
	// can't be packed (e.g. unresolved hash placeholders that slipped
	// past registerCallsigns).
	type side struct {
		c28 uint32
		s   string
	}
	pack := func(callsign string) (uint32, bool) {
		v, err := PackCallsign28(callsign)
		if err != nil {
			return 0, false
		}
		return v, true
	}

	c1Set := []side{{2, "CQ"}} // bare-CQ token, always included
	for _, c := range calls {
		if v, ok := pack(c); ok {
			c1Set = append(c1Set, side{v, c})
		}
	}
	var c2Set []side
	for _, c := range calls {
		if v, ok := pack(c); ok {
			c2Set = append(c2Set, side{v, c})
		}
	}
	if len(c2Set) == 0 {
		return nil
	}

	pairs := make([]ap3HypothesisPair, 0, len(c1Set)*len(c2Set))
	for _, a := range c1Set {
		for _, b := range c2Set {
			if a.s == b.s {
				continue
			}
			pairs = append(pairs, ap3HypothesisPair{a.c28, b.c28, a.s, b.s})
		}
	}
	return pairs
}
