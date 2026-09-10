package qsoservice

// snrReportMode reports whether mode's signal reports are SNR in dB rather
// than RST — the FT-family modes the FT8 subsystem logs (FT8, and FT4 since
// ADR 0080). Their RST fields carry the exchanged dB report or stay empty on a
// bare-roger contact, so the RST requirement and the "59" default do not
// apply. Mirrors the SPA's usesSignalReport; kept as literals here because
// qsoservice must not import internal/ft8.
func snrReportMode(mode string) bool {
	switch mode {
	case "FT8", "FT4":
		return true
	}
	return false
}
