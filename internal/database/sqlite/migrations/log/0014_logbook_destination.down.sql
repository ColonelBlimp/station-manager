-- Reverse of 0014: drop the binding table and the seed marker. Queue rows are
-- not renamed here: the collapse of binding names to the one name an older,
-- config-driven build drains is 0015's down step, which runs before this one
-- and needs 0015's `legacy_name` column. A file that stopped at 0014 (the
-- build that introduced the table wired no seed, so it holds no renamed rows)
-- loses nothing by the plain drop. QSO, logbook and queue rows are untouched.
DROP TABLE IF EXISTS logbook_destination;
ALTER TABLE archive_metadata DROP COLUMN destination_bindings_seeded_at;
