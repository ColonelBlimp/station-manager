# W-0021 — First-class QSO archives (ADR 0071 programme)

**Status:** Selected — slice 1 in progress (rulings (a)–(e) settled 2026-09-22)
**Selected:** 2026-09-21 (operator: "Close outcome 9 and start the ADR 0071 archives")
**Outcome:** The operator can create a physically separate QSO database for a contest, activate it
through an attended restart, and later return to the home archive — with every archive and every
logical logbook carrying a stable identity, the existing installation adopted in place without
movement or loss, `reference.db` and `evidence.db` staying station-global, and SM Cloud keyed by those
identities rather than by name. [ADR 0071](../decisions/0071-first-class-qso-archives.md) (Accepted
2026-09-19, files first) is the design; this dossier sequences its delivery and records evidence.

`W-0021` is an immutable identity. Its status may change, while priority and ranked position live only
in [`docs/backlog.md`](../backlog.md).

## Why this item exists

The data-model chain ruled on 2026-09-19 (W-0014 exchange): archives with stable identity come before
Settings → Logbooks and before contesting, so `default_logbook_id`, the SM Cloud identity and the
ADR 0056 bindings are built once against the archive model. A contest today can only be a logical
logbook inside the one file; the operator asked for the physical boundary (own file, queue, history,
backup, corruption boundary) and conceded the trade it makes (history is per file).

## Acceptance criteria

ADR 0071's eight criteria are the acceptance, unchanged: (1) physical isolation, (2) no mixed
generation, (3) failure-safe activation, (4) honest SPA state, (5) global-store continuity, (6) cloud
isolation and restore, (7) path confinement, (8) upgrade without movement or loss. Each slice below
names the criteria it proves. Nearest confusable outcomes to keep apart: a *copied* file registered as
a second archive (a backup of the same archive, refused); a candidate labelled active on a `202`; a
catalogue that names an archive whose file's embedded UUID differs (fail closed); an install that
mints a fresh archive UUID on every restart because the catalogue write raced the file write.

## Verified state (2026-09-21, tree at `3791bf87`)

Nothing of the programme exists in code: no `archive_metadata`, no `qso_archives`, no logbook UUID
(`logbook` is keyed by integer id and a UNIQUE name, migration 0001). `datastore.path` is the active
selector (`config.go:1360` defaults it to `<data_dir>/db/station-manager.db`); `cmd/smd` derives the
reference and evidence paths from `filepath.Dir(datastore.path)` (`lifecycle_adapters.go` startLogDB /
initEvidence; also `import.go`), with a one-time reference split and evidence relocation already in
place. `default_logbook_id` is read directly from the config snapshot in three packages (api: logbook
delete guard, FT8 completion, config handler; cmd/smd: import, restore, ensureDefaultLogbook, the
smcloud reconciler). SM Cloud identifies a logbook by `(tenant_id, name)` (cloud store migration
0001) and QSOs by `(tenant_id, uuid)` (0004). The log schema head is 11. UUIDv7 minting exists
(`utils.NewUUIDv7`). The station's live file is the default in-place path under the working
directory, not under a managed `db/qso-archives/` directory.

## Slice plan

Delivery follows ADR 0071's recommended order; each slice is its own commit series, RED-first, with
the compatibility promise that a daemon at any slice boundary starts the existing station unchanged.

