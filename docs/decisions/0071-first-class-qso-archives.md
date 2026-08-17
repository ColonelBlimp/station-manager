---
number: 0071
title: Manage physical QSO databases as first-class archives
status: Proposed
date: 2026-08-17
---

# 0071 — Manage physical QSO databases as first-class archives

## Context

Station Manager currently has one configured SQLite path
(`datastore.path`, normally `${working_dir}/db/station-manager.db`). That file is
both the daemon's active QSO store and an implicit piece of process wiring. The
SPA can create several logical `logbook` rows inside it, but an operator cannot
create a physically separate database for a contest, activate it, and later
return to the home archive.

A physical boundary is a legitimate additional guarantee. It gives a contest
its own SQLite/WAL files, migration history, upload queue, QSO history, backup,
and corruption/repair boundary. It does **not** replace the existing logical
logbook: one QSO archive may still contain several logbooks/callsigns, and a
new contest archive normally starts with one.

Treating this as a writable `datastore.path` setting is unsafe and incomplete:

- a browser path field would expose an arbitrary server-filesystem write
  primitive and has no meaningful relationship to the browser's local file
  picker;
- the current `default_logbook_id` is a database-local integer and may identify
  a different row in the next file;
- the HTTP handlers, QSO service, FT8 completion sink, forwarder workers,
  SM-Cloud reconciler, and enrichment history all retain the active database
  service; closing and repointing its handle underneath them is not a switch;
- `reference.db` and `evidence.db` are currently located relative to the QSO DB
  directory even though their data is station-global; and
- SM Cloud currently identifies a logbook by `(tenant, name)`. Two physical
  archives can both contain a logbook called `main`, so switching files without
  extending cloud identity would silently merge their backups. A filename is
  not a portable identity.

ADR 0070 supplies the lifecycle boundary this feature needs: dependency
metadata/topology belongs to iocdi, runtime transitions belong to the lifecycle
orchestrator, and individual services own their resources. Its supervisors are
terminal within one daemon generation. Reusing one database service after
teardown would therefore fight that design; a new process generation is the
safe first switch mechanism.

## Decision

Introduce a first-class **QSO archive**, identified by an immutable UUID and
backed by exactly one SQLite file. The daemon maintains a station-global archive
catalogue and active/pending archive selection in `config.json`; a small
`archive_metadata` row inside each QSO file proves its UUID and stores its
database-local current/default logbook. `datastore.path` ceases to be the active
archive selector: the selected catalogue entry resolves the QSO path before the
lifecycle graph is built, while the remaining datastore fields continue to
configure the SQLite connection.

The SPA may create, list, and activate managed archives. Creation is online and
does not disturb the active archive; activation is **restart-to-switch** through
the existing attended `POST /v1/restart` machinery. The HTTP layer records
selection intent but never closes or repoints a database. On shutdown, ADR
0070's orchestrator drains the old archive's producers and workers and closes
its SQLite node. The next process generation constructs a new graph against the
selected file.

### Identity and persistence model

The station-global catalogue contains only the facts needed before a QSO file
can be opened:

```text
active_qso_archive_id
pending_qso_archive_id       // present only during an activation attempt
qso_archives[]:
    id                       // immutable UUIDv7
    label                    // operator-facing, mutable
    ownership                // managed | external
    path                     // canonical absolute path; EXTERNAL entries only
    last_activation_error    // diagnostic, never a false active state
```

Managed entries carry no writable path at all: their location is derived from
the archive UUID. Only a grandfathered/future CLI-created external entry records
a canonical absolute path.

Each QSO file contains a singleton `archive_metadata` row with the same UUID,
its creation time, and its `default_logbook_id`. The UUID mismatch cases
(wrong file at a registered path, a copied archive attached twice) fail closed.
The current `default_logbook_id` config field is migrated into this row and
becomes a compatibility projection while callers are moved to an active-archive
view. Numeric logbook IDs remain local implementation keys.

Logical logbooks also gain a stable UUID for backup identity. The numeric ID can
remain on local URLs and foreign keys; neither that number nor a mutable name is
suitable for cross-file/cloud identity. Copying an archive file is a **backup of
the same archive**, not a new archive. A future Clone operation must mint a new
archive UUID and explicitly decide how duplicated QSO/cloud identities are
handled; copying then registering the file as an independent archive is refused.

