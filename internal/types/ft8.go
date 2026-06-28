package types

import "regexp"

// Ft8Config holds the daemon's FT8 subsystem configuration. The FT8 work
// runs as an in-process subsystem of `cmd/smd` under `internal/ft8`,
// decoding live receive audio into messages.
//
// Enabled gates the whole subsystem. When false (operator not running
// digital modes, no FT8 hardware, or the network-only aggregator
// deployment) the subsystem acquires no audio device and spins up no
// decoder goroutines. Default false — FT8 stays opt-in.
//
// Device selects the audio capture device. Under ADR 0028 the authoritative
// source is the per-rig RigConfig.Audio.RX (a device NAME) in the rig
// catalogue; Config.ActiveFt8() projects the active rig's value onto this
// field. So Device is a resolved runtime view, not an on-disk operator knob —
// mirroring how ActiveBridge() projects the active rig's port/driver onto
// bridge.serial.port / bridge.cat.driver. The value is a device name, resolved
// to a live index at acquire time (an integer string is still honoured as a
// raw index for any un-migrated config). omitempty so the resolved field
// doesn't persist in rewritten configs. Empty means the system default capture
// device.
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

	// DecodeLog configures the JTDX-style ALL.TXT decode log (RX decodes + our own
	// TX), a diagnostic record written independently of the daemon log level so an
	// operator can reconstruct an on-air exchange after the fact. Pointer-typed for
	// the same inert-block reason as TX/Display; nil or Enabled=false → no file.
	DecodeLog *Ft8DecodeLogConfig `json:"decode_log,omitempty"`

	// Frequencies maps a band label (e.g. "20m") to its FT8 dial frequency in Hz.
	// SPA-facing (the daemon doesn't consume it): the Main-Freq band buttons tune to
	// these and highlight the one matching the live dial. Stored sparse, served
	// resolved (defaults + operator overrides) on /v1/config like Display; nil/empty →
	// the ResolveFt8Frequencies defaults (the WSJT-X FT8 dial frequencies). omitempty
	// so an untouched config carries no inert block.
	Frequencies map[string]int `json:"frequencies,omitempty"`

	// FieldDay holds the operator's ARRL Field Day exchange (class + ARRL/RAC
	// section), sent when ANSWERING a CQ FD over FT8 — search & pounce only; SM
	// does not call CQ FD. Pointer-typed for the same inert-block reason as
	// TX/Display: FD is one weekend a year, so an unset block is the normal state.
	// The canonical section enumeration + the FD message ladder are owned by
	// go-ft8; this is just the SM-owned identity the reply carries.
	FieldDay *Ft8FieldDayConfig `json:"field_day,omitempty"`
}

// Ft8FieldDayConfig is the operator's ARRL Field Day exchange, transmitted when
// answering a CQ FD over FT8. Class is "<transmitters><category>" (e.g. "2A", "1D",
// "5F"); Section is the ARRL/RAC section, or "DX" for a station outside US/Canada.
// Both empty = FD identity not set (the off-season default). Stored upper-cased.
type Ft8FieldDayConfig struct {
	Class   string `json:"class,omitempty"`
	Section string `json:"section,omitempty"`
}

// ft8FieldDayClassPattern matches a Field Day class: transmitter count 1–99 followed
// by a category letter A–F (e.g. "2A", "1D", "10F"). Anchored, so junk is rejected.
var ft8FieldDayClassPattern = regexp.MustCompile(`^[1-9][0-9]?[A-F]$`)

// ft8FieldDaySectionPattern is a LOOSE guard only — 2–4 upper-case letters/digits,
// enough to catch a fat-finger. The authoritative ARRL/RAC section list is owned by
// go-ft8 (it encodes the section into the FD frame and will expose
// ARRLFieldDaySections() / ValidARRLFieldDaySection()); Ft8FieldDaySectionValid is
// the single swap point that delegates to that contract once the release is pinned.
var ft8FieldDaySectionPattern = regexp.MustCompile(`^[A-Z0-9]{2,4}$`)

// Ft8FieldDayClassValid reports whether s is a well-formed Field Day class.
func Ft8FieldDayClassValid(s string) bool {
	return ft8FieldDayClassPattern.MatchString(s)
}