1. **Local identities and in-place adoption** (AC 8, and the identity half of AC 6/7).
   - Log migration 0012: `archive_metadata` singleton (`archive_uuid`, `created_at`,
     `default_logbook_id`) and `logbook.uuid TEXT UNIQUE` — nullable at migration time, because SQL
     cannot mint UUIDv7; a Go-side idempotent backfill runs once per start after `Migrate()`, inside
     one transaction, minting only where NULL, and `InsertLogbook` mints on every runtime create so
     no row is ever born without one (review finding 4: a restart-only proof would miss it). Schema head 12 (pins in `handler_version_test` and the
     migration tests move with it).
   - Config catalogue: `active_qso_archive_id` and `qso_archives[]` (`id`, `label`, `ownership`,
     `path` for non-managed entries, `last_activation_error`). Adoption at start is database-first
     and idempotent (ADR 0071): write missing identities inside the file, then persist the catalogue
     entry with the UUID read back; a crash between the two reuses the embedded identity.
   - `default_logbook_id` moves into `archive_metadata`; the config field becomes a projection of the
     active archive (`ensureDefaultLogbook` and the seven consumers above read through it). Its
     existing self-heal (correct a wrong id to the only row) is preserved by characterization test.
   - **Critical order at start (reviewer, 2026-09-22; ADR 0071's database-first rule):** migrate
     the QSO file; in one transaction fill the missing archive and logbook UUIDs and the
     `default_logbook_id` pointer; read the archive UUID back from the file; only then persist the
     v4 catalogue entry. A restart after a failed catalogue write finds the embedded identity and
     reuses it — never a second UUID. The paired database and config downgrade drill is proven
     before this migration commit reaches trunk.
   - Built 2026-09-22, sub-commit A of slice 1: `Service.DowngradeLogSchemaTo(target)` (log set
     only, down only, dirty refused, returns the version it left) and `smd db-downgrade --to N
     --yes [--config p]` over the same container wiring as `smd import`, opening WITHOUT
     `Migrate()`; `docs/install.md` §7 gains the rollback recipe. Proofs: without the down-only
     guard a target of 11 from version 10 migrates up (test names it); without `--yes` the
     command runs. Rows retained across 11 → 10 asserted on QSO, upload and logbook.
   - Built 2026-09-22, sub-commit B of slice 1: config schema v4 — `types.QsoArchiveConfig`
     (`id`, `label`, `ownership` managed|legacy|external, `path`, `last_activation_error`),
     `Config.ActiveQsoArchiveID` / `PendingQsoArchiveID` / `QsoArchives` with `QsoArchiveByID`;
     the migration table gains `down`, v3→v4 stamps only, v4→v3 strips the three keys;
     `config.DowngradeDocument` (whole path checked before any step, floor named) and
     `config.WriteDocument` (raw durable write, same mode rules); `validateQsoArchives`
     (`invalid_qso_archive`); `smd config-downgrade --to N --yes`; `config.md` §3/§13 and
     the install guide's rollback recipe. PUT /v1/config carries no catalogue field — pinned by
     a test that saves a writable block and reads the catalogue back untouched. Proofs: no down
     step registered → the 4→3 test fails; validator unwired → every rule passes vacuously;
     stray argument → the default file would be rewritten.
   - **Rollback drill run 2026-09-22 (ruling (e)), both binaries, PASSED.** Harness:
     `scripts/rollback-drill.sh --new <smd> --new-schema N --new-config M --old <smd> --old-schema N
     --old-config M` — takes a consistent SQLite online backup of the live `station-manager.db`
     and a credential-scrubbed copy of `config.json` into a `0700` scratch under home, rewrites
     `data_dir`, `datastore.path`, `socket_path` (unix) and any `qso_archives[].path` naming the
     live file into it (any other catalogue path is refused), turns off forwarders,
     bridge, FT8, evidence capture and sync, SMTP, PSK Reporter and every lookup provider, refuses
     to start unless every resolved database path (catalogue entries included) lies under the
     scratch — re-checked after each daemon ran, since adoption writes paths — asserts the
     expected schema and config versions after the new start, after the downgrade and as reported
     by the old daemon (so a skipped step or an unmigrated build fails rather than passing on
     identical rows), fingerprints the rows
     (count + sha256 of sorted ids/uuids for qso, qso_history, qso_upload, logbook,
     operator_event), boots the NEW binary on the copy (it persisted config v4 at start), runs
     `smd db-downgrade` / `smd config-downgrade` with the new binary, boots the OLD binary, and
     compares; the config copy is shredded on exit. Run 1, old = the `pre-w0021` tag built from a
     worktree (schema 11, config 3): config 4 → 3, no db step needed, tagged build answered
     `/v1/version` schema 11. Run 2, old = the frozen alpha.3 RPM's binary (schema 9, config 3):
     db 11 → 9 (0011 and 0010 down), config 4 → 3, alpha.3 answered schema 9. Both runs: rows
     identical before → after — qso 8,129 (`70cc82b355b8a3f3`), qso_upload 15,733
     (`def65b35cbc93ca4`), qso_history 2, logbook 1, operator_event 0. The same command reruns
     the drill once migration 0012 exists, which is the gate before sub-commit C reaches trunk.
     Second review of the harness the same day fixed four findings (catalogue paths unconfined;
     a no-op run could pass; db + WAL copied at two instants; shred cannot reach replaced inodes
     — credentials are now scrubbed before the copy is written); both runs repeated green with
     the version assertions, and a negative run claiming schema 12 for the new build failed as
     required.
     Also from the b0d94f13 review: `--to 0` is now refused by an explicit floor at both
     boundaries (version 0 is no schema; 0001's down drops every table) rather than left to the
     migration library's error, with tests. Review of `7d99af72` (P1, valid): the fingerprint
     hashed row ids only, so a rewritten retained value would still read ROWS IDENTICAL. It now
     hashes every column of every protected table (rows sorted by key, NULL and blobs encoded
     explicitly); the verdict compares every column both schemas share, names the ones the
     downgrade drops — for 11 → 9 exactly `qso_upload.failure_class` and
     `qso_upload.upstream_id_generation` — and fails on any column present only after.
     Sensitivity proven with `--mutate-one-row` (a hook that changes one `qso.call` in the scratch
     copy, selecting a different value and refusing an empty QSO table): the run reports
     `qso.call: value hash changed`. Both drills rerun green under the
     full-column verdict. Follow-up review the same day: a dropped column was merely listed, so
     a down migration that removed `qso.call` would still pass — the verdict now takes an EXACT
     expected drop set (`--expect-dropped`, empty for the tag run, the two upload columns for
     alpha.3) and fails on any other drop or on an expected drop that did not happen; proven on
     the verdict function alone (a removed `qso.call` → "columns dropped that the drill did not
     expect") and by an alpha.3 run with an empty expected set, which fails. Both drills green
     with their exact sets.
   - `datastore.path` stays honoured as the compatibility selector until slice 2 resolves the path
     from the catalogue; a config whose path names a file with a different embedded UUID than the
     catalogue entry fails closed with a named diagnostic.
   - Proofs: fresh install adopts once; a second start mints nothing (identities byte-identical);
     a logbook created through the API after start reads a non-NULL UUID that survives restart;
     a crash after the file write and before the catalogue write is simulated and reuses the UUID;
     `GET /v1/version` or a new read surface reports the archive identity; a copied file at a second
     path is refused.
2. **Global stores off the QSO path, catalogue provisioning and last-known-good startup** (AC 3, 5,
   7). Reference and evidence paths derive from the working directory (frozen at their observed
   canonical paths when an old external layout already created them); managed provisioning under
   `db/qso-archives/<uuid>.db` (0700 dir, 0600 files, log migration set only, `.creating` artefact
   diagnosed and never listed); active-path resolution before the lifecycle graph is built;
   pending → active promotion before HTTP serves, rollback to the last-known-good archive on any
   candidate failure with `last_activation_error` recorded.
   - **One path resolver for every entry point** (review finding 2): the catalogue → active-archive
     resolution lives in one place used by the daemon, `smd import` and `smd restore`; both commands
     print the archive they target (label and UUID), accept `--archive <uuid>` (ADR 0071
     consequence) and never fall back to `datastore.path` once the catalogue exists. Proof: after
     activating B, an import lands in B; `--archive A` lands in A; neither derives `reference.db`
     from the QSO file's directory.
   - **Interim archive forwarding gate** (review finding 1; lands here, before activation exists):
     until the ADR 0056 per-logbook bindings ship, forwarding is admitted only in the adopted
     archive. In any other archive `shouldEnqueue` yields no rows for any destination, the boot
     SM Cloud reconciler and the auth re-arm are not constructed, `smd import --forward` is refused
     with a named reason, and `GET /v1/forwarder-queues` plus the Forwarding card state that
     forwarding is off in this archive until per-logbook bindings exist. Proof: a submit in a second
     archive with every forwarder enabled writes zero `qso_upload` rows and no reconciler starts.
     The gate is retired by the bindings, which replace it with explicit routing.
3. **API over the attended restart** (AC 2, 4): `GET/POST /v1/qso-archives`,
   `POST /v1/qso-archives/{uuid}/activate` (single-flight; refuses a daemon without the respawn
   contract; `202` + graceful restart). **Sealed admission** (review findings 3 and 3b): the
   activation manager takes the TX admission seal as one check-and-set at the two places that
   already decide "busy" atomically. First the FT8 service, under its established order
   `seqGate → txMu → s.mu` (the same acquisition `ClaimProfile` makes): if `seq.Active()`, or
   `txInFlight` (set under `txMu` before the slot wait, so a manual send waiting for its slot
   counts), or TX armed, the request is refused; otherwise `switchSealed` is set, and every
   admission path that holds those locks — `ClaimProfile`, `ArmTx`, `TransmitNext`, every
   `StartQso*`/`StartCallCq` site (13 `seqGate` holders) — refuses with a named
   `archive_switch_pending` reason. Then the bridge, under its own single-flight snapshot: if
   `tuneActive || ft8TxActive` the FT8 seal is dropped and the request refused; otherwise
   `txSealed` is set and `StartTune` and the FT8 keying entry refuse. Only then is
   `pending_qso_archive_id` persisted and the graceful restart requested, seals held until exit.
   Nothing is checked outside the lock that guards it, so there is no window. An armed but idle
   FT8 session counts as busy (ruled 2026-09-22): activation answers 409 until the operator
   disarms. **Abort path** (review finding 3c): the seals are owned by the activation attempt and
   released on every exit that is not the restart. Persisting the pending id has the three
   outcomes the config service already reports (PT-6): definitive failure → nothing on disk, both
   seals released, 500 `activation_persist_failed`, no restart; success → restart requested;
   durability uncertain (rename done, sync unconfirmed) → treated as success and the restart
   requested, because startup is safe under either truth (pending present → candidate activation
   with rollback; absent → the old archive serves, and the SPA reads the active archive honestly
   per AC 4) and the 202 body carries `durability: "uncertain"`. If the restart request itself
   fails after a persist, the attempt clears the pending id and releases both seals; if that clear
   also fails, the seals are still released, the response is 500 `pending_unclear`, the archive
   lists as `pending` with the diagnostic, and the next restart — whenever it comes — performs the
   activation with the ordinary rollback, which the operator has been told. Proofs: after a
   definitive persist failure an FT8 claim succeeds immediately; an uncertain write yields 202
   with the marker and a restart request; a failed restart request leaves no pending id and no
   seal; a failed clear leaves no seal and a `pending_unclear` listing. Proofs, each an
   interleaving: a manual send waiting for its slot → refused; a tune carrier → refused and no
   seal left behind; with the seal up, each of `StartQso`, `TransmitNext`, `ArmTx`, `ClaimProfile`
   and `StartTune` is refused with the named reason; the plain `POST /v1/restart` guard is
   unchanged. `api-endpoints.md` in the same commits.
4. **SPA**: the archive selector above the logbook selector in the shell header, the Archives view
   (label, state, ownership, size, last open; create form; Activate with the restart confirmation),
   and the end-to-end fault/restore drills (AC 1 on the station, with a real second archive).
5. **SM Cloud identity** (AC 6): `archives` entity, `logbook_uuid`, archive/logbook UUIDs on push,
   manifest, reconcile and export/restore, per-tenant legacy-archive adoption, and the reconciler per
   logical logbook (ADR 0056 archive-aware). **It also lays the first ADR 0056 binding** (review
   finding 2b): a `logbook_forwarders`-style row inside the archive, with SM Cloud as its first and
   only bindable destination — the archive creation form offers "back this archive up to SM Cloud"
   explicitly (ADR 0071's copy-compatible-bindings), the enqueue path routes SM Cloud by that row,
   and the interim gate narrows from "no forwarding outside the adopted archive" to "no destination
   without a binding"; QRZ, ClubLog and the rest stay unbindable until Settings → Logbooks extends
   the table. **The adopted archive keeps every route it has** (review finding 5b): slice 5's
   migration seeds, once and idempotently, one binding row per globally enabled forwarder on each
   logical logbook of the adopted archive, so its QRZ, ClubLog and SM Cloud uploads continue
   unchanged across the slice boundary; only a NEW archive starts with no bindings. Proof: a
   characterization test pinned before the migration (the set of `qso_upload` rows one submit
   creates in the adopted archive) passes unchanged after it. AC 6 is then provable with two
   archives each bound to SM Cloud under their own UUIDs.
   Until this ships, creating or activating a second SMC-enabled local archive is refused with a
   named reason (ADR 0071's gate), so slices 2–4 are usable on the station with SM Cloud bound to
   the adopted archive only. Needs the Postgres dev DB
   for its tests (`SMCLOUD_TEST_ALLOW_DEFAULT=1`).

Deferred by the ADR and not planned here: archive delete, external attach CLI, in-process switch,
cross-archive query.

## Design exchanges

- **2026-09-22, operator: "a new archive will look to itself for dupes?"** Yes, for both dupe
  questions the code asks today, because both are answered inside the open QSO file: the
  submit-time duplicate (the `dedupe_key` over call/band/mode/freq/date/HHMM, unique per file,
  `qsoservice/submit.go`) and the contest/worked-before check (`GET /v1/contest-dupe`,
  `IsContestDuplicateByLogbookIDWithContext`, and the FT8 run's `held: worked_before`), which is
  scoped to one logbook and therefore to the active archive. A new contest archive starts with no
  dupes and no worked-before holds from the home archive — the isolation the contest wants. The
  shared `reference.db` `contacted_station` cache does learn calls from every archive, but ADR 0071
  names it non-authoritative for cross-archive "worked before"; cross-archive dupe or scoring
  semantics are deferred and would be a read-only index service, never a join on inactive files.
- **2026-09-22, operator: "back-up to SMC will continue and each archive will be recoverable from
  SMC?"** Yes once slice 5 ships; with a stated interim. SMC's URL and token stay station-global
  (one tenant); what becomes per-archive is identity: the cloud gains an `archives` entity and a
  stable `logbook_uuid`, every push/manifest/reconcile carries them, and restore can rebuild one
  logbook or a whole archive into a newly provisioned file. The home archive adopts its existing
  cloud rows idempotently (no second empty copy). Only the ACTIVE archive uploads and reconciles —
  an inactive file has no worker and logs nothing, so it has nothing new to back up; its cloud copy
  stays as it was. Until slice 5, ADR 0071's gate holds: a second SMC-enabled archive is refused, so
  a contest archive created in slices 2–4 is local-only (plus the operator's ADIF export) and the
  home archive's backup continues unchanged. A new archive inherits no forwarding bindings (ADR
  0056 archive-aware), so SMC is enabled for it explicitly, never by accident.
- **2026-09-22, operator: "will each archive gain its own config.json or section for forwarders and
  other params?"** Neither. ADR 0056's rule stands: per-logbook or relational → the database;
  one-physical-station, startup-critical or hand-edited → `config.json`. So each archive carries
  its own bindings INSIDE its file (`logbook_forwarders`-style rows on the logical logbooks:
  which destination/account each logbook uploads to, and any per-callsign enrichment account),
  while `config.json` keeps exactly one station-global copy of the forwarder credentials and
  transport (QRZ key, ClubLog account, SMC URL and token, SMTP), the rig, bridge, FT8 and server
  settings, plus the new archive catalogue (which archive is active — startup-critical). A new
  archive starts with no bindings; the creation form may copy compatible ones explicitly. Those
  binding rows are unbuilt today (ADR 0056 dated update 2026-09-19) and are the first thing Settings
  → Logbooks builds after this programme, on the active archive.
- **2026-09-22, operator: "a LAN master SMD with node SMDs forwarding to it — a complication for
  this design?"** No; the archive design is the prerequisite for it. Nothing in the records names
  a LAN topology today (ADR 0052 defines the single-writer rule for SM Cloud; ADR 0071 is silent),
  so this is a first note, not a decision. Shape: a node → master link is the same shape as
  node → SM Cloud — a passive receiving store, one writer per QSO (ADR 0052), merged by identity —
  so the identity-aware protocol slice 5 builds (archive UUID + logbook UUID + QSO UUID on the
  wire) is what a master would consume; a master SMD would embed an SMC-style receiver rather than
  a new protocol, and its aggregate would be an archive of its own holding rows attributed to their
  source archives. What archives make possible: every node's contest file has its own UUID, several
  nodes can run the same contest callsign in logbooks that are distinct by UUID, and the catalogue
  is per machine (`config.json` on each node). The one genuine design item, and it is the same
  item ADR 0071 already defers: station-wide "worked before" for a multi-operator contest. Today
  the dupe question is answered inside the node's own file; with several nodes it must be answered
  by the master, which turns a local read into a network query with latency and availability to
  design (a cached answer that may be stale versus a refusal when the master is unreachable). Also
  to rule when selected: the master is a mirror, never an editor (edits at the master would break
  single-writer), or an explicit ownership handover. Not in W-0021's scope; it gets its own ADR
  after the contesting ADR, with archives and slice 5 as its foundation.
  Clarified the same day, operator: **the master is the one forwarding to SM Cloud** (nodes →
  master → SMC). Consequences to carry into that ADR: (i) nodes never bind SM Cloud themselves —
  one writer toward SMC per QSO, so a node's contest archive binds only the master destination
  (the per-logbook bindings make this a routing fact, not an operator discipline); (ii) the master
  is a relay, not only a store: a row received from a node — insert, update or delete — is
  re-enqueued to SMC from the master's own queue, which needs a new upload `origin` (e.g. `relay`)
  and means an edit made at the node reaches SMC two hops later through two queues, each with its
  own retry and reconcile; (iii) ownership per row: the originating node is the writer of its rows
  and the master relays them unchanged, while the master writes its own rows if it is also an
  operating position — no row has two writers, so ADR 0052 holds end to end; (iv) SM Cloud sees
  the master's tenant and the master's aggregate archive; whether SMC also records the source node
  archive per row (attribution survives the hop) is a ruling for that ADR; (v) node ↔ master
  reconcile is the SMC reconcile mechanism one hop down, and a node that cannot reach the master
  keeps its rows durably queued exactly as it does today for an unreachable destination.
- **2026-09-22, operator: "trunk with a tag, or a branch, for this structural change?"**
  Recommendation: trunk, with a tag and a rollback drill. Grounds: every commit here is built to be
  releasable on its own (the slice plan's invariant — a daemon at any slice boundary starts the
  existing station unchanged; activation is not even routed until slice 3 is whole), the per-commit
  Codex review, CI, the dev-build dogfood loop and the second coder all work on the shared HEAD,
  and the hot files (`config.go`, `lifecycle_adapters.go`) are exactly where a long-lived branch
  would rot. A branch would trade many small reviewed steps for one large merge the review flow is
  not built for. Safeguards that make trunk safe: (1) a tag at the last green main before slice 1's
  migration commit, so a known-good build can be redeployed; (2) slice 1 includes a rollback
  drill — the tagged binary opens a database already migrated to 0012 (golang-migrate stops at the
  last migration its source knows; the schema check tests for missing objects, not extra ones —
  an inference to prove, not assume) and the 0012 down migration is exercised; (3) the frozen
  alpha.3 RPM remains the independent fallback; (4) no feature-flag framework — a half-built
  surface is simply not routed or not rendered until its slice is complete. A branch is worth it
  only for slice 5 if the SM Cloud server and client must change together and the dev Postgres
  cannot host both versions; decide then.
  **Ruled 2026-09-22 (operator): trunk with a tag; the rollback drill as I first wrote it was
  wrong and is revised.** The tagged binary cannot open a schema-12 file: golang-migrate's
  `readUp` first requires the file's current version to exist in the binary's bundled source
  (`migrate.go:535`), the service treats that error as fatal (`migrations.go:126`), and the config
  loader rejects unknown keys after migration (`config.go:599`) and a newer-than-supported config
  version (`config.go:576`), so new catalogue keys alone would stop the old build. A branch would
  only postpone the same problem to merge. **Revised drill, run on disposable copies of the
  station's `station-manager.db` and `config.json` before the schema commit lands on trunk**
  (verified 2026-09-22: config migrations have no down step at all — `config/migrations.go:410`
  "downgrade is not supported" — and the frozen alpha.3 build pins schema head 9, so it already
  cannot open the live schema-11 file; migrations 0010 and 0011 landed after the freeze):
  (1) migrate a copy up to 0012 and apply the 0012 down migration — slice 1 ships the operator
  path for that (a `smd` subcommand that migrates the log set down to a named version; today only
  a test helper exists); (2) roll the config back to the tagged shape — slice 1's config migration
  is the first with a down step (the table gains one for it), or the catalogue lives at the same
  config version and the downgrade guide names the keys to strip; (3) start the tagged binary against
  both and prove QSO, `qso_upload`, history and operator-event row counts and UUIDs are byte-
  identical to the pre-drill copy; (4) repeat (3) with the frozen alpha.3 RPM's binary, which
  likewise needs the compatible data and is not a direct fallback for upgraded files. The drill
  is recorded in this dossier with the commands and counts before the migration commit.
  Refined the same day (operator): step 4 is not a repeat of step 3 — after 0012 down the file
  is at 11 and alpha.3 knows 9, so the alpha.3 drill also applies 0011 and 0010 down before its
  boot, then verifies the retained rows and QSO UUIDs (the `failure_class` and
  `upstream_id_generation` columns are what those downs drop; QSO rows are untouched). And the
  boot proofs run ISOLATED: copying the files does not isolate them — the station's config would
  start forwarder workers, connect to the rig and CAT, and could sync evidence or send mail — so
  the drill runs in a throwaway working directory with every forwarder disabled, the bridge and
  CAT driver off, evidence capture and sync off, SMTP off, and no network reachable, and the
  config copy (it holds credentials) lives in a `0700` scratch directory under home as `0600`
  files, shredded after. **Ruled: config schema v4 with an explicit down step** — the migration
  convention (`config/migrations.go:9`) bumps the version whenever the shipped shape changes, so
  the catalogue keys arrive as v4 and the table gains its first down migration for the drill.
  Further detail (operator, same day): a throwaway working directory alone does not isolate the
  drill — a non-empty `data_dir` in the copied config overrides `SM_WORKING_DIR`
  (`utils/working_dir.go:30`) and SQLite opens the config's `datastore.path` directly
  (`sqlite/service.go:86`), so an unedited copy would open the LIVE station file. The test config
  therefore rewrites `data_dir` and `datastore.path` (and, once they exist, the catalogue paths)
  into the scratch directory, and the drill asserts every resolved database path — QSO,
  reference, evidence, backups — lies under the scratch root before either binary is started;
  a path outside it aborts the drill.

## Review findings on the plan (2026-09-22)

Four findings from the operator's review, each fixed in the slice plan above rather than deferred:
(1) P1 interim forwarding — a second archive would have queued to every globally enabled
destination; slice 2 now carries the adopted-archive forwarding gate before activation exists.
(2) P1 `smd import` / `smd restore` — both open `datastore.path` and derive `reference.db` from it
(`import.go:177`, `restore.go:183`); slice 2 routes them through the one resolver with `--archive`.
(3) P1 activation idle window — `POST /v1/restart` checks only the bridge's TX state
(`handler_restart.go:42`); slice 3 seals FT8 claims and the TX arm before the check and holds the
seal through the restart. (4) P2 logbook UUID on runtime create — `InsertLogbook` mints in slice 1
with a create-after-start proof.
Second review, same day: (3b) P1 the seal covered claims and arming but not `StartQso`,
`TransmitNext` on an already armed session, a manual send waiting for its slot (invisible to the
bridge's keyed state), or `StartTune`; the seal is now a check-and-set under the FT8 lock order
that already owns those admissions plus a bridge-level seal for tune, and the busy check is inside
the acquisition. (2b) P1 slice 5 could not prove cloud isolation with the slice 2 gate in force;
slice 5 now lays the first ADR 0056 binding row with SM Cloud as the only bindable destination,
narrowing the gate to "no destination without a binding".
Third review, same day (armed-but-idle rule agreed: 409 until disarmed): (5b) P1 the adopted
archive's QRZ and ClubLog routes would have stopped at slice 5 under "no destination without a
binding" — slice 5 now seeds its bindings from the enabled forwarders, idempotently, with a
characterization proof across the boundary. (3c) P1 seals held until exit would have left TX
admission sealed on a persist failure — the abort path is now specified for definitive failure,
uncertain durability, a failed restart request and a failed clear, each with a proof.

## Rulings wanted before slice 1 code

**Ruled by the operator 2026-09-22: (a) `legacy` ownership for the adopted file; (b) Go-side UUIDv7
backfill with UUIDs minted on runtime creation; (c) SM Cloud identity in slice 5 behind the interim
forwarding gate. The slice 1 implementation discussion is complete: proceed RED-first in the recorded
database-first order, with the paired rollback drill proven before the migration commit reaches trunk.**

- (a) **Ownership of the adopted in-place file.** The live station file sits at the default path
  under the working directory but not in the managed `db/qso-archives/` layout, so it can be neither
  "managed" (path derived from the UUID) nor "external" (outside the working directory) as ADR 0071
  defines them. Recommend a third value, `legacy`: registered in place with its recorded path, ST-6
  permissions applied as today, never moved or renamed by the daemon; a later explicit "move into the
  managed directory" is a separate operator action. Alternative: treat it as `external` and drop the
  permission management it has today (not recommended).
- (b) **Logical-logbook UUID minting.** Recommend Go-side backfill after `Migrate()` (UUIDv7, like
  QSOs), the column nullable in SQL and enforced NOT NULL by the service after backfill. Alternative:
  a random (v4) `DEFAULT` in SQL, which breaks the project's v7 convention and time-ordering.
- (d) **Ruled 2026-09-22:** an armed but idle FT8 session is busy for activation (409 until disarmed).
- (e) **Ruled 2026-09-22:** trunk with a tag; config schema v4 with an explicit down step; the
  rollback drill (both binaries, isolated boot) runs before the migration commit.
- (c) **Slice 5's position.** Recommend after slices 2–4 with the "one SMC-enabled archive" gate in
  force, so the station gains physical archives first. Alternative: ADR 0071's literal order (SMC
  identity inside step 1), which blocks every local slice on the Postgres schema and protocol work.

## References

- [ADR 0071](../decisions/0071-first-class-qso-archives.md), [ADR 0056](../decisions/0056-per-logbook-config-in-database.md)
  (dated update 2026-09-19), [ADR 0070](../decisions/0070-daemon-lifecycle-graph.md), ADR 0052.
- [W-0014](W-0014-deferred-product-workstreams.md) — the 2026-09-19 exchange and the files-first ruling.
- `docs/v2-design/config.md` and `api-endpoints.md` change with slices 1–3.
