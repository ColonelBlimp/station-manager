-- Add qso_upload.origin: WHAT CAUSED this queue entry to exist.
--
-- Distinct from `action`, which says what mutation is being forwarded. Without it
-- the daemon's loudest subsystem cannot explain its own volume: live logging, a
-- bulk import, an operator edit, a manual backfill, a stamp-sync mirror and a
-- reconcile repair all produce identical upload rows and identical log records
-- (docs/reviews/forwarding-logging-gaps.md F1).
--
-- NOT NULL with NO DEFAULT, and a closed CHECK. The absent default is deliberate:
-- a future raw or generated insert that omits provenance must fail LOUDLY rather
-- than silently acquiring a value nobody chose. (A DB-side default would also let
-- SQLBoiler treat the column as optional and omit a zero value — the opposite of
-- the invariant.) Pre-existing rows are copied with the literal 'legacy', which
-- no producer ever assigns.
--
-- SQLite cannot ALTER a CHECK, so this uses the house rename-then-rebuild shape
-- (0002/0004/0006). Unlike those it rebuilds a CHILD table, so the rebuild-all-
-- three dance they need for `qso` does not apply — but three named schema objects
-- hang off qso_upload and would otherwise stay attached to qso_upload_old and be
-- destroyed with it. They are dropped BEFORE the rename and recreated after:
--   * trg_qso_upload_set_updated_at  (modified_at maintenance)
--   * idx_qso_upload_pending         (partial: pending / in_progress)
--   * idx_qso_upload_uploaded        (partial: uploaded)
-- The unique (qso_id, forwarder_name, action) constraint is recreated with the
-- table itself.

DROP TRIGGER IF EXISTS trg_qso_upload_set_updated_at;
DROP INDEX IF EXISTS idx_qso_upload_uploaded;
DROP INDEX IF EXISTS idx_qso_upload_pending;

ALTER TABLE qso_upload RENAME TO qso_upload_old;

CREATE TABLE qso_upload
(
    id              INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    modified_at     DATETIME,
    qso_id          INTEGER  NOT NULL,
    forwarder_name  TEXT     NOT NULL,
    forwarder_type  TEXT     NOT NULL,
    action          TEXT     NOT NULL DEFAULT 'insert' CHECK (action IN ('insert', 'update', 'delete')),
    status          TEXT     NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'in_progress', 'uploaded', 'failed')),
    attempts        INTEGER  NOT NULL DEFAULT 0,
    last_attempt_at INTEGER,
    next_attempt_at INTEGER  NOT NULL DEFAULT (strftime('%s', 'now')),
    last_error      TEXT,
    upstream_id     TEXT,
    origin          TEXT     NOT NULL CHECK (origin IN ('live', 'import', 'edit', 'manual', 'stamp_sync', 'reconcile', 'legacy')),
    CONSTRAINT uq_qso_forwarder_action UNIQUE (qso_id, forwarder_name, action),
    CONSTRAINT fk_qso_upload_qso FOREIGN KEY (qso_id) REFERENCES qso (id) ON DELETE CASCADE
);

INSERT INTO qso_upload (
    id, created_at, modified_at, qso_id, forwarder_name, forwarder_type,
    action, status, attempts, last_attempt_at, next_attempt_at, last_error, upstream_id, origin
)
SELECT
    id, created_at, modified_at, qso_id, forwarder_name, forwarder_type,
    action, status, attempts, last_attempt_at, next_attempt_at, last_error, upstream_id, 'legacy'
FROM qso_upload_old;

DROP TABLE qso_upload_old;

CREATE TRIGGER IF NOT EXISTS trg_qso_upload_set_updated_at
    AFTER UPDATE
    ON qso_upload
    FOR EACH ROW
BEGIN
    UPDATE qso_upload
    SET modified_at = datetime('now')
    WHERE id = OLD.id;
END;

CREATE INDEX IF NOT EXISTS idx_qso_upload_pending
    ON qso_upload (forwarder_name, next_attempt_at)
    WHERE status IN ('pending', 'in_progress');

CREATE INDEX IF NOT EXISTS idx_qso_upload_uploaded
    ON qso_upload (forwarder_name, modified_at)
    WHERE status = 'uploaded';
