# W-0024 acceptance — Set as default (revision 3, for ratification)

Status (2026-11-03): **ACCEPTANCE INCOMPLETE — A3 ON HOLD.** Superseded baseline: the candidate,
station, rollback and sidecar facts below describe 30 October; **[revision 4](#revision-4--reconciled-with-the-installed-artifact-2026-11-03-for-ratification)
reconciles the run sheet with the artifact now installed.** Original status line:
**ACCEPTANCE INCOMPLETE — A3 ON HOLD. Deployment is not approved.** F4 and F5 are to be fixed (reviewer, 2026-10-30), followed by a new artifact and the affected reruns. Revision 1's no-RF scope was approved with
amendments (operator, 2026-10-30); revisions 2 and 3 incorporate them. Deployment approval follows
this run sheet's ratification, the exact artifact evidence, the clean-install result and the
verified backup ([gate](../dogfood-acceptance.md), Gate A). **Full operator acceptance is a separate
decision** from this run's results.

- **Candidate:** commit `43d0ed98` (`v2.0.0-alpha.5-47-g43d0ed98`): W-0024 slices 1–5c, Set as
  default released in `46a07d07`. CI run 41012359480 green on `43d0ed98`. Not a release tag.
- **Station now:** `v2.0.0-alpha.5` (`0da2cd6a`), archive Home, Default logbook `Default`.
- **Rollback RPM:** `build/private/station-manager-2.0.0~alpha.5-1.x86_64.rpm`, SHA-256
  `3c10b6e1a7899807612db7720a0b95fe21badbb7cf21bbb8080265cd11693a9c` (verified 2026-10-30).
- **No RF.** The rig stays off, the FT8 view stays closed and TX unarmed throughout. Contacts are
  entered by hand on Phone / CW; nothing is transmitted.
- **Evidence:** the operator's UI observations and screenshots; read-only `GET` calls; the journal;
  database checks only on verified, stopped copies (`immutable=1`), never on the live directory.
  Operator data and credentials stay in private directories outside the repository; this record
  holds counts, hashes and outcomes only.

## Schema and rollback facts (verified 2026-10-30)

- No SQL migration between alpha.5 and the candidate; config version 6 in both.
- The candidate adds the config key `default_logbook_change` (ADR 0097) and may leave its record in
  `config.json` after a change. **alpha.5 refuses to start on an unrecognised config key** (ADR
  0074; `reject_unknown_keys`). A package-only downgrade is not a safe rollback.
- The default is the archive's own pointer, in Home's database.
- **Rollback = restore the pre-install backup + the alpha.5 RPM.** The same restore, with the
  candidate kept, removes every synthetic record afterwards.

## Paths, ownership and permissions (resolved 2026-10-30)

| | Value |
|---|---|
| Live working directory `WD` | `/home/mveary/.local/share/station-manager` — `mveary:mveary`, mode 0755; `config.json` 0600 |
| Contents | `config.json`, `config.json.pre-evidence`, `config.v5.json`, `db/`, `exports/`, `log/`, `pskreporter.id`; 183 MB |
| Databases | Home `db/station-manager.db`; Drill `db/qso-archives/01a15d43-6f90-7c26-82aa-828dda4bdebb.db`; `db/reference.db`; `db/evidence.db`; never opened: `db/reference-premigrate-20260723-024757.db` and the dated `db/20260722/`, `db/20260723/` |
| Known pre-existing sidecar pair (allowed after a clean stop, byte-identical only) | `db/reference-premigrate-20260723-024757.db-wal` (0 B, SHA-256 `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`) and `-shm` (32,768 B, SHA-256 `fd4c9fda9cd3f9ae7c962b0ddf37232294d55580e1aa165aa06129b8549389eb`) |
| Backups | `/home/mveary/sm-backup` (0700, existing); archives created 0600 |
| Work directory `W` | `/home/mveary/sm-w0024` (0700, created by the run); never inside the repository |
| Archive layout | one top-level directory `station-manager/`, created from `/home/mveary/.local/share` |
| Helpers (private, git-ignored) | `build/private/c-rows.py` (row snapshots and diffs); `build/private/w0024-check.py` (integrity, counts, upload linkage) — both self-tested on a synthetic database 2026-10-30, including the failing case |

Every command below runs as `mveary` in a shell with `set -euo pipefail`, `umask 077`,
`WD=/home/mveary/.local/share/station-manager`, `W=/home/mveary/sm-w0024`,
`P=/home/mveary/Development/station-manager/build/private`. A failed command stops the run.

## Test fixture and external services

- **Synthetic contacts use the operator's own call `7Q5MLV`** (no invented callsign), clearly
  labelled: comment `W-0024 ACCEPTANCE — synthetic, not a real QSO`.
- **Two contacts, separated by the minute:** the duplicate key has minute precision, so two
  same-call contacts in one minute could correctly produce one row. Contact 1: 20m SSB, Time On
  `HH:MM`; contact 2: 40m SSB, Time On `HH:MM+1` (both entered explicitly, UTC, today).
- **Test logbook T:** `W-0024 Acceptance`, callsign `7Q5MLV`, description `W-0024 acceptance test —
  synthetic, not a real log`, **SM Cloud unticked**. Before any contact is logged, Settings →
  Forwarding shows no enabled destination for T, and `GET /v1/qso-archives/{Home}/bindings` lists T
  with every destination `enabled: false`.
- **Other-call logbook P:** `W-0024 Other call`, callsign `7Q5MLV/P`, empty; only to see Set as
  default disabled with its reason; deleted afterwards.
- **External lookups disabled from step 16** — after the candidate is installed and started, not
  from the install itself: hamnut (the country-prefix source) and every callsign provider
  (`cmd/smd/main.go` builds each only when `enabled`). The change is **restart-required** (config
  reference §11.2: lookup construction applies after a daemon restart), so it is saved and followed
  by a restart, and verified in `GET /v1/config` (`lookup.hamnut.enabled` and each
  `lookup.chain[].enabled` false) **before any synthetic callsign is typed** anywhere. Between
  steps 15 and 16 the candidate runs with the station's existing lookup settings, as alpha.5 does
  now; nothing synthetic is entered in that interval.
  The names of the providers enabled beforehand are recorded (names only).
  **Restoring them:** the post-test backup restore (below) returns `config.json` to its pre-test
  state, and the original settings are verified in `GET /v1/config` after the restart. If the
  restore is not chosen, the recorded providers are re-enabled in Settings → Enrichment and the
  daemon restarted, then verified the same way.
- **Uploads:** contacts are logged only while T is effective; uploads route by the QSO's logbook
  (ADR 0082) and T has no enabled destination. Proven on the stopped post-test copy (AC-9), not by
  aggregate queue totals.

## Acceptance criteria (accepted with amendments, 2026-10-30)

- **AC-1 Candidate installed, Default unchanged.** The candidate starts on Home with Default
  effective; `/v1/version`, the sidebar build badge, the startup log line and `rpm -q` agree on the
  version; `archive.default_logbook_uuid` is Default's; `GET /v1/default-logbook` shows no change.
- **AC-2 Offer and disabled reason.** Set as default appears on T and P, not on Default; on P it is
  disabled, and its tooltip names the callsign mismatch. Descriptions show under the names.
- **AC-3 The confirmation.** It opens "Set ‘W-0024 Acceptance (7Q5MLV)’ as default and restart
  Station Manager?", names the target and effect, this window's partial Phone / CW entry ("OK
  discards this window’s entry"), this window's unsaved Settings edits, and other windows' unlogged
  work. The SM Cloud note appears only if Default's SM Cloud binding is enabled and not adopted,
  reserved or created on identity (expectation fixed by step B3's reading; recorded either way).
- **AC-4 The confirmation's Cancel sends nothing.** With the browser's Network panel recording in
  window A (filter `default-logbook`), Cancel produces **no POST** (screenshot), the status and
  daemon instance are unchanged, and the partial entry and the Logbooks Add draft survive.
- **AC-5 Switch to T.** OK: the daemon restarts (new instance), every open window reloads by itself,
  and afterwards `/v1/version`, `/v1/default-logbook` (T effective) and the header name T; Settings
  → Logbooks reports the settled outcome (R2).
- **AC-6 Other windows.** Window B (Operate, partial entry typed, never logged) and window C
  (Logbook view) reload by themselves onto T; B's partial entry is gone, as A's confirmation
  warned. No window logs to Default after the switch.
- **AC-7 Manual contacts reach the effective default.** The two synthetic contacts are stored in T:
  T's count is 2 and Default's unchanged in `/v1/logbook/{id}/count`.
- **AC-8 Switch back.** Set as default on Default restores it as effective, by the same restart and
  reloads; the Phone / CW form then names Default; no contact is logged.
- **AC-9 Preserved versus added (on stopped, verified copies).** Comparing the pre-install backup
  copy with the post-test copy by `c-rows.py diff` per database:
  - **expected additions only:** in Home, `qso` +2 rows, both in T's logbook id; `logbook` +2 — T
    active and P with `deleted_at` set (Delete is a soft delete,
    `internal/database/sqlite/api_context.go`, `DeleteLogbookByIDWithContext`); any
    `logbook_destination` rows for T or P all `enabled = 0`; `operator_event` and `qso_history` rows
    the test produced, listed and explained;
  - **preserved:** every pre-existing `qso`, `logbook` and `logbook_destination` row present and
    unchanged, except changes listed and explained (none expected); Drill, `reference.db` and
    `evidence.db` changes listed and explained;
  - **no test upload:** `w0024-check.py uploads` reports zero `qso_upload` rows naming either added
    QSO. Pre-existing `qso_upload` rows may change state through normal worker activity; such
    changes are listed, not required to be zero;
  - the archive pointer names Default again, and `GET /v1/default-logbook` shows no record in play.
- **AC-10 Delete protection.** While T is effective, Delete on T is unavailable; P (empty, not named
  by a change) deletes normally.
- **AC-11 Rollback ready.** The backup is verified (complete-set comparison and integrity) and the
  rollback RPM's full hash matches before the install.

## Recovery states and refusals

**Exercised without fault injection:**

- R1 the **confirmation's Cancel** (AC-4) — distinct from the recovery action *Cancel change*, which
  is not exercised (U1, U2).
- R2 the settled outcome after a normal start: the *“…” is now the default* notice; *Cleanup
  pending* only if the start's write is not reported durable — then **Retry cleanup** is exercised,
  else recorded as not arising.
- R3 the pending banner in window B during the restart — transient; recorded if seen, not required.
- R4 the other-callsign disabled reason (AC-2).

**NOT EXERCISED in this run** — covered by automated tests only. Not passed, and not waived:

- U1 *applies at the next restart* with **Cancel change** (`pending_unclear`): needs a failed restart.
- U2 *not applied at this start, still pending* with **Cancel change**: needs a target removed or
  made ineligible across the restart.
- U3 *not yet confirmed on disk* with **Confirm**, and the power-loss wording: needs an undurable
  config write.
- U4 the unreadable-status banner (**Check again**, **Reload**): needs the status read to fail.
- U5 the FT8 live path following the new default: needs a transmission.
- U6 busy refusals: FT8 armed or running, tune keyed (rig and RF), a Log in flight, a Settings save
  or archive operation in flight, a concurrent request (timing-dependent).
- U7 an unknown request outcome (lost response) and a stale tab's `default_stale`.

## Run sheet (revision 3)

### A1 — freeze, check and build (no outage)

0. Create the work directory once, for the whole run: `mkdir -m 0700 "$W"`, then `stat -c '%a %U'
   "$W"` is `700 mveary`. Later steps create only subdirectories (`mkdir -m 0700 "$W/<name>"`), each
   absent beforehand.
1. In a clean scratch worktree at `43d0ed98` (`git worktree add`, outside the repository's tree):
   `git status` clean; `task ci:local` exit 0, its result recorded.
2. Build once: `SM_FFT=pocketfft scripts/dev-rpm.sh`; record the RPM filename. **Copy it to
   `build/private/` in the main checkout before removing the worktree**, then verify the copy's
   SHA-256 equals the built file's; record the **full** hash. Unpack the copy (`rpm2cpio | cpio -id`
   in `$W/unpack`, created 0700) and record `/usr/bin/smd`'s full SHA-256 and the version it embeds (read with
   `strings`, never by running `smd` with a guessed subcommand). Remove the worktree and
   `$W/unpack`.
3. Release delta (recorded above): no migration; one config key; changed journeys Settings →
   Logbooks and the shell banner; manual section *Changing the Default logbook*.

### A2 — clean installation and fixture rehearsal (isolated guest; no live paths, credentials or hardware)

4. Guest by the [alpha.4 Appendix 3](dogfood-acceptance-v2.0.0-alpha.4.md) method (approved
   2026-10-22), renamed: domain `sm-w0024-clean` on `qemu:///session`, everything under
   `/home/mveary/sm-guest-w0024` (0700); the Fedora 44 Cloud image fetched again and verified
   (`gpgv` on the CHECKSUM file, then `sha256sum -c`) — a failed verification stops here; one user
   `smtest`; SSH on host `127.0.0.1:2222`; browser through `ssh -L 127.0.0.1:18080:127.0.0.1:8080`
   in a throwaway Firefox profile `/home/mveary/sm-guest-w0024/ff-smtest`. Only the candidate RPM is
   copied in (`scp`, SHA-256 verified inside the guest). No shared folders, devices, credentials or
   operator data.
5. Gate A2 by `install.md`: install, user-service enable and start, lingering, health, version, logs,
   browser access; the first-run welcome only; invalid, empty, normalised and valid callsigns;
   setup with `7Q5MLV` creating the default logbook and offering **Open Settings** and **Start
   logging**; survival across reload and daemon restart; Settings, Manual and first QSO
   discoverable; every point needing information absent from the guide or manual recorded.
6. **Fixture rehearsal** in the guest: disable hamnut and every callsign provider, save, restart,
   verify `GET /v1/config` — before any synthetic callsign is typed; create T and P; confirm T has no
   enabled destination; run the journey of steps 18–22 (windows B and C, the confirmation's Cancel,
   switch to T, the two contacts a minute apart, switch back) and confirm both contacts are stored
   and no upload row exists.
   **Lookup evidence:** the guest journal and the contacts' enrichment fields only *corroborate*
   that no lookup ran; their absence does not prove no request. **Direct evidence** in the
   isolated guest: a packet capture of the daemon's outbound connections for the rehearsal's
   duration (`tcpdump` installed in the guest by `dnf`), `sudo tcpdump -i any -nn -w /tmp/rehearsal.pcap 'tcp and (port 80 or port 443) and not host 127.0.0.1'`
   started before the first synthetic callsign is typed and stopped after the last contact is
   logged; then `tcpdump -nn -r /tmp/rehearsal.pcap` lists the connections made (none expected from
   the daemon; any connection is identified by its destination and explained). Possible only in the
   guest — no capture runs on the station host.
7. Gate A2 uninstall: `systemctl --user disable --now smd`; `sudo dnf remove station-manager`; the
   working directory retained as documented. Then dispose of the guest (domain undefined with its
   storage; `/home/mveary/sm-guest-w0024` removed), recorded.

### A3 — readings, backup and verification (agreed outage begins at step 9)

8. **Readings A** (daemon running, read-only): `GET /v1/version`, `/v1/healthz`, `/v1/config`
   (lookup `enabled` flags and provider names only, recorded), `/v1/logbook` and each
   `/v1/logbook/{id}/count`, `/v1/qso-archives`, `/v1/qso-archives/{Home}/bindings` (Default's SM
   Cloud markers fix AC-3's expectation), `/v1/forwarder-queues`.
9. Stop: `systemctl --user stop smd`; record the time; `systemctl --user show smd -p ActiveState -p
   MainPID -p Result` shows `inactive`, `0`, `success`; the journal shows the daemon's own orderly
   exit.
10. Sidecars: `find "$WD/db" -name '*-wal' -o -name '*-shm' -o -name '*-journal'` must list
    **exactly** the known premigrate pair, each byte-identical to the hashes above
    (`sha256sum` compared). **Anything else stops the run:** files preserved exactly as found, the
    daemon left stopped, the operator reviews.
11. Baseline (`$W` exists from step 0): `cd /home/mveary/.local/share`;
    `find station-manager -type f -print0 | sort -z | xargs -0 sha256sum > "$W/baseline.sha256"`;
    `find station-manager -printf '%m %u:%g %y %p\n' | sort > "$W/baseline.modes"`.
12. Backup: `UTC=$(date -u +%Y%m%dT%H%MZ)`;
    `B=/home/mveary/sm-backup/station-manager-$UTC-g0da2cd6a-pre-w0024.tar.gz`;
    `tar -C /home/mveary/.local/share -czf "$B" station-manager`; `stat -c '%a' "$B"` is `600`;
    record `sha256sum "$B"` (full) and `tar -tzf "$B" | wc -l`.
13. Verify: `mkdir -m 0700 "$W/pre"`; `tar -C "$W/pre" -xpzf "$B"`;
    `(cd "$W/pre" && find station-manager -type f -print0 | sort -z | xargs -0 sha256sum) | diff - "$W/baseline.sha256"`
    and `(cd "$W/pre" && find station-manager -printf '%m %u:%g %y %p\n' | sort) | diff - "$W/baseline.modes"`
    both empty. Then `python3 -I "$P/w0024-check.py" integrity` on the four databases' copies under
    `$W/pre/station-manager/db/` must print `ok` for each and exit 0 — **any failure, or a check that
    does not complete, stops the run**. Record `w0024-check.py counts` for the Home and Drill
    copies, and `c-rows.py snap` each of the four copies to `$W/pre-<name>.json`. **AC-11.**
14. Re-hash the live directory against `$W/baseline.sha256`: identical (nothing touched it).

**Deployment decision point.** The operator rules on the evidence of A1–A3. If deployment is not
approved now: `systemctl --user start smd` (alpha.5), and steps 9–14 are repeated in the window
that is approved.

### B — deploy and accept (only after approval; same outage)

15. The operator installs: `sudo dnf install <candidate rpm>` (SHA re-verified first);
    `systemctl --user daemon-reload`; `systemctl --user start smd`. **AC-1** readings; journal
    without `error`.
16. Disable lookups in Settings → Enrichment (hamnut and every enabled provider), save; Settings →
    **Restart daemon**; verify in `GET /v1/config`.
17. Window A, Settings → Logbooks: add T and P as specified. Bindings show nothing enabled for T or
    P. **AC-2.**
18. Window B: Operate → Phone / CW, a partial entry (callsign only), not logged. Window C: Logbook.
19. Window A: a partial Phone / CW entry, then Settings → Logbooks; a name in the Add form, unsaved;
    the Network panel recording; **Set as default** on T; screenshot the confirmation; **Cancel**.
    **AC-3, AC-4, R1.**
20. Window A: clear the Add draft and the entry, or keep them knowing OK discards them. **Set as
    default** on T → **OK**. Watch B (R3). After the reloads: **AC-5, AC-6, R2.**
21. Window B: the two synthetic contacts as specified (minute apart). **AC-7.** Delete on T
    unavailable; delete P. **AC-10.**
22. Window A: **Set as default** on Default → OK. After the reloads: **AC-8.**
23. Post-test snapshot: `systemctl --user stop smd` (clean, as step 9); sidecar check as step 10;
    `tar -C /home/mveary/.local/share -czf /home/mveary/sm-backup/station-manager-<UTC>-g43d0ed98-post-w0024.tar.gz station-manager`
    (0600; full SHA-256 recorded); extract to `$W/post` and compare with a fresh hash list of the
    stopped live directory (as steps 11 and 13); integrity `ok` on the copies; `c-rows.py snap` of
    each copy; `c-rows.py diff` pre → post per database; `w0024-check.py uploads` on Home's pre and
    post snapshots (exit 0 required). **AC-9.**

### C — after the test (operator's choice, below)

24. **Recommended: keep the candidate, remove the synthetic data.** With the daemon stopped after
    step 23: `mv "$WD" "$WD.w0024-post-<UTC>"` (kept, private, 0755 as before); `tar -C
    /home/mveary/.local/share -xpzf "$B"`; **while still stopped**, verify `$WD` against
    `$W/baseline.sha256` and `$W/baseline.modes` (both diffs empty) — byte-identical files mean the
    restored QSOs, bindings and upload queue equal step 13's verified backup counts, and the queue is
    judged here, not after the start (workers move it). Then `systemctl --user start smd` and verify:
    the candidate starts on Home with Default effective; QSO counts, bindings and lookup settings
    equal Readings A; queue rows may move from here on through normal worker activity.
    The moved directory is deleted only on the operator's instruction once accepted.
25. **Rollback instead** (on any failure the operator calls): as step 24, but before the start
    `sudo rpm -Uvh --oldpackage build/private/station-manager-2.0.0~alpha.5-1.x86_64.rpm` (full hash
    re-verified) and `systemctl --user daemon-reload`; then `GET /v1/version` shows alpha.5, Home,
    Default.
26. Cleanup: the credential-bearing copies `$W/pre` and `$W/post` removed with `shred -u` on files,
    then the directories (btrfs: `shred` is not a guarantee on copy-on-write storage — the 0700
    directory is the boundary); the hash lists, snapshots' counts and this record kept; snapshots
    themselves (`*.json`, operator data) shredded with the copies.

## Decisions wanted from the operator

1. **The synthetic callsign and lookup policy:** `7Q5MLV` (the operator's own call), lookups
   disabled from step 16 — after install and start, verified before any synthetic callsign is
   typed — until the restore, every T destination disabled — confirm.
2. **The post-test choice:** step 24 (keep the candidate, restore the pre-test backup — this
   **discards everything written between steps 12 and 23**, including any settings change), or step
   25 (roll back), or keep the synthetic data — confirm.
3. **The outage window** for steps 9–26, and later the deployment decision at the point marked.

## Execution log

Command-line checkpoints run by the agent; the operator rules every result. Times UTC.

1. **2026-10-30 — A1 step 0.** `/home/mveary/sm-w0024` created (`700 mveary`; absent before).
2. **A1 step 1.** Worktree `/home/mveary/sm-w0024-build` (detached at `43d0ed98`, outside the
   repository's tree): `git status --porcelain` empty; `git describe` `v2.0.0-alpha.5-47-g43d0ed98`;
   `scripts/version.sh` → `2.0.0-alpha.5-47-g43d0ed98` / RPM `2.0.0~alpha.5.47.g43d0ed98`.
   `task ci:local`: **exit 0**, "All CI gates passed locally"; 133 Go `ok` lines, none `(cached)`;
   log kept privately at `/home/mveary/sm-w0024/ci-local.log`. As at alpha.3–alpha.5, the CI
   mirror's SPA build left the tracked `frontend/app/dist/index.html` modified (alpha.3 Finding #1);
   restored from `HEAD`, `git status --porcelain` empty again. The build waits for the operator's
   `.env` copy (private key; the agent does not read `.env`).
3. **A2 step 4, guest preparation (agent).** `/home/mveary/sm-guest-w0024` created 0700 (absent
   before). `Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2`, `Fedora-Cloud-44-1.7-x86_64-CHECKSUM`
   and `fedora.gpg` downloaded from Fedora; `gpgv --keyring ./fedora.gpg`: "Good signature from
   Fedora (44)", key `36F612DCF27F7D1A48A835E4DBFCF71C6D9F90A6`; `sha256sum -c --ignore-missing`:
   OK (the 17 "improperly formatted" lines are the signature armour). Image SHA-256
   `28680fe5b371a5a82ebf43a31926e086a168e59949d03969c5093e7071f90b7f` (identical to alpha.4's), made
   read-only. A 20 GiB qcow2 overlay; a guest-only ed25519 key; a `cidata` seed (user `smtest`, key
   login only, passwordless `sudo`). Domain XML printed and reviewed before definition: domain
   `sm-w0024-clean` on `qemu:///session`, 2 vCPU, 2 GiB, the overlay and the seed, one `passt`
   interface whose only port forward is `127.0.0.1:2222` → 22; no graphics, sound, shared folders
   or host devices; virt-install's defaults also add a pty serial console, a guest-agent channel and
   a virtio RNG fed by `/dev/urandom` (no host file or hardware access). First boot: Fedora Linux 44
   (Cloud Edition), kernel 6.19.10; cloud-init `done` with `degraded` status solely for "Failed to set
   the hostname" (cosmetic); no `station-manager` package, no `polkit`, no 9p or virtiofs mounts,
   `Linger=no`, nothing listening on 8080.
4. **A1 step 2, build — FAILED, not yet diagnosed.** The operator copied `.env` into the worktree
   (0600, `mveary:mveary`; worktree at `43d0ed98`; no contents displayed). `SM_FFT=pocketfft
   scripts/dev-rpm.sh` (log private, 0600, `/home/mveary/sm-w0024/build.log`): stage 1 (SPA and
   manual) completed; stage 2 printed its header (`2.0.0-alpha.5-47-g43d0ed98`, PocketFFT) and the
   script exited 1 with **no further output** and no RPM. The identical `go build` (same tags
   `dev pocketfft`, same `-ldflags`) **succeeds without `.env`, with a placeholder key**, so the
   failure depends on what `.env` contributes. Diagnosing it needs `.env` sourced, which the agent's
   permissions blocked; the agent did not work around it. The stage 1 rewrite of
   `frontend/app/dist/index.html` was restored (`git status --porcelain` empty). No artifact exists;
   nothing was copied to `build/private/`.
5. **A1 step 2, build — by the operator.** The operator ran the private build with the existing
   `.env`: exit 0; no secret values or raw output displayed. The cause of the agent's earlier
   failed attempt remains unproven. RPM `station-manager-2.0.0~alpha.5.47.g43d0ed98-1.x86_64`.
   Verified by the agent: RPM SHA-256
   `f80b4bbef6466e79664fe7fd3e40dd35567be4c790b91fada6aded1f12cb44ab` (9,676,494 bytes); retained as
   `build/private/station-manager-2.0.0~alpha.5.47.g43d0ed98-1.x86_64.rpm` (0600; the destination did
   not exist before; SHA re-verified after the copy) **before** the worktree was removed. Unpacked in
   `$W/unpack` (0700, removed after): `usr/bin/smd` SHA-256
   `e1f3f943f76c0832847cbedb3a122ebcf52ce2e2cfed3e870e7079dec124318a` (matching the operator's), one
   embedded version string `2.0.0-alpha.5-47-g43d0ed98`, the `PRIVATE-BUILD-DO-NOT-DISTRIBUTE`
   marker; file list as alpha.4's. The build had again rewritten `frontend/app/dist/index.html`
   only. The worktree's `.env` copy was `shred -u` (btrfs: overwrite not assured; deletion is), then
   the worktree removed (`git worktree remove --force`, pruned).
6. **A2 step 5, install, enable, start (agent; install.md §2–§3).** `scp` to the guest's `~/rpms`
   (0600), SHA-256 inside the guest `f80b4bbe…cb44ab`, matching. `sudo dnf install -y`: exit 0,
   "skipped OpenPGP checks" (expected, §2), "Complete!"; installed `smctl`, `smd` (SHA-256
   `e1f3f943…24318a`), the user unit, the manual and the private marker. `systemctl --user
   daemon-reload` 0; `enable --now smd` 0; `smctl status` "SM is running."; `loginctl
   enable-linger "$USER"` → "Access denied" (no `polkit` in the Cloud image, as at alpha.4 — lane
   deviation; `sudo loginctl enable-linger smtest` → `Linger=yes`). `GET /v1/healthz` ok;
   `/v1/version` `2.0.0-alpha.5-47-g43d0ed98`, schema 18 clean, Home `legacy`; no `error` journal
   line. Before setup `GET /v1/default-logbook` answers 200
   `{"effective":{"id":1,"uuid":"","name":"","callsign":""}}` — the first-run case W-0024 5b
   covers. A fresh install seeds every lookup **disabled** (`hamnut` false; `qrzcqlookupservice`,
   `qrzlookupservice` false).
7. **A2 step 6, capture started (agent).** `tcpdump` installed in the guest first (so `dnf`'s own
   traffic is outside the capture), then `systemd-run --unit=w0024-capture tcpdump -i any -nn -U
   -w /var/tmp/rehearsal.pcap 'tcp and (port 80 or port 443) and not host 127.0.0.1'` from
   08:04:21Z (guest clock; host 08:04:28Z), **before any callsign is typed** — first-run setup
   included. Browser tunnel `127.0.0.1:18080` → guest `127.0.0.1:8080` open; throwaway profile
   `/home/mveary/sm-guest-w0024/ff-smtest` (0700, empty).
8. **A2 step 5, clean-install checks (operator drove the UI; screenshots
   `Screenshot_20261030_100904` … `_105535`; times local CAT = UTC+2).** Throwaway profile
   through the tunnel.
   - *Welcome only:* before setup, only the "Welcome to Station Manager" card, **Save** disabled on
     an empty field. Above it the amber banner "Station Manager could not read the state of a
     default logbook change. Live entry is paused until it can." with **Check again** and
     **Reload**: the served status before setup is `{"effective":{"id":1,"uuid":""…}}` (200,
     57 B), which the client reads as unreadable (**candidate finding F1**). Pre-setup `count` 404
     (one console error).
   - *Callsign:* `7Q5@MLV` → "Invalid callsign format", no request sent (17 requests unchanged);
     ` 7q5mlv ` (displayed upper-case) saved; the guest stores `station_callsign` `'7Q5MLV'`.
     A spaces-only value was not separately exercised (Save requires a non-blank value; code and
     tests).
   - *Setup:* "✓ Setup complete" with **Open Settings →** and **Start logging →**; the banner
     **cleared by itself** (re-read after `PUT config`: `default-logbook` 200, 176 B, naming the
     new default). Default logbook id 1, `7Q5MLV`, UUID `01a18bdd…ae27d2`, matching
     `/v1/version`.
   - *Persistence:* survives reload (normal app, no welcome, no banner; header "Archive Home ·
     Logbook Default (0) · Rig not set"; build badge `2.0.0-alpha.5-47-g43d0ed98`, matching
     `/v1/version`, RPM and binary) and a daemon restart by the agent (08:13:19Z guest,
     instance `dmi0lvfpzxof` → `dmi0t42ycv82`, `setup_complete` true, no error line): the open
     page reconnected without reload or banner; a later reload likewise.
   - *Discoverable:* Settings (Station prefilled, all tabs; Logbooks: Default with its
     description, Edit, Delete disabled, no Set as default — **candidate finding F2**: the hint
     "Live contacts keep going to the Default logbook." does not mention Set as default); Manual
     (`/manual/`, the new *Changing the Default logbook* section present with the five corrected
     points — **candidate finding F3**: "More than one window" mentions only archive switches);
     first QSO logged (`POST qso?logbook=1` 201, header "Default (1)").
   - *Fixture deviation (guest only):* the first QSO was logged as **7Q7EB**, not `7Q5MLV`;
     then `7Q5MLV` logged normally (logging one's own call is not refused — no such rule in the
     daemon or the form). No external effect (capture: 0 packets; Country "Unknown", no name).
9. **A2 step 6, rehearsal (operator drove the UI; `_105015` … `_111841`).**
   - *Lookups:* Settings → Enrichment shows Hamnut, QRZCQ.com and QRZ.com **DISABLED** (seeded
     disabled on a fresh install; `/v1/config` agrees). No change and no restart needed.
   - *T and P (AC-2):* T `W-0024 Acceptance` (`7Q5MLV`, description shown), P `W-0024 Other
     call` (`7Q5MLV/P`). **Set as default** enabled on T, disabled on P, absent on Default.
     Every destination for every logbook unbound and disabled (`clublog`, `qrz`, `qrzcq`; no SM
     Cloud account in the guest). P's disabled button carries `title="Its callsign 7Q5MLV/P is
     not the station callsign 7Q5MLV: only a logbook for the station callsign can be the
     default."` (Inspector), but **Firefox shows no tooltip on a disabled button** (operator) —
     **candidate finding F4** (the reason is present but not visible; the greyed Delete uses the
     same pattern).
   - *AC-3:* the confirmation, verbatim as specified: target and effect; "This window has an
     unlogged QSO on Phone / CW: 7Q5. OK discards this window’s entry and restarts; Cancel keeps
     it."; "The reload loses the unsaved Settings edits in this window: Logbooks. Cancel keeps
     them."; the other-windows warning; no SM Cloud note (no SM Cloud binding).
   - *AC-4:* Cancel → the `default-logbook`-filtered Network list "No requests"; instance
     `dmi0t42ycv82` and status (Default, no change) unchanged; `Draft` kept in the Add form; `7Q5`
     kept on Phone / CW (operator). The Settings leave guard then prompted for the `Draft`
     (expected).
   - *AC-5, AC-6 (switch to T):* initiated in window A; the daemon exited with the restart code at
     09:05:02Z and was restarted by systemd at 09:05:07Z (new instance `dmi1wtauzhal`); T effective
     and settled (`effective`, unconfirmed false); no error line. Windows A, B (Phone / CW, typed
     `7Q5MLV`, unlogged) and C (Logbook) reloaded onto "Logbook W-0024 Acceptance"; A's and B's
     entries discarded as warned. **Candidate finding F5:** the Logbook view opens on the first
     logbook (`logbooks[0]`, since 2026-09-21), not the effective default. **Observation O6:** a
     window's header count does not follow contacts logged in another window until it reloads.
     R3 (the transient pending banner) not seen.
   - *R2:* Settings → Logbooks notice "“W-0024 Acceptance” is now the default."; no cleanup
     pending.
   - *AC-7:* two contacts in T, `7Q5MLV`, synthetic comment, 20m 09:13:03Z and 40m 09:13:26Z;
     T 2, Default 2 (unchanged), P 0; `forwarder-queues` empty. **Deviation:** the two fell in the
     same minute (both stored — they differ by band); the minute separation was not exercised.
     The live run types each Time On explicitly.
   - *AC-10:* T's Delete disabled while effective; P deleted ("Logbook “W-0024 Other call”
     deleted."; its count now `logbook_not_found` — soft delete).
   - *AC-8 (switch back):* the confirmation names "Default (7Q5MLV)", no entry or edits lines; the
     daemon exited 09:18:10.73Z, was down until 09:18:16.4Z (≈5.5 s), new instance `dmi26vj5grz2`
     serving Default, settled; header "Logbook Default (2)"; notice "“Default” is now the
     default."; T keeps 2. Nothing logged after the switch.
   - *End state:* Default 2, T 2, P soft-deleted; status Default effective (last change
     `effective`, unconfirmed false); `config.json` holds no `default_logbook_change`; no upload
     queue. (The systemd journal holds only start/stop lines — the daemon logs to its own
     `log/smd.log`; see entry 10 for that log's levels.)
   - *Network measurement:* the capture ran 08:04:21Z–09:19:01Z (before any callsign, through
     setup, two restarts, both switches and four contacts). **No traffic matching the capture's
     filter — TCP port 80 or 443, excluding 127.0.0.1 — was observed** (24-byte file, header
     only). **Positive control:** the identical command and filter, with one deliberate
     `curl https://fedoraproject.org/` (200), recorded 414 packets (IPv6 and IPv4), so the
     measurement could see such traffic. This does **not** establish the absence of all outbound
     traffic: other ports, DNS (53) included, were outside the filter.
10. **A2 step 7 and disposal (agent; operator approved 2026-10-30).**
   - *Evidence retained first,* outside the guest, in `/home/mveary/sm-w0024/guest-evidence`
     (0700; files 0600; `MANIFEST.sha256`, 33 entries): `rehearsal.pcap` (24 B), `control.pcap`
     (491,111 B), `watch1.log`, `watch2.log`, the systemd journal, the daemon's own
     `log/smd.log` (as `smd-log.tar.gz`), the final state readings, and the 27 screenshots
     `Screenshot_20261030_100904` … `_111841`.
   - *Daemon log* (`smd.log`, 2,963 lines): 2,962 `info`, **no `error`**, **one `warn`** at
     08:03:48Z — the very first start, before setup: "startup: could not record the default
     logbook in the archive" (`SetArchiveDefaultLogbookWithContext`: "logbook 1 does not exist";
     `cmd/smd/archive_adopt.go:109`) — **observation O7**, the same pre-setup state as F1; not
     compared with alpha.5.
   - *Uninstall (install.md §10):* `systemctl --user disable --now smd` (wants symlink removed;
     `inactive`); `sudo dnf remove -y station-manager` exit 0; `rpm -q` not installed;
     `/usr/bin/smd` absent; after `daemon-reload` the unit is `not-found`. **The working directory
     is retained** (`config.json`, `db/`, `log/`). *Method deviation:* the agent's pre-uninstall
     hash list was taken while the daemon still ran, so the clean shutdown (WAL checkpoint,
     summary rewrite, final log lines) changed those files; the byte comparison therefore does
     not isolate the uninstall. Instead, after it, read-only (`immutable=1`): `integrity_check`
     ok; QSOs Default 2, T 2; logbooks Default, W-0024 Acceptance, W-0024 Other call (soft-
     deleted); **`qso_upload` rows 0**; no `-wal`/`-shm` sidecar (clean stop).
   - *Disposal:* `virsh destroy` and `undefine --remove-all-storage` (`vda` overlay and `sda`
     seed removed); the tunnel ended with the guest (port 18080 free; the agent's `pkill` pattern
     also matched its own shell, ending that command early — no other effect); the directory
     `/home/mveary/sm-guest-w0024` removed (base image, CHECKSUM, key, `known_hosts`, seed
     sources, domain XML, Firefox profile); no domain left on `qemu:///session`. No Firefox
     process was running (operator).

## Gate A2 result and findings (reviewer rulings, 2026-10-30)

**A2: procedure complete, with findings — not an unqualified pass.** Not exercised: a
spaces-only callsign. Deviations recorded: the 7Q7EB first contact (guest only); the two T
contacts in the same minute (both stored because the bands differ; minute separation not
exercised); the pre-uninstall hash comparison (above).

| Item | Status | Note |
|---|---|---|
| AC-2 | **PARTIAL / FAILED** | P's Set as default is disabled correctly, but the promised reason is not visible to the operator (F4); an Inspector attribute does not meet the outcome. |
| AC-3, AC-4, AC-5, AC-6, AC-7, AC-8, AC-10 | met in the rehearsal | AC-8 per the operator: the Session badge keeping the tab's two contacts is consistent with session ownership (ADR 0094). |
| F4 — disabled-button reasons invisible in Firefox | **blocking acceptance** (makes AC-2 partial) | Set as default on another callsign; the greyed Delete uses the same pattern. |
| F5 — Logbook view opens on the first logbook, not the effective default | **unmet W-0024 outcome — blocking acceptance** | The dossier states the Logbook view opens on the effective default. Its age (since 2026-09-21) does not resolve the mismatch: fix it, or obtain an explicit scope ruling, before closing acceptance. |
| F1 — first-run false-alarm banner before setup | non-blocking; follow-up | Clears by itself after setup. |
| F2 — Logbooks hint omits Set as default | non-blocking; follow-up | Wording. |
| F3 — manual "More than one window" mentions only archive switches | non-blocking; follow-up (W-0018) | Documentation. |
| O6 — counts in other windows refresh only on reload | recorded separately from routing correctness | Routing was correct throughout; only the displayed counts lag. |
| O7 — first start before setup logs one `warn` | non-blocking first-run diagnostic finding; follow-up with F1 | Same pre-setup state as F1. |

**Next (reviewer, 2026-10-30):** fix F4 — disabled reasons visibly available without relying on
a disabled button's tooltip, for both Set as default and Delete — and F5 — the Logbook view
initially selects the established effective default, preserving a deliberate selection of
another logbook and explicit navigation targets; the effective-default requirement is kept, not
narrowed. Failing tests and reversion proofs, review, then a new artifact and the required
checks and affected browser journeys rerun. **This invalidates candidate `43d0ed98` for
deployment.**

**A3 is on hold.** The reviewer recommends keeping the candidate and restoring the pre-test
backup afterwards; that discards intervening data and needs the **station operator's** consent,
and the operator chooses the outage window. Neither decision has been made. Live deployment is
not approved.

## F4 and F5 fixed — awaiting browser acceptance (2026-10-30)

`ea1ee70c` (reviewer approved; clean-room review clean): **F4** — Set as default and Delete,
unavailable for a reason, are `aria-disabled` (greyed, inert, focusable) and a click or Enter
shows the reason as text under the row, read live; transient locks stay `disabled`. **F5** — the
Logbook view opens on an explicit navigation target, else the operator's own earlier pick (while
it exists), else the established effective default (the tab's context UUID), else the first;
the dropdown records the operator's pick separately. The manual's Logbooks section updated with
it. Failing tests first; nine reversion proofs. **Both remain fixed, awaiting browser
acceptance**, which **invalidates candidate `43d0ed98`**: a new artifact is built after green
CI, and A1 and the affected A2 journeys are repeated.

**Rerun requirements (reviewer, 2026-10-30):** A1 (`ci:local`, one build, full hashes,
version agreement) for the new commit; A2 in a fresh guest by the same method — the clean
install and the rehearsal; in Firefox, **both a click and keyboard activation (Tab to the
greyed button, then Enter and Space)** reveal the reason for an unavailable Set as default and
an unavailable Delete **without opening a confirmation or sending a mutation** (Network panel);
the Logbook view opening on the effective default when it is not the first logbook, a pick in
its dropdown kept across leaving and returning, and an explicit target (Forwarding's failed
count link) still opening its logbook. The rehearsal's two contacts take explicit Time On
values in different minutes.

## Execution log — candidate 2 (`ea1ee70c`)

11. **2026-10-30 — CI and A1 step 1.** CI run 41036837352 green on `ea1ee70c` (job
    "Releasability gate": success). Worktree `/home/mveary/sm-w0024-build2` (detached at
    `ea1ee70c`): `git status --porcelain` empty; `git describe` `v2.0.0-alpha.5-50-gea1ee70c`;
    `scripts/version.sh` → `2.0.0-alpha.5-50-gea1ee70c` / RPM `2.0.0~alpha.5.50.gea1ee70c`.
    `task ci:local`: **exit 0**, "All CI gates passed locally"; 133 Go `ok` lines, none
    `(cached)`; log `/home/mveary/sm-w0024/ci-local-2.log`. `frontend/app/dist/index.html`
    restored from `HEAD` (alpha.3 Finding #1); `git status --porcelain` empty. The build waits for
    the operator (private key).
12. **A2 step 4, fresh guest (agent).** `/home/mveary/sm-guest-w0024b` (0700, absent before).
    Image, CHECKSUM and `fedora.gpg` fetched again: "Good signature from Fedora (44)", key
    `36F612DCF27F7D1A48A835E4DBFCF71C6D9F90A6`; `sha256sum -c` OK; image SHA-256
    `28680fe5b371a5a82ebf43a31926e086a168e59949d03969c5093e7071f90b7f` (unchanged), read-only.
    Domain `sm-w0024-clean2`, configuration identical to entry 3 (XML reviewed: one `passt`
    interface, only forward `127.0.0.1:2222` → 22; no shared folders, host devices, graphics or
    sound). First boot: Fedora Linux 44 (Cloud Edition), kernel 6.19.10; cloud-init `done`; no
    `station-manager` package, no 9p or virtiofs mounts, `Linger=no`, nothing on 8080.
13. **A1 step 2, build — by the operator.** First attempt exit 1: `.env` had not been copied
    into the new worktree, and the script stopped at its key check ("requires a non-empty"),
    building nothing. After the operator copied it (0600): exit 0. RPM
    `station-manager-2.0.0~alpha.5.50.gea1ee70c-1.x86_64`, SHA-256
    `6ab46bf7deaea878776f0cfe879a6edcf70f1b95d6603606330ce1fc9eecd0da` (9,676,933 bytes), PocketFFT
    (CGO); retained as `build/private/station-manager-2.0.0~alpha.5.50.gea1ee70c-1.x86_64.rpm`
    (0600; absent before; SHA re-verified) before the worktree was removed. Unpacked (removed
    after): `usr/bin/smd` SHA-256
    `d2f06f5eb520feb33f02cbf51e4989835f521e5ff7a9ef636381fc383e6b3e78`, one embedded version
    `2.0.0-alpha.5-50-gea1ee70c`, the private marker; file list as before. Only
    `frontend/app/dist/index.html` was rewritten. `.env` copy `shred -u` (btrfs caveat), worktree
    removed and pruned.
14. **A2 step 5, install (agent; §2–§3).** In-guest SHA `6ab46bf7…e3b78`… matching; `dnf install`
    exit 0 ("skipped OpenPGP checks", "Complete!"); `smd` SHA `d2f06f5e…3e78` matching; enable
    `--now` 0; self-linger "Access denied" (no `polkit`; `sudo loginctl enable-linger smtest` →
    `Linger=yes`, the recorded lane deviation); "SM is running."; healthz ok; `/v1/version`
    `2.0.0-alpha.5-50-gea1ee70c`, schema 18 clean, Home; pre-setup status
    `{"effective":{"id":1,"uuid":""…}}`; every lookup seeded disabled. Capture started
    11:01:27Z (same command and filter as entry 7) before any callsign; tunnel 18080 open;
    fresh profile `/home/mveary/sm-guest-w0024b/ff-smtest`.
15. **A2 step 5, clean-install checks — candidate 2 (operator drove the UI; screenshots
    `Screenshot_20261030_130754` … `_131941`).** Welcome only, amber banner (F1, unfixed); **a
    spaces-only callsign keeps Save disabled** (operator) — the candidate-1 gap now exercised;
    `7Q5@MLV` → "Invalid callsign format", no request; ` 7q5mlv ` saved as `'7Q5MLV'`; "Setup
    complete" with both journeys, the banner cleared by itself (`PUT config` then re-reads);
    reload: normal app, header "Logbook Default (0)", badge `2.0.0-alpha.5-50-gea1ee70c`;
    daemon restart by the agent at 11:10:32Z (instance `dmi4dlv57ltt` → `dmi4kstyfosq`,
    `setup_complete` true) and reload, no banner. **F4 on Delete (Default):** a click, **Enter**
    and **Space** each showed "The Default logbook can’t be deleted" under the row, with no dialog
    and no `DELETE` request (Network panel); keyboard activation reached the greyed button; a
    visible focus ring was not separately confirmed. Manual (`/manual/`): the three `ea1ee70c`
    texts present. First QSO `POST qso?logbook=1` 201, "Default (1)".
16. **A2 step 6, rehearsal — candidate 2 (`_132119` … `_134127`).** Lookups seeded disabled. T
    (id 2) and P (id 3) created; every destination for every logbook unbound and disabled.
    - **F4 on Set as default (P):** a click, Enter and Space each showed "Its callsign 7Q5MLV/P is
      not the station callsign 7Q5MLV: only a logbook for the station callsign can be the
      default." under P's row, with no dialog and no `POST`. **AC-2 met** (visible reason).
    - **AC-3** confirmation as candidate 1; **AC-4** Cancel: no request, instance `dmi4kstyfosq`
      and status unchanged, `Draft` kept.
    - **AC-5/AC-6:** `POST default-logbook` 202; exit 11:27:24.57Z, down ≈5.3 s, new instance
      `dmi4xtk7u53y` serving T, settled; window B reloaded onto T (entry discarded). Window A had
      `Draft` still typed: Firefox's leave prompt appeared (**O8**); **Stay on Page** left the page
      gated under the overlay — "The daemon restarted with another default logbook; this page must
      reload to rebind." — with logging paused (**O9**: archive-centred heading and gate line).
      Then **Reload now** raised the leave prompt, and **Leave Page did not reload** — the page
      stayed (session timer kept running), repeatedly (**F6**). **F5 then Leave Page reloaded
      it**: "“W-0024 Acceptance” is now the default."
    - **F5 (Logbook view):** window C reloaded after the switch and **opened on "W-0024
      Acceptance (7Q5MLV)"** — the effective default, second in the list ("No QSOs in this
      logbook."). After choosing Default, leaving to Phone / CW and returning, it **opened on
      Default** (the operator's pick kept) while T was effective. An explicit target (Forwarding's
      failed-count link) was **not exercised** — the guest has no forwarders.
    - **AC-7:** two contacts in T at 11:35:23 (20m) and 11:35:43 (40m) — the form filled Time On,
      so again the same minute (both stored; bands differ). A **third** contact, Time On typed
      **11:37:00** (20m, same band as the first, different minute), stored as `113700` with no
      duplicate warning: **minute separation exercised and passed**. T 3, Default 1.
    - **AC-10:** T's Delete unavailable while effective; P deleted (soft).
    - **AC-8:** exit 11:41:14.00Z, down ≈5.5 s, new instance `dmi58emojl78` serving Default,
      settled; window B "Logbook Default (1)". The transient pending state again not observed
      (R3), even at 0.25 s sampling.
    - *Network measurement:* capture 11:01:27Z–11:41:56Z, same filter: **no traffic matching TCP
      port 80/443 excluding 127.0.0.1 was observed** (24-byte file). (The candidate-1 positive
      control applies to the identical command and filter.)
17. **A2 step 7 and disposal — candidate 2.** Evidence first:
    `/home/mveary/sm-w0024/guest-evidence-2` (0700; 38 files in `MANIFEST.sha256`, 31
    screenshots). Daemon log: 7,182 `info`, **no `error`**, one `warn` (11:01:05.685Z, before
    setup: "could not record the default logbook in the archive" — O7, as before). The daemon was
    **stopped before** the working directory was hashed; after `disable --now`, `dnf remove`
    (exit 0, unit `not-found`, `/usr/bin/smd` absent) the working directory is **byte-identical**
    (5 files) — candidate 1's method limitation resolved. Read-only after: integrity ok; QSOs
    Default 1, T 3 (`113523`, `113543`, `113700`); P soft-deleted; **`qso_upload` rows 0**. Guest
    destroyed and undefined with its storage; tunnel ended with it; directory removed; no domain
    left.

### Candidate 2 (`ea1ee70c`) — A1 and A2 result

**A1: complete** (CI and `ci:local` green; one build; full hashes; version agreement across RPM,
binary, `/v1/version`, badge). **A2: procedure complete, with findings.** F4 and F5 now pass in
the browser: **AC-2 met**; the Logbook view opens on the effective default and keeps a
deliberate pick. Not exercised: an explicit navigation target (no forwarders in the guest; covered
by tests). New:

| Item | Status | Note |
|---|---|---|
| **F6** — the overlay's **Reload now** does not reload after Firefox's **Leave Page** while Settings holds unsaved edits | **candidate finding; severity for the reviewer** | The page stays gated (logging paused — the safety property holds) but its own way out fails; the browser's reload (F5 → Leave Page) works. Both paths reload the same way in code; the cause is not yet established. Likely shared with archive switching (same reload path); not yet verified. |
| **O8** — Set as default with unsaved Settings edits: the confirmation names them, then Firefox's leave prompt asks again on the reload | observation | The designed backstop; a double prompt. |
| **O9** — the post-restart overlay's heading and first sentence, and the Settings gate line, speak of an archive switch | observation (wording) | The reused reload latch, with default-specific detail (accepted 2026-10-30). |

### Reviewer rulings on candidate 2 (2026-10-30)

- **F6 is a P2 recovery defect and blocks acceptance.** The gate preserved safety and F5 is a
  demonstrated workaround, which limits severity; but the recovery the application offers
  (Reload now → Leave Page) repeatedly failed — an unresolved supported journey under the gate
  (`docs/dogfood-acceptance.md`, acceptance boundary). Fix before acceptance; only the station
  operator could waive it. Required outcome: after Stay on Page the stale tab stays gated with its
  unsaved edits intact; Reload now → Leave Page actually reloads, establishes the effective
  default and restores usable logging; choosing Stay again preserves the gate and a later retry
  still works; archive switching exercised as a regression case (its involvement unverified).
  Start from a reproducible Firefox failure; the regression assertion goes at the boundary the
  investigation identifies (the existing gate test only counts calls to a replacement reload
  function and cannot prove navigation completed); then the reversion proof and a packaged-browser
  rerun — a third artifact.
- **F4 / AC-2: satisfied.** Visible keyboard focus remains unverified — check it next run.
- **F5:** effective-default selection and deliberate-pick preservation passed; the
  explicit-target **browser** case remains not exercised (automated coverage does not satisfy the
  required browser check).
- **O8, O9:** non-blocking follow-ups on present evidence.
- **Candidate 2: A1 complete; A2 procedure complete with a blocking finding (F6)** — not an
  acceptance pass.
- **A3 on hold**; the reviewer recommends keeping the accepted candidate and restoring the
  pre-test backup afterwards; the station operator must explicitly consent to discarding all
  intervening writes and choose the outage window. Live deployment not approved.

## F6 investigation and fix (2026-10-30)

**Reproduced outside the app** — headless Firefox 159.0 over WebDriver BiDi, a bare page whose
`beforeunload` guard always prompts, prompt answers driven explicitly (probe and output kept at
`/home/mveary/sm-w0024/f6-probe/`):

| Sequence | Result |
|---|---|
| `location.reload()`, Leave | reloaded (control) |
| `location.reload()` Stay, then `location.reload()` Leave | **same page** (F6) |
| `location.reload()` Stay, guard removed, `location.reload()` | **same page, no prompt** |
| `location.reload()` Stay, then `location.assign(href)` / `location.href = href`, Leave | reloaded |
| `location.reload()` Stay, `location.replace(href)` Stay, `location.replace(href)` Leave | Stay kept the page each time; Leave reloaded |
| `location.replace(href)` Stay, Stay, Leave | as above |
| `location.replace(href)` Leave, no prior Stay | reloaded |

**Cause (observed, not cited):** once a `location.reload()` has been refused at the prompt, a later
`location.reload()` in the same document never completes in this Firefox — even with the guard
removed — while a navigation to the same address does, still honouring the prompt. The app's two
reload call sites both used `location.reload()`: `lib/utils/reload.ts` (Settings' Restart
daemon) and a private copy in `lib/config/archives.svelte.ts` (the gate's **Reload now**, the
default-change and archive-switch rebinds). In F6 the first reload (the rebind) was refused, so
**Reload now** — a second `location.reload()` — could never complete; the browser's own reload
(F5) is not a script reload and worked. Archive switching uses the same private copy and is
therefore exposed to the same failure (to confirm in the packaged rerun).

**Fix (test-first):** one seam. `lib/utils/reload.ts` navigates with `location.replace` to the
current address without its fragment (a fragment-only change would scroll, not reload); every
SPA response is `Cache-Control: no-cache` (`internal/api/spa.go`), so the navigation refetches as
a reload would. The archives store reloads through that seam. **Regression assertions at that
boundary:** `lib/utils/reload.test.ts` (replace, never reload; query kept; fragment dropped) and
`lib/config/archives.reload.svelte.test.ts` (the store's Reload now goes through the seam).
**Reversion proofs:** reload restored → all three seam cases fail; fragment kept → the fragment
case fails; the store bypassing the seam → its case fails. Gates: lint, format, svelte-check
(0/0), 2,612 tests, maintainability gate. **Browser acceptance pending** a third artifact and the
packaged rerun, which must show: after Stay the tab stays gated with its unsaved edits intact;
Reload now → Leave reloads onto the effective default with logging usable; Stay again keeps the
gate and a later retry works; and the same for an **archive switch** with unsaved Settings edits
(regression case). Also to check then: visible keyboard focus (F4) and the explicit-target
browser case (F5) — the latter needs a forwarder failed-count link in the guest.

*Note:* the host rebooted at 11:54Z (after candidate 2's guest was disposed); the agent's
scratchpad was cleared, `~/sm-w0024` (evidence) survived, and the live daemon restarted on its own
(`active`); nothing on the live station was touched.

**Review and commit (2026-10-30):** reviewer approved the fix (no blocking findings; probe accepted
as observed Firefox 159 behaviour; dropping the fragment matches the HTML navigation algorithm).
Committed `736ccd70` (fix and tests, Codex clean), pushed; CI run 41062947285 green. Reviewer
additions to the packaged rerun: include **Settings → Restart daemon** (it uses the changed seam);
for F5's explicit-target case, a guest-local HTTP responder returning 401 with a dummy QRZ key and
the endpoint pointed at guest loopback (a closed port retries indefinitely,
`internal/forwarding/worker/worker.go`; HTTP 401 is a terminal failure,
`internal/forwarding/qrz/qrz.go`), a separate fixture logbook and labelled synthetic contact, and the
actual Forwarding link clicked while its target differs from both the effective default and the
previously selected logbook. F6 remains **fixed, awaiting packaged-browser acceptance**; A3 stays on
hold; live deployment unapproved.

## Revision 4 — reconciled with the installed artifact (2026-11-03, for ratification)

**Ratified 2026-11-03** (below). R0–R2 may proceed; R4–R6 stay on hold until the station operator
chooses the outage window and consents to discarding intervening writes.

**Installed artifact (verified read-only, 2026-11-03).**

- `rpm -q`: `station-manager-2.0.0~alpha.5.89.gb766a145.dirty-1.x86_64`, built and installed
  2026-11-03 13:59 CAT by `task deploy:local:dev` at the operator's direction. `GET /v1/version`:
  `2.0.0-alpha.5-89-gb766a145-dirty`, schema 19 clean, archive Home, Default effective.
- RPM file `build/private/station-manager-dev.x86_64.rpm`, SHA-256
  `fe792081d7e0a1350bf0a177839add64da8aeb2dd2efe582d37c3c01340e2fd3`; its `usr/bin/smd` SHA-256
  `8a2d89eccb943d2a0ca2dbef8e798bdb674758e73639f39f01bb4a7ad8ae2923` equals the installed
  `/usr/bin/smd`; one embedded version string. The next `task deploy:local:dev` overwrites that
  file, so step R0 preserves it.
- `-dirty`: one uncommitted Markdown file (`docs/work/W-0012-…`) at build time; no code difference
  from `b766a145`, on which CI run 41626326294 is green. **Recorded deviation from A1**
  (`docs/dogfood-acceptance.md`, A1: the build worktree is clean): the artifact was built from a
  worktree with a Markdown-only difference. Kept by ruling (2026-11-03) — a rebuild would be another
  artifact to accept; its exact hashes are preserved (R0).
- Contains W-0024 slices 1–5c (`46a07d07`), the F4/F5 fix (`ea1ee70c`) and the F6 fix (`736ccd70`).
  W-0024 has been live since the contest install of 2026-10-31 (`c5e52fa3`), made outside this run
  sheet at the operator's direction; the "deployment not approved" gate is therefore moot, and
  acceptance now evaluates the installed artifact.
- New since candidate 2 and outside W-0024's journeys: schema 19 (the contest serial ledger) and the
  contest form. AC-9's comparison covers them (expected unchanged).

**Void for this run:** the candidates `43d0ed98` and `ea1ee70c`; "Station now: alpha.5"; the
alpha.5 rollback RPM (alpha.5 cannot open schema 19 without `smd db-downgrade --to 18`); the
30 October sidecar inventory; and every existing backup — including
`station-manager-20261031T0309Z-pre-contest.tar.gz` — as a restore source. Only the fresh pre-test
backup of step R4 may be restored.

**Remaining checks** (reviewer rulings, 2026-10-30):

- **F6** packaged-browser recovery: with unsaved Settings edits, after **Stay on Page** the tab stays
  gated with its edits intact; **Reload now → Leave Page** reloads onto the effective default with
  logging usable; **Stay** again keeps the gate and a later retry works. The same for an **archive
  switch**. **Settings → Restart daemon** with unsaved edits is a separate case: its handler
  (`frontend/app/src/lib/config/Settings.svelte`, `restarted()`) reloads directly and, with the
  archive and default unchanged, raises no switch overlay, so it has no **Reload now** step.
- **F4** visible keyboard focus on the greyed Set as default and Delete buttons.
- **F5** the actual Forwarding failed-count link opens its explicit target logbook, distinct from
  both the effective default and the previously selected logbook.

**Evidence retained:** F4's visible disabled reasons by click, Enter and Space with no dialog or
mutation (candidate 2, entries 15–16; AC-2 met); F5's effective-default selection and preserved pick
(entry 16). **U1–U7 stay unexercised** — not passed and not waived — unless new evidence or an
operator waiver resolves them. Closing F6 alone does not close this record: AC-1 to AC-11 on the
station remain.

### Revised run sheet (revision 4)

- **R0 — preserve the artifact (no outage).** Copy the RPM to
  `build/private/station-manager-2.0.0~alpha.5.89.gb766a145.dirty-1.x86_64.rpm` (absent before,
  0600); its SHA-256 equals `fe792081…2fd3`; unpacked in `$W/unpack` (0700), `usr/bin/smd` equals
  `8a2d89ec…2923` and the installed binary; unpack removed. Every later step names this file.
- **R1 — A1 for the installed commit (no outage).** No new build. CI run 41626326294 recorded; `task
  ci:local` once in a clean scratch worktree at `b766a145`, exit 0 recorded (required by the gate's
  A1; ruling 2026-11-03).
- **R2 — A2 in a fresh guest (no live paths).** The candidate-2 method (entries 12–17), domain
  `sm-w0024-clean3` under `/home/mveary/sm-guest-w0024c`, with the R0 RPM. Clean install (§2–§3),
  lookups seeded disabled, packet capture as before. Then:
  1. **F6 — default change.** Settings with an unsaved edit; Set as default on T → OK; on the reload
     prompt **Stay**: the tab stays gated, the edit intact, logging paused. **Reload now → Stay**:
     still gated. **Reload now → Leave**: reloads onto T, the settled notice, a contact can be
     logged (labelled synthetic).
  2. **F6 — archive switch** (regression): a second archive created in the guest; the same
     Stay / Stay / Leave sequence across a switch to it and back.
  3. **F6 — Settings → Restart daemon** (separate sequence; no overlay, no Reload now). With an
     unsaved Settings edit and **no** unlogged Phone / CW entry: **Restart daemon** → on the leave
     prompt **Stay**; the edit survives. **Restart daemon** again → **Stay**; the edit survives.
     **Restart daemon** a third time → **Leave**: the page actually reloads (fresh page load, the
     edit gone as warned). Each invocation is a separate daemon restart: record each new
     `/v1/version` instance and its journal exit/start lines.
  4. **F4 — focus.** Tab to the greyed Set as default (P) and Delete (Default): a visible focus
     ring, screenshot.
  5. **F5 — explicit target.** A guest-local HTTP responder returning 401; a QRZ forwarder with a
     dummy key pointed at it, bound only to fixture logbook F with one labelled synthetic contact,
     so the upload fails terminally (`internal/forwarding/qrz/qrz.go`). In the Logbook view select a
     logbook that is neither the effective default nor F; go to Settings → Forwarding and click the
     failed-count link: the Logbook view opens on F.

  Evidence, uninstall and disposal as entry 17. **Uploads in the guest:** fixture F's failed upload
  is deliberate and expected (one `qso_upload` row for F's contact, terminal failure); every other
  guest contact has no upload row. Candidate 2's zero-upload result does not carry over to this
  guest. The station's AC-9 still requires zero test uploads.
- **R3 — Readings A (station, daemon running, read-only).** As step 8, plus `rpm -q`, the installed
  `smd` SHA-256 (must equal R0's) and the schema version.
- **R4 — stop, sidecars, baseline, fresh backup and verification (outage begins).** As steps 9–14,
  with two changes. **Sidecars:** the inventory is taken afresh after the clean stop; the single
  allowed exception is the premigrate pair named above, and it is re-verified now — present
  byte-identical to the recorded hashes. Any other `-wal`, `-shm` or `-journal` file, or a change to
  that pair, stops the run (files preserved as found, daemon left stopped, operator reviews).
  **Backup:** named `station-manager-<UTC>-gb766a145-pre-w0024.tar.gz`. **This backup is the only
  restore source.**
- **R5 — accept on the station (same outage).** Step 15 becomes: no install; `systemctl --user
  start smd`; **AC-1** — `rpm -q`, `/v1/version`, sidebar badge and the startup log line agree on
  `2.0.0-alpha.5-89-gb766a145-dirty`, Default effective, `GET /v1/default-logbook` shows no change.
  Steps 16–23 as written; the post-test backup is named
  `station-manager-<UTC>-gb766a145-post-w0024.tar.gz`; AC-9's preserved set includes the contest
  serial ledger (expected unchanged).
- **R6 — after the test.** Step 24 as written, restoring R4's backup with the installed artifact
  kept. Step 25 (package rollback) is withdrawn: the installed artifact stays, and a package
  downgrade below schema 19 would need its own data-downgrade plan and ruling. Step 26 as written.
- **AC-11 (revised):** R4's backup verified (complete-set comparison and integrity) and R0's
  artifact hash equal to the installed binary before step R5. No rollback RPM.

### Rulings on revision 4 (2026-11-03)

1. **Artifact:** the installed `-dirty` RPM, exact hashes preserved; the Markdown-only build
   difference recorded as a deviation from A1.
2. **F4–F6:** in the guest, including the dummy forwarder.
3. **R1:** `task ci:local` once at `b766a145` in the scratch worktree (gate A1).
4. **Fixture:** `7Q5MLV`, labelled synthetic; lookups disabled and verified before any entry;
   station test logbooks have no enabled destination.
5. Corrections applied: R2.3 rewritten as the Restart daemon sequence; R4's sidecar exception made
   explicit; R2's expected fixture-F upload stated.

**Still the station operator's:** the outage window for R4–R6, and consent to step 24's discard of
everything written between R4's backup and the restore.

**Ratified (reviewer, 2026-11-03):** revision 4 is the acceptance run plan; the three corrections
match the rulings. R0–R2 may proceed as specified. R4–R6 stay on hold until the station operator
supplies the outage window and explicit consent to discard intervening writes. W-0024 acceptance
remains incomplete.

## Execution log — revision 4

18. **2026-11-03 — R0 (agent).** Copied `build/private/station-manager-dev.x86_64.rpm` to
    `build/private/station-manager-2.0.0~alpha.5.89.gb766a145.dirty-1.x86_64.rpm` (absent before;
    0600; 9,691,225 bytes); SHA-256 `fe792081…2fd3`, as recorded. Unpacked in `$W/unpack` (0700,
    absent before, removed after): `usr/bin/smd` `8a2d89ec…2923`, equal to the installed
    `/usr/bin/smd`; one embedded version `2.0.0-alpha.5-89-gb766a145-dirty`.
19. **R1 (agent).** Worktree `/home/mveary/sm-w0024-build3` (detached at `b766a145`):
    `git status --porcelain` empty; `git describe` `v2.0.0-alpha.5-89-gb766a145`. `task ci:local`:
    **exit 0**, "All CI gates passed locally"; 133 Go `ok` lines, none `(cached)`, no `FAIL`; log
    `/home/mveary/sm-w0024/ci-local-3.log` (0600). The SPA build rewrote
    `frontend/app/dist/index.html` (alpha.3 Finding #1); restored, worktree clean, removed, pruned.
    No build (R0's artifact is the acceptance artifact).
20. **R2 guest preparation (agent).** `/home/mveary/sm-guest-w0024c` (0700, absent before). Image,
    CHECKSUM and `fedora.gpg` fetched again: "Good signature from Fedora (44)", key
    `36F612DCF27F7D1A48A835E4DBFCF71C6D9F90A6`; `sha256sum -c` OK; image SHA-256 `28680fe5…90b7f`
    (unchanged), read-only. 20 GiB overlay; guest-only ed25519 key; `cidata` seed (user `smtest`,
    key login, passwordless `sudo`). Domain `sm-w0024-clean3` on `qemu:///session`, 2 vCPU, 2 GiB;
    XML reviewed: one `passt` interface forwarding only `127.0.0.1:2222` → 22; no graphics, sound,
    shared folders or host devices. First boot: Fedora Linux 44 (Cloud Edition), kernel 6.19.10;
    cloud-init `done`; no `station-manager`, no `polkit`, no 9p/virtiofs mounts, `Linger=no`,
    nothing on 8080.
21. **R2 install (agent; install.md §2–§3).** RPM `scp`'d; in-guest SHA-256 `fe792081…2fd3`
    matching; `dnf install` exit 0 ("skipped OpenPGP checks"); `/usr/bin/smd` `8a2d89ec…2923`
    matching; `enable --now` 0; `sudo loginctl enable-linger smtest` → `Linger=yes` (lane deviation,
    as before); "SM is running."; `/v1/version` `2.0.0-alpha.5-89-gb766a145-dirty`, schema 19 clean,
    Home; pre-setup `default-logbook` `{"effective":{"id":1,"uuid":""…}}`; lookups seeded disabled
    (`hamnut`, `qrzcqlookupservice`, `qrzlookupservice` false). `tcpdump` installed, then the
    capture (entry 7's command and filter, unit `w0024-capture`) from **13:50:24Z**, before any
    callsign.
22. **R2 F5 fixture (agent).** Guest-local responder (`w0024-r401`, Python `http.server` on
    `127.0.0.1:18401`, every request answered 401); control POST → 401. With the guest daemon
    stopped, its `config.json` QRZ account given the dummy key `DUMMY-W0024-NOT-A-KEY` and every
    endpoint `http://127.0.0.1:18401/api` (loopback http is allowed, `qrz.go`
    `CheckCredentialedURL`); the Settings UI has no endpoint field. Restarted: instance
    `dmlmhuhdstp0`, no `error` line. No logbook is bound to it yet. Browser tunnel
    `127.0.0.1:18080` → guest 8080 open; throwaway profile `/home/mveary/sm-guest-w0024c/ff-smtest`
    (0700, empty).
23. **R2 browser steps 1–9 — agent-driven (Codex via Firefox WebDriver BiDi, headed Firefox
    159.0, the throwaway profile), not by the station operator personally.** Evidence:
    `/home/mveary/sm-w0024/guest-evidence-3-browser/` (`RESULTS.txt`, `transcript.jsonl`, 41
    screenshots, `MANIFEST.sha256`); no state changed by script or direct API write. Results as
    reported there:
    - **F4 PASS:** a visible focus ring on P's greyed Set as default and Default's greyed Delete
      (`04a`, `04b`; `04` shows P's Edit focus and is not the evidence).
    - **F6 default change PASS:** Stay, then Reload now → Stay, each kept the draft and the
      logging-paused gate; Reload now → Leave loaded a new document on T; one labelled synthetic
      contact saved in T (14:02:00Z, 20m) (`05b`–`05d`, `05j`, `05k`).
    - **F5 PASS:** Default effective, T deliberately selected, the Forwarding link opened F (`07a`,
      `07c`, `07d`). *Observation O10:* the link is "Show the QSOs … not on QRZ Logbook" — the
      failed count itself is plain text — and it applies the `missing_from` filter, not a
      failed-only filter.
    - **F6 archive switch PASS, both directions:** archive `W-0024 Archive switch` (first logbook
      `W-0024 Archive fixture`) created; Stay / Reload now → Stay kept the draft and gate; Reload now
      → Leave loaded the destination archive (`08b`–`08g`); no contact in the second archive.
    - **F6 Restart daemon PASS:** no Phone / CW entry; the first two restarts' Stay kept the draft
      and the same document (`performance.timeOrigin` unchanged); the third's Leave loaded a new
      document, draft empty (`09a`–`09d`). Instances `dmlmy8tl2jv0` → `dmlmzahrbms4` →
      `dmlmzoqfzud9` → `dmln02z4jyig`.
    - *Automation corrections (as recorded there):* an open Rig panel intercepted date/time clicks
      in step 5 and a shifted button index picked 6m in step 6; both corrected before saving; no
      extra contacts. *Observation O11* (`05h`): the open Rig Control panel covers the Phone / CW
      form's date and time fields, and the manual frequency draft accepted the text
      `14.25514:02:00.0` (shown in the header pill) before correction — triage pending.
24. **R2 corroboration (agent, from the guest).**
    - *Restarts (journal and `smd.log`):* default changes at 13:59:36Z (→ T), 14:05:12Z (→ F),
      14:08:08Z (→ Default); archive activation 14:11:11Z (→ second archive) and 14:12:17Z (→ Home);
      plain restarts 14:13:39.639Z, 14:14:10.568Z, 14:14:41.678Z — matching the three Restart daemon
      confirmations (14:13:39.648, 14:14:10.578, 14:14:41.688). Each exit `status=3` (the respawn
      code), each followed by a start 5 s later.
    - *Uploads (stopped daemon, `immutable=1`; integrity `ok` on all three databases):* Home
      logbooks Default 0, T 1, P 0, F 1; second archive's fixture logbook 0. **One `qso_upload` row**
      — F's contact, `insert`, `failed`, `failure_class` `auth`, `last_error` "QRZ returned HTTP 401
      (body: unauthorized)"; T's contact has none; the only enabled binding is F → `qrz`. As expected
      for the fixture.
    - *Responder:* six POSTs to `/api` — the agent's 13:50:25Z control, then 14:08:13Z (the first
      attempt, at the start after F's contact was queued at 14:07:20Z) and one at each later Home
      start (14:12:22Z, 14:13:44Z, 14:14:15Z, 14:14:46Z), each `outcome: terminal`: the boot re-arm
      of auth failures (W-0010 Outcome 9), by design; none during the second archive's start.
    - *Daemon log:* 785 `info`, 15 `warn`, **no `error`**. Warns: two pre-setup "could not record the
      default logbook" (O7; the second from the agent's fixture restart, before setup); eight "the v5
      recovery copy could not be written" — **caused by the agent's fixture**: the hand-edited QRZ
      account carried `api_key` at account level, a v5-shaped field (`hasLegacyForwarderFields`,
      `cmd/smd/config_v6_strip.go`), so each Home start tried the strip and could not build the v5
      copy; the browser step entered the key on F's binding as well, which the upload used. Not seen
      on the station (`smd.log` holds no such line). The fixture edit was unnecessary; a deviation,
      not a product finding. Five "forwarding: attempt" warns (the 401s above).
    - *Capture:* 13:50:24Z–14:40:42Z, entry 7's filter: **no matching traffic** (24-byte file). The
      responder is on loopback, outside the filter.
25. **R2 uninstall and disposal (agent).** Daemon stopped first; working directory hashed (6 files);
    `disable --now`, `dnf remove` exit 0, `/usr/bin/smd` absent, unit `not-found`; the working
    directory **byte-identical** after. Evidence kept in `/home/mveary/sm-w0024/guest-evidence-3`
    (0700; journal, responder log, `smd.log` archive, capture, hash list; `MANIFEST.sha256`). Guest
    destroyed and undefined with its storage; the tunnel ended with it; no domain left; guest
    files removed **except the throwaway Firefox profile** `ff-smtest`, still in use by the
    browser left open for inspection. 2026-11-04: browser closed (no process using the profile);
    `/home/mveary/sm-guest-w0024c` (94 MB, only `ff-smtest`) removed. Disposal complete.

### Revision 4 — R2 result

**R0, R1 and R2 complete.** F4 (visible focus), F5 (explicit target) and F6 (default change,
archive switch, Restart daemon) **passed** in the packaged guest, by agent-driven browser evidence
corroborated from the guest's logs and database; the reviewer rules the result. Retained from
candidate 2: F4's visible reasons, F5's effective-default selection and preserved pick. **U1–U7
unexercised, not waived.** W-0024 acceptance stays **incomplete**: R4–R6 (station AC-1 to AC-11)
await the station operator's outage window and explicit consent to discard all intervening
writes. Completing R4–R6 does **not** by itself close acceptance: U1–U7 stay unexercised and
unwaived, and the operator's sign-off is still required. Next is R3's passive station readings.

**Triage (reviewer recommendations, 2026-11-04; recorded in W-0012):**
- *Finding #2 (heading overlap):* the minimum-width and heading-clipping direction is supported.
  Acceptance needs readable headings and every column reachable by horizontal scrolling, also with
  the side panel open; only hiding the overlap is not enough.
- *O10:* keep the wording. The explanation beside it
  (`frontend/app/src/lib/config/DestinationsSection.svelte:530`, text at line 532) already says the link covers every
  missing QSO, not only failed uploads. Closed, no change.
- *O11:* split into two W-0012 items. **O11a**, the Rig panel covering the form, is provisionally
  P2. **O11b** is the malformed manual frequency. A code reading found that the submit sink refuses
  a value `parseFrequency` rejects (`frontend/app/src/main.ts:587`), so it cannot be logged; this
  was not exercised. Its severity is not assigned. The defects left are a refusal message that
  says the frequency is "not set" when it is malformed, and no malformed indication in the panel.

### Revision 4 — station checks

26. **2026-11-04 05:21:25–05:22:05Z — R3 Readings A (agent; station, daemon running, read-only).**
    HTTP GETs on `127.0.0.1:8080` plus local package and file reads only; no database file opened.
    Saved in `/home/mveary/sm-w0024/r3-readings` (0700, files 0600); `/v1/config` was not saved
    raw, only the lookup flags below.
    - *Identity:* `rpm -q` `station-manager-2.0.0~alpha.5.89.gb766a145.dirty-1.x86_64`;
      `/usr/bin/smd` SHA-256 `8a2d89ec…2923`, **equal to R0's**; `/v1/version` daemon
      `2.0.0-alpha.5-89-gb766a145-dirty`, env `release`, **schema 19, not dirty**, instance
      `dmm552w2b559`; unit active since 2026-11-04 04:27:38Z, main PID 4976.
    - *Health:* `/v1/healthz` status, database and logging all `ok`.
    - *Logbooks:* one, Default (id 1, `7Q5MLV`), **8,327 QSOs** (`/v1/logbook/1/count`).
    - *Archives:* Home (legacy, active, current, 23,425,024 bytes, modified 2026-11-03 15:48:53Z;
      Default is its default with 8,327); Drill (managed, inactive, stale; one QSO).
    - *Home bindings (Default):* ClubLog on and enabled; QRZ Logbook on and enabled; QRZCQ off
      (binding disabled); **SM Cloud on and enabled, adoption `adopted`, locked field `logbook`**
      (fixes AC-3's expectation). Every queue waiting 0, failed 0, in flight 0.
    - *Forwarder queues:* clublog, qrz, smcloud and qrzcq all 0 waiting, 0 failed, 0 clearable,
      0 in flight.
    - *Lookups:* HamNut enabled; chain `qrzlookupservice` enabled, `qrzcqlookupservice` disabled.
    - The counts and the archive's size and modification time are mutable. If the station logs
      before R4's stop, they are re-read then, so restore comparisons use the true baseline.
27. **2026-11-04 — R4 (agent; outage authorised by the station operator for 2026-11-04, no time
    limit, with explicit consent to discard every write between this backup and its restore,
    including test contacts, default and logbook changes and any real contact; rig off).**
    - *Pre-stop re-read, 05:27:18Z:* Default 8,327; Home 23,425,024 bytes, modified
      2026-11-03 15:48:53Z; Drill 1; every queue 0; instance `dmm552w2b559`. Unchanged from R3, so
      R3 stands as the baseline.
    - *Step 9:* `systemctl --user stop smd` 05:27:19.011Z, returned 05:27:19.054Z; `inactive`, `0`,
      `success`; `smd.log` shows the daemon's own exit ("shutdown signal received" …
      "HTTP server shutdown complete", "smd stopped").
    - *Step 10:* exactly the premigrate pair; `-wal` 0 B `e3b0c442…b855`, `-shm` 32,768 B
      `fd4c9fda…89eb`, byte-identical to the recorded hashes. Nothing else.
    - *Step 11:* `$W/baseline.sha256` (73 files), `$W/baseline.modes` (81 lines).
    - *Step 12:* `/home/mveary/sm-backup/station-manager-20261104T0527Z-gb766a145-pre-w0024.tar.gz`,
      mode 600, 35,280,173 bytes, 81 entries, SHA-256
      `ae7990f9eb8d655bd05ccefd5e44861487e5ae6cc9f06a55e70164b9c6542ed6`. **The only restore
      source.**
    - *Step 13:* extracted to `$W/pre`; hash and mode comparisons with the baseline both empty;
      `w0024-check.py integrity` `ok` for Home, Drill, `reference.db` and `evidence.db`, exit 0.
      Counts (Home): logbook 1 only, 8,327 QSOs; `qso_upload` clublog 2,554, qrz 2,874, smcloud
      11,102, all `uploaded`; destinations clublog 1, qrz 1, qrzcq 0, smcloud 1. Counts (Drill):
      1 QSO; one smcloud upload `uploaded`; smcloud binding enabled. `c-rows.py snap` written for
      Home, Drill and evidence (0600).
    - *Deviation — `reference.db` snapshot.* `c-rows.py snap` on the `reference.db` copy failed
      ("Could not decode to UTF-8 column 'name'"), with no output. Ruled 2026-11-04 (reviewer):
      take the snapshot through a wrapper that tags undecodable TEXT unambiguously, keeping AC-9's
      row comparison. The wrapper `$W/c-rows-tagged.py` (0600, SHA-256 `856bf07d…3b85`, retained
      for R6's snapshot) loads the helper unchanged and alters only TEXT decoding: valid UTF-8
      stays a string; invalid TEXT becomes `{"invalid_utf8_text_hex": "<hex>"}`. Opening stays the
      helper's read-only, immutable URI; output stays 0600, never overwritten. *Synthetic
      demonstration* (scratch databases): identical databases compare `unchanged`; changing an
      invalid TEXT byte (ff → fe), replacing it with the literal text `ff`, or with the BLOB
      x'ff', are each reported `CHANGED` (column `v`, one row); the source database's hash is
      unchanged and it gained no sidecars. *Reversion proof:* the same fixture snapshotted
      with plain hex for invalid TEXT (the helper's BLOB conversion,
      `build/private/c-rows.py:28`) reports both the literal-text and the BLOB replacement
      `unchanged`. *Pre-test snapshot:* `$W/pre-reference.json` (0600; `contacted_station` 11,019
      rows, `country` 664, `sqlite_stat4` 73). Tagged cells: **15, all `contacted_station.name`**,
      none elsewhere. The earlier 36 `sqlite_stat4` count was mine, wrong: those cells are BLOB
      samples, as SQLite's format defines (https://www.sqlite.org/fileformat2.html#the_sqlite_stat4_table).
      After the snapshot the copy still equals the baseline and has no new sidecar. The helper's
      own conversion still cannot tell a BLOB from TEXT spelling the same hex; that limit is
      unchanged and outside this ruling. **AC-11 met** (backup verified).
    - *Step 14:* the live directory re-hashed at 05:28:21Z and again at 05:35:13Z: identical to the
      baseline. The daemon stays stopped until R5.
    - *Correction:* in an earlier message I gave QRZ lookups during test entry as a reason
      `reference.db` would change. That is wrong. R5 step 16 disables every lookup, restarts the
      daemon, and verifies them off before any synthetic entry. Lookups are not an agreed
      explanation for `reference.db` changes.
28. **2026-11-04 — R5 step 15 (agent).** No install. `systemctl --user start smd` 05:35:58.256Z;
    healthy by 05:36:00Z; active, PID 37055. `smd.log` "smd starting" at 05:35:58.280Z with
    version `2.0.0-alpha.5-89-gb766a145-dirty`; `rpm -q` `…alpha.5.89.gb766a145.dirty-1`;
    `/usr/bin/smd` `8a2d89ec…2923`; `/v1/version` the same build, schema 19 not dirty, instance
    `dmm6ledjow37`, Home, `default_logbook_uuid` Default's; `/v1/default-logbook` 200, effective
    Default, no change in play. No `error` line since the start; warnings are the known start-up set
    (RTS assertion notice, SM Cloud plain-http notice), plus the rig liveness strikes expected with
    the rig off. **AC-1:** agent readings agree; the sidebar build badge is for the browser step.
29. **2026-11-04 — R5 step 16.** *UI (agent-driven browser, action record
    `/home/mveary/sm-w0024/station-evidence-r5-browser/RESULTS-step16.txt`, retained privately):*
    HamNut and QRZ disabled, saved, Settings → Restart daemon; QRZCQ already disabled; all three
    still Disabled after reload; sidebar build matches the acceptance artifact (AC-1's badge);
    no callsign entered, no test logbook created. *Agent corroboration, 05:53:45Z:* `smd.log`
    "config saved" 05:43:02.348Z; "restart requested (POST /v1/restart)" 05:43:34.805Z, orderly
    shutdown to "smd stopped" 05:43:34.834Z (systemd: exit status 3, the respawn code; restart
    05:43:39Z); "smd starting" 05:43:39.977Z; then "lookup: chain entry disabled, skipping" (×2)
    and "lookup: pipeline disabled (no providers enabled)". **`/v1/config`:** `hamnut.enabled`
    false; chain `qrzlookupservice` false, `qrzcqlookupservice` false. Instance `dmm6rah68wow`,
    schema 19; logbooks: Default only, 8,327; `/v1/default-logbook` effective Default; every queue
    0; no `error` line since R5's start.
30. **2026-11-04 — R5 steps 17–18; blocked before step 19 (Cancel check).** *Agent-driven browser
    record:* `/home/mveary/sm-w0024/station-evidence-r5-browser/RESULTS-steps17-22.txt` (retained
    privately). **Step 17 passed:** T and P created; P's callsign-mismatch reason shown; every
    forwarding destination disabled for both (AC-2). **Blocked:** with windows A, B and C open, A's
    Settings request timed out after 15 s, and C showed "Archive binding unproven". Direct daemon
    reads stayed healthy. Closing C and pressing Retry restored Settings; its config request then
    completed in about 1 ms. *Reviewer's hypothesis, not verified:* HTTP/1.1 per-origin connection
    exhaustion. Six Firefox connections were observed, which matches the per-browser SSE
    connection limit (https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events);
    which window held each socket was not established. The reviewer recommends triage before
    resuming. *Agent reading after the operator's power cut, 06:07:22Z:* daemon unaffected (active
    since 05:43:39Z, instance `dmm6rah68wow`). Logbooks: Default 8,327, `W-0024 Acceptance` (T,
    id 2) 0, `W-0024 Other call` (P, id 3) 0. `/v1/default-logbook` effective Default. Every queue
    0; lookups all disabled. No default switch, QSO submission or deletion has occurred. Windows A
    and B remain open (C closed). Step 23 and R6 not started; the R4 backup remains the only
    restore source. **Paused by the operator: continue later.**
31. **2026-11-04 — reboot observation (agent, read-only).** The host rebooted at 11:38:14Z, after
    the operator's power cut; the kernel changed (7.2.14 → 7.2.15). The daemon started by itself at
    11:38:39Z (instance `dmmeb350k68x`) with the paused state intact: Default effective at 8,327; T
    and P present and empty; every queue 0; lookups disabled; R4's backup present (unchanged).
32. **2026-11-04 — triage of entry 30 (reviewer, code reading; agent re-checked the citations; no
    station request).** With CAT enabled, each SPA window holds two persistent streams for the
    page's life: the shell's `/v1/events` (`frontend/app/src/main.ts:719`, "Never closed") and
    `/v1/rig/events` (`frontend/app/src/main.ts:418`; handler `internal/bridge/handler.go:102`).
    That stream opens independently of the view, and is not removed by powering off the rig. FT8/FT4
    adds one more while its view is active. Windows A, B and C therefore hold **six** streams. The
    daemon serves a plain listener via `http.Server.Serve` (`internal/api/server.go:595`), with no
    TLS or unencrypted HTTP/2, so browsers speak HTTP/1.1 (https://pkg.go.dev/net/http#Server.Serve).
    Firefox's six connections per host are shared across windows, and SSE holds connections that
    ordinary requests also need (https://bugzilla.mozilla.org/show_bug.cgi?id=906896). Six streams
    can leave Settings and archive-verification requests waiting, and closing C frees two. This
    strongly explains entry 30. Which socket each window held during the incident remains
    unmeasured. The code is unchanged since `b766a145`. **Ruling (reviewer recommendation, adopted
    by the operator):** AC-6 stays blocked. Closing B or C cannot show the simultaneous automatic
    reloads AC-6 requires. End this run through R6, hold R5 for a fix, and start a future run
    with a fresh R4 backup. The defect is tracked separately in W-0012.
33. **2026-11-04 — R6 (agent; step 24, installed artifact kept).**
    - *Stop:* readings at 11:44:02Z: Default 8,327, T 0, P 0. `systemctl --user stop smd` 11:44:02.291Z;
      `inactive`, `0`, `success`; `smd.log` ends "smd stopped". Sidecars: only the premigrate pair,
      at the recorded hashes.
    - *Preservation:* `$WD` moved to
      `/home/mveary/.local/share/station-manager.w0024-post-20261104T1144Z` (0755
      `mveary:mveary`); its 73-file hash list is `$W/r6-moved.sha256`. It is kept until the
      operator instructs deletion.
    - *Restore:* R4's backup re-hashed (`ae7990f9…2ed6`) and extracted. **While still stopped,** the
      hash and mode comparisons with `$W/baseline.sha256` and `$W/baseline.modes` were both
      **empty**. The restored files are byte-identical to the verified backup, so QSOs, bindings
      and the upload queue equal R4's counts.
    - *Start:* 11:44:20.157Z, healthy 11:44:23Z, instance `dmmeffujdtti`, the same build, schema
      19, Home, `default_logbook_uuid` Default's. Compared with R3's saved readings: `/v1/logbook`
      **equal** (Default only); count **equal** (8,327); lookups **equal** (HamNut and QRZ enabled,
      QRZCQ disabled); Home bindings **equal**, excluding queue counters. `/v1/default-logbook`:
      effective Default. Every queue 0; no `error` line; warnings only the two start-up notices.
      Home's `modified_at` reads 05:27:19Z, against R3's 2026-11-03 15:48:53Z: the file's mtime
      from R4's clean stop, which the backup preserved. Its content is the hashed baseline.
    - **The station is back to its pre-test state.** No synthetic contact was ever created; T, P,
      the disabled lookups and the step 16 restart are discarded under the operator's consent.

### Revision 4 — outcome of this run (2026-11-04)

**Run ended after R6; R5 held for a fix.** Passed in this run: R3, R4 (AC-11), R5 step 15 (AC-1),
step 16 (lookups off, verified) and step 17 (AC-2). **Not reached:** AC-3 to AC-10, step 23 and AC-9.
The cause is the multi-window connection defect, entries 30 and 32, tracked in W-0012. W-0024
acceptance remains **incomplete**; U1–U7 remain unexercised and unwaived; the operator's sign-off
is still required. A future run starts with a fresh R4 backup. The R4 backup of this run and
`$W/pre` are superseded for restore once that run's backup is verified. Their cleanup (step 26) is
the operator's call.

## Revision 5 — the W1 candidate (2026-11-05; ratified with amendments)

**Why a new revision.** Revision 4 evaluated the installed artifact and assumed no installation
(its R5: "no install"). The multi-window fix, ADR 0099 W1, is a new artifact. It moves the shell
and rig streams to WebSocket twins, keeps FT8 on SSE, and makes no schema change: the schema stays
19. This revision prepares that candidate, refreshes R0–R2 for it, and puts the fresh R4 backup
**before** installation.

**Mandatory pre-deploy gate** (`docs/dogfood-acceptance.md`, "Ready to deploy"). Live installation
needs all of the following first:
- the identified candidate (R0);
- its clean-install evidence (R2);
- a verified backup (R4);
- the operator's ratification of this revision.

Nothing below touches the station until then.

**Candidate.**
- **Commit.** The commit that adds the read-only binding marker (W1-2), on top of `c41302c4`, the
  Origin-before-admission change. CI must be green on it.
- **Clean build.** It is built in a clean scratch worktree, so the artifact is **not** `-dirty`.
  That removes revision 4's recorded A1 deviation.
- **Contents:** `3f7af877` (daemon WebSocket twins), `fa2cddf8` (SPA on WebSocket), `c41302c4`
  (Origin before admission) and the binding marker. There is no migration and no config change. One changed journey:
  multiple windows. Operator-visible behaviour is otherwise unchanged.

### Revised run sheet (revision 5)

- **R0 — build and preserve the candidate (no outage).**
  1. Clean scratch worktree at the candidate commit (`git worktree add`, outside the tree):
     `git status` clean.
  2. Build `SM_FFT=pocketfft scripts/dev-rpm.sh`.
  3. Copy the RPM to `build/private/` under its versioned name (absent before, 0600), and record its
     full SHA-256.
  4. Unpack it in `$W/unpack` (0700). Record `usr/bin/smd`'s full SHA-256 and its one embedded
     version string. Never run `smd` with a guessed subcommand.
     - **Sanity check only:** `strings` on the binary finds `/v1/events/ws` and
       `/v1/rig/events/ws`. The daemon's own route table contains those strings too, so this cannot
       show that the **SPA** uses WebSockets.
     - **The SPA proof is in R2:** the installed artifact's **served JavaScript** (the bundle
       fetched from the guest daemon contains both paths), and the browser connections the daemon
       logs in W1-1 by path.
  5. Remove the unpack directory.

  The **rollback artifact** is revision 4's preserved
  `build/private/station-manager-2.0.0~alpha.5.89.gb766a145.dirty-1.x86_64.rpm` (`fe792081…2fd3`).
  It is re-verified now.
- **R1 — A1 for the candidate (no outage).** `task ci:local` once in that worktree, exit 0
  recorded. The CI run on the candidate commit is recorded green. Then the worktree is removed.
- **R2 — A2 in a fresh guest (no live paths).**
  - **Guest.** Domain `sm-w0024-clean4` under `/home/mveary/sm-guest-w0024d` (0700), from the
    Fedora 44 Cloud image fetched again and verified (`gpgv`, then `sha256sum -c`). Only the R0 RPM
    is copied in, SHA-256 verified inside the guest.
  - **Containment (approved 2026-11-05).** The domain has **no network interface** and is
    controlled over AF_VSOCK, as in ADR 0099's W1 demonstrations. Proof: `virsh domiflist` empty;
    only `lo`; empty IPv4 and IPv6 routes; outbound attempts by IP and by name fail.
    - **What this proves:** external network containment. It does **not** prove that no lookup was
      attempted, so the disabled-provider checks stay: providers seeded disabled, verified in
      `GET /v1/config` before any synthetic callsign is typed, plus the journal and enrichment
      fields as corroboration.
    - **No packet capture.** Revision 4's capture is not needed for containment.
    - **Packages.** The guest cannot fetch packages, but offline installation from a host-staged,
      signature-checked package set remains possible (the method of ADR 0099's trust run sheet), if
      the run needs a tool the image lacks.
  - **Fixture for W1** (set after first-run setup, before W1-1):
    - **CAT bridge enabled**, so every non-Map window opens a rig stream. Settings → Rig control:
      a rig (FTdx10 definition) on a serial port that does not exist, `/dev/ttyPROBE-ABSENT`.
      Verify in `GET /v1/config` that the bridge is enabled. The serial open fails and no pipeline
      starts, yet `/v1/rig/events/ws` is served, as observed in the probes. No device means nothing
      can reach a rig.
    - **FT8 enabled** (`ft8.enabled`), with no audio device in the guest. The FT8 window then holds
      its SSE stream, but nothing is captured or transmitted.
    - **HamNut pointed at a guest-local holding responder.** For the held cases only; every other
      provider stays disabled.
  - **Steps:**
    1. **A2 clean install** as revision 4: install, enable, linger, health, version, logs, first
       run, setup with `7Q5MLV`, survival across reload and restart, lookups seeded disabled.
    2. **F6 re-run.** The default change, the archive switch and Settings → Restart daemon, each
       as revision 4 R2.1–R2.3. These reload and gate decisions now run on the WebSocket
       reconnect, so revision 4's F6 pass does not carry over.
    3. **F4 and F5: carried over, not re-run.** They are UI surfaces the W1 change does not touch.
       Their revision 4 evidence stands unless the reviewer rules otherwise.
    4. **W1-1 — four windows, held lookups (D1-ii, D2a).** Settings, Operate Phone/CW, Logbook,
       Operate FT8 (FT8 enabled in the guest), with a guest-local holding responder as HamNut:
       - streams by path from the daemon's subscriber log: 4 `/v1/events/ws`, 4
         `/v1/rig/events/ws`, 1 `/v1/ft8/events`;
       - the four ordinary GETs timed from Settings: baseline, then 1, 3 and 4 distinct held
         lookups, barrier-confirmed;
       - each request within 1 s.
    5. **W1-2 — real restart with full binding (D2b).**
       - **The observable: a read-only binding marker** (approved 2026-11-05), shipped in the
         candidate as its own reviewed commit before R0. The archive layer publishes four attributes
         on `document.documentElement`:
         - `data-sm-archive` — the window's actual bound archive UUID;
         - `data-sm-default` — its established default logbook UUID;
         - `data-sm-binding` — `verifying`, `verified`, `unresolved` or `unproven`;
         - `data-sm-verified-instance` — the daemon instance accepted by the latest
           **successful** binding verification.

         Its contract:
         - It reflects the existing context and binding gates. It never reports `verified` while a
           verification is pending or an existing gate blocks operations.
         - Success is recorded only after the identity and binding checks pass.
         - An older overlapping check never overwrites a newer one's evidence.
         - It is observational: no application decision reads it, and publishing it makes no
           request.

         The driver reads it passively in every window, the FT8 view included, with
         `script.evaluate`: no clicks, no requests.
       - **Why instance and not time.** A `verified-at` later than T0 is not a reliable test. A
         healthy verification can finish before the host observes readiness, and an old-instance
         response can finish afterwards. Instance identity distinguishes the two.
       - **Expected values.** Before the restart, read host-side from the daemon (`GET
         /v1/version`): the archive UUID and the effective default logbook UUID.
       - **Before the restart:**
         - each window's marker is `verified`, with the expected UUIDs;
         - a page marker is set in each window;
         - in Phone/CW, a labelled synthetic contact is **prepared**: fields filled, not submitted.
       - **The restart:** `systemctl --user restart smd`. The host polls `GET /v1/version` until a
         new instance answers. **T0** is that moment, on the driver's monotonic clock, and the new
         instance ID is captured with it.
       - **Per window — passes when, measured from T0 on the driver's monotonic clock:**
         - `data-sm-binding` is `verified`;
         - `data-sm-verified-instance` equals the **new** instance;
         - `data-sm-archive` and `data-sm-default` equal the expected UUIDs;
         - the existing checks hold: no gate overlay, the page marker intact (no reload), and all 9
           streams back in the daemon's subscriber log by path.

         A verification that completed **before** T0 with the new instance is acceptable. Requiring
         another one would reject a successful early recovery.
       - **Pass:** every window's recovery time is ≤ 10 s after T0.
       - **Corroboration:** once its window passes, the prepared Phone/CW contact is submitted. It
         is accepted, and the stored QSO's logbook is the expected default (`GET /v1/qso/{uuid}`).
       - **AC-6 is separate:** it still requires the expected reloads and the new destination
         bindings.
       - **No FT8 TX intents** are exercised.
    6. **W1-3 — negative control: optional.** It was already shown in ADR 0099's W1
       demonstrations, on the same build lineage. Rerun it only if the reviewer asks: the preserved
       `b766a145` RPM installed in the guest, starvation reproduced, then the candidate reinstalled
       and verified by hash.
  - **Evidence, uninstall and disposal** as revision 4.
- **R3 — Readings A (station, daemon running, read-only).** As revision 4: `rpm -q` and the
  installed `smd` SHA-256 equal `b766a145`'s (`8a2d89ec…2923`), the schema, and the readings.
- **R4 — stop, sidecars, baseline, fresh backup and verification (outage begins).** As revision 4
  R4, **before any installation**. The backup is named
  `station-manager-<UTC>-gb766a145-pre-w0024.tar.gz`, because it is taken with the old artifact
  installed. It is the only restore source. AC-11 is this backup verified, plus R0's candidate hash
  re-verified and the rollback RPM re-verified.
- **R5 — install and accept on the station (same outage).** Step 15 is restored:
  1. Re-verify the candidate's SHA-256.
  2. The operator installs: `sudo dnf install <candidate rpm>`; `systemctl --user daemon-reload`;
     `systemctl --user start smd`.
  3. **AC-1:** `rpm -q`, `/v1/version`, the sidebar badge and the startup log line agree on the
     candidate version; schema 19; Default effective; the journal has no `error`.

  Steps 16–23 then run as written: windows A, B and C, plus a fourth window on Operate FT8 for
  D1-ii, opened before step 19.
  - **This supersedes the "No RF" condition's "the FT8 view stays closed"** (top of this record)
    for this revision. The rest of that condition stays: **the rig stays off and TX stays
    unarmed throughout**; FT8 is opened as a view, never armed, and nothing is transmitted.
  - **The W1-2 marker** is read in all four windows across AC-6's reloads and restart, as the
    station's binding evidence. AC-6's reloads are the station's real-restart
  check. The post-test backup is named `…-g<candidate>-post-w0024.tar.gz`.
- **R6 — after the test.**
  - **Keep (recommended):** step 24 as written. Restore R4's backup with the candidate kept, verify
    while stopped, then start.
  - **Rollback, reinstated (approved 2026-11-05)** on any failure the operator calls, including an
    early one:
    1. **Stop and confirm stopped first:** `systemctl --user stop smd`; `systemctl --user show smd
       -p ActiveState -p MainPID -p Result` shows `inactive`, `0`, `success`; the journal shows the
       daemon's orderly exit. If it is not stopped, stop here and let the operator review.
    2. The preserved `b766a145` RPM's hash is re-verified (`fe792081…2fd3`; the reviewer matched it
       to the record, 2026-11-05). Then `sudo rpm -Uvh --oldpackage <rollback rpm>` and
       `systemctl --user daemon-reload`.
    3. With the daemon still stopped, move the live directory aside and restore R4's backup.
       Verify the **complete** snapshot against R4's hash and mode lists (both diffs empty).
    4. Start; `GET /v1/version` shows `2.0.0-alpha.5-89-gb766a145-dirty`, Home, Default.

    **Why this is compatible:** the candidate changes neither the schema (19) nor the
    configuration, and the complete snapshot taken under the rollback artifact is restored.
  - **Cleanup:** step 26 as written, including revision 4's superseded backup and `$W/pre` once
    this run's R4 backup is verified, on the operator's instruction.

**Unchanged:** U1–U7 stay unexercised and unwaived; the operator's sign-off is still required;
completing R4–R6 does not by itself close acceptance.

**Rulings so far (reviewer, 2026-11-05).**
- **Origin commit:** approved and pushed as `c41302c4`; its CI result is recorded with R1.
- **Containment:** a guest with no network interface is approved; the disabled-provider checks
  stay.
- **F4 and F5:** carry over; F6 is re-run.
- **Negative control:** the existing SSE negative control stands.
- **Rollback:** reinstated, with the stop-and-confirm step and the complete R4 snapshot.
- **W1-2:** not yet sufficient. It has been rewritten above; it now depends on the read-only
  binding marker.
- **R0:** the `strings` check is a sanity check only.
- **R5:** the FT8-view restriction is superseded; rig off and TX unarmed are kept.

**Ratified (reviewer, 2026-11-05), with these amendments incorporated above:**
- the binding marker is approved as a separate pre-R0 change, with `data-sm-verified-instance`
  replacing a timestamp, under the contract in W1-2;
- W1-2 passes on instance identity, timed from T0 on the driver's monotonic clock;
- the prepared contact stays as corroboration;
- AC-6 stays separate.

The marker receives its normal code review, and R0–R2 use the resulting candidate with green CI.
**Still open:** the outage window for R4–R6, and consent to discard intervening writes.

## Execution log — revision 5

1. **2026-11-05 — candidate and CI (agent).** Candidate **`b2a16f5a`**
   (`b2a16f5a0ca044b8eb2fb952f1ada5e500076ede`; the reviewer's choice, 2026-11-05). It differs from
   the marker commit `a1a8b92e` only by the run-sheet documentation. Pushed `c41302c4..b2a16f5a`. CI run
   **42008949059: success** on `headSha` `b2a16f5a0ca0…`. CI run 42003712188 was green on `c41302c4`.
2. **R1 — `task ci:local` (agent).** A clean detached worktree `/home/mveary/sm-w0024/wt-b2a16f5a`
   (outside the repository tree; `HEAD` `b2a16f5a0ca0…`; `git status --porcelain` empty).
   `task ci:local`: **exit 0**, "All CI gates passed locally"; 133 Go `ok` lines, none `(cached)`.
   The log is kept privately at `/home/mveary/sm-w0024/ci-local-b2a16f5a.log` (0600). As before,
   the CI mirror's SPA build rewrote the tracked `frontend/app/dist/index.html`; it was restored
   from `HEAD`, and the porcelain is empty again.
3. **R0 — build and preserve, at `b2a16f5a` (agent; the operator supplied the key).**
   - **The key.** The operator copied `.env` into the worktree (0600, `mveary:mveary`, verified
     byte-for-byte, contents not displayed; Git-ignored, so the worktree stayed clean).
   - **The build.** `SM_FFT=pocketfft scripts/dev-rpm.sh` exited 0: "Built
     2.0.0-alpha.5-110-gb2a16f5a", with no `-dirty` suffix. The log is kept privately at
     `/home/mveary/sm-w0024/build-b2a16f5a.log` (0600).
   - **The RPM:** `station-manager-2.0.0~alpha.5.110.gb2a16f5a-1.x86_64`, SHA-256
     **`7fc165ae1308fde19f49f91f07027351fb5440db47aa4072cd9f2fe72d5b876f`** (9,755,207 bytes).
     Preserved as `build/private/station-manager-2.0.0~alpha.5.110.gb2a16f5a-1.x86_64.rpm` (0600;
     absent before; SHA re-verified after the copy) **before** the worktree was removed.
   - **Unpacked** in `$W/unpack` (0700, removed after):
     - `usr/bin/smd` SHA-256 **`3c6915d9d4089ea32d103fde755193781b2fef5bae61a720bf5e340440d9c964`**;
     - one embedded version string, `2.0.0-alpha.5-110-gb2a16f5a`;
     - the file list as before: `smd`, `smctl`, `smd.service`, the manual, and the private
       marker file `usr/share/doc/station-manager/PRIVATE-BUILD-DO-NOT-DISTRIBUTE`. The marker is
       a packaged file, not a string in `smd`.
     - Sanity check only: `strings` finds `/v1/events/ws` and `/v1/rig/events/ws`. The SPA proof
       is in R2.
   - **Rollback RPM re-verified:** `fe792081…2fd3`.
   - **Cleanup.** The worktree's `.env` copy was `shred -u` (btrfs: the overwrite is not assured;
     the deletion is). The build had rewritten `frontend/app/dist/index.html` only; it was restored.
     The worktree was removed (`git worktree remove --force`, then pruned).
4. **R2 preparation (agent; nothing defined or booted).** `/home/mveary/sm-guest-w0024d` (0700):
   - **Image.** `Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2`, the CHECKSUM file and
     `fedora.gpg` fetched again from Fedora. `gpgv`: "Good signature from Fedora (44)", key
     `36F612DCF27F7D1A48A835E4DBFCF71C6D9F90A6`; `sha256sum -c --ignore-missing`: OK. Image SHA-256
     `28680fe5…0b7f`, identical to before; made read-only.
   - **Guest.** A guest-only ed25519 key; a `cidata` seed (user `smtest`, key login only,
     passwordless `sudo`); a 20 GiB overlay. Domain XML for `sm-w0024-clean4`: **no network
     interface**, vsock CID 3101.
   - **Package.** The RPM declares no package requirements (`rpm -qp --requires` is empty), so the
     offline install needs nothing staged.
5. **R2 — containment and install (agent; 2026-11-05 11:29–11:31Z).**
   - **Containment.** Domain `sm-w0024-clean4` defined and booted: Fedora Linux 44 (Cloud Edition),
     kernel 6.19.10, cloud-init `done`. `virsh domiflist` empty; in the guest only `lo`; empty IPv4
     and IPv6 routes; outbound by IP and by name both failed. No `station-manager` package,
     `Linger=no`, nothing on 8080.
   - **Install.** The R0 RPM was streamed in over vsock; in-guest SHA-256 `7fc165ae…876f`, matching.
     `dnf install --disablerepo='*'`: "Complete!" (skipped OpenPGP checks, a local file).
     `/usr/bin/smd` `3c6915d9…c964`, matching R0. `enable --now` exit 0; `loginctl enable-linger`
     → `Linger=yes`; `healthz` ok.
   - **Version.** `/v1/version`: `2.0.0-alpha.5-110-gb2a16f5a`, schema 19 clean, archive Home.
   - **Lookups seeded disabled:** `hamnut`, `qrzcqlookupservice` and `qrzlookupservice` all false.
6. **R2 step 1 — A2 first run: PASS (agent-driven; headless Firefox 159 over WebDriver BiDi,
   `acceptInsecureCerts: false`, a throwaway profile).** Evidence `a2-01`…`a2-06`,
   `transcript-a2-*.jsonl`.
   - **Before setup:** only the welcome card. Save disabled for empty and blank-only input; `7Q5!!`
     → "Invalid callsign format", still on the welcome card.
   - **Setup:** ` 7q5mlv ` normalised and saved; "Setup complete" offers **Open Settings** and
     **Start logging**. `Default` (`7Q5MLV`) created.
   - **Survival:** a reload, and a real `systemctl --user restart smd` (new instance
     `dmn8s3g3z3km`).
   - **Discoverable:** Settings, Manual, and the Phone/CW form.
   - **Marker:** `unproven` before setup (no default yet), then `verified` with the new instance.
7. **R2 fixture for W1 (agent; a config edit with the daemon stopped, recorded as a fixture
   edit).**
   - Rig `yaesu-ftdx10` on `/dev/ttyPROBE-ABSENT` (`default_rig_id` 1); `bridge.enabled`;
     `ft8.enabled`.
   - HamNut at a loopback holding responder for the W1 cases only. Verified in `/v1/config`:
     `bridge_enabled` true, `ft8_enabled` and `ft8_running` true; the QRZ providers false.
   - The serial open fails, "serial port not found", as intended. The rig WebSocket route is
     served.
8. **R2 W1-1 — four windows, held lookups: PASS.**
   - **SPA proof:** the served `/assets/index.js` contains `/v1/events/ws` and `/v1/rig/events/ws`.
   - **Streams by path** (daemon log): `/v1/events/ws` 4, `/v1/rig/events/ws` 4,
     `/v1/ft8/events` 1.
   - **Timings:** the four GETs from Settings took 1–5 ms at baseline and with 1, 3 and 4
     distinct, barrier-confirmed held lookups; 1–3 ms on recovery.
9. **R2 W1-2 — real restart, binding by marker: PASS.**
   - **Before:** all four windows `verified` with the expected archive `01a1ab79…1fe3` and default
     `01a1ab7a…38d0`.
   - **T0:** 54 ms after `systemctl --user restart smd`, the new instance `dmn8viu6d4e1` observed
     host-side.
   - **Recovery after T0** (monotonic clock): Settings 4,972 ms, Phone/CW 4,973, Logbook 4,974,
     FT8 4,975. Each with the marker `verified`, the new instance, the expected UUIDs, no gate, no
     reload, and all 9 streams back.
   - **What bounds the time:** the all-streams condition. The eight WebSockets reconnected about
     1 s after T0; the FT8 SSE stream came back at about 5 s.
   - **Corroboration:** the prepared contact `9X4W12` was submitted after recovery and stored in
     `Default` (QSO `01a1ab7e-8936-7a9c-a1bd-4affca1e7bb5`).
   - **Correction (review, 2026-11-05):** the earlier observation that Log QSO "was still
     disabled" at preparation is unsupported. The evidence's `log_enabled: false` is a driver
     defect. `bidi.mjs:42` JSON-parses the string `"true"` into the boolean `true`, and
     `r2-w1.mjs:108` then compares that boolean with the string `'true'`. So `log_enabled`
     records `false` whether the button was enabled or disabled. The original evidence is kept
     unchanged. W1-2 is unaffected: its binding checks, its restored streams and the stored
     contact are independent of this field. No guest rerun is needed.
10. **R2 step 2 — F6 re-run: PASS (after a void attempt).**
    - **Void attempt 1.** It stalled on a driver deadlock: "Reload now" was clicked inside
      `script.evaluate`, and the beforeunload prompt it opened blocked that call. Its first Stay
      matched the expected result. It is kept as `attempt1-*`. It had committed the default
      change to T, so attempt 2 first set Default back, with no unsaved edit. Every
      prompt-opening click is now dispatched asynchronously.
    - **How the edit was made.** The unsaved Settings edit (Operator → `7Q5MLVQ`) was **typed**
      through BiDi keyboard input. Firefox arms beforeunload only after real user activation.
    - **F6.1, default change to T:**
      - Stay: the same document, the edit intact, the "Reload now" gate showing, marker
        `unresolved`;
      - Reload now → Stay: the same;
      - Reload now → Leave: a new document on T, marker `verified` with T `01a1ab80…f9a1` and
        instance `dmn96adnbftw`, header "Logbook T";
      - a synthetic contact `9X4F61` stored in T.
    - **F6.2, archive switch** to `W-0024 R2 Archive switch`:
      - the same Stay, then Reload now → Stay results;
      - Reload now → Leave: on the destination archive `01a1ab8c…85b9`, with its own default,
        `verified`;
      - back to Home: a new document, `verified` on Home.
    - **F6.3, Settings → Restart daemon** (instances `dmn9740a1zrc` → `dmn97hg05m9a` →
      `dmn97l083odp` → `dmn97phe0rmn`):
      - restarts 1 and 2 → Stay: the same document (`timeOrigin` unchanged), the edit intact;
      - restart 3 → Leave: a new document, the edit gone as warned.
    - **F4 and F5** carry over (ruling).
11. **R2 corroboration (stopped daemon; `immutable=1` opens).**
    - **Databases:** `station-manager.db` integrity `ok`, 2 QSOs (`9X4W12` in Default, `9X4F61` in
      T), **0 `qso_upload` rows**. The second archive's database: integrity `ok`, 0 QSOs, 0 upload
      rows.
    - **Daemon log:** 1,476 `info`, 81 `warn`, 80 `error`. **Every error** is the fixture's
      "bridge: serial open failed; pipeline not started", one per serial open attempt. 80 warns
      are the matching RTS/DTR notice; one is the known pre-setup "could not record the default
      logbook" (O7).
12. **R2 uninstall and disposal (agent).**
    - **Uninstall.** The daemon stopped; the working directory hashed (6 files); `disable --now`
      exit 0; `dnf remove` complete; `/usr/bin/smd` absent; the unit not found. The working
      directory was **byte-identical** afterwards.
    - **Evidence:** `/home/mveary/sm-w0024/guest-evidence-5` (0700, 46 files, `MANIFEST.sha256`):
      transcripts, screenshots, the journal, the responder journal, `smd.log`, the hash list,
      the drivers and the domain XML.
    - **Disposal.** Firefox stopped, the tunnel closed, the domain destroyed and undefined, and
      `/home/mveary/sm-guest-w0024d` removed. The ADR 0099 control guest was verified
      byte-identical.

**R0–R2 complete for candidate `b2a16f5a`.** Next: the station's R3–R6, which need the station
operator's outage window and consent to discard intervening writes. W-0024 acceptance stays
**incomplete**; U1–U7 stay unexercised and unwaived; the operator's sign-off is still required.

### Revision 5 — station run (2026-11-05)

Evidence for every entry below is in `/home/mveary/sm-w0024/r5-station` (0700; files 0600),
subdirectories `r3`, `r4`, `r5`, `r6` and `browser`. The browser was agent-driven: headless
Firefox 159.0 with a fresh profile, over WebDriver BiDi, through one persistent session. The
transcript, every command and its result are in `browser/ev/transcript.jsonl`, with screenshots
alongside. The driver's `eval` returns values unconverted, so entry 9's coercion defect cannot
recur.

13. **Authority.** The station operator authorised the outage on 2026-11-05 starting now, and
    explicitly consented to discarding every write made after R4's backup when it is restored,
    including real contacts and configuration changes. R6 restores that backup on either path.
    The rig stayed off and TX unarmed throughout. CI on `b2a16f5a` was green (R1).
14. **R3 Readings A, 13:29:14Z (read-only).**
    - *Package:* `rpm -q` `…alpha.5.89.gb766a145.dirty-1`; `/usr/bin/smd` `8a2d89ec…2923`.
    - *Daemon:* `/v1/version` `2.0.0-alpha.5-89-gb766a145-dirty`, schema 19 not dirty, Home,
      instance `dmmyxjvwxwih`. Healthz `ok`.
    - *Logbooks:* Default only, **8,428 QSOs** (8,327 in revision 4: real contacts logged since).
    - *Home bindings:* byte-equal to revision 4's reading. ClubLog, QRZ and SM Cloud are enabled
      (SM Cloud `adopted`); QRZCQ is disabled.
    - *Queues:* all 0.
    - *Lookups:* HamNut and QRZ enabled; QRZCQ disabled.
15. **R4 — stop, sidecars, baseline, backup, verification (outage begins).**
    - *Pre-stop re-read, 13:29:39Z:* Default 8,428; queues 0; instance unchanged. The archive line
      of this re-read failed on a `jq` path error; R3's reading 25 s earlier stands for size and
      modification time.
    - *Step 9:* stopped 13:29:39.663Z–.721Z; `inactive`, `0`, `success`; `smd.log` ends with
      "HTTP server shutdown complete", "smd stopped".
    - *Step 10:* exactly the premigrate pair, at the recorded hashes.
    - *Step 11:* `r4/baseline.sha256` (74 files) and `r4/baseline.modes` (82 lines).
    - *Step 12:* the backup.
      - **Path:** `/home/mveary/sm-backup/station-manager-20261105T1329Z-gb766a145-pre-w0024.tar.gz`.
      - **File:** mode 600, 36,774,100 bytes, 82 entries.
      - **SHA-256:** `ffb38a6823b88b5fae321973dc6a14d64d5c4c9f34b0a6209fcbcc876929eae8`.
      - **The only restore source.**
    - *Step 13:* extracted to `r4/pre`; the hash and mode diffs were both empty.
      - **Integrity:** `w0024-check.py integrity` `ok` on all four databases, exit 0.
      - **Counts (Home):** 8,428 QSOs. `qso_upload`: clublog 2,655, qrz 2,975, smcloud 11,304,
        all `uploaded`. Destinations: clublog 1, qrz 1, qrzcq 0, smcloud 1.
      - **Counts (Drill):** 1 QSO, with one smcloud upload.
      - **Snapshots:** Home, Drill and evidence through `c-rows.py`; `reference.db` through the
        ruled wrapper `c-rows-tagged.py` (`856bf07d…3b85`, re-verified).
      - **After the snapshots:** the copy still equals the baseline.
    - *Step 14:* the live directory was re-hashed at 13:30:10Z: identical.
    - **AC-11 met:** the backup is verified, and both RPMs were re-verified just before the stop
      (candidate `7fc165ae…876f`, rollback `fe792081…2fd3`).
16. **R5 step 15 — install and AC-1.**
    - *Install:* the station operator ran `sudo dnf install` on the candidate RPM, since the agent
      has no `sudo`: "Upgrading … 2.0.0~alpha.5.110.gb2a16f5a-1, replacing …89.gb766a145.dirty-1",
      "Complete!", OpenPGP checks skipped (a command-line package).
    - *Identity:* `rpm -q` `…alpha.5.110.gb2a16f5a-1`; `/usr/bin/smd` `3c6915d9…c964`, **equal to
      R0's**.
    - *Start:* `daemon-reload`; started 13:41:46.786Z; healthy 13:41:51.848Z.
    - **AC-1 pass:**
      - `smd.log` "smd starting" with version `2.0.0-alpha.5-110-gb2a16f5a`;
      - `/v1/version` the same build, schema 19 not dirty, Home, Default's UUID;
      - `/v1/default-logbook` effective Default, no change in play;
      - Default 8,428; queues 0;
      - the sidebar build badge `2.0.0-alpha.5-110-gb2a16f5a` (`browser/ev/16-00-settings-start.png`);
      - no `error` line. The warnings are the known start-up set (RTS/DTR notice, SM Cloud
        plain-http notice) and the rig-off liveness strikes.
17. **R5 step 16 — lookups off (UI).** Window A, Settings → Enrichment: HamNut and QRZ.com
    unticked, then Save. The restart-owed banner appeared, and its Restart daemon was confirmed.
    - **Before the restart:** `/v1/config` already showed `hamnut.enabled` false, with
      `qrzlookupservice` and `qrzcqlookupservice` false.
    - **The restart:** instance `dmnbjwn1fe6t` → `dmnblpejfx25` (13:44:08Z).
    - **After the restart:** `smd.log` "lookup: chain entry disabled, skipping" ×2 and "lookup:
      pipeline disabled (no providers enabled)", with `/v1/config` unchanged (`r5/step16-lookups.json`).
    - **Ordering:** no callsign had been typed before this point.
18. **R5 step 17 — T and P, AC-2: PASS.**
    - **Created through the Add form:** T `W-0024 Acceptance` (id 2, `7Q5MLV`, the specified
      description) and P `W-0024 Other call` (id 3, `7Q5MLV/P`). Upload to SM Cloud stayed
      unticked for both.
    - **Set as default:** Default has none. T's is offered. P's is `aria-disabled`, titled "Its
      callsign 7Q5MLV/P is not the station callsign 7Q5MLV: only a logbook for the station
      callsign can be the default." Clicking it showed that reason and opened no prompt.
    - **Descriptions:** T's shows under its name.
    - **Bindings:** every destination is unbound and disabled for T and P (`r5/step17-bindings.json`).
19. **R5 step 18 — windows B, C, D.** B is Operate → Phone / CW, C is Logbook, and D is Operate →
    FT8, opened before step 19.
    - **All four windows:** marker `verified`, instance `dmnblpejfx25`, Home, Default; no gate.
    - **Streams by path:** 4 `/v1/events/ws`, 4 `/v1/rig/events/ws`, 1 `/v1/ft8/events`.
    - **D (FT8):** sent `POST /v1/ft8/claim`, the view's presence claim. `smd.log`: "ft8: capture
      deferred — rig/CAT not live". Nothing armed, nothing keyed.
    - **B's partial entry:** callsign `7Q5MLV`, typed as real keystrokes, never logged.
    - **Page markers:** set in all four windows.
20. **R5 step 19 — the confirmation and its Cancel: AC-3, AC-4 and R1 PASS.**
    - **Setup:** window A went in-app to Phone / CW and typed the partial `7Q5MLV`, then in-app to
      Settings → Logbooks, where it typed an unsaved Add name (`Draft W24`).
    - **AC-3:** Set as default on T opened: "Set ‘W-0024 Acceptance (7Q5MLV)’ as default and
      restart Station Manager?"
      - It names the target and the effect.
      - "This window has an unlogged QSO on Phone / CW: 7Q5MLV. OK discards this window’s entry
        and restarts; Cancel keeps it."
      - "The reload loses the unsaved Settings edits in this window: Logbooks."
      - Other windows' unlogged work is lost.
      - Transmit must be idle and FT8 disarmed.
      - **No SM Cloud note**, as R3 fixed it: Default's SM Cloud binding is `adopted`.
    - **AC-4:** Cancel.
      - The browser's network events after the dialog opened show only a bindings `GET`.
      - The daemon log has no non-GET request after 15:44:53 local time.
      - The instance is unchanged, Default is still effective, the Add draft is intact.
      - Opening the confirmation again showed the same entry line, so the partial entry was
        intact. Cancel was pressed again, again with no non-GET request.
    - *Driver deviations, no effect:*
      - The first in-app click aimed at a hidden Phone / CW button and did nothing. The six
        keystrokes meant for it went to `<body>`: no request, no unsaved change.
      - Leaving Settings to look at the partial entry raised the SPA's own "Unsaved changes in
        Logbooks will be discarded. Leave Settings?" guard. It was answered Cancel, keeping A on
        Settings with the draft.
21. **R5 step 20 — switch to T: AC-5, AC-6 and R2 PASS.**
    - **The switch:** Set as default on T → OK at 13:49:00Z, with A's draft and entry kept, as
      the dialog had warned.
    - **The restart:** `smd.log` "default logbook change requested; live entry and transmit
      admission sealed, restarting", "smd stopped", "smd starting", "startup: the default logbook
      change was applied". The new instance `dmnbpi2pazxc` answered at 13:49:05.8Z.
    - **R3, the pending banner:** B showed "The daemon now serves another default logbook; this
      page must reload to rebind." and "Logging and transmitting are paused" before it reloaded.
    - **A:** Firefox's leave-page prompt opened in A, the only window with keyed unsaved Settings
      edits. This is the F6 behaviour seen in R2. It was answered Leave, as OK had warned.
    - **B, C and D:** they reloaded with no prompt.
    - **After the reloads:**
      - every page marker is gone and each window is a new document;
      - all four markers are `verified`, with instance `dmnbpi2pazxc` and default T;
      - every header names "W-0024 Acceptance (0)";
      - B's partial entry is gone;
      - no gate.
    - **Streams:** all nine were back by 15:49:07 local time (about 1.2 s after the new
      instance), and A's again after its Leave.
    - **The daemon:** `/v1/default-logbook` shows T effective (`state: effective`). Settings →
      Logbooks: "“W-0024 Acceptance” is now the default." (R2). Delete on T is refused: "The
      Default logbook can’t be deleted".
22. **R5 step 21 — two contacts and delete: AC-7 and AC-10 PASS.** Window B, Rig panel in manual
    mode ("Manual — confirm to log"). Tune was never pressed. No request was made by the panel
    actions.
    - **Contact 1:** 20m, USB, 14.255, Confirm; `7Q5MLV` typed; Time On 13:49:00, today; the
      labelled comment; Log QSO.
    - **Contact 2:** 40m (7.100.000, USB kept), Confirm; Time On 13:50:00; Log QSO.
    - **Requests:** each produced exactly one `POST /v1/qso?logbook=2` and the notice "Logged
      7Q5MLV".
    - **Stored:** `01a1abfa-2dc4-7630-8635-ed750189b02f` (20m SSB/USB, 134900) and
      `01a1abfa-d913-7ab9-9004-6f5531fd1573` (40m SSB/USB, 135000), both in T with the comment.
    - **Counts:** T 2, Default 8,428 unchanged.
    - **AC-10:** Delete on P: "Delete the logbook “W-0024 Other call”? It holds no QSOs." OK →
      `DELETE /v1/logbook/3`; P gone. Together with T's refusal, AC-10 passes.
23. **R5 step 22 — switch back: AC-8 PASS.** Set as default on Default → OK, from window A with no
    unsaved edits.
    - **The restart:** new instance `dmnbrwo30o2e` at 13:52:14Z.
    - **The windows:** all four reloaded by themselves with **no prompt**. Each is `verified` on
      the new instance with Default, and every header names "Default (8,428)".
    - **No contact was logged:** Default 8,428, T 2.
    - **Streams:** 4 + 4 + 1 live after the reloads, by connect and disconnect count.
    - **The daemon log:** no `error` from install to stop. The warnings are the start-up and
      rig-off set only.
24. **R5 step 23 — post-test snapshot: AC-9 PASS.**
    - **Before the stop:** the browser session was ended and Firefox closed.
    - **The stop:** 13:53:28Z, clean (`inactive`, `0`, `success`, "smd stopped"); the sidecars are
      the premigrate pair only.
    - **The backup:**
      - **Path:** `/home/mveary/sm-backup/station-manager-20261105T1353Z-gb2a16f5a-post-w0024.tar.gz`.
      - **File:** mode 600, 36,800,742 bytes, 82 entries.
      - **SHA-256:** `12873abdd6245f4f8ead3cdfc679d46cda518e675f917dce3cdbfd1f3655f0c8`.
    - **Verification:** extracted to `r6/post`; the hash and mode diffs against the stopped live
      directory were both empty; integrity `ok` on all four.
    - **Diffs, pre → post:**
      - **Home:** `qso` +2, both `logbook_id` 2 (T). `logbook` +2: T active, P with `deleted_at`
        set.
        - Unchanged: `logbook_destination`, `operator_event`, `qso_history`, `qso_upload` (16,934
          → 16,934), `archive_metadata` (pointer `default_logbook_id` 1, Default) and
          `contest_serial_allocation`.
        - Pre-existing rows: no `qso` or `logbook` row changed.
        - Changes from SQLite's statistics and autoincrement bookkeeping: `sqlite_stat1`,
          `sqlite_stat4` and `sqlite_sequence`.
      - **Drill:** unchanged.
      - **`evidence.db`:** `profile_active` reports changes, yet its band → profile mapping is
        identical as a set. Only the rowids moved: each start's `reconcileProfiles` deletes the
        table and reinserts it from a Go map (`internal/evidence/profiles.go:276-284`), whose
        iteration order varies. All other tables are unchanged.
      - **`reference.db`:** `contacted_station` +1, a row for `7Q5MLV` with country `Unknown`,
        created at 13:51 (the contact times). It is the QSO submit path's best-effort cache warm
        after commit (`internal/qsoservice/submit.go:557`), not a lookup: lookups were off, which
        the `Unknown` country corroborates. Otherwise only the SQLite statistics changed.
    - **Uploads:** `w0024-check.py uploads` on Home's pre and post snapshots: added QSOs
      `8429` and `8430` in logbook 2; **0 `qso_upload` rows** name them; exit 0.
    - **Change record:** the post copy's `config.json` holds no `default_logbook_change` key, so
      no record is in play on disk. The running instance had reported the settled change
      (`state: effective`) behind R2's notice.
    - *Agent error, corrected:* the first `uploads` call was given database paths instead of
      snapshots and failed to parse. It was rerun with the snapshots.
25. **R6 — step 24, the candidate kept.**
    - **Stopped first:** confirmed `inactive`, PID 0.
    - **The backup:** R4's backup re-hashed: equal.
    - **The move:** `$WD` moved to
      `/home/mveary/.local/share/station-manager.w0024-post-20261105T1354Z` (0755
      `mveary:mveary`; 74-file hash list `r6/moved.sha256`). It is kept until the operator
      instructs deletion.
    - **The restore:** R4's backup extracted. **While still stopped**, the hash and mode diffs
      against `r4/baseline.*` were both **empty**.
    - **The start:** 13:54:58.550Z; healthy 13:55:03Z; instance `dmnbu0dbvgnp`, build
      `2.0.0-alpha.5-110-gb2a16f5a`, schema 19, Home, Default.
    - **Compared with R3:** `/v1/logbook` **equal** (Default only); count **equal** (8,428);
      `/v1/default-logbook` **equal** (no change record in play); lookups **equal** (HamNut and
      QRZ enabled, QRZCQ disabled); Home bindings **equal** apart from queue counters. Queues 0.
      Warnings are the two start-up notices only.
    - **The station runs the candidate on its pre-test data.** The synthetic contacts, T, P, the
      lookup change and the three restarts are discarded under the operator's consent.

### Revision 5 — station result

**R3–R6 complete; AC-1 to AC-11 passed on the station.** The two former blockers are cleared:
- **AC-6's simultaneous reloads with windows B and C open, plus an FT8 window:**
  - On the first switch (to T), B, C and D reloaded automatically, without a prompt. Their
    streams were back about 1.2 s after the new instance answered.
  - Window A held Firefox's leave-page prompt because of its keyed unsaved Settings edits. It
    completed its reload only after the agent answered Leave, later and separately from that
    1.2 s observation.
  - On the switch back, all four windows reloaded automatically, without a prompt.
- **W1-2's binding evidence, in all four windows across AC-6's restarts:** each reached
  `verified` on the new instance with the expected archive and default.

**Open:**
- **CI is red on `62bec1f0`** (docs only, after the candidate). In the release gate's race step,
  `TestWSStream_ShutdownSends1001AndWaits` failed: "peer saw close StatusCode(-1), want 1001"
  (run 42036078646). The test is in the candidate's daemon change (`3f7af877`). It passed on
  `b2a16f5a` (CI and R1) and in 300 local race runs. The cause is not yet established.
- **U1–U7** stay unexercised and unwaived.
- **The operator's sign-off** is still required. W-0024 acceptance stays **incomplete** until
  then.
- **Cleanups (step 26) are the operator's call:** this run's `r4/pre` and `r6/post` copies, the
  moved post-test directory, revision 4's superseded backup and its `$W/pre`.
