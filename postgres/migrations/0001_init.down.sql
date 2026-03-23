-- sql
-- 0001 down: Rollback initialization schema for Station Manager (PostgreSQL)

-- Drop view(s)
DROP VIEW IF EXISTS api_keys_status;

-- Drop tables in reverse FK order
DROP TABLE IF EXISTS qso;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS logbook;
DROP TABLE IF EXISTS users;

-- Drop helper function used in exclusion constraint
DROP FUNCTION IF EXISTS qso_time_range(date, time, time);

-- Optionally drop extension
DROP EXTENSION IF EXISTS btree_gist;
