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
   - **Station drills after deploy** (operator-run, recorded here): Home unchanged after the
     upgrade (same `forwarded_to`, worker names and queue counts as before; bindings listed under
     Home with the legacy names); the Drill archive shows every destination off, no banner, and a
     dummy QSO stores with `forwarded_to: []`; enabling a destination on Drill needs the operator's
     say per occasion (a real key uploads a dummy QSO to a real logbook); the rollback drill on
     copies; the two-archive SM Cloud proof after 5F.

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
