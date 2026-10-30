# W-0024 — Settings → Logbooks, slice 2

**Status:** In implementation — slices 1–4 committed 2026-10-29 (see *Implementation progress*);
the restart application and transition contract are decided in
[ADR 0097](../decisions/0097-change-the-default-logbook-at-an-attended-restart.md) (Accepted
2026-10-29). **Set as default is not released:** its daemon routes stay unregistered until slices 4
and 5 land.
**Selected:** 2026-10-29 (operator: "Let's do 3 first as it is prereq for the contesting work")
**Outcome:** The operator can choose which logbook in the active archive receives live contacts, and
can describe each logbook, without any open tab silently logging into the previous one.

`W-0024` is an immutable identity. Its status may change, while priority and ranked position live only
in [`docs/backlog.md`](../backlog.md).

## Why this item exists

Step (4) of the data-model chain ruled on 2026-09-19 ([W-0014](W-0014-deferred-product-workstreams.md)):
Settings → Logbooks on the active-archive model, before contesting (step 5,
[W-0023](W-0023-contest-logging.md)). The first slice shipped in W-0021; what it left out of scope
— choosing the live logbook, and descriptions — is this item.

**Relation to contesting (stated plainly).** Under [ADR 0096](../decisions/0096-contest-occurrences-reserved-serials-and-submission-export.md)
a contest occurrence is owned by one logbook and membership is explicit, so the reference contest
(UK/EI DX, under the operator's own call) *can* run inside the current Default logbook. This slice
is a prerequisite for running a contest in a **separate** logbook, and it is the ruled order of the
chain; it is not strictly required for the reference case. A contest under a **different callsign**
needs the further step listed under *Out of scope*.

## Prior rulings and records

- **2026-09-19 (W-0014):** placement — Settings → Logbooks mirroring Settings → Rigs; the Logbook view
  stays the QSO surface; a header switcher is separate (ADR 0055). Different-call logbooks cannot be
  set as default until per-logbook service bindings exist; **Set as default cannot be copied
  mechanically from rigs**.
- **First slice (operator ruling 2026-09-29; built `412cca37`, in W-0021):** a Logbooks tab for the
  active archive — list with a read-only Default tag, Add (name, callsign, optional SM Cloud),
  Rename, Delete with the daemon's refusals, unknown outcomes on timeout, the archive-switch gate,
  the Settings leave guard. Its nearest confusable outcome: a logbook that looks usable for live
  logging — only the Default logbook receives live contacts "until default selection exists".
- **2026-10-06 (W-0021, routed from the inbox):** a **description** field in Settings → Logbooks (and
  in Settings → Archives' lists); **set the active logbook** — "a daemon change, with restart
  behaviour still to rule"; forwarding presented per logbook (the logbook-first layout, option B of
  the 2026-09-26 Forwarding exchange, which needs its own ADR).
- **ADR 0071 / ADR 0082:** the default logbook is per archive; destination bindings are per logbook
  inside the archive.

## Current state (verified 2026-10-29, tree at `cc8b9d20`)

- **Two default pointers.** The archive file holds the authoritative one
  (`archive_metadata.default_logbook_id`, `internal/database/sqlite/archive_identity.go:21,51`);
  config's `default_logbook_id` is its projection (`internal/api/handler_config.go:944-946`). Only
  the first-run setup `PUT` sets it today (same comment: "setup — the one PUT that sets the
  pointer"). No endpoint changes it.
- **Who reads it.** `DELETE /v1/logbook/{id}` refuses the configured default
  (`internal/api/handler_logbook.go:179`). A normal completed FT8 exchange is logged to the logbook
  ID **pinned** on it; config's current default is only the fallback when none is pinned
  (`cmd/smd/lifecycle_adapters.go:732-736`). The pin is the **daemon's** current default at session
  start, through `currentStationIdentity` (`internal/api/handler_ft8_qso.go:45`), not the tab's
  logbook. So FT8 does not re-read the default for each contact, and a run started from a stale tab
  uses the new default while that tab still displays — and its Phone / CW submits go to — the old
  one. (Corrected 2026-10-29; this line first said such a run carries the tab's logbook.) Phone / CW
  submits name a logbook explicitly (`POST /v1/qso?logbook=`, `docs/v2-design/api-endpoints.md:109`),
  and the SPA takes that id from the station context.
- **Config publication is immediate.** An ordinary config update publishes the new snapshot in
  memory as soon as it is written (`internal/config/config.go:2463` onward). Writing the new default
  to config and calling it "restart-required" would therefore expose it to the running process
  before the restart.
- **Upload routing does not follow the default** (reviewer finding, 2026-10-29). Destination seeding
  runs once per archive (its marker, `internal/database/sqlite/logbook_destination.go:161`); after
  that, every enqueue selects the enabled bindings by the QSO's own logbook ID
  (`internal/qsoservice/forwarders.go:82`). Existing uploads keep their binding names, and an unbound
  logbook inherits nothing.
- **SM Cloud adoption does follow the default.** The background adopter targets only Home's
  current default logbook (`internal/forwarding/smcloud/adoption/adopter.go:177-197`), and at start a
  binding whose adoption is not confirmed for the current station account is **held**
  (`cmd/smd/identity_wire.go:120-124`). After a switch away, the former default can lose its
  automatic adoption and confirmation path: its uploads are not redirected, but their recovery can
  be stranded.
- **Open tabs read it once.** The SPA reads the default logbook id at boot inside the archive
  identity bracket (`frontend/app/src/main.ts:445-456`); a reconnect compares the **archive**
  identity only. A tab open across a change of default would keep submitting to the previous
  logbook, and the header would keep naming it.
- **Description.** Stored and accepted by the daemon (`types.Logbook.Description`,
  `internal/types/logbook.go:12`; `POST` / `PATCH /v1/logbook`), present in the SPA's API type
  (`frontend/app/src/lib/api/logbooks.ts:33`) but not offered by the Logbooks tab. The archive
  summary (`types.QsoArchiveLogbook`) does not carry it, so an inactive archive cannot show it
  without the ADR 0084 sidecar recording it.
- **Restart class.** `config.md` §11 lists no class for `default_logbook_id`; there is no live path
  that changes it.

## Proposed scope (for ruling)

**In:** (a) logbook descriptions in the Logbooks tab; (b) **Set as default** for a logbook in the
active archive whose callsign matches the current default's.
**Out:** see *Out of scope*.

## Design review (2026-10-29, reviewer at the operator's request)

| Decision | Review |
|---|---|
| L1 | **Accepted:** descriptions and same-callsign defaults. Eligibility is **enforced in the daemon** too, not only in the SPA. |
| L2 | **Restart application**, as **one confirmed "Set as default and restart" operation**. The transition and failure states are defined before implementation. |
| L3 | **Accepted, strengthened:** a **daemon-enforced precondition** on live submissions and admissions; reconnect detection alone cannot close the race. Stale requests are refused, never silently retargeted. Intentional operations on other logbooks are preserved. |
| L4 | **Amended:** following archive activation, it inherits activation's busy refusals and admission sealing — including an **active FT8 run, armed TX and tune**. The existing archive switch does **not** simply terminate a running FT8 session. Competing default changes and archive activation are **serialised**. |
| L5 | **Accepted:** descriptions for inactive archives deferred. |

**Before coding:** an ADR records the restart-versus-live decision and the transition contract
below — recorded as [ADR 0097](../decisions/0097-change-the-default-logbook-at-an-attended-restart.md)
(Accepted 2026-10-29), which also chooses the SM Cloud recovery path. Implementation remains
unapproved.

## Transition contract (to define in the ADR)

- **Saved versus effective default.** The running process keeps the **old effective default** until
  admission closes; the replacement process adopts the new one. The pending target is recorded
  without publishing it as the running default (config publication is immediate, above).
- **States to define:** requested; refused (busy, ineligible, competing operation); pending (target
  recorded, admission sealed); restarting; effective (replacement started on the target); failed
  (persistence partial, restart refused or failed).
- **Partial persistence:** the archive's pointer and config's projection can disagree; the contract
  states which one the next start trusts and how the other is reconciled.
- **Restart failure:** the operator sees which default is effective and that the change is pending
  or abandoned; nothing logs to the target before it is effective.
- **Fresh tabs during the transition** read the effective default, and see the pending change and
  its sealed admission.
- **Timeout recovery:** a timed-out request is an unknown outcome; the tab re-reads the effective
  and pending state rather than assuming either.

## Implementation prerequisite — SM Cloud adoption and recovery

The adopter targets only Home's current default, and unconfirmed bindings are held at start (see
*Current state*). Before Set as default ships, the design must keep adoption and confirmation
reachable for **every** Home logbook with an SM Cloud binding — including the former default after
a switch — or show the operator which binding is held and how to confirm it. Uploads are not
redirected by this gap; their recovery is what can be stranded.

## Decisions wanted

- **L1 — scope:** the In/Out split above.
- **L2 — when a new default takes effect.**
  - **R (recommended; chosen by review): at an attended restart**, as the archive switch does. The
    target is recorded and applies when the daemon restarts, so every component starts against the
    new default together. (Drafted as "an FT8 run ends with the restart" — corrected by review: the
    archive switch refuses while a run, armed TX or tune is active rather than terminating it; see
    L4.) Cost: a restart to change logbook, which is expected to be rare (before and after an event).
  - **L: live.** Applies at once; needs a push to every tab, a guard on submits, and a rule for an FT8
    run in progress. More machinery for a rare action.
- **L3 — open tabs (required whichever L2 is chosen).** A tab built against the previous default
  must not log into it unknowingly: it learns of the change on reconnect and reloads, or its entry is
  gated until it does, as for an archive switch; a submit from such a tab is refused, never filed in
  the previous logbook. (Candidate mechanism: the default logbook's UUID joins the identity the tab
  checks on reconnect, and the SPA's submit carries it. Mechanism not decided.)
- **L4 — when Set as default is refused:** proposed — while an archive switch is in flight or
  unresolved (the existing gate), and, under L, while an FT8 run is in progress. (Amended by review,
  below.)
- **L5 — descriptions in Settings → Archives** for inactive archives: proposed out of this slice (it
  needs the summary sidecar to record descriptions).

## Proposed acceptance criteria (proposal, not ruled)

1. **Description:** Add and Rename (becoming **Edit**) carry an optional description; the list shows
   it. Clearing it stores an empty description. It joins the leave guard as the name does.
2. **Set as default:** offered on each logbook that is not the default and whose callsign matches
   the default's; it asks first, naming the effect: "Live contacts — Phone / CW and FT8 — will go to
   *Contest 2026 (7Q5MLV)* after the restart." A logbook with another callsign shows the action
   disabled with the reason (service bindings for different-call logbooks are not yet available).
3. **Saved versus effective:** until admission closes, the running process — Phone / CW and FT8 —
   keeps the old effective default; the replacement process starts on the new one. After the change
   takes effect, the archive's pointer and config's projection name the same logbook. Partial
   persistence, restart failure, fresh tabs during the transition and timeout recovery follow the
   transition contract.
4. **Every live path follows:** Phone / CW and FT8 contacts both go to the new default; the header
   names it and its count; the Logbook view opens on it.
5. **No silent logging to the previous logbook (L3):** the daemon refuses a live submission or FT8
   admission made against a superseded default — reconnect detection alone is not enough — and never
   retargets it. A tab open across the change reloads or is gated. An intentional operation on
   another logbook (an explicit edit, import or backfill) is unaffected.
6. **Uploads and their recovery do not move with the default:** each logbook's QSOs go to that
   logbook's own bindings (ADR 0082; seeding is once, routing is by the QSO's logbook); changing the
   default changes no binding. SM Cloud adoption and confirmation stay reachable for the former
   default, or its held binding is shown with the way to confirm it (prerequisite above).
7. **Delete protects the effective default and any pending target**, including against a delete
   racing the change; the former default becomes deletable (if empty) only after activation completes.
8. **Busy, competing and unknown outcomes:** Set as default is refused while archive activation's
   busy conditions hold (an archive switch, an active FT8 run, armed TX, tune) and while another
   default change or activation is in progress; the daemon serialises them. A timed-out request is an
   unknown outcome and the state is re-read, never reported as a failure or a success. Cancel,
   Confirm and Retry cleanup name the request they act on. A delayed one is refused once that
   request is resolved, and never touches a later request (ADR 0097, *Acceptance cases for
   removal*).

**Nearest confusable outcomes:** an open Operate tab still logging to the previous logbook; FT8
contacts going to one logbook and Phone / CW to another; the running process adopting the new default
before the restart; uploads moving with the default; the former default's SM Cloud recovery stranded; the header
naming the new logbook while submits go to the old one; a different-call logbook made default and
uploading under the personal accounts.

## Implementation progress

Each slice was test-first: failing tests reviewed before implementation, then reversion proofs.

| Slice | Commit | What it delivers |
|---|---|---|
| 1 | `20c7f65f` | AC1: logbook descriptions in Settings → Logbooks (Add, Edit with name and description, partial PATCH, leave guard). |
| 2 | `9eebb500` | SM Cloud prerequisite (ADR 0097 §5): adoption follows its marker, not the default; a claimed Home binding re-enables off the default; per-binding adopter state. |
| 3a | `76295c98` | A default change applied at start (ADR 0097 §1): the config record and its tombstones, confirmation only by a durable write holding them, the durable archive-pointer write, the read-back matrix; activation and deletion refuse while a change is unresolved. |
| 3b | `027d5845` | The request path (ADR 0097 §2–§3): FT8, rig and live-entry seals; the shared single flight with activation and deletion; withdrawal on a failed restart; Cancel / Confirm / Retry cleanup by request id; the status read. Its routes are **gated** (`api.DefaultChangeRoutesReleased` is false). |
| 4 | `457c9ba2` | The stale-tab default guard (ADR 0097 §4): live requests name the default of the tab's established station context (`X-SM-Default-Logbook`), checked after the archive guard and before any side effect, outside the submit rate limiter. FT8 admission is refused on another default before the service is asked; `POST /v1/qso` naming the asserted logbook is live (refused unless effective, never retargeted, sealed while a change drains), any other logbook deliberate. `/v1/version` reports the served default; the app reloads on another default at boot and on every reconnect; first-run setup establishes it without a reload. `loadgen` reads it at startup. Routes still **gated**. |
| 5a | `04ffdd08`, `8d80d467` | Set as default in Settings → Logbooks and its confirmation (ADR 0097 §4a, §5; ADR 0087): offered on a same-callsign logbook other than the default, hidden when the routes are not served, disabled when the status cannot be read or a change is held. The prompt names the target and its effect, this tab's unfinished entry or unknown Log outcome, its unsaved Settings edits, other windows' unlogged work and, when it applies, the SM Cloud adoption note. The request asserts the established context's default; a Log in flight, a Settings save or an archive operation refuses it before the prompt, after each await and immediately before the POST; without a readable baseline instance nothing is sent. `main.ts` gates submit, entry, FT8 admission and tune while it is pending; an unknown outcome is reconciled, never retried, and `pending_restart` keeps live work gated until the restart reloads the tab. Routes still **gated**. |
| 5a fix | `cb0cc972` | The status decoder reads the start's `not_applied_intent_pending` (`cmd/smd`); its test derives the states from both producers' Go source and pins that state with a held fixture. |
| 5b | `d748080d` | The change's state and its recovery (ADR 0097 §1–§4): the status read at boot inside the identity bracket, compared with the context's default, locking live work until a readable status proves it open (a 404 gates nothing; a failed read never clears a gate); Settings → Logbooks shows the state with **Cancel change**, **Confirm** (with the power-loss qualification) or **Retry cleanup**, each naming the request shown, one at a time, never retried; Delete refused for a held change's target; a shell banner (also above the first-run setup card) that links to Settings → Logbooks and, when the status cannot be read, offers Check again and Reload; an unseen restart re-reads the status and keeps watching instead of raising the archive overlay. Routes still **gated**. |
| 5c | `46a07d07` | Release (operator approved the route release 2026-10-30): `api.DefaultChangeRoutesReleased` is true; the release-gate test proves through the production router that every `/v1/default-logbook` route is registered, each mutation archive-guarded, and its probes write no record and request no restart. The API reference's release-gate wording and the manual's Logbooks section (Set as default; changing the Default logbook; recovery) updated with it. |

**CI budget (2026-10-29):** the race step's 10-minute per-package timeout failed CI on 3a and 3b
(`cmd/smd`, `internal/archive`, an uncached `internal/database/sqlite`; no assertion failed).
`e4c711af` raises it to 20 minutes and the job to 45, and runs once with `-count=1` to measure
uncached durations. **Measured** (run 40864387932, green): the slowest packages took `cmd/smd`
504 s, `internal/database/sqlite` 459 s and `internal/archive` 439 s (8.4 minutes at most, against
the 20-minute budget); the next were `adoption` 119 s, `qsoservice` 109 s and `forwarding/worker`
108 s. The race step took 9 min 56 s and the whole job 18 min 15 s (budget 45). The follow-up
removes `-count=1`, so caching resumes.

**5a review (2026-10-30):** the clean-room review of `04ffdd08` found one P2 — after an unknown
outcome reconciled as `pending_restart`, a later failed status read cleared the stored status,
reopening Phone / CW entry and stopping the restart watch. **Fixed** in `8d80d467`: the seal is
its own latch, kept by a failed read and released only by a readable status that is no longer
`pending_restart` (regression test and reversion proofs; that commit's review is clean).

**5b review (2026-10-30):** two operator review rounds before the commit closed boot ordering
(the status held inside the bracket; a status arriving before the context), consistent state
fixtures, restart watching after a failed re-read, the recovery banner after a sealed status
became unreadable, the Logbooks link with Settings already open, and first-run recovery at the
production boundary — which also put the banner above the setup card, where the recovery
control had been unreachable. Seventeen plus four reversion proofs; the clean-room review of
`d748080d` is clean.

**Implementation complete (2026-10-30):** slices 1–5c are committed; Set as default is released
in `46a07d07`. CI on 5b (`20fe5377`, run 41009559138) is green.

**Operator acceptance: pending.** Recorded separately from implementation completion. Before
deployment: green CI on `46a07d07`. Deployment and acceptance testing (and any RF test) need the
operator's own approval; none is given by the release approval.

### Slice 5 proposal (2026-10-29, for review; not ruled)

Slice 5 is the client of Set as default: no app code exists for it yet. Proposed in three
commits, each test-first; the release flag (`api.DefaultChangeRoutesReleased`) flips only in the
last, after review.

**5a — Set as default and its confirmation (AC2, ADR 0097 §4a).**
- **Offered** on each logbook that is not the default and whose callsign equals the default's;
  another callsign shows it disabled with the reason (AC2). It is shown only when the daemon
  serves `GET /v1/default-logbook` (a 404 — the routes gated — hides it), and refused while an
  archive switch is unresolved, a write is in flight, or a change is already in play.
- **Refused** while this tab's Log is in flight: checked before the confirmation and again
  before the request (ADR 0087 item 9).
- **The confirmation** (`window.confirm`, as Activate) names the target and the effect ("Live
  contacts — Phone / CW and FT8 — will go to *Contest 2026 (7Q5MLV)* after the restart"); names
  this tab's unfinished entry, partial included, with OK discarding it and Cancel keeping it;
  says an unknown Log outcome **may already be logged** in the current default's Logbook; and
  always warns that unlogged work in any other window — a Phone / CW QSO, unsaved Settings
  edits, a submit refused while the change is pending — is lost. Cancel sends nothing.
- **The request** names the target's `uuid` and, as `believed_default_uuid`, the default of this
  tab's established station context (the bound default) — never a fresh read. With none bound,
  nothing is sent. The logbook list decoder gains `uuid`.
- **Outcomes:** `202` → wait for a new daemon instance, then reload (the archive switch's
  settle path); no new instance → gated, never reported as done. `409 default_stale` → this
  tab's context is stale: reload. Any other refusal → the daemon's message, list and status
  re-read. A transport failure or timeout → **unknown outcome**: never success or failure; the
  tab waits for a new instance, and if the old one answers, reads the status and shows it.

**5b — the change's state and its recovery actions (AC8, ADR 0097 §1–§3).**
Settings → Logbooks reads `GET /v1/default-logbook` with the list and shows the change in play:
pending its restart; pending beside this process (*Pending, unclear*: **Cancel**); removal
unconfirmed (**Confirm**); applied, cleanup pending (**Retry cleanup**); not applied, with the
start's reason. Each action names its `request_id`; `already_resolved` / `pending_changed` →
re-read, never retried. Delete is not offered for a pending or unconfirmed target (the daemon
still decides).

**5c — release.** Flip `api.DefaultChangeRoutesReleased`; update the manual (Settings →
Logbooks; switching restarts and reloads every window, as for an archive) and the RG test to
assert the released routes.

**Rulings (operator, 2026-10-29)** — the split is approved (5a, 5b, 5c; the routes stay gated
through both client commits), with these amendments, which supersede the proposal above where
they differ:
- **Wording:** the confirmation opens "Set ‘Contest 2026 (7Q5MLV)’ as default and restart Station
  Manager?", then the target-and-effect sentence, then the unfinished-entry, unknown-outcome and
  other-window warnings. It does **not** warn of losing an active FT8 exchange: an active run,
  armed TX or tune refuses this change.
- **SM Cloud note (ADR 0097 §5):** when the current default has an enabled, unmarked SM Cloud
  binding, the confirmation says its existing uploads continue, but starting adoption for it
  later requires making that logbook the default again. *Detection (an inference from the
  bindings read, to be pinned by tests):* the default's enabled SM Cloud row whose `logbook` is
  neither in `locked_fields` (reserved or adopted) nor in `hidden_fields` (born on identity).
- **Unsaved Settings edits are named in this confirmation,** by the existing unsaved-section
  detection (`lib/config/unsaved.ts`), Logbooks drafts included: the unload prompt cannot cancel
  a restart already requested — cancelling it leaves the old page gated (`requestReload`). The
  leave guard stays as a backstop. Cancel preserves everything and sends nothing; a definitive
  refusal also preserves the drafts; agreeing does not erase them before the reload.
- **5c** also updates the API reference's release-gate wording and verifies the routes through
  the production router.
- **Status at boot:** the status is read inside the identity bracket, not only in Settings →
  Logbooks; a fresh Operate tab during `pending_restart` shows the pending change and sealed
  live entry (ADR 0097 §4). (5b.)
- **Recovery stays reachable:** the archive overlay makes the whole app inert; the default
  change must not copy it unchanged. Once the status proves the old default still effective and
  admission reopened, the recovery state is shown with its action — including *not applied,
  intent still pending* → **Cancel**, distinct from *abandoned*. (5b.)
- **Unsupported is not unreadable:** a 404 hides the feature; a timeout, a malformed status or a
  server error is never treated as idle or unsupported. An unknown mutation outcome reconciles by
  the status read, never by an automatic retry of the POST. (5a and 5b.)

**Rulings (operator, 2026-10-29, on the 5a red tests)** — two of my unruled choices accepted, and
four test gaps to close before implementation:
- **A 502 with an HTML body** is an unknown outcome, reconciled by the status read; no POST
  retry.
- **Bindings that cannot be read** give the SM Cloud note conditionally, qualified on both
  conditions: merely uploading to SM Cloud does not mean adoption would require returning to the
  default — only an enabled binding that is not yet adopted, reserved or born on identity does.
- **Gap 1 — where the believed default comes from.** The request asserts the established station
  context's default. A test makes the context, the fresh status and the list disagree: the tab
  reloads or blocks, or sends its original assertion — never silently adopts the other.
- **Gap 2 — asynchronous boundaries.** Barriers prove the instance baseline is read before the
  POST; an unreadable baseline (`waitForDaemonBack('')` accepts any answering instance) cannot
  prove a restart. A second click during preparation is refused. Live entry and admission are
  actually gated (the `main.ts` wiring, not only a state flag) while the POST or the restart wait
  is pending.
- **Gap 3 — held records versus history.** A settled change with `unconfirmed: false` permits
  another change. A malformed effective UUID, a missing or non-boolean `unconfirmed` and an
  unknown state are unusable status. For the three restart-failure codes, the store's resulting
  status is tested, including a failed reconciliation read.
- **Gap 4 — other writes and drafts.** Refuse while a Logbooks save or an archive activation or
  verification is in progress, including one starting during the awaited preparation. Unsaved
  edits in another Settings section, with Logbooks clean, are named.
- **Follow-up (operator, 2026-10-30, on the 5a implementation):** recheck the refusals after
  the baseline instance read, immediately before the POST — the live-work gate does not stop a
  Settings write or an archive operation starting in that wait. After an unknown outcome, a
  status of `pending_restart` keeps live work gated (it still names the old default while the
  daemon holds its admission seals); `pending_unclear` has reopened admission and releases it.
  Accepted: the unreadable-baseline refusal and hiding the action until the status is read; the
  archive wording of the unproven-restart gate is temporary, to be replaced in 5b.
- The locked/hidden distinction agrees with the daemon's binding projection
  (`internal/archive/bindings.go:241`). Boot status and recovery actions remain in 5b.

### Slice 5b proposal (2026-10-30, for review; not ruled)

**A 5a defect found while preparing 5b.** The status decoder (`lib/api/default-change.ts`)
accepts only the states named in `internal/archive/default_change.go`, and its test enshrined
that list. A start can also report `not_applied_intent_pending` (`cmd/smd/default_change.go`),
so that status reads as *unreadable* and the change it reports can never be cancelled. The
routes are gated, so nothing shipped is affected. Fix: accept it, and derive the test's list from
both Go sources.

**Behaviour, by the ADR 0097 state table.** Settings → Logbooks shows the change in play above the
list, by state; each action names the `request_id` it was shown and is never retried.

| State | Shown | Action | Live work |
|---|---|---|---|
| `pending_restart` | Changing to B — restarting | none (restart requested) | gated (seals held) |
| `pending_unclear` | The change to B applies at the next restart | **Cancel** | open |
| `not_applied_intent_pending` | Not applied at this start and still pending: *reason* | **Cancel** | open |
| `withdrawal_unconfirmed` | Cancelled, withdrawn or abandoned, not yet confirmed on disk | **Confirm** | open |
| `applied_cleanup_pending` | Default B; cleanup pending | **Retry cleanup** | open |
| `not_applied_abandoned`, `restart_failed`, `cancelled`, `withdrawn`, `effective` (settled) | the outcome and its reason | none | open |

- **Action outcomes:** 200 → the returned status. `already_resolved`, `pending_changed` or
  `not_permitted` → the daemon's words and a status re-read. `default_change_in_progress` → the
  daemon's words. `default_change_write_failed` → the daemon's words; the state is unchanged. An
  unknown outcome → status re-read, never a retry.
- **Boot:** the status is read inside the identity bracket (`bootArchiveScoped`), with the
  station context. A 404 means unsupported, and boot proceeds. An unreadable status leaves the
  state *unreadable* and does not block boot; the daemon's seals still refuse live work. A
  `pending_restart` status latches the live-work gate.
- **Operate:** while a change is pending (`pending_restart`), a banner says live entry is paused
  for the default change, and entry is locked. Other states that need an action show a one-line
  notice linking to Settings → Logbooks.
- **Recovery reachable; temporary archive wording replaced.** After an accepted request with no new
  instance, the tab no longer raises the archive overlay at once. It re-reads the status:
  - `pending_restart` latches the sealed gate and keeps watching.
  - A readable status naming the old default with admission reopened clears the gate and shows the
    recovery state.
  - Another default reloads.
  - An unreadable status keeps the gate, with default-change wording and *Reload*.
- **Delete** is not offered for the target of a change that config still holds
  (`unconfirmed: true`); the daemon still decides.

**Decisions wanted:**
1. **Where the actions live.** Proposed: Settings → Logbooks only; Operate shows the banner or
   notice with a link. The alternative is buttons in Operate too.
2. **No confirmation dialog for Cancel, Confirm or Retry cleanup.** Each is a single idempotent
   write naming its request, and none restarts or discards work.
3. **An unreadable status at boot does not block the tab.** Proposed: the daemon's seals still
   refuse live work. The alternative is to gate the tab until the status is read.
4. **The decoder fix** goes in 5b's first commit, or now as its own commit.

**Rulings (operator, 2026-10-30)** — 5b's direction is approved (design, not release; the routes
stay gated), with these rulings, which supersede the proposal above where they differ:
1. **Actions live in Settings → Logbooks only.** Operate shows the banner or notice linking there.
   Settings stays reachable while live work is gated.
2. **No confirmation dialogs** for Cancel, Confirm or Retry cleanup. Clear labels — especially
   **Cancel change** — naming the target. The displayed `request_id` is captured; overlapping
   actions are prevented; an unknown outcome reconciles by the status read, never by retrying
   the POST. A 200 is not necessarily durable completion: its returned state is rendered.
3. **An unreadable status at boot gates live work.** The daemon's seals stop writes and
   transmission, but not an entry typed and then discarded by the impending reload. The shell and
   Settings load; entry, Log, FT8 admission and tune are locked, with default-change wording and a
   recovery control. A 404 stays unsupported and imposes no gate.
4. **The decoder fix lands now, as its own commit with its test.** It covers both state
   producers, plus an explicit `not_applied_intent_pending` fixture with `unconfirmed: true` — a
   copied list alone is insufficient evidence.
- **Amendment:** `default_change_in_progress` also re-reads the status: the displayed action may be
  obsolete because a restart is under way.
- **Amendment:** every status result — an action's 200 included — checks the effective default
  against the established context before reopening live work. Another default reloads; never a
  silent rebind.
- **Red tests must cover:** boot unreadability; failed reads preserving an existing gate; a stale
  action response arriving after a newer status; uncertain removal staying actionable; the
  startup state exposing Cancel; and ADR 0097's power-loss qualification for an unconfirmed
  removal.

**Rulings (operator, 2026-10-30, on the 5b red tests)** — proceed to implementation once these
are strengthened:
- **Boot ordering:** hold the status response behind a barrier and prove live work stays locked
  until it settles inside the identity bracket; also a status arriving before the station context
  with disagreeing defaults.
- **State fixtures:** cover `cancelled`, `withdrawn` and `effective`; assert settled failures'
  reasons; make the cleanup fixture consistent (Contest effective when it is the applied change);
  for *target no longer exists*, remove Contest from the list and prove the notice and Cancel stay
  usable.
- **Restart watching:** the accepted-but-unseen path keeps watching and reloads, including after a
  failed status re-read.
- **Stale action response:** ignoring it preserves the gate as well as the newer status.
- **Choices accepted:** a later failed read imposes no new gate once admission was proven open —
  unless later restart or pending-change evidence invalidated that proof — and never clears an
  existing one; the ineligible reason reads "its callsign no longer matches the current
  default's callsign", with a useful fallback when no failure code is supplied; the banner is
  shell-wide beside the TX alarm banner; Settings opens at Logbooks by the existing tab handoff.

**Rulings (operator, 2026-10-30, on the 5b implementation)** — two defects and a coverage gap to
close before the commit:
- **The recovery banner must show whenever an unreadable status leaves live work gated** — also
  after `pending_restart`, where a failed read clears the status (and the notice) but keeps the
  seal. Test pending_restart → failed read → Check again, including the recovery.
- **The banner's link must select Logbooks when Settings is already open** — the tab handoff was
  read only on mount — without discarding drafts; tested with the real Settings mounted.
- **First-run recovery at the production boundary:** before setup the served endpoint answers
  an effective logbook without a UUID (`effectiveDefault`, `internal/archive/default_change.go`),
  which the client reads as unreadable. Cover: unreadable before setup, setup succeeds, the
  re-read pending, then a valid status releasing logging; and a failed re-read keeping the gate
  and the recovery controls.
- Accepted: the existing reload latch with default-specific detail for a proven mismatch;
  settled history confined to Settings.

## Out of scope

- **Different-call logbooks as default** — needs per-logbook station accounts or forwarding policy
  established for that callsign, and the logbook-first Forwarding layout (option B), which needs its
  own ADR. This is the step a contest under another callsign depends on.
- A header logbook switcher (ADR 0055 follow-up).
- Descriptions for inactive archives (L5).

## References

- [W-0014](W-0014-deferred-product-workstreams.md) (2026-09-19 rulings),
  [W-0021](W-0021-qso-archives.md) (first slice; 2026-10-06 routed notes; the 2026-09-26 Forwarding
  exchange), [W-0023](W-0023-contest-logging.md).
- ADR 0055 (logbook as station identity), ADR 0056, ADR 0071, ADR 0082, ADR 0084, ADR 0096.