// Ft8FieldDaySectionValid reports whether s passes SM's LOOSE section guard. This is
// deliberately NOT a membership test against the canonical ARRL/RAC list — go-ft8
// owns that (ValidARRLFieldDaySection); swap this body to delegate when go-ft8 lands.
func Ft8FieldDaySectionValid(s string) bool {
	return ft8FieldDaySectionPattern.MatchString(s)
}

// Ft8DecodeLogConfig configures the FT8 decode log — a WSJT-X/JTDX ALL.TXT-style
// file. When Enabled, every RX slot's decodes and our own transmissions are
// appended in JTDX line format, independent of the daemon's log level (so the
// decode stream survives at the default info level, where the per-decode log line
// is gated off). Off by default: the file grows unbounded, like WSJT-X's ALL.TXT,
// and the operator clears it. Path defaults to $SM_WORKING_DIR/log/ft8-all.txt
// when empty (next to smd.log).
type Ft8DecodeLogConfig struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path,omitempty"`
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
	// HighlightCalling is the text colour (CSS hex) for a station calling US — the
	// pile-up/toMe rows in Band Activity. Empty → default (amber). No LSPA picker:
	// it's edited via config.json / the config SPA (the LSPA only reads + applies it).
	HighlightCalling string `json:"highlight_calling,omitempty"`
	// CqToTop floats CQ decodes to the top of the Band Activity feed (the
	// actionable rows — the ones you can answer — pinned above the rest) instead
	// of leaving them interleaved by slot. Plain bool: default false (off) is the
	// zero value, so no resolve special-casing is needed.
	CqToTop bool `json:"cq_to_top,omitempty"`
	// HideHashedCalls drops Band Activity decodes carrying an unresolved hashed
	// callsign (rendered "<...>") — third-party traffic for a non-standard/compound
	// call the receiver can't expand, which can't be identified or worked, so it's
	// pure clutter. Stations calling US still show (the SPA's toMe-bypass). Plain
	// bool: default false (show everything), the zero value.
	HideHashedCalls bool `json:"hide_hashed_calls,omitempty"`
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
	DefaultFt8HighlightCalling  = "#b45309" // tailwind amber-700 — a station calling us
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
		HighlightCalling:  DefaultFt8HighlightCalling,
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
	if c.HighlightCalling != "" {
		d.HighlightCalling = c.HighlightCalling
	}
	d.CqToTop = c.CqToTop                 // plain bool — default false, operator value passes through
	d.HideHashedCalls = c.HideHashedCalls // plain bool — default false (show)
	return d
}

// DefaultFt8Frequencies returns the per-band FT8 dial frequencies (Hz) — the WSJT-X
// "watering hole" defaults (largely region-independent for FT8; 60 m = 5.357 MHz).
// The single source of truth, served resolved to the SPA via /v1/config; operator
// overrides merge over these (ResolveFt8Frequencies).
func DefaultFt8Frequencies() map[string]int {
	return map[string]int{
		"160m": 1_840_000,
		"80m":  3_573_000,
		"60m":  5_357_000,
		"40m":  7_074_000,
		"30m":  10_136_000,
		"20m":  14_074_000,
		"17m":  18_100_000,
		"15m":  21_074_000,
		"12m":  24_915_000,
		"10m":  28_074_000,
		"6m":   50_313_000,
	}
}

// ResolveFt8Frequencies overlays an operator's sparse band→Hz overrides onto the
// built-in defaults: a positive override replaces that band's default (and may add a
// band the defaults don't list); non-positive or absent entries keep the default. A
// nil/empty override yields the defaults unchanged. Returns a fresh map so callers can
// serve it without mutating the package defaults.
func ResolveFt8Frequencies(c map[string]int) map[string]int {
	d := DefaultFt8Frequencies()
	for band, hz := range c {
		if hz > 0 {
			d[band] = hz
		}
	}
	return d
}

