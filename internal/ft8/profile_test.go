package ft8

import (
	"math"
	"math/cmplx"
	"testing"
	"time"

	goft4 "github.com/ColonelBlimp/go-ft8/ft4"
	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/dsp/fourier"
)

// W-0019 slice 1 (ADR 0080): the FT4 profile's lattice, references, waveform
// geometry, timing and admission policy — each pinned against the FT8 profile
// in the same table so the two never silently share a number they should not.

func mkUTC(h, m int, sec float64) time.Time {
	whole := int(sec)
	nanos := int(math.Round((sec - float64(whole)) * 1e9))
	return time.Date(2026, 9, 12, h, m, whole, nanos, time.UTC)
}

func TestProfile_NextSlotBoundary_Lattice(t *testing.T) {
	cases := []struct {
		name string
		p    Profile
		now  time.Time
		want time.Time
	}{
		{"ft8 mid-slot", ProfileFT8, mkUTC(14, 30, 7.4), mkUTC(14, 30, 15)},
		{"ft8 on boundary is strictly after", ProfileFT8, mkUTC(14, 30, 15), mkUTC(14, 30, 30)},
		{"ft4 first half", ProfileFT4, mkUTC(14, 30, 7.4), mkUTC(14, 30, 7.5)},
		{"ft4 on half boundary is strictly after", ProfileFT4, mkUTC(14, 30, 7.5), mkUTC(14, 30, 15)},
		{"ft4 just before the minute", ProfileFT4, mkUTC(14, 30, 59.9), mkUTC(14, 31, 0)},
		{"ft4 sub-millisecond before a boundary", ProfileFT4, mkUTC(14, 30, 22.4999), mkUTC(14, 30, 22.5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.p.nextSlotBoundary(tc.now)
			require.True(t, got.Equal(tc.want), "next boundary after %v = %v, want %v", tc.now, got, tc.want)
		})
	}
}

func TestProfile_SlotRef_ParityAndFormat(t *testing.T) {
	cases := []struct {
		name       string
		p          Profile
		start      time.Time
		wantUTC    string
		wantPeriod string
	}{
		// FT8 references are unchanged: whole seconds, RFC3339 without a fraction.
		{"ft8 :00 even", ProfileFT8, mkUTC(14, 30, 0), "2026-09-12T14:30:00Z", "even"},
		{"ft8 :15 odd", ProfileFT8, mkUTC(14, 30, 15), "2026-09-12T14:30:15Z", "odd"},
		{"ft8 mid-slot floors", ProfileFT8, mkUTC(14, 30, 22), "2026-09-12T14:30:15Z", "odd"},
		// FT4: 7.5 s lattice, parity floor(unixMillis/7500) mod 2, millisecond format.
		{"ft4 :00.0 even", ProfileFT4, mkUTC(14, 30, 0), "2026-09-12T14:30:00.000Z", "even"},
		{"ft4 :07.5 odd", ProfileFT4, mkUTC(14, 30, 7.5), "2026-09-12T14:30:07.500Z", "odd"},
		{"ft4 :15.0 even", ProfileFT4, mkUTC(14, 30, 15), "2026-09-12T14:30:15.000Z", "even"},
		{"ft4 :22.5 odd", ProfileFT4, mkUTC(14, 30, 22.5), "2026-09-12T14:30:22.500Z", "odd"},
		{"ft4 mid-slot floors to the half boundary", ProfileFT4, mkUTC(14, 30, 11.2), "2026-09-12T14:30:07.500Z", "odd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := tc.p.SlotRefFromTime(tc.start)
			require.Equal(t, tc.wantUTC, ref.StartUTC)
			require.Equal(t, tc.wantPeriod, ref.Period)
			// The sequencers parse StartUTC back with time.RFC3339 and the TX-slot
			// match compares the string exactly, so the reference must round-trip
			// through both to the same boundary and the same string.
			back, err := time.Parse(time.RFC3339, ref.StartUTC)
			require.NoError(t, err)
			require.Equal(t, ref.StartUTC, tc.p.SlotRefFromTime(back).StartUTC)
		})
	}
}

func TestProfile_AdmitsRung_Edges(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		w := p.LateWindow.Seconds()
		require.False(t, p.admitsRung(-0.001), "%s: before the slot begins", p.Name)
		require.True(t, p.admitsRung(0), "%s: at the boundary", p.Name)
		require.True(t, p.admitsRung(w), "%s: exactly at the late window (%.3f s)", p.Name, w)
		require.False(t, p.admitsRung(w+0.001), "%s: past the late window", p.Name)
	}
	require.Equal(t, 4.5, ProfileFT8.LateWindow.Seconds())
	require.Equal(t, 2.0, ProfileFT4.LateWindow.Seconds())
}

// dominantHz returns the strongest bin of a waveform's spectrum in Hz.
func dominantHz(wave []float32) float64 {
	n := len(wave)
	in := make([]float64, n)
	for i, v := range wave {
		in[i] = float64(v)
	}
	fft := fourier.NewFFT(n)
	coeffs := fft.Coefficients(nil, in)
	best, bestMag := 0, 0.0
	for k, c := range coeffs {
		if m := cmplx.Abs(c); m > bestMag {
			best, bestMag = k, m
		}
	}
	return float64(best) * float64(goft8.SampleRate) / float64(n)
}

