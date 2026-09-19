# Dogfood acceptance — v2.0.0-alpha.3

Release-specific execution record for the canonical gate in
[`docs/dogfood-acceptance.md`](../dogfood-acceptance.md). Drafted 2026-09-19 from the frozen
candidate under the operator's scope ruling of the same day (below). Every `pending` row is the
operator's to execute and rule on. Hardware, rig-command and RF rows are out of scope for this
candidate and none is listed; nothing here authorises RF.

Candidate commit: `333427ea` — tag `v2.0.0-alpha.3` (annotated, local, **not pushed**; the tag
identifies the candidate, it does not mean acceptance passed). CI green on `333427ea` (run
35433062614, 2026-09-19 08:50Z) before the tag was cut.
Version/build shown by daemon: `2.0.0-alpha.3` expected on `GET /v1/version`, the sidebar build
badge, the startup log line and ADIF `PROGRAMVERSION` — verified first on the clean lane (A1-02),
repeated on the upgraded station (B1-02)
RPM filename: `build/private/station-manager-2.0.0~alpha.3-1.x86_64.rpm` (mode 0600) — the single copy
of the artifact `scripts/dev-rpm.sh` built as `station-manager-dev.x86_64.rpm` in a clean scratch worktree
checked out at the tag, then removed with the worktree (private keyed dogfood build, PocketFFT/CGO FFT
backend; carries the ClubLog key — **not distributable**). It does not share a path with
`task deploy:local:dev`'s output, so a later dev build cannot overwrite it — verify the SHA before every
install
RPM SHA-256: `23335ff6aa0d42d9271c003b6984f836990e1a85856e34b344312d53491da79d` (9,205,128 bytes, built
2026-09-19 09:19Z)
RPM-reported version: `station-manager 2.0.0~alpha.3-1 x86_64` expected (`rpm -qp --queryformat
'%{NAME} %{VERSION}-%{RELEASE} %{ARCH}'`); daemon build line `version: 2.0.0-alpha.3, FFT: PocketFFT
(CGO, dynamically linked)`
Previous installed version: `station-manager-2.0.0~alpha.2.124.gd90815c6-1.x86_64` — the dev build of
`d90815c6` deployed 2026-09-19 (the station has run dev builds since 2026-09-07; its schema is already 9
and its `config.json` already version 3), so the live upgrade B1-01 is dev-build → candidate. The
frozen-alpha.2 → candidate upgrade that any other alpha.2 station would follow is rehearsed on the clean
lane (A2-08).
Clean-install environment: pending — operator's choice of lane (an isolated RPM host or VM per §A2, or the
alpha.2 path of a fresh deployment on the dogfood host after a verified working-directory copy, under a
renewed risk decision recorded here)
Upgrade environment: the daily-driver dogfood station (this host)
Browser(s) and viewport(s): Firefox; the alpha.2 A4 parameters stand unless re-ruled at A4-01
Planned test date: 2026-09-19 onward
Operator: 7Q5MLV
Automated gate evidence: `task ci:local` passed at `333427ea` on 2026-09-19 in the clean scratch worktree —
SPA lint, prettier, svelte-check, vitest 1735/1735 (132 files), Vite build; manual build; Go gofmt, vet,
golangci-lint, maintainability observatory 0 regressions (26,500 measurements), race and full test runs,
static and PocketFFT builds, ClubLog build-key boundary (ST-7) OK; documentation catalog and agent-context
budgets; "All CI gates passed locally."
Backup location (do not record secrets): pending (A3-01)
Restore check: pending (A3-02)
Recovery policy: as alpha.2 — a stopped-daemon copy of the complete working directory is the primary
recovery artifact; the previous RPM (`2.0.0~alpha.2.124.gd90815c6-1`) is preserved for a package
rollback; migration 0009 has a `.down.sql` that keeps `notification` rows and discards `alarm` rows
Rollback artifact/command: pending (A3-04)

Ready to deploy: pending
Operator/date:

