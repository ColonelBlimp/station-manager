PRAGMA foreign_keys = ON;

-- operator_event — the local, categorised operator-facing event store: the
-- notification-history pilot (W-0001) of ADR 0061's shape, ratified by ADR 0076.
--
-- Unlike qso_history (an append-only audit trail kept forever), this is a
-- BOUNDED ring: retention prunes the oldest rows per category, so DELETE is
-- permitted and only UPDATE is refused — a recorded event is an immutable fact
-- until retention evicts it.
--
-- Only the `notification` category is wired now, and only two durable kinds
-- (ADR 0076): a browser-originated `export.adif_failed` and a daemon-originated
-- terminal `forward.failed`. Both CHECKs are deliberately CLOSED — a future
-- category or kind widens them through its own migration, the way 0002/0003/0006
-- widened a CHECK — so a stray write of an unplanned category/kind fails loudly
-- rather than silently landing in an operator-facing surface.
--
-- `detail` carries ONLY typed, bounded metadata the producing boundary chose
-- (e.g. forward.failed: qso id, forwarder, action, attempts) — NEVER raw
-- third-party/provider error text. The column only guarantees valid JSON; the
-- typed shape is enforced Go-side at each producer. This is the defence against
-- the third-party-string exfiltration shape ADR 0061 names; smd.log stays the
-- only home for raw provider strings.
--
-- severity, occurred_at, and build are stamped DAEMON-side for both sources (a
-- browser supplies only the typed kind fields). build is the canonical full
-- build-version string (buildinfo.Version), matching the diagnostic logs; it is
-- NOT NULL with no default, so an insert that omits provenance fails loudly
-- rather than acquiring a value nobody chose (the 0007 origin rule).
CREATE TABLE IF NOT EXISTS operator_event
(
    id          INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
    category    TEXT     NOT NULL CHECK (category IN ('notification')),
    kind        TEXT     NOT NULL CHECK (kind IN ('export.adif_failed', 'forward.failed')),
    severity    TEXT     NOT NULL CHECK (severity IN ('info', 'warn', 'error')),
    occurred_at DATETIME NOT NULL DEFAULT (datetime('now')),
    build       TEXT     NOT NULL,
    detail      JSON     NOT NULL DEFAULT ('{}') CHECK (json_valid(detail))
);

-- Per-category, insertion-ordered access. One index serves both directions of
-- the retention ring: newest-N retrieval (ORDER BY id DESC WHERE category = ?)
-- and oldest-first eviction (ORDER BY id ASC WHERE category = ?). id
-- (AUTOINCREMENT) is the monotonic arrival order eviction is defined against.
CREATE INDEX IF NOT EXISTS idx_operator_event_category_id
    ON operator_event (category, id);

-- Immutable-but-prunable: an event is a historical fact, so UPDATE is refused;
-- DELETE is intentionally NOT blocked (retention prunes the oldest rows per
-- category). Mirrors qso_history's append-only guard minus the no-delete half.
CREATE TRIGGER IF NOT EXISTS trg_operator_event_no_update
    BEFORE UPDATE
    ON operator_event
BEGIN
    SELECT RAISE(ABORT, 'operator_event is immutable — UPDATE not permitted (retention prunes via DELETE)');
END;
