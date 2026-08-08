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

	// Audio holds the RX audio-level meter's classification thresholds
	// (dogfood 2026-08-06). SPA-facing like Frequencies: the daemon publishes
	// raw dBFS measurements on /v1/ft8/events and the SPA classifies against
	// these, served resolved on /v1/config. Pointer-typed for the inert-block
	// discipline; calibration is a config.json edit + restart (no PUT path or
	// Settings UI yet — deliberate, until the numbers are hardware-calibrated).
	Audio *Ft8AudioConfig `json:"audio,omitempty"`

	// Meter holds the TX-drive (ALC) display threshold (ADR 0064); sparse,
	// resolved via ResolveFt8Meter for /v1/config.
	Meter *Ft8MeterConfig `json:"meter,omitempty"`

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
	// DefaultRstRcvd is the value logged as RST_RCVD for an FD QSO. Field Day exchanges
	// class+section, not a signal report, so we never receive an RST — but some OQRS
	// systems require RST_RCVD non-empty. The operator sets this to whatever their OQRS
	// wants (e.g. "59", "599", "-15"). Empty → RST_RCVD left blank. (RST_SENT is the
	// measured SNR, recorded from the decode like a standard FT8 QSO.)
	DefaultRstRcvd string `json:"default_rst_rcvd,omitempty"`
}

// ft8FieldDayClassPattern matches a Field Day class: transmitter count 1–99 followed
// by a category letter A–F (e.g. "2A", "1D", "10F"). Anchored, so junk is rejected.
var ft8FieldDayClassPattern = regexp.MustCompile(`^[1-9][0-9]?[A-F]$`)

