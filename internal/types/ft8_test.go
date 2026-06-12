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
}
