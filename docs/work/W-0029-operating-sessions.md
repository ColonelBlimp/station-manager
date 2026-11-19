# W-0029 — Daemon-owned operating sessions: building ADR 0100

**Status:** Opened 2026-11-18 (operator: "W-0029 is fine, open the dossier"); ranked in
[`docs/backlog.md`](../backlog.md). The build brief was drafted (operator, the same day:
"start the ADR 0100 brief"), reviewed by Codex in five rounds, and **its questions are ruled
(operator, 2026-11-19: "Accepted")**. Nothing is built. Part 1 waits for the removal of the
database half of the downgrade tooling (ruled; its plan is in W-0027). The
build waited for the removal of the operating-client switch (operator, 2026-11-18, below);
that removal is done and accepted at the station (W-0027 C2e, 2026-11-19), and the operator
has returned to this work (2026-11-19, below). Each part starts on the operator's direction,
after the brief has been reviewed.
**Outcome:** the Session, the list of contacts made in the current sitting, is owned by `smd`
and is the same in every window, as decided in
[ADR 0100](../decisions/0100-daemon-owned-operating-sessions-with-explicit-membership.md)
(Accepted 2026-11-13). A session opens when the desktop app starts and closes at its confirmed
exit; after a crash the app asks Continue or New; the Session panel, the Map, session email
and export, and the FT8 repeat hold all read the one membership.

`W-0029` is an immutable identity. Its status may change, while priority and ranked position live
only in [`docs/backlog.md`](../backlog.md).

**Why a dossier of its own.** [W-0027](W-0027-electron-desktop-host.md) builds the desktop
app's shell and names ADR 0100 as "separate prerequisite work outside this dossier's shell
scope". Two of its slices wait on this build: A2 (the window set, whose detached Session needs
one authority) and the whole of A3 (a confirmed exit reports to the daemon, which closes the
session and acknowledges it).

**Depends on:** the authenticated operating app, W-0027 C1, C2 and C4, all accepted (C4 on
2026-11-18). The app lifetime and its admission, built in C4a, are what a session's lifecycle
reports are bound to (ADR 0100, Consequences).

**Not in scope:** what ADR 0100 defers (O1 a split sitting, O2 add and remove, O3 browsing
older sessions, O10 deleting sessions with members); sessions in SM Cloud; any Station Event
(ADR 0100, Update 2026-11-13); contest mode (ADR 0101); the backup and restore design, which
applies ADR 0100's restore rule itself; the shell's window set and its close confirmation
(W-0027 A2, A3).

## Working rules

Those of W-0027 (operator, 2026-11-09): small parts, each with its acceptance criteria stated
first, built test-first with reversion proofs, gated, reviewed by the operator and by Codex
through `.scratch/`, every finding fixed, and committed on the operator's direction before the
next part starts.

## Rulings

- **2026-11-18 (operator): "commit and push the D1 docs, then start the ADR 0100 brief".** The
  brief is directed. No build is.
- **2026-11-18 (operator): "W-0029 is fine, open the dossier".** This dossier.
- **2026-11-18 (operator): "If we are now planning to code-around a temporary feature, then
  deal with the tmp feature first! You need to check we are not wasting more time on coding
  and testing for features that we remove the next cycle - we are wasting time, effort and
  money!"** Said of the brief's question 2, which proposed a behaviour for the
  operating-client check's off state, a state ruled temporary on 2026-11-13. So the switch's
  removal (W-0027) comes before this build, and the brief's question 2 and its "with the
  check off" evidence are withdrawn.
- **2026-11-19 (operator): "2. Let's get started on sessions."** Chosen ahead of the removal
  of the database half of the downgrade tooling, which was the other thing offered. The
  work returns to the brief; no part is directed to be built by this.
- **2026-11-19 (operator): "Accepted".** Said of the nine items Claude listed in chat after
  Codex's fourth review of the brief (codex-179), each with its recommendation. As listed,
  with the brief's own question numbers:
  1. **The order and what is switched on when (question 1):** accepted. The record, the
     lifecycle, archive switches, the recovery screen and the close flow go to the station
     first; membership and the Session panel then arrive together.
  2. **Session start (question 3, proposal A):** accepted. The session opens when the app is
     admitted, not by a request of its own.
  3. **No fallback rows (question 4, proposal F):** accepted.
  4. **Plain SQL for the new tables (question 5, proposal G):** accepted, as a recorded
     deviation from ADR 0100's wording (its *Update 2026-11-19*).
  5. **The rank (question 6):** accepted. W-0029 second in the backlog, under W-0027.
  6. **A first run (question 7, "setup pending"):** accepted. A new install is admitted with
     no session, and the save that creates the default logbook opens the first one. Recorded
     in ADR 0100 (*Update 2026-11-19*).
  7. **The downgrade tooling's database half before part 1 (question 8):** accepted.
  8. **A daemon restart inside an exit (question 9):** (a), as recommended. The app closes
     saying the close was not confirmed, and the next start asks Continue or New.
     (Scope, as the brief's D has it and codex-180 asked to keep visible: this is the case
     where the exiting app still holds an open session. One that holds none is acknowledged
     by the fourth row, and an empty remnant is discarded at the next start without a
     question.)
  9. **Folding the up migrations:** not folded. The up files stay; only the way down goes.
  No part is directed to be built by this.

