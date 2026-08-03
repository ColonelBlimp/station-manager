package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	ENRICHMENT: REMOVING A PROVIDER PASSWORD, AND WHAT A TTL OF ZERO MEANS.

	Two operator rulings, 2026-08-03, both carried over from the Email port:

	  1. A stored lookup password must be REMOVABLE, not just replaceable — the
	     same dead end SMTP had. Blank goes on meaning KEEP (it is what an
	     operator editing the username sends every save), so removal gets its own
	     signal: `password_clear`. If a payload carries both, CLEAR WINS — only
	     the flag can have been set deliberately.
	  2. An explicit TTL of 0 means "trust this cache indefinitely" and must
	     survive; OMITTING the field means "use the default". The wire carries
	     that distinction as a pointer, exactly as types.EnrichmentConfig does.

	L1 AND TestHandlePutConfig_LookupMaskedAndMerged ARE ONE RULE IN TWO HALVES.
	That test's step 3 pins blank-keeps; L1 pins clear-removes. Either alone is
	satisfiable by a wrong merge — blank-keeps alone by a merge that never
	removes, clear-removes alone by one that drops the password on every save.
	Read them together.

	L5's fixture uses 30/0 rather than the defaults: with 365 in the fixture,
	"resolved the default" and "echoed what was sent" agree, and the rule proves
	nothing.
*/

// L1: the clear signal removes the stored provider password and the GET stops
// reporting one.
func TestHandlePutConfig_LookupPasswordCleared(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Lookup.Chain = []types.LookupConfig{{
			Name: types.QRZLookupServiceName, Enabled: true,
			Username: "M0ABC", Password: "stored-pw",
			URL: types.QRZLookupDefaultURL, HttpTimeoutSec: 10,
		}}
	})

	body := `{"lookup":{"hamnut":{"name":"hamnutlookupservice"},` +
		`"chain":[{"name":"qrzlookupservice","enabled":false,"username":"M0ABC",` +
		`"password_clear":true}]}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	chain := srv.cfg.Snapshot().Lookup.Chain
	if len(chain) != 1 {
		t.Fatalf("chain length = %d, want 1", len(chain))
	}
	if chain[0].Password != "" {
		t.Errorf("stored password = %q after an explicit clear, want removed", chain[0].Password)
	}
	// The rest of the provider is untouched by the clear.
	if chain[0].Username != "M0ABC" {
		t.Errorf("clearing the password disturbed the provider: %+v", chain[0])
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Lookup == nil || len(resp.Lookup.Chain) != 1 || resp.Lookup.Chain[0].PasswordSet {
		t.Errorf("response still reports a password as set: %+v", resp.Lookup)
	}
}

// L2: clear beats a typed value, same rule the SMTP block applies, so the two
// credential surfaces cannot drift on the contradiction case.
func TestHandlePutConfig_LookupClearBeatsTypedPassword(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Lookup.Chain = []types.LookupConfig{{
			Name: types.QRZLookupServiceName, Enabled: true,
			Username: "M0ABC", Password: "stored-pw",
			URL: types.QRZLookupDefaultURL, HttpTimeoutSec: 10,
		}}
	})

	body := `{"lookup":{"hamnut":{"name":"hamnutlookupservice"},` +
		`"chain":[{"name":"qrzlookupservice","enabled":false,"username":"M0ABC",` +
		`"password":"typed-pw","password_clear":true}]}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	// "typed-pw" is neither the stored value nor empty, so this fixture tells
	// all three outcomes apart: kept-stored, took-typed, cleared.
	if got := srv.cfg.Snapshot().Lookup.Chain[0].Password; got != "" {
		t.Errorf("stored password = %q, want the explicit clear to win", got)
	}
}

