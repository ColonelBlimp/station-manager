# W-0029 — Daemon-owned operating sessions: building ADR 0100

**Status:** Opened 2026-11-18 (operator: "W-0029 is fine, open the dossier"); ranked in
[`docs/backlog.md`](../backlog.md). The build brief was drafted (operator, the same day:
"start the ADR 0100 brief"), reviewed by Codex in five rounds, and **its questions are ruled
(operator, 2026-11-19: "Accepted")**. Part 1's first slice, the migration and the default epoch, is committed (`62218133`,
2026-11-20), and so is its second, the transitions, the state read and the contention tests
(`6f2b4e27`), with two fixes from its reviews (`93a743f8`). **Part 1 is complete.** Built
and committed with this text: a cheaper fixture for the session tests. **Part 2's acceptance
criteria are at the end of this file; their six questions are ruled (2026-11-20) and Codex
has no remaining objection to them (codex-194). Nothing of part 2 is built.** The removal of the database half of
the downgrade tooling, which part 1 waited for, is committed (`0b21b2fb`, 2026-11-20; W-0027).
**Part 1's acceptance criteria are written, their three questions ruled, and agreed with
Codex after one round of corrections (2026-11-20).** The
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
- **2026-11-20 (operator): "write part 1's acceptance criteria".** The criteria are directed,
  for review by the operator and Codex. No build is.
- **2026-11-20 (operator): "1. recommendation accepted 2. Yes 3. Yes But, let's wait for
  Codex - consensus is a go".** Said of the three questions at the end of part 1's
  acceptance criteria:
  1. **Durable writes:** every session control (Open, New, Continue, the exit, the close at
     a switch) is written the durable way; the logging step stays as durable as its QSO.
  2. **The epoch counts from part 1,** so part 1 changes the three existing writers of the
     default pointer.
  3. **The interval-end table** in group D, as proposed.
  Claude's reading of the last sentence, not the operator's words: part 1's build waits for
  Codex's review of the criteria; once Codex and Claude agree on them, with every finding
  fixed, the build may start without a further direction. A disagreement, or a finding that
  changes one of these three rulings, comes back to the operator. The commit is still the
  operator's to direct.
- **2026-11-20 (operator): "make the fixture change, then write part 2's criteria".** Both
  are directed. No build of part 2 is.
- **2026-11-20 (operator): "All six accepted, consensus is a go, commit and push".** Said
  after Claude listed, in chat, the six questions at the end of part 2's acceptance
  criteria with a recommendation for each. Each is ruled as recommended:
  1. **The file cannot be read when the app starts:** the app is not admitted and stays on
     its waiting page, FT8 left stopped, until an admission succeeds.
  2. **The wait for unfinished FT8 logging at app start** is built with part 5, before
     contacts join sessions, not in part 2.
  3. **One event,** `operating_session.changed`, carrying the archive and the new
     generation; windows read the state again.
  4. **The default-epoch test at daemon start** is added with part 3, not in part 2.
  5. **Setup's first save fails if it cannot open the session;** the save is made again.
     Recording the default logbook in the archive file is a requirement of that save.
  6. **An exit or an app start that meets a default change or an archive switch in
     progress** is ordered against it, not refused, and leaves its seals alone. Two archive
     operations still refuse each other; a daemon restart before the exit is stored still
     leaves the close unconfirmed.
  - **"Consensus is a go"** is now said of part 2 as it was of part 1. Claude's reading,
    as before and not the operator's words: the build of part 2 may start once Codex and
    Claude agree on the criteria as ruled, with every finding fixed; a disagreement, or a
    finding that would change one of these six, comes back to the operator; commits stay
    the operator's to direct.
  - **"Commit and push"** is of the session tests' fixture, the two test fixes and this
    dossier.

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

### Part 1: acceptance criteria (2026-11-20; its questions ruled; revised after Codex's review; nothing built)

Codex's review (codex-184) found six things, each checked in the tree and taken: a refusal
and the default-epoch test's own close had to be told apart (C, D); F needed forced
orderings and the contact's destination, not concurrent calls and counts; the schema lets
through two states only the transitions can exclude, so D asserts them; the interval table's
less obvious cases are written out; B's scope is held to what each pointer writer refuses
today; and the local context gate failed on a stale capsule date (`docs/current.md`,
reconciled the same day).

On the operator's direction ("write part 1's acceptance criteria"). Read against ADR 0100
(§1 to §5, §8 and its updates), the brief above and the tree at `0b21b2fb`. Where this
section chooses a shape or a rule the ADR and the brief leave open, it says "proposed". The
questions at the end are the operator's.

**What part 1 is.** Log migration 0020, and the functions that change the session record.
Each function is one transaction; the one for logging is a step inside the QSO's own
transaction. Nothing in the daemon calls them, with the one exception in group B.

**What the operator sees.** Nothing new. After the install the daemon starts, the live file
and any archive file opened later come up to schema 20 with every existing row unchanged,
and logging, FT8, a default change and an archive switch behave as before. No session
exists at the station: part 1 writes no session row there.

**Nearest confusable outcome.** Each function passes its own test on one connection, while
two of them interleaved on a real file fail with "database is locked", leave two open
sessions, or hand out a generation twice. So every criterion is judged on stored rows read
back, and group F on a database file with competing connections.

**A. The migration.**
1. A file at 19 that holds logbooks, QSOs, upload rows, destination bindings and contest
   serials comes up to 20 with all of those rows unchanged; the version reads 20 and is not
   dirty. Files at 5, 14 and 18 do the same (the existing older-files test, at its new head).
2. After 0020 a file holds no session, interval, member or recorded exit; the session
   generation is 0 and the default epoch is 0. Existing contacts are in no session (ADR 0100
   O6: no backfill).
3. 0020 is an up file only (the up-only test already refuses anything else).
4. A file built from version 1 and a file brought up from 19 have the same session tables.
5. The stored record cannot hold, whatever code writes it: two open sessions in one file;
   two open intervals in one session; a closed session without a reason, or a reason
   without a closing time; a confirmed interval end without a time; a member row for a QSO
   or a session that does not exist; one QSO in two sessions.
6. The 24 comparisons with head 19 in 13 test files move to 20, and the whole tree's tests
   pass (a head change also reaches `internal/api`'s version test).

**B. The default epoch.** The one thing here that existing code runs: the two setters of
the file's default pointer (`SetArchiveDefaultLogbookWithContext` and its durable twin,
`internal/database/sqlite/archive_identity.go`).
1. Every write by a setter that moves the pointer to a different logbook advances the
   epoch by one in the same commit: the default change applied at start (X to Y), setup's
   first save (none to X), and archive adoption.
   - **Creating the identity row is not a change.** `EnsureArchiveIdentityWithContext` and
     `WriteArchiveIdentityWithContext` insert the row, with or without a pointer; the epoch
     starts at 0 either way. A later move from none to a logbook is a change and counts.
