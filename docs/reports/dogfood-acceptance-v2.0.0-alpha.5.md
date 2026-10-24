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
RPM filename: pending (A1-01); expected `build/private/station-manager-2.0.0~alpha.5-1.x86_64.rpm`
(mode 0600)
RPM SHA-256: pending (A1-01)
RPM-reported version: pending (A1-01); expected `station-manager 2.0.0~alpha.5-1 x86_64`
Previous installed version: unchanged from alpha.4, the dev build
`station-manager-2.0.0~alpha.3.262.g6217da21.dirty-1.x86_64` (alpha.4 was never installed on the
live station). By RPM ordering alpha.5 is an upgrade from it: pending check (A1-01)
Clean-install environment: the guest `sm-alpha4-clean` (alpha.4 record, Appendix 3), a fresh lane
for this artifact (A2-01)
Upgrade environment: the daily-driver dogfood station (this host)
Browser(s) and viewport(s): Firefox 158.0, the A4 parameters accepted at alpha.4 A4-01
Operator: 7Q5MLV
Automated gate evidence: pending (`task ci:local` at the tag, A1-01)
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
| `A1-01` | rerun | Candidate identity and build check: the artifact must be alpha.5's | pending | pending |
| `A1-02` | rerun | Version readings identify the artifact | pending | pending |
| `A1-03` | **revised** — review the delta above | A new delta, alpha.4 → alpha.5 | Delta drafted above | pending |
| `A1-04` | rerun | Run with the artifact's `smd`. No configuration code changed, so a different result would be a finding in itself | pending | pending |
| `A1-05` | rerun | Retires the B1-01 waiver only with this artifact's `smd` | pending | pending |
| `A2-01` … `A2-07` | rerun, as one clean-lane pass | Installation evidence must identify alpha.5, and the first-run SPA boot changed (`main.ts`, `archives.svelte.ts`: store binding and the action gate) | pending | pending |
| `A2-08` | rerun | Installation evidence (alpha.2 → candidate upgrade). No migration changed, so the expected counts are alpha.4's | pending | pending |
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
| `B2-18` | yes | **clean-guest, legacy lane** — kept separate from the fresh-install lane (A2-01 … A2-07). **Fixture, prepared while alpha.4 is still installed in the guest:** an alpha.4 working directory whose archive holds Phone / CW QSOs logged from one tab; that tab stays open at `http://127.0.0.1:18080` (same origin, through the tunnel) and is never closed, so its `sessionStorage` survives. Session `sessionStorage` lasts only as long as the tab: a closed tab, a closed browser or a power cut loses it (alpha.4 record, B2-09 attempt 1). **Verify before the first alpha.5 load,** read-only in that tab's DevTools console: `sessionStorage.getItem('sm.session.qsos')` is non-null and lists the logged UUIDs, and no `sm.session.qsos.<uuid>` key exists; `GET /v1/qso/{uuid}` returns each of them. Then upgrade that lane's package in place to the alpha.5 RPM (its data kept) and restart its daemon, without touching the tab. If the fixture cannot be prepared or is lost before the load, recreate it from the held alpha.4 RPM (`cebcd305…`); otherwise record the case **blocked** | Reload that tab once; read the notices, the Session panel and the Logbook; reload again; read `sessionStorage` again | One notice after the first reload: "Session history was reset by this update. Your logged QSOs are intact in the Logbook."; the Session panel is empty; the Logbook holds every fixture QSO; no notice after the second reload; `sm.session.qsos` is gone and `sm.session.qsos.<uuid>` holds an empty list or is absent | The old list adopted into the archive's Session list; the notice on every load; QSOs missing from the Logbook; a fixture that was never verified counted as a pass | pending | none | pending |
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

None yet. alpha.4's Findings #1–#4, #7 and #8 stay with their recorded destinations; #5 and #6 are
re-judged by B2-10, B2-11 and B2-17.

## Execution log

Command-line checkpoints run by the agent; the operator rules every result. Times UTC.
