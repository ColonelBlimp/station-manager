# Dogfood release acceptance

- **Status:** Mandatory pre-deploy gate; initial scaffold, 2026-08-30
- **Owner:** Operator
- **Applies to:** Every candidate installed on the daily-driver dogfood station

This document owns the current user-driven dogfood gate. It supersedes the manual release gates in
the historical [`v2-design/release-acceptance.md`](v2-design/release-acceptance.md). Automated tests,
CI, and artifact checks remain prerequisites, but they do not prove that an operator can install,
understand, and use the product.

The gate has two decisions:

1. **Ready to deploy:** the exact candidate is identified, its clean-install path has been exercised,
   the live data is backed up, rollback is ready, and the operator has ratified the release-specific
   test record.
2. **Dogfood accepted:** after the upgrade, every mandatory user-facing case has passed or received
   an explicit operator waiver. Otherwise the candidate is fixed, held, or rolled back.

Passing the first decision authorizes installation; it does not pre-judge the second.

## Acceptance boundary

The nearest confusable outcome is **“the automated release gate is green”**. That means the code and
artifact passed their programmed checks; it does not mean that first-run setup is understandable,
an existing station upgrades intact, every UI surface works in a real browser, or an external
service reports the outcome the operator sees.

The candidate is accepted only when all of these are true:

- the same identified artifact was used for clean-install and daily-driver testing;
- a clean installation reaches a usable first-run station without private explanation;
- an upgrade preserves configuration, credentials, logbooks, QSOs, audit history, and queued work;
- every current operator-facing surface is present in the release-specific case inventory;
- happy paths, empty/loading/error states, persistence across reload/restart, and recovery paths have
  operator-observable outcomes;
- the install guide and embedded manual describe the behavior actually encountered;
- no unresolved failure or blocker affects data integrity, RF safety, installation, rollback, or a
  supported operator journey;
- observations are triaged, and the operator records an explicit accept, hold, or rollback decision.

A page rendering is not proof that its actions work. A toast is not proof that data persisted. A
request being accepted is not proof that an external service stored it. An FT8 decode is not a QSO,
and a completed exchange—not a decode or keyed transmission—is the contact outcome.

## Execution rules

- The operator drives the tests and judges visible outcomes. Diagnostic commands may corroborate an
  observation; they never replace the user journey.
- Use the release candidate's normal UI and packaged commands. Direct API or database inspection is
  supporting evidence unless the case explicitly tests those operator interfaces.
- Never put credentials, operator data, full logs, or an unredacted configuration in this repository.
- A clean-install test uses a VM, spare host, or disposable working directory. Never erase or reset
  the daily-driver station to manufacture first-run state.
- Any candidate rebuild or code/UI/package change creates a new artifact identity. Rerun the affected
  cases and every prerequisite whose evidence the change invalidated. Safety, persistence,
  migration, or packaging changes require both gate decisions again.
- RF, tune, CAT writes, rig commands, and hardware-dependent experiments are listed in advance but
  are **not authorized by this document**. Each execution requires the operator's explicit agreement
  for that occasion. FT8 sessions remain operator-initiated.

### Result vocabulary

Each case ends in exactly one state:

| Result | Meaning |
|---|---|
| `PASS` | The expected operator-visible outcome occurred and the evidence was retained. |
| `FAIL` | The observed outcome differed. The candidate cannot pass while this remains unresolved. |
| `BLOCKED` | The case could not reach its decisive assertion. This is not a pass. |
| `N/A` | The release or supported deployment genuinely does not contain the surface; explain why. |
| `WAIVED` | The operator knowingly accepts a stated residual risk for this candidate. |

Only the operator may mark `WAIVED`. A waiver records the exact unmet outcome, risk, reason, and
expiry or follow-up destination. RF-safety, data-loss, unrecoverable-upgrade, and unusable-install
failures are not waivable for dogfood.

## Release-specific execution record

Before the ready-to-deploy decision, copy the template below to
`docs/reports/dogfood-acceptance-<version>.md`. That record holds candidate-specific cases, results,
evidence summaries, findings, and sign-off; this canonical document stays bounded and current.

```text
# Dogfood acceptance — <version>

Candidate commit:
Version/build shown by daemon:
RPM filename:
RPM SHA-256:
Previous installed version:
Clean-install environment:
Upgrade environment:
Browser(s) and viewport(s):
Planned test date:
Operator:
Automated gate evidence:
Backup location (do not record secrets):
Restore check:
Rollback artifact/command:

Ready to deploy: PASS | FAIL
Operator/date:

Dogfood accepted: PASS | HOLD | ROLLED BACK
Operator/date:
Residual waivers:
Follow-up destinations:
```

Use one row per decisive operator action:

| ID | Required | Environment and starting state | Operator action | Expected visible result | Nearest confusable failure | Evidence | Extra approval | Result |
|---|---|---|---|---|---|---|---|---|
| `AREA-01` | yes | … | … | … | … | … | none / hardware / rig command / RF | pending |

Do not mark a broad journey `PASS` when one action hides several independent outcomes. Split cases
where correct and incorrect behavior could otherwise produce the same observation.

