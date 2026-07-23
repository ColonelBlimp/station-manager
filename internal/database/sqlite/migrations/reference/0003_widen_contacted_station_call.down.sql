-- Revert 0003: restore the narrower contacted_station.call (<= 20) CHECK.
--
-- This rollback FAILS (by design) if any cached row now holds a call longer than
-- 20 chars — a schema cannot narrow below the data it already contains. Because
-- contacted_station is a rebuildable enrichment cache, the fix is to delete the
-- offending rows (they re-cache on the next cold miss), not to rescue them.

DROP INDEX IF EXISTS uq_contacted_station_active_call;

ALTER TABLE contacted_station RENAME TO contacted_station_old;

CREATE TABLE contacted_station
(
    id                INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    modified_at       DATETIME,
    deleted_at        DATETIME,
    last_refreshed_at DATETIME,
    name              TEXT     NOT NULL,
    call              TEXT     NOT NULL CHECK (length(trim(call)) <= 20),
    country           TEXT     NOT NULL CHECK (length(trim(country)) <= 50),
    additional_data   JSON     NOT NULL DEFAULT ('{}') CHECK (json_valid(additional_data))
);

INSERT INTO contacted_station (
    id, created_at, modified_at, deleted_at, last_refreshed_at,
    name, call, country, additional_data
)
SELECT
    id, created_at, modified_at, deleted_at, last_refreshed_at,
    name, call, country, additional_data
FROM contacted_station_old;

DROP TABLE contacted_station_old;

CREATE UNIQUE INDEX IF NOT EXISTS uq_contacted_station_active_call
    ON contacted_station (call)
    WHERE deleted_at IS NULL;
