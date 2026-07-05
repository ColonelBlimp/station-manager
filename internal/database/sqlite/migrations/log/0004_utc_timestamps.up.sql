-- Store DATETIME columns as canonical UTC (finding 1 of the 2026-07-05
-- internal/database review; fix-forward half — _time_format=sqlite + UTC Go
-- writers — shipped separately).
--
-- Two SQL-side writers still stamped LOCAL time, which modernc scans back as UTC
-- and so reads 2 h off (CAT = UTC+2):
--   * the `datetime('now','localtime')` DEFAULTs on qso_upload.created_at and
--     qso_history.at (qso.created_at's default is dead — sqlboiler always supplies
--     a UTC value — but it is fixed here too for robustness), and
--   * the trg_qso_set_modified_at / trg_qso_upload_set_updated_at triggers.
-- sqlboiler (created_at / deleted_at) and the Go writers (modified_at) already
-- store UTC, so those columns only carry LOCAL values in PRE-fix-forward debris
-- rows.
--
-- This migration: (a) rebuilds the three tables with `datetime('now')` (UTC)
-- defaults and recreates the two stamp triggers with UTC (same rename-then-rebuild
-- shape as 0002/0003 — SQLite can't ALTER a default or a trigger body), and
-- (b) NORMALISES every existing DATETIME value to UTC during the copy.
--
-- Normalisation is single-timezone (CAT, UTC+2, no DST) — verified safe because the
-- dogfood DB (7Q5MLV) is the ONLY database with pre-fix rows; 7Q8AC and any later
-- install start on the fix-forward build (UTC from row one), so no blanket-shift DB
-- ever carries mixed-offset history. THREE value classes — the first two are
-- already-UTC and must NOT be shifted, only the third moves:
--   * `…+00:00` (post-fix sqlboiler + Go writers, canonical) → leave verbatim;
--   * `… +0000 UTC` (PRE-fix sqlboiler `created_at`/`deleted_at` — `boil`'s timestamp
--     location is UTC, but modernc's pre-`_time_format` default rendered it via Go's
--     time.Time.String()). Its first 19 chars are ALREADY the UTC wall time, so
--     `datetime(substr(v,1,19))` reformats it canonically WITHOUT a shift. (Missing
--     this arm was a bug caught in staged review — the −2h arm would have made every
--     pre-fix created_at 2 h wrong.)
--   * anything else — naive-local from a DEFAULT/trigger, OR pre-fix Go modified_at
--     debris `… +0200 CAT m=+…` — has a LOCAL wall-time prefix, so
--     `datetime(substr(v,1,19),'-2 hours')` yields UTC.
-- The reformat/shift arms drop sub-second precision — acceptable for historical rows
-- (dedupe is minute-precision) and correct for reconcile (instant match).

DROP TRIGGER IF EXISTS trg_qso_history_no_delete;
DROP TRIGGER IF EXISTS trg_qso_history_no_update;
DROP TRIGGER IF EXISTS trg_qso_upload_set_updated_at;
DROP TRIGGER IF EXISTS trg_qso_set_modified_at;

DROP INDEX IF EXISTS idx_qso_history_qso_uuid;
DROP INDEX IF EXISTS idx_qso_upload_uploaded;
DROP INDEX IF EXISTS idx_qso_upload_pending;
DROP INDEX IF EXISTS idx_qso_logbook_date_time;
DROP INDEX IF EXISTS idx_qso_active_date_time;
DROP INDEX IF EXISTS idx_qso_active_call;
DROP INDEX IF EXISTS uq_qso_logbook_dedupe;
DROP INDEX IF EXISTS idx_qso_logbook_id;
DROP INDEX IF EXISTS idx_qso_date_time;
DROP INDEX IF EXISTS idx_qso_country;
DROP INDEX IF EXISTS idx_qso_band;
DROP INDEX IF EXISTS idx_qso_call;

ALTER TABLE qso RENAME TO qso_old;

CREATE TABLE qso
(
    id              INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    modified_at     DATETIME,
    deleted_at      DATETIME,

    uuid            TEXT     NOT NULL UNIQUE CHECK (
        length(uuid) = 36 AND
        substr(uuid, 9, 1) = '-' AND
        substr(uuid, 14, 1) = '-' AND
        substr(uuid, 15, 1) = '7' AND
        substr(uuid, 19, 1) = '-' AND
        substr(uuid, 24, 1) = '-'
    ),

    call            TEXT     NOT NULL CHECK (length(trim(call)) BETWEEN 1 AND 20),
    band            TEXT     NOT NULL CHECK (length(trim(band)) <= 10),
    mode            TEXT     NOT NULL CHECK (length(trim(mode)) <= 10),
    freq            INTEGER  NOT NULL CHECK (freq >= 0 AND freq <= 99999999),
    qso_date        TEXT     NOT NULL CHECK (
        length(qso_date) = 8 AND
        substr(qso_date, 1, 4) BETWEEN '0000' AND '9999' AND
        substr(qso_date, 5, 2) BETWEEN '01' AND '12' AND
        substr(qso_date, 7, 2) BETWEEN '01' AND '31' AND
        date(substr(qso_date, 1, 4) || '-' || substr(qso_date, 5, 2) || '-' || substr(qso_date, 7, 2)) IS NOT NULL
        ),
    time_on         TEXT     NOT NULL CHECK (
        (length(time_on) = 4 AND substr(time_on, 1, 2) BETWEEN '00' AND '23' AND
         substr(time_on, 3, 2) BETWEEN '00' AND '59')
        OR
        (length(time_on) = 6 AND substr(time_on, 1, 2) BETWEEN '00' AND '23' AND
         substr(time_on, 3, 2) BETWEEN '00' AND '59' AND
         substr(time_on, 5, 2) BETWEEN '00' AND '59')
        ),
    time_off        TEXT     NOT NULL CHECK (
        (length(time_off) = 4 AND substr(time_off, 1, 2) BETWEEN '00' AND '23' AND
         substr(time_off, 3, 2) BETWEEN '00' AND '59')
        OR
        (length(time_off) = 6 AND substr(time_off, 1, 2) BETWEEN '00' AND '23' AND
         substr(time_off, 3, 2) BETWEEN '00' AND '59' AND
         substr(time_off, 5, 2) BETWEEN '00' AND '59')
        ),
    rst_sent        TEXT     NOT NULL CHECK (length(rst_sent) <= 10),
    rst_rcvd        TEXT     NOT NULL CHECK (length(rst_rcvd) <= 10),
    country         TEXT     NOT NULL CHECK (length(trim(country)) <= 50),
    additional_data JSON     NOT NULL DEFAULT ('{}') CHECK (json_valid(additional_data)),
    dedupe_key      TEXT     NOT NULL CHECK (length(dedupe_key) = 64),

    logbook_id      INTEGER  NOT NULL,

    CONSTRAINT fk_qso_logbook_id FOREIGN KEY (logbook_id) REFERENCES logbook (id) ON DELETE RESTRICT ON UPDATE NO ACTION
);

INSERT INTO qso (
    id, created_at, modified_at, deleted_at, uuid, call, band, mode, freq,
    qso_date, time_on, time_off, rst_sent, rst_rcvd, country,
    additional_data, dedupe_key, logbook_id
)
SELECT
    id,
    CASE WHEN created_at  IS NULL OR created_at  LIKE '%+00:00'  THEN created_at
         WHEN created_at  LIKE '% +0000 UTC%'                    THEN datetime(substr(created_at,  1, 19))
         ELSE datetime(substr(created_at,  1, 19), '-2 hours') END,
    CASE WHEN modified_at IS NULL OR modified_at LIKE '%+00:00'  THEN modified_at
         WHEN modified_at LIKE '% +0000 UTC%'                    THEN datetime(substr(modified_at, 1, 19))
         ELSE datetime(substr(modified_at, 1, 19), '-2 hours') END,
    CASE WHEN deleted_at  IS NULL OR deleted_at  LIKE '%+00:00'  THEN deleted_at
         WHEN deleted_at  LIKE '% +0000 UTC%'                    THEN datetime(substr(deleted_at,  1, 19))
         ELSE datetime(substr(deleted_at,  1, 19), '-2 hours') END,
    uuid, call, band, mode, freq,
    qso_date, time_on, time_off, rst_sent, rst_rcvd, country,
    additional_data, dedupe_key, logbook_id
FROM qso_old;

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

-- last_attempt_at / next_attempt_at are INTEGER unix seconds (timezone-independent) — copied verbatim.
INSERT INTO qso_upload (
    id, created_at, modified_at, qso_id, forwarder_name, forwarder_type,
    action, status, attempts, last_attempt_at, next_attempt_at, last_error, upstream_id
)
SELECT
    id,
    CASE WHEN created_at  IS NULL OR created_at  LIKE '%+00:00'  THEN created_at
         WHEN created_at  LIKE '% +0000 UTC%'                    THEN datetime(substr(created_at,  1, 19))
         ELSE datetime(substr(created_at,  1, 19), '-2 hours') END,
    CASE WHEN modified_at IS NULL OR modified_at LIKE '%+00:00'  THEN modified_at
         WHEN modified_at LIKE '% +0000 UTC%'                    THEN datetime(substr(modified_at, 1, 19))
         ELSE datetime(substr(modified_at, 1, 19), '-2 hours') END,
    qso_id, forwarder_name, forwarder_type,
    action, status, attempts, last_attempt_at, next_attempt_at, last_error, upstream_id
FROM qso_upload_old;

DROP TABLE qso_upload_old;

ALTER TABLE qso_history RENAME TO qso_history_old;

CREATE TABLE qso_history
(
    id           INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    qso_uuid     TEXT     NOT NULL,
    op           TEXT     NOT NULL CHECK (op IN ('update', 'delete')),
    at           DATETIME NOT NULL DEFAULT (datetime('now')),
    source       TEXT     NOT NULL,
    before_image JSON     NOT NULL CHECK (json_valid(before_image)),
    CONSTRAINT fk_qso_history_qso FOREIGN KEY (qso_uuid)
        REFERENCES qso (uuid) ON DELETE NO ACTION
);

-- Normalise `at` during the copy — the append-only trigger (recreated below) would
-- reject a post-copy UPDATE.
INSERT INTO qso_history (id, qso_uuid, op, at, source, before_image)
SELECT
    id, qso_uuid, op,
    CASE WHEN at LIKE '%+00:00'      THEN at
         WHEN at LIKE '% +0000 UTC%' THEN datetime(substr(at, 1, 19))
         ELSE datetime(substr(at, 1, 19), '-2 hours') END,
    source, before_image
FROM qso_history_old;

DROP TABLE qso_history_old;
DROP TABLE qso_old;

CREATE INDEX IF NOT EXISTS idx_qso_call ON qso (call);
CREATE INDEX IF NOT EXISTS idx_qso_band ON qso (band);
CREATE INDEX IF NOT EXISTS idx_qso_country ON qso (country);
CREATE INDEX IF NOT EXISTS idx_qso_date_time ON qso (qso_date, time_on);
CREATE INDEX IF NOT EXISTS idx_qso_logbook_id ON qso (logbook_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_qso_logbook_dedupe
    ON qso (logbook_id, dedupe_key)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_qso_active_call ON qso (call) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_qso_active_date_time ON qso (qso_date, time_on) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_qso_logbook_date_time
    ON qso (logbook_id, qso_date DESC, time_on DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE TRIGGER IF NOT EXISTS trg_qso_set_modified_at
    AFTER UPDATE
    ON qso
    FOR EACH ROW
    WHEN NEW.modified_at IS NULL OR NEW.modified_at = OLD.modified_at
BEGIN
    UPDATE qso SET modified_at = datetime('now') WHERE id = OLD.id;
END;

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

CREATE INDEX IF NOT EXISTS idx_qso_history_qso_uuid
    ON qso_history (qso_uuid, at);

CREATE TRIGGER IF NOT EXISTS trg_qso_history_no_update
    BEFORE UPDATE
    ON qso_history
BEGIN
    SELECT RAISE(ABORT, 'qso_history is append-only — UPDATE not permitted');
END;

CREATE TRIGGER IF NOT EXISTS trg_qso_history_no_delete
    BEFORE DELETE
    ON qso_history
BEGIN
    SELECT RAISE(ABORT, 'qso_history is append-only — DELETE not permitted');
END;
