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
	// DialMHz is the rig dial this slot's audio was CAPTURED on (Slot.DialMHz),
	// 0/omitted when unknown. Publication lags capture by the decode
	// (~0.7–1.6 s), so consumers must attribute these decodes to a band from
	// THIS value, never from live rig state — a QSY in that gap otherwise files
	// stations heard on band A as band B (wrong PSK Reporter spots, wrong-band
	// Band Activity rows). DialChanged suppression cannot catch it: the move
	// postdates the capture window. Same attribution rule, and the same reason,
	// as OccupancyReport.DialMHz (review P1, 2026-08-07).
	DialMHz float64 `json:"dial_mhz,omitempty"`
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
// dialMHz is the slot's CAPTURED dial (Slot.DialMHz, 0 = unknown) — stamped
// here so no consumer has to attribute the decodes against live rig state
// (see DecodeReport.DialMHz).
func newDecodeReport(slot SlotRef, dialMHz float64, msgs []goft8.DecodedMessage) DecodeReport {
	lines := make([]DecodeLine, 0, len(msgs))
	for _, m := range msgs {
		lines = append(lines, DecodeLine{Text: m.Text, FreqHz: m.FreqHz, DTSec: m.DTSec, SNR: m.SNR})
	}
	return DecodeReport{Slot: slot, Decodes: lines, DialMHz: dialMHz}
}

// DecodeSlot decodes one 15-second FT8 slot from 12 kHz mono signed-16-bit
// PCM samples, logs each decoded message as a structured line, and returns
// the RICH decode set — every parse status, unfiltered. It is the STATELESS
// one-shot seam (a fresh decoder per call) for the offline paths: DecodeFile,
// the developer tools, and round-trip tests. The live path holds a
// slotDecoder instead, whose retained per-stream state resolves cross-slot
// hash references; both share one implementation, so the checked-input
// contract (a malformed slot is rejected up front with a typed error, never
// silently zero-padded), the fail-soft recover, the debug/warn logging policy
// and the nil-logger tolerance documented on slotDecoder.decode hold here
// identically.
//
// Callers wanting only operator-usable rows apply curateDecodes (or
// dropUnparsed) to the result — filtering is the curated branch's business,
// not the decode seam's (design §4 prerequisite 2).
//
// enableOSD turns on go-ft8's OSD-2/MRB fallback (deeper decode after BP
// misses) — measured ~1.1–1.7× slower for a real weak-signal recall gain. The
// daemon passes this from ft8.enable_osd (default true).
func DecodeSlot(samples []int16, enableOSD bool, log logging.Logger) []goft8.DecodedMessage {
	return newSlotDecoder(enableOSD, log).decode(samples)
}

// DecodeFile reads a WAV fixture into an int16 slot and decodes it, returning
// the RICH result like DecodeSlot (text-less payload rows included). The WAV
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

// zeroSlot is the all-silence slot skip feeds the stateful decoder for every
// physical slot SM refuses to decode. Shared and read-only.
var zeroSlot = make([]int16, SlotSamples)

// slotDecoder wraps ONE stateful goft8.Decoder for one receiver stream — one
// capture session's decode loop. go-ft8 retains callsign-hash and A7 hint
// state across adjacent slots (its a7.go), which is what lets a "<...>" hash
// reference resolve to a call heard two slots earlier; the instance is NOT
// goroutine-safe, so it lives loop-local in decodeLoop (one goroutine per
// capture session, sessions serialised by the Service — and a fresh decoder
// per session means no stale hint/hash context survives an operator-length
// gap or a band change).
//
// decode returns the RICH result — every parse status, own-TX included. The
// curated filters (curateDecodes) apply strictly downstream at the branch
// point, so the future evidence branch can tap the rich result upstream of
// them (design §4 prerequisite 2).
type slotDecoder struct {
	dec *goft8.Decoder
	log logging.Logger
}

// newSlotDecoder builds the per-stream decoder. enableOSD and the logging
// policy match DecodeSlot's documentation; a nil logger is tolerated.
func newSlotDecoder(enableOSD bool, log logging.Logger) *slotDecoder {
	if log == nil {
		log = logging.Noop()
	}
	return &slotDecoder{
		dec: goft8.NewDecoderWithOptions(goft8.DecoderOptions{EnableOSD: enableOSD}),
		log: log,
	}
}

