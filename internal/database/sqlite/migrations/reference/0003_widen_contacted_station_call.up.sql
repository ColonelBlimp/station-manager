-- Widen contacted_station.call from <= 20 to <= 32 (2026-07-22 sqlite review,
-- finding 3).
--
-- Log migration 0006 widened qso.call to 1..32 to match IsValidCallsign, but the
-- reference-domain enrichment cache kept the old <= 20 CHECK — the two columns
-- were split into separate migration sets before that widening, so 0006 could
-- only reach the log side. The result: a valid 21..32-char callsign logs fine but
-- every write to its enrichment cache row fails the CHECK. That failure is
-- best-effort (it never blocks logging, per the enrichment invariant), so it
-- surfaces only as a station that cold-misses its provider on EVERY lookup —
-- silent, repeated, and unbounded upstream traffic for those calls.
--
-- SQLite cannot ALTER a CHECK, so the table is rebuilt. contacted_station has no
-- inbound foreign keys and no triggers, so the rename-rebuild only has to carry
-- the partial unique index across. Column definitions are otherwise copied
-- verbatim from 0001 (including the localtime created_at default — changing
-- timestamp semantics is out of scope for a CHECK widening).

DROP INDEX IF EXISTS uq_contacted_station_active_call;

ALTER TABLE contacted_station RENAME TO contacted_station_old;

CREATE TABLE contacted_station
(
    id                INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    modified_at       DATETIME,
    deleted_at        DATETIME,
    -- Enrichment-cache freshness tracker (ADR 0017): timestamp of the
    -- most recent write from a callsign-class provider OR from a
    -- QSO-submit upsert. NULL means "never refreshed, treat as stale
    -- on first read." Read path uses this against the operator-
    -- configurable contacted_station TTL to branch cold/stale/fresh.
    last_refreshed_at DATETIME,
    name              TEXT     NOT NULL,
    call              TEXT     NOT NULL CHECK (length(trim(call)) <= 32),
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
