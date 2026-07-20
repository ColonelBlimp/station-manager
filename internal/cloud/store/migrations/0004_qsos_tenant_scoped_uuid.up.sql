-- Review 2026-07-20 #2: uuid was GLOBALLY unique (the table PK), while the
-- upsert's ON CONFLICT tenant guard turned a cross-tenant collision into a
-- zero-row update — reported to the client as HTTP 200 / applied:0, which the
-- forwarder deliberately treats as backup success (stale-push semantics). QSO
-- UUIDs appear in every exported logbook (not secret), so once a second tenant
-- exists, a known or reused UUID becomes permanently unbackable for the later
-- tenant — a silent denial-of-backup. Scope uniqueness to (tenant_id, uuid):
-- the same UUID may now exist under different tenants, and a conflict can only
-- ever be an intra-tenant re-push, which the revision guard orders correctly.
-- No FK references qsos(uuid); the 0002 composite logbook FK and the manifest
-- index are unaffected.
ALTER TABLE qsos DROP CONSTRAINT qsos_pkey;
ALTER TABLE qsos ADD PRIMARY KEY (tenant_id, uuid);
