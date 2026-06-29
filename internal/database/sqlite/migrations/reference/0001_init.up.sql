PRAGMA foreign_keys = ON;

-- Reference-domain schema (ADR: reference.db / log-db split). These two
-- enrichment caches are operator-global — shared across every log file —
-- so they live in reference.db rather than travelling with a log. Contest
-- dupe is a per-file qso-table query and does NOT read these tables.

CREATE TABLE IF NOT EXISTS contacted_station
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
    call              TEXT     NOT NULL CHECK (length(trim(call)) <= 20),
    country           TEXT     NOT NULL CHECK (length(trim(country)) <= 50),
    additional_data   JSON     NOT NULL DEFAULT ('{}') CHECK (json_valid(additional_data))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_contacted_station_active_call
    ON contacted_station (call)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS country
(
    id                INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    modified_at       DATETIME,
    deleted_at        DATETIME,
    -- Enrichment-cache freshness tracker (ADR 0017): timestamp of the
    -- most recent hamnut write. NULL means "never refreshed, treat as
    -- stale on first read." Read path uses this against the operator-
    -- configurable country TTL to branch cold/stale/fresh. Country is
    -- hamnut-exclusive — callsign-class providers never write here.
    last_refreshed_at DATETIME,
    name              TEXT     NOT NULL,
    cq_zone           TEXT     NOT NULL,
    itu_zone          TEXT     NOT NULL,
    continent         TEXT     NOT NULL,
    prefix            TEXT     NOT NULL UNIQUE CHECK (length(trim(prefix)) <= 20),
    ccode             TEXT     NOT NULL,
    dxcc_prefix       TEXT     NOT NULL,
    time_offset       TEXT     NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_country_name ON country (name);
