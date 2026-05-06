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

// TestQsoToRecord_EmitsAppSmQsoID pins ADR 0016 phase 2: when a QSO
// has a UUID, the daemon's ADIF emission carries it as
// APP_SM_QSO_ID so re-imports and forwarder uploads round-trip the
// canonical external identifier.
func TestQsoToRecord_EmitsAppSmQsoID(t *testing.T) {
	uuid := "01910d3a-7000-7abc-8def-0123456789ab"
	q := types.Qso{
		UUID: uuid,
		QsoDetails: types.QsoDetails{
			Band: "40m", Mode: "SSB", Freq: "7.050",
			QsoDate: "20250508", TimeOn: "0845", TimeOff: "0850",
			RstSent: "59", RstRcvd: "59",
		},
		ContactedStation: types.ContactedStation{Call: "M0CMC", Country: "England"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G4ABC"},
	}

	out := ConvertQsoToAdifNoHeader(q)
	want := "<APP_SM_QSO_ID:36>" + uuid
	if !strings.Contains(out, want) {
		t.Fatalf("ADIF output missing %q\nGot:\n%s", want, out)
	}
}

// TestQsoToRecord_OmitsAppSmQsoIDWhenEmpty pins the ,omitempty
// behaviour: a QSO with no UUID does not emit an empty
// APP_SM_QSO_ID tag.
func TestQsoToRecord_OmitsAppSmQsoIDWhenEmpty(t *testing.T) {
	q := types.Qso{
		QsoDetails: types.QsoDetails{
			Band: "40m", Mode: "SSB", Freq: "7.050",
			QsoDate: "20250508", TimeOn: "0845", TimeOff: "0850",
			RstSent: "59", RstRcvd: "59",
		},
		ContactedStation: types.ContactedStation{Call: "M0CMC", Country: "England"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G4ABC"},
	}

	out := ConvertQsoToAdifNoHeader(q)
	if strings.Contains(out, "APP_SM_QSO_ID") {
		t.Fatalf("ADIF output should omit APP_SM_QSO_ID when UUID is empty\nGot:\n%s", out)
	}
}
