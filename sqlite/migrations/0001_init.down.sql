PRAGMA foreign_keys = OFF;

-- Drop triggers
DROP TRIGGER IF EXISTS trg_qso_upload_set_updated_at;
DROP TRIGGER IF EXISTS trg_qso_set_modified_at;

-- Drop indexes
DROP INDEX IF EXISTS idx_qso_upload_uploaded;
DROP INDEX IF EXISTS idx_qso_upload_pending;
DROP INDEX IF EXISTS idx_qso_active_date_time;
DROP INDEX IF EXISTS idx_qso_active_call;
DROP INDEX IF EXISTS idx_qso_session_id;
DROP INDEX IF EXISTS idx_qso_logbook_id;
DROP INDEX IF EXISTS idx_qso_date_time;
DROP INDEX IF EXISTS idx_qso_country;
DROP INDEX IF EXISTS idx_qso_band;
DROP INDEX IF EXISTS idx_qso_call;
DROP INDEX IF EXISTS uq_logbook_api_key;
DROP INDEX IF EXISTS idx_country_name;
DROP INDEX IF EXISTS uq_contacted_station_active_call;

-- Drop tables (child tables first)
DROP TABLE IF EXISTS qso_upload;
DROP TABLE IF EXISTS contacted_station;
DROP TABLE IF EXISTS country;
DROP TABLE IF EXISTS qso;
DROP TABLE IF EXISTS logbook;
DROP TABLE IF EXISTS session;

PRAGMA foreign_keys = ON;