2. A rewrite to the logbook already selected (Y to Y) leaves the epoch as it was.
3. A write a setter refuses today changes neither value, and part 1 changes nothing about
   what each refuses: the ordinary setter refuses a missing identity row and a logbook that
   does not exist; the durable one also refuses a deleted logbook (codex-184: the two
   differ, and the epoch work does not align them).
4. X to Y to X moves the epoch twice.
5. No stored state exists in which the pointer moved and the epoch did not. Shown by
   reading both after each writer, and by a reversion proof with the advance removed.

**C. The generation.**
1. It is greater after every committed transaction that changes session state: an open, a
   New, a Continue, each close, each discard.
2. It is unchanged by a transaction that changes no session state: a read, a QSO joining
   an open session, an exit recorded for a lifetime that holds no session, and a refusal
   whose default-epoch test found nothing to close.
   - **A refused control can still advance it** (codex-184; ADR 0100 §5): when the
     default-epoch test closed a session in that transaction, the close is committed and
     the generation advances once, although the control itself is refused. The refused
     control contributes nothing of its own.
3. It never returns to an earlier value, including after a discard removes a session's
   row.
4. Proposed: exactly one advance for each such transaction, however many steps it takes (a
   New closes and opens; a logging step may close and open). ADR 0100 §5's example reads
   this way (New at g gives g+1). Callers only compare for equality.
5. Every state a function returns carries the generation it was committed at.

**D. The transitions.** Each is all stored or nothing stored. Each takes its instant from
the caller, as UTC; none reads the clock itself.

