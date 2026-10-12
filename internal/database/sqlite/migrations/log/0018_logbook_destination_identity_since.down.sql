-- Reverse of 0018. DowngradeLogSchemaTo refuses to reach this step while any
-- binding holds identity_since (ADR 0092): a schema-17 build would treat a
-- binding born on identity as a legacy one and upload it by name. Here nothing
-- is stamped, so the column holds nothing and dropping it loses nothing.

ALTER TABLE logbook_destination DROP COLUMN identity_since;
