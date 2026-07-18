ALTER TABLE qsos DROP CONSTRAINT IF EXISTS qsos_logbook_tenant_fk;
ALTER TABLE logbooks DROP CONSTRAINT IF EXISTS logbooks_id_tenant_key;