// Ft8FieldDayClassValid reports whether s is a well-formed Field Day class. The
// SECTION is NOT validated here — it's checked in internal/config against go-ft8's
// canonical ARRL/RAC list (ValidARRLFieldDaySection); types stays stdlib-only and so
// cannot import go-ft8, and class is the one field with a purely syntactic rule.
func Ft8FieldDayClassValid(s string) bool {
	return ft8FieldDayClassPattern.MatchString(s)
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
	// HighlightUnworked / HighlightWorked / HighlightCalling were the CQ-row tint
	// colours (CSS hex) for not-yet-worked-on-this-band vs worked-before, and the
	// text colour for a station calling US.
	//
	// ALL THREE ARE VESTIGIAL as of 2026-08-05 (operator's ruling): their only
	// consumer was the frontend/logging SPA, retired 2026-07-21, and the app
	// shell's Band Activity uses a theme-aware palette instead — a single hex
	// cannot serve both light and dark. Settings → FT8 therefore renders no
	// colour pickers. They are still resolved here and round-tripped verbatim by
	// the SPA, because ft8_display is a whole-block replace: dropping them from a
	// payload would erase a hand-set config.json value. Empty → defaults.
	HighlightUnworked string `json:"highlight_unworked,omitempty"`
	HighlightWorked   string `json:"highlight_worked,omitempty"`
	HighlightCalling  string `json:"highlight_calling,omitempty"`
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
// Ft8AudioConfig is the stored (sparse) form of the RX level thresholds —
// dBFS bounds of the good decoding window. Unset fields take the defaults.
type Ft8AudioConfig struct {
	LowDbfs  *float64 `json:"low_dbfs,omitempty"`
	HighDbfs *float64 `json:"high_dbfs,omitempty"`
}

// Ft8AudioLevels is the resolved form served on /v1/config.
type Ft8AudioLevels struct {
	LowDbfs  float64 `json:"low_dbfs"`
	HighDbfs float64 `json:"high_dbfs"`
}

// Default RX-level window (dBFS): below Low the decoder is starving, above
// High the input is running hot (clipping itself is pinned at the SPA's fixed
// near-0 dBFS peak check, not here). WSJT-X-convention starting points —
// expected to be calibrated on hardware, not findings.
const (
	DefaultFt8AudioLowDbfs  = -60.0
	DefaultFt8AudioHighDbfs = -10.0
)

// ResolveFt8Audio applies the defaults to a sparse (or absent) audio block.
func ResolveFt8Audio(c *Ft8AudioConfig) Ft8AudioLevels {
	r := Ft8AudioLevels{LowDbfs: DefaultFt8AudioLowDbfs, HighDbfs: DefaultFt8AudioHighDbfs}
	if c == nil {
		return r
	}
	if c.LowDbfs != nil {
		r.LowDbfs = *c.LowDbfs
	}
	if c.HighDbfs != nil {
		r.HighDbfs = *c.HighDbfs
	}
	return r
}

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
	// highest-SNR valid answerer in the slot (clear the strong ones first);
	// "operator_pick" (ADR 0065 decision 3, implemented 2026-08-07) lists answerers
	// on the ft8-qso frames instead of auto-committing — the CQ keeps calling until
	// the operator pops one via POST /v1/ft8/cq/pick, and the run resumes CQ after
	// the contact. All three are config.json-only knobs (the app SPA has no ft8.tx
	// settings surface; operator-ratified 2026-08-07). Empty/invalid → the
	// ResolveFt8CallerAnswerMode default (operator_pick as of 2026-08-08 — see
	// DefaultFt8CallerAnswerMode).
	CallerAnswerMode string `json:"caller_answer_mode,omitempty"`

	// MaxRepeats caps how many times an unanswered rung is re-sent before the
	// sequencer auto-abandons the contact (ADR 0031 off-ramp). The operator's knob;
	// 0/absent → DefaultFt8MaxRepeats, and any value is clamped to
	// [1, Ft8MaxRepeatsCeiling] by ResolveFt8MaxRepeats. The ceiling is a hard
	// internal limit a config edit cannot exceed — so a typo (or a deliberate large
	// value) can never leave the rig calling a dead station for minutes on end.
	MaxRepeats int `json:"max_repeats,omitempty"`

	// AutoWorkCallers keeps working stations that call US after an operator-started
	// QSO completes, until Abandon (ADR 0059). DEFAULT OFF: the run transmits without
	// a click per contact, so enabling it on upgrade would change what the station
	// does on the air without the operator asking.
	//
	// It never applies from idle — a run is armed only by an operator action — which
	// is what keeps every session operator-initiated. Selection reuses
	// CallerAnswerMode, and only the two AUTO modes arm a run: operator_pick promises
	// the operator chooses, so arming under it would advertise a run that must not
	// pick anyone (invariant 7 — do not offer a control where it cannot act).
	AutoWorkCallers bool `json:"auto_work_callers,omitempty"`

	// Occupancy tunes the per-slot occupancy detector and clear-offset ranking
	// (ADR 0029 step a). Pointer-typed for the same inert-block reason as TX.
	Occupancy *Ft8OccupancyConfig `json:"occupancy,omitempty"`

	// InhibitIdle asks the desktop to stop idling, blanking and suspending for as
	// long as TX is armed. An armed FT8 run is exactly when the host looks idle to
	// the session (no keyboard, no mouse, for hours) and exactly when nobody is
	// watching — and a session/power event mid-run is the suspected cause of the
	// 2026-07-28 silent-transmit incident, where SM kept keying for 24 minutes with
	// no audio reaching the rig. nil (absent) → true; an explicit false is honoured
	// for operators who would rather SM left their power management alone. Inert on
	// a host that grants no inhibition (headless, no D-Bus): the daemon logs and
	// transmits regardless, because inhibition is a courtesy and TX is not.
	InhibitIdle *bool `json:"inhibit_idle,omitempty"`
}

