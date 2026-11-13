---
number: 0100
title: Daemon-owned operating sessions with explicit membership
status: Proposed
date: 2026-11-06
---

# 0100 — Daemon-owned operating sessions with explicit membership

**Proposed (2026-11-06; revised 2026-11-10). Design only. Nothing is authorised or built.** The
2026-11-10 revision follows the operator's rulings on sessions in the Electron desktop app
(ADR 0102, W-0027): a session follows the operating app's lifetime, with no inactivity expiry. It
replaces the earlier activity-gap design. That design, its rulings (U1–U3, P1–P3), its
simplification review and its model comparison remain in the Git history up to `4a448c19`; *What
changed on 2026-11-10* summarises what was superseded and what was kept. The session-email mark fix
(inbox item 1) was independent of this ADR and has shipped (`c3826281`, W-0010 outcome 11).
*Update 2026-11-13* records two later operator rulings that overtake parts of this text: no
view-only browser client, and the retirement of Station Events. It changes no session rule.

## Context

The Session panel is today a per-tab list in `sessionStorage`, one list per archive
(`frontend/app/src/lib/operate/session.svelte.ts`, ADR 0094). It fills from the tab's own
Phone/CW submit responses and from the `ft8-logged` events on the tab's FT8 stream. It survives
reloads, daemon restarts and FT8 runs, and ends when the tab closes. The operator likes how it
looks and feels. Its flaws come from where it lives:
- each window has its own list, so a new window starts empty;
- the Map instead plots the last N hours of the logbook
  (`frontend/app/src/lib/map/mapData.svelte.ts`);
- nothing outside the tab can see it: not the daemon, not FT8 eligibility, not other windows.

`smd` has no session concept: no session table and no session id on QSOs. Session email and
export take UUID lists supplied by the tab (`internal/api/handler_session_email.go:44`).
`session.terminated` is an unrelated Station Events kind for the end of an FT8 contact
(`internal/stationevents/stationevents.go:42`). The header session clock is a third, separate
per-tab value (`frontend/app/src/lib/ui/SessionTimer.svelte`, `sm.session.startedAt`).

Two 2026-11-06 inbox items share this root cause (`docs/dogfood-inbox.md`, triage items 2 and 5).
The operator's direction is: "The daemon session is the cleanest and gives us one authority on the
matter." The aim is something that "looks and feels the same" as today's Session panel, but owned
by the daemon, with session controls on the Session panel.

**Why the lifecycle follows the app.** A browser cannot tell the daemon that the operator has
finished: a page cannot ask on close, and closing one tab of several says nothing about the
sitting. The earlier design therefore closed a session after a configurable period without
activity, which needed a timer, a contest suspension and a list of counted actions. The desktop
app (ADR 0102) is one operating app with a confirmed exit, so the daemon can know when a sitting
starts and ends. The operator: "should a session start when the shell is opened and close when
the shell is closed, with the ability to continue a session", and, asked whether to prompt at every
start, "asking the User on every app startup could be friction".

**Why this reopens ADR 0049 (Rejected 2026-07-16).** ADR 0049 proposed a daemon session table with
`session_id` stamped on every QSO in `prepareQso`. It was rejected because the only tenant was a
display feature, a derived boundary can be recomputed while a stamped one is frozen, and the
stamping sat on the most load-bearing write path. Its revisit trigger was "a session that must
carry data *not derivable from its QSOs*". That trigger has fired:
- **The boundaries are operator decisions**, not functions of QSO times: starting and leaving the
  app, New, Continue, and changes of archive or default logbook.
- **One authority for several consumers:** the Session panel in every window, the Map, email and
  export, and the daemon's own FT8 eligibility check.

How this ADR treats ADR 0049's objections: there are now several consumers; membership is a
separate association, not a stamped `qso` column, so it can be corrected without updating QSO
rows (correction tooling itself is deferred, O1, O2); and the association adds one write to the
existing logging transaction, which no session policy, prompt or external dependency can block.

## Decision

The daemon owns the **operating session**:
- **Where it lives:** an archive-local record with a stable UUIDv7 identity, bound to the logbook
  that was the effective default when it opened. At most one session is open per archive. Only one
  archive is served at a time, so the station has one current session.
- **Lifecycle (operator rulings 2026-11-10):** a session is open whenever the operating desktop app
  runs. Starting the app after a clean exit opens a new session automatically; after a crash or an
  unconfirmed exit the app asks Continue or New before showing the operating workspace. A
  confirmed exit closes the session. There is no inactivity expiry.
- **Membership:** an explicit association, written in the same transaction as the QSO by the live
  logging paths only.
- **Controls:** New session, and Continue from a list of previous sessions for the current logbook.
  A default-logbook change closes the session when it takes effect; an archive switch closes it
  when the switch is requested (§4, O9 ruled 2026-11-12). Either way a new one opens.
  Sessions that end with no members are discarded; deleting sessions with members is not in the
  first slice.
- **Consumers:** the Session panel in every window, the Map, session email and export, and FT8
  repeat-hold eligibility all read this one membership.
- **Stale actions:** each operator action names the archive's session-state generation it was
  taken against, and the daemon refuses it if the generation has moved.

### Operator rulings (2026-11-10)

After the design exchange (claude-078 to claude-085, codex-041 to codex-043), the operator ruled
"1-7 yes":

1. A session is always open while the operating app runs; a confirmed exit closes it.
2. No inactivity expiry.
3. After a clean exit, a new session starts automatically; after a crash or unconfirmed close, the
   app asks Continue / New before the operating workspace.
4. Sessions with no members at all are discarded when they close; all others are kept.
5. The Session panel offers New session and Continue from a list of previous sessions for the
   current logbook, most recent first.
6. A successful archive switch or default-logbook change closes the session and starts a new one.
   (Refined for archive switches on 2026-11-12, §4: the close happens when the switch is
   requested, whether or not the switch then succeeds.)
7. Deleting sessions that have contacts is not in the first slice.

### 1. Identity and record

