-- ADR 0050: per-row revision counter — the sync protocol's version marker.
--
-- modified_at (second precision, wall clock) cannot order two edits of the
-- same row within one second, and wall clock is not monotonic under NTP
-- steps — so the SM Cloud upsert guard needs an ordering the clock can't
-- provide. revision increments on every edit: distinct edits always carry
-- distinct, monotonically increasing values. Existing rows start at 0 (the
-- pre-revision protocol value; the cloud guard falls back to modified_at
-- when revisions tie, which preserves legacy semantics).
ALTER TABLE qso ADD COLUMN revision INTEGER NOT NULL DEFAULT 0;

-- The stamp trigger is REPLACED (same name — SQLite cannot ALTER a trigger)
-- by a combined form that owns BOTH stamps, because two cooperating triggers
-- chain: a revision-only trigger's inner UPDATE leaves modified_at untouched,
-- which fires the 0004 stamp trigger and overwrites the Go writer's explicit
-- µs-UTC modified_at with datetime('now') — exactly the canonicalisation 0004
-- fixed. One trigger, one inner UPDATE, no chain:
--
--   * fires on every update that does not itself change revision — every
--     real edit (updateActiveQso's column map omits revision) including the
--     soft delete. A writer that DOES set revision explicitly is taken as
--     owning both stamps (nothing does today — restore is an INSERT and
--     fires no update trigger).
--   * modified_at: stamped with datetime('now') (UTC) only when the update
--     left it untouched (the 0004 backstop for raw updates); an explicitly
--     written value — the daemon edit path's time.Now().UTC() — is kept.
--   * revision: always OLD.revision + 1.
--   * self-terminating even with recursive_triggers on: the inner UPDATE
--     changes revision, so a recursive evaluation fails the WHEN.
DROP TRIGGER IF EXISTS trg_qso_set_modified_at;
CREATE TRIGGER IF NOT EXISTS trg_qso_set_modified_at
    AFTER UPDATE
    ON qso
    FOR EACH ROW
    WHEN NEW.revision = OLD.revision
BEGIN
    UPDATE qso SET
        modified_at = CASE
            WHEN NEW.modified_at IS NULL OR NEW.modified_at = OLD.modified_at
            THEN datetime('now')
            ELSE NEW.modified_at END,
        revision = OLD.revision + 1
    WHERE id = OLD.id;
END;
