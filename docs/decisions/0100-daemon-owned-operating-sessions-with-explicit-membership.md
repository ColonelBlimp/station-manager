---
number: 0100
title: Daemon-owned operating sessions with explicit membership
status: Proposed
date: 2026-11-06
---

# 0100 — Daemon-owned operating sessions with explicit membership

**Proposed (2026-11-06). Design only. Nothing is authorised or built.** Three points are left
for the operator to rule on: what counts as activity, the default gap, and the contest policy
(*Unresolved*, below). The session-email mark fix (inbox item 1) is **independent** of this ADR
and does not wait for it.

## Context

The Session panel is today a per-tab list in `sessionStorage`, one list per archive
(`frontend/app/src/lib/operate/session.svelte.ts`, ADR 0094). It fills from the tab's own
Phone/CW submit responses and from the `ft8-logged` events on the tab's FT8 stream. It survives
reloads, daemon restarts and FT8 runs, and ends when the tab closes. The operator likes how it
looks and feels. Its flaws come from where it lives:
- each window has its own list, so a new window starts empty;
- the Map instead plots the last N hours of the logbook (`frontend/app/src/lib/map/mapData.svelte.ts`);
- nothing outside the tab can see it: not the daemon, not FT8 eligibility, not other windows.

`smd` has no session concept: no session table and no session id on QSOs. Session email and
export take UUID lists supplied by the tab (`internal/api/handler_session_email.go:44`).
`session.terminated` is an unrelated Station Events kind for the end of an FT8 contact
(`internal/stationevents/stationevents.go:42`).

Two 2026-11-06 inbox items share this root cause (`docs/dogfood-inbox.md`, triage items 2 and 5).
The operator's direction is: "The daemon session is the cleanest and gives us one authority on the
matter." The aim is something that "looks and feels the same" as today's Session panel, but owned
by the daemon, with session controls on the Session panel.

**Why this reopens ADR 0049 (Rejected 2026-07-16).** ADR 0049 proposed a daemon session table with
`session_id` stamped on every QSO in `prepareQso`. It was rejected for three reasons:
- the only tenant was a display feature, and a time-filtered read served it;
- a derived boundary can be recomputed, while a stamped one is frozen and needs correction
  tooling;
- the stamping sat on the most load-bearing write path.

Its revisit trigger was "a session that must carry data *not derivable from its QSOs*". That
trigger has now fired:
- **The boundaries are operator decisions**, so QSO times cannot give them: a manual New session,
  picking up a closed session, and closing on an archive or logbook change.
- **One authority for several consumers:** the Session panel in every window, the Map, email, and
  the daemon's own FT8 eligibility check.

How this ADR treats ADR 0049's three objections:
- **The tenant:** there are now several consumers (above), not one display feature.
- **Frozen boundaries:** membership is a separate association, not a stamped `qso` column
  (alternative C). Membership can be corrected without updating QSO rows. **Correction tooling
  itself is deferred, not solved:** pick-up resumes future logging only, and does not repair a
  split session (O1).
- **The write path:** the association adds one write to the logging transaction. No session-policy
  decision, operator prompt or external dependency can block logging. A database failure on that
  write rolls the QSO back atomically, like any other failure in that transaction (§2).

## Decision

The daemon owns the **operating session**:
- **Where it lives:** an archive-local record with a stable UUIDv7 identity, bound to the logbook
  that was the effective default when it opened. At most one session is open per archive. Only one
  archive is served at a time, so the station has one current session.
- **Membership:** an explicit association, written in the same transaction as the QSO by the live
  logging paths only.
- **Controls:**
  - sessions open and close automatically by activity;
  - the Session panel offers New session and pick-up (resume a closed session);
  - a window that opens with no session in progress asks "Continue the previous session or start a
    new one?";
  - an effective archive or default-logbook change closes the session at the moment of departure.
- **Consumers:** the Session panel in every window, the Map, session email and export, and FT8
  repeat-hold eligibility all read this one membership.
- **Stale actions:** each operator action names the archive's session-state generation it was
  taken against, and the daemon refuses it if the generation has moved.

### 1. Identity and record