- **Session record:** `id` (UUIDv7), `logbook` (the logbook's UUID), `opened_at`, `closed_at`,
  `close_reason`, `default_epoch` (§4), and its list of **open intervals**.
  - **An interval** has a start, an end and whether the end is **confirmed**. A new session has one
    interval; each Continue adds one. A crash leaves the open interval's end unconfirmed (§3).
  - **`close_reason`:** `app_closed`, `new_session`, `continued_other`, `logbook_changed`,
    `archive_switched` or `unconfirmed_exit` (§3). Names are illustrative; the set is fixed by the
    implementation and documented in `docs/v2-design/api-endpoints.md`.
  - **The operating app instance** that holds the open interval (§3, *App lifetimes*).
- **Archive-level session state:**
  - **`session_generation`:** one counter per archive, which advances on **every** session-state
    transition in that archive: an open, New, Continue, each close, and each discard. It is never
    reset, including when a discarded session's record is removed. A QSO joining an already open
    session is not a transition.
  - **`default_epoch`** (§4).
- **Home:** the archive file, beside the logbooks and QSOs it describes (ADR 0094, ADR 0096).
- **Authoritative, local only:** sessions are not sent to SM Cloud. A rebuild from SM Cloud restores
  QSOs without sessions (*Consequences*).
- **Naming:** `/v1/session/{email,export}` and `session.terminated` are already taken by other
  meanings. Wire names use **`operating_session`** (for example `operating_session.opened`).

### 2. Membership

- **Who joins:** a QSO joins the open session when it is stored by a **live** logging path into the
  session's logbook:
  - the Phone/CW submit that names the effective default logbook (the `requireLiveSubmit` and
    `submitClassifiedLive` classification in `internal/api/handler_qso.go`);
  - the FT8 completed-exchange sink (`cmd/smd/lifecycle_adapters.go:736`).
- **The logging transaction, in order,** inside the QSO's own transaction (the atomic-write
  invariant), at the daemon's current instant:
  1. Read the archive's open session, if any.
  2. **The default-change test (§4):** if a default-logbook change has taken effect since the
     session opened, close it with `logbook_changed` (advancing the generation).
  3. **The safety net:** if no session is open now, create one bound to the effective default
     logbook and the current `default_epoch`, and advance the generation. Under the lifecycle (§3)
     the operating app always holds a session, so this is expected only in rare races (for example
     a contact admitted while the app is starting); it exists so that no QSO is ever refused or
     left outside a session.
  4. Write the membership association.
- **What can and cannot stop a QSO.**
  - **Cannot:** no session-policy decision (a stale generation, a pending default change, an
    unanswered recovery question) and no external dependency can refuse or delay a live QSO.
  - **Never silently continued:** the safety net always opens a **new** session; continuing an
    earlier one is only ever the operator's Continue.
  - **Database failures:** the session writes are in the QSO's transaction. If one fails, the whole
    transaction rolls back, as a failure of the QSO row or its upload-queue rows does today. That is
    the existing permitted reason not to log (a broken local database).
  - **Contention must not become a failure.** Session transactions (controls, app start and exit,
    the default-change test) and the logging transaction **serialize before reading the mutable
    state they will update**, so none reads a snapshot another has changed and then fails to write.
    They respect cancellation and the existing transaction limits. The mechanism is left to
    implementation; the per-connection busy timeout (`internal/database/sqlite/internal.go`) alone
    does not establish this. **A concurrent logging-and-control test is required.**
- **Never joins:**
  - **existing contacts**, logged before this ships: sessions run forward only, with no backfill;
  - **imports** of any kind: ADIF import (`SubmitImport`, `SubmitImportBatch`), SM Cloud restore
    (`Restore`, `RestoreInto`), and archive copy or transfer, even when the QSO's time falls inside
    a session interval;
  - **a submit that names a non-default logbook** (a deliberate operation, ADR 0097 §2).
- **Later changes:**
  - **Edits** keep membership, including edits to time, band or mode (as for contest membership,
    ADR 0096).
  - **A deleted QSO** keeps its association row, but every consumer excludes it; an undelete brings
    it back. A session whose members are all deleted is therefore **not** empty (§3).
  - **Membership never moves on its own.** Explicit add or remove actions are deferred (O2).
- **Not derived:** membership is never computed from time windows. An import or edit would
  otherwise silently move a QSO in or out.

### 3. Lifecycle

#### App lifetimes

- **The operating app** is the authenticated desktop app (ADR 0102; its authentication is W-0027
  C1/C2). Only it opens, closes or continues sessions. View-only clients, the Map and detached
  panels never do. (No view-only client is built: *Update 2026-11-13*.)
- **An app lifetime** runs from the app's start to its exit. Each lifetime has its own identity,
  presented with its authenticated connection, so the daemon can tell a reconnect of the same app
  from a new start, and an old app's late message from the current app's.
- **Within one lifetime, nothing is a session boundary except the controls and changes below:** a
  renderer reload, a changed-build reload, a reconnect, a closed or reopened panel, a second launch
  that raises the running app, and an ordinary daemon restart on the same archive and default all
  leave the session as it is.

#### Opening

- **At the start of a new app lifetime**, the daemon classifies the archive from **durable state**,
  never from a renderer flag:
  - **No open session** (the previous lifetime ended with a confirmed exit, or this is the first
    start): open a new session automatically, bound to the effective default logbook, and show it
    ("Session started 09:15"). No question is asked.
  - **An open session held by an earlier lifetime** (the previous app crashed, lost power, or exited
    without a close the daemon acknowledged): this is a **recovery**: the app asks **Continue or
    New** before it shows the operating workspace (§6).
    - **An empty remnant (operator ruling 2026-11-10: "discard without asking"):** if that
      session has **no membership rows at all** (counting links to soft-deleted QSOs), it is
      discarded and a new session opens without the question, since there is nothing to continue.
      Before it is judged empty, any completion work the earlier lifetime had already earned must
      have finished: the FT8 logger runs it as a tracked background job that can enrich before
      submitting (`cmd/smd/lifecycle_adapters.go:743-777`, `cmd/smd/main.go:347-364`), so
      serialized database writes alone do not prove nothing is pending. Such a contact is
      preserved and joins the remnant (making it non-empty); the job is never cancelled to make
      cleanup possible.
  - **The state cannot be read:** the app reports the daemon as unavailable. An unreadable state is
    never treated as a clean exit.
