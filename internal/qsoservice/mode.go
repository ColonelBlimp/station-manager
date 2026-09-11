package qsoservice

// The modes whose signal reports are a signed dB SNR rather than RST — the
// WSJT-X family. Their RST fields carry the exchanged dB report or stay empty
// on a bare-roger contact, so the RST requirement and the "59" default do not
// apply. The set is the SPA's usesSignalReport (lib/utils/mode.ts) split the
// way ADIF 3.1.5 files it: FT8 and the JT family are main modes; FT4, FST4,
// FST4W, JS8, Q65 and JTMS are submodes of MFSK, so they arrive as that pair —
// a bare MODE=FT4 is canonicalised to it before this rule applies, never
// refused (codex 8701a6de P2). Literals because qsoservice must not import
// internal/ft8; a mode outside both sets (PSK31, MFSK16, RTTY) keeps RST.
var (
	snrReportMainModes = map[string]struct{}{
		"FT8": {}, "JT65": {}, "JT9": {}, "JT4": {}, "JT6M": {}, "JT44": {},
		"QRA64": {}, "MSK144": {}, "FSK441": {}, "ISCAT": {}, "WSPR": {},
	}
	snrReportMfskSubmodes = map[string]struct{}{
		"FT4": {}, "FST4": {}, "FST4W": {}, "JS8": {}, "Q65": {}, "JTMS": {},
	}
)

// snrReportMode reports whether a QSO's signal reports are SNR in dB rather
// than RST. Inputs are the canonical (upper-cased) pair.
func snrReportMode(mode, submode string) bool {
	if _, ok := snrReportMainModes[mode]; ok {
		return true
	}
	if mode != "MFSK" {
		return false
	}
	_, ok := snrReportMfskSubmodes[submode]
	return ok
}
