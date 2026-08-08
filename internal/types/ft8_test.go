package types

import (
	"encoding/json"
	"testing"
)

func TestResolveFt8Display(t *testing.T) {
	t.Run("nil yields all defaults", func(t *testing.T) {
		d := ResolveFt8Display(nil)
		if d.HistoryMax != DefaultFt8HistoryMax || d.FeedMode != DefaultFt8FeedMode ||
			d.HighlightUnworked != DefaultFt8HighlightUnworked || d.HighlightWorked != DefaultFt8HighlightWorked {
			t.Fatalf("nil override = %+v, want defaults", d)
		}
	})

	t.Run("zero/empty fields fall back per-field", func(t *testing.T) {
		d := ResolveFt8Display(&Ft8DisplayConfig{FeedMode: "single"})
		if d.FeedMode != "single" {
			t.Errorf("FeedMode = %q, want single", d.FeedMode)
		}
		if d.HistoryMax != DefaultFt8HistoryMax {
			t.Errorf("HistoryMax = %d, want default %d (unset)", d.HistoryMax, DefaultFt8HistoryMax)
		}
		if d.HighlightUnworked != DefaultFt8HighlightUnworked {
			t.Errorf("HighlightUnworked = %q, want default (unset)", d.HighlightUnworked)
		}
	})

	t.Run("history max clamps but unset keeps default", func(t *testing.T) {
		if d := ResolveFt8Display(&Ft8DisplayConfig{HistoryMax: 0}); d.HistoryMax != DefaultFt8HistoryMax {
			t.Errorf("HistoryMax 0 (unset) = %d, want default %d", d.HistoryMax, DefaultFt8HistoryMax)
		}
		if d := ResolveFt8Display(&Ft8DisplayConfig{HistoryMax: 5}); d.HistoryMax != 10 {
			t.Errorf("HistoryMax 5 = %d, want clamp to 10", d.HistoryMax)
		}
		if d := ResolveFt8Display(&Ft8DisplayConfig{HistoryMax: 99999}); d.HistoryMax != 2000 {
			t.Errorf("HistoryMax 99999 = %d, want clamp to 2000", d.HistoryMax)
		}
		if d := ResolveFt8Display(&Ft8DisplayConfig{HistoryMax: 250}); d.HistoryMax != 250 {
			t.Errorf("HistoryMax 250 = %d, want 250", d.HistoryMax)
		}
	})

	t.Run("invalid feed mode falls back to default", func(t *testing.T) {
		if d := ResolveFt8Display(&Ft8DisplayConfig{FeedMode: "bogus"}); d.FeedMode != DefaultFt8FeedMode {
			t.Errorf("FeedMode bogus = %q, want default %q", d.FeedMode, DefaultFt8FeedMode)
		}
	})

	t.Run("colours pass through when set", func(t *testing.T) {
		d := ResolveFt8Display(&Ft8DisplayConfig{HighlightUnworked: "#abcdef", HighlightWorked: "#123456"})
		if d.HighlightUnworked != "#abcdef" || d.HighlightWorked != "#123456" {
			t.Fatalf("colours = (%q, %q), want passthrough", d.HighlightUnworked, d.HighlightWorked)
		}
	})

	t.Run("cq_to_top defaults false and passes through", func(t *testing.T) {
		if ResolveFt8Display(nil).CqToTop {
			t.Error("CqToTop should default false")
		}
		if !ResolveFt8Display(&Ft8DisplayConfig{CqToTop: true}).CqToTop {
			t.Error("CqToTop=true should pass through")
		}
	})
}

