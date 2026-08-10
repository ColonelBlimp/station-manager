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
--
-- payload is TEXT, not JSONB, and that is load-bearing (codex-P1 fix
-- 2026-08-10): the digest is computed over canonical content that
-- preserves numeric LEXEMES, and jsonb normalizes them (plus key order and
-- duplicate keys) — a normalizing column would store bytes that no longer
-- verify against their own digest. TEXT keeps the submitted bytes
-- verbatim; JSON validity and digest verification are enforced at ingest
-- (store.UpsertEvidence), and any consumer that wants jsonb operators
-- casts (payload::jsonb).
--
-- History note: this column was JSONB for the two unpushed commits between
-- c8df1e9d and its review fix, and the file was edited IN PLACE — legal
-- exactly because no database ever applied version 5 in that window
-- (c8df1e9d was never pushed or deployed; the dev container rebuilds from
-- these files per test, verified bare on 2026-08-10). Had any v5 database
-- existed, this would have been a 0006 ALTER instead — in-place migration
-- edits are otherwise forbidden.
CREATE TABLE evidence_records (
    tenant_id   BIGINT      NOT NULL REFERENCES tenants (id),
    kind        TEXT        NOT NULL CHECK (kind IN ('observation', 'coverage', 'loss_interval', 'profile')),
    uuid        UUID        NOT NULL,
    digest_v    INTEGER     NOT NULL CHECK (digest_v >= 1),
    digest      TEXT        NOT NULL CHECK (length(digest) = 64),
    payload     TEXT        NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The upsert + digest-compare key AND the profile-existence probe
    -- (tenant, 'profile', uuid) — §5.4's retryable_missing_profile check.
    PRIMARY KEY (tenant_id, kind, uuid)
);
