# Dogfood acceptance — v2.0.0-alpha.5

Release-specific execution record for the canonical gate in
[`docs/dogfood-acceptance.md`](../dogfood-acceptance.md). alpha.5 replaces the held alpha.4
candidate ([alpha.4 record](dogfood-acceptance-v2.0.0-alpha.4.md), "B2-09–11 review and release
disposition"). alpha.4's tag (`3f7425ed`), its held RPM (`cebcd305…`) and its record are kept
unchanged. Every `pending` row is the operator's to execute and rule on. Hardware, rig-command and
RF cases keep their separate authorisation boundary, and nothing here authorises RF.

## Scope — operator rulings, 2026-10-24

- **Candidate:** `v2.0.0-alpha.5` at `0da2cd6a`, frozen once CI passes on that exact commit. If
  `main` advances first, the intervening changes are reviewed before a commit is selected. The
  agent creates the annotated tag locally; it is not pushed.
- **Why a new version:** one version identifies one frozen candidate. alpha.4 stays held, with its
  tag, artifact and record.
- **What it fixes:** the alpha.4 release blockers, Finding #5 (Session lists crossing archive
  boundaries) and Finding #6 (false-success incomplete export), through the accepted fix design
  (W-0021 "alpha.4 hold: fix design", revision 3; ADR 0094 update; ADR 0095), in five slices
  (release delta below). Findings #7 and #8 remain lower-severity follow-ups.
- **Build:** as alpha.4. In a clean scratch worktree at the tag, `task ci:local` runs first, then
  `SM_FFT=pocketfft scripts/dev-rpm.sh` builds once. The single versioned copy (mode 0600) lives in
  `build/private/`. It is a private keyed build carrying the ClubLog key and is **not
  distributable**.
- **Evidence rule:** still-valid alpha.4 evidence carries forward only by explicit row citation and
  rationale. Candidate identity, build checks and installation evidence must identify alpha.5.
  Affected cases and invalidated prerequisites are rerun, including B2-09–11 on the guest before
  A3.
- **Lanes:** the approved guest plan (alpha.4 record, Appendix 3) is reused as written.
- **Guest sequence (operator, 2026-10-24):** after A4-01 is ratified, B2-18's legacy fixture is
  prepared and verified while alpha.4 is still installed in the guest, that lane is upgraded in
  place and B2-18 runs. Only then does A2-01 start the fresh-install lane: the package is removed
  (as A2-07) and alpha.5 installed into a new working directory and user.
- **Recovery:** unchanged from alpha.4. Refresh the recovery backup before the live upgrade (A3).

## Header

Candidate commit: `0da2cd6a` — tag `v2.0.0-alpha.5` (annotated, local, **not pushed**; the tag
identifies the candidate and does not mean acceptance passed). CI green on `0da2cd6a` (run
40077880913, confirmed 2026-10-24); `main` had not advanced past it when the tag was created.
Version/build shown by daemon: `2.0.0-alpha.5` expected on `GET /v1/version`, the sidebar build
badge, the startup log line and ADIF `PROGRAMVERSION`
RPM filename: `build/private/station-manager-2.0.0~alpha.5-1.x86_64.rpm` (mode 0600), the single copy
of the artifact `scripts/dev-rpm.sh` built in a clean scratch worktree at the tag, since removed. It is a
private keyed build (PocketFFT/CGO; ships `/usr/share/doc/station-manager/PRIVATE-BUILD-DO-NOT-DISTRIBUTE`)
and is **not distributable**. Verify the SHA before every install.
RPM SHA-256: `3c10b6e1a7899807612db7720a0b95fe21badbb7cf21bbb8080265cd11693a9c` (9,631,676 bytes, built
2026-10-24 09:06Z); `/usr/bin/smd` inside it SHA-256
`b91bc34b237a34ea50d74a92c993df98b7263146eed0b02edc0e468d80d42a28`
RPM-reported version: `station-manager 2.0.0~alpha.5-1 x86_64` (read with `rpm -qp`); daemon build line
`version: 2.0.0-alpha.5, FFT: PocketFFT (CGO, dynamically linked)`
Previous installed version: unchanged from alpha.4, the dev build
`station-manager-2.0.0~alpha.3.262.g6217da21.dirty-1.x86_64` (alpha.4 was never installed on the
live station). By RPM ordering alpha.5 is an **upgrade** from it:
`rpm.labelCompare(2.0.0~alpha.5-1, 2.0.0~alpha.3.262.g6217da21.dirty-1)` = 1 (checked 2026-10-24)
Clean-install environment: the guest `sm-alpha4-clean` (alpha.4 record, Appendix 3), a fresh lane
for this artifact (A2-01)
Upgrade environment: the daily-driver dogfood station (this host)
Browser(s) and viewport(s): Firefox 158.0, the A4 parameters accepted at alpha.4 A4-01
Operator: 7Q5MLV
Automated gate evidence: `task ci:local` passed at `0da2cd6a` on 2026-10-24 (08:59:40–09:06:20Z) in the
clean scratch worktree: SPA lint, prettier, svelte-check, vitest 2329/2329 (156 files), Vite build;
Go gofmt, vet, golangci-lint, maintainability observatory 0 regressions (34,411 measurements); race
and full Go test runs, none from the test cache (131 `ok` lines, none `(cached)`); PocketFFT FT8
decode test; ClubLog build-key boundary (ST-7) OK; documentation and agent-context budgets; "All CI
gates passed locally." The smcloud store's Postgres tests skip locally; CI run 40077880913 ran them
on the same commit, green.
Backup location (do not record secrets): `~/sm-backup/station-manager-20261028T1035Z-g6217da21-pre-alpha5.tar.gz` (0600, 34,122,491 B, SHA-256 `77d969f0f0ac8ec039f2130c739036468b00930f22ab5c7323100cd784b72490`; A3-01); the 2026-10-21 backup (`2c21b99d…`) kept
Restore check: extraction identical to the post-shutdown baseline (70 paths, every hash); `integrity_check` ok for Home, Drill, `reference.db`, `evidence.db` (A3-02)
Recovery policy: as alpha.4. The candidate adds no migration and no configuration change over
alpha.4 (release delta), so it also adds none over the installed build, and a package rollback to
the preserved dev RPM needs no data downgrade.
Rollback artifact/command: the preserved dev RPM `build/private/station-manager-2.0.0~alpha.3.262.g6217da21.dirty-1.x86_64.rpm` (SHA-256 `2d3a0ec9…be180bdb`): `systemctl --user stop smd`; `sudo rpm -Uvh --oldpackage <dev rpm>`; `systemctl --user daemon-reload`; `systemctl --user start smd`; then `GET /v1/version` (the dev build, Home) and `GET /v1/healthz` ok. No data downgrade (alpha.5 adds no migration) (A3-04)

Ready to deploy: **PASS** — operator review, 2026-10-28 (Gate A complete: A1-01…A1-05, A2-01…A2-08, A3-01…A3-05, A4-01)
Dogfood accepted: **PASS** — operator final review ruling, 2026-10-28, for alpha.5 at `0da2cd6a`, RPM
SHA-256 `3c10b6e1…693a9c`, within the ratified acceptance scope. All scoped case results pass,
including recovery and browser stream reconnection; Findings #1–#4 have explicit non-blocking
dispositions, and the upgrade workaround remains: reload every open Station Manager tab. No residual
waiver remains. This does not close FT8-10/W-0002 and is not hardware or RF acceptance.
Residual waivers: none — the alpha.2 B1-01 waiver was retired by A1-05 and B1-01 on this artifact (operator, 2026-10-28)
Follow-up destinations: Findings #7 (W-0012) and #8 from the alpha.4 record, still open

## Release delta — `v2.0.0-alpha.4` → `v2.0.0-alpha.5` (11 commits, 5 outside Markdown)

The five code commits are the fix-design slices; the other six are alpha.4 record and reference
commits (`06636302`, `bb2efe5d`, `49a96f96`, `d8251f46`, `2012acd8`, `9c62707d`).

| Commit | Slice | Operator-visible change |
|---|---|---|
| `2a75fca9` | 1 — request guard | Archive-scoped requests name the tab's proven archive in `X-SM-Archive-Id`. A stale tab's submit, edit, delete, guarded read, session export or email, and every FT8 route that admits TX is refused with 409 `archive_mismatch` before any side effect; missing or malformed header 400. Stop and reduce controls are exempt. `POST /v1/qso` returns `archive_id`. The SPA sends nothing while its archive is unproven. |
| `2b10f81f` | 2 — FT8 stream identity | Every `/v1/ft8/events` connection names its archive first (`event: identity`), before the replay. |
| `305d8278` | 3 — daemon export/email counts | Session export adds `X-SM-Requested-Count`, `-Exported-Count`, `-Omitted-Count`; email adds `sent` and `omitted` lists. All-missing stays `no_qsos`. |
| `0d73aaca` | 4 — per-archive Session stores | The Session list and FT8 engagement are kept per archive in each tab (`sm.session.qsos.<uuid>`, `sm.ft8.engaged.<uuid>`), filed by the source's archive; a foreign write changes nothing and starts verification. Session edit, export and email wait while the archive is unproven. The old unscoped list is cleared once, with a notice. |
| `0da2cd6a` | 5 — SPA export/email accounting | The export and email notices report what the daemon confirmed for the selection at the click, naming rows that could not be found in this archive, rows without a QSO ID and collapsed duplicates; unconfirmed results never claim success, and an email marks only stamped rows. |

- **Migrations:** none (no change under `internal/database/`).
- **Configuration:** none (no change under `internal/config/`; config version 6 as at alpha.4).
- **API:** as above; `docs/v2-design/api-endpoints.md` and `docs/ft8.md` were updated in each
  slice's commit.
- **Packaged commands and install:** unchanged.
- **Manual:** `manual/content/chapters/qso-archives.md`, Session lists per archive and the export
  and email reports.

## Gate A — ready to deploy

Case definitions are the alpha.4 record's rows of the same ID unless stated. "Carry forward" cites
the alpha.4 result and gives why it still holds for this artifact; "rerun" executes the alpha.4
definition against alpha.5.

