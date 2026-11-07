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
  - an archive or default-logbook change closes the session when the change takes effect.
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
  - **`seen_serve_epoch` and `default_epoch`** (§4).
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
  2. **Evaluate departure, then expiry:**
     - if a change has taken effect (§4), close the session with `archive_changed` or
       `logbook_changed`;
     - otherwise, if it is open, it is **expired** (`now ≥ last_activity_at + gap`, the one
       deadline rule used everywhere) and the gap suspension is off (U3; the contest-end rule in
       §3), close it with `gap` (§3).

     Either close advances the generation.
  3. **If none is open now,** create a session: `opened_at = last_activity_at = now`, bound to the
     effective default logbook and the archive's current `default_epoch`. Advance the generation.
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
  - when an archive or default-logbook change takes effect, at `last_activity_at` (§4).
- **Activity baseline for operator actions:**
  - **New session:** `opened_at = last_activity_at = now`.
  - **Pick-up:** reopening a session starts a new open interval, sets `last_activity_at = now`
    whatever U1 decides, and sets the session's `default_epoch` to the archive's current one. The
    resume is the baseline, so a resumed session's old timestamp cannot expire it again at once.
- **Pick-up (the operator's "restart"):**
  - It resumes a closed session, which becomes the open one; **future** live QSOs are added to it.
    Nothing already logged moves (O1).
  - Closing the current session, if any (`picked_up_other`), and reopening the chosen one are one
    transaction.
  - Only an **eligible** session can be picked up: one whose `logbook` is the current effective
    default. Others are refused, because they would mix logbooks. Every offer (§6, §8) lists eligible
    sessions only, so the panel never offers an action the daemon would refuse.
- **Where departure (§4) and expiry are evaluated, in that order:**
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

### 4. Archive and default-logbook changes: closure when the change takes effect

*Revised 2026-11-06 after review. The first committed form (`abe5b7b1`) used a departure mark
written when the change was requested. That mark is withdrawn. Three cases showed that evidence
written at request time cannot be trusted later:*
- *a selector persist failure that left a committed mark behind;*
- *a retained `not_applied_intent_pending` intent whose mark an unsuccessful start would clear;*
- *a promotion that became durable before its mark was resolved.*

**The rule:** a session closes when an archive or default-logbook change **takes effect**. The
evidence is written by the write that makes the change take effect, so evidence and effect cannot
diverge. Nothing is written when a change is merely requested.

- **Archive changes: a serve epoch.**
  - **In config:** `archive_serve_epoch`, a station-global counter. It is incremented **in the same
    config write that promotes a candidate to active** (`startArchivePromote`,
    `cmd/smd/archive_adopt.go`: active = candidate, pending cleared). That write runs before
    `http`, so no live QSO reaches a candidate before it.
  - **In each archive file:** `seen_serve_epoch`, the value of `archive_serve_epoch` when that
    archive last evaluated its sessions.
  - **The test:** an archive's `seen_serve_epoch` differing from the current `archive_serve_epoch`
    means a promotion has taken effect since this archive last evaluated. Only a promotion moves the
    counter, and an archive can be promoted only while it is not active
    (`archive_active` refusal), so a difference means **this archive was left** after its last
    evaluation. The open session closes with `archive_changed`, and `seen_serve_epoch` is updated,
    in one transaction.
  - **Inequality, not ordering:** the test is a difference, not an ordering. An epoch restored to a
    **different** older value (an old `config.json`) also closes the session.
  - **Unresolved restoration limitation (O7):** a restore can bring back an **equal** epoch.
    - **Example:** A records 7; A→B promotes to 8; an older `config.json` is restored, naming A
      active at 7. A's stored value is also 7, so A's session silently continues although B was
      served in between.
    - **Consequence:** later QSOs merge into a session that should have closed. New and pick-up only
      correct future logging; they cannot repair membership already merged.
    - **Status:** the mechanism (for example, invalidating every archive's `seen_serve_epoch`, or
      closing open sessions, as a step of a supported restore) is **undecided**. It belongs with the
      backup and restore design (inbox item 4).
- **Default-logbook changes: a default epoch.**
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
    `logbook_changed`. That covers X→Y→X too, which a plain comparison of logbooks would miss: the
    epoch moved twice even though the logbook is X again.
- **The closure time** is the session's `last_activity_at`.
  - Every member was stored at or before it (§2, step 5), and nothing joins after a change takes
    effect.
  - The true departure instant is not recorded durably, and no evidence written before the change
    took effect is trusted for it. The operator sees "closed at hh:mm (archive changed)", the end of
    the sitting, as for a gap close.
- **Where the departure test runs:** in the **same evaluation step as expiry** (§3):
  - every logging transaction, before membership;
  - every control transaction;
  - every read;
  - a start node before `http`.

  The test is derived from durable state on both sides, so the start node is not load-bearing. If
  its write fails, the next session transaction repeats it. **Departure is tested before expiry:**
  both close the session at `last_activity_at`, and departure names the stronger reason.
- **What each activation and default-change outcome does** (from `internal/archive/activate.go`
  and `cmd/smd/default_change.go`):

  | Outcome | Epoch moves? | Sessions and controls |
  |---|---|---|
  | `activation_persist_failed` (definitive; seals released, nothing on disk) | no | unchanged; nothing to clean up |
  | `restart_failed` (pending cleared, seals released) | no | unchanged |
  | Archive activation, error code `pending_unclear` (restart failed, pending could not be cleared; the next restart activates) | not yet | unchanged until a later start promotes; that promotion moves the epoch |
  | Candidate failed at start, fallback to last-known-good (no promotion write) | no | the departing archive's session continues; time down counts towards its gap |
  | Candidate promoted | yes, in the promotion write | the departed archive's session closes when that archive next evaluates |
  | Default change `effective` / `applied_cleanup_pending` | yes, if the pointer changed X→Y (in that transaction); a Y→Y durability rewrite does not move it | the session closes at the first evaluation after an X→Y change |
  | `not_applied_intent_pending` (the selector survives; a later start retries) | no | unchanged **between attempts**; a later `effective` start moves the epoch then |
  | Default change, status `pending_unclear` (error code `default_change_pending_unclear`: restart failed and the change could not be withdrawn; it applies at the next restart unless cancelled; `internal/archive/default_change.go:255,463`) | not yet | unchanged until a start applies it; an X→Y pointer change moves the epoch then |
  | `not_applied_abandoned`, cancelled, `withdrawal_unconfirmed` | no | unchanged |

  - **Status versus error code:** `pending_unclear` is a default-change **status**, and
    `default_change_pending_unclear` is its **error code**. Archive activation uses
    `pending_unclear` as an **error code**. Whether the default-change status is reachable in
    production is the separately recorded wiring limitation, not this ADR's concern.

  - **Session controls are never blocked by a pending change.** There is nothing to block on.
    Between attempts of a retained intent, New, Continue and pick-up work normally. Whatever session
    is open when the change finally takes effect is the one it closes.
  - Live admission during a change stays governed by its own seals (ADR 0097, archive activation),
    not by this ADR.
- **Worked sequences:**
  - **A→B:** B's promotion moves the epoch from E to E+1. When A is next served, its
    `seen_serve_epoch` (E) differs, so A's open session closes at its `last_activity_at`.
  - **A→B, restart B, then B→A, with a crash** straight after A's promotion became durable and before
    A evaluated anything:
    - the epoch is E+2, written in the promotion's own write;
    - A's `seen_serve_epoch` is still E;
    - the next start looks plain, but the epochs differ, so A's session closes.

    No window exists, because the evidence is the promotion write itself.
  - **A→B where B fails at start:** there is no promotion and the epoch stays E, so A's session
    continues.
  - **A selector persist failure:** nothing was written, so nothing changes.
  - **A default change retained across two starts, then effective:** sessions and controls are
    normal throughout. The `effective` start moves `default_epoch`, and the session open at that
    point closes.
- **Residual: a lost promotion write.** The promotion is a config write. If a crash loses it (its
  durability was reported uncertain), the station's durable record says the promotion never
  happened, and sessions follow that record:
  - **if the retried start promotes again,** the epoch moves then;
  - **if it falls back instead,** the departing archive's session continues, although the
    candidate briefly served live QSOs, which stay in the candidate's own sessions.

  Pick-up and New recover either case. This ADR does not add machinery against it.
- **A requirement on any future feature** that reads a non-served archive's sessions (for example
  an archive summary or export): it must apply the same epoch test, so that such a session reads as
  closed.
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
  2. **Departure and expiry evaluation** (§3, §4).
  3. **Stale check.** If the expected generation is not the current one, refuse with a distinct
     stale-action code, change nothing, and return the current state with its generation; the window
     redraws to it.
  4. **Apply** the transition, advance the generation, and write the applied-request record (the
     request id, the action, the target, the result and the time), all in one transaction.
