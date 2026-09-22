-- Reverse of 0012: drop the identity index and column, then the metadata
-- table. Logbook and QSO rows are untouched (the rollback drill proves it).
DROP INDEX IF EXISTS idx_logbook_uuid;
ALTER TABLE logbook DROP COLUMN uuid;
DROP TABLE IF EXISTS archive_metadata;
