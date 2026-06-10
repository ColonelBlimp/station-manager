package ft8

import (
	"math"
	"sort"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Per-slot occupancy detection for FT8 TX-offset selection (ADR 0029, build
// step (a)). Occupancy is RX-only decision data: given one decoded slot it
// reports which audio offsets are busy and ranks the clear ones, so the
// operator can drop a transmission into a free slot without rendering a
// scrolling waterfall. The whole computation is pure and CGO-free — it runs
// on captured audio (so it is only *useful* in a CGO build, where capture is
// live) but the math itself compiles and tests without CGO, so the detector
// can be exercised against canned buffers on the static build.
//
// Two tiers feed one `Occupied` list:
//   - energy: a Hann-windowed Welch-averaged FFT of the slot catches all
//     signal energy, including signals too weak or garbled to decode;
//   - decode: each decode's reported frequency marks the band it occupies,
//     covering signals the crude energy threshold misses.
//
// The daemon owns all thresholding, merging, and ranking; the SPA receives
// intent (a handful of busy bands + ranked clear offsets), never raw spectrum.

const (
	// signalWidthHz is the audio bandwidth one FT8 signal occupies, measured
	// upward from its base (sync) tone: 8 FSK tones at 6.25 Hz spacing span
	// ~50 Hz. A clear TX slot needs at least this much contiguous room, and a
	// decode at base frequency f occupies [f, f+signalWidthHz].
	//
	// Hardcoded here for now. The authoritative geometry (tone spacing × tone
	// count) lives inside go-ft8; when the TX modulator lands (ADR 0029 step b)
	// that geometry should be exported as public consts so RX occupancy and TX
	// modulation derive the same number from one source instead of both
	// hardcoding 50.
	signalWidthHz = 50

	// occupancyFFTSize is the per-window FFT length for the slot energy
	// spectrum. 3840 = go-ft8's spectrogram NFFT1, giving 12000/3840 = 3.125 Hz
	// bins — half an FT8 tone, fine enough to resolve adjacent signals.
	occupancyFFTSize = 3840

	// maxSuggested caps the ranked clear-offset list. The picker only needs a
	// few options; the best-scoring handful is plenty and keeps the report
	// small. The SPA can still compute its own gaps from Occupied if it wants
	// more.
	maxSuggested = 8

	// minEnergyBandHz gates ENERGY-only detection: an undecoded over-threshold
	// run narrower than this is treated as a noise/leakage spike, not an
	// occupant, and dropped. A real FT8 signal is signalWidthHz (~50 Hz) wide,
	// so a quarter of that sits comfortably below any real signal yet above the
	// single-bin spikes seen on live audio (e.g. a stray 3 Hz energy 0.08 mark).
	// Decode-derived bands are never gated — a CRC-verified decode is a real
	// signal regardless of how wide its averaged energy reads.
	minEnergyBandHz = signalWidthHz / 4 // 12 Hz ≈ 4 bins at the 3.125 Hz resolution

	// defaultGuardMarginHz is the default clearance a suggested offset keeps
	// from adjacent occupied bands (config: ft8.tx.occupancy.guard_margin_hz,
	// 0 = off). ~10 Hz ≈ a fifth of a signal width — enough to avoid sitting
	// flush against a neighbour without eating much of a clear gap.
	defaultGuardMarginHz = 10
)

// occupancy source labels for Band.Source.
const (
	sourceDecode = "decode"
	sourceEnergy = "energy"
	sourceBoth   = "both"
)

// Band is an audio-frequency range [LowHz, HighHz] in Hz. Used both for the
// passband bounds and for each occupied range.
type Band struct {
	LowHz  int     `json:"low_hz"`
	HighHz int     `json:"high_hz"`
	Source string  `json:"source,omitempty"` // "decode" | "energy" | "both"; empty for the passband
	Level  float32 `json:"level,omitempty"`  // 0..1 peak energy relative to the loudest passband bin; optional shading
}

// SlotRef identifies the UTC slot an OccupancyReport covers.
type SlotRef struct {
	StartUTC string `json:"start_utc"` // RFC3339 UTC slot boundary
	Period   string `json:"period"`    // "even" | "odd"
}

// OccupancyReport is the per-slot decision data emitted for TX-offset
// selection. It is NOT a spectrogram: Occupied is a merged list of busy ranges
// (typically a handful), and Suggested is a daemon-ranked best-first list of
// clear base offsets for one-click slot selection. See ADR 0029.
type OccupancyReport struct {
	Slot          SlotRef `json:"slot"`
	Passband      Band    `json:"passband"`
	SignalWidthHz int     `json:"signal_width_hz"`
	Occupied      []Band  `json:"occupied"`
	Suggested     []int   `json:"suggested"`
}

// DefaultOccupancyConfig returns the fallback tuning (config path
// ft8.tx.occupancy.*). These are code defaults; resolveOccupancyConfig overlays
// any operator overrides onto them at Service construction.
func DefaultOccupancyConfig() types.Ft8OccupancyConfig {
	guard := defaultGuardMarginHz
	return types.Ft8OccupancyConfig{
		PassbandLowHz:   200,
		PassbandHighHz:  3000,
		ThresholdFactor: 4.0, // ~6 dB over the median floor
		WeightMargin:    0.5,
		WeightEdge:      0.2,
		WeightCentered:  0.3,
		GuardMarginHz:   &guard,
	}
}

// resolveOccupancyConfig overlays an operator's sparse overrides onto the
// built-in defaults: every zero field falls back to its default. A nil override
// yields the defaults unchanged.
func resolveOccupancyConfig(c *types.Ft8OccupancyConfig) types.Ft8OccupancyConfig {
	d := DefaultOccupancyConfig()
	if c == nil {
		return d
	}
	if c.PassbandLowHz != 0 {
		d.PassbandLowHz = c.PassbandLowHz
	}
	if c.PassbandHighHz != 0 {
		d.PassbandHighHz = c.PassbandHighHz
	}
	if c.ThresholdFactor != 0 {
		d.ThresholdFactor = c.ThresholdFactor
	}
	if c.WeightMargin != 0 {
		d.WeightMargin = c.WeightMargin
	}
	if c.WeightEdge != 0 {
		d.WeightEdge = c.WeightEdge
	}
	if c.WeightCentered != 0 {
		d.WeightCentered = c.WeightCentered
	}
	if c.GuardMarginHz != nil { // pointer: an explicit 0 (guard off) overrides the default
		d.GuardMarginHz = c.GuardMarginHz
	}
	return d
}

// Occupancy computes the per-slot occupancy report from a slot's raw samples
// and the decodes go-ft8 produced for the same slot. Pure and deterministic.
// cfg should be a resolved config (see resolveOccupancyConfig) — all fields
// meaningful, not the operator's sparse overrides.
func Occupancy(slot SlotRef, samples []int16, decodes []goft8.DecodedMessage, cfg types.Ft8OccupancyConfig) OccupancyReport {
	power := averagePowerSpectrum(samples, occupancyFFTSize)
	bands := detectEnergyBands(power, cfg)
	bands = append(bands, decodeBands(decodes, cfg)...)
	occupied := mergeBands(bands)
	return OccupancyReport{
		Slot:          slot,
		Passband:      Band{LowHz: cfg.PassbandLowHz, HighHz: cfg.PassbandHighHz},
		SignalWidthHz: signalWidthHz,
		Occupied:      occupied,
		Suggested:     suggestOffsets(occupied, cfg),
	}
}

// SlotRefFromTime builds a SlotRef from a slot boundary. Period is the FT8
// even/odd 15-second alternation aligned to the UTC minute: :00 and :30 are
// "even", :15 and :45 are "odd" (the WSJT-X convention).
func SlotRefFromTime(start time.Time) SlotRef {
	start = start.UTC()
	period := "even"
	if (start.Unix()/slotSeconds)%2 != 0 {
		period = "odd"
	}
	return SlotRef{StartUTC: start.Format(time.RFC3339), Period: period}
}

// averagePowerSpectrum computes a Hann-windowed, 50%-overlap Welch power
// spectrum of the slot. Averaging across the slot's windows is what separates
// a steady occupant from a transient click, and the Hann window keeps a strong
// signal from smearing across the band. Returns n/2+1 power bins (bin k centred
// at k·SampleRate/n Hz); an empty/short slot yields all zeros.
func averagePowerSpectrum(samples []int16, n int) []float64 {
	bins := n/2 + 1
	acc := make([]float64, bins)
	if len(samples) < n {
		return acc
	}
	plan := audio.NewRealPlan(n)
	win := hann(n)
	frame := make([]float32, n)
	step := n / 2
	count := 0
	for start := 0; start+n <= len(samples); start += step {
		for i := 0; i < n; i++ {
			frame[i] = float32(samples[start+i]) * win[i]
		}
		spec := plan.FFT(frame)
		for k := 0; k < bins; k++ {
			re, im := real(spec[k]), imag(spec[k])
			acc[k] += re*re + im*im
		}
		count++
	}
	if count > 0 {
		inv := 1.0 / float64(count)
		for k := range acc {
			acc[k] *= inv
		}
	}
	return acc
}

// hann returns an n-point Hann window.
func hann(n int) []float32 {
	w := make([]float32, n)
	if n == 1 {
		w[0] = 1
		return w
	}
	for i := range w {
		w[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	return w
}

// detectEnergyBands thresholds the power spectrum against a robust noise floor
// (the median passband bin) and returns contiguous over-threshold runs as
// energy-source bands, each carrying its peak level normalised against the
// loudest passband bin.
func detectEnergyBands(power []float64, cfg types.Ft8OccupancyConfig) []Band {
	binHz := float64(goft8.SampleRate) / float64(occupancyFFTSize)
	binLo := int(math.Round(float64(cfg.PassbandLowHz) / binHz))
	binHi := int(math.Round(float64(cfg.PassbandHighHz) / binHz))
	if binLo < 1 { // skip DC
		binLo = 1
	}
	if binHi >= len(power) {
		binHi = len(power) - 1
	}
	if binLo > binHi {
		return nil
	}

	floor := median(power[binLo : binHi+1])
	threshold := floor * cfg.ThresholdFactor

	maxPow := 0.0
	for k := binLo; k <= binHi; k++ {
		if power[k] > maxPow {
			maxPow = power[k]
		}
	}

	minEnergyBins := int(math.Round(minEnergyBandHz / binHz))
	if minEnergyBins < 1 {
		minEnergyBins = 1
	}

	var bands []Band
	for k := binLo; k <= binHi; {
		if power[k] <= threshold {
			k++
			continue
		}
		start := k
		peak := power[k]
		for k <= binHi && power[k] > threshold {
			if power[k] > peak {
				peak = power[k]
			}
			k++
		}
		end := k - 1 // last over-threshold bin
		if end-start+1 < minEnergyBins {
			continue // run too narrow to be a signal — noise/leakage spike, not an occupant
		}
		var level float32
		if maxPow > 0 {
			level = float32(peak / maxPow)
		}
		bands = append(bands, Band{
			LowHz:  int(math.Round(float64(start) * binHz)),
			HighHz: int(math.Round(float64(end+1) * binHz)), // +1 bin to cover the bin's upper edge
			Source: sourceEnergy,
			Level:  level,
		})
	}
	return bands
}

// decodeBands marks the [FreqHz, FreqHz+signalWidthHz] span each decode
// occupies (go-ft8 reports the base/sync tone; the signal extends upward),
// clamped to the passband. Decodes wholly outside the passband are dropped.
func decodeBands(decodes []goft8.DecodedMessage, cfg types.Ft8OccupancyConfig) []Band {
	var bands []Band
	for _, m := range decodes {
		base := int(math.Round(m.FreqHz))
		low, high := base, base+signalWidthHz
		if high <= cfg.PassbandLowHz || low >= cfg.PassbandHighHz {
			continue
		}
		if low < cfg.PassbandLowHz {
			low = cfg.PassbandLowHz
		}
		if high > cfg.PassbandHighHz {
			high = cfg.PassbandHighHz
		}
		bands = append(bands, Band{LowHz: low, HighHz: high, Source: sourceDecode})
	}
	return bands
}

// mergeBands sorts bands by start and coalesces overlapping or touching ones,
// combining their sources (decode+energy → both) and keeping the louder level.
// Merging is deliberately conservative: a contiguous busy region is one band,
// which keeps the picker from squeezing a transmission into a sliver between
// two near-adjacent signals.
func mergeBands(bands []Band) []Band {
	if len(bands) == 0 {
		return nil
	}
	sort.Slice(bands, func(i, j int) bool {
		if bands[i].LowHz != bands[j].LowHz {
			return bands[i].LowHz < bands[j].LowHz
		}
		return bands[i].HighHz < bands[j].HighHz
	})
	out := []Band{bands[0]}
	for _, b := range bands[1:] {
		last := &out[len(out)-1]
		if b.LowHz <= last.HighHz { // overlap or touch
			if b.HighHz > last.HighHz {
				last.HighHz = b.HighHz
			}
			last.Source = combineSource(last.Source, b.Source)
			if b.Level > last.Level {
				last.Level = b.Level
			}
			continue
		}
		out = append(out, b)
	}
	return out
}

// combineSource merges two band-source labels. Equal sources collapse to
// themselves; any mix becomes "both".
func combineSource(a, b string) string {
	if a == b {
		return a
	}
	return sourceBoth
}

// clearGap is a clear [lo, hi) audio-frequency range between occupied bands.
type clearGap struct{ lo, hi int }

// clearGaps inverts the (sorted, merged) occupied bands against the passband
// [lo, hi), returning the clear ranges between them.
func clearGaps(occupied []Band, lo, hi int) []clearGap {
	var gaps []clearGap
	cursor := lo
	for _, b := range occupied {
		if b.LowHz > cursor {
			gaps = append(gaps, clearGap{cursor, b.LowHz})
		}
		if b.HighHz > cursor {
			cursor = b.HighHz
		}
	}
	if cursor < hi {
		gaps = append(gaps, clearGap{cursor, hi})
	}
	return gaps
}

// resolveGuard returns the clearance a suggested offset keeps from the occupied
// bands bounding its gap, so a recommendation never sits flush against a
// neighbour. nil → default; explicit 0 → off (flush allowed); negatives clamp to 0.
func resolveGuard(cfg types.Ft8OccupancyConfig) int {
	guard := defaultGuardMarginHz
	if cfg.GuardMarginHz != nil {
		guard = *cfg.GuardMarginHz
	}
	if guard < 0 {
		guard = 0
	}
	return guard
}

// offsetClear reports whether a base offset is still usable for TX this slot:
// its signal [off, off+signalWidthHz] plus the guard margin on each side fits
// entirely within one clear gap. This is the same admission bar suggestOffsets
// applies to candidates, used by stickySuggested to decide if the previous
// recommendation remains "reasonably clear".
func offsetClear(occupied []Band, cfg types.Ft8OccupancyConfig, off int) bool {
	guard := resolveGuard(cfg)
	for _, g := range clearGaps(occupied, cfg.PassbandLowHz, cfg.PassbandHighHz) {
		if off >= g.lo+guard && off+signalWidthHz <= g.hi-guard {
			return true
		}
	}
	return false
}

// suggestOffsets inverts the occupied bands against the passband, finds clear
// gaps wide enough for a signal, and scores candidate base offsets (stepped one
// signal-width apart through each gap) by the configured weights. Returns the
// best-first offsets, capped at maxSuggested.
func suggestOffsets(occupied []Band, cfg types.Ft8OccupancyConfig) []int {
	lo, hi := cfg.PassbandLowHz, cfg.PassbandHighHz
	gaps := clearGaps(occupied, lo, hi)
	guard := resolveGuard(cfg)

	// Derived scoring references (not magic numbers — keyed to the geometry):
	// a gap four signals wide saturates the margin score; edge distance is
	// scored against half the passband.
	marginRef := float64(4 * signalWidthHz)
	edgeRef := float64(hi-lo) / 2.0
	if edgeRef <= 0 {
		edgeRef = 1
	}

	type scored struct {
		off   int
		score float64
	}
	var cands []scored
	for _, g := range gaps {
		w := g.hi - g.lo
		if w < signalWidthHz+2*guard { // can't fit the signal plus a guard band each side
			continue
		}
		gapCenter := float64(g.lo+g.hi) / 2.0
		gapHalf := float64(w) / 2.0
		marginScore := clamp01(float64(w) / marginRef)
		for off := g.lo + guard; off <= g.hi-guard-signalWidthHz; off += signalWidthHz {
			sigCenter := float64(off) + float64(signalWidthHz)/2.0
			centeredScore := clamp01(1 - math.Abs(sigCenter-gapCenter)/gapHalf)
			edgeScore := clamp01(math.Min(sigCenter-float64(lo), float64(hi)-sigCenter) / edgeRef)
			score := cfg.WeightMargin*marginScore +
				cfg.WeightEdge*edgeScore +
				cfg.WeightCentered*centeredScore
			cands = append(cands, scored{off, score})
		}
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].off < cands[j].off // stable, low-first tie-break
	})

	n := len(cands)
	if n > maxSuggested {
		n = maxSuggested
	}
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, cands[i].off)
	}
	return out
}