The design's own rulings are in ADR 0100 and are not repeated here.

## Record

### Build brief (2026-11-18; revised after review on 2026-11-18 and 2026-11-19; ruled 2026-11-19)

Written on the operator's direction ("start the ADR 0100 brief"). It is a plan for building an
accepted design, for review by the operator and Codex before any code. ADR 0100 decides the
behaviour and is not restated; where this brief proposes an order, a name, a shape or a
mechanism, that is a proposal. The questions at the end are the operator's. Codex's first
review (codex-146) found three things, all accepted: the first draft would have switched
membership on at the station before anything could ask Continue or New; its exit step checked
and waited where it had to exclude; and starting at the admission needed a rule that survives
a daemon restart and a lost answer. The order, proposals A, C and D, and the questions are
rewritten for them. Codex's second review (codex-177, 2026-11-19) found three more, all
checked in the source and accepted: the exit protocol closed "the session" even when the
exiting app held none; the start step had no state for a first run with no default logbook;
and a recorded exit was consulted only when no session was open. Proposals A, C and D, the
lock order, the evidence plan and the questions are revised for them. Its third review
(codex-178) accepted those and found one more, also checked and accepted: a recorded exit
lives in one archive file, and an archive can be switched by a caller with no lifetime while
a closing app still waits for its answer. D's "who may report an exit" and the shell's rule
below are for that. Its fourth review (codex-179) accepted that contract and asked for three
things, all taken: the shell's rule as a real barrier over admission work already in flight;
the four exit rows decided under the common order, not before it; and the third row's cost
put to the operator, which is question 9.

**Outcome the operator sees.** Starting the app opens a session and says so ("Session started
09:15"). The Session panel looks and works as it does today, but its rows are the daemon's:
every window shows the same list, and a contact deleted elsewhere leaves it. Closing the app
by its confirmed exit ends the session. After a crash, a kill or a power cut, the next start
asks Continue or New before the workspace, unless nothing was logged. The panel has New
session and Continue from a list. The Map shows the session, not the last hours of the
logbook. The header clock counts the current sitting and agrees in every window. After an
Ignore on an FT8 repeat hold, the daemon decides from the session.

**Nearest confusable outcome.** The panel looks right in the one window because it is still
its own browser list, while the daemon's membership differs: a contact missing, an import
included, or an old session silently carried on by a restarted app. Every part below is
judged against the daemon's membership, not against what one window shows.

**What exists today** (read from the tree on 2026-11-18; each line was checked at the place
named).
- The Session list is per window, in `sessionStorage` under `sm.session.qsos.<archive>`
  (`frontend/app/src/lib/operate/session.svelte.ts`). Rows come from the window's own
  Phone/CW submit response (`main.ts`) and from `ft8-logged` on its FT8 stream
  (`ft8Logged.ts`). There is no delete, no clear, no cap and no start time.
- Its readers: `SessionPanel`, the rail's count (`UtilRail`), `ExportDialog`, in-place edit
  (`sessionEdit.svelte.ts`), and the FT8 band activity's same-session check
  (`Ft8BandActivity.svelte`), which also reads a second per-window store,
  `sm.ft8.engaged.<archive>` (`ft8.svelte.ts`).
- The header clock is a third per-window value, `sm.session.startedAt`
  (`SessionTimer.svelte`), not tied to the list or the archive.
- `smd` has no operating session: no table, no column, no route. `/v1/session/email` and
  `/v1/session/export` take UUID lists from the window (`internal/api/server.go`).
- The live logging transaction is the QSO row and its upload-queue rows
  (`internal/qsoservice/submit.go`: `BeginTxContext`, `InsertQsoTx`, `InsertQsoUploadTx`,
  `Commit`). Phone/CW reaches it through `requireLiveSubmit`; an FT8 completion through a
  tracked job (`launchFt8QsoLog`, `cmd/smd/main.go`), which the FT8 stop does not wait for.
- The log schema's head is 19 (`migrations/log/0019_contest_serial`). Both ways of reaching
  a table are in the tree: generated models, which include tables of later migrations
  (`models/operator_event.go`, `archive_metadata.go`, `logbook_destination.go`), and plain
  SQL for the newest (`contest_serial.go`). The package comment that says the models come
  from the first migration is out of date (codex-146).
- The app lifetime (W-0027 C4a) is 16 random bytes made by the shell per start, sent as
  `X-Sm-App-Lifetime`, admitted by `POST /v1/app/lifetime`. The admission is in memory: a
  daemon restart forgets it and the running app is admitted again. Its check of a retired
  lifetime comes before it replaces the current one (`admit`,
  `internal/api/app_admission.go:149`). The shell sends nothing when it quits
  (`desktop/src/main.mjs`), and it admits before it loads the SPA
  (`desktop/src/waiting.mjs:199-206`).
- A first run has no logbook: before setup is saved there is no default logbook row
  (`ensureDefaultLogbook`, `cmd/smd/main.go:410`, returns early while `SetupComplete` is
  false); the SPA's setup save creates it (`seedDefaultLogbook`,
  `internal/api/handler_config.go:915`).
