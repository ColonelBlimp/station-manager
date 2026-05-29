package sandbox

// ShadowReject records a decode that passed BPDecode+Unpack77 (i.e. a
// CRC-valid 174-bit codeword that unpacked into legal i3/n3 text) but
// failed the post-decode quality gate (AcceptDecode). The full
// diagnostic context is captured so a corpus harness can answer the
// audit question: "are oracle misses gate-rejects or real decode
// failures?" — without re-running the pipeline.
//
// The shadow-reject machinery is the operator-requested 2026-05-29
// follow-up to the Session 103 gate-tightening sweep, which closed
// inconclusively because the marginal-real and CRC-lottery populations
// overlap in nsync/tone-agree space. With shadow rejects, every
// experiment can verify on its own corpus run which truths the gate
// chose to drop, instead of inferring it from aggregate counts.
//
// Identity rule: ShadowReject describes a candidate that *would have
// been* a DecodeRecord if AcceptDecode had passed. The (FreqHz, DtSec,
// Text, Codeword, DecodeMethod, LLRMetric, Pass) fields therefore
// mirror DecodeRecord exactly. The gate-diagnostic fields (Reason,
// NSync, ToneAgree, SNR2500DB, HardErrors) capture the values
// AcceptDecode saw at rejection time.
type ShadowReject struct {
	// Reason is the human-readable diagnostic from AcceptDecode that
	// caused the rejection — e.g. "hard-errors 38 > 36",
	// "OSD nsync 9 < 11 (metric=N3)", "OSD tone-agree 42/79 < 50".
	Reason string

	// NSync is HardSyncScore(grid), 0..21. Always populated; both BP
	// and OSD paths use it.
	NSync int

	// ToneAgree is ToneAgreementCount(codeword, grid), 0..79. Always
	// populated even for BP-path rejects so the audit can compare
	// distributions across methods. A real decode matches ~60-79; noise
	// ~10.
	ToneAgree int

	// SNR2500DB is the WSJT-X-convention 2500 Hz reference SNR. May be
	// the -1000 sentinel when the channelizer extraction at SNR
	// bandwidth failed.
	SNR2500DB float64

	// HardErrors is the Hamming distance between the codeword and the
	// channel's hard decision (per the LLR set that won at the
	// generator stage).
	HardErrors int

	// Method is BPResult.DecodeMethod: "BP" or "OSD-N".
	Method string

	// LLRMetric is one of the LLRMetric* constants (N1, N2, N3,
	// N1Norm, BestOfN) — which cascade pass produced this candidate.
	LLRMetric string

	// FreqHz / DtSec are the refined-candidate coordinates (same shape
	// as DecodeRecord). Used to correlate with truth manifests.
	FreqHz float64
	DtSec  float64

	// Text is the Unpack77 output. The codeword passed CRC and the
	// i3/n3 type field unpacked to a legal message; the text is what
	// the decoder would have surfaced if accepted.
	Text string

	// Codeword is the rejected 174-bit codeword. Kept so downstream
	// audits can re-encode tones, compare against truth codewords,
	// or feed into AP-decoder priorLLR experiments.
	Codeword [LDPCCodewordBits]uint8

	// Pass is the cascade pass index (1 or 2). Pass-2 shadow rejects
	// describe candidates that surfaced only because pass-1 decodes
	// were subtracted from the working audio.
	Pass int
}

// MultiPassResult is the rich return shape from MultiPassDecodeFull.
// .Decodes is the same slice that MultiPassDecodeWithHashes returns
// (production callers shouldn't notice a difference); .ShadowRejects
// is the new diagnostic channel that captures every CRC+unpack-valid
// candidate the gate threw away.
//
// Ordering: Decodes follows the existing pass-order + candidate-score
// rules. ShadowRejects is appended in the order the gate rejected
// candidates — pass-1 rejects first, then pass-2.
type MultiPassResult struct {
	Decodes       []DecodeRecord
	ShadowRejects []ShadowReject

	// Traces is the per-candidate decoder-trace channel. Populated
	// only when MultiPassOptions.TraceCandidates is true. One
	// CandidateTrace per candidate that reached the refine stage,
	// recording every LLR metric attempted + the final disposition
	// (accepted / gate_reject / cascade_fail / etc.). See
	// research/sandbox/trace.go for the record shape and ordering;
	// traces are appended in candidate-processing order within each
	// pass, all of pass 1 before any of pass 2.
	Traces []CandidateTrace
}
