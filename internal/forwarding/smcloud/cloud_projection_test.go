package smcloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// AW-1 alpha.2: the daemon→SM Cloud payload must not carry any daemon-LOCAL identifier. The
// cloud keys purely on uuid (+ tenant) and never reads these, so a boundary projection
// strips the local row id, logbook_id, dedupe_key, csid, country_details.id, and EVERY
// contact_history[].id, while uuid and the ADIF/enrichment fields ride through. This is a
// SEPARATE projection from the daemon's public API one (which keeps logbook_id and a
// transitional id). projectCloudQso's full field matrix is proven in
// cloud_projection_unit_test.go; this pins that Submit applies it to the wire body.
func TestSubmit_CloudPayloadOmitsDaemonLocalIDs(t *testing.T) {
	var qsoObj map[string]json.RawMessage
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var raw struct {
			Qsos []struct {
				Qso json.RawMessage `json:"qso"`
			} `json:"qsos"`
		}
		if err := json.Unmarshal(b, &raw); err != nil || len(raw.Qsos) != 1 {
			t.Errorf("decode body: %v (%s)", err, b)
		}
		_ = json.Unmarshal(raw.Qsos[0].Qso, &qsoObj)
		_ = json.NewEncoder(w).Encode(putResponse{Received: 1, Applied: ip(1)})
	}))
	defer ts.Close()

	f, _ := New(testConfig(ts.URL))
	q := testQso("0197f9a0-0000-7000-8000-0000000000aa")
	// Daemon-local identifiers that must NOT cross the cloud boundary.
	q.ID = 7
	q.LogbookID = 3
	q.DedupeKey = "deadbeefcafe"
	q.ContactedStation.CSID = 42
	q.CountryDetails = types.Country{ID: 99, Name: "Germany"}
	q.ContactHistory = []types.ContactHistory{
		{ID: 5, UUID: "0197f9a0-0000-7000-8000-0000000000bb", Call: "DL9UW"},
	}

	res := f.Submit(context.Background(), q, action.Insert, "")
	if res.Outcome != forwarding.OutcomeSuccess {
		t.Fatalf("outcome = %s (%v)", res.Outcome, res.Err)
	}

	for _, k := range []string{"id", "logbook_id", "dedupe_key", "csid"} {
		if v, ok := qsoObj[k]; ok {
			t.Errorf("cloud payload must not carry the daemon-local %q: %s", k, v)
		}
	}
	for _, k := range []string{"uuid", "call", "country_details", "contact_history"} {
		if _, ok := qsoObj[k]; !ok {
			t.Errorf("cloud payload must keep %q", k)
		}
	}
	// country_details keeps its descriptive fields but not the DXCC row PK.
	var cd map[string]json.RawMessage
	if err := json.Unmarshal(qsoObj["country_details"], &cd); err == nil {
		if _, ok := cd["id"]; ok {
			t.Errorf("country_details.id must be pruned from the cloud payload: %s", qsoObj["country_details"])
		}
	}
	// EVERY contact_history element loses its local id.
	var ch []map[string]json.RawMessage
	if err := json.Unmarshal(qsoObj["contact_history"], &ch); err == nil {
		for _, el := range ch {
			if _, ok := el["id"]; ok {
				t.Errorf("contact_history[].id must be pruned from the cloud payload: %s", qsoObj["contact_history"])
			}
		}
	}
}
