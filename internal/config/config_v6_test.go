package config

// Config v6 (W-0021 5C, ADR 0082 part 4). The forwarder entry becomes the
// STATION ACCOUNT: `name`, `enabled` and logbook-scoped credentials are binding
// facts. v6 keeps them KNOWN but deprecated, so a v5 file loads unchanged and
// keeps them until Home's seed commits (the strip is a later startup step).
//
//   V1  a v5 file loads as v6 with every legacy field intact.
//   V2  a stripped account (no name, no enabled) is valid v6; two of them of
//       different types are fine. A non-empty name is still unique, and one
//       entry per type still holds.
//   V3  a stripped entry is written without `name` or `enabled` keys.
//   V4  6→5 as a pure document step stamps an UNSTRIPPED file; a stripped one is
//       refused — it needs the data-aware downgrade, which reads Home's bindings.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

const v5WithForwardersDoc = `{"version":5,"data_dir":"/tmp/x","setup_complete":true,"default_logbook_id":1,
	"logging_station":{"station_callsign":"G4ABC"},
	"forwarders":[{"name":"cloud-home","type":"smcloud","enabled":true,"action_filter":["insert"],
		"credentials":{"url":"https://smc-v6.example.test","token":"V6-TOKEN-1","logbook":"v6-book"}}]}`

func TestConfigV6_V5FileLoadsWithLegacyFieldsIntact(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, v5WithForwardersDoc))
	if err != nil {
		t.Fatalf("V1: load: %v", err)
	}
	if cfg.Version != 6 {
		t.Fatalf("V1: in-memory version %d, want 6", cfg.Version)
	}
	fc := cfg.Forwarders[0]
	if fc.Name != "cloud-home" || !fc.Enabled || !strings.Contains(string(fc.Credentials), `"logbook":"v6-book"`) {
		t.Fatalf("V1: legacy fields not kept: %+v %s", fc, fc.Credentials)
	}
}

func TestConfigV6_StrippedAccountsValidate(t *testing.T) {
	stripped := []types.ForwarderConfig{
		{Type: "smcloud", ActionFilter: []string{"insert"}},
		{Type: "qrz", ActionFilter: []string{"insert"}},
	}
	if err := validateForwarders(stripped); err != nil {
		t.Fatalf("V2: two nameless accounts of different types: %v", err)
	}
	dupName := []types.ForwarderConfig{{Name: "a", Type: "smcloud"}, {Name: "a", Type: "qrz"}}
	if err := validateForwarders(dupName); err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("V2: duplicate non-empty names: err = %v", err)
	}
	dupType := []types.ForwarderConfig{{Type: "qrz"}, {Type: "qrz"}}
	if err := validateForwarders(dupType); err == nil || !strings.Contains(err.Error(), "one entry per destination type") {
		t.Fatalf("V2: two accounts of one type: err = %v", err)
	}
}

func TestConfigV6_StrippedEntryOmitsNameAndEnabled(t *testing.T) {
	b, err := json.Marshal(types.ForwarderConfig{Type: "qrz", ActionFilter: []string{"insert"}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["name"]; ok {
		t.Fatalf("V3: a stripped entry writes name: %s", b)
	}
	if _, ok := m["enabled"]; ok {
		t.Fatalf("V3: a stripped entry writes enabled: %s", b)
	}
}

func TestConfigV6_DocumentDownStampsUnstrippedRefusesStripped(t *testing.T) {
	unstripped := strings.Replace(v5WithForwardersDoc, `"version":5`, `"version":6`, 1)
	out, err := DowngradeDocument([]byte(unstripped), 5)
	if err != nil {
		t.Fatalf("V4: unstripped 6→5: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal(out, &d); err != nil {
		t.Fatal(err)
	}
	f := d["forwarders"].([]any)[0].(map[string]any)
	if d["version"] != float64(5) || f["name"] != "cloud-home" || f["enabled"] != true {
		t.Fatalf("V4: unstripped 6→5 = %v", d)
	}

	stripped := `{"version":6,"data_dir":"/tmp/x","forwarders":[{"type":"smcloud","credentials":{"url":"https://smc-v6.example.test","token":"V6-TOKEN-1"}}]}`
	if _, err := DowngradeDocument([]byte(stripped), 5); err == nil || !strings.Contains(err.Error(), "smd config-downgrade") {
		t.Fatalf("V4: a stripped file must need the data-aware downgrade: err = %v", err)
	}
	if _, err := DowngradeDocument([]byte(stripped), 4); err == nil {
		t.Fatal("V4: a stripped file must not reach v4 through the document steps either")
	}
}
