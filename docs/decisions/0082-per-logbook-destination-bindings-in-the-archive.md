---
number: 0082
title: Destination bindings live per logbook inside the archive; station accounts stay in config.json
status: Accepted (rulings confirmed 2026-09-25)
date: 2026-09-25
---

# 0082 — Destination bindings live per logbook inside the archive; station accounts stay in config.json

> **Dated update (2026-09-25, Codex P2 on `c3df0e12`, fixed in 5B).** Part 4's collapse rule for
> a downgrade — "the default logbook's binding name when present, otherwise the lexicographically
> first" — infers the name an older, config-driven build drains, and the inference is wrong when
> the default logbook changes after the seed or the legacy name sorts after a UUID-derived one
> (`station-qrz` after `qrz.<uuid>`): rows would be stranded under a name config never carried.
> The mapping is now DURABLE: the seed records `logbook_destination.legacy_name` on every row it
> creates, and both the 0014 down step and the 5C config downgrade collapse to that name first;
> the inferred rule survives only as the fallback for bindings that never derived from config.

## Context

W-0021 shipped physical QSO archives (ADR 0071): one open SQLite file per archive, a catalogue in
`config.json`, activation by restart. Forwarding did not move with it. Every destination is still one
station-global entry in `config.forwarders[]` carrying `enabled`, the credential blob, the transport
and the cadence, and every enqueue site (`qsoservice/submit.go`, `delete.go`, `stamp_sync.go`,
`enqueue.go`, `submit_batch.go`) fans out over that one list. An interim gate
(`archive.ForwardingAdmitted`) admits forwarding only in the adopted Home archive, so a contest
archive uploads nowhere and the Forwarding tab shows the station's settings with a banner saying they
do not apply here. Drill 2 (2026-09-24) showed that this reads wrong: the operator's eye goes to the
destination's state, and the state shown is not this archive's.

ADR 0056 already drew the boundary: per-logbook or relational → the database; one-physical-station
or startup-critical → `config.json`. Its binding rows were never built. ADR 0071 (dated update to
0056) placed them inside the archive and ruled that a new archive inherits none. Two facts from the
2026-09-24 design exchange sharpen the boundary further. First, credentials are not uniformly
station-wide: a QRZ.com API key belongs to one QRZ logbook (`qrz.go:122`, "Per-logbook"); a QRZCQ
upload authenticates with an account callsign plus key; a ClubLog upload authenticates the user with
an account email and an application password and is filed under one callsign, while the key that
identifies Station Manager itself is injected at build time (ADR 0054); SM Cloud's URL and bearer
token identify the tenant, and the same token is reused by evidence sync
(`config.EvidenceSyncCredentials`). Second, the operator's granularity: one switch per destination
for the active archive, defaulting to off for a new archive, with per-logbook exceptions belonging to
Settings → Logbooks.

Three load-bearing mechanics constrain any design. `qso_upload` is keyed by `forwarder_name`
(`UNIQUE (qso_id, forwarder_name, action)`), each worker claims rows by that name, and the ADIF upload
stamp is written per type prefix (`QRZCOM_…`). Forwarders are constructed once at daemon start from
config (`spawnForwarderWorkers`); every forwarder setting is restart-required (`config.md` §11.2), and
ADR 0039 discards a disabled forwarder's pending rows at that start. A QSO and its queue rows commit
in one transaction (invariant "QSO and upload-queue writes are atomic"); only a broken local database
may prevent a QSO from being logged. The SM Cloud store still identifies a logbook by
`(tenant_id, name)` (`store/migrations/0001_init.up.sql:26`) and the reconciler is built once for the
boot-time default logbook (`lifecycle_adapters.go:744`); ADR 0071 named the identity-aware protocol
as the prerequisite for a second SMC-enabled archive.

## Decision

A **destination binding** is a row per `(logical logbook, destination type)` inside the archive file.
It carries the on/off state, the logbook-scoped credential fields for that destination, and the
immutable `forwarder_name` that keys its queue rows and its worker. `config.json` keeps one
**station account** per destination type: the fields that identify the application or the tenant
(SM Cloud URL and token, the transport policy, endpoints, cadence and retry). The interim gate is
retired: an archive forwards exactly what its enabled bindings say, and a new archive has no bindings.

The parts, each settling one item of the 2026-09-24 direction:

