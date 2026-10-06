package main

// `smd config-downgrade --to 5` on a config v6 file (W-0021 5C, ADR 0082 part
// 4). With the daemon stopped it reads the adopted Home archive's bindings
// from its closed file and rebuilds each station account as one v5 entry —
// name, enabled and logbook-scoped credentials — or refuses by destination
// and logbook, writing nothing, when the bindings cannot become one entry
// without changing an on/off state or a credential.
//
//   D1  one-logbook Home: each account takes its binding's state and keys, under
//       the binding's legacy name; an account with no binding becomes a disabled
//       entry named after its type. Account fields (endpoints…) are kept.
//   D2  two logbooks in one state with one credential set collapse to the
//       legacy name (migration 0015 down's collapse target).
//   D3  one logbook on and one off → refused, naming both; file unchanged.
//   D4  acceptance case 9: an UNBOUND live logbook counts as off → refused.
//   D5  differing credentials across bindings → refused.
//   D6  Home not seeded and the file unstripped → a plain stamp.
//   D7  a stripped file with Home not seeded → refused (nothing to rebuild from).
//   D8  the catalogue's Home entry names a file of another archive → refused.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const homeUUID = "019fd5c5-efcc-7193-be4f-1fee532ee31a"

// downgradeBed writes a v6 config of station accounts (stripped unless
// legacy) and runs statements against Home's file (the testbed datastore).
func downgradeBed(t *testing.T, mut func(*config.Config), stmts ...string) string {
	t.Helper()
	var homePath string
	tmp := setupImportTestbed(t, func(c *config.Config) {
		homePath = c.Datastore.Path
		c.Forwarders = []types.ForwarderConfig{
			{Type: "qrz", ActionFilter: []string{"insert"}, Endpoints: map[string]string{"insert": "https://qrz-d.example.test/api"}},
			{Type: "smcloud", ActionFilter: []string{"insert"},
				Credentials: json.RawMessage(`{"url":"https://smc-d.example.test","token":"D-TOKEN"}`)},
			{Type: "qrzcq", ActionFilter: []string{"insert"}},
		}
		if mut != nil {
			mut(c)
		}
	})
	db, err := sql.Open("sqlite", "file:"+homePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	_ = db.Close()
	return filepath.Join(tmp, "config.json")
}

const (
	seeded    = `INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id, destination_bindings_seeded_at) VALUES (1, '` + homeUUID + `', 1, datetime('now'))`
	unseeded  = `INSERT INTO archive_metadata (singleton, archive_uuid, default_logbook_id) VALUES (1, '` + homeUUID + `', 1)`
	second    = `INSERT INTO logbook (name, callsign) VALUES ('Second', 'M0TEST')`
	qrzHome   = `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, legacy_name) VALUES (1, 'qrz', 'qrz-home', 1, '{"api_key":"D-KEY"}', 'qrz-home')`
	smcHome   = `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials, legacy_name) VALUES (1, 'smcloud', 'cloud-home', 0, '{"logbook":"d-book"}', 'cloud-home')`
	qrzSecond = `INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (2, 'qrz', 'qrz.second', 1, '{"api_key":"D-KEY"}')`
)

func entriesOf(t *testing.T, path string) (int, map[string]map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version    int              `json:"version"`
		Forwarders []map[string]any `json:"forwarders"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, f := range doc.Forwarders {
		out[f["type"].(string)] = f
	}
	return doc.Version, out
}

func downgradeTo5(t *testing.T, path string) error {
	t.Helper()
	return runConfigDowngradeTo(&strings.Builder{}, []string{"--to", "5", "--yes", "--config", path})
}

func assertRefusedUnchanged(t *testing.T, label, path string, err error, mention ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: the downgrade was not refused", label)
	}
	for _, m := range mention {
		if !strings.Contains(err.Error(), m) {
			t.Fatalf("%s: refusal %q does not name %q", label, err, m)
		}
	}
	if v, _ := entriesOf(t, path); v != 6 {
		t.Fatalf("%s: a refused downgrade rewrote the file (version %d)", label, v)
	}
}

func TestConfigDowngradeV6_D1_RecombinesOneLogbookHome(t *testing.T) {
	path := downgradeBed(t, nil, seeded, qrzHome, smcHome)
	if err := downgradeTo5(t, path); err != nil {
		t.Fatalf("D1: %v", err)
	}
	v, e := entriesOf(t, path)
	if v != 5 {
		t.Fatalf("D1: version %d, want 5", v)
	}
	qrz, smc, qcq := e["qrz"], e["smcloud"], e["qrzcq"]
	if qrz["name"] != "qrz-home" || qrz["enabled"] != true || qrz["credentials"].(map[string]any)["api_key"] != "D-KEY" ||
		qrz["endpoints"].(map[string]any)["insert"] != "https://qrz-d.example.test/api" {
		t.Fatalf("D1: qrz = %v", qrz)
	}
	sc := smc["credentials"].(map[string]any)
	if smc["name"] != "cloud-home" || smc["enabled"] == true || sc["logbook"] != "d-book" || sc["url"] != "https://smc-d.example.test" || sc["token"] != "D-TOKEN" {
		t.Fatalf("D1: smcloud = %v", smc)
	}
	if qcq["name"] != "qrzcq" || qcq["enabled"] == true {
		t.Fatalf("D1: unbound qrzcq = %v; want a disabled entry named after its type", qcq)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("D1: the v5 result does not load: %v", err)
	}
}

func TestConfigDowngradeV6_D2_SameStateCollapsesToTheLegacyName(t *testing.T) {
	path := downgradeBed(t, nil, seeded, second, qrzHome, qrzSecond, smcHome,
		`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (2, 'smcloud', 'smcloud.second', 0, '{"logbook":"d-book"}')`)
	if err := downgradeTo5(t, path); err != nil {
		t.Fatalf("D2: %v", err)
	}
	_, e := entriesOf(t, path)
	if e["qrz"]["name"] != "qrz-home" || e["qrz"]["enabled"] != true {
		t.Fatalf("D2: qrz = %v; want the legacy name, on", e["qrz"])
	}
}

func TestConfigDowngradeV6_D3_MixedStatesRefused(t *testing.T) {
	path := downgradeBed(t, nil, seeded, second, qrzHome,
		`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (2, 'qrz', 'qrz.second', 0, '{"api_key":"D-KEY"}')`)
	assertRefusedUnchanged(t, "D3", path, downgradeTo5(t, path), "qrz", "Test", "Second")
}

func TestConfigDowngradeV6_D4_UnboundLiveLogbookCountsAsOff(t *testing.T) {
	path := downgradeBed(t, nil, seeded, second, qrzHome)
	assertRefusedUnchanged(t, "D4", path, downgradeTo5(t, path), "qrz", "Second")
}

func TestConfigDowngradeV6_D5_DifferingCredentialsRefused(t *testing.T) {
	path := downgradeBed(t, nil, seeded, second, qrzHome,
		`INSERT INTO logbook_destination (logbook_id, destination, forwarder_name, enabled, credentials) VALUES (2, 'qrz', 'qrz.second', 1, '{"api_key":"D-OTHER-KEY"}')`)
	err := downgradeTo5(t, path)
	assertRefusedUnchanged(t, "D5", path, err, "qrz", "credentials differ")
	if strings.Contains(err.Error(), "D-KEY") || strings.Contains(err.Error(), "D-OTHER-KEY") {
		t.Fatalf("D5: the refusal carries a credential value: %v", err)
	}
}

func TestConfigDowngradeV6_D6_UnseededUnstrippedIsAStamp(t *testing.T) {
	path := downgradeBed(t, func(c *config.Config) {
		c.Forwarders = []types.ForwarderConfig{{Name: "qrz-legacy", Type: "qrz", Enabled: true, ActionFilter: []string{"insert"},
			Credentials: json.RawMessage(`{"api_key":"D6-KEY"}`)}}
	}, unseeded)
	if err := downgradeTo5(t, path); err != nil {
		t.Fatalf("D6: %v", err)
	}
	if v, e := entriesOf(t, path); v != 5 || e["qrz"]["name"] != "qrz-legacy" || e["qrz"]["enabled"] != true {
		t.Fatalf("D6: version %d, qrz %v; want the legacy fields stamped as v5", v, e["qrz"])
	}
}

func TestConfigDowngradeV6_D7_StrippedButUnseededRefused(t *testing.T) {
	path := downgradeBed(t, nil, unseeded)
	assertRefusedUnchanged(t, "D7", path, downgradeTo5(t, path), "config.v5.json")
}

func TestConfigDowngradeV6_D8_HomeConfirmedByIdentity(t *testing.T) {
	// The catalogue's Home entry points at a file whose identity is another
	// archive's (homeUUID), as a replaced or mislabelled file would.
	path := downgradeBed(t, func(c *config.Config) {
		c.QsoArchives = []types.QsoArchiveConfig{{ID: "019fd5c5-efcc-7193-be4f-1fee532ee31b", Label: "Home",
			Ownership: types.QsoArchiveOwnershipLegacy, Path: c.Datastore.Path}}
		c.ActiveQsoArchiveID = "019fd5c5-efcc-7193-be4f-1fee532ee31b"
	}, seeded, qrzHome)
	assertRefusedUnchanged(t, "D8", path, downgradeTo5(t, path), "identity")
}