// decode decodes one slot with the stream's retained state. Fail-soft and
// logging policy are exactly DecodeSlot's (see its comment): per-decode and
// per-slot diagnostics at debug, a warn for a rejected slot or a recovered
// panic, nil on failure. A rejected slot does not advance decoder state
// (go-ft8 validates before advancing).
func (d *slotDecoder) decode(samples []int16) (msgs []goft8.DecodedMessage) {
	defer func() {
		if r := recover(); r != nil {
			d.log.WarnWith().
				Interface("panic", r).
				Int("samples", len(samples)).
				Msg("ft8 decode panicked; slot skipped")
			msgs = nil
		}
	}()

	report, err := d.dec.DecodeMessagesChecked(samples)
	if err != nil {
		ev := d.log.WarnWith().Err(err).Int("samples", len(samples))
		// Surface the typed validation detail as queryable fields.
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

	// Per-decode detail is DEBUG, not INFO: it fires 12–16×/slot continuously
	// while FT8 runs (a firehose at info), and the live view is the SPA Band
	// Activity, not the log. At the default info level zerolog gates this to a
	// near-free no-op; set the level to debug to recover the decode stream for
	// on-air diagnosis. This logs the RICH stream — payload-only decodes
	// included — matching what the evidence branch will capture.
	for _, m := range report.Messages {
		d.log.DebugWith().
			Str("text", m.Text).
			Float64("freq_hz", m.FreqHz).
			Float64("dt_s", m.DTSec).
			Int("snr", m.SNR).
			Float64("sync", m.Sync).
			Msg("ft8 decode")
	}

	diag := report.Diagnostics
	d.log.DebugWith().
		Dur("duration", diag.Duration).
		Int("candidates_found", diag.CandidatesFound).
		Int("candidates_analyzed", diag.CandidatesAnalyzed).
		Int("ldpc_attempts", diag.LDPCAttempts).
		Int("ldpc_failures", diag.LDPCFailures).
		Int("unpack_failures", diag.UnpackFailures).
		Int("unique_messages", diag.UniqueMessages).
		Msg("ft8 slot decoded")

	return report.Messages
}

// skip advances decoder state across a physical slot SM refuses to decode
// (own TX, dial moved) by decoding one slot of silence: that performs the
// correct transition — clears the current A7 parity bucket (we could not hear
// that window), advances parity, preserves the hash table — while producing
// nothing. Without it, one skipped slot leaves the parity-keyed A7 hint
// buckets misaligned beyond the run that skipped it. Internal state
// advancement only: callers publish the slot's real reason (tx, dial moved)
// and no output exists to discard. Operator decision 2026-08-09, with the
// cost measured on v0.8.0 (6–8 ms clearing a 26–40-hint bucket, ~0.1 ms once
// empty); replace with goft8 Decoder.SkipSlot() when a release provides one.
func (d *slotDecoder) skip() {
	defer func() {
		if r := recover(); r != nil {
			d.log.WarnWith().Interface("panic", r).Msg("ft8 skip-slot decode panicked")
		}
	}()
	d.dec.DecodeMessages(zeroSlot)
	// The advance's only trace: the parity consequence is A7-internal and not
	// otherwise observable, so this line is both the on-air diagnostic ("did
	// the decoder advance over my TX slot?") and the executable guard the
	// loop-level test pins the once-per-skipped-slot rule against.
	d.log.DebugWith().Msg("ft8 skipped slot; decoder state advanced")
}

// curateDecodes is THE curated branch's boundary (design §4 prerequisite 2):
// the parse-status filter then the own-transmission drop. Every view-shaped
// consumer — Band Activity, the RX decode log, the sequencer, the PSK sink —
// receives its output; the evidence branch taps the rich slice upstream, so
// neither filter may mutate its input (both copy).
func curateDecodes(msgs []goft8.DecodedMessage, ownCall string) []goft8.DecodedMessage {
	return dropOwnTransmissions(dropUnparsed(msgs), ownCall)
}

// dropUnparsed filters a slot's decodes to those with canonical text
// (ParseStatusParsed) for the CURATED consumers — Band Activity, the RX decode
// log, the sequencer, PSK Reporter — which all assume usable text. go-ft8
// v0.8.0 returns CRC-valid but unsupported/reserved/invalid payloads as
// text-less messages (v0.7.1 rejected them); unfiltered they render blank
// Band Activity rows and malformed RX log records (codex P2 on 1df6d94d).
// Applied via curateDecodes, the curated branch's boundary.
//
// Copies rather than filtering in place: the input is the rich slice the
// evidence branch taps, and must survive intact.
func dropUnparsed(msgs []goft8.DecodedMessage) []goft8.DecodedMessage {
	kept := make([]goft8.DecodedMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.ParseStatus == goft8.ParseStatusParsed {
			kept = append(kept, m)
		}
	}
	return kept
}
