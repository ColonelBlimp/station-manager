package evidence

// evidence.db schema, version 4. Every row kind carries a UUIDv7 primary key
// and a synced flag (pure upload scheduling, §4.1), the v3 sync columns, and
// the v4 retention columns (retention-slice rulings 2026-08-10):
//
//   - sync_outcome: the exact terminal outcome, set with synced=1 —
//     synced alone cannot distinguish cloud-present (accepted /
//     already_present, the ONLY purge-eligible class) from tombstoned /
//     suppressed (terminal but NOT present remotely).
//   - loss_intervals.sealed: RT10 — an OPEN accumulator row is refreshed
//     in place and must never be sync-eligible; sealing freezes it, and no
//     offered or synced UUID may subsequently change content.
//   - supersedes (loss_intervals, retention_records): a compaction
//     summary's DIRECT predecessor UUIDs (JSON array, ≤ 64); NULL on plain
//     rows. retention_records are the §4.1 purge receipts — one immutable
//     record per purge chunk, committed with its deletions.
//
// v3 sync columns (§5 sync slice, operator rulings 2026-08-10):
//
//   - offered_at: conservative durable SEND-INTENT — NULL = never offered,
//     non-NULL = possibly offered, unacknowledged. Set with COALESCE before
//     dispatch, so a crash before bytes leave still reads as "possibly
//     offered"; the retention slice's three-valued loss taxonomy
//     (never_offered vs offered_unacknowledged) consumes exactly this.
//   - quarantine_reason: a permanent_reject outcome's reason — the row is
//     visible, never re-offered, never deleted. A quarantined row keeps
//     synced=0: it is unsynced AND refused, not synced.
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
const schemaVersion = "5"

const profileTablesSQL = `
CREATE TABLE IF NOT EXISTS profiles (
	uuid              TEXT PRIMARY KEY,
	lineage           TEXT NOT NULL,
	version           INTEGER NOT NULL,
	valid_from        TEXT NOT NULL,
	name              TEXT NOT NULL,
	type              TEXT,
	height_m          REAL,
	feedline          TEXT,
	locator           TEXT,
	bands             TEXT NOT NULL,
	noise_floor       TEXT NOT NULL DEFAULT 'not_measured',
	synced            INTEGER NOT NULL DEFAULT 0,
	offered_at        TEXT,
	quarantine_reason TEXT,
	sync_outcome      TEXT,
	UNIQUE(lineage, version)
);
CREATE TABLE IF NOT EXISTS profile_active (
	band         TEXT PRIMARY KEY,
	profile_uuid TEXT NOT NULL
);
`

// retentionTableSQL is shared by the fresh DDL and the 3→4 migration.
const retentionTableSQL = `
CREATE TABLE IF NOT EXISTS retention_records (
	uuid              TEXT PRIMARY KEY,
	start_utc         TEXT NOT NULL,
	end_utc           TEXT NOT NULL,
	observations      INTEGER NOT NULL,
	coverage          INTEGER NOT NULL,
	reason            TEXT NOT NULL,
	acknowledged      INTEGER NOT NULL,
	dial_mhz          REAL NOT NULL DEFAULT 0,
	supersedes        TEXT,
	synced            INTEGER NOT NULL DEFAULT 0,
	offered_at        TEXT,
	quarantine_reason TEXT,
	sync_outcome      TEXT
);
`

// schemaSQL creates a FRESH v3 archive (idempotent for an archive already at
// v3). Older archives migrate instead — Start dispatches on the stored
// schema_version and chains 1→2→3.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS schema_meta (
	k TEXT PRIMARY KEY,
	v TEXT NOT NULL
);
INSERT INTO schema_meta (k, v) VALUES ('schema_version', '5')
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
	synced                  INTEGER NOT NULL DEFAULT 0,
	offered_at              TEXT,
	quarantine_reason       TEXT,
	sync_outcome            TEXT
);
CREATE INDEX IF NOT EXISTS idx_observations_slot ON observations(slot_start_utc);

CREATE TABLE IF NOT EXISTS coverage (
	uuid           TEXT PRIMARY KEY,
	slot_start_utc TEXT NOT NULL,
	outcome        TEXT NOT NULL,
	dial_mhz       REAL NOT NULL,
	dial_tracked      INTEGER NOT NULL,
	decode_count      INTEGER NOT NULL,
	synced            INTEGER NOT NULL DEFAULT 0,
	offered_at        TEXT,
	quarantine_reason TEXT,
	sync_outcome      TEXT
);
CREATE INDEX IF NOT EXISTS idx_coverage_slot ON coverage(slot_start_utc);