## Gate A — ready to deploy

### A1. Freeze and identify the candidate

- Record the commit, version, build scope, RPM filename, and SHA-256.
- Confirm the worktree used to build it is clean and the version shown by `/v1/version`, the sidebar
  build badge, startup log, and RPM agree.
- Run `task ci:local` and the applicable release/build checks against that commit.
- Build the candidate once. Use that artifact for the remaining acceptance work.
- Review the release delta and enumerate migrations, configuration changes, changed user journeys,
  changed integrations, and any new manual or setup instruction.

### A2. Exercise a clean installation

Use the current [`install.md`](install.md) procedure on an isolated RPM-based environment with no
Station Manager data. At minimum, prove:

- package installation, user-service enable/start, lingering instructions, health, version, logs,
  and browser access behave as documented;
- only the first-run welcome surface appears before setup;
- invalid, empty, normalized, and valid callsign behavior is understandable;
- successful setup creates the default logbook and offers both **Open Settings** and **Start
  logging** journeys;
- setup completion survives browser reload and daemon restart;
- the Settings, Manual, and first QSO paths are discoverable without repository-only knowledge;
- uninstall behaves as documented, including the deliberate retention of operator data.

The clean environment must test the packaged artifact, not a source-run daemon. Record every point
where the operator needed information absent from the install guide or embedded manual.

### A3. Prepare daily-driver safety and recovery

- Make a recoverable backup of the complete working directory without copying it into the repository.
- Verify the backup can be listed/read and restore a copy into a scratch location when practical.
- Record pre-upgrade counts or checksums for durable records needed to detect loss, without recording
  their contents here.
- Preserve the previous RPM and record the rollback procedure.
- Confirm enough time is reserved to roll back before the station is next needed.
- Stop if the backup, restore evidence, prior artifact, or rollback procedure is uncertain.

### A4. Ratify the case inventory

The release-specific record must contain a case for every applicable surface below. It may reference
an earlier case only when the starting state, action, expected result, and evidence are genuinely the
same. The operator decides any thresholds, timeouts, visual tolerances, upstream-account checks, and
hardware/RF exercises before execution.

### A5. Drain the SM Cloud upload queue before upgrading — TEMPORARY (alpha.2 transition only)

alpha.2 projects daemon-local fields out of the SM Cloud QSO payload (`smcloud.projectCloudQso`).
A QSO the cloud committed under a **pre-alpha.2** daemon whose response was lost stays `pending`
locally; retried after the upgrade it sends the new (shorter) payload at the same version, which the
cloud's strict equal-version guard correctly rejects as a conflict — the row then fails terminally
even though the QSO is already backed up. Before upgrading a station that already talks to an SM
Cloud service:

- Inventory the SM Cloud forwarder's `qso_upload` rows whose status is **not** `uploaded`.
- Let `pending`/`in_progress` rows **drain to `uploaded` under the pre-alpha.2 daemon** first.
- Resolve any `failed` rows individually (understand and clear the cause).
- Do **not** blindly clear queue rows or wipe the cloud. A development-cloud reset is an explicit
  operator-approved fallback **only after local backup verification**.
- If no non-`uploaded` SM Cloud rows remain, the false-conflict trigger is absent; already-`uploaded`
  rows do not re-trigger via reconcile (the manifest matches on version, not payload).

**Remove this step once no pre-alpha.2 daemon can still upgrade against an existing SM Cloud service.**
Rationale: `smcloud.projectCloudQso` and ADR 0016.

## Gate B — deploy and accept the candidate

### B1. Exercise the upgrade itself

- Install through the documented dogfood path over the existing version; do not uninstall first.
- Observe stop, package replacement, systemd reload, start, and browser reconnection.
- Confirm the expected version/build identity and that the old embedded UI is not still running in
  an open tab; reload it deliberately.
- Confirm the existing station bypasses first-run setup and retains configuration, selected rig,
  logbook, operator identity, theme/navigation preferences where promised, and durable notifications.
- Compare the pre/post durable-record evidence and inspect startup/migration diagnostics.
- Exercise a controlled daemon restart and confirm the UI and event streams recover.

### B2. Current user-facing surface inventory

The release-specific record expands every applicable bullet into decisive cases. Before execution,
reconcile this inventory against the release candidate's sidebar, Settings tabs, packaged commands,
install guide, embedded manual, and release delta.

#### Application shell and navigation

- First-run welcome and setup-complete interstitial.
- Dashboard placeholder, Operate Phone/CW, Operate FT8, Logbook, Settings, Contacts Map, and Manual.
- Direct/deep links, browser back/forward, reload, lazy-loaded views, and separate Map tab.
- Header station/rig state, notification history, toasts, TX/drive alarms, connection/offline states,
  build identity, browser title, light/dark theme, collapsed navigation, and utility rail.
- Keyboard-only operation, shortcuts, focus, modal Escape behavior, unsaved-Settings navigation guard,
  readable narrow/wide layouts, and zoom at operator-selected representative sizes.

#### First-run and station configuration

