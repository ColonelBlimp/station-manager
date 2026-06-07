package ft8

import (
	"math"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// toneSlot synthesises a full FT8 slot of 12 kHz int16 audio: a sum of sine
// tones at the given base frequencies (amplitude amp each) plus low-level
// deterministic white noise so the median noise floor is realistic (a
// zero-noise floor would make the threshold meaningless). seed fixes the noise
// for reproducibility.
func toneSlot(amp float64, noiseAmp float64, seed int64, freqs ...float64) []int16 {
	out := make([]int16, SlotSamples)
	rng := rand.New(rand.NewSource(seed))
	sr := float64(goft8.SampleRate)
	for i := range out {
		v := 0.0
		for _, f := range freqs {
			v += amp * math.Sin(2*math.Pi*f*float64(i)/sr)
		}
		v += (rng.Float64()*2 - 1) * noiseAmp
		out[i] = int16(v)
	}
	return out
}

// bandContains reports whether hz lies within [b.LowHz, b.HighHz].
func bandContains(b Band, hz int) bool { return hz >= b.LowHz && hz <= b.HighHz }

// overlaps reports whether [aLo,aHi] and [bLo,bHi] share any range.
func overlaps(aLo, aHi, bLo, bHi int) bool { return aLo < bHi && bLo < aHi }

func TestOccupancy_SilentSlot_NoBands(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	rep := Occupancy(SlotRef{}, make([]int16, SlotSamples), nil, cfg)

	if len(rep.Occupied) != 0 {
		t.Fatalf("silent slot should have no occupied bands, got %+v", rep.Occupied)
	}
	if len(rep.Suggested) == 0 {
		t.Fatal("silent slot should still suggest clear offsets")
	}
	if rep.SignalWidthHz != signalWidthHz {
		t.Fatalf("SignalWidthHz = %d, want %d", rep.SignalWidthHz, signalWidthHz)
	}
	if rep.Passband.LowHz != cfg.PassbandLowHz || rep.Passband.HighHz != cfg.PassbandHighHz {
		t.Fatalf("passband = %+v, want [%d,%d]", rep.Passband, cfg.PassbandLowHz, cfg.PassbandHighHz)
	}
	for _, off := range rep.Suggested {
		if off < cfg.PassbandLowHz || off+signalWidthHz > cfg.PassbandHighHz {
			t.Fatalf("suggested offset %d does not fit in passband", off)
		}
	}
}

func TestOccupancy_SingleTone_MarksBandAndAvoidsIt(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	const tone = 1500
	rep := Occupancy(SlotRef{}, toneSlot(8000, 200, 1, tone), nil, cfg)

	var hit *Band
	for i := range rep.Occupied {
		if bandContains(rep.Occupied[i], tone) {
			hit = &rep.Occupied[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected an occupied band covering %d Hz, got %+v", tone, rep.Occupied)
	}
	if hit.Source != sourceEnergy {
		t.Fatalf("tone band source = %q, want %q", hit.Source, sourceEnergy)
	}
	if hit.Level <= 0 {
		t.Fatalf("tone band level = %v, want > 0", hit.Level)
	}

	// No suggested offset may place a 50 Hz signal over any occupied band.
	for _, off := range rep.Suggested {
		for _, b := range rep.Occupied {
			if overlaps(off, off+signalWidthHz, b.LowHz, b.HighHz) {
				t.Fatalf("suggested offset %d collides with occupied band %+v", off, b)
			}
		}
	}
}

func TestOccupancy_DecodeOnly_MarksUpwardSpan(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	// Silent audio, one decode at 1000.4 Hz → base 1000, occupies [1000,1050].
	decodes := []goft8.DecodedMessage{{Text: "CQ K1ABC FN42", FreqHz: 1000.4}}
	rep := Occupancy(SlotRef{}, make([]int16, SlotSamples), decodes, cfg)

	if len(rep.Occupied) != 1 {
		t.Fatalf("want exactly one occupied band, got %+v", rep.Occupied)
	}
	b := rep.Occupied[0]
	if b.Source != sourceDecode {
		t.Fatalf("source = %q, want %q", b.Source, sourceDecode)
	}
	if b.LowHz != 1000 || b.HighHz != 1000+signalWidthHz {
		t.Fatalf("decode band = [%d,%d], want [1000,%d] (span extends upward)", b.LowHz, b.HighHz, 1000+signalWidthHz)
	}
}

func TestOccupancy_EnergyAndDecodeOverlap_SourceBoth(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	const tone = 1500
	// Decode base 1480 → [1480,1530], overlapping the tone's energy band.
	decodes := []goft8.DecodedMessage{{FreqHz: 1480}}
	rep := Occupancy(SlotRef{}, toneSlot(8000, 200, 2, tone), decodes, cfg)

	var hit *Band
	for i := range rep.Occupied {
		if bandContains(rep.Occupied[i], tone) {
			hit = &rep.Occupied[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected a band covering %d Hz, got %+v", tone, rep.Occupied)
	}
	if hit.Source != sourceBoth {
		t.Fatalf("merged band source = %q, want %q", hit.Source, sourceBoth)
	}
	if hit.LowHz > 1480 {
		t.Fatalf("merged band low = %d, should extend down to the decode base 1480", hit.LowHz)
	}
}

func TestOccupancy_DecodeOutsidePassband_Dropped(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	decodes := []goft8.DecodedMessage{
		{FreqHz: 50},   // wholly below passband
		{FreqHz: 5000}, // wholly above passband
	}
	rep := Occupancy(SlotRef{}, make([]int16, SlotSamples), decodes, cfg)
	if len(rep.Occupied) != 0 {
		t.Fatalf("out-of-passband decodes should be dropped, got %+v", rep.Occupied)
	}
}

func TestOccupancy_DecodeClampedToPassband(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	// Base 180 → [180,230]; clamps to [200,230].
	decodes := []goft8.DecodedMessage{{FreqHz: 180}}
	rep := Occupancy(SlotRef{}, make([]int16, SlotSamples), decodes, cfg)
	if len(rep.Occupied) != 1 {
		t.Fatalf("want one band, got %+v", rep.Occupied)
	}
	if rep.Occupied[0].LowHz != cfg.PassbandLowHz {
		t.Fatalf("low = %d, want clamped to %d", rep.Occupied[0].LowHz, cfg.PassbandLowHz)
	}
}

func TestOccupancy_SuggestedCapped(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	rep := Occupancy(SlotRef{}, make([]int16, SlotSamples), nil, cfg)
	if len(rep.Suggested) > maxSuggested {
		t.Fatalf("suggested count %d exceeds cap %d", len(rep.Suggested), maxSuggested)
	}
}

func TestOccupancy_Deterministic(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	s := toneSlot(8000, 200, 7, 800, 1900)
	a := Occupancy(SlotRef{}, s, nil, cfg)
	b := Occupancy(SlotRef{}, s, nil, cfg)
	if len(a.Suggested) != len(b.Suggested) {
		t.Fatal("suggested length not deterministic")
	}
	for i := range a.Suggested {
		if a.Suggested[i] != b.Suggested[i] {
			t.Fatalf("suggested[%d] not deterministic: %d vs %d", i, a.Suggested[i], b.Suggested[i])
		}
	}
}

func TestMergeBands(t *testing.T) {
	in := []Band{
		{LowHz: 1000, HighHz: 1050, Source: sourceDecode},
		{LowHz: 1040, HighHz: 1080, Source: sourceEnergy, Level: 0.7}, // overlaps prev → both
		{LowHz: 1080, HighHz: 1100, Source: sourceEnergy, Level: 0.3}, // touches prev → merge
		{LowHz: 2000, HighHz: 2050, Source: sourceEnergy, Level: 0.9}, // distinct
	}
	out := mergeBands(in)
	if len(out) != 2 {
		t.Fatalf("want 2 merged bands, got %d: %+v", len(out), out)
	}
	if out[0].LowHz != 1000 || out[0].HighHz != 1100 {
		t.Fatalf("first band = [%d,%d], want [1000,1100]", out[0].LowHz, out[0].HighHz)
	}
	if out[0].Source != sourceBoth {
		t.Fatalf("first band source = %q, want %q", out[0].Source, sourceBoth)
	}
	if out[0].Level != 0.7 {
		t.Fatalf("first band level = %v, want the max 0.7", out[0].Level)
	}
	if out[1].LowHz != 2000 || out[1].Source != sourceEnergy {
		t.Fatalf("second band = %+v, want distinct energy [2000,2050]", out[1])
	}
}

func TestMergeBands_Empty(t *testing.T) {
	if got := mergeBands(nil); got != nil {
		t.Fatalf("merge of nil = %+v, want nil", got)
	}
}

func TestSuggestOffsets_NeverOverlapOccupied(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	occupied := []Band{
		{LowHz: 600, HighHz: 700},
		{LowHz: 1400, HighHz: 1600},
		{LowHz: 2400, HighHz: 2450},
	}
	for _, off := range suggestOffsets(occupied, cfg) {
		if off < cfg.PassbandLowHz || off+signalWidthHz > cfg.PassbandHighHz {
			t.Fatalf("offset %d outside passband", off)
		}
		for _, b := range occupied {
			if overlaps(off, off+signalWidthHz, b.LowHz, b.HighHz) {
				t.Fatalf("offset %d collides with %+v", off, b)
			}
		}
	}
}

func TestSuggestOffsets_FullyOccupied_NoSuggestions(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	occupied := []Band{{LowHz: cfg.PassbandLowHz, HighHz: cfg.PassbandHighHz}}
	if got := suggestOffsets(occupied, cfg); len(got) != 0 {
		t.Fatalf("fully occupied passband should yield no suggestions, got %v", got)
	}
}

// TestOccupancy_RealSlot is the end-to-end check: decode a real corpus slot,
// run occupancy on the same samples + decodes, and confirm the pipeline holds
// together on live data — every decoded signal lands inside an occupied band,
// the energy tier contributes (some band is energy/both, not decode-only), and
// no suggested offset collides with anything busy.
func TestOccupancy_RealSlot(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	cfg := DefaultOccupancyConfig()
	samples, err := readSlotWAV(filepath.Join("testdata", "20m_slot1.wav"))
	if err != nil {
		t.Fatalf("readSlotWAV: %v", err)
	}
	decodes := DecodeSlot(samples, true, logging.Noop())
	if len(decodes) == 0 {
		t.Fatal("expected decodes from the corpus slot")
	}

	rep := Occupancy(SlotRefFromTime(time.Unix(0, 0)), samples, decodes, cfg)
	if len(rep.Occupied) == 0 {
		t.Fatal("a busy 20m slot should report occupied bands")
	}

	// Every in-passband decode base frequency must fall in some occupied band.
	for _, m := range decodes {
		base := int(math.Round(m.FreqHz))
		if base < cfg.PassbandLowHz || base >= cfg.PassbandHighHz {
			continue
		}
		covered := false
		for _, b := range rep.Occupied {
			if bandContains(b, base) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("decode %q at %d Hz not covered by any occupied band", m.Text, base)
		}
	}

	// The energy FFT must contribute, not just the decode markers.
	sawEnergy := false
	for _, b := range rep.Occupied {
		if b.Source == sourceEnergy || b.Source == sourceBoth {
			sawEnergy = true
			break
		}
	}
	if !sawEnergy {
		t.Error("expected at least one energy/both band from the FFT tier")
	}

	for _, off := range rep.Suggested {
		for _, b := range rep.Occupied {
			if overlaps(off, off+signalWidthHz, b.LowHz, b.HighHz) {
				t.Errorf("suggested offset %d collides with occupied band %+v", off, b)
			}
		}
	}
}

func TestSlotRefFromTime_EvenOdd(t *testing.T) {
	cases := []struct {
		sec  int
		want string
	}{
		{0, "even"}, {15, "odd"}, {30, "even"}, {45, "odd"},
	}
	for _, c := range cases {
		ts := time.Date(2026, 6, 7, 14, 30, c.sec, 0, time.UTC)
		got := SlotRefFromTime(ts)
		if got.Period != c.want {
			t.Fatalf(":%02d → period %q, want %q", c.sec, got.Period, c.want)
		}
		if got.StartUTC != ts.Format(time.RFC3339) {
			t.Fatalf("StartUTC = %q, want %q", got.StartUTC, ts.Format(time.RFC3339))
		}
	}
}
