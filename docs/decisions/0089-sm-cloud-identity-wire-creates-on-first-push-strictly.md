---
number: 0089
title: Create SM Cloud archives on the first identity push, inside one strict transaction
status: Accepted
date: 2026-10-07
---

# 0089 — Create SM Cloud archives on the first identity push, inside one strict transaction

## Context

ADR 0088 put SM Cloud's archive identity on scoped paths that an old server rejects. It also made
adoption of the legacy logbook an explicit, idempotent call, and added a capability flag. Several
server behaviours on those paths were still open when 5F.2 was designed:
- how an archive the cloud has never seen comes into existence;
- what adoption does when its archive UUID is already taken;
- whether the identity wire may move a logbook or a QSO between archives;
- where the logbook's callsign lives;
- how mutable display labels are updated;
- how strictly the new request bodies are parsed.

The operator ruled on them in the W-0021 5F.2 design (2026-10-07).

Schema 7 (W-0021 5F.1) is already deployed. It holds one unadopted legacy archive per tenant, and
the name-only routes are confined to it, writes included (`cc98c5c8`).

## Decision

1. **Creation.** The first identity push to an unknown archive UUID creates that managed archive,
   and an unknown logbook UUID creates a logbook in it. Creating the archive and the logbook,
   updating metadata and writing the whole QSO batch is ONE transaction: a refused batch leaves
   nothing behind. Adoption is never inferred, from a label or from anything else.
2. **Adoption when the UUID is taken.** If the archive UUID already exists as a non-legacy archive,
   adoption is refused with 409 `archive_uuid_in_use`. An existing managed archive never absorbs
   the legacy archive. Adoption racing a first push ends with one winner and a refusal, and no
   partial change.
3. **No moves between archives.** A logbook UUID that belongs to another archive is refused with
   409 `logbook_in_other_archive`. A QSO stored in another archive than the REQUESTED target is
   refused with 409 `archive_conflict` at every revision. A QSO still moves between logbooks within
   one archive.
4. **The callsign.** Migration 0008 adds `logbooks.callsign` now. Its down step runs only while
   every callsign is empty, and refuses otherwise without changing anything.
5. **Display values.** A successful identity push, or the FIRST adoption, applies its non-empty
   labels and callsign; empty values keep what is stored. A repeated adoption with the same
   identity mapping is a no-op, metadata included. Labels follow server commit order, with no
   metadata revision.
6. **Strict envelopes.** The new request bodies refuse unknown keys, duplicate keys and trailing
   JSON with 400 `invalid_body`. The QSO rows inside them, and the name-only decoder, keep their
   current behaviour.

## Alternatives considered

### An explicit `POST /v1/archives` provisioning call before the first push

This would make creation a separate, inspectable act. It was rejected for three reasons:
- It adds a round trip and a second partial-failure state (provisioned but never pushed).
- It gives no safety the transaction does not already give: the push validates the whole batch and
  commits the containers with it, or not at all.
- When a client may push at all is decided by the 5F.4 enable gate, not by the server.

The name wire's create-on-first-push has the same shape and has not caused trouble.

### Let adoption merge into an existing managed archive with the same UUID

The client would "finish" a Home whose identity pushes ran before adoption. Rejected:
- It merges two row sets the server cannot tell apart.
- That state only arises if the client broke the adopt-before-push order (5F.3), which needs a
  person, not a guess.

The same reasoning rejects inferring adoption from a matching label: labels are mutable display
values, never identity (ADR 0071).

### Allow deliberate moves between archives on the identity wire now

ADR 0071 allows a QSO to move between archives. Implementing that in 5F.2 would mean a push to a
logbook UUID that lives elsewhere, or of a QSO stored elsewhere, silently relocates it. Rejected for
now:
- A push cannot express intent: a stale or misrouted client looks the same as a deliberate move.
- No caller needs it yet.

A move gets its own operation when a real need appears.

### Defer the callsign to whole-archive provisioning

That is the first consumer that needs the callsign. Rejected because:
- ADR 0082 part 8 already lists the callsign among what the cloud receives.
- Adding it later would need a backfill from clients that may no longer be sending it.

The cost of adding it now is one column and a guarded down step. The 0007 down's refusal does not
cover an 8→7 downgrade, hence its own guard.

### Re-apply metadata on every adoption

Every adoption would rewrite the labels. Rejected: a retried adoption, sent long after the operator
renamed the logbook, would silently undo the rename. Only the first adoption sets them.

### A metadata revision, so the latest local edit always wins

This was considered and not built. Commit order is enough for display labels. A delayed push can
briefly show an older label, and the next push corrects it. The limit is written down rather than
hidden.

### Lenient envelopes, matching the name-only decoder

The name-only decoder skips unknown keys for compatibility with old clients. A new wire has no such
clients. Leniency there is exactly how ADR 0088's merge hazard arises: a field the server does not
know is silently dropped. Rejected.

## Consequences

- **One transaction per push.** Every identity push writes its containers, metadata and rows in one
  transaction, so concurrent first pushes and adoptions are settled by the database's unique
  constraints and row locks, and the tests prove that with lock-wait barriers.
- **Error codes.** The server gains `archive_uuid_in_use`, `logbook_in_other_archive` and
  `invalid_body` on the new routes; `archive_conflict` already exists.
- **No moves.** Moving a QSO or a logbook between archives is not possible over SM Cloud until a
  dedicated operation exists.
- **Downgrades.** An 8→7 downgrade is possible only before any callsign is recorded.
- **Labels can lag.** A cloud label can briefly show an older value after out-of-order delivery.

## Triggers to revisit

- An operator needs to move a logbook or QSOs between archives: design the explicit move operation.
- Out-of-order label updates are seen to confuse in practice: add a metadata revision.
- A client appears that cannot send strict envelopes: reconsider S6 for that route only.

## References

- ADR 0071, first-class QSO archives.
- ADR 0082 parts 7 and 8.
- ADR 0088, the scoped identity paths.
- W-0021 dossier: the 5F.2 design and its rulings (2026-10-07); 5F.1 `a6affeed` and `cc98c5c8`.
