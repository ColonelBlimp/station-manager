package ft8

import (
	"math"
	"strings"
	"testing"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// TestModulate_Shape checks the cheap structural invariants without decoding:
// length is (nsym+2)*nsps, samples stay in [-1,1], and the ramped edges start
// near zero. No -short gate — pure DSP, no decoder.
func TestModulate_Shape(t *testing.T) {
	tones := make([]uint8, 79)
	for i := range tones {
		tones[i] = uint8(i % 8)
	}
	wave := Modulate(tones, 1500)

	want := (79 + 2) * txSamplesPerSymbol
	if len(wave) != want {
		t.Fatalf("len(wave) = %d, want %d", len(wave), want)
	}
	for i, v := range wave {
		if v < -1.0001 || v > 1.0001 {
			t.Fatalf("sample %d out of range: %v", i, v)
		}
	}
	// The leading ramp forces the first sample toward zero.
	if math.Abs(float64(wave[0])) > 0.05 {
		t.Errorf("first sample = %v, want ~0 (ramped edge)", wave[0])
	}
}

func TestModulate_EmptyTones(t *testing.T) {
	if got := Modulate(nil, 1500); got != nil {
		t.Fatalf("empty tones = %v, want nil", got)
	}
}

func TestEncodeToSlot_FullSlotLength(t *testing.T) {
	slot, err := EncodeToSlot("CQ K1ABC FN42", 1500, 0.5)
	if err != nil {
		t.Fatalf("EncodeToSlot: %v", err)
	}
	if len(slot) != SlotSamples {
		t.Fatalf("slot length = %d, want %d", len(slot), SlotSamples)
	}
}

func TestEncodeToSlot_RejectsUnsupported(t *testing.T) {
	// Free text is not an encodable standard message.
	if _, err := EncodeToSlot("HELLO BRAVE NEW WORLD", 1500, 0.5); err == nil {
		t.Fatal("expected an error for an unsupported message, got nil")
	}
}

// TestModulate_RoundTrip is the step-(b) de-risking proof: encode a standard
// message, modulate it to audio, and decode that audio back with the shipped
// decoder — asserting the text survives and lands at the chosen offset. Zero
// RF. Heavy (full decode), so gated under -short.
func TestModulate_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}

	// A spread of standard messages across the CQ→73 ladder, at different
	// offsets, including band edges of the picker's passband.
	cases := []struct {
		text   string
		offset float64
	}{
		{"CQ K1ABC FN42", 1500},
		{"K1ABC W9XYZ EN37", 800},
		{"W9XYZ K1ABC -15", 2400},
		{"K1ABC W9XYZ R-15", 1200},
		{"W9XYZ K1ABC RR73", 300},
		{"K1ABC W9XYZ 73", 2900},
	}

	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			// Canonical text the decoder will report (encoder may normalise).
			enc, err := goft8.EncodeStandardMessage(c.text)
			if err != nil {
				t.Fatalf("EncodeStandardMessage(%q): %v", c.text, err)
			}

			slot, err := EncodeToSlot(c.text, c.offset, 0.5)
			if err != nil {
				t.Fatalf("EncodeToSlot(%q): %v", c.text, err)
			}

			msgs := DecodeSlot(slot, true, logging.Noop())
			if len(msgs) == 0 {
				t.Fatalf("no decode for %q at %.0f Hz", c.text, c.offset)
			}

			var hit *goft8.DecodedMessage
			for i := range msgs {
				if strings.EqualFold(strings.TrimSpace(msgs[i].Text), enc.Text) {
					hit = &msgs[i]
					break
				}
			}
			if hit == nil {
				got := make([]string, len(msgs))
				for i, m := range msgs {
					got[i] = m.Text
				}
				t.Fatalf("decoded %v, none matched %q", got, enc.Text)
			}
			if math.Abs(hit.FreqHz-c.offset) > 2.0 {
				t.Errorf("decoded freq %.1f Hz, want ~%.0f Hz", hit.FreqHz, c.offset)
			}
		})
	}
}

// TestModulate_TruncatedDecodes is the ADR 0032 correctness proof: a synchronised
// transmission whose head was dropped (a late answer-a-CQ start) still decodes,
// because the receiver re-syncs on the middle/end Costas arrays. We build a full
// slot, then zero the leading nominal + late seconds — exactly what a receiver
// would hear if we started late but on the synchronised timebase — and assert the
// message still comes back. Heavy (full decode), gated under -short.
func TestModulate_TruncatedDecodes(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}

	const (
		text    = "K1ABC W9XYZ R-12"
		offset  = 1500.0
		lateSec = 2.0 // start 2 s past nominal → first Costas array lost
	)
	enc, err := goft8.EncodeStandardMessage(text)
	if err != nil {
		t.Fatalf("EncodeStandardMessage: %v", err)
	}
	slot, err := EncodeToSlot(text, offset, 0.5) // waveform at the nominal +0.5 s
	if err != nil {
		t.Fatalf("EncodeToSlot: %v", err)
	}
	// Silence everything up to nominal (0.5 s) + lateSec — the head a late starter
	// would never have transmitted.
	zeroUntil := int((0.5 + lateSec) * float64(goft8.SampleRate))
	for i := 0; i < zeroUntil && i < len(slot); i++ {
		slot[i] = 0
	}

	msgs := DecodeSlot(slot, true, logging.Noop())
	found := false
	for _, m := range msgs {
		if strings.EqualFold(strings.TrimSpace(m.Text), enc.Text) {
			found = true
			break
		}
	}
	if !found {
		got := make([]string, len(msgs))
		for i, m := range msgs {
			got[i] = m.Text
		}
		t.Fatalf("truncated (%.1fs late) signal did not decode; got %v, want %q", lateSec, got, enc.Text)
	}
}

// TestModulate_RoundTripOccupancy closes the loop with step (a): the offset we
// transmit on shows up as occupied in the occupancy detector reading the same
// audio — i.e. a signal we generate would correctly mark its own slot busy.
func TestModulate_RoundTripOccupancy(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	const offset = 1500
	slot, err := EncodeToSlot("CQ K1ABC FN42", offset, 0.5)
	if err != nil {
		t.Fatalf("EncodeToSlot: %v", err)
	}
	decodes := DecodeSlot(slot, true, logging.Noop())
	rep := Occupancy(SlotRef{}, slot, decodes, DefaultOccupancyConfig())

	covered := false
	for _, b := range rep.Occupied {
		if offset >= b.LowHz && offset <= b.HighHz {
			covered = true
			break
		}
	}
	if !covered {
		t.Fatalf("modulated signal at %d Hz not marked occupied: %+v", offset, rep.Occupied)
	}
	// And no suggested clear offset should collide with our own transmission.
	for _, off := range rep.Suggested {
		if off < offset+signalWidthHz && offset < off+signalWidthHz {
			t.Errorf("suggested offset %d overlaps the transmitted signal at %d", off, offset)
		}
	}
}
