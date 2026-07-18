-- ADR 0050: per-row revision counter — the sync protocol's version marker.
-- Mirrors the local SQLite migration 0005: modified_at (second precision on
-- the local side) cannot order same-second edits, so the upsert guard orders
-- on revision first and falls back to modified_at only when revisions tie
-- (which preserves exact legacy semantics for pre-revision rows at 0).
ALTER TABLE qsos ADD COLUMN revision BIGINT NOT NULL DEFAULT 0;
