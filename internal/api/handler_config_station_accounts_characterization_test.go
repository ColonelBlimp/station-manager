package api

// The forwarders block of GET and PUT /v1/config for a station-shaped config —
// QRZ, ClubLog and SM Cloud enabled, QRZCQ present and disabled — with
// synthetic, distinct credentials. Characterized under config v5 (W-0021 5C,
// commit a); the case marked CHANGED took its 5C form with config v6, and the
// PUT bodies are the v6 station-account shape (no name, no enabled):
//
//   G1  KEPT     — no credential VALUE appears anywhere in the GET body.
//   G2  CHANGED  — the view is the station account: type, label, action_filter
//                  and the STATION-scoped credentials_set. No `name` or `enabled`
//                  key, and no logbook-scoped key (QRZ api_key, ClubLog email /
//                  password / callsign, QRZCQ call / key, SM Cloud logbook), which
//                  v5 served.
//   P1  KEPT     — (QRZ held disabled: the api test binary cannot build it)
//                  a station-scoped edit (a new SM Cloud token) replaces that key
//                  only; every other stored secret, on that entry and the others,
//                  is preserved, and no entry's enabled state moves.
//   P2  KEPT     — a station key sent blank keeps its stored value — on an enabled
//                  account (where the PUT's build check would also catch a blanked
//                  URL) and on a DISABLED one (P2b), which nothing builds, so only
//                  the merge rule protects the stored value.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

var charSecrets = map[string]string{
	"qrz api_key":      "QRZ-KEY-CHAR-17",
	"clublog email":    "clublog-char@example.test",
	"clublog password": "CL-PASS-CHAR-23",
	"clublog callsign": "CL-CALL-CHAR-37",
	"qrzcq call":       "QCQ-CALL-CHAR-47",
	"qrzcq key":        "QCQ-KEY-CHAR-29",
	"smcloud url":      "https://smc-char.example.test",
	"smcloud token":    "SMC-TOKEN-CHAR-31",
	"smcloud logbook":  "cloud-book-char",
}

// charStationForwarders is shaped like the station's config.json after load:
// applyDefaults has given each entry its registered default endpoints.
func charStationForwarders() []types.ForwarderConfig {
	fwds := []types.ForwarderConfig{
		{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert"},
			Credentials: json.RawMessage(`{"api_key":"` + charSecrets["qrz api_key"] + `"}`)},
		{Name: "clublog", Type: "clublog", Enabled: true, ActionFilter: []string{"insert"},
			Credentials: json.RawMessage(`{"email":"` + charSecrets["clublog email"] + `","password":"` +
				charSecrets["clublog password"] + `","callsign":"` + charSecrets["clublog callsign"] + `"}`)},
		{Name: "qrzcq", Type: "qrzcq", Enabled: false, ActionFilter: []string{"insert"},
			Credentials: json.RawMessage(`{"call":"` + charSecrets["qrzcq call"] + `","key":"` + charSecrets["qrzcq key"] + `"}`)},
		{Name: "smcloud", Type: "smcloud", Enabled: true, ActionFilter: []string{"insert"},
			Credentials: json.RawMessage(`{"url":"` + charSecrets["smcloud url"] + `","token":"` +
				charSecrets["smcloud token"] + `","logbook":"` + charSecrets["smcloud logbook"] + `"}`)},
	}
	for i := range fwds {
		if eps, ok := forwarding.DefaultEndpointsFor(fwds[i].Type); ok {
			fwds[i].Endpoints = eps
		}
	}
	return fwds
}

func charServer(t *testing.T, mutate func([]types.ForwarderConfig)) *Server {
	t.Helper()
	return testServerWithCfg(t, func(c *config.Config) {
		c.Forwarders = charStationForwarders()
		if mutate != nil {
			mutate(c.Forwarders)
		}
	})
}

func charGetForwarders(t *testing.T, srv *Server) (string, []ForwarderInfo) {
	t.Helper()
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Forwarders []ForwarderInfo `json:"forwarders"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return w.Body.String(), body.Forwarders
}

func charStoredCreds(t *testing.T, srv *Server, name string) map[string]string {
	t.Helper()
	for _, fc := range srv.cfg.Snapshot().Forwarders {
		if fc.Name != name {
			continue
		}
		out := map[string]string{}
		if err := json.Unmarshal(fc.Credentials, &out); err != nil {
			t.Fatalf("stored %s credentials: %v", name, err)
		}
		return out
	}
	t.Fatalf("no stored forwarder %q", name)
	return nil
}

func TestCharacterize_GetConfigForwarders(t *testing.T) {
	raw, fwds := charGetForwarders(t, charServer(t, nil))

	// G1: no value leaves the daemon.
	for what, v := range charSecrets {
		if strings.Contains(raw, v) {
			t.Fatalf("G1: GET carries the %s value", what)
		}
	}

	// G2 (CHANGED): the station account only.
	var entries []map[string]any
	var body struct {
		Forwarders []map[string]any `json:"forwarders"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	entries = body.Forwarders
	for _, e := range entries {
		for _, k := range []string{"name", "enabled"} {
			if _, ok := e[k]; ok {
				t.Fatalf("G2: the %v account serves %q: %v", e["type"], k, e)
			}
		}
	}
	got := map[string]string{}
	for _, f := range fwds {
		set := append([]string(nil), f.CredentialsSet...)
		sort.Strings(set)
		got[f.Type] = strings.Join(set, ",")
	}
	// Station-scoped keys as the registry declares them: SM Cloud url and token
	// only (QRZ is not registered in this test binary, and its key is
	// logbook-scoped anyway).
	want := map[string]string{"qrz": "", "clublog": "", "qrzcq": "", "smcloud": "token,url"}
	for typ, w := range want {
		g, ok := got[typ]
		if !ok || g != w {
			t.Fatalf("G2 %s: credentials_set %q (present %v), want %q", typ, g, ok, w)
		}
	}
}