1. **Binding schema (archive file, log-set migration 0014).**

   ```sql
   CREATE TABLE logbook_destination (
       id                INTEGER  NOT NULL PRIMARY KEY AUTOINCREMENT,
       created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
       modified_at       DATETIME,
       logbook_id        INTEGER  NOT NULL REFERENCES logbook (id) ON DELETE CASCADE,
       destination       TEXT     NOT NULL,   -- registered forwarder type: qrz | qrzcq | clublog | smcloud
       forwarder_name    TEXT     NOT NULL UNIQUE,
       enabled           INTEGER  NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
       credentials       TEXT,                -- type-owned JSON; logbook-scoped fields only
       remote_adopted_at DATETIME,            -- when the remote identity was established (SM Cloud)
       CONSTRAINT uq_logbook_destination UNIQUE (logbook_id, destination)
   );
   ```

   Migration 0014 also adds nullable `archive_metadata.destination_bindings_seeded_at`. It is the
   one-time adoption marker: the adopted Home archive sets it in the same transaction as the seed;
   new managed archives set it during provisioning, and a non-legacy archive migrated later is
   marked not-applicable without seeding. A logbook created after that point therefore stays
   unbound even if a failed config rewrite has left legacy keys on disk.

   `forwarder_name` is minted once and never operator-editable. It is not presented as a setting;
   the bindings readout may carry it only as the opaque routing handle required by the existing
   name-keyed queue actions. In the ordinary one-logbook adoption, the seed (part 4) carries the
   legacy config entry's `name`, so Home's existing rows, workers, stamps and
   `/v1/forwarder/{name}/…` paths are untouched; a binding created afterwards is named
   `<destination>.<logbook uuid>`. An ordinary binding edit disables rather than deletes the row;
   its credentials survive a disable so re-enabling needs no re-entry. Explicit credential removal
   is separate (part 8). There is **no archive-level row**: the "one switch per destination" the
   operator sees is an aggregate over the archive's live logbooks — `on` when every logbook's
   binding is enabled, `off` when none is, `mixed` otherwise — and flipping it writes every
   logbook's row in one SQLite transaction. An absent binding counts as off and is created by the
   aggregate write. A per-logbook exception is the same row edited alone. The registry declares each
   credential field's scope (`station` or `logbook`) in the type descriptor, so the split is an
   enumerated allowlist the SPA renders from, never a hard-coded table in the client.

2. **A new logbook starts unbound**, whatever the archive-wide state, for every destination —
   including SM Cloud. Nothing binds implicitly, because a binding is either a credential that
   identifies someone's remote logbook (wrong to inherit) or a cloud identity that the operator should
   knowingly create. The aggregate switch then reads `mixed` and names the unbound logbook, and the
   logbook creation form offers the destinations that need no new secret (SM Cloud) as explicit
   unchecked options. **Ruled 2026-09-25:** this applies to SM Cloud too; no destination auto-binds.

3. **Credential ownership per destination.** Logbook-scoped (in the binding): QRZ `api_key`; QRZCQ
   `call` and `key`; ClubLog `email`, `password` (the application password) and `callsign` (defaulting
   to the logbook's callsign; one ClubLog account may hold several); SM Cloud `logbook` (the cloud
   logbook *name*, kept only to adopt the legacy `(tenant, name)` row once — identity is by UUID
   after that). Station-scoped (in `config.forwarders[]`): SM Cloud `url` and `token` and
   `allow_insecure_http`; every type's `endpoints`, `tick_interval_sec`, `batch_size`, `retry`,
   `label`; the ClubLog application key stays build-injected (ADR 0054) and is reported to the SPA
   only as present/absent. `action_filter` remains station-scoped and continues to narrow the type's
   registered supported actions; omitting it would otherwise widen an existing operator policy.
   `enabled` and the durable instance `name` leave the canonical station entry: whether an archive
   uploads, and the queue key it uses, are binding facts. The station configuration holds at most
   **one entry per destination type**. Config v6 preflights a legacy file with two entries of one
   type before any database or config write and refuses it by the conflicting names; the operator
   must choose the one station account the new model can represent (the multi-instance generality
   had no consumer).