The catalogue cannot live in the active QSO file—the daemon needs it to find
that file. `config.json` is chosen over turning `reference.db` into a control
plane because it is already the atomic, owner-private, startup-critical source
for datastore and active-rig selection. The QSO file remains self-identifying so
the catalogue can be rebuilt by an explicit attach/recovery workflow.

### Filesystem policy

SPA-created archives are **managed files only**, rooted below:

```text
${working_dir}/db/qso-archives/<archive-uuid>.db
```

The UUID-derived filename is immutable; changing the label does not rename an
open or backed-up file. The daemon creates the directory as `0700` and the DB,
WAL, SHM, temporary provisioning files, and backups as `0600`, under the existing
ST-6 policy. The SPA receives a display path but never submits a path and never
gets an arbitrary “browse server filesystem” field.

Files outside `${working_dir}/db` are not creatable or attachable by the SPA.
An existing installation whose configured datastore is external is
grandfathered into the catalogue as `ownership: external`: it remains usable,
is never chmod'd, moved, renamed, or deleted by the daemon, and retains the
existing high-signal permission warning. A future explicit
`smd archive attach --path ...` may add external local files while the daemon is
stopped, after canonical-path, identity, schema, locking, and duplicate-UUID
checks; it is not part of the first SPA release. External entries are detach-only
from the app—physical deletion remains the operator's job.

The first release has no archive-delete operation. Removing a file that contains
the operator's history needs its own backup/retention decision; “cannot delete
active” is insufficient protection.

### Provisioning and activation

`POST /v1/qso-archives` accepts semantic fields (archive label and initial
logbook identity), never a path. A single-flight archive manager:

1. allocates UUIDs for the archive and initial logical logbook;
2. builds a temporary DB in the managed directory using the **log migration set
   only**;
3. writes `archive_metadata`, seeds the initial logbook/default, validates the
   schema and foreign keys, checkpoints and closes SQLite;
4. establishes ST-6 modes and atomically renames the file into place; then
5. atomically adds the inactive catalogue entry.

A failure before step 5 leaves the active archive unchanged and no selectable
half-built archive. Creation is idempotent through an operation/request key;
crash-left `.creating` artifacts are diagnosed and recoverable, never presented
as archives.

`POST /v1/qso-archives/{uuid}/activate` is also single-flight. It refuses an
unknown/unhealthy archive, a duplicate request already in progress, active RF
transmission or FT8 exchange, or a daemon without the explicit service-manager
respawn contract. (Archive activation has a stronger idle prerequisite than a
plain restart: changing the destination while an exchange is between TX slots
must not discard or ambiguously place its eventual QSO.) It persists
`pending_qso_archive_id` while retaining the current
`active_qso_archive_id`, returns `202`, and triggers the normal graceful restart.
The SPA shows “switching” and reconnects through the existing restart/SSE path;
it must not label the candidate active merely because the request was accepted.

At startup, a pending candidate becomes the **effective** selection for a newly
constructed graph even though the durable active ID still names the old file. It
is opened, migrated, identity-checked, and its database-dependent construction
graph is brought up before it is committed as active. On success the daemon
atomically promotes pending → active and clears the old selection **before HTTP
serves requests**. If that persistence fails, or candidate activation fails, the
candidate graph is rolled back, the error is recorded, pending is cleared, and a
new graph generation is constructed once against the last-known-good active
archive. This prevents a corrupt, missing, permission-changed candidate—or a
config write failure—from putting systemd into a restart loop or serving one
archive while persisting another. Failure of the last-known-good archive remains
a normal fatal datastore startup failure.

Archive selection therefore has these observable states:

```text
inactive -> pending -> active
                 \-> failed (old active remains active)
```

There is no in-process handle swap in this ADR. A future zero-process-restart
switch would have to construct a **new generation** of every DB-dependent node,
keep the HTTP control plane outside that generation, seal QSO/FT8 admission,
drain the old graph, and atomically publish the new generation. Restart already
does all of that at the process boundary with fewer mixed-generation states.

### Ownership of lifecycle

Responsibilities are deliberately split:

| Owner | Responsibility |
|---|---|
| archive manager | catalogue, managed-file provisioning, validation, and active/pending intent |
| config service | atomic persistence of the startup catalogue/selection |
| `sqlite.Service` | one generation's open QSO handle, migrations, and close |
| lifecycle orchestrator (ADR 0070) | order and bound the shutdown/start/rollback of the DB-dependent graph |
| HTTP API / SPA | request and observe operations; never manipulate a DB handle or path |

There is no longer a special service called “the main database.” There is one
**active QSO archive connection** in a daemon generation, plus two independent
station-global stores below.

### `reference.db` and `evidence.db`

Both remain single, station-global files and **do not switch** with the QSO
archive:

- `${working_dir}/db/reference.db` contains replaceable/operator-global
  enrichment caches (`country`, `contacted_station`). Sharing it preserves warm
  lookups across contests. It may learn that a callsign was contacted from any
  archive, but it is not an authoritative cross-archive QSO view. Cross-archive
  search, scoring, and “worked before” semantics must be designed explicitly;
  they must not be inferred by opening or joining inactive files on a live QSO
  request.
- `${working_dir}/db/evidence.db` is the station's continuous RF/evidence record,
  governed by its own capture/sync consent and retention policy. A QSO archive
  switch is not an RF discontinuity and must neither rotate nor reset evidence.
  Evidence's LC-3 must-drain shutdown remains unchanged.

The current `cmd/smd` derivation of both paths from
`filepath.Dir(datastore.path)` is removed. On a normal/fresh installation their
canonical paths derive from the working directory, so selecting an external QSO
archive cannot relocate or duplicate either global store. On upgrade, if the old
external datastore layout has already created either global file outside the
working directory, its observed canonical path is frozen as a grandfathered
external global-store path; the daemon neither moves nor chmods it. Relocation
into the managed directory is a later explicit operator action. Existing
one-time relocation/bootstrap behavior is otherwise preserved and made
independent of subsequent active-QSO selection. QSO pre-split backups become
archive-scoped (for example
`${working_dir}/db/backups/<archive-uuid>/`) so equal basenames cannot collide.

### SM Cloud impact

SM Cloud must model the archive boundary by identity, not by local storage path.
Before multiple local archives may upload, its protocol and Postgres schema gain:

- an `archives` entity unique on `(tenant_id, archive_uuid)` with a mutable label;
- stable `logbook_uuid` on cloud logbooks, belonging to an archive, rather than
  identity by `(tenant_id, name)`; and
- archive/logbook UUIDs in push, manifest, reconcile, and export/restore
  contracts.

SMC's service URL and bearer token remain station-global transport credentials
in `config.json`: they identify one tenant, not one physical file. The current
`credentials.logbook` placement string is deprecated in favor of the UUIDs, and
the reconciler becomes per logical logbook (the already-accepted direction in
ADR 0056). Other genuinely per-logbook destinations/credentials continue to
live in each QSO file under ADR 0056. A new archive starts with no such bindings
unless its creation wizard explicitly copies compatible bindings from an
existing logbook; it must never inherit a QRZ/ClubLog account merely because the
archive was created from the same station.

QSO UUID uniqueness remains `(tenant_id, qso_uuid)`, preserving ADR 0052's
single-writer identity. Moving an existing QSO between logical logbooks/archives
updates its placement; cloning the same QSO into two independently writable
archives is not supported. Evidence sync remains tenant-scoped and is not linked
to a QSO archive.

Existing SMC rows migrate into a per-tenant legacy archive. The first upgraded
client performs an explicit, idempotent adoption of its existing cloud logbook
rather than creating a second empty one. A tenant whose prior clients already
merged two physical files under the same cloud name is flagged for manual
recovery—the server cannot infer a boundary that was never recorded. During a
rolling upgrade the old name-only wire remains a compatibility path into the
legacy archive; creating/activating a second SMC-enabled local archive is gated
until the identity-aware server is available.

This makes ADR 0056's per-logbook dispatch/reconcile work (or its equivalent
identity-aware routing) a prerequisite for enabling SMC on several logical
logbooks/archives. Merely adding `archive_uuid` to the envelope while retaining
one boot-time `DefaultLogbookID` reconciler would preserve the existing blind
spot under a new name.

Export becomes archive-aware and can restore either one logical logbook or a
whole archive into a newly provisioned file. The cloud stores domain identity
and membership, not filenames, WAL files, local connection settings, or
`reference.db`/`evidence.db`.

### SPA and API surface