func TestCharacterize_PutConfigStationFieldMerge(t *testing.T) {
	// The api test binary links ClubLog and SM Cloud but not the QRZ or QRZCQ
	// forwarder packages, and a PUT builds every ENABLED entry, so QRZ is held
	// disabled here. The merge never reads `enabled`, so its secrets still
	// prove preservation.
	srv := charServer(t, func(fwds []types.ForwarderConfig) {
		for i := range fwds {
			if fwds[i].Type == "qrz" {
				fwds[i].Enabled = false
			}
		}
	})
	put := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.handlePutConfig(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT status %d: %s", w.Code, w.Body.String())
		}
	}
	// The whole list rides, as the SPA sends it: each account echoed with its
	// type and action_filter (v6: no name, no enabled); only SM Cloud carries a
	// credential.
	echo := `{"type":"qrz","action_filter":["insert"]},` +
		`{"type":"clublog","action_filter":["insert"]},` +
		`{"type":"qrzcq","action_filter":["insert"]},`

	// P1: a new token replaces only the token.
	put(`{"forwarders":[` + echo +
		`{"type":"smcloud","action_filter":["insert"],"credentials":{"token":"SMC-TOKEN-CHAR-NEW"}}]}`)
	smc := charStoredCreds(t, srv, "smcloud")
	if smc["token"] != "SMC-TOKEN-CHAR-NEW" || smc["url"] != charSecrets["smcloud url"] || smc["logbook"] != charSecrets["smcloud logbook"] {
		t.Fatalf("P1: stored SM Cloud credentials = %v; want only the token replaced", smc)
	}
	if c := charStoredCreds(t, srv, "qrz"); c["api_key"] != charSecrets["qrz api_key"] {
		t.Fatalf("P1: QRZ api_key not preserved: %v", c)
	}
	if c := charStoredCreds(t, srv, "clublog"); c["email"] != charSecrets["clublog email"] || c["password"] != charSecrets["clublog password"] || c["callsign"] != charSecrets["clublog callsign"] {
		t.Fatalf("P1: ClubLog credentials not preserved: %v", c)
	}
	if c := charStoredCreds(t, srv, "qrzcq"); c["key"] != charSecrets["qrzcq key"] || c["call"] != charSecrets["qrzcq call"] {
		t.Fatalf("P1: QRZCQ credentials not preserved: %v", c)
	}
	for _, fc := range srv.cfg.Snapshot().Forwarders {
		if want := fc.Name == "clublog" || fc.Name == "smcloud"; fc.Enabled != want {
			t.Fatalf("P1: %s enabled = %v, want %v (unchanged)", fc.Name, fc.Enabled, want)
		}
	}

	// P2: a blank station key keeps the stored value.
	put(`{"forwarders":[` + echo +
		`{"type":"smcloud","action_filter":["insert"],"credentials":{"url":"","token":""}}]}`)
	smc = charStoredCreds(t, srv, "smcloud")
	if smc["token"] != "SMC-TOKEN-CHAR-NEW" || smc["url"] != charSecrets["smcloud url"] {
		t.Fatalf("P2: blank station keys changed the stored values: %v", smc)
	}
}

// P2b: a blank station key on a DISABLED SM Cloud account keeps the stored
// value. A PUT builds only enabled entries, so here nothing but the merge rule
// stands between a blank and the stored secret.
func TestCharacterize_PutConfigBlankKeepsDisabledAccountKeys(t *testing.T) {
	srv := charServer(t, func(fwds []types.ForwarderConfig) {
		for i := range fwds {
			if fwds[i].Type == "qrz" || fwds[i].Type == "smcloud" {
				fwds[i].Enabled = false
			}
		}
	})
	body := `{"forwarders":[` +
		`{"type":"qrz","action_filter":["insert"]},` +
		`{"type":"clublog","action_filter":["insert"]},` +
		`{"type":"qrzcq","action_filter":["insert"]},` +
		`{"type":"smcloud","action_filter":["insert"],"credentials":{"url":"","token":""}}]}`
	req := httptest.NewRequest(http.MethodPut, "/v1/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status %d: %s", w.Code, w.Body.String())
	}
	smc := charStoredCreds(t, srv, "smcloud")
	if smc["url"] != charSecrets["smcloud url"] || smc["token"] != charSecrets["smcloud token"] || smc["logbook"] != charSecrets["smcloud logbook"] {
		t.Fatalf("P2b: blank station keys changed a disabled account's stored values: %v", smc)
	}
}
