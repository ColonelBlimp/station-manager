package types

import "testing"

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

func TestResolveFt8CallerAnswerMode(t *testing.T) {
	cases := []struct {
		name string
		in   *Ft8TXConfig
		want string
	}{
		{"nil TX block → default", nil, DefaultFt8CallerAnswerMode},
		{"empty → default", &Ft8TXConfig{}, Ft8CallerAnswerAutoFirst},
		{"invalid → default", &Ft8TXConfig{CallerAnswerMode: "bogus"}, Ft8CallerAnswerAutoFirst},
		{"auto_first honoured", &Ft8TXConfig{CallerAnswerMode: "auto_first"}, Ft8CallerAnswerAutoFirst},
		{"auto_strongest honoured", &Ft8TXConfig{CallerAnswerMode: "auto_strongest"}, Ft8CallerAnswerAutoStrongest},
		{"operator_pick honoured", &Ft8TXConfig{CallerAnswerMode: "operator_pick"}, Ft8CallerAnswerOperatorPick},
	}
	for _, c := range cases {
		if got := ResolveFt8CallerAnswerMode(c.in); got != c.want {
			t.Errorf("%s: ResolveFt8CallerAnswerMode = %q, want %q", c.name, got, c.want)
		}
	}
	if DefaultFt8CallerAnswerMode != Ft8CallerAnswerAutoFirst {
		t.Errorf("default = %q, want auto_first", DefaultFt8CallerAnswerMode)
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
