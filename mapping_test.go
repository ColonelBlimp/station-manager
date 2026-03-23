package adif

import (
	"strings"
	"testing"

	"github.com/Station-Manager/types"
)

func Test_QsoToRecord_And_RenderIncludesQslAndMisc(t *testing.T) {
	q := types.Qso{
		QsoDetails: types.QsoDetails{
			Freq:    "7050000",
			QsoDate: "20250508",
			TimeOn:  "084500",
		},
		ContactedStation: types.ContactedStation{Call: "M0CMC"},
		LoggingStation:   types.LoggingStation{StationCallsign: "7Q5MLV/T"},
		Qsl: types.Qsl{
			QslMsg:   "TNX QSO",
			QslRDate: "2025-05-09",
			QslSDate: "2025-05-08",
			QslRcvd:  "Y",
			QslSent:  "Y",
			QslVia:   "B",
		},
		//Misc: types.Misc{
		QrzComUploadStatus:  "Y",
		QrzComUploadDate:    "20250508010101",
		SmQsoUploadStatus:   "Y",
		SmQsoUploadDate:     "20250508010101",
		SmFwrdByEmailStatus: "N",
		SmFwrdByEmailDate:   "",
		//		QslWanted:             "Y",
		//},
	}

	rec := QsoToRecord(q)
	adif := rec.String()

	// Check some expected tags exist
	mustContain := []string{
		"<FREQ:5>7.050",
		"<CALL:5>M0CMC",
		"<QSLMSG:7>TNX QSO",
		"<QSLRDATE:8>20250509",
		"<QSL_SENT:1>Y",
		"<QSL_VIA:1>B",
		"<QRZCOM_QSO_UPLOAD_STATUS:1>Y",
		"<SM_QSO_UPLOAD_STATUS:1>Y",
		//		"<QSL_WANTED:1>Y",
	}
	for _, s := range mustContain {
		if !strings.Contains(adif, s) {
			t.Fatalf("ADIF missing expected segment: %s\nGot:\n%s", s, adif)
		}
	}
}
