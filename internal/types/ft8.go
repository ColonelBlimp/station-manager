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

	// Display holds the SPA's FT8 Band Activity display preferences (row cap,
	// feed mode, CQ highlight colours). These are durable per-operator settings,
	// edited from the FT8 Settings tab via /v1/config — NOT per-session browser
	// state — so they live in config.json rather than localStorage. Pointer-typed
	// for the same inert-block reason as TX; only operator-set values persist
	// (the daemon serves resolved defaults via ResolveFt8Display).
	Display *Ft8DisplayConfig `json:"display,omitempty"`
}

// Ft8DisplayConfig holds the FT8 Band Activity display preferences the SPA reads
// from and writes to /v1/config. The daemon does not consume these (they're pure
// SPA presentation) — it stores the operator's choices and serves them resolved
// against the defaults below, so a fresh config still yields sensible values.
//
// Every field is zero-means-use-the-default (resolved by ResolveFt8Display);
// omitempty keeps unset knobs out of a rewritten config.
type Ft8DisplayConfig struct {
	// HistoryMax caps the rolling Band Activity decode feed (rows). 0 → default.
	HistoryMax int `json:"history_max,omitempty"`
	// FeedMode is the Band Activity feed mode: "accumulate" (roll slots up,
	// capped at HistoryMax) or "single" (show only the current 15 s slot).
	// Empty → default "accumulate".
	FeedMode string `json:"feed_mode,omitempty"`
	// HighlightUnworked / HighlightWorked are the CQ-row tint colours (CSS hex)
	// for a station not-yet-worked-on-this-band vs worked-before. Empty → defaults.
	HighlightUnworked string `json:"highlight_unworked,omitempty"`
	HighlightWorked   string `json:"highlight_worked,omitempty"`
}

// FT8 display defaults. Code constants (the single source of truth, shared by
// daemon and — via the resolved /v1/config response — the SPA), used as fallback
// when the operator has set nothing. Mirror the SPA's former localStorage
// defaults (row cap 100, accumulate, WSJT-X-land green/grey).
const (
	DefaultFt8HistoryMax        = 100
	DefaultFt8FeedMode          = "accumulate"
	DefaultFt8HighlightUnworked = "#15803d" // tailwind green-700 — un-worked (attention)
	DefaultFt8HighlightWorked   = "#9ca3af" // tailwind gray-400 — worked-before (muted)
)

// Ft8FeedModeValid reports whether s is an accepted feed-mode literal.
func Ft8FeedModeValid(s string) bool {
	return s == "accumulate" || s == "single"
}

// ResolveFt8Display overlays an operator's sparse Display overrides onto the
// built-in defaults: every zero/empty field falls back to its default. A nil
// override yields the defaults unchanged. The clamp on HistoryMax matches the
// SPA's former bounds so a hand-edited config can't hide every row or balloon
// the feed.
func ResolveFt8Display(c *Ft8DisplayConfig) Ft8DisplayConfig {
	d := Ft8DisplayConfig{
		HistoryMax:        DefaultFt8HistoryMax,
		FeedMode:          DefaultFt8FeedMode,
		HighlightUnworked: DefaultFt8HighlightUnworked,
		HighlightWorked:   DefaultFt8HighlightWorked,
	}
	if c == nil {
		return d
	}
	if c.HistoryMax != 0 { // 0 = unset → keep the default
		h := c.HistoryMax
		if h < 10 {
			h = 10
		} else if h > 2000 {
			h = 2000
		}
		d.HistoryMax = h
	}
	if Ft8FeedModeValid(c.FeedMode) {
		d.FeedMode = c.FeedMode
	}
	if c.HighlightUnworked != "" {
		d.HighlightUnworked = c.HighlightUnworked
	}
	if c.HighlightWorked != "" {
		d.HighlightWorked = c.HighlightWorked
	}
	return d
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

	// Mode is the rig data-mode literal the TX controller switches the rig to
	// before keying PTT (ADR 0030), e.g. "DATA-U" on the FTdx10 — the same
	// vocabulary set_mode uses — and restores after the transmission. Empty
	// leaves the rig's current mode untouched, for operators who keep the rig in
	// the data mode themselves. omitempty so the inert field doesn't persist in
	// a rewritten config.
	Mode string `json:"mode,omitempty"`

	// CallerAnswerMode selects how a sequenced Call-CQ session picks which station
	// to work when stations answer (ADR 0033): "auto_first" works the first valid
	// answerer (WSJT-X "Auto Seq"); "operator_pick" queues answerers for the
	// operator to pop (the pile-up stack). Empty/invalid → the
	// ResolveFt8CallerAnswerMode default (auto_first). Edited from the FT8 Settings
	// tab once operator_pick ships; until then the daemon reads it from config.json.
	CallerAnswerMode string `json:"caller_answer_mode,omitempty"`

	// Occupancy tunes the per-slot occupancy detector and clear-offset ranking
	// (ADR 0029 step a). Pointer-typed for the same inert-block reason as TX.
	Occupancy *Ft8OccupancyConfig `json:"occupancy,omitempty"`
}

// Caller-answer-mode literals (ADR 0033) for Ft8TXConfig.CallerAnswerMode.
const (
	Ft8CallerAnswerAutoFirst    = "auto_first"    // work the first valid answerer (WSJT-X "Auto Seq")
	Ft8CallerAnswerOperatorPick = "operator_pick" // queue answerers; operator pops one (pile-up stack)

	// DefaultFt8CallerAnswerMode is the resolve fallback (ADR 0033).
	DefaultFt8CallerAnswerMode = Ft8CallerAnswerAutoFirst
)

// Ft8CallerAnswerModeValid reports whether s is an accepted caller-answer-mode literal.
func Ft8CallerAnswerModeValid(s string) bool {
	return s == Ft8CallerAnswerAutoFirst || s == Ft8CallerAnswerOperatorPick
}

// ResolveFt8CallerAnswerMode returns the effective caller-answer mode: the operator's
// setting when valid, else the default (auto_first). A nil TX block resolves to the
// default. ADR 0033 — when WE call CQ, which answering station do we work: the first
// one automatically, or one the operator picks from the stack.
func ResolveFt8CallerAnswerMode(c *Ft8TXConfig) string {
	if c == nil || !Ft8CallerAnswerModeValid(c.CallerAnswerMode) {
		return DefaultFt8CallerAnswerMode
	}
	return c.CallerAnswerMode
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