The archive selector sits one level above the existing logical-logbook selector
in the shared app shell. The header always names both, for example
`Archive: 2026 Field Day / Logbook: 7Q5MLV`, so identical logbook names in two
files are never visually confusable.

An Archives management view provides:

- label, active/pending/failed state, managed/external status, file size and
  last successful open;
- the initial-logbook creation form;
- Create and Activate actions, with a clear “restarts Station Manager and ends
  current operating activity” confirmation; and
- SM Cloud identity/backup status once the archive-aware protocol is available.

The first API slice is deliberately small:

```text
GET  /v1/qso-archives
POST /v1/qso-archives
POST /v1/qso-archives/{uuid}/activate
```

Create and activate are separate operations: creation cannot unexpectedly end
an operating session, and a prepared contest file can be inspected before use.
After restart, existing logbook/QSO endpoints naturally address the active
archive only. Inactive archives are not attached to the live connection and are
not accepted as a hidden archive parameter on ordinary QSO routes.

## Acceptance criteria

1. **Physical isolation.** After creating and activating archive B, a submitted
   QSO and its upload/history rows exist in B and not A. Reactivating A reveals
   A's unchanged logbooks and rows.
2. **No mixed generation.** Work accepted before activation is drained to A (or
   remains durably queued in A); work after the restarted daemon reports B active
   goes only to B. No handler, FT8 completion, worker, or reconciler uses A's
   handle after B is published active.
3. **Failure-safe activation.** A corrupt/missing/unopenable candidate never
   replaces `active_qso_archive_id`; startup rolls it back and serves the
   last-known-good archive with an observable failed-candidate diagnostic.
4. **Honest SPA state.** A `202` activation is displayed as pending/restarting,
   never active. The new archive appears active only after reconnect to a daemon
   that successfully opened it.
5. **Global-store continuity.** Switching A → B → A uses the same canonical
   `reference.db` and `evidence.db`; it creates no copies beside either QSO file,
   resets no evidence counters, and preserves the evidence shutdown drain.
6. **Cloud isolation and restore.** Two archives with the same label and same
   logical-logbook name produce distinct SMC archive/logbook identities and can
   each reconcile/restore without rows from the other. A legacy archive adopts
   its existing cloud rows without duplicate backup state.
7. **Path confinement.** No SPA payload can cause creation, rename, chmod, or
   deletion outside the managed archive directory. A grandfathered external DB
   remains usable and operator-owned, with warnings preserved.
8. **Upgrade without movement or loss.** An existing install is registered as
   its first archive in place, receives identities idempotently, retains its
   current default logbook, QSO/history/upload counts and cloud backup mapping,
   and can restart repeatedly without minting new identities. Existing external
   reference/evidence files remain at their frozen paths until explicitly moved.

## Alternatives considered

### Keep one SQLite file and use logical logbooks for contests

This remains the simpler workflow and should stay available, but it does not
provide the requested physical corruption, copy, migration, queue, or backup
boundary. It cannot be the only model once physical separation is a user
requirement.

### Expose `datastore.path` in the SPA and restart

Rejected. It makes a mutable filesystem path masquerade as archive identity,
permits path traversal/arbitrary server-file creation, loses per-file default
logbook state, gives no provisioning transaction or fallback, and collides in SM
Cloud when names repeat.

### Hot-swap the existing `sqlite.Service` in place

Rejected. Consumers retain the service and run concurrent work; safely swapping
would require draining and rebuilding every dependent. It also contradicts ADR
0070's terminal per-generation lifecycle. A process restart is already
available, TX-safe, observable, and supervised.

### Run one daemon process per archive

Rejected. Each process would contend for the rig, sound device, PSK Reporter
identity, socket/listener, evidence capture, and global config. The operator
wants selectable storage, not several station controllers.

### Rotate `reference.db` and `evidence.db` with each QSO file

Rejected. Reference data is reusable cache and evidence is a continuous
station/RF archive with independent consent and retention. Per-contest copies
would fragment evidence, duplicate caches, complicate SMC evidence sync, and
make selecting an external QSO file unexpectedly relocate unrelated private
data.

### Let the browser attach arbitrary external paths

Rejected for the first release. A browser cannot select a server-local path in a
portable way, and accepting text paths expands the API into a filesystem
management surface. Existing external configuration remains supported; a
future stopped-daemon CLI is the explicit escape hatch.