- **After every completed transition,** read back: an open session has exactly one open
  interval, and a closed session has none; no interval or member of another session has
  changed. The schema alone does not exclude an open session with no interval or a closed
  one with an open interval (Codex's probe, repeated here on SQLite 3.51.2), so the
  transitions are what is tested.
- **The order inside each.**
  - New, Continue and the exit of a current lifetime: the default-epoch test, then the
    stale check, then the action.
  - Open: is L's exit recorded, first; only for a lifetime that is not retired, the
    default-epoch test; then the table below. So "held by L, nothing changes" can never
    leave a session of an earlier default open.
  - An exit already recorded, and the exit of a lifetime that is not current: no
    default-epoch test. They touch no session, whoever holds it.
- **The default-epoch test.** An open session whose epoch differs from the file's is closed
  with `logbook_changed`, whoever holds it. Its effect is the test's, not the control's: it
  is committed even when the control is then refused (ADR 0100 §5: a refusal is not a
  rollback).
- **The stale check.** A control naming a generation other than the current one is refused
  with its own code and returns the current state with its generation. The control writes
  nothing of its own: no replacement session, no interval, no recorded exit.
  - **The test for the two together:** a session at an old epoch, the file's epoch moved, a
    control carrying the old generation. Stored after: the session closed with
    `logbook_changed` (or removed, if it had no member row); the generation advanced once;
    the control refused; nothing the control asked for. The same press repeated adds
    nothing. A refusal with no such close changes nothing at all.
- **Open, for a lifetime L** (proposal A's table):

  | The file holds | Stored after |
  |---|---|
  | L's exit recorded | nothing changes; the answer is "retired" |
  | no default logbook | nothing changes; the answer is "setup pending" |
  | an open session held by L | nothing changes |
  | an open session held by another lifetime, with any member row | nothing changes; the answer is "recovery pending"; L holds nothing |
  | an open session held by another lifetime, with no member row | that session and its intervals are removed and a new one is open, held by L, in one commit |
  | no open session | a new session: a UUIDv7 identity, the default logbook's UUID, the current epoch, the given instant, and one open interval held by L |

  Run twice, it leaves what running it once leaves. A file that cannot be read is an
  error, told apart from "setup pending".
- **New, for L at generation g.**
  - L holds the open session: it is closed with `new_session` and a new one is open, held
    by L.
  - Another lifetime holds it (the recovery answer New): it is closed with
    `unconfirmed_exit` and a new one is open, held by L.
- **Continue, for L at generation g, naming a session.**
  - The named session is the open one held by another lifetime (the recovery answer
    Continue): its identity and members are kept, its interrupted interval gets an
    unconfirmed end, and a new interval held by L is open.
  - The named session is closed, in this file, bound to the current default logbook, and
    not the current one: the current session is closed with `continued_other`; the named
    one is open again with a new interval held by L and the file's current epoch; its
    identity, its earlier intervals and its earlier members are kept as they were. Anything else named is refused with its own
    code and changes nothing; a failure leaves the current session open.
  - ADR 0100 §8's own acceptance: continue S0 after X to Y to X, then run the logging
    step: S0 stays open and receives the contact.
  - Proposed: a closed session named while a remnant held by another lifetime is open is
    refused. The question is answered first.
- **Close for an archive switch** (at the request, and at an arrival): the open session,
  whoever holds it, is closed with `archive_switched`. With none open, nothing changes.
- **The exit of L,** given by the caller whether L is the current lifetime (brief, D):

  | Case | Stored after |
  |---|---|
  | L's exit is already recorded | nothing changes, whatever generation is named; the answer is "already recorded" |
  | L is current, the generation is stale | the exit is not recorded and no session changes by the exit |
  | L is current and holds the open session | one commit: the exit recorded, the session closed with `app_closed`, the generation advanced |
  | L is current and holds none | the exit recorded; no session changes by the exit; the generation does not move by it |
  | L is not current and holds the open session | refused with its own code; nothing changes; the exit is not recorded |
  | L is not current and holds none | the exit recorded; nothing else changes, whatever generation is named |

  In the three rows for a current L the default-epoch test has run first. What it closed,
  if anything, is its own effect, stored and counted as above; the row states the exit's.
  If the test closed L's own session, the generation has moved and the exit is refused as
  stale, by the second row. An exit of L and an open for L, in either order, end with L
  recorded as exited and holding no open session.
- **The logging step,** on the QSO's own transaction, after the QSO row is written:
  the default-epoch test; then, if no session is open, a new one bound to the default
  logbook and the current epoch; then the member row.
  - It takes no generation and no policy refuses it. With a remnant awaiting its answer,
    the contact joins the remnant.
  - The session that receives the contact is bound to the QSO's own logbook, asserted on
    the rows. Proposed: given a QSO whose logbook is not the file's default, the step
    writes nothing and changes no session (ADR 0100 §2: such a submit never joins).
  - If the QSO's transaction rolls back, nothing of the step is stored: no member row, no
    session opened, no close, no generation moved.
  - If a statement of the step fails, the step reports it and the caller's rollback
    removes the QSO too (ADR 0100 §2: the existing reason not to log).
- **Discarding.** Any close of a session with no member row removes the session and its
  intervals instead of keeping them. A session whose only members are deleted QSOs is
  kept.
- **Interval ends** (proposed; ADR 0100 says it for the exit and the two recovery answers
  only):

  | Close | The open interval's end |
  |---|---|
  | `app_closed`, `new_session`, `continued_other` | confirmed, at the closing instant |
  | `logbook_changed` | confirmed, at the recorded closing time (as §4 says of `closed_at`) |
  | `archive_switched` at the request | confirmed, at the closing instant |
  | `archive_switched` at an arrival | unconfirmed, no time: an open session found there is what a crash left |
  | `unconfirmed_exit`, and the interval a recovery Continue interrupts | unconfirmed, no time |

  - **Ruled 2026-11-20, and what follows from it (codex-184).** The reason decides, not the
    holder. So a switch request, or the default-epoch test, that meets a remnant an earlier
    crashed app left also ends its interval as confirmed, at the closing instant. That time
    is when the daemon closed it, not when that app stopped; no crash time is recorded
    anywhere. This qualifies ADR 0100 §1's "a crash leaves the open interval's end
    unconfirmed", which holds for the arrival and the two recovery answers. Both cases are
    tested. Neither can be reached from the app while the recovery question is showing; a
    caller with no lifetime, or a default change already pending at a start, can.

**E. One read.** The state: the generation, the epoch, the open session if any (identity,
logbook, opening time, the open interval's start and holder, whether it has any member
row), and whether a named lifetime's exit is recorded. It is one snapshot: every part
belongs to the generation it is returned with, shown by reading while transitions commit
on another connection. The list for Continue and the member rows are part 2's and part
6's.

**F. Contention** (ADR 0100 §2 requires the test). On a database file in a temporary
directory, with the service's own connection settings. Calls that merely run at the same
time can pass by running one after another, so each case below is forced: a barrier the
test can see holds one transaction at a known point on its own physical connection while
another, on a different connection, is started against it.
1. **Logging first.** Logging holds the writer's position (its QSO row written, not
   committed) when a New starts. The New waits, then proceeds. Stored after: the contact in
   the session the New closed, which is therefore kept; the new session empty.
2. **The control first.** A New holds the writer's position when logging starts. Logging
   waits, then proceeds. Stored after: the contact in the new session.
3. **A cancelled waiter.** A transaction waiting behind a holder is cancelled: it stores
   nothing, the holder commits, and a third transaction then proceeds.
4. **A pointer change crossing a session read:** against a control and against logging, in
   both orders. The epoch test sees the change or does not, by the order, and never half
   of it.
5. **An exit of L against an open for L,** in both orders: exit then open gives "retired";
   open then exit closes what the open made. Each answer is told apart from a database
   failure.
6. **In every case, read back:** which session holds the contact, that session's logbook,
   its interval and holder, the generation, and the QSO with its upload rows. No case ends
   in a busy, locked or snapshot error; a reader never finds two open sessions.
7. **The mechanism is chosen by measurement,** and the numbers are recorded here. The
   logging transaction already writes before it reads, for this reason
   (`internal/database/sqlite/api_context.go`, `InsertQsoTx`). `durableTx` gives the
   stronger commit, not the ordering: it begins as any transaction does.
8. **The reversion proof forces the unsafe order:** a control made to read the mutable
   state first, a competing write committed after that read, then the control's write. It
   must reach the snapshot failure; taking the mechanism out without forcing that order
   proves nothing. `-race` is run too, and is not evidence for this.
9. A run of many mixed transactions without barriers is kept as a supplement: every QSO
   has exactly one member row, and the generation ends at its start plus the number of
   state-changing commits.
10. No new time limit: a waiter waits within the existing busy and transaction limits.

**G. What part 1 must not do.** No route, no event, no start node; no call from the
admission, the logging paths, the archive switch or an exit. No generated model changes
(plain SQL, ruled). No session row written at the station. Shown in review by a search for
callers of the new functions, not by a guard in the tree.

**Evidence.** Each criterion test-first, seen to fail on its own assertion. Reversion
proofs in temporary copies: the epoch advance removed; the stale check removed; the
discard inverted; the one-open-session constraint removed; the retired check removed; the
contention mechanism removed. The usual gates. Nothing at the station, and no
transmission, keyed test or rig command.

**Proposed schema** (for review; the names are proposals):

```sql
ALTER TABLE archive_metadata ADD COLUMN session_generation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE archive_metadata ADD COLUMN default_epoch      INTEGER NOT NULL DEFAULT 0;

CREATE TABLE operating_session (
    id            TEXT     NOT NULL PRIMARY KEY CHECK (length(id) = 36),
    logbook_uuid  TEXT     NOT NULL,
    default_epoch INTEGER  NOT NULL,
    opened_at     DATETIME NOT NULL,
    closed_at     DATETIME,
    close_reason  TEXT,
    CHECK ((closed_at IS NULL) = (close_reason IS NULL))
);
CREATE UNIQUE INDEX idx_operating_session_one_open
    ON operating_session ((closed_at IS NULL)) WHERE closed_at IS NULL;

CREATE TABLE operating_session_interval (
    session_id   TEXT     NOT NULL REFERENCES operating_session (id) ON DELETE CASCADE,
    seq          INTEGER  NOT NULL,
    started_at   DATETIME NOT NULL,
    ended_at     DATETIME,
    end_state    TEXT     NOT NULL CHECK (end_state IN ('open', 'confirmed', 'unconfirmed')),
    app_lifetime TEXT,
    PRIMARY KEY (session_id, seq),
    CHECK ((end_state = 'confirmed') = (ended_at IS NOT NULL))
);
CREATE UNIQUE INDEX idx_operating_session_interval_one_open
    ON operating_session_interval (session_id) WHERE end_state = 'open';

CREATE TABLE operating_session_member (
    qso_id     INTEGER NOT NULL PRIMARY KEY REFERENCES qso (id),
    session_id TEXT    NOT NULL REFERENCES operating_session (id)
);
CREATE INDEX idx_operating_session_member_session ON operating_session_member (session_id);

CREATE TABLE operating_app_exit (
    app_lifetime TEXT     NOT NULL PRIMARY KEY,
    exited_at    DATETIME NOT NULL
);
```

- The generation and the epoch sit on the file's one identity row, beside the pointer the
  epoch counts.
- The close reasons are checked in Go, not by a `CHECK`: a later reason (ADR 0100 names
  `archive_restored` for the restore design) would otherwise need the table rebuilt.
- QSOs are never removed from the file by the daemon (deletion sets `deleted_at`; read in
  `internal/database/sqlite/api_context.go`), so a member row's reference to its QSO holds.
- A recorded exit is one small row for each start of the app, and nothing removes them.

**Left to later parts, named so they are not lost.**
- Who holds a session the logging step opens when no app is current (part 5). In part 1
  the caller passes the holder, which may be none.
- The wait for completion work before a remnant is judged empty (part 2; it is not a
  database matter).
- The list for Continue and its ordering (part 2).
- What a closing app does when its exit report is refused as stale because the
  default-epoch test closed its session in that transaction (part 2 and W-0027 A3). It can
  follow only a default change applied at a daemon start.

**Questions for the operator. All three ruled 2026-11-20 as recommended (Rulings, above).**
1. **Which session writes must survive a power cut once reported.** The file runs in a
   mode where a committed write can be lost by a power cut. ADR 0100 §3 says a committed
   clean close "is never reopened". I recommend: every control (Open, New, Continue, the
   exit, the close at a switch) is written the durable way the tree already has
   (`durableTx`), at the cost of one disk sync for each; the logging step stays as durable
   as the QSO it rides with. Each loss the other way fails towards asking Continue or New,
   not towards losing a contact.
2. **The epoch counts from part 1.** That changes the three existing writers of the
   default pointer, so part 1 is not strictly "called by nothing". I recommend it: an
   epoch that missed a change cannot be repaired afterwards. The other choice is part 2.
3. **The interval-end table in D,** where ADR 0100 is silent: a switch and a default
   change end the sitting's interval as confirmed; an arrival that finds a session still
   open ends it as unconfirmed.

### Part 1, first slice: the migration and the default epoch (2026-11-20; committed `62218133`)

**The go-ahead.** Codex agreed the revised criteria (codex-185: "The criteria are agreed for
the build"), which was the condition of the operator's direction of the same day. Codex
added one detail to the guard on a QSO outside the default logbook, taken for the slice
that builds the logging step: the QSO's logbook is checked before any session change, in
the QSO's own transaction after its row is written; a file with no session and a file
whose session needs the default-epoch test are both tested, and the QSO and its upload
rows are read back as stored.

**Part 1 is built in slices, each for review.** This one is groups A and B. The
transitions, the read and the contention tests (C to F) are the next; they need each other,
since the measurement in F chooses how the transitions begin.

**Built.**
- `migrations/log/0020_operating_session.up.sql`: the proposed schema, unchanged.
- The two pointer setters share one statement (`setDefaultPointerSQL`,
  `internal/database/sqlite/archive_identity.go`): it advances the epoch when the pointer
  names a different logbook than before and writes the pointer, together. What each setter
  refuses is as it was.
- The comparisons with head 19 moved to 20: 14 in the `sqlite` package's tests and the
  version route's in `internal/api`. (This file said 24 in 13 files; that count took each
  comparison and its message as two.)

**Seen, in order.**
- The new tests first, before the migration existed: every one failed, the schema tests on
  "no such table" where they want a constraint failure, which their own assertion tells
  apart. Two fixture mistakes of mine showed here and were fixed: a notification kind the
  schema does not allow, and two in-memory services of one test sharing one database.
- With the migration and no epoch code: the migration tests passed and the epoch tests
  failed on their own assertion ("epoch 0; want 1").
- With the statement: all pass. The whole tree then failed in one place, the version
  route's test, on its own assertion, as the criteria said it would.
- **Reversion proofs,** in a temporary copy since removed, each restored and compared:
  the advance taken out (epoch 0, want 1); every write counted (a rewrite gives 3, want 2);
  the one-open-session index taken out (a second open session accepted); the migration
  made to change a QSO row (the row comparison fails).
- **Gates, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0 issues), the
  maintainability metrics, `go test ./... -count=1` (66 packages ok), the documentation
  check, the context gate, `git diff --check`.

