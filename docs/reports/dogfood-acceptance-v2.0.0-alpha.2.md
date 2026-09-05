# Dogfood acceptance — v2.0.0-alpha.2

Release-specific execution record for the canonical gate in
[`docs/dogfood-acceptance.md`](../dogfood-acceptance.md). Drafted 2026-09-05 from the frozen
candidate; corrected the same day per operator review (A1-02 moved to the clean-install host, A4-01
added, grouped B2 rows expanded). Unless superseded by the 2026-09-05 scope reduction below, every
`pending` row is the operator's to execute and rule on.
Hardware, rig-command and RF rows carry their own approval column and are never implied by a
passive `PASS`.

Candidate commit: `b8e0356f` — tag `v2.0.0-alpha.2` (annotated, local, **not pushed**; the tag
identifies the candidate, it does not mean acceptance passed)
Version/build shown by daemon: `2.0.0-alpha.2` expected on `GET /v1/version`, the sidebar build
badge, the startup log line and ADIF `PROGRAMVERSION` — verified first on the A2 clean-install
host (A1-02), repeated on the upgraded station (B1-02)
RPM filename: `build/private/station-manager-dev.x86_64.rpm` (private keyed dogfood build,
PocketFFT/CGO FFT backend; carries the ClubLog key — **not distributable**)
RPM SHA-256: `7b2fa20a247ee0980667fe9b09e02a2a6c458b772c25f40d85e5cf48c7c24856` (9,053,968 bytes,
built 2026-09-05 09:49 local; an identical copy is kept as
`build/private/station-manager-2.0.0~alpha.2-1.x86_64.rpm` so a later dev build cannot overwrite
the candidate — verify the SHA before every install)
RPM-reported version: `station-manager 2.0.0~alpha.2-1 x86_64` (`rpm -qp --queryformat
'%{NAME} %{VERSION}-%{RELEASE} %{ARCH}'`); daemon build line `version: 2.0.0-alpha.2, FFT:
PocketFFT (CGO, dynamically linked)`
Previous installed version: `station-manager-2.0.0~alpha.1.1238.gd9cd38ae.dirty-1.x86_64`
(commit `d9cd38ae` plus uncommitted local changes at the time it was built)
Clean-install environment: retired 2026-09-05 by operator risk decision; the partially prepared
Fedora 44 `systemd-nspawn` rootfs never installed or ran Station Manager
Upgrade environment: the daily-driver dogfood station (this host); the borrowed IC-7300 attached
only for the B3 groups, each separately agreed
Browser(s) and viewport(s): Firefox 155; see "Operator parameters (A4)"
Planned test date: 2026-09-05 onward
Operator: 7Q5MLV
Automated gate evidence: `task ci:local` passed at `b8e0356f` on 2026-09-05 — SPA lint, prettier,
svelte-check 0/0, vitest 1566/1566, Vite build; manual build; Go gofmt, vet, golangci-lint,
maintainability observatory 0 regressions, race and full test runs, static and PocketFFT builds,
PocketFFT FT8 decode test, ClubLog build-key boundary (ST-7); agent-context 10030/10240 bytes
Backup location (do not record secrets): private SMC LAN host; exact endpoint retained outside the
repository; remote archive SHA-256 recorded at A3-01
Restore check: completed at A3-02; the archive opened read-only with integrity and counts equal to A3-03
Recovery policy: keep a stopped-daemon copy of the complete working directory as the primary recovery
artifact; optionally keep an ADIF export for a simpler rebuild/import. If the candidate fails, prefer a
forward fix or rebuild from the repository and restore/import. The operator accepts that ADIF-only
recovery can lose Station Manager-specific metadata. PKG-10a/10b and the formal package downgrade are
not executed; Appendix 3 remains a prepared record, not a rehearsed claim.

Ready to deploy: pending
Operator/date:

Dogfood accepted: pending
Operator/date:
Residual waivers:
Follow-up destinations:

## Scope reduction — operator decision, 2026-09-05

The operator judged the original zero-data-loss, timed-backout acceptance plan disproportionate for
this personal dogfood workstation. The Fedora nspawn clean-install suite, synthetic failure injection,
PKG-10a, PKG-10b and all other rows whose environment begins **nspawn** are closed as **NOT EXECUTED —
scope retired by operator risk acceptance**, never PASS. The same disposition applies to any row whose
only purpose was the formal rollback rehearsal. The accepted residual risk is a rebuild plus
working-directory restore or ADIF import, with possible metadata loss for ADIF-only recovery.

The replacement deployment path is deliberately small: the operator makes a fresh local copy of the
complete working directory while `smd` is stopped; verify the frozen candidate RPM SHA; install it;
daemon-reload/start; then inspect version, migration, QSO/queue counts and the basic UI. Hardware and RF
remain separately gated and are not authorized by this decision.

## Operator parameters (A4)

Selected parameters for operator ratification at A4-01; hardware/RF parameters remain separately
gated.

- Browsers: Firefox 155 only; no second browser engine is required for this candidate.
- Representative viewport sizes and browser zoom values: 1920×1080 at 100%, 1280×800 at 100%,
  1024×768 at 125%, and 1280×800 at 200%.
- Enabled integrations (enrichment providers, forwarders, SM Cloud, session email, PSK Reporter) and
  the failure checks permitted against each (rejected credentials, unavailability, retry):
  Hamnut country lookup, QRZ callsign lookup, ClubLog, QRZ, SM Cloud, SMTP and PSK Reporter are
  enabled; QRZCQ and evidence synchronization are N/A while disabled. Live success checks use only
  an existing legitimate QSO or the next genuine contact—never a fabricated production QSO.
  Credential rejection, unavailable-endpoint and retry checks use only the isolated nspawn system
  with synthetic credentials/endpoints, at most one authentication rejection per applicable
  provider. Do not change live credentials. SMTP permits an unreachable-endpoint check but no
  deliberate authentication rejection. Disk-full testing is permitted only inside nspawn.
- Reconnect / timeout tolerances (SSE reconnection, daemon-unavailable indication, write-timeout
  "unknown" wording appearance): daemon unavailable within 15 seconds; ordinary write unknown
  within 32 seconds; email unknown within 47 seconds; rig/FT8 confirm-by-push within 3 seconds after
  pushed state; SSE recovery within 5 seconds after the daemon answers, or immediately after one
  tab-focus/online revival event; shutdown within the configured budget plus 2 seconds.
- Recovery method: formal rollback rehearsal and reserved window retired by the operator's 2026-09-05
  scope decision. Primary recovery is the stopped-daemon working-directory copy; rebuild/restore or
  ADIF import is acceptable, including possible Station Manager metadata loss in the ADIF-only case.
- ALARM-02 amber threshold: **30 — effective default; not explicitly stored** (`ft8.meter.alc_amber` is
  unset in the live configuration; `DefaultFt8AlcAmber`, `internal/types/ft8.go`; `config.md` §FT8 meter).
  Ratified 2026-09-05.
- FT8-10 clock skew: **no value ratified** — the case has neither a defined boundary nor an unambiguous
  visible result, and there is no proof that `faketime` governs the candidate's Go time source. FT8-10 is
  `BLOCKED` until a safe, independent clock mechanism is designed and ratified; only the operator may
  instead mark it `WAIVED` with the residual risk. It is never `PASS` or `N/A`.