- **Which outcomes are replayed, and which are evaluated afresh:**
  - **Replayed:** only **applied** outcomes. They are the only ones recorded, and a retry of an
    applied action returns its recorded result.
  - **Evaluated afresh:** every refusal, because a refusal changes nothing and is not recorded.
    - **A stale-generation refusal** stays stale on an unchanged retry, because the generation never
      returns to an earlier value. The window must redraw and the operator must act again.
    - **Other refusals can become applicable** on an unchanged retry once their condition clears.
      For example, a transient database failure leaves the generation where it was, so the retry
      can apply. A refusal whose condition clears only through a session transition (for example
      a pick-up made eligible by a later default change) meets the stale check instead, because
      that transition moved the generation.
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
- Gap closing is suspended while the daemon-owned contest-active state is on (U3).

## Operator rulings on U1–U3 (2026-11-06) and what stays open

**Attribution:** U1 and U3 were recommended by the reviewer and then **confirmed by the station
operator** in their own words: "yes, confirm U1 (b) and U3 (b)" (2026-11-06). The U2 default of
2 hours was approved by the station operator on the same day.

- **U1. Activity: ruled (b).** Activity is a logged QSO plus a **finite, documented set of
  deliberate operating actions**.
  - **Not activity:** passive decodes, an open window, and merely remaining armed. They never reset
    the clock.
  - **Still to define:** the exact list of counted actions, written into the design before
    implementation. **Ruled 2026-11-07 (operator): P1 below.**
  - **Unchanged:** §2, step 5 (a logged QSO is activity); New session and pick-up reset the
    baseline (§3).
