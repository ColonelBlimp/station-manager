package qsoservice

import "testing"

// The daemon's SNR-report set mirrors the SPA's usesSignalReport, filed as ADIF
// 3.1.5 does: main modes by MODE, the MFSK family by SUBMODE.
func TestSnrReportMode_MirrorsTheSpaSet(t *testing.T) {
	for _, m := range []string{"FT8", "JT65", "JT9", "JT4", "JT6M", "JT44", "QRA64", "MSK144", "FSK441", "ISCAT", "WSPR"} {
		if !snrReportMode(m, "") {
			t.Fatalf("%s: want SNR report", m)
		}
	}
	for _, s := range []string{"FT4", "FST4", "FST4W", "JS8", "Q65", "JTMS"} {
		if !snrReportMode("MFSK", s) {
			t.Fatalf("MFSK/%s: want SNR report", s)
		}
		if snrReportMode(s, "") {
			t.Fatalf("bare %s: not a mode — the pair is canonicalised before this rule", s)
		}
	}
	for _, pair := range [][2]string{{"MFSK", ""}, {"MFSK", "MFSK16"}, {"PSK", "PSK31"}, {"SSB", "USB"}, {"RTTY", ""}, {"CW", ""}} {
		if snrReportMode(pair[0], pair[1]) {
			t.Fatalf("%s/%s: want RST", pair[0], pair[1])
		}
	}
}