func TestProfile_FT4_WaveformGeometry(t *testing.T) {
	// FT4 sync reference and waveform origin, as ratified (ADR 0080).
	require.Equal(t, 500*time.Millisecond, ProfileFT4.SyncStart)
	require.Equal(t, 452*time.Millisecond, ProfileFT4.WaveformOrigin)
	require.Equal(t, 7500*time.Millisecond, ProfileFT4.Slot)
	require.Equal(t, 90000, ProfileFT4.SlotSamples)

	wave, err := ProfileFT4.EncodeWaveform("CQ K1ABC FN42", 1500)
	require.NoError(t, err)
	require.Len(t, wave, goft4.WaveformSamples, "FT4 waveform is (103+2) symbols of 576 samples")

	// Tone spacing: a steady tone 0 and a steady tone 1 sit one bin apart at
	// SampleRate/SamplesPerSymbol = 20.833 Hz (FT8: 6.25 Hz).
	steady := func(p Profile, tone uint8) []float32 {
		tones := make([]uint8, p.ToneCount)
		for i := range tones {
			tones[i] = tone
		}
		return p.Modulate(tones, 1500)
	}
	for _, tc := range []struct {
		p       Profile
		spacing float64
	}{{ProfileFT4, 12000.0 / 576}, {ProfileFT8, 12000.0 / 1920}} {
		f0 := dominantHz(steady(tc.p, 0))
		f1 := dominantHz(steady(tc.p, 1))
		require.InDelta(t, 1500, f0, 0.5, "%s tone 0 sits at the offset", tc.p.Name)
		require.InDelta(t, tc.spacing, f1-f0, 0.5, "%s tone spacing", tc.p.Name)
	}
}

func TestProfile_FT4_RoundTrip_DTNearZero(t *testing.T) {
	// The offline proof for the ratified timing: a waveform whose PCM sample
	// zero sits at slot + 0.452 s puts its first Costas array at + 0.500 s, so
	// go-ft8's FT4 decoder (DT relative to SyncStartSeconds) reports DT ≈ 0.
	const text = "CQ K1ABC FN42"
	slot, err := ProfileFT4.EncodeToSlot(text, 1500, ProfileFT4.WaveformOrigin.Seconds())
	require.NoError(t, err)
	require.Len(t, slot, ProfileFT4.SlotSamples)

	msgs := goft4.DecodeMessages(slot)
	require.NotEmpty(t, msgs, "the FT4 decoder must recover SM's own FT4 waveform")
	var got *goft4.DecodedMessage
	for i := range msgs {
		if msgs[i].Text == text {
			got = &msgs[i]
		}
	}
	require.NotNil(t, got, "decoded %+v, want %q", msgs, text)
	require.InDelta(t, 0, got.DTSec, 0.05, "DT relative to the 0.5 s sync reference")
	require.InDelta(t, 1500, got.FreqHz, 2)
}

func TestProfile_FT8_RoundTrip_DTCharacterised(t *testing.T) {
	// FT8 keeps its shipped convention (sample zero at +0.5 s). Checked, not
	// assumed (ADR 0080): the waveform's one-symbol Gaussian overhang puts the
	// first Costas array at +0.66 s, so the FT8 decoder reports DT ≈ +0.16 s —
	// well inside WSJT-X's tolerance, and the profile records it as SyncStart
	// rather than claiming FT4's exact alignment.
	const text = "CQ K1ABC FN42"
	slot, err := ProfileFT8.EncodeToSlot(text, 1500, ProfileFT8.WaveformOrigin.Seconds())
	require.NoError(t, err)
	msgs := goft8.DecodeMessages(slot)
	require.NotEmpty(t, msgs)
	require.Equal(t, text, msgs[0].Text)
	symbol := float64(ProfileFT8.SamplesPerSymbol) / goft8.SampleRate
	require.InDelta(t, symbol, msgs[0].DTSec, 0.05, "one symbol late relative to the 0.5 s reference")
	require.Equal(t, ProfileFT8.WaveformOrigin+time.Duration(symbol*float64(time.Second)), ProfileFT8.SyncStart)
}

func TestProfile_FT4_MaxDecodableSkip(t *testing.T) {
	// The second FT4 Costas array is tone 33; with one overhang symbol in front
	// it begins at waveform symbol 34, i.e. 34 × 576 samples = 1.632 s in. That
	// is the most head the controller may drop (ADR 0080: the post-admission
	// check that stays authoritative behind the +2.000 s admission policy).
	require.Equal(t, 34*576, ProfileFT4.maxDecodableSkip(goft4.WaveformSamples))
	// FT8 is unchanged: tone 36, symbol 37 of 81.
	require.Equal(t, 37*1920, ProfileFT8.maxDecodableSkip((79+2)*1920))
}