**After Codex's review of the slice (codex-186: no blocking finding).** It asked for three
things, all added the same day:
- **The two paths B1 names are now tested where they run,** reading the stored epoch
  through a read-only connection to the test's own file, as the command tests already do.
  Setup's first save moves the pointer from none to the new logbook and the epoch reads 1; a
  second save leaves it (`internal/api/handler_config_default_epoch_test.go`). Adoption of
  a file with no pointer reads 1 after the first start and after a second; a file that has
  its pointer reads 0 (`cmd/smd/archive_adopt_test.go`). My earlier reason for leaving
  them, that nothing outside the package could read the epoch, was wrong.
- The zero start is asserted through `WriteArchiveIdentityWithContext` too, with and
  without a pointer.
- The refusal with no identity row is asserted for both setters.
- **Reversion proof for the two new path tests,** in a second temporary copy, since
  removed: with the advance taken out, both fail on "epoch 0; want ... 1".
- The gates were run again afterwards: the same eight, all by exit code, 66 packages ok.

**Not shown.** Nothing was run at the station.

### Part 1, second slice: the transitions, the read and the contention tests (2026-11-20; committed `6f2b4e27`)

Groups C to F of the criteria, built after the first slice was committed, under the same
direction. Nothing in the daemon calls any of it (a search finds no caller outside the
file and its tests), so at the station it changes nothing.

**Built.** `internal/database/sqlite/operating_session.go`: the state read; Open, New,
Continue, the close for a switch and the exit, each one durable transaction that takes the
writer's position with its first statement; and the logging step on the QSO's own
transaction. A refusal is an outcome with the current state, never an error; an error is a
database failure and nothing was stored. Each takes its instant from the caller, the
session's identity included.

**How the transitions are ordered (F7, measured).** A control's first statement is a write
that changes nothing (`takeWriterPosition`), as the logging transaction's is its QSO row.
The measurement is a test in the tree, on two connections of one file, each step following
from the one before:
- reading the state first, then writing after another connection has committed: refused
  with "database is locked (517)", SQLite's stale-snapshot refusal;
- taking the writer's position first: the other connection's write is refused at once
  with a busy limit of zero, refused again with a limit of 300 ms (after 301 ms in one run
  and 182 ms in another; the time is logged and not asserted), and goes through once the
  holder has committed. Both writes are counted.

**The forced cases, and why they were rebuilt (codex-187).** My first version started the
second transaction, slept 150 ms and then looked. Codex showed, with a build overlay, that
such a case passes with the second transaction not yet begun: nothing in it proved the two
overlapped. SQLite's own wait cannot be seen from outside (the pinned driver offers no way
to watch its busy handler), so the cases now stand where the busy handler stands:
- the contender runs on a connection whose busy limit is zero, so its first statement is
  refused at once while the holder holds, and the test receives that refusal (SQLite's
  busy error, not the stale-snapshot one);
- only then does the test let the holder commit, and only after that commit does it have
  the contender repeat the same statement in the same transaction, as the busy handler
  would;
- no case sleeps, and none passes unless the refusal was seen. A contender that is late is
  only late.
- **What this does not show:** a single call waiting inside SQLite and then going on. The
  measurement above shows the handler waiting its limit and the next attempt succeeding;
  the mixed run exercises the real handler without proving any two transactions
  overlapped.

**Seen.**
- The first 27 transition tests were written against stubs and all failed on their own
  assertions (an empty outcome where `applied` was wanted); with the code, all pass.
