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
RPM filename: `build/private/station-manager-2.0.0~alpha.4-1.x86_64.rpm` (mode 0600) — pending
RPM SHA-256: pending
RPM-reported version: `station-manager 2.0.0~alpha.4-1 x86_64` expected
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
Automated gate evidence: pending (A1-01)
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

To be enumerated at A1-03 against `docs/v2-design/config.md` (v4–v6: the archive catalogue,
binding-owned forwarder fields under ADR 0082, the station-account split).

### Changed API surface (the embedded SPA is the only client)

New endpoints: `POST /v1/forwarder/{name}/queue/retry`; `GET /v1/logbook/{id}/destinations`;
`GET` and `POST /v1/qso-archives`; `POST /v1/qso-archives/{uuid}/activate`; `GET` and
`PUT /v1/qso-archives/{uuid}/bindings`. Changed semantics on existing endpoints are enumerated at
A1-03 from `docs/v2-design/api-endpoints.md`.

### Changed user journeys

- Settings: Archives, Logbooks and per-logbook Destinations sections; the station accounts split from
  bindings on Forwarding; stored-secret fields; help tips and manual links.
- The shell's archive switch, over the attended restart (`ArchiveSwitchGate`).
- FT8: the off-state notice (`FtOffNotice`).
- SM Cloud by archive and logbook identity: adoption of Home, `uploads_held`, identity uploads and
  reconciles.

### Packaged commands

`smd` subcommands at the cut: `import`, `restore` (now by archive and logbook UUID), `provision`
(new), `config-check`, `db-downgrade` (new) and `config-downgrade` (new). Known defect, not in scope
for this candidate: an unknown first argument starts the daemon (dogfood inbox, 2026-10-21).

### New manual or setup instructions

`docs/install.md` changed, including "Provisioning a cloud archive on this station" and "Rolling back
to an older build". To be reviewed at A1-03.

## Gate A — ready to deploy

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `A1-01` | yes | **record** — clean scratch worktree at tag `v2.0.0-alpha.4` (`3f7425ed`) | Build once with `SM_FFT=pocketfft scripts/dev-rpm.sh` after `task ci:local`; restore the committed `frontend/app/dist/index.html` first if the CI mirror rewrote it (alpha.3 Finding #1); record filename, SHA-256 and RPM version; keep the versioned copy | One artifact; RPM version `2.0.0~alpha.4-1`; `ci:local` exit 0; SHA recorded in the header | A `-dirty` or `alpha.4-N-g…` version; a second build overwriting the copy | pending | none | pending |
| `A1-02` | yes | **clean** — the A2-01 install, candidate installed and started | Verify the RPM SHA before installing; read `GET /v1/version`, the sidebar badge, the startup log line and `rpm -q` | All four read `2.0.0-alpha.4`; schema `{18, dirty:false}` after the first start | A badge from a cached tab; a dev-build version string | pending | none | pending |
| `A1-03` | yes | **record** — delta drafted above | Review the release delta against the candidate, completing the configuration and API items | Delta accepted or corrected here | Silent scope creep in the delta | pending | none | pending |
| `A1-04` | yes | **host-scratch** — 0700 scratch with a copy of the live `config.json`; the candidate `smd` extracted from the RPM (alpha.3 record, Appendix 1); live service untouched | `smd config-check --config` on the untouched copy (control), then on a top-level and a nested unknown-key mutation | Control exits 0; each mutation exits non-zero naming only the key path; nothing started, bound or written | Control non-zero: the candidate would refuse to start on the station. Fix and re-run; no waiver possible | pending | none | pending |
| `A1-05` | yes | **host-scratch** — as the alpha.3 record's Appendix 2, with this artifact's `smd` | The CC-5 reconcile on the alpha.1-era file, then the permutation control | Copy exits 0; the permutation exits non-zero naming `qrzcq` and the unsupported action | Both exit 0; the copy refused for `allow_insecure_http` | pending | none | pending |
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
- **Archives:** list, create, activate over the attended restart, the shell switch, last-known-good
  start, and Home's legacy entry.
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