func TestProfile_FT4_TruncatedStartDecodes(t *testing.T) {
	// A rung admitted at the +2.000 s edge, keyed 200 ms later, drops 2.2 −
	// 0.452 ≈ 1.75 s of head: past the second Costas array, which is exactly the
	// case maxDecodableSkip refuses. Within the skip limit the truncated
	// remainder still decodes on the surviving arrays.
	const text = "CQ K1ABC FN42"
	wave, err := ProfileFT4.EncodeWaveform(text, 1500)
	require.NoError(t, err)

	limit := ProfileFT4.maxDecodableSkip(len(wave))
	require.Greater(t, limit, 0)
	require.Less(t, limit, 2*576*40)

	for _, skipSamples := range []int{576 * 10, limit} {
		rest := truncateHead(append([]int16(nil), wave...), skipSamples, ProfileFT4.rampSamples)
		slot := make([]int16, ProfileFT4.SlotSamples)
		origin := int(ProfileFT4.WaveformOrigin.Seconds()*goft8.SampleRate) + skipSamples
		copy(slot[origin:], rest)
		msgs := goft4.DecodeMessages(slot)
		found := false
		for _, m := range msgs {
			if m.Text == text {
				found = true
				require.InDelta(t, 0, m.DTSec, 0.05, "truncate, don't shift: DT stays on the timebase")
			}
		}
		require.True(t, found, "skip %d samples (%.3f s): want %q, got %+v", skipSamples, float64(skipSamples)/goft8.SampleRate, text, msgs)
	}
}

func TestProfile_NextSlotPeriod_DiffersByLattice(t *testing.T) {
	// 14:30:09 — FT8's next slot is :15 (odd on its lattice); FT4's next slot
	// is :15.0 too, but that is an EVEN FT4 index (7.5 s slots since the epoch).
	at := mkUTC(14, 30, 9)
	require.Equal(t, "odd", ProfileFT8.nextSlotPeriod(at))
	require.Equal(t, "even", ProfileFT4.nextSlotPeriod(at))
	// 14:30:03 — both lattices' next slots are odd (:15 / :07.5).
	at = mkUTC(14, 30, 3)
	require.Equal(t, "odd", ProfileFT8.nextSlotPeriod(at))
	require.Equal(t, "odd", ProfileFT4.nextSlotPeriod(at))
}

// The sequencer's rung timing follows its profile: on the FT4 lattice a rung
// is admitted up to +2.000 s into our 7.5 s slot and deferred past it, where
// the FT8 profile would still admit it (4.5 s). Same ladder, different clock.
func TestSequencer_FT4Profile_LateWindowOnItsOwnLattice(t *testing.T) {
	r := &seqRecorder{}
	s := newTestSeq(r)
	s.profile = ProfileFT4

	// K1ABC heard in the even FT4 slot at the epoch; we answer in the odd slots
	// (7.5 s, 22.5 s, …). The start lands in THEIR slot, so nothing fires yet.
	theirSlot := ProfileFT4.SlotRefFromTime(time.Unix(0, 0).UTC())
	require.Equal(t, "even", theirSlot.Period)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", theirSlot.StartUTC, 1500, 14.080, time.Unix(0, 0).UTC()))
	require.Empty(t, r.sentMsgs())

	// Their slot 15.0–22.5 decodes; OnSlot lands 1.0 s into our 22.5 slot → sent.
	s.OnSlot(ProfileFT4.SlotRefFromTime(time.UnixMilli(15_000).UTC()), nil, time.UnixMilli(22_500+1_000).UTC())
	require.Len(t, r.sentMsgs(), 1, "1.0 s into a 7.5 s slot is inside FT4's window")

	// 2.5 s into our 37.5 slot: past FT4's +2.000 s admission edge → deferred.
	s.OnSlot(ProfileFT4.SlotRefFromTime(time.UnixMilli(30_000).UTC()), nil, time.UnixMilli(37_500+2_500).UTC())
	require.Len(t, r.sentMsgs(), 1, "2.5 s is past FT4's late window although inside FT8's")
	require.True(t, s.Active(), "a deferred rung does not end the QSO")

	// Exactly +2.000 s into our 52.5 slot: the edge itself is admitted.
	s.OnSlot(ProfileFT4.SlotRefFromTime(time.UnixMilli(45_000).UTC()), nil, time.UnixMilli(52_500+2_000).UTC())
	require.Len(t, r.sentMsgs(), 2, "the +2.000 s edge is admitted")

	// The same 2.5 s lateness under the FT8 profile is admitted (4.5 s window).
	r8 := &seqRecorder{}
	s8 := newTestSeq(r8)
	require.NoError(t, s8.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", ProfileFT8.SlotRefFromTime(time.Unix(0, 0).UTC()).StartUTC, 1500, 14.074, time.Unix(0, 0).UTC()))
	s8.OnSlot(ProfileFT8.SlotRefFromTime(time.Unix(30, 0).UTC()), nil, time.Unix(45+2, 500_000_000).UTC())
	require.Len(t, r8.sentMsgs(), 1)
}