- Callsign setup, default-logbook creation, post-setup choice, and persistence.
- Every Settings section: Station, Rigs, FT8, Forwarding, Email, Enrichment, and General/About.
- Initial values, validation, save, no-effective-change save, secret redaction/preservation, reload,
  restart-required messaging, discard/leave behavior, simultaneous errors, and recovery.
- Hardware discovery and the distinction between optional logging-only setup and CAT/FT8 setup.

#### Phone/CW operating journey

- Manual and CAT-derived frequency/mode state, confirmation rules, callsign entry, enrichment,
  worked/country context, comments/RST, duplicate handling, submission, and cleared/retained fields.
- Session list, session timer, callsign stack/pile-up behavior, rig/session panels, export, session
  email, and map launch.
- QSO persistence, audit/history effects, upload-queue creation, and visible error recovery.

#### FT8 receive and operating journey

- View-open capture ownership, audio-level and occupancy surfaces, band/filter controls, decodes,
  caller queue, enrichment, arming/disarming, skipped callers, CQ and directed-QSO flows, completion,
  logging, and terminal state.
- Offline, missing-audio, CAT-loss, dial-change, stale/moved-slot, timing, decode-empty, and daemon
  restart behavior as applicable.
- Preserve the distinction between decode, attempted exchange, completed exchange, logged QSO, and
  forwarded QSO in every expected result.

#### Logbook and records

- Logbook selection, pagination, page sizes, selection, empty/error/loading states, and upload/email
  filters.
- QSO detail/edit, genuine no-op edit, changed edit, validation failure, deletion if exposed,
  re-enrichment, selected-row email, forwarder retry/backfill policy, and selection clearing.
- ADIF import (default and selected logbook, duplicate/error summary, restart handoff), export, and
  round-trip checks that are safe for the candidate data.

#### Map and visual context

- Origin/no-origin states, time window, band filter, legend/counts, grey line, pan/zoom/reset, live
  updates, offline state, unplottable contacts, and capped/error messaging.
- Representative viewport sizes, browser zoom, colour/theme legibility, and no obscured controls or
  clipped decisive information.

#### Packaged commands, lifecycle, and recovery

- `smctl` start, stop, restart, status, and import journeys with the documented user-visible output.
- Boot/login start behavior, daemon unavailable/reappearing, browser reload, SSE reconnection,
  shutdown with work in flight where safe, log/journal diagnostics, and disk/config/database errors
  that can be exercised without endangering the live data.
- Rollback rehearsal or the strongest non-destructive proof agreed by the operator.

#### Configured integrations

- Each enabled enrichment provider, forwarder, SM Cloud backup/synchronization path, session email,
  PSK Reporter path, and any other configured external surface.
- Success, rejected credentials, upstream unavailability, retry, duplicate/idempotent behavior,
  operator-visible status, and independent upstream confirmation where applicable.
- Enrichment failure must leave logging usable; QSO plus upload-queue creation remains atomic.

#### Documentation and support journey

- A clean-install operator can follow the install guide from package to first loggable QSO.
- The embedded manual opens without internet access and matches current labels, navigation, setup,
  rig/CAT, FT8, forwarding, import, shortcuts, troubleshooting, and file locations.
- UI help text, validation messages, alarms, warnings, and recovery instructions name the action the
  operator can actually take.

### B3. Hardware, rig-command, and RF cases

Keep these cases in separately approved groups so passive UI acceptance cannot accidentally become
a hardware or transmission exercise.

1. **Passive hardware observation:** enumeration, connection identity, pushed rig state, receive
   audio, reconnect, and failure communication.
2. **Rig commands without intended RF:** frequency/mode/VFO controls and any setup-dependent CAT
   behavior. Obtain explicit agreement before each execution group.
3. **Tune/keyed tests:** start/stop, hard auto-off, single-flight ownership, retune stop, disconnect,
   daemon shutdown, and physical unkey confirmation. Define power, load, duration, abort action, and
   observer before agreement.
4. **FT8 transmit:** operator opens the subscription, initiates the session, chooses the station or
   CQ action, and controls arming. Define band, power, duration, abort action, and expected exchange
   before agreement.
5. **W-0002 type-4 validation:** remains its own acceptance boundary. If performed in the dogfood
   window, its completed-exchange evidence goes to that dossier; merely running this gate does not
   close it.

## Findings, reruns, and sign-off

- Record observable surprises immediately in [`dogfood-inbox.md`](dogfood-inbox.md), with sanitized
  evidence and no premature mechanism claim.
- Triage each finding as a defect, planned follow-up, duplicate, working-as-designed outcome, or
  documentation correction. Route durable work through the authoritative backlog; do not grow the
  execution record into another worklist.
- A fix creates a new candidate. State which cases must be rerun and why; do not silently carry a
  prior pass across changed behavior.
- At Gate B, record one decision: **accept for dogfood**, **hold installed but do not operate**, or
  **roll back**. Include all waivers and follow-up destinations.

The gate is complete only when the operator signs the decision. Time pressure, a green CI run, or
the absence of an immediately visible problem is not sign-off.