- **Session record:** `id` (UUIDv7), `logbook` (the logbook's UUID), `opened_at`,
  `last_activity_at`, `closed_at` and `close_reason`.
  - `close_reason` is one of `new_session`, `gap`, `logbook_changed`, `archive_changed` or
    `picked_up_other`.
  - A resumed session keeps its `id`. Each open interval is recorded in that session's own interval
    list, so the history stays readable.
- **Archive-level session state:**
  - **`session_generation`:** one counter per archive, which advances on **every** session-state
    transition in that archive. That includes an automatic open, New, pick-up, and each close
    (gap, archive, logbook). A QSO joining an already open session is not a transition, so the
    generation does not move.
  - **Departure mark** (§4).
  - **Applied-request records** (§5).
- **Home:** the archive file, beside the logbooks and QSOs it describes. This follows ADR 0094 (per
  archive) and ADR 0096 (contest occurrences live in the archive file).
- **Authoritative, local only:** sessions are not sent to SM Cloud in this design. A rebuild from
  SM Cloud therefore restores QSOs without sessions (*Consequences*).
- **Naming:** `/v1/session/{email,export}` and `session.terminated` are already taken by other
  meanings. Wire names use **`operating_session`** (for example `operating_session.opened`) so they
  cannot collide.

### 2. Membership

- **Who joins:** a QSO joins the open session when it is stored by a **live** logging path into the
  session's logbook:
  - the Phone/CW submit that names the effective default logbook. That is the `requireLiveSubmit`
    and `submitClassifiedLive` classification already in `internal/api/handler_qso.go`.
  - the FT8 completed-exchange sink (`cmd/smd/lifecycle_adapters.go:737`).
- **The logging transaction, in order.** All of this happens inside the QSO's own transaction (the
  atomic-write invariant), at the daemon's current instant `now`, never at the QSO's `time_on`:
  1. Read the archive's open session, if any.
  2. **Evaluate expiry:** if it is open and `now − last_activity_at` exceeds the gap, close it with
     `gap` (§3) and advance the generation.
  3. **If none is open now,** create a session: `opened_at = last_activity_at = now`, bound to the
     effective default logbook. Advance the generation.
  4. Write the membership association.
  5. Set `last_activity_at = max(last_activity_at, now)`. This assumes a QSO counts as activity,
     which U1 must confirm; U1 may add further sources but cannot exclude a logged QSO without
     revisiting this step.

  So a QSO can never join a session that had already expired. That holds even when no read or
  timer ran during the gap.
- **What can and cannot stop a QSO.**
  - **Cannot:** no session-policy decision (expiry, a stale generation, a pending departure), no
    operator prompt (the opening question, an unanswered Not now) and no external dependency can
    refuse or delay a live QSO. The transaction resolves the session state itself.
  - **Never silently resumed:** the QSO never resumes an older session. That takes the operator's
    pick-up.
  - **Database failures:** the session writes are database operations in the QSO's transaction. If
    one fails, the whole transaction rolls back, exactly as a failure of the QSO row or its
    upload-queue rows does today. That is the existing permitted reason not to log (a broken local
    database), and atomic rollback is kept deliberately.
- **Never joins:**
  - **existing contacts**, logged before this ships: sessions run forward only, with no backfill;
  - **imports** of any kind: ADIF import (`SubmitImport`, `SubmitImportBatch`), SM Cloud restore
    (`Restore`, `RestoreInto`), and archive copy or transfer. This holds even when the QSO's time
    falls inside a session interval;
  - **a submit that names a non-default logbook.** It is a deliberate operation (ADR 0097 §2) and
    is outside the session's logbook.
- **Later changes:**
  - **Edits** keep membership, including edits to time, band or mode (as for contest membership,
    ADR 0096).
  - **A deleted QSO** keeps its association row, but every consumer excludes it; an undelete brings
    it back.
  - **Membership never moves on its own.** Explicit add or remove actions are an open question
    (O2).
- **Not derived:** membership is never computed from time windows. ADR 0049's "recomputable"
  advantage does not survive an import or edit that would silently move a QSO in or out.

### 3. Lifecycle

- **Opens:**
  - automatically, on the first live QSO when none is open;
  - by **New session** on the Session panel;
  - by **Continue** at the opening question, or **pick-up** on the panel.
