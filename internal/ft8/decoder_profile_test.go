package ft8

import (
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/stretchr/testify/require"
)

// W-0019 slice 2 (ADR 0080): the live decoder wrapper is profile-aware. An
// FT4 capture session decodes 7.5 s slots with go-ft8's FT4 decoder, keeps
// the stream-scoped skip/reset contract, and the decode loop publishes FT4 rows
// on FT4 slot references. FT8 behaviour is pinned unchanged alongside.

// mixSlotOn renders several messages into one slot of the profile at their
// offsets, each at the profile's waveform origin, summed at reduced amplitude.
func mixSlotOn(tb testing.TB, p Profile, msgs map[string]float64) []int16 {
	tb.Helper()
	sum := make([]int32, p.SlotSamples)
	for text, offset := range msgs {
		slot, err := p.EncodeToSlot(text, offset, p.WaveformOrigin.Seconds())
		if err != nil {
			tb.Fatalf("%s EncodeToSlot(%q): %v", p.Name, text, err)
		}
		for i, v := range slot {
			sum[i] += int32(float64(v) * 0.45)
		}
	}
	out := make([]int16, p.SlotSamples)
	for i, v := range sum {
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out[i] = int16(v)
	}
	return out
}

func TestSlotDecoder_DecodesOwnWaveform_BothProfiles(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		t.Run(p.Name, func(t *testing.T) {
			d := newSlotDecoder(p, true, logging.Noop())
			slot := mixSlotOn(t, p, map[string]float64{"CQ K1ABC FN42": 1500, "CQ A61DI LL64": 2200})
			msgs, ok := d.decode(slot)
			require.True(t, ok, "%s: a well-formed slot must not be rejected", p.Name)
			texts := textsOf(msgs)
			require.Contains(t, texts, "CQ K1ABC FN42")
			require.Contains(t, texts, "CQ A61DI LL64")
			for _, m := range msgs {
				require.InDelta(t, 0, m.DTSec, 0.2, "%s: %q on the timebase", p.Name, m.Text)
			}
		})
	}
}

func TestSlotDecoder_FT4_RejectsAnFT8LengthSlot(t *testing.T) {
	// The FT4 decoder's frame is 90 000 samples; an FT8-length buffer is a
	// producer bug the wrapper reports as a rejected slot (ok=false), never a
	// silent zero-decode "success".
	d := newSlotDecoder(ProfileFT4, true, logging.Noop())
	msgs, ok := d.decode(make([]int16, ProfileFT8.SlotSamples))
	require.False(t, ok)
	require.Nil(t, msgs)
}

func TestSlotDecoder_FT4_SkipAndResetKeepDecoding(t *testing.T) {
	d := newSlotDecoder(ProfileFT4, true, logging.Noop())
	slot := mixSlotOn(t, ProfileFT4, map[string]float64{"CQ K1ABC FN42": 1500})

	d.skip() // harmless on a fresh decoder
	msgs, ok := d.decode(slot)
	require.True(t, ok)
	require.Contains(t, textsOf(msgs), "CQ K1ABC FN42", "decodes after a skipped slot")

	d.skip()
	d.skip()
	msgs, ok = d.decode(slot)
	require.True(t, ok)
	require.Contains(t, textsOf(msgs), "CQ K1ABC FN42", "decodes after consecutive skips")

	d.reset() // a QSY: fresh FT4 state, same profile
	msgs, ok = d.decode(slot)
	require.True(t, ok)
	require.Contains(t, textsOf(msgs), "CQ K1ABC FN42", "decodes after a reset")
}

// The live loop under the FT4 profile: one FT4 slot in, one ft8-decode event
// out carrying the row on a millisecond slot reference, and an occupancy
// report sized to FT4's signal width.
func TestDecodeLoop_FT4Profile_PublishesRowsOnItsLattice(t *testing.T) {
	s, sink, events, _ := newSplitHarness(t)
	s.setProfileForTest(ProfileFT4)

	samples := mixSlotOn(t, ProfileFT4, map[string]float64{"CQ A61DI LL64": 2200})
	start := ProfileFT4.slotStart(time.Now().UTC().Add(-time.Minute)).Add(7500 * time.Millisecond)
	ch := make(chan Slot, 1)
	ch <- Slot{StartUTC: start, Samples: samples, DialTracked: true, DialMHz: 14.080}
	close(ch)
	s.decodeLoop(ch, nil)

	decodes, occupancy := drainDecodeEvents(events)
	require.Len(t, decodes, 1)
	rep := decodes[0]
	require.Len(t, rep.Decodes, 1, "rows: %+v", rep.Decodes)
	require.Equal(t, "CQ A61DI LL64", rep.Decodes[0].Text)
	require.InDelta(t, 2200, rep.Decodes[0].FreqHz, 2)
	require.Equal(t, ProfileFT4.SlotRefFromTime(start), rep.Slot)
	require.True(t, strings.HasSuffix(rep.Slot.StartUTC, ".500Z") || strings.HasSuffix(rep.Slot.StartUTC, ".000Z"),
		"FT4 references carry milliseconds: %q", rep.Slot.StartUTC)
	require.Equal(t, 14.080, rep.DialMHz)
	require.Equal(t, "FT4", rep.Mode, "the report names the profile it was decoded on")
	require.Equal(t, 1, occupancy)
	if occ := s.LatestOccupancy(); occ != nil {
		require.Equal(t, ProfileFT4.SignalWidthHz, occ.SignalWidthHz)
	}
	require.Len(t, sink.all(), 1, "the PSK-shaped sink sees the FT4 row too")
	require.Equal(t, "FT4", sink.all()[0].Mode, "and spots it as FT4")
}

