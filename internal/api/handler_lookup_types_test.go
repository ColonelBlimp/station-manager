package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/lookupdef"
)

/*
	GET /v1/lookup-types — THE DESCRIPTOR SURFACE (ADR 0062).

	CRITERION:

	    A lookup provider compiled into the daemon shows up in Settings →
	    Enrichment with its proper name and the right fields, without anyone
	    editing the SPA.

	This is the endpoint that makes that true. Its counterpart,
	/v1/forwarder-types, has done the same job for destinations since ADR 0028;
	enrichment carried a hardcoded map in the SPA instead until this landed.

	T3 IS THE ONE WITH TEETH. Serving the list is easy; serving it with the
	CREDENTIAL FACTS is what lets the section decide whether to draw a
	username/password pair at all. Get that wrong and hamnut — anonymous by
	design — sprouts login boxes the operator will try to fill in.
*/

// withRegisteredProviders installs a known set for one test. The registry is
// process-global and the api package's other tests do not touch it, so cleanup
// matters more than setup here.
func withRegisteredProviders(t *testing.T, ds ...lookupdef.ProviderDescriptor) {
	t.Helper()
	lookupdef.ResetForTests()
	for _, d := range ds {
		lookupdef.RegisterProvider(d)
	}
	t.Cleanup(lookupdef.ResetForTests)
}

func getLookupTypes(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/lookup-types", nil)
	w := httptest.NewRecorder()
	srv.handleLookupTypes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// T1 — every registered provider is served.
func TestHandleLookupTypes_ServesRegisteredProviders(t *testing.T) {
	withRegisteredProviders(t,
		lookupdef.ProviderDescriptor{
			Name: "anonprov", DisplayName: "Anon Country", Kind: lookupdef.KindCountry,
		},
		lookupdef.ProviderDescriptor{
			Name: "credprov", DisplayName: "Credentialed Callsign", Kind: lookupdef.KindCallsign,
			NeedsCredentials: true, MinUsernameLen: 3, MinPasswordLen: 5,
		},
	)
	srv := testServer(t)

	types, _ := getLookupTypes(t, srv)["types"].([]any)
	if len(types) != 2 {
		t.Fatalf("served %d types, want 2", len(types))
	}
}

// T2 — an EMPTY registry is a clean empty list, not a null or a 500. A daemon
// built without provider packages is unusual but not broken, and the SPA must
// render "no sources" rather than fail to parse.
func TestHandleLookupTypes_EmptyRegistryServesEmptyList(t *testing.T) {
	lookupdef.ResetForTests()
	t.Cleanup(lookupdef.ResetForTests)
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/lookup-types", nil)
	w := httptest.NewRecorder()
	srv.handleLookupTypes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if strings.Contains(w.Body.String(), "null") {
		t.Errorf("empty registry served null rather than []: %s", w.Body.String())
	}
}

// T3 — the credential facts ride, and they are what the section needs to decide
// whether to draw a login at all. Asserted on the RAW JSON keys because the
// SPA reads those names, not the Go field names.
func TestHandleLookupTypes_CarriesCredentialFacts(t *testing.T) {
	withRegisteredProviders(t,
		lookupdef.ProviderDescriptor{
			Name: "anonprov", DisplayName: "Anon Country", Kind: lookupdef.KindCountry,
			Help: "no login needed",
		},
		lookupdef.ProviderDescriptor{
			Name: "credprov", DisplayName: "Credentialed Callsign", Kind: lookupdef.KindCallsign,
			Help: "needs a subscription", NeedsCredentials: true,
		},
	)
	srv := testServer(t)

	byName := map[string]map[string]any{}
	for _, raw := range getLookupTypes(t, srv)["types"].([]any) {
		d := raw.(map[string]any)
		byName[d["name"].(string)] = d
	}

	if got := byName["credprov"]["needs_credentials"]; got != true {
		t.Errorf("credprov needs_credentials = %v, want true", got)
	}
	// The anonymous one must say so explicitly rather than omit the field —
	// an absent key and "false" read the same to a client only if the client
	// happens to default correctly, and hamnut sprouting login boxes is the
	// failure this prevents.
	if got, present := byName["anonprov"]["needs_credentials"]; !present || got != false {
		t.Errorf("anonprov needs_credentials = %v (present=%v), want an explicit false", got, present)
	}
	if got := byName["credprov"]["display_name"]; got != "Credentialed Callsign" {
		t.Errorf("display_name not served: %v", got)
	}
	if got := byName["anonprov"]["kind"]; got != "country" {
		t.Errorf("kind = %v, want country", got)
	}
	if got := byName["credprov"]["help"]; got != "needs a subscription" {
		t.Errorf("help not served: %v", got)
	}
}

// ADR 0068 — Settings receives the completion-field catalogue from the same
// package config validation uses, so adding a field cannot make the UI and
// daemon disagree about the accepted JSON name.
func TestHandleLookupTypes_CarriesCompletionFields(t *testing.T) {
	lookupdef.ResetForTests()
	t.Cleanup(lookupdef.ResetForTests)
	srv := testServer(t)

	fields, ok := getLookupTypes(t, srv)["completion_fields"].([]any)
	if !ok || len(fields) != 2 {
		t.Fatalf("completion_fields = %#v, want the two supported fields", fields)
	}
	want := []string{"name", "gridsquare"}
	for i, raw := range fields {
		field := raw.(map[string]any)
		if got := field["name"]; got != want[i] {
			t.Errorf("completion_fields[%d].name = %v, want %q", i, got, want[i])
		}
		if field["display_name"] == "" {
			t.Errorf("completion_fields[%d] has no display_name", i)
		}
	}
}
