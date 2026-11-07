---
number: 0100
title: Daemon-owned operating sessions with explicit membership
status: Proposed
date: 2026-11-06
---

# 0100 — Daemon-owned operating sessions with explicit membership

**Proposed (2026-11-06). Design only. Nothing is authorised or built.** The operator has ruled
on activity, the gap and the contest policy (U1–U3, P1–P3), the Map (§7), and O4, O5, O6 and O8
(P4), and the simplification to "A with S2, S3 and S5" (*Simplification review*); O1–O3 are
deferred with revisit triggers, and O7 is resolved by S3 (*Open questions*, below). The session-email mark fix (inbox item 1) was independent of this ADR
and has shipped (`c3826281`, W-0010 outcome 11).

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
  picking up a closed session, and closing on a default-logbook change.
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
  - a default-logbook change closes the session when the change takes effect; an archive switch
    does not, and the gap decides on return (§4).
- **Consumers:** the Session panel in every window, the Map, session email and export, and FT8
  repeat-hold eligibility all read this one membership.
- **Stale actions:** each operator action names the archive's session-state generation it was
  taken against, and the daemon refuses it if the generation has moved.

### 1. Identity and record

- **Session record:** `id` (UUIDv7), `logbook` (the logbook's UUID), `opened_at`,
  `last_activity_at`, `closed_at` and `close_reason`.
  - `close_reason` is one of `new_session`, `gap`, `logbook_changed` or `picked_up_other`. An
    archive change does not close a session (§4).
  - A resumed session keeps its `id`. Each open interval is recorded in that session's own interval
    list, so the history stays readable.
- **Archive-level session state:**
  - **`session_generation`:** one counter per archive, which advances on **every** session-state
    transition in that archive. That includes an automatic open, New, pick-up, and each close
    (gap, logbook). A QSO joining an already open session is not a transition, so the
    generation does not move.
  - **`default_epoch`** (§4).
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
  2. **Evaluate the default change, then expiry:**
     - if a default-logbook change has taken effect (§4), close the session with
       `logbook_changed`;
     - otherwise, if it is open, it is **expired** (`now ≥ last_activity_at + gap`, the one
       deadline rule used everywhere) and the gap suspension is off (U3; the contest-end rule in
       §3), close it with `gap` (§3).

     Either close advances the generation.
  3. **If none is open now,** create a session: `opened_at = last_activity_at = now`, bound to the
     effective default logbook and the archive's current `default_epoch`. Advance the generation.
  4. Write the membership association.
  5. Set `last_activity_at = max(last_activity_at, now)`: a logged live QSO is activity (U1, ruled;
     the complete list of counted activity is P1).

  So a QSO can never join a session that had already expired. That holds even when no read or
  timer ran during the gap.
- **What can and cannot stop a QSO.**
  - **Cannot:** no session-policy decision (expiry, a stale generation, a pending default change), no
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
  - **Membership never moves on its own.** Explicit add or remove actions are deferred (O2).
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
  - when a default-logbook change takes effect, at `last_activity_at` (§4). An archive switch does
    not close a session; on return the gap decides (§4).
- **Activity baseline for operator actions:**
  - **New session:** `opened_at = last_activity_at = now`.
  - **Pick-up:** reopening a session starts a new open interval, sets `last_activity_at = now`,
    and sets the session's `default_epoch` to the archive's current one. The
    resume is the baseline, so a resumed session's old timestamp cannot expire it again at once.
- **Pick-up (the operator's "restart"):**
  - It resumes a closed session, which becomes the open one; **future** live QSOs are added to it.
    Nothing already logged moves (O1).
  - Closing the current session, if any (`picked_up_other`), and reopening the chosen one are one
    transaction.
  - Only an **eligible** session can be picked up: one whose `logbook` is the current effective
    default. Others are refused, because they would mix logbooks. Every offer (§6, §8) lists eligible
    sessions only, so the panel never offers an action the daemon would refuse.
- **Where the default-change test (§4) and expiry are evaluated, in that order:**
  - **The logging transaction** (§2), which is the load-bearing evaluation;
  - **every session-control transaction** (§5), as its first step;
  - **a daemon timer** set for the open session's expiry instant. It persists the `gap` close and
    emits the event, so open panels show "closed" without waiting for the next action;
  - **a change of the configured gap** (U2), which also reschedules the timer;
  - **every read:** if a read finds the open session expired, the daemon persists the close and
    emits the event before answering. A reported state is therefore always a persisted state with
    its generation (§5).

  Every evaluation is idempotent, and whichever runs first records the close.
- **The contest end is part of every evaluation** (P2). The suspension is on only while the
  contest-active state is active **and** `now` is before its window's end. Each evaluation above
  first checks that: if the state is still recorded as active but its end has passed, the evaluation
  ends it (as ADR 0101 §4's end of window would), then evaluates expiry with the suspension off. No
  evaluation depends on the contest-end timer having run punctually.
  - **Example:** the contest ends at 12:00, its timer is delayed, and a QSO is saved at 12:02 after
    two quiet hours. The logging transaction ends the contest state, closes the overdue session at its
    `last_activity_at`, opens a new session and adds the QSO to it.
  - **Atomic, events after commit:** all the transitions an evaluation finds are persisted in its one
    transaction, each advancing the generation. Their events (contest ended, session closed, session
    opened) are published only after the commit, in that order. A rolled-back transaction publishes
    none.
  - **The session timer while suspended** is set for the contest's end, where the next evaluation is
    due.
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

### 4. Archive and default-logbook changes

*Revised 2026-11-07 by the operator's simplification ruling (S3): an archive change no longer
closes a session, so the archive serve epoch, its activation outcomes and the O7 restoration
limitation are gone. Default-logbook changes keep their epoch (S4 rejected). The earlier forms
(`abe5b7b1`, then the serve-epoch design) remain in the Git history.*

- **Archive changes: the gap decides (S3).**
  - Each archive holds its own sessions and its own QSOs, so nothing crosses archives: QSOs logged
    while B is served are in B's file and can never join A's session.
  - While an archive is not served, its sessions are not evaluated. When it is served again, its
    first evaluation (§3) applies the gap to the time since its `last_activity_at`, which includes
    the time it was not served.
  - **Returned within the gap:** its session continues. **Returned after the gap:** its session
    closes by `gap`, at its `last_activity_at`, like any idle session, and the opening question
    offers it if it is eligible (§6).
  - **Example (gap 2 h):** A's last QSO at 10:00; switch to B at 10:30. Back to A at 11:15: A's
    session continues. Back to A at 12:30 instead: A's session is closed as of 10:00.
  - **A requirement on any future feature** that reads a non-served archive's sessions (for
    example an archive summary): it applies the same gap test, so an expired session reads as
    closed.
- **Default-logbook changes: closure when the change takes effect, by a default epoch.**
  - **The rule:** a session closes when a default-logbook change **takes effect**. The evidence is
    written by the write that makes the change take effect, so evidence and effect cannot diverge;
    nothing is written when a change is merely requested. (An earlier departure mark written at
    request time was withdrawn: a selector persist failure could leave it behind, and a retained
    `not_applied_intent_pending` intent could have it cleared by an unsuccessful start.)
  - **In the archive file:** `default_epoch`, advanced **only by an actual change of the default
    pointer**, not by every pointer write (`applyDefaultChange`, `cmd/smd/default_change.go`):
    - **X→Y:** the pointer and the incremented epoch are written in one SQLite transaction;
    - **Y→Y**, the deliberate durable rewrite of an already-selected target (the crash-recovery
      branch, `cmd/smd/default_change.go:170`, and `commitPointer`): the epoch is **preserved**.
      A retained selector followed by another restart therefore closes no legitimate session;
    - **crash recovery:** the pointer and its epoch commit together or not at all. A start finds
      either (X, e) or (Y, e+1), never a mixed pair, and keeps whichever pair committed.
  - **In each session:** the `default_epoch` it was opened, or last picked up, under.
  - **The test:** a session whose `default_epoch` differs from the archive's closes with
    `logbook_changed`. That covers X→Y→X too, whether or not anything was evaluated while Y was the
    default: the epoch moved twice even though the logbook is X again. This determinism is why the
    epoch is kept (S4 rejected).
- **The closure time** is the session's `last_activity_at`.
  - Every member was stored at or before it (§2, step 5), and nothing joins after a change takes
    effect.
  - The true change instant is not recorded durably, and no evidence written before the change
    took effect is trusted for it. The operator sees "closed at hh:mm (logbook changed)", the end
    of the sitting, as for a gap close.
- **Where the test runs:** in the **same evaluation step as expiry** (§3): every logging
  transaction, before membership; every control transaction; every read; and a start node before
  `http`. The test is derived from durable state in the archive file, so the start node is not
  load-bearing: if its write fails, the next session transaction repeats it. **The default test
  runs before expiry:** both close the session at `last_activity_at`, and `logbook_changed` is the
  stronger reason.
- **What each default-change outcome does** (from `cmd/smd/default_change.go` and
  `internal/archive/default_change.go`):

  | Outcome | Epoch moves? | Sessions and controls |
  |---|---|---|
  | `effective` / `applied_cleanup_pending` | yes, if the pointer changed X→Y (in that transaction); a Y→Y durability rewrite does not move it | the session closes at the first evaluation after an X→Y change |
  | `not_applied_intent_pending` (the selector survives; a later start retries) | no | unchanged **between attempts**; a later `effective` start moves the epoch then |
  | status `pending_unclear` (error code `default_change_pending_unclear`: restart failed and the change could not be withdrawn; it applies at the next restart unless cancelled; `internal/archive/default_change.go:255,463`) | not yet | unchanged until a start applies it; an X→Y pointer change moves the epoch then |
  | `restart_failed`, `not_applied_abandoned`, cancelled, `withdrawal_unconfirmed` | no | unchanged |

  - Whether the `pending_unclear` status is reachable in production is the separately recorded
    wiring limitation, not this ADR's concern.
  - **Session controls are never blocked by a pending change.** There is nothing to block on.
    Between attempts of a retained intent, New, Continue and pick-up work normally. Whatever session
    is open when the change finally takes effect is the one it closes.
  - Live admission during a change stays governed by its own seals (ADR 0097), not by this ADR.
- **Worked sequences:**
  - **A selector persist failure:** nothing was written, so nothing changes.
  - **A default change retained across two starts, then effective:** sessions and controls are
    normal throughout. The `effective` start moves `default_epoch`, and the session open at that
    point closes.
  - **X→Y→X with nothing evaluated while Y was the default:** the epoch moved twice, so X's session
    closes at the first evaluation after the return.

### 5. Several windows, conflicting choices

- **One session for every window:** all windows show the same session, and the daemon is the only
  authority. The Session panels, the opening question and the Map all follow the
  `operating_session.*` events on the shell event stream (`/v1/events` and its WebSocket twin,
  ADR 0099).
- **Each control action carries:**
  - the **expected generation**: the archive's `session_generation` from the state the window
    showed;
  - the archive identity (the existing `X-SM-Archive-Id` request guard).

  There is no request id and no recorded result (operator ruling 2026-11-07, S2).
- **Every state carries its generation, including "none open".** A bare "none open" would be
  ambiguous: a delayed action could observe none, another window could open and close a session,
  and the delayed action would still match none. The generation advances across both transitions,
  so the delayed action no longer matches.
- **The order of a control transaction:**
  1. **Default-change and expiry evaluation** (§3, §4).
  2. **Stale check.** If the expected generation is not the current one, refuse with a distinct
     stale-action code, change nothing, and return the current state with its generation; the window
     redraws to it.
  3. **Apply** the transition and advance the generation, in the same transaction.
- **A refusal is not a rollback** (acceptance review 2026-11-07). "Changes nothing" refers to the
  **requested control** only. A close found by step 1's evaluation (default change or
  expiry) is committed, advances the generation, and publishes its event after the commit, **even
  when step 2 or another refusal then rejects the control**. Only a database failure rolls the transaction
  back, closes included; the evaluation is then repeated by the next one (§3, P3's retry).
  - **Example:** a window shows S open at generation g. S's deadline passes and the timer has not
    yet run. The operator presses New against g. Step 1 closes S by gap (g+1), step 2 finds g
    stale and refuses New. S's close stands, its event is published, and the window redraws at
    g+1, where the operator can act again.
  - **Consequence the operator sees:** a control pressed on a session that had silently expired is
    refused as stale once, and the window redraws to the closed state. This is deliberate: the
    window's view was out of date. An exemption for closes found by the same transaction was
    considered and not adopted (reviewer, 2026-11-07): the same press would otherwise succeed or fail
    depending on whether the timer or the control discovered the expiry.
- **Retries after a lost response (S2).** A retry carries the generation of the window's original
  press, unchanged.
  - **If the first attempt committed,** the generation has moved, so the retry is refused as stale
    and the window redraws to the state the first attempt produced. The action can never apply
    twice. **Accepted:** the operator may see one stale refusal after a lost response, with the
    correct state shown.
  - **If the first attempt never committed,** the retry is evaluated as the press it is.
  - **Client rule:** a retry keeps its original generation. A window never silently re-reads the
    state and resends the press against the new generation; after a refusal the operator acts again.
  - **Other refusals** (for example a transient database failure, which leaves the generation
    where it was) can apply on an unchanged retry once their condition clears.
- **Examples:**
  - window A presses New at generation g, which becomes g+1; window B's delayed Continue, sent
    against g, is refused;
  - window A picks up S0 while window B presses New, both at g: the first applied wins, and the other
    is refused;
  - window B saw "none open" at g. Window A then opened (g+1) and closed (g+2) a session. B's
    delayed New at g is refused.
  - window A's New times out and is retried at g: the first attempt had committed (g+1), so the
    retry is refused as stale, the window shows the new session, and no second session is opened.
- **The opening question** can be shown in several windows at once. The first answer **that
  changes state** (Continue or New) wins. Each other window closes its question when the resulting
  event arrives. A later answer is refused as stale, so nothing changes twice. Not now is local to
  its window (§6).
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
  - **Not now across windows: local** (operator ruling 2026-11-07, "yes, local Not now",
    following the reviewer's recommendation; raised in the acceptance review).
    - **Ruled: local.** Not now dismisses the question **in that window only**. It sends
      nothing, changes no session state and does not move the generation. Every other window keeps
      its question, and a Continue or New from any of them still applies against the same
      generation. The dismissing window then follows that result through the event, like any
      other window.
      - **Example:** windows A and B both ask at generation g. A chooses Not now; B then chooses
        Continue at g, which applies (g+1), and A's panel shows the continued session.
      - **Why:** Not now expresses "not from this window, not yet", not a station decision. Making
        it station-wide needs a new recorded state ("declined") with its own reset rules, for one
        answer that changes no session.
    - **Rejected alternative: station-wide.** Not now becomes a daemon transition, recorded and advancing
      the generation, so every window closes its question; a later Continue from B at g is refused
      as stale and B must press Continue on the panel instead. It gives one answer for the
      station, at the cost of a new state.
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
- **Map — operator ruling (2026-11-07), following the reviewer's recommendation; inbox item 5;
  this ADR remains Proposed:**
  - **Operator-observable outcome:** a newly opened Map and a Session panel show the same session
    identity and underlying member UUIDs, before map filters and coordinate availability. A new
    window never creates an empty private session. The nearest confusable outcome is a Map that
    still shows recent contacts excluded from the panel's session.
  - **Shared display rule (also amends the panel presentation above and §6):** show the open session;
    when none is open, retain the latest eligible closed session in the effective default logbook,
    clearly labelled Closed, alongside "No session in progress". If neither exists, both views are
    empty. This is read-only selection from the daemon's session records, not a second session
    state. Showing a closed session never resumes it, suspends expiry, or changes §7's FT8 check
    when no session is open. New session replaces the displayed membership in both views with an
    empty session; pick-up follows the chosen session. Past-session browsing remains O3.
  - **First slice:** the Map follows this session automatically. Remove the last-N-hours selector
    from this view; do not apply a hidden time cutoff or inherit `sm-map-window`. Imports and
    unrelated contacts remain outside it even when their times fall within the session's intervals.
    An edit of a member's QSO time does not remove it. Deleted members are excluded as in §2.
  - **Presentation:** show the session's start and open/closed state. Retain band filtering and
    distinguish the session's member count from the plotted and filtered counts; a missing location
    does not remove membership. An empty session, a load failure and partial coverage have distinct
    presentations. A failed read never falls back to a recent-hours query.
  - **Several windows and recovery:** New, pick-up, gap closure, archive/default-logbook changes,
    QSO edits and deletion update both views. A reconnect or return to a visible window re-reads
    authoritative state and members. A late read from a previous archive or displayed session cannot
    overwrite the new view. Opening or refreshing the Map is passive and never counts as activity.
  - **Alternatives weighed:** keeping only the existing time window and documenting the difference
    leaves the operating surprise intact. Keeping an explicit Recent contacts alternative preserves
    historical viewing but adds another scope to choose and explain; defer it until requested.
    Emptying both views immediately at gap closure is consistent, but removes the just-finished
    sitting from view; retaining the labelled closed session is recommended. None of these choices
    revives ADR 0049's stamped `qso.session_id` design; membership remains the association in §2.
- **Session email and export:**
  - the request names the session `id` and the selected member UUIDs, and the daemon refuses
    non-members. The UUIDs are still listed so that a not-yet-emailed delta can be selected;
  - "already emailed" comes from the row's `sm_fwrd_by_email_status`, not a per-tab flag, so every
    window agrees;
  - ADR 0095 (exports report exactly what they delivered) is unchanged;
  - the mark race (inbox item 1) is fixed on its own track.
- **FT8 repeat hold after Ignore (inbox item 2):**
  - **The verdict moves to the daemon.** The deciding tab no longer supplies "no qualifying QSO in
    my session". The daemon decides from the current session's membership. The scope is the same
    call, band, mode and submode (P4, O4, ruled).
  - **Stale-action protection remains.** An Ignore names `run_id`, a `hold_id`, and the held
    station's call, band and mode. The daemon applies it only if that run is live and that hold is
    still the open one for that station. Otherwise it refuses as stale and changes nothing. A
    delayed Ignore can never act on a different caller or a replacement run.
  - **Rechecked at use:** the single-use bypass is used only if, at the moment of the direct answer,
    the current session still holds no qualifying QSO. A New session or a pick-up between Ignore and
    use is therefore judged against the session in force when it matters.
  - **With no session open,** "no QSO in the session" holds, and the Ignored caller is eligible
    (P4, O5, ruled).
  - The rest of the draft (consumption, an unsuccessful direct answer, the first hold unchanged)
    carries over. Its deciding-tab and tab-lifecycle parts are replaced by this section.

### 8. Session panel controls

- **New session:** closes the current session and opens an empty one.
- **Continue:** resumes the most recent **eligible** closed session (§3, §6). It is shown only when
  one exists.
- **Choosing an older session to resume, or browsing past sessions:** deferred (O3).

Every control obeys §5. Wording follows the no-unactionable-instructions ruling: a control appears
only where it can act.

### 9. Boundaries: sessions, FT8 runs and contest occurrences

| | Operating session | FT8 run | Contest occurrence (ADR 0096) |
|---|---|---|---|
| Started by | first live QSO, New, Continue, pick-up | Call CQ, answering, auto-work | declared by the operator |
| Ends | gap, New, pick-up of another, default-logbook change | Stop, abandon, last FT8 subscriber gone | its declared window |
| Identity | session UUIDv7 (archive) | `RunID` UUIDv7, `APP_SM_RUN_ID` on QSOs | occurrence identity (archive) |
| Membership | explicit, live paths | the run's own QSOs | explicit, in contest mode |
| Owns | the Session panel, Map, email, FT8 eligibility | the run's declines and bypasses | serials, submission export |

- The three concepts are **independent**. A session contains runs and may contain contest and
  non-contest QSOs; a contest usually spans several sessions. None of them creates, closes or
  supplies membership for another.
- A run can cross a session boundary (New or pick-up mid-run). Its next QSO joins whichever
  session is open at storage time.
- A default-logbook change ends both: it requires a restart, which ends the run, and its epoch
  closes the session. An archive switch ends the run; the departed archive's session is decided by
  the gap when that archive is next served (§4).
- Gap closing is suspended while the daemon-owned contest-active state is on (U3).

## Operator rulings on U1–U3 (2026-11-06) and what stays open

**Attribution:** U1 and U3 were recommended by the reviewer and then **confirmed by the station
operator** in their own words: "yes, confirm U1 (b) and U3 (b)" (2026-11-06). The U2 default of
2 hours was approved by the station operator on the same day.

- **U1. Activity: ruled (b).** Activity is a logged QSO plus a **finite, documented set of
  deliberate operating actions**.
  - **Not activity:** passive decodes, an open window, and merely remaining armed. They never reset
    the clock.
  - **The counted actions:** ruled 2026-11-07 (operator), P1 below.
  - **Unchanged:** §2, step 5 (a logged QSO is activity); New session and pick-up reset the
    baseline (§3).
- **U2. The gap: configurable, default 2 hours (both operator rulings).**
  - **Where:** a station-wide `config.json` setting, edited from Settings and documented in
    `docs/v2-design/config.md` when it is built. One value applies to every window and archive.
    Ruled 2026-11-07 (operator), P3 R1 below.
  - **When a change takes effect (ruled 2026-11-07, operator: P3 R2 and R3 below, which refine
    this outline):** immediately, without a restart.
    - Saving a new gap **re-evaluates expiry** of the open session and **reschedules its timer**
      (§3) to the new deadline, so shortening it never waits for the old deadline.
    - Lowering it so that `now ≥ last_activity_at + gap` closes the open session at once, at its
      `last_activity_at`, **unless the contest suspension (U3) is active**.
  - **Default (operator ruling, 2026-11-06): 2 hours of inactivity** closes a session. It remains
    configurable. It is longer than the typical one-hour contest off-time, independently of U3. Pick-up makes an early gap close cheap to recover from for future logging,
    but not free (O1).
- **U3. Contest policy: ruled (b), with a design condition.** Gap expiry is suspended while a contest
  is active.
  - **The condition:** the suspension depends on an **explicit, daemon-owned contest-active state**,
    not on whichever browser tab has a contest selected. Every window must agree on when that state
    starts and ends.
  - **Today:** the contest selection (the Phone form's contest toggle and the header contest,
    W-0023) is per-tab state, so that daemon-owned state does not exist yet. Designing it, with its
    own start, end, restart and several-window semantics, is a prerequisite. It belongs with W-0023.
  - **While suspended:** the session ends only by New session, pick-up of another session, or a
    default-logbook change (§4).
  - **The baseline when the contest state ends:** an expiry already due is applied at once, without
    inventing activity. Ruled 2026-11-07 (operator), P2 below; the rejected alternative (the end
    counts as activity) is recorded there.

## Operator rulings P1 and P2 (2026-11-07): counted actions and the contest-end baseline

**Status: ruled 2026-11-07 by the station operator; design only, and this ADR remains Proposed.**
Drafted by the agent to the reviewer's brief and approved by the reviewer, with three clarifications
written in: the credit is bound to the session at acceptance (P1), the contest end is checked in
every evaluation (§3, P2), and one deadline rule, `now ≥ last_activity_at + gap` (§2). The operator
then confirmed, in their own words: "I confirm P1 and P2 as my rulings, including the three
clarifications".

### P1. Counted activity (completes U1)

**Rule (ruled): activity is a small, closed list of daemon-observable events. Anything not on
the list never counts.**

| Class | Event | Counts when | On failure or refusal |
|---|---|---|---|
| Logged QSO | a live QSO stored into the session (§2) | the QSO's transaction commits | rolled back with the QSO; nothing counts |
| Baseline resets | New session, Continue or pick-up (§3) | the session-control transaction commits | refused or stale (§5): nothing changes |
| Deliberate operating | `POST /v1/ft8/cq/start` (Call CQ) | the daemon accepts it (202): the run is installed | any refusal or error response (not armed, already in progress, archive guard, bad field, FT8 disabled): not counted |
| Deliberate operating | `POST /v1/ft8/qso/start` (answer a CQ) | accepted (202) | as above |
| Deliberate operating | `POST /v1/ft8/qso/work` (work a caller) | accepted (202) | as above |
| Deliberate operating | `POST /v1/ft8/cq/pick` (answer a listed caller) | accepted (202) | `ft8_answerer_not_listed`, `ft8_no_cq_pick_run`, `ft8_cq_contact_in_flight`: not counted |

- **Accepted, not completed.** A deliberate action counts at the daemon's acceptance, because that
  is the operator's act. It does not wait for the first transmission or a completed QSO, and a later
  abandon, silence or timeout does not undo it. Completion is already counted, as a logged QSO.
- **Bound to the session at acceptance.** When the FT8 controller accepts the action, the daemon
  captures the **acceptance instant** `accepted_at` and the archive's session state at that instant:
  its `session_generation` and the open session's `id`, or "none open". Capturing is an ordinary
  evaluation (§3), so a session already due to close at `accepted_at` is closed first and the
  capture records "none open".
- **How the credit is written.** One session transaction, started straight after acceptance and
  before the response is sent:
  1. **The credit applies only if the generation is unchanged** since the capture, and the capture
     named an open session. The generation advances on every transition (§1): a gap close, New,
     pick-up (even of the same session), or a default-logbook change. So an unchanged generation means the same
     session, in the same open interval, with nothing in between.
  2. If it applies, set `last_activity_at = max(last_activity_at, accepted_at)`, **the acceptance
     instant, not the write's**.
  3. Then evaluate as every evaluation does, at the write's `now` (§3, including the contest end),
     and reschedule the timer.

  So a delayed write can never extend a replacement session or reopen a closed one. If the generation
  moved, the credit is dropped, and the transition that moved it already set its own baseline (New,
  pick-up) or closed the session.
- **Server-side acceptance is what counts.** The write does not depend on the response reaching the
  browser. A lost 202 changes nothing, and a client retry after it is refused as already in
  progress (409), which counts for nothing, so the action is credited once.
- **An action never opens a session.** With none open at acceptance, it changes nothing; the next
  live QSO opens one (§2). So a late action cannot rescue a session that had already expired.
  - **A failed activity write never undoes or refuses the action.** Session policy cannot block
    operating (§2). The failure is logged, and its only effect is that the session may close earlier
    than it should; pick-up recovers it for future logging. (A broken database also stops logging, so
    this is the same failure, seen sooner.)
- **Never counted:** decodes; an open, visible or refreshed window (including the Map, §7); an open
  FT8 subscription; remaining armed (`tx/arm`); the run's own automatic CQ repeats, sequencing,
  retries and the bag drain; Tune (`rig/tune`), rig, VFO and band changes; generic clicks, form
  typing and drafts; serial allocation (`POST /v1/contest-serial`); settings saves; Abandon, Stop,
  Next, Skip, Ignore, bag, unbag and Resume.
- **Observable examples (2-hour gap):**
  - Last QSO 14:00. At 15:30 the operator presses Call CQ; nobody answers and the run keeps calling
    by itself. With no further QSO or deliberate action, the session closes at 17:30 with
    `closed_at` 15:30. The CQ repeats between 15:30 and 17:30 did not count.
  - Last QSO 14:00. The operator leaves FT8 armed and decoding, with the window open, and walks away.
    The session closes at 16:00 with `closed_at` 14:00.
  - Last QSO 14:00. At 15:50 the operator answers a CQ; the daemon refuses with
    `ft8_tx_not_armed`. Nothing counts, and the session still closes at 16:00.
  - Last QSO 14:00. At 16:10 (already expired) the operator presses Call CQ. The session closes as
    of 14:00; no new session opens until the first QSO of the run is logged.
  - Last QSO 14:00, Call CQ accepted at exactly 16:00:00. The deadline rule is `now ≥
    last_activity_at + gap`, so the session is already expired at acceptance: it closes as of 14:00
    and the action is not credited.
  - Call CQ accepted at 15:59:59, and a New session from another window commits before the credit is
    written. The generation has moved, so the credit is dropped; the new session's baseline is its
    own `opened_at`.
- **Alternatives considered:**
  - **Logged QSOs only.** The simplest rule, but a long, unanswered CQ run on a quiet band closes the
    sitting while the operator is plainly operating. Rejected by U1 (b).
  - **Count at completion only** (the action counts when its exchange logs). That duplicates the
    logged-QSO rule and adds nothing.
  - **Count Phone/CW serial allocation** (committing a callsign in contest mode). Deliberate and
    daemon-observable, but during a contest the gap is suspended anyway, so it matters only in the
    minutes around a contest's end (P2). Not recommended; it adds a source for one edge.
  - **Count every operator request, or rig changes.** A band or VFO change, a Tune or a settings
    save is not operating; counting them keeps a session alive for activity that is not a sitting.
    Rejected by the brief.

### P2. The baseline when the contest state ends (completes U3's open choice)

**Rule (ruled): when the contest-active state ends, overdue expiry applies at once, and the
session keeps its last actual activity time.** Neither an explicit Stop nor the automatic end at
the window's end is activity, and neither moves `last_activity_at`.

- **Mechanism:** the transaction that ends the contest state (a Stop, the end-of-window timer, or
  any evaluation that finds the end has passed, §3) evaluates expiry as its last step, with the
  suspension now off. An overdue session closes with `gap`, `closed_at =
  last_activity_at`, the generation advances and the event is emitted. A session still within its
  gap stays open, and its timer is rescheduled to `last_activity_at + gap`.
- **Explicit Stop.** Contest 12:00Z Saturday to 12:00Z Sunday; last contest QSO Saturday 17:20;
  Stop pressed Saturday 21:00. The session closes at once, `closed_at` Saturday 17:20. A Stop at
  Saturday 18:00 leaves it open until 19:20 unless the operator logs or acts again.
- **Automatic end.** Same contest, operator stopped logging Saturday 17:20 and never pressed Stop.
  At Sunday 12:00 the state ends itself and the session closes as of Saturday 17:20.
- **Restart after the contest ended.** The daemon was off over the end (power cut Saturday night,
  restarted Sunday 13:00). ADR 0101 §4 does not restore the contest. The first evaluation after
  start finds the suspension off and closes the session as of Saturday 17:20.
- **A QSO in progress at the end or at Stop.** Draft preservation is ADR 0101's: the draft keeps its
  occurrence and serial and can be saved. **Which operating session receives it is decided
  separately**, by the logging transaction (§2) when it is saved.
  - Last QSO 11:40, contest ends 12:00, draft saved 12:03: the session is within its gap and
    receives it.
  - Last QSO Saturday 17:20, a draft begun Sunday 11:55, saved 12:02: the session closed at 12:00 as
    of Saturday 17:20, so the saved QSO opens a new session. It is still a member of the contest
    occurrence it names (ADR 0101 §5); the two memberships are independent (§9). The draft itself
    was never activity.
- **The alternative: a fresh gap from the end.** The contest's end (or the Stop) counts as activity,
  so the session stays open for one further gap.
  - **What the operator sees:** after the weekend example's automatic end, the Session panel, the Map
    and the header clock keep showing the contest weekend's session as open until Sunday 14:00. A
    non-contest QSO at Sunday 13:00 joins that weekend session instead of starting a new one, and a
    Session email at 13:30 would include it. Under the ruling they show the weekend session
    as Closed from 12:00 (§7), and the 13:00 QSO opens a new session.
  - **Why rejected:** it manufactures activity that did not happen, makes `closed_at` depend
    on how the contest ended, and keeps a session open after a sitting that may have ended a day
    earlier. Its benefit, not splitting a sitting that continues straight after the end, is
    available by pick-up (with O1's limitation) or by logging within the gap, which keeps the session
    open under either rule.

## Operator ruling P3 (2026-11-07): the gap setting (completes U2)

**Status: ruled 2026-11-07 (operator): R1–R3 and R4. P3 is complete.** Drafted by the agent to the reviewer's
brief. **Reviewer approval (2026-11-07): R1–R3 technically approved**, including R2's documented
restart exception and the serialization clarification. **Operator ruling (2026-11-07):** R1–R3
"Confirmed", and R2's restart exception "Accept"; R4's values adopted from the reviewer's
recommendation (below). **R2 was then simplified by the operator's ruling S5 (2026-11-07):** the
boundary design and its restart exception are withdrawn; R2 below is the ruled form. This ADR
remains Proposed. Already ruled and not reopened here: the gap is **configurable**, and its
default is **2 hours** (U2, 2026-11-06).

### Recommendations

- **R1. One station-wide setting in `config.json`, edited from Settings.**
  - One value for every window and every archive. It is station policy, like the FT8 repeat cap,
    not archive data, so it is not stored in an archive file and does not travel with an archive.
  - **Sparse on disk, served resolved** (config.md §4, "read/serve resolver"): absent means the
    ruled default of 2 hours; `GET /v1/config` serves the effective value, so Settings shows it even
    when the file does not hold it. The field's name and unit are an implementation detail, settled
    with R4's granularity and documented in `docs/v2-design/config.md` when built.
  - A non-served archive has no evaluations while it is not served. At its next start, its first
    evaluation (§3) uses the value in force then.
- **R2. A saved change applies from the next evaluation, without a restart** (simplified by the
  operator's ruling of 2026-11-07, S5; the earlier effective-policy boundary, its pending
  boundaries and its restart exception are withdrawn and remain in the Git history).
  - It becomes config.md §11's second **explicit live side effect**, alongside `ft8.tx.max_repeats`
    (§11.5): the PUT persists the candidate first, then hands the committed value to the session
    service, which **runs an evaluation at once** (§3) under that value and reschedules the timer to
    the new deadline, or to the contest end while suspended. A rejected or failed write changes
    nothing. Like the FT8 precedent, the latest committed value is the one applied.
  - **Accepted explicitly (S5):**
    - **lengthening can preserve an overdue but still-open session:** if the old deadline has
      passed but no evaluation has closed the session yet, the save's evaluation uses the new,
      longer gap, and the session stays open;
    - **a superseded value may never cause a closure:** a short value replaced before any
      evaluation ran under it closes nothing;
    - **already-closed sessions stay closed.**
  - **A failed evaluation never leaves the session unscheduled.** A failure (the save's evaluation,
    the timer, or any other evaluation) leaves a future evaluation scheduled, independent of browser
    reads or operating activity: the timer is re-armed rather than consumed, and retries every 60 s
    (R4). The saved setting stands and is not rolled back.
  - A save reported `durability: "unconfirmed"` (PT-6) is in effect, so its evaluation runs.
  - **A settings save is never activity** (P1). It never moves `last_activity_at`.
  - **Closed sessions stay closed.** Lengthening the gap never reopens a session that has already
    closed; pick-up is the way back (§3).
- **R3. The contest suspension still prevents a gap close.** While the contest-active state is on
  and its end has not passed (§3, P2), a gap change is saved and takes effect, but no gap close
  happens. When the suspension ends, the evaluation P2 requires uses the value in force then.

### Examples (default 2 hours unless stated)

- **Shortening past an existing deadline.** Last activity 14:00. At 15:00 the operator saves
  30 minutes. `15:00 ≥ 14:30`, so the save's evaluation closes the session at once with `gap`,
  `closed_at` 14:00. Every window shows it Closed (§7), and the next live QSO opens a new session.
- **Shortening, not past the deadline.** Last activity 14:00. At 14:10 the operator saves
  30 minutes. The session stays open, and its timer moves from 16:00 to 14:30.
- **Lengthening before expiry.** Last activity 14:00. At 15:30 the operator saves 3 hours. The
  session stays open, and its timer moves from 16:00 to 17:00. The save itself is not activity:
  with nothing else, the session closes at 17:00, `closed_at` 14:00.
- **Lengthening after a close.** The session closed by gap at 16:00 (`closed_at` 14:00). At 16:30
  the operator saves 4 hours. It stays closed; pick-up resumes it for future logging.
- **Lengthening after the old deadline, before any evaluation closed it (accepted, S5).** Last
  activity 14:00, old deadline 16:00, no close persisted yet (the timer is late). At 16:10 the
  operator saves 3 hours. The save's evaluation uses 3 hours: `16:10 < 17:00`, so the session stays
  open until 17:00 unless the operator logs or acts again.
- **Two saves in quick succession (accepted, S5).** Last activity 14:00. At 14:45 the operator
  saves 30 minutes, and the save's evaluation closes the session at once (`14:45 ≥ 14:30`). Had a
  3-hour save been committed first, so that no evaluation ran under 30 minutes, the session would
  stay open: a superseded value may never cause a closure.
- **Changing it during the contest suspension.** Contest 12:00Z Saturday to 12:00Z Sunday; last
  contest QSO Saturday 17:20.
  - At Saturday 21:00 the operator saves 1 hour. Nothing closes, and the timer stays at the
    contest's end. At Sunday 12:00 the evaluation uses 1 hour and closes the session, `closed_at`
    Saturday 17:20.
  - Last contest QSO Saturday 17:50, gap saved as 1 hour at 17:55, Stop pressed at 18:00:
    `18:00 < 18:50`, so the session stays open until 18:50 unless the operator logs or acts again.
- **A rejected or failed save.** The configured gap remains unchanged. Any ordinary expiry already
  committed under the old policy stands.
  - **Refused at validation** (a value outside R4's limits): the PUT is refused before anything is
    written or evaluated, so the save itself causes no session change.
  - **Failed while persisting:** the save also makes no session change of its own, because its
    evaluation runs only after a successful write. Expiry under the old policy continues, by any
    evaluation, as if the save had not been attempted.

### R4: validation limits and retry (ruled 2026-11-07)

- **R4. Validation limits:**
  - **the minimum** gap;
  - **the maximum** gap, if any;
  - **the unit and granularity** Settings offers (for example whole minutes);
  - **whether "never close by gap" is allowed.** If it is, only New session, pick-up and a
    default-logbook change close a session, and the Session panel would need to say so;
  - **an out-of-range value found in `config.json` at start:** refused like any other validation
    error (config.md §12: Load makes error findings fatal, so the daemon would not start), or clamped
    to the nearest limit as the FT8 repeat cap is (config.md §4.3).
    **The reviewer supports rejection**, with an actionable error naming the field and the allowed
    range, because clamping would silently change the operator's declared policy. (API and startup
    consistency alone does not decide it: a consistent clamp would also be consistent.) The choice is
    the operator's, because it decides whether a hand edit can stop the daemon;
  - **the retry interval** after a failed evaluation (R2).

  **Operator ruling (2026-11-07), adopting the reviewer's recommendation** ("yes, adopt them as my
  ruling"):

  | Decision | Recommendation | Reason given |
  |---|---|---|
  | Minimum gap | 15 minutes | Allows short sessions without making brief pauses too disruptive. |
  | Maximum gap | 24 hours | Substantial flexibility; the contest suspension already handles longer contest runs. |
  | Settings unit and step | minutes, in 15-minute increments | Simple choices; the default is 120 minutes. |
  | "Never close by gap" | not offered initially | Keeps automatic expiry predictable; the contest suspension covers the established exception. |
  | Invalid value at startup | refuse | The error names the field, the supplied value and the allowed values; the policy is never silently changed. |
  | Failed-check retry | every 60 seconds | Avoids rapid retries while recovering automatically; ordinary evaluations may retry sooner. |

  For consistency, the same rule applies through Settings, the API and `config.json`: 15–1440
  minutes, in multiples of 15. A missing field still means 120 minutes (the ruled default).

## Operator ruling P4 (2026-11-07): FT8 eligibility, first start and the header clock (O4, O5, O6, O8)

**Status: ruled 2026-11-07 (operator: "Confirm").** Drafted by the agent to the reviewer's brief.
**Reviewer approval (2026-11-07):** P4 as written, covering O4, O5, O6 and O8, and the deferral of
O1–O3 with their stated revisit triggers, after three documentation corrections the reviewer
verified. **Operator ruling (2026-11-07):** P4 confirmed, including the O1–O3 deferrals. This ADR
remains Proposed. O7 was later resolved by S3 (below, under *Open questions*).

### O4. What counts as a qualifying QSO for the Ignore bypass (§7)

**Recommendation: same call, same band, same mode and submode, among the open session's members.**
A QSO qualifies when **all** of these hold:
- it is a **member of the currently open session** (§2), and not deleted;
- its `CALL` equals the held station's call **exactly**, as stored (upper case, whole call):
  `6W/IK2HKT` and `IK2HKT` are different calls;
- its `BAND` equals the band of the run's **pinned dial** (`dial_freq_mhz`, the band the contact
  would be logged on), not live rig state;
- its ADIF `MODE` equals the run profile's mode, and its `SUBMODE`, with absent read as empty,
  equals the profile's submode. Today that is FT8 = `MODE FT8`, no submode; FT4 = `MODE MFSK`,
  `SUBMODE FT4` (`internal/ft8/profile.go`). So an FT8 QSO never qualifies for an FT4 run, or the
  reverse.

**Unchanged: the first worked-before hold.** It still asks the whole logbook
(`internal/ft8/repeathold.go`, `IsContestDuplicateByLogbookIDWithContext` in
`internal/database/sqlite/api_context.go`), with its existing predicate: same call, band and
`MODE`; `SUBMODE` is compared **only when the run's submode is non-empty**. So an FT4 run
(`SUBMODE FT4`) matches only `SUBMODE FT4`, while an FT8 run (empty submode) matches any `MODE FT8`
row whatever its submode. It still holds, never auto-skips and never auto-answers. **The session
predicate above differs deliberately:** it requires `SUBMODE` equality **including empty**, so an
FT8 run counts only `MODE FT8` members with no submode. This record does not change the first
hold's predicate. Only what happens **after Ignore** reads
the session. The hold's wording must not suggest "this session" (inbox ruling 2026-11-06).

- **Examples:**
  - JE1TRF worked on 12 m FT8 in July, not today. Held (logbook-wide); Ignore; no qualifying QSO
    in today's session, so the bypass is granted and its next call is answered directly, once.
  - JE1TRF worked on 12 m FT8 an hour ago **in this session**. Held; Ignore; a qualifying QSO
    exists, so it is ignored for the rest of the run, as today.
  - JE1TRF worked on 12 m **FT4** in this session, now held on a 12 m **FT8** run: FT4 does not
    qualify, so the bypass is granted.
  - JE1TRF worked on **15 m** FT8 in this session, held on 12 m FT8: not qualifying; bypass
    granted.
- **Alternatives:**
  - **Any QSO with the call in the session.** Simpler to state, but a station worked on another
    band (a new band slot for the operator) could then never be answered after an Ignore. Not
    recommended.
  - **Mode only, ignoring submode.** FT8 and FT4 already differ by mode (`FT8` versus `MFSK`), so
    this would not merge them. But several submodes share `MODE MFSK`: FT4 is one
    (`internal/ft8/profile.go`), and so are Q65 and FST4 (`internal/enums/modes/adif-modes.json`). Ignoring
    submode would let an `MFSK`/`Q65` member count against an `MFSK`/`FT4` run, a contact in a
    different mode. Not recommended.

### O5. Ignore when no session is open

**Recommendation: allow the bypass.** With no session open, the session has no qualifying QSO,
so an Ignore grants the explicit, single-use direct-answer bypass (§7), exactly as with an open
session that has none.
- **Rechecked at use.** When the station next calls and would be answered, the daemon checks the
  session **open at that moment**. If a session has opened since (by another live QSO, New, or
  pick-up) and it now holds a qualifying QSO, the bypass is void and the station is treated as
  ignored for the run. Otherwise the station is answered directly, once.
- **Ignore is not activity and never opens a session** (P1). The direct answer it enables is the
  run's own automatic continuation, so it does not count either; the completed QSO counts, and
  opens a session if none is open (§2).
- **Examples:**
  - No session open (the last closed by gap). JE1TRF held, Ignore. Its next call is answered
    directly; when that QSO is logged it opens a new session.
  - No session open. JE1TRF held, Ignore. Before it calls again, another station's QSO opens a new
    session; the recheck finds no qualifying QSO with JE1TRF there, so it is answered directly.
  - No session open. JE1TRF held, Ignore. Before it calls again, the operator picks up the morning
    session, which already holds a 12 m FT8 QSO with JE1TRF: the bypass is void; JE1TRF stays
    ignored for the run.
- **Alternative: refuse the bypass with no session** (ignored for the run, as today). It is
  safer against repeats, but it brings back the 2026-11-06 surprise exactly when a session has
  just closed by gap, the moment it is least visible. Not recommended.

### O6. The first start after this ships

**Recommendation: start with an empty daemon session history.**
- Initially, no session exists. No opening question is shown (§6: no eligible closed session
  exists); the panel offers New session, and the first live QSO otherwise opens one
  automatically.
- **Not imported:** the tabs' `sessionStorage` lists (they are tab-supplied and per tab, so they
  cannot be trusted as membership), and **no membership is inferred** from existing QSOs (§2,
  "Never joins: existing contacts").
- **What the operator is told, once, on the Session panel:** for example "Sessions are now shared
  by every window. Earlier contacts are unchanged in the logbook; they are not part of a session."
  The panel, the Map (§7) and the header clock show "No session" until the first session opens.
- **Examples:** after the upgrade the panel is empty in every window although this morning's
  contacts are in the Logbook; the first FT8 QSO opens a session, and every window shows it.
- **Alternatives:**
  - **Import the tabs' lists.** Rejected: several tabs hold different lists, and membership would
    rest on browser data the daemon cannot verify.
  - **Infer a session from today's QSOs by time** (for example, the last gap's worth). Rejected:
    it is the derived membership §2 rejects; imports and edits would be swept in.

### O8. The header session clock

**Recommendation: the elapsed time of the current open interval.**
- **Start:** a New session or an automatic open starts it at zero (`opened_at`); a pick-up starts
  a new open interval (§3), so the clock restarts at zero.
- **Stable across reloads and ordinary restarts:** the baseline is the daemon's stored interval
  start, so reloading, opening another window, or a plain daemon restart does not reset it. Every
  window shows the same value.
- **With no session open:** the clock shows "No session" and stops ticking. It does not keep
  counting a closed session.
- **Distinct from contest operating time:** the contest timers (Last, Op, Elapsed;
  `contestTimers.ts`) are unchanged and follow the scorers' rules. The session clock is not
  labelled or used as operating time.
- **Examples:**
  - Session opened 06:00; at 07:30 every window shows 1:30:00.
  - Last activity 06:10, so the session closes by gap at 08:10 (`closed_at` 06:10); the clock
    shows "No session".
  - Picked up at 09:00: the clock shows 0:00:00 and counts from 09:00.
  - The daemon restarts at 07:45 while the session is open: the clock still shows the time since
    06:00.
- **Alternatives:**
  - **Time since the session's original opening.** After a pick-up it would include the closed
    hours (in the example, 3:00:00 at 09:00), which reads as continuous operating. Not
    recommended.
  - **Accumulated open time across intervals.** Accurate as "time open", but a pick-up resumes at a
    non-zero value that matches neither wall-clock nor any visible start, and it invites confusion
    with contest operating time. Not recommended for the header; it could appear in the session's
    details later.

## Open questions: dispositions

- **O1. A split session.**
  - **The case:** a gap closes S0 during a break, and the next QSO opens S1. Picking up S0 then
    leaves S1's QSOs in S1. Pick-up does not repair the split: it resumes **future** logging into
    S0 only.
  - **Recommendation (reviewer):** no automatic movement of members on pick-up.
  - **The open decision:** if folding a successor back proves to be needed, it is a **separate
    explicit action** with a preview of the membership it would move. It is not part of pick-up.
    Whether to build it is left open. It is the correction tooling ADR 0049 feared, deferred.
  - U2 and U3 decide how often a split happens.
  - **Deferred (operator ruling 2026-11-07, P4).** **Revisit when** a gap close splits a
    sitting on air and the split membership matters to an output the operator uses (a session
    email or export, the Map, or FT8 eligibility).
- **O2.** Explicit Add to session and Remove from session (for example from the Logbook view),
  for existing contacts and imports. Not proposed for the first slice.
  - **Deferred (operator ruling 2026-11-07, P4).** **Revisit when** the operator needs an imported or pre-upgrade contact,
    or a contact logged to the wrong session, in a session email, export or the Map.
- **O3.** Resuming an older session than the latest, and browsing past sessions on the panel.
  - **Deferred (operator ruling 2026-11-07, P4).** **Revisit when** the operator wants to resend or review an earlier
    session (its email, export or Map), or needs to resume a session other than the latest.
- **O4.** The FT8 eligibility scope. **Ruled 2026-11-07 (operator): P4 (O4) above.**
- **O5.** The FT8 Ignore when no session is open (§7). **Ruled 2026-11-07 (operator): P4 (O5)
  above.**
- **O6. The first start after this ships.** **Ruled 2026-11-07 (operator): P4 (O6) above.**
- **O7. Restoration with an equal epoch.** **Resolved by S3 (operator ruling 2026-11-07):** the
  archive serve epoch it concerned no longer exists; an archive's session is decided by the gap on
  return (§4). `default_epoch` lives in the archive file with the default pointer, so restoring that
  file restores both together. The backup and restore design (inbox item 4) still has to state how a
  restore treats open sessions in general.
- **O8. The header session clock** (`frontend/app/src/lib/ui/SessionTimer.svelte`, rendered in
  `lib/ui/Header.svelte`). **Ruled 2026-11-07 (operator): P4 (O8) above.**
  - **Today:** a third per-tab "session". Its start is the tab's first page load, kept in
    `sessionStorage` under `sm.session.startedAt`. It is separate from the Session panel's list,
    so a new window restarts it.
  - **Under this design:** its replacement uses the shared daemon session, so every window shows
    the same clock. What it shows is ruled in P4 (O8): the elapsed time of the current open
    interval, and "No session" when none is open; the alternatives are recorded there.
  - **Not affected:** operating-session duration is **distinct from contest operating time**. The
    contest timers in the same header (Last, Op, Elapsed) are counted per contest occurrence by the
    scorers' rules (`contestTimers.ts`) and stay as they are. The session clock must not be
    presented as, or substitute for, contest operating time.

## Simplification review (2026-11-07): ruled "A with S2, S3 and S5"

**Why:** every mechanism adds persistent state, transaction ordering, recovery paths and
interactions that future changes must preserve; maintenance cost grows faster than line count
(operator, 2026-11-07: "complexity == higher maintenance"). Acceptance requires the guarantees to be
**consistent** and **worth their maintenance**. This review was done before acceptance at the
reviewer's direction; the ADR remains Proposed. Nothing here changes a ruling until the operator
rules on it.

**How each mechanism is judged:** the operator-visible failure it prevents; whether that failure
has been **observed** (with the evidence) or is **hypothetical** (no likelihood is estimated);
its ongoing cost (schema, migrations, recovery, tests, coupling); and the simpler alternative with
the behaviour that would be explicitly accepted.

**Kept as the feature's core, not reviewed for removal:** the archive-local session records **with
history** (several sessions per archive, each with its open intervals, which pick-up needs); the
membership association written in the QSO's transaction; the gap close and its timer; New,
Continue and pick-up; the shared events; the opening question with local Not now.

| # | Mechanism | Failure it prevents | Observed? | Ongoing cost | Simpler alternative, and the behaviour accepted | Recommendation |
|---|---|---|---|---|---|---|
| S1 | Generation guard on every control (§5) | a press from a window showing an older state changes the wrong session | **Observed context:** several windows are ordinary use (four-window check, inbox 2026-11-06) | one counter, one comparison per control | none adequate | **Keep** (reviewer and agent) |
| S2 | Durable applied-request records and replay of the original result (§5) | a control retried after a lost response returns an error instead of its original success | **Hypothetical:** no lost-response retry has been observed | a table in the archive file, its migration, retention bound, replay and misuse rules, tests | **Drop.** A retry carries its original generation; after a successful first attempt it is refused as stale and the window redraws to the state the first attempt produced. It can never apply twice. **Client rule:** a retry keeps its original generation and never silently re-reads and resends. **Accepted:** the operator may see one stale refusal after a lost response, with the correct state shown | **Drop** (reviewer recommendation; operator to rule) |
| S3 | Archive serve epoch (config counter, per-archive value) and its outcome table (§4) | after A→B→A, QSOs logged on return to A join A's session from before the switch | **Archive switching observed** (alpha.5 acceptance B2-10/11); the merge itself is **hypothetical** (not built) | a config field, an archive field, start-node evaluation, the activation/default-change outcome table, the O7 restore limitation, tests across crash points | **Rely on the gap.** Sessions are already per archive and B's QSOs are in B's file, so nothing crosses archives. **Accepted:** returning to A **within the gap** continues A's session; after the gap it is closed as usual. O7 disappears with it. Changes behaviour from the original design brief, so it needs the operator | **Propose dropping** (agent; the reviewer asked for cuts only with concrete replacement behaviour, which this states) |
| S4 | Default epoch in the archive file (§4) | X→Y→X default changes within the gap leave the old session open | **Default changes observed** (W-0024 acceptance); X→Y→X within the gap **hypothetical** | an archive field written in the pointer transaction, Y→Y preservation, crash pairing, tests | **Compare the open session's logbook with the effective default** at each evaluation; a mismatch closes it (`logbook_changed`). X→Y is caught by **any** evaluation while Y is the default (a QSO, a control, a read, the timer). **Accepted:** only an X→Y→X sequence with **no evaluation while Y was the default** escapes detection and continues X's session; otherwise X's session is closed and returning to X does not continue it | **Propose replacing** (agent) |
| S5 | P3 R2's effective-policy boundary, pending boundaries and their replay | a gap change crossing a deadline, a superseded short value, or a failed boundary transaction lets an overdue session survive | **Hypothetical:** the feature is not built | an ordered in-memory list, replay before every evaluation, rollback semantics, the restart exception, tests | **A saved gap applies from the next evaluation**, which runs immediately after the save under the new value only, and reschedules the timer. **Accepted:** a session already overdue under the old value, if the timer has not yet closed it, survives a lengthening; a short value superseded before any evaluation may never close a session. The 60 s retry after a failed evaluation stays | **Propose simplifying.** Reopens the operator's P3 R2 ruling (reviewer agrees it should be reconsidered) |
| S6 | P1 credit bound to the session at acceptance (generation captured, credit dropped if it moved) | a delayed activity write extends a replacement session | **Hypothetical** | a capture step and a comparison | **Not yet established.** Running capture and write inside one HTTP request does not stop another request or the timer from changing sessions between the FT8 controller's acceptance and the session transaction. A simplification needs a stated serialization boundary between acceptance and the credit, and must keep the rule that a failed activity write never refuses an accepted FT8 action | **Deferred** (reviewer, 2026-11-07); P1's capture-and-compare stays as ruled; not needed to adopt S2–S5 |
| S7 | Contest-end check in every evaluation (§3, P2) | a late contest-end timer lets a session be judged under the suspension after the contest ended | **Power cut during a contest observed** (2026-10-31); a late timer is **hypothetical** | one comparison per evaluation | none simpler that keeps P2's guarantee | **Keep provisionally** (reviewer) |
| S8 | FT8 bypass recheck at use (§7, O5) | a station answered after a session change made it ineligible | **Hypothetical** in this form; the underlying hold problem was **observed** (12 m pile-up, 2026-11-06) | one query at use | none safer | **Keep provisionally** (reviewer) |

**Net effect if S2–S5 are adopted** (S6 deferred): no applied-request table; no config or archive epoch fields;
no activation outcome table; no pending-boundary list; no O7 limitation. Departure detection
becomes one comparison (the open session's logbook against the effective default). What remains is
the session history, the membership link, the generation guard, the gap timer with a simple
evaluation rule, the contest check and the FT8 recheck.

**A whole-design alternative (derived membership)** is assessed in the *Model comparison* below.

**Operator decisions needed:** S2 (drop request records), S3 (rely on the gap for archive
returns), S4 (logbook comparison instead of the default epoch), S5 (simple gap-change rule;
reopens P3 R2). S6 is deferred (P1 stays as ruled) and does not gate S2–S5. S1, S7 and S8 stay
unless the operator directs otherwise. After the rulings, the affected sections are rewritten to the
chosen design, so the accepted ADR describes one design, not its history.

**Operator ruling (2026-11-07): "A with S2, S3 and S5"**, adopting the reviewer's recommendation
(operator: "ADR 0100 - yes"). S2, S3 and S5 are adopted; S4 is rejected and `default_epoch` is
kept; S1, S7, S8 and P1 are kept; S6 is deferred. §1, §2, §3, §4, §5, §9, U3, P1, P3 R2, O7 and
*Consequences* have been rewritten to this design. This ruling does not accept ADR 0100 or
authorise implementation.

| Proposal | Recommendation | Reason given |
|---|---|---|
| S2 | Accept | The generation guard prevents duplicate application; returning the current state after a lost response is sufficient, and durable result replay adds little value. |
| S3 | Accept | Continuing an archive's sitting within the gap is understandable; membership stays within its archive, and substantial cross-file recovery machinery goes. O7 goes with it (it concerned the config serve epoch). |
| S4 | **Reject as written; retain `default_epoch`** | Whether a read or timer ran between two switches is invisible to the operator; making continuity depend on that incidental check gives the same actions different outcomes. The epoch keeps closure on a default-logbook change deterministic. |
| S5 | Accept | The latest saved gap applies at the next evaluation. Accepted explicitly: lengthening can preserve an overdue but still-open session, and a superseded value may never cause a closure. Already-closed sessions stay closed. |

S1, S7, S8 and P1 are kept; S6 stays deferred. The result is called **A with S2, S3 and S5**, not
A′ as defined below (A′ includes S4).

## Model comparison (2026-11-07): stored membership versus derived membership

**Status: analysis for a choice, no rulings.** Written at the reviewer's direction before any
model is chosen. ADR 0100 remains Proposed. Query cost is **unmeasured** throughout.

Three models are compared:
- **A: ADR 0100 as ruled** (stored sessions and membership, the full mechanisms).
- **A′: A with the simplification proposals S2–S5** (above; S6 is deferred, so P1 stays as ruled).
- **D: derived membership.** Sessions are computed at read time from live-logged QSOs, and only
  the operator's decisions are stored.

### D defined precisely enough to compare

- **Stored state:**
  - a **live-origin marker** on each QSO, set by the live logging paths only. No such marker exists
    today: `qso_upload.origin` (`0007_qso_upload_origin.up.sql`) is on upload rows, which exist only
    for logbooks with an enabled destination, so it cannot serve. The marker is a new `qso`
    column, so **D also changes the logging path and the schema**;
  - a **boundary-event table** per archive: `(id, logbook, kind New|Continue, at)`;
  - for P1 activity other than QSOs, an **activity-event table** (or P1 changes; see below);
  - for the contest suspension, **contest-mode intervals** (or the suspension changes; see below).
- **Time basis: logging time, not QSO time.** A QSO is placed by when it was **stored**
  (`qso.created_at`, supplied on insert; edits change `modified_at`, not `created_at`; whether any
  path rewrites `created_at` is **unverified**). Using `time_on` instead would let a time edit move
  a QSO between sessions and would put a late-logged contact in an earlier session; logging time
  avoids both and matches A's rule (§2: evaluated at `now`, never at `time_on`).
- **The current session** of the effective default logbook: take that logbook's live-marked,
  non-deleted QSOs and its boundary events in logging-time order. A session **breaks** at a New
  event, and between two consecutive activity points **at least** the gap apart (the ruled
  deadline, `now ≥ last_activity_at + gap`), unless suspended.
  **Continue** at time c removes the break immediately before c: everything logged after c joins
  the session that was latest before c.
- **Which contacts Continue includes (example, gap 2 h):** QSOs at 09:00, 09:30 (session S0); quiet
  until 13:00; Continue at 13:05; QSOs at 13:10, 13:20. The current session is
  {09:00, 09:30, 13:10, 13:20}. Variant: a single QSO at 12:00 (2 h 30 m after 09:30, so it starts
  S1 by itself); quiet again; Continue at 14:30; QSOs at 14:40, 14:50. Continue bridges to S1, the
  latest, not to S0: {12:00, 14:40, 14:50}. S0 stays separate.
  This matches A, where pick-up resumes the latest eligible session.

### Each claimed saving, qualified

| Claim | Qualification |
|---|---|
| Nothing touches logging | **False.** D needs the new live-origin column, written by the live paths, and its migration. It removes the membership **row** write, not the logging-path change. |
| Two New presses are harmless | **Not without a guard.** Unguarded: window A presses New at 10:00; a QSO is logged at 10:01; window B's New lands at 10:02, and the 10:01 QSO becomes a session of its own. A delayed retry of a New that had succeeded at 10:00, arriving at 10:20 after five QSOs, would split them off. D therefore needs an **expected latest boundary** on each press, refused if a boundary was added since the window's view: a smaller form of S1. **By S2's reasoning, that guard alone suffices:** B's press and the unchanged retry both carry the boundary they saw and are refused as stale after the first success; a retry whose first attempt never committed applies when it lands, leaving the QSOs before it in the old session. Request ids with recorded results are needed only if replaying the original success remains a requirement. |
| Archive and default counters disappear | **Only by changing the closure rule.** Filtering by logbook shows the right sitting, but returning to the previous archive or default within the gap **resumes** its old sitting, because nothing breaks it. That is the behaviour A′ proposes in S3 and S4; D gets it automatically and cannot easily avoid it. Keeping A's rule in D needs a recorded boundary on each change, which brings the evidence problem of §4 back. |
| Contest windows replace contest-end checks | **Only by changing the suspension.** The declared window differs from when contest mode was actually on (an early Start, a Stop, a restart). Preserving U3 needs the contest-mode interval history (ADR 0101's state, logged as intervals). The simpler substitute, "gaps between two QSOs of the same occurrence do not break", is a different rule and needs the operator. |
| Timers disappear | **The persisted close disappears, a timer does not.** An idle, already-open panel, Map and clock must still change to Closed at the deadline. Each client can compute the deadline (last activity + gap) and re-read then, or the daemon emits an event at the deadline. Either way there is a timer; it just persists nothing. |
| Gap changes become immediate | **Yes, and history changes with them.** Lengthening the gap re-segments the past: two sittings separated by 2 h 30 m merge under a 3 h gap, and a session shown Closed can read as open again. Shortening splits past sessions. Avoiding that needs the gap recorded per period, which is stored state again. **Accepted only if** the operator agrees that past session boundaries follow the current gap. |
| No stable session identity | **New cost, not a saving.** Email and export name a session (§7); a derived session has no stored id, and its membership can change with the gap. An id such as its first boundary or first member can serve for display but is not stable under re-segmentation. |

### Operator-visible comparison

| Situation | A (as ruled) | A′ (S2–S5) | D (derived) |
|---|---|---|---|
| Lost response, retry of New | original success returned | one stale refusal, correct state shown | one stale refusal, correct state shown (boundary guard) |
| A→B→A within the gap | A's session closed on return | A's session continues | A's session continues |
| Default X→Y→X within the gap | X's session closed | X's session closed if any evaluation ran while Y was the default; continues only if none did | X's session continues |
| Gap lengthened from 2 h to 3 h | open session extended; closed ones stay closed | as A, without the old-value check | past sittings may merge; a closed session may reopen |
| Time edit on a member | stays a member | stays a member | stays (logging time is unaffected) |
| Unanswered CQ run, 2 h | session stays open (P1) | as A | closes, unless activity events are stored |
| Contest Stop early, then QSOs | P2 expiry at Stop | as A | depends on interval history or a changed rule |
| Idle panel at the deadline | shows Closed (timer event) | as A | shows Closed only with a client or daemon timer |

### Code, persistent state and maintenance

| | A | A′ | D |
|---|---|---|---|
| New schema | session table, interval list, membership table, applied-request table, archive epoch fields, config epoch | session table, interval list, membership table | live-origin column, boundary-event table, activity-event table (if P1 kept), contest-interval table (if U3 kept) |
| Logging transaction | membership write + session evaluation | the same | live-origin column write |
| Recovery paths | epochs, outcome table, pending boundaries, replay | gap timer only | none persisted; deadline timer for display |
| Hard cases to test | many (crash points, epochs, boundaries, replay) | fewer (generation, timer, contest check) | segmentation edge cases, re-segmentation after gap change, Continue bridging, query performance |
| Query cost | membership lookup | the same | segmentation per read: **unmeasured** |

### O4–O8 under D (none carries over automatically)

- **O4 (qualifying QSO):** **preserved** in rule; it runs against the derived current session.
  Its cost per FT8 answer is **unmeasured**.
- **O5 (Ignore with no session):** **changed in meaning.** "No session open" becomes "the last
  activity is older than the gap", a computed state; the rule itself can be kept.
- **O6 (first start):** **preserved.** Existing QSOs have no live marker and never join.
- **O7 (restore limitation):** **disappears**, because there are no epochs; this is the closure-rule
  change above.
- **O8 (header clock):** **preserved** in rule (the current open interval: since the latest New,
  Continue, or first QSO after a break), but "open" is computed, so the clock needs the display
  deadline timer.
- **Also affected:** P1 (deliberate FT8 actions need an activity-event table, or only QSOs count:
  **changed**); P2 and U3 (need contest intervals, or a changed rule: **unresolved**); P3 (largely
  moot, at the cost of re-segmenting history: **changed**); §5 (a smaller guard is still needed:
  **changed**); §7 email and export (**changed**: no stable session id).

### What the comparison shows (agent reading, for the operator's choice)

- D removes the stored membership and the persisted close, but **not** a logging-path change, a
  press guard, or a display timer. To keep P1, U3 and stable history it needs three more tables,
  and its new hard cases (re-segmentation, bridging, query cost) are of a different kind, not fewer.
- A′ removes the epochs, the replay table, the pending boundaries and O7, while keeping stable
  session identity, fixed history and the existing rulings for P1, P2 and O4–O8. **A′ means
  S2–S5 with P1 retained as ruled.** S6 is deferred and is not needed to choose A′.
- **The behaviour differences:**
  - **Archive return within the gap:** A closes the old session; A′ and D continue it.
  - **Default X→Y→X within the gap:** A and A′ close X's session (A′ except when no evaluation ran
    while Y was the default); D continues it.
  - **A superseded gap value:** A′ and D may fail to close a session under it; A never does.
  - **Gap lengthened:** D re-segments past sessions; A and A′ do not.
- If the A′ behaviours are acceptable, A′ is the smaller step.

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
  - New, Continue and pick-up, each with an expected generation (no request ids or recorded
    results; S2);
  - `default_epoch` in the archive file, incremented in the default pointer's write (no archive
    serve epoch; S3);
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
