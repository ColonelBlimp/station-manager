package smcloud

import (
	"encoding/json"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// AW-1 alpha.2: projectCloudQso prunes every daemon-local identifier (id, logbook_id,
// dedupe_key, csid, country_details.id, and each contact_history[].id) and preserves the
// rest — uuid, the ADIF/enrichment fields, country_details minus its PK, and
// contact_history minus its per-row PK — without mutating the source. Unlike the daemon's
// public API projection, it also drops logbook_id and keeps no transitional id.
func TestProjectCloudQso_FieldMatrix(t *testing.T) {
	q := types.Qso{
		ID:        7,
		UUID:      "0197f9a0-0000-7000-8000-000000000001",
		LogbookID: 3,
		DedupeKey: "deadbeefcafe",
		QsoDetails: types.QsoDetails{
			Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260717", TimeOn: "0700",
		},
		ContactedStation: types.ContactedStation{CSID: 42, Call: "DL9UW", Country: "Germany"},
		CountryDetails:   types.Country{ID: 99, Name: "Germany", Prefix: "DL"},
		ContactHistory: []types.ContactHistory{
			{ID: 5, UUID: "0197f9a0-0000-7000-8000-000000000002", Call: "DL9UW"},
			{ID: 6, UUID: "0197f9a0-0000-7000-8000-000000000003", Call: "G4ABC"},
		},
		LoggingStation: types.LoggingStation{StationCallsign: "G4ABC"},
	}

	raw, err := projectCloudQso(q)
	if err != nil {
		t.Fatalf("projectCloudQso: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("projected body is not an object: %v (%s)", err, raw)
	}

	// Pruned: every daemon-local identifier (logbook_id and id included, unlike the API projection).
	for _, k := range []string{"id", "logbook_id", "dedupe_key", "csid"} {
		if _, ok := obj[k]; ok {
			t.Errorf("%q must be pruned from the cloud payload", k)
		}
	}
	// Preserved: uuid + ADIF/enrichment fields + the pruned-but-present nested objects.
	for _, k := range []string{"uuid", "band", "mode", "call", "country", "country_details", "contact_history"} {
		if _, ok := obj[k]; !ok {
			t.Errorf("%q must survive the cloud projection: %s", k, raw)
		}
	}

	var cd map[string]json.RawMessage
	if err := json.Unmarshal(obj["country_details"], &cd); err != nil {
		t.Fatalf("country_details not an object: %v", err)
	}
	if _, ok := cd["id"]; ok {
		t.Errorf("country_details.id must be pruned: %s", obj["country_details"])
	}
	if _, ok := cd["name"]; !ok {
		t.Errorf("country_details.name must survive")
	}

	var ch []map[string]json.RawMessage
	if err := json.Unmarshal(obj["contact_history"], &ch); err != nil {
		t.Fatalf("contact_history not an array: %v", err)
	}
	if len(ch) != 2 {
		t.Fatalf("contact_history length = %d, want 2", len(ch))
	}
	for i, el := range ch {
		if _, ok := el["id"]; ok {
			t.Errorf("contact_history[%d].id must be pruned from the cloud payload", i)
		}
		if _, ok := el["uuid"]; !ok {
			t.Errorf("contact_history[%d].uuid must survive", i)
		}
	}

	// The source QSO — including the nested contact_history slice — must not be mutated.
	if q.DedupeKey != "deadbeefcafe" || q.ContactedStation.CSID != 42 || q.CountryDetails.ID != 99 {
		t.Errorf("projectCloudQso mutated the source QSO scalars: %+v", q)
	}
	if len(q.ContactHistory) != 2 || q.ContactHistory[0].ID != 5 || q.ContactHistory[1].ID != 6 {
		t.Errorf("projectCloudQso mutated the source contact_history ids: %+v", q.ContactHistory)
	}
}