func TestFt8FeedModeValid(t *testing.T) {
	for _, ok := range []string{"accumulate", "single"} {
		if !Ft8FeedModeValid(ok) {
			t.Errorf("Ft8FeedModeValid(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "Accumulate", "rolling", "off"} {
		if Ft8FeedModeValid(bad) {
			t.Errorf("Ft8FeedModeValid(%q) = true, want false", bad)
		}
	}
}

// The resolve default is operator_pick (operator-ratified 2026-08-08,
// superseding ADR 0033's auto_first): automatic operation has licensing
// implications in many jurisdictions, so a station whose operator never
// CHOSE an auto mode must not auto-work anyone — a clean install, an absent
// key, and an invalid literal all fail toward the NON-automatic mode. The
// expectations below are hardcoded literals, not the Default const, so a
// regression of the const cannot re-derive its own expectation.
func TestResolveFt8CallerAnswerMode(t *testing.T) {
	cases := []struct {
		name string
		in   *Ft8TXConfig
		want string
	}{
		{"nil TX block → operator_pick", nil, "operator_pick"},
		{"empty → operator_pick", &Ft8TXConfig{}, "operator_pick"},
		{"invalid → operator_pick, never an auto mode", &Ft8TXConfig{CallerAnswerMode: "bogus"}, "operator_pick"},
		{"auto_first honoured", &Ft8TXConfig{CallerAnswerMode: "auto_first"}, Ft8CallerAnswerAutoFirst},
		{"auto_strongest honoured", &Ft8TXConfig{CallerAnswerMode: "auto_strongest"}, Ft8CallerAnswerAutoStrongest},
		{"operator_pick honoured", &Ft8TXConfig{CallerAnswerMode: "operator_pick"}, Ft8CallerAnswerOperatorPick},
	}
	for _, c := range cases {
		if got := ResolveFt8CallerAnswerMode(c.in); got != c.want {
			t.Errorf("%s: ResolveFt8CallerAnswerMode = %q, want %q", c.name, got, c.want)
		}
	}
	if DefaultFt8CallerAnswerMode != Ft8CallerAnswerOperatorPick {
		t.Errorf("default = %q, want operator_pick", DefaultFt8CallerAnswerMode)
	}
	for _, m := range []string{Ft8CallerAnswerAutoFirst, Ft8CallerAnswerAutoStrongest, Ft8CallerAnswerOperatorPick} {
		if !Ft8CallerAnswerModeValid(m) {
			t.Errorf("Ft8CallerAnswerModeValid(%q) = false, want true", m)
		}
	}
	if Ft8CallerAnswerModeValid("bogus") {
		t.Error("Ft8CallerAnswerModeValid(bogus) = true, want false")
	}
}

// ResolveFt8AutoWorkCallers and the auto_work_callers knob retired with ADR
// 0067 — the licensing rule they enforced ("no automation the operator never
// chose") now rests entirely on the mode default: an absent/invalid mode
// resolves operator_pick (pinned above), and the arming gate reads the
// SESSION's mode alone (internal/ft8/adr0067_test.go A1-A3 — pick arms only a
// LISTING run that transmits nothing without a pop).

func TestFt8FieldDayClassValid(t *testing.T) {
	valid := []string{"1A", "2A", "5F", "1D", "10A", "99F"}
	for _, s := range valid {
		if !Ft8FieldDayClassValid(s) {
			t.Errorf("Ft8FieldDayClassValid(%q) = false, want true", s)
		}
	}
	// empty, no count, no category, leading zero, bad category, lowercase, 3 digits,
	// trailing junk, padding.
	invalid := []string{"", "A", "1", "0A", "1G", "2a", "100A", "1AA", "A1", " 1A", "1A "}
	for _, s := range invalid {
		if Ft8FieldDayClassValid(s) {
			t.Errorf("Ft8FieldDayClassValid(%q) = true, want false", s)
		}
	}
}

func TestResolveFt8MaxRepeats(t *testing.T) {
	cases := []struct {
		name string
		in   *Ft8TXConfig
		want int
	}{
		{"nil TX block → default", nil, DefaultFt8MaxRepeats},
		{"unset (0) → default", &Ft8TXConfig{}, DefaultFt8MaxRepeats},
		{"negative → default", &Ft8TXConfig{MaxRepeats: -3}, DefaultFt8MaxRepeats},
		{"in-range honoured", &Ft8TXConfig{MaxRepeats: 4}, 4},
		{"at ceiling honoured", &Ft8TXConfig{MaxRepeats: Ft8MaxRepeatsCeiling}, Ft8MaxRepeatsCeiling},
		{"above ceiling clamped", &Ft8TXConfig{MaxRepeats: 99}, Ft8MaxRepeatsCeiling},
	}
	for _, c := range cases {
		if got := ResolveFt8MaxRepeats(c.in); got != c.want {
			t.Errorf("%s: ResolveFt8MaxRepeats = %d, want %d", c.name, got, c.want)
		}
	}
	if Ft8MaxRepeatsCeiling != 10 {
		t.Errorf("ceiling = %d, want 10", Ft8MaxRepeatsCeiling)
	}
}

func TestResolveFt8Frequencies(t *testing.T) {
	// nil → the WSJT-X defaults, unchanged.
	d := ResolveFt8Frequencies(nil)
	if d["20m"] != 14_074_000 || d["6m"] != 50_313_000 || d["60m"] != 5_357_000 {
		t.Fatalf("nil override = %v, want WSJT-X defaults", d)
	}
	if len(d) != len(DefaultFt8Frequencies()) {
		t.Errorf("nil override len = %d, want %d", len(d), len(DefaultFt8Frequencies()))
	}
	// Positive override replaces; non-positive is ignored; unknown band is added.
	got := ResolveFt8Frequencies(map[string]int{"20m": 14_074_500, "40m": 0, "4m": 70_154_000})
	if got["20m"] != 14_074_500 {
		t.Errorf("20m override = %d, want 14074500", got["20m"])
	}
	if got["40m"] != 7_074_000 {
		t.Errorf("40m (zero override ignored) = %d, want default 7074000", got["40m"])
	}
	if got["4m"] != 70_154_000 {
		t.Errorf("4m (added) = %d, want 70154000", got["4m"])
	}
	// Returns a fresh map — mutating it must not leak into the package defaults.
	got["20m"] = 1
	if DefaultFt8Frequencies()["20m"] != 14_074_000 {
		t.Error("ResolveFt8Frequencies leaked into the package defaults")
	}
}

// ResolveFt8InhibitIdle's contract, with the nil-BLOCK case first because that is
// the one an implementation is most likely to get wrong and the one that bit here:
// Config.ActiveFt8() leaves Ft8Config.TX nil when the operator has no ft8.tx block,
// no ft8.tx.mode and no rig TX-audio device, so a caller that reads the field off
// the block cannot see the default at all. Defaulting must survive an ABSENT block,
// not merely an absent field — otherwise the documented "on unless you say
// otherwise" silently becomes "off" for a minimal config, and the operator only
// discovers it when their machine blanks mid-transmission.
func TestResolveFt8InhibitIdle(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		in   *Ft8TXConfig
		want bool
	}{
		{"nil TX block → default on", nil, true},
		{"block present, field unset → default on", &Ft8TXConfig{}, true},
		{"explicit true honoured", &Ft8TXConfig{InhibitIdle: &yes}, true},
		{"explicit false honoured", &Ft8TXConfig{InhibitIdle: &no}, false},
	}
	for _, c := range cases {
		if got := ResolveFt8InhibitIdle(c.in); got != c.want {
			t.Errorf("%s: ResolveFt8InhibitIdle = %v, want %v", c.name, got, c.want)
		}
	}
}

// ResolveFt8Audio — the RX level meter's classification thresholds (dogfood
// 2026-08-06). The daemon publishes measurements; the SPA classifies against
// THESE, served resolved on /v1/config, so an unset config still yields a
// working meter and an operator can calibrate by editing config.json.
// Defaults are the pre-hardware-calibration WSJT-X-convention window and are
// expected to be tuned against the PCM2903C — they are starting points, not
// findings.
func TestResolveFt8Audio(t *testing.T) {
	low, high := -70.0, -20.0
	cases := []struct {
		name     string
		in       *Ft8AudioConfig
		wantLow  float64
		wantHigh float64
	}{
		{"nil block → defaults", nil, DefaultFt8AudioLowDbfs, DefaultFt8AudioHighDbfs},
		{"empty block → defaults", &Ft8AudioConfig{}, DefaultFt8AudioLowDbfs, DefaultFt8AudioHighDbfs},
		{"both set → honoured", &Ft8AudioConfig{LowDbfs: &low, HighDbfs: &high}, -70, -20},
		{"one set → other defaults", &Ft8AudioConfig{LowDbfs: &low}, -70, DefaultFt8AudioHighDbfs},
	}
	for _, c := range cases {
		got := ResolveFt8Audio(c.in)
		if got.LowDbfs != c.wantLow || got.HighDbfs != c.wantHigh {
			t.Errorf("%s: ResolveFt8Audio = (%v, %v), want (%v, %v)",
				c.name, got.LowDbfs, got.HighDbfs, c.wantLow, c.wantHigh)
		}
	}
}

// ResolveFt8Meter — the TX-drive (ALC) display threshold (ADR 0064). ONE
// threshold as of 2026-08-08: the amber floor (ft8.meter.alc_amber, ratified
// 30 on 2026-08-07 — green is the HEALTHY band: live FT8 measured 15–18,
// low-power 7–12, voice 26). The red band was FOLDED INTO AMBER
// (operator-ratified 2026-08-08) after the §4 deliberate-overdrive run
// measured the RM ALC answer SATURATING at ~30 of 255 while the front-panel
// needle sat +20 dB over the zone: no ALC-only threshold above ~30 can ever
// fire, so amber is the terminal "reduce drive" state and alc_red was
// removed. Rules: nil/sparse → default, explicit value honoured, clamp to
// the usable 1–255 scale (0 would flag every reading; the old amber>red
// cross-clamp died with the red line — 999 now clamps to 255, not to 50).
// The nearest confusable regression: a resolver still carrying alc_red in
// its served JSON, which the SPA would dutifully render as a phantom red.
func TestResolveFt8Meter(t *testing.T) {
	iv := func(n int) *int { return &n }
	cases := []struct {
		name string
		in   *Ft8MeterConfig
		want int
	}{
		{"nil block → ratified default", nil, DefaultFt8AlcAmber},
		{"empty block → ratified default", &Ft8MeterConfig{}, DefaultFt8AlcAmber},
		{"explicit value honoured", &Ft8MeterConfig{AlcAmber: iv(20)}, 20},
		{"zero clamps to 1", &Ft8MeterConfig{AlcAmber: iv(0)}, 1},
		{"over-scale clamps to 255 — no red line to cross-clamp to",
			&Ft8MeterConfig{AlcAmber: iv(999)}, 255},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveFt8Meter(c.in); got.AlcAmber != c.want {
				t.Fatalf("ResolveFt8Meter(%+v).AlcAmber = %d, want %d", c.in, got.AlcAmber, c.want)
			}
		})
	}
}

// The served shape carries ONLY alc_amber. A resolved payload still holding
// alc_red would hand the SPA a phantom red band back; the wire shape is the
// contract the fold has to hold at.
func TestResolveFt8Meter_ServedShapeHasNoRed(t *testing.T) {
	b, err := json.Marshal(ResolveFt8Meter(nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(b), `{"alc_amber":30}`; got != want {
		t.Fatalf("served ft8_meter shape = %s, want %s", got, want)
	}
}

// A config.json written before the fold may still hold ft8.meter.alc_red.
// It must be IGNORED — tolerated by decode (no strict field check) and
// absent from resolution — not an error that stops the daemon loading its
// own previous config.
func TestResolveFt8Meter_LegacyRedKeyIgnored(t *testing.T) {
	var c Ft8MeterConfig
	if err := json.Unmarshal([]byte(`{"alc_amber":40,"alc_red":50}`), &c); err != nil {
		t.Fatalf("legacy alc_red must not break decoding: %v", err)
	}
	if got := ResolveFt8Meter(&c); got.AlcAmber != 40 {
		t.Fatalf("AlcAmber = %d, want 40", got.AlcAmber)
	}
}
