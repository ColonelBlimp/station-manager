-- Per-logbook destination bindings (ADR 0082, W-0021 slice 5B): one row per
-- (logical logbook × destination type) inside the archive file. It carries the
-- on/off state, the logbook-scoped credential fields for that destination
-- (type-owned JSON, so the file holds secrets — it is 0600 inside 0700) and
-- the immutable forwarder_name that keys the binding's qso_upload rows and its
-- worker. There is no archive-level row: the operator's one switch per
-- destination is an aggregate over these rows.
--
-- archive_metadata.destination_bindings_seeded_at is the one-time adoption
-- marker: NULL until the daemon has decided the seed for this file (the adopted
-- Home archive seeds from the legacy config entries; a managed or external
-- archive is marked with no rows). A logbook created after that point stays
-- unbound whatever config.json still carries.

CREATE TABLE IF NOT EXISTS logbook_destination
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
    CONSTRAINT uq_logbook_destination UNIQUE (logbook_id, destination)
);

ALTER TABLE archive_metadata ADD COLUMN destination_bindings_seeded_at DATETIME;
