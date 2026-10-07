# W-0021 — First-class QSO archives (ADR 0071 programme)

**Status:** Selected — slice 1 complete and drilled at schema 12 (2026-09-22)
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
   - **Built 2026-09-22, sub-commit C of slice 1 (the migration commit).** Log migration 0012:
     `archive_metadata` singleton (`singleton = 1` CHECK, `archive_uuid` UNIQUE, `created_at`,
     `default_logbook_id` REFERENCES logbook ON DELETE SET NULL) and `logbook.uuid TEXT` with a
     partial UNIQUE index; down drops index, column, table. Schema head 12 (all pins bumped;
     sqlboiler models regenerated — the generator moved its shared helpers into the new model
     file). `types.Logbook.UUID` (never accepted from a client); `InsertLogbookWithContext`
     mints; `Service.ArchiveIdentityWithContext` / `EnsureArchiveIdentityWithContext(ctx,
     defaultLogbookID)` (one transaction: identity row only if absent, default pointer only for
     an existing row, uuid for every NULL logbook; returns the row as READ BACK) /
     `SetArchiveDefaultLogbookWithContext`. `cmd/smd` `adoptArchive` runs in `startQso` after
     `ensureDefaultLogbook`, in the ruled database-first order: identity ensured and read back →
     catalogue reconciled against that UUID (empty → legacy entry "Home" at the canonical
     symlink-resolved path + active; same UUID → untouched, path refreshed; active entry naming a
     DIFFERENT uuid → refused by name, nothing rewritten) → `default_logbook_id` projected (file
     wins when it names a row; a file with none learns config's). A catalogue persist failure
     warns and continues on the in-memory catalogue; the next start registers the same UUID.
     First-run setup records the seeded default in the archive before the config commit
     (`seedDefaultLogbook`). `GET /v1/version` gains `archive {id, label, ownership}` (never the
     path). Proofs: insert without minting, backfill disabled, mismatch guard off, projection off
     — each fails its test by name; adoption tests run on a real file (the shared test helper
     pins ":memory:", where two services even shared one store). Review found a startup ordering
     case: when the file already names a default but config's projected id is stale and missing,
     the old self-heal could seed a spurious row or fail on a duplicate name before adoption ran.
     The self-heal now defers to an existing file default; the next adoption projects it. A real-file
     regression test first failed on the duplicate-name path, then passed with that guard.
   - **Rollback drill at schema 12, both binaries, PASSED (the gate before this commit):** new
     build opened the station copy, migrated 11 → 12, adopted it (identity + catalogue written,
     config v4); tag run 12 → 11 / 4 → 3, tagged build answered schema 11, rows identical, source
     columns dropped: none; alpha.3 run 12 → 9 / 4 → 3, alpha.3 answered schema 9, rows identical,
     source columns dropped exactly `qso_upload.failure_class`, `qso_upload.upstream_id_generation`.
     Lesson recorded in the harness: `--expect-dropped` names the SOURCE copy's columns the old
     schema lacks; a column the new schema adds and the down removes (`logbook.uuid`) is invisible
     to the before/after comparison and is covered by 0012's own down test.
   - **Deployed 2026-09-22 as `2.0.0-alpha.3-24-g74d5cf70` (slice 1 commit `74d5cf70`;
     startup self-heal defers when the file already has a default, then adoption projects it, so a
     stale config default cannot seed a duplicate logbook). Station reads, all passive:**
     `GET /v1/version` → schema 12, `archive {id 01a0c8cb-904f-7394-afca-ef922b41bbfb, label Home,
     ownership legacy}`; `smd.log` one line `startup: archive catalogue written (existing
     installation adopted in place)` with that id and the canonical path, no persist warning;
     `config.json` version 4, one catalogue entry (legacy, that path), active = that id, pending
     absent, `default_logbook_id` 1; the file's `archive_metadata` row holds the same UUID with
     default logbook 1; the one logbook now carries uuid `01a0c8cb-904f-713c-af34-5f887f153a22`,
     visible on `GET /v1/logbook`. AC 8 holds on the live install (registered in place, no move,
     counts unchanged). Still to observe: a second restart writes no new catalogue line (the
     idempotency proof on the real file; proven in tests).
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
   - Built 2026-09-22, sub-commit 2A: package `internal/archive` (`Resolve(cfg, id) Paths` —
     active or named entry, pre-adoption fallback to `datastore.path`, refusals by name for an
     unknown id, an active id outside the catalogue, or entries with no active; `PathFor`
     derives a managed path from the id; `Reference`/`Evidence` under `<data_dir>/db` with the
     freeze rule for a file observed beside an external QSO file; `Backups` archive-scoped).
     `cmd/smd` `resolveArchivePaths` + `openArchiveDatabases` shared by `smd import` and
     `smd restore` (both gain `--archive <uuid>` and print the resolved archive); the daemon
     resolves in `run()` via `startupPaths` before the graph is built and `startLogDB` opens
     `paths.QSO`, `startRefDB` `paths.Reference`, `initEvidence` relocates into `paths.Evidence`;
     `datastore.path` no longer selects an adopted QSO file; its original directory remains the
     station-global store freeze anchor. Proofs: import dropping the
     `--archive` value lands in the active archive (test names it); the resolver preferring the
     file beside the QSO file over the global one fails the freeze test and the import test that
     asserts `reference.db` under `<data_dir>/db` with an external QSO file. Maintainability
     baseline ratcheted for `runImport`/`runRestore` (the shared open sequence removed nine
     branches from each). `install.md` and `config.md` §3 updated. Review of 2A (three findings,
     all fixed with RED tests and proofs): (i) the freeze rule keyed on the SELECTED file's
     directory, so A → B → A would have moved evidence.db — it now keys on the station's
     pre-archive layout (`filepath.Dir(datastore.path)`), proven by a three-step A → B → A resolve;
     (ii) `--archive B` took the default logbook from the active archive's config projection — the
     commands now use the targeted file's identity default (`targetLogbook`), falling back to
     config only for the active or not-yet-adopted archive and refusing a named archive without
     one, proven by an import into B whose default is its logbook 2 while config says 1;
     (iii) `sqlite.Service.Initialize` created/checked the directory of `datastore.path` before
     `SetDatabasePath` applied — the check now runs at `Open` on the effective path, proven by a
     stale, uncreatable `datastore.path` with a good managed path opening cleanly.
     Codex review of the commit (`cc1078b7`, P1, valid): the catalogue path was trusted — a legacy
     or external entry pointing at another archive's file would have been split, migrated and
     written to. `sqlite.PeekArchiveIdentity` (a separate read-only connection, before any write)
     and `verifyArchiveIdentity` now run before the split in both the commands' shared open
     sequence and the daemon's `startLogDB`: a file holding another archive's identity is refused
     naming both ids; a file with no identity behind a catalogue entry is refused too.
     Proofs: B pointing at A's file is refused and A stays unchanged; a named archive with no
     identity is refused; guard disabled → the import into B is accepted.
     Second review of the follow-up: (i) the guard's "active entry may be identity-less"
     exception was wrong — under the database-first order an entry, active included, exists only
     after the identity was written, so an identity-less file behind ANY entry is mis-pointed;
     refused now, proven by a default import into an active entry whose file has no identity;
     (ii) the chosen-id write committed the identity before the logbook backfill and a retry
     returned early — the backfill now commits inside the same transaction and a same-id retry
     runs the idempotent backfill, proven by an orphan NULL-uuid row repaired on retry.
   - Built 2026-09-22, sub-commit 2B: `archive.ForwardingAdmitted(entry)` (nil or `legacy` →
     admitted); `qsoservice.SetArchive` / `ForwardingAdmitted` / `forwardersForEnqueue` — the one
     list every enqueue path iterates (live submit, edit, delete, stamp sync, manual backfill,
     import), `forwarding_gated` refusals for the manual backfill and for an import naming
     forwarders; the daemon sets the archive in `startQso` and `startWorkers` returns after the
     orphan sweep in a gated archive (no discard, re-arm, workers or reconciler, one log line);
     `smd import` sets the TARGET archive; `GET /v1/forwarder-queues` carries `forwarding_gated`
     + `gate_reason`, retry answers 400 `forwarding_gated`; the Forwarding card shows the reason
     once (role=status) and disables Retry. Proofs: enqueue ignoring the gate → a live submit in a
     managed archive enqueues; daemon gate off → the boot re-arm runs; import not naming its target
     → `--forward` into B is accepted. Lesson: an import with no `--forward` never enqueues, gate or
     not — the first submit-path test was vacuous until it used the live `Submit`.
   - **Built 2026-09-23, sub-commit 2C:** `internal/archive/manager.go`
     (single-flight `Manager.Create`: mint ids → build `<managed>/<id>.db.creating` with the log
     set → seed logbook + `WriteArchiveIdentity` → `CheckIntegrityWithContext` → close to a single
     file, sidecars checked → chmod 0600 → rename → INACTIVE catalogue entry; failure at any step
     removes the file; request-key idempotency; `RequestError` for 400s; `DiagnoseCreatingArtefacts`
     logs and returns leftovers, never deletes) + `sqlite.Service.CheckIntegrityWithContext`; five
     tests green (provision, idempotent key, validation, persist failure leaves nothing, artefacts).
     The catalogue entry is written with `config.Service.Update` (file first; a failed write
     leaves memory untouched — `UpdateInMemoryThenPersist` would have kept a half-built archive
     selectable in memory, which the persist-failure test caught). The daemon runs
     `DiagnoseCreatingArtefacts` in `startQso` after adoption and records the result
     (`d.creatingArtefacts`) — lifecycle test: a stray `.creating` file is named, left in place
     and never listed. Proofs: removal at step 5 skipped → the placed file survives a failed
     catalogue write; identity not written → the provisioned file has none; request key not
     remembered → a retry makes a second archive. Fixture lesson: the persist-failure test must
     lock only the config directory — locking `data_dir` blocks the managed directory at step 2
     and never reaches step 5. `config.md` §3 and the install guide's backup set updated.
     Review of 2C (five findings, all fixed with RED tests): (i) P1 request-key idempotency was
     process-local — the key now lives on the catalogue entry (`request_key`, config v5 with a
     down step; duplicates refused by validation) and a NEW manager returns the same archive;
     (ii) P1 the backup guidance ignored WAL — it now requires a stopped daemon or SQLite's online
     backup, never a live copy; (iii) P2 an existing 0755 managed directory stayed 0755 and SQLite
     created the file under the umask — the directory is chmod'd 0700 and the `.creating` file is
     pre-created 0600 before SQLite opens it, proven on a pre-existing 0755 directory; (iv) P2
     cleanup failures were discarded — a failed removal is logged, folded into the returned error,
     and unlisted `.db` files are diagnosed at start beside `.creating` ones; (v) P2 the log-only
     rule was unproven — the provisioning test asserts no reference tables and no reference
     migration tracking in the new file.
     Third review round (three findings, fixed with RED tests): (i) P1 the managed directory
     could be a symlink to outside the working directory — `ensureContainedManagedDir` refuses a
     symlinked directory and any path that resolves outside (`fsperm.Contained`), secures it with
     `fsperm.SecureApplicationPath` (never through a symlink) and verifies 0700; proven with a
     symlink to an external 0755 directory that stays 0755 and empty (with the two symlink checks
     disabled the fsperm mode verification still refuses — defence in depth, the proof names it);
     (ii) P2 a durable retry returned an incomplete result — the logbook identities left the
     contract (the file's default logbook is read like any other) and the retry test compares
     the whole result; (iii) P2 a close-time cleanup error was discarded — folded through
     `withCleanup`, unit-proven on an unremovable file. The dogfood-inbox note logged today is a
     separate path, outside this commit. Fourth round (two findings, fixed): (i) P1 a PARENT
     symlink (`<data_dir>/db` → outside) was created through before the refusal — containment is
     now checked on the deepest existing ancestor BEFORE `MkdirAll` (with the old order the test
     finds `qso-archives` created outside); (ii) P2 the close-branch fix had no valid proof — the
     pre-create is its own helper with an injectable close, and reverting the branch to a bare
     removal drops the removal failure from the error, which the test now catches.
     Committed by me as `8c8af208` (operator: "Commit, no tagline"). Its Codex review found two
     valid P2s, fixed as a follow-up: (i) the managed directory was not synced between the
     rename and the catalogue commit — `syncDir` (a seam) now fsyncs the managed directory and
     its parent before the entry is written, proven by a hook that records the syncs while the
     catalogue is still empty; a durable retry whose file is gone is an error naming the file,
     never a "reused" success; (ii) the start-time diagnosis excluded only MANAGED entries, so a
     legacy or external file inside the managed directory would have been called removable —
     every catalogued file is excluded after symlink resolution, proven with a legacy entry
     pointing into the managed directory.
     Committed as `16611884`; its review found two more P2s, fixed: (i) the durability chain
     stopped at `db/` — when `db/` and `qso-archives/` are both new, `data_dir`'s entry for `db/`
     was never synced; `dirsUpTo` now syncs every directory from the managed one up through the
     working directory (the ordering test requires all three); (ii) the durable retry checked
     existence only — it now reads the file's embedded identity (`sqlite.PeekArchiveIdentity`)
     and requires a match with the catalogue id; a missing file, arbitrary bytes, or another
     archive's file copied over the path is refused naming both ids.
     Committed as `83ffc913`; its review's one P2 fixed: `dirsUpTo` compared parents against an
     uncleaned `data_dir`, so a configured trailing slash would have walked the sync past the
     working directory to `/` (and a non-readable ancestor would then fail creation) — both
     paths are cleaned and the walk is bounded (a directory not under the root syncs only
     itself); unit-proven with a trailing-slash root, an outside directory and the root itself.
   - **Interim archive forwarding gate** (review finding 1; lands here, before activation exists):
     until the ADR 0056 per-logbook bindings ship, forwarding is admitted only in the adopted
     archive. In any other archive `shouldEnqueue` yields no rows for any destination, the boot
     SM Cloud reconciler and the auth re-arm are not constructed, `smd import --forward` is refused
     with a named reason, and `GET /v1/forwarder-queues` plus the Forwarding card state that
     forwarding is off in this archive until per-logbook bindings exist. Proof: a submit in a second
     archive with every forwarder enabled writes zero `qso_upload` rows and no reconciler starts.
     The gate is retired by the bindings, which replace it with explicit routing.
   - **2D design (2026-09-23), pending → active at start:** `archive.ResolveEffective` selects
     the PENDING entry as the candidate (marked `Paths.Candidate`) when one is set, else the
     active one. `run()` builds the graph as a GENERATION: attempt 1 on the effective selection;
     the graph itself is the validation (`startLogDB` verifies the file's identity and migrates,
     `startQso` adopts/projects, workers and the rest come up) and a new lifecycle node
     `archive-promote` (after `qsoservice` and `workers`, before `http`) persists
     `active = candidate`, clears `pending` and the entry's `last_activation_error` with
     `config.Service.Update` (file first). If ANY node of attempt 1 fails while the candidate is
     effective — the file missing, wrong identity, corrupt, or the promotion write failing — the
     orchestrator's rollback tears attempt 1 down, the failure is recorded on the candidate's
     entry (`last_activation_error`, memory-first so the daemon serves this session even when
     config.json is unwritable) and `pending` cleared, and attempt 2 is built once against the
     last-known-good active archive; a failure there is the ordinary fatal start. `adoptArchive`
     compares the file's identity with the EFFECTIVE entry and projects that file's default.
     Observable states: `inactive → pending → active`, or `pending → failed` with the old active
     still active (ADR 0071). No in-process handle swap.
   - **2D built (2026-09-23), uncommitted:** as designed, with one mechanism change found while
     building — a rolled-back graph cannot be re-driven (the logging service is once-initialised by
     design, so a second `Start` on the same instances would run silent), so a GENERATION is a fresh
     container + services + daemon + graph: `buildDaemon` (factored out of `run()`) and
     `startGenerations(cfgSvc, paths, build)` own the two attempts; `recordActivationFailure`
     writes the entry's `last_activation_error` and clears `pending` memory-first and the
     last-known-good generation logs the failure once its logger is open (`daemon.activationFailure`).
     `archive-promote` node: `StartAfter` qsoservice, log/ref DB, workers, events, evidence, ft8,
     qso-log; `http` `StartAfter` it (plan-order test). `adoptArchive` compares the file with the
     EFFECTIVE entry and no longer sets `active` when an entry is selected. Tests
     (`lifecycle_archive_promote_test.go`, real provisioner + adopting generation 0): valid candidate
     promoted in memory and on disk in one generation; wrong-identity candidate → two generations,
     Home serves, error recorded on both; unwritable `config.json` at promotion → Home serves,
     memory records, disk keeps `pending` for the next start; no candidate → no second generation
     and the catalogue untouched. Six compiling reversion proofs (effective selection, promote no-op,
     no second generation, failure unrecorded, adoption against `active`, http not waiting) each
     failed at the intended assertion. Harness: `newOrchestratedDaemon` split into
     `seedOrchestratedConfig` (config.json in its own `etc/` dir, unix listener so self-heals
     validate) + `buildOrchestratedDaemon`. Observatory: `run()` improved; baseline ratcheted.
     Review P1 (fixed): a start-failure rollback calls a RUNNING node's `Stop` without
     `PrepareStop`, and the workers node's `Stop` only waited — a pending LEGACY candidate
     (switching back to Home) with forwarding admitted whose promotion could not be written hung
     in `workerWG.Wait` and `startGenerations` never reached the fallback. `Stop` is now
     `drainWorkers` (cancel, then wait; the clean-shutdown second cancel is a no-op). Test
     `TestLifecycle_LegacyCandidateWithWorkersRollsBackWithoutDeadlock` (stub forwarder, unwritable
     config, 20 s guard); with the old `Stop` it times out.
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
   - **3 build plan (2026-09-23):** three sub-commits, RED-first. **3A** FT8 seal in
     `internal/ft8`: `SealTxAdmission()` is one check-and-set under `seqGate → txMu`
     (`seq.Active()`, `txInFlight`, `txArmed` refuse with the busy reason; else `switchSealed`);
     `ReleaseTxAdmissionSeal()`; refusals carry `ErrArchiveSwitchPending` from `sessionTxGate`
     (every session start), `TransmitNext`, `armTx` and `ClaimProfile`. **3B** bridge seal in
     `internal/bridge`: `SealTx()` under `keyMu → mu` (`tuneActive || ft8TxActive` refuse; else
     `txSealed`), `ReleaseTxSeal()`; `StartTune` and `KeyFt8Tx` refuse with `ErrTxSealed`.
     **3C** the activation on `archive.Manager` behind two seal PORTS (`archive.Seal{Seal() error;
     Release()}`) and a restart port (`func() error`), so `internal/archive` imports neither
     subsystem and `internal/api` reaches all of it through ONE injected port
     (`api.ArchiveManager`: `List`, `Create`, `Activate`; wired by cmd/smd like `SetRestart`) —
     `internal/archive` stays out of the ADR 0043 frozen import set. Wire shapes live in
     `internal/types` (`QsoArchiveCreateRequest`, `QsoArchiveView` with `state`
     active|pending|inactive, `QsoArchiveActivation` with `durability`). `Activate` order: single
     flight → entry exists and is not active → restart port wired → the file's identity peeked
     and matching → FT8 seal → bridge seal (FT8 seal dropped on refusal) → persist `pending` →
     restart; the abort path per finding 3c. Error codes map in the handler by `RequestError`
     code (400 field errors, 404 `archive_not_found`, 409 `archive_active` /
     `activation_in_progress` / `tx_busy` / `archive_unavailable`, 503 `restart_unavailable`,
     500 `activation_persist_failed` / `pending_unclear`); the FT8 and tune handlers map the seal
     refusals to 409 `archive_switch_pending`.
   - **3 built (2026-09-23), uncommitted:** 3A `ft8.SealTxAdmission` / `ReleaseTxAdmissionSeal`
     (seqGate → txMu check-and-set; precedence session, in-flight, armed → `ErrQsoInProgress`,
     `ErrTxInFlight`, new `ErrTxArmed`; `switchSealed` refuses in `sessionTxGate`, `TransmitNext`,
     `armTx`, `ClaimProfile` with `ErrArchiveSwitchPending`); 3B `bridge.SealTx` / `ReleaseTxSeal`
     (keyMu → mu; `tuneActive || ft8TxActive` → `ErrTxActive`; `txSealed` refuses `StartTune` and
     `KeyFt8Tx` with `ErrTxSealed`); both mapped to 409 `archive_switch_pending` in the FT8 and
     tune handlers. 3C `archive.Seal` port + `Manager.SetActivation(tx, rig, restart)`, `List`,
     `CreateArchive`, `Activate` (order and abort path as designed; `RequestError.RequestCode()`
     classifies across the port); `api.ArchiveManager` port + `SetArchiveManager`, routes
     `GET/POST /v1/qso-archives`, `POST /v1/qso-archives/{uuid}/activate`, `archiveErrorStatus`
     map; cmd/smd `archive_port.go` seal adapters, `initHTTP` builds the manager with
     `qsoservice.IsValidCallsign` and the same restart trigger as `POST /v1/restart` (nil without
     `SM_SELF_RESTART=1`). Wire types in `internal/types` (`QsoArchiveCreateRequest`,
     `QsoArchiveView`, `QsoArchiveCreated`, `QsoArchiveActivation`); `archive.CreateRequest` is
     now an alias. Tests: ft8 `archive_seal_test.go` (4), bridge `tx_seal_test.go` (3), api
     `handler_ft8_seal_test.go` + tune mapping + `handler_qso_archives_test.go` (fake port, every
     code→status), archive `activate_test.go` (success + single flight, 5 refusals before sealing,
     FT8/rig busy, persist failure, restart failure, unclearable pending), cmd/smd
     `lifecycle_archive_activate_test.go` (real ports: seals held, pending persisted, restart
     channel closed; refused without the contract, admission untouched). Reversion proofs: 7 (3A)
     + 1 (3A api) + 4 (3B) + 5 (3C), each failing at its assertion. `api-endpoints.md` gained the
     "QSO archives" section. Not in this slice: the SPA (slice 4) and the SM Cloud identity (5).
4. **SPA**: the archive selector above the logbook selector in the shell header, the Archives view
   (label, state, ownership, size, last open; create form; Activate with the restart confirmation),
   and the end-to-end fault/restore drills (AC 1 on the station, with a real second archive).
   - **4 design (2026-09-23):** the listing gains `size_bytes` and `modified_at` (the file's
     stat; "last open" is not tracked — the file's last write is the honest proxy and is labelled
     so). SPA: `lib/api/qso-archives.ts` (list / create / activate outcomes, `refused` carries the
     daemon's code + message), `lib/config/archives.svelte.ts` (the shared store: `list`,
     `active`, `pending`, `load()`, `create()`, `activate(id)` — confirm, capture the daemon
     instance, POST activate, on 202 the same `waitForDaemonBack` reconciliation the Settings
     restart uses, then reload; a refusal is a toast with the daemon's message; a timed-out POST is
     the ambiguous write per ADR 0078, never "failed"), `lib/config/ArchivesSection.svelte` (a
     Settings tab "Archives": table label / state badge / ownership / size / last written / last
     activation error; create form with a client-minted `request_key` kept for the retry; an
     Activate button per non-active archive, disabled while an activation is pending or a request
     is in flight), and the header: an "Archive" line ABOVE the Logbook line showing the active
     archive as a `<select>` of the catalogue whose change runs the same `activate` (reset to the
     active on refusal or cancel) — one flow, two entry points. Honest state (AC 4): the header
     and the list show the daemon's `state`; nothing is called active on a 202. `main.ts` loads
     the catalogue at boot beside the station context and after the daemon is back. The FT8
     claim banner and the tune toast already show the daemon's `archive_switch_pending` message.
     Manual: a "QSO Archives" chapter (weight 45). The station drills (AC 1) are operator-run
     after deploy.
   - **4 built (2026-09-23), uncommitted:** as designed. Go: `QsoArchiveView.SizeBytes` /
     `ModifiedAt` from the file's stat (test + proof). SPA: `lib/api/qso-archives.ts` (+7 tests),
     `lib/config/archives.svelte.ts` (`loadArchives`, `createArchive`, `activateArchive(id,
     confirm)` with the ADR 0078 reconciliation, `mintRequestKey`; 12 tests: declined confirm,
     active never re-activated, accepted → new instance → reload, accepted-not-back = unknown,
     refused shows the reason and reloads, timed-out reconciles, single flight, create
     created/refused/timed-out), `lib/config/ArchivesSection.svelte` (Settings tab "Archives"
     after Station; 6 rendered tests: states + error + Activate only off the active archive,
     pending disables every Activate, declined/confirmed Activate, create form with one request
     key kept on a refusal, cleared on success), the header `<select aria-label="Active archive">`
     above the Logbook line (3 tests), `main.ts` `loadArchives()` at boot beside the build
     identity (baseline rekeyed `line@500 → line@504` in the same change). Manual chapter
     `manual/content/chapters/qso-archives.md`. Gates: lint / format / svelte-check / vitest
     (1785) / go / observatory green. Left for the operator: deploy, then the AC 1 station drills
     with a real second archive.
     Review (2026-09-23), two P1s fixed: (1) after A → B every archive-scoped store — station
     context (default logbook id / name / count), the Phone/CW submission target, dupe checks, a
     mounted Logbook view, FT8 state — was still bound to A; the switch now ends in a GUARDED page
     reload (only once a DIFFERENT instance answered AND the fresh catalogue read succeeded; never
     on an unknown outcome), so everything rebinds through the boot path; the confirmation says the
     page reloads. (2) a failed catalogue re-read retained the old list as if current and
     `settleRestart` named its active entry; `loadArchives` now returns the read's success and
     marks a retained list `stale` (error + Retry shown in the tab, Activate and the header
     selector disabled), and nothing names an active archive from it. Three reversion proofs.
     Second review, one P1 (fixed): the branches that could not prove the new generation (no
     baseline instance, the wait expired, or a new daemon whose catalogue read failed) cleared
     `activating` and only warned, leaving Phone/CW submit, FT8 and the Logbook usable on A's
     bindings against B. Now: a DIFFERENT instance answering reloads at once, whatever the
     catalogue read says; otherwise `archivesState.switchUnresolved` gates the app — a shell-level
     `ArchiveSwitchGate` overlay (`role="alertdialog"`, one control: Reload now) plus injected
     seams `setSubmitGate` (qso.svelte.ts, refuses before the submit seam) and
     `setFt8AdmissionGate` (ft8.svelte.ts `txStartRefusal`: arm and every session start refuse;
     disarm passes), both wired to `archiveSwitchGate()` in main.ts, which also names an in-flight
     activation; another activation is refused while gated; with a baseline the store keeps
     watching (`keepWatching`, 10 × 30 s) and reloads on sight. Tests: no-baseline gate, wait
     expired → gate + watch → reload on sight, watch gives up, new instance + failed read →
     reload, timed-out request + new instance → reload, submit gate, FT8 gate, overlay. Four
     reversion proofs (unique anchors, asserted on patch and restore).
     Third review, two P1s (fixed): (1) other open tabs never rebound — only the requesting tab
     reloaded; every tab now records the daemon's identity at boot (`recordBootIdentity`,
     `/v1/version` `instance` + `archive.id`) and on every reconnect of the always-on stream
     (`onReconnect` in main.ts) runs `verifyArchiveGeneration`: the same archive → nothing, another
     archive → reload, an identity unreadable after 3 tries → the gate (fail closed); a tab whose
     boot read failed adopts the first identity it can read. (2) a non-timeout transport failure
     on the activation POST was shown as a definite error and left the app open; the client now
     keeps the classification (`network` = unconfirmed, timed out or not; `error` = the daemon
     answered without a code) and the store reconciles EVERY `network` outcome through the same
     new-instance wait and gate, leaving only an explicit daemon answer ungated (create: a
     `network` outcome refreshes the list and keeps the request key for the reuse). Tests: client
     classification + `fetchDaemonIdentity`; store: non-timeout network → reconcile + gate, uncoded
     answer → error only, reconnect same / changed / unreadable / transient / no-boot-identity.
     Three reversion proofs (restore only after an applied patch).
     Fourth review, P1 + P2 (fixed): (1) the boot identity was read independently of the
     archive-scoped reads, so a boot straddling a switch could record B while the stores held A,
     and a missing baseline adopted the first identity it saw; now `bootArchiveScoped(reads)`
     brackets the station context + catalogue reads with two identity reads — agreeing reads are
     recorded, differing ones (a restart or switch mid-boot) reload at once, an unreadable side
     leaves the baseline unknown, and a missing baseline REBINDS (reloads) on the first readable
     reconnect, never adopts. (2) the gate overlay was mounted inside the shell branch only, so a
     full-window Map tab with an unreadable identity kept its map; `ArchiveSwitchGate` now sits
     outside the route conditional (App test renders it over the map route). Three reversion
     proofs.
     Fifth review, P1 (fixed): an incomplete boot bracket only cleared the baseline and waited
     for a reconnect that a first-time stream connection never delivers — fail-open. Now either
     identity read failing (after 3 tries each) LATCHES the gate at once and starts
     `watchForDaemon` (`waitForDaemonBack('')`, 10 rounds) which reloads as soon as any daemon
     answers, so an outage at boot heals itself and a flaky identity endpoint cannot storm
     reloads. The overlay title is now "Archive binding unproven" (it covers boot, reconnect and
     activation). RED test: the latch is asserted right after `bootArchiveScoped`, without
     calling `verifyArchiveGeneration`. Two reversion proofs.
   - **4 committed `bb4a29a7`; Codex review (2 P1 + P2), fixed in a follow-up (uncommitted):**
     (1) operations stayed admitted while an identity verification was pending (a reconnect
     check up to 3 × 15 s; the boot bracket's trailing read after the shell opened) —
     `archivesState.verifying` now spans `verifyArchiveGeneration` and the whole
     `bootArchiveScoped` bracket and `archiveSwitchGate()` refuses meanwhile (no overlay, a
     refusal message). (2) a reload request could be cancelled by the Settings leave-guard's
     beforeunload prompt, leaving a page with stale bindings and an open gate — every reload the
     store requests now goes through `requestReload(detail)`, which LATCHES the gate first; a
     surviving page stays gated with the overlay. (3) the overlay left the covered app keyboard-
     operable — App now wraps every route branch in `<div inert={switchUnresolved}>`, the
     overlay focuses its Reload control, and the tune seam (`rig.svelte.ts` `setTuneGate`,
     window shortcut included) refuses a START while gated (a stop never). Tests: verifying
     blocks (reconnect + boot bracket), latch-before-reload (activation + reconnect), inert cover,
     tune gate. Four reversion proofs.
   - **Follow-up committed `230d08bd` (operator); its Codex review (P1 + P2), fixed, uncommitted:**
     (1) the inert cover made the STOP controls unreachable while a run or a tune carrier could
     still be keyed on the daemon — the overlay now offers "Disable FT8 TX" (while `ft8State.tx.armed`)
     and "Stop tune" (while `rig.tuneActive`), each reaching its seam (a disarm and a tune stop
     bypass the admission gates by design). (2) overlapping reconnect checks each cleared the
     shared `verifying` flag when their own read finished, so an older check finding the original
     archive reopened admission while a newer one still read the replacement daemon — checks are
     now COUNTED (`beginCheck`/`endCheck`; `verifying` holds until the last settles). Tests:
     overlapping checks; stop controls absent when nothing is keyed, present and reaching the
     seams otherwise. Two reversion proofs. Committed `4353c93f` (operator).
   - **Review of `4353c93f` (P2, fixed, uncommitted):** the gate's stop handlers discarded an
     UNKNOWN outcome (a stop that timed out with no confirming push within the grace) — the
     transmission may still be up; the outcome is now said on the gate surface (`stop-note`,
     `role="status"`) and as a warn toast, the control staying usable. Fake-timer tests for the FT8
     disable and the tune stop; one reversion proof. Committed `8b0ee330`, Codex clean.
   - **Deployed 2026-09-24 13:31 local: `2.0.0-alpha.3-39-g8b0ee330-dirty`, schema 12, Home
     active, catalogue = Home only.** The restart wrote no catalogue line — adoption is idempotent
     on the station (the slice 1 "second restart" check, closed). **AC 1 station drills** (no RF):
     - Drill 1 (create "Drill"): BUG — the Logbook callsign field accepted lowercase as typed
       (the daemon and the submit uppercase it, so the archive was created correctly; the field
       lied meanwhile). Fixed: uppercase on input like the logging card, `font-mono uppercase`;
       rendered test asserts the field's value; reversion proof. Uncommitted. BUG 2 — no
       client-side callsign validation (a one-character callsign reached the daemon, which
       refused it 400). Fixed: the shared `isValidCallsign` rule marks the field (`input-error`,
       `aria-invalid`, inline hint) and holds the submit; rendered test + reversion proof.
       Both fixed in `f731286b` (operator), deployed `2.0.0-alpha.3-40-gf731286b` 13:44 local.
     - **Drill 1 PASSED (2026-09-24 13:47 local):** "Drill" created from the SPA (201 in 33 ms).
       File `db/qso-archives/01a0d33e-4809-7002-85a1-278299e88967.db`, 135,168 B, mode 0600 in a
       0700 directory, no `-wal`/`-shm` beside it; `archive_metadata` = (1, that id, default
       logbook 1); logbook 1 "Drill" 7Q5MLV with its own UUIDv7; 0 QSOs; schema_migrations_log [(12, 0)]. Catalogue:
       Home active legacy at its recorded path, Drill inactive managed with `request_key`
       `a87a023d-…`; `pending` empty. `GET /v1/qso-archives` lists both with size and last write.
       Log: "managed archive created (inactive until activated)"; the `.creating` file was renamed
       into place. AC 7 (path confinement) observed on the station: the SPA named no path.
     - **Drill 2 PASSED (13:50 local), Home → Drill:** SPA Activate → 202 →
       "activation requested; transmit admission sealed, restarting" → the same restart path as
       `POST /v1/restart` → "smd stopped" 13:50:16.76 → "smd starting" 13:50:21.83 (systemd
       respawn) → "candidate activated (pending → active)" 13:50:23.22 after the graph came up.
       `/v1/version` archive = Drill (managed), new instance id; catalogue Drill active / Home
       inactive, `pending` cleared, no `last_activation_error`; `default_logbook_id` 1 (Drill's
       projection); `/v1/forwarder-queues` `forwarding_gated` true with the interim reason
       (AC 2 half: nothing queued in Drill). The page reloaded itself and showed Drill active
       (AC 4). Operator findings → inbox: the header's "Restart daemon" button placement, and
       the station identity block now cramped (Archive / Logbook / Rig stacked).
       Operator question answered with evidence: Drill forwards nothing — the start on Drill
       logged "forwarding is off in this archive…; no worker started", `/v1/forwarder-queues`
       reports the gate, and the 2B test proves zero upload rows on a submit; the forwarders shown
       are the station-global config (one config.json), never copied into the archive. Fixed from
       the screenshot (uncommitted): the banner led with a fixed prefix in front of the daemon's
       reason (doubled phrase) — the reason now leads once; while gated an enabled destination's pill is
       never green and reads "not forwarding here" (operator: the eye reads the green pill
       before the grey banner), and the expanded card leads with "Station-wide settings — not in
       effect in this archive" above its Enabled checkbox; rendered test + proof.
     - **Drill 3 PASSED (14:40 local, build `-41-gd8fbd269`), physical isolation (AC 1, AC 2's
       "nothing to B's destinations"):** one Phone QSO logged in Drill (7Q7EB, 17 m SSB, 12:40:35Z,
       logbook 1; the operator used a real call rather than a TEST one — it is a dummy contact in
       the Drill archive only). Drill file: 1 `qso` row, 0 `qso_upload` rows, the log's submit line
       carries `forwarded_to: []`. Home file: 8,129 QSOs, no row with that call, last QSO still
       2026-09-15 (untouched). `/v1/logbook/1/count` = 1 (the header count); the Logbook view
       showed the one contact. Note for later: the Drill archive holds this dummy QSO; no
       delete/detach exists yet (out of this programme's scope).
     - **Drill 4 PASSED (14:43 local), Drill → Home:** activation requested 14:43:24.25 → "smd
       stopped" .27 → "smd starting" 14:43:29.33 → the three forwarder workers started (Home is
       admitted) → "candidate activated (pending → active)" 14:43:30.78. Home active with
       `/v1/logbook/1/count` = 8,129, Drill inactive, no activation error, `pending` cleared,
       `forwarding_gated` false. AC 5 observed: `reference.db` and `evidence.db` stayed at their
       canonical paths, no copy beside either QSO file; the Drill file was closed to a single
       file (no `-wal`/`-shm`) and still holds its one dummy QSO (AC 1 second half: reactivating
       Home reveals Home's unchanged rows; Drill's row stayed in Drill). Operator finding → inbox:
       after the reload Settings opened on Station, not Archives (tab state is not in the URL).
     - **Drill 5 PASSED (14:46–14:48 local), failed candidate (AC 3, AC 4).** Pass 1, file
       renamed aside with the daemon running: Activate → 409 `archive_unavailable` in 0 ms naming
       the missing file; no "activation requested" line, no `pending`, no error recorded, an FT8
       claim still admitted (nothing sealed). Pass 2, file restored, Activate → 202 → "activation
       requested; transmit admission sealed" 14:48:17.42 → "smd stopped" .44 → a watcher moved
       the file aside at .446 → "smd starting" 14:48:22.58 → generation 1 failed in `startLogDB`
       (the candidate's identity could not be proven) → "smd starting" .595 (generation 2 on the
       last-known-good archive) → the three forwarder workers started on Home. Result: Home
       active, Drill inactive with `last_activation_error` = the startLogDB chain, `pending`
       cleared, no stray file created in the managed directory; the SPA reloaded to Home and shows
       the failure under Drill with Activate offered (retry after fixing the file). Findings →
       inbox: the start-time wording says "carries no identity" for a MISSING file; the SPA shows
       the whole error chain. Afterwards the file was restored and the empty `-wal`/`-shm` (left by
       read-only peeks) removed; Drill lists with its size again; its failure text stays until the
       next successful activation clears it (by design).
   - **Activation follow-up (directed 2026-09-24; acceptance boundary as ruled):** (a) the
     daemon owns failure classification — `archive.ActivationFailure{Code}` with the stable codes
     `archive_file_missing` (os.Stat first, so a missing file is never "no identity"),
     `archive_file_unreadable`, `archive_no_identity`, `archive_identity_mismatch`,
     `promotion_persist_failed`, `pending_unclear`, `archive_start_failed`; one classifier
     `archive.VerifyIdentity(paths)` serves the activation preflight (409 with the code and the
     plain message; the path only in the log) and the start-time check (`startLogDB`, the
     commands' open); `archive.FailureMessage(code)` is the operator wording. (b) the catalogue
     entry persists ONLY the code in `last_activation_error` (never raw error text or paths; an
     older free-text value maps to `archive_start_failed`); the listing carries
     `last_activation_code` + the plain `last_activation_error`; the SPA renders, never parses.
     (c) Station Events: `archive.activated` (info) and `archive.activation_failed` (error) join
     the `notification` category — migration 0013 rebuilds `operator_event`'s pair CHECK (down
     restores 0009's and discards the two kinds' rows, the 0009 policy); facts carry typed,
     bounded metadata only: archive id (UUID grammar), label (trimmed, printable, ≤ 80), code
     (identifier grammar). `archive.activated` is recorded in `startArchivePromote` only after the
     promotion write succeeds (so in the newly active archive's file); `archive.activation_failed`
     is recorded by the last-known-good generation's events node once it is up (so in the
     recovered archive's file), never for expected refusals (tx_busy, already active, cancel) and
     never for a 202 alone. Recording is the recorder's best-effort enqueue: it cannot affect
     activation, rollback, fallback or logging. Schema head 13.
   - **Built (2026-09-24), uncommitted:** `internal/archive/failure.go` (codes, `ActivationFailure`,
     `FailureCode`, `FailureMessage`, `NormalizeFailureCode`, `VerifyIdentity` with `os.Stat`
     first); `Activate` preflight and the start-time check both call it; the entry stores the code
     (`recordActivationFailure`, `abortAfterPersist`), `startArchivePromote` classifies its write
     failure; `QsoArchiveView.LastActivationCode`; API map gains the four identity codes
     (`archive_unavailable` retired). Station Events: vocabulary + facts + recorder conversion
     (category looked up per kind; `archiveID` UUID grammar, `archiveLabel` ≤ 80 printable runes,
     `code` identifier grammar), migration 0013 up/down (+ test: pairs accepted, down discards only
     the archive rows), every schema-head pin → 13, `startArchivePromote` → `ArchiveActivated`
     after the write, `startEvents` → `ArchiveActivationFailed(f.At)` on the fallback generation.
     Lifecycle tests: activated row in the NEW archive's file only; failed row (code
     `archive_identity_mismatch`) in the RECOVERED archive's file only; a file removed after the
     202 → `archive_file_missing` on the entry, the event and the listing (no path); a refused
     activation records nothing and marks nothing. SPA: events wording for both kinds (stable code
     → words), `lastActivationCode` on the client type. Docs: api-endpoints (codes, listing
     fields, the two event kinds), config.md (the entry holds the code), manual station-events
     chapter, capsule. Five Go reversion proofs. The station's Drill entry still holds the old
     free-text value → reads as `archive_start_failed` / "The daemon could not start on this
     archive." until its next activation clears it.
5. **Per-logbook destination bindings and SM Cloud identity** (AC 6;
   [ADR 0082](../decisions/0082-per-logbook-destination-bindings-in-the-archive.md), accepted
   2026-09-25, which supersedes the SM-Cloud-only binding planned here on 2026-09-22 and retires the
   interim gate). Planned 2026-09-25; RED-first; each sub-slice is its own releasable commit series
   and the station's Home archive forwards identically at every boundary.
   - **5A — pins and descriptor scope (no schema).** (i) Characterization tests in their own
     commit before anything else (ADR part 10): in `internal/qsoservice` a station-shaped config
     (`qrz`, `clublog`, `smcloud` enabled; `qrzcq` present, disabled) pins the exact `qso_upload`
     rows and `forwarded_to` of one live submit, one edit and one delete in the adopted archive; in
     `cmd/smd` the worker names spawned at start (`lifecycle_startup_test` shape) and the
     re-arm/discard calls per name; the existing
     `TestLifecycle_GatedArchiveStartsNoForwardingAtAll` is kept as the "new managed archive: zero
     rows, `[]`, no worker" pin and re-targeted in 5B. (ii) `forwarding.CredentialField.Scope`
     (`station` | `logbook`, registration panics on anything else — allowlist, enumerated first):
     qrz `api_key` logbook; qrzcq `call`,`key` logbook; clublog `email`,`password`,`callsign`
     logbook; smcloud `url`,`token` station, `logbook` logbook. `/v1/forwarder-types` serves it; the
     SPA `CredentialField` type carries it (no rendering change yet). Docs: `api-endpoints.md`
     forwarder-types. Proof: a descriptor without a scope fails registration.
     **Built 2026-09-25, 5A(i) pins:** `internal/qsoservice/fanout_characterization_test.go` —
     the station shape (`qrz` insert/update/delete, `clublog` insert/delete, `smcloud`
     insert/update/delete enabled; `qrzcq` disabled) pins the exact row set
     (name/type/action/origin/status) after a live submit (3 inserts), an edit (+ qrz and smcloud
     update rows; clublog's filter has no update), a delete (+ 3 delete rows) and both
     `forwarded_to` lists in config order; a legacy catalogue entry's submit fans out identically;
     a managed archive queues nowhere for all three with `forwarded_to: []`.
     `cmd/smd/lifecycle_workers_characterization_test.go` — the same names as stub-typed entries
     plus a real loopback `smcloud` entry: workers started = {clublog, qrz, smcloud}, `qrzcq`
     skipped, the reconciler built, the disabled name's pending row discarded, an enabled name's
     pending row kept, an enabled name's auth-failed row re-armed. Compiling reversion proofs:
     enabled gate dropped → qrzcq row appears; archive gate opened → managed archive fans out;
     discard skipped; re-arm skipped; worker spawned for a disabled name; each restored.
     **Built 2026-09-25, 5A(ii) scope:** `forwarding.CredentialField.Scope` (`ScopeStation` |
     `ScopeLogbook`; registration panics on any other value, empty included) — qrz `api_key`
     logbook; qrzcq `call`,`key` logbook; clublog `email`,`password`,`callsign` logbook; smcloud
     `url`,`token` station, `logbook` logbook; stub `mode` station. `/v1/forwarder-types` serves
     `scope`; the SPA `CredentialField` type requires it and the wire decoder drops a field whose
     scope is missing or unknown (same-binary rule: malformed, never an old daemon); no rendering
     change. Proofs: the two panic cases fail with the check removed (restored); a decoder test
     pins pass-through and drop. `api-endpoints.md` documents `scope`.
   - **5B — migration 0014, the seed, routing and workers by binding (the boundary commit).**
     Log migration 0014 exactly as ADR part 1: `logbook_destination` plus
     `archive_metadata.destination_bindings_seeded_at` (down: rename `<type>.<uuid>` queue names
     back to that destination's default-logbook binding name, then drop the table and marker);
     sqlboiler models regenerated as slice 1 did for `archive_metadata` (SQLBoiler 4.19.7, sqlite
     driver; no generation config is checked in — the command is recorded here when run), never by
     hand; schema pins → 14. A read-only duplicate-type preflight runs immediately after config
     load and before `persistResolvedConfig`, `Migrate()` or any other write; the config PUT
     candidate applies the same guard so a running 5B daemon cannot save a conflict for its next
     restart. Startup on the legacy archive, after `Migrate()` and the identity backfill, seeds only
     while the marker is NULL, in ONE transaction — one row per (logbook × legacy entry), enabled
     state and logbook-scoped keys copied (disabled entries seeded disabled with their keys), the
     default logbook keeping the legacy `name`, additional logbooks named `<type>.<logbook uuid>`
     with their existing `qso_upload` rows renamed, then the marker set. NO config strip yet (5C):
     config stays v5 and is read only as the seed source and the station account. A migrated
     non-legacy archive marks the adoption not-applicable without seeding; new managed archives set
     the marker during provisioning. The workers node takes one snapshot of the active archive's
     bindings: per enabled binding a `ForwarderConfig` is synthesised (station entry's
     endpoints/cadence/retry/`allow_insecure_http` + the binding's credentials merged over the
     entry's station-scoped keys, `Name` = `forwarder_name`) and built through the unchanged
     `Build`; discard and re-arm per binding; rows whose name matches no binding discarded loudly.
     Every enqueue site (`submit`, `submit_batch`, `update`, `delete`, `stamp_sync`, `enqueue`
     backfill and delete-backfill, `import --forward`) routes by the QSO's logbook from that snapshot;
     `ForwardingAdmitted`/`archive.ForwardingGateReason` and `forwarding_gated`/`gate_reason` are
     removed. `GET /v1/forwarder-queues` lists binding names, and clear/retry validate against the
     startup binding snapshot (retry still requires its worker); the backfill's
     `forwarder_unavailable` means "no enabled binding of that name". The compatibility SM Cloud
     reconciler is constructed only from the enabled default-logbook SM Cloud binding and its merged
     station account; no binding means no reconciler. It remains one reconciler until 5F.

     The existing config-driven Forwarding controls cannot remain live after ownership moves. This
     boundary therefore also installs a truthful transitional view: no config `enabled` pill or
     logbook-scoped credential editor, station-scoped fields only (from 5A's scopes), binding-keyed
     queue counts, and a read-only note that destination bindings are not editable until 5D. The
     config PUT wire rejects any attempted change to a legacy binding-owned field during this
     transition instead of accepting a save that cannot affect the binding; omitted masked fields
     remain preserved. The final aggregate/per-logbook UI still lands in 5E. Before 5B, tag the
     green 5A HEAD as the known-good pre-migration rollback build. Rollback drill on disposable
     copies BEFORE the 5B commit lands (slice 1 procedure: isolated scratch working directory,
     every resolved path asserted under it, config copy `0600` in `0700` under home): 0014 down on
     a seeded copy, then the binary built from that tag boots it with byte-identical
     QSO/queue/identity rows. Proofs:
     seed idempotent across two starts; a second start after a crash between seed and anything later
     inserts nothing; a logbook created after that committed seed remains unbound across another 5B
     restart; two-logbook fixture partitions names and existing rows with no shared worker name;
     disabled qrzcq has a disabled binding with credentials preserved but no `qso_upload` row and no
     worker; 5A pins pass unchanged; a migrated non-legacy archive and a new managed archive each
     have the marker set, no binding rows and no worker; a config PUT attempting to change a legacy
     binding-owned field is rejected with config and bindings unchanged; each enqueue site has a
     reversion proof that routing by config would produce a different row set.
     **Built 2026-09-25, 5B commit (a) — schema, the seed service, markers, preflight
     (routing and workers still by config; the adopted archive's seed lands in (b)).**
     Migration 0014 (`logbook_destination` as ADR part 1 +
     `archive_metadata.destination_bindings_seeded_at`; down collapses `<type>.<uuid>` queue
     names to the destination's default-logbook binding name, else the lexicographically first,
     via a temp collapse table, then drops table and marker; a collision on
     `(qso_id, forwarder_name, action)` would fail the down step loudly rather than drop a row).
     sqlboiler models regenerated — recipe, reproducing the checked-in files byte for byte
     before the change: apply every `migrations/log` and `migrations/reference` up file in order
     to a scratch SQLite file, then `sqlboiler sqlite3` with `no-tests`, `no-hooks`,
     `add-soft-deletes`, `wipe`, `add-enum-types=false` and the aliases
     `qso.rst_sent → RstSent`, `qso.rst_rcvd → RstRcvd` (SQLBoiler 4.19.7, sqlboiler-sqlite3);
     schema pins → 14. `types.LogbookDestination`; `sqlite.ListLogbookDestinationsWithContext`
     (live logbooks only), `DestinationBindingsSeededAtWithContext`,
     `SeedLogbookDestinationsWithContext(seeds)` (one transaction: marker NULL → insert-missing
     per live logbook × seed, the default logbook — else the lowest id — keeps the legacy name,
     others `<type>.<logbook uuid>` with their legacy-named queue rows renamed; a binding that
     already exists keeps its name and the rows follow THAT name; marker set last; nil seeds =
     mark only; ErrNotFound without an identity row; a uuid-less logbook fails and rolls
     everything back). `forwarding.LogbookScopedKeys(type)`. `cmd/smd/archive_bindings.go`
     `seedDestinationBindings` in `startQso` after adoption: a managed or external archive
     records the empty decision (marker, no rows); the adopted archive is left UNDECIDED at this
     boundary. `archive.Manager.build` records the empty seed at provisioning. Duplicate-type
     preflight in `validateForwarders` (shared by load and `PUT /v1/config`): "one entry per
     destination type (%q and %q are both %q)".
     **Operator review of the first (a) draft (2026-09-25), all fixed:** (1) a standalone (a)
     that seeded the adopted archive would have renamed extra-logbook queue rows to names no
     config-driven worker drains, while new rows kept the legacy name and would become unmatched
     at (b) — the adopted archive's seed and renames now land in (b) with routing and workers;
     (2) a malformed or non-object credential blob must not silently become "no credentials"
     under a committed marker (forwarder construction runs later and disabled entries are never
     constructed, so a corrected config could not reseed) — REQUIREMENT for (b)'s seed helper:
     reject a blob that is not a JSON object (valid non-object JSON such as a string or an array
     can sit inside a valid config file) and a type with no registered descriptor BEFORE the
     seed transaction, failing the start by name; (3) insert-missing on a conflicting durable
     row now renames to the existing row's `forwarder_name`, never a freshly computed one
     (proof: computed-name rename → the logbook's rows carry a name no binding has);
     (4) the down-migration fallback tests now use fixtures where the default logbook's name is
     NOT the lexicographic minimum (default wins: `qrz.z` over `qrz.a`) and where, with no
     default binding, the minimum is NOT the lowest logbook's name (`qrz.a` of logbook 2 over
     `qrz.b` of logbook 1); proof: COALESCE reduced to MIN → the default-wins test fails.
     Note for fresh installs (unchanged by the review): the adopted file's seed covers the
     logbooks present when (b) first starts it; a logbook created later stays unbound until the
     bindings tab (5E). Proofs (compiling, restored): down collapse removed → both collapse tests
     fail; keeper rule removed → renamed count 2; marker check removed → second seed inserts;
     existing-binding rule removed → rows follow a computed name; default-wins branch removed →
     fallback test fails; the marker assertion at provisioning failed before the change.
     Gates: whole-tree vet + tests, gofmt, observatory 0 regressions.
     **Rollback drill (2026-09-25, `scripts/rollback-drill.sh` on online-backup copies of the
     station's Home file and a scrubbed config, 0700 scratch, every path confined, all external
     paths off, shredded after):** source copy schema 12 (station `-41`), 8,129 QSOs, 15,733
     queue rows, 2 history rows, 1 logbook. (1) new build (14) vs the 5A HEAD build
     (`f28a70d6`, 13): migrated 12→14, `db-downgrade --to 13`, old build booted at 13, ROWS
     IDENTICAL, no columns dropped — run twice, once with the first draft (which seeded 4
     bindings / 0 renamed on the copy, the log line read from the kept copy) and again with the
     final (a) build (no seed on the adopted copy, marker NULL). (2) new build vs the deployed
     `/usr/bin/smd` (`2.0.0-alpha.3-41-gd8fbd269`, 12): 14→12, booted at 12, ROWS IDENTICAL.
     (3) `--mutate-one-row` proof run: ROWS DIFFER, exit 1. Config stayed v5 throughout.
     **Codex review of `c3df0e12` (P2, fixed 2026-09-25, follow-up commit):** the 0014 down
     step inferred the collapse target from the current default logbook or sort order; with a
     changed default, or a legacy name sorting after `qrz.<uuid>` (e.g. `station-qrz`), rows
     would be renamed to a name the older config never carried and stranded. Now
     `logbook_destination.legacy_name` (NULL unless seeded) records the config name on every row
     the seed creates, `types.LogbookDestination.LegacyName` carries it, and the down step
     collapses to it FIRST, falling back to the ADR rule only for bindings that never derived
     from config; ADR 0082 carries a dated update. Tests: Codex's two scenarios (no default +
     `station-qrz`; default moved to logbook 2) collapse to the recorded name; the seed test
     asserts `legacy_name` on all four rows. Proof: the legacy branch removed → both scenario
     tests fail (restored). Models regenerated (same recipe); gates green; drill (new vs 5A
     HEAD at 13) rerun on the rebuilt binary: ROWS IDENTICAL.
     **Codex review of `b1231672` (P2, fixed 2026-09-25, follow-up commit):** editing 0014's up
     step in place would leave a file the 0014 build had already migrated without the column
     (`Migrate()` accepts ErrNoChange), and the edited 0014 down referenced a column such a file
     lacks. The durable name is now migration **0015** (`ALTER TABLE logbook_destination ADD
     COLUMN legacy_name`); its DOWN step performs the whole collapse (recorded legacy name →
     default logbook's binding name → lexicographically first) while the column exists, and
     0014's down is a plain drop (the 0014 build wired no seed, so a file that stopped there holds
     no renamed rows). Schema head 15; pins moved. Tests: the five collapse scenarios assert at
     14 (after 0015's down) and the drop at 13; an upgrade-path test takes a file down to the
     0014 shape, inserts bindings without `legacy_name`, runs `Migrate()` to 15, lists them with
     an empty LegacyName and collapses by the fallback. Proofs (non-empty mutations, restored):
     collapse UPDATE neutralised → five down tests fail; legacy branch neutralised → both
     recorded-name tests fail. Models: the regeneration recipe reproduces the checked-in files
     unchanged (same final schema). Gates green; drill (new at 15 vs 5A HEAD at 13) rerun on the
     rebuilt binary: 15 → 13, ROWS IDENTICAL, no columns dropped.
     **Codex review of `ea56170f` (P1, fixed 2026-09-25, follow-up commit):** two shapes of
     schema 14 exist — the `c3df0e12` build's table without `legacy_name` and the `b1231672`
     build's with it — and 0015's unconditional `ADD COLUMN` fails with a duplicate column on the
     second (reproduced RED: "duplicate column name: legacy_name"). 0015 up is now a
     rename-then-rebuild in the 0013 style: one final shape from either variant, every row copied
     with its id, the AUTOINCREMENT high-water mark carried, constraints restated; the second
     shape's column content is not carried (that build wired no seed, so no real file of that
     shape holds a seeded binding). Test: a file taken to 14, given the column by hand and two
     bindings, its AUTOINCREMENT sequence advanced to 9 by an inserted-then-deleted row, migrates
     to 15 with both rows, UNIQUE intact, and the next insert takes id 10 (operator review: the
     first assertion, MAX(id) = 3, passed with the sequence carry removed). Proofs: the naive ADD
     COLUMN form fails with the duplicate-column error; the sequence carry removed → id 3, not
     10 (restores verified before any gate ran). Models unchanged (same final schema); gates green; drill
     (15 vs 5A HEAD at 13) rerun on the rebuilt binary: ROWS IDENTICAL.
     **Built 2026-09-25, 5B commit (b) — routing, workers and the adopted archive's seed by
     binding; the gate retired.** `forwarding.BindingConfig` / `ResolveBindings` /
     `BoundForwarder` / `BindingFault` / `DescriptorFor`: a binding resolved against its station
     account is one synthesized `ForwarderConfig` (account's station-scoped keys + binding's
     logbook-scoped keys, account's transport/cadence/retry/endpoints/label/action_filter,
     binding's name and enabled state; a non-object blob on either side, a type mismatch or a
     type without a descriptor is refused by name; a binding with no account is a named fault,
     logged, no worker, rows untouched). `sqlite.LogbookIDsByQsoIDsWithContext`,
     `QueuedForwarderNamesWithContext`. `qsoservice.SetDestinationRoutes` / `DestinationRoutes`
     / `routesFor(logbookID)` / `routeByName`: `submit`, `submit_batch`, `update`, `delete`,
     `stamp_sync` (per QSO's logbook), backfill and delete-backfill (by binding name; a QSO of
     another logbook lands in `skipped_other_logbook`), `import --forward` all route from the
     snapshot; `SetArchive`, `ForwardingAdmitted`, `ForwardingGateReason`, `forwarding_gated`
     and `internal/archive/gate.go` are gone. `cmd/smd/archive_bindings.go`: `destinationSeeds`
     (refuses non-object blobs — a string or an array inside a valid config — and types without a
     descriptor, by name, BEFORE the seed transaction: operator finding 2 on 5B(a)),
     `seedDestinationBindings` (legacy seeds with `legacy_name`; managed/external mark only),
     `resolveDestinationRoutes`, `routeConfigs`; `startQso` seeds then installs the snapshot;
     `startWorkers` builds from the snapshot (discard for disabled bindings AND for queued names
     no binding carries, re-arm and spawn per enabled binding), the boot-time SM Cloud reconciler
     keys on the DEFAULT logbook's enabled smcloud binding (its synthesized config), `initHTTP`
     hands the running names to the API (`Server.SetRunningForwarders`; the queues readout and
     retry gate no longer state a gate). `smd import`: routes from the archive's bindings,
     `--forward` names must be enabled bindings of the target logbook (`importForwardNames`,
     extracted — observatory ratchet on `runImport`: cognitive 52→40, cyclomatic 39→35, MI
     15→16); a file the daemon never started under this build has no bindings and the message
     names that. SPA: the queues decoder, the tab's banner, gated pill hiding, card note and
     retry gating removed with their tests; `uploads.ts` decodes `skipped_other_logbook`. Docs:
     `api-endpoints.md` (backfill bucket and errors, queues readout, retry gating),
     `install.md` and the manual's importing chapter (`--forward` names bindings). Test
     fixtures mirror the seed: `seedLogbook` (qsoservice), `createTestLogbook` (api) and the
     qrzcq acceptance harness install routes from the config entries; the pt5 import rollback
     test routes by binding; a `stub2`/`stub3` registration keeps the station-shaped pin at one
     entry per type. Tests: `routing_test.go` (no bindings → nothing queued, backfill refused
     by name, disabled binding not a route; two logbooks fan out to their own bindings only,
     backfill and delete-backfill skip the other logbook; stamp sync per logbook; import
     --forward to an unbound name queues nothing), `binding_test.go`, `queue_routing_test.go`,
     daemon seed/refusal/no-binding/managed tests, the API running-set test, the retargeted
     import refusal. The 5A pins pass with their assertions unchanged (fixtures bound from
     config). Proofs (non-empty mutations, restores verified): routing ignores the logbook →
     two-logbook and stamp-sync tests fail; both enabled guards dropped together (routesFor and
     shouldEnqueue) → a disabled binding queues; backfill's logbook check dropped → the other
     logbook's QSO is enqueued; unknown-name discard dropped → rows survive in a bindingless
     archive; non-object blob accepted → the refusal test fails. Gates: whole-tree vet + tests,
     gofmt, observatory 0 regressions (baseline ratcheted), frontend lint/format/svelte-check/
     vitest (1,831). Drill (new at 15 vs 5A HEAD at 13) on the final build: the scrubbed copy
     seeds 4 bindings (all disabled, credentials scrubbed) and starts no worker; 15→13; ROWS
     IDENTICAL. Behaviour note: the SM Cloud reconciler still follows the default logbook until
     5F; a binding whose station account was removed keeps its rows and gets no worker.
     **Operator review of the first (b) draft (2026-09-25), six findings, all fixed:** (1) P1
     scope crossing — `BindingConfig` now takes ONLY the account's station-scoped keys and ONLY
     the binding's logbook-scoped keys, per the descriptor (an undeclared key is dropped from
     both); proof: an SM Cloud binding carrying `url` cannot replace the station's URL. (2) P1 an
     unresolved binding lost its rows — the workers node judges the unknown-name discard against
     the file's full binding-name set (`destinationSnapshot`), not the resolved routes; proof: a
     binding whose account was removed keeps its pending row while an orphan name is discarded.
     (3) P1 the tab was misleading — the transitional view the plan named is in: no on/off pill,
     no Enabled checkbox, only station-scoped credential fields (a destination with none says
     so), a note that bindings are owned by the active archive and not editable yet; the store
     sends `enabled` exactly as stored and only station-scoped keys, and refuses a reset of a
     logbook-scoped field; `PUT /v1/config` refuses a changed `enabled` (or one set on a new
     entry) and any logbook-scoped key, blank or not, with `forwarder_field_binding_owned`
     naming entry and field (`refuseBindingOwnedForwarderEdit`; `completeSetupDryRun` extracted
     for the observatory — `handlePutConfig` ratcheted 64→55 / 40→37 / 16→18); thirteen config
     PUT tests retargeted to station-scoped fields and unchanged `enabled`; new
     `TestHandlePutConfig_RefusesBindingOwnedForwarderEdits`, U20, F10. (4) P2 queue endpoints
     by binding names — `Server.SetForwarderQueueNames` (every binding in listing order,
     resolved or not); the readout lists them and clear/retry accept only them (404 otherwise);
     test `TestForwarderQueues_KeyedByBindingNames`. (5) P2 ordering — the listing orders by
     `logbook_id, id` (insertion = config order), so Home's `forwarded_to` order is what
     config.json had; test `TestListLogbookDestinations_KeepsInsertionOrder`; the drill's seed
     line now reads clublog, qrz, smcloud, qrzcq (the station's order). (6) P2 the outcome logs
     carry `skipped_other_logbook`. Proofs (non-empty mutations, restores verified): scope
     switch admits binding keys → override test fails; discard judged against routes → kept-row
     test fails; PUT guard neutralised → guard test fails; listing by destination → order test
     fails. Gates rerun: whole-tree vet + tests, gofmt, observatory 0 regressions (ratcheted),
     frontend lint/format/svelte-check/vitest (1,833); drill rerun on the final build: ROWS
     IDENTICAL, 4 bindings seeded disabled on the scrubbed copy in config order.
     **Second review round on (b) (2026-09-25), two findings, both fixed:** (1) P1 a DISABLED
     binding whose account cannot resolve kept its rows because the disabled discard iterated
     the resolved routes — the snapshot now carries every disabled binding's name, resolved or
     not, and the workers node discards for all of them; an ENABLED unresolved binding still
     keeps its rows. Test: two account-less bindings, `keep` enabled and `drop` disabled — keep's
     two rows survive, drop's row is discarded. Proof: discard restricted to resolved names →
     the test fails (restored). (2) P2 the tab rendered queue counts only for station-account
     names — queue entries keyed by any other binding name (an additional logbook's
     `<type>.<uuid>`, a binding whose account is gone) now get their own cards under the
     destinations, named by the binding, with counts, Retry failed and Clear queue (U21). Gates
     rerun: whole-tree vet + tests, gofmt, observatory 0 regressions, frontend gates (1,834);
     drill rerun on the final build: ROWS IDENTICAL.
     **Third review round on (b) (2026-09-25), two P2s, both fixed:** (1) the unresolved-binding
     log claimed "rows are kept" for every fault while a disabled one's rows were then discarded —
     the message now follows the binding's own state ("kept" when enabled, "discarded like any
     disabled binding's" when disabled), the snapshot and resolver comments say the same, and the
     disabled-unresolved test asserts the log lines match the fate of each binding. (2)
     `api-endpoints.md` still called `{name}` a config name and the queue list "configured
     forwarders in config order" in the backfill, queues, clear and retry entries — all four now
     state the binding-name contract.
     **Codex review of `7990011b` (b) (2026-09-25), P1 + P2, both fixed in a follow-up
     commit:** (1) P1 the seed committed bindings and the marker before worker construction
     validated the entries — an enabled entry with an empty blob passed the object check, was
     seeded enabled without its key, then failed `qrz.New` at every start with no way back (the
     binding, not config, now supplied the key). `seedDestinationBindings` now constructs every
     ENABLED entry (`forwarding.Build`, exactly as the workers node would) BEFORE the seed and
     fails the start by name with config.json still the thing to fix; a disabled entry is seeded
     as it is. Test: a bogus enabled stub entry refuses the start, leaves the marker NULL and no
     rows, and a corrected config on the same file then seeds (second generation on the same
     working directory). The rollback test's workers-node injection moved to a stub-backed type
     with no default retry (`stub4`), since an unbuildable entry now fails earlier. Proof: the
     Build check neutralised → the refusal test fails (restored). (2) P2 the logbook backfill
     picker took names from `/v1/config`, so a migrated additional logbook offered `qrz` and
     every selected row landed in `skipped_other_logbook` as "Queued 0". New
     `GET /v1/logbook/{id}/destinations` (the logbook's bindings from the snapshot: name, type,
     label, enabled — never a credential); the logbook store loads it per selected logbook
     BEFORE the first page (the handoff from the Forwarding tab included) and reports
     `skipped_other_logbook` in the upload notice; `missing_from` resolves a binding name first
     (station entries as the fallback for a file with no bindings yet) and its refusal wording
     names bindings. Tests: the endpoint lists each logbook's own binding and no credential;
     `missing_from` accepts a `<type>.<uuid>` name; the store test asserts the destinations read
     precedes the first page. Gates rerun: whole-tree vet + tests, gofmt, observatory 0
     regressions, frontend gates (1,834); drill rerun on the rebuilt binary: ROWS IDENTICAL.
     **Operator review of the backfill follow-up (2026-09-25), three findings, all fixed:** (1) P1
     switching logbooks kept the previous logbook's binding name in the picker — after the new
     logbook's bindings load, the picked destination is remapped BY TYPE onto that logbook's own
     enabled binding, else reset to All (the picked type is remembered across the switch);
     (2) P1 the bindings fetch had no stale-response guard — it now carries a request generation
     like the page and count loaders, so a slow answer for logbook A never overwrites B's list;
     (3) P2 `missing_from` accepted any archive binding and fell back to station config — it now
     accepts only the requested logbook's binding routes (list and count), and its refusal names
     "this logbook". Tests: remap by type, reset to All, late answer discarded (store); another
     logbook's binding and a bindingless config name refused on both endpoints (API). Proofs
     (restores verified): remap removed, guard removed, logbook scope removed → each test
     fails. Gates rerun: whole-tree vet + tests, gofmt, observatory 0 regressions, frontend
     gates (1,837); drill rerun on the rebuilt binary: ROWS IDENTICAL.
     **Operator review, one further P1 (2026-09-25), fixed:** the pre-seed construction check ran
     on every legacy-archive start, so once the seed was decided a config entry that had lost its
     logbook-scoped key (a deprecated shadow of the binding's) would have killed every start
     although the binding resolved fine. The check — and the seed helper — now run ONLY while the
     marker is NULL; a decided seed returns before consulting config's copies. Converse test with
     the real `qrz` type (its key is logbook-scoped): a first start seeds the binding with the key,
     config.json then loses it, the entry no longer constructs on its own, and a second generation
     on the same file starts, routes from the seeded binding carrying its own key and spawns the
     qrz worker. Proof: the marker gate neutralised → that start is refused (restored). Gates
     rerun: whole-tree vet + tests, gofmt, observatory 0 regressions; drill rerun: ROWS IDENTICAL.
     **Codex review of `a7abb537` (2026-09-25), P2, fixed in a follow-up commit:** awaiting the
     new bindings fetch in `selectLogbook` deferred the invalidation of the previous logbook's
     in-flight page and count until the new loaders started, so A's late page could land under
     B's selector while B's bindings were still loading. The switch now bumps the page and count
     generations SYNCHRONOUSLY before the await, and carries a selection generation checked after
     it, so a switch superseded while its bindings loaded never loads pages for a logbook whose
     bindings it never saw (found by the new test's first ordering). Two race tests, one per
     ordering (A's page already in flight; A's bindings in flight); proofs: each guard removed
     fails its own case (restored). Frontend gates: 1,839.
     **Order change (operator, 2026-09-25, "let's aim at 5E"):** 5D and 5E are built BEFORE 5C.
     5C only cleans config.json (deprecated keys, one-per-type, the narrowed view, the data-aware
     downgrade); the PUT guard already makes those keys inert, so the tab does not need it first.
     5C follows 5E.
     **Built 2026-09-25, 5D — the bindings API:** `types.ArchiveBindingsView` /
     `ArchiveBindingsRequest` (wire shapes, stdlib only); `sqlite.UpsertLogbookDestinationsWithContext`
     (one transaction, name never changed on an existing row); `internal/archive/bindings.go` —
     `BindingsView` (one entry per registered descriptor; aggregate `on`/`off`/`mixed` over live
     logbooks with absent = off; account presence = an entry of the type with every non-clearable
     station field set; `reason` for no account or SM Cloud outside the adopted archive — the 5F
     remnant; per-row `credentials_set`, queue counts by binding name; `restart_required` =
     fingerprint of the table vs the daemon's start listing), `applyBindings` (whole candidate
     validated first: known type, live logbook, no duplicate row, enable refusals, merge blank-keeps
     over the stored logbook-scoped keys, station keys refused, `credentials_clear` only on a row
     that ends disabled, every non-clearable logbook-scoped field required when the row ends
     enabled; then one upsert transaction; new rows `<type>.<logbook uuid>`), the manager port
     (`SetActiveBindings(db, atStart)`, `Bindings`, `ApplyBindings`: `bindings_unavailable`,
     `archive_not_found`, `archive_not_active`). API: `GET`/`PUT /v1/qso-archives/{uuid}/bindings`
     through the archive port (ADR 0043's frozen import set — the boundary test caught a direct
     `internal/archive` import), codes mapped in `archiveErrorStatus`; `cmd/smd` hands the active
     DB and the start listing to the manager at HTTP init. Tests: view aggregation + masking,
     whole-candidate refusal writes nothing, aggregate write names new rows, merge blank-keeps,
     station key refused, clear rules, enable refusals (no account, half-configured account,
     disable needs none), SM Cloud gate on a managed archive, port refusals, API mapping +
     passthrough. Proofs (restored): required-field check, SM Cloud gate, clear rule and
     restart_required each removed → their test fails. Gates: whole-tree vet + tests, gofmt,
     observatory 0 regressions. Docs: `api-endpoints.md` (both routes).
     **Operator review of 5D (2026-09-25), three findings, all fixed before its commit:** (1) P1
     enabled bindings were only presence-checked — a DISABLED seeded QRZ binding holding
     `{"api_key":123}` (supported: the seed copies a disabled entry's logbook-scoped keys as they
     are and never constructs it) passed and could be enabled, then fail construction at every
     restart. `applyBindings` now synthesizes every row that ends enabled exactly as the next
     start will (`forwarding.BindingConfig` over its station account) and builds it
     (`buildCandidate`); a failure refuses the whole PUT with `binding_unusable` (the wire names
     destination and logbook; the cause goes to the log, as the config save's
     `forwarder_unusable` does). Pinned twice: in `internal/archive` with a seeded disabled
     malformed row and a valid row in the same request (refused whole, every row byte-identical
     afterwards, a retyped valid key then enables), and end to end in `cmd/smd` with the real QRZ
     constructor through the daemon's wired port (the daemon starts and seeds the disabled
     malformed entry as it is; enabling it as it is is refused with no row changed; a valid key
     enables). (2) P1 the merge read happened outside the write and the port released its lock
     first, so two masked edits could both merge onto the same old blob — `Manager.ApplyBindings`
     now holds `bindingsMu` over the whole read/validate/build/write (a process lock suffices:
     the running daemon is the only writer of the open archive's bindings). Proof harness: a
     gated database holds PUT A at its write while PUT B is started; B may reach its own write
     only if it could read concurrently; both disjoint field edits (ClubLog-like email and
     password) must survive. With the lock removed the test fails 10 of 10 runs; with it, passes
     10 of 10 and ten times under `-race`. (3) P2 `credentials_clear` removed any key — it now
     accepts only the type's logbook-scoped keys (station-scoped and undeclared keys refused,
     never reported as cleared) and refuses a key both typed with a value and cleared. Also fixed
     in passing: `activeEntryForBindings` read `m.activeDB` without the lock (now passed the value
     captured under it). Proofs (restores verified): build check off → both pins fail; lock off →
     concurrent edit loses a field; clear allowlist off and typed-and-cleared check off → the
     clear test fails. Gates: whole-tree vet + tests, gofmt, observatory 0 regressions.
     `api-endpoints.md` states serialization, the build rule, the clear rules and
     `binding_unusable`.
     **Built 2026-09-25, 5E — the Forwarding tab on the bindings API:** daemon: ClubLog registers
     an application-key presence probe (`forwarding.RegisterBuildKey` / `BuildKeyPresent`; only
     presence leaves the package) and the station account view reports `build_key`
     `present`/`absent` for such a type; the enable reason now names which gap — no entry in
     `config.json`, or an entry with a station field unset — so the tab can offer the fix only where
     it exists. SPA: `lib/api/archive-bindings.ts` (GET/PUT client; rows without a numeric logbook id
     and destinations without a type are dropped; an unknown aggregate reads `off`; a timed-out PUT
     is ambiguous), `bindings.svelte.ts` (drafts per row; the pill is the daemon's state and the
     switches the draft; client validation marks a required field, restores the switch and sends
     nothing; a daemon refusal restores every switch and keeps typed values; a timeout re-reads and
     keeps typed values; removal marks survive only on a row that is off with the key stored),
     `DestinationsSection.svelte` (one card per destination: state pill, "off for …" naming the
     logbooks of a mixed destination (ADR part 2), the every-logbook switch only when the archive
     has more than one logbook — with one it would duplicate the row's switch — per-logbook rows
     with masked fields, queue counts, Retry failed / Clear queue and the gap link carrying the
     logbook; the reason note, with "Open its station account" only when the station account is the
     gap; the restart banner), `ForwardingSection.svelte` reduced to the host plus "Station accounts"
     (only entries with station fields, a build key, or no descriptor; no switch or pill; ClubLog
     says whether this build carries its key), loading apart from the destinations so a station
     reload never remounts them over the operator's drafts. The router hands the logbook id with a
     "not on X" link (`?missing_from=…&logbook=N`, the logbook only beside a destination) and the
     logbook view opens that logbook. The Settings leave guard counts, discards and waits on the
     destination drafts too. The archive creation form states that a new archive uploads nowhere.
     The queue tests of the old tab were ported to the binding rows (D10–D19); the unused
     queue-list client (`fetchForwarderQueues`) and its tests were removed. **Judgement to confirm:**
     ADR part 9 asks for "the link to where it is fixed" also for SM Cloud outside the adopted
     archive; that gap is fixed by the identity-aware server (5F), not on this tab, so the card
     states the reason without a link rather than send the operator to a card that cannot fix it.
     **Not built (ADR part 3):** ClubLog `callsign` defaulting to the logbook's callsign — today
     the field is required when a row is turned on; where the default lives (daemon merge, or a
     descriptor hint the SPA pre-fills) wants a ruling. Self-review found and fixed before
     presenting: "Open its station account" opened a card only on the first click (a forced-open
     state that never changed again) — the card's own `open` is set now; a stale count after a
     failed refresh still showed in the card's summary total; removal marks survived a refusal or
     timeout that left the row on. Proofs (compiling, restores verified by hash; each fails its own
     test): ClubLog presence probe forced true; the no-entry reason collapsed into the incomplete
     one; the account link offered on a complete account; "off for" naming the on logbooks; every
     station entry listed; the account link never opening, and opening only the first time; the
     build-key texts swapped; the leave guard ignoring destination edits, skipping their discard,
     and ignoring a destinations save in flight; the saving flag not raised; the new-archive
     sentence dropped; validation ignoring a missing required key; the router dropping the logbook,
     and taking a bare `?logbook=`; the logbook view ignoring the handed-off logbook; the decoder
     keeping id-less rows (its fixture first lacked such a row — fixed); a stale count kept, and its
     summary total shown; a marked field never marked invalid; removal marks kept on an on row, and
     for a key no longer stored; the destinations coupled to the station-account load. Gates:
     frontend lint, format, svelte-check, vitest; Go whole-tree vet and tests, gofmt, observatory 0
     regressions. Docs: `api-endpoints.md` (`build_key`, reason wording); manual `forwarding.md`
     rewritten for the tab, `qso-archives.md` (a new archive uploads nowhere; destinations are per
     archive, SM Cloud Home-only until 5F).
     **Found while gating 5E:** CI had been red since the 5B routing commit (`7990011b`, through
     `4f42ce2a` and `c8ca2a7a`): the SM Cloud reconcile end-to-end tests build a reconciler over a
     local stack with no routing snapshot, so every heal row was refused `forwarder_unavailable`.
     They skip without Postgres, so local gates never ran them. Fixed test-side in its own commit
     before this one (`bindLogbook` installs the seeded binding wherever a reconciler runs); the
     Postgres suites and both CI Go steps pass locally with the dev database.
     **Operator rulings (2026-09-26) on the two 5E questions:** (1) the non-Home SM Cloud identity
     refusal stays unlinked until 5F — no current screen can fix it; closed with no code change.
     (2) ClubLog's `callsign` defaults IN THE DAEMON when a binding is saved that ends enabled and
     has neither a stored nor a typed callsign: the logbook's callsign is persisted with the
     binding in the same transaction, so a later logbook callsign change never silently retargets
     uploads; an explicit stored or typed value always wins; disabled rows are untouched. The
     descriptor carries a generic marker (`defaults_to: "logbook_callsign"`), and the SPA uses it
     — never ClubLog-specific logic — to treat the field as satisfied in validation, show the
     logbook callsign as the placeholder, and leave the value out of the request so the daemon
     stays authoritative.
     **Built 2026-09-26 per ruling (2):** `CredentialField.DefaultsTo` / `defaults_to` with the one
     source `logbook_callsign` (registration refuses another source, a station-scoped or a
     clearable field); ClubLog's `callsign` carries it; `mergeBindingCredentials` fills such a field
     from the logbook's callsign for a row ending enabled with neither stored nor typed value
     (`fillDefaults`), inside the same upsert, before the required check and the build. SPA: the
     decoder passes only the known source; validation treats the field as satisfied when the
     logbook has a callsign; the request carries nothing for it; the placeholder names the
     callsign. Proofs (restores verified): registry check off; ClubLog marker removed; no default
     filled; default overwriting a stored/typed value; disabled rows defaulted; decoder accepting
     any source; SPA still requiring the field; SPA defaulting without a logbook callsign; no
     placeholder — each fails its own test. Docs: `api-endpoints.md` (`defaults_to`, the PUT rule);
     manual `forwarding.md` (Club Log callsign).
     **Station review after the deploy (2026-09-26, build `-62`, schema 15):** read-only checks
     passed (schema 15 clean; the seed ran once on this start — 4 bindings, 0 queue rows renamed;
     every queue empty; QRZ/ClubLog/SM Cloud on, QRZCQ off; no restart pending; a `stub` card shows
     because the station build carries the `dev` tag). The operator found the destinations intro
     rendering "archiveHome" and ruled the explanation out of the tab: the heading names the archive
     from the daemon's label ("Destinations for Home", "this archive" until known), its paragraph
     goes, Station accounts reads "Shared by every archive.", and the tab intro becomes a "How
     forwarding works" link to the manual's Forwarding chapter (`/manual/#forwarding`, verified in a
     built manual) in a new tab like the sidebar's Manual link. Refusals, ClubLog key status and the
     restart banner stay. Proofs: label ignored, link to the manual root, same-tab link, grown
     station text — each fails its own test.
     **Operator rulings (2026-09-26, second pass):** (1) the "How forwarding works" line goes; the
     link becomes an ⓘ icon beside the "Destinations for <archive>" heading (tooltip and accessible
     name "How forwarding works", manual Forwarding chapter in a new tab) — a native `title`
     tooltip cannot hold a clickable link, so the icon is itself the link. (2) **ClubLog is not a
     station account** — a deliberate departure from ADR 0082 part 9, which listed "the ClubLog
     application key as present/absent" under Station accounts. The API key identifies the
     software and is built into the daemon; the application password is the user's, tied to the
     callsigns of their account (operator's citations: Club Log help, "API keys",
     https://clublog.freshdesk.com/support/solutions/articles/54910-api-keys, and "What are
     application passwords?",
     https://clublog.freshdesk.com/support/solutions/articles/3000057171-what-are-application-passwords-).
     Neither is a station-wide account setting, so ClubLog appears only under the destinations,
     where a build without the key is already stated on its card.
     (3) Station accounts gets the same ⓘ ("How station accounts work", `/manual/#station-accounts`)
     with a new short manual section of that name; the icon is one shared `ManualLink` component
     used by both headings. Both anchors verified unique in a built manual.
     **Settings explanations to the manual (inbox 2026-09-26, operator-agreed order):** the
     Archives list and the New archive form lose their paragraphs for ⓘ links (`#qso-archives`,
     `#creating-an-archive`); the form's "a new archive uploads nowhere" sentence (ADR 0082
     part 9) now lives only in the manual's "Creating an archive", which already carried it; a
     failed activation is a ⚠ after the label whose tooltip and accessible name carry the reason
     (also fixes the 2026-09-24 note that the text pushed the columns); Station accounts loses
     "Shared by every archive.".
     **Station drills 2026-09-26 (in progress, build `-71`):** B deferred (off air). C.4 passed on
     its criteria (Drill: SM Cloud destination "disabled", the adopted-Home reason, switch
     disabled, no account link) but surfaced a naming defect: two cards titled "SM Cloud" — the
     destination (per-logbook switch, cloud logbook name, state pill) and the station account
     (URL and token, no pill by design) — read as a duplicate with a missing pill. **Ruled:** do
     both, after the drills finish — rename the station card "SM Cloud service and token", and add
     a pointer line on the SM Cloud destination card to Station accounts. Also seen, not ruled: the
     refused destination's "Cloud logbook name" stays editable; the Service URL is masked like the
     token.
     C.5 behaved as designed (nothing saved, switch restored, no banner) but surfaced two defects:
     the refusal is an inline banner where every other Settings section reports a refused save as
     a "Save failed: …" error toast; and the refused card collapses when its switch is restored,
     hiding the field it marks. **Ruled (build after the drills, with the rename):** outcome
     messages (refused, failed, saved) are toasts in the existing wording — a SPA or daemon refusal
     becomes "Save failed: <destination> for <logbook>: <field> is required to turn it on." and the
     inline refusal banner goes; standing conditions (restart required, "Can't be turned on here")
     stay inline; a card holding a marked field opens itself and the mark stays until typed into. A
     refused form save is not a Station Event (nothing changed in the station).
     C.6–C.8 passed (ClubLog callsign placeholder 7Q5MLV; discard restored everything; a Drill QSO
     queued nothing — Drill has no bindings, so no queues). D steps 1–2 passed (Drill's QRZ row
     saved off with its key stored, restart pending, banner shown). D surfaced: the masked field's
     eye reveals only what was typed — a stored value never reaches the browser — so on an empty
     field it looks broken. **Ruled (after the drills):** `MaskedField` hides the eye while the field
     is empty (all four users: Station accounts, destinations, Email, Enrichment).
     **Superseded the same day (operator ruling):** the "•••••••• (set — leave blank to keep)"
     placeholder packed a status, a rule and an instruction into an empty-looking box. A field with
     a stored value shows no input — a status line "<Label>: ✓ saved" with **Replace** (opens an
     empty input with the eye, plus Cancel) and **Remove** (offered only where removal is allowed
     today); a field with nothing stored shows the input directly. Applies everywhere stored
     secrets appear (destinations, Station accounts, Email, Enrichment); it replaces the hide-the-eye
     fix. Build after the drills.
     **Declutter ruling (2026-09-26, during D; build after the drills):** the operator found the
     card with a pending removal noisy and hard to interpret. On top of the status line and the
     refusal toast: (3) a field's help line moves behind an ⓘ/tooltip (was an inbox note); (4) a
     row's queue counts and Retry/Clear appear only when something is queued or failed; (5) one
     unsaved-change star, on the card title only; (6) the restart banner becomes one short line,
     "Saved changes apply after a restart", with its own Restart daemon button. Target: a QRZ card
     showing only the row switch, "API key: ✓ saved [Replace] [Remove]" and the ⓘ.
     D step 3 passed (the pending removal saved; Drill's QRZ row off with no key; restart pending).
     **Operator direction:** re-run the whole drill set (C–F, and B once on air) on the build that
     lands the rulings above; these results stand only for build `-71`.
     E passed (the leave prompt named Forwarding; Cancel kept the edit; the confirm half was not
     run — covered by the guard suite and the re-run). F passed: Home active again, no restart
     pending, QRZ/ClubLog/SM Cloud on with keys stored, QRZCQ off, queues empty, only the legacy
     queue names (nothing of Drill's leaked into Home); Drill's ⚠ cleared by its successful
     activation. Seen during F, not investigated: Drill's "Last written" stayed at 2026-09-24
     14:43 after today's writes — inference: the column shows the main file's mtime, which SQLite's
     WAL mode updates only at checkpoint/close. The window.confirm dialogs look dated; a styled
     in-app dialog is logged as the next item after the declutter build.
     **Built 2026-09-26, the declutter (all rulings above):** `StoredSecretField` — a stored value is
     "✓ saved" with Replace (an empty input, with Cancel that sends a blank so the saved value is
     kept) and Remove where allowed; a pending removal is its own line with Undo; used by the
     destinations, Station accounts ("Reset to default" only on a STORED clearable field), Email and
     Enrichment (their own removal notes). `HelpTip` — a field's help behind a focusable ⓘ.
     Destinations: refused saves are "Save failed: …" error toasts (the inline box and the store's
     `refusal` state are gone); a card holding a marked field stays open; an idle row shows no
     queue line; one unsaved star (card title); the restart banner is "Saved changes apply after a
     restart." with Settings' own Restart daemon handed down. Station card titled "<name> service
     and token" (the suffix fits the one station-scoped type; revisit with a second); the SM Cloud
     destination with a complete account points at it ("Show"), never beside a refusal that
     already links. Manual: saved-value, restart and queue wording. Proofs (restores verified), each
     failing its own test: saved field still an input; Cancel keeping the typed value (the first
     test could not see it — strengthened); Remove everywhere; help not on focus; client and daemon
     refusals not error toasts; marked card collapsing; idle rows showing the queue; the row star
     back; the banner button inert; the pointer beside a refusal; the plain station title.
     **Re-run on `-74` (2026-09-26):** A passed (saved status lines right). Capitalisation fixed
     to "✓ Saved" (`a6ad1bec`). During C.2 the operator ruled (build after the drills): (1) a
     refused save — the SPA's or the daemon's — leaves every switch as the operator set it; the
     card stays starred as unsaved and its pill shows what the daemon holds. **A deliberate
     departure from ADR 0082 part 9** ("restores the last persisted switch state… never claims
     more than the daemon saved"): that rule predates the state pill, which now carries the
     daemon's truth, and restoring made the operator re-find the switch after typing the key. (2)
     The "Required to turn this on." line goes; the reason becomes the marked field's placeholder,
     in the same red as the ring (colour alone would not say why; a screen reader reads the
     placeholder).
     **Built 2026-09-28, the post-test bundle (fresh-install rulings, inbox):** rig picker with an
     unsaved draft and the last rig deletable with CAT off (`3fc11918`, `91a3939d`, `49bf6dc4`);
     reload after a confirmed restart, never over an unlogged QSO (`8b100ef2`, `1df0cfe9`); FT8/FT4
     off hides the FT links and lands every way in on Phone / CW, gated on `ft8_running`
     (`3ac5dada`, `e0f8ea5d`); Station accounts hidden when empty and no Ownership column
     (`cb735e7b`); the FT8 / FT4 and Enrichment tabs explain by ⓘ with a restart notice that stays
     after Save (`08bef531`, `a75f0654`, `bd6765f8`, `9f2a35d0`). Not deployed yet. Still open from
     the `-74` re-run: the C.2 ruling (a refused save keeps the switches as set; the reason as the
     field's red placeholder).
     **Built 2026-10-06 (`b02354e0`), the C.2 ruling (selected 2026-10-06, before 5C and 5F).** Acceptance: a
     refused save — the SPA's for a blank required field, or the daemon's — keeps every drafted
     switch, typed value and removal mark; the card stays starred as unsaved and its pill still
     shows what the daemon holds; the marked field's reason is its own placeholder in the mark's
     red, with no line under it. Nearest confusable outcome: the draft kept while the pill follows
     it and reads as saved. `bindings.svelte.ts` drops both restores (`#restoreSwitches` and the
     pre-wire switch reset); a re-read owed meanwhile now waits for a save or a discard, as for any
     unsaved draft. `StoredSecretField` puts `invalidNote` in the placeholder, with
     `placeholder:text-invalid` on the text input and in `MaskedField`. Manual: Forwarding step 3.
     ADR 0082 gained a dated update for the departure. Tests, RED first: B4, B6, B8b (the kept
     removal mark is resent by the next save), B24 (the owed re-read waits, then is paid on
     discard), D4 (switch kept, star, pill `mixed`, placeholder, no line), D4b (daemon refusal:
     switch kept, star, pill `mixed`), S7 and S7b. Guards that passed before the change: B6b
     (discard after a refusal returns the daemon's switches) and S7c (an unmarked field keeps its
     ordinary placeholder and colour). Reversion proofs, each failing its intended assertion with
     the restore verified: P1 the pre-wire switch reset (B4, B24, D4); P2 the daemon-refusal
     restore (B6, B8b, D4b); P3 the pill reading the draft (D2, D4, D4b); P4 the reason not used as
     the placeholder (S7, S7b, D4); P5 and P6 the red class dropped from the text and masked inputs
     (S7b; S7); P7 the separate line restored (S7, D4). Gates: lint, format, svelte-check, vitest
     (148 files, 2,085 tests), maintainability, manual build — all exit 0. jsdom does not paint:
     the red placeholder is the operator's visual check.
     **Accepted on screen 2026-10-06 (operator; deployed `2.0.0-alpha.3-169-geb6e1af5`, Home, Default
     logbook):** ClubLog, QRZ Logbook and QRZCQ switched on without their keys and saved — refused
     by the SPA; all three switches stayed on, each card kept its `*`, each pill still read
     DISABLED; every missing required field showed *Required to turn this on.* as its red
     placeholder inside the red outline, with no line beneath; ClubLog's callsign, which defaults
     to the logbook's, was not marked; one toast named each destination and its missing fields.
     **Design exchange (2026-09-26, during C.3–C.4): the Forwarding tab frames the wrong thing.**
     The operator read the per-logbook row "Drill 7Q5MLV" as the archive — the Drill archive's
     only logbook is also named "Drill" (Home's is "Default"). Found: the tab is destination-first
     and archive-framed ("Destinations for Drill"), while the unit that decides where uploads go is
     the LOGBOOK with its callsign; the same logbook row repeats in every destination card; the
     rows never say "logbook"; the every-logbook switch reinforces archive thinking; logbooks cannot
     be managed in Settings at all. An archive has NO callsign — each logbook has its own. Options
     weighed: A relabel rows ("Logbook: Drill (7Q5MLV)") keeping the layout; **B logbook-first** —
     choose a logbook (name + callsign, the Logbook view's selector) and list that logbook's
     destinations, the every-logbook switch becoming "copy to another logbook"; C a logbooks ×
     destinations grid. **Operator favours B** ("if I am confused, users will definitely be
     confused"). It changes the layout ADR 0082 part 9 decided, so it takes a NEW ADR, likely
     built as the Forwarding part of Settings → Logbooks; the data model is already per logbook.
     Not yet scheduled.
     **B's details (same exchange, recorded for its ADR):** a logbook picker at the top — tabs
     "Main · 7Q5MLV | Portable · 7Q5MLV/P" for a few logbooks, a dropdown beyond that; with one
     logbook a plain line "Logbook: Drill · 7Q5MLV"; the heading names the selection ("Where
     logbook Main (7Q5MLV) uploads", archive as small context); edits kept across logbook
     switches, a star on each logbook with unsaved edits, one Save. ClubLog's callsign becomes an
     override behind the logbook's own: one line "Uploads to your Club Log log for 7Q5MLV — this
     logbook's callsign" with "Use a different Club Log log"; an override always shows the
     mismatch ("…for 7Q5MLV/P, not this logbook's 7Q5MLV"). **Settled by review (2026-09-26):**
     keep persisting the defaulted callsign at save — the morning ruling's safety property (uploads
     never silently move to another Club Log log) is not reopened by a presentation change; a
     changed logbook callsign shows the mismatch until confirmed. A reversal would need the
     operator's explicit approval and its migration semantics. **Interim (operator, same day): option A now** — rows labelled as
     logbooks and the heading naming the archive plainly, until B replaces the layout.
     **Review of the 26 September packet (outcomes):** (a) the "service and token" station-card
     suffix stays while SM Cloud is the only station-scoped type — revisit when a second one
     appears; (b) leaving unconfigurable destinations out of `GET /v1/qso-archives/{uuid}/bindings`
     stays server-side: that endpoint is an actionable view, while `GET /v1/forwarder-types`
     remains the complete type catalogue; (c) the ClubLog callsign stays persisted at save (above).
     Findings fixed the same day: the packet's pushed/local wording, the archive-summary rules
     (inbox), the capsule refresh; the reboot-persistence routing awaits an operator ruling (inbox).
   - **5C — config v6 and the station account.** Legacy binding-owned keys (`name`, `enabled`,
     logbook-scoped credentials) known but deprecated at v6 (ADR 0075's shape). The version bump may
     retain those keys; only the adopted Home archive's committed seed marker permits the file-first
     stripping rewrite, which is retried on later starts while the keys remain. `validateForwarders`
     allows one entry per type; `ForwarderInfo` on
     `GET`/`PUT /v1/config` narrows to the station account (type, label, station-scoped
     `credentials_set`/merge, `action_filter`; no `enabled`, no logbook-scoped keys). A
     station-account PUT Build-probes every enabled binding in the active archive with the candidate
     account before persistence; an inactive archive is checked when activation builds its workers.
     The transitional view from 5B moves to this narrowed contract in the same commit.
     `EvidenceSyncCredentials` and `smd restore` read a credentialed SM Cloud account by presence,
     never a binding's enabled state.
     `smd config-downgrade --to 5` becomes data-aware: it opens the adopted Home archive through
     the `db-downgrade` container wiring, recombines each station account with that destination's
     bindings, refuses by name when the bindings cannot collapse to one v5 instance without
     changing enabled state or credentials, and otherwise writes the v5 shape. The collapse name is
     the default-logbook binding name, otherwise the lexicographically first binding name; no
     binding produces a disabled entry with a deterministic unused name derived from its type.
     Migration 0014 down uses the same target. The ruled order is config 6→5 first, then log 14→13.
     Drill on copies before the commit: both downgrades, both binaries boot. Docs: `config.md` §3.3,
     §7, §11, §13 and `install.md` §7 rollback recipe.
     Proofs: strip happens only after the seed marker commits; a failed strip is retried;
     duplicate-type file refused before any write; the non-collapsible downgrade refuses without
     rewriting.
     **5C review package (2026-10-06; rulings given the same day; no code yet).**
     *Already in the tree, so not 5C work:* one entry per type is enforced by `validateForwarders`
     (`internal/config/config.go:1895`, ADR 0082 part 3) at load and on the PUT candidate; the
     queue-name collapse for a downgrade is migration 0015's down (`legacy_name` first, then the
     default logbook's binding, then the first name), which supersedes the plan's inferred rule
     above (ADR 0082 dated update 2026-09-25). The seed marker and `legacy_name` exist (0014, 0015,
     `SeedLogbookDestinationsWithContext`). Nothing strips config.json yet; config is still v5.
     *Operator-observable outcomes:*
     1. After the first start on this build with Home's bindings seeded, `config.json` is v6 and
        its forwarder entries hold only station-account fields (type, label, station-scoped
        credentials, `action_filter`, endpoints, cadence, retry, `allow_insecure_http`); no
        `name`, `enabled`, QRZ/QRZCQ/ClubLog keys or SM Cloud `logbook`. Uploads, queue names and
        Forwarding look exactly as before. Nearest confusable: a stripped file whose bindings were
        never seeded (keys lost) — the strip runs only after Home's marker has committed.
     2. With another archive active at upgrade, the file is v6 but keeps the deprecated keys until
        Home is next active; nothing forwards from them (bindings rule), and the strip then runs.
     3. A failed strip write is logged and retried at the next start; the daemon still starts.
     4. Settings → Forwarding's Station accounts and `GET /v1/config` show one account per type
        with station fields only, whether or not the file has been stripped yet.
     5. Saving a station account (say a new SM Cloud token) is refused, naming the binding, when
        any enabled binding in the active archive could not be built with it; nothing is written.
     6. Evidence sync and `smd restore` keep working after the strip: they find SM Cloud by its
        complete station account, not by an `enabled` flag.
     7. Rollback: with the daemon stopped, `smd config-downgrade --to 5` rebuilds the v5 entries
        from Home's bindings plus the station accounts and writes them, or refuses by destination
        and logbook name — writing nothing — when a destination's Home bindings cannot become one
        v5 entry without changing an enabled state or a credential. Then `smd db-downgrade` takes
        the log schema below 14 (it opens `datastore.path`, not the catalogue's active archive —
        to verify in the drill that this is the Home file the config downgrade reads). Both old and new binaries boot the results (drilled on copies).
     *Code changes (inventory):* `migrations.go` v5→v6 stamp step (keys stay known); the
     data-aware 6→5 path in `cmd/smd/config_downgrade.go` (opens Home through `db-downgrade`'s
     container wiring; a pure document down is impossible); the strip after the seed in
     `startQso` (file-first, `config.WriteDocument`), gated on Home's committed marker;
     `types.ForwarderConfig` keeps `Name`/`Enabled` for v5 input and the down path only;
     `validateForwarders` stops requiring `name`; `applyDefaults`' seeded accounts carry no
     `name`/`enabled`; `ForwarderInfo` narrows (type, label, station `credentials_set`,
     `action_filter`) and the PUT merges by type, extending `forwarder_field_binding_owned` to
     `name`; `ForwarderStartupFinding` on the PUT becomes a probe of the active archive's enabled
     bindings with the candidate accounts (`forwarding.BindingConfig` + `Build`, as
     `archive.buildCandidate`); `EvidenceSyncCredentials` (`validate.go:452`) by a complete
     account; `smd restore`'s default cloud logbook; `smd config-check`'s "enabled forwarder"
     count; the SPA's `api/forwarders.ts` and `config/forwarding.svelte.ts` (no name, no enabled;
     keyed by type) and the dead `fetchForwarders` in `api/config-blocks.ts`. Docs: `config.md`
     §3.3, §7, §11, §13; `api-endpoints.md` (`ForwarderInfo`); `install.md` §7 — its rollback
     recipe today runs `db-downgrade` FIRST, the reverse of the ruled order.
     *Rulings (operator, 2026-10-06):*
     (R1) A station-account PUT carrying `name` or `enabled` is refused 400
     `forwarder_field_binding_owned` on the key's PRESENCE — an empty `name` and `enabled: false`
     included — naming the field and saying the tab needs reloading; the refusal does not reload it.
     (R2) `smd restore` defaults to Home's default-logbook SM Cloud binding. `--forwarder` selects a
     named SM Cloud binding in Home, a disabled one included (restoring needs no upload consent).
     `--cloud-logbook` stays an explicit override that works even when Home's database is
     unavailable. When the default binding cannot be resolved, restore requires that override
     rather than guessing. A binding whose `logbook` field is empty keeps the existing cloud-name
     default.
     (R3) A once-only, owner-only (`0600`) recovery copy, `config.v5.json`. It must be an actual v5
     document: startup persists the migrated config BEFORE seeding (`persistResolvedConfig`,
     `cmd/smd/main.go:250`), so a copy taken at strip time would already be v6 — the copy is taken
     from the v5 bytes before that persistence. It is never overwritten, and its historical
     credentials are never substituted automatically for current bindings. A failed copy defers
     the strip; startup continues and a later start retries. It is historical recovery material,
     not a guaranteed lossless rollback after later edits. Settled 2026-10-06 (operator): a retry after a failed
     first copy may re-stamp the persisted, still-unstripped v6 document as version 5 only when
     that reconstruction is a complete v5-compatible document, validated against v5's
     requirements — a stamp-only migration does not prove later v6 saves kept the shape; if it
     cannot be represented faithfully, the deprecated fields stay and the strip is deferred. Such a
     copy is described as recovered at retry time, not as the original pre-upgrade bytes. The copy
     takes `persistResolvedConfig`'s ClubLog scrub with its existing guard (`cmd/smd/main.go:522`):
     only `credentials.api`, and only when the build carries a nonblank injected key; the logbook
     credentials stay. Tests for keyed and keyless builds with synthetic values; the reconstruction
     and backup-failure tests ship with the implementation commit.
     (R4) Confirmed: evidence sync requires a complete SM Cloud station account; `evidence.sync`
     stays the consent switch. Binding enablement and the active archive never decide that consent.
     *Acceptance cases added before implementation (operator, 2026-10-06):*
     8. A station-account save while Home's seed is deferred (another archive active) preserves
        the hidden legacy fields the seed still needs (`name`, `enabled`, logbook-scoped keys):
        the narrowed PUT must not drop what the narrowed GET does not show.
     9. The downgrade treats an UNBOUND live logbook as off. Comparing only existing binding rows
        could permit a collapse that turns forwarding on for that logbook in v5, where one entry
        serves every logbook.
     *Drill (pending):* config-first downgrade order; the Home file confirmed by its archive
     identity, not assumed from `datastore.path`; the old and new binaries each boot their OWN
     copy, so the new binary's upward migrations cannot touch the old binary's proof.
     *Status:* built 2026-10-06 (`52077508`, `ba830078`, `25692f7a`, `6fadea1d`, `2f6a3677`, `2e4e49eb`); deployed; evidence below.
     **5C commit (a), characterization (2026-10-06, tests only, passing on v5):** synthetic,
     distinct values throughout, so reading the wrong source fails visibly.
     `cmd/smd/config_load_forwarders_characterization_test.go` — an explicit version-5 JSON file
     with all four destinations (QRZ, ClubLog, SM Cloud enabled; QRZCQ disabled), loaded through
     `config.Load` with every forwarder package linked: L1 kept: exactly the four entries; L2
     CHANGES: each keeps `name` and `enabled`; L3 CHANGES: every credential preserved exactly,
     logbook-scoped keys included; L4 kept: label, action_filter, endpoints, tick, batch, retry,
     allow_insecure_http. `internal/config/evidence_credentials_characterization_test.go` — E1
     kept: the SM Cloud entry's url and token, beside QRZ; E2 CHANGES: a disabled complete entry
     is refused today; E3 kept: an incomplete account refused, no value in the error; E4 CHANGES:
     evidence.sync's finding for that disabled entry.
     `internal/api/handler_config_station_accounts_characterization_test.go` — station-shaped
     entries built directly: G1 kept: no credential value (every one, ClubLog `callsign` and QRZCQ
     `call` included) in the GET body; G2 CHANGES: name, enabled and logbook-scoped keys served;
     P1 kept: a new SM Cloud token replaces only that key, every other secret and enabled state
     preserved; P2 kept: blank station keys keep their values on an enabled account; P2b kept: the
     same on a DISABLED account, which no build check covers. The PUT cases hold QRZ disabled: the
     api test binary does not link the QRZ package (an existing test registers its ADIF prefix by
     hand), and a PUT builds every enabled entry; this does not claim to test QRZ construction.
     `cmd/smd/restore_selection_characterization_test.go` (dry-run with the export seam): S1
     CHANGES: no flags → the DISABLED SM Cloud entry and its logbook; S2 CHANGES: `--forwarder`
     matches the entry name case-insensitively, a non-SM Cloud name is refused and nothing
     fetched; S3 kept: `--cloud-logbook` overrides; S4 kept: an empty logbook restores the
     cloud's "main". Sensitivity, each mutation restored and verified: M1 evidence ignoring
     `enabled` fails E2 and E4; M2 a case-sensitive `--forwarder` match fails S2; M3 ignoring
     `--cloud-logbook` fails S3; M4 an under-reported credentials_set fails G2; M5 a blank
     overwriting a stored station key fails P2 at the PUT's build check and P2b at its
     stored-value assertion; M6 load overwriting a station tick fails L4; M7 default seeding by
     name instead of type fails L1. Gates: gofmt (whole tree), vet, `go test ./...`,
     maintainability — all exit 0.
     **5C commit (b), the slice (2026-10-06, `52077508`; review fix `ba830078`).** The L2/L3 labels
     were corrected to KEPT (operator): loading never strips — a v5 document keeps its legacy
     fields until Home's seed commits, and only the later strip changes the file. What was built:
     config v6 (stamp-only 5→6; `6→5` document step stamps only an unstripped file); `name`
     optional in `validateForwarders`, `name`/`enabled` omitempty; the strip in `startQso` after
     the seed, Home only (`config_v6_strip.go`); the once-only `config.v5.json` (v5 bytes read
     before `persistResolvedConfig`, else the unstripped v6 file re-stamped and validated in
     memory by `config.CheckDocument`; guarded ClubLog scrub; exclusive staging + link, never
     overwritten); the narrowed `ForwarderInfo` (station-scoped `credentials_set`, `name` and
     `enabled` pointers only to refuse their presence), the merge by type keeping the deprecated
     fields, and the account-save probe of the active archive's enabled bindings
     (`station_account.go`); evidence sync by a complete account; restore's source from Home's
     bindings (`archive.ReadHomeBindings` → `sqlite.PeekDestinationBindings`, read-only,
     identity-confirmed); the data-aware `config-downgrade` (`config_downgrade_v6.go`); the
     default accounts without name/enabled and the seed's type-name fallback; the SPA station
     accounts keyed by type; `config-check`'s count worded; `rollback-drill.sh` config-first.
     Found while building: an existing guard (CS6) caught the config-save log printing a nameless
     account's whole entry, credentials included — the diff now keys accounts by type and never
     renders a list of objects whole (tests D3, D4 in `diff_test.go`).
     Tests, RED first: `config_v6_test.go` V1–V4; evidence E2/E4 flipped (CHANGED);
     `handler_config_station_account_test.go` A1 (R1 presence, incl. "" and false), A2 (case 8),
     A3 (binding probe; disabled binding not probed); G2 flipped; `config_v6_strip_test.go` T1–T7
     (T2 and T5's deferral were guards — green before); restore S1, S2, S4–S6 (S3 a guard);
     `config_downgrade_v6_test.go` D1–D10 (D6 a guard; D5 sharpened to require the credentials
     reason); SPA `forwarding.v6.svelte.test.ts` W1–W3, the store tests moved to the v6 wire.
     Existing tests updated to the narrowed contract (bodies without name/enabled; the build-check
     and setup tests now break an enabled legacy entry; the valid-blob test reads SM Cloud's
     station keys). Reversion proofs, each failing its intended assertion, restore verified: R1,
     A2, A3, G2, E2, diff D3/D4, V4, T1, T2, T3, T6 (guard removed; the first attempt did not
     compile and printed nothing — redone so it compiles), T6b, T7, S1, S6, D4, D5, D7, D8, W1,
     W2, C1, C2. Not separately provable: the copy's never-overwrite (an existing-copy check AND a
     link that refuses an existing file — either alone holds) and T5's validation (the document
     step refuses a nameless entry before the name check would).
     **Codex P1 on `52077508`, fixed in `ba830078`:** the rebuilt name came from the live
     bindings while migration 0015 down picks its queue target over ALL bindings (a deleted
     logbook's included) keyed on the file's default logbook; after a default change and the
     original logbook's deletion they diverged. The peek now runs the migration's own target
     SELECT; a destination bound only on a deleted logbook takes its target too. Tests D9, D10;
     proofs C1, C2. Codex clean on `ba830078`.
     Gates: gofmt, vet, `go test ./...`, maintainability (`run` and `handlePutConfig` kept at their
     floors by moving the new lines into helpers; `runRestore` ratcheted 62→53, 46→40, MI 15→16),
     frontend lint, format, svelte-check, vitest (149 files, 2,088 tests); cloud tests against
     `sm-pg` (smcloud, cloud, qsoservice; `-race`); `task ci:local` — all exit 0 (the release gate
     ran before the wrong-order message and the P1 fix; both re-verified with whole-tree Go
     tests and maintainability).
     **Rollback drill (2026-10-06, synthetic station in a 0700 directory under $HOME, never the
     operator's files; each binary booted its OWN copy):** P0 the pre-5C build (`d2ca1b42`, config
     5 / schema 15) seeded Home's four bindings from a v5 file; P1 the new build stripped its copy
     to station accounts (SM Cloud url and token only), wrote `config.v5.json` 0600 byte-equal to
     the v5 file read before its start, left the bindings unchanged; P1b a second start changed
     nothing; P2 `db-downgrade --to 13` first, then `config-downgrade --to 5`, was refused naming
     the cause ("Home's file holds no destination bindings … was smd db-downgrade run first?"),
     config untouched; P3 `config-downgrade --to 5` (catalogue Home id = the file's identity)
     rebuilt the exact v5 entries (qrz-home on, clublog / cloud-home / qrzcq off, their keys), and
     the pre-5C build booted its own copy with the bindings intact; P4 `config-downgrade --to 3`
     then `db-downgrade --to 9`, and alpha.3 booted its own copy at schema 9. Work directories
     shredded and removed.
     **Operator review of `52077508`/`ba830078` (2026-10-06): three gaps, all fixed.** (P1) The
     recovery copy and the downgrade could write a file v5 rejects: v6 relaxed TWO v5 rules, and
     only the name rule had been restored — v5 also requires an ENABLED smcloud entry while
     `evidence.sync` is on. `checkV5Compatible` now enforces both before either write; nothing is
     changed to pass it (copy deferred; downgrade refused naming evidence.sync). Tests T8 (both
     sources), D11/D12; proofs V1, V2. (P2) An interrupted copy's fixed `.tmp` staging file blocked
     every retry: staging is now unique (`CreateTemp`), publication stays exclusive (link), and
     stale staging files (they hold credentials) are removed once a copy stands. The rework of T3
     exposed a further defect, fixed with it: a directory at `config.v5.json` counted as an
     existing copy, so the strip ran with no copy — only a regular file counts now. Tests T3
     (a non-file at the copy's name defers), T3b (leftover staging never blocks and is removed);
     proofs S1, S2, S3. (P2) R1 missed explicit JSON nulls: `ForwarderInfo.UnmarshalJSON` records
     key presence, so `"name": null` and `"enabled": null` are refused 400 with nothing written.
     A1 null cases; proofs N1, N2. References updated (config.md §3.3, §13.2 — whose rebuild
     condition now says "once Home's seed has committed, stripped or not"; install.md §7;
     api-endpoints.md). Gates re-run: gofmt, vet, `go test ./...`, maintainability, `task ci:local`
     — all exit 0. Drill re-run with two added phases: P1c an interrupted staging file present →
     copy written, leftover removed; P3b evidence sync on with Home's SM Cloud binding off →
     downgrade refused naming the conflict, file untouched; P0–P4 as before.
     Committed `25692f7a`. **Codex P2 on `25692f7a`, fixed in `6fadea1d`:** the v5 check looked
     up `url`/`token` case-sensitively, while v5 decodes them into a tagged struct (encoding/json
     matches keys case-insensitively), so a v5-valid `URL`/`TOKEN` entry would have blocked the
     copy and the downgrade; the check now decodes the same struct. Test T9; proof K1. Codex clean
     on `6fadea1d`; whole-tree Go tests, vet and maintainability re-run (the release gate and the
     drill ran on `25692f7a`; the fix only widens which credential spellings pass, and the drill
     uses lower-case keys).
     **Operator review, R1 case variants (2026-10-06), fixed in `2f6a3677`:** presence was recorded
     under the exact lower-case keys while encoding/json matches keys case-insensitively, so
     `"Name": null` / `"ENABLED": null` returned 200. Presence now matches as the decoder does.
     A1 gains both (and `"NAME"`, already refused — a guard), asserting 400
     `forwarder_field_binding_owned` with the stored account unchanged; proofs N3, N4. Codex clean;
     API package, whole-tree Go tests and maintainability re-run; no drill needed (API only).
     **Deployed 2026-10-06 (`2.0.0-alpha.3-169-geb6e1af5`).** First start with Drill Arc active:
     config v6, nothing stripped, no copy — and no log line, which read as a cleanup that never ran
     (the capsule's "waits, logged" was wrong for this case). Operator ruling (b): log the wait —
     `2e4e49eb` (T2 asserts the line, T2b its absence once stripped; proofs L1, L2; codex clean).
     After switching to Home (16:12:40): "wrote the one-time v5 recovery copy" (source: the v6
     file re-stamped, correctly — the deploy start had already persisted v6) and "config.json now
     holds station accounts only"; `config.v5.json` 0600 with the v5 names, no staging leftovers;
     `config.json` v6 with no names. Forwarding unchanged: Home holds NO bindings — its seed was
     decided at the fresh-install test's first start (2026-09-28 12:40:56), before setup created
     the Default logbook, so it seeded zero rows, and the logbook created afterwards stays unbound
     by design (ADR 0082 part 2). Uploads are therefore set per logbook under Destinations; none
     run today, before or after 5C.
     *Commit split (accepted):* (a) passing characterization tests first, pinning today's v5
     behaviour the change must keep (load of the station's shaped config; GET/PUT station fields; evidence sync and
     restore lookup); (b) the slice in one releasable commit with its docs references; (c) the
     dossier and ADR 0082 dated note. RED-first proofs: strip only after the marker commits; a
     failed strip retried; strip not run with another archive active; the narrowed GET before
     and after the strip; a PUT with `name`/`enabled`/logbook keys refused and nothing written; an
     account save refused when an enabled binding cannot build; downgrade recombines a seeded
     two-logbook Home or refuses without writing; evidence sync and restore after the strip;
     `applyDefaults` seeds no name/enabled. Cloud tests against `sm-pg` (credential readers).
     Rollback drill on copies before commit (b): config 6→5 then log 15→13; the tagged alpha.3
     and the new binary each boot.
   - **5D — the bindings API.** `GET`/`PUT /v1/qso-archives/{uuid}/bindings`, 409 unless `{uuid}`
     is the active archive; GET = per destination the station account's presence, the aggregate
     state (`on`/`off`/`mixed`) and per live logbook the binding (enabled, `credentials_set`,
     `forwarder_name`, queue counts), plus `restart_required` against the running snapshot; PUT
     validates the whole candidate first and Build-probes every resulting enabled binding using the
     same station/binding merge as startup. If any enabled candidate lacks a required
     logbook-scoped field, the whole request fails naming that field and no persisted row changes.
     An aggregate write creates absent rows; `credentials_clear` is accepted only for a row that
     ends disabled; every affected row commits in one transaction; masked-on-GET, merge-on-PUT, no
     value ever echoed.
     Until 5F proves identity support, only the seeded adopted-Home SM Cloud binding may be enabled;
     any other SM Cloud enable is refused with `smcloud_identity_unavailable`. Queue counts and
     actions use the binding-aware ports landed in 5B. Docs: `api-endpoints.md` (new section under
     QSO archives; forwarder endpoints reworded). Proofs: 409 for inactive; atomicity (one invalid
     row rejects the whole PUT); clear refused on an enabled row; no credential value in any
     response or log.
   - **5E — the Forwarding tab and the logbook consumers.** `ForwardingSection.svelte` and
     `forwarding.svelte.ts` rewritten for the active archive as ADR part 9 (destinations with the
     aggregate switch and per-logbook rows; station accounts with no pill; a required field left
     blank rejects the complete save, marks the field and restores the last persisted switch
     state, so the switch never claims more than the daemon saved; disabled switch with reason + link when the account is missing
     or when SM Cloud is not identity-ready; restart-required banner as today);
     the tab's gate banner, the gated-pill code and their tests removed (`ArchiveSwitchGate`, the
     identity overlay, is untouched); the
     archive creation form gains the one sentence; the logbook view's `uploadStatus` and backfill
     picker source destinations from the bindings readout, not `/v1/config`. The logbook creation
     form's explicit SM Cloud option (ADR part 2) ships with Settings → Logbooks — no SPA logbook
     creation exists today — the rule itself holds from 5B. Manual: `forwarding.md`,
     `qso-archives.md`. Frontend gates + rendered tests for each state.
   - **5F — SM Cloud identity (AC 6).** Server: Postgres migration (`archives` unique on
     `(tenant_id, archive_uuid)` with label; `logbooks.uuid` unique per tenant, `archive_id`; the
     per-tenant legacy archive), UUIDs accepted on `PUT /v1/qsos`, manifest, reconcile and export,
     an explicit idempotent adoption endpoint keyed by the legacy name, `GET /v1/version` reporting
     identity support. The server-compatible commits land before the client begins sending the new
     wire. Client: the smcloud binding's adoption once per archive (`remote_adopted_at`),
     `legacy_logbook_ambiguous` when several seeded bindings share one legacy name (compatibility
     pushes + the legacy reconciler kept, no identity-aware reconciler), refusal to enable an SM
     Cloud binding outside the adopted archive until server and mapping are identity-ready, one
     reconciler per enabled binding replacing the boot-time one, `POST /v1/smcloud/reconcile`
     aggregated, `smd restore` by logbook UUID into `--archive` and whole-archive provisioning.
     Needs the Postgres dev DB (`SMCLOUD_TEST_ALLOW_DEFAULT=1`, `sm-pg`). Proof of AC 6: two
     archives with the same label and logbook name, each bound, reconcile and restore without the
     other's rows; the legacy archive adopts its existing rows without a duplicate.
     **5F review package (2026-10-07; rulings given the same day; no code yet).** Protocol
     alternatives weighed in [ADR 0088](../decisions/0088-sm-cloud-identity-protocol-on-scoped-paths.md).
     *Survey of the tree at `d183e8e8` (read-only):* nothing of the identity exists on the SM Cloud
     side. The cloud keys a logbook by `UNIQUE (tenant_id, name)` (`internal/cloud/store/migrations/0001_init.up.sql:20-27`)
     and a QSO by `(tenant_id, uuid)` (0004). It has no `archives` table and no `logbooks.uuid`.
     `PUT /v1/qsos` carries only the logbook NAME (`server.go:371-374`), and the server creates the
     logbook on first push (`EnsureLogbook`, `:445`). Its streaming decoder silently SKIPS any key
     other than `logbook` and `qsos` (`server.go:206-230`), and `GET /v1/export` reads no query
     parameter (`:555`). `GET /v1/version` returns only `{"version"}` (`:351`). Reconcile and
     manifest address the cloud's numeric id; the client resolves the name to an id through
     `GET /v1/logbooks`, taking the first match by name (`reconcile.go:435-451`). Restore does the
     same over the whole-tenant export (`cmd/smd/restore.go:90-104`).
     The client never probes the server's version, and it treats every 4xx except 408 and 429
     (401 re-armed) as terminal, a 404 included (`smcloud.go:386`). The cloud-name normalization is
     `TrimSpace`, then blank = `"main"`; the comparison is exact and case-sensitive (`smcloud.go:169-172`).
     `remote_adopted_at` exists (0014/0015) but nothing writes it. One reconciler runs, for the
     default logbook only (`lifecycle_adapters.go:877-907`). `POST /v1/smcloud/reconcile` answers
     its single summary; no SPA code calls it, and `docs/smcloud-deploy.md` documents it with `curl`.
     The 5D gate (`internal/archive/bindings.go:331-343`, reason `binding_not_enableable`) refuses SM
     Cloud on any archive that is not legacy-owned. Inside Home it allows EVERY logbook, not only
     the default binding. The seed copies the one `logbook` name to every live Home logbook
     (`cmd/smd/archive_bindings.go:27-62`). The logbook form's "with SM Cloud" sends no name, so it
     means `"main"`. **Two Home logbooks can therefore already push into one cloud logbook today.**
     That was v5's normal shape too: one entry served every logbook. The cloud upsert MOVES a QSO's
     logbook on a newer revision, and refuses a tie at a different logbook with 409 (`store.go:222-253`).
     Evidence sync is tenant-scoped (no logbook or archive field), which matches ADR 0071; 5F leaves it alone.
     Found in passing: `testguard.go:23` and its returned skip message both say `task test` and
     `task db:pg:up` set `SMCLOUD_TEST_ALLOW_DEFAULT`. They do not (`Taskfile.yml:236-249`); only CI
     does. That gets its own separate correction.
     *Operator-observable outcomes:*
     1. Upgrading the daemon while the SM Cloud server is still old changes nothing. Home's SM
        Cloud uploads, reconcile and restore behave exactly as today. On any other archive, the
        switch stays off, and the reason distinguishes "the SM Cloud server does not report archive
        identity support" from "the SM Cloud server could not be reached at start". Neither stops a
        QSO being logged. Nearest confusable: an identity upload that an OLD server accepts by
        ignoring new fields and filing under the name. That would be a silent merge; ADR 0088's
        scoped paths make it impossible.
     2. Upgrading the server moves no data. Every existing cloud logbook sits in the tenant's legacy
        archive with the same rows and counts. An old client's push, listing, reconcile and restore
        all still work, and keep working when a managed archive holds a logbook with the same
        display name.
     3. The first daemon start against an identity-ready server adopts Home's cloud logbook once.
        This happens only when the mapping is unambiguous (Q3). It stamps the cloud logbook with
        Home's archive UUID and the logbook's UUID, and records `remote_adopted_at`. From then on
        Home uploads to the identity paths. A repeat (or a retry) is a no-op, and no second, empty
        cloud logbook appears. Nearest confusable: adoption by a different archive (a second station
        on the same tenant), or a conflicting logbook UUID mapping. Both are refused by name, never
        merged. A copied, unchanged Home file carries the same UUIDs, so its adoption looks like a
        retry. That is ADR 0071's copy semantics (a backup of the same archive), and 5F does not
        claim to detect it.
     4. When Home's legacy mapping is ambiguous (Q3), adoption does not run. Home keeps the old wire
        and the default-logbook reconciler. Settings → Forwarding says why
        (`legacy_logbook_ambiguous`) and that the fix is manual. Nothing is stamped.
     5. An identity binding never falls back to the name. Against an old or unreachable server its
        rows stay queued and retry; they are never stranded and never filed by name.
     6. One reconciler runs per enabled SM Cloud binding. `POST /v1/smcloud/reconcile` runs them all
        and answers one result per binding (Q6). Only then can SM Cloud be turned on for a second
        logbook or a managed archive such as Drill. Its QSOs reach a cloud logbook keyed by the
        logbook's UUID, under that archive's UUID.
     7. `smd restore` restores one cloud logbook by UUID into `--archive` through a scoped export,
        and never receives another archive's rows. The legacy name is a separate, explicit option.
     8. AC 6: two archives with the same label and the same logbook name are each bound. They
        reconcile and restore without the other's rows, and the legacy archive adopts its existing
        rows without a duplicate.
     *Sub-slices* (each its own RED-first commit series; every boundary releasable; the server
     commits are deployed before any client commit relies on them):
     5F.0 (ruled Q0, a separate commit, before 5F proper): new SM Cloud enables in Home are refused
     for every binding except the one on Home's default logbook. This covers both the bindings PUT
     and the logbook form's "with SM Cloud". Existing enabled bindings and their queues are KEPT for
     compatibility. This prevents NEW merges; it does not repair a merge that already exists.
     5F.1 server schema: Postgres 0007 `archives` (`tenant_id`, nullable `archive_uuid` unique per
     tenant, `label`, `legacy` flag; one legacy row per tenant), `logbooks.archive_id` (backfilled
     to the legacy row), nullable `logbooks.uuid` unique per tenant, and name uniqueness narrowed to
     `(archive_id, name)`. The name-only routes (push, listing, reconcile, manifest, export) resolve
     and list ONLY the legacy archive's logbooks. The legacy name is its own column, independent of
     the mutable display label. Down step included. No wire change for old clients.
     5F.2 server identity wire (ADR 0088): the scoped upload, manifest, reconcile and export paths;
     `POST /v1/archives/adopt`; and `identity_protocol: 1` on `GET /v1/version`.
     5F.3 client, Home only: the version check at start (unreachable and unsupported reported
     separately), Home's adoption, the ambiguity rule, Home's default binding moved to the identity
     paths, and 404 on an identity path classified as retryable. The multiple-binding gate stays as
     it is (5F.0's Home-default-only rule, and the refusal on managed archives).
     5F.4 reconcilers and the gate lift, delivered together: one reconciler per enabled SM Cloud
     binding (lifecycle per ADR 0070), the aggregate endpoint, and only then the gate moved from
     "Home default only" to "identity-ready and unambiguous". ADR 0071 makes per-logbook reconcile a
     prerequisite (`0071-first-class-qso-archives.md:288`). `api-endpoints.md` and
     `smcloud-deploy.md` change in the same commit.
     5F.5 restore by UUID into `--archive` via the scoped export.
     *Characterization first (tests only, passing today):*
     - the name-only wire: create on first push, and the same name reaching the same logbook;
     - today's silent skip of unknown push keys and of export query parameters (the hazard behind
       ADR 0088);
     - 404 classified as terminal;
     - the single reconcile summary's shape;
     - restore selecting by name from the whole-tenant export;
     - the Home-only gate (already `TestBindings_SmcloudOnlyOnTheAdoptedArchiveUntil5F`).
     *Rulings (operator, 2026-10-07):*
     (Q0) Yes, as 5F.0 above.
     (Q1) `"identity_protocol": 1` on `GET /v1/version` (unauthenticated, absent on an old server),
     checked at daemon start with a bounded timeout. An unreachable server is distinguished from one
     lacking support, and neither blocks local logging. A server upgrade takes effect at the next
     daemon start.
     (Q2) New scoped paths (ADR 0088), with no name fallback for identity bindings. The promised
     queue retention is implemented explicitly: today a 404 is terminal (`smcloud.go:386`); on an
     identity path it becomes retryable, and the row stays queued. The check gates only enabling and
     adoption.
     (Q3) Ambiguity, revised. The rule counts enabled AND disabled Home SM Cloud bindings and groups
     them by the forwarder's actual normalization (`TrimSpace`, blank = `"main"`, exact match). It
     examines EVERY name group being adopted, not only the default's. A logbook is not excluded
     because its live count is zero: deletion leaves tombstones and history
     (`internal/qsoservice/delete.go:16`). A logbook may be excluded only on evidence that it never
     contributed to that cloud name; otherwise the group stays ambiguous and needs manual recovery.
     (Q4) `POST /v1/archives/adopt`, transactional and idempotent, with `legacy_name`,
     `archive_uuid`, `archive_label`, `logbook_uuid`, `logbook_name` and `callsign`. It refuses with
     409 `legacy_archive_adopted_elsewhere` when the legacy archive is already stamped with a
     different archive UUID, and with 409 when a logbook UUID or a legacy name is already mapped
     differently. It makes NO claim to detect an unchanged copied Home file: identical UUIDs are
     indistinguishable from a retry (ADR 0071, `0071-first-class-qso-archives.md:110`). A legacy
     name the cloud has never seen still succeeds, creating the logbook under the adopted archive.
     (Q5) After the transition the SM Cloud `logbook` field is only the adoption key. It is not sent
     and not shown for identity bindings. The server keeps legacy name resolution independent of the
     mutable display labels while compatibility clients are supported.
     (Q6) `{"results": [{forwarder_name, logbook_uuid, summary | error}]}`. Each error is sanitized
     and names the binding and the fault, never a credential. 503 `smcloud_unavailable` when no
     reconciler runs. `api-endpoints.md` and the `smcloud-deploy.md` `curl` examples change together.
     (Q7) Restore by one logbook UUID. Whole-archive provisioning (a new managed file carrying the
     cloud archive's UUID) is a later slice after 5F. The legacy name is an explicit option, never a
     fallback after a failed UUID lookup.
     (Q8) Revised. An old export handler ignores query parameters, so the guarantee that a restore
     never downloads another archive's rows needs the scoped export path an old server rejects
     (ADR 0088), not a query filter on `GET /v1/export`. The whole-tenant export stays for
     compatibility.
     Sequencing (operator, mandatory): the multiple-binding gate stays until per-binding
     reconciliation is ready; it lifts in 5F.4 together with the reconcilers. Server-first
     deployment stands.
     *Proofs to plan (RED first, each with a reversion proof, all against `sm-pg`):*
     - **Identity wire against an old server:** the server rejects the identity path, and the row stays
       queued and retries, never filed by name.
     - **Old client after 5F.1:** push, listing, reconcile, manifest and restore by name all reach the
       legacy archive, including when a managed archive holds a logbook with the same display name.
     - **Adoption:**
       - it is idempotent;
       - it refuses a different archive UUID;
       - it refuses a conflicting logbook UUID;
       - it creates no duplicate.
     - **Ambiguity:**
       - every name group is checked;
       - a disabled binding counts;
       - a zero-count logbook with tombstones or history counts;
       - an ambiguous Home stamps nothing and keeps the old wire.
     - **The gate:** it holds through 5F.3; the 5F.0 refusal covers both entry points and keeps the
       existing bindings.
     - **Reconcilers:** one per enabled binding, none for a disabled one; the aggregate carries no
       credential.
     - **AC 6:** two managed archives with equal labels and logbook names never see each other's rows
       in push, manifest, reconcile, export or restore.
     *Station drill (operator-run, per occasion):* deploy the server first, then the daemon. Then
     check, in order: Home adopts on start; counts are unchanged and nothing is duplicated; then,
     only with the operator's say, a dummy QSO on Drill. That QSO uploads to the operator's real SM
     Cloud tenant.
     **5F.0 built (2026-10-07, `4daf2a2e`; review fixes `956a01db`, `205f57ec`, `264335f7`, `f9440ade`, `f1499c7e`, `d8edef3b`).** In Home, a NEW SM Cloud enable is refused with
     `binding_not_enableable` unless it is on the default logbook (config's projection of the
     archive's own, the logbook the boot-time reconciler serves). An already-enabled binding is
     kept: re-submitting it, or editing its fields while it stays on, is not a new enable. Turning
     it back on after a saved disable is a new enable. Nothing at start changed: existing bindings
     route and keep their queues. This prevents NEW merges; it does not repair an existing one.
     Wire: `reason` per row (only on a row that is not enabled) and `new_logbook_reason` per
     destination on `GET /v1/qso-archives/{uuid}/bindings`. SPA: a refused row's switch is disabled
     with "Can't be turned on for this logbook: …". The every-logbook switch turns on only the rows
     that may be turned on. The logbook form's "Upload to SM Cloud" is offered only when
     `new_logbook_reason` is empty, and a create carrying the tick while it is refused is refused
     whole before the wire (no logbook, no binding). A daemon refusal arriving AFTER a create, for
     another cause, keeps the existing ruled behaviour: the logbook stays and the toast says SM Cloud
     was not turned on. Docs: `api-endpoints.md` (bindings GET/PUT), manual `forwarding.md` and
     `qso-archives.md`.
     Tests, RED first:
     - `internal/archive/bindings_smcloud_home_test.go` H1–H5: H2 (absent row beside a valid default
       enable writes nothing; stored disabled row), H4 (disable allowed, re-enable refused) and H5
       (row reason, `new_logbook_reason`, other destinations unaffected) were RED. H1 (default
       enable) and H3 (unchanged submission, its own field edited, the default enabled beside it,
       another destination edited; name and queue unchanged) passed before: they are guards against
       over-refusing.
     - `cmd/smd` `TestLifecycle_ExistingNonDefaultHomeSmcloudBindingSurvivesARestart`, a guard:
       the route is built and both queued rows are kept.
     - SPA `bindings.home-smcloud.svelte.test.ts` F1, F2 and F4 were RED; F3 (an existing enabled
       row toggled in the draft) was a guard. `logbooks.svelte.test.ts` adds two RED cases: not
       offered, and the add refused whole.
     Existing fixtures updated: the 5D gate test's config now names its default logbook; two SPA
     fixtures carry the new fields.
     Reversion proofs, each restored and verified byte-identical:
     - P1, the PUT ignores the row rule: H2 fails.
     - P2, a stored-enabled row not kept: H3 fails ("must stay editable").
     - P3, the view's row reason dropped: H5 fails.
     - P4, `new_logbook_reason` dropped: H5 fails.
     - P5, start drops non-default SM Cloud bindings: the restart guard fails (routes = []).
     - P6, `setRow` ignores the row reason: F1 fails.
     - P7, `setAll` ignores it: F2 fails.
     - P8, the row switch is not disabled: F4 fails.
     - P9, the add is not refused before the wire: the refusal assertion fails.
     - P10, the offer ignores `new_logbook_reason`: the not-offered case fails.
     Gates: gofmt (whole tree), vet, `go test ./...`, maintainability (0 regressions), frontend
     lint, format, svelte-check, vitest (150 files, 2,094 tests) — all exit 0. `task ci:local` passed
     on `4daf2a2e`. Subsequent review fixes through `d8edef3b` passed the frontend checks and
     maintainability checks; the full release gate has not been rerun on that revision.
     **Codex on 5F.0: five P2 rounds, all in the SPA's eligibility refresh, all fixed.** Codex was
     clean on `d8edef3b`.
     1. `4daf2a2e`: a refresh after a station-account save copied only destination fields, losing
        the new row refusals. The refresh now takes the row reasons and `new_logbook_reason`
        (`956a01db`; test F5).
     2. `956a01db`: a late eligibility answer undid a saved disable's refusal (`205f57ec`; F6, and
        F6b for a timed-out save).
     3. `205f57ec`: discarding edits re-applied the cached view and cancelled a read in flight
        (`264335f7`; F7).
     4. `264335f7`: a read sent while a load waited for the daemon's identity could patch the
        loaded view. The generation counter became ordering by SEND: each read carrying
        eligibility (a load's GET, a save's PUT, a timed-out save's re-read, an eligibility GET)
        is numbered as it is sent. A failed read counts as superseded only by a later eligibility
        read or load, never by a save that may be refused (`f9440ade`; F8, B16b).
     5. `f9440ade`: a late full view moved the boundary backwards. The boundary is now monotonic,
        and newer eligibility is kept on top of a late full view (`f1499c7e`; F9). The overlay
        source is the complete newest answer, not the cached view, so a row only the late view
        brings still gets its refusal (`d8edef3b`; F10).
     Each fix was RED first, with reversion proofs restored and verified: Q1, Q2, Q1b, Q3,
     R1–R11, R1b–R3b. Frontend gates and maintainability exit 0 on each commit (vitest 150 files,
     2,102 tests at `d8edef3b`). The lesson is the clustered-fix one again: a field read from a
     partial refresh needs every view-installing path and every ordering of their answers
     enumerated BEFORE the first fix.
     **5F.1 built (2026-10-07; server schema, no wire change): characterization `70cb1f9f`, slice
     `a6affeed`, archive-conflict fix `cc98c5c8`; codex clean on `70cb1f9f` and `cc98c5c8`.** Postgres 0007: the `archives` table
     with one `legacy` archive per tenant, unadopted (`archive_uuid` NULL), and `archive_uuid` unique
     per tenant (required unless legacy). `logbooks.name` is renamed `legacy_name`, now nullable,
     with a separate display `label` backfilled from it. `logbooks.archive_id` ties each logbook to
     its tenant's archive (composite FK). `logbooks.uuid` is nullable and unique per tenant. The
     legacy name is unique per ARCHIVE, no longer per tenant. Every existing logbook lands in the
     legacy archive; nothing moves.
     The down step is lossless while nothing is adopted, and refuses (changing nothing) once an
     archive or logbook identity exists.
     Store: `EnsureTenant` ensures the legacy archive (race-safe on the partial unique index). The
     name-only reads are scoped to the legacy archive: `EnsureLogbook`, `Logbooks`, `Logbook` (the
     per-id ownership check) and the export snapshot. A managed archive's logbook is therefore never
     resolved, listed, reconciled, manifested or exported for an old client: 404 on its id, absent
     from the listing and export. A push by name creates a legacy logbook beside it.
     Docs: `smcloud-deploy.md` (the schema-7 upgrade note and the rollback recipe); package docs
     for `store` and `server`.
     *Measured (throwaway tests, since removed; recorded here because they no longer exist):*
     - A golang-migrate instance holding only 0001–0006, run against a schema-7 database, returns
       `no migration found for version 7: read down for version 7 migrations: file does not
       exist`. `cmd/smcloud` treats a `store.Migrate` error as fatal (`main.go:440`), so an older
       smcloud refuses to boot on schema 7.
     - The rollback drill, on the dev Postgres (`sm-pg`): version-6 data (two tenants; logbooks
       main and portable; 3 QSOs) was migrated to 7, then
       `podman exec -i sm-pg psql -U smcloud smcloud -1 -v ON_ERROR_STOP=1 -f - -c "UPDATE
       schema_migrations SET version = 6"` ran with 0007's down file on stdin. psql printed
       `DO`, nine `ALTER TABLE`, `DROP TABLE`, `UPDATE 1`. Afterwards the version was 6, the names
       read main and portable, and there were 3 QSOs. The 0001–0006 migrator's `Up` then returned
       `no change`. The runbook gives the same command for a production box.
     Tests:
     - `server/legacy_wire_characterization_test.go` C1–C3, passing before the change: create by
       name, same name same logbook, the listing and per-id reads, export mapping.
     - `store/archives_migration_test.go` A1–A3: upgrade over version-6 data with two tenants, one
       with no logbooks; a new tenant's legacy archive; down lossless and refused for both identity
       kinds.
     - `server/legacy_archive_test.go` L1–L4: a managed archive planted FIRST, with a logbook whose
       display label is "main" and a QSO, so its id sorts first.
     - `migrate_test` now ends at version 7 from a version-1 database with data.
     All were RED before the migration. Every harness now drops `archives`: a shared `dropAll`
     replaced the down-file clean slate, which cannot drop `tenants` once `archives` references it.
     Reversion proofs, restored and verified:
     - K1, the listing not scoped: L2 fails with HTTP 500 (the unscoped read cannot scan the
       managed logbook's NULL legacy name).
     - K2, the per-id read not scoped: L3 fails with HTTP 500, not 404.
     K1 and K2 surface as compatibility regressions (an old client's listing or per-id read
     erroring), not as an observed data leak. The proofs did not show another archive's data
     reaching the client; they show the unscoped read failing.
     - K3, the export not scoped: L4 fails (the managed QSO exported).
     - K4, a new tenant gets no legacy archive: A2 fails.
     - K5, the backfill creates no legacy archive: A1 fails (the migration cannot set `archive_id`).
     - K6, the label not backfilled: A1 fails.
     - K7, the down refusal removed: A3 fails for both cases.
     - K8, the down step not restoring per-tenant name uniqueness: A3 fails.
     **Superseded ruling.** The operator first directed (2026-10-07) that the move-on-newer-revision
     rule stay unchanged and be recorded. Under it, an old client pushing a newer revision of a QSO
     stored in a managed archive would overwrite it and move it into the legacy logbook.
     Codex P1 on `a6affeed` showed this defeats the boundary that the slice enforces for reads.
     The operator accepted the finding the same day ("documenting the relocation behavior does not
     satisfy the legacy archive boundary"): a deliberate move between archives (ADR 0071) belongs to
     an identity-aware operation, never to the name-only wire.
     **Fix `cc98c5c8`:**
     - A name-only write to a QSO stored in another archive is refused with 409 `archive_conflict`,
       naming the UUID, at newer, equal and older revisions.
     - The guard sits in the upsert's own `ON CONFLICT ... WHERE` (the stored row's archive equals
       the target logbook's) and in the check of the row that clause locked. An archive conflict is
       reported before the version-tie check.
     - The legacy logbook (and archive) is provisioned in the SAME transaction as the QSOs
       (`store.UpsertLegacy`; ensure-by-`DO NOTHING`-then-read, so no row lock couples concurrent
       pushes). A refused batch therefore writes nothing, its logbook included; that resolves the
       earlier gap where `EnsureLogbook` ran before the upsert transaction.
     - Within the legacy archive a newer revision still moves a QSO between logbooks.
     Tests in `server/archive_conflict_test.go`:
     - X1, RED: newer, equal and older revisions each refused; payload, revision and placement
       untouched.
     - X2, RED: a mixed batch to a NEW name, with the valid QSO written first, stores neither and
       creates no logbook.
     - X3, a guard: the within-legacy move is unchanged.
     - X4, RED: a managed insert held uncommitted while the push waits on it, with a
       `pg_stat_activity` lock-wait barrier, is still refused once committed.
     The correlation test for a dead-database push now expects the single "upsert failed" line.
     Proofs, restored and verified:
     - G1, no archive clause in the conflict update: X1 (newer) and X4 fail.
     - G2, the post-check ignores the archive: X1 fails.
     - G3, the logbook created outside the transaction: X2 fails ("created its logbook").
     - G4, a refused batch committed: X2 fails (the valid QSO stored).
     Validation scope:
     - Before `a6affeed`, and again before `cc98c5c8`, all exited 0 on the committed contents:
       gofmt (whole tree), vet, `go test ./...`, the cloud suites with `-race` against `sm-pg`,
       maintainability (0 regressions) and `task ci:local`.
     - `70cb1f9f` holds a test that was in those runs unchanged.
     **5F.1 deployed (2026-10-07, operator).** smcloud was upgraded to schema 7 from the pushed tree
     (CI green on `aba1289e`, run 37600837941), following `smcloud-deploy.md`: a `pg_dump` first,
     before and after counts, the RPM upgrade and a restart. The operator reports that it booted and
     migrated, and the after query showed every logbook in its tenant's unadopted legacy archive
     with the same names and QSO counts. The counts were not pasted here. The rollback was not
     needed. Keep the pre-schema-7 dump until 5F.2's adoption is deployed and checked: once anything
     is adopted, the down step refuses and the dump is the only way back to version 6.
     **5F.2 design (2026-10-07; rulings given the same day; no code yet).** The server identity wire
     per ADR 0088. Choices weighed here are recorded in
     [ADR 0089](../decisions/0089-sm-cloud-identity-wire-creates-on-first-push-strictly.md).
     Already ruled, so not reopened:
     - the scoped paths that an old server 404s;
     - `identity_protocol: 1` on `GET /v1/version`;
     - transactional, idempotent `POST /v1/archives/adopt` with 409 `legacy_archive_adopted_elsewhere`
       and refusal of conflicting logbook mappings;
     - the scoped export;
     - the name-only routes stay legacy-only (5F.1).
     *Routes:*
     - `POST /v1/archives/adopt` with body `{legacy_name, archive_uuid, archive_label, logbook_uuid,
       logbook_name, callsign}`.
     - `PUT /v1/archives/{archive_uuid}/logbooks/{logbook_uuid}/qsos` with body `{archive_label,
       logbook_label, callsign, qsos: [...]}`. The `qsos` rows are as today, with the same validation
       and the same 1,000-row cap.
     - `GET /v1/archives/{a}/logbooks/{l}/reconcile`, answering `{archive_uuid, logbook_uuid, count,
       hash}`.
     - `GET /v1/archives/{a}/logbooks/{l}/manifest`, answering `{archive_uuid, logbook_uuid,
       entries}`. Corrected by the operator: the scoped responses carry the UUIDs, not only today's
       numeric logbook identity.
     - `GET /v1/archives/{a}/logbooks/{l}/export`, answering `{archive: {uuid, label}, logbook:
       {uuid, label, callsign}, qsos: [...]}` from one snapshot.
     - Every scoped read is 404 unless the archive is the tenant's and the logbook is in it.
     *Commit order:* the version flag lands LAST. A server deployed between 5F.2 commits never
     claims support it does not fully have.
     *Rulings (operator, 2026-10-07):*
     - (S1) **Create on the first identity push.** An unknown archive UUID becomes a managed archive,
       and an unknown logbook UUID a logbook in it. Archive creation, logbook creation, metadata
       updates and the whole QSO batch commit in ONE transaction; a rejected batch leaves none of
       them behind. Adoption is never inferred from a matching label.
     - (S2) **409 `archive_uuid_in_use`.** An existing non-legacy archive cannot absorb the legacy
       archive. Concurrent adoption and first-push attempts must end consistently, without partial
       changes.
     - (S3) **Refuse.** A logbook UUID belonging to another archive is refused with 409
       `logbook_in_other_archive`. A QSO stored in another archive is refused with 409
       `archive_conflict` at every revision, comparing against the REQUESTED target archive.
       Within-archive QSO relocation is kept. Deliberate cross-archive movement stays outside 5F.2.
     - (S4) **Migration 0008 adds `logbooks.callsign` now** (at most 32 characters, default `''`). Its
       down step is allowed only while every callsign is empty; otherwise it refuses before changing
       anything. 0007's guard does not protect an 8→7 downgrade from losing callsigns.
     - (S5) **Display values.** A successful identity push and the FIRST adoption apply non-empty
       `archive_label`, `logbook_label` / `logbook_name` and `callsign`; empty values keep the stored
       ones. A REPEATED adoption with the same identity mapping is a no-op, metadata included, so an
       adoption retry cannot undo a later label change. The legacy name stays immutable. Push
       metadata follows SERVER COMMIT ORDER: with no metadata revision, a delayed request can
       overwrite a later local edit's label. This is accepted and documented, not guaranteed against.
     - (S6) **Strict envelopes on the new bodies.** Unknown keys, duplicate keys and trailing JSON are
       refused with 400 `invalid_body`. The QSO payload contract (the rows inside `qsos`) and the
       name-only decoder keep today's behaviour.
     *Proofs to plan (RED first, against `sm-pg`):*
     - **Adoption:**
       - stamps once; a repeat is a no-op, metadata included;
       - a different archive UUID is refused;
       - a conflicting logbook UUID or legacy name is refused;
       - an unseen legacy name creates the logbook;
       - S2 refuses;
       - the whole call is atomic;
       - an adoption REPLAYED after a later label update leaves the label alone.
     - **Identity push:**
       - it creates the managed archive and the logbook;
       - it reaches an adopted legacy logbook by UUID;
       - S3 refuses, at every revision, against the requested archive;
       - within-archive relocation is kept;
       - labels follow S5;
       - a MIXED batch refused rolls back the metadata and the newly created archive and logbook.
     - **Concurrency:** adoption racing a first push for the same archive UUID; two conflicting
       claims of one UUID (archive or logbook) racing. Each ends in one consistent winner and a
       refusal with no partial change. Proven with lock-wait barriers, not sleeps.
     - **Strict envelopes:** unknown, duplicate and trailing content each give 400 `invalid_body`;
       the QSO rows are validated as today.
     - **Scoped reads:** 404 across archives and across tenants; reconcile and manifest carry both
       UUIDs.
     - **AC 6, server side:** two archives with equal labels and equal logbook names never see each
       other's rows in push, manifest, reconcile or export.
     - **The old wire after adoption:** still served from the legacy archive by name.
     - **Migration 0008:** the up step; the down step succeeds with empty callsigns and refuses,
       changing nothing, with a populated one.
     - **The version flag:** absent before its commit, present after.
   - **Station drills after deploy** (operator-run, recorded here): Home unchanged after the
     upgrade (same `forwarded_to`, worker names and queue counts as before; bindings listed under
     Home with the legacy names); the Drill archive shows every destination off, no banner, and a
     dummy QSO stores with `forwarded_to: []`; enabling a destination on Drill needs the operator's
     say per occasion (a real key uploads a dummy QSO to a real logbook); the rollback drill on
     copies; the two-archive SM Cloud proof after 5F.

   - **Archive contents (ADR 0084; ruled 2026-09-28).** Settings → Archives lists each
     archive's logbooks under its row, nested on the table's grid (name under Label,
     callsign under State, count under Size), muted and indented:

     ```
     Home             Active      148 KB    28/09/2026, 14:28:17
        Default       7Q5MLV      7,468 QSOs
        Contest       7Q5MLV      312 QSOs
     Drill            Inactive    96 KB     26/09/2026, 11:02:40   [Activate]
        Drill         7Q5MLV      1 QSO
     New archive      Inactive    40 KB     27/09/2026, 09:15:00   [Activate]
        Not known until it has been opened
     ```

     Rulings: the count carries its unit ("7,468 QSOs", singular "1 QSO") — the
     nested lines have no heading; EVERY logbook is listed (no "+N more": collapsing
     would hide the contents this view exists to show). An archive whose file changed
     since its summary was taken says "Counts may be out of date — the file changed
     since it was last open"; one with no summary yet says "Not known until it has
     been opened"; one with no logbooks, "No logbooks". The active archive is always
     current. Three slices, each test-first:
     1. **Sidecar store** (`internal/archive`): the versioned, UUID-keyed file at
        `<data_dir>/db/qso-archive-summaries.json` (via `GlobalDir`) — read (missing,
        corrupt or unsupported-version = cache miss with a reason, never an error that
        stops startup), atomic 0600 write, and the change signature (device, inode,
        ctime; size and mtime for diagnostics) with the stale comparison. Pinned: a
        same-size replacement with a preserved mtime reads stale.
     2. **Keeping it current** (daemon): build the active archive's summary on open;
        a bounded, coalescing post-commit observer for submit, delete, batch import,
        restore and logbook create/rename/delete, off the QSO response path (a blocked
        summary writer must not delay a QSO response); persist after a clean
        checkpoint (`wal_checkpoint(TRUNCATE)`) and close, and after an offline import
        or restore; serve the summaries on `GET /v1/qso-archives` (api-endpoints.md).
     3. **SPA + manual**: the nested logbook lines and the three states above; the
        manual's QSO Archives chapter.
     **Slice 2 rulings (2026-09-28).** Wire: each `QsoArchiveView` gains `logbooks`
     (`[{uuid, name, callsign, qso_count}]`, always present, `[]` when none) and
     `contents_status` (`current` | `stale` | `unknown`). The sidecar is written only on
     clean close, archive creation, and offline import and restore — never
     periodically; every failure is non-fatal (derived state). Sidecar
     read-modify-write merges are serialised so concurrent archive operations cannot
     lose entries. Split: **2a** builds and persists internally (no API exposure until
     2b keeps the active summary current) — on shutdown, after the writers drain: a
     final recount, `wal_checkpoint(TRUNCATE)`, close, the signature, then the merge;
     creation writes the new archive's summary. **2b** — a directly injected,
     non-blocking dirty notifier called after QSO commits and from the logbook handlers
     (not the hub: its subscriber can be evicted); a notification during a recount
     schedules another; the active summary reads `stale` while dirty or recounting and
     `current` only after a successful recount; then the API exposure. **2c** — `smd
     import` / `smd restore` rebuild the target archive's summary after close.
     **Built 2026-09-29 (2b review fix, 2c and slice 3):** an inactive archive
     with a non-empty WAL now reads stale even when its main-file signature still
     matches (absent and zero-byte WALs remain current); both offline commands
     recount their selected target, checkpoint and close it before recording the
     final signature, with every summary failure remaining non-fatal; the SPA
     strictly decodes the contents wire and renders every logbook plus the ruled
     stale, unknown and known-empty messages; the manual explains the list.
     Reversion proofs removed the WAL predicate, both command finalisers, the
     decoder mapping and the nested iteration: each reached its intended new
     assertion before restoration. Focused Go packages and all frontend gates
     passed (1,973 frontend tests; zero Svelte warnings); `task ci:local` then
     passed the full race, non-race, build and boundary gate.
     **Review of that worktree, 2026-09-29 (rulings the same day):** the SPA read
     the catalogue once at page load, so the active archive's counts froze at
     their boot value; the Archives tab now reads it again on every opening,
     never by polling, and a hidden tab reads nothing. The active archive's
     statuses have their own wording, because it is open: `stale` reads **Counts
     are being updated** and `unknown` reads **Current counts are not available**.
     Inactive archives keep the closed-file wording. Reversion proofs covered the
     reload (the section and the Settings tab), and each wording.
     **Deferred risk, bounded (ruled not to fix here):** `smd import` / `smd
     restore` assume the daemon is stopped but do not enforce it. Run against an
     inactive archive while the daemon runs, the command and the daemon can both
     merge into `qso-archive-summaries.json`; `MergeSummary`'s lock serializes
     only within one process, so one merge can overwrite the other's entry. The
     loss is derived state only: that archive reads `unknown` (or its older
     summary, judged by signature) until it next closes or is imported into.
     Cross-process locking is out of scope for this change.
     **Default tag, 2026-09-29 (operator ruling, option B):** each summary
     logbook carries `default` — the file's own default pointer, read from its
     identity row by `BuildLogbookSummaries`, so every archive's summary (live,
     at close, create, import and restore) marks the logbook it logs to when
     active; a file with no identity row marks none, and any other identity read
     failure fails the build like a failed count. The sidecar keeps format 1: a
     summary recorded before the field reads unmarked until its archive next
     closes. Setup that adopts an existing default logbook now notifies the
     summary too (the notify moved into `recordDefaultInArchive`). The SPA
     refuses a logbook row without the flag and tags it **Default**.
     **Settings → Logbooks, first slice (operator ruling 2026-09-29; inbox).**
     Bounded: it manages logbooks in the ACTIVE archive only, through the
     existing `POST`/`PATCH`/`DELETE /v1/logbook`; creating one never activates an
     archive, makes it the default or enables uploads implicitly. Acceptance
     criteria (operator-observable):
     1. A **Logbooks** tab names the active archive's label and lists its
        logbooks: name, callsign, QSO count, a read-only **Default** tag. The
        list is read again each time the tab opens.
     2. **Add logbook**: Name and Callsign (any valid callsign, prefilled with
        the station callsign, uppercased as typed). The form says live contacts
        keep going to the Default logbook; with a callsign other than the
        station's, it adds that such a logbook cannot receive live contacts yet.
     3. SM Cloud appears as an explicit, unticked option only when this archive
        can turn it on (the bindings view lists it with no `reason`); no other
        destination is offered and none is enabled implicitly. Ticked, the
        logbook is created and then its SM Cloud binding enabled; if that second
        step fails, the logbook stays and the message says SM Cloud was not
        turned on. A binding applies after a restart, and the message says so.
     4. **Rename** edits the name in place (the callsign is not editable).
        Renaming the default logbook also updates the header's logbook name.
     5. **Delete** asks first; it is disabled, with the reason as a tooltip, for
        the default logbook, one holding QSOs, and one whose count is unknown.
        A daemon refusal (`has_qsos`, `default_logbook`, `not_found`,
        `duplicate_name`) is shown and the list re-read.
     6. A timed-out create, rename or delete is reported as an unknown outcome
        and the list re-read — never as a failure or a success.
     7. Every write is refused while an archive switch is in flight or
        unresolved (the archive-switch gate).
     8. After any change, Settings → Forwarding's rows are re-read when it has
        no unsaved edits; with unsaved edits they are left, not overwritten.
     9. (Ruling 2026-09-29, after review.) The Add form and a changed rename
        join the Settings leave guard, between Station and Rigs: switching tabs
        keeps them unprompted; leaving Settings or reloading warns; the
        untouched prefilled form and a merely opened Rename are not edits; a
        typed name, a changed callsign, a ticked SM Cloud or a changed rename
        is; a confirmed discard clears the Add form and cancels the rename; a
        create, rename or delete in flight refuses the leave until it resolves.
     Nearest confusable outcome: a logbook that looks usable for live logging.
     It is not — only the Default logbook receives live contacts until default
     selection exists (out of scope, with multi-callsign operating and
     contesting).
     **Built 2026-09-29 (uncommitted):** `createLogbook` / `renameLogbook` /
     `deleteLogbook` in `api/logbooks.ts` keep the refusal code and mark a
     timeout; `config/logbooks.svelte.ts` holds the rules (the switch gate,
     unknown outcomes, SM Cloud only as ticked, the header rename, the
     Forwarding re-read, and a read generation so an older read that answers
     last never replaces a newer list); `LogbooksSection.svelte` is the tab,
     placed after Station. The manual's QSO Archives chapter gains a Logbooks
     section (and the active archive's two count messages). No daemon change.
     Reversion proofs: API writes; SM Cloud implicit; the switch gate; timeout
     as unknown; Forwarding's unsaved edits; the Default not deletable; the SM
     Cloud reason; the header rename; the different-callsign note; the SM Cloud
     option's visibility; the read generation; and for criterion 9, the
     prefilled form counted as an edit, an opened rename counted, the discard
     keeping the Add form, an in-flight write ignored, the section unguarded, a
     load overwriting a typed callsign, and Logbooks placed after Rigs — each
     failed its intended test. The drafts moved from the component into
     `logbooksState` so the guard can read and discard them.
     **Review 412cca37 (Codex, two P2s, fixed 2026-09-29):** (a) a successful
     create or rename cleared the draft even when the operator had typed a newer
     one while the request was on the wire — now it clears only the draft it
     submitted, so a newer one stays and stays guarded; (b) a logbook change
     made while Forwarding held unsaved edits skipped Forwarding's re-read for
     good, and discarding those edits restored the old snapshot without the new
     logbook — the bindings store now owes the re-read (`requestReload`),
     reading at once when nothing is unsaved, paying the debt when edits are
     discarded or when a load that was already on the wire ends, and settling
     it on a save's fresh view. **Review of those fixes (three P2s, fixed the
     same day):** a kept rename draft now measures against the name just saved;
     a fresh view (load, save response, timeout re-read) settles only the
     re-read requests made before it was read — requests and coverage are
     counters, not a flag, so a save sampled before a logbook change no longer
     erases that change's re-read; and an owed re-read is paid wherever the
     edits can come to nothing (every edit, the end of any save including a
     refused one, a discard, the end of a load), not only on Discard. Reversion
     proofs for each rule failed their intended tests.
     **New archive form in the leave guard (inbox gap, ruled 2026-09-29).** The
     form's draft (label, first logbook, callsign, request key) moved from
     `ArchivesSection.svelte` into `archiveDraft` in `archives.svelte.ts`, and
     Archives joined the guard, last, as on the tab strip: an empty form (or a
     request key alone) is not an edit; any typed field is; a confirmed discard
     clears the fields and the key; a creation in flight refuses the leave; a
     successful creation clears only the draft it submitted, and a newer one
     typed meanwhile keeps its fields with a fresh request key (the old key
     names the archive just made). Reversion proofs for each rule — and for
     the section's position — failed their intended tests.
     Review (P2): the key, now in a module singleton, outlived an emptied form
     — a create whose response was lost, the fields erased, a clean exit, then
     a different archive submitted under the old key and answered with the
     first. The key now belongs to one draft's retries: it is retired the
     moment every field is empty and on every exit from Settings with nothing
     at stake (a `leaving` hook in the guard); a kept, typed draft keeps it.
     Reversion proofs for each retirement point failed their intended tests.

**ADR 0085 rule 3 slice 2 — Restore: acceptance criteria (proposed 2026-10-01; corrected
and RULED the same day — ADR 0085 records the rulings).** Built on the ADR 0086 panel and
the attribution prerequisite (`c204e32f`). Each line is tested — several need more than one
case; "apart from" names the nearest wrong outcome.

*Record.* RS1 A save records the attribution read from `GET /v1/submit-attribution` inside
the boot identity bracket; a failed read records it as missing (`null`), never a value from
later configuration. Missing attribution reads "Original attribution unavailable" (a failed
read or an older record alike). RS2 A record carries a state — `draft`, or `logged` with the
returned QSO UUID — and, when a submission was attempted, the EXACT attempted submission. A
v1 record keeps its `outcome`: `unknown` stays unknown — apart from a migration that makes it
read as definitely unlogged.

*Offering Restore (panel).* RS3 Restore is offered only on a `draft` record, in a browser
with Web Locks; otherwise the entry says why and keeps Show details / Copy. RS4 On Phone / CW
Restore needs the record's archive to be the one this page booted on and its logbook UUID to
be the active default's; otherwise it names the source and never switches. RS5 MY_RIG must
equal today's server stamp — a mismatch is a hard refusal showing saved and current; missing
attribution refuses as "Original attribution unavailable". Saved OPERATOR / MY_NAME are NOT
compared with today's defaults: Restore supplies them explicitly and the server checks all
three on the prepared QSO. RS6 Other pages offer "Go to Phone / CW", which only navigates.
RS7 A `logged` record shows its QSO UUID and Discard only.

*Claiming.* RS8 Restore takes an exclusive Web Lock on the record's UUID with `ifAvailable`;
two tabs restoring at once leave exactly one winner; the other form is unchanged and says
"In use in another tab". RS9 With the lock held, the form, destination, MY_RIG and the record
(re-read from storage) are checked again; any change releases and refuses — apart from filling
a form typed into while the claim was pending. RS10 The lock stays with its tab across panel
close and navigation; Discard elsewhere must acquire the same lock and is refused while held
(`locks.query()` is display only). RS11 Closing or reloading the owning tab releases it; the
record keeps its latest committed revision.

*The restored form.* RS12 Every saved field is restored with its original times; the QSO
clock neither runs nor restamps, and a blank recovered end time does NOT become "now" at Log.
RS13 The card shows "Recovered QSO from ‘Home’" with the saved frequency, band, mode/submode
and their basis; Log stays disabled until they are confirmed or corrected; changing any
withdraws the confirmation; a missing or inconsistent value must be corrected. No rig command
is sent. RS14 While a recovered QSO is on the form, rig reports neither rewrite its reports
nor restart its clock, and the stack/load shortcuts (Shift+Enter, Shift+↑/↓, the pile-up
Load) cannot replace or clear it without going through Clear's protection. RS15 Report
validation uses the recovered mode.

*Saving edits.* RS16 Each edit is saved: one write in flight, the newest edit coalesced into
the next, no timer; a failed save is shown and retried by the next edit. RS17 Clear / Escape
waits for the latest edit — including one arriving DURING that final save — to commit, then
empties the form and releases; a failed save keeps the form, says so and keeps the lock.

*Discard in the owning tab (ruling 3).* RS17a Allowed directly, with confirmation: outstanding
saves are settled and later writes cannot recreate the record; the delete is awaited; only
then is the form emptied and the lock released. A failure keeps everything. Clear and Discard
are disabled while a submission is in flight.

*Logging.* RS18 Log is gated by the archive gate, the active default logbook's UUID, the lock,
the confirmed rig values, validation and the in-flight latch — NOT by the CAT link. RS19
Before the request, the EXACT submission about to be sent is persisted and the write awaited;
a failed write sends nothing. Later edits keep that snapshot. RS20 The request carries the
saved frequency, band, mode, submode, OPERATOR, MY_NAME and MY_GRIDSQUARE explicitly (not the
rig's or today's), and `expect_*` from the saved attribution; the stored QSO's values are
asserted. RS21 A 409 `attribution_changed` keeps form and record and shows saved and current
values. RS22 A confirmed store deletes the record, else marks it `logged` with the QSO UUID;
whatever cleanup does, this tab can no longer submit it — the confirmed UUID is shown with a
cleanup retry, and cleanup never resubmits. Only a reload, if both cleanup writes failed, falls
back to the persisted unknown state. RS23 A transport failure, a malformed success response,
an interruption after sending, or a tab closed mid-request leaves the persisted attempt: the
record reads "logging outcome unknown" from then on, is never retried automatically or called
"not logged", and a later definite refusal does not erase it.

*Unknown outcome (ruling 1).* RS24 An unknown-outcome record may be restored under the usual
gates; Log first requires "I checked the original Logbook; this contact is not already
logged." Cancel sends nothing; confirming authorises ONE attempt and keeps the earlier
uncertainty recorded.

*Duplicate response (ruling 2).* RS25 A recovered submit answered "duplicate" keeps the form
and the record, offers the existing QSO for inspection, and keeps "Log anyway" only as an
explicit "this is a separate contact" decision; it never marks the draft logged (the dedupe
key ignores reports, notes, attribution and seconds — internal/qsoservice/dedupe.go:91).

*Real browsers (ruled 2026-10-01).* Option 2: a scripted real-browser drill — two windows
in the same profile and origin — plus jsdom tests; Playwright stays a separate decision. RS8
and RS11 stay pending until the drill passes: competing Restore attempts (not sequential
clicks after ownership), foreign Discard refusal, the claim kept across navigation, and
release on close and on reload. Restore stays unavailable until commit 4 integrates the
whole path; earlier commits expose nothing to ordinary Log, Clear or stack/load.

*Commit 1 — record format and panel preparation (BUILT 2026-10-01, uncommitted).* Record
version 2 (`attribution`, `state`, `loggedQsoUuid`, `attempt` — the exact attempted
submission, shape only until commit 4); `readSavedDraft` reads version 1 as version 2 with
attribution MISSING and the outcome kept, and refuses unknown or damaged records
(`drafts/savedDraft.ts`). `fetchSubmitAttribution` reads `GET /v1/submit-attribution`
strictly — a missing or non-string field is a failed read (`api/submit-attribution.ts`).
`main.ts` reads it inside the boot identity bracket and again after a Station save unless a
switch is in flight; the preserver records it, `null` when the read failed. The details and
the failed-save gate show the attribution — "(none)" for known-empty, "Original attribution
unavailable" when missing — and a `logged` record reads "Logged as QSO <uuid> — this
browser's saved copy could not be removed." `drafts/restore.ts` holds the RS3–RS7 decision
as a pure function, wired to nothing. Tests D5–D10, P9, the attribution read ×3, E1–E8;
seven reversion proofs fail their intended assertions. Not covered by a unit test: the
`main.ts` wiring (the bracket read and the Station-save refresh), as for the rest of that
file. The operator's SPA has no roster or default-operator editor, so a mid-session change
to those comes only from outside this tab; the server's own check on a recovered submit
covers it.
Operator review of commit 1 (2026-10-01; commits HELD, to resolve 2026-10-02 after a
scheduled power cut). Two issues. (1) Stale attribution survives a Station change: the
previous attribution stays in use while the refresh is pending, or when the guard skips
it — reproduced a record preserved with operator B but attribution for A. Fix: invalidate
the attribution immediately on a Station save; a preservation records MISSING attribution
until a valid refresh completes. The recovered-submit expectation would not catch this:
the wrongly saved operator/name sent explicitly can satisfy the same wrong expectation.
(2) Refreshes can apply out of order or after the archive guard closes: the Station-save
callback discards the refresh promise, bypassing the save latch — reproduced an older B
response overwriting C, and a response applying after the archive became unresolved. Fix:
await the refresh through the latch; drop a response whose request is no longer the latest
or whose archive binding is no longer valid. Plan: extract the coordinator from `main.ts`
into a tested module and add regression tests for the three sequences (stale-in-flight
save, B-after-C, applied-after-unresolved).
FIXED 2026-10-02 (uncommitted). The attribution moved out of `main.ts` into
`drafts/attributionSource.ts`: every config write invalidates it AT ONCE — reported by
`safeFetch` (`setConfigWriteListener`), the one path every request takes, so a `my_rig`
override changed in Settings → Rigs counts as well as a Station save; a read applies only if
it is the newest, no config write started since it began or is still in flight, and the
archive binding is still valid (`archiveSwitchGate() === null`); a dropped or failed read
leaves it missing and marks a retry, taken after the next proven reconnect. The boot read is
made inside the identity bracket and applied only after the bracket proved the binding and
nothing started since. The Station save holds its latch on the re-read
(`attributionSettled`). Tests AS1–AS7 (the three reproduced sequences: AS1 save during a
pending refresh, AS2 B after C, AS3 after unresolved) and the four `safeFetch` cases; nine
reversion proofs fail their intended assertions (two tests were strengthened after their
first proofs: one timed out instead of failing, one passed under the reversion). `main.ts`
baseline re-keyed to line@577.
COMMITTED `21feafae` (code) + `0933ca82` (docs), 2026-10-02. Codex review of `21feafae`: two
P2s, both real, OPEN pending the operator's choice of mechanism (both need a server change).
(1) Another tab or another browser can change config (e.g. the pinned rig's `my_rig`
override) without passing through this tab's `safeFetch`; the cached attribution then stays
stale indefinitely — a same-archive reconnect only retries a failed read. (2) The endpoint
computes attribution for the daemon's `logging_station.operator`, but a Phone / CW submit
sends this tab's `ctx.operator`; a Station save whose response timed out (write landed, the
reconciliation read failed, `onSaved` not called) leaves the cache at B while `ctx.operator`
is still A, and a save records operator A beside attribution B. Facts: no config revision
and no config-changed event exist (`internal/config` `Service.Update`; `/v1/events` carries
`qso.*`/`forward.*`). Proposed: (1) the daemon publishes `config.updated` on `/v1/events`
after every successful config write from any client; the SPA invalidates on it and re-reads,
and also re-reads after EVERY proven same-archive reconnect (an event missed while
disconnected), not only a pending retry — a same-browser BroadcastChannel alone would miss
other browsers. (2) `GET /v1/submit-attribution?operator=<the operator the tab sends>`
returns the attribution for exactly that operator; the cache records which operator it was
read for, and a save records the attribution only when it was read for the current
`ctx.operator`, else MISSING.
RULED 2026-10-02: build both; contract recorded in ADR 0085 § Attribution freshness contract.
BUILT 2026-10-02 (uncommitted, two commits). Go: `config.Service.SetOnChanged` — called once,
outside the lock, after a write made a new config live (`Update` durable or uncertain,
`UpdateIfChanged` when changed or forced, `UpdateInMemoryThenPersist` even when its disk write
fails), never for a rejected write; `api.New` wires it to publish `config.updated` (empty
payload) on the hub. `GET /v1/submit-attribution` takes `?operator=` (present-empty → the
default-operator fallback; absent → `logging_station.operator`) and parses its query strictly.
Tests L1–L5 (config), W7–W10 (api); eight reversion proofs fail their intended assertions.
SPA: `attributionSource.ts` files each read under the operator it was REQUESTED for and returns
it only for that operator; `setRequestedOperator` (called by `applyStationIdentity`, so the
startup context and every Station save) invalidates and re-reads; `noteConfigUpdated`,
`noteDisconnected` and the page's own config writes clear it and drop reads in flight; every
proven reconnect re-reads (`noteReconnectProven`). The boot read is made inside the identity
bracket for `ctx.operator`, after the context named it. `log-events.ts` dispatches
`config.updated`; `fetchSubmitAttribution(operator)` always sends `operator=`. Tests AS1–AS12,
the events dispatch, the requested-operator URL, and `main.attributionboot.test.ts` (M1 boot
read for the sent operator, M2 config.updated re-reads, M3 a proven reconnect re-reads). Eleven
frontend proofs: ten fail their intended assertions; one (config.updated without the epoch step
in `invalidate`) passed — an equivalent mutation, since `noteConfigUpdated` always calls
`refreshAttribution`, which advances the epoch itself and so drops the in-flight read anyway.
In all: **18 successful reversion proofs, one equivalent mutation (F3)**. `main.ts` baseline
re-keyed to line@581. Restore stays unavailable.
COMMITTED 2026-10-02: `4a27711e` (Go), `3668c3d1` (SPA), `386c7981` (docs). Codex: `3668c3d1`
clean; `4a27711e` one P2, FIXED (uncommitted) — `?operator=` was resolved verbatim while the
ADIF parser right-trims a submitted OPERATOR, so a trailing space made the endpoint's own
answer fail the submit's expectation (409). The parser's normalization is now one exported
helper, `adif.NormalizeValue`, used by the parser and by the endpoint for the requested and the
default operator alike. Test W11 (a "G0XYZ \t" request reports G0XYZ, and a submit carrying
that padded OPERATOR with the reported expectation stores); its reversion proof fails its
intended assertion.

**Restore commit 2 — claim and recovered form (COMMITTED 2026-10-02: `5ebb09ca`).**
`drafts/draftLock.ts` reserves the saved-record UUID with an exclusive, non-queued Web Lock;
its callback stays pending for the tab's ownership. `restoreSession.ts` rechecks entry,
destination and MY_RIG after acquisition and after re-reading storage, and refuses a removed,
damaged or changed record. A pending claim cannot overwrite newly typed work or admit a
second Restore in the same tab. Discard takes the same lock and holds it through the delete;
an acquisition error refuses. Browsers without Web Locks retain existing Discard behavior,
since they cannot Restore.

The recovered context lives outside component lifetimes. The card shows the source archive,
frequency, band, mode/submode and saved-reading basis, with correction and explicit
confirmation; missing/inconsistent values cannot be confirmed, and a correction withdraws
confirmation. Corrections do not command the rig. Saved fields (including blank end times)
survive clock entry, live report refills and enrichment, including a late response and
retraction of an identical value written by an earlier lookup. Report validation follows the
recovered mode. Stack keys, the stack button and pile-up Load cannot replace recovered work.

Build boundary retained: no public Restore action or production caller of `restoreSavedDraft`.
Ordinary Log (including force), Clear and reset cannot consume a recovered form. Commit 3
supplies edit persistence and protected Clear/Discard; commit 4 supplies recovered submission
and public entry. The existing ordinary-form tests remain green.

Evidence: L1–L2, C1–C9 and R1–R10 (41 cases), with 196 focused tests passing. Eighteen reversion
proofs each verified the mutation applied, reached an intended failing assertion, and restored
the implementation: lock options/lifetime, foreign Discard, form/destination/record rechecks,
clock/end stamping, RST refill, report validation, confirmation invalidation/validation,
stack keys/Load, enrichment fill/retraction, ordinary-submit exclusion and correction display
after remount. `SKIP_NPM_CI=1 task ci:local` PASSED: 2,209 SPA tests, lint/format/Svelte checks,
SPA/manual builds, Go vet/lint, maintainability (zero regressions), race/full tests, static and
CGO builds, FT8 decode tests and the build-boundary checks. The first run hit the sandbox's
read-only Go build cache; the passing run had the required cache access. No dependency changed.

**RS8 and RS11 remain OPEN for the operator's two-window drill.** The deterministic lock fake
and component unmount/remount tests establish application decisions only; they do not prove
browser scheduling, closure or reload. No hardware or RF experiment was run.

Review of commit 2 (2026-10-02, before commit): no defect in this commit — every new guard is
inert until a recovered record exists, and nothing in production installs one. Carried into
commits 3–4, which must close them: (a) a rebind reload while a recovered form is on the card
would run the ordinary preserver and save it as a NEW record with today's attribution and rig
reading — preservation must instead flush the final edit to the record already owned (same id,
original archive, logbook, attribution and rig) and save nothing new; (b) Discard of the record
this tab owns currently asks for the same lock and is refused as "In use in another tab" —
ruling 3 (owning-tab Discard) belongs to commit 3; (c) Clear / Escape on a recovered form is a
silent no-op until commit 3 gives it the save-then-release path; (d) the recovered card shows
the reading's capture time as raw ISO — format it as the saved-QSO details do.

The operator reran the drafts and operate suites: 775 tests passed; the context check passed.
Post-commit Codex review of `5ebb09ca` (2026-10-02): no actionable findings.

**Restore commit 3 — edits, Clear, Discard (BUILT 2026-10-02, uncommitted).**
`drafts/recoveredSave.svelte.ts` watches the recovered form and saves every edit to the record
it came from (same id; its original archive, logbook, attribution and rig reading kept): one
write in flight, newer edits coalesced into the next, no timer; a failed write is shown on the
card and retried by the next edit; installing the record writes nothing. Clear / Escape
(`requestClearDraft`, the card's two entry points) wait until every edit — one made during
that final save included — is stored, then detach the QSO and empty the form synchronously and
release the reservation (`restoreSession.finishRecovered`); a failed final save keeps the form
and the lock and says so; refused while a submission is in flight. Discard of the record this
tab owns (operator ruling 3; `discardSavedDraft` delegates to `discardRecovered`): no new
writes, the one in flight settled, the delete awaited, then the form emptied and the lock
released; a failed delete keeps everything; refused while a submission is in flight. A rebind
save with a recovered QSO on the form flushes to the owned record and creates no new one; if
the flush fails, the save fails and the reload is held. The recovered card shows its save
error, a "saving before clearing" note, and the capture time as UTC. Review notes (a)–(d)
above are closed. Corrected rig values: see the ruling below (first built without them).

Evidence: RS16a–d, RS17a–d, requestClearDraft, DA1, DA1b, DA2, DA3, PR1, PR2 and the card's
Escape / failed Clear / UTC time (18 cases). Sixteen reversion proofs plus two redone after
strengthening: RS16d now counts writes (it first checked only stored content, which an
identical rewrite leaves unchanged) and DA1b was added (an edit typed while the delete is in
flight; DA1 alone did not reach it). One mutation survived as EQUIVALENT: dropping the
revision check from the flush loop, since every edit starts a write at once and keeps `loop`
non-null, so the flush cannot return before it lands. In all: **16 successful reversion
proofs, one equivalent mutation**. Gates: lint, format, svelte-check, 2,227 SPA tests,
maintainability (0 regressions). Restore stays unavailable until commit 4.

Pre-commit review (2026-10-02) found and fixed two additional boundaries. Restoring the same
UUID after another owner edited it triggered an unnecessary installation write: the edit
baseline now belongs to each installed record, not its UUID. An edit typed while Discard's
delete was pending remained unsaved after that delete failed: the failure now resumes queued
writes without requiring another keystroke. RS16e and DA2b failed on their intended assertions
before the fixes and under verified reversions afterward. The earlier R6 guard test no longer
expects Clear / Escape to do nothing; commit 3's card tests own their save-then-release behavior.
Drafts and operate suites: 795 tests passed. This adds two cases and two successful reversion
proofs to the evidence above (20 new cases, 18 proofs, one equivalent mutation).
The subsequent `SKIP_NPM_CI=1 task ci:local` passed: 2,229 SPA tests, lint/format/Svelte checks,
SPA/manual builds, Go vet/lint, maintainability (zero regressions), race/full tests, static and
CGO builds, FT8 decode tests and build-boundary checks. The context check passed after the
documentation update. RS8/RS11 remain open for the operator's two-window drill.

RULED 2026-10-02 (operator): correction persistence belongs in commit 3. BUILT: record version
3 adds `rigCorrection` (null when nothing is corrected), kept apart from `rig`, the original
reading; versions 1 and 2 read as version 3 with no correction. The edit-saving loop treats a
correction (frequency, band, ADIF mode, submode) as an edit and stores it; correcting back to
the original stores null. Restore loads the saved correction as the working values and starts
UNCONFIRMED — the confirmation is never stored. The card shows "Corrected from the original
reading: …"; the saved-QSO details add a "Corrected rig values" row while keeping the original
reading's rows. Tests D11–D13, CP1–CP3 and the card's original-reading line (7 cases); D5's
"unknown version" example moved from 3 to 4. Eight reversion proofs, each failing its intended
assertion: correction not written, correction not counted as an edit, back-to-original stored,
Restore ignoring the saved correction, version 2 not read, correction unvalidated, correction
not shown in the details, original line not shown on the card. Gates: lint, format,
svelte-check, 2,236 SPA tests, maintainability (0 regressions). Running total for commit 3:
27 new cases, 26 successful reversion proofs, one equivalent mutation.
Review (2026-10-03, commit held): P2 — the owning tab's Saved QSOs list stayed stale after its
own recovered edits (the change notice reaches only other tabs), so Details / Copy showed old
values and a later Restore from that entry was refused as changed. FIXED: after each
committed write the save loop awaits a re-read of this tab's list
(`setRecoveredWrittenListener`, wired by `savedDrafts.svelte.ts`, so no import cycle); a
flush resolves only once the list is current. Tests SL1 (stored edit and correction reach the
list), SL2 (the listed record then restores) and SL3 (a flush waits for the re-read); three
reversion proofs fail their intended assertions (no refresh, refresh not awaited, listener not
wired) — the await proof first passed and SL3 was added to reach it. P3 — the capsule's
"decide when to persist rig corrections" was stale; `docs/current.md` updated. Running total
for commit 3: 30 new cases, 29 successful reversion proofs, one equivalent mutation.
Review (2026-10-03, commit held again): P2 — awaiting the local re-read did not mean the list
was current: the loader resolved when a newer read superseded it (Clear completed while the
newer read was pending) and when its read failed. FIXED: `loadSavedDrafts` now reports
`applied` / `superseded` / `failed`; `refreshSavedDraftsNow` follows the newest read when its
own is superseded and is false on failure. The save loop keeps a committed write separate from
list freshness: a failed refresh records `listError` ("Saved, but the Saved QSOs list could not
be refreshed…"), never an unsaved edit; `flushRecoveredEdits` (Clear) retries the refresh
without another edit and keeps the QSO if it still fails; `flushRecoveredWrites` (the rebind
save) needs only the committed write, so a stale list never holds a reload. Tests SL4
(superseded), SL5 (failed twice: QSO kept, list message, retry on the next Clear), SL6
(rebind unaffected). Six reversion proofs fail their intended assertions; the rebind proof was
first a non-compiling swap (ReferenceError) and was redone as a compiling mutation. Running
total for commit 3: 33 new cases, 35 successful reversion proofs, one equivalent mutation.
Review (2026-10-03, third hold): P2 — the rebind save still waited on a PENDING list read: the
write loop awaited its refresh, and the writes-only flush awaited the loop (SL6 used a read that
rejects at once). FIXED: a committed write starts the list refresh and does not await it; only
the newest refresh decides the list's state (a token), and Clear's flush waits for that refresh
separately — retrying a stale list once per Clear, then keeping the QSO. P3 — "Saved, but the
list…" survived a later unsaved edit: it is now cleared whenever a newer edit is noted (the
refresh after that edit's write sets it again). Tests SL7 (the rebind save resolves while the
list read is still pending) and SL8 (a later failed write leaves no "Saved, but…"). Proofs: the
write awaiting its refresh fails SL7; keeping the message beside a new edit fails SL8; Clear
skipping the pending refresh fails SL3. Two mutations EQUIVALENT: clearing the message on the
write-failure path as well (a failed write always follows an edit, which already cleared it —
that line was removed), and letting an older refresh decide (a superseded refresh follows the
newest read, so it reaches the same result). Running total for commit 3: 35 new cases, 38
successful reversion proofs, three equivalent mutations.
Review (2026-10-03, fourth hold): two overlap cases. P2 — Clear awaited the refresh promise it
captured, so after a newer edit committed and its read updated the list, Clear stayed blocked
until the obsolete read finished. FIXED: Clear waits on a "newest refresh finished" signal
(`listSettled`), woken by whichever refresh is newest when it completes. P3 — an older refresh,
still the newest because a failed write starts none, could set "Saved, but…" back beside the
newer unsaved edit when it failed late. FIXED: its completion shows the message only when every
edit is stored (`committed >= revision`). Tests SL9 and SL10 (the reviewer's reproductions);
two reversion proofs fail their intended assertions (Clear waiting on the captured promise;
the message ignoring newer edits). Running total for commit 3: 37 new cases, 40 successful
reversion proofs, three equivalent mutations.
Review (2026-10-03, fifth hold): the same P2 on two more paths. Clear's retry awaited the retry
promise directly, and `refreshSavedDraftsNow` waited for its own read before noticing a newer
one — so a newer edit's refresh, or a visibility / other-tab read, could update the list while
Clear stayed blocked on the obsolete read. FIXED: the loader announces every completed read to
listeners, and the barrier resolves true as soon as ANY read begun at or after it sets the
list (false only when the newest such read fails), never waiting on its own; Clear's retry is
started and then waits on the newest-refresh signal. Tests SL11 (a pending retry read) and SL12
(a read made for another reason) — SL12's "released" check moved before the old read is
answered, so a reversion fails that assertion instead of hanging. The barrier proof (waiting
on its own read only) fails SL12. The retry proof (awaiting the retry directly) PASSED and is
EQUIVALENT: with the new barrier, the awaited retry itself resolves as soon as the newer read
lands. Running total for commit 3: 39 new cases, 41 successful reversion proofs, four
equivalent mutations.

**Restore commit 4 — recovered Log and public Restore (BUILT 2026-10-03; committed `c551c828` 2026-10-04 after two pre-commit review rounds).**
Operator approval 2026-10-03 with boundaries: persist the exact request before the POST;
after confirmed storage the UUID is terminal in this tab and cleanup never resubmits;
ambiguous outcomes are distinct from definite refusals and a later refusal never erases
earlier uncertainty; gates rechecked after every wait; CAT bypassed only for recovered
submissions; Restore wired last, and "Go to Phone / CW" only navigates.

`drafts/recoveredRequest.ts` builds the request from the record alone: saved fields and times
(a blank end time stays blank), the recovered — possibly corrected — frequency, band, mode and
submode, the record's station callsign and MY_GRIDSQUARE, and the saved OPERATOR / MY_NAME
(a known-empty value is omitted; the daemon then applies today's default, and the `expect_*`
check refuses the submit if that default no longer matches). `submitQso` sends
`expect_my_rig` / `expect_operator` / `expect_my_name` (all three, empty included).
The stored attempt (record v3) now carries the whole request: archive id, logbook UUID and id,
force flag, ADIF and expected attribution; a record missing any of them is unreadable.

`drafts/recoveredSubmit.svelte.ts` (`logRecovered`): gates — attribution present, archive
switch gate, the record's archive and logbook UUID equal to the page's, this tab's reservation,
confirmed rig values, callsign / date / time and field validation, the in-flight latch — and
NOT the CAT link. It flushes outstanding edits, rechecks, stores the attempt with outcome
"unknown" (`persistRecordChange`; memory rolls back if the write fails, and nothing is sent),
rechecks again (a gate closing meanwhile withdraws the attempt and sends nothing), then sends.
Confirmed: the UUID is held as terminal; edits stop; the record is deleted, else rewritten as
`logged` with the UUID; if both fail the card shows the UUID with "Retry cleanup", Log is
disabled and the lock kept (a reload falls back to the persisted unknown state, RS22).
Ambiguous (transport failure, malformed success, abort): the attempt stays, outcome unknown,
no retry. Definite (validation, `attribution_changed` shown verbatim — the daemon's message
names current and expected values — and duplicate): the state before this request returns, so
an earlier unknown outcome and its attempt survive. Duplicate: the record and form are kept,
"Show contacts with <call>" opens the worked-before panel, and "Log anyway — this is a
separate contact" resends with force; Ctrl+Enter never forces. An unknown-outcome record asks
"I checked the original Logbook; this contact is not already logged." first (RS24). The
Log's messages and confirmed UUID belong to the installed record they arose on: a QSO
restored later — even the same saved record — starts clean and is not blocked.

The card's Log button and Ctrl+Enter take this path whenever a recovered QSO is on the form;
the button's disabled state and title come from `recoveredLogBlock`. `main.ts` wires the
sender (refreshing the header count on success), the environment (`archiveSwitchGate`, the new
`bootArchiveId()`, the active logbook) and the enrichment fields — factored with the ordinary
Log into `enrichmentExtras(call, myGrid)`, whose bearing uses the RECORD's grid. That cut the
ordinary submit's complexity from 39 to 23; its baseline key moved `line@581` → `line@589`.

Public entry, wired last: each Saved QSOs entry shows Restore when eligible on Phone / CW,
"Go to Phone / CW" elsewhere (navigation only), the reason when unavailable or MY_RIG
differs, and nothing when logged or when the page has no Restore wired; a refusal at the
moment of pressing is shown in that entry with the panel and record kept. `restoreSession`
holds the page environment (`setRestoreEnv`, `restoreFromPanel`). The boot identity and the
attribution cache became reactive (`attributionSource.ts` → `attributionSource.svelte.ts`)
so an entry follows them as they are read, instead of showing a stale "could not be read".

Evidence: 40 new cases — LG1–LG15 (19), RQ1–RQ4, LC1–LC6 (7), C11–C17 (7), the expect_*
request (2) and `bootArchiveId` (1); D10 widened; C4 now promises no archive switch rather
than no restore. **36 successful reversion proofs** each failing its intended assertion:
CAT gating the button, Ctrl+Enter routing, attempt not stored first, failed store still
sending, memory not rolled back, no recheck after the store, refusal erasing an earlier
unknown, malformed/aborted treated as definite, confirmed UUID not blocking, writes not
stopped before cleanup, fallback not `logged`, RS24 skipped, attribution gate, extras grid,
Log anyway unforced, inspection inert, title, cleanup retry, outcome message, Restore not
closing the panel, Go to Phone / CW restoring, unavailable / logged / unwired entries offering
Restore, refusal not shown, `bootArchiveId`, both reactivity reverts, the per-record message
view and its reset, the empty `expect_my_name`, attempt `force` / `logbookUuid` validation,
blank end time, recovered frequency, enrichment extras. Two of them (confirmed-UUID gate,
attribution gate) fail at the intended `expect` as a TypeError (`toMatch` given null).
**One equivalent mutation:** dropping the empty-value guard on MY_NAME in the request, since
`formatAdifRecord` already omits empty fields. Gates: lint, format, svelte-check (0/0), 2,288
SPA tests in 164 files, maintainability (0 regressions), context check. No Go change, no
dependency change. Not unit-tested: the `main.ts` wiring itself, as for the rest of that file.

Pre-commit review (2026-10-04, pasted), both fixed before commit:

- **P1, "Retry cleanup" could erase the next contact.** Overlapping retries each let go of the
  form, so a later one emptied a contact begun after the first. Fix:
  - Cleanups run one at a time, each for the QSO it was asked for.
  - A cleanup lets go of the form only while this tab still holds that QSO.
  - While any cleanup runs, the Log's latch (`submitState.busy`) stays held, so Clear and
    Discard stand down, as they do for the request.
  - Tracing the fix turned up the same race in the fallback: a Discard during the "logged"
    write would have written the discarded record back. The latch closes that too.
- **P2, the submitting tab's Saved QSOs list went stale after cleanup.** The change notice
  reaches only the other tabs. Fix: either cleanup outcome now refreshes this tab's list.

LC5 had restored storage while the first cleanup was still running, so it passed only because
of the race. It now waits for the shown failure first.

Evidence:
- **New cases (4):** LG16 (overlapping retries; Clear and Discard standing down) and LG17
  (the deleted and the "logged" outcome both reach this tab's list).
- **Six successful reversion proofs**, each failing its intended assertion: no serialization,
  no early let-go guard, no list refresh, no latch, latch never released, plus the
  original-race RED.
- **One equivalent mutation, recorded honestly:** the ownership check before letting go,
  which the latch already makes unreachable. It is kept as defence in depth.
- **Gates:** lint, format, svelte-check (0/0), 2,292 SPA tests, maintainability
  (0 regressions).

Follow-up review (2026-10-04, pasted), fixed before commit:

- **P1, the latch was released under a queued cleanup.** The Log's own `finally` released the
  latch unconditionally. So a Retry cleanup clicked while the Log's first cleanup was still
  running outlived the Log, and ran with Clear and Discard allowed again. A Discard during it
  let the retry's fallback write the discarded record back.
- **Fix:** the Log keeps the latch while any cleanup remains.
- **Evidence:**
  - **New case (1):** LG16, a retry queued before the Log settles.
  - **Reversion proof:** P47 (latch released unconditionally) fails at the intended
    assertion.
  - **Gates:** lint, format, svelte-check (0/0), 2,293 SPA tests, maintainability
    (0 regressions).

Codex review of `c551c828` (2026-10-04), two P2 findings:

- **Finding 1: edits made while a recovered Log is in flight are lost.** The ADIF is built
  before the attempt is stored and sent. A correction typed during either wait was saved to
  the record, but the older snapshot was logged, and cleanup then deleted the record.
  - **Operator ruling (2026-10-04): freeze the recovered form.** Rejected alternative:
    detect changes, refuse before the send, and keep a "logged" record carrying the edits.
  - **The freeze starts before the first asynchronous save.** It covers the fields, the
    rig corrections, the shortcuts and the automatic field fills.
  - **It holds through the submission and the cleanup.**
  - **After a failure, a refusal or an unknown outcome,** editing returns, and the recorded
    attempt and its uncertainty are kept.
  - **After confirmed storage with a failed cleanup,** the contact stays read-only:
    corrections belong on the logged QSO.
  - **Acceptance:** mutations are attempted during both the persistence and the POST. The
    ordinary Log is unchanged.
  - **Built (committed `899bda8c` 2026-10-04).** `recovered.frozen` is set before the first save. It is
    released afterwards unless the Log was confirmed and the cleanup failed; a later
    successful cleanup releases it when the form is let go.
    - **Card:** every bound field, the rig-correction inputs and the comment field (with its
      recent-comment picker) are read-only while frozen.
    - **Model:** `correctRecoveredRig` refuses while frozen.
    - **Already standing down:** the shortcuts and automatic fills (stamps, RST, enrichment,
      pile-up, callsign stack) already stood down for a recovered QSO. Clear and Discard are
      held off by the latch.
  - **Evidence:**
    - **New cases (6):** LG18 (4), LC7 (fields, rig inputs and shortcuts during both the store
      and the request; editable after a refusal) and a CommentField read-only case.
    - **Ten successful reversion proofs (P48–P57),** each failing its intended assertion: never
      frozen; frozen only after a wait; never or always given back; corrections not refused;
      let-go keeping the freeze; comment field, picker, card fields or rig inputs editable.
    - **Removed rather than kept untested:** two unreachable guards (confirmation while frozen,
      and the reset on install).
    - **Gates:** lint, format, svelte-check (0/0), 2,299 SPA tests, maintainability
      (0 regressions).
- **Finding 2: a recovered QSO is missing from the Session panel, export and email.**
  **Operator ruling (2026-10-04): add it when the Log returns a confirmed `stored`.** Its
  original contact time stays intact; it was logged during this session.
  - **Exactly one row,** using the returned UUID and the submitted snapshot: the fields, the
    corrected band, mode and submode, the original time on, and the enrichment actually sent.
  - **Independent of the browser cleanup:** the row is added whether the cleanup succeeds or
    fails. Cleanup retries never add another row.
  - **Refusals, duplicates and unknown outcomes add nothing.** An explicitly forced
    submission adds a row only when it is confirmed stored.
  - **Export and email include it normally,** through the existing UUID-based flows.
  - **Tests:** today's rig and enrichment are made deliberately different. Cover cleanup
    failure and retry, and every non-stored outcome, and assert that the UUID reaches the
    export and email selection.
  - **Built (committed `899bda8c` 2026-10-04).** `settle` adds the row on `stored`, before the
    cleanup, from the snapshot the request was built from:
    - `fields`, the corrected rig (mode shown by `sessionModeLiteral`) and the extras carried.
    - It is never added on a cleanup retry, and never from today's rig or lookup.
    - The export and email flows pick it up unchanged.
  - **Evidence:**
    - **New cases (4, LG19):** the row's values with today's rig (15m CW) and lookup (Spain)
      deliberately different; a failed cleanup and both retries leave exactly one row;
      validation, duplicate, network, server, abort and a forced duplicate add none, while a
      forced stored Log adds one; the UUID reaches both `/v1/session/export` and
      `/v1/session/email`.
    - **Four successful reversion proofs (P58–P61):** no row; a row for every answer; mode
      from the captured literal; country looked up again.
    - **One equivalent mutation (P62):** fields read from the live form, which the freeze
      keeps equal to the snapshot.
    - **Gates:** lint, format, svelte-check (0/0), 2,303 SPA tests, maintainability
      (0 regressions).

**RS8 and RS11 remain OPEN for the operator's two-window drill**, now against the public entry.

**Two-window drill script (defined 2026-10-04, after the deploy of `a1ed4f89`).**
- **Setup:**
  - Two separate, visible windows of the same browser profile at the same origin. Not two
    tabs: background tabs slow timers to about once a second.
  - The record comes from the app's own path. Window A holds a Phone / CW draft. Window B,
    with an empty form, switches the archive: A detects the new daemon instance, saves its
    draft and reloads. B then switches back.
  - **Corrected 2026-10-04 at S2.3.** The first version had A switch the archive itself;
    that is correctly refused over unlogged work (`refuseOverUnloggedWork`).
- **D1 race (RS8):** both windows click Restore at a shared whole-minute boundary, scheduled
  from the console. Expect exactly one winner. The loser's form stays empty and it shows "In
  use in another tab." `navigator.locks.query()` shows one held
  `station-manager.saved-qso.<id>`. Run it three times, releasing with Escape between runs.
- **D2 foreign Discard (RS10):** refused in the loser; the record is kept.
- **D3 claim retained (RS10):** across a panel close and in-app navigation in the winner.
- **D4 release on close (RS11):** the other window restores the record with its latest
  committed edit.
- **D5 release on reload (RS11):** the same, with a reload in place of the close.
- **Cleanup:** Discard the drill record. Never Log it.
- **Direction under discussion (2026-10-04, operator, during drill setup; NO decision).**
  - **Proposal:**
    - Allow an archive change (perhaps also a logbook change) only from Settings → Archives,
      never while operating on Phone / CW or FT8 / FT4.
    - Remove the saved-QSO machinery (ADR 0085 / 0086, Restore commits 1–4).
    - Instead, warn any other browser that holds an unlogged QSO, like the restart alarm.
  - **Facts checked:**
    - The switch has two entry points today: the header selector (`Header.svelte`) and
      Settings → Archives (`ArchivesSection`).
    - The initiating page is already refused over unlogged work.
    - A same-archive daemon restart does NOT reload a page holding a draft; it only warns
      (`Settings.svelte` `restarted()`). Only an archive CHANGE forces the rebind reload
      (`archives.svelte.ts` `verifyAfterRigReconnect` → `requestReload`).
  - **Claude's analysis:**
    - Restricting the entry point does not protect OTHER browsers: their reload is driven by
      the daemon changing archive.
    - The replacement for save-then-reload would be hold-and-warn: the page stays bound to
      its boot archive, logging is gated, and the draft stays on screen. A switch back
      lifts the gate; "Discard and reload" is the only other way out.
    - Cost: the draft is memory-only again, so closing the tab or a browser crash while held
      loses it. That durability is what ADR 0085 bought with IndexedDB, at the cost of the
      Restore machinery and the expect_* API.
    - The RS8/RS11 drill is moot if Restore goes.
  - **Needs an ADR superseding 0085 / 0086 before any code.** Drill paused at S2.3 pending the
    ruling.
  - **Operator's simpler alternative for the second window (2026-10-04):** no cross-window
    machinery at all. Plain wording at the switch says what happens to an unlogged QSO open in
    another browser window.
    - **Claude:** the outcome is not indeterminate. The other window reloads and its in-memory
      draft is lost, so the wording can say exactly that.
    - **Gap against the 2026-09-13 ruling ("never silently clear a draft"):** the other
      window's operator would see the QSO vanish with no notice in that window.
    - **Cheapest closure:** before the reload, that window shows a blocking notice listing the
      unlogged QSO's values, which must be acknowledged. Memory-only; no storage, locks or
      Restore.
- **Third-party review of the options (2026-10-04).** Review brief:
  https://claude.ai/artifact/WpeG7hzijqhRnBH86fXNNp. The response was pasted by the operator.
  - **Recommendation:** A (keep the design; fix wording and discoverability) for the next
    release. Re-run the discovery step with the operator before deciding whether the mechanism
    goes. A corrected D is the simplest defensible replacement, but only if the operator
    explicitly accepts losing recovery on tab close or crash. Lines and tests already written
    should carry little weight.
  - **Correction to D:** an acknowledgement before reload means holding the old page.
    Displayed values are not a lasting record. Describe D as "hold for manual copying, then
    explicitly discard and reload": keep every value and its source archive and logbook
    visible, block stale submits, offer Copy, and label the action "Discard draft and
    reload", not "could not be kept".
  - **Correction to B:** a switch back does not prove that rig values, attribution or the
    logbook binding still apply, because the ordinary Log reads the live context
    (`main.ts:589`). B needs a context revalidation rule, which brings back recovery
    complexity.
  - **Every replacement needs an unknown-outcome case:** a POST in flight, or a lost
    response, during the switch. C and D cannot always say "not logged".
  - **Answers to the open questions:**
    - **Durability:** memory-only is acceptable only if deliberately accepted.
    - **Warning the switching window:** keep the local refusal and add a warning about other
      windows. No daemon-side draft tracking.
    - **Default logbook:** it is not selectable today. **Verified:** `logbooks.svelte.ts`
      says default selection is out of scope, and only setup sets the pointer
      (`handler_config.go:908`). A future default selector must define where existing drafts
      go.
    - **Existing records:** never strand or auto-delete them. A retirement keeps a
      read/copy/export/discard surface with its identities and outcome wording.
    - **`expect_*`:** keep it through any transition, and never silently ignore it for old
      pages. It checks attribution, not the archive. **Verified:** `submit.go:615`.
    - **Process:** choose the loss policy, then a superseding ADR, then the removal on its
      own. Acceptance covers unlogged, in-flight/unknown, repeated switches, existing
      records and old open pages.
  - **Concrete A:** lead with "Not logged"; rename the control "Unlogged QSOs"; give the
    message a direct action that opens it; keep distinct unknown-outcome wording.
  - **Operator ruling (2026-10-04): option A for now.** The revisit is parked in the backlog
    ("Revisit saved-QSO recovery"), so it is not lost.
  - **Built (committed `c2600910` 2026-10-04):**
    - **Wording leads with the outcome:**
      - Kept, not logged: "Not logged — QSO from ‘X’, kept in this browser."
      - Kept, outcome unknown: "Logging outcome unknown — QSO from ‘X’, kept in this browser.
        Check the Logbook in ‘X’ before logging it."
      - Unsaved: "Not logged, and not saved — …" or "Logging outcome unknown, and not saved — …".
    - **The control is "Unlogged QSOs (n)",** and the panel is named the same.
    - **The rebind announcement** ends "It is under Unlogged QSOs." and carries an "Open
      Unlogged QSOs" action. It has no automatic timeout; the existing five-toast limit can still
      evict it. Toasts gained an optional action, which dismisses the toast when used.
    - **The panel focuses its Close button however it opens.**
  - **Evidence:**
    - **Tests:** the wording and name assertions were updated across 7 test files. C9 now checks
      that the announcement has no timeout, opens the panel and moves focus there. Two renderer cases
      cover the action button.
    - **Eight successful reversion proofs (P63–P70).**
    - **Gates:** lint, format, svelte-check (0/0), 2,305 SPA tests, maintainability
      (0 regressions), context check.
  - **Code review (2026-10-04, pasted):** clear. Keep the no-timeout announcement; the bounded
    five-toast eviction stays. The cleanup-only "logged" exception is acceptable for this
    interim change. Two documentation corrections were made before commit:
    - **The embedded manual** (`manual/content/chapters/qso-archives.md`) now says Unlogged QSOs
      and describes the direct action. It also gains the Restore paragraph that was missing
      since `c551c828`.
    - **"No automatic timeout"** replaces "stays until dismissed" in comments and here.
  - **Note:** a "logged" record (both cleanup writes failed) also appears under "Unlogged QSOs",
    with its own "Logged as QSO …" headline.
  - **Next:** repeat the discovery step with the operator, then resume the drill.
- **Drill results (2026-10-04):**
  - **S2.3 PASS.** Window B switched the archive. Window A saved its draft and reloaded,
    with the toast "Unlogged QSO saved from ‘Drill Arc’ — not logged. It is under Saved
    QSOs."
  - **Inbox:** two notes logged during setup.
  - **Discovery check of option A PASS (2026-10-05, after the deploy of `152fb36b`).**
    - The existing 7Q7CT record showed under "Unlogged QSOs (1)" with the headline "Not
      logged — QSO from ‘Drill Arc’, kept in this browser."
    - A held a new `7Q7DT` draft; B switched to ‘Drill Arc’. A reloaded with the sticky
      "Not logged — …" toast and its **Open Unlogged QSOs** action. The action opened the
      panel with focus on Close, which listed 2 records. `7Q7DT` was then discarded.
  - **S2.4 PASS** (B's switch back to ‘Drill Arc’ was the discovery switch).
  - **S2.5 reported PASS, then corrected to FAIL at D1 (2026-10-05).** The operator's
    screenshot shows the 7Q7CT entry on Phone / CW with ‘Drill Arc’ / ‘Drill Lb’ active,
    offering only Show details and Discard, with the reason "Original attribution
    unavailable." The record was saved with `attribution: null`.
  - **Cause (traced in code; not yet proven by a test).**
    - Window A, which did not start the switch, learns of it only on reconnect.
    - The events-stream transport error calls `noteDisconnected()` first (`main.ts:755`),
      which clears the page's attribution (`attributionSource.svelte.ts` `invalidate`).
    - The rebind save then reads `currentAttribution(ctx.operator)` (`main.ts:519`) and
      gets null. The re-read after the reconnect is gated by `bindingValid()` and cannot
      apply on a daemon that serves another archive.
    - So every save made by a window that did not start the switch records attribution as
      missing. That window is the one RS8/RS11 exist for. The initiating window is refused
      over unlogged work, and a same-archive restart does not save. So in practice no
      app-made record is restorable. Unit fixtures always supply an attribution, which is
      why the tests did not show this.
    - This follows the 2026-10-02 contract in ADR 0085 ("a disconnect of the events stream
      clears it too"). Its interaction with the rebind save was not weighed then.
  - **Drill paused at D1** pending the operator's ruling on the contract.
  - **S2 re-run (2026-10-05, after the deploy of `dee99e79`; fresh record).**
    - **S2.3 toast:** the wording is correct, with the Open action. But an unlogged record
      raises an `info` toast, and `info` renders as a green check-circle (`Toasts.svelte`).
      A success mark on "Not logged" contradicts the text that option A fixed.
      - Possible fix: raise it at `warn` level (amber).
      - The level is the operator's call; the drill continues meanwhile.
    - **S2.3 side check FAIL on the deployed fix (`gdee99e79`, installed 10:27).** The
      fresh record still shows "Original attribution unavailable."
    - **Second cause (traced; not yet proven by a test).**
      - B's activation persists the pending archive selector with `m.cfg.Update`
        (`internal/archive/activate.go:200`). Every config write publishes `config.updated`
        (`internal/api/server.go:159`).
      - Window A receives that event BEFORE the restart. `noteConfigUpdated` → `invalidate`
        clears the attribution outright and starts a re-read.
      - The restart then drops the stream. `noteDisconnected` advances the epoch, so the
        re-read in flight is dropped (or it fails against the stopping daemon). The rebind
        save finds nothing.
      - `8d50dd01` fixed only the disconnect half. The attribution source is the only
        consumer of `config.updated` in the SPA.
    - **S2.4 PASS; S2.5 FAIL.** Both windows on ‘Drill Arc’ / ‘Drill Lb’ show the record
      (08:46:13 UTC) without Restore, with "Original attribution unavailable." This matches
      the second cause.
    - Drill paused at S2.5 pending the operator's ruling.
- **Redesign opened (2026-10-05, operator).** "I don't want to spend any more effort on this.
  Let's redesign." This un-parks the backlog revisit of ADR 0085 / 0086. The drill is abandoned,
  and the second-cause fix (options 1 and 2 above) is not pursued.
  - **Design point 1 (operator):** change the archive ONLY via Settings → Archives; navigating
    to Settings clears anything in the QSO logging space.
  - **Claude's view (discussion, no decision):**
    - **Single entry point: agree.** The header selector becomes display-only.
    - **Clear on navigation: disagree as stated.**
      - The loss is tied to a navigation, not to the switch. Settings is visited mid-operation
        for unrelated reasons (rig, station, forwarding), and a QSO half-entered on Phone / CW
        would silently vanish.
      - It also does not reach the case that drove ADR 0085: the draft in ANOTHER window.
        Clearing window B's form leaves window A's QSO exactly where it is today.
    - **Suggested form:** the gate sits on the Activate action, in the window that switches.
      - Today `refuseOverUnloggedWork` (`archives.svelte.ts:93`) refuses with a toast when this
        window holds a Phone / CW draft.
      - Instead, Activate shows the unlogged QSO and offers "Discard it and switch" or
        "Cancel". Navigation stays lossless.
    - **Point 2 then decides the other windows:** an FT8 / FT4 run and a Phone / CW draft held
      elsewhere. The backlog's "explicit loss policy" lives there.
    - **To confirm in point 1:** "the QSO logging space" means the Phone / CW form (the only
      unlogged state). Session rows are already logged and survive a reload.
  - **Point 1 RULED (operator, 2026-10-05):** "agreed, gate on Activate with
    discard-and-switch." Settings → Archives is the only switch; the header selector becomes
    display-only.
  - **Design point 2 (operator):** cover other windows with a clear manual statement, "something
    like: any data in any secondary open browser window, may be lost or corrupted".
  - **Claude's view (discussion, no decision):**
    - **Agree:** a stated loss policy instead of browser-held recovery.
    - **Say "lost", not "corrupted".**
      - The rebind reload and the switch gate stay. They are what stop a stale window logging
        into the wrong archive.
      - What can happen is loss of unlogged work. Claiming corruption would be inaccurate and
        alarming.
    - **"Any other window", not "secondary".** No window is primary.
    - **Name what is lost:**
      - An unlogged Phone / CW QSO.
      - An FT8 / FT4 exchange not yet completed. The switch seals transmit and restarts the
        daemon.
      - Unsaved Settings edits.
      - Logged QSOs are never affected.
    - **Optional:** one toast in the reloaded window, so the loss is not silent. For example:
      "The archive changed to ‘X’ in another window; this window reloaded. Any unlogged QSO here
      was discarded." It needs no storage; it would be raised only when a draft was actually
      discarded.
    - **Still to decide:**
      - What removal does with records already in browsers' IndexedDB (drop the store, or
        leave it orphaned).
      - Whether `GET /v1/submit-attribution` and the submit `expect_*` check go with the
        machinery.
      - A superseding ADR comes before any code.
  - **Rationale (operator, 2026-10-05):** "What we were working on 'looks' clever but is just a
    pain to debug and support and with little used feature - really it is protecting against
    something we only 'think' might be an issue." No observed loss motivated ADR 0085. Two drill
    failures in a row came from interactions between its own parts (the disconnect, then
    `config.updated`), not from operating. This goes in the superseding ADR's context.
  - **Rulings (operator, 2026-10-05)** on the three open points:
    1. **No toast.** The Activate warning and the manual statement carry it. No state crosses
       the reload solely for a notification.
    2. **Existing browser records are left orphaned.** All application access is removed, with
       no automatic deletion or migration. The ADR says so explicitly.
    3. **Remove the attribution endpoint and the recovery-only submit check.** Ordinary daemon
       attribution stamping stays. Old pages that submit `expect_*` get an explicit refusal
       telling them to reload; the expectations are never silently ignored.
    - **Reaffirmed:** the archive-binding gate and the forced rebind stay. Accepting the loss of
      an unfinished entry must not permit logging into the wrong archive.
  - **[ADR 0087](../decisions/0087-switch-archives-only-from-settings-and-state-the-loss.md)
    written (Accepted, 2026-10-05).** It supersedes ADR 0085 and ADR 0086.
    - The refusal is `409 reload_required` for any `expect_*` key.
    - Whether `config.updated` and `rigReadingForSave` stay is left to the removal change.
    - The removal is its own change, next.

### ADR 0087 removal plan (2026-10-05, for operator review)

**One releasable commit**, with tests and code together. The docs commit follows.

**Caller evidence**
- **`config.updated`.**
  - Daemon side, its only publisher is `internal/api/server.go:158`. It goes through
    `config.Service.SetOnChanged` / `announceIfLive` (`internal/config/config.go`). That is the
    hook's only caller.
  - SPA side, the only consumer is `log-events.ts:134` → `onConfigUpdated` → `main.ts:758` →
    `noteConfigUpdated` (the attribution source).
  - All of this was introduced by `4a27711e` for ADR 0085.
  - **Remove:** the event, the payload, the hook, and the SPA listener and handler.
- **`rigReadingForSave` / `rigSnapshot.svelte.ts`** (added by `ebe244dc` for ADR 0085).
  - `rigReadingForSave`'s only caller is `preserve.ts:60`.
  - The rest of the module serves only the held reading:
    - `noteRigDrop` (`main.ts:470`).
    - `rigDropEpoch` and `retireRigSnapshot` (`archives.svelte.ts:516/548/561/582`).
    - `rigSnapshotHeld`, the RST-refill guard (`qso.svelte.ts:211`).
  - **Remove the module.** The RST refill returns to its pre-ADR 0085 rule: refill on every
    default change.
  - **Keep `isRetainedDefault`** (`bbfcb608`). It still stops untouched default reports from
    counting as unlogged work.
- **Other code added only for recovery.** The commits are `ebe244dc`, `21feafae`, `899bda8c`
  and Restore commits 1–4.
  - **Remove:**
    - `setConfigWriteListener` (`_helpers.ts`) and `lib/api/submit-attribution.ts`.
    - The `recovered` hooks in `qso.svelte.ts`, `enrich.svelte.ts`, `CallsignStackPanel` and
      `LoggingCard`.
    - The `readonly` / `frozen` props on `LoggingCard`, `CommentField` and `RecoveredContext`.
    - `SavedQsosControl` in `Header` and `MapView`.
    - The held-save block in `ArchiveSwitchGate`.
    - The preserver and `saveFailed` state in `archives.svelte.ts`.
    - All of `lib/drafts/`.
  - **Keep** `logOutcomeUnknown` (`ebe244dc`), now for the Activate prompt (see the
    operator decision on in-flight logs below).
- **Daemon (Go).**
  - **Remove:**
    - `GET /v1/submit-attribution` (`server.go:279`, `handler_submit_attribution.go`).
    - `expectedAttribution`.
    - `SubmitExpecting`, `LiveAttribution` and `checkAttribution`, plus the `expect` parameter
      of `qsoservice.submit`.
    - `types.SubmitAttribution`.
  - **Keep:** `stampedMyRig` and `effectiveOperatorAndName`, the ordinary stamping, and the
    strict query parse in `handler_qso.go`, which also guards `force`.

**Behaviour, tests first. RED before code unless marked characterization (C).**
- **C1 (Go) — ordinary attribution stamping is unchanged.**
  - Port `TestLiveAttribution_MatchesWhatSubmitStores` to assert `Submit` alone:
    - MY_RIG is pinned to the startup rig, including after a runtime default change.
    - OPERATOR is the supplied value or the default.
    - MY_NAME comes from the roster.
  - Green before and after.
  - Kept alongside `pin_myrig_test` and `TestSubmit_DefaultsOperatorFromRoster`.
- **C2 (SPA) — navigation keeps the draft.** Fill the Phone / CW form, go Operate → Settings →
  Archives → Operate, and the draft is intact. Green now; it guards "opening Settings clears
  nothing".
- **C3 (SPA) — RST refill.** A mode change refills both reports. Untouched default reports are
  not unlogged work. Green before and after the module's removal.
- **A1 — Activate with an empty form.**
  - Confirm text adds the other-windows statement.
  - OK sends the request.
  - Cancel sends nothing.
- **A2 — Activate with an unlogged QSO.**
  - No refusal toast.
  - The confirm names the QSO (call, Time On) and says it will be discarded, and repeats the
    other-windows statement.
  - OK sends the request; Cancel sends nothing and the draft is untouched.
  - Reversion: restoring `refuseOverUnloggedWork` fails A2.
- **A3 — the draft is discarded by the reload, not before the request.**
  - **Definite refusal, or an uncoded answer that did not accept:** draft intact, no reload.
  - **Accepted, with the new instance proven:** reload and nothing saved. The preserver is gone,
    and no storage, lock or channel is touched.
  - **Unknown outcome, unproven:** gated, draft intact.
- **A4 — a log in flight or with an unknown outcome** (operator decision below).
- **R1 — cross-window rebind.**
  - Another archive on reconnect latches the gate BEFORE the reload, then reloads with no save
    step and no held-reload state.
  - The existing `verifyArchiveGeneration`, boot-bracket and gate tests are retained as
    characterization.
- **H1 — header.**
  - The archive is shown as text with no selector, and nothing in the header activates.
  - There is no Unlogged QSOs control in the header or on the Map.
- **Q1 (Go) — `POST /v1/qso` with any `expect_`-prefixed query key → `409 reload_required`.**
  - Cases: each of the three keys alone, all three, and an unknown `expect_x`.
  - Nothing is stored: QSO rows and upload-queue rows are unchanged.
  - A malformed query stays 400.
  - Reversion: ignoring the keys fails Q1.
- **Q2 (Go) — `GET /v1/submit-attribution` → 404** from the route fallback.
- **O1 — orphaned records are untouched.**
  - **Behavioural:** with spies on `indexedDB.open`, `navigator.locks.request` and
    `BroadcastChannel`, booting the real `main.ts` and running an accepted switch plus a
    foreign rebind calls none of them.
  - **Structural (allowlist empty):** no non-test source under `src/` names `indexedDB`,
    `navigator.locks`, `BroadcastChannel` or `saved-qso-drafts`.
  - Reversion: re-adding a `draftStore` open at boot fails O1.

**Tests deleted with the code they prove**
- **SPA, whole files:**
  - All 15 of `lib/drafts/*.test.ts`.
  - `main.attributionboot.test.ts`.
  - `lib/api/submit-attribution.test.ts`.
  - `lib/operate/rigSnapshot.svelte.test.ts`.
- **SPA, cases within files:**
  - `archives.svelte.test.ts`: the "rebind reloads preserve unlogged work" block (V1–V10) and
    U1/U2. U1/U2 are replaced by A2. U3/U4 are kept.
  - `ArchiveSwitchGate.svelte.test.ts`: G1–G6. The stop-control and gate cases are kept.
  - `Header.svelte.test.ts`: the selector cases (replaced by H1) and the Saved QSOs control.
  - `App.svelte.test.ts`: the Saved QSOs on the Map cases. The gate-on-Map cases are kept.
  - `log-events.test.ts`: the `config.updated` block.
  - `CommentField.svelte.test.ts`: the read-only case.
- **Go:**
  - `qsoservice/attribution_test.go`, after C1 is ported.
  - `api/handler_submit_attribution_test.go`. `TestSubmitQso_MalformedQueryIs400AndStoresNothing`
    moves to the handler_qso tests and is kept.
  - `config/config_live_hook_test.go`.

**Docs in the same change**
- **`api-endpoints.md`:** remove the `config.updated` notice, the endpoint and the `expect_*`
  text, and add `409 reload_required`.
- **Manual `qso-archives.md`:** the Settings-only switch, the Activate prompt and the loss
  statement. Remove Unlogged QSOs.
- **Baseline:** `quality/maintainability-baseline.json` if `main.ts` line keys move.

**Gates**
- SPA: lint, format, svelte-check, vitest.
- Go: `gofmt` (whole tree), `go vet ./...`, `go test ./...`, the maintainability check.
- Cloud tests with the database (`submit` signature change).
- `task ci:local`.

**Operator decisions needed**
1. **Prompt mechanism.** `window.confirm` keeps the existing Activate confirm, with OK meaning
   "discard it and switch" stated in the text. The alternative is a styled dialog with named
   buttons. Recommendation: `window.confirm` now; styled dialogs stay their own item.
2. **A log in flight.** Refuse Activate while a Log is in flight ("wait for the log to finish").
   An unknown outcome is allowed, but the prompt says to check the Logbook in this archive
   first. Recommendation: as stated.

**Operator review of the plan (2026-10-05).**
- **Decisions:** both recommendations accepted and recorded in ADR 0087 as items 8 and 9.
  - The Activate refusal for a Log in flight is checked before the confirmation AND again before
    the activation starts.
  - With an unknown outcome, the prompt says the QSO "may already be logged" in the original
    archive's Logbook.
  - The prompt names the destination and covers partial drafts.
- **Evidence:** the caller evidence is accepted. Both `config.updated` and the rig snapshot
  module are removed.
- **Corrections to the plan:**
  1. **C3 needs its own failing test:** a disconnect followed by a changed mode default now
     refills the reports immediately. An ordinary mode change alone cannot tell the removed
     behaviour apart.
  2. **V10 is not deleted wholesale.** Its unreadable-identity gate, foreign-archive reload and
     unproven-boot assertions are retained. Only the preservation and held-reading assertions go.
  3. **O1 also spies on `indexedDB.deleteDatabase`.**
  4. **`api-endpoints.md` and the manual ship in the implementation commit.** The dossier and
     handoff notes follow separately.

**ADR 0087 removal built (2026-10-05, uncommitted at the time of writing).**
- **RED on the old code, failing on the intended assertion:**
  - Q1 (`reload_required`, all six key cases) and Q2 (the route still answered 200).
  - C3b: a drop followed by a mode change left 59/59.
  - R1/O1: the failed save held the reload.
  - H1 header ×3.
  - A1–A4 and R2.
- **Characterization, passing before and after:**
  - C1 stamping.
  - C2 navigation keeps the draft.
  - C3a mode-change refill.
  - The malformed query is still 400.
  - U3/U4, the retained V10 cases (unreadable identity gates, another archive reloads,
    unproven boot does nothing), and the gate and stop-control tests.
- **Guards that passed before the change (not discriminating; recorded honestly):**
  - The Map page and the header showing no Unlogged QSOs control. App and Header never loaded
    the saved list themselves; `main.ts` did, so O1 on the real boot is the discriminating test.
- **Reversion proofs.** Each was applied with a unique anchor, verified, and restored to
  green:
  - **P1 (Go):** the `expect_` prefix check neutralised → Q1 fails "want 409 reload_required".
  - **P2:** the old refusal over unlogged work restored → A2, A3, A4 and R2 fail.
  - **P3:** the check after the prompt dropped → "A4 a Log started while the prompt is open"
    fails (`activating`/request).
  - **P4:** `indexedDB.open` at boot → O1 behavioural (`opens` = ['station-manager']) and the
    structural guard fail.
  - **P5:** `indexedDB.deleteDatabase` at boot → O1 fails on `deletes`.
- **Removed beyond the plan's list:** each was introduced only by a recovery commit, and each
  now has no consumer.
  - The toast `action` (option A, `c2600910`).
  - The Map branch's own toast renderer and the `h-screen` wrapper (`ebe244dc` / `894b5359`).
  - `adif.NormalizeValue` (`9573017e`).
  - The SPA `submitQso` `expect` option (`c551c828`).
  - `StationContext.logbookUuid` and `isoAt` (`ebe244dc`).
  - `bootArchiveId` (RS18).
  - Each file was restored to its version before that commit wherever recovery was its only
    change: `LoggingCard`, `ArchiveSwitchGate` (+ test), `CommentField` (+ test), `enrich`,
    `log-events` (+ test), `_helpers` (+ test), `seams` (+ test), `time` (+ test), `App`,
    `MapView`, `toasts` / `Toasts` (+ test), the SPA `qso` API (+ test), `config.go`
    (byte-identical to before `4a27711e`), and `parse.go`.
- **Kept:**
  - The `enrichmentExtras` split in `main.ts`, so the submit seam stays at complexity 23. The
    baseline is rekeyed `line@590` → `line@529`.
  - `verifyAfterRigReconnect`, now without the rig-reading retirement.
  - `submitState.uncertain` / `logOutcomeUnknown`, for the Activate prompt.
  - `isRetainedDefault`.
- **New, small:** the display-only header names a pending candidate ("Home (switching to
  Contest)"), which the old selector showed as an option.
- **Gates:**
  - SPA lint, format, svelte-check (0/0) and vitest 2,080/2,080.
  - Go: `gofmt` clean, `go vet`, and the whole-tree `go test`.
  - The Postgres-gated cloud suites (`sm-pg` started for the run, then stopped).
  - Maintainability 0 regressions after the rekey.
  - `task ci:local`: PASS ("All CI gates passed locally").
- **Review corrections (2026-10-05, before commit).**
  - **Wording.** With an unknown Log outcome, the Activate prompt now says "a Phone / CW entry
    whose Log outcome is unknown" and never "unlogged QSO". A4 asserts this and was RED first.
  - **The old-page refusal** now reads "this page is out of date; reload it. This request stored
    nothing — an earlier attempt may already be logged, so check the Logbook". It no longer tells
    the page to log again.
  - **Q1 strengthened:**
    - An enabled insert forwarder is bound to the logbook. The plain-submit control stores 1
      QSO AND queues 1 upload.
    - Every refused case leaves both counts at 0.
    - The decoded `message` must contain "reload" and "this request stored nothing", and must
      not say "log … again". It failed RED on the old message.
  - **P6, store-then-refuse:** submitting before the 409 fails Q1 with "stored 1 QSOs, want 0".
    The queue assertion cannot fail on its own: QSO and upload rows commit in one transaction,
    so the control is what proves the fixture can queue.
  - **Gates after the corrections:**
    - SPA lint, format, svelte-check (0/0) and vitest 2,080/2,080.
    - Go `gofmt`, `vet` and `./internal/...`.
    - Maintainability 0.
    - `task ci:local` was not re-run.
- **Codex review of `55178b58`: P2, fixed in `c31c0d7e`.**
  - **Finding:** removing the Map branch's `<Toasts />` left a failed gate stop silent in a
    Map window.
  - **Ruling (operator):** show the failed stop on the gate itself.
  - **Fix:** a failed stop now sets the gate's stop note; the toast stays.
  - **Test:** through the gate's Stop tune button with no toast renderer mounted. It was RED
    first. P7 (the toast-only path restored) fails on the missing note.
  - **Gates:** SPA lint, format, svelte-check (0/0), vitest 2,081/2,081, maintainability 0.
  - **Codex review of `c31c0d7e`:** no actionable findings.
  - **CI:** the run on `70d006a9` does not cover `c31c0d7e`. Its CI comes after the push.
- **CI and acceptance (2026-10-05).**
  - **CI** passed on `c31c0d7e` (run 37307101145) and on `245b7dde` (run 37311006137).
  - **Operator check after the deploy: PASS.** Three cases in Settings → Archives → Activate:
    1. Empty form: the prompt names the destination and warns about other windows.
    2. A QSO typed and not logged: the prompt names it, and OK discards it and switches.
    3. Cancel: no switch, and the QSO stays in the form.

  - **Option 1 selected and implemented (2026-10-05; uncommitted).** ADR 0085's dated
    update supersedes disconnect-clears: retain the last matching attribution for the
    switch-triggered save, unconfirmed until a successful proven re-read. Restore still
    uses only confirmed current attribution. Config writes, `config.updated`, and requested
    operator changes still clear both uses immediately. Missing historical values are never
    backfilled; the existing `7Q7CT` record still needs Discard and a fresh S2 record.
  - **Cause proven test-first (2026-10-05).** M4 in `main.attributionboot.test.ts` imports
    real `main.ts`, reads attribution through the stubbed HTTP transport, emits the events
    drop, then reopens against another archive. The real preserver writes to the test's
    memory store before its reload seam runs. Before the fix it reached
    `expect(saved.attribution).toEqual(original)` with null; afterwards it preserves all
    three values and the original archive/logbook, and passes Restore eligibility with
    that original destination active again. Same-archive reconnect writes no record, and
    the different-archive reconnect makes no attribution read from the new archive.
  - **Reversion proofs:** independently reinstated disconnect-clears and the preserver's
    confirmed-only getter. Both mutations were verified present; each failed M4 at that
    exact saved-attribution assertion (null versus the three original values). Both were
    restored. AS11–AS14 cover late responses, failed refresh, definite invalidation after
    a drop, operator identity, known-empty/missing values and a boot read crossing a drop.
    The focused attribution/preservation/eligibility/recovered-submit run passed 71 tests.
    RS8/RS11 remain OPEN; none of this is a browser ownership or closure proof.
  - **Validation and candidate (2026-10-05).** `SKIP_NPM_CI=1 task ci:local` PASS:
    164 frontend files / 2,311 tests, lint/format/type checks, production build, Go vet/lint,
    zero maintainability regressions, race/full Go tests, static/PocketFFT builds and the
    build-boundary checks. The added import moved the existing `main.ts` complexity entry
    from line 589 to 590; only its location changed, not its limit of 23. An unnecessary
    test `async` was removed after lint caught it; the sandbox-blocked Go cache stage was
    rerun with cache access. `SM_FFT=pocketfft task rpm:dev` PASS, producing
    `build/private/station-manager-dev.x86_64.rpm`, version
    `2.0.0-alpha.3-147-g152fb36b-dirty`.
  - **Deployment and drill still pending.** The daemon was observed active; checking the
    exact RPM install permission with `sudo -n -l` returned "a password is required."
    Nothing was installed or restarted. Operator: run `task deploy:local:dev` in a terminal,
    refresh both browser windows, Discard the old `7Q7CT` record, and repeat S2 using a fresh
    app-created record before D1. No browser-control tool was available to discard that
    origin-bound record here. S2.5 remains FAIL and RS8/RS11 remain OPEN until observed.
No hardware or RF experiment was run; no daemon was restarted.

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
- **2026-09-24, operator (drill 2, Forwarding tab in Drill): "forwarding is NOT critical to SMD
  startup and therefore can be in an archive; forwarders should be part of the archive — when a
  new archive is created they CAN be enabled but default to DISABLED; the current text is
  confusing."** Agreed, and it is the 2026-09-22 rule restated: routing (which destination this
  archive uploads to) lives in the archive file as bindings, a new archive starts with none; only
  the station's credentials and transport stay in `config.json`. Granularity reconciled in the UI:
  the Forwarding tab shows ONE switch per destination for the ACTIVE archive (ENABLED / DISABLED,
  meaning "this archive uploads new QSOs here"), which binds every logbook in the archive;
  per-logbook exceptions belong to Settings → Logbooks. Station accounts (credentials) become a
  separate section with no on/off pill. With the switches in place the interim gate is redundant
  (an archive with every destination DISABLED forwards nothing by construction; adoption seeds
  Home's switches from the station's enabled flags once, idempotently). Consequence for slice 5:
  widen it from "SM Cloud binding only" to the per-destination switch for all four services (the
  same binding row). Interim, RULED 2026-09-24 and built: the summary pills are hidden while gated (no per-destination
  truth to tell; the banner carries the one fact); queue counts, the card note and the non-gated
  presentation are unchanged until slice 5 replaces the tab.
- **2026-09-24, operator correction: credentials are not uniformly station-wide.** ClubLog has
  two keys: the **API key** identifies Station Manager to ClubLog (injected at build time, never a
  config field — `clublog.go` InjectedAPIKey) and the **application password** identifies the
  user's ClubLog account (today in `config.json` with the account email and the callsign the
  upload is filed under; one account may hold several callsigns); a QRZ.com API key is per logbook on the QRZ side (two QRZ logbooks = two
  keys); SM Cloud's URL and token identify the tenant. So the rule is: what identifies the
  application or the tenant → `config.json`; what identifies a specific remote logbook or account
  → the binding row inside the archive file, beside the logical logbook it serves (ADR 0056's
  "which forwarder credential/account each logbook uploads to", taken literally). Consequences:
  (i) the per-archive switch for QRZ is not a bare on/off — enabling asks for that logbook's key;
  ClubLog asks which account/callsign, defaulting to the station's; the "station accounts"
  section holds only the application-wide and tenant-wide items; (ii) secrets then live in the
  archive file (0600 in 0700, already the station's most sensitive data) — a backup or export of
  an archive carries keys, and the cloud restore path must never ship them: a required line in
  slice 5's design before any binding row holds a key. Discussion only; no code changed.
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
- **2026-09-25, slice 5 opens with its own ADR (directed 2026-09-24; accepted 2026-09-25).**
  [ADR 0082](../decisions/0082-per-logbook-destination-bindings-in-the-archive.md): a
  `logbook_destination` row per (logical logbook × destination type) inside the archive file, the
  operator's one-switch-per-destination as an aggregate (`on`/`off`/`mixed`) over those rows, no
  archive-level row and no implicit binding for a new logbook; logbook-scoped fields (QRZ key, QRZCQ
  call+key, ClubLog email/application password/callsign, SM Cloud legacy logbook name) in the
  binding, station accounts (SM Cloud URL/token, transport, cadence, retry; ClubLog application key
  build-injected) in `config.json` with `action_filter` retained, `enabled` retired and one entry per
  type; idempotent Home seed of enabled and disabled entries on each logbook present, then a
  file-first config strip with v6 keeping the old keys known but deprecated. The default logbook
  keeps each legacy `forwarder_name`; additional logbooks receive UUID-derived names and their queue
  rows are renamed in the same transaction, while duplicate legacy entries of one type are refused
  before mutation. The data-aware 6 → 5 config down runs before 0014 down, reconstitutes exactly
  representable v5 entries, and restores legacy queue names; binding edits are restart-required, one
  worker per binding, with ADR 0039 discard/re-arm per binding; all four destinations route by the
  QSO's logbook inside the existing atomic transaction; SM Cloud identity by UUID with an explicit
  idempotent adoption call and the last gate remnant scoped to "no SM Cloud binding outside the
  adopted archive until the server and adopted mapping are identity-ready"; bindings are never
  exported, restored, logged or served unmasked, and explicit clearing requires the binding to be
  off; the Forwarding tab is rewritten for the ACTIVE archive
  (`PUT /v1/qso-archives/{uuid}/bindings`, 409 otherwise) with a station-accounts section and no
  pills; a characterization commit pins Home's fan-out before migration 0014. **Ruled 2026-09-25:**
  (i) a new logbook starts unbound even for SM Cloud; (ii) binding edits are restart-required in this
  slice; (iii) bindings are editable on the active archive only, with `409` otherwise. The ADR is
  accepted; no slice 5 code, migration or detailed implementation plan exists yet.
- **2026-10-06, Settings → Logbooks and Forwarding notes routed here (inbox of 2026-10-05; not
  selected).**
  - **Logbook description:** Settings → Logbooks should offer a description field, and Settings →
    Archives should show each logbook's description in its archive's list. Fact checked: the
    daemon already stores and accepts it (`types.Logbook.Description`; `POST` and `PATCH
    /v1/logbook` in `handler_logbook.go`). The archive summary (`types.QsoArchiveLogbook`) does not
    carry it, so showing it for an inactive archive needs the ADR 0084 sidecar to record it.
  - **Set the active logbook:** a Settings → Logbooks action that chooses the logbook live logging
    goes to. This is the "Make default" that the 2026-09-29 first-slice ruling left out of scope (a
    daemon change, with restart behaviour still to rule).
  - **Forwarding is per logbook:** the operator's note says forwarding is a per-logbook setting,
    because the callsign is often the key field. The data model already binds forwarding per
    logbook (ADR 0082); the note concerns presentation, and is the logbook-first layout (option B)
    of the 2026-09-26 design exchange on the Forwarding tab ("the Forwarding tab frames the wrong
    thing"), which still needs its own ADR.

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
