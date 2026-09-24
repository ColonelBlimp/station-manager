-- operator_event's `notification` category gains the archive switch outcomes
-- (ADR 0071, W-0021 activation follow-up): archive.activated and
-- archive.activation_failed, recorded by the daemon at the lifecycle boundary
-- with typed, bounded metadata (archive id, label, stable failure code — never
-- error text or a path).
--
-- Same rename-then-rebuild as 0009 (SQLite cannot ALTER a CHECK): every row
-- copies across with its id, the AUTOINCREMENT high-water mark is carried, the
-- severity set, the JSON detail rule, the index and the immutability trigger
-- are unchanged. The pair table remains internal/stationevents.KindsByCategory.

DROP TRIGGER IF EXISTS trg_operator_event_no_update;
DROP INDEX IF EXISTS idx_operator_event_category_id;

ALTER TABLE operator_event RENAME TO operator_event_old;

CREATE TABLE operator_event
(
    id          INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    category    TEXT     NOT NULL,
    kind        TEXT     NOT NULL,
    severity    TEXT     NOT NULL CHECK (severity IN ('info', 'warn', 'error')),
    occurred_at DATETIME NOT NULL DEFAULT (datetime('now')),
    build       TEXT     NOT NULL,
    detail      JSON     NOT NULL DEFAULT ('{}') CHECK (json_valid(detail)),
    CONSTRAINT ck_operator_event_category_kind CHECK (
        (category = 'notification' AND kind IN ('export.adif_failed', 'forward.failed',
                                                'archive.activated', 'archive.activation_failed'))
        OR
        (category = 'alarm' AND kind IN ('tx_alarm.raised', 'tx_alarm.cleared',
                                         'drive_alarm.raised', 'drive_alarm.cleared',
                                         'tx.disarmed', 'session.terminated'))
    )
);

INSERT INTO operator_event (id, category, kind, severity, occurred_at, build, detail)
SELECT id, category, kind, severity, occurred_at, build, detail
FROM operator_event_old;

CREATE TEMP TABLE operator_event_seq AS
SELECT MAX(seq) AS seq FROM sqlite_sequence WHERE name IN ('operator_event', 'operator_event_old');
DELETE FROM sqlite_sequence WHERE name IN ('operator_event', 'operator_event_old');
INSERT INTO sqlite_sequence (name, seq)
SELECT 'operator_event', seq FROM operator_event_seq WHERE seq IS NOT NULL;
DROP TABLE operator_event_seq;

DROP TABLE operator_event_old;

CREATE INDEX IF NOT EXISTS idx_operator_event_category_id
    ON operator_event (category, id);

CREATE TRIGGER IF NOT EXISTS trg_operator_event_no_update
    BEFORE UPDATE
    ON operator_event
BEGIN
    SELECT RAISE(ABORT, 'operator_event is immutable — UPDATE not permitted (retention prunes via DELETE)');
END;
