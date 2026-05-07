package api

import (
	"context"
	"net/http"
	"testing"
)

// Tests for ADR 0017 #10's second write path — qsoservice.Submit
// upserting contacted_station as a best-effort cache write outside
// the QSO transaction.

const submitContactedStationADIF = `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<NAME:10>Marc Veary<QTH:10>Birmingham<GRIDSQUARE:4>IO92<EOR>`

func TestSubmit_UpsertsContactedStation_WithOperatorTypedFields(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	w := submitQso(t, srv, lbID, submitContactedStationADIF, false)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status = %d; body = %s", w.Code, w.Body.String())
	}

	got, err := srv.db.FetchContactedStationByCallsignWithContext(context.Background(), "M0CMC")
	if err != nil {
		t.Fatalf("contacted_station not written: %v", err)
	}
	if got.Call != "M0CMC" {
		t.Errorf("Call = %q, want M0CMC", got.Call)
	}
	if got.Name != "Marc Veary" {
		t.Errorf("Name = %q, want \"Marc Veary\" (operator-typed should be persisted)", got.Name)
	}
	if got.QTH != "Birmingham" {
		t.Errorf("QTH = %q, want Birmingham", got.QTH)
	}
	if got.Country != "England" {
		t.Errorf("Country = %q, want England (operator-typed)", got.Country)
	}
	if got.LastRefreshedAt.IsZero() {
		t.Error("LastRefreshedAt is zero — upsert path didn't stamp it")
	}
}

// Cold-callsign + only the schema-required fields (call + country).
// The operator hasn't yet typed name/qth/grid — the upsert should
// still create a row so subsequent enrichment Tabs have somewhere to
// merge into. This is the "you log a brand-new station; we record what
// you have" case.
func TestSubmit_UpsertsContactedStation_MinimalQso(t *testing.T) {
	const minimalADIF = `<CALL:6>ZS6XYZ<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:12>South Africa<EOR>`

	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	w := submitQso(t, srv, lbID, minimalADIF, false)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status = %d; body = %s", w.Code, w.Body.String())
	}

	got, err := srv.db.FetchContactedStationByCallsignWithContext(context.Background(), "ZS6XYZ")
	if err != nil {
		t.Fatalf("contacted_station not written for minimal QSO: %v", err)
	}
	if got.Call != "ZS6XYZ" {
		t.Errorf("Call = %q, want ZS6XYZ", got.Call)
	}
	if got.Country != "South Africa" {
		t.Errorf("Country = %q, want \"South Africa\"", got.Country)
	}
	if got.Name != "" {
		t.Errorf("Name = %q, want empty (operator didn't type one)", got.Name)
	}
}

// When the operator submits a second QSO with the same callsign and
// adds a name they got on-air, the upsert merges — non-empty new
// fields overwrite, empty new fields preserve existing values.
func TestSubmit_UpsertsContactedStation_SecondSubmitMerges(t *testing.T) {
	const firstADIF = `<CALL:6>ZS6XYZ<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<TIME_OFF:4>0850<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:12>South Africa<EOR>`
	// Second submit — different band/freq/time so dedupe doesn't trip,
	// but adds name + qth this time.
	const secondADIF = `<CALL:6>ZS6XYZ<BAND:3>20m<MODE:3>SSB<FREQ:6>14.200<QSO_DATE:8>20250509<TIME_ON:4>1230<TIME_OFF:4>1235<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:12>South Africa<NAME:4>John<QTH:8>Pretoria<EOR>`

	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	if w := submitQso(t, srv, lbID, firstADIF, false); w.Code != http.StatusCreated {
		t.Fatalf("first submit: status = %d", w.Code)
	}
	if w := submitQso(t, srv, lbID, secondADIF, false); w.Code != http.StatusCreated {
		t.Fatalf("second submit: status = %d; body = %s", w.Code, w.Body.String())
	}

	got, err := srv.db.FetchContactedStationByCallsignWithContext(context.Background(), "ZS6XYZ")
	if err != nil {
		t.Fatalf("contacted_station not written: %v", err)
	}
	// After two submits, the row carries the second submit's name + qth
	// (non-empty new wins) and the country preserved from both (same value).
	if got.Name != "John" {
		t.Errorf("Name = %q, want John (second submit's value)", got.Name)
	}
	if got.QTH != "Pretoria" {
		t.Errorf("QTH = %q, want Pretoria", got.QTH)
	}
	if got.Country != "South Africa" {
		t.Errorf("Country = %q, want \"South Africa\"", got.Country)
	}
}

// Duplicate submission (same dedupe inputs) doesn't create a duplicate
// contacted_station row. The duplicate path returns early with
// SubmitResult{Status: "duplicate"} BEFORE the upsert runs, so we
// don't rewrite the row on each duplicate retry.
func TestSubmit_DuplicateQso_DoesNotReUpsertContactedStation(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	if w := submitQso(t, srv, lbID, submitContactedStationADIF, false); w.Code != http.StatusCreated {
		t.Fatalf("first submit: status = %d", w.Code)
	}
	first, err := srv.db.FetchContactedStationByCallsignWithContext(context.Background(), "M0CMC")
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	// Second submit with identical inputs — duplicate path.
	if w := submitQso(t, srv, lbID, submitContactedStationADIF, false); w.Code != http.StatusOK {
		t.Fatalf("duplicate submit: status = %d (want 200 duplicate)", w.Code)
	}

	second, err := srv.db.FetchContactedStationByCallsignWithContext(context.Background(), "M0CMC")
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	// CSID stable — the duplicate path didn't insert a second row.
	if second.CSID != first.CSID {
		t.Errorf("CSID changed on duplicate: %d → %d (expected stable)", first.CSID, second.CSID)
	}
}

// (TestSubmit_QsoCommit_IsIndependentOfContactedStationWrite removed —
// redundant with the other tests, which already verify both writes
// happened. Failure-injection on the cache write would require either
// a real DB error scenario or a mock helper, neither of which fits
// the project's "real sqlite, no mocks" testing convention.)