- **Closes:**
  - when New session or pick-up of another session closes the current one (`new_session`,
    `picked_up_other`);
  - after the configured **gap** with no activity (`gap`; U1 and U2). For a gap close, `closed_at`
    is the session's `last_activity_at`: the sitting ended then, not when the daemon noticed;
  - on an effective logbook or archive change, at the moment of departure (§4).
- **Activity baseline for operator actions:**
  - **New session:** `opened_at = last_activity_at = now`.
  - **Pick-up:** reopening a session starts a new open interval and sets `last_activity_at = now`,
    whatever U1 decides. The resume is the baseline, so a resumed session's old timestamp cannot
    expire it again at once.
- **Pick-up (the operator's "restart"):**
  - It resumes a closed session, which becomes the open one; **future** live QSOs are added to it.
    Nothing already logged moves (O1).
  - Closing the current session, if any (`picked_up_other`), and reopening the chosen one are one
    transaction.
  - Only an **eligible** session can be picked up: one whose `logbook` is the current effective
    default. Others are refused, because they would mix logbooks. Every offer (§6, §8) lists eligible
    sessions only, so the panel never offers an action the daemon would refuse.
- **Where expiry is evaluated:**
  - **The logging transaction** (§2), which is the load-bearing evaluation;
  - **every session-control transaction** (§5), as its first step;
  - **a daemon timer** set for the open session's expiry instant. It persists the `gap` close and
    emits the event, so open panels show "closed" without waiting for the next action;
  - **every read:** if a read finds the open session expired, the daemon persists the close and
    emits the event before answering. A reported state is therefore always a persisted state with
    its generation (§5).

  Every evaluation is idempotent, and whichever runs first records the close.
- **Restart recovery:**
  - A plain restart (power cut, deploy, crash) **does not close** the session. The open record is
    durable.
  - Time the daemon was down counts as time without activity. The first evaluation after the start
    closes a session that expired meanwhile.
- **Clock steps:** evaluation uses stored UTC instants.
  - **Backwards:** if `now` is earlier than `last_activity_at`, the session is not expired, and
    `last_activity_at` never moves backwards (§2, step 5). A session reopens only by an operator
    action.
  - **Forwards:** a step can close a session by gap; pick-up recovers it for future logging.
  - A guest host's clock stepped during W-0021 acceptance.

### 4. Archive and default-logbook changes: closure at departure

The rule is that the session closes **when the archive or logbook is left**. Two facts shape the
mechanism:
- **The outcome is unknown at departure.** An archive activation can fail and fall back to the
  last-known-good archive. A default change (ADR 0097) can end `cancelled`,
  `not_applied_intent_pending`, `withdrawal_unconfirmed` or `pending_unclear`, as well as
  `effective`.
- **The commit instant is known, but admission is not always sealed after it.**
  - A default change seals live Phone/CW entry, FT8 and the rig (ADR 0097 §2).
  - Archive activation seals FT8 and the rig, but **does not seal Phone/CW submissions** (ADR 0097,
    Context; `internal/archive/activate.go`). A Phone/CW QSO can therefore still be stored in the
    departing archive between the commit and the process exit.
  - That QSO is a live QSO in the archive still being served. It joins the departing session
    normally (§2), because session policy never refuses a QSO. The closure time accounts for it
    (below).

The departure is therefore recorded in two stages.

- **1. The departure mark, written before the change's own durable selector.**
  - **Where:** when the operation commits (after its seals, before its selector), the departing
    process writes into **the departing archive's file** a departure mark. It names the operation
    (`archive_activation` or `default_change`), its request id, the target, and the instant `D`.
  - **Order:** the mark is written **before** the change's own durable selector in config (ADR
    0097's pending selector, or `pending_qso_archive_id`). A crash between the two writes leaves a
    mark with no selector.
  - **While the mark stands:**
    - live QSOs still join the departing session as in §2;
    - session **controls** (New, Continue, pick-up) are refused with a distinct "change pending"
      code. That refuses a control, never a QSO;
    - if the operation is cancelled before its restart and the process carries on serving, the
      cancel clears the mark in the same process.
  - **The effective closure time** is `D* = max(D, last_activity_at)`, evaluated when the mark is
    resolved. That is the departure, or a later live QSO the unsealed path admitted before the exit.
- **2. Resolution, the next time that archive is served.**

  | How the archive comes to be served next | Meaning | Action |
  |---|---|---|
  | As the **activated candidate** of a later activation into it | it was left at `D*` | close the open session: `closed_at = D*`, `archive_changed` |
  | As the **last-known-good fallback** after a failed candidate | it was never left | clear the mark; the session continues, subject to expiry from `last_activity_at` |
  | A **plain start** with no selector naming another archive (activation cancelled, or a crash before the selector) | it was never left | clear the mark; the session continues |
  | A start where the **effective default differs** from the session's `logbook` (the default change became `effective`) | the logbook was left at `D*` | close: `closed_at = D*`, `logbook_changed` |
  | A start where the default change ended **not effective** (cancelled, not applied, withdrawal unconfirmed) | the logbook was never left | clear the mark; the session continues |
  | `pending_unclear` | undecided | keep the mark. Admission stays governed by ADR 0097, and the mark resolves when that state resolves. |

  - **Fallback rule:** a start that cannot classify itself by the rows above compares the
    session's `logbook` with the effective default, and the served archive with the mark's origin.
    A difference closes the session at `D*`; no difference clears the mark. The rows above are
    that comparison made explicit; the fallback only covers states this table missed.
  - **Expiry first:** a session that had already passed its gap before `D` closes as `gap` at its
    `last_activity_at`, not as a departure.
  - Resolution and the close are one transaction, and they advance the generation. Repeating the
    resolution after a crash is idempotent.
- **Closure is effective at departure, recorded on return.** The close time is `D*`, not the
  return time.
  - **This is deliberately deferred recording, not deferred closure.** While another archive is
    served, no process serves the departed archive's file, so no QSO can join that session and no
    consumer reads it. Nothing can observe it as open between the exit and the return.
  - Resolving the mark only writes down a closure that had already taken effect.
  - **A requirement on any future feature** that reads a non-served archive's sessions (for
    example an archive summary or export): it must apply the same rule, so that a session with a
    departure mark, in an archive not being served, reads as closed at `D*`.
