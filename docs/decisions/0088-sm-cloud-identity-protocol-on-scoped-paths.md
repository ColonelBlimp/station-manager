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

## Refinement (2026-10-13): restore by identity (W-0021 5F.5)

The operator ruled (W-0021 dossier, rulings K1–K6) how `smd restore` uses the scoped export.

- **Source.** A restore reads only the target's own identity: the target archive's UUID (its
  catalogue id, `--archive`, default the active archive) and the target logbook's UUID
  (`--logbook`, default that archive's own default logbook). No flag names another source, and
  nothing is provisioned.
- **Default.** With no source flag, every archive restores by identity, Home included. The legacy
  name is selected only by an explicit `--cloud-logbook NAME`, where NAME is the cloud logbook's
  name. `--forwarder`, which read a name from a binding, is retired and refused before anything is
  fetched.
- **Preflight.** The existing bounded support check runs first, and only `supported` proceeds.
  Unsupported, unreachable and other errors are refused in their own words. A later 404 is refused
  naming both UUIDs and the legacy option, without claiming the server still supports identity: the
  preflight records what the server reported, not that it stayed unchanged.
- **Validation.** An export is used only when its head names exactly the requested archive and
  logbook and the document is complete. Nothing is inserted otherwise. A valid empty export
  succeeds.
- **No consent needed, nothing changed.** A restore needs a valid station account and the target's
  existing identities, not a binding. It never creates, enables or stamps a binding, and never
  queues an upload.
- **Read-only until the export is accepted.** Target resolution, the preflight and the fetch run
  before the archive is opened for writing, so a dry-run, or a refusal before that open, migrates
  nothing, creates no missing file and writes no archive summary. The re-check after the open
  (the target changed during the fetch) refuses with nothing inserted, but follows the open's
  migrations. The read-only target resolution reads logbook UUIDs whether or not the file has a
  binding table, so an archive at log schema 12 or 13 resolves its target by identity without
  being migrated. Archive-file identity verification is kept. A legacy dry-run whose target file
  is missing reads nothing, since that file holds no binding to refuse on; a real restore still
  refuses the missing file before fetching.
- **What "untouched" permits (ruling A, 2026-10-14).** The read-only reads open the closed file
  with SQLite's `mode=ro`. On a WAL-mode file that leaves an empty `-wal` and a 32 KiB `-shm`
  beside it (measured), which the next read-write open removes; the database file is unchanged.
  Only those are permitted: newly created, beside the archive file actually inspected, the `-wal`
  zero bytes, each with that file's mode. Every pre-existing entry stays byte-for-byte unchanged,
  existing sidecars included, and the database, summary and configuration stay under strict
  comparison. A write held in a nonempty WAL is a change even though the main file is unchanged.

Alternatives rejected:

- **A source flag for another logbook of the same archive.** An identity push at a newer revision
  moves a cloud row between logbooks of one archive, so restored UUIDs in another logbook would
  later merge the two silently.
- **A source flag for another archive (disaster recovery before provisioning).** The server
  refuses one archive's UUIDs in another as `archive_conflict`, so the restore would succeed and
  every later edit fail at upload. Restoring a lost archive file belongs to whole-archive
  provisioning, a later slice.
- **Keeping the name default in Home.** It makes the default depend on the archive's ownership,
  and it is the name fallback this ADR rules out.
- **Filtering the whole-tenant export by UUID.** It reads the same rows from a new server, but an
  old server ignores the filter and delivers every archive's rows.
- **Keeping `--forwarder` as a second legacy option.** An identity binding's `logbook` field is
  only the adoption key, so the name it supplies says nothing about where the binding uploads.
- **Opening the read-only reads with `immutable=1` to leave no sidecars.** No `-wal` at one instant
  does not make the file immutable for the whole read. SQLite documents that `immutable=1` skips
  locking and change detection, so a later change can produce incorrect results or corruption
  errors (<https://www.sqlite.org/uri.html#uriimmutable>). That contract does not suit the shared
  read helpers, which daemon start and archive activation also use.

## References

- ADR 0071 — first-class QSO archives, §"SM Cloud impact".
- ADR 0082 part 7 — the client side of SM Cloud identity and adoption.
- ADR 0052 — SM Cloud backup identity and single-writer QSO invariant.
- W-0021 dossier — the 5F review package and rulings (2026-10-07).