- **The forced cases (F1 to F5)** pass: logging first and a New behind it; a New first and
  logging behind it; a refused New cancelled before it can try again, which reports the
  cancellation itself and stores nothing; a pointer change against a New and against
  logging, both orders; an exit against an open of one lifetime, both orders. Each reads
  the rows back.
- **A database failure part-way through a control (codex-187)** stores nothing. A trigger
  on the test's own file makes a later statement fail after earlier ones in the same
  transaction: Continue's reopen after its close, and after its discard; its new interval
  after both; the recovery Continue's new interval; the exit's close, and its discard,
  after the exit row; New's new session after its close; the generation's advance after
  everything; Open's new session after its discard; the close for a switch; a stale New
  whose default-epoch close fails. Each returns the failure, not a refusal, and every
  session, interval, member, recorded exit and both counters read as before.
- The logging step's close-and-open rolls back with its QSO: the earlier session is open
  as it was, and the QSO, its upload row and its member row are gone.
- **The mixed run (F9):** 160 logging transactions and 60 News on six goroutines, nothing
  held, about 100 ms, no failure, one New refused as stale; every QSO has one member row
  and the generation ends at its start plus 60.
- The session tests pass under `-race`, and the forced cases eight times over without it.
- **Reversion proofs,** in temporary copies since removed, each change checked as applied
  and the file restored and compared:
  - **the writer's position made a read:** every forced case reports that the contender
    "was not refused while the holder held", and where a New had read before the other
    committed it then fails with "database is locked (517)". That is the unsafe order
    established step by step, not a mechanism removed and a window hoped for;
  - **a control that commits after a failure:** ten of the failure cases report "changed
    the record", so the earlier writes were real and only the rollback removes them;
  - the stale check removed; the discard inverted; the retired check removed; the logging
    step's advance removed; the rebinding removed; the default-logbook guard removed; an
    exit of a lifetime that is not current treated as current; an arrival's end confirmed;
    the default-epoch test removed; the recovery New's end confirmed; Continue's
    eligibility skipped; the recovery Continue's end confirmed; the exit not recorded with
    its close: each fails at least one test;
  - the state read made three separate reads: the snapshot test fails in five runs of
    five ("a read mixed two generations").
- **Gates, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0 issues), the
  maintainability metrics (0 regressions), `go test ./... -count=1` (66 packages ok).

**For part 2 and W-0027 A3, found while building and agreed with Codex (codex-187).**
- **An exit that races its own admission is refused as stale first.** The exit named the
  generation from before the open, the open moved it, and the stale check comes before the
  exit's own effect. The answer carries the new generation. **The closing app may repeat
  that same lifetime's exit report at the generation returned, without admitting again.**
  This is an exception for the exit only, beside the stale exit after a default change
  already named above. It does not allow New or Continue to be replayed against a newly
  read generation (ADR 0100 §5). On every attempt the recorded exit still comes first, and
  the current-lifetime and holder checks and the terminal not-current answer still apply.
- **New and Continue do not check that the caller is current.** They know nothing of the
  admission. Part 2 must validate that the caller is the current lifetime, under its one
  order from the admission's lock, before it calls them. The storage functions refuse only
  what the file records: a lifetime whose exit is recorded (the fix below).
- **A cancelled request that is waiting for the writer's position is not interrupted at
  once.** Measured in my first version: a cancelled New had not returned 150 ms after its
  cancellation while the holder held, and returned "context canceled" once the holder
  committed. Codex read the pinned driver and reports that its busy handler waits and
  retries against the configured limit without looking at the context. That is a delay
  seen, not a deadline proved: no test here shows a five-second bound. The existing limits
  stay; nothing new is chosen.

**Choices made where the criteria were silent (Codex: acceptable at this boundary).**
- New with no session open opens one.
- Continue naming the session the caller already holds is refused as ineligible.
- A holder of "" is stored as no holder, for the logging step's safety net (part 5).
- One seam for tests is in the code: `takeSessionWriterPosition`, the lock statement as a
  function variable, which only the contention tests replace.

**Not shown.**
- The mixed run has no archive-switch close and never reaches the safety net: every
  logging transaction there finds a session open. Both are covered one transaction at a
  time and, for the safety net, in the forced pointer-change case.
- That the controls are durable is shown only by their passing through the durable path
  (the forced cases set the contender's busy limit through that path's own seam); no power
  loss was simulated.
- Nothing was run at the station.

### Part 1, second slice: two fixes from its reviews (2026-11-20; committed `93a743f8`)

**The finding (P2), which is right.** The review of `6f2b4e27` read New and Continue and
found that a recorded exit did not stop them. A lifetime at the recovery question holds no
session, so its exit is recorded without moving the generation. A New or a Continue it had
sent before, arriving after, names a generation that is still current: New would close the
other lifetime's session and open one for a lifetime that has gone, and Continue would hand
that session to it. A repeat of the exit report is then answered "already recorded", so
nothing would close the session.

**Why it was there.** I had left recorded exits to part 2's current-lifetime check and
written that down as a choice; Codex accepted it with the caller's precondition made
explicit. The review, reading the code alone, shows the storage functions can refuse this
themselves from what the file holds, in the same transaction as the generation check. That
is the stronger place, and it costs one read.

**The fix.** New and Continue ask first whether the caller's exit is recorded, as Open
does, and answer "retired" before the default-epoch test and the stale check. A retired
caller touches no session.

**Seen.** The four new cases failed first on their own assertions: a delayed New and a
delayed Continue after the exit were "applied"; with the default moved they were "stale"
where "retired" is wanted, having closed the other lifetime's session on the way. With the
fix all pass. With the check taken out of New, and then of Continue, in a temporary copy
since removed, the cases for each fail again. The gates were rerun, by exit code: `gofmt`,
`go vet`, `golangci-lint` (0 issues), the maintainability metrics, `go test ./... -count=1`
(66 packages ok), and the session tests under `-race`.

**The same gap in the logging step, found by Codex (codex-189, P2), and fixed with it.**
The step's safety net gave the session it opens to whatever holder it was passed, an
exited lifetime included: Codex reproduced a session open under a lifetime whose exit was
recorded. Now, when the step must open a session, a holder whose exit is recorded holds
nothing and the session is opened with no holder. That covers both ways the step opens one:
none was open, and the default-epoch test closed the one that was. The QSO, its upload row
and its member row are stored as always; the step refuses no contact for this. A session
already open keeps the holder it has.
- Four cases, the two that open a session seen to fail first with the exited lifetime as
  holder; the other two are the neighbours that must not change (an open session keeps its
  holder; a holder with no recorded exit still holds).
- Reversion proofs, in a temporary copy since removed: the holder's check taken out, the
  two opening cases fail; the step made to rewrite an open session's holder, the joining
  case and an older test fail.
- The gates were rerun afterwards, the same set, 66 packages ok.