Dogfood accepted: pending
Operator/date:
Residual waivers: none carried in — the alpha.2 waiver B1-01 (Finding #6) expires with this candidate and
is retired by A1-05 plus B1-01 below
Follow-up destinations: pending

## Scope — operator ruling, 2026-09-19

The operator directed this freeze as soon as CI passed on `333427ea`, limited to: **W-0008 CC-5** (the
alpha.1-written `qrzcq` `action_filter` reconciled at load), **clean-install and upgrade evidence**, and
**retiring the alpha.2 B1-01 waiver**. No P2 item enters this candidate: the rulings of 2026-09-19 (landing
view preference, the "CQ run" header wording, Excel export) are post-freeze W-0012 slices and are not in
`333427ea`. The B2 surface inventory and the B3 hardware/RF groups are **not executed for this candidate**:
the alpha.2 B2 evidence and the dogfood record entries 34–41 since (FT4 contest, 300-QSO FT8 soaks, the
Station Events passive check of 2026-09-19) are the operator's standing evidence for those surfaces, and
the canonical condition "every current operator-facing surface is present in the case inventory" is
knowingly not met here — recorded as a scope reduction the operator owns, like alpha.2's. Lane labels as in
the alpha.2 record: **live**, **host-scratch**, **clean** (the isolated clean-install lane), **record**.

## Release delta — `v2.0.0-alpha.2` → `v2.0.0-alpha.3` (126 commits)

Enumerated per Gate A1. Full list: `git log --oneline v2.0.0-alpha.2..v2.0.0-alpha.3`. Themes with the
commits that anchor each claim.

### Migrations

- **Log database schema 0008 → 0009** (`d198385a`; W-0020 slice 1): `operator_event` rebuilt with a joint
  (category, kind) CHECK so the `alarm` category and its six kinds join the `notification` rows; the
  AUTOINCREMENT high-water mark is carried across the rebuild. `.up.sql` and `.down.sql`; the down path
  keeps `notification` rows and discards `alarm` rows. Runs once at the first start of the new daemon —
  inspect at B1-04 (live: already applied by the dev build on 2026-09-19, `GET /v1/version` schema 9 not
  dirty; the from-schema-8 run is observed on the clean lane at A2-08).
- **`config.json` version stays 3.** The v2 → v3 migration gained the CC-5 reconcile (`db4bcb58`): a
  pre-version-3 document whose `qrzcq` forwarder carries exactly alpha.1's ordered `["insert","update",
  "delete"]` becomes `["insert"]`; any other unsupported filter, a permutation, or the same content in a
  version-3 document is still refused. Exercised at A1-05. A version-3 file (every alpha.2 station) is
  untouched by it.

### Configuration changes

- `ft8.ft4_frequencies` — the FT4 dial table, three cited bands (`ad20add3`; ADR 0080). Filled with
  defaults on the first normalising write, so the file gains key paths at the first start (as alpha.2's
  Finding #2 did) — expected, not a finding.
- `ft8.tx.max_repeats` code default 6 → 5 (`dfd0898f`); a station file without the key now runs 5. This
  station removed the key on 2026-09-16 and serves 5 already.
- A rig `mode_mappings` entry whose `mode` names an ADIF submode (FT4, FST4, FST4W, JS8, Q65) is
  canonicalised to its `MFSK` pair at load and PUT instead of refused (`8701a6de`, `40238d47`).
- `smd config-check` now runs `config.Load` (migrate in memory, unknown keys, validate) and constructs
  every enabled forwarder — the preflight reports what the restart would refuse (`6ddb4c97`; CC-6).
  `install.md` §7 documents it.

### Changed API surface (embedded SPA is the only client)

- `GET /v1/station-events?category=&severity=&limit=` (`43f21b0d`) replaces `GET /v1/notifications`
  (retired; `POST /v1/notifications` kept for the browser-originated export failure).
- `POST /v1/ft8/claim` selects the FT profile; `GET /v1/ft8/events?mode=` refuses a mismatch; `mode` on
  the status event and on `ft8-tx` (`fb6480a7`, `67cc1b96`); `cq_message` on `ft8-qso` (`77d5453c`);
  `/v1/contest-dupe` takes the ADIF `submode` (`67cc1b96`).

### Changed user journeys

- **FT4 as a third Operate item** (W-0019, ADR 0080): claim-then-subscribe, 7.5 s clock, per-mode dial
  snapshot, refusal banner with countdown; an FT4 exchange is filed `MODE=MFSK SUBMODE=FT4`.
- **Station Events page** replaces the header notification slide-over and rail (W-0020, `5882b81f`):
  `/events` in the sidebar between Logbook and Settings; category and severity chips; the alarm family
  (TX/drive alarm raised and cleared, non-operator disarm, abnormal exchange termination) recorded by a
  bounded recorder. The header button and `ui.notificationsOpen` are gone.
- **Dashboard retired** (`4ee74516`): a bare `/` lands on the last-used Operate mode (Phone/CW first).
- Call-CQ run: a same-band/profile repeat is **held** before TX with Answer anyway / Ignore (`76e28cb7`,
  `c3b5b8b3`, `82969cfd`); a **custom CQ** token (`bf472ba6`, `77d5453c`); the confirm-hold policy is
  unchanged (ruling 2026-09-16).
- Band Activity: typed-filter chip with the hidden count (`5cbc6be2`, `e0e9a2dc`); enrichment capped at
  two lookups in flight per tab (`1fe16e2b`); the bag chord named only where accepted (`33693be3`).
- Occupancy keeps the TX slot's last reading with its age; the "pause TX" line is gone (`219db4f1`).
- Settings → FT8 → Contacts gains the repeat cap (1–10, applied live) (`42cff385`).
- Rig: the Mode field holds the pre-tune mode for the whole tune (`2544b3da`, `f4c46e81`, `c0bbd80a`);
  FT views show Mode as a profile-owned readout (`4bc1fb3f`, `13d95084`, `a229d1f2`); the default rig
  cannot be deleted (`a364c21a`, `17fc6a7e`); rig detail subtitle dropped (`5e763dc8`).
- Phone/CW draft survives a mode switch and shows its age (`f6161e81`).
- Shell: Manual opens in a new tab (`f0b8e6eb`); "Welcome" tab title on first run (`c1045efa`); header
  count refreshes on the shell stream's reconnection, `: connected` first bytes on every SSE stream
  (`33e975bc`, `9b60b65e`); one `/v1/events` per tab and no rig stream in a Map tab (`c4d0613a`,
  `7126ab64`, `e409b842`); ALC chip healthy state unlabelled (`05f32ac4`); run-surface idle text and dot
  tidied (`b19f5bc7`, `3dbcc5e2`).
- Bridge: a first "still keyed" answer after unkey re-sends the stop and re-asks before any TX alarm
  (`02823278`, `ae8cb9a5`, `90686986`, `68eb0236`; W-0011, closed on the 2026-09-15 soak); liveness flaps
  warn once then debug with a count (`bddc9c2a`).

### Changed integrations

- go-ft8 v0.9.0 (`37f0ca2d`) — FT4 encoder and decoder; the only dependency change in the delta.
- QRZ forwarder never stores the API key QRZ echoes (`68e74f90`); a QSO stored under a former main mode
  is recognised by its legacy dedupe key (`bb9f4dfe`); PSK Reporter spots carry `FT4` for FT4 decodes.
- ClubLog, QRZ and SM Cloud receive the `MFSK`/`FT4` pair for FT4 contacts (worked in the 2026-09-12
  contest, every upload first attempt).

### New manual or setup instructions

- Embedded manual: new **Station Events** chapter (`11d07076`); FT8 chapter gains "FT8 or FT4", the custom
  CQ, the repeat cap and the second-tab note; troubleshooting's TX-alarm section gains an *Afterwards*
  pointer. Verify at A2-06 that the chapter list matches the sidebar.
- `install.md` §7: the `smd config-check` step and the file-only-key note; the "Known: alpha.1 → alpha.2
  qrzcq refusal" paragraph is amended in the freeze commit to say alpha.3 reconciles that shape itself.

## Gate A — ready to deploy

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `A1-01` | yes | **record** — clean scratch worktree at `333427ea` = tag `v2.0.0-alpha.3` | Build once with `SM_FFT=pocketfft scripts/dev-rpm.sh` after `task ci:local`; record filename, SHA-256, RPM version; keep the versioned copy | One artifact; RPM version `2.0.0~alpha.3-1`; `ci:local` exit 0; SHA recorded in the header | A `-dirty` or `alpha.3-N-g…` version (untagged or dirty tree); a second build overwriting the copy | Execution log #2–#4: `ci:local` exit 0; the first build came out `2.0.0-alpha.3-dirty` / `2.0.0~alpha.3.dirty-1` because the CI mirror's SPA build had rewritten the tracked `frontend/app/dist/index.html` (Finding #1) — **discarded**; the committed file was restored byte-for-byte, `git status --porcelain` empty, `git describe` `v2.0.0-alpha.3`, rebuilt: `2.0.0-alpha.3`, RPM `station-manager 2.0.0~alpha.3-1 x86_64`, 9,205,128 B, SHA-256 `23335ff6…da79d3`, copied to the versioned path (mode 0600) and the worktree removed | none | pending — operator ruling |
| `A1-02` | yes | **clean** — the A2-01 install, candidate installed and started | Verify the RPM SHA before installing; read `GET /v1/version`, the sidebar badge, the startup log line, `rpm -q` | All four read `2.0.0-alpha.3`; schema `{9, dirty:false}` after the first start | Badge from a cached tab; a dev-build version string | pending | none | pending |
| `A1-03` | yes | **record** — record drafted | Review the release delta above against the candidate (migration, configuration, journeys, integrations, manual) | Delta accepted or corrected here | Silent scope creep in the delta | pending | none | pending |
| `A1-04` | yes | **host-scratch** — 0700 scratch directory with a copy of the live `config.json`; the candidate `smd` extracted from the frozen RPM (Appendix 1); live service untouched | Run `smd config-check --config` on the untouched copy (control), then on a top-level and a nested unknown-key mutation | Control exits 0 (loads, validates, forwarders construct); each mutation exits non-zero naming only the key path; nothing started, bound or written | Control non-zero — the candidate would refuse to start on the station: fix the live config and re-run, no waiver possible | pending | none | pending |
| `A1-05` | yes | **host-scratch** — the same scratch area; `config.json` extracted from the pre-alpha.2 archive (`~/sm-backup/station-manager-pre-alpha2-20260905T1114Z.tar.gz`, the alpha.1-era version-2 document behind alpha.2 Finding #6) with two edits: the qrzcq `action_filter` set to the ordered slice alpha.1's start wrote (`["insert","update","delete"]` — the archive predates that write and holds `null`), and the SM Cloud `allow_insecure_http` acknowledgement added (Finding #1's known fix) so nothing else refuses (Appendix 2) | Run the candidate's `smd config-check --config` on that copy; then on a mutation whose qrzcq `action_filter` is the permutation `["update","insert","delete"]` | Copy exits 0 (the reconcile turned the filter into `["insert"]` in memory and validation passed); the permutation exits non-zero naming `qrzcq` and the unsupported action — proof the check reaches validation and the reconcile is narrow | Both exit 0 (the check skipped validation, as alpha.2's key-only preflight did); the copy refused for `allow_insecure_http` (fixture not prepared); a value printed | pending | none | pending |
| `A2-01` | yes | **clean** — isolated RPM environment with no Station Manager data (lane recorded in the header) | Install per `install.md`; enable/start the user service; note the lingering step | Service active; health and version answer; logs readable; browser reaches `/` | Loopback-only bind with a differing guide URL; lingering omitted so the service dies at logout | pending | none | pending |
| `A2-02` | yes | **clean** — first browser open after A2-01 | Open the app | Only the first-run welcome surface; tab titled "Welcome"; no operate/logbook chrome | Operate chrome visible before setup | pending | none | pending |
| `A2-03` | yes | **clean** — first-run callsign form | Enter invalid, empty, lowercase/whitespace-normalised, then valid callsign | Each state understandable; the normalised form shown | A silent accept of the invalid form | pending | none | pending |
| `A2-04` | yes | **clean** — valid callsign submitted | Complete setup | Default logbook created; both **Open Settings** and **Start logging** offered | Only one journey offered | pending | none | pending |
| `A2-05` | yes | **clean** — setup complete | Reload the browser; restart the daemon; reopen | Setup stays complete; the bare `/` lands on Phone/CW (first use, no Dashboard) | Welcome surface returns; a Dashboard placeholder | pending | none | pending |
| `A2-06` | yes | **clean** — setup complete, guide and manual only | Find Settings, the embedded Manual (opens in a new tab) and its Station Events chapter, and log a first QSO using only the guide and manual | All reachable without repository knowledge; Station Events in the sidebar between Logbook and Settings, empty state correct | A step needing repository-only knowledge (record it) | pending | none | pending |
| `A2-07` | yes | **clean** — installed, test QSOs in the logbook | Uninstall per the guide | Package removed; operator data deliberately retained as documented | Data directory deleted by the package | pending | none | pending |
| `A2-08` | yes | **clean** — a second fresh working directory: the frozen **alpha.2** RPM (SHA `7b2fa20a…c24856`) installed and set up, a few test QSOs, daemon running | Upgrade per `install.md` §7 to the candidate: `dnf install`, `smd config-check`, `daemon-reload`, `restart` | `config-check` exit 0; migration 0009 applied once at the first start (schema 8 → 9 logged, no error); setup bypassed; QSOs, logbook and config retained; `ft8.ft4_frequencies` written on the first normalising save | Migration re-runs on every start; a refusal at the restart the preflight did not name; `notification` rows lost | pending | none | pending |
| `A3-01` | yes | **live** — dogfood station, daemon stopped or quiescent | Back up the complete working directory outside the repo | Backup exists; listable; SHA-256 recorded | Backup copied into the repository tree | pending | none | pending |
| `A3-02` | yes | **host-scratch** — the A3-01 archive | Restore a copy into a scratch location; open it read-only; remove it | Restore completes; integrity and counts equal A3-03 | Restore needs the live directory | pending | none | pending |
| `A3-03` | yes | **live** — pre-upgrade | Record durable-record counts (QSOs per logbook, upload-queue rows by status, `operator_event` rows per category) | Numbers recorded here without contents | Counting after the upgrade began | pending | none | pending |
| `A3-04` | yes | **live** — pre-upgrade | Preserve the previous RPM (`2.0.0~alpha.2.124.gd90815c6-1`, from the dev build of 2026-09-19) and record the rollback procedure: package downgrade, then the guarded 0009 down-migration or the working-directory restore | Artifact present; procedure named in the header | Only the RPM preserved | pending | none | pending |
| `A3-05` | yes | **live** — before B1 | Confirm rollback time is reserved before the station is next needed | Window recorded | Upgrade started with a session imminent | pending | none | pending |
| `A4-01` | yes | **record** — delta reviewed (A1-03) | Ratify this inventory under the 2026-09-19 scope ruling; re-rule or confirm the alpha.2 A4 parameters | Inventory ratified | An execution before ratification | pending | none | pending |
| `A5-01` | n/a | **record** — the station has run alpha.2-lineage daemons since 2026-09-05 | None: the pre-alpha.2 false-conflict trigger cannot arise on this station; the canonical A5 step stays in the gate until the operator removes it | — | — | N/A by construction | none | N/A |

## Gate B — deploy and accept

### B1 — the upgrade itself (and the B1-01 waiver retirement)

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `B1-01` | yes | **live** — dev build `alpha.2.124` running; A1-04, A1-05 and A3 done | Upgrade per `install.md` §7: `sudo dnf install <candidate rpm>`, `smd config-check`, `systemctl --user daemon-reload`, `systemctl --user restart smd` (no uninstall, no separate stop) | Package replaced while the old daemon runs; preflight exit 0; restart observed; browser reconnects within the A4 tolerance. **Retires the alpha.2 B1-01 waiver** together with A1-05 | Install succeeds but the restart is skipped; a scriptlet leaves the service stopped | pending | none | pending |
| `B1-02` | yes | **live** — upgraded | Repeat A1-02: `/v1/version`, badge, startup log, `rpm -q`; deliberately reload the open tab | All four read `2.0.0-alpha.3`; new UI; no old chunk from cache | An unreloaded tab still running the dev build's chunks | pending | none | pending |
| `B1-03` | yes | **live** — upgraded | Confirm setup bypassed; config, rig, logbook, operator identity, theme/rail preference, durable events retained | All retained; Station Events shows the same rows as before | First-run gate reappears | pending | none | pending |
| `B1-04` | yes | **live** — upgraded | Compare A3-03 counts; inspect startup diagnostics | Counts equal; `databases open and migrated` with nothing to apply (schema already 9); no error records | Counts differ; a migration re-runs | pending | none | pending |
| `B1-05` | yes | **host-scratch** — immediately before B1-01, a fresh copy of the live `config.json` | Re-run the A1-04 control only (Appendix 1 steps 4–5) | Exit 0 | Non-zero: stop, fix the named item on the live file, re-run — no waiver | pending | none | pending |
| `B1-06` | yes | **live** — upgraded | Controlled daemon restart | UI and the rig/log/FT streams recover without a manual reload within the A4 tolerance | Streams dead until reload | pending | none | pending |

## Appendix 1 — A1-04 / B1-05 live-config preflight

As the alpha.2 record's Appendix 2, with the candidate's artefact: verify the RPM SHA in the header,
extract only `./usr/bin/smd` with `rpm2cpio | cpio -idm`, copy the live `config.json` into a 0700 scratch
directory under your home as a 0600 file, run `./usr/bin/smd config-check --config <copy>`, then the two
`jq` mutations (a top-level `typo_key_a1_04` and a nested `bridge.typo_nested_a1_04`), record the isolation
evidence (`systemctl --user is-active smd`, the live file's mtime, nothing new under the working
directory), and shred the copies. The candidate's check goes further than alpha.2's: the control now also
loads, validates and constructs every enabled forwarder, so its success message reads "the file loads and
validates as startup would; N enabled forwarder(s) construct".

## Appendix 2 — A1-05 CC-5 reconcile on the real alpha.1 configuration (host-scratch)

Purpose: prove, without a daemon, that the candidate loads the very file alpha.2 refused (Finding #6) and
that the reconcile is narrow. `config.Load` reads the file, migrates the document **in memory**, rejects
unknown keys, validates, and writes nothing (`internal/config/config.go` `Load`); `config-check` adds
only the enabled-forwarder construction.

1. `S=~/sm-a1-05-scratch && mkdir -m 0700 -p "$S" && cd "$S"` (the file holds credentials — never `/tmp`,
   never the repository).
2. Extract the alpha.1-era file: `tar -xzOf ~/sm-backup/station-manager-pre-alpha2-20260905T1114Z.tar.gz
   station-manager/config.json > config.alpha1.json && chmod 0600 config.alpha1.json`. Confirm
   `jq .version` prints `2`. Checked 2026-09-19 (key names only, no values read into the record): the
   archive's qrzcq forwarder is `enabled: false` with `action_filter: null` — the archive was taken with the
   daemon stopped at 11:14Z, before the alpha.1 start of pass 2 wrote the all-actions default that alpha.2
   then refused (Finding #6: `config.go:1227` at `d9cd38ae` fills an omitted filter at load and the next
   write persists it). The fixture therefore recreates that write explicitly.
3. Build the fixture — alpha.1's persisted shape, plus Finding #1's acknowledgement so the SM Cloud
   forwarder's cleartext LAN URL cannot mask the result (`allow_insecure_http` is a forwarder-level,
   file-only field, `config.md` §forwarders):
   `jq '(.forwarders[] | select(.type=="qrzcq") | .action_filter) = ["insert","update","delete"]
   | (.forwarders[] | select(.type=="smcloud")) += {"allow_insecure_http": true}'
   config.alpha1.json > config.a.json && chmod 0600 config.a.json`. Confirm
   `jq '.forwarders[] | select(.type=="qrzcq") | .action_filter' config.a.json` prints the three actions in
   that order.
4. **The reconcile:** `./usr/bin/smd config-check --config "$S/config.a.json"; echo "exit=$?"` — expected
   exit 0 and the "loads and validates as startup would" line.
5. **The narrowness control:** `jq '(.forwarders[] | select(.type=="qrzcq") | .action_filter) =
   ["update","insert","delete"]' config.a.json > config.b.json && chmod 0600 config.b.json` and run the
   same check — expected
   non-zero with `startup would refuse: invalid config (invalid_forwarder): forwarder "qrzcq": type "qrzcq"
   does not support action "update"`.
