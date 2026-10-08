-- Reverse of 0017. DowngradeLogSchemaTo refuses to reach this step while any
-- binding holds a confirmation (ADR 0091): a schema-16 build would upload by
-- name without the account-confirmation hold. Here nothing is confirmed, so the
-- column holds nothing and dropping it loses nothing.

ALTER TABLE logbook_destination DROP COLUMN remote_adopted_account;