- Events for the windows go through one hub (`internal/events`) to `/v1/events` and its
  WebSocket twin; the SPA forwards only the three `qso.*` names (`lib/api/log-events.ts`).

**Proposed parts. Each is reviewed and committed before the next. Committing a part and
switching its behaviour on are separate, and the second is stated for each.**

| Part | What it is | On when it is at the station? |
|---|---|---|
| 1 | The record: log migration 0020 and the transitions as transactions, called by nothing | No behaviour. The schema moves to 20 |
| 2 | The lifecycle, the read routes, the exit report and the events | Yes, and nothing shows: with no membership every session is empty and is discarded when it closes, so no start ever has a question to ask |
| 3 | Archive switches (ADR 0100 §4): the live-entry gate at a switch, the completion drain, the close, the closing phase, the arrival | Yes. One thing shows, already ruled: a Phone/CW submit during a switch is refused as during a default change |
| 4 | The recovery question, as a screen of the main window's page, with its answer | Yes, and it cannot appear yet: it needs a remnant with members |
| (W-0027 A3) | The shell's close flow: the question on close, its own disarm, the exit report and its answer | Yes. From here a normal close is a confirmed exit |
| 5 | Membership: the association, the default-change test and the safety net in the logging transaction | **Held.** Committed, but not to reach the station without part 6 |
| 6 | The Session panel, the rail's count, the header clock and the controls read the daemon; the browser lists retire | With part 5, as one change at the station |
| 7 | The Map follows the session | Yes |
| 8 | Session email and export name the session; the daemon refuses non-members | Yes |
| 9 | The FT8 repeat hold: the verdict after Ignore moves to the daemon | Yes |

- **Why membership waits for parts 4, A3 and 6.** The moment a contact is in a session, a
  start after an unconfirmed exit must ask Continue or New before the workspace (ADR 0100
  §3). So the screen that asks (part 4) and the close that confirms an exit (A3) come first.
  And from that moment the panel must show the daemon's rows, or the question's answer would
  change nothing the operator can see: so parts 5 and 6 go to the station together.
- **If parts 5 and 6 were deployed apart,** the daemon would hold the session and ask after a
  crash, while the panel still showed its own browser list. Nothing is lost, but the two
  disagree. I propose that as not acceptable at the station; it is question 1.
- **The cutover of each reader, at part 6.** The store module keeps its shape and its
  callers, and changes where its rows come from:

  | Reader | Before part 6 | From part 6 | Finished by |
  |---|---|---|---|
  | Session panel, rail count, in-place edit | the window's browser list | the daemon's members, kept current by events | part 6 |
  | Export and email dialog | UUIDs from the browser list, sent to the existing routes | UUIDs from the daemon's members, sent to the existing routes | part 8 adds the session's id and the refusal of non-members |
  | FT8 band activity's same-session check | the browser list and `sm.ft8.engaged` | the daemon's members and `sm.ft8.engaged` | part 9 retires `sm.ft8.engaged` with the verdict |
  | Header clock | `sm.session.startedAt` | the open interval's start | part 6 |
  | Map | the last hours of the logbook | unchanged | part 7 |

  No reader depends on a list that has been retired.
