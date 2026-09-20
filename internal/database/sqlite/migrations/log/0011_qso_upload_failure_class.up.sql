-- A failed qso_upload row gains a durable, typed reason class (W-0010 outcome
-- 9, ruling 2026-09-20 (c)). Recovery code must not parse last_error: that text
-- is provider-owned and redacted. `auth` = the destination rejected the
-- configured credential; those rows are re-armed once per daemon restart so a
-- corrected key drains them, while a callsign mismatch or malformed record
-- (NULL class) is never made retryable by a new key.
--
-- Every existing row stays NULL (ruling (d)): no class is inferred from
-- last_error, however recognisable its prefix. The CHECK enumerates the
-- classes; adding one is a migration, as for `origin`.

ALTER TABLE qso_upload
    ADD COLUMN failure_class TEXT
    CHECK (failure_class IS NULL OR failure_class IN ('auth'));
