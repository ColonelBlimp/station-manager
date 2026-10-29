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

**CI budget (2026-10-29):** the race step's 10-minute per-package timeout failed CI on 3a and 3b
(`cmd/smd`, `internal/archive`, an uncached `internal/database/sqlite`; no assertion failed).
`e4c711af` raises it to 20 minutes and the job to 45, and runs once with `-count=1` to measure
uncached durations. **Measured** (run 40864387932, green): the slowest packages took `cmd/smd`
504 s, `internal/database/sqlite` 459 s and `internal/archive` 439 s (8.4 minutes at most, against
the 20-minute budget); the next were `adoption` 119 s, `qsoservice` 109 s and `forwarding/worker`
108 s. The race step took 9 min 56 s and the whole job 18 min 15 s (budget 45). The follow-up
removes `-count=1`, so caching resumes.

**Next:** slice 5, the confirmation (ADR 0097 §4a, ADR 0087's policy for unlogged work). The
routes stay gated until its confirmation flow is complete and reviewed.

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
