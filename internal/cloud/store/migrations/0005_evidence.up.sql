-- §5 evidence sync, sync slice (spot-network design §5.1/§5.2, operator
-- rulings 2026-08-10). ONE uniform table for the four synced record kinds —
-- the contract is deliberately uniform per §5.1, and digest identity is
-- (tenant, kind, uuid) over versioned canonical immutable content. Payload
-- is stored VERBATIM as submitted (replay-complete, §5.3 sequencing
-- amendment): no occurrence index and no callsign-identity semantics until
-- the page slice, which will build/backfill occurrences by replaying these
-- rows. The reserved `retention` wire kind is rejected at ingest until the
-- retention slice lands (§4.1 sequencing amendment) — a new kind is a
-- migration event, hence the CHECK.
CREATE TABLE evidence_records (
    tenant_id   BIGINT      NOT NULL REFERENCES tenants (id),
    kind        TEXT        NOT NULL CHECK (kind IN ('observation', 'coverage', 'loss_interval', 'profile')),
    uuid        UUID        NOT NULL,
    digest_v    INTEGER     NOT NULL CHECK (digest_v >= 1),
    digest      TEXT        NOT NULL CHECK (length(digest) = 64),
    payload     JSONB       NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The upsert + digest-compare key AND the profile-existence probe
    -- (tenant, 'profile', uuid) — §5.4's retryable_missing_profile check.
    PRIMARY KEY (tenant_id, kind, uuid)
);