// L2b: clearing the password of a provider left ENABLED is REFUSED, and the
// stored config is untouched.
//
// This is the P1 from clean-room review 9732ab7914af. The save used to return
// 200 and the daemon then failed to START at the next restart, because QRZ's own
// Initialize rejects an empty password and buildEnrichment's error aborts run().
// Hours could separate the settings change from the dead station.
//
// The second assertion is the load-bearing half: a 400 that had already written
// the empty password would leave exactly the state the 400 exists to prevent.
func TestHandlePutConfig_LookupClearOnEnabledProviderRefused(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Lookup.Chain = []types.LookupConfig{{
			Name: types.QRZLookupServiceName, Enabled: true,
			Username: "M0ABC", Password: "stored-pw",
			URL: types.QRZLookupDefaultURL, HttpTimeoutSec: 10,
		}}
	})

	body := `{"lookup":{"hamnut":{"name":"hamnutlookupservice"},` +
		`"chain":[{"name":"qrzlookupservice","enabled":true,"username":"M0ABC",` +
		`"password_clear":true}]}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_lookup") {
		t.Errorf("body = %s, want invalid_lookup", w.Body.String())
	}

	// The write is ABANDONED, not partially applied.
	if got := srv.cfg.Snapshot().Lookup.Chain[0].Password; got != "stored-pw" {
		t.Errorf("stored password = %q after a refused save, want it untouched", got)
	}
}

// L3: password_clear is a PUT-only command and must never be echoed — a client
// that round-tripped a GET body would otherwise wipe the secret by accident.
func TestHandleGetConfig_LookupNeverEchoesPasswordClear(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Lookup.Chain = []types.LookupConfig{{
			Name: types.QRZLookupServiceName, Enabled: true,
			Username: "M0ABC", Password: "stored-pw",
			URL: types.QRZLookupDefaultURL, HttpTimeoutSec: 10,
		}}
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)

	// Assert on the RAW body: a struct read reports false for both "absent" and
	// "present and false", and absent is the rule.
	if strings.Contains(w.Body.String(), "password_clear") {
		t.Errorf("GET body carries password_clear; it is PUT-only: %s", w.Body.String())
	}
}

// L4: an omitted TTL takes the default; an explicit 0 is kept as the operator's
// "never goes stale". Both halves in one test because the pair IS the rule —
// either alone is satisfiable by a handler that ignores the field entirely.
func TestHandlePutConfig_LookupTTLAbsentVsExplicitZero(t *testing.T) {
	srv := testServer(t)

	// station_ttl_days omitted → default; country_ttl_days explicitly 0 → kept.
	body := `{"lookup":{"hamnut":{"name":"hamnutlookupservice"},"chain":[],` +
		`"country_ttl_days":0}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Lookup
	if got.CountryTTLDays == nil {
		t.Fatal("country_ttl_days is nil after the PUT")
	}
	if *got.CountryTTLDays != 0 {
		t.Errorf("country_ttl_days = %d, want the operator's explicit 0", *got.CountryTTLDays)
	}
	if got.StationTTLDays == nil {
		t.Fatal("station_ttl_days is nil after the PUT; Normalize should have filled it")
	}
	if *got.StationTTLDays != 90 {
		t.Errorf("station_ttl_days = %d, want the default 90", *got.StationTTLDays)
	}
}

// L5: and the response echoes both, so the form shows what was actually stored
// rather than the hole it sent. Fixture uses 30 and 0 — neither is a default,
// so "echoed what was sent" and "echoed the default" cannot agree.
func TestHandlePutConfig_LookupResponseEchoesResolvedTTLs(t *testing.T) {
	srv := testServer(t)

	body := `{"lookup":{"hamnut":{"name":"hamnutlookupservice"},"chain":[],` +
		`"country_ttl_days":0,"station_ttl_days":30}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Lookup == nil || resp.Lookup.CountryTTLDays == nil || resp.Lookup.StationTTLDays == nil {
		t.Fatalf("response TTLs missing: %+v", resp.Lookup)
	}
	if *resp.Lookup.CountryTTLDays != 0 || *resp.Lookup.StationTTLDays != 30 {
		t.Errorf("response TTLs = %d/%d, want 0/30",
			*resp.Lookup.CountryTTLDays, *resp.Lookup.StationTTLDays)
	}
}