6. Isolation evidence and clean-up as Appendix 1 (`shred -u` the copies, remove `$S`).

The frozen alpha.2 binary is **not** a control here: its `config-check` checked keys only (Finding #6 was
found at the restart, B1-05 having passed), so it exits 0 on both files. Nor is the untouched archive file
(`null` filter): every build since alpha.2 fills an omitted filter with the type's insert-only default, so
it loads under the candidate without touching the reconcile.

## Execution log

Command-line checkpoints run by the coder; the operator rules every result. Times UTC.

1. **2026-09-19 — freeze.** CI green on `333427ea` (run 35433062614). Annotated tag `v2.0.0-alpha.3`
   created locally at `333427ea`; a scratch worktree checked out at the tag (`git status --porcelain`
   empty; `scripts/version.sh` → `2.0.0-alpha.3`); `.env` copied into it for the private build key.
   `task ci:local` and the PocketFFT build run there.
2. **09:01Z–09:18Z — `task ci:local` in the worktree.** Exit 0: vitest 1735/1735, observatory 0 regressions,
   race and full Go tests, static and PocketFFT builds, ST-7 boundary, catalog and context budgets.
3. **09:18Z — first build DISCARDED.** `scripts/dev-rpm.sh` (PocketFFT) exited 0 but stamped
   `2.0.0-alpha.3-dirty` (RPM `2.0.0~alpha.3.dirty-1`, SHA `9e6d164b…`): the CI mirror's SPA build had
   left the tracked `frontend/app/dist/index.html` modified (two chunk hashes, `daemonClock` and
   `_helpers`), and the script derives the version from `git describe --dirty` at its start. Not the
   candidate — deleted with the worktree. Finding #1.