- **While an operating app is attached and no session is open** (after a default-logbook change or
  an archive switch, §4), the daemon opens a new session at once. This is the same rule, so there is
  no state in which the running app has no session, apart from the recovery question and the
  closing phase of an archive switch (§4), when the daemon opens none until it exits or the
  switch fails.
- **An arrival after an archive switch is never a recovery** (§4): a session still open in the
  served archive is closed first, and a new one opens without the question.
- **One session per app lifetime at start:** initialization and its retries are idempotent; a
  retried start never opens a second session.

#### Closing

- **Confirmed exit:** after the operator confirms the app's exit (ADR 0102 ruling 6), and **after**
  FT8 stop and disarm and any completion logging already earned have finished, the app reports the
  exit, naming its lifetime and the expected generation. The daemon closes the session with
  `app_closed`, `closed_at` now and a confirmed interval end, and **acknowledges** it. Cancel at the
  exit confirmation changes nothing.
- **An unacknowledged exit** (the daemon was unavailable, or the report was lost before commit)
  leaves the session open. It is then an unconfirmed exit, handled as a recovery at the next start;
  no timeout is invented.
- **A late exit report from an earlier lifetime** never closes a later lifetime's session: the
  lifetime and generation checks refuse it.
- **New session:** closes the current session (`new_session`) and opens a new one.
- **Continue of another session:** closes the current session (`continued_other`) and resumes the
  chosen one (§8).
- **Default-logbook change or archive switch:** §4.
- **Discarding empty sessions (ruling 4):** whenever a session closes, the same transaction checks
  whether it has **any** membership row (including rows for soft-deleted QSOs). If none, the
  session's record is removed instead of kept. The check is serialized with membership writes, the
  generation still advances, and the resulting state is published after the commit. Emptiness is
  never inferred from plotted points, filtered rows or a failed query.
- **The app closes while the recovery question is unanswered:** nothing is resolved; the earlier
  session stays open, and the next start asks again.
- **The exit acknowledgement is lost after the close committed:** at its next contact the app
  reconciles from durable state. A committed clean close is never reopened or reclassified as an
  unconfirmed exit.

#### Recovery

- **Continue (recovery):** keeps the session's identity; records the interrupted interval's end as
  **unconfirmed** (no crash time is invented); starts a new interval held by the new lifetime; and
  opens the workspace. The header clock starts again at zero (§7).
- **New (recovery):** closes the recovered session with `unconfirmed_exit` and an unconfirmed
  interval end (discarding it if empty), then opens a new session.
- **Neither** resumes an FT8 run, arms TX, starts or stops contest mode, or moves any contact.
- **Before the answer,** the operating workspace is not shown, so no Phone/CW contact can join the
  recovered session by accident. A contact the daemon has already admitted (for example an FT8
  completion that was in flight) is still logged, into the recovered session, which is still open;
  session policy never refuses it.

#### Restart and clock steps

- **A daemon restart** (power cut, deploy, crash) **does not close** the session. The record is
  durable; when the same app reconnects, its lifetime still holds the session.
- **Time is stored as UTC instants.** With no expiry, a clock step changes only the recorded times
  of later transitions; it never closes a session.

### 4. Archive and default-logbook changes

Both are **attended daemon restarts** with a page reload; the app itself stays running
(`internal/archive/activate.go`; `frontend/app/src/lib/config/archives.svelte.ts` `settleRestart`,
`reloadRestarted`; `defaultChange.svelte.ts`). A restart or reload is therefore not a boundary by
itself; the boundary is the change **taking effect**.

- **Default-logbook changes: closure when the change takes effect, by a default epoch.**
  - **In the archive file:** `default_epoch`, advanced **only by an actual change of the default
    pointer** (`applyDefaultChange`, `cmd/smd/default_change.go`):
    - **X→Y:** the pointer and the incremented epoch are written in one SQLite transaction;
    - **Y→Y**, the durable rewrite of an already-selected target (`cmd/smd/default_change.go:170`,
      `commitPointer`): the epoch is **preserved**, so a retained selector followed by another
      restart closes nothing;
    - **crash recovery:** the pointer and its epoch commit together or not at all.
  - **The test:** a session whose `default_epoch` differs from the archive's closes with
    `logbook_changed`. Its `closed_at` is the **recorded closure time**, the evaluation that
    persists the close, not a measured instant of the change. That covers X→Y→X: the epoch moved
    twice even though the logbook is X again.
  - **Where the test runs:** a start node before `http`, every session transaction, and every
    logging transaction before membership (§2). It is derived from durable state, so no single run
    is load-bearing.
  - **Then:** with the app attached and no session open, a new session opens for the new default
    (§3). Continue offers only the new default's sessions (§8).
  - **Outcomes** (from `cmd/smd/default_change.go` and `internal/archive/default_change.go`):

    | Outcome | Epoch moves? | Session |
    |---|---|---|
    | `effective` / `applied_cleanup_pending` | yes, if the pointer changed X→Y; a Y→Y rewrite does not move it | closes at the first evaluation after an X→Y change, and a new one opens |
    | `not_applied_intent_pending` | no | unchanged between attempts; a later `effective` start moves the epoch then |
    | `pending_unclear` | not yet | unchanged until a start applies it |
    | `restart_failed`, `not_applied_abandoned`, cancelled, `withdrawal_unconfirmed` | no | unchanged |

  - **A rejected or failed change never ends the sitting.** Session controls are never blocked by a
    pending change. Live admission during a change stays governed by its own seals (ADR 0097).
