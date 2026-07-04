// Package store is the smcloud Postgres persistence layer (ADR 0040 /
// docs/v2-design/sm-cloud-p1.md, step S1): the authoritative, UUID-keyed,
// retentive off-site QSO store behind the SM Cloud backup/restore service.
//
// # Data model
//
// Three tables — tenants, logbooks, qsos. A QSO is stored whole as JSONB (the
// full types.Qso), with its identity + reconcile fields lifted into columns:
//
//   - uuid        the QSO's own UUID — the upsert key, preserved end to end so a
//     restore round-trips identity (never an ADIF re-import).
//   - modified_at drives reconcile diffing on (uuid, modified_at).
//   - deleted_at  a tombstone; the cloud is retentive, so a peer that missed a
//     delete still reconciles to the tombstone rather than resurrecting.
//
// Single-tenant in P1 but multi-tenant-ready — every QSO carries tenant_id +
// logbook_id from the start.
//
// # Queries: hand-written, not ORM-generated
//
// The hot-path operations — batch upsert-by-UUID and the (uuid, modified_at)
// manifest — are hand-written SQL. Postgres UPSERT and JSONB are outside
// sqlboiler's generated CRUD, so the ORM (sqlboiler.toml + `task models:cloud`)
// covers the relational scaffolding (tenants/logbooks) and the qsos read model,
// while these core ops stay explicit. This is deliberate, not a workaround: an
// ORM buys little for a JSONB upsert store.
//
// # Driver + migrations
//
// Store is driver-agnostic: the caller opens *sql.DB with a registered Postgres
// driver and hands it in. Migrations live in migrations/ (golang-migrate,
// Postgres dialect) and are applied out of band (dev: `task migrate:cloud:up`;
// a runtime applier lands with cmd/smcloud). Auth credentials are deliberately
// NOT modelled here — ADR 0040 phases auth, and credentials arrive in their own
// table once the security assessment lands.
package store