// Caller-answer-mode literals (ADR 0033) for Ft8TXConfig.CallerAnswerMode.
const (
	Ft8CallerAnswerAutoFirst     = "auto_first"     // work the first valid answerer (WSJT-X "Auto Seq")
	Ft8CallerAnswerAutoStrongest = "auto_strongest" // work the highest-SNR valid answerer in the slot
	Ft8CallerAnswerOperatorPick  = "operator_pick"  // queue answerers; operator pops one (pile-up stack)

	// DefaultFt8CallerAnswerMode is the resolve fallback — operator_pick
	// (operator-ratified 2026-08-08, superseding ADR 0033's auto_first; dated
	// note in ADR 0065). The reasoning is the subsystem's licensing posture:
	// automatic operation is licence-restricted in many jurisdictions, so a
	// station whose operator never CHOSE an auto mode must not auto-work
	// anyone. A clean install, an absent key, and an invalid literal all fail
	// toward the non-automatic mode; with auto_work_callers also defaulting
	// off, a fresh install is fully manual until BOTH automations are
	// explicitly opted into. Consequence carried knowingly: an existing
	// config with auto_work_callers=true but no caller_answer_mode stops
	// arming runs until an auto mode is written (ResolveFt8AutoWorkCallers
	// excludes operator_pick).
	DefaultFt8CallerAnswerMode = Ft8CallerAnswerOperatorPick
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

// ResolveFt8InhibitIdle reports whether the daemon should keep the host awake
// while TX is armed. Absent → ON, and that must hold for an absent BLOCK as well
// as an absent field: Config.ActiveFt8() leaves Ft8Config.TX nil when there is no
// ft8.tx block, no ft8.tx.mode and no rig TX-audio device, so a caller that reads
// the field off the block would resolve a minimal config to "off" and silently
// contradict the documented default. A resolver rather than an applyDefaults
// entry for exactly that reason — there is nowhere to write a default into a block
// that does not exist. Only an explicit false turns it off.
func ResolveFt8InhibitIdle(c *Ft8TXConfig) bool {
	if c == nil || c.InhibitIdle == nil {
		return true
	}
	return *c.InhibitIdle
}

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
// setting when valid, else the default (operator_pick — see the const's licensing
// rationale). A nil TX block resolves to the default. ADR 0033 — when WE call CQ,
// which answering station do we work: one the operator picks from the stack, or the
// first/strongest automatically for operators who opted into automation.
func ResolveFt8CallerAnswerMode(c *Ft8TXConfig) string {
	if c == nil || !Ft8CallerAnswerModeValid(c.CallerAnswerMode) {
		return DefaultFt8CallerAnswerMode
	}
	return c.CallerAnswerMode
}

// ResolveFt8AutoWorkCallers reports whether an auto-work-callers run may be armed
// (ADR 0059): the knob must be on AND the answerer-selection mode must be one of the
// two AUTO modes.
//
// operator_pick is excluded deliberately rather than treated as auto_first. It
// promises the operator chooses the station, so a run under it would either pick
// nobody or contradict the promise — and reporting a run as armed when it cannot
// select is the false-advertisement failure invariant 7 exists to prevent.
func ResolveFt8AutoWorkCallers(c *Ft8TXConfig) bool {
	if c == nil || !c.AutoWorkCallers {
		return false
	}
	switch ResolveFt8CallerAnswerMode(c) {
	case Ft8CallerAnswerAutoFirst, Ft8CallerAnswerAutoStrongest:
		return true
	default:
		return false
	}
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

// Ft8MeterConfig is the stored (sparse) form of the TX-drive display
// threshold (ADR 0064), raw rig 0-255 ALC scale: alc_amber is where green
// (healthy drive) ends and amber begins. ONE threshold: the red band was
// folded into amber 2026-08-08 (see DefaultFt8AlcAmber); a legacy alc_red
// key in an older config.json is silently ignored by decode.
type Ft8MeterConfig struct {
	AlcAmber *int `json:"alc_amber,omitempty"`
}

// Ft8MeterLevels is the resolved form served on /v1/config.
type Ft8MeterLevels struct {
	AlcAmber int `json:"alc_amber"`
}

// DefaultFt8AlcAmber is the amber floor — operator-RATIFIED 2026-08-07 from
// the first live FT8 TX data: healthy drive measured ALC 15–18 (min 15, max
// 18, every slot; low-power slots 7–12; voice datum 26) with PO flat at
// target, so green must cover the healthy band — a zero-only green could
// never show during a correct transmission, and amber nagged toward reducing
// audio that was already right. 30 clears every healthy datum with headroom.
//
// Amber is the TERMINAL state (operator-ratified 2026-08-08): the §4
// deliberate-overdrive run measured the RM ALC answer SATURATING at ~30 of
// 255 while the front-panel needle sat +20 dB over the zone and in-band PO
// collapsed 121→35 (internal/bridge/meters.go carries the measurement), so
// an ALC reading of ~30 means AT LEAST zone-edge drive and no ALC-only
// threshold above it can ever fire. The provisional alc_red (50) was
// therefore unreachable and was removed rather than documented dead; a
// distinct overdrive state would need ALC-at-ceiling paired with collapsed
// PO (captured in docs/dogfood-inbox.md, not built).
const DefaultFt8AlcAmber = 30

// ResolveFt8Meter applies the default to a sparse (or absent) meter block,
// clamping the threshold to the rig's usable 1-255 scale (0 would flag
// every reading).
func ResolveFt8Meter(c *Ft8MeterConfig) Ft8MeterLevels {
	r := Ft8MeterLevels{AlcAmber: DefaultFt8AlcAmber}
	if c != nil && c.AlcAmber != nil {
		r.AlcAmber = *c.AlcAmber
	}
	if r.AlcAmber < 1 {
		r.AlcAmber = 1
	}
	if r.AlcAmber > 255 {
		r.AlcAmber = 255
	}
	return r
}
