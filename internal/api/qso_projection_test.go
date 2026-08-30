package api

import (
	"encoding/json"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// AW-1 alpha.2: projectPublicQso prunes exactly the server-internal identifiers
// (dedupe_key, csid, country_details.id) and preserves everything else — including the
// canonical uuid, the transitional local id, logbook_id, representative ADIF/enrichment
// fields, and contact_history[].id (retained through alpha.2) — without mutating the source.
func TestProjectPublicQso_FieldMatrix(t *testing.T) {
	q := types.Qso{
		ID:        7,
		UUID:      "01a05288-0000-7000-8000-000000000001",
		LogbookID: 3,
		DedupeKey: "deadbeefcafe",
		QsoDetails: types.QsoDetails{
			Band: "40m", Mode: "SSB", Freq: "7.050", QsoDate: "20250508", TimeOn: "0845",
		},
		ContactedStation: types.ContactedStation{CSID: 42, Call: "M0CMC", Country: "England"},
		CountryDetails:   types.Country{ID: 99, Name: "England", Prefix: "G"},
		ContactHistory: []types.ContactHistory{
			{ID: 5, UUID: "01a05288-0000-7000-8000-000000000002", Call: "M0CMC"},
		},
		LoggingStation: types.LoggingStation{StationCallsign: "G4ABC"},
	}

	raw, err := projectPublicQso(q)
	if err != nil {
		t.Fatalf("projectPublicQso: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("projected body is not an object: %v (%s)", err, raw)
	}

	// Pruned: the server-internal identifiers.
	for _, k := range []string{"dedupe_key", "csid"} {
		if _, ok := obj[k]; ok {
			t.Errorf("%q must be pruned from the public projection", k)
		}
	}

	// Survive: canonical id, transitional id, logbook key, ADIF + enrichment fields.
	for _, k := range []string{"uuid", "id", "logbook_id", "band", "mode", "call", "country", "country_details", "contact_history"} {
		if _, ok := obj[k]; !ok {
			t.Errorf("%q must survive the public projection: %s", k, raw)
		}
	}

	// country_details keeps its descriptive fields but loses the DXCC row PK.
	var cd map[string]json.RawMessage
	if err := json.Unmarshal(obj["country_details"], &cd); err != nil {
		t.Fatalf("country_details not an object: %v", err)
	}
	if _, ok := cd["id"]; ok {
		t.Errorf("country_details.id must be pruned: %s", obj["country_details"])
	}
	if _, ok := cd["name"]; !ok {
		t.Errorf("country_details.name must survive the projection")
	}

	// contact_history[].id is RETAINED through alpha.2 (removed in alpha.3).
	var ch []map[string]json.RawMessage
	if err := json.Unmarshal(obj["contact_history"], &ch); err != nil {
		t.Fatalf("contact_history not an array: %v", err)
	}
	if len(ch) != 1 {
		t.Fatalf("contact_history length = %d, want 1", len(ch))
	}
	if _, ok := ch[0]["id"]; !ok {
		t.Errorf("contact_history[].id must be retained in alpha.2")
	}
	if _, ok := ch[0]["uuid"]; !ok {
		t.Errorf("contact_history[].uuid must survive the projection")
	}

	// The source QSO must not be mutated by projection.
	if q.DedupeKey != "deadbeefcafe" || q.ContactedStation.CSID != 42 || q.CountryDetails.ID != 99 {
		t.Errorf("projectPublicQso mutated the source QSO: %+v", q)
	}
}