4. **Idempotent Home adoption; legacy `config.json` fields.** On the first start of the legacy
   (adopted) archive under this build, while its `destination_bindings_seeded_at` marker is NULL, the
   daemon snapshots the binding-owned legacy config fields and seeds — in one transaction in the
   archive file — one binding per legacy forwarder on **each logical logbook present at that
   moment**, with that entry's enabled state and logbook-scoped fields copied in (ADR 0056
   implementation requirement 2, W-0021 review finding 5b). Disabled entries are seeded disabled:
   moving ownership must not erase a saved key merely because its route is currently off. The same
   transaction sets the marker after all inserts and queue renames; a failure rolls back both.

   For each destination the archive's default logbook keeps the legacy entry's `name`. Additional
   logbooks receive `<destination>.<logbook uuid>`; in the same transaction every existing
   `qso_upload` row for such a logbook and legacy name is renamed to that binding's name. This is the
   only general shape compatible with both the table's global `forwarder_name` uniqueness and the
   one-worker-per-name invariant. The station's ordinary one-logbook Home therefore changes no queue
   name or API path. The duplicate-type preflight in part 3 runs before this transaction.

   The seed is a Go-side startup step, not SQL (SQL cannot read `config.json`). The marker makes a
   committed seed final: a retry never overwrites a durable row or binds a later-created logbook.
   Only after that transaction commits does the daemon rewrite `config.json` once, file-first,
   stripping binding-owned `name`, `enabled` and logbook-scoped credential keys from every station
   entry. A failed rewrite is retried on the next start independently of whether rows now exist; the
   already-created bindings win. Config v6 keeps the legacy keys **known but deprecated** until that
   strip, so ADR 0074's unknown-key rejection never fires on an unmigrated v5 file (ADR 0075's
   pattern). A new managed or external archive never seeds: it starts with no rows.

   Config v6 has an explicit, data-aware down path to v5. With the daemon stopped,
   `smd config-downgrade --to 5` reads the adopted Home archive before `smd db-downgrade` removes
   migration 0014, recombines each station account with its bindings, and restores the v5
   `name`/`enabled`/credential shape. It proceeds only when every Home binding of a destination can
   collapse to one v5 instance without changing enabled state or credentials; otherwise it refuses
   before rewriting and directs the operator to reconcile the bindings or restore the secured
   pre-v6 config copy. The collapse name is the default-logbook binding's name when present,
   otherwise the lexicographically first binding name; a station account with no binding becomes a
   disabled v5 entry with a deterministic unused name derived from its type. The 0014 down path
   collapses UUID-derived queue names to the same target, then drops the table and seed marker. The
   ruled rollback order is therefore **config 6 → 5 first, log schema 14 → 13 second**. Rehydrating
   secrets into owner-only `config.json` is the sole explicit exception to part 8 and exists only
   for rollback.

   Entering config v6 may therefore leave the known deprecated keys in place: the stripping rewrite
   is gated specifically on the adopted Home archive's committed marker, never on whichever archive
   happens to be active. Because the seed runs only when the Home file is open, a station whose
   active archive is another one at upgrade keeps its legacy config fields until Home is next
   activated; meanwhile that other archive forwards nothing (no rows), exactly as under the retired
   gate.

