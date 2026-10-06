package api

// The station-account contract of /v1/config under config v6 (W-0021 5C, ADR
// 0082 parts 3, 4 and 8; operator rulings 2026-10-06).
//
//   A1  R1: a PUT entry that carries `name` or `enabled` — PRESENCE, an empty
//       name, enabled:false and an explicit null included — is refused 400
//       forwarder_field_binding_owned, naming the field and saying the tab needs
//       reloading; nothing is written.
//   A2  Acceptance case 8: a narrowed account save while Home's seed is still
//       deferred keeps the hidden legacy fields the seed needs — the stored
//       name, enabled state and logbook-scoped credentials.
//   A3  Outcome 5: an account save is refused, naming the binding, when an
//       ENABLED binding in the active archive could not be built with the
//       candidate account; nothing is written. A disabled binding is not probed.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestStationAccount_A1_NameOrEnabledPresenceRefused(t *testing.T) {
	srv := testServerWithCfg(t, func(c *config.Config) {
		c.Forwarders = []types.ForwarderConfig{{
			Type: "smcloud", ActionFilter: []string{"insert"},
			Credentials: json.RawMessage(`{"url":"https://a1.example.test","token":"A1-TOKEN"}`),
		}}
	})
	cases := map[string]struct{ body, field string }{
		"a name":        {`{"forwarders":[{"type":"smcloud","name":"cloud","action_filter":["insert"]}]}`, "name"},
		"an empty name": {`{"forwarders":[{"type":"smcloud","name":"","action_filter":["insert"]}]}`, "name"},
		"enabled true":  {`{"forwarders":[{"type":"smcloud","enabled":true,"action_filter":["insert"]}]}`, "enabled"},
		"enabled false": {`{"forwarders":[{"type":"smcloud","enabled":false,"action_filter":["insert"]}]}`, "enabled"},
		// An explicit null is still the key's presence (review 2026-10-06).
		"a null name":    {`{"forwarders":[{"type":"smcloud","name":null,"action_filter":["insert"]}]}`, "name"},
		"a null enabled": {`{"forwarders":[{"type":"smcloud","enabled":null,"action_filter":["insert"]}]}`, "enabled"},
		// The decoder matches keys case-insensitively; so must presence.
		"a null Name":    {`{"forwarders":[{"type":"smcloud","Name":null,"action_filter":["insert"]}]}`, "name"},
		"a null ENABLED": {`{"forwarders":[{"type":"smcloud","ENABLED":null,"action_filter":["insert"]}]}`, "enabled"},
		"a NAME":         {`{"forwarders":[{"type":"smcloud","NAME":"x","action_filter":["insert"]}]}`, "name"},
	}
	for label, c := range cases {
		w := putConfig(t, srv, c.body)
		var body struct{ Code, Message string }
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != http.StatusBadRequest || body.Code != "forwarder_field_binding_owned" ||
			!strings.Contains(body.Message, c.field) || !strings.Contains(strings.ToLower(body.Message), "reload") {
			t.Fatalf("A1 %s: %d %s; want 400 forwarder_field_binding_owned naming %q and saying to reload", label, w.Code, w.Body.String(), c.field)
		}
	}
	got := srv.cfg.Snapshot().Forwarders[0]
	if got.Name != "" || got.Enabled || !strings.Contains(string(got.Credentials), "A1-TOKEN") {
		t.Fatalf("A1: a refused PUT changed the stored account: %+v %s", got, got.Credentials)
	}
}

func TestStationAccount_A2_DeferredSeedKeepsHiddenLegacyFields(t *testing.T) {
	srv := testServerWithCfg(t, func(c *config.Config) {
		c.Forwarders = []types.ForwarderConfig{{
			Name: "cloud-legacy", Type: "smcloud", Enabled: true, ActionFilter: []string{"insert"},
			Credentials: json.RawMessage(`{"url":"https://a2.example.test","token":"A2-TOKEN","logbook":"a2-book"}`),
		}}
	})
	w := putConfig(t, srv, `{"forwarders":[{"type":"smcloud","action_filter":["insert"],"credentials":{"token":"A2-TOKEN-NEW"}}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("A2: narrowed account save: %d %s", w.Code, w.Body.String())
	}
	got := srv.cfg.Snapshot().Forwarders[0]
	var creds map[string]string
	if err := json.Unmarshal(got.Credentials, &creds); err != nil {
		t.Fatal(err)
	}
	if got.Name != "cloud-legacy" || !got.Enabled || creds["logbook"] != "a2-book" ||
		creds["token"] != "A2-TOKEN-NEW" || creds["url"] != "https://a2.example.test" {
		t.Fatalf("A2: stored account after the save = %+v %v; want the token replaced and the legacy name, enabled and logbook kept", got, creds)
	}
}

func TestStationAccount_A3_SaveProbesEnabledBindings(t *testing.T) {
	srv := testServerWithCfg(t, func(c *config.Config) {
		c.Forwarders = []types.ForwarderConfig{{
			Type: "smcloud", ActionFilter: []string{"insert"},
			Credentials: json.RawMessage(`{"url":"https://a3.example.test","token":"A3-TOKEN"}`),
		}}
	})
	lbID := createTestLogbook(t, srv, "Main", "G4ABC")
	bind := func(enabled bool) {
		t.Helper()
		if err := srv.db.UpsertLogbookDestinationsWithContext(context.Background(), []sqlite.DestinationUpsert{{
			LogbookID: lbID, Destination: "smcloud", ForwarderName: "smcloud-a3", Enabled: enabled,
			Credentials: json.RawMessage(`{"logbook":"a3-book"}`),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	// A cleartext URL to a remote host cannot be built (ST-4a) — a candidate the
	// account alone would accept as a stored string.
	cleartext := `{"forwarders":[{"type":"smcloud","action_filter":["insert"],"credentials":{"url":"http://a3-remote.example.test"}}]}`

	bind(true)
	w := putConfig(t, srv, cleartext)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "smcloud-a3") {
		t.Fatalf("A3: an account the enabled binding cannot build with: %d %s; want 400 naming the binding", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "A3-TOKEN") || strings.Contains(w.Body.String(), "a3-book") {
		t.Fatalf("A3: the refusal carries a credential value: %s", w.Body.String())
	}
	if creds := string(srv.cfg.Snapshot().Forwarders[0].Credentials); !strings.Contains(creds, "https://a3.example.test") {
		t.Fatalf("A3: a refused save changed the stored account: %s", creds)
	}

	bind(false)
	if w := putConfig(t, srv, cleartext); w.Code != http.StatusOK {
		t.Fatalf("A3: with the binding disabled nothing builds it: %d %s; want 200", w.Code, w.Body.String())
	}
}
