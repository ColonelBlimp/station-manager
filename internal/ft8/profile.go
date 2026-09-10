package ft8

import (
	"math"
	"time"

	goft4 "github.com/ColonelBlimp/go-ft8/ft4"
	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Profile is one FT-family mode's timing and modulation geometry (ADR 0080).
// FT8 and FT4 share their 77-bit messages, LDPC code and the sequencer ladders;
// they differ only in what this value carries. Everything below the sequencer
// that used to hard-code the FT8 numbers reads them from the profile instead.
type Profile struct {
	// Name is the ADIF mode stamped on a logged QSO, its PSK Reporter spot and
	// the decode log's transmit line.
	Name string

	// Slot is the transmit / receive period. Boundaries fall on multiples of
	// Slot from the Unix epoch (which is itself a boundary for both modes):
	// FT8 …:00/:15/:30/:45; FT4 …:00.0/:07.5/:15.0/:22.5/….
	Slot time.Duration

	// SlotSamples is one Slot of 12 kHz mono samples — the decoder's frame
	// length and the scheduler's ring capacity.
	SlotSamples int

	// SamplesPerSymbol is the channel-symbol length in samples; the tone
	// spacing is SampleRate / SamplesPerSymbol.
	SamplesPerSymbol int

	// GfskBT is the Gaussian pulse's bandwidth-time product (QEX Table 4).
	GfskBT float64

	// ToneCount is the encoder's symbol count (Costas + data); the modulated
	// waveform adds one symbol of Gaussian overhang at each end.
	ToneCount int

	// rampSamples is the raised-cosine edge taper the modulator applies at the
	// very start and end of a waveform. FT8 ramps over T/8 (QEX §5); FT4 ramps
	// over its whole leading and trailing ramp symbol, as go-ft8's jt9 oracle
	// renders it.
	rampSamples int

	// SyncStart is where the FIRST Costas array lands relative to the slot
	// boundary under this profile's WaveformOrigin. The receiver's DT = 0
	// reference is 0.5 s for both modes (go-ft8 ft4.SyncStartSeconds; WSJT-X's
	// nominal start), so FT4 hits it exactly and FT8 lands one symbol late.
	SyncStart time.Duration

	// WaveformOrigin is where PCM sample zero belongs relative to the slot
	// boundary. FT4's waveform carries one ramp symbol BEFORE the first Costas
	// array, so its origin is SyncStart minus one symbol (0.452 s — operator
	// ruling 2026-09-10, ADR 0080). FT8 keeps the convention the shipped,
	// on-air-validated controller has always used: sample zero at 0.5 s, which
	// puts its first Costas array one symbol later (SyncStart 0.66 s; the FT8
	// decoder reports DT ≈ +0.16 s on SM's own waveform — profile_test.go).
	WaveformOrigin time.Duration

	// LateWindow is the ADMISSION policy: the latest into our slot a sequencer
	// will start a rung. It is not a decodability guarantee — the controller
	// adds its pre-key lead and CAT latency after admission, and the post-key
	// maxDecodableSkip check stays authoritative (ADR 0080).
	LateWindow time.Duration

	// SignalWidthHz is the audio bandwidth one signal occupies upward from its
	// base tone (tones × spacing), for occupancy and the TX-offset picker.
	SignalWidthHz int

	// costasResyncTone is the tone index of the SECOND Costas array. A late
	// start truncates the head (ADR 0032) and the receiver re-syncs on the
	// remaining arrays; once the truncation reaches this array too little
	// survives to call the transmission sent, so the controller refuses it.
	costasResyncTone int

	// encode turns a standard message into the mode's tone sequence.
	encode func(text string) ([]uint8, error)
}

// ProfileFT8 is the shipped FT8 geometry (QEX Table 4; ADR 0029/0032).
var ProfileFT8 = Profile{
	Name:             "FT8",
	Slot:             15 * time.Second,
	SlotSamples:      goft8.SampleRate * 15,
	SamplesPerSymbol: 1920,
	GfskBT:           2.0,
	ToneCount:        79,
	rampSamples:      1920 / 8,
	SyncStart:        660 * time.Millisecond, // origin + one 0.16 s symbol; measured DT ≈ +0.155 s
	WaveformOrigin:   500 * time.Millisecond,
	LateWindow:       4500 * time.Millisecond,
	SignalWidthHz:    50,
	costasResyncTone: 36,
	encode: func(text string) ([]uint8, error) {
		enc, err := goft8.EncodeStandardMessage(text)
		if err != nil {
			return nil, err
		}
		return enc.Tones[:], nil
	},
}

// ProfileFT4 is the FT4 geometry from go-ft8's exported constants (ADR 0080).
// The waveform origin sits one ramp symbol before the sync reference; the late
// window is the +2.000 s admission policy the operator ratified 2026-09-10.
var ProfileFT4 = Profile{
	Name:             "FT4",
	Slot:             7500 * time.Millisecond,
	SlotSamples:      goft4.SlotSamples,
	SamplesPerSymbol: goft4.SamplesPerSymbol,
	GfskBT:           goft4.GFSKBT,
	ToneCount:        goft4.ToneSymbols,
	rampSamples:      goft4.SamplesPerSymbol,
	SyncStart:        time.Duration(goft4.SyncStartSeconds * float64(time.Second)),
	WaveformOrigin:   time.Duration(goft4.NominalStartSeconds * float64(time.Second)),
	LateWindow:       2000 * time.Millisecond,
	SignalWidthHz:    84, // ceil(4 × 20.833 Hz) — goft4.SignalWidthHz rounded up to whole Hz
	costasResyncTone: 33,
	encode: func(text string) ([]uint8, error) {
		enc, err := goft4.EncodeStandardMessage(text)
		if err != nil {
			return nil, err
		}
		return enc.Tones[:], nil
	},
}

// slotStartFormat renders a boundary with millisecond precision. The FT8
// lattice has no fractional part and keeps plain RFC3339 so its wire strings,
// decode-log lines and the TX-slot match are byte-identical to before.
const slotStartFormat = "2006-01-02T15:04:05.000Z07:00"

// admitsRung is the late-window admission policy for a rung that would start dt
// seconds into our slot: never before the slot has begun, and never past
// LateWindow. Both edges are explicit — exactly LateWindow is admitted.
func (p Profile) admitsRung(dtSec float64) bool {
	return dtSec >= 0 && dtSec <= p.LateWindow.Seconds()
}

// slotStart floors t to the boundary of the slot containing it, on this
// profile's lattice (integer milliseconds from the Unix epoch, which is a
// boundary for every FT-family period).
func (p Profile) slotStart(t time.Time) time.Time {
	ms := t.UnixMilli()
	slotMs := p.Slot.Milliseconds()
	return time.UnixMilli(ms - ms%slotMs).UTC()
}

// nextSlotBoundary returns the next boundary strictly after now.
//
// Examples (FT8 / FT4, now → next):
//
//	14:30:07.4 → 14:30:15.000 / 14:30:07.500
//	14:30:15.0 → 14:30:30.000 / 14:30:22.500  (strictly after)
//	14:30:59.9 → 14:31:00.000 / 14:31:00.000
func (p Profile) nextSlotBoundary(now time.Time) time.Time {
	return p.slotStart(now).Add(p.Slot)
}

// SlotRefFromTime builds a SlotRef for the slot containing start. Period is
// the even/odd alternation WSJT-X uses ("Tx even/1st"): the slot index since
// the epoch, modulo two — :00/:30 even and :15/:45 odd for FT8; :00.0/:15.0
// even and :07.5/:22.5 odd for FT4.
func (p Profile) SlotRefFromTime(start time.Time) SlotRef {
	// Floor to the lattice so a mid-slot time yields the same StartUTC as its
	// boundary; wasTxSlot's exact-string match depends on every producer
	// formatting the same instant the same way.
	start = p.slotStart(start)
	period := "even"
	if (start.UnixMilli()/p.Slot.Milliseconds())%2 != 0 {
		period = "odd"
	}
	layout := time.RFC3339
	if p.Slot%time.Second != 0 {
		layout = slotStartFormat
	}
	return SlotRef{StartUTC: start.Format(layout), Period: period}
}

// minLiveWindowSamples is the per-window delivery floor below which a capture
// source counts as starved: a quarter slot, far below a healthy window and far
// above a trickle from a dead stream (deadsource.go).
func (p Profile) minLiveWindowSamples() int64 {
	return int64(p.SlotSamples / 4)
}

// waveformSamples is the bare GFSK waveform length: the Gaussian pulse adds one
// symbol of overhang at each end, so (tones+2) symbols.
func (p Profile) waveformSamples() int {
	return (p.ToneCount + 2) * p.SamplesPerSymbol
}

// Modulate turns a tone sequence (each value below the mode's tone count) into
// a normalised GFSK waveform in [-1, 1] at the given audio offset, sampled at
// goft8.SampleRate. The returned length is (len(tones)+2)*SamplesPerSymbol:
// the Gaussian pulse spans three symbols, so the smoothed signal runs one
// symbol-width past each end of the nominal tone sequence.
//
// Modulation is continuous-phase GFSK, the scheme WSJT-X uses (QEX §5): each
// tone is a frequency `offset + tone × SampleRate/SamplesPerSymbol`, and
// transitions are smoothed by a Gaussian pulse of the profile's BT rather than
// hard-switched. Phase is integrated sample by sample, with a raised-cosine ramp
// of rampSamples at the very start and end to suppress key clicks.
//
// Pure and deterministic. offsetHz is the audio frequency of tone 0 (the base
// tone) — the same reference the decoder reports as FreqHz, so an offset chosen
// from the clear-offset picker round-trips back as the decoded frequency.
func (p Profile) Modulate(tones []uint8, offsetHz float64) []float32 {
	nsps := p.SamplesPerSymbol
	nsym := len(tones)
	if nsym == 0 {
		return nil
	}
	nwave := (nsym + 2) * nsps
	fs := float64(goft8.SampleRate)

	// GFSK frequency pulse over three symbols. For a run of equal symbols the
	// overlapping pulses sum to unity, so a steady tone produces a constant
	// frequency offset; at transitions the sum slews smoothly between tones.
	pulse := make([]float64, 3*nsps)
	c := math.Pi * math.Sqrt(2.0/math.Ln2)
	for i := range pulse {
		tt := (float64(i) - 1.5*float64(nsps)) / float64(nsps)
		pulse[i] = 0.5 * (math.Erf(c*p.GfskBT*(tt+0.5)) - math.Erf(c*p.GfskBT*(tt-0.5)))
	}

	// Per-sample angular frequency: the carrier plus each symbol's pulse-shaped
	// tone contribution. dphiPeak is one tone-step of phase advance per sample.
	dphi := make([]float64, nwave)
	base := 2 * math.Pi * offsetHz / fs
	for k := range dphi {
		dphi[k] = base
	}
	dphiPeak := 2 * math.Pi * txModIndex / float64(nsps)
	for j := 0; j < nsym; j++ {
		ib := j * nsps
		tone := float64(tones[j])
		for i := 0; i < 3*nsps; i++ {
			dphi[ib+i] += dphiPeak * pulse[i] * tone
		}
	}

	// Integrate phase → samples.
	wave := make([]float32, nwave)
	phi := 0.0
	for k := 0; k < nwave; k++ {
		wave[k] = float32(math.Sin(phi))
		phi += dphi[k]
		if phi > 2*math.Pi {
			phi -= 2 * math.Pi
		}
	}

	// Raised-cosine ramp on the leading/trailing edges to suppress key clicks.
	nramp := p.rampSamples
	if nramp > nwave/2 {
		nramp = nwave / 2
	}
	for i := 0; i < nramp; i++ {
		w := float32(0.5 * (1 - math.Cos(math.Pi*float64(i)/float64(nramp))))
		wave[i] *= w
		wave[nwave-1-i] *= w
	}
	return wave
}

// EncodeToSlot encodes a standard message and lays its modulated waveform into a
// full slot buffer (SlotSamples int16 at goft8.SampleRate), starting dtSec into
// the slot with silence before and after — the offline building block the
// round-trip tests use. offsetHz is the base-tone audio frequency.
//
// Errors only if the message isn't an encodable standard message. go-ft8
// encodes standard messages, /P and /R suffix calls, ARRL Field Day exchanges,
// and type-4 compound/nonstandard calls — but a type-4 message carries only
// CQ/RR73/73 with the partner call hashed, so a prefix-compound directed message
// that needs a grid or report (e.g. "PJ4/NA2AA 7Q5MLV KH53") is still rejected.
// Free text is rejected too.
func (p Profile) EncodeToSlot(text string, offsetHz, dtSec float64) ([]int16, error) {
	const op errors.Op = "ft8.Profile.EncodeToSlot"
	tones, err := p.encode(text)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsgf("encode %q", text)
	}
	wave := p.Modulate(tones, offsetHz)

	slot := make([]int16, p.SlotSamples)
	start := int(dtSec * float64(goft8.SampleRate))
	if start < 0 {
		start = 0
	}
	for i, v := range wave {
		k := start + i
		if k >= len(slot) {
			break // waveform runs past the slot end — truncate the silent tail
		}
		slot[k] = int16(float64(v) * txAmplitude * math.MaxInt16)
	}
	return slot, nil
}

