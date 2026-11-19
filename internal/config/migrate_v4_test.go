package config

import (
	"fmt"
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
	if v := m["version"]; v != float64(CurrentSchemaVersion()) {
		t.Fatalf("version after migration = %v, want %d (the current head)", v, CurrentSchemaVersion())
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
	// Idempotent: a current-version document comes back byte-identical.
	current := strings.Replace(v5Doc, `"version":5`, fmt.Sprintf(`"version":%d`, CurrentSchemaVersion()), 1)
	out, err := migrateDocument([]byte(current))
	if err != nil || string(out) != current {
		t.Fatalf("a current-version document must pass through unchanged (err %v)", err)
	}
}

// Load round-trips the catalogue into the typed Config.
func TestLoad_V4CatalogueRoundTrip(t *testing.T) {
	p := writeTempConfig(t, v4Doc)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != CurrentSchemaVersion() {
		t.Fatalf("Version = %d, want %d (a v4 file migrates up at load)", cfg.Version, CurrentSchemaVersion())
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

// v5 (W-0021 slice 2C review): `qso_archives[].request_key` makes archive
// creation idempotent across restarts. 4→5 stamps only.
const v5Doc = `{"version":5,"data_dir":"/tmp/x","setup_complete":true,"default_logbook_id":1,
	"logging_station":{"station_callsign":"G4ABC"},
	"qso_archives":[{"id":"019fd5c5-efcc-7193-be4f-1fee532ee316","label":"Contest","ownership":"managed","request_key":"req-1"}]}`

func TestMigrateV4toV5_StampsOnly(t *testing.T) {
	m := migratedMap(t, v4Doc)
	if v := m["version"]; v != float64(CurrentSchemaVersion()) {
		t.Fatalf("version after migration = %v, want %d", v, CurrentSchemaVersion())
	}
	cfg, err := Load(writeTempConfig(t, v5Doc))
	if err != nil || cfg.QsoArchives[0].RequestKey != "req-1" {
		t.Fatalf("Load v5: %v %+v", err, cfg.QsoArchives)
	}
}
