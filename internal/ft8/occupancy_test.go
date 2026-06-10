package ft8

import (
	"math"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
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

// signalSlot synthesises a slot containing one FT8-width (~signalWidthHz)
// energy occupant whose base tone is at `base` — eight tones at 6.25 Hz spacing,
// the footprint of a real FT8 signal — so it reads as a signal to the detector
// rather than a single-bin spike the min-width gate would drop. Use this (not a
// pure tone) wherever a test needs a genuine energy occupant.
func signalSlot(amp, noiseAmp float64, seed int64, base float64) []int16 {
	freqs := make([]float64, 8)
	for i := range freqs {
		freqs[i] = base + float64(i)*6.25
	}
	return toneSlot(amp, noiseAmp, seed, freqs...)
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
	rep := Occupancy(SlotRef{}, signalSlot(8000, 200, 1, tone), nil, cfg)

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

// TestOccupancy_NarrowEnergyGated confirms the min-width gate: a pure tone is
// only ~1-3 bins wide — narrower than a real ~50 Hz FT8 signal — so it must NOT
// produce an energy band. (Deliberate trade-off: a very narrow strong carrier
// is also dropped from energy detection; decode-tier still catches anything
// that decodes, and live single-bin noise spikes no longer leak in.)
func TestOccupancy_NarrowEnergyGated(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	rep := Occupancy(SlotRef{}, toneSlot(8000, 200, 3, 1500), nil, cfg)
	for _, b := range rep.Occupied {
		if b.Source == sourceEnergy {
			t.Fatalf("narrow pure tone should be gated out of energy detection, got %+v", b)
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
	rep := Occupancy(SlotRef{}, signalSlot(8000, 200, 2, tone), decodes, cfg)

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

func TestSuggestOffsets_GuardMarginKeepsClearance(t *testing.T) {
	cfg := DefaultOccupancyConfig() // guard on at defaultGuardMarginHz
	occupied := []Band{
		{LowHz: 600, HighHz: 700},
		{LowHz: 1400, HighHz: 1600},
		{LowHz: 2400, HighHz: 2450},
	}
	for _, off := range suggestOffsets(occupied, cfg) {
		sigLo, sigHi := off, off+signalWidthHz
		for _, b := range occupied {
			if sigLo < b.HighHz && b.LowHz < sigHi {
				t.Fatalf("offset %d overlaps %+v", off, b)
			}
			if sigHi <= b.LowHz && b.LowHz-sigHi < defaultGuardMarginHz {
				t.Errorf("offset %d clears %+v below by %d Hz (< guard %d)", off, b, b.LowHz-sigHi, defaultGuardMarginHz)
			}
			if b.HighHz <= sigLo && sigLo-b.HighHz < defaultGuardMarginHz {
				t.Errorf("offset %d clears %+v above by %d Hz (< guard %d)", off, b, sigLo-b.HighHz, defaultGuardMarginHz)
			}
		}
	}
}

func TestSuggestOffsets_GuardOnRejectsTightGap(t *testing.T) {
	cfg := DefaultOccupancyConfig() // guard on
	// One exactly-signalWidthHz (50 Hz) gap [1000,1050]: fits flush but not with a guard.
	occupied := []Band{{LowHz: 200, HighHz: 1000}, {LowHz: 1050, HighHz: 3000}}
	if got := suggestOffsets(occupied, cfg); len(got) != 0 {
		t.Fatalf("guard on should reject a flush-only 50 Hz gap, got %v", got)
	}
}

func TestSuggestOffsets_GuardOffAllowsFlush(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	zero := 0
	cfg.GuardMarginHz = &zero // guard off
	occupied := []Band{{LowHz: 200, HighHz: 1000}, {LowHz: 1050, HighHz: 3000}}
	found := false
	for _, o := range suggestOffsets(occupied, cfg) {
		if o == 1000 { // flush against the band ending at 1000
			found = true
		}
	}
	if !found {
		t.Fatal("guard off should offer the flush offset 1000")
	}
}

func TestOffsetClear(t *testing.T) {
	cfg := DefaultOccupancyConfig() // guard on at defaultGuardMarginHz (10)
	occupied := []Band{{LowHz: 600, HighHz: 700}, {LowHz: 1400, HighHz: 1600}}

	// 1000 sits in the wide 700–1400 gap with room for signal + guard each side.
	if !offsetClear(occupied, cfg, 1000) {
		t.Error("1000 should be clear in the 700–1400 gap")
	}
	// An offset whose signal lands inside an occupied band is not clear.
	if offsetClear(occupied, cfg, 620) {
		t.Error("620 overlaps the 600–700 band; should not be clear")
	}
	// Flush against a neighbour fails the guard margin: 700 + 50 = 750, but the
	// gap starts at 700 so the low guard (10 Hz) isn't satisfied.
	if offsetClear(occupied, cfg, 700) {
		t.Error("700 sits flush against the band ending at 700; guard should reject it")
	}
}

func TestStickySuggested(t *testing.T) {
	cfg := DefaultOccupancyConfig()
	occupied := []Band{{LowHz: 600, HighHz: 700}, {LowHz: 1400, HighHz: 1600}}

	t.Run("no previous pick returns the fresh ranking", func(t *testing.T) {
		fresh := []int{2000, 1000, 800}
		got := stickySuggested(fresh, occupied, cfg, 0)
		if !equalInts(got, fresh) {
			t.Fatalf("prev=0 should be untouched: got %v", got)
		}
	})

	t.Run("a still-clear previous pick is floated to the front", func(t *testing.T) {
		fresh := []int{2000, 1000, 800} // 800 ranks last this slot
		got := stickySuggested(fresh, occupied, cfg, 800)
		if len(got) == 0 || got[0] != 800 {
			t.Fatalf("clear prev 800 should lead, got %v", got)
		}
		// No duplication and the others still follow.
		if !equalInts(got, []int{800, 2000, 1000}) {
			t.Fatalf("expected [800 2000 1000], got %v", got)
		}
	})

	t.Run("a previous pick that is now occupied is dropped", func(t *testing.T) {
		fresh := []int{2000, 1000}
		got := stickySuggested(fresh, occupied, cfg, 620) // 620 now overlaps 600–700
		if !equalInts(got, fresh) {
			t.Fatalf("occupied prev should not be floated: got %v", got)
		}
	})

	t.Run("a clear prev absent from the fresh list is still prepended", func(t *testing.T) {
		fresh := []int{2000, 1000} // 900 isn't a generated candidate this slot
		got := stickySuggested(fresh, occupied, cfg, 900)
		if len(got) == 0 || got[0] != 900 {
			t.Fatalf("clear prev 900 should lead even when absent, got %v", got)
		}
	})

	t.Run("respects the maxSuggested cap after prepending", func(t *testing.T) {
		fresh := make([]int, maxSuggested) // a full list, none equal to prev
		for i := range fresh {
			fresh[i] = 1000 + i // all clear in the 700–1400 gap region is irrelevant; cap check only
		}
		got := stickySuggested(fresh, occupied, cfg, 1000) // 1000 is in fresh, moved to front
		if len(got) > maxSuggested {
			t.Fatalf("result exceeds cap: len=%d", len(got))
		}
	})
}

// equalInts reports whether two int slices are element-wise equal.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestResolveOccupancyConfig_GuardOverride(t *testing.T) {
	// Explicit 0 must survive resolution (not be treated as "unset → default").
	zero := 0
	got := resolveOccupancyConfig(&types.Ft8OccupancyConfig{GuardMarginHz: &zero})
	if got.GuardMarginHz == nil || *got.GuardMarginHz != 0 {
		t.Fatalf("explicit guard 0 lost in resolve: %v", got.GuardMarginHz)
	}
	// nil override keeps the default.
	def := resolveOccupancyConfig(nil)
	if def.GuardMarginHz == nil || *def.GuardMarginHz != defaultGuardMarginHz {
		t.Fatalf("nil override should keep default guard %d, got %v", defaultGuardMarginHz, def.GuardMarginHz)
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

func TestResolveOccupancyConfig(t *testing.T) {
	def := DefaultOccupancyConfig()

	// nil → defaults unchanged. (Field-wise, not struct ==, since the pointer
	// field GuardMarginHz holds distinct addresses across DefaultOccupancyConfig
	// calls; TestResolveOccupancyConfig_GuardOverride covers the guard pointer.)
	if n := resolveOccupancyConfig(nil); n.PassbandLowHz != def.PassbandLowHz ||
		n.PassbandHighHz != def.PassbandHighHz || n.ThresholdFactor != def.ThresholdFactor ||
		n.WeightMargin != def.WeightMargin || n.WeightEdge != def.WeightEdge ||
		n.WeightCentered != def.WeightCentered {
		t.Fatalf("nil override changed a default field: %+v vs %+v", n, def)
	}

	// Sparse override: only set fields win; zeros fall back to default.
	got := resolveOccupancyConfig(&types.Ft8OccupancyConfig{
		PassbandLowHz: 300,
		WeightEdge:    0.9,
	})
	if got.PassbandLowHz != 300 {
		t.Errorf("PassbandLowHz = %d, want override 300", got.PassbandLowHz)
	}
	if got.WeightEdge != 0.9 {
		t.Errorf("WeightEdge = %v, want override 0.9", got.WeightEdge)
	}
	if got.PassbandHighHz != def.PassbandHighHz {
		t.Errorf("PassbandHighHz = %d, want default %d", got.PassbandHighHz, def.PassbandHighHz)
	}
	if got.ThresholdFactor != def.ThresholdFactor {
		t.Errorf("ThresholdFactor = %v, want default %v", got.ThresholdFactor, def.ThresholdFactor)
	}
	if got.WeightMargin != def.WeightMargin {
		t.Errorf("WeightMargin = %v, want default %v", got.WeightMargin, def.WeightMargin)
	}
}

// TestDecodeLoop_PublishesOccupancy proves the decode→occupancy glue: a slot
// fed through decodeLoop populates LatestOccupancy. A short (wrong-length) slot
// makes DecodeSlot reject fast (no heavy decode), so this stays a quick glue
// test — the detector math itself is covered above and against a real slot.
func TestDecodeLoop_PublishesOccupancy(t *testing.T) {
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), newFakeSource())

	if s.LatestOccupancy() != nil {
		t.Fatal("expected nil occupancy before any slot")
	}

	ch := make(chan Slot, 1)
	ch <- Slot{StartUTC: time.Date(2026, 6, 7, 14, 30, 0, 0, time.UTC), Samples: make([]int16, 1000)}
	close(ch)
	s.decodeLoop(ch)

	rep := s.LatestOccupancy()
	if rep == nil {
		t.Fatal("decodeLoop did not publish an occupancy report")
	}
	if rep.Slot.Period != "even" {
		t.Errorf("slot period = %q, want even", rep.Slot.Period)
	}
	if rep.Passband.LowHz != s.occCfg.PassbandLowHz {
		t.Errorf("report passband low = %d, want resolved %d", rep.Passband.LowHz, s.occCfg.PassbandLowHz)
	}
}