- **An upgrade scenario, for the tests of parts 5 and 6:** an app left running across the
  install, with a contact logged, closed normally by the old code and by a kill. Both are an
  exit the daemon did not acknowledge, so the next start asks once. That is the designed
  behaviour, shown by test, not a defect to work around.

**Proposals inside the parts, for review.**

- **A. App start is the admission, decided from durable state.** ADR 0100 says the daemon
  classifies "at the start of a new app lifetime"; C4a has that moment, a lifetime becoming
  current. I propose the session's start step runs there, not as a request of its own. The
  admission is in memory and the same app is admitted again after every daemon restart, so
  the step decides only from what the archive file holds.
  - **Before the step, at every start:** the arrival after an archive switch and the
    default-epoch test (ADR 0100 §4) have run, so a session that a switch or an X to Y
    default change ended is already closed. "Held by L" below therefore never means a
    session of an earlier default or of a departed archive.
  - **First, before any scope is stopped: is L's own exit recorded (C)?** If so the
    admission is refused as retired, whatever session is open and whoever holds it. A later
    lifetime's session, its holder and its scopes are not touched by a retired caller. This
    keeps the order the in-memory check has today.
  - **Then,** for a lifetime L becoming current, after the scopes are stopped and before the
    lifetime is published:

  | The archive holds | The step does |
  |---|---|
  | no default logbook (setup not completed) | nothing. The state is "setup pending": L is admitted so the app can reach setup, and holds no session. See below |
  | an open session held by L | nothing. This is a reconnect, a daemon restart, or a retry after a start that committed and whose answer was lost |
  | an open session held by another lifetime, with members | nothing. The state is "recovery pending": L does **not** hold it, and nothing continues it until the operator answers (part 4) |
  | an open session held by another lifetime, with no members | waits for completion work the earlier lifetime had earned (ADR 0100 §3), judges again, and if still empty discards it and opens a new one held by L |
  | no open session | opens a new one held by L |

  Every row is idempotent: running it twice gives the state of running it once. An
  unreadable state fails the admission, which the shell shows as its waiting page; a default
  that is absent and a database that cannot be read are told apart, and only the first is
  "setup pending".
  - **Setup pending (found by codex-177; a question for the operator, 7).** ADR 0100 names
    two states in which a running app has no session, the recovery question and the closing
    phase of a switch. A first run is a third: there is no default logbook to bind a session
    to, and the app must be admitted to reach the setup that creates one. I propose the
    daemon opens no session and invents no logbook; the save that creates the default
    logbook then opens the first session for the current lifetime, by ADR 0100's own rule
    ("while an operating app is attached and no session is open, the daemon opens a new
    session at once"). A daemon restart between the save and that open is covered by the
    last row at the next admission. That open takes the one order below from the
    admission's lock, and checks again there that the lifetime is still current and not
    retired, so a save racing an exit or a replacement opens nothing for a lifetime that
    has gone. If accepted, ADR 0100 gets a dated update naming the state.
- **B. Withdrawn.** The first draft stored a digest of the lifetime. The lifetime is "a
  label, not a secret" (`internal/api/app_admission.go`), so the record holds the label, and
  the current-lifetime and generation checks are what protect an action, as today.
- **C. A recorded exit retires the lifetime, durably, and the record is authoritative.**
  Every acknowledged exit records which lifetime exited, whether or not it closed a session
  (D). Retirement in memory is not enough: after the exit commits, the answer is lost and
  the daemon restarts, the retired set is gone and the still-exiting app would otherwise be
  admitted and given a new session. With the record, A's first check answers it "retired",
  which that app's close flow takes as its acknowledgement. The app invents no new action
  to learn what committed. The record is in the archive file, with the close it belongs to.
  - **The record is per archive, and that is a limit to close, not to assume away
    (codex-178).** An archive switch can be asked for by a caller with no lifetime: the
    activation route has no current-lifetime check (`internal/api/server.go:371`), and
    settings are served "whatever lifetime a request carries, or none"
    (`internal/api/app_admission.go`). So: an exit commits in archive A and its answer is
    lost; such a caller switches to B; the closing app, still waiting, comes back to a
    daemon serving B, whose file has no record of its exit. The single-instance lock does
    not exclude that caller. Two rules close it, without reading or changing a file that is
    not served (ADR 0100 §4, Ownership):
    1. **A closing app never admits again** (a requirement on W-0027 A3). Once the operator
       has confirmed the exit, the shell stops answering a daemon restart with an admission
       (which it does today, `watchDaemon`) and only repeats its exit report. The daemon
       cannot make this distinction for it: an app that is still operating when an archive
       is switched under it must be admitted in B and given a session there.
       - **It is a barrier, not a flag on one path (codex-179).** The shell's admission can
         have work under way when closing begins (`desktop/src/admission.mjs`): an
         admission in progress, its read followed by its request, the loop that reads again
         after a stale answer, and markers kept meanwhile that start another ask when the
         work settles. So, from the confirmation: no new admission is launched; the loop
         and the kept markers start nothing; and the exit report is sent only after the
         work already issued has settled, because cancelling a request does not show the
         daemon did not apply it.
       - **What the daemon's order adds:** an admission of L and an exit report of L are
         decided under the same order (D), so whichever the daemon takes first, L ends
         retired holding no open session: admitted then exit, the exit closes what the
         admission opened; exit then admission, the admission is refused as retired.
       - **Tests (A3, with the scenario harness):** the confirmation placed at each of
         those points, and the A to B case; an old admission's answer arriving after
         closing began restores nothing and starts nothing; a confirmation cancelled before
         it is accepted leaves ordinary admission working.
    2. **The exit report is answered for a caller that is not current** (D, below), so the
       closing app gets its acknowledgement in B, is recorded there as exited, and never
       holds a session in B.
- **D. One exit protocol for a confirmed exit,** made of the gates that exist, in the order
  an archive switch uses (ADR 0100 §4).
  - **Who may report an exit, decided in this order, before anything is sealed, and under
    the one order below from the admission's lock** (not as a look taken before it: the
    fourth row's "holds no session" and its write of the exit must be ordered against an
    admission of L, or an admission could open a session between the two):
    1. **L's exit is already recorded in the served archive:** acknowledged as the
       committed result. Nothing changes and no scope is stopped, whoever is current now
       and whatever generation the report expected. This is the answer to a repeat after a
       lost answer.
    2. **L is the current lifetime:** the steps below.
    3. **L is not current, and the served archive has an open session held by L** (the
       daemon restarted before the exit committed, or another lifetime is at its recovery
       question): refused as not current, and nothing changes. The session stays open as an
       unconfirmed exit and the next start asks (ADR 0100 §3, an unacknowledged exit). The
       exit is not completed on behalf of a lifetime that was never admitted to this
       process.
       - **This is a terminal answer, with its own code,** distinct from an exit already
         committed (row 1, acknowledged) and from a failure that may still pass (the daemon
         not answering). A closing app does not repeat it in the hope of becoming current,
         since it never admits again; A3 shows it as a close that was not confirmed. No
         timeout and no made-up acknowledgement stands in for it.
       - **Its cost is the operator's to weigh (codex-179; question 9).** ADR 0100 does not
         require this refusal. The other policy: when no lifetime is current, the daemon
         completes the close from the file's own record of the holder, under the same
         archive, holder and generation checks and the same order, and never while another
         app is current.
    4. **L is not current, and the served archive has no open session held by L:** L's exit
       is recorded there and acknowledged. No session changes, the session generation does
       not move, and no scope is stopped. This covers the switch above, and a late report
       from a lifetime whose session a later one has already resolved.
  - **An equal session generation never authorises anything for a lifetime that is not
    current.** The admission's own generation and its retirement are separate from the
    session generation; rows 3 and 4 change no session whatever generation they carry.
  - **The steps, for row 2:**
  1. **Validate:** the archive is the served one and the generation is the expected one. A
     refusal here has sealed nothing.
  2. **Exclude new work:** under the admission's lock, seal and stop every scope by the
     replacement's own step (a no-op on a scope the app has already disarmed, and the
     backstop if it has not), then seal the live-entry gate, which waits for Phone/CW
     submits already admitted and refuses later ones with its own code.
  3. **Drain what was earned:** wait for the FT8 completion jobs already launched. The drain
     holds neither the session's serialization nor a database transaction, so an earned
     completion can finish. A completion launched before the seal does not seek admission
     again; its write finishes.
  4. **Commit, by what L holds,** in one transaction that also records the exit for C:
     - **L holds the open session:** close it (discarding it if empty) and advance the
       generation.
     - **L holds none:** close nothing. This is an exit while the recovery question is
       unanswered (the earlier lifetime's session stays open with its members, no interval
       end is written for it, and the next start asks again: ADR 0100 §3), an exit during
       setup pending, or an exit in the closing phase of a switch (the session is already
       closed: §4). The generation does not move, since no session changed.
  5. **Then, before any answer:** retire the lifetime in the admission, release the
     live-entry gate, publish, answer.
  - **On a failure or a cancellation before the commit of step 4:** release the gate,
    publish the lifetime to its scopes again, leave everything as it was, and answer the
    error. The exit is then unconfirmed unless the app retries.
  - **After that commit, nothing restores operation under the lifetime.** A cancellation or
    a lost answer is reconciled from the recorded exit: step 5 completes, or the next
    contact from L is answered "retired".
  - **No contact is refused or cancelled by this.** A submit admitted before the seal
    commits into the session being closed. A completion job is never cancelled; its write is
    not bound by the job's own time limit today (`context.WithoutCancel`), so the drain's
    bound is the database's own limits, and no exit timeout is invented here.
  - **A finished job is not a stored contact.** The tests look at the rows: the QSO, its
    upload-queue rows and its membership in the closed session. A completion whose write
    failed is not stored, as today, and does not block the close; that case has its own test.
