-- legacy_name is the config.json forwarder name a seeded binding derives from —
-- the ONE name an older, config-driven build drains — recorded on every row the
-- seed creates, so a downgrade collapses queue names to it instead of inferring
-- it from the current default logbook or from sort order (ADR 0082 dated update
-- 2026-09-25; Codex P2 on c3df0e12). NULL on a binding created after the seed.
--
-- A REBUILD, not ADD COLUMN: two shapes of schema 14 exist — the c3df0e12 build's
-- table without the column and the b1231672 build's table with it (that build
-- carried it inside 0014) — and SQLite cannot add a column conditionally
-- (Codex P1 on ea56170f). Rename-then-rebuild reaches one final shape from
-- either: every row copies across with its id, the AUTOINCREMENT high-water
-- mark is carried, the constraints are restated. The column's content from the
-- second shape is not carried: that build wired no seed, so no real file of
-- that shape holds a seeded binding.

ALTER TABLE logbook_destination RENAME TO logbook_destination_old;

CREATE TABLE logbook_destination
(
    id                INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    modified_at       DATETIME,
    logbook_id        INTEGER  NOT NULL REFERENCES logbook (id) ON DELETE CASCADE,
    destination       TEXT     NOT NULL,
    forwarder_name    TEXT     NOT NULL UNIQUE,
    enabled           INTEGER  NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    credentials       TEXT,
    remote_adopted_at DATETIME,
    legacy_name       TEXT,
    CONSTRAINT uq_logbook_destination UNIQUE (logbook_id, destination)
);

INSERT INTO logbook_destination (id, created_at, modified_at, logbook_id, destination, forwarder_name,
                                 enabled, credentials, remote_adopted_at)
SELECT id, created_at, modified_at, logbook_id, destination, forwarder_name,
       enabled, credentials, remote_adopted_at
FROM logbook_destination_old;

CREATE TEMP TABLE logbook_destination_seq AS
SELECT MAX(seq) AS seq FROM sqlite_sequence WHERE name IN ('logbook_destination', 'logbook_destination_old');
DELETE FROM sqlite_sequence WHERE name IN ('logbook_destination', 'logbook_destination_old');
INSERT INTO sqlite_sequence (name, seq)
SELECT 'logbook_destination', seq FROM logbook_destination_seq WHERE seq IS NOT NULL;
DROP TABLE logbook_destination_seq;

DROP TABLE logbook_destination_old;