4. **09:19Z — the candidate build.** `git show HEAD:frontend/app/dist/index.html` restored the committed
   file; `git status --porcelain` empty; `git describe` `v2.0.0-alpha.3`; rebuild exit 0 — daemon
   `version: 2.0.0-alpha.3, FFT: PocketFFT (CGO, dynamically linked)`, RPM `2.0.0~alpha.3-1`,
   9,205,128 bytes, SHA-256 `23335ff6aa0d42d9271c003b6984f836990e1a85856e34b344312d53491da79d`; copied to
   `build/private/station-manager-2.0.0~alpha.3-1.x86_64.rpm` (0600), SHA re-verified after the copy; the
   worktree (with its `.env` copy and the dirty build) removed and pruned. The build again rewrote the
   tracked `dist/index.html` afterwards, as expected from Finding #1; the version had been fixed before it.

## Findings

Record surprises in [`dogfood-inbox.md`](../dogfood-inbox.md) as they happen; triage each here as
defect / planned follow-up / duplicate / working-as-designed / documentation correction, and route
durable work through the backlog.

| # | Case | Observation (sanitized) | Triage | Destination |
|---|---|---|---|---|
| 1 | A1-01 | The tracked `frontend/app/dist/index.html` (last committed 2026-08-27, `a7e12ca4`) is not the output of a fresh `npm run build` at the candidate: two hashed chunk names differ, so any SPA build dirties the tree and a version derived after it carries `-dirty`. The RPM is unaffected — `dev-rpm.sh` builds the SPA fresh and embeds that — but the stamp is order-dependent (computed before the SPA build). | Build hygiene, not a candidate defect. Options for the operator: stop tracking `dist/` (the build always regenerates it), or recommit it with each SPA change, or make `dev-rpm.sh` refuse a dirty tree before building. | W-0009 (maintainability residuals) — operator to route |