func BenchmarkDecodeSlotFT4(b *testing.B) {
	// A busy synthetic FT4 slot: six standard messages spread across the
	// passband. Pre-G2 synthetic sizing evidence only (a mean ns/op on synthetic
	// input): AC7 requires the live-slot p95, which gate G2 measures on air.
	samples := mixSlotOn(b, ProfileFT4, map[string]float64{
		"CQ K1ABC FN42":     600,
		"CQ A61DI LL64":     1000,
		"K1ABC G0XYZ IO91":  1400,
		"G0XYZ K1ABC -10":   1800,
		"7Q5MLV DL9UW JO41": 2200,
		"DL9UW 7Q5MLV RR73": 2600,
	})
	log := logging.Noop()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := newSlotDecoder(ProfileFT4, true, log)
		if msgs, ok := d.decode(samples); !ok || len(msgs) < 6 {
			b.Fatalf("decoded %d messages, ok=%v", len(msgs), ok)
		}
	}
}

// FT4 keeps callsign hash history across the slots of one receiver stream, as
// FT8 does (go-ft8 v0.10.0: "Per-instance FT4 history for 10-, 12-, and 22-bit
// callsign hashes"). W-0030 depends on it: a DXpedition's combined message
// names its sender only by a hash, and is read only when that hash is resolved.
// Until v0.10.0 the FT4 decoder kept nothing, so on FT4 it never was (found at
// the station 2026-11-24: two contacts the other station had logged were lost).
//
// The fixture is FT8's (decodesplit_test.go): a call heard in full, then a
// message that names it by its 12-bit hash. The control comes first: a fresh
// decoder cannot resolve it, so a failure there means the fixture is wrong.
func TestSlotDecoder_FT4_HashResolvesAcrossSlots(t *testing.T) {
	teach := mixSlotOn(t, ProfileFT4, map[string]float64{hashTeachText: 1500})
	ref := mixSlotOn(t, ProfileFT4, map[string]float64{hashRefText: 1500})

	control := newSlotDecoder(ProfileFT4, true, logging.Noop())
	got, ok := control.decode(ref)
	require.True(t, ok)
	require.Contains(t, textsOf(got), hashUnresolved, "fixture control: a fresh FT4 decoder cannot resolve the hash")

	d := newSlotDecoder(ProfileFT4, true, logging.Noop())
	got, ok = d.decode(teach)
	require.True(t, ok)
	require.Contains(t, textsOf(got), hashTeachText)
	got, ok = d.decode(ref)
	require.True(t, ok)
	require.Contains(t, textsOf(got), hashResolved, "the call heard a slot earlier must resolve its hash on FT4")
}

// A slot SM refuses to decode (its own transmission) keeps the history: the
// run path transmits every other slot, which is exactly where the station being
// worked was heard in full one slot and names itself by hash two slots later.
func TestSlotDecoder_FT4_SkipPreservesHashState(t *testing.T) {
	d := newSlotDecoder(ProfileFT4, true, logging.Noop())
	got, _ := d.decode(mixSlotOn(t, ProfileFT4, map[string]float64{hashTeachText: 1500}))
	require.Contains(t, textsOf(got), hashTeachText)

	d.skip() // our TX slot between them
	got, _ = d.decode(mixSlotOn(t, ProfileFT4, map[string]float64{hashRefText: 1500}))
	require.Contains(t, textsOf(got), hashResolved, "a skipped slot must not lose the history")
}

// A dial move clears it. The hash table is band-blind, so a call heard on the
// old band must not resolve a hash on the new one: a collision would print a
// valid-looking wrong call (review cd1757a7cda2 P1, now true of FT4 as well).
func TestSlotDecoder_FT4_ResetClearsHashState(t *testing.T) {
	d := newSlotDecoder(ProfileFT4, true, logging.Noop())
	got, _ := d.decode(mixSlotOn(t, ProfileFT4, map[string]float64{hashTeachText: 1500}))
	require.Contains(t, textsOf(got), hashTeachText)

	d.reset() // a QSY
	got, _ = d.decode(mixSlotOn(t, ProfileFT4, map[string]float64{hashRefText: 1500}))
	require.Contains(t, textsOf(got), hashUnresolved, "a call heard before the dial moved must not resolve a hash after it")
	require.NotContains(t, textsOf(got), hashResolved)
}
