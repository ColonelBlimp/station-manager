-- Defense-in-depth: the SCHEMA must refuse a QSO filed under another tenant's
-- logbook. 0001's independent single-column FKs verify that tenant_id and
-- logbook_id each exist, but nothing ties them together — Store.Upsert could
-- store tenant A's QSO under tenant B's logbook, leaking its metadata through
-- B's manifest and breaking A's export mapping. The HTTP layer supplies
-- consistent ids today; the store invariant must not depend on handler
-- discipline. A composite FK needs a matching unique key on the parent.
ALTER TABLE logbooks ADD CONSTRAINT logbooks_id_tenant_key UNIQUE (id, tenant_id);
ALTER TABLE qsos ADD CONSTRAINT qsos_logbook_tenant_fk
    FOREIGN KEY (logbook_id, tenant_id) REFERENCES logbooks (id, tenant_id);
