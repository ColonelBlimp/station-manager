-- Retention slice (spot-network §4.1 retention rulings, 2026-08-10):
-- activate the reserved `retention` wire kind (compaction summaries and
-- purge receipts sync like every other kind) and add SUPERSESSION
-- tombstones. A summary's ingest inserts tombstones for its DIRECT
-- predecessors BEFORE deleting their rows — one transaction — so a later
-- old-backup re-offer of a deleted predecessor answers `tombstoned`
-- instead of quietly re-creating it: without the tombstone, supersession
-- deletion would not be idempotent. Ingest checks tombstones before every
-- upsert, for every kind.
ALTER TABLE evidence_records DROP CONSTRAINT evidence_records_kind_check;
ALTER TABLE evidence_records ADD CONSTRAINT evidence_records_kind_check
    CHECK (kind IN ('observation', 'coverage', 'loss_interval', 'profile', 'retention'));

CREATE TABLE evidence_tombstones (
    tenant_id  BIGINT      NOT NULL REFERENCES tenants (id),
    kind       TEXT        NOT NULL,
    uuid       UUID        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, kind, uuid)
);