- **Archive switches (ruling 6; O9 ruled 2026-11-12): a switch closes the current session, and
  the archive that comes up opens a new one.** The operator: "Switching an archive should close
  the current session and open a new one - simple for now. If something else is needed will deal
  with it when there is a need."
  - Each archive holds its own sessions and QSOs; nothing crosses archives.
  - **The order of a switch request** (`Activate`, `internal/archive/activate.go`):
    1. **Validation and preflight**, unchanged: an invalid target, a pending default change, a
       missing restart port or a failed identity check is refused before anything is sealed or
       closed, so a refusal never ends the sitting.
    2. **Admission seals:** FT8 TX and the rig, as today, then the **live Phone/CW entry gate**
       (`api.LiveEntryGate`, today taken only by a default change, ADR 0097; operator ruling
       2026-11-12: "Refuse, as recommended — like the default change"). Taking it waits
       for every already admitted Phone/CW submit to finish; each commits into the open session
       before the close. A submit arriving after it is refused with the switch's own code and
       "nothing was logged — log it again once the page reloads", as for a default change.
    3. **The FT8 completion drain:** the switch then waits for every already earned FT8
       completion logging job (`launchFt8QsoLog`, tracked on `qsoLogWG`, each bounded by the
       existing `ft8QsoLogTimeout`, `cmd/smd/main.go:81`) to finish; each commits into the open
       session before the close. With TX sealed no new completion can start, and the
       implementation must prove that no job launched before the seal escapes the wait. A
       switch requested just after an FT8 contact therefore waits for that contact's logging.
       How long that takes has not been measured. Under the operator's ruling of 2026-11-12
       (`docs/dogfood-inbox.md`: FT8/FT4 logging never waits on enrichment), it is only the
       write once that is built. A finished job is not proof the contact was stored: a job can
       end with a failed write, and a context deadline is cooperative, not a forced stop.
    4. **The close,** in the departing archive's file: `close_reason` `archive_switched`, the
       generation advanced, an empty session discarded. If it fails, every seal is released and
       the switch is not requested, so a broken database is reported rather than switched away
       from. The session is still open and the sitting continues.
    5. **The pending intent** (`PendingQsoArchiveID`, in `config.json`) and **the restart
       request**, as today. These are a different durability boundary from the archive file
       (`internal/config/doc.go`), so the close and the intent are not one transaction. If the
       intent cannot be recorded or the restart request fails, the seals are released as today;
       the close stands, the closing phase ends, and the ordinary rule (§3) opens one new session
       in the departing archive. A retried switch closes that session in turn; it is discarded if
       nothing was logged in it, so retries do not accumulate sessions.
  - **What is already true before a switch can be asked for** (source reading, 2026-11-13; the
    operator's question: "if I switch out of FT8 to Settings, does not the TX immediately
    disable?"). Not immediately: the disarm **starts** five seconds after the last FT8
    subscriber has gone, provided none has returned; in the desktop app there is no second FT8
    view (below). Five seconds is the grace before the disarm begins, not a bound on when it
    has finished: taking the locks and waiting for a transmission in flight to end come after
    it (codex-103).
    - Activation is offered only in Settings → Archives
      (`frontend/app/src/lib/config/ArchivesSection.svelte`). The shell shows one main view at a
      time (`App.svelte`), so opening Settings destroys the Operate view.
    - The FT8 event stream is view-scoped: the FT8 view opens it on mount and closes it on
      destroy (`Ft8View.svelte`, `stopFt8`).
    - When the last `/v1/ft8/events` subscriber is gone, the daemon waits `captureLinger`, 5 s
      (`internal/ft8/service.go:21`, `onSubscriberRemoved`). If none has returned, it disarms TX
      (`disarmUnattended`), which aborts a transmission in flight and **abandons** the active
      contact, and then releases the audio device (`onLingerExpired`, `disarmTx`). A return
      within the five seconds changes nothing.
    - **The desktop app has one FT8 view** (the operator, 2026-11-13: "with the new desktop,
      there should be not other window or tab having FT8 open?"). Correct, by design and as
      built: the app is single-instance and a second launch raises the running window
      (`desktop/src/main.mjs`, `requestSingleInstanceLock`); it creates one window
      (`desktop/src/window.mjs`); and ADR 0102 ruling 2's window set keeps Operate, and so the
      FT8 view, in the one main window. The Map, Manual, Rig Control and Session windows of
      W-0027 A2 are not the FT8 view, and each exists at most once. Only `Ft8View` opens the
      stream in the SPA.
    - **Any other client of `/v1/ft8/events` is a subscriber today**, a plain browser tab being
      the likely one: `smd` still serves the UI to a browser on its own listener, and the route
      has no operating-client check yet (`internal/api/server.go`; codex-103). Such a client
      keeps a subscriber, so TX would stay armed and a run continue after the desktop app left
      FT8. ADR 0102 assigns the remedy to W-0027 C2: presence becomes the desktop app's
      authenticated operating subscription. C2 is not built. The operator (2026-11-13): "These
      situations is what partly prompted the switch from browser to desktop". Claude's reading
      of that, not an explicit ruling: no interim measure is planned and C2 settles it.
      (Overtaken in part: the check now exists and is off by default; see *Update 2026-11-13*.)
    - Either way a switch never runs beside live FT8, and that does not depend on navigation:
      the FT8 seal in step 2 refuses while a sequence is active, a transmission is in flight or
      TX is armed (`SealTxAdmission`, `internal/ft8/servicetx.go`). It is the safety gate. So
      an activation asked for before the disarm has finished, or while another client keeps
      FT8 running, is refused as busy.
    - An abandoned contact is not logged. A contact that had already completed is: its logging
      job is what the drain in step 3 waits for.
    - Not measured on the station.
  - **The closing phase** runs from the close until the process exits or the switch fails
    (step 5). Because both live paths were drained before the close, no contact can join a
    session in it, so it creates no session. In it:
    - the daemon opens no session for the attached app;
    - **New** and **Continue** are refused with a distinct switch-in-progress code, changing
      nothing; the window redraws when the page reloads;
    - **an exit report** is acknowledged without change (the session is already closed), so the
      app can exit cleanly; the next start then finds no open session and opens a new one.
    - The safety net (§2) is unchanged: a contact stored here through a path this ADR does not
      know of would still be logged in a new session, then handled by the ordinary rules (§3),
      including recovery. Such a path is a defect for the tests below to catch.
  - **The arrival:** at every start that processes a pending activation, whether it is applied or
    falls back to the departing archive, a session still open in the served archive is closed with
    `archive_switched` (discarded if empty) before the app is served, and the ordinary rule (§3)
    opens a new one without the recovery question. That covers a remnant left in the target by an
    earlier crash. A switch never relies on the arriving archive holding no open session.
  - **Ownership:** only the served archive's sessions are live. Nothing reads or changes the
    sessions of an archive that is not served; they are acted on only when it is next served.
  - **Accepted cost:** a switch that fails after its close still ends the sitting in the
    departing archive. Its contacts stay where they are, and Continue from the list brings the
    closed session back. A power loss after the close and before the intent is durably recorded
    (including an uncertain `config.json` write that did not survive) leaves the session closed
    and nothing open, because the drains left no closing-phase session; the next start opens a
    new one without a question.
  - **Returning to an archive** later is an arrival, so it opens a new session there. Continue
    from the list brings an old one back. Nothing is continued silently.
  - **Required tests:**
    - a Phone/CW submit crossing the seal: admitted before it, logged in the session being
      closed; after it, refused and nothing logged;
    - an FT8 completion job running when the switch is requested: the close waits for it, and the
      contact is **stored** (the QSO row and its upload-queue rows committed) in the closed
      session; the job reaching the end of the wait is not enough;
    - no completion launched before the TX seal escapes the drain;
    - power loss after the close and before the intent, and the same with an uncertain
      `config.json` write that did not survive: the next start opens a new session without the
      recovery question, and the earlier session stays closed with its contacts;
    - a restart-request failure with the app attached: seals released, the phase ended, one new
      session in the departing archive; a retry does not leave an empty session behind;
    - a close failure: nothing requested, the session still open;
    - New, Continue and an exit report during the closing phase;
    - an arrival with a remnant in the target: closed, a new one opened, no recovery question.
  - **An active contest** in the departed archive keeps its record (ADR 0101); contest mode is
    independent of sessions (§9).

### 5. Several windows, conflicting choices

- **One session for every window:** all windows show the same session, and the daemon is the only
  authority. The Session panels and the Map follow the `operating_session.*` events on the shell
  event stream (`/v1/events` and its WebSocket twin, ADR 0099).
- **Each control action carries** the **expected generation** (the archive's `session_generation`
  from the state the window showed) and the archive identity (the existing `X-SM-Archive-Id`
  request guard). There is no request id and no recorded result.
- **Every state carries its generation.**
- **The order of a control transaction:**
  1. **The default-change test** (§4).
  2. **Stale check.** If the expected generation is not the current one, refuse with a distinct
     stale-action code, change nothing, and return the current state with its generation; the
     window redraws to it.
  3. **Apply** the transition and advance the generation, in the same transaction.
- **A refusal is not a rollback.** A close found by step 1 is committed and published even when step
  2 then refuses the control. Only a database failure rolls the transaction back.
- **Retries after a lost response.** A retry carries the generation of the original press,
  unchanged. If the first attempt committed, the retry is refused as stale and the window redraws to
  the state the first attempt produced; the action can never apply twice. A window never silently
  re-reads the state and resends the press against a new generation.
- **Examples:**
  - window A presses New at generation g, which becomes g+1; window B's delayed New at g is refused;
  - window A continues S0 while window B presses New, both at g: the first applied wins, and the
    other is refused;
  - window A's New times out and is retried at g after the first attempt committed: refused as
    stale; the window shows the new session, and no second session is opened.
- **Closing a window** never affects the session. Only the confirmed exit of the operating app does
  (§3).

### 6. The recovery question

- **Shown only after a crash or unconfirmed exit** that left a session with members (§3), at the
  start of the next app lifetime, **before the operating workspace**. A clean start shows no
  question.
- **It names the session:** its start time, contact count, and that the previous sitting did not end
  normally. It offers **Continue** and **New session**. There is no "Not now": the workspace opens
  after the answer.
- **Answers are generation-checked.** A late or duplicated answer is refused as stale and the app
  redraws to the current state.
- **One operating app owns the question.** The Map, detached panels and view-only clients never ask,
  and never open a session. (No view-only client is built: *Update 2026-11-13*.)

### 7. Consumers of the one membership

- **Session panel:** looks and feels as today (same columns, in-place edit, export, email). Its rows
  are the current session's members, read from the daemon and updated live. It shows the session's
  start time, contact count and state, and the controls (§8). The per-tab `sessionStorage` lists
  retire.
- **Map (operator ruling 2026-11-07, adapted):**
  - **Outcome:** a Map and a Session panel show the same session identity and member UUIDs, before
    map filters and coordinate availability. A new window never creates a private session.
  - **Display rule:** show the open session. While the app runs, that is always the current session,
    even when it is new and empty: neither view falls back to an older session's rows. When no
    session is open (no operating app attached, for a view-only observer), show the latest closed
    session in the effective default logbook, clearly labelled Closed, or nothing if none exists.
    Showing a closed session never continues it. (The view-only observer is not built; what is left
    of this fallback is in *Update 2026-11-13*.)
  - **First slice:** the Map follows this session automatically. Remove the last-N-hours selector
    from this view; do not apply a hidden time cutoff or inherit `sm-map-window`. Imports and
    unrelated contacts stay outside it even when their times fall within the session's intervals.
  - **Presentation:** show the session's start and state; retain band filtering; distinguish the
    member count from the plotted and filtered counts; a missing location does not remove
    membership; an empty session, a load failure and partial coverage have distinct presentations;
    a failed read never falls back to a recent-hours query.
  - **Several windows and recovery:** New, Continue, closes, archive and default changes, QSO edits
    and deletion update both views. A reconnect re-reads authoritative state. A late read from a
    previous archive or session cannot overwrite the new view. Opening the Map never affects a
    session.
- **Session email and export:**
  - the request names the session `id` and the selected member UUIDs, and the daemon refuses
    non-members. The UUIDs are still listed so that a not-yet-emailed delta can be selected;
  - "already emailed" comes from the row's `sm_fwrd_by_email_status`, so every window agrees;
  - ADR 0095 (exports report exactly what they delivered) is unchanged.
- **FT8 repeat hold after Ignore (inbox item 2):**
  - **The verdict moves to the daemon.** It decides from the current session's membership whether
    the held station has a **qualifying QSO**: a member of the open session, not deleted, with the
    held station's `CALL` exactly, the `BAND` of the run's pinned dial (`dial_freq_mhz`), and the
    run profile's `MODE` and `SUBMODE` (absent read as empty; FT8 = `MODE FT8`, no submode; FT4 =
    `MODE MFSK`, `SUBMODE FT4`, `internal/ft8/profile.go`). (Ruled 2026-11-07, O4.)
  - **Unchanged: the first worked-before hold**, which still asks the whole logbook with its
    existing predicate (`internal/ft8/repeathold.go`, `IsContestDuplicateByLogbookIDWithContext`).
    Only what happens after Ignore reads the session. The hold's wording must not suggest "this
    session".
  - **Stale-action protection:** an Ignore names `run_id`, a `hold_id`, and the held station's call,
    band and mode; the daemon applies it only if that run is live and that hold is still open for
    that station.
  - **Rechecked at use:** the single-use bypass is used only if, when the station is answered, the
    session then open still holds no qualifying QSO. New and Continue therefore change the scope
    deliberately: Continue brings the continued session's contacts back into scope.
  - **If the recheck's lookup fails,** the existing bypass stays usable and the failure is logged
    ("never block operation on a lookup", `internal/ft8/repeathold.go:67-69`).
  - **With no session open** (rare under §3: the safety net and the recovery question), "no
    qualifying QSO" holds and the bypass is granted (ruled 2026-11-07, O5).
- **The header session clock** (`frontend/app/src/lib/ui/SessionTimer.svelte`): the elapsed time of
  the **current open interval** (ruled 2026-11-07, O8, adapted). A new session or a Continue (normal
  or recovery) starts it at zero. It is stable across reloads, other windows and daemon restarts,
  because its baseline is the daemon's stored interval start. It is distinct from contest operating
  time (`contestTimers.ts`), which is unchanged.

### 8. Session panel controls

- **New session:** closes the current session and opens a new one. Labelled for the new session it
  opens. There is no separate Stop: a session is always open while the app runs, so stopping would
  be New.
- **Continue a previous session (ruling 5):**
  - **The list:** closed sessions in the active archive whose `logbook` is the effective default,
    excluding the current one; most recent interval start first, with the UUID as a stable
    tie-breaker; each showing its dates, contact count and whether an interval end is unconfirmed.
    No end time is manufactured for sorting.
  - **Choosing one** closes the current session (`continued_other`; discarded if empty) and resumes
    the chosen one with a new interval, atomically, under the generation guard. A failure leaves the
    current session open. Eligibility is checked again inside the transaction; a foreign, removed or
    newly ineligible target is refused.
  - **Rebinding (codex-044):** after eligibility is checked, the same transaction sets the resumed
    session's `default_epoch` to the archive's current one and binds the new interval to the current
    app lifetime. Its identity and all earlier membership are kept. Without the rebinding, a session
    chosen after an X→Y→X change would be closed by §4's test at the next contact. **Acceptance:**
    continue S0 after X→Y→X, then log a contact: S0 stays open and receives it.
  - **Contacts never move.** Contacts already logged in the current session stay in it, and that
    session stays in the list. The panel says so beside the action. Continue is not a merge (O1).
- **Delete:** not in the first slice for sessions with members (ruling 7); empty sessions are
  discarded automatically (§3).

Every control obeys §5. A control appears only where it can act (the no-unactionable-instructions
ruling).

### 9. Boundaries: sessions, FT8 runs and contest occurrences

| | Operating session | FT8 run | Contest occurrence (ADR 0096) |
|---|---|---|---|
| Started by | app start, New, Continue, the safety net | Call CQ, answering, auto-work | declared by the operator |
| Ends | confirmed app exit, New, Continue of another, default change, archive switch | Stop, abandon, last FT8 subscriber gone | its declared window |
| Identity | session UUIDv7 (archive) | `RunID` UUIDv7, `APP_SM_RUN_ID` on QSOs | occurrence identity (archive) |
| Membership | explicit, live paths | the run's own QSOs | explicit, in contest mode |
| Owns | the Session panel, Map, email, FT8 eligibility | the run's declines and bypasses | serials, submission export |

- The three are **independent**. A session contains runs and may contain contest and non-contest
  QSOs; a contest may span several sessions and a session may span contests. None creates, closes or
  supplies membership for another.
- **Contest mode is untouched by sessions:** closing a session never stops contest mode, and New or
  Continue never starts it. ADR 0101 keeps its own state, end-of-window handling, serials, restart
  rules and draft preservation.
- A run can cross a session boundary (New or Continue mid-run): its next QSO joins whichever session
  is open at storage time.
- The app's confirmed exit stops the run and disarms TX first (ADR 0102 ruling 6), then closes the
  session. A default-logbook change or archive switch ends the run by its restart.

## What changed on 2026-11-10

- **Superseded:** the activity gap and its timer (U2, ruled 2 hours; P3's setting, limits and
  retry); counted activity (U1, P1); the contest suspension and its end baseline (U3, P2);
  `last_activity_at`; the opening question with "Not now" (old §6); lazy opening on the first QSO
  as the normal path; pick-up of only the latest session; S3's "archive returns follow the gap";
  S5 and S7 (no gap to change, no suspension to check); S6 (P1 is gone).
- **Kept:** the archive-local record with history; the membership association in the QSO's
  transaction and its exclusions; the generation guard and stale refusals (S1, S2); `default_epoch`
  (S4 rejected, 2026-11-07); the shared events; the Map ruling (adapted to "always an open session
  while the app runs"); the FT8 eligibility scope, recheck at use and lookup-failure rule (O4, O5,
  S8); the interval clock (O8, adapted).
- **Alternatives weighed in the 2026-11-10 exchange:**
  - **Keep the gap with the desktop app.** Rejected: the app's exit is a better signal, and the gap
    brought a timer, a setting, a contest suspension and counted actions.
  - **Ask Continue / New at every start.** Rejected by the operator as friction; asked only after a
    crash.
  - **Open lazily on the first live QSO, with "Not now".** Rejected by the operator: a session that
    appears when the app starts is more obvious than one that starts silently.
  - **Continue a crashed session silently.** Rejected: a crash can be days old, so the operator
    chooses.
  - **Keep empty sessions as history.** Rejected (ruling 4): a session with no contacts carries
    nothing to match against the log. A record of on-air time without contacts would be a separate
    station-activity feature.
- **ADR 0101 was revised to match** (2026-11-10, `d824755b`): contest mode no longer suspends or
  evaluates sessions, and it checks its own window end.

## Update 2026-11-13

Two operator rulings made after this ADR's last revision (2026-11-12) overtake parts of its text.
Neither changes a session rule, an operator ruling above or an open question. The passages are
left as they were written, each with a pointer here.

### No view-only browser client

**Operator rulings (chat, 2026-11-13):** "yes, drop view-only for C2", and "I don't want to have
code in the base to cater for a browser - too much complexity. Direct access by any client without
the secret, or a second+ client is refused." They are recorded in ADR 0102 (*Update 2026-11-13*,
amending its ruling 1) and in W-0027 (the C2 brief).

