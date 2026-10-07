---
number: 0088
title: Carry SM Cloud archive identity on scoped paths an old server rejects
status: Accepted
date: 2026-10-07
---

# 0088 — Carry SM Cloud archive identity on scoped paths an old server rejects

## Context

ADR 0071 (§"SM Cloud impact") and ADR 0082 part 7 decided WHAT SM Cloud must model:
- archives unique on `(tenant_id, archive_uuid)`;
- logbooks identified by UUID;
- an explicit, idempotent adoption of the legacy cloud logbook;
- a capability flag on `GET /v1/version`;
- one reconciler per bound logbook;
- restore by UUID.

They left open HOW the wire carries the identity, and how an identity-aware daemon behaves against a
server that does not have it yet. The operator ruled on those questions in the W-0021 5F review
package (2026-10-07).

The server as it stands (tree at `d183e8e8`) constrains that choice:
- **Unknown keys are dropped.** The `PUT /v1/qsos` decoder skips any key other than `logbook` and
  `qsos` (`internal/cloud/server/server.go:206-230`).
- **Query parameters are ignored.** `GET /v1/export` reads none (`:555`).
- **Logbooks are created by name.** The server creates a logbook by name on first push
  (`EnsureLogbook`).
- **A 404 strands a row.** The client treats every 4xx except 408 and 429 (401 re-armed) as terminal
  (`internal/forwarding/smcloud/smcloud.go:386`).

So an identity field added to an existing request would be silently discarded by an old server and
the QSO filed by name. That is a merge across archive boundaries that neither side would report.

## Decision

Identity traffic uses new, archive-scoped URL paths that an old server answers with 404:
- upload: `PUT /v1/archives/{archive_uuid}/logbooks/{logbook_uuid}/qsos`;
- manifest, reconcile and export: their scoped counterparts under the same archive and logbook path;
- adoption: `POST /v1/archives/adopt`.

The other parts of the protocol:
- **Capability.** The server reports `"identity_protocol": 1` on `GET /v1/version`. The daemon checks
  it at start, and reports an unreachable server separately from one that lacks support.
- **What the check gates.** It gates only enabling an identity binding and running adoption. A
  binding already on the identity wire never falls back to the name.
- **A 404 on an identity path is retryable.** The row stays queued.
- **The name-only routes stay.** They remain a compatibility surface, scoped to the tenant's legacy
  archive.

## Alternatives considered

### Identity fields on the existing requests (`archive_uuid` in the `PUT /v1/qsos` body, `?archive_uuid=` on export)

This is the smallest diff and the first thing the package proposed. It was rejected because an old
server ignores both:
- an upload would land in the named logbook, merging archives silently;
- an export would return every archive's rows, so the guarantee that a restore never downloads
  another archive's QSOs would rest on a client-side filter.

A daemon starting against an old server would need the version check to be right every time. The
check is taken at start, while a server can be rolled back afterwards.

### Rely on the version check alone to choose the wire

The daemon would send the name-only request whenever the check did not report support.

This was rejected because the check is a start-time snapshot. A binding enabled while the server
reported support could fall back to the name after a rollback or an unreachable start, which is a
merge. Instead, the check controls only the transitions: enabling a binding and adopting. The wire
itself is chosen by the binding's own state.

### Bump the whole API to `/v2/`

This would draw the same boundary as scoped paths. It was rejected as wider than the change:
- health, version, evidence and the tenant listing have no archive dimension;
- the name-only routes have to live on beside the identity routes for compatibility clients
  anyway.

### Let the server adopt by name on the first identity push

This was already rejected in ADR 0082 because a silent match cannot refuse. It is listed here
because scoped paths make it tempting again: a first push to an unknown `{archive_uuid}` could
simply claim the legacy logbook.

Adoption stays a separate, transactional, idempotent call. It refuses when the legacy archive is
already stamped with a different archive UUID, and when a logbook UUID is already mapped
differently.

A copied Home file carries the same UUIDs, so its adoption is indistinguishable from a retry. That
is ADR 0071's copy semantics: a copy is a backup of the same archive.

### Split an ambiguous legacy logbook during adoption by local membership

The adoption call would carry each local logbook's QSO UUIDs and the server would partition the rows.

This was rejected as machinery for a state the client cannot fully resolve:
- rows that exist only in the cloud stay unassignable;
- a deleted logbook's history and tombstones belong to no live logbook.

Ambiguity is detected and reported for manual recovery instead (W-0021 5F, ruling Q3).

## Consequences

- **The server carries two route families for as long as compatibility clients are supported.**
  The name-only routes resolve names inside the tenant's legacy archive only. The legacy listing,
  reconcile, manifest and export must stay correct when a managed archive holds a logbook with the
  same display name.
- **The client's HTTP classification changes.** A 404 is retryable on identity paths and stays
  terminal on the name-only path.
- **Restore by UUID uses the scoped export.** An old server rejects it rather than over-delivering.
  The legacy name is an explicit option, never a fallback after a failed UUID lookup.
- **Rollout order is server first, then client.** A daemon that runs against an old server keeps
  every identity binding queued, not merged. The cost is a backlog that drains when the server is
  upgraded.

## Triggers to revisit

- When no supported client uses the name-only wire any more, retire the compatibility routes and
  the legacy-archive name resolution.
- If a second kind of archive-scoped resource appears (for example evidence per archive), revisit
  whether a `/v2/` prefix becomes the cheaper boundary.
- If ambiguous legacy mappings turn out to be common on real stations, revisit the
  membership-split alternative.

## References

- ADR 0071 — first-class QSO archives, §"SM Cloud impact".
- ADR 0082 part 7 — the client side of SM Cloud identity and adoption.
- ADR 0052 — SM Cloud backup identity and single-writer QSO invariant.
- W-0021 dossier — the 5F review package and rulings (2026-10-07).
