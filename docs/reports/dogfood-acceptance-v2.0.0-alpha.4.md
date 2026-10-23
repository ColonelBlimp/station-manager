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
Clean-install environment: the isolated RPM guest `sm-alpha4-clean` on `qemu:///session` (Appendix 3,
approved with amendments 2026-10-22)
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

Ready to deploy: FAIL — Codex review requested by the operator, 2026-10-23; Findings #5 and #6
Operator/date: no operator deploy sign-off

Dogfood accepted: HOLD — fix the Session archive boundary and false-success export before a new candidate
Operator/date: Codex release-disposition ruling requested by the operator, 2026-10-23; no waiver
Residual waivers: the alpha.2 B1-01 waiver (Finding #6), carried from alpha.3, is retired by A1-05
plus B1-01 below
Follow-up destinations: Findings #1–#8 below; W-0021 (archive startup, Session isolation and export
correctness), W-0012 (Country presentation), W-0018 (install-guide correction), W-0009 (existing packaging residual), and
`docs/v2-design/config.md` §13.4 (wording). Priority remains in `docs/backlog.md`.

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

**Case review, 2026-10-22 (Codex, requested by the operator):** A1-02 and A2-01…A2-08 pass
their stated outcomes on the recorded operator execution and supporting daemon evidence. These
are case-level review rulings, not the operator’s Gate A or Gate B sign-off; neither gate is
complete and no waiver is entered.