- **What exists now.** `smd` has the operating-client check (W-0027 C2a, `90db4857`): with
  `server.require_desktop_app` on, it serves only the desktop app's listener, and only to a request
  carrying the install's credential. The switch is temporary and off by default (operator ruling
  2026-11-13). Switching it on at the operator's station is W-0027 C2d, which is not done.
- **Where this ADR says "view-only clients" or "a view-only observer"** (§3 *App lifetimes*, §6,
  §7's Map display rule): that class of client is not built. With the check on, the only clients
  are the app's own windows and the operator's same-user tools, and none of them opens, closes or
  continues a session except the operating app, as ruled.
- **§7's Map display rule, the fallback.** "No operating app attached" can no longer be seen by
  anyone once the check is on, because every view is a window of the app. What is left of "no
  session is open" is the two short states §3 names: the recovery question, and the closing phase
  of an archive switch. The rule for them is unchanged: show the latest closed session, labelled
  Closed, or nothing. Whether those two states need the fallback at all is for the build brief,
  not decided here (the operator agreed to leaving it there, chat, 2026-11-13).
- **§4's "any other client of `/v1/ft8/events` is a subscriber today".** With the check on, a
  client without the credential is refused on that route like any other, so a plain browser tab
  can no longer hold an FT8 stream and keep a run alive. While the switch exists and is off, the
  2026-11-12 text still describes the daemon. Which connection counts as the operator's presence,
  and what its loss does, is W-0027 C4, which is not built. §4's sentence that ADR 0102 assigns the
  remedy to C2 is therefore true only in part: C2 refuses the other clients, C4 defines presence.
- **The dependency is unchanged:** sessions are built after the app's authentication (W-0027 C1,
  C2 and C4; *Consequences*).

