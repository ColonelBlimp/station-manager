-- Reverse 0013: restore 0009's pair CHECK.
--
-- POLICY (the 0009 down's): rows of the two archive kinds are DELIBERATELY
-- DISCARDED — they cannot exist under the narrower CHECK — and every other row
-- is preserved with its id and detail. The AUTOINCREMENT high-water mark is
-- carried.

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
        (category = 'notification' AND kind IN ('export.adif_failed', 'forward.failed'))
        OR
        (category = 'alarm' AND kind IN ('tx_alarm.raised', 'tx_alarm.cleared',
                                         'drive_alarm.raised', 'drive_alarm.cleared',
                                         'tx.disarmed', 'session.terminated'))
    )
);

INSERT INTO operator_event (id, category, kind, severity, occurred_at, build, detail)
SELECT id, category, kind, severity, occurred_at, build, detail
FROM operator_event_old
WHERE kind NOT IN ('archive.activated', 'archive.activation_failed');

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