- Lane labels (the first token of every row's environment cell): **live** = the daily-driver station
  with genuine data — never a fabricated production QSO, never a live credential change;
  **nspawn** = retired lane; every such row is NOT EXECUTED under the scope decision above;
  **host-scratch** = a read-only or scratch-copy check on this host that never
  touches the live working directory; **record** = a paperwork step.
- Hardware, rig-command and RF approvals: **deferred**; each B3 group is agreed separately before
  execution, with power, load, duration, abort action and observer recorded there.

## Resume contract — Gate A execution

**Superseded 2026-09-05:** do not resume steps 4, 5 or 5b. The current path is the short live deployment
sequence in "Scope reduction" above, and it must wait for the operator's fresh local-backup confirmation.

This is the durable handoff after an operator logout. A new Codex session in this repository executes
the command-line checkpoints below; it reads `AGENTS.md`, `docs/current.md`, this record and the
canonical gate first. The operator supplies sudo/SSH approval, performs the visible Firefox actions
and logout/login observation, judges each result, and remains the only authority for `PASS`, `FAIL`,
waivers, the ready-to-deploy decision and every later hardware/RF approval.

Resume prompt: **Continue the simplified alpha.2 dogfood deployment from the scope-reduction decision;
do not touch hardware/RF.**

1. Recheck the worktree, exact tag/commit, both candidate hashes, installed alpha.1 NVR and inactive
   live service. Stop on any identity change.
2. Before any candidate install, verify the installed alpha.1 payload with `rpm -V`; install
   `rpmrebuild` with operator approval, reconstruct the installed package, and compare NVR, file
   metadata/digests, scripts, dependencies and capabilities. Record its filename and SHA-256. It is
   a reconstructed rollback artifact, not the lost original RPM.
3. With live `smd` still stopped, archive the complete working directory and transfer it over SSH to
   a private directory on the trusted SMC LAN host. Encryption is not required on this two-machine
   trusted LAN. Require destination directory mode `0700`, archive mode `0600`, matching local and
   remote SHA-256, and no repository copy. Restore into a private scratch directory, open databases
   read-only, compare A3-03 counts, then remove the scratch copy.
3b. Run A1-04 (Appendix 2): the read-only `smd config-check` preflight on a scratch copy of the live
   `config.json` using the candidate binary extracted from the frozen RPM. A non-zero control result
   is a hard stop — the candidate would refuse to start on the station and no waiver can change that.
4. Provision a Fedora 44 `systemd-nspawn` rootfs with a dedicated user/private home and no live-data
   bind mounts. Stop and record the clean baseline before transferring or installing the candidate.
5. Verify the protected RPM SHA again, install that exact artifact only inside nspawn, and execute
   A2-01 through A2-07 plus A1-02. The operator performs the Firefox first-run, documentation and
   logout/login checks. Preserve transcripts/screenshots and record each result separately.
5b. Rehearse the complete rollback in two lanes (Appendix 3): PKG-10a inside nspawn on synthetic data
   under a new dedicated rollback user with an empty data directory (A2-07 leaves schema-8 data behind) —
   actual alpha.1 install, candidate upgrade, actual `rpm -Uvh --oldpackage` downgrade, guarded database
   down-migration, daemon-reload and restart; then PKG-10b on a host-scratch restore of the A3 archive with
   extracted binaries and a configuration rebased and contained under the scratch root (config-check plus
   the containment audit must pass, and the live paths, timestamps and counts must be unchanged before
   and after) — QSO preservation via the guarded down path, then the working-directory-restore variant with
   ADIF export/re-import.
6. Reconcile A1, A3, A4 and A5 evidence, run `task docs:check`, and present the record diff to the
   operator. Do not mark **Ready to deploy**, commit, push, install on the live station, start the
   live daemon, or touch hardware/RF without a new operator instruction.

Hard stops: a non-zero live-config preflight (A1-04); a rollback rehearsal that cannot restart
alpha.1 with equal QSO counts; any candidate hash/version mismatch; unexpected `rpm -V` change; unusable rollback RPM;
backup checksum/restore mismatch; accidental visibility of live station data inside nspawn; or an
A2 failure affecting installation, persistence, data safety or recovery.

## Release delta — `d9cd38ae` → `v2.0.0-alpha.2` (347 commits)

Enumerated per Gate A1. Full list: `git log --oneline d9cd38ae..v2.0.0-alpha.2`. Themes, newest
first, with the commits that anchor each claim.

### Migrations

- **Log database schema 0007 → 0008:** `operator_event` table for the durable notification history
  (`9fc9acfb`, `ca83a339`; W-0001 / ADR 0076). Has an `.up.sql` and `.down.sql`. Runs on first
  start of the new daemon; inspect the startup/migration diagnostics at B1-04.
- **No config.json version migration was added**; the config *migration runner* changed behaviour
  instead (next section).

### Configuration changes

- **Unknown `config.json` keys are rejected before any startup write** (`3c45fc48`, `24febadc`;
  ADR 0074 / W-0006). A hand-edited config with a stray or misspelled key that the old daemon
  silently kept now refuses to start with a named key. Exercised deliberately and in isolation at
  A1-04 (B1-05 is only the control rerun) — it is the one change in this delta that can turn an upgrade into a non-start.
- Config replacement is crash-durable and Settings surfaces the applied-durability caveat
  (`f5a74422`); datastore and logging blocks are validated at the config boundary (`f7d1de4a`,
  `d72e0bfb`); normalize-then-validate at the service update boundary.
- Fail-closed on an unacknowledged non-loopback TCP bind; zoned IPv6 loopback classified correctly
  (`505e0566`, `e78c7617`; ST-3a). A station bound off-loopback without the acknowledgement key will
  not start.
- Credentialed forwarders require https and follow only same-origin redirects; SM Cloud LAN
  cleartext needs an explicit acknowledgement (`cf269984`, `63322da3`; ST-4).
- **Prioritised callsign lookup** (`6aa974ab`): `lookup.chain` provider `priority` is authoritative
  (not array position); normalisation sorts the chain and validation requires positive, unique,
  contiguous priorities including disabled entries (`config.md` §lookup). An existing config with
  duplicate or gapped priorities is refused at the config boundary — covered by A1-04's control run.
- **Packaged unit stop backstop** (`d8e0eee9`; LC-2): `packaging/smd.service` sets `TimeoutStopSec=20s`
  as the absolute backstop behind the daemon's own budget-bounded teardown
  (`server.shutdown_timeout_sec`, 10 s floor) — the daemon's overrun report fires first (PKG-06).
- `config.md` reduced to the current contract (`1cfaf862`); the Settings tabs are now the only
  config editing surface (config SPA retired, `feccc67e`, `86e21afb`).

### Changed user journeys

- **Canonical root:** the consolidated operator SPA is served at `/`; `/app/…` paths redirect and
  the redirect is normalised against open-redirect abuse (`4852a499`, `cfb74cfa`). Logbook, Settings
  and FT8 are lazy-loaded chunks (`a7e12ca4`) — first open of each is a network fetch.
- Config SPA, logbook SPA and logging SPA retired into the app: Settings gained the QSL defaults
  editor, Rigs add/edit/delete with the CAT master switch, ClubLog retry-only upload workflow
  (`17bb2ffa`, `3e892067`, `8c42755e`, `7b5ed1d2`, `1e3a7bed`, `23adba90`, W-0003).
- Build identity in the shell and tab title, guarded against stale out-of-order loads
  (`86f3f833`, `84214b76`).
- Durable notification history rail with browser-export failures and `forward.failed` recorded
  (`7ba04d7e`, `6a160a26`, `3a0b57de`; W-0001).
- Forwarder queue depth and a **Clear queue** action per forwarder (`2a8a5297`, `88dd7a3b`,
  `6c99a5fb`, `ac1aef80`; W-0005), with reconcile-on-indeterminate-clear (`53ae72f0`, `6b8477d2`).
- **Ambiguous write outcomes (F-04, ADR 0078):** a timed-out QSO edit, upload enqueue, session
  email/export, restart, config save, rig tune, rig command or FT8 arm now reports *unknown* with
  operation-specific guidance, or reconciles via re-read / pushed state — never a definite failure
  the daemon then contradicts (`d12d8146`, `ca2ee9b8`, `f405be74`…`4df158e9`, `8e7b382e`,
  `3caa8a70`, `73f097c9`). Expect new wording on those paths; the old "failed" toast is gone.
- SPA validates success/safety wire records (F-03, ADR 0077): a malformed frame is dropped, not
  rendered (`ef6d9141`, `d43720ce`, `eb23d1cf`).
- A QSO update with no effective change is a no-op (`ded00ab1`); logbook PATCH is field-level
  partial (`15b2232b`); re-enrichment keeps prior data across a partial same-callsign lookup and
  discards stale results after a callsign change (`88916515`, `11c4c94a`, `dc15e188`).
- FT8 occupancy colours unified into readable light/dark tokens (`d6fa11cd`).
- Session-email stamps only the QSO revisions it actually sent (`83b32731`); SMTP rejection text
  and recipient/subject are redacted from logs (`9e520a2b`, `a2aa5fe6`; H4).

### Changed integrations

- **SM Cloud payload (alpha.2):** daemon-local ids are stripped from the cloud QSO payload and
  public QSO responses are projected (`ddb144c0`, `f0bdd51d`, `6cad7cec`, `a0f5b596`; ADR 0016).
  **Gate A5 applies to this upgrade** — see the A5 rows and the prepared inventory procedure.
- SM Cloud rejects equal-version divergent writes as `version_conflict` (`8bc0d9b3`); ingest batch
  capped at 1,000 rows and oversized bodies map to 413 (`0a825691`, `8635306b`); reconcile summary
  carries discovered/skipped/deferred/limit (`6e4d43ee`).
- ClubLog build-key boundary enforced: the key is baked only into private builds (`a81948b8`; ST-7)
  — this RPM is a private build.
- ADR 0073 HamQTH lookup/forwarder is **design only** (`dfa1e6ab`); nothing to exercise.
- Import aborts instead of replaying after an unverified rollback (`7b5bf743`); DB files secured on
  import/restore and existing backups (`88e4fc8e`; ST-6).
- Daemon lifecycle runs through the ADR 0070 orchestrator (`3a850604`, `22303de0`; phases 1–3):
  start/stop ordering, drain-before-close for databases and PSK Reporter. Observable at B1-06 and
  the PKG rows.

### New manual or setup instructions

- **Embedded manual:** the forwarding chapter now documents three destinations — QRZ.com, Club Log and
  **QRZCQ** (`58563607`, the QRZCQ QSO forwarder) — and forwarder queue clearing (`60f3a67c`). Verify
  at DOCS-02 that these match the Settings → Forwarding surface (QRZCQ is disabled on this station,
  so its documented setup is read-only evidence here).
- `install.md` reconciled for the SPA retirements (`8ede343d`); §7 Update is the upgrade path B1-01
  follows: `sudo dnf install <rpm>` over the existing install, then `systemctl --user daemon-reload`,
  then `systemctl --user restart smd` (replace → reload → restart; no separate stop, no uninstall).
  The IC-7300 pre-connect checklist (USB SEND = OFF, CI-V Transceive enabled, linked USB/REMOTE CI-V,
  matching baud) is the existing `install.md` §IC-7300 setup and is a precondition of B3 group 1.

## Gate A — ready to deploy

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `A1-01` | yes | **record** — Clean tree at `b8e0356f`, tag `v2.0.0-alpha.2` | Build once with `task rpm:dev:pocketfft`; record filename, SHA-256, RPM version | One artifact; RPM version `2.0.0~alpha.2-1`; SHA recorded above | A `-dirty` or `alpha.1-N-g…` version (built from an untagged or dirty tree) | header fields; step-1 recheck 2026-09-05 11:10Z: HEAD `b8e0356f` = tag `v2.0.0-alpha.2^{}`; both RPM copies `7b2fa20a…c24856`, 9,053,968 B; installed `2.0.0~alpha.1.1238.gd9cd38ae.dirty-1`; `smd` inactive, no process; tag local only; `-dirty` in `git describe` is the uncommitted capsule/record only | none | pending (evidence recorded; operator to rule) |
| `A1-02` | yes | **nspawn** — the A2 clean-install container, candidate installed and started (after A2-01) | Verify the candidate's SHA-256 before install; then compare `GET /v1/version`, the sidebar build badge, the startup log line and `rpm -q station-manager` | All four read `2.0.0-alpha.2` (RPM `2.0.0~alpha.2-1`); SHA matches the header | Badge shows a cached previous build; log shows `dev`; a different SHA (a rebuilt or overwritten `…-dev` RPM) | screenshot + log line + `sha256sum` output | none | pending |
| `A1-03` | yes | **record** — Record drafted | Review the release delta above against the candidate (migrations, configuration, journeys, integrations, manual/setup) and correct anything missing | Delta complete enough that every B case has a cause | Accepting the delta without opening `git log d9cd38ae..v2.0.0-alpha.2` | reviewer initials | none | pending |
| `A1-04` | yes | **host-scratch** — a 0700 scratch directory holding a copy of the live `config.json`; the candidate `smd` extracted from the frozen RPM (Appendix 2); live service untouched | Run the read-only preflight `smd config-check --config` on the untouched copy (control), then on the two mutated copies (a top-level and a nested unknown key) | Control exits 0 ("no unrecognised keys"); each mutation exits non-zero naming only the key path; nothing started, bound or written | Control exits non-zero — the candidate would refuse to start on the station: fix the live config and re-run (**no waiver is possible**); a mutation exits 0 (old behaviour); a value printed | run 2026-09-05 11:15Z (Execution log #4): SHA verified; `smd` extracted, never installed; control on the live copy → exit 0 "no unrecognised keys"; top-level `typo_key_b1_05` → exit 1 naming only the path; nested `bridge.typo_nested_b1_05` → exit 1 naming only the path; live `config.json` mtime unchanged (2026-08-18 14:01:28); `smd` inactive before/after; scratch shredded and removed | none | pending (evidence recorded; operator to rule) |
| `A2-01` | yes | **nspawn** — Isolated RPM host, no SM data | Install per `install.md`; enable/start user service; note lingering step | Service active; health and version endpoints answer; logs readable; browser reaches `/` | Service starts but binds loopback only and the guide's URL differs; lingering omitted so the service dies at logout | terminal transcript | none | pending |
| `A2-02` | yes | **nspawn** — Fresh install, first browser open | Open the app | Only the first-run welcome surface; no operate/logbook chrome | Dashboard placeholder renders before setup completes | screenshot | none | pending |
| `A2-03` | yes | **nspawn** — First-run callsign form | Enter invalid, empty, lowercase/whitespace-normalized, then valid callsign | Each state explains itself; the normalized form is shown before acceptance | Lowercase accepted silently without showing the normalized value | screenshots | none | pending |
| `A2-04` | yes | **nspawn** — Valid callsign submitted | Complete setup | Default logbook created; both **Open Settings** and **Start logging** offered | Only one journey offered, or logbook missing in Logbook view | screenshot + Logbook view | none | pending |
| `A2-05` | yes | **nspawn** — Setup complete | Reload browser; restart daemon; reopen | Setup stays complete; no welcome surface | Welcome reappears after restart (setup flag not persisted) | screenshots | none | pending |
| `A2-06` | yes | **nspawn** — Setup complete, guide only | Find Settings, the embedded Manual, and log a first QSO using only the install guide + manual | All three reachable without repository knowledge; note every gap | Reaching Manual only via a URL learned from the repo | note of gaps | none | pending |
| `A2-07` | yes | **nspawn** — Installed | Uninstall per the guide | Package removed; operator data deliberately retained as documented | Data directory deleted, or package left half-removed | `ls` of the data dir after removal | none | pending |
| `A3-01` | yes | **live** — Dogfood station, daemon stopped or quiescent | Back up the complete working directory outside the repo | Backup exists; listable | Backup copied into the repository tree | local staging archive 2026-09-05 11:14Z (Execution log #3): `~/sm-backup/station-manager-pre-alpha2-20260905T1114Z.tar.gz`, 18,185,501 B, mode 0600, SHA-256 `066f769b38914ba8fed3cc31f7082d5b9f76ae6d3e169390dccab2f0cd0503b9`, 67 entries, listable; `smd` quiescent and the log database unheld at archive time (WAL present and included); operator copied it with the prescribed 0700-directory/0600-archive sequence to the private SMC LAN host (exact endpoint retained outside the repository); remote SHA-256 matched exactly | none | pending (evidence recorded; operator to rule) |
| `A3-02` | yes | **host-scratch** — restore of the A3 archive: Backup made | Restore a copy into a scratch location; open it read-only | Restore completes; record counts readable | Backup lists but does not restore (permissions / partial copy) | run 2026-09-05 11:18Z (Execution log #5): archive extracted into a 0700 scratch dir (68 entries); database opened read-only (`mode=ro`): `schema_migrations_log` `(7, 0)`, `integrity_check` ok, 7,468 `qso` rows (7,468 with `deleted_at` NULL; logbook 1), `qso_upload` 13,089 `uploaded` + 1 `failed` (forwarder_type `qrz`), `operator_event` absent — all equal to A3-03; scratch config copies shredded, directory removed; live untouched | none | pending (evidence recorded; operator to rule) |
| `A3-03` | yes | **live** — Pre-upgrade | Record durable-record counts/checksums (QSOs per logbook, upload-queue rows by status, notifications) | Numbers recorded here without contents | Counting after the upgrade began | Read-only count, 2026-09-05: logbook id 1 — 7,468 total/active QSOs, 0 deleted; upload queue — 13,089 uploaded, 1 failed; `operator_event` absent as expected before migration 0008 | none | pending (operator to confirm after A3 backup) |
| `A3-04` | yes | **live** — pre-upgrade | Produce and verify all three rollback artifacts: the reconstructed alpha.1 RPM (`rpmrebuild`; NVR and SHA-256 recorded), the A3 archive, and the rehearsed database rollback path from PKG-10a/10b (Appendix 3) | Artifacts present beside each other; the header names the rehearsed path (down-migration, with the workdir-restore variant), its quiescence barrier and its post-upgrade-QSO handling | Only the RPM preserved — alpha.1 refuses the version-8 database and the rollback stalls mid-window | reconstructed RPM 2026-09-05 11:40Z (Execution log #6): `build/private/x86_64/station-manager-2.0.0~alpha.1.1238.gd9cd38ae.dirty-1.x86_64.rpm`, 7,359,884 B, mode 0600, SHA-256 `c4fd092f329980c32283e773e0311ab07e7ae5f621ff177362b88e9ca174a807`; exact NVR/arch and payload size/mtime/digest/mode/owner/group match; scripts/triggers/file capabilities match; regenerated-header differences recorded in the log; local/remote A3 archive hashes match; PKG-10a/10b still pending | none | pending |
| `A3-05` | yes | **live** — Before starting B1 | Confirm rollback time is reserved before the station is next needed | Window recorded in "Operator parameters" | Upgrading right before an operating session | note | none | pending |
| `A4-01` | yes | **record** — Record corrected; delta reviewed (A1-03) | **Ratify the case inventory:** confirm a decisive case exists for every applicable B1/B2/B3 bullet of the canonical gate; fill "Operator parameters (A4)" — browsers, viewport/zoom values, enabled integrations and permitted failure checks, reconnect/timeout tolerances, rollback rehearsal method and window; split any row that still hides several outcomes | Every A4 parameter filled (browsers, viewports/zoom, integrations and permitted failure checks, tolerances, rollback rehearsal/window, the ALARM-02 threshold, and FT8-10's threshold or its recorded BLOCKED/WAIVED disposition); no B row depends on an unstated threshold; hardware/RF approvals explicitly deferred | Executing B rows with tolerances decided ad hoc during the run, so `PASS`/`FAIL` cannot be judged consistently | this record's parameters section + operator initials/date | none | pending |
| `A5-01` | yes | **live** — read-only: Old daemon still installed, SM Cloud forwarder enabled | Run the prepared read-only inventory (Appendix 1) for the SM Cloud forwarder's `qso_upload` rows whose status is not `uploaded` | Counts by status (`pending` / `in_progress` / `failed`) recorded here | Counting all forwarders' rows, not only SM Cloud's; or running the query with write access | Read-only/query-only inventory, 2026-09-05: zero non-`uploaded` SM Cloud rows; zero failed SM Cloud rows | none | pending (operator to confirm) |
| `A5-02` | yes | **live** — Non-uploaded rows exist | Let `pending`/`in_progress` drain under the **old** daemon; resolve each `failed` row individually | Zero non-uploaded SM Cloud rows before upgrading | Clearing the queue with the new Clear-queue action instead of draining | A5-01 found no applicable rows; no drain or queue mutation performed | none | pending (operator to mark N/A) |
| `A5-03` | yes | **live** — Zero non-uploaded rows | Proceed to B1; after upgrade confirm reconcile reports no `version_conflict` | No terminal failures attributable to the payload change | A `failed` row appearing right after upgrade (the false conflict A5 exists to prevent) | forwarder status view | none | pending |

## Gate B — deploy and accept

### B1 — the upgrade itself

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `B1-01` | yes | **live** — old version running; A1-04, A3, A5 and PKG-10a/10b done | Upgrade per `install.md` §7: `sudo dnf install <candidate rpm>` over the existing install, then `systemctl --user daemon-reload`, then `systemctl --user restart smd` (no uninstall, no separate stop) | Package replaced while the old daemon runs; reload; restart observed; browser reconnects within the A4 tolerance | Install succeeds but the restart is skipped (old binary still serving); or a `dnf` scriptlet stops the service and leaves it stopped | transcript | none | pending |
| `B1-02` | yes | **live** — Upgraded dogfood station | **Repeat A1-02 here:** `/v1/version`, badge, startup log, `rpm -q`; then deliberately reload the open tab | All four read `2.0.0-alpha.2`; new UI; no old chunk served from cache | Old lazy-loaded chunk still executing in an unreloaded tab; badge from a cached tab | screenshot + log line | none | pending |
| `B1-03` | yes | **live** — Upgraded | Confirm setup bypassed; config, rig, logbook, operator identity, theme/navigation, durable notifications retained | All present as before | First-run welcome shown (setup flag lost) or rig list empty | screenshots | none | pending |
| `B1-04` | yes | **live** — Upgraded | Compare A3-03 counts; inspect startup/migration diagnostics for 0008 | Counts equal; migration to schema 8 logged once, no errors | Counts differ, or migration re-runs on every start | log excerpt + counts | none | pending |
| `B1-05` | yes | **host-scratch** — immediately before B1-01, a fresh copy of the live `config.json` as it is at that moment | Re-run the A1-04 control check only (Appendix 2 steps 4–5) | Exit 0 | Non-zero: stop, fix the named key on the live file, re-run — a refusal cannot be waived because the candidate would not start | transcript | none | pending |
| `B1-06` | yes | **live** — Upgraded | Controlled daemon restart | UI and event streams (rig/FT8/notifications SSE) recover without a manual reload, within the A4 reconnect tolerance | Streams stay dead until reload | screenshot of reconnected state | none | pending |

### B2 — surface inventory (one decisive row per outcome)

#### Application shell and navigation

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `SHELL-01` | yes | **live** — Upgraded, setup complete, first open of the session | Open Dashboard placeholder, Operate Phone/CW, Operate FT8 | Each renders immediately | A view renders a blank pane while a chunk fails silently | screenshots | none | pending |
| `SHELL-02` | yes | **live** — Same session | Open Logbook, Settings, FT8 for the first time, then again | First open fetches its chunk once; second open is instant | A lazy chunk 404s after upgrade (stale hashed asset name in a cached shell) | network panel + screenshots | none | pending |
| `SHELL-03` | yes | **live** — Same session | Open Manual; open Contacts Map in its separate tab | Manual renders offline; Map opens in a new tab with its own title | Map replaces the operating tab | screenshots | none | pending |
| `SHELL-04` | yes | **live** — Any view | Deep link to each route; browser back/forward; reload | Route restored; no redirect loop | `/app/logbook` bounces to `/` root instead of Logbook | URL bar + view | none | pending |
| `SHELL-05` | yes | **live** — Any view | Follow a stale `/app/…` link, then an `/app//evil.example` style link | Same-origin redirect to the canonical route; external target never followed | Redirect leaves the origin | URL bar | none | pending |
| `SHELL-06` | yes | **live** — Any view | Inspect header station/rig state and build identity; tab title | Identity `2.0.0-alpha.2`; station/rig state matches Settings | Identity of a previous build from a cached tab | screenshot | none | pending |
| `SHELL-07a` | yes | **live** — durable notifications exist (A3-03 count) | Open the notification history rail; reload the tab | Rail lists the durable entries; identical after reload | Rail empties or shows stale entries after reload (stale-response overwrite) | screenshots | none | pending |
| `SHELL-07b` | yes | **nspawn** — clean install with one synthetic QSO | Induce one browser-export failure (e.g. refuse the download) and re-open the rail | Exactly one new durable `export.adif_failed` entry, once, surviving reload | Entry duplicated or missing | screenshot | none | pending |
| `SHELL-08a` | yes | **live** — an export (PHONE-12) and a refused Settings save (SETUP-10) | Trigger an info toast and an error toast | Levels visually distinct; dismiss and auto-expiry per design | An error styled as info | screenshots | none | pending |
| `SHELL-08b` | yes | **nspawn** — the unknown-outcome write of PHONE-08 | Trigger the warn toast | Warn distinct from error; dismiss/expiry per design | The warn rendered as an error | screenshot | none | pending |
| `SHELL-09` | yes | **live** — Rig disconnected / daemon stopped | Observe connection and offline states; restart | Distinct "daemon unavailable" and "CAT lost" indications; recovery within tolerance | Silent blank views with no offline indication | screenshots | none | pending |
| `SHELL-10` | yes | **live** — Light and dark theme | Toggle theme; collapse navigation; open the utility rail | Readable in both; states persist where promised | Occupancy colours unreadable in one theme | screenshots | none | pending |
| `SHELL-11` | yes | **live** — Keyboard only | Tab through the operate view; use documented shortcuts | Focus visible; shortcuts act on the documented target | A rig shortcut fires while typing in a field | notes | none | pending |
| `SHELL-12` | yes | **live** — A modal open (e.g. session-edit) | Press Escape; then try a rig shortcut while the modal is open | Modal closes; rig shortcuts are inert under a modal | Escape closes the modal and a rig shortcut still detunes | notes | none | pending |
| `SHELL-13` | yes | **live** — Settings with unsaved edits | Navigate away | Unsaved guard prompts; discard/stay both work | Edits lost silently | screenshot | none | pending |
| `SHELL-14a` | yes | **live** — 1920×1080 at 100%, light and dark | Inspect every view | No clipped decisive information; no obscured controls in either theme | Controls hidden below the fold | screenshots | none | pending |
| `SHELL-14b` | yes | **live** — 1280×800 at 100%, light and dark | Inspect every view | As 14a | As 14a | screenshots | none | pending |
| `SHELL-14c` | yes | **live** — 1024×768 at 125%, light and dark | Inspect every view | As 14a | Legend/rail clipped at this width | screenshots | none | pending |
| `SHELL-14d` | yes | **live** — 1280×800 at 200%, light and dark | Inspect every view | As 14a; layouts reflow rather than overflow horizontally | Horizontal page scroll or overlapping controls | screenshots | none | pending |

#### TX and drive alarms

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `ALARM-01` | yes | **live** — A stuck-TX alarm raised (only possible in B3 group 3/4, or by a daemon-published alarm frame if the operator permits a non-RF trigger) | Dismiss the banner; raise a NEW alarm; observe the daemon's clear | Dismissal hides it; a new alarm re-shows even after dismissal; the daemon's positive-RX clear retires it | A dismissed banner never returns for a new alarm; a clear leaves it showing | screenshots | RF (live raising) — passive verification of dismiss/re-show only | pending |
| `ALARM-02` | yes | **live** — TX drive present (B3 group 3/4) | Observe the TX-drive chip during and after transmission | Amber at the A4-agreed ALC threshold; chip marks itself stale after the meter-poll window | Chip stays "healthy" from a stale reading | screenshot with timestamp | RF | pending |
| `ALARM-03` | yes | **live** — CAT live, no TX | Disconnect the rig while the drive chip is displayed | Chip goes stale/unknown, never a false "healthy" | Last value frozen as current | screenshot | hardware | pending |

#### First-run and station configuration

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `SETUP-01` | yes | **live** — Settings → Station | Change a value, save; save again with no change | Saved once; no-change save is a visible no-op, not an error | No-change save reports "saved" and rewrites config | screenshots | none | pending |
| `SETUP-02` | yes | **live** — Settings → Station → QSL defaults | Edit defaults, save, reload | Persisted; applied to the next QSO | Defaults saved but not applied | screenshot + next QSO | none | pending |
| `SETUP-03a` | yes | **nspawn** — Settings → Rigs, no rigs | Add a scratch rig | The row appears immediately with the entered model / ft8_mode / MY_RIG; restart-required messaging where promised | Row appears only after a reload | screenshot | none | pending |
| `SETUP-03b` | yes | **nspawn** — the scratch rig present | Edit its model, then ft8_mode, then MY_RIG, saving each | Each edit lands on the right row immediately | An edit lands on a different row or reverts on reload | screenshots | none | pending |
| `SETUP-03c` | yes | **nspawn** — two scratch rigs present | Delete the second | Only the second row disappears; the first is intact after reload | The first row is removed | screenshot | none | pending |
| `SETUP-04` | yes | **live** — Settings → Rigs | Flip the CAT master switch off and on | State reflected without reload; rig surfaces gate closed/open accordingly | Needs a reload to reflect | screenshot | none | pending |
| `SETUP-05` | yes | **live** — Settings → FT8 | Initial values, validation, save, reload, discard | Persist; validation refuses out-of-range; discard restores | Out-of-range accepted | screenshots | none | pending |
| `SETUP-06` | yes | **nspawn** — Settings → Forwarding | Edit a forwarder incl. a credential; save; reload | Credential redacted on read and preserved on save; https requirement enforced for credentialed forwarders | Masked secret written back as the mask string | screenshots + forwarder still works | none | pending |
| `SETUP-07` | yes | **nspawn** — Settings → Email | Edit SMTP settings incl. password; save; send a test if offered | Redaction/preservation as SETUP-06; rejection text not leaked into logs | Password cleared on an unrelated save | screenshots + log grep | none | pending |
| `SETUP-08` | yes | **nspawn** — Settings → Enrichment | Edit provider settings/credentials; save; reload | As SETUP-06 | — | screenshots | none | pending |
| `SETUP-09` | yes | **live** — Settings → General/About | Inspect version/build, theme, general options; save | Version matches B1-02; options persist | About shows a different version than the badge | screenshot | none | pending |
| `SETUP-10` | yes | **live** — Any Settings tab | Provoke two simultaneous validation errors | Both shown; fixing one leaves the other | Only the first error shown | screenshot | none | pending |
| `SETUP-11` | yes | **nspawn** — Settings save with the daemon paused | Save, then resume the daemon | Outcome reported as unknown with edits kept; re-read reconciles (F-04c) | "Failed" toast while the save actually persisted | screenshot + config on disk | none | pending |
| `SETUP-12` | yes | **live** — Settings → Rigs, no rig attached | Hardware discovery | Distinguishes logging-only setup from CAT/FT8 setup; no error for an absent rig | Discovery error blocks logging-only use | screenshot | none | pending |

#### Phone/CW operating journey

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `PHONE-01` | yes | **live** — Operate Phone/CW, CAT off | Set band/mode/frequency manually | Manual state shown; band and frequency cannot disagree | Frequency outside the selected band accepted silently | screenshot | none | pending |
| `PHONE-02` | yes | **live** — Manual state set | Attempt to log before confirming the band | Confirmation required first; after confirm, logging enabled | Logging allowed against an unconfirmed band | screenshot | none | pending |
| `PHONE-03` | yes | **live** — CAT live (after HW-01) | Observe frequency/mode/VFO derived from the rig; turn the rig's dial and mode knob | Display follows pushed state; band re-derives; confirm gate re-arms on band change | Display lags to a stale value after a VFO switch | screenshots | hardware | pending |
| `PHONE-04` | yes | **live** — Callsign entry | Type a callsign | Enrichment, worked-before and country context fill or degrade to empty without blocking | Enrichment failure blocks submission | screenshot | none | pending |
| `PHONE-05` | yes | **live** — Callsign entered | Edit RST sent/received; add a comment via the picker and free text | Defaults per mode; edits retained into the submitted QSO | RST reset to default on submit | Logbook row | none | pending |
| `PHONE-06` | yes | **live** — the next genuine contact only: Filled form | Submit | QSO persisted once; cleared vs retained fields as promised | Retained field silently cleared | Logbook + session list | none | pending |
| `PHONE-07` | yes | **nspawn** — Same station again within a minute | Submit a duplicate | Duplicate handled as documented (fold or explicit deliberate-repeat) | Second contact silently lost | Logbook | none | pending |
| `PHONE-08` | yes | **nspawn** — Submit while the daemon is paused | Submit, resume | Unknown-outcome wording; re-read shows the QSO once (F-04a) | Retry creates a second QSO | Logbook | none | pending |
| `PHONE-09` | yes | **live** — Session with QSOs | Session list and session timer | Rows and timer correct | Timer restarts on reload | screenshot | none | pending |
| `PHONE-10` | yes | **live** — Several callsigns typed | Callsign stack / pile-up behaviour | Stack retains, recalls and clears as documented | Stack lost on view switch | screenshot | none | pending |
| `PHONE-11` | yes | **live** — Session | Rig and session panels | Panels reflect state; Go-manual confirm affordance where applicable | Panel shows CAT connected while CAT is off | screenshot | none | pending |
| `PHONE-12` | yes | **live** — Session | Export | Download arrives; server-side backup archived | "Failed" while the archive was written (F-04b wording) | file + archive | none | pending |
| `PHONE-13` | yes | **live** — once, an existing legitimate QSO: Session, email configured | Session email | Delivered once to the configured recipient; only sent revisions stamped | Sent twice after an unknown outcome | inbox + row history | none | pending |
| `PHONE-14` | yes | **live** — Session | Map launch | Map opens with the session's contacts | Map opens empty | screenshot | none | pending |
| `PHONE-15` | yes | **live** — the same genuine contact: QSO submitted | Inspect audit/history and upload-queue creation | Queue rows created atomically with the QSO | QSO stored with no queue row | queue counts | none | pending |
| `PHONE-16` | yes | **nspawn** — Provoke a visible error (e.g. forwarder credentials rejected) | Observe recovery guidance | Error names the action the operator can take; logging stays usable | Generic "error" with no recovery | screenshot | none | pending |

#### FT8 receive and operating journey

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `FT8-01` | yes | **live** — Operate FT8, no audio device | Open the view | Missing-audio state explained; nothing captured | Silent empty band activity | screenshot | none | pending |
| `FT8-02` | yes | **live** — Audio present, CAT off | Open the view | Capture ownership shown; level meter and occupancy render; controls disabled with "CAT required" | Band buttons enabled while CAT is off | screenshot | none | pending |
| `FT8-03` | yes | **live** — Decodes arriving | Band/filter controls, decode feed, hashed-call hiding | Feed updates; filters apply | Decode from the previous band shown after a QSY | screenshots | none | pending |
| `FT8-04` | yes | **live** — Decodes arriving | Caller queue and enrichment on a CQ | Queue and enrichment populate; no transmit | Queue entry treated as an engagement | screenshot | none | pending |
| `FT8-05` | yes | **live** — CAT live, TX **disarmed** | Press **Enable TX** with the daemon paused, then resume | Warn "Couldn't confirm that FT8 TX was enabled…"; control follows pushed state; **no RF** | Control flips to enabled on the click itself | screenshot | rig command (arming) | pending |
| `FT8-06` | yes | **live** — CAT live, TX armed, idle (B3 group 2 agreed) | Disconnect CAT | TX disarms with the cause shown (`cat_lost`); session, if any, ends with the reason | Stays armed after CAT loss | screenshot + log | hardware | pending |
| `FT8-07` | yes | **live** — CAT live, TX armed | Turn the rig's dial | TX disarms (`dial_moved`); any session ends with `dial_moved`; no key | A rung keyed on the new frequency | screenshot + log | hardware | pending |
| `FT8-08` | yes | **live** — Capture running | A slot spanning a dial change (QSY during a slot) | That slot's decodes suppressed; empty `ft8-decode` still advances the clock; no spot, no sequencer advance | Mixed-slot decodes shown or spotted | feed + PSK page | hardware | pending |
| `FT8-09` | yes | **live** — Capture running, quiet band | Decode-empty slots | Slot clock advances; no rows added | Feed stalls | screenshot with slot times | none | pending |
| `FT8-10` | yes | **nspawn** — no safe independent clock mechanism designed | Do not execute. Reopen only when a mechanism with a defined skew boundary, an unambiguous visible result, and proof that it governs the daemon's Go time source is ratified at A4 | — | Treating a container clock trick or "not exercised" as evidence | this row + A4 | none | BLOCKED (operator may mark WAIVED with the residual risk) |
| `FT8-11` | yes | **live** — Daemon restart mid-view | Restart | Terminal state shown; view recovers on reconnect; no stale "armed" | Stale armed indicator after restart | screenshot | none | pending |
| `FT8-12` | yes | **live** — Any FT8 exchange (B3 group 4) | Trace one exchange end to end | Distinct evidence for decode → attempted exchange → completed exchange → logged QSO → forwarded QSO; only the completed exchange logs | A decode or attempted exchange logged as a QSO | screenshots + logbook + forwarder | RF | pending |
| `FT8-13` | yes | **live** — Caller queue with a skipped caller (B3 group 4) | Skip-if-silent, then Next | Skip arms daemon-side; silence ends the rung without keying the repeat; next caller offered | Repeat keyed despite skip | screenshots + log | RF | pending |

#### Logbook and records

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `LOG-01` | yes | **live** — Logbook | Select logbook; paginate; change page size | Correct rows and counts; selection behaviour documented | Page size change resets selection silently | screenshots | none | pending |
| `LOG-02` | yes | **live** — Logbook | Empty, error and loading states; upload/email filters | Each state distinct; filters correct | Loading shown as empty | screenshots | none | pending |
| `LOG-03` | yes | **live** — a genuine no-op edit: A QSO row | Open detail; save with no change | Genuine no-op: no revision, no queue row | No-op edit bumps the revision | row history | none | pending |
| `LOG-04` | yes | **nspawn** — A QSO row | Change a field and save; enter an invalid value | Changed edit persists; validation refuses | Invalid value stored | row history | none | pending |
| `LOG-05` | yes | **nspawn** — A QSO row, daemon paused | Save an edit, resume | Unknown wording, re-read confirms (F-04a) | Duplicate revision on retry | row history | none | pending |
| `LOG-06` | no | **n/a** — QSO deletion is not exposed in the candidate's UI (`DELETE /v1/qso/{uuid}` exists in the API only; no Logbook control) | None — record `not exposed` | — | Treating the API route as an operator surface | this row | none | not exposed |
| `LOG-07` | yes | **live** — an existing legitimate QSO: Rows selected | Re-enrich | Enrichment updated or preserved on a partial lookup; stale result after a callsign change discarded | Prior enrichment wiped by a partial lookup | screenshots | none | pending |
| `LOG-08` | yes | **live** — once, an existing legitimate QSO: Rows selected, email configured | Email selected rows | Delivered once; selection clears as promised | "Failed" toast but delivered (F-04b) | inbox | none | pending |
| `LOG-09a` | yes | **live** — the one existing failed non-SM-Cloud upload row | First, read-only: identify its destination forwarder and `last_error` cause (Appendix 1 pattern, `forwarder_type <> 'smcloud' AND status = 'failed'`) and confirm the QSO is legitimate; then retry that single row once | Exactly one external retry; truthful resulting status; no other row touched | Retrying without the read-only identification, or a second attempt | read-only query output + status after | operator confirmation immediately before the single retry | pending |
| `LOG-09b` | yes | **nspawn** — synthetic rows, some already uploaded | ClubLog retry-only backfill | Backfill sends only rows never uploaded | Already-uploaded rows re-sent | synthetic endpoint log | none | pending |
| `LOG-10` | yes | **nspawn** — Scratch logbook | ADIF import (default and selected logbook) | Duplicate/error summary; restart handoff documented | Import replays after an unverified rollback | summaries | none | pending |
| `LOG-11` | yes | **nspawn** — Scratch logbook | Export, then re-import the export | Round-trip clean; counts equal | Re-import creates duplicates | counts | none | pending |

#### Map and visual context

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `MAP-01` | yes | **live** — Contacts Map, origin set | Time window, band filter, legend/counts, grey line | Correct counts; legend matches filters | Band colour overrides ignored | screenshots | none | pending |
| `MAP-02` | yes | **live** — Origin unset | Open the map | No-origin state explained; nothing plotted wrongly at 0,0 | Contacts plotted from a null origin | screenshot | none | pending |
| `MAP-03` | yes | **live** — Map | Pan, zoom, reset; live update on a new QSO | Controls work; new contact appears without reload | Reset leaves the previous zoom | screenshots | none | pending |
| `MAP-04` | yes | **live** — Map, daemon paused | Observe | Offline state; live updates resume | Frozen map with no indication | screenshot | none | pending |
| `MAP-05` | yes | **live** — Contacts without a plottable grid; a window exceeding the cap | Open the map | Unplottable count shown; capped messaging names the cap | Unplottable contacts silently dropped | screenshot | none | pending |
| `MAP-06` | yes | **nspawn** — Force a data error (e.g. daemon returns an error for the window) | Observe | Error state named; map remains usable | Empty map shown as "no contacts" | screenshot | none | pending |
| `MAP-07` | yes | **live** — Each A4 viewport/zoom, both themes | Inspect | Legend legible; no obscured controls | Legend clipped at narrow width | screenshots | none | pending |

#### Packaged commands, lifecycle and recovery

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `PKG-01` | yes | **live** — Shell | `smctl status` with the daemon up and down | Truthful state each time | `status` reports active while the daemon is down | transcript | none | pending |
| `PKG-02` | yes | **live** — Shell | `smctl start`, `stop`, `restart` | Documented output; UI reconnects after restart | Restart returns before the daemon answers | transcript | none | pending |
| `PKG-03` | yes | **nspawn** — Shell, scratch ADIF | `smctl import` journey | Documented output; summary matches LOG-10 | Import handoff leaves the daemon stopped | transcript | none | pending |
| `PKG-04a` | yes | **nspawn** — `smtest` session with linger enabled per the guide | Exit the `machinectl shell` (logout), check from the host, then `machinectl reboot fedora44` and check again (Appendix 4) | Daemon survives logout and is up after the container boots without a manual start | Needs a manual start after either | transcript | none | pending |
| `PKG-04b` | yes | **live** — after B1 only | Real desktop logout/login, then a reboot when the station is free | Daemon up without a manual start each time | Needs a manual start | transcript | none | pending |
| `PKG-05` | yes | **live** — Daemon running, browser open | Stop; wait beyond the A4 tolerance; start | Unavailable indication then recovery; SSE reconnects without reload | Rail/streams stay dead | screenshots | none | pending |
| `PKG-06` | yes | **live** — work in flight (an upload retry pending) | Shutdown where safe | Drains or defers cleanly; no lost queue row; the daemon's own budget (`server.shutdown_timeout_sec`, 10 s floor) completes before systemd's `TimeoutStopSec=20s` backstop, and any overrun is reported by the daemon first | Queue row lost, or systemd's SIGKILL backstop fires before the daemon reports | counts + journal | none | pending |
| `PKG-07` | yes | **live** — Shell | `journalctl --user -u smd` and the log file location per the manual | Diagnostics readable; each error names its subsystem; no secrets | Errors only in a file the manual does not name | excerpt | none | pending |
| `PKG-08` | yes | **nspawn** — scratch copies of a synthetic config and database | Provoke a malformed-JSON config (distinct from the unknown-key preflight A1-04) and a database error (read-only database file) | Each refusal is a named, actionable diagnostic; exit without touching the container's real data | Generic "failed to start" | log | none | pending |
| `PKG-09` | yes | **nspawn** — Scratch working directory only, if permitted at A4 | Disk-full / write-refused simulation | Refusal named; no partial write; recovery documented | Silent partial write | log + file state | none | pending |
| `PKG-10a` | yes | **nspawn** — a NEW dedicated rollback user (`smroll`) with an empty private data directory, created after A2-07 (whose uninstall deliberately leaves the A2 user's schema-8 data in place, `install.md` §uninstall) — or a pristine container snapshot restored first; the reconstructed alpha.1 RPM and the frozen candidate RPM copied in and SHA-verified | Actual alpha.1 install → log one synthetic QSO → candidate upgrade per `install.md` §7 → confirm schema 8 → `systemctl --user stop smd`, verify inactive → actual `rpm -Uvh --oldpackage` downgrade → `daemon-reload` + start → alpha.1 refuses (record) → verify the unit is inactive and no `smd` process remains → guarded down-migration (Appendix 3) → start | alpha.1 runs after the downgrade with the synthetic QSO present; unit replaced and reloaded each way; the service was provably stopped before every database step; timed | Alpha.1 installed over schema-8 data from A2; SQL run while a daemon still held the database; downgrade "succeeds" but the candidate is still serving | transcript + counts + timings | none | pending |
| `PKG-10b` | yes | **host-scratch** — a restore of the A3 archive under a scratch root; binaries extracted, never installed; configuration rebased and contained (Appendix 3, containment audit passed); live paths/timestamps/counts recorded before | Candidate binary migrates the copy to 8 → alpha.1 binary refuses → guarded down-migration → alpha.1 binary starts with equal QSO counts; then the workdir-restore variant with ADIF export/re-import of a candidate-era QSO; live invariants re-checked after | Refusal reproduced; both variants preserve the QSOs; live working directory provably untouched | Any live path in the scratch config; a live timestamp changed; a QSO count differing | audit output + transcript + before/after invariants | none | pending |

#### Configured integrations (rows per enabled integration listed at A4)

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `INTEG-01a` | yes | **live** — Hamnut country/prefix lookup | Enter a legitimate callsign | Country/zones fill; Hamnut credited as the source | Wrong provider credited | screenshot | none | pending |
| `INTEG-01b` | yes | **live** — QRZ callsign lookup | Enter a legitimate callsign | Name/grid/QTH fill; QRZ credited | Fields filled from a stale cache while QRZ is credited | screenshot | none | pending |
| `INTEG-02a` | yes | **nspawn** — Hamnut endpoint pointed at an unreachable synthetic address | Enter a callsign | Logging stays usable; status names the unavailable provider | Outage blocks QSO submit | screenshot | none | pending |
| `INTEG-02b` | yes | **nspawn** — QRZ with one synthetic bad credential (A4: at most one rejection) then an unreachable endpoint | Enter a callsign twice | Rejected credential named once; unavailability named; logging usable both times | Rejection masquerades as "no data" | screenshots | none | pending |
| `INTEG-03a` | yes | **live** — ClubLog, the next genuine contact | Log it | Status truthful; ClubLog shows exactly one record | Duplicate upstream record | upstream view | none | pending |
| `INTEG-03b` | yes | **live** — QRZ logbook, the same contact | Observe | One record upstream | Duplicate | upstream view | none | pending |
| `INTEG-03c` | yes | **live** — SM Cloud, the same contact | Observe | One record upstream; reconcile summary consistent | Duplicate or `version_conflict` | status view + cloud | none | pending |
| `INTEG-04a` | yes | **nspawn** — ClubLog with one synthetic bad credential, then unreachable | Forward a synthetic QSO; retry | Named status each time; one retry per policy; no second upstream record | Retry creates a second record | status + synthetic endpoint log | none | pending |
| `INTEG-04b` | yes | **nspawn** — QRZ, as 04a | As 04a | As 04a | As 04a | status | none | pending |
| `INTEG-04c` | yes | **nspawn** — SM Cloud pointed at a synthetic endpoint (unreachable, then rejecting) | As 04a | As 04a; the row returns to `pending`/`failed` truthfully | A failed row shown as uploaded | status | none | pending |
| `INTEG-05` | yes | **live** — SM Cloud | Reconcile after upgrade (A5-03) and a normal sync | Summary shows discovered/skipped/deferred; no `version_conflict` | Rows failing terminally after upgrade | status view | none | pending |
| `INTEG-06` | yes | **live** — once: Session email | Send | Delivered once | Sent twice after an unknown outcome retry | inbox | none | pending |
| `INTEG-07` | yes | **live** — PSK Reporter path | Observe reports during capture | Reported once per decode window; none for suppressed slots | Reports from a mixed slot | PSK page | none | pending |
| `INTEG-08a` | yes | **live** — the next genuine contact (shared with PHONE-06) | Submit | The QSO and its forwarder queue rows appear together | QSO without queue rows | queue counts | none | pending |
| `INTEG-08b` | yes | **nspawn** — synthetic QSO with a forwarder configured against a synthetic endpoint | Submit | The QSO and its queue rows appear together | QSO without queue rows | counts | none | pending |
| `INTEG-08c` | yes | **nspawn** — as 08b, with the container's database made read-only mid-transaction (only if the operator permits this failure injection at A4) | Submit | Neither the QSO nor its queue rows are written; the refusal is named | A QSO row without queue rows (partial write) | counts + log | none | pending |

#### Documentation and support journey

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `DOCS-01` | yes | **nspawn** — Clean-install operator (A2-06) | Follow the guide to a first loggable QSO | Reachable; gaps noted | Guide references the retired config SPA | notes | none | pending |
| `DOCS-02` | yes | **live** — No internet | Open the embedded manual; compare labels/navigation/setup/rig/FT8/forwarding/import/shortcuts/troubleshooting/file locations | Matches the app | Manual still describes `/app/config` or the old SPAs | notes | none | pending |
| `DOCS-03` | yes | **live** — Any surface | Read help text, validation, alarms, warnings, recovery instructions | Each names the action the operator can take | "Check the rig" where the daemon, not the rig, reports state | notes | none | pending |

### B3 — hardware, rig-command and RF (separately approved groups; never implied by passive rows)

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `HW-01` | yes | **live** — IC-7300 pre-configured per `install.md` (USB SEND = OFF, CI-V Transceive on, linked USB/REMOTE CI-V, matching baud); rig **powered, no antenna/RF path decision yet** | Passive observation: enumeration, connection identity, pushed rig state, receive audio, unplug/replug reconnect, failure communication | Identity and pushed freq/mode/VFO shown; reconnect recovers; faults named | Bridge shows "connected" from a cached frame after unplug | screenshots + log | hardware | pending |
| `HW-02` | yes | **live** — HW-01 passed; rig on a dummy load or antenna as agreed; TX inhibited | Rig commands without intended RF: frequency, mode, VFO select/swap, band; a command with the daemon paused | Rig follows; pushed state confirms; timed-out command reports unknown then reconciles (F-04) — **no PTT** | Optimistic UI shows the new state while the rig never moved | rig display + screenshots | rig command | pending |
| `HW-03a` | yes | **live** — HW-02 passed; power, load, duration, abort action and observer agreed in writing | Tune start, then stop | Carrier keys only on start; stop unkeys; observer confirms physical unkey | UI unkeyed while the rig stays keyed | observer log | RF | pending |
| `HW-03b` | yes | **live** — as 03a | Tune start and let the hard auto-off expire | Auto-off unkeys at the documented limit; observer confirms | Carrier persists past the limit | observer log + timestamp | RF | pending |
| `HW-03c` | yes | **live** — as 03a | Tune start, then request FT8 arm/second tune from another tab | Single-flight: the second request is refused, the first carrier is unaffected | Two owners, or the refusal drops the first carrier | observer log + screenshot | RF | pending |
| `HW-03d` | yes | **live** — as 03a | Tune start, then retune (change frequency) | Retune stops the carrier first; no key on the new frequency without a new start | Carrier follows the QSY | observer log | RF | pending |
| `HW-03e` | yes | **live** — as 03a | Tune start, then disconnect CAT | Carrier drops on disconnect (release-on-disconnect); observer confirms physical unkey | Rig stays keyed with CAT gone | observer log | RF | pending |
| `HW-03f` | yes | **live** — as 03a | Tune start, then stop the daemon | Carrier drops during shutdown before the process exits; observer confirms | Daemon exits with the rig keyed | observer log + journal | RF | pending |
| `HW-04` | yes | **live** — HW-03 passed; band, power, duration, abort action, expected exchange agreed | FT8 transmit: operator opens subscription, initiates, chooses CQ/station, controls arming; abort once | Exchange proceeds as expected; disarm stops RF; decode ≠ QSO; only a completed exchange logs | A decode or attempted exchange logged as a QSO | screenshots + logbook | RF | pending |
| `HW-05` | no | **live** — A real nonstandard/compound station present | W-0002 type-4 validation | Evidence goes to the W-0002 dossier only | Treating a run of this gate as closing W-0002 | dossier | RF | pending |

## Appendix 1 — A5 read-only upload-queue inventory (prepared, NOT executed)

Run under the **old** daemon (still installed), before B1. It only reads; it changes nothing.

1. The working directory is pinned by the packaged unit: `/usr/lib/systemd/user/smd.service`
   sets `Environment=SM_WORKING_DIR=%h/.local/share/station-manager`, so on this station it is
   `~/.local/share/station-manager` (present). Without that override the daemon would resolve
   `${XDG_DATA_HOME:-$HOME/.local/share}/station-manager` for a system-path binary
   (`internal/utils/working_dir.go`). Confirm with `systemctl --user show smd -p Environment`.
2. Locate the log database: `datastore.path` in `<workdir>/config.json` if set, else the default
   `<workdir>/db/station-manager.db` (`internal/config/config.go:1336`). Read only that one field:
   `jq -r '.datastore.path // empty' <workdir>/config.json` — do not print the rest of the file (it
   holds credentials).
3. Inventory, read-only. The `sqlite3` CLI is **not installed** on this station; Python's
   standard-library `sqlite3` module is (SQLite 3.51.2), and a `mode=ro` URI opens the file
   without write intent. Readers do not block a running WAL writer, so the old daemon may stay up.

   ```sh
   DB=<path from step 2>
   python3 - "$DB" <<'PY'
   import sqlite3, sys
   con = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
   # SM Cloud rows that are not uploaded, by forwarder / status / action
   print("forwarder_name | status | action | rows | max_attempts")
   for r in con.execute("""
       SELECT forwarder_name, status, action, COUNT(*), MAX(attempts)
       FROM qso_upload
       WHERE forwarder_type = 'smcloud' AND status <> 'uploaded'
       GROUP BY forwarder_name, status, action
       ORDER BY forwarder_name, status, action"""):
       print(" | ".join(map(str, r)))
   # The failed rows individually (A5-02 resolves each): ids and attempt counts only
   print("--- failed: id | qso_id | action | attempts | last_attempt_at | next_attempt_at")
   for r in con.execute("""
       SELECT id, qso_id, action, attempts, last_attempt_at, next_attempt_at
       FROM qso_upload
       WHERE forwarder_type = 'smcloud' AND status = 'failed'
       ORDER BY id"""):
       print(" | ".join(map(str, r)))
   con.close()
   PY
   ```

   Alternative, if you prefer the CLI: `sudo dnf install sqlite` (package `sqlite-3.51.2` is in
   the updates repo), then `sqlite3 -readonly "$DB"` with the same two `SELECT`s.

   Schema: `qso_upload(forwarder_name, forwarder_type, action ∈ insert|update|delete,
   status ∈ pending|in_progress|uploaded|failed, attempts, last_attempt_at, next_attempt_at,
   last_error, upstream_id)` — `internal/database/sqlite/migrations/log/0002…0007`. The SM Cloud
   forwarder's `forwarder_type` is the registered id `smcloud` (`internal/config/config.go:1822`);
   `forwarder_name` is the durable key the UNIQUE constraint is built on.
4. Record here **only** the counts, ids and attempt numbers. If a `last_error` must be consulted to
   resolve a failed row, read it in the terminal and summarise the cause; never paste it (it can
   carry upstream response text).
5. A5-02: let `pending` / `in_progress` drain under the old daemon (re-run step 3 until the first
   query returns no rows); resolve each `failed` row by cause. Do **not** use the candidate's new
   Clear-queue action for this — clearing is not draining.
6. A5-03 (after B1): the candidate exposes per-forwarder queue counts in Settings → Forwarding and
   via the forwarder queue-count endpoint (`88dd7a3b`); confirm no `version_conflict` failures
   appear for SM Cloud. The old daemon predates that surface, which is why step 3 goes to the
   database directly.

Requires `python3` (with its standard `sqlite3` module) and `jq` — both present on this host
(checked 2026-09-05); the `sqlite3` CLI is optional.

## Appendix 2 — A1-04 / B1-05 live-config preflight (prepared, NOT executed)

Purpose: prove the candidate refuses a `config.json` with an unknown key **and** that the live
config is clean, without starting a daemon, installing the RPM, or touching the live working
directory. The candidate ships `smd config-check [--config <path>]`, a read-only preflight
(`cmd/smd/config_check.go`): a clean file exits 0; unknown keys (or a malformed / newer-version
file) exit non-zero with the key *paths* only — values omitted — and nothing is migrated, written,
bound or started. This is the isolation: the daemon never runs.

1. Scratch area under your home, not `/tmp`, because a copy of the live config holds credentials:
   `S=~/sm-b1-05-scratch && mkdir -m 0700 -p "$S" && cd "$S"`.
2. Verify the candidate before using it:
   `sha256sum ~/Development/station-manager/build/private/station-manager-2.0.0~alpha.2-1.x86_64.rpm`
   must print `7b2fa20a247ee0980667fe9b09e02a2a6c458b772c25f40d85e5cf48c7c24856`.
3. Extract **only** the daemon binary from the RPM — no install, no scriptlets:
   `rpm2cpio <that rpm> | cpio -idm ./usr/bin/smd` → `$S/usr/bin/smd`
   (`./usr/bin/smd -h` is the PocketFFT build; it is dynamically linked and was built on this host,
   so it runs here).
4. Copy the live config read-only into the scratch area and tighten it:
   `cp -p <workdir>/config.json "$S/config.live.json" && chmod 0600 "$S/config.live.json"`
   (workdir per Appendix 1 step 1). The live file is never opened for writing.
5. **Control — the live config is clean (the actual go/no-go for B1):**
   `./usr/bin/smd config-check --config "$S/config.live.json"; echo "exit=$?"`
   Expected: exit 0 and `config-check: … — no unrecognised keys; startup would not refuse for
   unknown keys.` If it exits non-zero here, the real upgrade would refuse to start: stop, record
   the named key paths in Findings, fix the live config, re-run. No waiver exists for this result — the candidate would not start.
6. **Top-level unknown key:**
   `jq '. + {"typo_key_b1_05": true}' "$S/config.live.json" > "$S/config.typo.json"`
   `./usr/bin/smd config-check --config "$S/config.typo.json"; echo "exit=$?"`
   Expected: non-zero exit; message `… has 1 unrecognised configuration key(s) — startup would
   refuse (values omitted):` followed by `typo_key_b1_05`; no value printed.
7. **Nested unknown key** (covers `24febadc`, unknown keys inside struct-valued maps):
   `jq '.bridge += {"typo_nested_b1_05": 1}' "$S/config.live.json" > "$S/config.typo2.json"`
   and run the same check. Expected: non-zero exit naming `bridge.typo_nested_b1_05`.
8. Isolation evidence, recorded with the row: `systemctl --user is-active smd` unchanged before and
   after; `stat -c %y <workdir>/config.json` unchanged; nothing new under `<workdir>`; the scratch
   area contains only the files created above.
9. Clean up: `shred -u "$S"/config.*.json 2>/dev/null || rm -f "$S"/config.*.json` (they hold
   credentials), then `rm -rf "$S"`.

Not recommended: starting the extracted binary as a daemon against a scratch working directory to
watch the startup refusal itself. It would open scratch databases and try to bind the configured
listener, colliding with the live service; the preflight is the same check (`config.go:592-600`,
"reject unknown keys … before any write") without those side effects. If you still want the
daemon-start form, stop the live service first and change the scratch config's listen address —
and record it as a separate case.

Requires `jq`, `rpm2cpio`, `cpio` (checked present on this host 2026-09-05).

## Appendix 3 — PKG-10a/10b complete rollback rehearsal (prepared, NOT executed)

Why: alpha.1's embedded migrations stop at 7 and golang-migrate's `readUp` checks that the database's
current version exists in the source (`versionExists`, migrate.go:532), so once the candidate has
migrated the log database to 8, downgrading the package alone leaves a daemon that refuses to start.
`0008` is purely additive (`operator_event` table, `idx_operator_event_category_id`,
`trg_operator_event_no_update`; no QSO table touched), so the down path preserves every post-upgrade
QSO. The rehearsal has two lanes; neither touches the live working directory or the live package.

### PKG-10a — package path, inside nspawn, synthetic data only

1. After A2-07 the A2 user's data directory still holds schema-8 data (the uninstall leaves it in
   place by design), so alpha.1 would meet version 8 immediately. Create a NEW dedicated user `smroll`
   with an empty private home (or restore a pristine container snapshot) and do everything below as
   that user. Copy in and SHA-verify the reconstructed alpha.1 RPM (A3-04) and the frozen candidate RPM.
   Install alpha.1 per `install.md`, complete first-run with a synthetic callsign, log one synthetic QSO. Record `schema_migrations_log` = `(7, 0)` and the QSO count.
2. Upgrade per `install.md` §7 (`sudo dnf install <candidate>` → `daemon-reload` → `restart`). Confirm
   `/v1/version` = `2.0.0-alpha.2`, the migration to 8 in the startup log, `(8, 0)`, QSO count unchanged.
3. Quiesce, then downgrade: `systemctl --user stop smd`; verify `is-active` = `inactive` and `pgrep -x
   smd` is empty; `sudo rpm -Uvh --oldpackage <reconstructed-alpha.1.rpm>`; `systemctl --user
   daemon-reload`; `systemctl --user start smd`. Expected: alpha.1 refuses to start naming the missing
   migration version — record the exact message and that the unit was replaced.
4. Verify the unit is `inactive` (or `failed`) and no `smd` process remains — only then apply the guarded
   down-migration below to the container's database, then `systemctl --user start smd`. Expected:
   alpha.1 up, `/v1/version` = alpha.1, `(7, 0)`, the synthetic QSO present. Time steps 3–4 against the
   A4 window.

### PKG-10b — database path, host-scratch restore of the A3 archive, binaries never installed

1. Scratch root `SR=~/sm-rollback-scratch` (0700). Extract the A3 archive into `$SR/work`.
2. **Containment (hard prerequisite).** The restored `config.json` carries the live station's absolute
   paths, and the daemon defaults `data_dir` / `datastore.path` only when they are blank
   (`internal/config/config.go:1228`, `:1333`) — `SM_WORKING_DIR` does not override a stored absolute
   path, so an un-rebased copy would migrate the **live** database. Edit `$SR/work/config.json`:
   - rebase every path under `$SR/work`: `data_dir`, `datastore.path`, and any other absolute path the
     audit below reports; keep `logging.rel_log_file_dir` relative (it resolves under
     `SM_WORKING_DIR`, which will be `$SR/work`);
   - `socket_path` is the daemon's single listener (`ListenAndServe(cfg.SocketPath)`,
     `cmd/smd/lifecycle_adapters.go:661`): if it is a unix-socket path, rebase it under `$SR/work`; if it
     is a TCP `host:port`, set an unused loopback address such as `127.0.0.1:18080` (check `ss -ltn`) —
     never the live value, so the copy can neither collide with nor be mistaken for the station;
   - disable every side effect: `bridge.enabled=false`, `ft8.enabled=false`, `ft8.decode_log.enabled=false`,
     `evidence.capture=false` and any evidence synchronisation flag, every `forwarders[].enabled=false`,
     the mailer, PSK Reporter, `lookup.hamnut.enabled=false` and every `lookup.chain[].enabled=false`;
3. Audit before either binary runs (read-only; fails closed):

   ```sh
   LIVE_SOCKET=$(jq -r '.socket_path // empty' ~/.local/share/station-manager/config.json)   # one non-secret field
   python3 - "$SR/work/config.json" "$SR/work" "$LIVE_SOCKET" <<'PY'
   import ipaddress, json, os, socket, sys
   cfg = json.load(open(sys.argv[1])); root = os.path.realpath(sys.argv[2]); live_socket = sys.argv[3]
   problems = []
   def inside(path):
       real = os.path.realpath(path)            # canonical: no '..', no symlink escape
       return os.path.commonpath([root, real]) == root
   def walk(v, path):
       if isinstance(v, dict):
           for k, x in v.items(): walk(x, f"{path}.{k}" if path else k)
       elif isinstance(v, list):
           for i, x in enumerate(v): walk(x, f"{path}[{i}]")
       elif isinstance(v, str) and v.startswith('/') and not inside(v):
           problems.append(f"PATH OUTSIDE SCRATCH: {path} = {v}")
       elif v is True and path.rsplit('.', 1)[-1] in ('enabled', 'capture', 'sync', 'allow_insecure_http'):
           problems.append(f"SIDE-EFFECT FLAG STILL TRUE: {path}")
   walk(cfg, '')
   for key, val in (('data_dir', cfg.get('data_dir', '')), ('datastore.path', cfg.get('datastore', {}).get('path', ''))):
       if not val or not inside(val): problems.append(f"{key} not under the scratch root: {val!r}")
   sp = cfg.get('socket_path', '')
   if sp == live_socket: problems.append(f"socket_path equals the live value: {sp!r}")
   if sp.startswith('/'):
       if not inside(sp): problems.append(f"unix socket_path outside scratch: {sp!r}")
   else:
       host, _, port = sp.rpartition(':')
       try:
           ok_host = host in ('localhost',) or ipaddress.ip_address(host.strip('[]')).is_loopback
       except ValueError:
           ok_host = False
       if not ok_host: problems.append(f"socket_path host is not loopback: {sp!r}")
       try:
           with socket.socket() as s: s.bind((host.strip('[]'), int(port)))   # must be free right now
       except OSError as e:
           problems.append(f"socket_path port not free: {sp!r} ({e})")
   for pr in problems: print(pr)
   if problems:
       print("containment audit FAILED"); sys.exit(1)
   print("containment audit OK")
   PY
   <candidate smd> config-check --config "$SR/work/config.json"   # must exit 0
   ```

   (Explicit checks and a non-zero exit, not `assert`, so `python -O` cannot silence them. `setup_complete`
   is not a side-effect flag and is not listed. If the audit prints any problem, fix the copy and re-run;
   never proceed on a failing audit.)
4. Live invariants, recorded before and re-checked after: `stat -c '%y %s'` of the live `config.json`,
   `db/station-manager.db` (and its `-wal`/`-shm` if present), and `reference.db`; `systemctl --user
   is-active smd` (must stay inactive); the A3-03 counts on the live database via Appendix 1's read-only
   pattern. Any change = hard stop.
5. Binaries: the candidate `smd` (Appendix 2 step 3) and the alpha.1 `smd` extracted from the
   reconstructed RPM with `rpm2cpio | cpio -idm ./usr/bin/smd` into `$SR/bin-alpha1/` and `$SR/bin-alpha2/`.
6. Baseline on the copy: `qso` rows per logbook; `schema_migrations_log` = `(7, 0)`.
7. Upgrade the copy: `SM_WORKING_DIR="$SR/work" "$SR/bin-alpha2/usr/bin/smd" --config "$SR/work/config.json"`
   → migration to 8 in the log. While it is still running, log one synthetic QSO through the copy's UI
   at its scratch `socket_path` (needed for steps 10–11). Then stop it (SIGTERM), confirm no `smd` process
   remains, and read `(8, 0)`.
8. Reproduce the blocker: start the alpha.1 binary the same way → refusal naming the missing version;
   record it, and confirm the process has exited (`pgrep -f bin-alpha1` empty) before step 9.
9. Guarded down-migration — one transaction, explicit checks before commit (each raises
   `RuntimeError`; never `assert`, which `python -O` disables), migration SQL pinned inline (the text
   of `0008_operator_event.down.sql` at `v2.0.0-alpha.2`, not read from the working tree):

   ```sh
   python3 - "$SR/work/db/station-manager.db" <<'PY'
   import sqlite3, sys
   DOWN = ["DROP TRIGGER IF EXISTS trg_operator_event_no_update",
           "DROP INDEX IF EXISTS idx_operator_event_category_id",
           "DROP TABLE IF EXISTS operator_event"]           # 0008_operator_event.down.sql @ v2.0.0-alpha.2
   OBJS = ('operator_event', 'idx_operator_event_category_id', 'trg_operator_event_no_update')
   con = sqlite3.connect(sys.argv[1], isolation_level=None)
   def check(cond, msg):
       if not cond: raise RuntimeError(msg)
   before = con.execute("SELECT COUNT(*) FROM qso").fetchone()[0]
   con.execute("BEGIN IMMEDIATE")
   try:
       check(con.execute("SELECT version, dirty FROM schema_migrations_log").fetchall() == [(8, 0)], "tracker not at (8,0)")
       for s in DOWN: con.execute(s)
       n = con.execute("UPDATE schema_migrations_log SET version = 7, dirty = 0 WHERE version = 8 AND dirty = 0").rowcount
       check(n == 1, f"tracker rows updated: {n}")
       check(con.execute("SELECT version, dirty FROM schema_migrations_log").fetchall() == [(7, 0)], "tracker not (7,0)")
       left = [r[0] for r in con.execute("SELECT name FROM sqlite_master WHERE name IN (?,?,?)", OBJS)]
       check(not left, f"objects still present: {left}")
       check(con.execute("SELECT COUNT(*) FROM qso").fetchone()[0] == before, "QSO count changed")
       con.execute("COMMIT"); print("down OK: (7,0); qso rows", before)
   except Exception as e:
       con.execute("ROLLBACK"); print("ROLLED BACK:", e); sys.exit(1)
   PY
   ```

   (SQLite DDL is transactional: a failed check raises, the `except` rolls back, and the copy stays
   exactly at `(8, 0)`. Adjust the database path if the copy's `datastore.path` differs. The same script,
   run inside the container, is PKG-10a step 4.)
10. Start the alpha.1 binary against the copy → starts; `(7, 0)`; `qso` counts = step 6 plus the
    step-7 QSO — the "post-upgrade QSOs preserved" proof. Stop.
11. Restore variant: on a second scratch copy (`$SR/work2`, containment steps 2–4 applied), repeat step 7
    only up to the point where the candidate is running with the synthetic QSO logged; **while it is still
    running**, export that QSO to ADIF from the copy's UI; then stop it and verify quiescence (no
    `bin-alpha2` process, `fuser` silent on the copy's `datastore.path`) before replacing `$SR/work2` with a
    fresh extract of the A3 archive (re-apply containment). Start alpha.1 and verify it serves; **stop it**
    (`smd import` requires a stopped daemon — sqlite single-writer, `cmd/smd/import.go`); run
    `SM_WORKING_DIR="$SR/work2" "$SR/bin-alpha1/usr/bin/smd" import --config "$SR/work2/config.json"
    <the ADIF>` **without** the forward option; start alpha.1 again → exactly one copy of the QSO. Stop.
12. Re-check the live invariants (step 4). Time both variants against the A4 window. Clean up:
    `shred -u` every `config*.json` under `$SR` (credentials survive even with everything disabled), then
    `rm -rf "$SR"`.

## Appendix 4 — nspawn provisioning, access and login/logout (prepared, NOT executed)

- Rootfs: `sudo dnf --installroot=/var/lib/machines/fedora44 --releasever=44 --use-host-config group
  install core`, then `sudo dnf --installroot=/var/lib/machines/fedora44 --releasever=44
  --use-host-config install python3 jq` — `--use-host-config` is required because a new installroot has
  no repository configuration of its own. The verified
  local `core` group ("Smallest possible installation": systemd, dnf, curl, iproute, shadow-utils and the
  other essentials) carries no Python, and PKG-10a's guarded migration and the count queries need
  `python3` (`jq` for the container-side config/count commands); plus whatever `install.md` lists as
  prerequisites. No browser inside the container (host Firefox is used). `machinectl` lists it as
  `fedora44`.
  No bind mounts of `$HOME` or the live working directory — ever (a hard stop in the resume contract).
  Create a dedicated user inside (`smtest`) with a private home; that user owns the A2 run.
- Network / reaching the loopback-only listener from host Firefox: `machinectl start` runs the
  `systemd-nspawn@.service` template, whose `ExecStart` passes `--network-veth` — a private network in
  which the daemon's loopback bind is unreachable from the host. Override it in
  `/etc/systemd/nspawn/fedora44.nspawn`:

  ```ini
  [Network]
  VirtualEthernet=no
  ```

  (the template runs with `--settings=override`, so the file wins). The container then shares the
  host network namespace and the daemon's `http://127.0.0.1:8080` is the host's. Verify **before**
  installing the candidate: inside the container `ip link` shows the host's interfaces and no `host0`;
  after the daemon starts, `curl -sI http://127.0.0.1:8080/` from the host answers. Preconditions: the
  live `smd` stays stopped throughout A2 (it is inactive now; B1 comes after A2) and nothing else
  listens on 8080 (`ss -ltn`). A private network would need an in-container forwarder such as `socat`,
  because port-forwarding cannot reach a `127.0.0.1` listener — do not use it.
- Boot and sessions: `sudo machinectl start fedora44`; `machinectl shell smtest@fedora44` is the
  operator's "login". The install guide's user-service steps (`systemctl --user enable --now smd`,
  `loginctl enable-linger`) run inside that shell.
- Logout/login and boot behaviour (A2-01 lingering, PKG-04): exit the `machinectl shell` (logout) and
  from the host run `systemctl --user -M smtest@fedora44 is-active smd` — with linger the daemon
  survives the logout; then `sudo machinectl reboot fedora44` and repeat after the container's
  boot — the daemon must be up without a manual start. Without linger the first check must show it
  stopped, which is the documented failure the guide warns about.
- Clean baseline before the candidate: record `rpm -qa | wc -l`, the absence of any
  `station-manager` package, and an empty `/home/smtest/.local/share/station-manager` before
  transferring the frozen RPM in (`machinectl copy-to fedora44 <rpm> /home/smtest/`), then verify its
  SHA-256 inside the container before `sudo dnf install`.

## Execution log — Gate A (ratified 2026-09-05 through A4-01; no Gate B, no hardware/RF)

Command-line checkpoints run by the coder under the resume contract; the operator rules every result.
Times UTC.

1. **11:10Z — step 1, identity recheck (read-only).** HEAD `b8e0356f` = tag `v2.0.0-alpha.2^{}`; tag not on
   origin; 5 commits ahead; both RPM copies SHA-256 `7b2fa20a…c24856`, 9,053,968 bytes; installed
   `station-manager-2.0.0~alpha.1.1238.gd9cd38ae.dirty-1`; `smd` inactive, no process; worktree = the
   uncommitted capsule + this record only (hence `git describe … -dirty`; the artifact was built from a
   clean tree, header). No identity change → continue.
2. **11:10Z — step 2 (read-only half), `rpm -V station-manager`.** Exit 0, no output: the installed alpha.1
   payload is unmodified. `rpmrebuild` install and reconstruction wait for the operator's `sudo`.
3. **11:14Z — step 3 (local half), A3-01 archive.** Quiescence verified (`smd` inactive, no process,
   `fuser` silent on the resolved `datastore.path` `~/.local/share/station-manager/db/station-manager.db`,
   WAL present). `tar --numeric-owner -czp` of the whole working directory (`config.json`,
   `config.json.pre-evidence`, `db/`, `evidence.db`, `exports/`, `log/`) →
   `~/sm-backup/station-manager-pre-alpha2-20260905T1114Z.tar.gz`, 18,185,501 bytes, 0600,
   SHA-256 `066f769b38914ba8fed3cc31f7082d5b9f76ae6d3e169390dccab2f0cd0503b9`, 67 entries. Live files'
   mtimes unchanged. At 11:48Z the operator reported the prescribed SCP transfer to the private SMC
   LAN host (exact endpoint retained outside the repository) and remote SHA-256
   `066f769b38914ba8fed3cc31f7082d5b9f76ae6d3e169390dccab2f0cd0503b9`, an exact match. If the live
   daemon runs before B1, repeat A3-01 and A3-03.
4. **11:15Z — step 3b, A1-04 live-config preflight (Appendix 2).** Candidate SHA verified; `smd` extracted
   from the RPM (not installed); control run on a 0600 copy of the live config → **exit 0, "no
   unrecognised keys"** (the upgrade will not refuse on unknown keys); top-level `typo_key_b1_05` → exit 1
   naming only the key path; nested `bridge.typo_nested_b1_05` → exit 1 naming only the key path; live
   `config.json` mtime unchanged; `smd` inactive before and after; scratch copies shredded, directory
   removed.
5. **11:18Z — step 3, A3-02 restore-check.** Archive extracted into a 0700 scratch directory (68 entries).
   The restored `config.json` carries the live station's absolute `datastore.path` — the containment
   hazard PKG-10b's audit exists for, confirmed empirically — so the restored copy's own `db/` file was
   opened, read-only (`mode=ro`): `schema_migrations_log` `(7, 0)`; `PRAGMA integrity_check` ok; `qso`
   7,468 rows, all `deleted_at` NULL, all logbook 1; `qso_upload` 13,089 `uploaded`, 1 `failed` whose
   `forwarder_type` is `qrz` (LOG-09a's target); `operator_event` absent (schema 7). All figures equal
   the recorded A3-03 counts. The live WAL was fully checkpointed at archive time (0 bytes). Scratch
   config copies shredded, directory removed; live `config.json` mtime unchanged; `smd` inactive.
6. **11:40Z — step 2, reconstructed alpha.1 rollback RPM.** After the operator installed
   `rpmrebuild-2.21-2.fc44`, `rpmrebuild --batch --notest-install` ran as the ordinary user with its
   temporary build tree under `/tmp`; it did not install the result. Output:
   `build/private/x86_64/station-manager-2.0.0~alpha.1.1238.gd9cd38ae.dirty-1.x86_64.rpm`, 7,359,884
   bytes, mode 0600, SHA-256 `c4fd092f329980c32283e773e0311ab07e7ae5f621ff177362b88e9ca174a807`;
   `rpm -K` reports `digests OK`. NVR, epoch and architecture match the installed package exactly.
   Every payload path's size, mtime, SHA-256, mode, RPM owner and RPM group match; package scripts,
   triggers and file capabilities also match. Expected reconstruction-only header differences:
   modern `rpmbuild` added five `rpmlib(...)` feature requirements and the architecture-qualified
   provide, marked the two manual files `%doc`, and populated file classification metadata; it added
   no runtime-library requirement and changed no payload byte or executable metadata. The sandbox
   projects these installed root-owned files as `nobody:nobody`, producing `rpmrebuild`'s `UG` warning;
   the earlier host `rpm -V` was clean, sandbox `rpm -V --nouser --nogroup` is clean, and the rebuilt
   header correctly retains `root:root`. The optional final `rpm --test` was skipped because the
   sandbox falsely reported insufficient `/` space; PKG-10a is the isolated real install/downgrade proof.
7. **11:48Z — identity-change disposition.** During step 2, `main` and `origin/main` advanced from the
   frozen candidate commit to intentional docs-only commit `564909e8` (`docs: add the alpha.2 dogfood
   acceptance record and refresh the capsule`). The operator confirmed the commit was intentional;
   candidate tag `v2.0.0-alpha.2^{}` and both frozen RPMs remain pinned to `b8e0356f`/the recorded SHA.
8. **12:49Z — step 4, nspawn rootfs bootstrap.** The first `dnf --installroot` invocation stopped before
   a transaction because the empty installroot supplied no repositories. The diagnostic required
   `--use-host-config`; the operator reran the `core` group and `python3 jq` installs with that flag.
   `machinectl list-images` now reports one writable directory image, `fedora44`, created at 13:52 CAT.
   `/var/lib/machines` is mode 0700, so utility/package verification waits until the first container
   shell. Live `smd` is inactive, no `smd` process exists, and host port 8080 is free. The
   `/etc/systemd/nspawn/fedora44.nspawn` override is not yet present; do not start the container.
9. **12:54Z — partial nspawn preparation, no candidate execution.** The operator created the
   `VirtualEthernet=no` override; it was verified `root:root`, 0644, and the template's
   `--settings=override` made it effective. `fedora44` booted Fedora 44 with `python3` and `jq`; it saw
   the host interfaces and no `host0`, while live `smd` remained inactive and port 8080 remained free.
   Dedicated user `smtest` had a 0700 home; baseline was 368 packages, no `station-manager` package and
   no Station Manager working directory. `machinectl copy-to` was denied across the private-user
   boundary, so the frozen RPM was copied once through `systemd-run --machine --pipe` (no bind mount):
   9,053,968 bytes, 0644, `smtest:smtest`, SHA-256 `7b2fa20a…c24856` inside the container. Guest account
   authentication then became test-harness work unrelated to Station Manager. The candidate RPM was
   never installed or run.
10. **13:34Z — operator scope reduction and nspawn retirement.** The operator accepted rebuild plus
    working-directory restore or ADIF import as proportionate recovery for this personal dogfood host,
    including possible metadata loss for ADIF-only recovery. The nspawn/failure-injection suite and
    PKG-10a/10b were closed NOT EXECUTED, not passed. `fedora44` powered off cleanly; image deletion and
    override removal require the operator's host `sudo`. Live alpha.1 remained installed and inactive;
    no live data, candidate package, hardware or RF was touched. Deployment now waits for the
    operator's fresh local stopped-daemon backup.

## Findings

Record surprises in [`dogfood-inbox.md`](../dogfood-inbox.md) as they happen; triage each here as
defect / planned follow-up / duplicate / working-as-designed / documentation correction, and route
durable work through the backlog.

| # | Case | Observation (sanitized) | Triage | Destination |
|---|---|---|---|---|
| — | — | — | — | — |