5. **Queued rows when a destination is disabled or its credentials change.** Binding edits are
   restart-required, like every forwarder setting today: the daemon takes one snapshot of the active
   archive's enabled bindings at start and builds the worker set and the enqueue set from it, so
   every queued row has a worker by construction. At start, per binding: an enabled one is built
   (its forwarder constructed from the binding's fields plus its station account), its `auth`-classed
   failures re-armed once (W-0010 outcome 9), and its worker spawned; a disabled one, and any
   `forwarder_name` in the queue that matches no binding, has its `pending`, `failed` and
   `in_progress` rows discarded loudly, `uploaded` rows kept for their `upstream_id` (ADR 0039's rule,
   now per binding). Changed credentials leave rows alone: the next attempt uses the new fields, and
   the re-arm covers rows the old credential had failed. A save whose bindings differ from the running
   snapshot answers `restart_required: true`; the tab shows the same "restart to apply" it shows
   today. **Ruled 2026-09-25:** this slice does not rebuild workers or enqueue routing live.

6. **Routing to all four destinations; atomic writes.** Every enqueue site replaces
   `forwardersForEnqueue()` with the snapshot's enabled bindings **for the QSO's logbook**, filtered
   by the station account's `action_filter` within the destination's registered supported actions as
   today (`shouldEnqueue`); the QSO row and its binding
   rows still commit in one transaction and a queue-row failure still rolls the QSO back. The
   `forwarded_to` log field, `POST /v1/forwarder/{name}/uploads`, `queue/clear`, `queue/retry` and
   `GET /v1/forwarder-queues` keep their contracts with binding names; the backfill's
   `forwarder_unavailable` now means "no enabled binding of that name for the active archive", and
   `smd import --forward <name>` names bindings. `forwarding_gated` and `gate_reason` are retired from
   the queues readout; a QSO in an archive with no enabled binding for its logbook is stored with
   `forwarded_to: []`, no error, no banner.

7. **SM Cloud identity and legacy-cloud adoption** (the client side of ADR 0071 §"SM Cloud impact").
   An SM Cloud binding holds no secret. On the wire a push, manifest, reconcile and export carry
   `archive_uuid`, `logbook_uuid` and the mutable labels; the server gains the `archives` entity and
   `logbooks.uuid`, unique on `(tenant_id, uuid)`, QSO identity staying `(tenant_id, qso_uuid)`
   (ADR 0052 single writer). Adoption of the existing cloud row is an **explicit, idempotent call**
   from the client, keyed by the binding's legacy `logbook` name, that stamps the cloud logbook with
   the local UUIDs inside the per-tenant legacy archive and returns the same answer on repeat; the
   client records `remote_adopted_at` and never sends the name again. Adoption is coordinated once
   for an archive, not attempted independently by several bindings against the same legacy name. If
   several seeded local bindings point at that one name, the client cannot infer which retentive
   cloud-only rows belong to which local logbook: it keeps compatibility pushes and the existing
   default-logbook legacy reconciler, starts no identity-aware reconciler, and reports
   `legacy_logbook_ambiguous` for manual recovery rather than stamping one cloud row with several
   UUIDs.

   A server that does not report identity support on `GET /v1/version`, or a legacy mapping still
   ambiguous as above, keeps the name-only compatibility wire for the adopted archive's seeded
   bindings and **refuses to enable an SM Cloud binding anywhere else** with a named reason — the
   last remnant of the interim gate, now scoped to one destination and lifted only when the server
   and adopted mapping are identity-ready. Once identity-ready, one reconciler runs per enabled SM
   Cloud binding (per logical logbook), replacing the boot-time default-logbook reconciler;
   `POST /v1/smcloud/reconcile` runs them all and answers an aggregate (ADR 0056 requirement 4).
   `smd restore` addresses a cloud logbook by UUID, into an archive selected by `--archive`, and a
   whole-archive restore provisions a managed file carrying the archive UUID.

8. **Binding credentials never leave the archive during normal operation.** The cloud receives the
   projected QSO body, logbook identity (UUID, name, callsign) and archive identity;
   `logbook_destination` rows are never pushed,
   exported or restored — a restored archive starts with no bindings and the operator re-enters keys
   (stated on the restore output). Local copies stay under the existing `0600`/`0700` regime
   (`SecureDataFiles` covers the data files and the backup directory); ADIF exports carry QSOs only.
   Binding constructors and startup findings name the field and the fault, never the value (the
   `Build` rule, `registry.go`); the "QSO stored" line and every `forward.*` or Station Event carries
   the binding's `forwarder_name` and outcome only. The binding API is masked-on-GET
   (`credentials_set` lists keys that hold a value) and merge-on-PUT (blank keeps the stored value),
   the `ForwarderInfo` contract unchanged in kind. An explicit `credentials_clear` list removes named
   stored fields; it is accepted only when the resulting binding is disabled, so clearing a required
   field cannot create an enabled binding that will fail the next start. Disable-and-clear may be one
   atomic PUT. `GET /v1/config` stops serving logbook-scoped fields because they no longer exist there.

9. **The Forwarding tab, reworked.** It is titled for the **active** archive and edits only that
   archive: a binding write goes to `PUT /v1/qso-archives/{uuid}/bindings`, refused with `409` when
   `{uuid}` is not the active archive (an inactive file is closed; opening it for an edit is not in
   this decision). **Ruled 2026-09-25:** there is no inactive-archive edit path in this slice. The PUT
   validates the complete candidate first and commits all affected binding rows atomically. Section
   "Destinations for this archive": one card per registered type with the
   aggregate switch (`on` / `off` / `mixed`) and, expanded, one row per live logbook with its own
   switch, its logbook-scoped fields (masked: set or blank), its queue counts and its Retry / Clear
   actions. Turning a switch on with a required field blank rejects the complete save, names and
   marks the field, and restores the last persisted switch state; no sibling row changes. The switch
   therefore never claims more than the daemon saved. A card whose station account is missing (SM
   Cloud without URL and token), or whose server lacks identity support in a non-adopted archive,
   shows its switch disabled with the reason and the link to where it is fixed (no unactionable
   instruction). Section "Station accounts":
   the SM Cloud URL and token, the ClubLog application key as present/absent, no on/off pill anywhere.
   Saving bindings shows the existing restart-required banner and the Restart daemon control. The
   archive creation form gains no forwarding controls; it states that a new archive starts with every
   destination off and that Forwarding is set after activation.

10. **Characterization proof before the migration.** A test pinned in its own commit before 0014
    lands records, for a config shaped like the station's (`qrz`, `clublog` and `smcloud` enabled,
    `qrzcq` present and disabled), the exact `qso_upload` rows and `forwarded_to` list one live submit
    produces in the adopted archive and the worker set spawned at start; the same test passes
    unchanged after the seed. A sibling proves the disabled QRZCQ binding and its credentials survive
    without a row or worker, and a two-logbook fixture proves names and existing queue rows are
    partitioned without two workers sharing a name. A newly created managed archive produces zero
    rows, `forwarded_to: []`, and spawns no worker. The paired down proof reconstitutes the v5 config
    and queue names before dropping 0014. Two archives each bound to SM Cloud under their own UUIDs
    then prove ADR 0071 acceptance criterion 6.