// stickySuggested applies offset hysteresis to a slot's ranked clear offsets:
// if prev (the previous slot's top recommendation) is still clear this slot, it
// is floated to the front so the ★ recommendation doesn't hop to a marginally
// wider gap while the operator's current spot remains usable. The per-slot
// scoring is unchanged — this only decides which clear offset leads the list;
// the rest still follow in score order, and the operator sees every option.
//
// prev == 0 (no previous pick) or a prev that has since become occupied returns
// the fresh ranking untouched — stickiness never keeps a spot a signal has
// moved into. "Still clear" is offsetClear's guard-margin bar.
func stickySuggested(suggested []int, occupied []Band, cfg types.Ft8OccupancyConfig, prev int) []int {
	if prev == 0 || !offsetClear(occupied, cfg, prev) {
		return suggested
	}
	// prev may not be among this slot's freshly-generated candidates (gap
	// stepping shifts the offsets), but it is still usable — lead with it, then
	// the fresh ranking minus any duplicate.
	out := make([]int, 0, len(suggested)+1)
	out = append(out, prev)
	for _, o := range suggested {
		if o != prev {
			out = append(out, o)
		}
	}
	if len(out) > maxSuggested {
		out = out[:maxSuggested]
	}
	return out
}

// median returns the median of xs without mutating it. Empty → 0.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := make([]float64, len(xs))
	copy(cp, xs)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

// clamp01 clamps x to [0, 1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
