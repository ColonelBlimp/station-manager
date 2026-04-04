package adif

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestRecord_String(t *testing.T) {
	record := &Record{
		QsoDetails: types.QsoDetails{
			Freq:       "7.050.000",
			Band:       "40m",
			Mode:       "SSB",
			Submode:    "LSB",
			QsoDate:    "2025-05-08",
			QsoDateOff: "2025-05-08",
			TimeOn:     "08:45:00",
			TimeOff:    "08:50:00",
			RstRcvd:    "59",
			RstSent:    "59",
		},
		ContactedStation: types.ContactedStation{
			Call: "M0CMC",
			Name: "Marc L",
		},
		LoggingStation: types.LoggingStation{
			StationCallsign: "7Q5MLV/T",
			MyName:          "Veary",
		},
	}

	out := record.String()

	mustContain := []string{
		EorStr,
		"<CALL:5>M0CMC",
		"<BAND:3>40m",
		"<MODE:3>SSB",
		"<QSO_DATE:8>20250508", // dashes stripped
		"<TIME_ON:6>084500",    // colons stripped
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Fatalf("ADIF output missing expected segment: %s\nGot:\n%s", s, out)
		}
	}
}
