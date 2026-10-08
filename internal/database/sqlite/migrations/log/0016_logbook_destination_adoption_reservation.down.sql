-- Reverse of 0016. DowngradeLogSchemaTo refuses to reach this step while any
-- binding holds an adoption reservation or a recorded adoption (ADR 0091): it
-- checks before migrating, because a refusal raised from inside a down step
-- would leave the schema dirty. Here nothing is reserved, so the column holds
-- nothing and dropping it loses nothing.

ALTER TABLE logbook_destination DROP COLUMN adoption_reserved_at;
