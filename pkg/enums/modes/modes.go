package modes

import "strings"

type Mode string

const (
	AM           Mode = "AM"
	CW           Mode = "CW"
	FM           Mode = "FM"
	RTTY         Mode = "RTTY"
	SSB          Mode = "SSB"
	DIGITALVOICE Mode = "DIGITALVOICE"
	MFSK         Mode = "MFSK"
	PSK          Mode = "PSK"
	HELL         Mode = "HELL"
	PACKET       Mode = "PACKET"
)

// IsValidMode returns true if s is one of the supported ADIF main modes used by this app.
func IsValidMode(s string) bool {
	switch Mode(strings.ToUpper(strings.TrimSpace(s))) {
	case AM, CW, FM, RTTY, SSB, DIGITALVOICE, MFSK, PSK, HELL, PACKET:
		return true
	default:
		return false
	}
}

type SubMode string

const (
	USB SubMode = "USB"
	LSB SubMode = "LSB"
)

// submodeToMode maps common ADIF submodes to their parent main mode.
var submodeToMode = map[string]Mode{
	// DIGITALVOICE family
	"C4FM":   DIGITALVOICE,
	"DMR":    DIGITALVOICE,
	"DSTAR":  DIGITALVOICE,
	"FREEDV": DIGITALVOICE,
	"M17":    DIGITALVOICE,

	// MFSK family (WSJT-X and other MFSK-based)
	"FT4":       MFSK,
	"FST4":      MFSK,
	"FST4W":     MFSK,
	"Q65":       MFSK,
	"OLIVIA":    MFSK,
	"CONTESTIA": MFSK,
	"DOMINOEX":  MFSK,
	"FSQ":       MFSK,
	"JS8":       MFSK,
	"MFSK16":    MFSK,
	"MFSK8":     MFSK,
	"MT63":      MFSK,
	"THOR":      MFSK,
	"THROB":     MFSK,

	// PSK family
	"PSK10":  PSK,
	"PSK31":  PSK,
	"PSK63":  PSK,
	"PSK125": PSK,
	"QPSK31": PSK,
	"QPSK63": PSK,
	"BPSK31": PSK,
	"BPSK63": PSK,

	// Hellschreiber family
	"HELL80":  HELL,
	"FMHELL":  HELL,
	"FSKHELL": HELL,
	"HFSK":    HELL,
	"HHELL":   HELL,
	"PSKHELL": HELL,

	// Packet family
	"PKT":  PACKET,
	"APRS": PACKET,

	// SSB sidebands as submodes
	"LSB": SSB,
	"USB": SSB,
}

// IsValidSubMode returns true if the string corresponds to a known ADIF submode handled by this app.
func IsValidSubMode(s string) bool {
	_, ok := submodeToMode[strings.ToUpper(strings.TrimSpace(s))]
	return ok
}

// GetModeBySubmode returns the parent main mode for a given ADIF submode.
// The bool return indicates whether a mapping was found.
func GetModeBySubmode(s string) (Mode, bool) {
	m, ok := submodeToMode[strings.ToUpper(strings.TrimSpace(s))]
	return m, ok
}

func (m Mode) String() string    { return string(m) }
func (m SubMode) String() string { return string(m) }
