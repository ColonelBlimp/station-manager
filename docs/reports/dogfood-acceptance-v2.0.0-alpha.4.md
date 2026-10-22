# Dogfood acceptance — v2.0.0-alpha.4

Release-specific execution record for the canonical gate in
[`docs/dogfood-acceptance.md`](../dogfood-acceptance.md). It replaces alpha.3 under the operator's
Option 1 ruling of 2026-10-21 ([alpha.3 record](dogfood-acceptance-v2.0.0-alpha.3.md),
"Compatibility and recovery plan"). alpha.3 is superseded and was never accepted; its tag is kept.
Every `pending` row is the operator's to execute and rule on. Hardware, rig-command and RF cases keep
their separate authorisation boundary, and nothing here authorises RF.

## Scope — operator rulings, 2026-10-21

- **Option 1** (alpha.3 record): a new candidate cut from `main` after checklist C passed and AC 6
  closed (W-0021, `3f7425ed`), before Settings → Logbooks or any unrelated implementation.
- **Candidate:** `v2.0.0-alpha.4` at `3f7425ed`, cut once CI is green on that commit. The agent
  creates the annotated tag locally; it is not pushed.
- **Build:** as alpha.3. In a clean scratch worktree at the tag, `task ci:local` runs first, then
  `SM_FFT=pocketfft scripts/dev-rpm.sh` builds once. The single versioned copy (mode 0600) lives in
  `build/private/`. It is a private keyed build carrying the ClubLog key and is **not
  distributable**.
- **Lanes:** an isolated RPM guest is the clean lane, and the same artifact is the daily driver's
  upgrade. No guest exists on this host yet. The agent drafts a guest plan (Appendix 3) for the
  operator's review, and nothing is created until it is approved.
- **Gates in scope:** Gate A; B1; CC-5 and the retirement of the alpha.2 B1-01 waiver (A1-05 and
  B1-01 rerun on this artifact); and an updated B2 inventory that explicitly exercises archives,
  bindings, restore and provisioning.
- **Recovery:** refresh the recovery backup before the live upgrade. The 09:04Z backup of
  2026-10-21 and the preserved dev RPM (`2d3a0ec9…`) remain the recovery pair until then.

## Header

Candidate commit: `3f7425ed` — tag `v2.0.0-alpha.4` (annotated, local, **not pushed**; the tag
identifies the candidate and does not mean acceptance passed). CI green on `3f7425ed` (run
39612261239, confirmed 2026-10-22).
Version/build shown by daemon: `2.0.0-alpha.4` expected on `GET /v1/version`, the sidebar build
badge, the startup log line and ADIF `PROGRAMVERSION`.
RPM filename: `build/private/station-manager-2.0.0~alpha.4-1.x86_64.rpm` (mode 0600), the single copy
of the artifact `scripts/dev-rpm.sh` built in a clean scratch worktree at the tag, since removed. It is a
private keyed build (PocketFFT/CGO; ships `/usr/share/doc/station-manager/PRIVATE-BUILD-DO-NOT-DISTRIBUTE`)
and is **not distributable**. Verify the SHA before every install.
RPM SHA-256: `cebcd305e99db31671ca0dbdeace70f84064e38a150f744f497aec9059c5532b` (9,625,351 bytes, built
2026-10-22 05:26Z); `/usr/bin/smd` inside it SHA-256
`08aebee97d2556c2385bf4105f3e5b5339d2011f0a00ce208236941bf0c6426b`
RPM-reported version: `station-manager 2.0.0~alpha.4-1 x86_64` (read with `rpm -qp`); daemon build line
`version: 2.0.0-alpha.4, FFT: PocketFFT (CGO, dynamically linked)`
Previous installed version: `station-manager-2.0.0~alpha.3.262.g6217da21.dirty-1.x86_64`, the dev
build of `6217da21` deployed 2026-10-21. By RPM ordering this is an **upgrade**:
`rpm.labelCompare(2.0.0~alpha.4-1, 2.0.0~alpha.3.262.g6217da21.dirty-1)` = 1 (checked 2026-10-21).
The station's data is config version 6 and log schema 18, the same as the candidate's head, so B1
expects no migration to apply.
Clean-install environment: pending — an isolated RPM guest (Appendix 3)
Upgrade environment: the daily-driver dogfood station (this host)
Browser(s) and viewport(s): Firefox; the A4 parameters to be confirmed at A4-01
Planned test date: 2026-10-21 onward
Operator: 7Q5MLV
Automated gate evidence: `task ci:local` passed at `3f7425ed` on 2026-10-22 (05:19–05:26Z) in the clean
scratch worktree: SPA lint, prettier, svelte-check, vitest 2158/2158 (153 files), Vite build; manual
build; Go gofmt, vet, golangci-lint, maintainability observatory 0 regressions (33,719 measurements);
race and full Go test runs, none from the test cache; static, all-`cmd/` and PocketFFT builds; PocketFFT
FT8 decode test; ClubLog build-key boundary (ST-7) OK; documentation catalog and agent-context budgets;
"All CI gates passed locally." The smcloud store's Postgres tests skip locally (`ci:local` starts no
Postgres); CI run 39612261239 ran them on the same commit, green.
Backup location (do not record secrets): pending (A3-01, a refresh of the 09:04Z backup)
Restore check: pending (A3-02)
Recovery policy: a stopped-daemon copy of the complete working directory is the primary recovery
artifact. The previous RPM is preserved for a package rollback. Every log migration since alpha.3
has a `.down.sql`, but `smd db-downgrade` refuses below 17 while Home's adopted SM Cloud binding
holds its confirmation (`refuseErasingAdoptionClaims`, `internal/database/sqlite/downgrade.go`).
Since the candidate adds no migration over the installed build, a package rollback to the preserved
dev RPM needs no data downgrade.
Rollback artifact/command: pending (A3-04)

