package types

// Ft8Config holds the daemon's FT8 subsystem configuration. The FT8 work
// runs as an in-process subsystem of `cmd/smd` under `internal/ft8`,
// decoding live receive audio into messages.
//
// Enabled gates the whole subsystem. When false (operator not running
// digital modes, no FT8 hardware, or the network-only aggregator
// deployment) the subsystem acquires no audio device and spins up no
// decoder goroutines. Default false — FT8 stays opt-in.
//
// Device selects the audio capture device, a single identifier string.
// Under ADR 0028 the authoritative source is the per-rig
// RigConfig.Audio.Device in the rig catalogue; Config.ActiveFt8() projects
// the active rig's value onto this field, always winning over any value
// left here. So Device is a resolved runtime view, not an on-disk source —
// hand-setting it has no effect when a rig catalogue exists. omitempty so
// the inert loose field doesn't persist in rewritten configs (mirrors the
// empty bridge.serial.port / bridge.cat.driver loose fields). Empty means
// the system default capture device.
//
// EnableOSD turns on go-ft8's OSD-2/MRB fallback decode (ordered-statistics
// decoding after belief-propagation misses) — the analog to WSJT-X/jt9's
// deeper LDPC effort. It recovers weak signals BP alone misses (measured
// 2026-06-02 against jt9 -d 3: ~5 extra of 7 per the live A/B) for ~1.1–1.7×
// decode time, well inside the 15 s slot budget. Pointer-typed so applyDefaults
// can distinguish "absent" (nil → default true) from an explicit operator
// false. Default true — the cost is negligible and the recall gain is real.
//
// The surface is deliberately minimal — it grows (frequency lists, decode
// policy, transmit policy) as the corresponding implementation lands, so
// config schema and behaviour stay in lockstep.
type Ft8Config struct {
	Enabled   bool   `json:"enabled"`
	Device    string `json:"device,omitempty"`
	EnableOSD *bool  `json:"enable_osd,omitempty"`

	// TX groups the FT8 transmit-related tunables (ADR 0029). Pointer-typed
	// so an operator who sets nothing leaves no inert block behind in a
	// rewritten config (same discipline as Device/EnableOSD).
	TX *Ft8TXConfig `json:"tx,omitempty"`
}

// Ft8TXConfig holds the FT8 transmit configuration (ADR 0029). Minimal today —
// the occupancy detector that feeds TX-offset selection and the output-device
// selection are built; it grows (PTT, sequencing) as those layers land.
type Ft8TXConfig struct {
	// Device selects the audio OUTPUT (playback) device the TX waveform is
	// streamed to — an integer index string as listed by `cmd/ft8-tx-probe
	// -list`, separate from the capture-side Ft8Config.Device because the
	// playback and capture device enumerations are independent (even when the
	// rig's USB codec is physically one device). Empty → system default
	// playback device. Name-based matching is a noted follow-up, mirroring the
	// capture side. Like Ft8Config.Device, ADR 0028's per-rig audio device is
	// expected to win over this loose field once TX device resolution lands.
	Device string `json:"device,omitempty"`

	// Occupancy tunes the per-slot occupancy detector and clear-offset ranking
	// (ADR 0029 step a). Pointer-typed for the same inert-block reason as TX.
	Occupancy *Ft8OccupancyConfig `json:"occupancy,omitempty"`
}

// Ft8OccupancyConfig tunes the per-slot occupancy detector and the clear-offset
// ranking that feeds TX-frequency selection (ADR 0029). Every field is
// zero-means-use-the-built-in-default (resolved against ft8.DefaultOccupancyConfig);
// omitempty keeps unset knobs out of a rewritten config.
//
// Caveat of the zero-means-default rule: a ranking weight cannot be set to
// exactly 0 to disable its term — a zero reads as "unset" and falls back to the
// default. Use a small value (e.g. 0.001) to effectively disable a term.
type Ft8OccupancyConfig struct {
	// PassbandLowHz, PassbandHighHz bound the audio range the picker spans.
	PassbandLowHz  int `json:"passband_low_hz,omitempty"`
	PassbandHighHz int `json:"passband_high_hz,omitempty"`

	// ThresholdFactor multiplies the per-slot noise-floor estimate (median
	// passband power) to set the occupied/clear cutoff. Higher marks fewer,
	// stronger bands busy.
	ThresholdFactor float64 `json:"threshold_factor,omitempty"`

	// Ranking weights for the suggested clear offsets (best-first). Each term
	// scores a candidate 0..1; the weighted sum orders them. Only relative
	// sizes matter.
	WeightMargin   float64 `json:"weight_margin,omitempty"`   // reward wider clear room in the offset's gap
	WeightEdge     float64 `json:"weight_edge,omitempty"`     // reward distance from passband edges
	WeightCentered float64 `json:"weight_centered,omitempty"` // reward sitting centered in the gap

	// GuardMarginHz is the clearance (Hz) a suggested offset must keep from the
	// nearest occupied band on each side, so a recommendation never sits flush
	// against a neighbour ("brushed edge"). Pointer-typed so resolve can tell
	// "absent" (nil → default, guard on) from an explicit 0:
	//   - nil  → default (ft8.defaultGuardMarginHz, guard on)
	//   - 0    → guard off (flush placements allowed — more options in a
	//            crowded band, at the cost of brushed edges)
	//   - N    → require N Hz clearance each side
	// Larger values yield safer placement but fewer options in a busy band
	// (a gap must be signalWidthHz + 2·N wide to offer anything).
	GuardMarginHz *int `json:"guard_margin_hz,omitempty"`
}