| ID | Disposition | Rationale | Evidence | Result |
|---|---|---|---|---|
| `A1-01` | rerun | Candidate identity and build check: the artifact must be alpha.5's | Execution log #1–#3: `ci:local` exit 0; the CI mirror rewrote the tracked `dist/index.html` again (alpha.3 Finding #1), restored before the build; `git status --porcelain` empty, `git describe` `v2.0.0-alpha.5`. One build: RPM `station-manager 2.0.0~alpha.5-1 x86_64`, 9,631,676 B, SHA-256 `3c10b6e1…693a9c`, copied to the versioned path (0600) with the SHA re-verified; alpha.4's held RPM unchanged (`cebcd305…`) | **PASS** — operator, 2026-10-24: independently verified the tag target, RPM metadata, permissions, size, RPM and packaged `smd` hashes, private-build marker and upgrade ordering; alpha.4's RPM hash unchanged; build and gate results accepted from the execution record. Restoring `dist/index.html` before the clean build is an acceptable workaround for the open finding |
| `A1-02` | rerun | Version readings identify the artifact | Execution log #6, #7: `rpm -q` `station-manager-2.0.0~alpha.5-1.x86_64`, `GET /v1/version` `2.0.0-alpha.5`, the startup line and the sidebar badge (operator) all read alpha.5; schema `{18, dirty:false}` after the first start | **PASS** — operator, 2026-10-24: the four version readings agree, schema 18 clean |
| `A1-03` | **revised** — review the delta above | A new delta, alpha.4 → alpha.5 | Delta drafted above | **PASS** — operator, 2026-10-24: the 11-commit delta accurately identifies the five code slices; no database, configuration, packaging-script or dependency changes |
| `A1-04` | rerun | Run with the artifact's `smd`. No configuration code changed, so a different result would be a finding in itself | Execution log #4: control exit 0 ("loads and validates as startup would; 0 enabled forwarder(s) construct", as at alpha.4); top-level mutation exit 1 naming only `typo_key_a1_04`; nested exit 1 naming only `bridge.typo_nested_a1_04`, "(values omitted)"; no unexpected disclosure matches; nothing written; live config, service and file names unchanged | **PASS** — operator, 2026-10-24 (execution log #4): the control and both rejection cases reached their intended outcomes, with scratch isolation documented |
| `A1-05` | rerun | Retires the B1-01 waiver only with this artifact's `smd` | Execution log #4: fixture a (config version 2, qrzcq `["insert","update","delete"]`, scratch-only `allow_insecure_http`) exit 0, "3 enabled forwarder(s) construct"; permutation `["update","insert","delete"]` exit 1: `forwarder "qrzcq": type "qrzcq" does not support action "update" (supports [insert])`; nothing written; live untouched | **PASS** — operator, 2026-10-24 (execution log #4). Retiring the alpha.2 waiver still requires B1-01 |
| `A2-01` … `A2-07` | rerun, as one clean-lane pass | Installation evidence must identify alpha.5, and the first-run SPA boot changed (`main.ts`, `archives.svelte.ts`: store binding and the action gate) | Execution log #6–#8 (`smtest3`, profile `ff-smtest3`): install, service, health, version, listener and file modes as specified, one expected `warn` (alpha.4 Finding #1); A2-02…A2-06 as expected per the operator, Station Events between Logbook and Settings, first QSO stored (`Default` 0 → 1); A2-07 package removed, data retained. Finding #1 (manual) raised at A2-06, not blocking | **PASS** — operator, 2026-10-24: installation, first-run setup, persistence, first logging and removal with data retained. A2-06's Settings journey is discoverable through the app; the manual gap stays deferred under Finding #1 and W-0018 |
| `A2-08` | rerun | Installation evidence (alpha.2 → candidate upgrade). No migration changed, so the expected counts are alpha.4's | Execution log #9 (`smtest4`, profile `ff-smtest4`): alpha.2 → alpha.5 per §7; `config-check` exit 0; config 3 → 6 and schema 8 → 18 once; setup kept; the same 3 QSOs; a second restart re-ran nothing; browser lands on Phone/CW, badge alpha.5, Session reset notice and Session (0) | **PASS** — operator, 2026-10-24: one-time migrations, setup and the three QSO UUIDs retained, Session empty after the legacy reset. Archive-switch isolation still awaits B2-09–11 |
| `A3-01` … `A3-05` | pending, unchanged definitions | Never executed at alpha.4 | Execution log #10 (2026-10-28, plan revision 3): A3-04 rollback RPM verified and the rollback recorded in the header; A3-05 window 10:34Z–14:00Z (operator; no loadshedding), station idle, station-needed-by up to a week; A3-03 authoritative counts from the verified backup copies (Home `Default` 8,130; Drill 1; uploads all `uploaded`; events 1 per archive; bindings Home clublog/qrz/smcloud on, qrzcq off, Drill smcloud on), consistent with the API observations; A3-01 backup `77d969f0…`; A3-02 extraction identical to the post-shutdown baseline and integrity ok; live directory preserved (70 paths, every hash); old daemon restarted | **PASS** — operator, 2026-10-28: verified recovery backup, complete snapshot comparison, clean shutdown, authoritative counts, preserved rollback RPM and sufficient recovery time |
| `A4-01` | **revised** — ratify the B2 changes below | The B2-10 and B2-11 expected results change with the fix; four cases are added (B2-17 … B2-20). The A4 parameters carry forward | Revised inventory with two amendments (B2-18's verifiable legacy fixture; B2-19 and B2-20 as automated-evidence rows for FT8 and email) and the guest sequence in Scope | **PASS** — Codex inventory review requested by the operator, 2026-10-24. This ratifies the case definitions and the guest sequence, not their results or either release gate |

### A3 outage plan — revision 3, for operator review (2026-10-24)

Revision 3 reinstates the clean-shutdown and sidecar check that revision 2 dropped (operator: `inactive`
and `MainPID=0` do not establish that committed changes were checkpointed out of a WAL — [SQLite, How
WAL Works](https://www.sqlite.org/wal.html#how_wal_works); demonstrated by the operator on disposable
databases: with committed rows left in a copied WAL, `immutable=1` reported integrity ok and 1 row
where ordinary read-only access reported 2) and makes the extraction check cover the complete path set.
Revision 2 applied the operator's four corrections to the first draft: a content baseline taken after
shutdown, A3-03's API readings scoped honestly with the stopped backup as the authority, differences
between checkpoints explained rather than forbidden, and a defined failure exit. **Agreed for this
occasion (operator, 2026-10-24):** station-account SM Cloud support, listing and export reads for
B2-15/16, with local writes confined to the verified scratch copies. Rollback RPM independently
verified by the operator: version `2.0.0~alpha.3.262.g6217da21.dirty-1`, SHA-256 `2d3a0ec9…e180bdb`.
**Not yet approved:** the stop and the live upgrade; the window awaits the operator's available UTC
interval. **Station-needed-by (operator, 2026-10-26):** the station can be offline for up to a week if
needed, so recovery time is not the constraint; the recovery reserve is that week.

No RF, rig command or FT8 activity is part of the plan; the station is idle throughout. No live
database file is opened directly; every file-level check runs on scratch copies. Every artifact that
may hold credentials (backup archive, scratch copies, the records' directory) is created with
restrictive permissions from the outset (`umask 077`; directories 0700, files 0600), outside the
repository, under the home directory.

**Before the stop (daemon running, station idle) — observations, not the authority:**

1. **A3-04.** The preserved dev RPM verified (SHA-256 `2d3a0ec9…e180bdb`, `rpm -qp` version as above).
   Rollback recorded in the header: `systemctl --user stop smd`; `sudo rpm -Uvh --oldpackage <dev rpm>`;
   `systemctl --user daemon-reload`; `systemctl --user start smd`; then `GET /v1/version` (the dev
   build's version and Home) and `GET /v1/healthz` ok. No data downgrade: alpha.5 adds no migration.
2. **A3-05.** The operator records the window, the station-needed-by deadline, and recovery time
   reserved beyond the 20–30-minute stop.
3. **API observations, labelled with their scope:** `GET /v1/qso-archives` (QSOs per logbook per
   archive, from the catalogue summaries); `GET /v1/forwarder-queues` (the **active archive's** binding
   backlog counts — not every upload-row status); `GET /v1/station-events?limit=1000` (the retained
   ring; no pagination); `GET /v1/qso-archives/{Home}/bindings` (Drill, inactive, answers 409 and is
   not read here). Forwarders may change queues while the station is idle, so these are reference
   points only.

**The stop (agreed outage):**

4. `systemctl --user stop smd`; record the time; confirm `inactive` (`MainPID=0`) and a **clean
   shutdown** — the journal shows the daemon's own orderly exit (no kill, no failure result). Then
   check **every database file** in the working directory for `-wal`, `-shm` and `-journal` sidecars:
   - the active databases (`station-manager.db`, `reference.db`, `evidence.db`) and Drill's
     `qso-archives/01a15d43-….db` must have **none** after a clean shutdown;
   - **known pre-existing pair**, recorded 2026-10-24 12:10Z with the daemon running:
     `db/reference-premigrate-20260723-024757.db-wal` (0 bytes, SHA-256 `e3b0c442…7852b855`) and `-shm`
     (32,768 bytes, SHA-256 `fd4c9fda…549389eb`), both last modified 2026-07-28 — a migration-era
     leftover the daemon does not open. It passes only if both are byte-identical to that reading; it is
     preserved untouched, and that database is never opened;
   - any other sidecar, or any change to the known pair, **stops the plan before any `immutable=1`
     read**: the sidecars are preserved exactly as found (never deleted or checkpointed to satisfy the
     check), the daemon stays stopped, and the operator reviews.
   The dated copies `db/20260722/` and `db/20260723/` are inventoried and hashed but never opened.
5. **Preservation baseline (after shutdown).** Under `umask 077`, record every file's relative path and
   SHA-256 in the stopped working directory to `~/sm-a3/baseline.sha256` (a 0700 directory outside the
   live directory). This, not any earlier reading, is the snapshot to preserve.
6. **A3-01.** `tar -czf ~/sm-backup/station-manager-<UTC>-g6217da21-pre-alpha5.tar.gz` of the complete
   working directory (0600 from creation, outside the repository); its SHA-256 and `tar -tzf` count
   recorded; the 2026-10-21 backup kept.
7. **A3-02 and A3-03 (authoritative).** In `~/sm-a3/scratch` (0700, absent before): verify the archive's
   SHA, extract, and verify the extraction against the step 5 baseline as **complete sets**: the
   relative-path set must be identical (a missing or extra file fails) and every file's SHA-256 must
   match. Then, on
   those verified copies opened read-only with `immutable=1`: `PRAGMA integrity_check` = `ok` for each
   database it opens (Home's `station-manager.db`, Drill's file, `reference.db`, `evidence.db`; not the
   premigrate file or the dated copies); and the authoritative inventory and counts — QSOs per logbook per archive (Home and Drill
   files), `qso_upload` rows by forwarder and status, `operator_event` rows per category, bindings per
   archive (Home and Drill) — recorded without contents. Differences from step 3's observations are
   explained (uploads processed, events added, catalogue-summary lag), not required to be zero.
8. **B2-15a…c and B2-16a…c** in `~/sm-a3/c` by checklist C's fixture method with alpha.5's `smd`
   (A1-04's extraction): separate `restore/` and `provision/` copies made from the verified extraction,
   paths rewritten into each copy, and the resolved-path inventory (`build/private/c-paths`) showing
   every write target inside the copy (live control: all outside). SM Cloud reads as agreed above.
   Expected results as the alpha.4 definitions; their counts are compared with step 7's.
9. **Preservation check, before any restart:** re-hash the live working directory and compare with the
   step 5 baseline as complete sets (paths and hashes) — identical expected. Then clean up: credential-bearing copies `shred -u` (btrfs
   caveat as A1-04), `~/sm-a3/scratch` and `~/sm-a3/c` removed; the baseline and count records kept.
10. **Restart the old daemon:** `systemctl --user start smd`; `GET /v1/version` (dev build, Home, new
    `instance`), `GET /v1/healthz` ok; the step 3 readings repeated and any difference from step 7
    explained (the restart may move queue state).

**Failure exit.** Any failed integrity, isolation (a write target outside a copy), restore or
provisioning check **holds B1**. If step 9 shows the live directory identical to the baseline, restart
the old daemon (step 10) and stop for review. If it does not, **do not restart**: preserve the live
directory, the baseline, the backup and the scratch evidence as they are, and stop for the operator's
review.

**Then B1, separately, on the operator's go:** B1-05 (a fresh copy of the live `config.json` through
alpha.5's `config-check`), B1-01 (install.md §7 while the old daemon runs), B1-02…B1-04 and B1-06, then
B2-12 and B2-14a (read-only). B1-04's comparison is against step 7's authoritative counts, with
differences explained.

**Estimate:** steps 4–10 about 20–30 minutes, most of it B2-15/16; recovery time is reserved beyond that.

## Gate B — deploy and accept

B1 rows: the alpha.4 definitions run on this artifact (none was executed at alpha.4). Executed
2026-10-28 in the A3 window (execution log #11), in this order: B1-05, B1-01, B1-02…B1-04, B1-06.

| ID | Evidence (2026-10-28) | Result |
|---|---|---|
| `B1-05` | 10:44Z, a 0700 scratch: the RPM's SHA-256 verified (`3c10b6e1…693a9c`) and alpha.5's `smd` extracted (`b91bc34b…`); a fresh copy of the live `config.json` (identical by `cmp`; `899c9978…`, the content A1-04 checked) through `SM_WORKING_DIR=<empty> smd config-check --config <copy>`: **exit 0**, "no unrecognised keys; the file loads and validates as startup would; 0 enabled forwarder(s) construct"; nothing written; the copy shredded and the scratch removed | **PASS** — operator, 2026-10-28 |
| `B1-01` | The operator ran `sudo dnf install <alpha.5 rpm>` (the agent has no non-interactive sudo): "Upgrading station-manager 2.0.0~alpha.5-1 … replacing 2.0.0~alpha.3.262.g6217da21.dirty-1", "skipped OpenPGP checks" (expected), "Complete!", no scriptlet output. The old daemon kept serving through the install (PID 27301, the deleted dev binary, instance `dmgeovvsg2gi`); `/usr/bin/smd` `b91bc34b…`; `smd config-check` on the live config **exit 0**; `systemctl --user daemon-reload` exit 0; `restart` 10:46:16Z, answering 10:46:17.7Z: PID 28837, `NRestarts=0`, `2.0.0-alpha.5`, instance `dmget66413ge`, Home, schema `{18, dirty:false}`, health ok. The open tab reconnected by itself within seconds (operator). Startup `warn` lines identical in kind to the dev build's own starts today (RTS/DTR advisory, plain-http SM Cloud notice, rig quiet while the rig is unreachable); no `error`. **Retires the alpha.2 B1-01 waiver** with A1-05 (on the operator's ruling) | **PASS** — operator, 2026-10-28. **The alpha.2 B1-01 waiver is retired** (with A1-05) |
| `B1-02` | `rpm -q` `station-manager-2.0.0~alpha.5-1.x86_64`, `GET /v1/version` `2.0.0-alpha.5`, startup line `smd starting` from `2.0.0-alpha.5`, sidebar badge `2.0.0-alpha.5` (screenshot `Screenshot_20261028_124721.png`). **Deviation, then the prescribed step:** the open tab had reconnected after the restart without reloading, so it still ran the dev build's code while its badge showed `2.0.0-alpha.5`; opening Settings rendered a blank page, the console showing the dev build's chunk `Settings-ZxPXJqMH.js` refused as `text/html` (the server's SPA fallback for a missing asset; screenshot `Screenshot_20261028_124937.png`). Assets are served `Cache-Control: no-cache`, so this was the unreloaded tab, not the browser cache. The deliberate reload (F5) then loaded alpha.5's code and Settings rendered (Finding #4) | **PASS** — operator, 2026-10-28, after its prescribed deliberate reload (Finding #4) |
| `B1-03` | After the reload (operator; screenshots `Screenshot_20261028_125058.png`, `_125120.png`, `_125148.png`, `_125205.png`, `_125240.png`): no setup or Welcome page; lands on Phone / CW; header `Archive Home`, `Logbook Default (8,130)`, `Rig Yaesu FTdx10`; dark theme and the collapsed rail as before; FT8/FT4 still listed. Archives: Home Active 22.3 MB, `Default` 7Q5MLV 8,130; Drill Inactive 148 KB, `Drill` M0CMC 1. Forwarding: ClubLog, QRZ.com and SM Cloud ENABLED, QRZCQ.com DISABLED; station account "SM Cloud service and token". Rigs: FT-710, FTdx10 (DEFAULT, its model, DATA-U, MY_RIG, serial port and audio devices intact), IC-7300. Station Events: the one event, "Archive switched" Home 21/10/2026 09:19:17 by the dev build. Session (0) — the per-archive list starts empty (whether the one-time reset toast showed was not observed) | **PASS** — operator, 2026-10-28 |
| `B1-04` | API 10:47:43Z against A3-03's authoritative counts: Home `Default` 8,130, Drill 1; all four queues 0 waiting, 0 failed; 1 station event (`archive.activated` 2026-10-21); bindings clublog/qrz/smcloud on, qrzcq off, `restart_required` false — all equal. Startup diagnostics: only the per-start "starting migrations" / "databases open and migrated" lines, no migration, adoption, seeding or config rewrite (schema 18, config 6); no `error` | **PASS** — operator, 2026-10-28 |
| `B1-06` | 10:52:57Z `smctl restart` ("SM Restarted.", exit 0): PID 28837 → 30527, `NRestarts=0`, instance `dmget66413ge` → `dmgeya74dzhr`, alpha.5, Home, health ok by 10:53:05Z. The open Phone / CW tab recovered by itself within seconds, no overlay, no reload (operator) **Per-stream re-check (operator's corrections applied):** the reloaded tab on **FT8** (profile claimed, rig off, TX unarmed, nothing pressed), DevTools Network with Persist Logs, filter `events`; `smctl restart` 11:00:47Z ("SM Restarted.", exit 0), PID 30527 → 32503, instance `dmgeya74dzhr` → `dmgf4aby95s2`, alpha.5, Home. The three pre-restart streams ended (the FT8 stream at the restart; two `events` requests `NS_ERROR_NET_PARTIAL_TRANSFER`). Three new `eventsource` requests, each **200 OK**, `text/event-stream`, response `Date` 11:00:52Z, identified by their request URLs (screenshots `Screenshot_20261028_130222.png`, `_130401.png`, `_130504.png`, `_130520.png`, `_130602.png`, `_130644.png`, `_130717.png`): **`/v1/ft8/events?mode=ft8`** — post-restart message `{"archive_id":"01a0c8cb-904f-7394-afca-ef922b41bbfb"}` (the identity frame) at 11:00:52.511Z; **`/v1/rig/events`** — post-restart messages `{"code":"rig_no_data"}` (rig disconnected, valid with the rig off) at 11:00:53.794Z, then `{"count":2}` 11:01:01.722Z and `{"count":1}` 11:01:36.725Z (the agent's daemon-side capture joining and leaving); **`/v1/events`** (the log/shell stream, `lib/api/log-events.ts:84`) — no listed message (Firefox does not list SSE comments), but its size grew in 13-byte steps (117 → 143 → 156 → 169 B), one `: keepalive` per 30 s; the other two streams grew by the same 26 B between screenshots a minute apart. Daemon-side corroboration (11:01:01–11:01:37Z, the agent's own clients; no FT8 subscriber opened): `/v1/events` 200 `text/event-stream`, `: connected`, `: keepalive`; `/v1/rig/events` 200, `: connected`, `rig-disconnected` `{"code":"rig_no_data"}`, `rig-clients`, `: keepalive`. The page recovered without a reload and with no overlay (FT8 Idle, rig pill "CAT required") | **PASS** — operator, 2026-10-28: the re-check establishes successful browser reconnection and post-restart activity on all three streams, plus page recovery without reload; no further restart needed |

### B2 — dispositions

| ID | Disposition | Rationale |
|---|---|---|
| `B2-01` … `B2-04`, `B2-06` … `B2-08` | rerun on the guest | Executed at alpha.4 but never ruled. The archive-switch flows they exercise now pass through the changed boot binding, the request guard on submit and the Session action gate |
| `B2-05`, `B2-13b`, `B2-14b` | rerun as record cases | Cite this candidate's automated evidence (A1-01's `ci:local`); the named tests are unchanged |
| `B2-09` | rerun | Control case for B2-10 and B2-11 on this artifact. Expected result unchanged (alpha.4 definition) |
| `B2-10` | rerun, **expected result revised** (below) | The alpha.4 FAIL is what slices 4 and 5 fix |
| `B2-11` | rerun, **expected result revised** (below) | As B2-10 |
| `B2-12`, `B2-14a` | pending, live, unchanged definitions | Never executed |
| `B2-13a`, `B2-15a` … `B2-16c` | pending, unchanged definitions | Never executed |
| `B2-17` | **new** | The same-archive missing-row export the alpha.4 triage required for Finding #6 |
| `B2-18` | **new** | The one-time legacy reset notice (slice 4), observable only in a tab that still holds an alpha.4 Session list |
| `B2-19` | **new**, record | FT8 archive and engagement isolation as automated evidence; not observable on the guest |
| `B2-20` | **new**, record | Email accounting as automated evidence, including partition rejection and stamp failure; the guest has no SMTP account |

### B2 execution — 2026-10-24

**Lane and profiles.** **clean-guest** is the `smtest4` lane, upgraded alpha.2 → alpha.5 at A2-08:
Home adopted (legacy) with one logbook, `Default` (M0CMC), 3 QSOs — the live station's shape, as
at alpha.4. Each group opens one fresh Firefox profile, created empty and actually used:
`~/sm-guest/ff-b2a5` for B2-01…B2-08, and another for B2-09…B2-11 in which the same tab is kept
throughout. Durable QSO counts and UUIDs are re-read before every comparison. B2-05 stays automated
evidence tied to alpha.5; B2-06 and B2-07 carry forward the approved guest-only interruption and
fixture procedures (browser timing checkpoint, full fixture restoration). Visible fields are recorded
apart from API-only evidence.

| ID | Evidence (2026-10-24) | Result |
|---|---|---|
| `B2-01` | 10:01–10:03Z, profile `ff-b2a5` (empty before launch; 60 entries after), instance `dmcz4s93szza`. **Visible** (screenshot `Screenshot_20261024_120311.png`): header `Archive Home` above `Logbook Default (3)`, `Rig not set`; badge `2.0.0-alpha.5`. Settings → Archives: Home `Active`, `160 KB`, last written `24/10/2026, 11:51:08` (local; 09:51:08Z); logbook `Default` [Default] `M0CMC` `3 QSOs`; no contents note. **API only** (`GET /v1/qso-archives`, 10:01Z): ownership `legacy`, state `active`, `contents_status` `current`, size 163,840 B, `qso_count` 3. As at alpha.4 the SPA shows neither ownership nor a `current` status (alpha.4 B2-01 wording gap; unchanged code) | **PASS** — operator, 2026-10-24; ownership and contents status are API-only evidence |
| `B2-02a` | 10:06Z, `ff-b2a5`. **Visible** (operator): **Create archive** stays disabled while any field is empty (the SPA keeps Create disabled; it does not refuse with a message). **API**: the catalogue unchanged, one archive (Home, active, `Default` 3), instance `dmcz4s93szza` | **PASS** — operator, 2026-10-24 |
| `B2-02b` | 10:06:56Z, `ff-b2a5`. **Visible** (screenshot `Screenshot_20261024_120707.png`): a second row `Contest` `Inactive` `148 KB`, last written `24/10/2026, 12:06:56` (local), with an **Activate** button; its logbook `B2 Log` [Default] `M0CMC` `0 QSOs`; Home unchanged (`Active`, `Default` 3); header still `Archive Home`. **API** (10:07Z): Contest `01a16d60-b42e-7c7c-a31e-c42011101242`, ownership `managed`, state `inactive`, `contents_status` `current`, 151,552 B, `B2 Log` 0 QSOs; no restart (instance still `dmcz4s93szza`, serving Home) | **PASS** — operator, 2026-10-24 |
| `B2-03` | 10:08Z, `ff-b2a5`. An unlogged Phone / CW entry, VK9XX (Time On 10:08:11), then **Activate** on Contest. **Visible** (screenshot `Screenshot_20261024_120845.png`), the prompt: "Switch to the archive “Contest”? This window has an unlogged QSO on Phone / CW: VK9XX (Time On 10:08:11). OK discards this window’s entry and switches; Cancel keeps it. Station Manager restarts to open it (about 5 seconds; the connection drops briefly), then every open window reloads: unlogged work in any other open window — a Phone / CW QSO, an FT8 / FT4 exchange not yet completed, unsaved Settings edits — is lost. Logged QSOs are never affected. Transmit must be idle and FT8 disarmed; nothing transmits until the restart completes." Operator answered **Cancel**; VK9XX still in the form. **API** (after Cancel): instance `dmcz4s93szza`, serving Home; Home `active` (`Default` 3), Contest `inactive` (`B2 Log` 0) | **PASS** — operator, 2026-10-24 |
| `B2-04` | 10:10Z, `ff-b2a5`, two tabs: tab 1 with VK9XX unlogged on Phone / CW, tab 2 on the Logbook. **Activate** Contest → **OK**. **Visible** (operator): both tabs reloaded by themselves; the header reads `Archive Contest`; VK9XX gone. Station Events (screenshot `Screenshot_20261024_121252.png`): "1 event" — **Archive switched** [Notification], `Contest`, `24/10/2026, 12:10:27` (local) · `2.0.0-alpha.5`; header `Archive Contest`, `Logbook B2 Log (0)`. **API**: new instance `dmczjkfaekv4` (was `dmcz4s93szza`), alpha.5, schema 18 clean, archive Contest (`managed`); Home `inactive` (`Default` 3), Contest `active` (`B2 Log` 0); `GET /v1/station-events`, served from the active archive, returns `archive.activated` 10:10:27.36Z, `{"archive_id": "01a16d60-b42e-7c7c-a31e-c42011101242", "label": "Contest"}` — recorded in Contest's file | **PASS** — operator, 2026-10-24 |
| `B2-05` | **Automated evidence**, tied to alpha.5: `frontend/app/src/lib/config/archives.svelte.test.ts` "A4 a Log in flight refuses before the prompt" (line 472 at the tag) and "A4 a Log started while the prompt is open refuses before the request" (line 483), both present at `v2.0.0-alpha.5` and passing in A1-01's `task ci:local` (vitest 2329/2329, 156 files) and CI run 40077880913 on `0da2cd6a`. Not a manual browser pass | **PASS** — operator, 2026-10-24 — **automated evidence** |
| `B2-06` | 10:15–10:22Z, `ff-b2a5`, two tabs. Home active first by a normal activation (instance `dmczn85nvjic`, PID 11761). Guest watcher (`~smtest4/b2-06-watch.sh`, recreated from the alpha.4 record; armed 10:18:07.188Z) polled `smd`'s SubState every 50 ms. The operator activated Contest → OK from tab 1 (Settings → Archives). `smd` exited status 3 at 10:18:55.119Z; the watcher saw `auto-restart` and `systemctl --user stop` completed at 10:18:55.174Z, inside `RestartSec=5s`. **Checkpoint met, from the guest:** no `Started` line between the stop and the agent's start, so no replacement instance existed for the browser to observe; guest `8080` refused meanwhile. **Evidence-method deviation (operator, 2026-10-24):** the planned `202`/`version` capture in the activating tab was missed — the DevTools Network log was open in tab 2. The recorded stop-before-restart timing, the absence of a replacement process and the refused port are accepted as sufficient timing evidence for this run. **Visible** (screenshot `Screenshot_20261024_121952.png`), tab 1: overlay "Archive binding unproven — Station Manager cannot tell which archive the daemon serves this page’s data from. The daemon did not answer as a new instance within the wait. Logging and transmitting are paused so nothing lands in the wrong archive. Reload to continue on the archive the daemon is serving." with **Reload now** (not pressed); logging in tab 1 unreachable behind it. **Observation, tab 2** (Phone / CW, which had not initiated the switch): a Log QSO attempt was sent and failed at the transport — toast "Cannot confirm the outcome — the connection to the daemon failed mid-submit; the QSO may still have been logged. Check the Logbook before retrying." (screenshot `Screenshot_20261024_122100.png`). That tab learns of a switch only when its event stream reconnects, which cannot happen while the daemon is down; nothing was stored (Home 3, Contest 0 after the start). **This attempt did not exercise the archive guard:** it failed at the transport. The guard's protection against a replacement daemon serving Contest is supported separately by code and automated tests (slice 1; B2-19); the observation does not contradict the accepted design (operator, 2026-10-24). The agent started `smd` at 10:21:28.365Z; it answered at 10:21:28.631Z as `dmczs03q94xb`, archive Contest. **Both tabs reloaded by themselves onto Contest** (operator), without Reload now | **PASS** — operator, 2026-10-24, with the recorded evidence-method deviation: the activating tab was gated and recovery was observed |
| `B2-07a` | 10:55–10:57Z. Home activated normally by the operator (instance `dmd0i55j99y0`; `pending_qso_archive_id` absent). **Fixture (agent, daemon stopped, approved method):** `systemctl --user stop smd` 10:56:01.1Z; `config.json` preserved to `~smtest4/b2-07-hold/config.preserved.json` with a SHA-256 manifest of `db/`; Contest's file `db/qso-archives/01a16d60-b42e-7c7c-a31e-c42011101242.db` moved to the hold directory (no `-wal`/`-shm`/`-journal` existed); only `pending_qso_archive_id` set to Contest (parsed diff: that key alone); started 10:56:01.2Z. **API:** Home serves (instance `dmd0igc77goh`, `legacy`), the **active selector** fallback; Contest `inactive`, `last_activation_code` `archive_file_missing`, "The archive's file is missing.", `contents_status` `stale`; Home's `GET /v1/station-events` `archive.activation_failed` 10:56:01.196Z `{"label": "Contest", "code": "archive_file_missing"}`; `config.json` pending cleared, Contest's entry `last_activation_error` `archive_file_missing`; one `error` log line naming the missing path. **Visible** (screenshots `Screenshot_20261024_125647.png`, `_125709.png`): header `Archive Home`; Archives: Contest ⚠ `Inactive`, size and last written `—`, note "Counts may be out of date — the file changed since it was last open" (alpha.4 Finding #8 recurring, unfixed lower-severity follow-up); the ⚠ hover text (read after the restore, before Contest's reactivation) "Last activation failed: The archive's file is missing". Station Events: "Archive switch failed" [Notification] "Contest · the archive file is missing" 12:56:01 (local), above Home's two "Archive switched" entries — B2-04's Contest event is absent from Home's list, consistent with its being recorded in Contest's file | **PASS** — operator, 2026-10-24; the stale-file wording is alpha.4 Finding #8 recurring |
| `B2-07b` | 10:57–10:59Z. **Agent, daemon stopped:** stop 10:57:32.4Z; Contest's file moved back from the hold directory to `db/qso-archives/`; its SHA-256 identical to the pre-fixture manifest; `config.json` against the preserved copy: Home's entry unchanged, Contest's entry differs only by `last_activation_error` (the recorded error), the pending selector cleared; started 10:57:32.5Z, Home serves (`dmd0jm9wg5bn`); Contest then still `stale` with `archive_file_missing` from the failed attempt. **Operator** activated Contest normally: instance `dmd0kg6pwoz9`, Contest `active` (`managed`), `contents_status` `current`, `last_activation_code` cleared (API and `config.json`), `B2 Log` 0; Home `inactive`, `current`, `Default` 3, path unchanged; pending absent. **Visible:** the ⚠ and the "Counts may be out of date" note gone from Contest's row. Contest's Station Events (screenshot `Screenshot_20261024_125911.png`): three "Archive switched — Contest" entries, 12:10:27 (B2-04), 12:21:28 (B2-06 recovery start) and 12:58:37 (local), none of Home's — each archive's events stay in its own file. B2-08's starting state verified | **PASS** — operator, 2026-10-24 |
| `B2-08` | 10:59–11:00Z. The operator activated Home. **API:** instance `dmd0l8sakxer`, archive `01a16d52-1c87-7dfd-b5ec-7caba7193539` Home, ownership `legacy` — the same id as adopted at A2-08; catalogue of 2 entries, Home's `path` unchanged (`db/station-manager.db`); the adoption line ("existing installation adopted in place") appears once in `smd.log`, from A2-08's first start (no re-adoption); `Default` 3 with UUIDs identical (diff) to the A2-08 baseline (DR1LL `01a16d51-6380-…`, DR2LL `…-88e0-…`, DR3LL `…-a5b9-…`); Contest `inactive`, `current`, `B2 Log` 0. **Visible** (screenshot `Screenshot_20261024_125956.png`): header `Archive Home`, `Logbook Default (3)`; Logbook `Default (M0CMC)` lists DR3LL, DR2LL, DR1LL (2026-10-24 09:50, 20m, 14.255, SSB (USB), Unknown), "showing 1–3 of 3". Side observation: the Logbook's last column headers render overlapped ("EMAIL" over "COMMENT") | **PASS** — operator, 2026-10-24; the heading overlap is Finding #2 |
| `B2-09` | 11:13–11:17Z. Profile `ff-b2s5` (empty before launch; 60 entries in use), **one tab** kept through B2-11; Home active (instance `dmd0l8sakxer`), Session 0, `Default` 3. The operator logged two Phone / CW QSOs: 7Q6C `01a16d9e-3e02-7725-afed-2bf3724f9469` 11:14:07Z and 7Q5B `01a16d9e-5bb3-7b20-b932-2ab847e4bc78` 11:14:16Z; **Session (2)**, rail badge 2, header `Default (5)` (screenshot `Screenshot_20261024_131434.png`); durable `Default` **5**. **Control:** reload (F5) → Session 2; 7Q6C's comment edited to `B2-09 edit` in the Session panel and saved; `GET /v1/qso/01a16d9e-3e02-…` returns comment `B2-09 edit` (7Q5B none); reload → Session 2, the edit kept; export → notice reported by the operator as "exported 2 QSOs"; file `session-20261024-111631.adi` (944 B, SHA-256 `8e1bd388…3ff75e`) holds exactly 2 records — 7Q5B 20261024 111416 and 7Q6C 20261024 111407 with `COMMENT` `B2-09 edit`, `STATION_CALLSIGN` M0CMC, `PROGRAMVERSION` `2.0.0-alpha.5` — and none of Home's three earlier rows. Side observation: after the edit 7Q6C's Country reads `Unknown` while 7Q5B's stays empty — alpha.4 Finding #7 recurring (W-0012; unfixed, expected) | **PASS** — operator, 2026-10-24: reload persistence, the saved edit and the file count |
| `B2-10` | 11:18–11:21Z, the same `ff-b2s5` tab. The operator activated Contest (instance `dmd0yw7xj76o`) and logged one distinguishable QSO, 7Q4A `01a16da3-ee8c-730f-ad54-1c88b1d89782` 11:20:15Z (`B2 Log` 0 → 1; Home `Default` still 5). **Visible** (screenshot `Screenshot_20261024_132042.png`): header `Archive Contest`, `Logbook B2 Log (1)`; **Session (1)** listing only 7Q4A; rail badge 1; no Home row (7Q5B, 7Q6C) listed, so none is offered for edit. Export: file `session-20261024-112054.adi` (617 B, SHA-256 `8a06dcc5…270939`) holds exactly **1 record**, 7Q4A 20261024 112015 M0CMC, `PROGRAMVERSION` `2.0.0-alpha.5`; the notice (screenshot `Screenshot_20261024_132138.png`) an info toast **"Exported 1 QSO to ADIF"**, matching the file. At alpha.4 this panel showed four rows and the toast "Exported 4 QSOs" over a one-record file | **PASS** — operator, 2026-10-24: archive isolation and file/notice counts |
| `B2-11` | 11:21–11:24Z, the same `ff-b2s5` tab. The operator activated Home (instance `dmd12jdpvv29`). **Visible** (screenshot `Screenshot_20261024_132227.png`): header `Archive Home`, `Logbook Default (5)`; **Session (2)** — 7Q5B 11:14:16 and 7Q6C 11:14:07 (Country `Unknown` on the edited row, Finding #7) — and 7Q4A absent; rail badge 2. First export: info toast **"Exported 2 QSOs to ADIF"** (screenshot `Screenshot_20261024_132246.png`); file `session-20261024-112242.adi` (944 B, SHA-256 `e01bc18f…6ac3152`) holds exactly 7Q5B and 7Q6C (`B2-09 edit`), no Contest row. That export preceded the edit step, so the operator then edited 7Q5B's comment to `B2-11 edit`, saved and reloaded: `GET /v1/qso/01a16d9e-5bb3-…` returns `B2-11 edit` (7Q6C still `B2-09 edit`); a second export, `session-20261024-112345.adi` (967 B, SHA-256 `bc59c8cb…e717be5b`), holds exactly 7Q5B (`B2-11 edit`) and 7Q6C (`B2-09 edit`). Durable counts: Home `Default` 5, Contest `B2 Log` 1 (untouched). At alpha.4 this panel showed four rows instead of three | **PASS** — operator, 2026-10-24: Home's rows restored, edits saved to Home, file/notice counts; the second export completes the post-edit check |
| `B2-17` | 11:25–, continuing the same `ff-b2s5` tab (B2-09…B2-11 sequence). The operator logged VK6YY `01a16da8-f247-7b93-ac41-9af25f9dbd82` 11:25:48Z: **Session (3)** — VK6YY, 7Q5B, 7Q6C — rail badge 3, header `Default (6)` (screenshot `Screenshot_20261024_132941.png`); durable Home 6. **Fixture preparation (agent, 11:33:49Z, amended method):** Home active (instance `dmd12jdpvv29`); guarded `GET /v1/qso/01a16da8-…` 200 (VK6YY 20261024 112548, logbook 1); `DELETE` with `X-SM-Archive-Id` `01a16d52-1c87-7dfd-b5ec-7caba7193539` → **204**; guarded `GET` → **404** `not_found`; durable Home **5**. **Export 1** (no reload; Session still 3): warning **"Exported 2 of 3 selected QSOs. 1 could not be found in this archive."** (screenshot `Screenshot_20261024_133510.png`); file `session-20261024-113508.adi` (967 B, SHA-256 `2ada5a5a…aa6cbc24`) holds exactly 7Q5B (`B2-11 edit`) and 7Q6C (`B2-09 edit`); VK6YY still listed, Session (3) (screenshot `Screenshot_20261024_133546.png`). **Export 2** (no reload): the same warning (operator); file `session-20261024-113921.adi` (967 B, SHA-256 `aaa3b84b…f356ea76db`) holds the same two records. **After a reload:** VK6YY still listed, Session (3). Side observation: until that reload the header read `Logbook Default (6)` while the durable count was 5 — the delete was made through the API with no SPA action, and the reload showed `Default (5)`. This matches the deferred event-driven count updates in [ADR 0079](../decisions/0079-shell-events-stream-reconnect-refresh.md) ("QSO-event-driven live updates … Deferred, not rejected") and does not invalidate B2-17 (operator, 2026-10-24) | **PASS** — operator, 2026-10-24: both exports contain exactly the two surviving rows, repeat the correct warning, and VK6YY stays listed, also after a reload |
| `B2-12` | 2026-10-28 10:55–10:57Z, live after B1, read only, the reloaded tab. **API** (Home bindings): clublog on (account configured, build key present), qrz on, qrzcq off, smcloud on (support `supported`, adoption `adopted` "Adopted."); each `Default` row bound, enabled except qrzcq; every queue 0 waiting / 0 failed / 0 in flight; `restart_required` false; nothing held. **Visible** (screenshots `Screenshot_20261028_125503.png`, `_125523.png`, `_125546.png`, `_125602.png`, `_125623.png`): ClubLog ENABLED — `Default · 7Q5MLV` ticked, account email, application password and callsign "✓ Saved"; QRZ.com ENABLED — ticked, API key "✓ Saved"; QRZCQ.com DISABLED — unticked, callsign and API key "✓ Saved"; SM Cloud ENABLED — "SM Cloud service and token: shared by every archive, under Station accounts", ticked, "Adopted."; Station accounts → SM Cloud service and token: service URL and bearer token "✓ Saved", no value shown. No restart-required or held banner. Matches the pre-upgrade state (A3-03, B1-03); queue counts equal A3-03's, no difference to explain. SM Cloud support state is API-only (no visible wording when supported) | **PASS** — operator, 2026-10-28, including the explicitly labelled API-only fields |
| `B2-14a` | Read only, no failed rows manufactured. Every queue is idle (0/0/0), and by design an idle row shows **neither its queue line nor Retry failed / Clear** (`DestinationsSection.svelte:467`, "Only a row with work shows its queue … (declutter ruling 2026-09-26)"; unchanged since the tag). So no Retry failed button is visible on any card. The case's expected result ("disabled where the failed count is 0") assumed a visible button; the button's own rule (`disabled` when `failed === 0`, a retry in progress or uploads held; line 489) applies only while the row has work. For the operator's ruling: accept absence on idle rows as meeting the intent, or keep pending until a real backlog occurs | **PASS** — operator, 2026-10-28, with the case wording corrected: idle rows hide the queue controls; rows with work apply the disabled/enabled rules (`DestinationsSection.svelte:467`). No backlog needs manufacturing or waiting for. This passes control visibility, not the retry action |
| `B2-15a` | 2026-10-28 10:36–10:39Z (execution log #10). In the `restore/` copy only, ZS4JAN `01a15dc4-b0ea-78be-96cd-b3a96a136e2b` (id 8,130; no `qso_history` rows) deleted with foreign keys on: diff `qso` 8,130 → 8,129 (that row only), `qso_upload` 15,737 → 15,733 (its four rows, by cascade), all else unchanged; foreign-key check clean. Cloud export (the station account, read-only): 8,130 records, 0 tombstones; cloud minus copy exactly `[ZS4JAN]`, copy minus cloud none. Live untouched (step 9) | **PASS** — operator, 2026-10-28 |
| `B2-15b` | `smd restore --archive 01a0c8cb-… --logbook 1 --dry-run`: `archive:` line `Home (…, legacy) — …/sm-a3/c/restore/…/station-manager.db`; 8,130 records, 0 tombstones; "dry-run: no DB writes", exit 0; archive file SHA-256 unchanged (`9fc05d02…`); the open created an empty `-wal` and a `-shm` in scratch | **PASS** — operator, 2026-10-28 |
| `B2-15c` | Real restore: stored **1**, skipped 8,129, failed 0, exit 0. Diff: `qso` +1 (ZS4JAN, rowid 8,131), 0 existing rows changed, `qso_upload` unchanged (no upload work), `sqlite_sequence` and planner statistics moved. `c-cloud verify` matches the export (revision 2, `modified_at` 09:23:05Z, 49 payload keys). Against its origin (the verified extraction): `id` 8,131 v 8,130 (the AUTOINCREMENT sequence kept 8,130 after the deletion); `created_at` the restore time; `modified_at` the same instant in two text forms (ruled not a blocker at C); `additional_data` differs only by `csid`, absent from the cloud projection. Rerun: stored 0, skipped 8,130, failed 0; every data table unchanged (statistics only) | **PASS** — operator, 2026-10-28 |
| `B2-16a` | `provision/` copy, Drill's file removed (no sidecars), its inactive entry kept. `--list`: Home "legacy (Home; cannot be provisioned)", active, `Default` 8,130; Drill "catalogued as managed", `Drill` M0CMC 1 QSO. `--dry-run`: "would be provisioned at …/provision/…/01a15d43-….db", "the existing entry \"Drill\" kept, inactive", "nothing was written" (directory still empty) | **PASS** — operator, 2026-10-28 |
| `B2-16b` | Real provision 10:40:03Z: "1 QSO in 1 logbook", the existing entry kept, inactive, exit 0; `config.json` hash unchanged. `c-cloud verify` matches Drill's export (`01a15d4e-…`, revision 0, `modified_at` 07:13:19Z, 20 payload keys); archive and logbook UUIDs Drill's own; **0 `logbook_destination`, 0 `qso_upload`**. Diff against the backup's Drill as C recorded (new `created_at`, the cloud's `modified_at`, no event/binding/upload rows, statistics). Home in the copy unchanged in every table | **PASS** — operator, 2026-10-28 |
| `B2-16c` | Provision again, dry run and real: both exit 1, `archive_file_exists: …/01a15d43-….db already exists; it is never overwritten or removed by provisioning`; the file's hash and `config.json` unchanged | **PASS** — operator, 2026-10-28 |
| `B2-13a` | 11:43–11:45Z, the same `ff-b2s5` tab, Home active (instance `dmd12jdpvv29`). **Starting state verified (API/config):** `config.json` forwarder accounts only `clublog`, `qrz`, `qrzcq` (the packaged defaults) — no SM Cloud account; Home's `GET /v1/qso-archives/{id}/bindings` lists ClubLog, QRZ Logbook and QRZCQ (each `off`, account configured, `Default` bound, not enabled) — no `smcloud` destination or binding. **Visible** (screenshot `Screenshot_20261024_134445.png`): Settings → Forwarding "Where each logbook in archive Home uploads" shows ClubLog, QRZ Logbook and QRZCQ, each `DISABLED`; **no SM Cloud card** and no unactionable "no station account" row | **PASS** — operator, 2026-10-24 |
| `B2-13b` | **Automated evidence**, tied to alpha.5. Present at `v2.0.0-alpha.5`: `internal/archive/bindings_test.go` `TestBindings_EnableRefusals` and `TestBindings_AccountReportsBuildKeyAndReasonNamesTheGap`; the SPA's `frontend/app/src/lib/config/bindings.svelte.test.ts`. No `t.Skip` in the Go file; no `.skip`/`.only`/`.todo` in the TS file. A1-01's `task ci:local` at `v2.0.0-alpha.5` (`0da2cd6a`) passed (exit 0: vitest 2329/2329 in 156 files, every Go package run fresh) and CI run 40077880913 passed on the same commit. Not a manual pass | **PASS** — operator, 2026-10-24 — **automated evidence** (18 cited files and 30 named citations checked at the tag; no skip/focus markers) |
| `B2-14b` | **Automated evidence**, tied to alpha.5. Present at `v2.0.0-alpha.5`: `internal/api/handler_forwarder_queue_test.go` `TestRetryForwarderQueue_HappyPath`, `_Validation`, `_UsesStartupWorkerSet`, `_PreservesExactName`; `cmd/smd/lifecycle_rearm_auth_test.go` `TestLifecycle_StartRearmsAuthFailedRowsForEnabledForwarder` (the separate start-time re-arm); `frontend/app/src/lib/api/forwarder-queues.test.ts`; `frontend/app/src/lib/config/DestinationsSection.adoption.svelte.test.ts` "U5: a held row cannot retry …" (line 457). No skips in the cited files. A1-01's `task ci:local` at `v2.0.0-alpha.5` (`0da2cd6a`) passed (exit 0: vitest 2329/2329 in 156 files, every Go package run fresh) and CI run 40077880913 passed on the same commit. The manual retry and the start-time re-arm are distinct tests | **PASS** — operator, 2026-10-24 — **automated evidence** (18 cited files and 30 named citations checked at the tag; no skip/focus markers) |

### B2 cases revised or added for alpha.5 (ratified at A4-01, 2026-10-24)

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `B2-10` | yes | **clean-guest** — B2-09; switch to the other archive in the same tab (it reloads); log one **distinguishable** QSO there | Read the Session panel and the rail badge; look for any Home row to edit; export, then **open the downloaded file** | Panel and badge show only the one new QSO; no Home row is offered for edit; the file holds exactly that one record and the notice reads "Exported 1 QSO to ADIF" | Home's rows shown or offered for edit here; a notice whose count differs from the file's records. (FT8 engagement is not observable on this guest: B2-19) | pending | none | **PASS** — see B2 execution, `B2-10` (operator, 2026-10-24) |
| `B2-11` | yes | **clean-guest** — B2-10; switch back to Home in the same tab | Read the panel; edit a Home row (save and reload), and export | Home's two rows are back and the other archive's row is absent; the edit saves to Home (`GET /v1/qso/{uuid}`); the file holds Home's two records and the notice reads "Exported 2 QSOs to ADIF" | Home's list lost on return; the other archive's row listed; an edit written to the wrong archive | pending | none | **PASS** — see B2 execution, `B2-11` (operator, 2026-10-24) |
| `B2-17` | yes | **clean-guest** — Home active, three Session rows; one of them deleted from Home. **Amended at A4-01 (operator, 2026-10-24):** the SPA offers no QSO delete (Finding #3), so the agent prepares the fixture through the documented `DELETE /v1/qso/{uuid}` with Home's `X-SM-Archive-Id` (guarded `GET` 200 before, `204`, guarded `GET` 404 after, durable count and Session checked); the exports and their criteria are unchanged | Export from the Session dialog and open the file; export again | Both exports: the file holds the two remaining records and the notice is a warning, "Exported 2 of 3 selected QSOs. 1 could not be found in this archive."; the deleted row stays listed | "Exported 3 QSOs"; the row silently dropped from the list; a missing row described as deleted | pending | none | **PASS** — see B2 execution, `B2-17` (operator, 2026-10-24) |
| `B2-18` | yes | **clean-guest, legacy lane** — kept separate from the fresh-install lane (A2-01 … A2-07). **Fixture, prepared while alpha.4 is still installed in the guest:** an alpha.4 working directory whose archive holds Phone / CW QSOs logged from one tab; that tab stays open at `http://127.0.0.1:18080` (same origin, through the tunnel) and is never closed, so its `sessionStorage` survives. Session `sessionStorage` lasts only as long as the tab: a closed tab, a closed browser or a power cut loses it (alpha.4 record, B2-09 attempt 1). **Verify before the first alpha.5 load,** read-only in that tab's DevTools console: `sessionStorage.getItem('sm.session.qsos')` is non-null and lists the logged UUIDs, and no `sm.session.qsos.<uuid>` key exists; `GET /v1/qso/{uuid}` returns each of them. Then upgrade that lane's package in place to the alpha.5 RPM (its data kept) and restart its daemon, without touching the tab. If the fixture cannot be prepared or is lost before the load, recreate it from the held alpha.4 RPM (`cebcd305…`); otherwise record the case **blocked** | Reload that tab once; read the notices, the Session panel and the Logbook; reload again; read `sessionStorage` again | One notice after the first reload: "Session history was reset by this update. Your logged QSOs are intact in the Logbook."; the Session panel is empty; the Logbook holds every fixture QSO; no notice after the second reload; `sm.session.qsos` is gone and `sm.session.qsos.<uuid>` holds an empty list or is absent | The old list adopted into the archive's Session list; the notice on every load; QSOs missing from the Logbook; a fixture that was never verified counted as a pass | Execution log #5. 2026-10-24, guest clock stepped first; `smtest2` lane (alpha.4, Home, `Default` 8); new empty profile `ff-b2l` (0700), one tab at `127.0.0.1:18080`. Two Phone / CW QSOs logged: 7Q7EB `01a16d3b-1bc9-7e7d-b0a4-6ece15286ec7` 09:25:35Z and 7Q7CT `01a16d3b-378f-7186-bb88-8b4c66ad4524` 09:25:57Z; `Default` 10. **Fixture verified before the alpha.5 load:** the DevTools Storage inspector (Firefox's console blocks pasted code) showed `sm.session.qsos` holding both rows and, besides it, only `sm.session.startedAt`, no scoped key; `GET /v1/qso/{uuid}` 200 for both. Package upgraded in place to alpha.5 (09:29:28Z, `/usr/bin/smd` `b91bc34b…`), `smtest2`'s daemon restarted (instance `dmcyo9gpkfen`, Home); the tab stayed open and did not reload by itself. **First reload (F5, operator):** the reset notice shown once; Session panel empty. **Second reload:** no notice. Storage afterwards: only `sm.session.startedAt` (the unscoped key gone; no scoped key written, which the row allows). Home still `Default` 10 with both UUIDs (`GET /v1/qso/{uuid}` 200, 09:31Z, read through the daemon rather than the Logbook view) | none | **PASS** — operator, 2026-10-24: fixture verified before alpha.5 loaded, the notice once, the legacy list gone, both durable QSOs retained. Deviations accepted: the Storage inspector reads the required state; QSO retention is **API evidence**, not a visual Logbook check; manual F5 is the prescribed action |
| `B2-19` | yes | **record** — automated evidence from A1-01's `ci:local` at the alpha.5 tag | Cite the archive-boundary and engagement tests (list below) and confirm each is present at the tag and passing in that run | Every cited test present and passing | Read as a browser or on-air pass: the FT8 repeat decision runs in TX preflight and row shading reflects separate enrichment evidence, so neither is observable on this guest | **Automated evidence**, verified 2026-10-24: every test named in the B2-19 citations below is present at `v2.0.0-alpha.5` (each name found once; `archive_guard_test.go` and `archive-guard.test.ts` present), with no `t.Skip` in the cited Go files and no `.skip`/`.only`/`.todo` in the cited TS suites; A1-01's `task ci:local` at `v2.0.0-alpha.5` (`0da2cd6a`) passed (exit 0: vitest 2329/2329 in 156 files, every Go package run fresh) and CI run 40077880913 passed on the same commit | none | **PASS** — operator, 2026-10-24 — **automated evidence** (18 cited files and 30 named citations checked at the tag; no skip/focus markers) |
| `B2-20` | yes | **record** — automated evidence from A1-01's `ci:local` at the alpha.5 tag | Cite the export and email accounting tests (list below), including partition rejection and stamp failure, and confirm each is present at the tag and passing in that run | Every cited test present and passing | Read as a manual email pass; the guest has no SMTP account | **Automated evidence**, verified 2026-10-24: every test named in the B2-20 citations below is present at `v2.0.0-alpha.5`, including each partition violation "rejected before anything is marked", partial stamping and the stamp-failure cases, with no skips in the cited files; A1-01's `task ci:local` at `v2.0.0-alpha.5` (`0da2cd6a`) passed (exit 0: vitest 2329/2329 in 156 files, every Go package run fresh) and CI run 40077880913 passed on the same commit | none | **PASS** — operator, 2026-10-24 — **automated evidence** (18 cited files and 30 named citations checked at the tag; no skip/focus markers) |

**B2-19 citations** (archive boundary and FT8 engagement):

- `frontend/app/src/lib/operate/archiveStores.svelte.test.ts`: "Home → Contest → Home restores each,
  and a same-archive reload preserves both"; "a reconnect from another archive files nothing
  anywhere and verifies at once"; "a foreign frame reusing a UUID this list shows still verifies,
  writes nothing, toasts"; "a repeat delivery from the same archive adds no row, mark or toast";
  "an unlabelled frame is a protocol fault: dropped, no verification"; "releases only records
  tagged with the proven archive, merging hydrated UUIDs"; the legacy-store tests (after proof,
  before proof, a failed remove); "blocked before binding, while the gate reports a check, and
  free otherwise".
- `frontend/app/src/lib/operate/Ft8BandActivity.svelte.test.ts`: "an engagement from another
  archive does not flag a repeat here"; "Home → Contest → Home: an engagement flags a repeat only
  in its own archive" (both through `allow_duplicate`).
- `frontend/app/src/lib/config/archives.svelte.test.ts`, "the per-archive stores follow the proven
  binding": an agreeing boot binds the stores; a straddling or unreadable boot binds nothing and
  keeps the legacy key; a store write naming another archive verifies at once.
- `frontend/app/src/lib/api/ft8-sse.test.ts`: frames tagged by the identity frame; untagged before
  it; reset on every reconnect; a malformed identity clears the tag.
- `internal/ft8/handler_identity_test.go`: `TestHTTPHandler_IdentityFramePrecedesTheReplay`,
  `TestHTTPHandler_EveryConnectionNamesItsArchive`, `TestHTTPHandler_UnsetArchiveSendsAnEmptyIdentity`.
- `internal/api/archive_guard_test.go` and `frontend/app/src/lib/api/archive-guard.test.ts`: the
  request guard on submit, edit, delete, export, email and the TX-admitting FT8 routes.

**B2-20 citations** (export and email accounting):

- `internal/api/session_counts_test.go`: `TestSessionExport_CountsWhatTheFileHolds`,
  `TestSessionExport_AllMissingWritesNoBackup`, `TestSessionEmail_ReportsSentAndOmitted`,
  `TestSessionEmail_CompleteSendHasEmptyOmitted`,
  `TestSessionEmail_StampFailureAfterSendStillAccountsExactly`.
- `frontend/app/src/lib/operate/sessionAccounting.test.ts`: every `exportReport` and
  `emailReport` case, including each partition violation "rejected before anything is marked",
  partial stamping, "a stamp failure after the send marks nothing and says so", and "an
  unconfirmed send still states what the tab knows, and marks nothing".
- `frontend/app/src/lib/operate/ExportDialog.svelte.test.ts`, "export and email accounting": the
  short export, collapsed duplicates, no QSO IDs, unconfirmed counts, a row logged in flight, an
  email partition violation marks nothing, partial stamping, selection and recipient fixed at the
  click, and the Send N denominator.
- `frontend/app/src/lib/api/session-export.test.ts` and `session-email.test.ts`: count headers and
  lists parsed, never guessed.

## Findings

Numbered for this record. alpha.4's Findings #1–#4, #7 and #8 stay with their recorded destinations
(#1 recurred at A2-01, as expected: the code is unchanged); #5 and #6 are re-judged by B2-10, B2-11 and
B2-17. Severity, triage and any waiver are the operator's.

| # | Case | Observation (sanitized) | Triage | Destination |
|---|---|---|---|---|
| 1 | A2-06 | Operator, 2026-10-24: working from the guide and the manual alone, there is no Settings link or instruction to find. The manual's First Run chapter (`manual/content/chapters/first-run.md`), which should hold the tour of the main screen, is still a placeholder ("*Draft outline — content to be written.*"); `my-station.md` does not mention Settings; Settings appears only in passing ("Settings → Rigs" in `cat.md`, the sidebar order in `station-events.md`). The app itself shows Settings in the sidebar and offers **Open Settings** at the end of setup (A2-04), so no repository knowledge is needed; what fails is the manual. **Not a regression:** the alpha.4 tag has the identical placeholder (this delta changed only `qso-archives.md`), and alpha.4's A2-06 passed without recording the gap. | **Disposition: deferred to W-0018, not blocking** — operator, 2026-10-24: the manual is well out of step with the app and is left for a separate, focused piece of work; the priority now is getting the app ready for a beta release | [W-0018](../work/W-0018-bring-the-embedded-manual-to-release-readiness.md) (its outcome: "an operator can complete first run … from the manual alone") |
| 2 | B2-08 | Operator-reported, 2026-10-24: in the Logbook view the last two column headings overlap, "EMAIL" drawn over "COMMENT" (screenshot `Screenshot_20261024_125956.png`, Firefox, profile `ff-b2a5`). **Viewport evidence (recorded 2026-10-28):** zoom **100%**, recovered — the profile holds no per-site zoom (`content-prefs.sqlite` empty, read `immutable=1` with the profile closed) and no zoom or device-pixel preference in `prefs.js`; whether the window was **maximised**, and on **which monitor**: **not established from retained evidence**; the screenshot is 2,348 × 1,936 physical pixels including the window frame. *Conditional on today's display scale only:* if the window was on a display at the 1.7× scale now set on this host's 4K displays, that would be about 1,381 × 1,139 logical pixels against a 2,259 × 1,271 logical desktop. This does not establish the original logical dimensions or the maximised state, because the display configuration on 2026-10-24 is not established. Any reproduction records its own settings separately. Cause and wider impact **not investigated**. | **Disposition: non-blocking follow-up in W-0012** — operator, 2026-10-24 (not blocking the Session cases); consolidated 2026-10-28 | [W-0012](../work/W-0012-operator-experience-followups.md) |
| 3 | B2-17 | The SPA provides no individual-QSO delete action despite daemon support (`DELETE /v1/qso/{uuid}`, api-endpoints.md; the only SPA deletes are logbooks, `LogbooksSection.svelte`). B2-17's fixture was therefore prepared through the API. No recorded decision deliberately excludes it (operator, 2026-10-24). | **Disposition: non-blocking follow-up in W-0012** — a product gap for separate prioritisation (operator, 2026-10-24); consolidated 2026-10-28 | [W-0012](../work/W-0012-operator-experience-followups.md) |
| 4 | B1-02 | After the package upgrade and restart, an open tab reconnects by itself and keeps running the previous build's code, while its sidebar badge already shows the new daemon version (`2.0.0-alpha.5`). Its first lazily loaded page then fails: Settings rendered blank, because the old code requested a chunk the new build no longer has (`Settings-ZxPXJqMH.js`), and the server's SPA fallback answered that asset path with the HTML page (200, `text/html`) instead of a 404, so the browser refused the module. A reload fixes it. Nothing reached the daemon; no data affected. Cause and scope of the badge reading not investigated beyond the observed behaviour. | **Disposition: non-blocking follow-up in W-0012** — a real upgrade UX defect (operator, 2026-10-28). **Workaround: reload every open Station Manager tab deliberately after an upgrade** (B1-02's prescribed step); until then a tab may show the new version badge while running the old code | [W-0012](../work/W-0012-operator-experience-followups.md) |

## Execution log

Command-line checkpoints run by the agent; the operator rules every result. Times UTC.

1. **2026-10-24 — freeze.** CI green on `0da2cd6a` (run 40077880913); `main` and `origin/main` both at
   `0da2cd6a`. Annotated tag `v2.0.0-alpha.5` created locally at `0da2cd6a`, not pushed. A worktree
   checked out at the tag in a 0700 directory under the home directory (not `/tmp`, since it held
   the build key): `git status --porcelain` empty, `git describe` `v2.0.0-alpha.5`,
   `scripts/version.sh` → `2.0.0-alpha.5` / RPM `2.0.0~alpha.5`. The operator copied `.env` in (0600,
   Git-ignored) for the private build key.
2. **08:59:40Z–09:06:20Z — `task ci:local` in the worktree.** Exit 0; details in the header.
3. **09:06:33Z — the candidate build.** The CI mirror's SPA build had again left the tracked
   `frontend/app/dist/index.html` modified (alpha.3 Finding #1, still open); `git checkout` restored it,
   `git status --porcelain` empty, `git describe` `v2.0.0-alpha.5`. `SM_FFT=pocketfft
   scripts/dev-rpm.sh` exit 0, once: daemon `version: 2.0.0-alpha.5, FFT: PocketFFT (CGO, dynamically
   linked)`, RPM `2.0.0~alpha.5-1`, 9,631,676 bytes, SHA-256
   `3c10b6e1a7899807612db7720a0b95fe21badbb7cf21bbb8080265cd11693a9c`. Copied to
   `build/private/station-manager-2.0.0~alpha.5-1.x86_64.rpm` (0600; the copy and the source hash
   identically). `rpm -qp` `station-manager-2.0.0~alpha.5-1.x86_64`; the package lists
   `PRIVATE-BUILD-DO-NOT-DISTRIBUTE`; packaged `/usr/bin/smd` SHA-256 `b91bc34b…d42a28`;
   `labelCompare` against the installed dev build = 1. The worktree, with its `.env`, was then
   removed. alpha.4's RPM still hashes `cebcd305…`.
4. **2026-10-24 09:20Z — A1-04 and A1-05 (host-scratch), by the agent.** The alpha.4 method (alpha.4
   record, execution log #4), run by one script. Scratch `~/sm-a1-scratch` (0700, absent before). The
   RPM's SHA-256 was verified, `./usr/bin/smd` extracted with `rpm2cpio`, and its SHA-256 matched the
   header's (`b91bc34b…d42a28`). Every run was `SM_WORKING_DIR=$S/wd smd config-check --config <copy>`,
   with `$S/wd` an empty 0700 directory.
   - *A1-04.* The live `config.json` copied 0600 (hash identical); the mutations added the top-level
     `typo_key_a1_04` and the nested `bridge.typo_nested_a1_04`, each on its own copy.
     - Control: exit 0. `config-check: config.live.json — no unrecognised keys; the file loads and
       validates as startup would; 0 enabled forwarder(s) construct (legacy entries; destination
       bindings live in each archive and are checked at startup). Not checked: databases, listeners
       and other runtime dependencies.`
     - Top-level: exit 1. `config.top.json has 1 unrecognised configuration key(s) — startup would
       refuse (values omitted): typo_key_a1_04`.
     - Nested: exit 1, the same wording, naming only `bridge.typo_nested_a1_04`.
   - *A1-05.* The alpha.1-era file from `station-manager-pre-alpha2-20260905T1114Z.tar.gz` read in
     memory (version 2, qrzcq `enabled: false`, `action_filter: null`). Fixture a sets qrzcq's filter
     to `["insert","update","delete"]` and smcloud's `allow_insecure_http` to true; fixture b is a with
     the filter `["update","insert","delete"]`.
     - Fixture a: exit 0, "the file loads and validates as startup would; 3 enabled forwarder(s)
       construct".
     - Fixture b: exit 1. `startup would refuse: invalid config (invalid_forwarder): forwarder
       "qrzcq": type "qrzcq" does not support action "update" (supports [insert])`.
   - *Disclosure check.* As at alpha.4: every string value of six characters or more in each input
     was searched for in its output, matches listed by key path only. A1-04's control matched
     `qso_archives.0.ownership` (the word in the fixed text "legacy entries"); A1-05's permutation
     matched only `action_filter` entries (the action names the refusal names by design). No
     unexpected matches. Shorter strings and non-string values were outside the scan.
   - *Nothing written.* A SHA-256 manifest of the scratch directory was identical before and after
     each of the five runs, and `$S/wd` stayed empty.
   - *Live untouched* (before / after the whole step, identical):
     - live `config.json` SHA-256 `899c9978…` (the same content as at alpha.4's A1-04), mtime
       2026-10-24 07:17:59.433 +0200;
     - `smd` `ActiveState=active`, `MainPID=4660`, `NRestarts=0`;
     - the working directory's top-level file-name list (7 names) hashed `38c7327c…`. (alpha.4 listed
       76 names by a different listing, so the two hashes are not comparable.)
   - *Clean-up.* The five config copies were removed with `shred -u` and the scratch directory
     deleted. As at alpha.4, `$HOME` is btrfs, so the overwrite is not assured: files deleted, secure
     erasure unverified.
5. **2026-10-24 09:23–09:32Z — B2-18 (clean-guest, legacy lane), the operator and the agent.** The guest
   clock was 36,474.70 s slow; `systemctl restart chronyd` stepped it at 09:24:07Z, matching the host
   to within 20 ms. The tunnel `127.0.0.1:18080` → `smtest2`'s daemon (alpha.4, instance
   `dmc8afyve2su`, Home, `Default` 8) was opened. The operator launched the new empty profile
   `~/sm-guest/ff-b2l` alone, one tab, and logged the two fixture QSOs (`Default` 10). Pasting the
   planned console check triggered Firefox's paste warning, so the fixture was read in the Storage
   inspector instead (read-only). The alpha.5 RPM was copied into the guest (0600; SHA-256
   `3c10b6e1…693a9c` verified there) and installed with `dnf --disablerepo='*' upgrade`; the running
   daemon kept the deleted alpha.4 binary until `systemctl --user restart smd` as `smtest2` (PID 9711,
   `NRestarts=0`, `/usr/bin/smd`), which answered `2.0.0-alpha.5`, instance `dmcyo9gpkfen`, Home,
   schema 18 clean. The `smtest` lane's daemon was stopped throughout. Observations in the B2-18 row.
6. **2026-10-24 09:33–09:38Z — legacy lane closed; `smtest3` lane; A2-01 and A1-02 (daemon side), by
   the agent.**
   - *Close (install.md §10).* The `ff-b2l` tunnel closed (host `18080` then refused). As `smtest2`
     and `smtest`: `systemctl --user disable --now smd` (both `inactive`, `disabled`); `sudo dnf
     remove station-manager` (Complete; `rpm -q` not installed; `/usr/bin/smd` absent); `systemctl
     --user daemon-reload` in each lane (`LoadState=not-found`). Guest `127.0.0.1:8080` free. Both
     lanes' data directories kept (`smtest2`: `config.json`, `config.v5.json`, `db`, `exports`, `log`;
     `smtest`: `config.json`, `db`, `log`). Lingering left as it was.
   - *Lane.* New user `smtest3` (uid 1002), a fresh home with only the guest key authorised, no
     working directory; `sudo loginctl enable-linger smtest3` (`Linger=yes`), the Cloud-image
     workaround recorded at alpha.4 (no polkit).
   - *A2-01, install (§2).* The guest copy's SHA-256 re-verified (`3c10b6e1…693a9c`); `sudo dnf
     install -y` exit 0, "skipped OpenPGP checks" (expected, per §2). Installed paths: `/usr/bin/smctl`,
     `/usr/bin/smd` (`b91bc34b…`, matching the header), `/usr/lib/systemd/user/smd.service`,
     `/usr/share/doc/station-manager/PRIVATE-BUILD-DO-NOT-DISTRIBUTE`, the manual's `index.html` and
     `.gitkeep`. No scriptlet output.
   - *A2-01, enable and start (§3), as `smtest3`.* `daemon-reload` exit 0; `enable --now smd` exit 0;
     `active (running)`, PID 10367, `NRestarts=0`; `smctl status` "SM is running."
   - *A2-01, health, version and logs.* `GET /v1/healthz` `{"database":"ok","logging":"ok","status":"ok"}`;
     `GET /v1/version` `daemon 2.0.0-alpha.5`, `env release`, schema `{18, dirty:false}`, archive Home
     (legacy, `01a16d45-05fd-7206-9fb9-8aed8492be3a`), instance `dmcytq76egld`; listener
     `127.0.0.1:8080` only; `GET /` 200 `text/html`. Working directory: `config.json` 0600, `db/` 0700
     (`station-manager.db`, `reference.db`, each with `-wal`/`-shm`, all 0600), `log/smd.log` 0600 —
     25 lines, every one carrying `"version":"2.0.0-alpha.5"`, one `warn`: alpha.4 Finding #1's
     first-start "no archive identity row, or logbook 1 does not exist" (unchanged code, expected).
   - *A1-02 (daemon side).* `rpm -q` `station-manager-2.0.0~alpha.5-1.x86_64` and `GET /v1/version`
     agree; the startup line carries `2.0.0-alpha.5`. The sidebar badge awaits the browser.
   - *Browser access.* Tunnel `127.0.0.1:18080` → `smtest3`'s `127.0.0.1:8080` opened; the host
     listens on `127.0.0.1:2222` and `127.0.0.1:18080` only. New empty profile `~/sm-guest/ff-smtest3`
     (0700).
7. **2026-10-24 09:38–09:46Z — A2-02…A2-06 and the A1-02 badge (clean), by the operator, with a
   daemon restart by the agent.** Profile `~/sm-guest/ff-smtest3`, which held 60 entries by the
   restart, so Firefox ran in the throwaway profile this time (alpha.4's profile deviation not
   repeated). After the operator reported A2-04 done, `config.json` read `setup_complete: true`,
   `default_logbook_id: 1`; `smctl restart` "SM Restarted.", exit 0, PID 10367 → 10452,
   `NRestarts=0`, new instance `dmcyvuc6j0tm`, still alpha.5, schema `{18, dirty:false}`, Home;
   `Default` 0 QSOs. During A2-06 the operator reported that the manual offers no Settings link or
   instruction (Finding #1). First QSO: 7Q5MLV, 2026-10-24 09:45:01Z, 14.255 MHz, 20m SSB, UUID
   `01a16d4d-00a6-73dc-b560-c2538f595f14`, logbook 1 (`Default` 0 → 1; the `smd.log` store line); no
   new `warn` or `error` (the only one is A2-01's first-start line). The operator read the sidebar
   badge as `2.0.0-alpha.5`. Operator report on A2-02…A2-06: all as expected — Station Events in the
   sidebar between Logbook and Settings — apart from Finding #1.
8. **2026-10-24 09:48Z — A2-07 (clean), by the agent, install.md §10.** As `smtest3`: `systemctl --user
   disable --now smd` exit 0 (`inactive`, `disabled`, the wants symlink removed); `sudo dnf remove
   station-manager` Complete, `rpm -q` not installed, `/usr/bin/smd`, `/usr/bin/smctl` and the user unit
   absent; `systemctl --user daemon-reload` exit 0 (`LoadState=not-found`); lingering, enabled solely for
   Station Manager, disabled (`sudo loginctl disable-linger smtest3`, `Linger=no`; the polkit
   workaround). Guest `127.0.0.1:8080` free. Data retained as documented: `config.json` (0600),
   `log/`, `db/` (0700) with `station-manager.db`, `reference.db` and `qso-archive-summaries.json`, no
   `-wal`/`-shm` left (a clean shutdown). The `smtest3` tunnel then closed.
9. **2026-10-24 09:49–09:53Z — A2-08 (clean), by the agent and the operator.** New user `smtest4`
   (fresh home and working directory, key-only login, `Linger=yes` via sudo); guest `127.0.0.1:8080`
   free. The frozen alpha.2 RPM (SHA-256 `7b2fa20a…c24856`, verified on the host and in the guest)
   installed and started: PID 11081, schema 8, config 3. Tunnel to `smtest4`; new empty profile
   `~/sm-guest/ff-smtest4`. The operator completed setup and logged three Phone / CW QSOs.
   - *Before:* alpha.2, schema `{8, dirty:false}`, config version 3, `setup_complete` true,
     `default_logbook_id` 1; logbook `Default` (M0CMC) with 3 QSOs: DR1LL
     `01a16d51-6380-7f24-bfe0-1bfc25dc7204` 09:50:04Z, DR2LL `01a16d51-88e0-7fbd-89b4-12a1392b7db7`
     09:50:20Z, DR3LL `01a16d51-a5b9-7954-8207-e52f41a05749` 09:50:27Z.
   - *Upgrade (install.md §7).* `sudo dnf install` of the alpha.5 RPM (SHA re-verified) upgraded in place
     while alpha.2 kept serving (PID 11081); `smd config-check` exit 0, "no unrecognised keys; the file
     loads and validates as startup would; 0 enabled forwarder(s) construct", `config.json`
     byte-identical after it; `systemctl --user daemon-reload` and `restart smd` exit 0.
   - *After:* PID 11430, `NRestarts=0`; healthz ok; `2.0.0-alpha.5`, schema `{18, dirty:false}`, archive
     Home (legacy) `01a16d52-1c87-7dfd-b5ec-7caba7193539`; config version 6, setup still complete;
     `config.v5.json` written (0600, version 5); `Default` 3 with the same three UUIDs. The first start
     logged the one-time lines: catalogue written (installation adopted in place), destination bindings
     decided once, the v5 recovery copy written, config stripped to station accounts.
   - *Second restart:* PID 11494, new instance, same version and schema; `config.json` and
     `config.v5.json` byte-identical; none of the one-time lines (only the per-start "starting
     migrations" / "databases open and migrated"); 3 QSOs. No `warn` or `error` from alpha.5 in this
     lane. The log still names neither the schema nor the config transition (alpha.4 Finding #4).
   - *Browser (operator, `ff-smtest4`, the tab kept open across the upgrade and reloaded once):* no
     Welcome page, lands on Phone/CW; badge `2.0.0-alpha.5`; header "Archive Home · Logbook Default
     (3)"; the one-time Session reset notice shown, and **Session (0)**, "No QSOs logged this session."
     (screenshot `Screenshot_20261024_115236.png`). At alpha.4 this panel showed 4 rows (alpha.4
     Finding #5); the tab's alpha.2-era unscoped list is now discarded, not adopted.
10. **2026-10-28 10:34–10:41Z — A3 outage (plan revision 3) with B2-15 and B2-16, by the agent.** The
    operator approved revision 3 and gave the window 10:34Z–14:00Z (no loadshedding), the station idle;
    station-needed-by: up to a week. Records under `~/sm-a3` (0700; files 0600).
    - *A3-04 (10:34Z, read-only).* Dev RPM SHA-256 `2d3a0ec9…be180bdb`, `rpm -qp`
      `station-manager-2.0.0~alpha.3.262.g6217da21.dirty-1.x86_64`; installed the same; daemon PID 4537 on
      Home (`dmgd8z5ul821`), schema 18, health ok.
    - *Step 3 observations (10:34:55Z, scoped):* catalogue summaries Home `Default` (7Q5MLV) 8,130, Drill
      `Drill` (M0CMC) 1; active archive's queues all 0; `station-events?limit=1000` returned 1 event;
      Home bindings clublog on, qrz on, smcloud on, qrzcq off, `restart_required` false.
    - *Step 4, stop (10:35:03Z):* `inactive`, `MainPID=0`, `Result=success`, exit status 0; `smd.log`
      ends "HTTP server shutdown complete", "smd stopped". **Sidecars:** only the known July pair,
      byte-identical to its 2026-10-24 reading (`e3b0c442…`, `fd4c9fda…`); Home, Drill, `reference.db`
      and `evidence.db` have none.
    - *Step 5, baseline (10:35:13Z):* 70 files, 190,877,198 B, no symlinks; manifest `d92ff194…`.
    - *Step 6, A3-01:* the backup in the header; 78 entries, 70 files.
    - *Step 7, A3-02 and A3-03:* the archive's SHA verified; extraction's path set and every hash
      identical to the baseline; `integrity_check` ok (Home, Drill, `reference.db`, `evidence.db`;
      the premigrate file and dated copies not opened). Authoritative counts (`immutable=1` on the
      verified copies): Home `Default` 8,130 live of 8,130 rows; `qso_upload` clublog 2,357, qrz 2,672,
      smcloud 10,708, all `uploaded`; `operator_event` notification 1; bindings clublog, qrz, smcloud
      enabled, qrzcq disabled. Drill `Drill` 1; `qso_upload` smcloud (Drill's identity) 1 `uploaded`;
      `operator_event` 1; binding smcloud enabled. Consistent with step 3.
    - *Step 8, B2-15/16* (alpha.5 `smd` `b91bc34b…`; helpers built from this tree, which matches
      the tag outside `docs/`): `restore/` and `provision/` copies of the verified extraction, three
      paths rewritten; `c-paths` 0 of 22 outside for each, live control 22 outside. The first export
      failed `no route to host` (the SM Cloud host was powered down; nothing sent or written); the
      operator powered it up and the step continued. Results in the B2 execution table.
    - *Step 9 (10:40:26Z):* live directory against the baseline — 70 paths, every hash identical.
      Config copies `shred -u` (btrfs caveat), scratch trees removed; the baseline, manifests and counts
      kept. **Deviation:** `~/sm-a3/counts-authoritative.txt` (counts and binding on/off flags only; no
      credentials or row contents) had been created 0644 — `tee` ran outside the `umask 077` — and was
      corrected to 0600 before the restart.
    - *Step 10 (10:40:42Z):* dev build restarted, PID 27301, `dmgeovvsg2gi`, Home, health ok;
      Home 8,130, Drill 1, queues 0, 1 event — no difference from step 7.
11. **2026-10-28 10:44–10:53Z — B1 on the live station, by the agent and the operator.** Ready to deploy
    ruled PASS (operator, after A3). B1-05 preflight in a 0700 scratch (removed after). The package
    install needed the operator's `sudo`; the agent then ran `smd config-check`, `daemon-reload` and
    `restart`, read the API and logs, and ran the B1-06 restart. Details in the B1 table; the stale-tab
    behaviour is Finding #4.
12. **2026-10-28 — guest disposal (alpha.4 record, Appendix 3, item 7), by the agent, after the final
    acceptance ruling.** `sm-alpha4-clean` shut down cleanly, then `virsh --connect qemu:///session
    undefine sm-alpha4-clean --remove-all-storage` (exit 0; volumes `sm-alpha4-clean.qcow2` and
    `seed.iso` removed — the guest copies of the private keyed RPMs went with the disk); `~/sm-guest`
    removed (the Fedora base image and checksum files, the guest-only SSH key, the ten throwaway Firefox
    profiles). No guest port listening afterwards. Retained outside it: `~/sm-backup` (the A3-01 and
    2026-10-21 backups), `~/sm-a3` (baseline, manifests, counts, observations), every RPM in
    `build/private`, and the screenshots and export files cited in this record.