**What part 2 still owes.** This closes the case the file can see. A lifetime that is not
current and has no recorded exit (one an archive switch or a daemon restart left behind)
is still part 2's to refuse, by the admission.

### The session tests' fixture (2026-11-20; built, committed with this record)

On the operator's direction ("make the fixture change, then write part 2's criteria"),
after both agents recommended it.

**Why.** Hosted run 44688068888 took 755.6 s for `internal/database/sqlite` under the race
detector, against 485.5 s before the second slice; the limit is 20 minutes for a test
binary. Each session test built its own file and ran every log migration.

**The change, to test code only.** One log database is migrated once by the real Service
and closed, so its main file is the whole database (a template whose write-ahead file
survives is refused). Its bytes are kept, and its scratch directory removed. Each session
bed writes those bytes to a file of its own and opens it through the real Service. The
migration tests, and every helper that stops at an older version, still run the
migrations.

**Codex's terms (codex-191), each met.**
- Nothing a test can change is shared: `TestSessionBeds_ShareNothing` fills one bed and
  finds a second empty, with its own archive identity and logbook uuids, the first bed
  unchanged and the template's bytes unchanged.
- The template is what the migrations make: `TestMigratedTemplate_IsWhatTheMigrationsMake`
  compares a copy's schema with a file migrated in the test, and checks the head, not
  dirty, and no row in any table a bed fills.
- The contention tests are untouched: a real file, separate connections, the observed
  refusals.
- Both new tests bite, in a temporary copy since removed: every bed put on one file, the
  first fails; a template with an index dropped, the second fails.
- The same assertions pass, under the race detector too; no limit is raised and nothing is
  run in parallel.

**Measured here, under the race detector, before and after.**

| | Before | After |
|---|---|---|
| the session tests | 48.1 s, 48.0 s | 9.6 s, 8.5 s (with the two new tests) |
| the whole package | 224.5 s | 185.3 s, 183.6 s |

- The hosted figure for the package with this change is not known until a run includes it.
  Hosted times vary: the run on `93a743f8` took 571.9 s for the same package that took
  755.6 s one commit earlier with fewer tests.
- **What is left is not the session tests.** They now total about 7 s of the package's 184.
  Of its 317 tests, 249 take 0.4 s or more under the race detector, each migrating a
  database of its own in memory. Making those cheaper is a wider change, to tests this work
  did not write; not started, and named here so it is not lost.

**Two tests of mine made independent of timing, found while measuring.**
- The busy-handler measurement asserted that a refusal under a 300 ms limit took at least
  200 ms. In one whole-suite run it took 182 ms and the test failed. The assertion is
  gone; the time is logged. That assertion was in `6f2b4e27` and `93a743f8` as pushed; the
  hosted run on `93a743f8` happened to pass. I have not established why the wait was
  shorter than its limit.
- The snapshot test failed if fewer than fifteen reads ran beside its writes, which
  depended on scheduling. The writer now goes on until the reader has made 200 reads, and
  the reader until the writer has made 60 commits. With the state read made three separate
  reads it still fails, five runs of five.

**Gates, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0 issues), the maintainability
metrics, `go test ./... -count=1` twice (66 packages ok each time), the package under
`-race -short`.

### Part 2: acceptance criteria (2026-11-20; its six questions ruled; agreed with Codex, codex-194; nothing built)

Codex's review (codex-192) found two gaps and asked for two things to be completed, each
checked in the source and taken: the one order left out the default change and the
activation, which exist today and seal the same gate an exit seals (G); setup's save was
allowed to succeed with no session, a state nobody had ruled and nothing recovered from
(C, question 5); what the admission is after a failed step, and what restores it, was not
said (B4); and the exit lacked the cross-archive case and the evidence for its drain (E).
Its next review (codex-193) accepted those and found that the repeat of a failed setup
save, which the revision relied on, does not work when the configuration names a logbook
id other than the one allocated; C5 and G6 are revised for it, and its last review
(codex-194) has no remaining objection. Codex adds one caution for the build, taken: the
three ways C5 names of recovering the earlier logbook are alternatives to assess against
all three failure points, not each a complete answer; whichever is chosen must leave
logbooks that were already there alone.

On the operator's direction ("make the fixture change, then write part 2's criteria").
Read against ADR 0100 §3 and §5, the brief's proposals A, C, D and H, part 1 as committed,
and `internal/api/app_admission.go`, `live_entry.go`, `internal/events` and
`cmd/smd/lifecycle_adapters.go` as they stand. "Proposed" marks what the ADR and the brief
leave open. The questions at the end are the operator's.

**What part 2 is.** The daemon begins to use the record of part 1:
- the session's start step, inside the app's admission;
- setup's save opening the first session;
- one read route for the session state;
- the exit report, with the brief's exit protocol;
- the events;
- the API reference.

It has no membership (part 5), no New or Continue route (parts 4 and 6), no archive-switch
work (part 3) and no change to the UI.

**What the operator sees.** Nothing new in the workspace. Underneath, starting the app
opens a session. No contact joins one yet, so every session is empty. The app sends no
exit report until W-0027 A3, so each start finds the previous start's empty session,
discards it and opens another; no question can be asked. **One thing can show:** the
admission now reads the archive file. If the file cannot be read, the app stays on its
waiting page instead of opening the workspace (ADR 0100 §3: an unreadable state is never
treated as a clean exit). Today the admission cannot fail that way.

**Nearest confusable outcome.** The read route reports a session and looks right, while
the admission and the file disagree: a current lifetime with no session outside the named
states; a session held by a lifetime that is not current; two sessions opened for one
start; an exit acknowledged whose session stayed open. Every criterion is judged on the
file's rows and the admission's state together, through the real routes.

**A. A daemon restart.**
1. An ordinary restart, on the same archive and the same default, changes no session row.
   The same app, admitted again by the new process, holds the session it held (ADR 0100
   §3). A restart that applies a default change is not this case: the admission's
   default-epoch test then closes the session of the earlier default and opens one for the
   new.
2. A new process takes no lifetime as current, and gives no session to one, without an
   admission.

**B. The start step, inside the admission** (proposal A). It runs under the admission's
lock, after the scopes are stopped and before the lifetime is published.
1. **A recorded exit comes first.** A lifetime whose exit is recorded in the served file is
   answered `app_lifetime_retired`, as a lifetime replaced in this process is today, before
   any scope is stopped. The current app, its scopes and every session are untouched. This
   holds across a daemon restart, which forgets the in-memory retirement.
2. **Each row of the step's table, through the real route, read back from the file:**

   | The file holds | The admission answers | Stored after |
   |---|---|---|
   | no default logbook | admitted | no session ("setup pending") |
   | an open session held by this lifetime | admitted | nothing changes |
   | an open session of another lifetime, with a member | admitted | nothing changes; this lifetime holds none ("recovery pending") |
   | an open session of another lifetime, empty | admitted | that one removed, a new one held by this lifetime |
   | no open session | admitted | a new one held by this lifetime |

   The third row cannot arise at the station before part 5; it is tested with member rows
   put in by the test.