### Station Events are to be retired

**Operator (chat, 2026-11-13):** "Station Events are going to be retired for something more
useful." Recorded in `docs/dogfood-inbox.md`; no replacement is designed, and `docs/backlog.md`
still lists W-0020 at the time of writing.

- **This design adds no Station Event.** The `operating_session.*` events ride the shell event
  stream (`/v1/events` and its WebSocket twin, ADR 0099; §5), which is not Station Events and is
  not part of that retirement.
- **`session.terminated`** (*Context*) is the existing Station Events kind for the end of an FT8
  contact. It stays unrelated to operating sessions and goes when Station Events go. The name
  `operating_session` was chosen to avoid it (§1); that choice stands either way.
- **The build must not record session lifecycle as Station Events** (agreed by the operator,
  chat, 2026-11-13). If a durable record of session opens and closes is wanted beyond the session
  table itself, it waits for whatever replaces Station Events.

## Open questions

**All settled (operator, 2026-11-12):** O9 ruled; O1, O2, O3 and O10 stay deferred, each with the
revisit trigger below, and are not in the first build; O6 was already ruled; the restore rule is
ruled. Nothing remains open for the first build.

- **O1. A split sitting.** Contacts logged in an automatically opened session stay there when the
  operator then continues an earlier one. Folding them in would be a separate, explicit action with
  a preview, not part of Continue. **Deferred.** Revisit when such a split matters to an output the
  operator uses (email, export, the Map, FT8 eligibility).