CREATE TABLE IF NOT EXISTS loss_intervals (
	uuid          TEXT PRIMARY KEY,
	start_utc     TEXT NOT NULL,
	end_utc       TEXT NOT NULL,
	slots         INTEGER NOT NULL,
	observations  INTEGER NOT NULL,
	reason        TEXT NOT NULL,
	remote_status     TEXT NOT NULL,
	dial_mhz          REAL NOT NULL,
	synced            INTEGER NOT NULL DEFAULT 0,
	offered_at        TEXT,
	quarantine_reason TEXT,
	sync_outcome      TEXT,
	sealed            INTEGER NOT NULL DEFAULT 1,
	supersedes        TEXT
);
` + profileTablesSQL + retentionTableSQL

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

// migrate2to3SQL adds the sync columns ADDITIVELY: NULL on every existing
// row (never offered, never quarantined — exactly what was true of a pre-
// sync archive). Note: a v1 archive's 1→2 step creates the profiles table
// from the CURRENT profileTablesSQL, which already carries the v3 columns —
// so this migration touches only the three §4.1 tables plus, on a genuine
// v2 archive, the profiles table; the ADD COLUMN duplicates are split so
// each statement stays valid on the archive shape it runs against.
const migrate2to3SQL = `
ALTER TABLE observations ADD COLUMN offered_at TEXT;
ALTER TABLE observations ADD COLUMN quarantine_reason TEXT;
ALTER TABLE coverage ADD COLUMN offered_at TEXT;
ALTER TABLE coverage ADD COLUMN quarantine_reason TEXT;
ALTER TABLE loss_intervals ADD COLUMN offered_at TEXT;
ALTER TABLE loss_intervals ADD COLUMN quarantine_reason TEXT;
UPDATE schema_meta SET v = '3' WHERE k = 'schema_version';
`

// migrateProfiles2to3SQL is the profiles-table half of 2→3, applied only
// when the profiles table actually lacks the columns (a genuine v2 archive;
// a v1 archive got them via profileTablesSQL during 1→2).
const migrateProfiles2to3SQL = `
ALTER TABLE profiles ADD COLUMN offered_at TEXT;
ALTER TABLE profiles ADD COLUMN quarantine_reason TEXT;
`

// migrate3to4SQL adds the retention columns ADDITIVELY. Existing loss rows
// arrive sealed=1: pre-migration rows are frozen by definition (any open
// accumulator died with its process). The profiles half is conditional for
// the same reason as 2→3 (chained archives create profiles from the current
// DDL); retention_records is CREATE IF NOT EXISTS and needs no condition.
const migrate3to4SQL = `
ALTER TABLE observations ADD COLUMN sync_outcome TEXT;
ALTER TABLE coverage ADD COLUMN sync_outcome TEXT;
ALTER TABLE loss_intervals ADD COLUMN sync_outcome TEXT;
ALTER TABLE loss_intervals ADD COLUMN sealed INTEGER NOT NULL DEFAULT 1;
ALTER TABLE loss_intervals ADD COLUMN supersedes TEXT;
UPDATE observations SET sync_outcome = '` + legacySyncedOutcome + `' WHERE synced = 1;
UPDATE coverage SET sync_outcome = '` + legacySyncedOutcome + `' WHERE synced = 1;
UPDATE loss_intervals SET sync_outcome = '` + legacySyncedOutcome + `' WHERE synced = 1;
UPDATE profiles SET sync_outcome = '` + legacySyncedOutcome + `' WHERE synced = 1;
` + retentionTableSQL + `
UPDATE schema_meta SET v = '4' WHERE k = 'schema_version';
`

// legacySyncedOutcome backfills a v3 row already synced=1 (codex-P1 fix
// 2026-08-10): the exact outcome was never recorded, but a v3 client could
// only ever receive accepted/already_present — SMC had no tombstones until
// migration 0006 — so CLOUD-PRESENT is a sound class inference, the
// legacy_unprofiled precedent applied to sync. NULL would strand every
// upgraded synced row outside both purge classes and wedge the archive in
// drop_new at the watermark. Local-only: the wire never carries it.
const legacySyncedOutcome = "legacy_synced"

const migrateProfiles3to4SQL = `
ALTER TABLE profiles ADD COLUMN sync_outcome TEXT;
`

// migrate4to5SQL (package-review P1-4c, 2026-08-10): retention receipts
// carry dial context — §4.1's compaction criterion requires band/dial
// agreement for BOTH metadata kinds, and a receipt without it lets
// cross-band purges merge. Pre-v5 receipts arrive as 0: unattributed is
// honest for a receipt that never recorded its dial. v4 IS deployed
// (dogfood, 2026-08-10), so this is a real migration, never an in-place
// v4 edit. The ALTER is conditional in migrate4to5 (chained archives
// created the table from the current DDL).
const migrate4to5SQL = `
UPDATE schema_meta SET v = '5' WHERE k = 'schema_version';
`

const migrateRetention4to5SQL = `
ALTER TABLE retention_records ADD COLUMN dial_mhz REAL NOT NULL DEFAULT 0;
`
