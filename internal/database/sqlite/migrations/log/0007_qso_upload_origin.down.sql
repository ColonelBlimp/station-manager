-- Reverse 0007: drop qso_upload.origin, restoring the 0006 table shape exactly.
--
-- Same rename-then-rebuild, and the same three named schema objects must be
-- dropped before the rename and recreated after, or they follow qso_upload_old
-- into oblivion. Every pre-0007 column is copied by name — the explicit list is
-- where a rebuild silently loses data.

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
    CONSTRAINT uq_qso_forwarder_action UNIQUE (qso_id, forwarder_name, action),
    CONSTRAINT fk_qso_upload_qso FOREIGN KEY (qso_id) REFERENCES qso (id) ON DELETE CASCADE
);

INSERT INTO qso_upload (
    id, created_at, modified_at, qso_id, forwarder_name, forwarder_type,
    action, status, attempts, last_attempt_at, next_attempt_at, last_error, upstream_id
)
SELECT
    id, created_at, modified_at, qso_id, forwarder_name, forwarder_type,
    action, status, attempts, last_attempt_at, next_attempt_at, last_error, upstream_id
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
