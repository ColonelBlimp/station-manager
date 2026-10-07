-- W-0021 5F.1 (ADR 0071 §"SM Cloud impact", ADR 0082 part 7, ADR 0088): the
-- archive boundary. A station's QSO archive is identified by its UUID, and a
-- logbook by its UUID within the tenant; names are mutable labels. Every
-- existing logbook lands in its tenant's ONE legacy archive, unadopted
-- (archive_uuid NULL until the explicit adoption call stamps it, 5F.2). The
-- name-only wire keeps resolving logbook names — now `legacy_name` — inside the
-- legacy archive only, so an old client never reaches another archive.

CREATE TABLE archives (
    id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id    BIGINT      NOT NULL REFERENCES tenants (id),
    -- The station archive's own UUID; NULL only for an unadopted legacy archive.
    archive_uuid UUID,
    -- Display only, mutable; never an identity.
    label        TEXT        NOT NULL DEFAULT '' CHECK (length(label) <= 64),
    legacy       BOOLEAN     NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT archives_id_tenant_key UNIQUE (id, tenant_id),
    CONSTRAINT archives_tenant_uuid_key UNIQUE (tenant_id, archive_uuid),
    CONSTRAINT archives_uuid_unless_legacy CHECK (legacy OR archive_uuid IS NOT NULL)
);
CREATE UNIQUE INDEX archives_one_legacy_per_tenant ON archives (tenant_id) WHERE legacy;

INSERT INTO archives (tenant_id, legacy) SELECT id, true FROM tenants;

-- The name the old wire resolves is the legacy name; the display label is
-- separate and starts equal to it.
ALTER TABLE logbooks RENAME COLUMN name TO legacy_name;
ALTER TABLE logbooks ALTER COLUMN legacy_name DROP NOT NULL;
ALTER TABLE logbooks ADD COLUMN label TEXT NOT NULL DEFAULT '' CHECK (length(label) <= 64);
UPDATE logbooks SET label = legacy_name;

ALTER TABLE logbooks ADD COLUMN archive_id BIGINT;
UPDATE logbooks l SET archive_id = a.id FROM archives a WHERE a.tenant_id = l.tenant_id AND a.legacy;
ALTER TABLE logbooks ALTER COLUMN archive_id SET NOT NULL;
-- A logbook's archive belongs to the same tenant (as 0002 ties a QSO to its
-- tenant's logbook).
ALTER TABLE logbooks ADD CONSTRAINT logbooks_archive_tenant_fk
    FOREIGN KEY (archive_id, tenant_id) REFERENCES archives (id, tenant_id);

ALTER TABLE logbooks ADD COLUMN uuid UUID;
ALTER TABLE logbooks ADD CONSTRAINT logbooks_tenant_uuid_key UNIQUE (tenant_id, uuid);

-- A legacy name is unique within its archive, no longer across the tenant.
ALTER TABLE logbooks DROP CONSTRAINT logbooks_tenant_id_name_key;
ALTER TABLE logbooks ADD CONSTRAINT logbooks_archive_legacy_name_key UNIQUE (archive_id, legacy_name);