- **O2. Add to and remove from a session** (for example from the Logbook view), for existing
  contacts and imports. **Deferred.** Revisit when the operator needs an imported, pre-upgrade or
  mis-filed contact in a session output.
- **O3. Older sessions.** Continue from a list is ruled (ruling 5). Browsing past sessions beyond
  that list (reviewing or resending an old session's email, export or Map) remains **deferred**.
- **O6. The first start after this ships.** Start with an empty daemon session history: the first
  app start opens a new session, and earlier contacts are not part of any session. The tabs'
  `sessionStorage` lists are not imported and no membership is inferred. The Session panel says
  once, for example: "Sessions are now shared by every window. Earlier contacts are unchanged in the
  logbook; they are not part of a session." (Ruled 2026-11-07, adapted.)
- **O9. The archive-departure mechanism.** **Ruled 2026-11-12:** close at the switch request and
  open a new session in whichever archive comes up; a failed switch ending the sitting is accepted
  (§4). Revisit only if that cost matters in use.
- **O10. Deleting sessions with members** (ruling 7: not in the first slice). If selected later:
  confirmation that it removes the grouping, not contacts; contest membership, email marks and
  delivered files unchanged; later email or export naming the removed session refused explicitly;
  stale Continue choices and in-flight exports given their own acceptance rules.
- **Restore of open sessions.** **Ruled 2026-11-12:** a restore is treated like an archive switch.
  Any session open in the restored file is closed (`close_reason` illustratively
  `archive_restored`; an empty one is discarded), and a new session opens for the attached app.
  Nothing is continued silently; Continue from the list can bring the old session back. The
  backup and restore design (inbox item 4) applies this; `default_epoch` travels with the
  archive file.

## Alternatives considered

### A. Keep the per-tab session and share it between windows (BroadcastChannel or localStorage)

The daemon would stay unchanged. Rejected: the daemon's FT8 check still cannot see the session; a
window's view depends on another window staying open; several lists still disagree after a tab
closes. ADR 0049 rejected the same coupling.

### B. Derived sessions: boundaries computed at read time

ADR 0049's recomputable form, and the 2026-11-07 model comparison's model D. Rejected: the
boundaries are operator decisions, not functions of QSO times; an import or a time edit would
silently move a QSO in or out; a derived session has no stable identity for email and export. ADR
0096 rejected the window-derived form for contest membership for the same reason.

### C. A stamped `session_id` column on `qso` (ADR 0049's decision)

Rejected for the association: membership corrections would be QSO updates that bump `revision`
(ADR 0050) and re-sync rows with no contact change; and it adds a column to the most load-bearing
table. The association is written in the same transaction, so atomicity is equal.

### D. One station-wide session across archives

Rejected: QSOs and membership are archive-local. A session spanning archives would have members in
several files, and an archive's session history would no longer travel with it.

### E. A tab-supplied FT8 eligibility verdict (the earlier item 2 draft)

Superseded: the daemon holds the membership. The draft's stale-action protection (`run_id`,
`hold_id`) is kept.

