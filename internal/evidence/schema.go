package evidence

// schemaSQL is evidence.db's v1 schema. Every row kind carries a UUIDv7
// primary key and a synced flag (pure upload scheduling, §4.1 — unused until
// the sync slice) so the sync layer needs no migration to adopt them.
// profile_uuid is nullable by design: NULL means "no profile was recorded",
// never "pending" (§5.4 amendment, operator 2026-08-10).
const schemaSQL = `
CREATE TABLE IF NOT EXISTS schema_meta (
	k TEXT PRIMARY KEY,
	v TEXT NOT NULL
);
INSERT INTO schema_meta (k, v) VALUES ('schema_version', '1')
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
	synced                  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_observations_slot ON observations(slot_start_utc);

CREATE TABLE IF NOT EXISTS coverage (
	uuid           TEXT PRIMARY KEY,
	slot_start_utc TEXT NOT NULL,
	outcome        TEXT NOT NULL,
	dial_mhz       REAL NOT NULL,
	dial_tracked   INTEGER NOT NULL,
	decode_count   INTEGER NOT NULL,
	synced         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_coverage_slot ON coverage(slot_start_utc);

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
`