3. **Twice is once.** The same admission repeated, and a repeat after a lost answer, leave
   what one leaves: never a second session.
4. **If the step fails, the lifetime is not admitted** (proposed, question 1). The answer
   is an error, not a success. What is true afterwards, in each case:

   | The failed admission was | Current lifetime | Admission generation | Retired | The file | Scopes |
   |---|---|---|---|---|---|
   | the first in this process | none, as before | unchanged | nothing | unchanged, session generation included | authorize nobody, as before |
   | a replacement of A by B | A, as before | unchanged | nothing | unchanged | stopped: they authorize nobody, A included |
   | a repeat by the current lifetime, completing an earlier failure | that lifetime, as before | unchanged | nothing | unchanged | still stopped |

   - **Recovery, once the file can be read and written:** B admits again and is admitted:
     A is retired, B is current, the session step has run and the scopes are published to
     B. Or, if it is A that returns, its admission must not answer success and leave the
     scopes authorizing nobody: **an admission by the current lifetime completes whatever a
     failed step left undone**, the session step and the scopes' publication, and changes
     nothing when nothing was left undone. Today that repeat returns at once
     (`app_admission.go`, `admit`); this is the one change to it.
   - **Publishing is not arming.** After recovery TX is disarmed and stays so until the
     operator arms it.
5. **What the admission answers today is unchanged** for a repeat, a stale read and a
   replaced lifetime: its existing tests pass as they are.

**C. Setup's first save.**
1. A save that answers success while a lifetime is current has left a session open for
   that lifetime, bound to the new default logbook (question 5 is what happens when it
   cannot).
2. It takes the one order from the admission's lock and checks there that the lifetime is
   still current and has no recorded exit. A save that meets a replacement or an exit
   opens nothing for a lifetime that has gone.
3. A save with no current lifetime opens nothing; the next admission opens the session.
4. **If the session cannot be opened, the save does not succeed** (proposed, question 5).
   The session is opened after the default logbook exists and is recorded in the file, and
   before the configuration is committed. A failure there answers an error and setup is
   not complete.
5. **The save can be made again, by the same lifetime, and then completes.** No new
   lifetime and no restart is needed. Three places it can fail after the logbook was made,
   each tested for what is stored after the failure and after the repeat:
   - recording the default in the archive file fails;
   - the session open fails;
   - the configuration commit fails (the session is then open and setup incomplete).

   After the repeat in each: exactly one default logbook, the file's pointer on it, the
   configuration naming it, setup complete, and one session held by the lifetime.
   - **The fixture must have the configuration name a logbook id other than the one the
     database allocates** (codex-193). Today's helper looks the logbook up by the id the
     configuration names and inserts a new one when that id is absent
     (`seedDefaultLogbook`); the configuration learns the allocated id only at its commit.
     So after a failure before that commit, a repeat looks up the old id again, finds
     nothing, and its insert of a second "Default" is refused as a duplicate name: Codex
     reproduced it. With equal ids the repeat happens to work, which is what my first
     reading saw.
   - **The repeat must not find the logbook by the configured id alone, nor by its name
     alone.** How the earlier logbook is recovered or undone is for the build to choose and
     show: taking the file's own default pointer as the authority where it is set; making
     the logbook and recording the pointer one transaction, so a failure leaves neither;
     or removing the logbook just made when a later step fails. No operator decision is
     needed to choose between them.

**D. The read route.** `GET /v1/operating-session` (the name is proposed, with H).
1. It returns one snapshot: the archive's identity, the generation, and the open session
   if any: its identity, its logbook, when it opened, when the current sitting started,
   whether it has contacts, and whether the caller's lifetime holds it. It names the
   state for the caller: open, setup pending, recovery pending, or none.
2. Proposed: it says "held by you", and does not give another lifetime's label.
3. It changes nothing, whatever lifetime asks, or none: the file reads the same after.
4. A failed read is an error. It is never an empty session (proposal F).

**E. The exit report.** `POST /v1/operating-session/exit` (name proposed), naming the
lifetime, the generation it expects and the archive. Nothing sends it until W-0027 A3; the
tests do. **The archive is checked only for a current lifetime.** The route is not behind
the guard that refuses every request naming another archive: a closing app whose archive
was switched under it names the old one, and must still reach cases 1 and 7.
1. **Its exit is already recorded:** acknowledged. Nothing changes and no scope is
   stopped, whoever is current and whatever generation is named.
2. **It is the current lifetime:**
   - a wrong archive or a stale generation at the first check is refused, and nothing has
     been sealed: the scopes and the live-entry gate are as they were;
   - then every scope is sealed and stopped, and the live-entry gate sealed. A Phone/CW
     submit admitted before the seal is stored; one arriving after is refused with the
     gate's own code and nothing is logged;
   - then the FT8 completion jobs already earned finish. None is cancelled, and none
     launched before the seal escapes the wait. The tests read the stored QSO and its
     upload rows: a job seen to finish is not a contact stored. A completion whose write
     fails stores nothing and does not block the exit;
   - then one commit: the exit recorded and, if it holds the open session, that session
     closed (removed while sessions are empty). If it holds none, only the exit;
   - then the lifetime is retired in the admission, the gate released, the event
     published, and the answer given.
3. **It is refused as stale at the commit** (the generation moved after the first check):
   the gate is released and the lifetime published to its scopes again; the answer carries
   the current generation. The same lifetime may repeat its exit at that generation,
   without admitting again. This is the exit's exception only.
4. **A failure or a cancellation before the commit:** the gate released, the lifetime
   published to its scopes again, an error. The exit is not confirmed.
5. **After the commit nothing restores operation under that lifetime.** A lost answer is
   met by a repeat (case 1) or by "retired" at its next admission, across a daemon restart
   too.
6. **It is not current and holds the open session:** refused with a code of its own,
   terminal. Nothing changes (operator ruling 2026-11-19, question 9).
7. **It is not current and holds none:** recorded and acknowledged. No scope is stopped,
   no gate sealed, no session changed.
8. A late report from a replaced lifetime never closes the current lifetime's session.
9. **The exit whose answer was lost, after the archive was switched** (the brief, C): an
   exit commits in archive A and its answer is lost; a caller with no lifetime switches
   the daemon to B; the closing app, which never admits again, repeats its exit, naming A.
   In B it is not current and holds none: its exit is recorded in B's file and
   acknowledged, and B's session and its holder are untouched. Tested through the real
   route, with the old archive's header on the request.

**F. The events,** on the shell's event stream.
1. Proposed (question 3): one event, `operating_session.changed`, carrying the archive and
   the new generation. A window that sees a generation above its own reads the state
   again. No session detail travels in the event.
