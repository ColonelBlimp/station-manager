-- identity_since marks a binding born on SM Cloud identity (ADR 0092, W-0021
-- 5F.4 commit 3): it uploads by UUID from its first upload and is never
-- adopted. It is written in the same durable save as the qualifying enable and
-- never cleared: not by a disable, an account change or a restart.
-- DowngradeLogSchemaTo refuses to drop it while any binding holds it.

ALTER TABLE logbook_destination ADD COLUMN identity_since DATETIME;