- **Worked sequences:**
  - **A→B:** A's mark is written at `D`. A Phone QSO logged before A's exit joins A's session.
    B comes up and serves its own sessions. When A is next served, A's session is closed at `D*`.
  - **A→B, restart B, then B→A:** the restart of B is plain, and B's session continues. B→A writes
    B's mark at `D'`. A starts as the activated candidate and closes its own session at A's `D*`,
    the original departure, not `D'` or the return time. B's session is closed at its own `D'*`
    when B is next served.
  - **A→B where B fails:** the fallback serves A, and A's mark is cleared. A's session continues,
    and time spent down counts towards its gap.
  - **A crash between the mark and the selector:** a plain start of A, so the mark is cleared.
  - **A crash after the selector:** the start proceeds as the activation would, and the table
    applies.
- **Returning to an archive:** its last session is closed, and the opening question offers it if it
  is eligible (§6). That matches today's per-archive restore (ADR 0094), but it is now the
  operator's choice.

### 5. Several windows, conflicting choices

- **One session for every window:** all windows show the same session, and the daemon is the only
  authority. The Session panels, the opening question and the Map all follow the
  `operating_session.*` events on the shell event stream (`/v1/events` and its WebSocket twin,
  ADR 0099).
- **Each control action carries:**
  - the **expected generation**: the archive's `session_generation` from the state the window
    showed;
  - the archive identity (the existing `X-SM-Archive-Id` request guard);
  - a **request id** minted by the window for this press.
- **Every state carries its generation, including "none open".** A bare "none open" would be
  ambiguous: a delayed action could observe none, another window could open and close a session,
  and the delayed action would still match none. The generation advances across both transitions,
  so the delayed action no longer matches.
- **The order of a control transaction:**
  1. **Replay check.** If the request id has an applied-request record, return the **recorded
     result** (the generation and the session it produced), and change nothing. The stale check is
     not applied to an already-applied retry. If the same request id arrives with a different
     action or target, it is refused as a misuse.
  2. **Expiry evaluation** (§3).
  3. **Stale check.** If the expected generation is not the current one, refuse with a distinct
     stale-action code, change nothing, and return the current state with its generation; the window
     redraws to it. Refusals are not recorded, so a retry of a refused action is refused again on
     the same check.
  4. **Apply** the transition, advance the generation, and write the applied-request record (the
     request id, the action, the target, the result and the time), all in one transaction.