- **One order for every transition, to be fixed in part 2 before A is wired:** the
  admission's lock; then a scope's own gate; then the live-entry gate; then the session's
  serialization; then the database transaction. A replacement, an exit, New, Continue, an
  archive switch and a default change each start at the admission's lock and take the rest
  in that order, so any two are ordered and none interleaves. Logging takes only the last
  three, in that order. Membership uses the QSO's own transaction, handed to it as the
  upload-queue write is.
- **E. The recovery question is a screen of the main window's page,** shown in place of the
  workspace, in the UI's own style. The shell is not involved; a detached window (A2) never
  asks.
- **F. No fallback to a closed session.** ADR 0100 left to this brief whether the recovery
  question and the closing phase need "show the latest closed session, labelled Closed". I
  propose not: during the question no workspace is shown, and the closing phase ends in a
  reload. Both show their own state and no rows. A failed read is shown as a failure, never
  as an empty session.
- **G. Plain SQL for the new tables, without regenerating the models.** ADR 0100's
  Consequences names a sqlboiler regeneration. The tree has both precedents (above). I
  propose plain SQL as `contest_serial.go` does, because the session's writes are a handful
  of statements inside transactions others own. This departs from the ADR's wording and is
  recorded as a deliberate deviation if accepted; no generated file is touched either way.