// EncodeWaveform encodes a standard message to the BARE GFSK waveform as int16
// PCM (no slot padding: ~12.96 s for FT8, ~5.04 s for FT4). This is the shape
// the controller transmits: the tones come out on the synchronised timebase
// with no leading silence, and there is no trailing silence to hold PTT into
// the next slot. Errors only on an unencodable standard message.
func (p Profile) EncodeWaveform(text string, offsetHz float64) ([]int16, error) {
	const op errors.Op = "ft8.Profile.EncodeWaveform"
	tones, err := p.encode(text)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsgf("encode %q", text)
	}
	wave := p.Modulate(tones, offsetHz)
	out := make([]int16, len(wave))
	for i, v := range wave {
		out[i] = int16(float64(v) * txAmplitude * math.MaxInt16)
	}
	return out, nil
}

// maxDecodableSkip is the largest head-truncation, in samples, that still leaves
// a transmission worth calling sent — everything from the second Costas array
// onward (FT8: tones 36–42 of 79; FT4: tones 33–36 of 103). Scaled off the
// waveform's own length rather than assuming a standard-length message, so it
// holds for any tone sequence. Modulate prepends one symbol of Gaussian
// overhang, hence tone N begins at waveform symbol N+1.
func (p Profile) maxDecodableSkip(waveLen int) int {
	return waveLen * (p.costasResyncTone + 1) / (p.ToneCount + 2)
}
