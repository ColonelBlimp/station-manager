package ft8

import (
	stderrors "errors"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const opDecodeFile errors.Op = "ft8.DecodeFile"

// DecodeReport is the per-slot set of decoded messages published to the SPA as
// the ft8-decode SSE event — the live Band Activity feed. Distinct from the
// occupancy report (TX-offset selection, ADR 0029): this is RX display data.
type DecodeReport struct {
	Slot    SlotRef      `json:"slot"`
	Decodes []DecodeLine `json:"decodes"`
}

// DecodeLine is one decoded message in operator-facing form: the base-tone
// frequency, the time offset, the SNR (dB, WSJT-X 2500 Hz reference), and the
// message text — the fields the operator reads to pick a station to work and
// the report to send back. SNR also feeds the step (e) answer-a-CQ sequencer.
type DecodeLine struct {
	Text   string  `json:"text"`
	FreqHz float64 `json:"freq_hz"`
	DTSec  float64 `json:"dt_s"`
	SNR    int     `json:"snr"`
}

// newDecodeReport projects go-ft8's decodes into the wire DTO for one slot.
func newDecodeReport(slot SlotRef, msgs []goft8.DecodedMessage) DecodeReport {
	lines := make([]DecodeLine, 0, len(msgs))
	for _, m := range msgs {
		lines = append(lines, DecodeLine{Text: m.Text, FreqHz: m.FreqHz, DTSec: m.DTSec, SNR: m.SNR})
	}
	return DecodeReport{Slot: slot, Decodes: lines}
}

// DecodeSlot decodes one 15-second FT8 slot from 12 kHz mono signed-16-bit
// PCM samples, logs each decoded message as a structured line, and returns
// the decodes. It uses go-ft8's checked, stateless API so a malformed slot
// (wrong sample count) is rejected up front with a typed error rather than
// silently zero-padded — which surfaces a plumbing bug in the slot producer
// (the live path's ring buffer must emit exactly one slot) instead of hiding
// it. A stateful per-stream Decoder is a later concern for the live path.
//
// Logging policy:
//   - one info line per decoded message ("heard this");
//   - a debug line per slot with aggregate diagnostics (off at the default
//     info level; flip to debug when bringing the live path up or chasing an
//     empty slot — covers the why-nothing case for every slot, decoding or
//     not);
//   - a warn for a rejected slot or a recovered panic.
//
// Fail-soft: a panic inside the decoder is recovered and logged, never
// propagated, and a rejected slot returns nil. An FT8 failure must never take
// down the daemon.
//
// enableOSD turns on go-ft8's OSD-2/MRB fallback (deeper decode after BP
// misses) — measured ~1.1–1.7× slower for a real weak-signal recall gain. The
// daemon passes this from ft8.enable_osd (default true).
//
// A nil logger is tolerated (treated as a no-op) so the offline/dev path can
// pass logging.Noop() without ceremony.
func DecodeSlot(samples []int16, enableOSD bool, log logging.Logger) (msgs []goft8.DecodedMessage) {
	if log == nil {
		log = logging.Noop()
	}
	defer func() {
		if r := recover(); r != nil {
			log.WarnWith().
				Interface("panic", r).
				Int("samples", len(samples)).
				Msg("ft8 decode panicked; slot skipped")
			msgs = nil
		}
	}()

	report, err := goft8.DecodeMessagesChecked(samples, goft8.DecoderOptions{EnableOSD: enableOSD})
	if err != nil {
		ev := log.WarnWith().Err(err).Int("samples", len(samples))
		// Surface the typed validation detail as queryable fields. With the
		// zero-value DecoderOptions only DecodeInputError can fire today, but
		// handle both so this stays correct if options are passed later.
		var inErr *goft8.DecodeInputError
		var optErr *goft8.DecoderOptionError
		switch {
		case stderrors.As(err, &inErr):
			ev = ev.Int("got_samples", inErr.GotSamples).Int("want_samples", inErr.WantSamples)
		case stderrors.As(err, &optErr):
			ev = ev.Str("option_field", optErr.Field).Str("option_reason", optErr.Reason)
		}
		ev.Msg("ft8 slot rejected; skipped")
		return nil
	}

	// Per-decode detail is DEBUG, not INFO: it fires 12–16×/slot continuously while
	// FT8 runs (a firehose at info), and the live view is the SPA Band Activity, not
	// the log. At the default info level zerolog gates this to a near-free no-op (no
	// build, no file I/O); set the log level to debug to recover the decode stream for
	// on-air diagnosis. Matches the per-slot summary below, also debug.
	for _, m := range report.Messages {
		log.DebugWith().
			Str("text", m.Text).
			Float64("freq_hz", m.FreqHz).
			Float64("dt_s", m.DTSec).
			Int("snr", m.SNR).
			Float64("sync", m.Sync).
			Msg("ft8 decode")
	}

	d := report.Diagnostics
	log.DebugWith().
		Dur("duration", d.Duration).
		Int("candidates_found", d.CandidatesFound).
		Int("candidates_analyzed", d.CandidatesAnalyzed).
		Int("ldpc_attempts", d.LDPCAttempts).
		Int("ldpc_failures", d.LDPCFailures).
		Int("unpack_failures", d.UnpackFailures).
		Int("unique_messages", d.UniqueMessages).
		Msg("ft8 slot decoded")

	return report.Messages
}

// DecodeFile reads a WAV fixture into an int16 slot and decodes it. The WAV
// must already meet go-ft8's contract (12 kHz, mono, 16-bit PCM); readSlotWAV
// rejects anything else rather than mis-decoding it. enableOSD is forwarded to
// DecodeSlot.
func DecodeFile(path string, enableOSD bool, log logging.Logger) ([]goft8.DecodedMessage, error) {
	samples, err := readSlotWAV(path)
	if err != nil {
		return nil, errors.New(opDecodeFile).WithErr(err)
	}
	return DecodeSlot(samples, enableOSD, log), nil
}
