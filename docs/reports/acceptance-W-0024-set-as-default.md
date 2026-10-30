# W-0024 acceptance — Set as default (revision 3, for ratification)

Status: **ACCEPTANCE INCOMPLETE — A3 ON HOLD. Deployment is not approved.** F4 and F5 are to be fixed (reviewer, 2026-10-30), followed by a new artifact and the affected reruns. Revision 1's no-RF scope was approved with
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
