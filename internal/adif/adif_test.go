package adif

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestRecordToQso_RestoresUUIDAndQslSentVia pins that RecordToQso is a faithful
// inverse of QsoToRecord for the two fields the review found dropped: UUID
// (from APP_SM_QSO_ID, H1) and QslSendVia (from QSL_SENT_VIA, M1).
func TestRecordToQso_RestoresUUIDAndQslSentVia(t *testing.T) {
	rec := Record{}
	rec.Call = "M0CMC"
	rec.AppSmQsoID = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	rec.QslSection.QslSentVia = "B"

	q := RecordToQso(rec, 7)
	if q.UUID != "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b" {
		t.Errorf("UUID = %q, want it restored from AppSmQsoID", q.UUID)
	}
	if q.Qsl.QslSendVia != "B" {
		t.Errorf("QslSendVia = %q, want B (restored from QSL_SENT_VIA)", q.Qsl.QslSendVia)
	}
	if q.LogbookID != 7 {
		t.Errorf("LogbookID = %d, want 7", q.LogbookID)
	}
}

// TestQsoRecordRoundTrip_PreservesUUIDAndQslSentVia proves the converter
// asymmetry is gone: QsoToRecord → RecordToQso is an identity for UUID and
// QslSendVia (both of which the emitter writes).
func TestQsoRecordRoundTrip_PreservesUUIDAndQslSentVia(t *testing.T) {
	q := types.Qso{
		UUID:      "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
		LogbookID: 3,
	}
	q.Call = "M0CMC"
	q.Qsl.QslSendVia = "B"

	back := RecordToQso(QsoToRecord(q), 3)
	if back.UUID != q.UUID {
		t.Errorf("UUID round-trip: got %q, want %q", back.UUID, q.UUID)
	}
	if back.Qsl.QslSendVia != q.Qsl.QslSendVia {
		t.Errorf("QslSendVia round-trip: got %q, want %q", back.Qsl.QslSendVia, q.Qsl.QslSendVia)
	}
}

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

// TestQsoToRecord_EmitsAppSmRequestQsl pins the request-QSL flag's
// emission contract: when q.AppSmRequestQsl is true the daemon emits
// <APP_SM_REQUEST_QSL:1>Y. The bool ↔ "Y" conversion happens at the
// Qso/Record boundary so the rest of the pipeline can keep working
// with the bool semantic.
func TestQsoToRecord_EmitsAppSmRequestQsl(t *testing.T) {
	q := types.Qso{
		AppSmRequestQsl: true,
		QsoDetails: types.QsoDetails{
			Band: "40m", Mode: "SSB", Freq: "7.050",
			QsoDate: "20250508", TimeOn: "0845", TimeOff: "0850",
			RstSent: "59", RstRcvd: "59",
		},
		ContactedStation: types.ContactedStation{Call: "M0CMC", Country: "England"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G4ABC"},
	}

	out := ConvertQsoToAdifNoHeader(q)
	want := "<APP_SM_REQUEST_QSL:1>Y"
	if !strings.Contains(out, want) {
		t.Fatalf("ADIF output missing %q\nGot:\n%s", want, out)
	}
}

// TestQsoToRecord_OmitsAppSmRequestQslWhenFalse pins the omit-when-
// false rule: the SPA only emits APP_SM_REQUEST_QSL when the operator
// affirmatively flagged it. Round-tripping false → empty string →
// omitempty drops the field on the way out.
func TestQsoToRecord_OmitsAppSmRequestQslWhenFalse(t *testing.T) {
	q := types.Qso{
		AppSmRequestQsl: false,
		QsoDetails: types.QsoDetails{
			Band: "40m", Mode: "SSB", Freq: "7.050",
			QsoDate: "20250508", TimeOn: "0845", TimeOff: "0850",
			RstSent: "59", RstRcvd: "59",
		},
		ContactedStation: types.ContactedStation{Call: "M0CMC", Country: "England"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G4ABC"},
	}

	out := ConvertQsoToAdifNoHeader(q)
	if strings.Contains(out, "APP_SM_REQUEST_QSL") {
		t.Fatalf("ADIF output should omit APP_SM_REQUEST_QSL when false\nGot:\n%s", out)
	}
}

// TestRecordToQso_ParsesAppSmRequestQsl pins the parser side. The
// SPA emits <APP_SM_REQUEST_QSL:1>Y when the operator has flagged
// the QSO; the daemon must surface that as q.AppSmRequestQsl=true so
// the value reaches storage and the SessionPanel edit overlay sees
// the correct initial state.
func TestRecordToQso_ParsesAppSmRequestQsl(t *testing.T) {
	body := []byte(
		"<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050" +
			"<QSO_DATE:8>20250508<TIME_ON:6>084500<TIME_OFF:6>085000" +
			"<RST_SENT:2>59<RST_RCVD:2>59<COUNTRY:7>England" +
			"<STATION_CALLSIGN:5>G4ABC<APP_SM_REQUEST_QSL:1>Y<EOR>",
	)
	parsed, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(parsed.Records))
	}
	q := RecordToQso(parsed.Records[0], 1)
	if !q.AppSmRequestQsl {
		t.Errorf("AppSmRequestQsl = false, want true (parsed from <APP_SM_REQUEST_QSL:1>Y)")
	}
}

