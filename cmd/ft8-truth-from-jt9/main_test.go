package main

import "testing"

// TestParseJT9Line pins the parser against representative lines
// of jt9 -8 stdout, in both the canonical "decode" form and the
// non-decode forms we must skip (header, blank, malformed).
//
// jt9's column widths drift slightly between versions; the parser is
// whitespace-tolerant by design (strings.Fields), so this test
// includes both tight and padded variants.
func TestParseJT9Line(t *testing.T) {
	type want struct {
		ok   bool
		text string
		freq float64
		dt   float64
	}

	cases := []struct {
		name string
		line string
		want want
	}{
		{
			name: "tight-decode",
			line: "000000  2 0.5 650 ~  CQ K1JT FN20",
			want: want{ok: true, text: "CQ K1JT FN20", freq: 650, dt: 0.5},
		},
		{
			name: "padded-decode",
			line: "000000   2  0.5  650 ~  CQ K1JT FN20",
			want: want{ok: true, text: "CQ K1JT FN20", freq: 650, dt: 0.5},
		},
		{
			name: "negative-snr-and-dt",
			line: "133015  -8 -0.4 1824 ~  CQ DX S56GD JN65",
			want: want{ok: true, text: "CQ DX S56GD JN65", freq: 1824, dt: -0.4},
		},
		{
			name: "fractional-snr",
			line: "131500  -8.3  0.1  650 ~  CQ DX S56GD JN65",
			want: want{ok: true, text: "CQ DX S56GD JN65", freq: 650, dt: 0.1},
		},
		{
			name: "blank-line",
			line: "",
			want: want{ok: false},
		},
		{
			name: "decode-finished-footer",
			line: "<DecodeFinished>",
			want: want{ok: false},
		},
		{
			name: "header-no-tilde",
			line: "  UTC dB DT Freq Message",
			want: want{ok: false},
		},
		{
			name: "tilde-but-empty-text",
			line: "000000  2 0.5 650 ~  ",
			want: want{ok: false},
		},
		{
			name: "tilde-but-too-few-header-fields",
			line: "000000 ~  partial",
			want: want{ok: false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig, ok := parseJT9Line(tc.line)
			if ok != tc.want.ok {
				t.Fatalf("ok = %v, want %v (sig=%+v)", ok, tc.want.ok, sig)
			}
			if !ok {
				return
			}
			if sig.Text != tc.want.text {
				t.Errorf("text = %q, want %q", sig.Text, tc.want.text)
			}
			if sig.FreqHz != tc.want.freq {
				t.Errorf("freq = %g, want %g", sig.FreqHz, tc.want.freq)
			}
			if sig.DTSec != tc.want.dt {
				t.Errorf("dt = %g, want %g", sig.DTSec, tc.want.dt)
			}
		})
	}
}

// TestParseJT9Output exercises the full multi-line parse against a
// representative jt9 -8 capture-style stdout.
func TestParseJT9Output(t *testing.T) {
	stdout := `<DecodeFinished>
000000  -1 -0.2 1500 ~  CQ K1JT FN20
000000  -8  0.1  650 ~  CQ DX S56GD JN65
000000 -12  0.3 1824 ~  OH6IH IU7KEG JN81
<DecodeFinished>
`
	signals := parseJT9Output([]byte(stdout))
	if len(signals) != 3 {
		t.Fatalf("got %d signals, want 3", len(signals))
	}
	if signals[0].Text != "CQ K1JT FN20" {
		t.Errorf("signals[0].Text = %q, want %q", signals[0].Text, "CQ K1JT FN20")
	}
	if signals[2].FreqHz != 1824 {
		t.Errorf("signals[2].FreqHz = %g, want 1824", signals[2].FreqHz)
	}
}