- **H. Wire names.** Routes under `/v1/operating-session`; events `operating_session.*`;
  the close reasons as ADR 0100 lists them. Fixed in part 2 with the API reference.

**Evidence plan.** No transmission, keyed test or rig command is part of building this.
- Each part is built test-first with reversion proofs in temporary copies. Behaviour is
  asserted where the operator would see it: rows stored, states read back, events published.
- Part 1: every transition and every refusal; the generation never reset; the X to Y to X
  default change; and the concurrent control-and-logging test, on a database file with
  competing connections, since one connection cannot show contention.
- Part 2: each row of A's table, twice over; the exit protocol's steps and each failure
  path; a Phone/CW submit and an FT8 completion crossing the seal; a lost answer followed by
  a daemon restart and the same app returning; a late exit report from a replaced lifetime;
  over the real listeners. There is no "check off" case: that state is removed.
  - **Exit during recovery** (codex-177): a remnant with members, a new app admitted, closed
    without an answer, restarted: the same remnant is offered again, with no interval end
    written for it. The same with the exit's answer lost and the exit repeated.
  - **A first run:** a fresh configuration and archive reach setup, complete it, and have
    exactly one session, for the actual default, held by that lifetime; retries and a daemon
    restart at each point give no second session and no recovery question; an unreadable
    database is a failed admission, not "setup pending".
  - **A retired lifetime returning** after a lost answer and a daemon restart, with no
    session open and with a later lifetime's session open: refused as retired both times,
    and in the second the later session, its holder and its scopes are untouched.
  - **Each row of "who may report an exit":** a repeat after a lost answer, with the same
    app, with a later holder present, and during a recovery question; an exit that had not
    committed when the daemon restarted (refused with its terminal code, the session
    offered at the next start; or completed, if question 9 is ruled the other way);
    an admission of L and an exit report of L sent together, in both orders;
    and the switch case: an exit committed in A, the answer lost, an activation to B by a
    caller with no lifetime, the restart, the report repeated: acknowledged, recorded in B,
    no session opened in B, and an admission by that lifetime afterwards refused as retired.
  - **The default-change boundary** at the start step: Y to Y leaves L's session current;
    X to Y and X to Y to X close it before the step, which then opens a new one.
- Part 3: the ADR's own list for a switch (§4).
- Part 5: the atomic-write invariant restated for the new write: a failed association rolls
  the QSO back, and no session state can refuse or delay a live contact. The exclusions:
  imports, restores, a submit naming another logbook.
