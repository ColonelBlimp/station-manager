package ft8

import (
	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
)

// Evidence outcomes — the §4.1 coverage vocabulary, as strings so the
// evidence package and this one stay import-independent (cmd/smd adapts;
// go-ft8 is the shared vocabulary, same seam shape as the QSO logger).
const (
	EvidenceDecoded        = "decoded"
	EvidenceNoDecode       = "no_decode"
	EvidenceTx             = "tx"
	EvidenceDialChanged    = "dial_changed"
	EvidenceDecoderError   = "decoder_error"
	EvidenceCaptureDropped = "capture_dropped"
)

// EvidenceSlot is one physical slot's evidence: its coverage outcome and the
// RICH decode set — every parse status, own transmissions included, tapped
// UPSTREAM of every curated filter (design §4 prerequisite 2). Decodes is
// nil for any non-decoded outcome.
type EvidenceSlot struct {
	Slot        SlotRef
	DialMHz     float64
	DialTracked bool
	Outcome     string
	Decodes     []goft8.DecodedMessage
}

// SetEvidenceSink injects the evidence observer, called once per PHYSICAL
// slot — delivered or omitted (scheduler drops surface as capture_dropped) —
// from the decode loop. Install before Start. The sink MUST NOT block: it is
// called synchronously on the decode goroutine, so a slow sink would eat the
// slot budget (the evidence writer's own queue provides the buffering).
// nil = no observer.
func (s *Service) SetEvidenceSink(fn func(EvidenceSlot)) {
	s.evidenceSink = fn
}