Ready to deploy: pending
Operator/date:

Dogfood accepted: pending
Operator/date:
Residual waivers: the alpha.2 B1-01 waiver (Finding #6), carried from alpha.3, is retired by A1-05
plus B1-01 below
Follow-up destinations: pending

## Release delta — `v2.0.0-alpha.3` → `v2.0.0-alpha.4` (270 commits, 161 outside Markdown)

Drafted by the agent from `git diff v2.0.0-alpha.3 3f7425ed`; reviewed and corrected at A1-03.

### Migrations

- **Log schema 9 → 18:** `0010_qso_upload_upstream_id_generation`, `0011_qso_upload_failure_class`,
  `0012_archive_identity`, `0013_operator_event_archive`, `0014_logbook_destination`,
  `0015_logbook_destination_legacy_name`, `0016_logbook_destination_adoption_reservation`,
  `0017_logbook_destination_adoption_account`, `0018_logbook_destination_identity_since`. Each has a
  `.down.sql`; `smd db-downgrade` guards the adoption claims (above).
- **SM Cloud server schema:** `0007_archives` and `0008_logbook_callsign`. They are already deployed
  on the station's smcloud (2026-10-21); the candidate is a daemon artifact only.
- **Config version 3 → 6** (`internal/config/migrations.go`, `currentConfigVersion = 6`), with
  `smd config-downgrade` down steps 6 → 5 → 4 → 3.

### Configuration changes

Checked at A1-03 against `docs/v2-design/config.md` and the code at both tags (the JSON keys of
`internal/config` and `internal/types` compared; none removed).

- **New file keys (v4, v5), server-managed:** `active_qso_archive_id`, `pending_qso_archive_id` and the
  `qso_archives` catalogue (`id`, `label`, `ownership`, `path` for legacy and external entries,
  `last_activation_error`, and `request_key` from v5). `PUT /v1/config` carries none of them; adoption
  at start and the archive manager write them.
- **Forwarders become station accounts (v6, ADR 0082):** at most one entry per `type`. `name`,
  `enabled` and logbook-scoped credential keys stay known but deprecated until Home's binding seed
  commits; startup then strips them, first writing `config.v5.json` once (0600, never overwritten).
  Whether a logbook uploads, and its per-logbook credentials, move to the archive's bindings.
- **`datastore.path`** selects the QSO file only until adoption; afterwards the active catalogue
  entry does.
- **Evidence sync** depends on a complete SM Cloud station account (`url` and `token`), no longer on
  an enabled `smcloud` forwarder.
- **`default_rig_id: 0`** means no default rig and is valid with rigs configured; load no longer
  stamps rig 1 (`4ffc727a`). With no default rig, `bridge.enabled` is refused.
- **Down steps:** `4 → 3`, `5 → 4` and `6 → 5` through `smd config-downgrade`; `6 → 5` rebuilds
  entries from Home's bindings and refuses where they cannot become one v5 entry per destination.
- **Not a change:** the unknown-key refusal (ADR 0074) was already in alpha.3's code
  (`PreflightUnknownKeys`); only the reference's wording was corrected.
- **Reference defect found:** the §3 table in `docs/v2-design/config.md` (line 69) read "Schema
  version; currently `5`"; the code (`currentConfigVersion = 6`) and §13.1 say 6. Corrected on
  `main` by a separate docs-only change (operator ruling, 2026-10-22); the candidate's behaviour is
  unaffected and needs no rebuild.

### Changed API surface (the embedded SPA is the only client)

Checked at A1-03: the registered routes at both tags were compared (54 → 61 explicit `/v1`
method/path registrations; none removed), and each changed item below was confirmed new in the code
since alpha.3 (corrected at the operator's review, 2026-10-22).

New endpoints (the seven added routes): `POST /v1/forwarder/{name}/queue/retry`;
`GET /v1/logbook/{id}/destinations`; `GET` and `POST /v1/qso-archives`;
`POST /v1/qso-archives/{uuid}/activate`; `GET` and `PUT /v1/qso-archives/{uuid}/bindings`.

Changed semantics on existing endpoints:

- **Forwarder names are binding names** of the active archive (the seeded legacy names on Home,
  `<type>.<logbook uuid>` on another logbook): `POST /v1/forwarder/{name}/uploads` (new bucket
  `skipped_other_logbook`; `forwarder_unavailable` now means no enabled binding),
  `POST /v1/forwarder/{name}/queue/clear` (404 when not a binding), and the `missing_from` filter of
  `GET /v1/logbook/{id}/qso` and `/count`.
- **`GET /v1/forwarder-queues`:** one entry per binding; adds `waiting` and `failed` beside
  `clearable` and `in_flight`.
- **`POST /v1/smcloud/reconcile`:** one pass per enabled SM Cloud binding, concurrently under one
  25 s deadline; the response becomes `{"results": [...]}` per binding (a breaking shape change; the
  embedded SPA is the only client). **200 whenever at least one reconciler runs, whatever its
  outcome:** a binding's failure, timeout or held/unresolved state is an `error` entry inside the
  200. **503 `smcloud_unavailable` only when no reconciler is runnable** (no enabled SM Cloud binding,
  or every one held, unresolved or unbuildable), with the entries beside the error envelope.
  Acceptance assertions must read the per-binding entries, not the status alone.
- **Archive-switch refusals on existing paths:** once an activation is accepted, `POST /v1/rig/tune`
  and the FT8 admissions (`POST /v1/ft8/claim`, `/tx/arm`, `/tx/send`, and session starts) answer
  **409 `archive_switch_pending`** until the daemon restarts; the stop paths are never refused.
- **`GET` / `PUT /v1/config`:** `forwarders` are station accounts (`{type, label?, action_filter?,
  credentials_set?}`, no `name` or `enabled`); a PUT carrying `name`, `enabled` or a logbook-scoped
  key is refused 400 `forwarder_field_binding_owned`; `forwarder_unusable` now judges enabled
  bindings; new read-only `ft8_running` (`bridge_enabled` and `ft8_enabled` were already in
  alpha.3); `default_rig_id: 0` clears the default, refused while the bridge is enabled; new 500
  `bindings_read_failed`.
- **`POST /v1/qso`:** an `expect_*` query key is refused 409 `reload_required` (ADR 0087), and the
  query is parsed strictly.
- **`GET /v1/qso/{uuid}/uploads`:** each item carries `failure_class` (`"auth"` or `""`, migration
  0011).
- **`GET /v1/logbook`:** each logbook carries its `uuid` (migration 0012).
- **`GET /v1/station-events`:** new `archive.activated` and `archive.activation_failed` kinds.
- **`GET /v1/forwarder-types`:** credential fields gain `scope` (`station` or `logbook`) and
  `defaults_to` (`clearable` was already in alpha.3). **`GET /v1/lookup-types`:** entries gain `summary`.
- **`GET /v1/version`:** adds `archive` (`{id, label, ownership}`, never the path).

### Changed user journeys

Corrected at the operator's review (2026-10-22); each item checked against the SPA at the tag.

- **Settings:** Archives, Logbooks and per-logbook Destinations sections; the station accounts split
  from bindings on Forwarding; stored-secret fields; help tips and manual links.
- **Archive switching, only from Settings → Archives** (ADR 0087). The header shows the active
  archive above the logbook, display only, and "(switching to …)" while a candidate is pending
  (`Header.svelte`). Activating asks for confirmation (`activateConfirmText`, `archives.svelte.ts`):
  - it names the target, and warns that the restart takes about 5 s, that every open window reloads,
    and that unlogged work in **any other window** (a Phone/CW QSO, an unfinished FT8/FT4 exchange,
    unsaved Settings edits) is lost; logged QSOs are never affected; TX must be idle and FT8 disarmed;
  - when this window holds an unlogged Phone/CW entry, it names it and says OK discards it and
    Cancel keeps it; an entry whose Log outcome is unknown is named as possibly already logged, with
    a pointer to check the old archive's Logbook;
  - a Log in flight refuses the switch ("A QSO is being logged on Phone / CW — wait for it to
    finish, then switch"), checked before and again after the prompt.
  After an accepted request the tab waits for a **different** daemon instance and reloads; every
  other open tab reloads when its reconnect finds another archive served. When the new instance
  cannot be proven (no baseline, or the wait expires), the app is gated (`ArchiveSwitchGate`): an
  overlay blocks logging and transmitting until reload, keeping the TX and tune stops reachable.
- **Upload failures:** Settings → Forwarding shows waiting, failed and in-flight counts per
  destination and per logbook; **Retry failed (N)** re-arms that binding's failed rows (any failure
  class) and reports how many were re-queued; rows the destination rejected for authentication
  (`failure_class: auth`) are re-armed automatically when the worker starts, once per daemon
  restart.
- **FT8/FT4 availability follows the running daemon** (`ft8_running`, not the saved switch): with
  FT8 not served, the FT8/FT4 mode links are hidden and every way into an FT mode (bookmark, stored
  last mode, Back/Forward, a click) lands on Phone/CW with the off-state notice (`FtOffNotice`)
  linking to the setting; the fallback does not re-tune the rig. A saved change applies after the
  restart.
- **Rigs:** "+ Add rig" opens a model picker and then an **unsaved draft** that Cancel discards;
  adding never selects the default; **Set as default** is the explicit act and needs a restart.
  Delete is an immediate, confirmed write; the default rig deletes only with CAT off (button
  disabled, "Turn off the rig connection (CAT) first"), which leaves no default rig.
- **SM Cloud by archive and logbook identity:** adoption of Home, `uploads_held`, identity uploads
  and reconciles.

### Packaged commands

`smd` subcommands at the cut: `import`, `restore` (now by archive and logbook UUID), `provision`
(new), `config-check`, `db-downgrade` (new) and `config-downgrade` (new). Known defect, not in scope
for this candidate: an unknown first argument starts the daemon (dogfood inbox, 2026-10-21).

### New manual or setup instructions

**Manual** (`manual/content/chapters/`, 8 files, +470/−159):

- **New chapter `qso-archives.md`:** where archives are, logbooks, creating an archive, switching
  archives, what stays shared.
- **`forwarding.md`** (largely rewritten): where forwarding is set up, station accounts, turning a
  destination on, SM Cloud backup, the queue (retrying and clearing), advanced upload pacing.
- **`cat.md`:** new "Adding your rig and choosing the default".
- **`ft8.md`:** new "Settings for FT8 and FT4" (PSK Reporter, decode log).
- **`enrichment.md`:** a section per lookup source, filling gaps from other sources, and how long
  looked-up details are kept.
- **`what-is-sm.md`:** new "More than one window".
- **`importing.md`, `station-events.md`:** small wording changes.

**Install guide** — `docs/install.md` (+186/−8):

- **Imports and restores:** `smctl import --forward` names bindings; `smd import` and `smd restore`
  resolve the active archive unless `--archive` names another; restore goes by archive and logbook
  identity, with `--cloud-logbook` for an old name-based backup, and `--forwarder` is retired.
- **New sections:** "Provisioning a cloud archive on this station" (`smd provision`) and "Rolling
  back to an older build" (`config-downgrade` first, then `db-downgrade`, with their refusals).
- **Upgrade and backup:** `config-check` does not open bindings; a full backup is `config.json`
  plus every archive and the station-global databases, taken stopped or with SQLite's online backup.
  A file-only key the old daemon does not know is now added only with the old daemon stopped, as it
  refuses unknown keys.

## Gate A — ready to deploy

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `A1-01` | yes | **record** — clean scratch worktree at tag `v2.0.0-alpha.4` (`3f7425ed`) | Build once with `SM_FFT=pocketfft scripts/dev-rpm.sh` after `task ci:local`; restore the committed `frontend/app/dist/index.html` first if the CI mirror rewrote it (alpha.3 Finding #1); record filename, SHA-256 and RPM version; keep the versioned copy | One artifact; RPM version `2.0.0~alpha.4-1`; `ci:local` exit 0; SHA recorded in the header | A `-dirty` or `alpha.4-N-g…` version; a second build overwriting the copy | Execution log #1–#3: `ci:local` exit 0. The CI mirror rewrote the tracked `dist/index.html` again (alpha.3 Finding #1); the committed file was restored before the build, `git status --porcelain` empty, `git describe` `v2.0.0-alpha.4`. One build: `2.0.0-alpha.4`, RPM `station-manager 2.0.0~alpha.4-1 x86_64`, 9,625,351 B, SHA-256 `cebcd305…c5532b`, copied to the versioned path (0600) with the SHA re-verified | none | **PASS** — operator, 2026-10-22: independently verified the tag's commit, the RPM's permissions, size, SHA-256, package version, private-build marker and packaged `smd` hash; CI results accepted from the execution record. Finding #1 stays open and does not block this artifact (the committed file was restored before the build, and the build regenerates the SPA after the version is fixed) |
| `A1-02` | yes | **clean** — the A2-01 install, candidate installed and started | Verify the RPM SHA before installing; read `GET /v1/version`, the sidebar badge, the startup log line and `rpm -q` | All four read `2.0.0-alpha.4`; schema `{18, dirty:false}` after the first start | A badge from a cached tab; a dev-build version string | pending | none | pending |
| `A1-03` | yes | **record** — delta drafted above | Review the release delta against the candidate, completing the configuration and API items | Delta accepted or corrected here | Silent scope creep in the delta | Configuration, API and install items completed by the agent from the code and canonical references at both tags (route and JSON-key lists compared); one reference defect found (`config.md` line 69 said version 5, corrected on `main`, no rebuild). Operator review 2026-10-22: pending, with corrections — three fields wrongly labelled new, the archive-switch journey, the switch and reconcile failure semantics, the journeys beyond component names, the manual inventory, and the route count (54 → 61); all applied | none | **PASS** — operator, 2026-10-22: the revised delta resolves the review findings, including the B2 archive-switch wording; the reference fix is limited to 5 → 6 and needs no candidate rebuild |
| `A1-04` | yes | **host-scratch** — 0700 scratch with a copy of the live `config.json`; the candidate `smd` extracted from the RPM (alpha.3 record, Appendix 1); live service untouched | `smd config-check --config` on the untouched copy (control), then on a top-level and a nested unknown-key mutation | Control exits 0; each mutation exits non-zero naming only the key path; nothing started, bound or written | Control non-zero: the candidate would refuse to start on the station. Fix and re-run; no waiver possible | Execution log #4: control exit 0 ("loads and validates as startup would; 0 enabled forwarder(s) construct", as v6 keeps uploads in each archive's bindings); top-level mutation exit 1 naming only `typo_key_a1_04`; nested exit 1 naming only `bridge.typo_nested_a1_04`, "(values omitted)"; no unexpected matches among the checked string values (six characters or more); nothing written; live config, service and file names unchanged | none | **PASS** — operator, 2026-10-22: the valid control succeeds and both independent mutations fail with the intended key-path diagnostics; the hashes, empty working directory and unchanged live service support isolation. Boundaries: zero enabled legacy forwarders validates neither archive bindings nor upstream connectivity (later cases cover them); the disclosure scan excluded shorter strings and non-string values |
| `A1-05` | yes | **host-scratch** — as the alpha.3 record's Appendix 2, with this artifact's `smd` | The CC-5 reconcile on the alpha.1-era file, then the permutation control | Copy exits 0; the permutation exits non-zero naming `qrzcq` and the unsupported action | Both exit 0; the copy refused for `allow_insecure_http` | Execution log #4: fixture (config version 2, qrzcq `["insert","update","delete"]`, scratch-only `allow_insecure_http`) exit 0, "3 enabled forwarder(s) construct"; permutation `["update","insert","delete"]` exit 1: `forwarder "qrzcq": type "qrzcq" does not support action "update" (supports [insert])`; nothing written; live untouched | none | **PASS** — operator, 2026-10-22: the reconstructed legacy fixture succeeds while the permutation reaches validation and fails for the intended unsupported action, distinguishing the repair from indiscriminate acceptance. The alpha.2 B1-01 waiver still awaits B1-01 |
| `A2-01`…`A2-07` | yes | **clean** — the isolated RPM guest (Appendix 3), no Station Manager data | As the alpha.3 record's A2-01…A2-07 (install, first-run, callsign, setup, persistence, guide and manual, uninstall), with this artifact | As there | As there | pending | none | pending |
| `A2-08` | yes | **clean** — a second fresh working directory in the guest: the frozen alpha.2 RPM (SHA `7b2fa20a…c24856`) installed and set up, a few test QSOs, daemon running | Upgrade per `install.md` §7 to the candidate | `config-check` exit 0; config 3 → 6 and schema 8 → 18 applied once at the first start; setup bypassed; QSOs, logbook and config retained | Migrations re-run on every start; a refusal at the restart the preflight did not name | pending | none | pending |
| `A3-01` | yes | **live** — daemon stopped | Refresh the recovery backup of the complete working directory outside the repository | Backup exists and is listable; SHA-256 recorded | Backup copied into the repository tree | pending | none | pending |
| `A3-02` | yes | **host-scratch** — the A3-01 archive | Restore a copy into scratch; open read-only (`immutable=1`, a verified unchanging copy); remove it | Integrity `ok`; counts equal A3-03 | Restore needs the live directory | pending | none | pending |
| `A3-03` | yes | **live** — pre-upgrade | Record durable-record counts: QSOs per logbook per archive, upload-queue rows by status, `operator_event` rows per category, bindings per archive | Numbers recorded here without contents | Counting after the upgrade began | pending | none | pending |
| `A3-04` | yes | **live** — pre-upgrade | Confirm the preserved previous RPM (`2d3a0ec9…`) and record the rollback: package downgrade with `rpm -Uvh --oldpackage`; no data downgrade needed (same heads) | Artifact present; procedure named in the header | Only the RPM preserved | pending | none | pending |
| `A3-05` | yes | **live** — before B1 | Confirm rollback time is reserved before the station is next needed | Window recorded | Upgrade started with a session imminent | pending | none | pending |
| `A4-01` | yes | **record** — delta reviewed (A1-03) | Ratify this inventory, including the B2 additions below; confirm the A4 parameters | Inventory ratified | An execution before ratification | pending | none | pending |

## Gate B — deploy and accept

### B1 — the upgrade itself (and the B1-01 waiver retirement)

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `B1-01` | yes | **live** — dev build `alpha.3.262.g6217da21.dirty` running; A1-04, A1-05 and A3 done | Upgrade per `install.md` §7: `sudo dnf install <candidate rpm>`, `smd config-check`, `systemctl --user daemon-reload`, `systemctl --user restart smd` | Package replaced while the old daemon runs; preflight exit 0; restart observed; browser reconnects within the A4 tolerance. **Retires the alpha.2 B1-01 waiver** with A1-05 | Install succeeds but the restart is skipped; a scriptlet leaves the service stopped | pending | none | pending |
| `B1-02` | yes | **live** — upgraded | Repeat A1-02 and deliberately reload the open tab | All four read `2.0.0-alpha.4`; no old chunk from cache | An unreloaded tab still running the dev build | pending | none | pending |
| `B1-03` | yes | **live** — upgraded | Confirm setup bypassed; config, rig, archives (Home active, Drill inactive), logbooks, bindings, theme and rail, and durable events retained | All retained | First-run gate reappears; the archive catalogue or a binding lost | pending | none | pending |
| `B1-04` | yes | **live** — upgraded | Compare A3-03 counts; inspect startup diagnostics | Counts equal; nothing to migrate (schema 18, config 6); no error records | Counts differ; a migration re-runs | pending | none | pending |
| `B1-05` | yes | **host-scratch** — immediately before B1-01, a fresh copy of the live `config.json` | Re-run the A1-04 control only | Exit 0 | Non-zero: stop and fix; no waiver | pending | none | pending |
| `B1-06` | yes | **live** — upgraded | Controlled daemon restart | UI and the rig, log and FT streams recover without a manual reload | Streams dead until reload | pending | none | pending |

### B2 — additions for the archive programme (to be expanded into cases before A4-01)

The alpha.3 scope ruling's B2 stands. Option 1 adds explicit cases for:
- **Archives:** list, create, activate from Settings → Archives over the attended restart (the
  confirmation's discard and cancel, the other-window warning, the refusal while a Log is in flight,
  the reload of every tab, and the unresolved-switch gate), the header's archive display,
  last-known-good start, and Home's legacy entry.
- **Bindings:** per-logbook destinations, station accounts, `restart_required`, `uploads_held`,
  adoption status, and the Forwarding-page wording (dogfood inbox, 2026-10-21).
- **Restore:** `smd restore` by archive and logbook UUID, dry run and real, on a scratch copy
  (checklist C's method).
- **Provisioning:** `smd provision` list, dry run, real run and the refusal of an existing file, on a
  scratch copy.

## Appendix 3 — clean-lane guest plan (DRAFT for the operator's review; nothing created)

Purpose: an RPM environment with no Station Manager data and no route to the live station's
files, for A2-01…A2-08. The host is Fedora 44 with `virt-install`, `qemu-img`, `xorrisofs` and
libvirt (one existing VM, `win10`, on `qemu:///system`; checked 2026-10-21). Nothing below has been
run.

1. **Connection: `qemu:///session`** (the operator's user, no root, separate from the system VMs).
   Directory `~/sm-guest` (0700) holds everything; nothing under the working directory or the
   repository.
2. **Image:** Fedora Cloud Base 44 qcow2 from `download.fedoraproject.org`, verified against the
   release's signed `CHECKSUM` file with Fedora's published GPG key before use. A qcow2 overlay on
   it is the guest disk, so a reset is deleting the overlay. *Unverified:* the exact file name for
   44, which is read from the release directory when this step runs.
3. **First boot by cloud-init:** a seed ISO built with `xorrisofs` (`cidata` label) holding
   `user-data` and `meta-data`. One user, `smtest`, with an SSH key generated for this guest only
   (in `~/sm-guest`, never reused), no password login, and lingering enabled at the A2-01 step the
   guide describes rather than by cloud-init, so the guide's instruction is exercised.
4. **Network:** user-mode networking (`passt` under the session connection), so the guest reaches
   the Fedora mirrors for package dependencies and nothing on the LAN is addressed by name.
   Port forwards on host loopback only: `127.0.0.1:2222` → guest 22 and `127.0.0.1:18080` → guest
   8080, for SSH and the browser. *Unverified:* that this libvirt build supports `passt`
   port forwards in a session; if not, the fallback is QEMU user-mode `hostfwd`, recorded here.
5. **Isolation:** no shared folders, no host devices (no serial, no audio passthrough), no
   credentials and no copy of any operator data. The only files copied in are the candidate RPM
   and, for A2-08, the frozen alpha.2 RPM, each by `scp` and SHA-verified inside the guest.
   The candidate carries the ClubLog key, so the guest is private and disposed of after Gate A.
6. **A2-08 sequencing:** the two RPMs cannot be installed together. A2-08 runs after A2-07's
   uninstall, as a second user `smtest2` with a fresh working directory: install the alpha.2 RPM,
   set up, log a few test QSOs, then upgrade per `install.md` §7.
7. **Disposal:** after Gate A, `virsh --connect qemu:///session undefine --remove-all-storage` and
   removal of `~/sm-guest`, the SSH key included. The operator decides the timing.

Operator decisions before anything is created: approve this plan or name another lane, and approve
the image download.

## Execution log

Command-line checkpoints run by the agent; the operator rules every result. Times UTC.

1. **2026-10-22 — freeze.** CI green on `3f7425ed` (run 39612261239). Annotated tag `v2.0.0-alpha.4`
   created locally at `3f7425ed`, not pushed. A scratch worktree (agent scratchpad) checked out at the
   tag: `git status --porcelain` empty, `git describe` `v2.0.0-alpha.4`, `scripts/version.sh` →
   `2.0.0-alpha.4` / RPM `2.0.0~alpha.4`. `.env` copied in (0600, Git-ignored) for the private build key.
2. **05:19Z–05:26Z — `task ci:local` in the worktree.** Exit 0; details in the header. No Go result came
   from the test cache (131 `ok` lines, none `(cached)`).
3. **05:26Z — the candidate build.** As at alpha.3, the CI mirror's SPA build had left the tracked
   `frontend/app/dist/index.html` modified (Finding #1 of the alpha.3 record, still open). `git show
   HEAD:frontend/app/dist/index.html` restored it; `git status --porcelain` empty; `git describe`
   `v2.0.0-alpha.4`. `SM_FFT=pocketfft scripts/dev-rpm.sh` exit 0, once: daemon `version: 2.0.0-alpha.4,
   FFT: PocketFFT (CGO, dynamically linked)`, RPM `2.0.0~alpha.4-1`, 9,625,351 bytes, SHA-256
   `cebcd305e99db31671ca0dbdeace70f84064e38a150f744f497aec9059c5532b`. Copied to
   `build/private/station-manager-2.0.0~alpha.4-1.x86_64.rpm` (0600; the destination did not exist
   before), SHA re-verified after the copy. The extracted `smd` carries the version string
   `2.0.0-alpha.4` and no other release version. The build again rewrote `dist/index.html` afterwards,
   as expected; the version had been fixed before it. The worktree, with its `.env` copy, was then
   removed (`git worktree remove --force`, pruned).
4. **2026-10-22 05:30Z — A1-04 and A1-05 (host-scratch), by the agent.** Scratch `~/sm-a1-scratch`
   (0700, absent before). The RPM's SHA-256 was verified, `./usr/bin/smd` extracted with `rpm2cpio`, and
   its SHA-256 matched the header's (`08aebee9…6426b`). Every run was
   `SM_WORKING_DIR=$S/wd smd config-check --config <copy>`, with `$S/wd` an empty 0700 directory.
   - *A1-04.* The live `config.json` copied 0600 (`cmp` identical); the mutations added the top-level
     `typo_key_a1_04` and the nested `bridge.typo_nested_a1_04`, each on its own copy.
     - Control: exit 0. `config-check: config.live.json — no unrecognised keys; the file loads and
       validates as startup would; 0 enabled forwarder(s) construct (legacy entries; destination
       bindings live in each archive and are checked at startup). Not checked: databases, listeners
       and other runtime dependencies.` The 0 is expected at config v6, where an entry is enabled only
       while it holds its deprecated v5 fields (`cmd/smd/config_check.go`).
     - Top-level: exit 1. `config.top.json has 1 unrecognised configuration key(s) — startup would
       refuse (values omitted): typo_key_a1_04`.
     - Nested: exit 1, the same wording, naming only `bridge.typo_nested_a1_04`.
   - *A1-05.* Appendix 2 of the alpha.3 record. The alpha.1-era file from
     `station-manager-pre-alpha2-20260905T1114Z.tar.gz` reads version 2, qrzcq `enabled: false`,
     `action_filter: null`. Fixture a keeps version 2 and sets qrzcq's filter to
     `["insert","update","delete"]` and smcloud's `allow_insecure_http` to true. Fixture b is a with the
     filter `["update","insert","delete"]`.
     - Fixture a: exit 0, "the file loads and validates as startup would; 3 enabled forwarder(s)
       construct".
     - Fixture b: exit 1. `startup would refuse: invalid config (invalid_forwarder): forwarder
       "qrzcq": type "qrzcq" does not support action "update" (supports [insert])`.
   - *Disclosure check.* Every string value of six characters or more in each input was searched for in
     its output; matches were listed by key path only. A1-04's control matched
     `qso_archives.0.ownership` (the word in the fixed text "legacy entries"), and A1-05's permutation
     matched only `action_filter` entries (the action names the refusal names by design). No unexpected
     matches among the checked string values. Strings shorter than six characters and non-string
     values were outside the scan.
   - *Nothing written.* A SHA-256 manifest of the scratch directory was identical before and after each
     of the five runs, and `$S/wd` stayed empty.
   - *Live untouched* (before / after the whole step, identical):
     - live `config.json` SHA-256 `899c9978…7d2c08`, mtime 2026-10-22 07:00:55.597 +0200;
     - `smd` `ActiveState=active`, `MainPID=4606`, `NRestarts=0`;
     - the working directory's sorted file-name list hashed `7956b952…` (76 names).
   - *Clean-up.* The six config copies were removed with `shred -u`, and the scratch directory deleted.
     `$HOME` is btrfs, a copy-on-write filesystem, and `shred` "assumes the file system and hardware
     overwrite data in place" (shred(1)), so the overwrite is not assured there. Files deleted; secure
     erasure unverified.