- Parts 4 and 6 to 9: the SPA's tests, and the desktop scenario harness for what only
  Electron shows (the question before the workspace; two windows agreeing once A2 exists).
- The station: the operator's own acceptance from parts 5 and 6 onward, on the installed
  pair.

**What this brief does not settle.** The schema's exact columns and indexes (part 1). The
serialization mechanism (part 1, by measurement; log transactions begin deferred today, so
"read, then write" can fail under contention unless something orders them). The wording of
the question, the notice and the controls (parts 4 and 6, put to the operator then). How
long an FT8 completion takes to store, which ADR 0100 records as unmeasured (part 3).

**Questions for the operator. All ruled 2026-11-19 ("Accepted"; Rulings, above): each as
proposed here, and question 9 as (a).**
1. **The order and what is switched on when** (the table above): parts 1 to 4 and A3 first,
   each on at the station. Parts 1, 2 and 4 show nothing; part 3 shows one thing already
   ruled (a submit during a switch is refused); A3 shows its exit confirmation. Then
   membership and the panel together.
2. **Withdrawn (operator, 2026-11-18).** This asked what a session should do with the
   operating-client check off. The operator directed that the temporary switch be removed
   first (Rulings, above), so there is no off state to design for.
3. **Proposal A:** the session's start at the admission, not as a request of its own.
4. **Proposal F:** no fallback to a closed session during the question and the closing phase.
5. **Proposal G:** plain SQL, although the ADR's text says regeneration.
6. **The rank.** W-0029 stands second in the backlog's P2 list, under W-0027, because two of
   W-0027's slices wait on it. That position is my proposal and the backlog says so. The
   backlog alone owns priority; move it if that is wrong.
7. **Setup pending** (proposal A, from codex-177): on a first run the app is admitted with
   no session, and the save that creates the default logbook opens the first one. This adds
   a third no-session state to ADR 0100, recorded there as a dated update if accepted.
8. **The downgrade tooling's database half before part 1** (the return section below), so
   migration 0020 is written upward only.
9. **A daemon restart that lands inside an exit** (D, the third row): you confirm the
   close, and the daemon restarts before the exit is stored. Either (a) the app closes
   saying the close was not confirmed, and the next start asks Continue or New; or (b) the
   daemon, when the closing app's report arrives after the restart and no other app is
   current, completes the close from its own record of who held the session, and the next
   start asks nothing. (a) is what the brief proposes: one rule, only an admitted app
   changes a session, and it is ADR 0100's outcome for an exit the daemon did not
   acknowledge. (b) spares a question at the cost of one more path that closes a session.
   Codex selects neither.

### The return to the brief (2026-11-19; nothing ruled)

On the operator's direction ("Let's get started on sessions"). The brief above stands as
drafted; this is what has changed around it, read from the tree the same day.

- **The switch is gone.** The operating-client check is permanent (W-0027 C2e, config
  version 7, accepted at the station). In "What exists today" the sentence "With the
  operating-client check off there is no lifetime at all" no longer describes anything: an
  admitted app always has a lifetime. Question 2 stays withdrawn.
- **The rest of "What exists today" still holds,** checked at the places it names: the three
  per-window stores and their keys; no session table, column or route in `smd`; the two
  `/v1/session/` routes taking UUID lists; the logging transaction's calls; `launchFt8QsoLog`
  and `requireLiveSubmit`; the log schema's head at 19; the lifetime "a label, not a
  secret"; the SPA forwarding only the three `qso.*` events; the shell sending nothing at
  quit.
- **Codex has not reviewed the revised brief.** Its first review (codex-146) led to the
  revision; codex-147 then confirmed one clarification and said "I have not completed a
  second approval review of the parked session protocol; the other revisions remain for the
  return to W-0029". That review is the next step before any ruling is acted on.
