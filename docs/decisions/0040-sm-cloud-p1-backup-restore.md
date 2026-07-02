---
number: 0040
title: SM Cloud P1 — full-fidelity backup + restore via a forwarder to a same-repo Postgres service
status: Accepted
date: 2026-07-02
---

# 0040 — SM Cloud P1: backup + restore

## Context

ADR 0016 deferred SM Cloud (a multi-tenant hosted service) with zero drivers,
capturing two cheap-now prep decisions: UUIDv7 as the canonical QSO ID and the
`qso_history` audit table. A driver has now arrived. A QRZ round-trip destroyed
the local `TIME_ON` seconds on a QSL-manager (M0URX) exchange, exposing that the
operator has **no authoritative, full-fidelity, off-site copy of the log** — QRZ
and ClubLog are lossy and not the operator's to query. Separately the user base
is growing (7Q8AC testing), and the long-term intent is multi-user / multi-logbook
with LoTW-style auto-confirmation of QSOs between SM operators.

The forcing question: design SM Cloud now, and if so, what is the smallest first
phase that is genuinely useful *and* does not paint the multi-user/community
future into a corner? This ADR reactivates the deferred design and settles P1's
architecture. It does **not** supersede 0016's prep decisions (still load-bearing)
or its analysis; it sequences the deferral (single-tenant first, not indefinite).

## Decision

Build **SM Cloud P1 = headless full-fidelity backup + restore**, launched
single-tenant but architected multi-tenant-ready (7Q8AC onboarded next, gated on
a security assessment). Concretely:

- **Transport is a new `smcloud` Forwarder** — insert/update/delete ops →
  **upsert-by-UUID**, pushing the full `types.Qso` JSON (not lossy ADIF),
  reusing the forwarder queue and ADR-0038 forever-retry.
- **Store is Postgres.** Data model mirrors local: `tenant → logbook → QSO`,
  UUID-keyed.
- **Same repo, `cmd/smcloud`** beside `cmd/smd`, importing the real
  `internal/types` + `internal/adif` (its own binary + deploy target).
- **Directionality is split-ownership:** QSO *content* flows up (local always
  authoritative); *confirmation/QSL* flows down (cloud authoritative) as a
  disjoint field-set — built at P3, but the schema reserves for it in P1.
- **Delete is soft-delete/tombstone** (`deleted_at`); the cloud is a retentive
  **superset** of local. Purge is a separate, explicit admin action.
- **Restore is a full-JSON round-trip** (`smd import` gains a JSON mode), never
  an ADIF re-import.
- **Reconcile keys on `(UUID, modified_at)`, not a content hash.** The routine
  check compares per-logbook `{count, hash-of-sorted-(UUID|modified_at)}`; on
  mismatch the daemon pulls the `(UUID, modified_at)` manifest, diffs, and
  **re-enqueues the diverged UUIDs** through the forwarder (detect + self-heal).
  The push carries `modified_at` so the cloud stores local's view of it.
- **Auth/identity is phased.** P1 = one admin bearer token; P2 =
  trust-on-provisioning (the admin issues a per-tenant token to vouched
  operators, no self-serve, no ownership-verification infra); P3 = real callsign
  verification via **accepted LoTW/TQSL certs**, coupled with auto-confirmation —
  the gate that actually needs proof of ownership.

## Alternatives considered

### Directionality: pure one-way vs bidirectional peer

Pure one-way (cloud never writes back) can't express auto-confirmation, which
must push a derived "confirmed" fact down into each operator's log. A full
bidirectional peer (edit on cloud or run SM on two machines) demands
conflict-resolution (vector clocks / last-writer-wins) — a distributed-sync
tarpit for what is fundamentally a backup. **Split-ownership** threads both:
different sides own disjoint columns, so there is never a merge conflict, and we
get auto-confirm without the tarpit. It's the LoTW model.

### Store: SQLite (litestream) vs Postgres

