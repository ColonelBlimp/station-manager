package evidence

// evidence.db schema, version 2. Every row kind carries a UUIDv7 primary key
// and a synced flag (pure upload scheduling, §4.1 — unused until the sync
// slice) so the sync layer needs no migration to adopt them.
//
// profile_uuid / unprofiled_reason (§4.2, operator rulings 2026-08-10):
// exactly one is set on every observation — NULL profile means "no profile
// was recorded", never "pending" (§5.4), and the reason says why, per row,
// so the answer survives retention and needs no counter persistence. The
// pairing is enforced by the writer (a CHECK would diverge between fresh
// and migrated archives — SQLite cannot ADD a constraint to an existing
// table).
//
// The profiles table is append-only lineage history: (lineage, version)
// unique, rows never updated — an edit mints the next version
// (profiles.go). bands is the canonical sorted comma-joined ADIF band set
// the version governed — a pinned FACT (codex-P1 ruling 2026-08-10): a
// membership change mints, so which declaration governed any historical
// observation stays reconstructable; band ORDER is normalized away and
// never mints. profile_active is the current band → version mapping,
// rebuilt atomically at each activation; it is reconciliation-internal
// state (asserted only through Status) but persisted so a later boot can
// tell a retired lineage from an unchanged one (re-add must mint), and so
// the archive stays self-describing for sync.
const schemaVersion = "2"

const profileTablesSQL = `
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

// schemaSQL creates a FRESH v2 archive (idempotent for an archive already at
// v2). A v1 archive must go through migrate1to2SQL instead — Start dispatches
// on the stored schema_version.
const schemaSQL = `
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
` + profileTablesSQL

// migrate1to2SQL adopts a v1 archive ADDITIVELY (PR9): the reason column and
// profile tables are added, pre-existing NULL profile references backfill
// legacy_unprofiled — their NULL predates the feature and must not claim an
// operator choice (no_declaration is reserved for rows written after
// adoption) — and no existing row is otherwise touched.
const migrate1to2SQL = `
ALTER TABLE observations ADD COLUMN unprofiled_reason TEXT;
UPDATE observations SET unprofiled_reason = '` + ReasonLegacyUnprofiled + `'
	WHERE profile_uuid IS NULL;
` + profileTablesSQL + `
UPDATE schema_meta SET v = '2' WHERE k = 'schema_version';
`