// Ft8TXConfig holds the FT8 transmit configuration (ADR 0029). Minimal today —
// the occupancy detector that feeds TX-offset selection and the output-device
// selection are built; it grows (PTT, sequencing) as those layers land.
type Ft8TXConfig struct {
	// Device selects the audio OUTPUT (playback) device the TX waveform is
	// streamed to. Authoritative source is the per-rig RigConfig.Audio.TX (a
	// device NAME); Config.ActiveFt8() projects the active rig's value here, so
	// this is a resolved runtime view, not an on-disk operator knob — the
	// playback analogue of Ft8Config.Device (RX). Separate from the capture side
	// because playback and capture enumerate independently (the rig's one USB
	// codec is capture index 4 / playback index 2 on the IC-7300). The value is
	// a device name resolved to a live index at acquire time (an integer string
	// still honoured as a raw index for any un-migrated config). Empty → system
	// default playback device.
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
	// answerer by decode order (WSJT-X "Auto Seq"); "auto_strongest" works the
	// highest-SNR valid answerer in the slot (clear the strong ones first). Both are
	// operator-writable from the logging SPA's FT8 Settings tab. "operator_pick" is
	// **superseded by the SPA pile-up stack** (Ctrl/Cmd+click a caller → FIFO →
	// work-a-caller drain) and is **rejected at start** (501 ft8_caller_mode_unsupported)
	// rather than silently auto-picking — it is a config.json-only literal, never
	// offered over the wire. Empty/invalid → the ResolveFt8CallerAnswerMode default
	// (auto_first).
	CallerAnswerMode string `json:"caller_answer_mode,omitempty"`

	// MaxRepeats caps how many times an unanswered rung is re-sent before the
	// sequencer auto-abandons the contact (ADR 0031 off-ramp). The operator's knob;
	// 0/absent → DefaultFt8MaxRepeats, and any value is clamped to
	// [1, Ft8MaxRepeatsCeiling] by ResolveFt8MaxRepeats. The ceiling is a hard
	// internal limit a config edit cannot exceed — so a typo (or a deliberate large
	// value) can never leave the rig calling a dead station for minutes on end.
	MaxRepeats int `json:"max_repeats,omitempty"`

	// Occupancy tunes the per-slot occupancy detector and clear-offset ranking
	// (ADR 0029 step a). Pointer-typed for the same inert-block reason as TX.
	Occupancy *Ft8OccupancyConfig `json:"occupancy,omitempty"`
}

// Caller-answer-mode literals (ADR 0033) for Ft8TXConfig.CallerAnswerMode.
const (
	Ft8CallerAnswerAutoFirst     = "auto_first"     // work the first valid answerer (WSJT-X "Auto Seq")
	Ft8CallerAnswerAutoStrongest = "auto_strongest" // work the highest-SNR valid answerer in the slot
	Ft8CallerAnswerOperatorPick  = "operator_pick"  // queue answerers; operator pops one (pile-up stack)

	// DefaultFt8CallerAnswerMode is the resolve fallback (ADR 0033).
	DefaultFt8CallerAnswerMode = Ft8CallerAnswerAutoFirst
)

// Ft8CallerAnswerModeValid reports whether s is an accepted caller-answer-mode literal.
func Ft8CallerAnswerModeValid(s string) bool {
	return s == Ft8CallerAnswerAutoFirst ||
		s == Ft8CallerAnswerAutoStrongest ||
		s == Ft8CallerAnswerOperatorPick
}

// Unanswered-rung repeat cap (ADR 0031 off-ramp) for Ft8TXConfig.MaxRepeats.
const (
	// DefaultFt8MaxRepeats is the resolve fallback — ~6 of our slots ≈ 90 s of calling.
	DefaultFt8MaxRepeats = 6
	// Ft8MaxRepeatsCeiling is the hard internal ceiling the config can never exceed.
	Ft8MaxRepeatsCeiling = 10
)

// ResolveFt8MaxRepeats returns the effective unanswered-rung repeat cap: the
// operator's value clamped to [1, Ft8MaxRepeatsCeiling], or DefaultFt8MaxRepeats
// when unset/non-positive. A nil TX block resolves to the default. The ceiling is
// a safety bound — like the tune-power / auto-off clamps (ADR 0027) — so no config
// value can keep the sequencer transmitting at a silent station indefinitely.
func ResolveFt8MaxRepeats(c *Ft8TXConfig) int {
	if c == nil || c.MaxRepeats <= 0 {
		return DefaultFt8MaxRepeats
	}
	if c.MaxRepeats > Ft8MaxRepeatsCeiling {
		return Ft8MaxRepeatsCeiling
	}
	return c.MaxRepeats
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
