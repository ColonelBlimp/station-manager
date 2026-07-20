-- Restores the global-unique uuid PK. Fails (by design) if different tenants
-- hold the same UUID — such rows have no valid pre-0004 representation.
ALTER TABLE qsos DROP CONSTRAINT qsos_pkey;
ALTER TABLE qsos ADD PRIMARY KEY (uuid);