PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS logbook
(
    id          INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    modified_at DATETIME,
    deleted_at  DATETIME,

    -- Human metadata
    name        TEXT     NOT NULL UNIQUE CHECK (length(name) <= 64),
    /*
        The callsign associated with the logbook. This should be treated as the 'station_callsign' according to the
        ADIF standard (the one used 'on the air').
    */
    callsign    TEXT     NOT NULL CHECK (length(callsign) BETWEEN 3 AND 32),

    description TEXT
);

CREATE TABLE IF NOT EXISTS qso
(
    id              INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    modified_at     DATETIME,
    deleted_at      DATETIME,

    /* Globally-unique, time-ordered external identifier (UUIDv7 per RFC 9562).
       Daemon-generated at create time, never reused. The canonical external
       identifier across API responses, ADIF exports, and any future
       inter-daemon synchronisation (per ADR 0016 — SM Cloud deferred prep).
       The integer `id` PK above stays as the local storage key; `uuid` is
       what callers see. UNIQUE-indexed; format is the standard 36-char
       lowercase hex form (xxxxxxxx-xxxx-7xxx-yxxx-xxxxxxxxxxxx). */
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
    /* freq is in kHz, thus app MUST multiply by 1000 for MHz */
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
        ),
    time_off        TEXT     NOT NULL CHECK (
        (length(time_off) = 4 AND substr(time_off, 1, 2) BETWEEN '00' AND '23' AND
         substr(time_off, 3, 2) BETWEEN '00' AND '59')
        ),
    rst_sent        TEXT     NOT NULL CHECK (length(rst_sent) <= 3),
    rst_rcvd        TEXT     NOT NULL CHECK (length(rst_rcvd) <= 3),
    country         TEXT     NOT NULL CHECK (length(trim(country)) <= 50),
    additional_data JSON     NOT NULL DEFAULT ('{}') CHECK (json_valid(additional_data)),

    /* Dedupe key: SHA-256 hex of CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON
       (uppercased, pipe-separated; FREQ is the normalized integer-kHz string
       so MHz decimal variants collapse to the same hash). Used by the
       'submit' endpoint to detect duplicate QSOs. Unique per logbook
       (active rows only). */
    dedupe_key      TEXT     NOT NULL CHECK (length(dedupe_key) = 64),

    logbook_id      INTEGER  NOT NULL,

    -- Note: no CHECK constraint preventing promoted fields from appearing in
    -- additional_data. The adapter strategy (docs/v1-analysis/design-decisions-log.md)
    -- deliberately duplicates promoted fields in the JSON blob via json.Marshal(qso).
    -- Columns are authoritative on read; the blob carries everything for round-trip
    -- fidelity.
    CONSTRAINT fk_qso_logbook_id FOREIGN KEY (logbook_id) REFERENCES logbook (id) ON DELETE RESTRICT ON UPDATE NO ACTION
);

CREATE INDEX IF NOT EXISTS idx_qso_call ON qso (call);
CREATE INDEX IF NOT EXISTS idx_qso_band ON qso (band);
CREATE INDEX IF NOT EXISTS idx_qso_country ON qso (country);
CREATE INDEX IF NOT EXISTS idx_qso_date_time ON qso (qso_date, time_on);
-- Index on the FK column for joins/deletes
CREATE INDEX IF NOT EXISTS idx_qso_logbook_id ON qso (logbook_id);

-- Dedupe: unique per logbook among active (non-deleted) rows
CREATE UNIQUE INDEX IF NOT EXISTS uq_qso_logbook_dedupe
    ON qso (logbook_id, dedupe_key)
    WHERE deleted_at IS NULL;

-- Optional partial indexes to speed queries that ignore soft-deleted rows
CREATE INDEX IF NOT EXISTS idx_qso_active_call ON qso (call) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_qso_active_date_time ON qso (qso_date, time_on) WHERE deleted_at IS NULL;

-- Composite index for cursor-based QSO list pagination
-- (GET /v1/logbook/{id}/qso). Query shape:
--   WHERE logbook_id = ? AND deleted_at IS NULL
--     AND (qso_date, time_on, id) < (?, ?, ?)   -- cursor predicate
--   ORDER BY qso_date DESC, time_on DESC, id DESC
--   LIMIT ?+1
-- Without this index SQLite picks idx_qso_logbook_id or
-- idx_qso_active_date_time then does an in-memory sort of the filtered
-- set. This composite lets the planner seek directly to the cursor
-- position and walk the index in sorted order — O(limit) rather than
-- O(rows in logbook).
CREATE INDEX IF NOT EXISTS idx_qso_logbook_date_time
    ON qso (logbook_id, qso_date DESC, time_on DESC, id DESC)
    WHERE deleted_at IS NULL;