## Alternatives considered

### Archive-wide rows only (one row per destination per archive)

The literal "one switch per destination". Rejected because the credential is the identity of a
specific remote logbook or account: an archive holding two callsign logbooks would upload the second
callsign with the first's QRZ key — ADR 0056's original objection to "global list plus per-logbook
enable". The aggregate switch gives the same one-switch experience over per-logbook rows.

### Two levels: an archive default row plus per-logbook overrides

Rejected because inheritance makes a new logbook start uploading under a default it never chose, the
exact accident ADR 0071 forbids for archives, and because the precedence logic is a second table and a
resolver for no observable gain over the aggregate view. If per-logbook exceptions turn out never to
be used, the UI can hide the rows; the schema needs no second level either way.

### Credentials stay in `config.json`; the archive binds by reference

Bindings would carry a credential id pointing into a station-global list, so an archive file never
holds a secret and export is safe by construction. Rejected by the operator's 2026-09-24 ruling and
on its merits: the credential *is* the remote logbook's identity and belongs with the logbook it
serves; a pointer list re-creates in `config.json` the N×M matrix ADR 0056 rejected; and an archive
attached on another station would point at ids that station does not have. The cost — secrets in
the archive file — is met by part 8 rather than avoided.

### One worker per destination type, resolving credentials per row

