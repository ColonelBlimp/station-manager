-- The QSO file becomes a self-identifying ARCHIVE (ADR 0071, W-0021 slice 1).
--
-- archive_metadata is a singleton row: the archive's immutable UUIDv7, its
-- creation time and its database-local default logbook. config.json's catalogue
-- carries the same UUID; the file is the authority when they disagree, and a
-- catalogue entry whose file holds a different UUID fails closed.
--
-- Both UUID columns are NULLABLE here because SQLite cannot mint a UUIDv7: the
-- daemon writes the identity row and backfills logbook.uuid once, in one
-- transaction, after Migrate(), and mints a uuid on every runtime logbook
-- insert (ruling (b), 2026-09-22). A file migrated by this step but never
-- started by the daemon simply has no identity yet; nothing is inferred.
--
-- default_logbook_id moves here from config.json (which keeps a projection of
-- the active archive's value). SET NULL on delete keeps the identity row valid
-- if the default logbook is ever removed; the API refuses that today.

CREATE TABLE IF NOT EXISTS archive_metadata
(
    singleton          INTEGER  NOT NULL PRIMARY KEY CHECK (singleton = 1),
    archive_uuid       TEXT     NOT NULL UNIQUE CHECK (length(archive_uuid) = 36),
    created_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    default_logbook_id INTEGER  REFERENCES logbook (id) ON DELETE SET NULL
);

ALTER TABLE logbook ADD COLUMN uuid TEXT CHECK (uuid IS NULL OR length(uuid) = 36);

-- Unique when present; NULL rows (pre-backfill, or inserted by an older build
-- before the daemon's next start) do not collide.
CREATE UNIQUE INDEX IF NOT EXISTS idx_logbook_uuid ON logbook (uuid) WHERE uuid IS NOT NULL;
