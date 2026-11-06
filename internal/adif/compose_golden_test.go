package adif

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// updateComposeGolden rewrites testdata/compose_golden.adi from the current
// emitter. Use it only to capture a deliberate, reviewed output change.
var updateComposeGolden = flag.Bool("update-compose-golden", false, "rewrite testdata/compose_golden.adi")

// goldenQsos exercises every emitter special case the session-email mark relies
// on (W-0010 outcome 11): FREQ/FREQ_RX Hz→MHz, dash-free dates, colon-free
// times, empty fields omitted, the adif-tag (Record's own sections) and json-tag
// fallback (the embedded types sections), a non-string field skipped (CSID), a
// non-numeric DXCC dropped, the bool→"Y" APP_SM_REQUEST_QSL mapping, decimal
// lat/lon converted to the ADIF Location type, and several records in order.
func goldenQsos() types.QsoSlice {
	full := types.Qso{
		UUID:                "0190a1b2-c3d4-7e5f-8a9b-000000000001",
		AppSmRunID:          "0190a1b2-c3d4-7e5f-8a9b-0000000000aa",
		AppSmRequestQsl:     true,
		ClubLogUploadDate:   "20261106",
		ClubLogUploadStatus: "Y",
		QrzComUploadDate:    "20261106",
		QrzComUploadStatus:  "Y",
		SmQsoUploadDate:     "20261106",
		SmQsoUploadStatus:   "Y",
		SmFwrdByEmailDate:   "20261105",
		SmFwrdByEmailStatus: "Y",
		QrzlogLogid:         "123456789",
		QsoDetails: types.QsoDetails{
			Band: "20m", Freq: "14074000", FreqRx: "14076000", Mode: "FT8",
			QsoDate: "2026-11-06", QsoDateOff: "2026-11-06",
			TimeOn: "06:58:46", TimeOff: "06:59:30",
			RstSent: "-10", RstRcvd: "-12", Distance: "6789",
		},
		ContactedStation: types.ContactedStation{
			CSID: 42, Call: "TJ1GD", Name: "Ann <x>", Country: "Cameroon", DXCC: "406",
			CQZ: "36", ITUZ: "47", Cont: "AF", Gridsquare: "JJ43", Lat: "4.5", Lon: "-9.25",
		},
		LoggingStation: types.LoggingStation{
			MyGridsquare: "KI93", MyLat: "-3.375", MyLon: "36.68",
		},
		Qsl: types.Qsl{QslSent: "Y", QslSendVia: "B", QslVia: "BURO", QslSDate: "2026-11-06"},
	}
	invalidDxcc := types.Qso{
		UUID: "0190a1b2-c3d4-7e5f-8a9b-000000000002",
		QsoDetails: types.QsoDetails{
			Band: "40m", Freq: "7050000", Mode: "SSB",
			QsoDate: "2026-11-05", TimeOn: "18:00:00",
		},
		ContactedStation: types.ContactedStation{Call: "G4ABC", DXCC: "G"},
		Qsl:              types.Qsl{QslRDate: "2026-10-01", QslRcvd: "Y"},
	}
	minimal := types.Qso{
		QsoDetails: types.QsoDetails{
			Band: "2m", Mode: "FM", QsoDate: "2026-11-04", TimeOn: "07:15",
		},
		ContactedStation: types.ContactedStation{Call: "M0CMC"},
	}
	return types.QsoSlice{full, invalidDxcc, minimal}
}

const goldenTimestamp = "20261106123456"

var createdTimestampRe = regexp.MustCompile(`<CREATED_TIMESTAMP:14>\d{14}`)

// TestCompose_GoldenByteIdentity pins the composed attachment byte for byte with
// a FIXED header timestamp, so the emitter refactor behind the session-email
// content comparison (W-0010 outcome 11) cannot change what is sent.
func TestCompose_GoldenByteIdentity(t *testing.T) {
	qsos := goldenQsos()
	recs := make([]Record, 0, len(qsos))
	for _, q := range qsos {
		recs = append(recs, QsoToRecord(q))
	}
	got := (&Adif{HeaderSection: HeaderSection{CreatedTimestamp: goldenTimestamp}, Records: recs}).String()

	path := filepath.Join("testdata", "compose_golden.adi")
	if *updateComposeGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Fatalf("composed ADIF differs from the golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	// ComposeToAdifString is the production entry point; it stamps time.Now() in
	// the header, so pin it to the same bytes once that one value is fixed.
	composed, err := ComposeToAdifString(qsos)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if n := len(createdTimestampRe.FindAllString(composed, -1)); n != 1 {
		t.Fatalf("composed output carries %d CREATED_TIMESTAMP fields, want 1", n)
	}
	fixed := createdTimestampRe.ReplaceAllString(composed, "<CREATED_TIMESTAMP:14>"+goldenTimestamp)
	if fixed != string(want) {
		t.Fatalf("ComposeToAdifString differs from the golden beyond its timestamp\n--- got ---\n%s", fixed)
	}
}