### F. Purely manual sessions

Rejected, as in ADR 0049: the operator would forget, and the panel would be empty. Under this design
the session is never manual to start: it opens with the app.

### G. The activity-gap lifecycle (this ADR's design until 2026-11-10)

Superseded by the operator's rulings; see *What changed on 2026-11-10*.

## Consequences

- **New authoritative state in the archive file:** a log-set migration (a session table, its open
  intervals, the membership association, the per-archive `session_generation`, and `default_epoch`
  written in the default pointer's transaction); sqlboiler regeneration; the migration-head fallout
  across whole-tree tests. It is the first log-schema change since schema 19.
- **New API and events:** read the current session and its members; list sessions for Continue; New
  and Continue, each with an expected generation; the app start and confirmed-exit reports, bound to
  the app lifetime and generation, with the exit acknowledged; the recovery answer;
  `operating_session.*` events. `docs/v2-design/api-endpoints.md` changes with them.
- **Depends on the authenticated operating app** (ADR 0102; W-0027 C1, C2 and C4): only that app
  may open, close or continue sessions, and its lifetime identity is part of each lifecycle report.
  Sessions are built after that authentication, and they are W-0027 A2's prerequisite.
- **ADR 0102 ruling 6 is amended** (2026-11-10): a confirmed exit also closes the operating session,
  after FT8 stop, disarm and completion logging.
- **ADR 0094 is superseded in part** when this is accepted: per-archive Session lists stay, but they
  are daemon-owned and shared by every window. ADR 0049 stays Rejected; this ADR records that its
  trigger fired.
- **The live-submit and FT8-sink transactions gain session work:** the default-change test, the
  rarely used safety-net open, and the association write. No session policy, prompt or external
  dependency blocks logging; a database failure rolls the QSO back atomically.
- **Sessions are local only.** A rebuild from SM Cloud restores QSOs without sessions (input to the
  backup and restore design, inbox item 4).
- **Going forward only:** existing contacts have no session.
- **The Map surprise (item 5) and the FT8 Ignore machinery (item 2) get simpler.** The tab-verdict,
  deciding-tab and tab-lifecycle rules go away; the stale-action rules stay.
- **Simpler than the gap design:** no timer, no gap setting, no contest suspension, no counted
  activity, no expiry evaluation on every read.

## Triggers to revisit

- The app is routinely left running for days and operators want sittings split automatically:
  reconsider an optional inactivity limit (it would bring back a timer and its rules).
- A split sitting (O1) matters to an output: decide the explicit fold-in.
- A second operator or a real multi-op contest: sessions become the carrier for operator identity
  per shift (ADR 0049's profile trigger). Revisit the record shape.
- Sessions are wanted in SM Cloud or on another device: design their sync explicitly; do not stamp
  `qso` to get it.
- Operators routinely want to add existing or imported QSOs: build O2.

## References

- ADR 0049 (Rejected; this ADR's revisit), 0050 (`revision`), 0094 (per-archive Session lists), 0095
  (session exports), 0096 (contest occurrences: explicit membership), 0097 (default change at
  restart; its states), 0099 (event streams), 0101 (contest mode), 0102 (the desktop app; ruling 6
  amended).
- W-0027 (Electron desktop host; C1, C2 and C4; the A2 prerequisite; the C2 brief and its
  rulings of 2026-11-13).
- `docs/dogfood-inbox.md`, the triage of the 2026-11-06 notes: the cross-cutting daemon-session
  entry, item 1 (independent), item 2 (FT8 draft), item 5 (Map).
- `frontend/app/src/lib/operate/session.svelte.ts`; `frontend/app/src/lib/map/mapData.svelte.ts`;
  `frontend/app/src/lib/ui/SessionTimer.svelte`; `internal/api/handler_qso.go` (live
  classification); `internal/api/handler_session_email.go`; `internal/qsoservice/submit.go`,
  `submit_batch.go`, `restore.go`; `cmd/smd/lifecycle_adapters.go` (the FT8 sink);
  `cmd/smd/default_change.go`; `internal/archive/activate.go`, `internal/archive/default_change.go`;
  `internal/ft8/repeathold.go`, `internal/ft8/sequencer.go` (`RunID`), `internal/ft8/profile.go`;
  `internal/stationevents/stationevents.go`.
- The 2026-11-10 design exchange (scratch entries claude-078 to claude-085, codex-041 to codex-043);
  the earlier design and its reviews in the Git history up to `4a448c19`.