### Keep SM Cloud name-keyed and prefix names with the filename

Rejected. Filenames and labels change, paths differ by machine, and prefixes do
not give restore a durable archive boundary. Stable UUID identity is the same
lesson already applied to QSOs.

### Keep SM Cloud aware only of logical logbooks

Stable logbook UUIDs alone would prevent row merging, and the physical grouping
could remain a local implementation detail. Rejected because the operator has
made that grouping durable domain state: it names the contest boundary they
expect to back up, inspect, and restore as a unit. Omitting it would preserve QSO
payloads but lose archive membership/labels and make “restore this archive” a
manual regrouping exercise. SMC still does not store the SQLite file itself; it
stores the minimal archive/logbook identity needed to reconstruct one.

## Consequences

- The normal managed path is simple and safe; advanced external storage is
  deliberately less convenient and remains operator-owned.
- Archive activation costs a graceful daemon restart and the service manager's
  restart delay. This is visible but rare (normally once at contest start/end)
  and avoids mixed database generations.
- `config.json` gains a small startup catalogue while each log DB gains archive
  metadata and logical-logbook UUIDs. `datastore.path` needs a compatibility
  migration rather than abrupt removal.
- `default_logbook_id` becomes per archive. Code that reads it directly from a
  global config snapshot must move behind an active-archive projection.
- Import/restore commands need an archive selector (default active) and should
  eventually support `--new-archive`; they must not silently target whichever
  file an old `datastore.path` happens to name.
- Operational backup must include `config.json`, all managed QSO archive files,
  `reference.db`, and `evidence.db`. A single active DB copy is no longer a full
  station backup.
- SMC receives a real schema/protocol migration. This is required work, not an
  optional polish: without it, physical local isolation and off-site backup
  would tell contradictory stories.
- Archive creation/listing adds bounded inactive-file I/O, but ordinary logging
  still holds exactly one QSO DB open. Cross-archive query/federation is deferred.

Recommended delivery order:

1. local archive/logbook identities + compatibility migration and SMC
   identity-aware schema/protocol/adoption;
2. archive catalogue, managed provisioner, active-path resolution, and
   last-known-good startup fallback;
3. create/list/activate API over the attended restart contract;
4. shared-shell archive UX and end-to-end fault/restore drills; then
5. optional stopped-daemon external attach and, only if restart friction proves
   material, a separately-designed in-process graph-generation switch.

The local first-archive migration is database-first and idempotent: migrate and
write missing archive/logbook UUIDs inside the legacy QSO file, then persist a
catalogue entry containing the UUID read back from that file. If the process
dies between those writes, the next start observes and reuses the same embedded
identity; it never mints a second archive merely because the catalogue write did
not complete.

## Triggers to revisit

- If archive switches become frequent enough that the restart delay disrupts
  operation, design generational in-process replacement against ADR 0070; do not
  mutate the existing handle in place.
- If operators need inactive archives in one report or contest-scoring view,
  add a read-only archive query/index service rather than attaching arbitrary
  files to live QSO requests.
- If removable/external local storage is a common workflow, promote the stopped-
  daemon attach command and define disappearance/remount semantics before adding
  it to the SPA.
- If SMC intentionally becomes a byte-for-byte file backup rather than a
  domain-level QSO backup, revisit its archive metadata model; SQLite files still
  must not be uploaded while live without a consistent snapshot operation.
- If an archive must be independently writable on two devices, ADR 0052's
  single-writer invariant is broken and conflict semantics must be decided
  first.

## References

- ADR 0070 — declarative daemon lifecycle graph and per-generation supervisors.
- ADR 0052 — SM Cloud backup identity and single-writer QSO invariant.
- ADR 0055 — selected logical logbook and station identity.
- ADR 0056 — per-logbook relational config belongs in the QSO database.
- `internal/config/config.go` — current `datastore.path` and
  `default_logbook_id` startup state.
- `internal/database/sqlite/service.go` and `bootstrap.go` — current QSO handle,
  migration-set split, and reference bootstrap.
- `cmd/smd/main.go` — current QSO/reference/evidence path and lifecycle wiring.
- `internal/forwarding/smcloud` and `internal/cloud/store` — current name-keyed
  cloud logbooks and tenant-scoped QSO UUIDs.