- **Applied-request records:**
  - **Durable across restart:** they are written to the archive file, in the transition's
    transaction. A retry after a crash or restart therefore returns the recorded result, or, if
    the transaction never committed, is evaluated fresh.
  - **Bounded retention:** the bound is an implementation-design number, and the operator is asked
    if it is ever visible. After a record expires, a late retry is evaluated as a new action. The
    generation has moved by then, so the stale check refuses it, which is safe.
- **Examples:**
  - window A presses New at generation g, which becomes g+1; window B's delayed Continue, sent
    against g, is refused;
  - window A picks up S0 while window B presses New, both at g: the first applied wins, and the other
    is refused;
  - window B saw "none open" at g. Window A then opened (g+1) and closed (g+2) a session. B's
    delayed New at g is refused.
  - window A's New times out and is retried with the same request id: the first attempt had
    committed, so the retry returns the recorded result, and no second session is opened.
- **The opening question** can be shown in several windows at once. The first answer wins. Each
  other window closes its question when the resulting event arrives. A later answer is refused as
  stale, so nothing changes twice.
- **Closing a window** never affects the session (the browser cannot ask on close; acceptance
  entry 21).

### 6. Asking on opening (operator ruling)

When a window with a Session panel loads:
- **a session is open and within its gap:** no question; the window shows it;
- **no session is open and an eligible closed session exists:** the window asks "Continue the
  previous session or start a new one?"
  - "Eligible" means the latest closed session whose `logbook` is the current effective default
    (§3).
  - The question names that session's start time and contact count. It offers Continue (pick-up of
    that session), New session, and Not now.
  - With Not now, the panel shows "No session in progress". The next live QSO opens a new session.
- **no session is open and no eligible session exists:** no question. That covers a new archive,
  and the first opening after a default change to a logbook with no sessions. The latest closed
  session belonging to the previous default is **not** offered, because pick-up would refuse it.
  The panel shows "No session in progress", with New session.

A window without a Session panel (the Map, Settings, the Logbook) never asks; it follows the
current state.

### 7. Consumers of the one membership

- **Session panel:** looks and feels as today (same columns, in-place edit, export, email). Its rows
  are the current session's members, read from the daemon and updated live. It shows:
  - the session's start time, contact count and state (open, or closed with the reason);
  - the controls New session and Continue or pick-up (§8).

  The per-tab `sessionStorage` lists retire.
- **Map:** in session mode it plots the current session's members. Whether the last-N-hours window
  remains as a clearly labelled alternative is the Map decision in inbox item 5, not this ADR.
- **Session email and export:**
  - the request names the session `id` and the selected member UUIDs, and the daemon refuses
    non-members. The UUIDs are still listed so that a not-yet-emailed delta can be selected;
  - "already emailed" comes from the row's `sm_fwrd_by_email_status`, not a per-tab flag, so every
    window agrees;
  - ADR 0095 (exports report exactly what they delivered) is unchanged;
  - the mark race (inbox item 1) is fixed on its own track.
