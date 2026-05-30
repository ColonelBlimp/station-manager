package sandbox

// Per-candidate decoder trace. Pure observe instrumentation —
// gated behind MultiPassOptions.TraceCandidates and emitted via
// MultiPassResult.Traces. The decode loop's behaviour is unaffected
// by enabling tracing: every continue / break / accept path is
// preserved; we just record what happened on the way through.
//
// Intended consumer: the miss funnel + ad-hoc audits answering
// questions like "did any metric produce a CRC-valid codeword for
// the truths that didn't decode" and "are accepted extras coming
// from BP successes or OSD successes." See cmd/sandbox-miss-funnel
// (Session-107 wiring) for the headline aggregation.

// TraceAttempt records the BP+OSD outcome for one LLR-generation
// metric attempted on one candidate. The cascade runs metrics in
// order until one succeeds (BPResult.OK = true); the trace captures
// every metric that was actually attempted before the cascade short-
// circuited or exhausted. So a matched truth has 1 attempt
// (whichever metric won); a missed-via-cascade-exhaustion truth has
// 4 attempts (N1, N2, N3, N1Norm at strict defaults) all with
// BR.OK = false; AP-form recoveries (when enabled) accumulate one
// attempt per hypothesis tried.
type TraceAttempt struct {
	// Metric is one of the LLRMetric* constants (LLRMetricN1,
	// LLRMetricN2, LLRMetricN3, LLRMetricN1Norm, LLRMetricBestOfN,
	// LLRMetricAPCQ, LLRMetricAP3).
	Metric string

	// BR is the BPResult for this attempt's BP+OSD run. Carries
	// OK / Iterations / SyndromeClean / CRCValid / DecodeMethod
	// ("BP" / "OSD-N" / "fail") / Codeword / Message91. Field
	// shape lets a downstream consumer bucket each attempt as:
	//   - BR.OK = true                           → success (winning metric)
	//   - BR.SyndromeClean && !BR.CRCValid       → BP found a wrong LDPC codeword
	//   - !BR.SyndromeClean && method = "OSD-N"  → OSD attempted but failed
	//   - !BR.SyndromeClean && method = "fail"   → neither BP nor OSD found a codeword
	BR BPResult

	// Text carries the Unpack77 output when BR.OK = true and the
	// outer Unpack77 call also returned OK. Empty otherwise. Only
	// the winning metric's BPResult triggers Unpack77 in
	// MultiPassDecodeFull, so non-winning attempts always have an
	// empty Text — that's by design (Unpack on a not-CRC-valid
	// codeword would produce gibberish).
	Text string

	// TextOK is true when Text was populated via successful Unpack77.
	// Distinct from Text != "" because the empty-string case is
	// "Unpack wasn't run" rather than "Unpack ran and returned empty."
	TextOK bool

	// MeanAbsLLR is the mean |LLR| over the 174 channel LLRs this
	// metric fed to BP, BEFORE BP's median-renormalisation. Diagnostic
	// only — lets a trace consumer see the per-metric LLR scale (e.g.
	// whether N1Norm's per-symbol scaling collapsed the spread).
	MeanAbsLLR float64
}

// CandidateTrace is the per-candidate decoder-trace record produced
// when MultiPassOptions.TraceCandidates is true. One record per
// candidate that reached the refine stage; carries every metric
// attempted plus the final disposition.
type CandidateTrace struct {
	// Pass is the multi-pass loop pass number this candidate was
	// produced from. Pass 1 = raw audio; Pass 2 = pass-1-residual
	// audio (after subtraction of pass-1 decodes); etc.
	Pass int

	// FreqHz / DtSec are the refined coordinate the trace records.
	// For Outcome="refine_fail" the unrefined coordinate is recorded
	// (the candidate's coarse freq/dt) so the trace still surfaces a
	// usable location for cross-referencing against truth.
	FreqHz float64
	DtSec  float64

	// AudioGeo / GridGeo are the audio-side Stage2 GeoContrast
	// (VerifyCostasAt) and grid-side Costas GeoContrast
	// (VerifyCostasGrid) for this candidate. Populated only when
	// TraceCandidates is true (otherwise the trace itself wouldn't
	// be emitted). 0 when the candidate didn't reach the relevant
	// measurement stage (refine_fail → both 0; extract_fail → audio
	// populated, grid 0).
	AudioGeo float64
	GridGeo  float64

	// Attempts is the per-metric BP+OSD trace, in cascade order.
	// Empty for refine_fail / extract_fail outcomes.
	Attempts []TraceAttempt

	// Outcome classifies the candidate's final disposition:
	//   "refine_fail"             — RefineCandidate returned an error
	//   "extract_fail"            — ExtractSymbols returned an error
	//   "cascade_fail"            — every metric returned BR.OK = false
	//   "unpack_fail"             — winning metric's CRC was valid but
	//                                Unpack77 returned !OK
	//   "ap_guard_fail"           — AP form produced a CRC-valid decode
	//                                whose text didn't start with the
	//                                hypothesis's TextGuard prefix
	//   "gate_reject:<reason>"    — passed BP+OSD+Unpack, gate rejected
	//   "accepted"                — survived all stages
	Outcome string

	// GateReason is the AcceptDecode failure reason string when
	// Outcome starts with "gate_reject:". Lets consumers bucket by
	// reason without re-parsing the Outcome string.
	GateReason string

	// UnpackDetail is UnpackResult.Detail when Outcome="unpack_fail" —
	// the unpack layer's reason string (e.g. which message type/field
	// rejected the 77-bit payload). Empty for other outcomes.
	UnpackDetail string

	// Sep* summarise the per-data-symbol winner-vs-runner-up separation
	// sep_d = (√top1 − √top2)/σ̂_d across the grid's 58 data symbols —
	// the statistic SoftLLRsN1SepWeighted keys on. SepNumNearTied counts
	// symbols with sep_d < 1 (ambiguous top tone); SepMin / SepMedian
	// give the distribution. Populated whenever a grid was extracted
	// (Outcome past extract_fail). Lets the trace test directly whether
	// the unpack_fail truths actually carry near-tied symbols (the
	// premise of the separation-weighting hypothesis).
	SepNumNearTied int
	SepMin         float64
	SepMedian      float64

	// Accept-path physical evidence — populated when the candidate
	// reached the gate (Outcome "accepted", "accepted_unresolved", or
	// "gate_reject:*"). Lets the unresolved-emit audit judge whether an
	// emitted "<...>" decode is backed by a real signal (strong nsync /
	// tone-agreement, low hard-errors) or is a CRC-lottery launder.
	// I3 is the unpacked message type (1/2/4); 0 when not reached.
	NSync      int
	ToneAgree  int
	HardErrors int
	SNR2500DB  float64
	I3         int
}
