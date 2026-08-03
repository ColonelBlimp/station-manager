package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	LOOKUP SOURCE DISPLAY LABEL — OPERATOR-SET IN config.json, READ-ONLY ELSEWHERE.

	CRITERION (operator, 2026-08-03):

	    When I set a lookup source's label in config.json and restart, Settings →
	    Enrichment shows my label — and a source with no label still shows the
	    built-in name rather than a blank or a raw service id.

	The same shape as the forwarder label (forwarder_label_test.go), for the same
	reason: the friendly name otherwise lives in the SPA's PROVIDERS map, so it is
	a build + deploy to change and unreachable for the operator. It matters more
	here than it looks, because an UNRECOGNISED provider currently shows its raw
	wire name (`hamqth`) — a label is the only way to give it a readable one
	without shipping a new build.

	NOT `name`: that is the key mergeLookup matches on to carry each provider's
	stored password across a save, and the key LookupServiceConfig resolves. A
	rename there silently detaches the credentials.

	M2 IS THE ONE THAT BITES, and it is a defect the code base has already been
	bitten by twice. mergeLookup REBUILDS each provider from the PUT payload, and
	the payload has no label field — so unless the merge carries it over
	explicitly, the first save from the Enrichment section DELETES it. That is
	exactly what handler_config.go:1311 documents for forwarders (where the
	sibling `Endpoints` was lost silently for a while), and the identical rebuild
	is one function away.
*/

// M1 — a label set in config.json reaches the client. Without this the field is
// unreadable and the section can only ever show the built-in name.
func TestLookupLabel_IsServedToTheClient(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Lookup.Hamnut = types.LookupConfig{
			Name: types.HamNutLookupServiceName, Enabled: true,
			URL: types.HamNutLookupDefaultURL, HttpTimeoutSec: 10,
			Label: "Prefix lookup",
		}
		cfg.Lookup.Chain = []types.LookupConfig{{
			Name: types.QRZLookupServiceName, Enabled: false,
			URL: types.QRZLookupDefaultURL, HttpTimeoutSec: 10,
			Label: "QRZ (club account)",
		}}
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Lookup == nil {
		t.Fatal("lookup block missing from GET")
	}
	if got := resp.Lookup.Hamnut.Label; got != "Prefix lookup" {
		t.Errorf("hamnut label = %q, want %q", got, "Prefix lookup")
	}
	if len(resp.Lookup.Chain) != 1 || resp.Lookup.Chain[0].Label != "QRZ (club account)" {
		t.Errorf("chain label not served: %+v", resp.Lookup.Chain)
	}
}

// M2 — A SETTINGS SAVE MUST NOT DELETE THE LABEL. The section has no label
// control, so every PUT it sends omits the field; mergeLookup rebuilds each
// provider from that payload and would drop it.
//
// The fixture edits something ELSE (the username) so the save is a realistic
// one: a test that changed nothing could pass against a handler that skipped
// the merge entirely.
func TestLookupLabel_SurvivesASaveThatDoesNotCarryIt(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Lookup.Hamnut = types.LookupConfig{
			Name: types.HamNutLookupServiceName, Enabled: true,
			URL: types.HamNutLookupDefaultURL, HttpTimeoutSec: 10,
			Label: "Prefix lookup",
		}
		cfg.Lookup.Chain = []types.LookupConfig{{
			Name: types.QRZLookupServiceName, Enabled: true,
			URL: types.QRZLookupDefaultURL, HttpTimeoutSec: 10,
			Username: "M0ABC", Password: "s3cret",
			Label: "QRZ (club account)",
		}}
	})

	body := `{"lookup":{` +
		`"hamnut":{"name":"hamnutlookupservice","enabled":true,` +
		`"url":"https://api.hamnut.com/v1/call-signs/prefixes","timeout_sec":10},` +
		`"chain":[{"name":"qrzlookupservice","enabled":true,` +
		`"url":"https://xmldata.qrz.com/xml/current/","timeout_sec":10,` +
		`"username":"M0XYZ"}]}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Lookup
	if got.Hamnut.Label != "Prefix lookup" {
		t.Errorf("hamnut label = %q after a save that omitted it, want it preserved", got.Hamnut.Label)
	}
	if len(got.Chain) != 1 || got.Chain[0].Label != "QRZ (club account)" {
		t.Errorf("chain label lost by the save: %+v", got.Chain)
	}
	// The edit that accompanied it did land, so this is a real save.
	if got.Chain[0].Username != "M0XYZ" {
		t.Errorf("username = %q, want the edit to have applied", got.Chain[0].Username)
	}
}

// M3 — and a label SENT on a PUT is ignored. config.json is the only place it
// may be set, so a client cannot rename a source (nor blank one out by sending
// an empty string, which M2 already covers by omission).
func TestLookupLabel_IsIgnoredOnPut(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Lookup.Chain = []types.LookupConfig{{
			Name: types.QRZLookupServiceName, Enabled: false,
			URL: types.QRZLookupDefaultURL, HttpTimeoutSec: 10,
			Label: "QRZ (club account)",
		}}
	})

	body := `{"lookup":{"hamnut":{"name":"hamnutlookupservice"},` +
		`"chain":[{"name":"qrzlookupservice","enabled":false,` +
		`"url":"https://xmldata.qrz.com/xml/current/","timeout_sec":10,` +
		`"label":"renamed by a client"}]}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	if got := srv.cfg.Snapshot().Lookup.Chain[0].Label; got != "QRZ (club account)" {
		t.Errorf("label = %q; a PUT must not be able to set it", got)
	}
}