**Profile deviation:** Appendix 3 item 6 was not followed. Both lanes used the normal Firefox
profile; the execution record reports that `127.0.0.1:18080` was unused before A2-02, then the
same tab/origin was reused for A2-08. Separate fresh-profile isolation is **not proved**. This
does not negate the reported first-run actions, the matching version readings, or the independent
install/uninstall and migration evidence. No blanket rerun is required for those assertions.
A2-08 proves retention of the three daemon QSOs; it does not prove a fresh Session panel or
archive-switch behavior. Finding #5’s archive concern needs a distinct B2 case before acceptance.

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `A1-01` | yes | **record** — clean scratch worktree at tag `v2.0.0-alpha.4` (`3f7425ed`) | Build once with `SM_FFT=pocketfft scripts/dev-rpm.sh` after `task ci:local`; restore the committed `frontend/app/dist/index.html` first if the CI mirror rewrote it (alpha.3 Finding #1); record filename, SHA-256 and RPM version; keep the versioned copy | One artifact; RPM version `2.0.0~alpha.4-1`; `ci:local` exit 0; SHA recorded in the header | A `-dirty` or `alpha.4-N-g…` version; a second build overwriting the copy | Execution log #1–#3: `ci:local` exit 0. The CI mirror rewrote the tracked `dist/index.html` again (alpha.3 Finding #1); the committed file was restored before the build, `git status --porcelain` empty, `git describe` `v2.0.0-alpha.4`. One build: `2.0.0-alpha.4`, RPM `station-manager 2.0.0~alpha.4-1 x86_64`, 9,625,351 B, SHA-256 `cebcd305…c5532b`, copied to the versioned path (0600) with the SHA re-verified | none | **PASS** — operator, 2026-10-22: independently verified the tag's commit, the RPM's permissions, size, SHA-256, package version, private-build marker and packaged `smd` hash; CI results accepted from the execution record. Finding #1 stays open and does not block this artifact (the committed file was restored before the build, and the build regenerates the SPA after the version is fixed) |
| `A1-02` | yes | **clean** — the A2-01 install, candidate installed and started | Verify the RPM SHA before installing; read `GET /v1/version`, the sidebar badge, the startup log line and `rpm -q` | All four read `2.0.0-alpha.4`; schema `{18, dirty:false}` after the first start | A badge from a cached tab; a dev-build version string | Execution log #5: RPM SHA verified before install; `rpm -q` `station-manager-2.0.0~alpha.4-1.x86_64`; `GET /v1/version` `2.0.0-alpha.4`, `env: release`, schema `{18, dirty:false}`; the startup line `smd starting` carries `"version":"2.0.0-alpha.4"`. Sidebar badge read `2.0.0-alpha.4` by the operator (execution log #6): all four agree | none | **PASS** — Codex review, 2026-10-22: All four version readings agree and schema 18 is clean; the profile deviation does not contradict this identity check. |
| `A1-03` | yes | **record** — delta drafted above | Review the release delta against the candidate, completing the configuration and API items | Delta accepted or corrected here | Silent scope creep in the delta | Configuration, API and install items completed by the agent from the code and canonical references at both tags (route and JSON-key lists compared); one reference defect found (`config.md` line 69 said version 5, corrected on `main`, no rebuild). Operator review 2026-10-22: pending, with corrections — three fields wrongly labelled new, the archive-switch journey, the switch and reconcile failure semantics, the journeys beyond component names, the manual inventory, and the route count (54 → 61); all applied | none | **PASS** — operator, 2026-10-22: the revised delta resolves the review findings, including the B2 archive-switch wording; the reference fix is limited to 5 → 6 and needs no candidate rebuild |
| `A1-04` | yes | **host-scratch** — 0700 scratch with a copy of the live `config.json`; the candidate `smd` extracted from the RPM (alpha.3 record, Appendix 1); live service untouched | `smd config-check --config` on the untouched copy (control), then on a top-level and a nested unknown-key mutation | Control exits 0; each mutation exits non-zero naming only the key path; nothing started, bound or written | Control non-zero: the candidate would refuse to start on the station. Fix and re-run; no waiver possible | Execution log #4: control exit 0 ("loads and validates as startup would; 0 enabled forwarder(s) construct", as v6 keeps uploads in each archive's bindings); top-level mutation exit 1 naming only `typo_key_a1_04`; nested exit 1 naming only `bridge.typo_nested_a1_04`, "(values omitted)"; no unexpected matches among the checked string values (six characters or more); nothing written; live config, service and file names unchanged | none | **PASS** — operator, 2026-10-22: the valid control succeeds and both independent mutations fail with the intended key-path diagnostics; the hashes, empty working directory and unchanged live service support isolation. Boundaries: zero enabled legacy forwarders validates neither archive bindings nor upstream connectivity (later cases cover them); the disclosure scan excluded shorter strings and non-string values |
| `A1-05` | yes | **host-scratch** — as the alpha.3 record's Appendix 2, with this artifact's `smd` | The CC-5 reconcile on the alpha.1-era file, then the permutation control | Copy exits 0; the permutation exits non-zero naming `qrzcq` and the unsupported action | Both exit 0; the copy refused for `allow_insecure_http` | Execution log #4: fixture (config version 2, qrzcq `["insert","update","delete"]`, scratch-only `allow_insecure_http`) exit 0, "3 enabled forwarder(s) construct"; permutation `["update","insert","delete"]` exit 1: `forwarder "qrzcq": type "qrzcq" does not support action "update" (supports [insert])`; nothing written; live untouched | none | **PASS** — operator, 2026-10-22: the reconstructed legacy fixture succeeds while the permutation reaches validation and fails for the intended unsupported action, distinguishing the repair from indiscriminate acceptance. The alpha.2 B1-01 waiver still awaits B1-01 |
| `A2-01` | yes | **clean** — the guest `sm-alpha4-clean`, user `smtest` (Appendix 3) | Install per `install.md`; enable/start the user service; note the lingering step | Service active; health and version answer; logs readable; browser reaches `/` | Loopback-only bind with a differing guide URL; lingering omitted so the service dies at logout | Execution log #5: `dnf install` exit 0 (SHA verified in the guest); `daemon-reload` and `enable --now` exit 0; service `active (running)`; `/v1/healthz` `{database ok, logging ok, status ok}`; `/v1/version` `2.0.0-alpha.4`; listener `127.0.0.1:8080` only, as the guide says; `smd.log` readable (26 info lines, one warn: Finding #1). Lingering: the guide's `loginctl enable-linger "$USER"` was refused ("Access denied") because the Cloud image has no polkit; enabled with `sudo` instead (lane deviation, not a product defect). Browser reaching `/`: HTTP 200 through the tunnel, then the operator executed setup in execution log #6 | none | **PASS** — Codex review, 2026-10-22: Install, service, health, logs and browser access are demonstrated. The sudo lingering adjustment is recorded; execution log #7 also shows startup before login. Findings #1 and #2 remain follow-ups. |
| `A2-02` | yes | **clean** — first browser open after A2-01 | Open the app | Only the first-run welcome surface; tab titled "Welcome"; no operate/logbook chrome | Operate chrome visible before setup | Operator executed (execution log #6) | none | **PASS** — Codex review, 2026-10-22: Accept the operator-reported first-run welcome/title/chrome check; the first lane used the previously unused origin, not the promised fresh profile. |
| `A2-03` | yes | **clean** — first-run callsign form | Enter invalid, empty, lowercase/whitespace-normalised, then valid callsign | Each state understandable; the normalised form shown | A silent accept of the invalid form | Operator executed (execution log #6) | none | **PASS** — Codex review, 2026-10-22: Accept the operator-reported invalid, empty, normalised and valid callsign sequence; no contrary outcome is recorded. |
| `A2-04` | yes | **clean** — valid callsign submitted | Complete setup | Default logbook created; both **Open Settings** and **Start logging** offered | Only one journey offered | Operator executed (execution log #6) | none | **PASS** — Codex review, 2026-10-22: Accept the operator-reported setup and both offered journeys; the persisted default logbook corroborates creation. |
| `A2-05` | yes | **clean** — setup complete | Reload the browser; restart the daemon; reopen | Setup stays complete; the bare `/` lands on Phone/CW (first use, no Dashboard) | Welcome surface returns; a Dashboard placeholder | Operator executed (execution log #6), across two agent restarts of the guest daemon (PIDs 1564 → 1984 → 2072, each `smctl restart` exit 0, `NRestarts=0`); `setup_complete` true and `default_logbook_id` 1 persisted | none | **PASS** — Codex review, 2026-10-22: The reported reload/reopen journey and two daemon restarts preserve setup completion; no Welcome or placeholder regression is reported. |
| `A2-06` | yes | **clean** — setup complete, guide and manual only | Find Settings, the embedded Manual (opens in a new tab) and its Station Events chapter, and log a first QSO using only the guide and manual | All reachable without repository knowledge; Station Events in the sidebar between Logbook and Settings, empty state correct | A step needing repository-only knowledge (record it) | Operator executed (execution log #7); the first test QSO stored: logbook `Default` count 0 → 1, one QSO on 20m SSB under the logbook's callsign; no new warning or error | none | **PASS** — Codex review, 2026-10-22: Accept the reported Settings, Manual and Station Events journey, completed by the first stored test QSO in execution log #7. |
| `A2-07` | yes | **clean** — installed, test QSOs in the logbook | Uninstall per the guide | Package removed; operator data deliberately retained as documented | Data directory deleted by the package | Execution log #7: §10 followed. `systemctl --user disable --now smd` exit 0 (enable symlink removed); `sudo dnf remove` exit 0; `daemon-reload` exit 0; the unit, both binaries and the package gone, nothing on 8080. Data retained: `config.json`, `db/` (the QSO database `integrity_check` ok, 1 QSO) and `log/`. `loginctl disable-linger "$USER"` refused as in A2-01 (no polkit), done with `sudo`. An empty, unowned `/usr/share/doc/station-manager/manual/` is left behind | none | **PASS** — Codex review, 2026-10-22: The package, binaries and unit are removed and data retained. Finding #3 is the existing empty-directory packaging residual, not a retention failure. |
| `A2-08` | yes | **clean** — a second fresh working directory in the guest: the frozen alpha.2 RPM (SHA `7b2fa20a…c24856`) installed and set up, a few test QSOs, daemon running | Upgrade per `install.md` §7 to the candidate | `config-check` exit 0; config 3 → 6 and schema 8 → 18 applied once at the first start; setup bypassed; QSOs, logbook and config retained | Migrations re-run on every start; a refusal at the restart the preflight did not name | Execution log #8: before, alpha.2 schema 8, config 3, setup complete, logbook `Default` with 3 QSOs. `dnf install` upgraded in place while alpha.2 kept serving (same PID); `smd config-check` exit 0, "no unrecognised keys; loads and validates", config byte-identical; `daemon-reload` and `restart` exit 0. After: `2.0.0-alpha.4`, schema `{18, dirty:false}`, config 6, archive Home (legacy) adopted, bindings seeded once, `config.v5.json` written (0600, version 5), setup still complete, the same 3 QSO UUIDs. A second restart re-ran nothing: `config.json` unchanged, no adoption, seed or strip line. No warning or error. The log names neither transition (Finding #4). In the browser (operator, normal Firefox profile, same tab/origin reused and reloaded after the upgrade): no Welcome page, lands on Phone/CW, badge `2.0.0-alpha.4`, Logbook shows the 3 QSOs; the Phone/CW Session panel shows 4 QSOs (Finding #5) | none | **PASS** — Codex review, 2026-10-22: The preflight, one-time migrations, retained setup/logbook and unchanged three QSO UUIDs pass. Finding #4 is diagnostic wording; Finding #5 is confirmed browser-session carry-over. Fresh-profile isolation and archive switching are not claimed. |
| `A3-01` | yes | **live** — daemon stopped | Refresh the recovery backup of the complete working directory outside the repository | Backup exists and is listable; SHA-256 recorded | Backup copied into the repository tree | pending | none | pending |
| `A3-02` | yes | **host-scratch** — the A3-01 archive | Restore a copy into scratch; open read-only (`immutable=1`, a verified unchanging copy); remove it | Integrity `ok`; counts equal A3-03 | Restore needs the live directory | pending | none | pending |
| `A3-03` | yes | **live** — pre-upgrade | Record durable-record counts: QSOs per logbook per archive, upload-queue rows by status, `operator_event` rows per category, bindings per archive | Numbers recorded here without contents | Counting after the upgrade began | pending | none | pending |
| `A3-04` | yes | **live** — pre-upgrade | Confirm the preserved previous RPM (`2d3a0ec9…`) and record the rollback: package downgrade with `rpm -Uvh --oldpackage`; no data downgrade needed (same heads) | Artifact present; procedure named in the header | Only the RPM preserved | pending | none | pending |
| `A3-05` | yes | **live** — before B1 | Confirm rollback time is reserved before the station is next needed | Window recorded | Upgrade started with a session imminent | pending | none | pending |
| `A4-01` | yes | **record** — delta reviewed (A1-03) | Ratify this inventory, including the B2 additions below; confirm the A4 parameters | Inventory ratified | An execution before ratification | Revision 2 reviewed against the frozen tag; Session choice recorded in ADR 0094; restore CLI/dry-run expectations, missing-account fixture, held Retry state and browser-response evidence corrected below; A4 parameters accepted with Firefox 158.0 as reported by the operator | none | **PASS** — Codex inventory review requested by the operator, 2026-10-22. This accepts the case definitions, not their results or either release gate. Guest B2-09–11 must precede A3; the expected Session failure needs triage before live upgrade |

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
- **Session across archive changes** (Finding #5, operator ruling 2026-10-22): Session rows and
  count, and edit and export, across an archive switch, with a same-archive reload as the control.

#### B2 cases — revision 2 reviewed at A4-01 (Codex, 2026-10-22); execution recorded 2026-10-23

Revised after Codex review of revision 1 (A4-01 then kept pending): B2-10's expected behaviour
stated first, B2-05 and the retry action made record cases, B2-06 and B2-07 approved for the guest
with observable checkpoints, B2-15 and B2-16 rerun on the candidate binary, B2-13's fixture
corrected, B2-12's queue comparison loosened, and independent actions split into subcases.

**Sequences and profiles.** **clean-guest** is the `smtest2` lane of `sm-alpha4-clean`, already on
the candidate with Home (legacy) holding three test QSOs, so archive switching never touches the
station. Each sequence opens **one fresh Firefox profile**, created empty for it and actually used:
`~/sm-guest/ff-b2a` for B2-01…B2-08 and `~/sm-guest/ff-b2s` for B2-09…B2-11, where **the same tab
is kept throughout**. The agent records each profile's file count after use, to show it was opened.
**Durable-count baselines** (QSOs per logbook per archive) are re-read after every intentional test
QSO and before each case that compares counts.

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `B2-01` | yes | **clean-guest** — Home active, one logbook | Open Settings → Archives; read the header | Home listed: legacy, active, logbook `Default` with the baseline count, contents status shown; the header reads `Archive Home` above the logbook | A count from a stale summary; the header archive missing | 2026-10-23 07:40 local, profile `ff-b2a` (fresh, empty before launch), instance `dmbdmxvkgyw7`. Header: `Archive Home` above `Logbook Default (3)`, badge `2.0.0-alpha.4`; Session rail with no count. Settings → Archives: Home `Active`, 160 KB, last written 22/10/2026 14:47:47; logbook `Default` [Default] `M0CMC` `3 QSOs`; no contents note. `GET /v1/qso-archives`: ownership `legacy`, state `active`, `contents_status` `current`, `qso_count` 3. **Wording gap:** the SPA shows neither ownership (no Ownership column, fresh-install ruling 2026-09-26, `ArchivesSection.svelte:127`) nor a `current` status (a note appears only for `unknown`/`stale`, `:185`/`:219`); both were read from the API | none | pending — operator ruling |
| `B2-02a` | yes | **clean-guest** — B2-01 | Try to create an archive with one required field empty | No archive is created; the SPA either refuses, naming the field, or keeps Create disabled (record which); the catalogue is unchanged | An archive created with an empty field | 2026-10-23, `ff-b2a`: with the callsign empty, **Create archive stays disabled** (the SPA's choice; `ArchivesSection.svelte` `canSubmit` requires label, first logbook and callsign non-blank, `:53-55`, `:284`); no request possible. Afterwards `GET /v1/qso-archives` lists only Home (active, `Default` 3); instance `dmbdmxvkgyw7` unchanged | none | pending — operator ruling |
| `B2-02b` | yes | **clean-guest** — B2-02a | Create an archive with every field valid | The archive appears inactive, its logbook at 0 QSOs; no restart (same `instance`) | The archive appears active, or a restart happens on create | 2026-10-23, `ff-b2a`: created label `Contest`, first logbook `B2 Log`, callsign `M0CMC`. Listed `Inactive`, 148 KB, with an Activate button; `B2 Log` [Default] `M0CMC` `0 QSOs`. API: id `01a1640f-815a-75e4-bae2-b9ef8d86f8c3`, ownership `managed`, state `inactive`, `contents_status` `current`. Instance `dmbdmxvkgyw7` unchanged (no restart). **Lane deviation:** the guest clock read 2026-10-22 14:42Z against the host's 2026-10-23 05:44Z (about 15 h behind, after the host was off overnight), so `modified_at` reads 2026-10-22T14:41:40Z. Corrected at the operator's direction before B2-03: `chronyc makestep` did nothing (every source rejected, "Root distance … exceeds maxdistance"), so guest `chronyd` was restarted; it stepped the clock by +54,161.99 s at 2026-10-23 05:46:03Z (`makestep 1.0 3`), guest and host then agreeing to the second; daemon instance unchanged | none | pending — operator ruling |
| `B2-03` | yes | **clean-guest** — an unlogged Phone/CW entry typed, not logged | Activate the new archive; answer **Cancel** | The prompt names the target and this window's entry, warns that unlogged work in other windows is lost, and says TX must be idle; after Cancel the entry is kept and nothing changes (same `instance`, Home still active) | The entry lost, or the switch proceeding, on Cancel | 2026-10-23 07:47 local, `ff-b2a`, unlogged `B2TEST` on Phone / CW. Native confirm: "Switch to the archive “Contest”? This window has an unlogged QSO on Phone / CW: B2TEST. OK discards this window’s entry and switches; Cancel keeps it. Station Manager restarts to open it (about 5 seconds; the connection drops briefly), then every open window reloads: unlogged work in any other open window — a Phone / CW QSO, an FT8 / FT4 exchange not yet completed, unsaved Settings edits — is lost. Logged QSOs are never affected. Transmit must be idle and FT8 disarmed; nothing transmits until the restart completes." After **Cancel**: `B2TEST` still in the Callsign field (operator); instance `dmbdmxvkgyw7` unchanged; Home `active`, Contest `inactive`; guest `config.json` has `active_qso_archive_id` = Home and no `pending_qso_archive_id` | none | pending — operator ruling |
| `B2-04` | yes | **clean-guest** — as B2-03, plus a second tab on the Logbook | Activate again; answer **OK** | The entry is discarded; the daemon restarts (a new `instance`); both tabs reload; the header and `GET /v1/version` name the new archive; Station Events in the new archive shows `archive.activated` | A tab left running against the old archive; the event recorded in the old file | 2026-10-23 07:53 local, `ff-b2a`: tab 1 on Settings → Archives with `B2TEST` unlogged, tab 2 opened at `/` and left on **Phone / CW** (not Logbook as written; a recorded deviation, the reload check unaffected). Activate Contest → **OK**. Instance `dmbdmxvkgyw7` → `dmbzfzpap7oo` (guest journal: one scheduled `smd.service` restart); `GET /v1/version` archive `Contest`/`managed`. Both tabs reloaded by themselves (operator) and their headers read `Archive Contest`, `Logbook B2 Log (0)`; tab 1's Phone / CW form empty, `B2TEST` discarded (operator). Station Events in Contest: one event, "Archive switched" (Notification, `Contest`, 23/10/2026 07:53:07, alpha.4); API `archive.activated`, id 1, `occurred_at` 2026-10-23T05:53:07Z, `detail.archive_id` = Contest | none | pending — operator ruling |
| `B2-05` | yes | **record** — the candidate's automated evidence | Cite `frontend/app/src/lib/config/archives.svelte.test.ts`: "A4 a Log in flight refuses before the prompt" (no prompt, no request, the "being logged … wait for it to finish" toast) and "A4 a Log started while the prompt is open refuses before the request" (prompt shown once, no instance read, no request, not left activating), run by `task ci:local` at `3f7425ed` (vitest 2158/2158) and CI run 39612261239 | Both assertions present at the tag and passing in that evidence | Described as a manual browser pass — it is not one | pending | none | pending |
| `B2-06` | yes | **clean-guest** — B2-04's state reversed (Home active); the agent watching the guest | The operator activates the other archive. The agent captures the browser’s `202` response in DevTools with the network log preserved across reload, then stops the guest daemon **before** the browser observes the replacement instance (checkpoint: no completed browser version response with a different `instance`; daemon logs only corroborate this), and keeps it stopped until the overlay and the logging block are seen; then starts it while the recovery watcher is still active | The page is gated: "An archive switch is unresolved — reload the page before logging or transmitting"; logging is blocked; after the start, the page reloads by itself once the new instance answers. If the browser saw the new instance before the stop, the fault was **not exercised**: record that and repeat. Automatic reload is not promised after the watcher expires | Logging allowed against an unproven archive; a reload claimed from a missed timing window | 2026-10-23, `ff-b2a`, two tabs; Home active first by a normal activation (instance `dmbziomtxtsj`, PID 2667). Guest watcher (`~/b2-06-watch.sh`, armed 05:57:16Z) polled `smd`'s SubState/MainPID every 50 ms. Activate Contest → OK: DevTools (Persist Logs) `POST …/activate` **202** (6 ms). `smd` exited status 3 at 05:58:22.814Z; the watcher saw `auto-restart` and `systemctl --user stop` completed at 05:58:22.870Z, inside `RestartSec=5s`; no `Started` line followed. **Checkpoint met:** every later `version`/`events` request failed `NS_ERROR_NET_RESET`, 0 B — no completed version response with a different instance. After the ~30 s wait the page showed the overlay "Archive binding unproven — Station Manager cannot tell which archive the daemon serves this page’s data from. The daemon did not answer as a new instance within the wait. Logging and transmitting are paused so nothing lands in the wrong archive. Reload to continue on the archive the daemon is serving." with **Reload now** (not pressed); a Phone / CW log attempt was blocked (operator). (The case's quoted string is the refusal text, `archives.svelte.ts:63`; the overlay heading differs.) Started `smd` at 06:00:13.0Z; it answered at 06:00:13.5Z as `dmbzlfbs9iww`, archive Contest. Tab 1 then reloaded within the watch (screenshot 06:00:17Z): header `Archive Contest`, `B2 Log (0)`, overlay gone, version 200 after the reload; the operator confirmed both tabs **reloaded by themselves**, without Reload now, and tab 2 reads Contest. The refusal wording of the blocked log attempt was not captured |  approved method (Codex review, 2026-10-22): guest-only daemon interruption | pending |
| `B2-07a` | yes | **clean-guest** — activate Home normally and verify it is active (`GET /v1/version`, Archives) | The agent stops the daemon, preserves `config.json`, moves aside the inactive candidate archive's file **and any `-wal`/`-shm` sidecars**, and sets only `pending_qso_archive_id` to that archive; then starts the daemon | Fallback to the **active selector** (`cmd/smd/main.go` `startGenerations`, which resolves the active archive, not Home by name): Home serves; the candidate's entry shows `archive_file_missing`; Station Events in Home shows `archive.activation_failed`; pending cleared | The daemon fails to start, or serves the missing archive | 2026-10-23. Home active by a normal activation (instance `dmbzmwcl3v5a`; `config.json` active = Home, no pending). Agent, guest only: `systemctl --user stop smd` 06:02:27.970Z (dead); `config.json` copied to `~/b2-07/config.json.pre` (0600); `db/qso-archives/01a1640f-815a-75e4-bae2-b9ef8d86f8c3.db` (151,552 B, SHA-256 `2d25e60c…408663e`; **no `-wal`/`-shm` sidecars existed**) moved to `~/b2-07/`; only `pending_qso_archive_id` set to Contest (JSON key diff against the copy: that key alone; mode 0600). Started 06:02:33.664Z → instance `dmbzn7y19p70` serving **Home** (legacy). `GET /v1/qso-archives`: Home `active`, `Default` 3; Contest `inactive`, `last_activation_code` `archive_file_missing`, `last_activation_error` "The archive's file is missing.", `contents_status` `stale`. Home's Station Events: id 3 `archive.activation_failed` (error, 06:02:33.679Z, Contest, `archive_file_missing`). `config.json`: active = Home, pending cleared, Contest entry `last_activation_error` `archive_file_missing`. Browser (operator screenshots 08:03 local): header `Archive Home`, `Default (3)`; Settings → Archives shows Contest with a ⚠ (its tooltip is "Last activation failed: {error}", `ArchivesSection.svelte:150-163`; not hovered), Size and Last written "—", Activate offered, and the note "Counts may be out of date — the file changed since it was last open" (**candidate finding:** the file is missing, not changed). Station Events (Home): "Archive switch failed" (red, Notification) "Contest · the archive file is missing", 23/10/2026 08:02:33 |  approved method (Codex review, 2026-10-22): stopped-guest fixture edit | pending |
| `B2-07b` | yes | **clean-guest** — B2-07a done | The agent stops the daemon, restores the moved file and sidecars, verifies `config.json` against the preserved copy except the cleared pending selector and the recorded error, starts, and re-establishes B2-08's starting state (the other archive active, by a normal activation) | The fixture is fully undone; B2-08's start state verified | B2-08 run on a broken fixture | 2026-10-23, agent, guest only: stopped `smd` 06:03:56.081Z; moved the Contest file back (SHA-256 matches the recorded `2d25e60c…408663e`; no sidecars to restore); `config.json` against the preserved copy: only Contest's entry differs, `last_activation_error` `archive_file_missing` (the recorded error); no pending selector in either. Started 06:04:00.413Z → `dmbzobsq64xc`, Home active, `Default` 3; Contest `inactive`, 151,552 B, `B2 Log` 0, still carrying `archive_file_missing` and `stale` until its next activation. `~/b2-07` (config copy, checksum) removed after the comparison. B2-08's start state then re-established by a normal activation of Contest (operator, 08:06 local): instance `dmbzq5yaclhe`, archive Contest; Contest `active`, `current`, no activation error, `B2 Log` 0; Home `inactive`, `Default` 3; `config.json` active = Contest, no pending. Contest's Station Events: three "Archive switched" (07:53:07, 08:00:13, 08:06:24) |  as B2-07a | pending |
| `B2-08` | yes | **clean-guest** — the other archive active | Activate Home again | Home serves: legacy entry, its path unchanged, the baseline QSOs and their UUIDs intact | Home re-adopted as a new archive, or its counts changed | Baseline while Home was inactive (2026-10-23 ~06:07Z, agent, guest): `db/station-manager.db` read with SQLite `immutable=1` (SHA-256 `b84552aa…bce50e0` identical before and after; no sidecar created): `qso` rows 1 `01a163a6-010d-7d61-bca8-3cd679030cbd` 7Q7EB, 2 `01a163a6-1ccd-786b-81ee-4f83e0f5b0d8` 7Q5MLV, 3 `01a163a6-3f5c-7407-93e5-3ba031319348` 7Q7CT, none deleted; catalogue entry id `01a163a7-0f04-7f69-85e9-255664b58d11`, `legacy`, path `…/db/station-manager.db`. After the operator activated Home (both tabs reloaded): instance `dmbzrncwb8ft`, `GET /v1/version` archive Home/`legacy`; the **same** catalogue entry (id unchanged, `legacy`, path `/home/smtest2/.local/share/station-manager/db/station-manager.db` unchanged, no second Home entry); `Default` 3, `current`, no activation error; `GET /v1/logbook/1/qso` returns exactly the three baseline UUIDs and calls; Contest `inactive`, `current`, `B2 Log` 0; `config.json` active = Home, no pending |  none | pending |
| `B2-09` | yes | **clean-guest** — Home active; profile `ff-b2s`, one tab; two QSOs logged in it (baselines updated) | Control: reload the same tab, without switching; open a row's edit, change the comment, save, reload; export | Session rows and count unchanged across the reload; the edit saves to Home (`GET /v1/qso/{uuid}` shows the new comment after the reload); the downloaded ADIF holds both records | The list emptied by a reload; an edit not persisted | 2026-10-23. `ff-b2a` closed after B2-08 with **341 files** (used). `ff-b2s` created empty (0700) and launched alone, one tab (244 files while in use). Home active, instance `dmbzrncwb8ft`. Operator logged two Phone / CW QSOs; Session shows **2**. New baseline: `Default` **5** — the two session QSOs `01a16763-d35e-787d-8cda-0af355a277fc` 7Q5MLV 06:12:29Z and `01a16763-ee4e-77f4-b228-d31697a11ab6` 7Q7CT 06:12:42Z (callsigns repeat two A2 rows; told apart by UUID and time), plus the three A2 rows. **Attempt 1 void:** a host power cut closed the `ff-b2s` Firefox before the control steps, so its `sessionStorage` list was lost; its two QSOs remain in Home as baseline rows. After power returned, the guest VM showed as running, but the guest clock was 17,011 s slow (guest 07:20:35Z against host 12:04:06Z). Restarting chronyd stepped it by +17,011.21 s at 12:04:12Z, matching the host to the millisecond. The tunnel was reopened; the daemon was still instance `dmbzrncwb8ft` on Home with `Default` 5. Attempt 2 uses a fresh profile, `ff-b2s2` (created empty, 0700; launched alone, one tab, 62 files while in use). The operator logged two Phone / CW QSOs; Session shows **2**. New baseline: `Default` **7**. The attempt-2 session QSOs are `01a168c1-04e0-7047-ad8d-da81db1fe366` ZS4JAN 12:33:53Z and `01a168c1-2998-7db9-bab1-fc7b0ce4d302` JA8EPO 12:34:09Z, with no comment on either. **Control:** the operator edited JA8EPO's comment in the Session panel to `B2-09 edit` and saved, then reloaded the same tab; Session still shows **2**. `GET /v1/qso/01a168c1-2998-7db9-bab1-fc7b0ce4d302` returns comment `B2-09 edit`; ZS4JAN is unchanged, with no comment. The Session ADIF export `~/Downloads/session-20261023-123619.adi` (948 B, SHA-256 `353be63337c02c4cc43aecb19858cd562d551fc2d3dec724c5cef97cf2dd2fe7`) holds exactly 2 records, JA8EPO 20261023 123409 with `COMMENT` `B2-09 edit` and ZS4JAN 20261023 123353, and none of the other five Home rows. The ADIF carries no UUID, so the records are matched by call, date and time. **Side observation (candidate finding, operator-reported):** after the edit, the Session panel's Country column read `Unknown` for JA8EPO but stayed empty for the unedited ZS4JAN. The daemon stores `country` `Unknown` on both. At logging, the session row takes Country from the browser's own enrichment (`main.ts` `addSessionQso`, `e?.country ?? ''`), which is empty in this network-isolated guest. The edit path writes the daemon's merged row back (`sessionEdit.svelte.ts`). So the same stored state displays differently depending on whether the row was edited. The B2-09 outcome is unaffected | Guest clock stepped after the power cut (guest-only; same remedy as B2-02b); attempt 1 voided by the power cut | **PASS** — Codex review, 2026-10-23; attempt 2, reload/edit/export outcomes met; Finding #7 is separate |
| `B2-10` | yes | **clean-guest** — B2-09; switch to the other archive in the same tab (it reloads); log one **distinguishable** QSO there | Read the Session panel and the rail badge; try to edit a Home row; export, then **open the downloaded file**; check the FT8 "worked this session" marker for a Home row's callsign where FT8 is available | **Expected (Codex design ruling requested by the operator, 2026-10-22; [ADR 0094](../decisions/0094-retain-session-lists-per-archive-within-each-tab.md)):** retain a separate Session list per archive within each tab, identified by archive UUID. After the switch the panel, badge, edit and export, and the FT8 "worked this session" check show only this archive's rows: the one new QSO. Returning restores Home's list (B2-11). **Against this candidate** nothing clears `sm.session.qsos` (`session.svelte.ts`), and the session export skips UUIDs it cannot find with only a log warning (`internal/api/handler_session_export.go`, `fetchSessionQsos`), so a mixed list can download successfully yet incompletely: the downloaded records, not the toast, decide | A successful but incomplete export; Home's rows shown or marked as worked in the other archive | 2026-10-23: **setup slip, attempt 1:** the archive switch was not made before logging. The daemon was still `dmbzrncwb8ft` on Home, so VK9XX (`01a168c5-9302-7486-8f3d-c0ab5332579e`, 12:38:55Z) was filed in Home `Default`, which now holds **8**. Session showed 3 rows (VK9XX, JA8EPO, ZS4JAN) with badge 3: correct for a Home tab, so it is not B2-10 evidence. VK9XX joins Home's session list as baseline; B2-10 then switches and logs a different call. **Attempt 2:** the operator activated Contest from Settings → Archives in the `ff-b2s2` tab; the tab reloaded by itself. At 12:40:36Z the daemon was instance `dmc83sqm9foc` serving Contest `01a1640f-815a-75e4-bae2-b9ef8d86f8c3` (managed), logbook `B2 Log` holding 0 rows; Contest's events 2–4 are `archive.activated`. The operator logged VK0YY in Contest: `B2 Log` holds **1**, `01a168c7-800a-7e1d-a4f8-b9e68f72a584` VK0YY 12:40:47Z. **Session panel shows 4 rows, VK0YY, VK9XX, JA8EPO and ZS4JAN, and the rail badge reads 4** (operator screenshot; JA8EPO still shows Country `Unknown` from its B2-09 edit). The three Home UUIDs (`01a168c5-9302…`, `01a168c1-2998…`, `01a168c1-04e0…`) return **404** from `GET /v1/qso/{uuid}` on the Contest daemon. Panel and badge **contradict ADR 0094** (expected: 1 row, VK0YY). **Home-row edit:** clicking ZS4JAN opened no dialog; the panel showed in red "Cannot load ZS4JAN for editing: QSO not found". No write could be attempted, and `B2 Log` still holds 1. The panel offers a row that it then cannot edit. **Export:** the download from a 4-row panel succeeded. `~/Downloads/session-20261023-124438.adi` (618 B, SHA-256 `b23342184890bca7232bd48e36719ae8cd28154f3286bac024d6b22c735ad491`) holds **1 record**, VK0YY 20261023 124047. Guest `smd.log` at 12:44:38.643Z has three `warn` lines "session export: QSO not found, skipping", one for each Home UUID. This is the **successful but incomplete export** named as the confusable outcome: four rows were offered and one exported, with only a daemon-side warning. The toast read **"Exported 4 QSOs"** (operator report). The SPA uses the displayed Session length (`session.qsos.length`), not a count of returned records (`ExportDialog.svelte` `downloadAdif`, `count`). So the operator-facing success message overstates the file by 3. **Candidate finding: a false export count** (silent data omission). Two repeat exports from the same panel, at 12:45:42Z and 12:48:28Z, each also hold 1 record (VK0YY); the files differ in SHA-256 from the first. The 12:48:28Z toast was captured in a screenshot: green success, "Exported 4 QSOs to ADIF". FT8 "worked this session": **not available** (no FT8 in the guest). **Observed:** fails ADR 0094 on panel, badge and export (the edit is refused, not misapplied), as predicted from source | none | **FAIL** — Codex review, 2026-10-23; ADR 0094 list/action boundary and false-success export; Findings #5 and #6 |
| `B2-11` | yes | **clean-guest** — B2-10; switch back to Home in the same tab | Read the panel; edit a Home row (save and reload), and export | Home's list restored (per the B2-10 expectation); the edit saves to Home; the downloaded file holds Home's rows only | A row's edit written to the wrong archive | 2026-10-23: the operator activated Home from Settings → Archives in the `ff-b2s2` tab; the tab reloaded. At 12:49:17Z the daemon was instance `dmc8afyve2su` serving Home `01a163a7-0f04-7f69-85e9-255664b58d11` (legacy), `Default` **8**; VK0YY's UUID `01a168c7-800a…` returns **404** here. **Session panel shows 4 rows, VK0YY, VK9XX, JA8EPO and ZS4JAN, and the badge reads 4** (operator screenshot). ADR 0094 expects Home's 3; Contest's VK0YY is still listed. **Edit:** the operator edited VK9XX's comment to `B2-11 edit` (VK9XX instead of JA8EPO, which already carried `B2-09 edit`), saved and reloaded. `GET /v1/qso/01a168c5-9302…` on Home returns `B2-11 edit`; JA8EPO keeps `B2-09 edit`; ZS4JAN has none; `Default` is still 8. The edit saved to Home. **Export:** `~/Downloads/session-20261023-125155.adi` (1,276 B, SHA-256 `d5254439f3d0afc565412aac5c446ba8ac0ba1a0f13a60d50f91e18f41d23096`) holds **3 records**: VK9XX 123855 (`B2-11 edit`), JA8EPO 123409 (`B2-09 edit`) and ZS4JAN 123353. Guest `smd.log` at 12:51:55.198Z has one `session export: QSO not found, skipping` warning, for VK0YY `01a168c7-800a…`. **VK0YY edit:** no dialog opened; the panel showed in red "Cannot load VK0YY for editing: QSO not found" (operator screenshot). VK9XX's Country changed from empty to `Unknown` after its edit, repeating the B2-09 side observation. The export toast was not captured. **Observed:** the edit and the export file are correct for Home, but the panel and badge contradict ADR 0094. Another archive's row is offered, and the export silently omits it | none | **FAIL** — Codex review, 2026-10-23; Home list not isolated; edit and file contents correct for Home; toast unobserved; Findings #5 and #6 |
| `B2-12` | yes | **live** — after B1, Settings → Forwarding, read only | Read each destination's station account, per-logbook rows (Home's logbooks), on/off/mixed state, `restart_required`, `uploads_held`, the SM Cloud adoption status and support, and the queue counts | Bindings, accounts, states, adoption and held state match the pre-upgrade state (B1-03); `restart_required` false. Queue counts are explained, not equal: every difference from A3-03 is accounted for by uploads processed or QSOs logged between the snapshots, with no unexplained `failed` row. Wording as recorded in the inbox (2026-10-21) | A binding lost or re-seeded by the upgrade; uploads held after an upgrade with no account change; a new unexplained failure | pending | none | pending |
| `B2-13a` | yes | **clean-guest** — no SM Cloud station account and no SM Cloud binding (verify this starting state) | Open Settings → Forwarding | SM Cloud is omitted because it has neither an account nor a binding (`internal/archive/bindings.go`). This is destination-specific: the packaged daemon seeds QRZ, QRZCQ and ClubLog accounts (`config.applyDefaults`, `forwarding.DefaultForwarderConfigs`), so their cards do not fail this case | An unactionable SM Cloud "no station account" row offered, or an empty registry from a unit test mistaken for the packaged default | pending | none | pending |
| `B2-13b` | yes | **record** — the candidate's automated evidence | Cite `internal/archive/bindings_test.go` `TestBindings_EnableRefusals` (no account and no binding → not listed) and `TestBindings_AccountReportsBuildKeyAndReasonNamesTheGap` (a binding without its account → the "no station account" reason), plus the SPA's `bindings.svelte.test.ts` refusal wording | Present at the tag and passing in A1-01's evidence | Described as a manual pass | pending | none | pending |
| `B2-14a` | yes | **live** — after B1, read only; no failed rows manufactured | Read each destination's Retry failed button | Disabled where the failed count is 0 or uploads are held; otherwise enabled with the count when no retry is in progress (`DestinationsSection.svelte`). Nothing pressed. This observes the control only and cannot pass the retry action | Pressing it on live rows | pending | none | pending |
| `B2-14b` | yes | **record** — the candidate's automated evidence | Cite `internal/api/handler_forwarder_queue_test.go` `TestRetryForwarderQueue_HappyPath` (every failed row re-armed whatever its class, others untouched), `_Validation`, `_UsesStartupWorkerSet`, `_PreservesExactName`; `cmd/smd/lifecycle_rearm_auth_test.go` `TestLifecycle_StartRearmsAuthFailedRowsForEnabledForwarder` (the separate automatic re-arm at worker start); the SPA's `forwarder-queues.test.ts` (the name-scoped retry call) and `DestinationsSection.adoption.svelte.test.ts` U5 (a held row cannot retry) | Present at the tag and passing in A1-01's evidence | The manual retry and the start-time re-arm treated as one path | pending | none | pending |
| `B2-15a` | yes | **host-scratch** — checklist C's fixture method, not its counts (W-0021, C run sheet): a 0700 scratch, the A3-01 backup's SHA verified and extracted, separate `restore/` and `provision/` copies, each `config.json`'s `data_dir`, `datastore.path` and Home's legacy path rewritten into its copy, and the resolved-path inventory (`build/private/c-paths`) showing every write target inside the copy (live control: all outside); the candidate `smd` extracted from the RPM (A1-04's method). The copies hold live credentials and are removed after the record | Prepare the deliberate scratch-only difference — remove one QSO the cloud holds — and verify it (count down by one, the UUID absent, the cloud copy present) | The difference exists only in scratch; the live directory untouched (manifest before and after) | A "missing" row assumed from the backup rather than prepared | pending | none | pending |
| `B2-15b` | yes | **host-scratch** — B2-15a | `SM_WORKING_DIR=<restore-copy> <candidate-smd> restore --config <restore-copy>/config.json --archive <uuid> --logbook <local-integer-id> --dry-run` | Reports the fetched export’s record and tombstone counts and "dry-run: no DB writes"; the archive file unchanged. The numeric local logbook selects the UUID used for the cloud request. This dry run does not compute the missing-row delta; B2-15a proves that separately (`cmd/smd/restore.go:71,102-130`) | A dry run that writes | pending | none | pending |
| `B2-15c` | yes | **host-scratch** — B2-15b | The real restore, then the same again | First run restores exactly the prepared row (row contents compared as C did); the rerun restores none; other rows, identities, bindings and queues unchanged | A restore by name; other rows changed | pending | none | pending |
| `B2-16a` | yes | **host-scratch** — the `provision/` copy (B2-15a), with Drill's archive file **and any `-wal`/`-shm`/`.creating` sidecars** removed and its inactive catalogue entry kept, as C did; the station's SM Cloud account | `smd provision --list`, then `--dry-run` for the Drill archive | Listed with logbooks and counts, its catalogue state shown; the dry run writes nothing | A dry run that writes | pending | none | pending |
| `B2-16b` | yes | **host-scratch** — B2-16a | The real provision of Drill | The existing inactive entry is **kept unchanged**, not a new one added; the file is built with the cloud's UUIDs and payloads preserved, no bindings and no queued uploads; Home unchanged (rows, identities, bindings and queues), as C verified | A new catalogue entry; the existing entry's state changed | pending | none | pending |
| `B2-16c` | yes | **host-scratch** — B2-16b | The same provision again over the now-existing file | Refused, naming the file; nothing overwritten or removed | An overwrite | pending | none | pending |

B2-15 and B2-16 use the extracted candidate by explicit executable path, with `SM_WORKING_DIR`
and `--config` pointing at the appropriate copy for every command. They run between A3 and B1-01.
The whole-live-directory manifest comparison requires the live daemon to remain stopped through
these checks, sharing A3-01’s stop as checklist C did; a running daemon’s log writes would invalidate
byte equality. After the final comparison, start the old daemon to establish B1-01’s running-old-build
precondition. No separate early outage is scheduled for these cases.

**A4 parameters (carried forward from the alpha.2 record's ratified values, with the browser
updated):** Firefox **158.0** (`firefox-158.0-1.fc44`; alpha.2 ratified Firefox 155), no second
engine. Viewports: 1920×1080 at 100%, 1280×800 at 100%, 1024×768 at 125%, 1280×800 at 200%.
Daemon-unavailable indication within 15 s. SSE recovery within 5 s **after the daemon answers**, or
immediately after one tab-focus/online revival event, which is recorded explicitly when used; this
is not a five-second total restart deadline. Ordinary write "unknown" within 32 s, email "unknown"
within 47 s, rig/FT8 confirm-by-push within 3 s after the pushed state, shutdown within the
configured budget plus 2 s. Hardware and RF (B3) stay outside this inventory, under their own
authorisation.

**Session choice settled:** retain per archive, as ADR 0094 records. The timer and legacy
unscoped-store migration are outside this list-policy ruling. Run the guest cases, especially
B2-09–11, before A3. The current candidate is expected from source to fail the Session contract;
record the observed result without weakening that contract. A4-01 accepts the inventory only.
Any failure must receive its release disposition before the live upgrade; no waiver, implementation
or deployment approval is implied.

### B2-09–11 review and release disposition — 2026-10-23

Codex ruling requested by the operator, based on the recorded operator execution and corroborating
source review. This review did not repeat the browser actions or independently inspect the
downloaded files. Earlier B2 rows retain their recorded review status.

- **B2-09 PASS:** attempt 2 proves same-archive Session retention, the saved Home comment and the
  two-record export. The power-cut attempt remains void. The Country discrepancy is Finding #7.
- **B2-10 FAIL:** Contest shows four Session rows instead of its one. The captured success toast
  reports four exported records while the file contains one. Refusing the Home-row edit prevents
  the observed wrong-archive write attempt; it does not satisfy ADR 0094's list/action boundary.
- **B2-11 FAIL:** Home shows four rows instead of its three. The Home edit persisted and the file
  contains exactly the three Home records, but the offered Contest row remains in the panel and
  is silently omitted from the export. The export toast was not captured; no wording or count is
  inferred as observed evidence. B2-10 already proves the false-count defect, so another B2-11
  export is unnecessary for this ruling.

**Release disposition: HOLD alpha.4; fix Findings #5 and #6 before a replacement candidate.**
Do not proceed to A3's live outage or deploy this artifact. Keeping these two failures as accepted
known findings is declined: they break the supported archive-switch/review/export journey. No
wrong-archive write or loss of durable QSOs was observed; the demonstrated loss is omission from
the offered export. Findings #7 and #8 are lower-severity follow-ups, not additional blockers for
the replacement candidate. This triage grants no implementation authority or operator waiver.
The guest and tunnel remain up at the operator's direction.

**Required outcomes for the fix review and replacement gate:**

- ADR 0094 applies to the panel, badge, edit, export, email selection and FT8 "worked this session"
  consumers. Retain by stable archive UUID within the tab; preserve same-archive reload/restart,
  restore the previous list on return, and show an empty list for an archive with no Session rows
  in that tab. Establish the served identity before using a list. Legacy unscoped rows need an
  explicit migration decision; assigning them to whichever archive is active is not acceptable.
- An export's reported count must equal the file's actual records for that request. Missing rows
  must produce an explicit incomplete outcome with accurate exported/omitted counts, or an
  explicit refusal; a successful-looking partial file is the nearest confusable failure. Resolve
  that partial-export policy in the fix design. Cover missing/deleted rows independently of an
  archive switch, plus complete and all-missing selections: list isolation alone cannot prove
  export completeness. Changing the toast alone cannot close silent omission.
- Use informative failing assertions and reversion proofs for both defects. Cover every Session
  consumer, including email selection and FT8, with offline evidence where possible. Neither was
  exercised by these guest cases; FT8's cross-archive effect remains source-based, not an RF result.
- A code change creates a new candidate identity. Freeze and build that candidate, rerun automated
  gates and affected Gate A prerequisites, and rerun Gate B's affected archive/Session/export
  cases on the exact replacement artifact, including B2-09–11 before A3. Complete the remaining
  mandatory cases and obtain both gate decisions under the canonical acceptance rules. Preserve
  alpha.4's failures as its record; do not relabel this artifact or transfer its results as new
  passes. Refresh backup/rollback evidence when the replacement is ready for the live lane.

## Findings

Numbered for this record; alpha.3's Finding #1 (the CI mirror rewriting `dist/index.html`) is cited by
its record. The triage below is Codex review requested by the operator, 2026-10-22 and 2026-10-23; release
acceptance and any waiver remain the operator’s. No implementation is authorised by this triage.

| # | Case | Observation (sanitized) | Triage | Destination |
|---|---|---|---|---|
| 1 | A2-01 | On a fresh install's first start, before setup, `smd.log` carries a `warn`: "startup: could not record the default logbook in the archive" (`default_logbook_id` 1, "no archive identity row, or logbook 1 does not exist"). Config's default logbook id is 1 before setup creates logbook 1. `SetArchiveDefaultLogbookWithContext` reports the missing row as a plain error, so the caller's `ErrNotFound` filter (`cmd/smd/archive_adopt.go:107`) never matches and the warning is logged. Inference, unverified: every start before setup logs it. A warning on a correct fresh install | **Defect, low severity:** a misleading warning on an otherwise usable first install. Does not invalidate A2-01; recurrence before setup remains untested. | [W-0021](../work/W-0021-qso-archives.md), startup adoption; correct the expected pre-setup case while retaining diagnostics for real failures. Priority remains in the backlog. |
| 2 | A2-01 | `install.md` §2 says the package installs three files and that "the post-install scriptlet prints the next steps". The candidate installs six paths (the two binaries, the unit, `PRIVATE-BUILD-DO-NOT-DISTRIBUTE` and the manual's `index.html` and `.gitkeep` under `/usr/share/doc/station-manager/`) and carries no scriptlets (`rpm -q --scripts` empty, as the alpha.2 record found), so nothing is printed | **Documentation correction:** the no-scriptlet behavior is deliberate (`nfpm.yaml`); the install guide’s payload and next-steps claims are stale. Does not invalidate the completed install. | [W-0018](../work/W-0018-bring-the-embedded-manual-to-release-readiness.md), `docs/install.md` §2; describe the manual and the private-build marker accurately. No candidate rebuild for a repository-guide-only correction. |
| 3 | A2-07 | `dnf remove station-manager` leaves `/usr/share/doc/station-manager/manual/` behind, empty and owned by no package (`rpm -qf`: not owned): the package ships files under it but does not own the directories | **Duplicate:** alpha.2 Finding #17, a low-severity packaging residual. Files and operator data behaved as expected; A2-07 passes. | [W-0009](../work/W-0009-maintainability-audit-residuals.md), existing Packaging residual, with the next packaging change; no new work item. |
| 4 | A2-08 | The upgrade start logs neither transition as numbers. The config rewrite is `config saved` with `{"field":"schema_version","from":"","to":""}` (empty from and to), and the log migration says only `m.Up completed or no change`. Config 3 → 6 and schema 8 → 18 were shown by the file's `version` and by `GET /v1/version` before and after, not by the log. `config.md` §13.4 says the persistence reason names the version | **Working as designed; documentation wording correction.** ADR 0075 requires a `schema_version` reason naming no value; `cmd/smd/main.go:547-552` implements it and `config_migration_startup_test.go:100-140` explicitly requires empty `From`/`To`. The SQL completion message also matches `internal/database/sqlite/migrations.go:124-134`. Neither message proves the numeric transitions; the retained before/after evidence does. No migration defect. | `docs/v2-design/config.md` §13.4: clarify that the record names the schema-version field/reason, not numeric versions. Numeric transition logging would be a separately selected observability change, not a repair required by A2-08. |
| 5 | A2-08; B2-10, B2-11 | After the upgrade and a reload, the Phone/CW Session panel shows 4 QSOs while the Logbook and the daemon hold 3 (three UUIDs, unchanged across the upgrade). Investigated (operator request): the daemon received exactly three `POST /v1/qso` in this lane (201 each, 12:46Z, alpha.2). The panel counts `session.qsos`, which lives in the tab's `sessionStorage` (`sm.session.qsos`, `frontend/app/src/lib/operate/session.svelte.ts`): it survives every reload, and no production code clears it (only a test seam does), so it is keyed by browser tab and origin, not by daemon, archive or logbook. Both throwaway profiles are still empty (0 files), so both lanes ran in the operator's normal Firefox profile on the same origin, `127.0.0.1:18080`. The fourth row is A2-06's QSO from the `smtest` lane (`7Q7EB`, 12:43:01Z): the operator confirmed the panel lists `7Q7EB` twice (2026-10-22). Product consequence, an inference from the code and not exercised: an archive switch reloads the page, so the Session panel would keep showing the previous archive's QSOs after it (no design in ADR 0071 or 0087 covers this). **2026-10-23 confirmation:** B2-10 and B2-11 reproduce cross-archive retention in an actually used fresh profile: Contest shows four rows instead of one; Home shows four instead of three. Inactive-archive row edits are refused. ADR 0094 supplies the contract; the earlier inference is now a confirmed browser defect. | **Release blocker: Session archive isolation.** The A2 test-isolation explanation still stands, but the distinct B2 failure is now confirmed. Refused edits mitigate the observed write risk; they do not make the mixed panel or its actions correct. No durable-QSO loss or wrong-archive write observed. alpha.4 held; no waiver. | [W-0021](../work/W-0021-qso-archives.md), [ADR 0094](../decisions/0094-retain-session-lists-per-archive-within-each-tab.md): all Session consumers must select the served archive's per-tab list; rerun B2-09–11 on the replacement. W-0012's “New session” action remains separate. Export completeness is independently tracked in Finding #6. |
| 6 | B2-10; B2-11 | B2-10 offers four Session rows and downloads one record; the captured toast says “Exported 4 QSOs to ADIF”. B2-11 downloads three Home records from a four-row panel, silently omitting Contest's row; its toast is unobserved. `ExportDialog.svelte:36,72-79` uses the displayed Session length for success; `handler_session_export.go:50-68,103-146` skips missing UUIDs and returns the remaining records without omission metadata. The relevant source is unchanged from the candidate tag. | **Release blocker: false-success, incomplete export.** Independently fix the export contract, not just the archive-list trigger or the toast text. No claim that stored QSOs were deleted. B2-10 is sufficient evidence; no further B2-11 export required. | [W-0021](../work/W-0021-qso-archives.md), Session/export closure for the replacement gate. Report actual records and make omissions explicit, or refuse the incomplete export; settle the policy before implementation. Include a same-archive missing/deleted-row case. Update the canonical API reference with any response-contract change. |
| 7 | B2-09; B2-11 | Country is empty when logged and becomes `Unknown` after a comment edit, although the daemon stored `Unknown` both times. `main.ts:584-600` adds the Phone/CW Session row using browser enrichment; `sessionEdit.svelte.ts:91-106` overlays the saved daemon row. The edit did not newly discover a country. | **Defect, low severity: inconsistent presentation.** Does not invalidate B2-09's retention/edit/export outcome and is not a replacement-candidate blocker. No persisted-data corruption shown. | [W-0012](../work/W-0012-operator-experience-followups.md), Session presentation follow-up: identical stored unknown-country state should render consistently before and after an unrelated edit, with known-country rows preserved. No implementation authorised. |
| 8 | B2-07a | The inactive archive file is missing, but Archives says “the file changed since it was last open”. `ArchivesSection.svelte:219-226` uses that explanation for every inactive `stale` entry. Station Events correctly reports the missing file, and the recorded fallback and pending-selector clearing succeeded. | **Defect, low severity: misleading recovery wording.** The stale-count warning is appropriate; its asserted cause is not. This is not evidence of failed fallback or data loss, and is not a replacement-candidate blocker. | [W-0021](../work/W-0021-qso-archives.md), archive diagnostics follow-up: explain a known missing file accurately, or use cause-neutral stale wording; preserve the distinct active-archive recount message. No implementation authorised. |

## Appendix 3 — clean-lane guest plan (approved with amendments, 2026-10-22)

Purpose: an RPM environment with no Station Manager data, for A1-02 and A2-01…A2-08. The host is
Fedora 44 with libvirt 12.0.0, `passt` `0^20261002.gcba3570`, `qemu-kvm` 10.2.2, `virt-install`
5.1.0 and `xorriso` 1.5.8; the session connection had no domains and `~/sm-guest` did not exist
(checked 2026-10-22).

**Operator ruling, 2026-10-22:** the Fedora 44 Cloud x86_64 image download and this approach are
approved, with three amendments incorporated below: browser access through an SSH tunnel (item 4),
the isolation boundary stated as it actually is (item 5), and explicit lane transitions with
guest-specific disposal (items 6 and 7).

1. **Connection and ownership: `qemu:///session`** (the operator's user, no root, separate from
   the system VMs). Domain name **`sm-alpha4-clean`**. Everything the guest owns is under
   `~/sm-guest` (0700), and nothing else:
   - `~/sm-guest/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2` — the verified base image, read-only;
   - `~/sm-guest/sm-alpha4-clean.qcow2` — the overlay that is the guest disk;
   - `~/sm-guest/seed.iso` — the cloud-init seed;
   - `~/sm-guest/id_ed25519` and `.pub` — the guest-only SSH key;
   - `~/sm-guest/fedora.gpg`, the `CHECKSUM` file, its verified copy `CHECKSUM.verified`, and the
     cloud-init sources `user-data` and `meta-data`;
   - `~/sm-guest/known_hosts` — the guest's SSH host key, kept out of `~/.ssh`;
   - `~/sm-guest/ff-smtest` and `~/sm-guest/ff-smtest2` — the two throwaway Firefox profiles.
   Nothing goes under the working directory or the repository.
2. **Image:** `Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2`, verified per Fedora's published
   procedure (https://fedoraproject.org/cloud/download/): the `Fedora-Cloud-44-1.7-x86_64-CHECKSUM`
   file verified with `gpgv --keyring ./fedora.gpg` against Fedora's key file, then
   `sha256sum -c --ignore-missing`. A failed verification stops here.
3. **First boot by cloud-init:** a `cidata` seed ISO holding `user-data` and `meta-data`. One user,
   `smtest`, with the guest-only key, no password login, and lingering left for A2-01 to enable as
   the guide says.
4. **Network and browser access (amendment 1):** user-mode networking via `passt`, with exactly one
   port forward: host `127.0.0.1:2222` → guest SSH (22). The daemon keeps its default configuration,
   which listens on guest loopback `127.0.0.1:8080` (`internal/config/config.go`, the default
   `socket_path`); a VM port forward targets the guest's network interface and would not reach it.
   The browser goes through an SSH local forward instead,
   `ssh -p 2222 -L 127.0.0.1:18080:127.0.0.1:8080 smtest@127.0.0.1`, which connects to the
   destination from inside the guest (ssh(1) `-L`, https://man.openbsd.org/ssh#L). The browser opens
   `http://127.0.0.1:18080/`.
5. **Isolation boundary (amendment 2):** user-mode networking gives the guest outbound connectivity
   — the Fedora mirrors for package dependencies, and also the LAN; it does **not** restrict LAN
   access (libvirt, "Userspace connection using passt",
   https://libvirt.org/formatdomain.html#userspace-connection-using-passt). The boundary is instead:
   - no shared folders or files and no host devices (no serial, no audio passthrough);
   - no station credentials and no copy of any operator data — the only files copied in are the
     candidate RPM and, for A2-08, the frozen alpha.2 RPM, each by `scp` and SHA-verified inside the
     guest;
   - no requests to live station services: setup enters only a test callsign, and no forwarder,
     lookup credential, SM Cloud account, rig or SMTP is configured in the guest.
   The candidate carries the ClubLog key, so the guest is private and disposed of after Gate A.
6. **Lane transitions (amendment 3):**
   - each lane uses a **fresh, throwaway Firefox profile** (`firefox -no-remote -profile <new empty
     dir>`), one for the `smtest` lane (A1-02, A2-01…A2-07) and another for the `smtest2` lane
     (A2-08), so no cached chunk, storage or cookie crosses lanes;
   - A2-08 starts only after A2-07 has **stopped and disabled** `smtest`'s `smd` user service and
     uninstalled the package, so nothing else holds guest `127.0.0.1:8080`. Then a second user
     `smtest2` with a fresh working directory installs the alpha.2 RPM, sets up, logs a few test
     QSOs, and upgrades per `install.md` §7;
   - each lane's SSH tunnel is closed before the next opens.
7. **Disposal (guest-specific):** after Gate A, at the operator's timing,
   `virsh --connect qemu:///session undefine sm-alpha4-clean --remove-all-storage` (which removes
   only the storage that domain references: the overlay and the seed), then removal of the files
   listed in item 1 and the directory itself, the SSH key and both browser profiles included.

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
5. **2026-10-22 09:45–09:55Z — guest preparation, A1-02 and A2-01 (clean), by the agent.**
   - *Image.* `~/sm-guest` created 0700. `Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2`,
     `Fedora-Cloud-44-1.7-x86_64-CHECKSUM` and `fedora.gpg` downloaded from Fedora.
     `gpgv --keyring ./fedora.gpg`: "Good signature from Fedora (44)", key
     `36F612DCF27F7D1A48A835E4DBFCF71C6D9F90A6`; `sha256sum -c --ignore-missing`: OK. Image SHA-256
     `28680fe5b371a5a82ebf43a31926e086a168e59949d03969c5093e7071f90b7f`, made read-only.
   - *Guest.* A 20 GiB qcow2 overlay on the image; a guest-only ed25519 key; a `cidata` seed (user
     `smtest`, key login only, passwordless `sudo` for the install steps). `virt-install` on
     `qemu:///session`: domain `sm-alpha4-clean`, 2 vCPU, 2 GiB, the overlay and the seed, one `passt`
     interface whose only port forward is `127.0.0.1:2222` → 22, no graphics, no sound, no shared
     folders, no host devices (domain XML reviewed before creation). First boot: cloud-init `done`,
     Fedora Linux 44 (Cloud Edition), kernel 6.19.10; no `station-manager` package, no 9p or virtiofs
     mounts, `Linger=no`, nothing listening on 8080.
   - *A2-01, install (§2).* The candidate RPM copied by `scp` to `~/rpms` (0600); SHA-256 inside the
     guest `cebcd305…c5532b`, matching. `sudo dnf install -y <rpm>`: exit 0, "skipped OpenPGP checks"
     (expected, per §2). Installed paths: `/usr/bin/smctl`, `/usr/bin/smd`,
     `/usr/lib/systemd/user/smd.service`, `/usr/share/doc/station-manager/PRIVATE-BUILD-DO-NOT-DISTRIBUTE`,
     `/usr/share/doc/station-manager/manual/index.html` and `.gitkeep`. No scriptlet output (Finding #2).
   - *A2-01, enable and start (§3).* `systemctl --user daemon-reload` exit 0; `enable --now smd`
     exit 0; `systemctl --user status smd` `active (running)`; `smctl status` "SM is running." The
     guide's `loginctl enable-linger "$USER"` exited 1, "Could not enable linger: Access denied":
     the Cloud image ships no `polkit` (`rpm -q polkit`: not installed), whereas the shipped polkit
     policy allows self-linger (`allow_any`/`allow_inactive`/`allow_active` all `yes`). Lingering was
     enabled with `sudo loginctl enable-linger smtest` (`Linger=yes`); a lane deviation, not a product
     defect.
   - *A2-01, health, version and logs.* `GET /v1/healthz` `{"database":"ok","logging":"ok","status":"ok"}`;
     `GET /v1/version` `daemon 2.0.0-alpha.4`, `env release`, `schema {18, dirty:false}`, archive
     `Home` (legacy), adopted at the first start; listener `127.0.0.1:8080` only; `GET /` 200. The
     working directory `~/.local/share/station-manager` holds `config.json` (0600, "first run: wrote
     default config to disk"), `db/` (0700: `station-manager.db`, `reference.db`) and `log/smd.log`
     (0600): 27 lines when counted, all carrying `"version":"2.0.0-alpha.4"`, 26 `info` and one `warn`
     (Finding #1). The `logging configured` line carries its `level` key twice (seen, not triaged).
   - *A1-02.* `rpm -q`: `station-manager-2.0.0~alpha.4-1.x86_64`; the version endpoint and the startup
     line `smd starting` agree. The sidebar badge awaits the browser.
   - *Browser access.* SSH local forward `127.0.0.1:18080` → guest `127.0.0.1:8080` (Appendix 3,
     item 4) opened by the agent; through it `GET /v1/version` and `GET /` (200, `text/html`)
     answer. The host listens on `127.0.0.1:2222` and `127.0.0.1:18080` only. No browser has
     opened the app yet; A2-02 onward is the operator's, in the fresh profile `~/sm-guest/ff-smtest`.
6. **2026-10-22 — A2-02…A2-05 and the A1-02 badge (clean), by the operator, with daemon restarts by
   the agent.** The operator ran A2-02, A2-03 and A2-04 through the tunnel and reported them
   done. The profile was initially recorded as `~/sm-guest/ff-smtest`; investigation in log #8
   established that the normal Firefox profile was used. The agent then restarted the guest daemon
   (`smctl restart`, "SM Restarted.", exit 0; PID 1564 → 1984, `NRestarts=0`; a new `instance` on
   `/v1/version`, still `2.0.0-alpha.4`, schema `{18, dirty:false}`, archive Home); `config.json`
   then read `setup_complete: true`, `default_logbook_id: 1`. Finding #1's warning did not recur after
   setup. A power cut at the operator's end interrupted the session; this host and the guest stayed
   up (host boot 06:59 local; guest, tunnel and the live daemon, PID 4606, unchanged). The operator
   completed A2-05 and the agent restarted the daemon again (PID 1984 → 2072, as before; no new warning
   or error). The operator read the sidebar badge as `2.0.0-alpha.4`.
   A2-06 was reported done before its first QSO was logged: the guest then held one archive (Home,
   active) and one logbook (`Default`), with 0 QSOs (`GET /v1/logbook/1/count`) and no store line in
   `smd.log`. The operator will log it once power is restored; A2-07 waits for it.
7. **2026-10-22 12:42–12:50Z — A2-06 and A2-07 (clean).** The host had rebooted after the shutdown;
   the guest was shut down cleanly before it. The agent started the guest: it booted at 12:42:20Z and
   `smd` was active at 12:42:27Z, before any login, as lingering promises; `/v1/version` unchanged
   (new instance), no new warning. The tunnel was reopened. The operator logged the first test QSO
   (A2-06): count 0 → 1, 20m SSB, under the logbook's callsign; no forwarder queues
   (`{"forwarders":[]}`).
   A2-07, by the agent, per `install.md` §10: `systemctl --user disable --now smd` (exit 0, enable
   symlink removed), `sudo dnf remove -y station-manager` (exit 0), `systemctl --user daemon-reload`
   (exit 0); `rpm -q`: not installed; `/usr/bin/smd`, `/usr/bin/smctl` and the unit absent;
   `systemctl --user status smd`: unit not found; nothing listening on 8080. `loginctl
   disable-linger "$USER"` exited 1, "Access denied" (no polkit, as in A2-01); done with
   `sudo loginctl disable-linger smtest` (`Linger=no`). Data retained in
   `~/.local/share/station-manager`: `config.json` (byte-identical before and after), `db/` and
   `log/`. The stop itself checkpointed the WAL and wrote the archive summary (the log ends
   `smd stopped` at 12:43:40Z), so those files changed at the stop, not at the removal. The stopped
   QSO database read through `immutable=1`: `integrity_check` ok, 1 QSO. Left behind: an empty
   `/usr/share/doc/station-manager/manual/`, owned by no package (Finding #3).
8. **2026-10-22 12:44–12:50Z — A2-08 (clean), the `smtest2` lane.** The `smtest` tunnel was closed
   first (A2-07 had stopped, disabled and removed that lane's service). The agent created `smtest2`
   (fresh home, the guest-only key, passwordless `sudo` for the package steps), copied both RPMs in
   and verified them (`7b2fa20a…c24856`, `cebcd305…c5532b`), installed the frozen alpha.2 RPM,
   enabled lingering with `sudo`, and ran `enable --now`: `2.0.0-alpha.2`, schema 8, `config.json`
   version 3, setup not complete. The `smtest2` tunnel opened on the same host port; the operator
   completed setup and logged three test QSOs in the normal Firefox profile, reusing the
   first lane’s tab/origin (initially recorded as `~/sm-guest/ff-smtest2`; corrected below).
   - *Before:* logbook `Default` with 3 QSOs; `config.json` version 3, setup complete, SHA-256
     `66c23055…f6b73`; PID 1707; no warning or error in `smd.log`.
   - *`install.md` §7:* `sudo dnf install` of the candidate: "Upgrading", exit 0; `rpm -q`
     `station-manager-2.0.0~alpha.4-1.x86_64`; the old daemon still serving (PID 1707,
     `2.0.0-alpha.2`). `smd config-check`: exit 0, "no unrecognised keys; the file loads and validates
     as startup would; 0 enabled forwarder(s) construct …"; `config.json` unchanged (same SHA).
     `systemctl --user daemon-reload` exit 0; `systemctl --user restart smd` exit 0.
   - *After:* PID 1930, `NRestarts=0`; `2.0.0-alpha.4`, schema `{18, dirty:false}`, archive Home
     (legacy); `config.json` version 6, setup complete, default logbook 1, the catalogue holding Home;
     `config.v5.json` written beside it (0600, version 5); the same three QSO UUIDs, count 3. The
     start logged, once each: `config saved` (`schema_version`), the archive catalogue written
     (adopted in place), the destination bindings decided once, the v5 recovery copy written, and
     `config.json` stripped to station accounts. No warning or error.
   - *Idempotence:* a second `smctl restart` (PID 1995) left `config.json` byte-identical and logged
     no adoption, seed, copy or strip line; count still 3.
   - Neither transition is logged as numbers (Finding #4).
   - *Browser (operator):* after reloading the reused normal-profile tab, no Welcome page, Phone/CW, badge
     `2.0.0-alpha.4`, Logbook 3 QSOs. The Phone/CW Session panel shows 4 QSOs (Finding #5; inbox
     note of 2026-10-22).
   - *Lane deviation:* the throwaway profiles `~/sm-guest/ff-smtest` and `ff-smtest2` were never
     opened (both empty); A1-02's badge and A2-02…A2-08's browser steps ran in the operator's
     normal Firefox profile, on an origin (`127.0.0.1:18080`) it had not used before A2-02.
     Appendix 3 item 6 (a fresh profile per lane) was therefore not met for either lane.