- **FT8 repeat hold after Ignore (inbox item 2):**
  - **The verdict moves to the daemon.** The deciding tab no longer supplies "no qualifying QSO in
    my session". The daemon decides from the current session's membership. The scope stays the
    recommended same call, band and mode or profile (the earlier draft's open choice, O4).
  - **Stale-action protection remains.** An Ignore names `run_id`, a `hold_id`, and the held
    station's call, band and mode. The daemon applies it only if that run is live and that hold is
    still the open one for that station. Otherwise it refuses as stale and changes nothing. A
    delayed Ignore can never act on a different caller or a replacement run.
  - **Rechecked at use:** the single-use bypass is used only if, at the moment of the direct answer,
    the current session still holds no qualifying QSO. A New session or a pick-up between Ignore and
    use is therefore judged against the session in force when it matters.
  - **With no session open,** "no QSO in the session" holds, and the Ignored caller is eligible.
    Operator to confirm (O5).
  - The rest of the draft (consumption, an unsuccessful direct answer, the first hold unchanged)
    carries over. Its deciding-tab and tab-lifecycle parts are replaced by this section.

### 8. Session panel controls

- **New session:** closes the current session and opens an empty one.
- **Continue:** resumes the most recent **eligible** closed session (§3, §6). It is shown only when
  one exists.
- **Choosing an older session to resume, or browsing past sessions:** open question O3.

Every control obeys §5. Wording follows the no-unactionable-instructions ruling: a control appears
only where it can act.

### 9. Boundaries: sessions, FT8 runs and contest occurrences

| | Operating session | FT8 run | Contest occurrence (ADR 0096) |
|---|---|---|---|
| Started by | first live QSO, New, Continue, pick-up | Call CQ, answering, auto-work | declared by the operator |
| Ends | gap, New, pick-up of another, archive or logbook change | Stop, abandon, last FT8 subscriber gone | its declared window |
| Identity | session UUIDv7 (archive) | `RunID` UUIDv7, `APP_SM_RUN_ID` on QSOs | occurrence identity (archive) |
| Membership | explicit, live paths | the run's own QSOs | explicit, in contest mode |
| Owns | the Session panel, Map, email, FT8 eligibility | the run's declines and bypasses | serials, submission export |

- The three concepts are **independent**. A session contains runs and may contain contest and
  non-contest QSOs; a contest usually spans several sessions. None of them creates, closes or
  supplies membership for another.
- A run can cross a session boundary (New or pick-up mid-run). Its next QSO joins whichever
  session is open at storage time.
- An archive or logbook change ends both, because it requires a restart.
- Whether a selected contest suspends gap closing is U3.

## Unresolved (operator rulings, not decided here)

- **U1. What counts as activity:** a logged QSO only, or any operator action. Also, whether FT8
  decoding, an armed FT8 run or a CQ with no answer keeps a session open.
- **U2. The default gap** (configurable, per the operator). Pick-up makes an early gap close cheap
  to recover from, but not free (O1).
- **U3. Contest policy:** a gap longer than typical contest off-times (about 1 h), or suspending gap
  closing while a contest is selected (the Phone form's contest toggle or the header contest,
  W-0023), or another rule.

## Open questions (operator or reviewer)

- **O1. A split session.**
  - **The case:** a gap closes S0 during a break, and the next QSO opens S1. Picking up S0 then
    leaves S1's QSOs in S1. Pick-up does not repair the split: it resumes **future** logging into
    S0 only.
  - **Recommendation (reviewer):** no automatic movement of members on pick-up.
  - **The open decision:** if folding a successor back proves to be needed, it is a **separate
    explicit action** with a preview of the membership it would move. It is not part of pick-up.
    Whether to build it is left open. It is the correction tooling ADR 0049 feared, deferred.
  - U2 and U3 decide how often a split happens.
- **O2.** Explicit Add to session and Remove from session (for example from the Logbook view),
  for existing contacts and imports. Not proposed for the first slice.
- **O3.** Resuming an older session than the latest, and browsing past sessions on the panel.
- **O4.** The FT8 eligibility scope: same call, band and mode (recommended), or any QSO with the
  call.
- **O5.** The FT8 Ignore when no session is open (§7).
- **O6. The first start after this ships:** no session exists, and the first live QSO opens one.
  Each open tab's `sessionStorage` list is not imported. That avoids trusting tab-supplied
  membership, but the panel empties once at upgrade.

## Alternatives considered

### A. Keep the per-tab session and share it between windows (BroadcastChannel or localStorage)

The daemon would stay unchanged. Rejected:
- the daemon's FT8 check still cannot see the session;
- a window's view depends on another window staying open;
- several lists still disagree after a tab closes.

ADR 0049 rejected the same coupling.

### B. Derived sessions: a time-gap rule evaluated at read time

ADR 0049's recomputable form. Rejected:
- the operator's boundaries (New, pick-up, the archive and logbook rules) are not functions of QSO
  times;
- an import or a time edit would silently move a QSO in or out.

ADR 0096 rejected the window-derived form for contest membership for the same reason.

### C. A stamped `session_id` column on `qso` (ADR 0049's decision)

Rejected for the association:
- **membership changes would be QSO updates.** Setting or correcting a `session_id` after the
  insert (a fold-back O1, an add or remove O2, a closure correction) updates the `qso` row. The
  update trigger bumps `revision` (ADR 0050), and the row re-syncs with no contact change. That is
  revision churn for a local concept; inbox item 1 shows how such bumps already disturb a
  revision-keyed consumer.
  - A new column does not by itself enter the SM Cloud payload. The objection is the churn, not the
    payload.
- it puts a new column on the most load-bearing table.

The association is written in the same transaction, so atomicity is equal; only the coupling
differs.

### D. One station-wide session across archives

Rejected by the operator's ruling that changing the archive closes the session. Sessions describe
QSOs, which live in an archive's file.

### E. Ask "Close session?" when a window closes

Rejected:
- a page cannot show its own question on close; Firefox shows only its generic leave-page dialog
  (acceptance entry 21);
- that dialog cannot tell a close from a reload;
- with several windows open, closing one should not close the session.

The operator chose to ask on opening instead.

### F. A tab-supplied FT8 eligibility verdict (the earlier item 2 draft)

Superseded: the daemon now holds the membership. The draft's stale-action protection (`run_id`
and `hold_id`) is kept.

