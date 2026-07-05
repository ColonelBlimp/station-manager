-- smcloud P1 store (ADR 0040 / docs/v2-design/sm-cloud-p1.md, step S1).
--
-- The authoritative, UUID-keyed, retentive off-site QSO store. Single-tenant in
-- P1 but multi-tenant-ready: every QSO carries tenant_id + logbook_id. The QSO
-- itself is stored WHOLE as JSONB (the full types.Qso, UUID + additional_data +
-- HH:MM:SS seconds intact); the identity + reconcile fields (uuid, modified_at,
-- deleted_at) are lifted into columns so diffing is index-served, not JSONB-dug.

CREATE TABLE tenants (
    id         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- The operator/station callsign that owns this tenant. Identity ONLY — auth
    -- credentials are deliberately NOT here (ADR 0040 phased auth: P1 is a bearer
    -- token, and any username/password/TQSL model arrives in its own table once
    -- the security assessment lands, so it can evolve without touching identity).
    callsign   TEXT        NOT NULL UNIQUE CHECK (length(callsign) BETWEEN 3 AND 32),
    name       TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE logbooks (
    id         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id  BIGINT      NOT NULL REFERENCES tenants (id),
    name       TEXT        NOT NULL CHECK (length(name) <= 64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A logbook is identified within its tenant by name, so EnsureLogbook is idempotent.
    UNIQUE (tenant_id, name)
);

CREATE TABLE qsos (
    -- The QSO's own UUID (from types.Qso) — the upsert key, preserved end to end so
    -- restore round-trips identity (never an ADIF re-import, which would mint a new one).
    uuid        UUID        PRIMARY KEY,
    tenant_id   BIGINT      NOT NULL REFERENCES tenants (id),
    logbook_id  BIGINT      NOT NULL REFERENCES logbooks (id),
    -- Lifted from the QSO; drives reconcile diffing on (uuid, modified_at).
    modified_at TIMESTAMPTZ NOT NULL,
    -- Tombstone: non-null = soft-deleted. The cloud is RETENTIVE (keeps deleted rows
    -- so a peer that missed the delete still reconciles to the tombstone).
    deleted_at  TIMESTAMPTZ,
    -- The full types.Qso as sent on the wire.
    payload     JSONB       NOT NULL
);

-- Manifest read: (uuid, modified_at, deleted flag) for a logbook. The manifest
-- SELECTs deleted_at too, so it is carried as an INCLUDE payload column — that
-- keeps the read a true index-only scan (no heap fetch, JSONB untouched) rather
-- than the index only covering the key columns.
CREATE INDEX qsos_logbook_manifest ON qsos (logbook_id, uuid, modified_at) INCLUDE (deleted_at);
-- Cross-logbook recency scans (housekeeping / future sync windows).
CREATE INDEX qsos_modified_at ON qsos (modified_at);
