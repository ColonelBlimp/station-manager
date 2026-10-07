-- Reverses 0007 only while it is lossless: every logbook still sits in its
-- tenant's unadopted legacy archive with a legacy name and no UUID. Once any
-- archive or logbook identity exists, version 6 cannot represent it (names
-- would collide or adoption would be lost), so the step refuses and changes
-- nothing.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM archives WHERE NOT legacy OR archive_uuid IS NOT NULL)
       OR EXISTS (SELECT 1 FROM logbooks WHERE uuid IS NOT NULL OR legacy_name IS NULL) THEN
        RAISE EXCEPTION 'smcloud 0007 down refused: archive or logbook identity data exists and version 6 cannot hold it';
    END IF;
END $$;

ALTER TABLE logbooks DROP CONSTRAINT logbooks_archive_legacy_name_key;
ALTER TABLE logbooks DROP CONSTRAINT logbooks_tenant_uuid_key;
ALTER TABLE logbooks DROP CONSTRAINT logbooks_archive_tenant_fk;
ALTER TABLE logbooks DROP COLUMN uuid;
ALTER TABLE logbooks DROP COLUMN archive_id;
ALTER TABLE logbooks DROP COLUMN label;
ALTER TABLE logbooks ALTER COLUMN legacy_name SET NOT NULL;
ALTER TABLE logbooks RENAME COLUMN legacy_name TO name;
ALTER TABLE logbooks ADD CONSTRAINT logbooks_tenant_id_name_key UNIQUE (tenant_id, name);
DROP TABLE archives;