- **Part 1 meets the downgrade tooling (found on this return; a question for the operator).**
  Part 1 adds log migration 0020. Every log migration to date carries a `.down.sql` and a
  test of its down step (`TestMigrate0019_DownDropsTheCounter`), and two earlier ones needed
  refusal rules in `DowngradeLogSchemaTo` for rows a down step would erase. That tooling is
  ruled for removal (W-0027, 2026-11-18, option (a)), not yet removed. Built to today's
  convention, part 1 would write a down step for sessions, test it, and decide what a
  downgrade does with membership rows, all for tooling that is going. The operator's ruling
  of 2026-11-18 above names this kind of waste. **Proposed by Claude, not ruled:** the
  brief's review goes ahead now (documents only), and the removal of the database half of
  the downgrade tooling is built before part 1, so that migration 0020 is written once,
  upward only. The alternative is to write 0020 without a down step while the tooling still
  exists, which by my reading (not run) leaves `smd db-downgrade` unable to step below 20
  until it is removed.
  **That reading was wrong (codex-177; repeated by Claude the same day).** The pinned
  library treats a missing down file as an empty step. With a synthetic 0020 that creates a
  table and has no down file: a step down to 19 returns no error, records version 19 clean
  and leaves the table; the next step up fails with "table ... already exists" and leaves
  the file at version 20, dirty. So the alternative is not a safe half-measure: while the
  tooling exists, a 0020 without a down step lets a downgrade damage a file. The tests'
  24 comparisons with head 19, in 13 files, also move with any new head. Codex
  recommends the removal first; so does Claude. Not ruled.
- **The operator asked (2026-11-19) whether removing the downgrade tooling means folding the
  up scripts into `0001_init.up.sql`, and then: "why not fold all the up scripts together
  since I am the only install - a new install doesn't need all the up scripts?"** Not ruled.
  The removal takes the way down only; the 19 up files stay. On the fold, the operator's
  reasoning holds for a fresh install, which needs only the final schema. What was measured
  for the answer, read-only at the station, versions only and no contents: the live log
  database is at 19; **one file under `qso-archives` is at log version 18**; two dated
  directories of July hold log files at versions 5 and 6. So this one install holds log
  files below the head, and the up steps are what bring such a file to the head when it is
  opened or restored (by my reading of `applyMigrationSetTo`; not run against those files).
  A folded set has no step from 18, 6 or 5. In the tree, about a dozen test files in
  `internal/database/sqlite` drive particular steps. **Claude's recommendation, not ruled:**
  do not fold; remove the way down only.
- **The archive-open path, traced on the operator's direction (2026-11-19: "Trace the
  archive-open path").** This corrects the line above: `applyMigrationSetTo` is not the
  path; it only brings the reference set up during the old single-file split. The path is:
  1. `Activate` (`internal/archive/activate.go:160`) does not open the file for writing. It
     checks the identity through a read-only connection (`VerifyIdentity`,
     `PeekArchiveIdentity`), seals transmit admission, writes `PendingQsoArchiveID` to the
     config and asks for the restart.
  2. The next start resolves the pending candidate (`archive.ResolveEffective`,
     `cmd/smd/main.go:1191`), checks the identity again, opens the file and runs
     `d.db.Migrate()` with the log set only (`cmd/smd/lifecycle_adapters.go:306-318`), which
     is `doMigrations` and the library's `Up` to the head
     (`internal/database/sqlite/migrations.go:78-131`).
  3. If that fails for a candidate, `startGenerations` (`cmd/smd/main.go:1150`) records the
     failure on the candidate, clears the pending selection and starts once more on the
     last-known-good archive. A start that is not a candidate's has no second attempt.
  4. The commands that name an archive (`openArchiveDatabases`,
     `cmd/smd/archive_paths.go:47`) open and migrate the same way.
  So an inactive archive stays at its version until it is activated or a command names it;
  until then it is read only through read-only peeks and the summaries file.
- **Measured the same day,** with a throwaway test added by build overlay (nothing in the
  tree), on files built from our own migrations in a temporary directory, not the
  station's files, with the pinned library (v4.19.1):

  | Migration set | File at | Result |
  |---|---|---|
  | today's (up and down files) | 18 | 19, no error |
  | up files only (the removal) | 18 | 19, no error |
  | up files only (the removal) | 5 | 19, no error |
  | folded (one file, version 1) | 18 | stays 18: "no migration found for version 18" |
  | folded (one file, version 1) | 19 | stays 19: "no migration found for version 19" |

  With a folded set, by step 3, activating the archive at 18 would fail and the daemon
  would come back on the previous archive; and by the last row the live file at 19 would
  stop the daemon starting at all until its version record was rewritten by hand. The
  last sentence is the trace and the measurement together, not a run of `smd`.
  **Corrected (codex-178):** "would come back on the previous archive" promises too much.
  Step 3 only attempts the previous archive; under the same folded set that file, at 19, is
  refused too, and `startGenerations` returns that second failure. So under a fold, an
  activation of the archive at 18 would leave no daemon running at all. Codex also notes
  that a folded baseline could keep the current version number instead of 1, and that
  rewriting a version record is not a schema upgrade: older files would still need an
  explicit way up. Neither changes the recommendation, which Codex shares: keep the up
  files, remove the way down, and treat a fold as a separate change. Not ruled.