SQLite is zero-ops and fastest to "stable" for single-tenant P1. Rejected as the
foundation because the cloud is a fresh codebase either way (it doesn't reuse the
daemon's sqlboiler models), and multi-user concurrency + cross-tenant matching
queries (P3) would force a later engine swap — a *code* rearchitecture, the exact
thing the "data, not rearchitecture" principle exists to avoid.

### Codebase: separate repo vs same repo

A separate `station-manager-cloud` repo gives clean separation and independent
deploy, but the wire contract *is* `types.Qso`, and the only drift-proof way to
share a type in Go is to import the same package. A separate repo means a
published shared module or a duplicated struct that drifts — which has bitten
this project (the "one canonical DTO" rule). Same repo makes the contract
compile-time shared.

### Delete: honour literally vs tombstone

Honouring deletes keeps cloud == local exactly, but then a fat-fingered local
delete, a bug, or DB corruption propagates to the backup and destroys the
off-site copy — the precise failure a backup exists to prevent. A backup that
faithfully executes destructive operations is not a backup.

### Transport: dedicated sync subsystem vs reuse the forwarder

A bespoke sync subsystem duplicates the queue, retry, and op-type machinery the
forwarder already has, and would couple with the log store — against narrow
daemon scope. The forwarder's insert/update/delete ops map directly onto
upsert/upsert/tombstone, and ADR-0038's indefinite outage retry was designed for
exactly a flaky link.

### Tenancy: self-hosted-OSS / marc-SaaS-multitenant / single-then-multi

Self-hosted-OSS avoids central liability but blocks cross-tenant matching without
federation. Full multi-tenant SaaS up front loads a solo operator with auth, PII,
and ops before the sync is even proven. **Single-tenant launch, multi-tenant-ready
schema** proves push/restore with no third-party liability; adding tenants becomes
data, not a rearchitecture.

### Reconcile signal: dedupe key vs new content hash vs `modified_at`

The existing local hash (`ComputeDedupeKey`) is an *identity* key (CALL|BAND|MODE|
FREQ|DATE|TIME-to-minute) — an edit that fixes a comment or stamps a confirmation
leaves it unchanged, so it's blind to content drift. A new full-content hash would
catch drift but needs a column + hashing every row. `modified_at` (already
trigger-bumped on every UPDATE) is a cheaper proxy that catches every normal edit,
so reconcile keys on it. Its limit: it won't catch content that changes *without*
an UPDATE (bit-rot, direct tampering) — accepted for a backup-reconcile.

### Identity: phase it vs verify ownership early

Real callsign-ownership proof (TQSL-grade) is only *needed* once one operator's
QSOs confirm another's log — i.e. auto-confirmation, which is P3. Building it
earlier (TQSL, or weaker email/QRZ) front-loads the hardest part before anything
uses it. Phasing lets a trusted circle (7Q8AC first) back up under admin-issued
tokens while the hard identity work waits for the phase that requires it.

## Consequences

- The security assessment (previously deferred) now **gates onboarding tenant #2
  (7Q8AC)**, not the P2 community phase. Auth + tenant isolation are designed in
  from the start even though P1 ships with one tenant + one bearer token.
- The daemon's `go.mod` gains Postgres + web deps (linked only into `cmd/smcloud`
  but listed); CI/release grow a second service + deploy target.
- The cloud is a retentive superset (tombstones + never-drop), so storage grows
  unbounded until a retention/purge policy is added — parked.
- `smd import` grows a full-JSON restore mode alongside its ADIF import.
- A read-only web log view is deferred; its justification when it lands is
  eyes-on reconciliation (verify cloud == local), not general browsing.
- Reconcile reuses primitives already built for this: the `modified_at` trigger,
  local `deleted_at` soft-delete (already matches the cloud tombstone), and
  `qso_history` (ADR-0016 prep #2, whose doc names SM Cloud sync). No new column.
- The push payload must carry `modified_at`; reconcile self-heals by re-enqueuing
  diverged UUIDs, so no separate repair path is needed.

## Triggers to revisit

- **Cloud DB size gets out of hand** → design the parked purge/retention (age-out
  or size-driven GC on tombstones + optionally live rows).
- **Real need for cloud-side editing or multi-device reconciliation** → revisit
  split-ownership toward a bidirectional peer (conflict resolution).
- **No second operator ever materialises** → the multi-tenant infra was premature;
  the single-tenant path still stands on its own as a backup.
- **A second language/stack proves warranted for the cloud** → revisit same-repo /
  same-stack reuse.
- **Silent content corruption matters** (disk bit-rot, direct DB writes that
  bypass UPDATE) → add a true per-row content hash; `modified_at` is only a proxy
  for content change and won't catch it.

## References

- `docs/v2-design/sm-cloud-p1.md` — the long-form P1 implementation plan
  (sequenced steps, tests, build-time checks) this ADR anchors.
- ADR 0016 — SM Cloud deferred; prep decisions (UUIDv7, `qso_history`). This ADR
  reactivates P1 of that deferred design.
- ADR 0038 — forwarder durable connectivity retry (the backup's transport relies
  on it).
- ADR 0039 — forwarder `enabled` gates enqueue / config-driven (the `smcloud`
  destination is registered under this model).
- Memory `project_sm_online_db_community`, `project_sm_security_assessment`,
  `project_sm_forwarder_durability`.
