---
number: 0052
title: SM Cloud identity — no-data-loss backup first, passive store always
status: Accepted
date: 2026-07-19
---

# 0052 — SM Cloud identity: no-data-loss backup first, passive store always

## Context

SM Cloud P1 is complete, proven (six fault drills, real restore, clean audit),
and hardened (rate limiting landed 2026-07-19; the ADR 0040 assessment is the
remaining Phase-2 gate). With the build stable, the question shifted from "does
it work" to "what is this component" — because plausible futures were pulling
in different directions: laptop and phone clients syncing through it (the POTA
workflow: seed the laptop, log offline at the park, push on return, shack pulls
down), a qrz.com-like multi-tenant core (logbooks + QSOs owned by tenants,
maybe cross-tenant QSL confirmations), and the Merkle-lite/delta-sync bandwidth
work. Without a declared identity, every future proposal re-derives the
architecture — or worse, quietly bends it.

Two operator declarations anchor this ADR: **"First and foremost, SMC is a
no-data-loss backup"** and **"there will never be 2+ clients editing the same
QSO record"** (both 2026-07-19).

## Decision

SM Cloud is, first and foremost, a **no-data-loss backup** of tenant-owned
logbooks. Its permanent identity, which every richer feature layers over and
never changes:

1. **Passive, revision-guarded store.** SMC stores client rows verbatim and
   arbitrates nothing — the ADR 0050 revision-first guard orders writes; there
   is no server-side merge logic, ever.
2. **Single writer per QSO.** Each QSO is edited on one client at a time — the
   operator is the mutex. The guard makes a violation *safe* (one edit wins
   cleanly, reconcile surfaces the drift), not impossible; that trade is
   accepted.
3. **Forwarding-by-origin.** Enrichment and upstream forwarding (QRZ, ClubLog,
   …) happen on the client that *logged* the QSO. Rows pulled down from the
   cloud never enter a forwarding queue — the pull path stays as pure as
   restore (metadata only).
4. **Everything richer is a layer over the store.** Sync legs, query API,
   multi-tenant service, QSL matching — all read or derive from tenant rows;
   none mutates a tenant's QSO payload server-side.

**First milestone (operator, 2026-07-19): multiple tenancy** — provision a
second real tenant (7Q8AC, the ship-cycle operator) on the same instance. The
server's token→tenant map already supports N entries; the build work is boot
provisioning (today `cmd/smcloud` accepts exactly one
SMCLOUD_CALLSIGN/SMCLOUD_TOKEN pair) plus the tenant-isolation checks the
tests already pin. This is small-m multi-tenancy — trusted, hand-provisioned
tenants on a private instance — NOT the public service (see alternatives).

Roadmap *direction* beyond that (sequenced, each step useful alone; not
commitments): bidirectional reconcile (pull `CloudOnly` rows through the
restore write path — completes the POTA loop) → per-device tokens (lost
phone = one revocation) → delta pull / Merkle-lite manifest → phone-shaped
query API. The qrz.com-core direction (QSL confirmations as the first
server-owned *derived* table) is **explored, not decided** — captured here so
it isn't re-derived, deliberately unscheduled.

## Alternatives considered

### Sync hub as the primary identity (cloud becomes source of truth)

The multi-client future could make SMC the arbiter and the shack DB a replica.
Rejected: it inverts P1's direction of trust (LOCAL IS AUTHORITATIVE —
`internal/forwarding/smcloud/reconcile.go`), and it puts the backup at risk
from arbitration bugs — the component whose whole job is "never lose data"
should not also be the component that decides which data wins.

### Server-side merge / conflict resolution (vector clocks, CRDTs)

The standard answer to multi-client sync. Rejected because its only customer is
concurrent editing of one row, which the operator has declared will never
happen. Complexity with no user; last-writer-wins by revision is sufficient and
auditable.

### Forwarding on pull (shack forwards QSOs the laptop logged)

Tempting so the laptop needs no credentials. Rejected three ways: it needs
double-forwarding dedup between clients; it breaks restore-path purity (pulled
rows would write queue rows); and a pulled batch entering the queue is exactly
the history-less backfill shape the ClubLog retry-only guard refuses — it would
re-open the realtime.php promise we enforce in code.

### Build the public multi-operator service / QSL layer now

Distinct from the milestone-1 second tenant (trusted, hand-provisioned): the
*public service* means self-signup, quotas, abuse handling, funding, and the
identity problem behind QSL (a confirmation claims the token-holder IS that
callsign — LoTW grew a certificate apparatus for this), plus cross-tenant
privacy. Deferred: those costs are outside the code and have no current
demand beyond the two known operators; the rule is to build nothing that
*forecloses* it. Milestone 1's provisioning work is a strict subset either
way.

## Consequences

- Every smcloud proposal gets one sequencing test: **does it serve the backup
  now without blocking the layers later?** Anything that risks the store's
  simplicity or durability loses by default.
- The POTA workflow needs exactly one increment (bidirectional reconcile at the
  existing `CloudOnly` seam); push, seed-by-restore, and offline logging
  already work.
- A laptop that should upload park QSOs to QRZ/ClubLog must carry its own
  forwarder credentials — accepted cost of forwarding-by-origin.
- The accepted residual risk: a genuine single-writer violation (same row
  edited on two clients before they sync) silently drops the losing edit.
  Reconcile surfaces it as drift; nothing corrupts.
- QSL confirmations, if ever built, live in their own derived, rebuildable
  table — a tenant's QSO payload is never server-mutated.
- Milestone 1 means the instance holds **someone else's** logbook: the
  Postgres backup story (the backup's backup) and token hygiene (rotation,
  per-tenant revocation) stop being nice-to-haves and become duties owed to
  another operator.

## Triggers to revisit

- A second client (laptop) enters regular use → start the sync legs
  (bidirectional reconcile + device tokens); that's execution, not a revisit.
- Any feature needs the same QSO editable from multiple clients (e.g. a
  logbook-editing web UI on SMC itself) → the single-writer invariant breaks;
  that is a superseding ADR with real conflict semantics, not a patch.
- Tenants beyond the trusted hand-provisioned circle (self-signup) → reopen
  the public-service questions (identity, quotas, abuse, privacy, funding).
- A phone client becomes concrete → design the query API and decide whether a
  thin client talks to SMC directly or through a daemon.
- QSL confirmations get demand → the identity/verification decision comes
  first, before any matching code.

## References

- ADR 0040 (SM Cloud P1), ADR 0050 (revision counter sync protocol).
- `docs/v2-design/sm-cloud-p1.md`; `docs/smcloud-deploy.md`.
- `internal/forwarding/smcloud/reconcile.go` — direction-of-trust comment and
  the `CloudOnly`/`CloudNewer` counters (the bidirectional-reconcile seam).
- `docs/backlog.md` — "smcloud hardening — pre-Phase-2 gate" (remaining:
  ADR 0040 assessment + token rotation).