- **U2. The gap: configurable, default 2 hours (both operator rulings).**
  - **Where (proposal):** a station-wide `config.json` setting, edited from Settings and documented
    in `docs/v2-design/config.md` when it is built. One value applies to every window and archive.
    The operator raised Settings and `config.json` as a question; it is not yet a settled ruling.
  - **When a change takes effect (agent proposal):** immediately, without a restart.
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
  - **While suspended:** the session ends only by New session, pick-up of another session, or an
    archive or logbook change (§4).
  - **Open: the baseline when the contest state ends.** One choice is that an expiry already due is
    applied at once; the other is that the contest's end counts as activity. Decide it with the
    contest-active state's design. **Ruled 2026-11-07 (operator): an expiry already due is applied
    at once, without inventing activity (P2 below).**

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
     pick-up (even of the same session), or a departure. So an unchanged generation means the same
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
- **O7. Restoration with an equal epoch (§4):** how a supported restore invalidates session state.
  This is decided with the backup and restore design (inbox item 4). Until then it is a recorded
  limitation.
- **O8. The header session clock** (`frontend/app/src/lib/ui/SessionTimer.svelte`, rendered in
  `lib/ui/Header.svelte`).
  - **Today:** a third per-tab "session". Its start is the tab's first page load, kept in
    `sessionStorage` under `sm.session.startedAt`. It is separate from the Session panel's list,
    so a new window restarts it.
  - **Under this design:** its replacement **must use the shared daemon session**, so every window
    shows the same clock. The open question is what it shows:
    - time since the session's `opened_at`;
    - time since the current open interval began (after a pick-up);
    - or total open time across intervals.

    It also needs a state for no session open.
  - **Not affected:** operating-session duration is **distinct from contest operating time**. The
    contest timers in the same header (Last, Op, Elapsed) are counted per contest occurrence by the
    scorers' rules (`contestTimers.ts`) and stay as they are. The session clock must not be
    presented as, or substitute for, contest operating time.

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
  - `archive_serve_epoch` in config, incremented in the promotion write, and `default_epoch` in the
    archive file, incremented in the default pointer's write;
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