ADR 0056's sketch: the worker looks up the binding for each row's logbook. Rejected because it
changes the `Forwarder` interface for all four types, makes one instance hold several accounts'
state (ClubLog's 403 breaker is per account), and gives up `forwarder_name` as the queue key that the
backfill, clear, retry and queue readout already use. One worker per binding reuses the existing
constructor and worker unchanged.

### Apply binding edits live, without a restart

Rejected for this decision: the worker set and the enqueue snapshot are startup-bound today for
every forwarder setting, and a live rebuild is a lifecycle change under ADR 0070 with its own drain
and rollback semantics. Restart-required keeps the invariant "every queued row has a worker" trivially
true and matches what the tab already tells the operator.

### Let the cloud adopt by name on the first identity-aware push

Rejected: ADR 0071 requires explicit idempotent adoption because two physical files may already have
been merged under one cloud name and the server cannot infer a boundary it never recorded. An
explicit call can refuse with a diagnostic; a silent match cannot.

### Bind SM Cloud automatically for a new logbook when the archive's others are bound

Considered because a logbook silently missing from the backup is a data-loss risk. Not chosen: an
implicit binding is the one thing this decision otherwise forbids, and the `mixed` state plus the
creation form's explicit option make the omission visible at the moment it is made. Reopen if a
logbook is found unbacked in practice.

### Keep the interim gate and bind only SM Cloud in this slice

The dossier's earlier plan. Superseded by the 2026-09-24 direction: with switches per destination
the gate is redundant, and leaving QRZ/ClubLog unbindable would keep the Forwarding tab showing
settings that are not this archive's.

## Consequences

- **Schema and config.** Log-set migration 0014 (`logbook_destination`, with a down step that first
  restores legacy queue names, then drops it) and config v6 (deprecated binding-owned keys,
  one-entry-per-type validation, and the data-aware down path in part 4). Schema head pins move to
  14; the rollback drill runs on copies before the migration commit, as ruled 2026-09-22.
- **Archive files hold secrets.** Already `0600` inside `0700`; the new fact is that copying or
  attaching an archive moves keys with it. Export and restore exclude bindings (part 8); the manual
  and the restore output say so.
- **Startup.** The workers node reads the active archive's bindings, builds one forwarder and worker
  per enabled binding, discards per binding, re-arms per binding, and starts one SM Cloud reconciler
  per bound logbook. The seed and the config strip are two more idempotent startup steps on the
  adopted archive only. Evidence sync reads the SM Cloud station account's presence, not `enabled`.
- **API.** New `GET`/`PUT /v1/qso-archives/{uuid}/bindings` (active archive only), a `scope` on
  `/v1/forwarder-types` credential fields, `restart_required` on the bindings response,
  `forwarding_gated`/`gate_reason` retired, `/v1/config` forwarders narrowed to station accounts,
  `POST /v1/smcloud/reconcile` aggregated. `api-endpoints.md` and `config.md` change in the same
  commits.
- **SM Cloud protocol and Postgres migration.** `archives`, `logbooks.uuid`, UUIDs on the wire, the
  adoption call and a version flag; the dev Postgres tests (`SMCLOUD_TEST_ALLOW_DEFAULT=1`) gate the
  server commits. Trunk with a tag, as ruled: the client refuses non-adopted SM Cloud bindings until
  the server reports support, so every commit stays releasable.
- **SPA.** The Forwarding tab is rewritten (part 9); the gate overlay and its tests go; the archive
  creation form gains one sentence.
- **Accepted costs.** Restart-required binding edits; no editing of an inactive archive's bindings;
  a station upgraded while a non-Home archive is active carries legacy config fields until Home is
  next activated; N workers per archive instead of one per type; a v5 downgrade refuses when the
  bindings cannot be represented without changing routing; an already-merged multi-logbook legacy
  cloud mapping needs manual recovery before identity-aware reconcile.

## Triggers to revisit

- If a destination arrives whose credential is genuinely station-wide with no per-logbook part, it
  binds with an empty logbook scope and the model holds; revisit only if such a destination also
  needs no per-logbook on/off. (LoTW and HamQTH are unbuilt; their scopes are settled when their
  forwarders are, ADR 0073 for HamQTH.)
- If the LAN master/node topology (W-0021 exchange 2026-09-22) is selected, a node's bindings point
  at the master and the master relays with a new upload `origin`; that ADR extends part 7, it does not
  reopen the binding shape.
- If operators need to change a binding without a restart (a contest mid-run switching QRZ keys),
  reopen the live-apply alternative as an ADR 0070 lifecycle change.
- If editing an inactive archive's bindings is wanted, it needs a second, short-lived connection
  policy for closed files — a separate decision.
- If a logbook is found missing from the cloud backup in practice, reopen the auto-bind alternative.

## References

- ADR 0056 (per-logbook config in the database; dated update 2026-09-19), ADR 0071 (first-class QSO
  archives; §"SM Cloud impact", acceptance criteria 6 and 8), ADR 0039 (enabled gates enqueue;
  startup discard), ADR 0038 (durable connectivity retry), ADR 0052 (SM Cloud identity, single
  writer), ADR 0054 (ClubLog API key build injection), ADR 0055 (station identity by logbook),
  ADR 0070 (lifecycle graph), ADR 0074/0075 (unknown-key rejection and retired-key migration),
  ADR 0078 (ambiguous write outcome).
- `docs/work/W-0021-qso-archives.md` — design exchanges of 2026-09-22 and 2026-09-24 (forwarding per
  archive, credential ownership corrections), review findings 2b and 5b, drill 2 evidence.
- `docs/v2-design/forwarding.md` §2, §4, §6, §7; `docs/v2-design/config.md` §3.3, §7, §11;
  `docs/v2-design/api-endpoints.md` (forwarder, queue, smcloud and archive endpoints).
- Code anchored: `internal/qsoservice/forwarders.go` (`forwardersForEnqueue`, the gate),
  `internal/archive/gate.go`, `cmd/smd/lifecycle_adapters.go` (`startWorkers`, `startReconciler`),
  `internal/forwarding/registry.go` (`TypeDescriptor`, `CredentialField`, `Build`),
  `internal/forwarding/{qrz,qrzcq,clublog,smcloud}` credential structs and descriptors,
  `internal/api/handler_config.go` (`ForwarderInfo`), `internal/cloud/store/migrations/0001_init.up.sql`,
  `internal/database/sqlite/migrations/log/0012_archive_identity.up.sql`.
