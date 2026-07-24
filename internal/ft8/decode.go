package ft8

import (
	stderrors "errors"
	"strings"

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

// dropOwnTransmissions removes decodes whose sender (the DE callsign) is our own
// station call — SM decoding its OWN FT8 transmission, which the rig loops back
// into the capture input while keyed (monitor / USB-audio TX bleed). A legitimate
// decode is never FROM our own call, so this is unconditionally safe: it declutters
// Band Activity (our own CQ/RR73 appearing as a phantom station on our TX offset)
// and keeps our own signal out of the sequencer + occupancy. ownCall "" → no-op.
func dropOwnTransmissions(msgs []goft8.DecodedMessage, ownCall string) []goft8.DecodedMessage {
	ownCall = strings.ToUpper(strings.TrimSpace(ownCall))
	if ownCall == "" {
		return msgs
	}
	out := make([]goft8.DecodedMessage, 0, len(msgs))
	for _, m := range msgs {
		// parseMessage upper-cases; from is the transmitting station (the second call
		// of a directed message, the caller of a CQ) — empty for free text / hashed
		// calls, which never match a non-empty ownCall and so pass through.
		if parseMessage(m.Text).from == ownCall {
			continue
		}
		out = append(out, m)
	}
	return out
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
//   - one debug line per decoded message ("heard this");
//   - one debug line per slot with aggregate diagnostics (covers the
//     why-nothing case for every slot, decoding or not);
//   - a warn for a rejected slot or a recovered panic.
//
// Both debug streams are off at the default info level (zerolog gates them to a
// near-free no-op — no build, no file I/O); set the level to debug when bringing
// the live path up or chasing an empty slot to recover the decode stream for
// on-air diagnosis. The live view is the SPA Band Activity, not the log.
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
// must already meet go-ft8's contract (12 kHz, mono, 16-bit PCM) AND be exactly
// one slot long (SlotSamples); readSlotWAV rejects the wrong format and this
// rejects the wrong duration rather than mis-reporting either. enableOSD is
// forwarded to DecodeSlot.
//
// The duration check lives here, not in DecodeSlot: DecodeSlot is fail-soft and
// swallows go-ft8's wrong-length rejection (returning nil), which is correct for
// the live pipeline — the scheduler always emits exactly one slot, so a short
// count is a producer bug to log-and-skip, not to fail on. But the offline path
// takes an arbitrary operator-supplied file; a wrong-duration WAV that DecodeSlot
// silently drops would surface as "0 decodes" with a nil error — success — hiding
// that the file simply isn't a decodable slot. So reject it up front instead.
func DecodeFile(path string, enableOSD bool, log logging.Logger) ([]goft8.DecodedMessage, error) {
	samples, err := readSlotWAV(path)
	if err != nil {
		return nil, errors.New(opDecodeFile).WithErr(err)
	}
	if len(samples) != SlotSamples {
		return nil, errors.New(opDecodeFile).WithMsgf(
			"WAV is %d samples; a decode slot must be exactly %d (%d s at %d Hz)",
			len(samples), SlotSamples, slotSeconds, goft8.SampleRate)
	}
	return DecodeSlot(samples, enableOSD, log), nil
}