// TestRecordToQso_ParsesContactedStationEnrichment pins the parser side
// for the enriched contacted-station fields the SPA now emits at submit
// (the COUNTRY="Unknown" bug was the SPA never sending these). The daemon
// must surface COUNTRY/CQZ/ITUZ/DXCC/GRIDSQUARE onto ContactedStation so
// they persist and survive the email-out/ADIF-export round trip.
func TestRecordToQso_ParsesContactedStationEnrichment(t *testing.T) {
	body := []byte(
		"<CALL:5>R3KNS<BAND:3>15m<MODE:3>SSB<FREQ:6>21.240" +
			"<QSO_DATE:8>20260531<TIME_ON:4>1220<TIME_OFF:4>1223" +
			"<RST_SENT:2>53<RST_RCVD:2>55" +
			"<COUNTRY:6>Russia<CQZ:2>17<ITUZ:2>30<DXCC:2>54<GRIDSQUARE:4>KO85" +
			"<STATION_CALLSIGN:6>7Q5MLV<EOR>",
	)
	parsed, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(parsed.Records))
	}
	cs := RecordToQso(parsed.Records[0], 1).ContactedStation
	if cs.Country != "Russia" {
		t.Errorf("Country = %q, want %q", cs.Country, "Russia")
	}
	if cs.CQZ != "17" {
		t.Errorf("CQZ = %q, want %q", cs.CQZ, "17")
	}
	if cs.ITUZ != "30" {
		t.Errorf("ITUZ = %q, want %q", cs.ITUZ, "30")
	}
	if cs.DXCC != "54" {
		t.Errorf("DXCC = %q, want %q", cs.DXCC, "54")
	}
	if cs.Gridsquare != "KO85" {
		t.Errorf("Gridsquare = %q, want %q", cs.Gridsquare, "KO85")
	}
}

// TestRecordToQso_AbsentAppSmRequestQslDecodesFalse pins the
// not-present default. A QSO that didn't carry APP_SM_REQUEST_QSL
// must decode to false — the operator didn't flag it, so the
// edit-overlay checkbox starts unchecked.
func TestRecordToQso_AbsentAppSmRequestQslDecodesFalse(t *testing.T) {
	body := []byte(
		"<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050" +
			"<QSO_DATE:8>20250508<TIME_ON:6>084500<TIME_OFF:6>085000" +
			"<RST_SENT:2>59<RST_RCVD:2>59<COUNTRY:7>England" +
			"<STATION_CALLSIGN:5>G4ABC<EOR>",
	)
	parsed, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	q := RecordToQso(parsed.Records[0], 1)
	if q.AppSmRequestQsl {
		t.Errorf("AppSmRequestQsl = true with field absent; want false")
	}
}

// TestRecordToQso_RoundTripsAppSmRequestQsl exercises the full
// round-trip: bool true → ADIF emit → ADIF parse → bool true. Catches
// regressions where the emitter / parser drift apart on the encoding.
func TestRecordToQso_RoundTripsAppSmRequestQsl(t *testing.T) {
	q := types.Qso{
		AppSmRequestQsl: true,
		QsoDetails: types.QsoDetails{
			Band: "40m", Mode: "SSB", Freq: "7.050",
			QsoDate: "20250508", TimeOn: "0845", TimeOff: "0850",
			RstSent: "59", RstRcvd: "59",
		},
		ContactedStation: types.ContactedStation{Call: "M0CMC", Country: "England"},
		LoggingStation:   types.LoggingStation{StationCallsign: "G4ABC"},
	}

	emitted := ConvertQsoToAdifNoHeader(q)
	parsed, err := Parse([]byte(emitted))
	if err != nil {
		t.Fatalf("Parse round-trip: %v", err)
	}
	if len(parsed.Records) != 1 {
		t.Fatalf("expected 1 record after round-trip, got %d", len(parsed.Records))
	}
	round := RecordToQso(parsed.Records[0], 1)
	if !round.AppSmRequestQsl {
		t.Errorf("AppSmRequestQsl lost in round-trip; emitted: %s", emitted)
	}
}

// TestAppSmRunID_RoundTrips pins the FT8 run identity's ADIF carriage
// (runidentity_test.go RI7's export half): a QSO carrying AppSmRunID emits
// APP_SM_RUN_ID and a parsed record restores it; empty emits nothing.
func TestAppSmRunID_RoundTrips(t *testing.T) {
	runID := "01910d3a-7000-7abc-8def-0123456789ab"
	q := types.Qso{
		AppSmRunID: runID,
		QsoDetails: types.QsoDetails{
			Band: "17m", Mode: "FT8", Freq: "18.100",
			QsoDate: "20260809", TimeOn: "154500", TimeOff: "154600",
			RstSent: "-08", RstRcvd: "-12",
		},
		ContactedStation: types.ContactedStation{Call: "DL9UW"},
		LoggingStation:   types.LoggingStation{StationCallsign: "7Q5MLV"},
	}

	out := ConvertQsoToAdifNoHeader(q)
	want := "<APP_SM_RUN_ID:36>" + runID
	if !strings.Contains(out, want) {
		t.Fatalf("ADIF output missing %q\nGot:\n%s", want, out)
	}

	back := RecordToQso(Record{AppSmRunID: runID}, 1)
	if back.AppSmRunID != runID {
		t.Fatalf("RecordToQso dropped app_sm_run_id: got %q", back.AppSmRunID)
	}

	q.AppSmRunID = ""
	if strings.Contains(ConvertQsoToAdifNoHeader(q), "APP_SM_RUN_ID") {
		t.Fatal("empty run id must emit no APP_SM_RUN_ID tag")
	}
}
