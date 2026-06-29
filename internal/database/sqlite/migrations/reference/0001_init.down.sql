PRAGMA foreign_keys = OFF;

DROP INDEX IF EXISTS idx_country_name;
DROP INDEX IF EXISTS uq_contacted_station_active_call;

DROP TABLE IF EXISTS country;
DROP TABLE IF EXISTS contacted_station;

PRAGMA foreign_keys = ON;
