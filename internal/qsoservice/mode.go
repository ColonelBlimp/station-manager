package qsoservice

// snrReportMode reports whether a QSO's signal reports are SNR in dB rather
// than RST — the FT-family modes the FT8 subsystem logs. Their RST fields
// carry the exchanged dB report or stay empty on a bare-roger contact, so the
// RST requirement and the "59" default do not apply. FT8 is an ADIF mode; FT4
// is a submode of MFSK (ADIF 3.1.5), logged as MODE=MFSK SUBMODE=FT4, and it
// reaches here only as that pair — a bare MODE=FT4 is refused by the mode
// catalogue before the report rule applies. Mirrors the SPA's
// usesSignalReport; literals because qsoservice must not import internal/ft8.
func snrReportMode(mode, submode string) bool {
	switch mode {
	case "FT8":
		return true
	case "MFSK":
		return submode == "FT4"
	}
	return false
}
