-- operator_event gains the `alarm` category and its six kinds (W-0020 Station
-- Events, operator rulings 2026-09-14; ADR 0076's shape, ADR 0061's alarm
-- pilot): tx_alarm.raised / tx_alarm.cleared, drive_alarm.raised /
-- drive_alarm.cleared, tx.disarmed, session.terminated.
--
-- The CHECK now enforces the valid (category, kind) PAIRS, not two independent
-- allowlists: `notification` + `tx_alarm.raised` is illegal, and so is `alarm`
-- + `forward.failed`. The pair table is internal/stationevents.KindsByCategory,
-- which the schema test enumerates in both directions.
--
-- SQLite cannot ALTER a CHECK, so this is a rename-then-rebuild of the one
-- table (the 0002/0006 shape; operator_event has no foreign keys in either
-- direction). Every 0008 row copies across unchanged with its id, and the
-- AUTOINCREMENT high-water mark is carried explicitly: an empty ring rebuilt
-- without it would restart at 1 and reissue an id retention already evicted,
-- and id is the monotonic arrival order both the ring and the SPA key on.
-- severity stays the closed {info, warn, error}; build NOT NULL with no
-- default; detail typed JSON only — the 0008 rules are unchanged.

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
FROM operator_event_old;

-- Carry the AUTOINCREMENT high-water mark. The rename moved the sequence row
-- to the old name; the copy above created one for the new name only if a row
-- existed. Take the larger of the two, then leave exactly one row.
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