-- Trigger to set modified_at on updates (safe pattern: update the row after the user's update)
CREATE TRIGGER IF NOT EXISTS trg_qso_set_modified_at
    AFTER UPDATE
    ON qso
    FOR EACH ROW
    WHEN NEW.modified_at IS NULL OR NEW.modified_at = OLD.modified_at
BEGIN
    UPDATE qso SET modified_at = datetime('now', 'localtime') WHERE id = OLD.id;
END;

CREATE TABLE IF NOT EXISTS contacted_station
(
    id              INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    modified_at     DATETIME,
    deleted_at      DATETIME,
    name            TEXT     NOT NULL,
    call            TEXT     NOT NULL CHECK (length(trim(call)) <= 20),
    country         TEXT     NOT NULL CHECK (length(trim(country)) <= 50),
    additional_data JSON     NOT NULL DEFAULT ('{}') CHECK (json_valid(additional_data))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_contacted_station_active_call
    ON contacted_station (call)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS country
(
    id          INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    modified_at DATETIME,
    deleted_at  DATETIME,
    name        TEXT     NOT NULL,
    cq_zone     TEXT     NOT NULL,
    itu_zone    TEXT     NOT NULL,
    continent   TEXT     NOT NULL,
    prefix      TEXT     NOT NULL UNIQUE CHECK (length(trim(prefix)) <= 20),
    ccode       TEXT     NOT NULL,
    dxcc_prefix TEXT     NOT NULL,
    time_offset TEXT     NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_country_name ON country (name);

-- qso_upload — forwarding queue. One row per (QSO, forwarder, action) triple.
-- See docs/v2-design/forwarding.md §6 for the row-shape rationale.
-- forwarder_name is the per-instance handle (matches config.forwarders[].name);
-- forwarder_type is the plugin identifier (matches Forwarder.Type()). Storing
-- both keeps rows interpretable even if the instance is later deleted from config.
CREATE TABLE IF NOT EXISTS qso_upload
(
    id              INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    modified_at     DATETIME,
    qso_id          INTEGER  NOT NULL,
    forwarder_name  TEXT     NOT NULL,
    forwarder_type  TEXT     NOT NULL,
    action          TEXT     NOT NULL DEFAULT 'insert' CHECK (action IN ('insert', 'update', 'delete')),
    status          TEXT     NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'in_progress', 'uploaded', 'failed')),
    attempts        INTEGER  NOT NULL DEFAULT 0,
    last_attempt_at INTEGER, -- Unix time; diagnostic only, workers do not read this
    next_attempt_at INTEGER  NOT NULL DEFAULT (strftime('%s', 'now')), -- Unix time; load-bearing for the claim query
    last_error      TEXT,
    upstream_id     TEXT,    -- optional; set from Result.UpstreamID on success
    CONSTRAINT uq_qso_forwarder_action UNIQUE (qso_id, forwarder_name, action),
    CONSTRAINT fk_qso_upload_qso FOREIGN KEY (qso_id) REFERENCES qso (id) ON DELETE CASCADE
);

-- Keep modified_at fresh on updates
CREATE TRIGGER IF NOT EXISTS trg_qso_upload_set_updated_at
    AFTER UPDATE
    ON qso_upload
    FOR EACH ROW
BEGIN
    UPDATE qso_upload
    SET modified_at = datetime('now', 'localtime')
    WHERE id = OLD.id;
END;

-- Pending work per forwarder, ordered by next_attempt_at so retries don't jump
-- the queue. Filtered to pending/in_progress to keep the index small.
CREATE INDEX IF NOT EXISTS idx_qso_upload_pending
    ON qso_upload (forwarder_name, next_attempt_at)
    WHERE status IN ('pending', 'in_progress');

-- Fast lookup of uploaded rows per forwarder (for operator-facing "recent successful
-- uploads" views; not on the worker's hot path)
CREATE INDEX IF NOT EXISTS idx_qso_upload_uploaded
    ON qso_upload (forwarder_name, modified_at)
    WHERE status = 'uploaded';

-- qso_history — append-only audit table for QSO mutations (ADR 0016 prep #2).
-- Captures the pre-edit/pre-delete snapshot for every UPDATE and DELETE on a
-- QSO. INSERTs are deliberately not audited here — initial-insert provenance
-- already lives on qso.additional_data via the source-tagging pass from
-- ADR 0014 prep #4. The before_image is the full json.Marshal(types.Qso) of
-- the row prior to mutation; we keep the whole snapshot rather than a diff
-- because at personal-operator scale the storage cost is negligible and a
-- complete snapshot is trivial to replay or export.
--
-- FK is by qso_uuid (not qso.id): UUID is the canonical external identifier
-- and survives any internal renumbering or cross-daemon synchronisation
-- (the same shape SM Cloud will need). ON DELETE NO ACTION is intentional —
-- audit rows must outlive their QSO; deleting a QSO soft-deletes (sets
-- deleted_at) rather than removing the row, so the FK stays satisfied.
--
-- Append-only is enforced by BEFORE UPDATE / BEFORE DELETE triggers below.
-- This is belt-and-braces on top of "the daemon never UPDATEs/DELETEs this
-- table"; the triggers also prevent accidental damage from manual sqlite3
-- sessions.
CREATE TABLE IF NOT EXISTS qso_history
(
    id           INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    qso_uuid     TEXT     NOT NULL,
    op           TEXT     NOT NULL CHECK (op IN ('update', 'delete')),
    at           DATETIME NOT NULL DEFAULT (datetime('now', 'localtime')),
    source       TEXT     NOT NULL,
    before_image JSON     NOT NULL CHECK (json_valid(before_image)),
    CONSTRAINT fk_qso_history_qso FOREIGN KEY (qso_uuid)
        REFERENCES qso (uuid) ON DELETE NO ACTION
);

-- Lookup by QSO, time-ordered — operator-facing "show edit history for this
-- QSO" reads, plus future SM Cloud sync queries.
CREATE INDEX IF NOT EXISTS idx_qso_history_qso_uuid
    ON qso_history (qso_uuid, at);

-- Append-only enforcement
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