2. One for each advance of the generation, in order, published after the commit.
3. None for a transition that was rolled back, or that changed nothing.
4. A refused control whose default-epoch test closed a session did advance the generation,
   and is published.
5. Nothing is recorded as a Station Event (ADR 0100, Update 2026-11-13). The UI does not
   listen yet (part 6).

**G. The one order.** The admission's lock, then a scope's gate, then the live-entry gate,
then the writer's position in the file. An admission, an exit and setup's session open
each start at the admission's lock. **A default change and an archive activation exist
today, take seals of their own, and are part of this order now** (the brief lists both;
codex-192). What is true in the source: a default change seals FT8, the rig and the
live-entry gate under the archive manager's single flight, not the admission's lock; the
live-entry gate is one closed flag, and its release reopens it whoever sealed it
(`internal/api/live_entry.go`). So an exit that sealed a gate a default change had already
sealed would, on finishing or failing, reopen live entry under the default change.
1. An admission of a lifetime against its own exit, in both orders, through the real
   handlers: it ends retired and holding no session.
2. An admission of a second lifetime while the first's exit is between its seal and its
   commit: the exit finishes first; the second is then admitted and gets a new session.
3. Setup's save against a replacement: the session belongs to the lifetime that is current
   when the step runs, or to none.
4. Setup's save against an exit of the same lifetime, in both orders: a session opened is
   closed by the exit that follows it; an exit first means the save opens nothing for it.
5. **No operation releases another's seal.** A default change holds its seals, the time
   after its restart request has returned included; an exit then seals, commits and
   releases: live entry is still closed and TX admission still sealed. And the other
   order: an exit is between its seal and its commit when a default change is asked for;
   when the exit releases, the default change's seals hold. The same for the seals an
   activation takes, and for the exit's failure path, which publishes the lifetime to its
   scopes again: that must not undo a seal another operation holds.
6. **They are ordered, not interleaved.** Proposed (question 6): an exit or an admission is never
   refused because a default change or an activation is in flight; it is ordered against
   it and takes effect without touching its seals. A default change or an activation asked
   for while an exit, an admission or setup's open is under way waits for it. Three limits
   on that, from Codex's review (codex-193):
   - **Two archive operations still refuse each other as today,** the second also when the
     first is itself waiting behind an exit. An exit and an admission are ordered with
     them without becoming an archive operation; putting everything behind one blocking
     lock would queue the second request and change its answer.
   - **A seal kept after its request has returned is not waited for.** A default change
     answers with live entry still closed until the restart; an exit in that time goes
     ahead and releases only what it sealed itself (G5). Waiting for that seal to go would
     be waiting for the restart.
   - **"Not refused" is not a promise of an acknowledgement.** If the daemon restarts
     before the exit is stored, the ruled answer still applies afterwards: a lifetime that
     is not current and holds the open session is refused.
7. These are forced by holding one at a seen point and observing the other shut out, as
   part 1's contention tests now are. None sleeps and then looks.

**H. What part 2 must not do, and the reference.** No member row is written. No New or
Continue route. No session is closed or opened at an archive switch (part 3); the
coordination with the existing default change and activation in G is in scope. No Station
Event. No UI change.
`docs/v2-design/api-endpoints.md` gains the two routes, the event and the codes in the
same change.

**Evidence.** Tests first, through the API test server and its real routes, with a clock
the test sets. Reversion proofs in temporary copies. The usual gates. No transmission,
keyed test or rig command: the scopes are stopped against the test keyer.

**Proposed slices.** First the start step in the admission, setup's save, the read route
and the event (A to D, F). Then the exit report (E) with the order's forced cases (G).

**Left to later parts, named so they are not lost.**
- The wait for earned completion work before a remnant is judged empty (the brief's row
  for an empty remnant). Question 2. **Owed by part 5, and before membership reaches the
  station:** deferring it is not leave to omit it.
- The default-epoch test as a start node of its own (ADR 0100 §4). Question 4. **Owed by
  part 3,** as a named temporary departure from the ADR.
- A window's baseline: the event stream replays nothing, so a consumer reads the state on
  connecting and on reconnecting, and compares generations only within the archive the
  event names (part 6).
- The list for Continue and the New and Continue routes (parts 4 and 6).
- The shell sending the exit, and repeating it after a stale answer (W-0027 A3).

**Questions for the operator. All six ruled 2026-11-20, each as recommended (Rulings,
above).**
1. **If the session cannot be opened at admission** (the file cannot be read or written): I
   recommend the app is not admitted and stays on its waiting page, with FT8 left stopped,
   and nothing else changed. The other choice is to admit it with no session and let the
   workspace open, which hides a broken database behind a working screen.
2. **The wait for FT8 completion work at admission.** The brief has the admission wait for
   an earlier app's unfinished logging before it discards an empty session. Until part 5
   no contact joins a session, so the wait cannot change anything. I recommend building it
   with part 5, where it matters, and not now.
3. **The event's shape.** I recommend one event that says "the session state changed, here
   is the new generation", and windows read the state again. The other choice is a named
   event for each kind of change, each carrying session details, which is more to keep
   right and gives a window nothing the read does not.
4. **The default-epoch test at daemon start.** ADR 0100 names a start node for it. Every
   session transaction and the admission itself already run the test, so before part 3
   the node would close a session only a moment earlier than the admission does. I
   recommend adding it with part 3, whose arrival close needs a start node anyway.
5. **If setup's save cannot open the session** (found by Codex; my first draft let the
   save succeed without one). I recommend the save fails: it answers an error, setup is
   not complete, and you save again, which reuses the logbook already made. Success then
   always means a session is open. This also makes recording the default logbook in the
   archive file a requirement of that save, where today a failure there is only logged.
   The other choice is to let the save succeed and name a new state, "no session though
   setup is done", with something that retries; I could not name a retry that does not
   need a restart or a new start of the app.
6. **An exit or an app start that meets a default change or an archive switch in
   progress** (G6). Today a second archive operation is refused while one is in flight. I
   recommend an exit and an app start are not refused that way: they are ordered against
   it and leave its seals alone. A refused exit would leave the close unconfirmed, and once
   contacts join sessions that brings the Continue-or-New question back at the next start
   for no fault of yours. Two archive operations would still refuse each other as now. And
   if the daemon restarts before the exit is stored, the close is still unconfirmed, as
   you ruled on 2026-11-19.

**Codex's recommendations (codex-192, codex-193), which are not rulings:** on 1 to 4, refuse
the admission, once B4 says how it recovers; defer the completion wait only while
membership is unwired; one event is enough, provided consumers read a baseline; defer the
start node to part 3 as an explicit departure. On 5, the strict save is reasonable once
its repeat is shown to work (C5). On 6, reasonable with the three limits in G6. On each
Codex and I recommend the same.
