-- legacy_name is the config.json forwarder name a seeded binding derives from —
-- the ONE name an older, config-driven build drains — recorded on every row the
-- seed creates, so a downgrade collapses queue names to it instead of inferring
-- it from the current default logbook or from sort order (ADR 0082 dated update
-- 2026-09-25; Codex P2 on c3df0e12). NULL on a binding created after the seed.
-- A separate migration, not an edit of 0014: a file the 0014 build migrated
-- already reports 14 and would never receive an edited column.
ALTER TABLE logbook_destination ADD COLUMN legacy_name TEXT;