### G. Purely manual sessions

Rejected, as in ADR 0049: the everyday operator would forget, and the panel would be empty.

## Consequences

- **New authoritative state in the archive file:**
  - a log-set migration (a session table, its open intervals, and the membership association);
  - sqlboiler regeneration;
  - the migration-head fallout across whole-tree tests.

  It is the first log-schema change since schema 19.
- **New API and events:**
  - read the current session and its members; list sessions (scope per O3);
  - New, Continue and pick-up, each with an expected generation and a request id, plus their
    durable applied-request records;
  - departure marks, written by archive activation and the default change, and resolved at start;
  - `operating_session.*` events;
  - `docs/v2-design/api-endpoints.md` changes with them.
- **ADR 0094 is superseded in part** when this is accepted: per-archive Session lists stay, but
  they are daemon-owned and shared by every window, not per tab. ADR 0049 stays Rejected; this ADR
  records that its trigger fired.
- **The live-submit and FT8-sink transactions gain session work:** expiry evaluation, a possible
  session creation, the association write and the activity update.
  - **No session-policy decision, operator prompt or external dependency blocks logging.**
  - **A database failure** in that work rolls the QSO back atomically, as any failure in that
    transaction does.
  - The added writes are local SQLite statements in a transaction that already exists.
- **Sessions are local only.** A rebuild from SM Cloud restores QSOs without sessions. That is
  input to the backup and restore design (inbox item 4).
- **Going forward only:** existing contacts have no session.
- **The Map surprise (item 5) and the FT8 Ignore machinery (item 2) get simpler.** The tab-verdict,
  deciding-tab and tab-lifecycle rules go away. The stale-action rules stay.

## Triggers to revisit

- Gap closes misfire on air (a sitting split or merged): revisit U2 and U3, and decide O1's
  explicit fold-back.
- A second operator or a real multi-op contest: sessions become the carrier for operator identity
  per shift (ADR 0049's profile trigger). Revisit the record shape.
- Sessions are wanted in SM Cloud or on another device: design their sync explicitly. Do not stamp
  `qso` to get it.
- Operators routinely want to add existing or imported QSOs: build O2.

## References

- ADR 0049 (Rejected; this ADR's revisit), 0050 (`revision`), 0084 (discardable sidecar), 0094
  (per-archive Session lists), 0095 (session exports), 0096 (contest occurrences: explicit
  membership), 0097 (default change at restart; its states), 0099 (event streams).
- `docs/dogfood-inbox.md`, the triage of the 2026-11-06 notes: the cross-cutting daemon-session
  entry, item 1 (independent), item 2 (FT8 draft), item 5 (Map).
- `frontend/app/src/lib/operate/session.svelte.ts`; `frontend/app/src/lib/map/mapData.svelte.ts`;
  `internal/api/handler_qso.go` (live classification); `internal/api/handler_session_email.go`;
  `internal/qsoservice/submit.go`, `submit_batch.go`, `restore.go`;
  `cmd/smd/lifecycle_adapters.go` (the FT8 sink); `internal/ft8/repeathold.go`,
  `internal/ft8/sequencer.go` (`RunID`); `internal/stationevents/stationevents.go`.
- `docs/reports/acceptance-W-0024-set-as-default.md`, entry 21 (the browser leave-page dialog).
