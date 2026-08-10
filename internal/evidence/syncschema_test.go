package evidence

/*
   §5 sync slice — schema v3 (the sync columns). Full SY1–SY9 acceptance
   header in sync_test.go; this file pins the archive-shape half:

   V3a  offered_at is conservative durable SEND-INTENT (operator ruling
        2026-08-10): NULL = never offered; non-NULL = possibly offered,
        unacknowledged — set with COALESCE before dispatch, so a crash
        before bytes leave still reads as "possibly offered". The
        retention slice's three-valued loss taxonomy consumes exactly
        this distinction.
   V3b  quarantine_reason marks permanent_reject rows: visible, never
        re-offered, never deleted. Exactly-one-of with synced=1 is NOT a
        constraint (a quarantined row keeps synced=0 — it is unsynced AND
        refused).
   V3c  A v2 archive migrates ADDITIVELY to v3: both columns arrive NULL
        on every existing row, every row survives with its content
        (distinguishable from a reseeded archive by preserved UUIDs), and
        a v1 archive chains 1→2→3 in one Start under the same cap
        projection that gates 1→2.

   v2SchemaForTest below is the v2 DDL frozen VERBATIM at the moment v3
   was designed — it must never track schema.go.
*/

import (
	"database/sql"
	"testing"
)

const v2SchemaForTest = `
CREATE TABLE IF NOT EXISTS schema_meta (
	k TEXT PRIMARY KEY,
	v TEXT NOT NULL
);
INSERT INTO schema_meta (k, v) VALUES ('schema_version', '2')
	ON CONFLICT(k) DO NOTHING;

CREATE TABLE IF NOT EXISTS observations (
	uuid                    TEXT PRIMARY KEY,
	slot_start_utc          TEXT NOT NULL,
	dial_mhz                REAL NOT NULL,
	dial_tracked            INTEGER NOT NULL,
	freq_hz                 REAL NOT NULL,
	dt_sec                  REAL NOT NULL,
	snr                     INTEGER NOT NULL,
	payload                 BLOB NOT NULL,
	parse_status            TEXT NOT NULL,
	text                    TEXT,
	prov_algorithm          TEXT NOT NULL,
	prov_ap_profile         TEXT NOT NULL DEFAULT '',
	prov_ap_source          TEXT NOT NULL DEFAULT '',
	metric_sync             REAL NOT NULL,
	metric_hard_sync        INTEGER NOT NULL,
	metric_costas_geo       REAL NOT NULL,
	metric_costas_min_block REAL NOT NULL,
	metric_blocks           INTEGER NOT NULL,
	metric_hard_errors      INTEGER NOT NULL,
	metric_dmin             REAL NOT NULL,
	decoder_build           TEXT NOT NULL,
	profile_uuid            TEXT,
	unprofiled_reason       TEXT,
	synced                  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS coverage (
	uuid           TEXT PRIMARY KEY,
	slot_start_utc TEXT NOT NULL,
	outcome        TEXT NOT NULL,
	dial_mhz       REAL NOT NULL,
	dial_tracked   INTEGER NOT NULL,
	decode_count   INTEGER NOT NULL,
	synced         INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS loss_intervals (
	uuid          TEXT PRIMARY KEY,
	start_utc     TEXT NOT NULL,
	end_utc       TEXT NOT NULL,
	slots         INTEGER NOT NULL,
	observations  INTEGER NOT NULL,
	reason        TEXT NOT NULL,
	remote_status TEXT NOT NULL,
	dial_mhz      REAL NOT NULL,
	synced        INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS profiles (
	uuid        TEXT PRIMARY KEY,
	lineage     TEXT NOT NULL,
	version     INTEGER NOT NULL,
	valid_from  TEXT NOT NULL,
	name        TEXT NOT NULL,
	type        TEXT,
	height_m    REAL,
	feedline    TEXT,
	locator     TEXT,
	bands       TEXT NOT NULL,
	noise_floor TEXT NOT NULL DEFAULT 'not_measured',
	synced      INTEGER NOT NULL DEFAULT 0,
	UNIQUE(lineage, version)
);
CREATE TABLE IF NOT EXISTS profile_active (
	band         TEXT PRIMARY KEY,
	profile_uuid TEXT NOT NULL
);
`

func hasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	n := countRows(t, db,
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column)
	return n == 1
}

func TestMigration_V2ToV3AdditivePreservation(t *testing.T) {
	cfg := testConfig(t, true)

	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(v2SchemaForTest); err != nil {
		t.Fatalf("create v2 archive: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO profiles (uuid, lineage, version, valid_from, name, bands, synced)
		 VALUES ('v2-prof-a', 'DX Commander', 1, '2026-08-01T00:00:00Z', 'DX Commander', '80m', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`INSERT INTO observations (uuid, slot_start_utc, dial_mhz, dial_tracked, freq_hz,
			dt_sec, snr, payload, parse_status, text, prov_algorithm,
			metric_sync, metric_hard_sync, metric_costas_geo, metric_costas_min_block,
			metric_blocks, metric_hard_errors, metric_dmin, decoder_build, profile_uuid)
		 VALUES ('v2-obs-a', '2026-08-01T12:00:00Z', 3.573, 1, 1200, 0.1, -5, X'00', 'parsed',
			'CQ TEST', 'bp', 1, 1, 1, 1, 1, 0, 1, 'v0.9.0', 'v2-prof-a')`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	s := newRunning(t, cfg)
	s.Stop()

	db := openRaw(t, cfg.Path)
	var ver string
	if err := db.QueryRow(`SELECT v FROM schema_meta WHERE k = 'schema_version'`).Scan(&ver); err != nil || ver != "3" {
		t.Fatalf("V3c: schema_version = %q (err %v), want \"3\"", ver, err)
	}
	for _, table := range []string{"observations", "coverage", "loss_intervals", "profiles"} {
		for _, col := range []string{"offered_at", "quarantine_reason"} {
			if !hasColumn(t, db, table, col) {
				t.Fatalf("V3c: %s.%s missing after migration", table, col)
			}
		}
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM observations WHERE uuid = 'v2-obs-a' AND offered_at IS NULL AND quarantine_reason IS NULL`); n != 1 {
		t.Fatal("V3a/V3c: the v2 observation must survive with NULL sync columns — its NULL means never offered, never quarantined")
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM profiles WHERE uuid = 'v2-prof-a' AND synced = 1 AND offered_at IS NULL`); n != 1 {
		t.Fatal("V3c: the v2 profile must survive with its synced flag intact")
	}
}

func TestMigration_V1ChainsToV3(t *testing.T) {
	cfg := testConfig(t, true)
	raw, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(v1SchemaForTest); err != nil {
		t.Fatalf("create v1 archive: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO observations (uuid, slot_start_utc, dial_mhz, dial_tracked, freq_hz,
			dt_sec, snr, payload, parse_status, text, prov_algorithm,
			metric_sync, metric_hard_sync, metric_costas_geo, metric_costas_min_block,
			metric_blocks, metric_hard_errors, metric_dmin, decoder_build, profile_uuid)
		 VALUES ('v1-obs-chain', '2026-08-01T12:00:00Z', 14.074, 1, 1200, 0.1, -5, X'00', 'parsed',
			'CQ TEST', 'bp', 1, 1, 1, 1, 1, 0, 1, 'v0.8.0', NULL)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	s := newRunning(t, cfg)
	s.Stop()

	db := openRaw(t, cfg.Path)
	var ver string
	if err := db.QueryRow(`SELECT v FROM schema_meta WHERE k = 'schema_version'`).Scan(&ver); err != nil || ver != "3" {
		t.Fatalf("V3c: v1 archive after one Start: schema_version = %q (err %v), want \"3\" (1→2→3 chain)", ver, err)
	}
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM observations WHERE uuid = 'v1-obs-chain' AND unprofiled_reason = ? AND offered_at IS NULL`,
		ReasonLegacyUnprofiled); n != 1 {
		t.Fatal("V3c: the chained migration must keep 1→2's legacy backfill AND add v3's NULL sync columns")
	}
}
