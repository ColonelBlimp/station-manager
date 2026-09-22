package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// W-0021 slice 1 (ADR 0071, ruling (e) 2026-09-22): config schema v4 adds the
// station-global QSO archive catalogue — `active_qso_archive_id`,
// `pending_qso_archive_id`, `qso_archives[]` — and is the first version with an
// explicit DOWN step, because the rollback drill has to return a v4 file to the
// v3 shape a tagged older build can read (that build refuses unknown keys and a
// newer version). The 3→4 step itself adds nothing: the catalogue is filled by
// adoption at daemon start, never by the migration.

const v3Doc = `{"version":3,"data_dir":"/tmp/x","setup_complete":true,"default_logbook_id":1,
	"logging_station":{"station_callsign":"G4ABC"}}`

const v4Doc = `{"version":4,"data_dir":"/tmp/x","setup_complete":true,"default_logbook_id":1,
	"logging_station":{"station_callsign":"G4ABC"},
	"active_qso_archive_id":"019fd5c5-efcc-7193-be4f-1fee532ee315",
	"pending_qso_archive_id":"019fd5c5-efcc-7193-be4f-1fee532ee316",
	"qso_archives":[
		{"id":"019fd5c5-efcc-7193-be4f-1fee532ee315","label":"Home","ownership":"legacy","path":"/tmp/x/db/station-manager.db"},
		{"id":"019fd5c5-efcc-7193-be4f-1fee532ee316","label":"Contest","ownership":"managed"}]}`

func TestMigrateV3toV4_StampsVersionAddsNoCatalogue(t *testing.T) {
	m := migratedMap(t, v3Doc)
	if v := m["version"]; v != float64(4) {
		t.Fatalf("version after migration = %v, want 4", v)
	}
	for _, k := range []string{"active_qso_archive_id", "pending_qso_archive_id", "qso_archives"} {
		if _, present := m[k]; present {
			t.Errorf("3→4 added %q; the catalogue is filled by adoption at start, not by the migration", k)
		}
	}
	// Existing keys survive untouched.
	if m["default_logbook_id"] != float64(1) || m["setup_complete"] != true {
		t.Errorf("3→4 disturbed existing keys: %v", m)
	}
	// Idempotent: a v4 document comes back byte-identical.
	out, err := migrateDocument([]byte(v4Doc))
	if err != nil || string(out) != v4Doc {
		t.Fatalf("a v4 document must pass through unchanged (err %v)", err)
	}
}

func TestDowngradeDocument_V4toV3_StripsCatalogueKeepsTheRest(t *testing.T) {
	out, err := DowngradeDocument([]byte(v4Doc), 3)
	if err != nil {
		t.Fatalf("downgrade 4→3: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v := m["version"]; v != float64(3) {
		t.Fatalf("version after downgrade = %v, want 3", v)
	}
	for _, k := range []string{"active_qso_archive_id", "pending_qso_archive_id", "qso_archives"} {
		if _, present := m[k]; present {
			t.Errorf("%q survived the 4→3 down step; the v3 loader refuses it as unknown", k)
		}
	}
	if m["default_logbook_id"] != float64(1) || m["data_dir"] != "/tmp/x" {
		t.Errorf("4→3 disturbed keys it does not own: %v", m)
	}
	// The result is a document the v3 pipeline accepts: no unknown keys.
	if unknown := unknownKeysInMigrated(out); len(unknown) != 0 {
		t.Errorf("downgraded document still carries unknown keys: %v", unknown)
	}
}

func TestDowngradeDocument_Refusals(t *testing.T) {
	// At or above current: nothing to do, refused rather than silently stamped.
	for _, to := range []int{4, 5} {
		if _, err := DowngradeDocument([]byte(v4Doc), to); err == nil {
			t.Errorf("downgrade to %d accepted; want a refusal (target must be below the document's version)", to)
		}
	}
	// v3→v2 has no down step: refused by name, never a partial rewrite.
	_, err := DowngradeDocument([]byte(v4Doc), 2)
	if err == nil || !strings.Contains(err.Error(), "3") {
		t.Fatalf("downgrade to 2 = %v; want a refusal naming the version without a down step", err)
	}
	// A document newer than this build cannot be downgraded by it.
	if _, err := DowngradeDocument([]byte(`{"version":9}`), 3); err == nil {
		t.Error("a version-9 document was accepted for downgrade by a v4 build")
	}
	// Malformed input is a parse error, not a stamped file.
	if _, err := DowngradeDocument([]byte(`{"version":4,`), 3); err == nil {
		t.Error("malformed document accepted")
	}
}

// Load round-trips the catalogue into the typed Config.
func TestLoad_V4CatalogueRoundTrip(t *testing.T) {
	p := writeTempConfig(t, v4Doc)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != 4 {
		t.Fatalf("Version = %d, want 4", cfg.Version)
	}
	if cfg.ActiveQsoArchiveID != "019fd5c5-efcc-7193-be4f-1fee532ee315" || cfg.PendingQsoArchiveID != "019fd5c5-efcc-7193-be4f-1fee532ee316" {
		t.Fatalf("selection = active %q pending %q", cfg.ActiveQsoArchiveID, cfg.PendingQsoArchiveID)
	}
	if len(cfg.QsoArchives) != 2 || cfg.QsoArchives[0].Label != "Home" || cfg.QsoArchives[1].Ownership != "managed" {
		t.Fatalf("catalogue = %+v", cfg.QsoArchives)
	}
	if a := cfg.QsoArchiveByID(cfg.ActiveQsoArchiveID); a == nil || a.Path != "/tmp/x/db/station-manager.db" {
		t.Fatalf("QsoArchiveByID(active) = %+v", a)
	}
	// A v3 file with no catalogue is the normal pre-adoption state and loads too.
	if _, err := Load(writeTempConfig(t, v3Doc)); err != nil {
		t.Fatalf("Load v3: %v", err)
	}
}

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}
