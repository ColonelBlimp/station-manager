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
Backup location (do not record secrets): pending (A3-01)
Restore check: pending (A3-02)
Recovery policy: as alpha.4. The candidate adds no migration and no configuration change over
alpha.4 (release delta), so it also adds none over the installed build, and a package rollback to
the preserved dev RPM needs no data downgrade.
Rollback artifact/command: pending (A3-04)

Ready to deploy: pending
Dogfood accepted: pending
Residual waivers: the alpha.2 B1-01 waiver, to be retired by A1-05 plus B1-01 on this artifact
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
| `A3-01` … `A3-05` | pending, unchanged definitions | Never executed at alpha.4 | pending | pending |
| `A4-01` | **revised** — ratify the B2 changes below | The B2-10 and B2-11 expected results change with the fix; four cases are added (B2-17 … B2-20). The A4 parameters carry forward | Revised inventory with two amendments (B2-18's verifiable legacy fixture; B2-19 and B2-20 as automated-evidence rows for FT8 and email) and the guest sequence in Scope | **PASS** — Codex inventory review requested by the operator, 2026-10-24. This ratifies the case definitions and the guest sequence, not their results or either release gate |

## Gate B — deploy and accept

B1 rows (`B1-01` … `B1-06`): pending, the alpha.4 definitions run on this artifact (none was
executed at alpha.4).

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

### B2 cases revised or added for alpha.5 (ratified at A4-01, 2026-10-24)

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `B2-10` | yes | **clean-guest** — B2-09; switch to the other archive in the same tab (it reloads); log one **distinguishable** QSO there | Read the Session panel and the rail badge; look for any Home row to edit; export, then **open the downloaded file** | Panel and badge show only the one new QSO; no Home row is offered for edit; the file holds exactly that one record and the notice reads "Exported 1 QSO to ADIF" | Home's rows shown or offered for edit here; a notice whose count differs from the file's records. (FT8 engagement is not observable on this guest: B2-19) | pending | none | pending |
| `B2-11` | yes | **clean-guest** — B2-10; switch back to Home in the same tab | Read the panel; edit a Home row (save and reload), and export | Home's two rows are back and the other archive's row is absent; the edit saves to Home (`GET /v1/qso/{uuid}`); the file holds Home's two records and the notice reads "Exported 2 QSOs to ADIF" | Home's list lost on return; the other archive's row listed; an edit written to the wrong archive | pending | none | pending |
| `B2-17` | yes | **clean-guest** — Home active, three Session rows; delete one of them from the Logbook (the QSO is gone from Home) | Export from the Session dialog and open the file; export again | Both exports: the file holds the two remaining records and the notice is a warning, "Exported 2 of 3 selected QSOs. 1 could not be found in this archive."; the deleted row stays listed | "Exported 3 QSOs"; the row silently dropped from the list; a missing row described as deleted | pending | none | pending |
| `B2-18` | yes | **clean-guest, legacy lane** — kept separate from the fresh-install lane (A2-01 … A2-07). **Fixture, prepared while alpha.4 is still installed in the guest:** an alpha.4 working directory whose archive holds Phone / CW QSOs logged from one tab; that tab stays open at `http://127.0.0.1:18080` (same origin, through the tunnel) and is never closed, so its `sessionStorage` survives. Session `sessionStorage` lasts only as long as the tab: a closed tab, a closed browser or a power cut loses it (alpha.4 record, B2-09 attempt 1). **Verify before the first alpha.5 load,** read-only in that tab's DevTools console: `sessionStorage.getItem('sm.session.qsos')` is non-null and lists the logged UUIDs, and no `sm.session.qsos.<uuid>` key exists; `GET /v1/qso/{uuid}` returns each of them. Then upgrade that lane's package in place to the alpha.5 RPM (its data kept) and restart its daemon, without touching the tab. If the fixture cannot be prepared or is lost before the load, recreate it from the held alpha.4 RPM (`cebcd305…`); otherwise record the case **blocked** | Reload that tab once; read the notices, the Session panel and the Logbook; reload again; read `sessionStorage` again | One notice after the first reload: "Session history was reset by this update. Your logged QSOs are intact in the Logbook."; the Session panel is empty; the Logbook holds every fixture QSO; no notice after the second reload; `sm.session.qsos` is gone and `sm.session.qsos.<uuid>` holds an empty list or is absent | The old list adopted into the archive's Session list; the notice on every load; QSOs missing from the Logbook; a fixture that was never verified counted as a pass | Execution log #5. 2026-10-24, guest clock stepped first; `smtest2` lane (alpha.4, Home, `Default` 8); new empty profile `ff-b2l` (0700), one tab at `127.0.0.1:18080`. Two Phone / CW QSOs logged: 7Q7EB `01a16d3b-1bc9-7e7d-b0a4-6ece15286ec7` 09:25:35Z and 7Q7CT `01a16d3b-378f-7186-bb88-8b4c66ad4524` 09:25:57Z; `Default` 10. **Fixture verified before the alpha.5 load:** the DevTools Storage inspector (Firefox's console blocks pasted code) showed `sm.session.qsos` holding both rows and, besides it, only `sm.session.startedAt`, no scoped key; `GET /v1/qso/{uuid}` 200 for both. Package upgraded in place to alpha.5 (09:29:28Z, `/usr/bin/smd` `b91bc34b…`), `smtest2`'s daemon restarted (instance `dmcyo9gpkfen`, Home); the tab stayed open and did not reload by itself. **First reload (F5, operator):** the reset notice shown once; Session panel empty. **Second reload:** no notice. Storage afterwards: only `sm.session.startedAt` (the unscoped key gone; no scoped key written, which the row allows). Home still `Default` 10 with both UUIDs (`GET /v1/qso/{uuid}` 200, 09:31Z, read through the daemon rather than the Logbook view) | none | **PASS** — operator, 2026-10-24: fixture verified before alpha.5 loaded, the notice once, the legacy list gone, both durable QSOs retained. Deviations accepted: the Storage inspector reads the required state; QSO retention is **API evidence**, not a visual Logbook check; manual F5 is the prescribed action |
| `B2-19` | yes | **record** — automated evidence from A1-01's `ci:local` at the alpha.5 tag | Cite the archive-boundary and engagement tests (list below) and confirm each is present at the tag and passing in that run | Every cited test present and passing | Read as a browser or on-air pass: the FT8 repeat decision runs in TX preflight and row shading reflects separate enrichment evidence, so neither is observable on this guest | pending | none | pending — **automated evidence** |
| `B2-20` | yes | **record** — automated evidence from A1-01's `ci:local` at the alpha.5 tag | Cite the export and email accounting tests (list below), including partition rejection and stamp failure, and confirm each is present at the tag and passing in that run | Every cited test present and passing | Read as a manual email pass; the guest has no SMTP account | pending | none | pending — **automated evidence** |

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
| 1 | A2-06 | Operator, 2026-10-24: working from the guide and the manual alone, there is no Settings link or instruction to find. The manual's First Run chapter (`manual/content/chapters/first-run.md`), which should hold the tour of the main screen, is still a placeholder ("*Draft outline — content to be written.*"); `my-station.md` does not mention Settings; Settings appears only in passing ("Settings → Rigs" in `cat.md`, the sidebar order in `station-events.md`). The app itself shows Settings in the sidebar and offers **Open Settings** at the end of setup (A2-04), so no repository knowledge is needed; what fails is the manual. **Not a regression:** the alpha.4 tag has the identical placeholder (this delta changed only `qso-archives.md`), and alpha.4's A2-06 passed without recording the gap. | **Not blocking; deferred** — operator, 2026-10-24: the manual is well out of step with the app and is left for a separate, focused piece of work; the priority now is getting the app ready for a beta release | [W-0018](../work/W-0018-bring-the-embedded-manual-to-release-readiness.md) (its outcome: "an operator can complete first run … from the manual alone") |

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
