# W-0029 — Daemon-owned operating sessions: building ADR 0100

**Status:** Opened 2026-11-18 (operator: "W-0029 is fine, open the dossier"); ranked in
[`docs/backlog.md`](../backlog.md). The build brief was drafted (operator, the same day:
"start the ADR 0100 brief"), reviewed by Codex in five rounds, and **its questions are ruled
(operator, 2026-11-19: "Accepted")**. Part 1's first slice, the migration and the default epoch, is committed (`62218133`,
2026-11-20), and so is its second, the transitions, the state read and the contention tests
(`6f2b4e27`), with two fixes from its reviews (`93a743f8`). **Part 1 is complete.** Built
and committed with this text: a cheaper fixture for the session tests. **Part 2's acceptance
criteria are at the end of this file; their six questions are ruled (2026-11-20) and Codex
has no remaining objection to them (codex-194). Part 2's first slice, the start step in
the admission, the read route and the event, is committed (`a8317035`), with one fix from
its clean-room review (`de4eb3fd`). The second slice, setup's save, is committed
(`3f2f08aa`), with one fix from its clean-room review (`e5058ed5`) and a correction to
that fix's error reporting (`806d5a09`). The third slice, the exit report with the one
order, is committed (`d49b0d4e`) with the fixes from Codex's two reviews of it (codex-203,
codex-204; Codex's third reading, codex-205, had no remaining blocking objection). The
commit after it makes the operator's Abandon log a contact the partner has rogered (ruled
2026-11-20), with the findings of Codex's three reviews of that change fixed (codex-206,
codex-207, codex-208; the last five sections of this file). Codex had not read the last
of those fixes when the operator directed the commit.
The corrections that followed are committed too (`696c28f0`, `113375af`, `01c5c69a`).
With it part 2's criteria are built; what is not shown is listed there. Part 3's acceptance
criteria are written, at the end of this file (2026-11-21); two of its questions are ruled,
the third is withdrawn and the fourth ruled, Codex agrees to the criteria (codex-215), and
its build is directed (2026-11-21), the first slice first. That first slice, the arrival,
is committed (`b0c636ba`); Codex's review of it is closed (codex-217). The second slice,
the switch request and the closing phase, is built and committed with this text (the last
section of this file). Codex found no defect in the switch path and asked for two
follow-ups (codex-219): one is done; the other, a demonstration of the late hand-over
through the daemon, is answered by a source check on the operator's decision, which Codex
has not read. With it part 3's criteria are built; G7 is owed by parts 4 and 6. A finding
of that commit's clean-room review is fixed and committed with this text (the refusal
reworded, 2026-11-23). Part 4's acceptance criteria are written, at the end of this file
(2026-11-23), revised after Codex's review of them (codex-222); their four questions are
ruled (2026-11-23); Codex supports the revision with no design finding remaining
(codex-223). Part 4's first slice, the daemon's side, is built and committed with this
text (the last section of this file); Codex's review of its runtime is complete, with no defect found
(codex-224). Its second, the screen, is not built.** The removal of the database half of
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
- **2026-11-20 (operator): "build the exit report slice".** The third slice of part 2 is
  directed: the exit report (E) with the order's cases (G).
- **2026-11-21 (operator): "write part 3's acceptance criteria".** The criteria are directed,
  for review by the operator and Codex. No build is.
- **2026-11-21 (operator): "Both agreed."** Said after Claude listed, in chat, the two of
  part 3's three questions that the revision after codex-212 had changed, with a
  recommendation for each. Claude's reading, not the operator's words: "both" is those two,
  each ruled as recommended; the third question was not put again and is not ruled.
  1. **What a submit refused during a switch says:** 409 `archive_switch_pending`, "The
     archive is being switched and the daemon is restarting. This attempt logged nothing.
     After the page reloads, check which archive is active before you log the contact
     again."
  2. **A switch's wait for FT8 logging has no limit of its own.** Nothing bounds that wait
     today, and none is added.
  No build of part 3 is directed by this, and Codex had not yet agreed to the criteria.
- **2026-11-21 (operator): "a".** Said of part 3's question 4, how far the arrival must
  hold when `config.json` cannot be written reliably, put in chat with three choices.
  **(a): the limit is accepted and named.** An arrival happens once, and exactly once,
  through any single interruption. The chain in which one activation serves A, then B,
  then A again is not covered; its worst result is a session carried on, or a
  Continue-or-New question, where a new session was due. Not chosen: keeping the app on
  its waiting page until the daemon has durably recorded how a switch attempt ended; and a
  third durable record of the archive last served. No build is directed by this.
- **2026-11-21 (operator): "commit and build".** Said after Codex agreed to part 3's
  criteria (codex-215) and Claude offered two things: the commit of this dossier, and the
  build of part 3's first slice. Claude's reading, not the operator's words: the commit is
  of the dossier, without a push, which was not said; the build is of part 3, its first
  slice first, each slice reviewed and committed on direction before the next, by the
  working rules above.
- **2026-11-21 (operator): "commit, push and move on with the build 2nd slice - we are way
  behind...".** The first slice's commit and push, and the build of the second slice, are
  directed.
- **2026-11-22 (operator): "Accepted".** Said of the clean-room review's finding on
  `b0c636ba`, put in chat as a choice between accepting it and taking one of the two
  choices passed over on question 4. After a fallback, if both of `config.json`'s writes
  for that activation are lost to a power cut, the next start closes the session opened
  after the fallback as `archive_switched`, though it processed no switch. No contact is
  lost or moved. It is a second named chain beyond a single interruption, beside Home,
  Contest, Home; nothing is built for it.
- **2026-11-22 (operator): "It seems to me that the CI is really slowing down dev work. Is
  there anyway to be a little more focused on the tests we run locally, and when we think
  we have finished a step, then run the whole CI locally before the push?"** Adopted: the
  named tests and the changed packages while building; the whole local gate
  (`task ci:local`) once, when a step is thought finished, before the push.
- **2026-11-22 (operator): "go with recommendations".** Said of five recommendations put
  in chat for part 3's second slice.
  1. **No method in `internal/ft8` for a test alone** (the operator had asked: "you want
     to add the method purely for a test?"). `DriveSlotForTest`, its guard and the daemon
     test that used it are removed; the drain's hand-over wait is pinned by a source check
     in `cmd/smd` instead. This is weaker than the behavioural demonstration Codex asked
     for (codex-219), and was put to the operator as such.
  2. **The three decisions made in the build are approved:** a close that fails answers
     `session_close_failed`, 503; an exit in the closing phase is acknowledged whatever
     generation it names; the switch's two waits are made with the archive manager's lock
     held.
  3. **Commit and push of the second slice,** after the first item, with one
     `task ci:local` first. Claude's reading, not the operator's words: this is the
     direction to commit and push once that run passes, without another round with Codex.
  4. **Part 3 goes to the station as a whole,** not its first slice alone. When is not
     decided.
  5. **The one measurement** (how long an FT8 completion takes to store) is left until
     part 3 is at the station.
- **2026-11-23 (operator): "go with the reword, then write part 4's acceptance
  criteria".** The reword is of what a submit refused during a switch says, ruled on
  2026-11-21 (question 1 of part 3) and now changed. It was put with the clean-room
  review's finding on `df655a2b` and one alternative, accepting the finding with a
  comment. The message is now: "The archive is being switched. This attempt logged
  nothing. Check which archive is active before you log the contact again." The code
  (409 `archive_switch_pending`) is unchanged. The default change's refusal, which has
  the same shape, was not asked for and is not changed.
- **2026-11-23 (operator): "go with recommendations".** Said of part 4's four questions,
  each ruled as recommended.
  1. **The two answer routes are built whole:** New and Continue in all their uses, the
     Session panel's included, not the recovery answers alone.
  2. **The words on the recovery screen,** as proposed. Heading: "The last session is still
     open". Text: "Station Manager did not close normally last time. The session that
     started {date and time} has {n} contacts and was never closed." Buttons: "Continue
     this session" and "Start a new session". Under them: "If you start a new one, the
     earlier session is closed and kept, with its contacts."
  3. **Nothing else is on that screen:** no Settings and no rail.
  4. **The read gate at the page's load:** a page that cannot read the session's state
     says so and offers to try again, and does not show the workspace. It applies at the
     load only.
  No build is directed by this. Codex had not yet read the criteria as revised.
- **2026-11-23 (operator): "commit, push and build part 4's first slice".** Said after
  Codex supported the revised criteria (codex-223). The commit and push are of the reword
  and the criteria (`36d42db7`); the build is of part 4's first slice, the daemon's side.

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
   | a replacement of A by B | A is still the lifetime the admission holds, and is not retired; **its read says "not current"** while the scopes are stopped | unchanged | nothing | unchanged | stopped: they authorize nobody, A included |
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
   - **A fourth road to the third state (codex-198):** the configuration's last
     validation, which runs after the logbook and the session are stored, refuses the save
     with a 400. The file keeps the logbook, its pointer and the session; setup is
     incomplete; the corrected save completes and keeps that session. The archive file and
     the configuration are two stores: a save is not rolled back across both, and no
     criterion here says it is.
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

### Part 2, first slice: the start step in the admission, the read route, the event (2026-11-20; committed `a8317035`)

Groups A, B, D and F of part 2's criteria. Setup's save (C) is the next slice, then the
exit report (E) with the order's forced cases (G). Built under the operator's direction of
2026-11-20 ("consensus is a go"), after Codex's agreement on the criteria (codex-194).

**What changes at the station when this is deployed.** Starting the app opens a session
in the archive file; nothing shows it. With no contact joining one and no exit report yet,
each start discards the previous start's empty session and opens another. If the archive
file cannot be read or written when the app starts, the app stays on its waiting page.

**Built.**
- `internal/api/app_admission.go`: inside the admission, in this order: the caller is
  already current (it completes anything a failed step left undone, and otherwise changes
  nothing); retired in this process; **its exit is recorded in the file, refused as
  retired before anything is stopped**; stale; the scopes stopped; **the session's start
  step**; and only then the earlier lifetime retired, this one current, the scopes
  published. A step that fails leaves the lifetime not admitted and the scopes stopped.
- `internal/api/operating_session.go` (new): the start step through part 1's Open; the
  one event; `GET /v1/operating-session`.
- `internal/database/sqlite/operating_session.go`: the state now also says the file's
  default logbook and whether the control that returned it advanced the generation.
- `internal/events/events.go`: `operating_session.changed` and its payload.
- `docs/v2-design/api-endpoints.md`: the admission's new step and its 503, the read route,
  the event.

**One change to the tests' fixture, none to the admission's existing tests.** The API test
server now gives its database the archive identity row, as the daemon's start does before
it binds a listener. Without it every admission in that package was refused (150 tests),
which is the ruled behaviour for a record that cannot be read. With it, the admission's
existing tests pass as written.

**Seen.**
- The ten new tests were written first, against a server with no session route and an
  unchanged admission: all failed on their own assertions (a 404 for the route; a 200
  where a 503 is wanted; no event).
- With the code, they pass, as does the whole tree: 66 packages.
- **Through the real daemon:** the desktop scenario that starts a built `smd` in a
  temporary directory still passes all 19 checks, among them "the app was admitted before
  its workspace loaded". No station file or port is used by it.
- **Reversion proofs,** in temporary copies since removed, each change checked as applied
  and the file restored and compared. Eight failed their tests at once: the durable
  retirement check removed; a failed step admitting anyway; the current lifetime's repeat
  repairing nothing; no start step; no event; a failed read answered as an empty session;
  recovery pending reported as open; "held by you" true for anyone.
  - **One did not, and the test was at fault:** publishing an event when nothing changed
    passed, because the only unchanged admission in the test never reached the step. The
    test now also admits the same lifetime after a restart and admits against another
    lifetime's session with a contact, and requires no event from either; the change then
    fails it ("an admission that found its own session published [2]").
- **Gates, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0 issues), the
  maintainability metrics (0 regressions), `go test ./... -count=1` (66 packages ok), the
  slice's tests under `-race`.

**How a daemon restart is modelled in these tests.** The admission's in-memory state is
cleared on the same server and file: no lifetime current, nothing retired, generation 0.
A second process is not started. The real-daemon scenario above covers one true start.

**Choices made, for review.**
- The state the route names is decided from the file and the admission together: `open`
  when the current lifetime holds the session or it is empty; `recovery_pending` when it
  has contacts and its holder is not current. Between a daemon restart and the app's
  admission, a session with contacts therefore reads `recovery_pending` for a moment. No
  window reads before the app is admitted.
- The start step and the durable retirement read do not follow the request's context: an
  admission the client gave up on is not left half done.
- A failed step is logged with the database's error text. That text holds no credential:
  it names tables and constraints of the archive file.

**After Codex's review of the slice (codex-195: the admission and the fixture pass; one P2
in D).** The route read the file and then asked the admission who was current, as two
separate looks. A close and retirement landing between them gave an answer that never
existed: Codex forced it and got the old session, held by the caller, named "recovery
pending".
- **Fixed:** the file is read and the state named with the admission's order held across
  both (`observe`), and the current lifetime seen there is handed to the code that names
  the state. The route still changes nothing. A read now waits for an admission that is
  under way.
- **The forced case, written first:** at the point between the read and the naming, the
  test tries to take the admission's lock. Before the fix it got it, performed the close
  and the retirement there, and the route returned Codex's combination: the test failed on
  both counts. After the fix the lock is held, nothing can come between, and the answer is
  the whole state from before.
- The admission's header comment, which still said no session is opened here, is
  corrected.
- Gates rerun: the same set, 66 packages ok, the slice's tests under `-race`.

**Not built, and owed by the next slices.** Setup's save and its repeat (C). The exit
report (E). The coordination with a default change and an activation (G): until then
nothing in part 2 seals the live-entry gate, so the gap Codex found is not open.

**Not shown.** Nothing was run at the station. That publishing the scopes arms nothing is
the scopes' existing contract; it is not asserted again here.

### Part 2, first slice: a fix from its clean-room review (2026-11-20; committed `de4eb3fd`)

**The finding (P2), which is right.** After a replacement fails on the session's record,
the admission still holds the earlier lifetime A while FT8 authorizes nobody. The criteria
(B4) promise that A's own admission then repairs it. But the desktop app does not send an
admit when its read says it is current: it reads, and stops there
(`desktop/src/admission.mjs`, `admit`). The read said "current" for A, so the app would
have believed itself admitted while FT8 refused its commands. My recovery test sent the
admit directly and so never met the app's read.

**The fix.** While the scopes are stopped after a failed step, the read says "not current"
for everyone, A included. A's app therefore sends its admit, which completes the step and
publishes the scopes to it; after that its read says "current" again and the flow stops at
the read as before. The admission's memory is as the criteria say: A is still held, nothing
is retired, the generation has not moved. B4's table is reworded for it.

**Seen.** A new test follows the app's own flow (read; admit only if the read does not say
current) through a failed replacement. Before the fix it failed at once: "the earlier
lifetime reads ... Current:true while its scopes are stopped". After it: while the file
still fails the flow sends its admit and gets the 503; once the fault is removed the flow
sends its admit, succeeds, and the scope records the publication to A; a further flow
stops at the read and publishes nothing; a later replacement by B still works. One
assertion of the earlier test, which required A to read as current after the failure, is
changed to match. Gates rerun: the same set, 66 packages ok, the slice's tests under
`-race`.

**Two things Codex adds for the next slices (codex-197), taken.**
- The read's "current" now also means the scopes are published. It is an answer for the
  app, not the admission's own fact. Setup's save and the exit decide from the lifetime the
  admission holds, under its lock, and must not take "reads as not current" for retired or
  absent.
- Nothing tells an app that is already loaded that a replacement failed: the marker and
  generation on its responses are unchanged. The repair happens at its next admission
  attempt, which is what the criteria promise.

**How reachable it was.** A replacement happens because a new start of the app admits, and
the app is single-instance, so the earlier app is normally gone and it is the new one that
tries again; that path was already right. The fix is for the case the criteria name, the
earlier lifetime returning.

### Part 2, second slice: setup's first save opens the first session (2026-11-20; committed `3f2f08aa`)

Group C of part 2's criteria, built under the operator's standing direction for part 2.

**What changes for a new install.** The first save of setup now also opens the first
session, for the app that is running. If the archive file cannot record the default
logbook, or the session cannot be opened, that save fails with an error and setup is not
complete; saving again completes it. Before, a failure to record the default logbook in
the file was only logged and the save succeeded.

**Built.**
- `internal/database/sqlite/archive_identity.go`: `InsertArchiveDefaultLogbookWithContext`
  makes a logbook and points the file's default at it in one transaction.
- `internal/api/handler_config.go`: the save now finds its logbook in this order: the one
  the archive file already points at; a logbook that was already there at the id the
  configuration names (adopted as before, and recorded in the file, which is now
  required); otherwise a new one, made and recorded together. Then it opens the session,
  before the configuration is committed.
- `internal/api/operating_session.go`: `openSessionAtSetup`, with the admission's order
  held, for the lifetime the admission holds (not what a read would say of it); nothing
  for nobody; nothing for a lifetime whose exit is recorded.
- `docs/v2-design/api-endpoints.md`: the save's new step and its two failures.

**How the repeat works, assessed against all three failure points (codex-194).** Two of
the three mechanisms the criteria name are used, because neither is enough alone:
- **the logbook and its pointer in one transaction** answers a failure to record the
  pointer: nothing is left, so the repeat starts clean;
- **the file's pointer as the authority** answers a failure of the session open and of the
  configuration commit: the logbook and its pointer are stored, and the repeat finds the
  logbook there whatever id the configuration still names.
- A logbook that was already there is found by the configured id, as before, its callsign
  checked, and nothing of it changed but the pointer naming it.

**Seen.**
- The tests first, against the unchanged save: the session cases failed with no session
  stored; the three failure cases failed because the save answered 200, or stored no
  session before the failed commit.
- With the code, they pass, with every older test of the save unchanged and passing.
- **The three failure points, each with the configuration naming logbook 42** while the
  database allocates 1: after the failure the stored state is as the criteria say (nothing
  at all; the logbook and pointer and no session; the logbook, pointer and session with
  setup incomplete), and the repeated save ends with exactly one default logbook, the
  pointer and the configuration on it, setup complete, and one session held by the
  lifetime. After the commit failure the repeat keeps the session the first save opened.
- **Reversion proofs,** in a temporary copy since removed, each change checked as applied
  and the file restored and compared: no open at setup; a failed open ignored; the
  pointer not consulted (the repeat then fails after a session failure and after a commit
  failure); the logbook made and then recorded as two writes (the pointer failure then
  leaves a logbook); a session opened for nobody; an adopted logbook not recorded. Each
  fails its tests.
- **Gates, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0 issues), the
  maintainability metrics (0 regressions), `go test ./... -count=1` (66 packages ok).

**Choices made, for review.**
- The save answers 500 `db_error` when the default cannot be recorded and 503
  `operating_session_unavailable` when the session cannot be opened.
- A save by a lifetime other than the one the admission holds still opens the session for
  the held one: the session is the running app's, whoever sent the save.
- If the open answers "setup pending" although the logbook was just recorded, the save
  fails: there is nothing to bind a session to.

**After Codex's review of the slice (codex-198: no blocking finding).** Codex repeated two
of the reversions and tested the road I had named and not tested: a refusal by the
configuration's last validation after the session was opened. Taken from it:
- **A permanent test of that road,** `TestSetupSave_ALateRefusalKeepsWhatWasStored_...`: a
  rig the save selects is removed between its first validation and its last, as a second
  window could; the save answers 400 with the logbook, its pointer and the session stored
  and setup incomplete; the corrected save completes and keeps the session. It passed when
  written: it records behaviour, it did not change any.
- The API reference lists the 503 among the save's errors and says the save is not
  all-or-nothing across the archive file and the configuration.
- `seedDefaultLogbook`'s opening comment, which still described the old lookup, is
  rewritten.

**Not built.** The exit report (E) and the order's forced cases (G), which include setup's
save against a replacement and against an exit under contention; here those two are
tested one after the other, not forced.

**Not shown.** Nothing was run at the station. The real-daemon desktop scenario was not
rerun for this slice: it does not perform setup's save.

### Part 2, second slice: a fix from its clean-room review (2026-11-20; committed `e5058ed5`)

**The finding (P2), which is right about the mechanism.** After a setup save has made its
logbook and before the configuration is committed, the configuration still names the old
id. The delete route protects only the configured default, so the new logbook could be
deleted: by a request landing between the save's logbook and its session open, or simply
after a save that failed. The session open then finds no live default (503), and every
repeat fails for good: the deleted row is skipped, and a second "Default" cannot be made
because a logbook's name is unique among deleted rows too.

**Checked:** `logbook.name` is `UNIQUE` with no exception for deleted rows
(`0001_init.up.sql`); the delete handler compares only `cfg.DefaultLogbookID`.

**How far it goes.** It needs the configuration to name an id other than the one
allocated, which an ordinary first run does not (both are 1, and the configured default
is refused already), and a delete of that logbook through the API during setup. The stuck
state after a failed save was reachable before this slice too, by the old lookup. What the
slice added is the 503 for a delete that lands inside the save.

**The fix.** The logbook the archive file names as its default cannot be deleted. The
refusal is a third condition in the one statement that deletes, beside "is live" and "has
no live QSO", so no request can come between the check and the delete; the route answers
it as it answers the configured default, 409 `default_logbook`. The file's pointer is the
authority for the default (ADR 0071) and the configuration's id its projection; the two
differ while a setup save is between making its logbook and committing its configuration,
and after one that failed there. A file with no identity row has no default, and deletes
as before.

**Seen.** Two tests first, failing on the unchanged code: the storage delete of the
file's default returned no error; the delete route answered 204 for the logbook a failed
save had made. After the fix: the delete is refused and the logbook is live; the former
default can be deleted once the default has moved, and the new default cannot; with no
identity row a delete and a second delete behave as before; after a failed save the
logbook cannot be deleted and the repeated save completes. The whole tree passes
unchanged otherwise: no existing test deleted a file's default through this function.

**Not closed by this.** A logbook's name stays taken by a deleted row. That is older
than this work and reaches further than setup; it is not changed here. Codex and Claude
both recommend it goes to the operator as a decision of its own (codex-200): whether a
deleted logbook's name stays reserved or can be used again, and what a restore of the
deleted one then meets.

**Codex's review (codex-200: no blocking finding).** The statement is the right place for
the rule, and it found no conflict with a default change or an activation, which move the
pointer at a daemon start and delete nothing through this function. It removed only the
new condition in its own overlay and saw both tests fail. Two wordings corrected at its
request: the two ids differ during a save as well as after a failed one; and the delete's
comments, which still spoke of two preconditions.

### The delete's reported reason (2026-11-20; committed `806d5a09`)

**The finding (P3), from the clean-room review of `e5058ed5`, which is right.** The fix
above added a third reason a delete can be refused. The reason was then looked for in
separate reads after the refused delete. If the default moved to another logbook between
the two, none of the reads found a cause, and the function fell through to its last
answer, "has QSOs", for a logbook that held none. The delete itself was never wrong; only
the reason given.

**The correction.** The three conditions are now read in one statement, so they are one
snapshot. If none of them holds, the reason has gone since the delete was refused, and
the delete is tried again; at most three times. A reason is reported only when it is true
at the moment it is read. This also changes an older behaviour for the better: when a
logbook's last QSO was deleted between the refused delete and its diagnosis, the function
used to answer "has QSOs" (its comment called that slightly stale); it now deletes the
logbook.

**Seen.** A test forces the gap with a seam that runs after a refused delete and before
the reason is read. Before the correction: the default moved away in the gap, and the
delete returned "logbook contains QSOs"; the last QSO deleted in the gap, the same. After
it: both logbooks are deleted. A third case holds the three lasting reasons to their own
answers (the default, a logbook with a QSO, no such logbook), with nothing deleted and one
refusal each. The whole tree passes: 66 packages, with lint and the maintainability
metrics.

**After Codex's review (codex-201: no blocking finding).** It asked that the retry's two
ends stay tested in the repository, having forced them itself in an overlay. Added: the
seam now also runs before each try, and a test brings the obstruction back before a try
and takes it away after each refusal. With the obstruction absent at the third try the
logbook is deleted at the third; with it back every time the delete gives up after three
tries with an error that names none of the three reasons, and the logbook is live. In a
temporary copy since removed: with two tries allowed the first case fails; with the
give-up answering "has QSOs" the second fails. Gates rerun: 66 packages ok.

**Not shown.** A cancellation between tries is not tested here (Codex forced it in its
overlay and saw the cancellation returned with nothing deleted). Three is a bound on work,
not a measured figure.

### Part 2, third slice: the exit report and the one order (2026-11-20; committed `d49b0d4e`)

On the operator's direction ("build the exit report slice"). Groups E and G of part 2's
criteria.

**What changes at the station when this is deployed.** Nothing an operator sees: nothing
sends an exit report until W-0027 A3. Two things do change underneath. A change of the
default logbook or an archive activation now waits for an app start that is under way
before it seals anything (an app start takes moments). And the FT8 decode loop takes one
more lock around its call into the sequencer; nothing about keying, a rung or a completion
changes. **Added by the review below:** a stop of FT8 (a disarm, a lost rig, a dial move,
an app replaced) that lands in the instant between the partner's roger and our closing 73
being admitted now logs that contact; before, it was lost.

**Built.**
- **Owned seals** (`internal/api/live_entry.go`). The gate counts its holders; each
  releases only its own seal, and live entry is open only when none is held. The gate's
  own `Seal` and `Release` are the holder the archive manager already uses; `Owner` makes
  another, and the exit has one.
- **The exit route** (`internal/api/operating_session.go`):
  `POST /v1/operating-session/exit`, not behind the archive guard, with the admission's
  order held from start to finish. In order: the exit already recorded in the file
  (acknowledged; nothing stopped); the lifetime the admission holds (its archive and
  generation checked with nothing sealed; then scopes stopped, live entry sealed with the
  exit's own seal, the completion drain, one commit, the lifetime retired, the seal
  released, the change published); not held and holding the open session (refused for
  good); not held and holding none (recorded; nothing else). If the commit fails or
  refuses the exit as stale, the seal is released and the lifetime published to its scopes
  again; the stale answer carries the current generation.
- **The FT8 completion wait** (`cmd/smd`, `drainFt8Completions`): first a contact the
  decode loop earned in a slot still under way is handed to its job, then every tracked
  job finishes. The first half is new in `internal/ft8`: the decode loop's one call into
  the sequencer goes through `driveSequencer`, which marks the slot, and
  `AwaitCompletionHandover` waits for a marked slot. See "The gap in FT8" below.
- **The order with archive operations** (`internal/archive`): a default change and an
  activation, having taken the single flight, take the admission's order
  (`takeSessionOrder`) before the manager's lock, any seal and any write, and hold it
  until the request returns. The API gives the order as `HoldSessionOrder`; `cmd/smd`
  wires it.
- `internal/api/archive_guard.go`: the guard's rule as a function that writes no response,
  so the exit can apply it to one case.
- The reference (`docs/v2-design/api-endpoints.md`: the route, its codes, and the order
  on the default change and the activation) and the FT8 guide (`docs/ft8.md`).

**The gap in FT8, found while building E.** E says of the completion jobs "none launched
before the seal escapes the wait". Read in the source, then shown by a test: a completed
contact is handed to its logging job from one of two goroutines. From the transmission's
own, when the closing 73 played or was cut short; `RetireOperating` waits for that, and
tests of W-0027 C4 show it. Or from the decode loop, when the closing 73 was refused
before a transmission started (the rig not ready, the dial moved): a Group A contact is
logged anyway, and the hand-over runs after the sequencer has released its lock. Nothing
waited for that one. A stop landing in that gap returned with the hand-over still to
come, and a wait for the jobs taken then found none: the contact would have been stored
after the exit closed the session.
- The test was written first and failed as the gap predicts: the stop returned with the
  hand-over in progress, and a wait that does not wait returned with nothing logged.
- The fix adds a lock and a wait. It changes no completion rule, no order inside the
  sequencer and nothing about keying: the FT8 package's seven invariants are as they were,
  and its whole test suite passes under `-race`.
- A source test keeps the decode loop's call single: any mention of the sequencer's slot
  handler outside `driveSequencer`, a call or a method value, fails it.
- **An app start does not use this wait** (ruling 2 of part 2: the wait at an admission
  is built with part 5). A replacement that lands in the same gap still returns before
  the hand-over; the contact is stored, as it is today.

**Seen.**
- **Tests first.** The gate's tests against a gate with one shared flag: four of five
  failed, Codex's counterexample among them. The exit's tests with no route: all failed
  (404). The FT8 hand-over test against a wait that does not wait: failed. The two tests
  of a default change and an activation asked for with an exit under way, before the
  order existed: both failed, each operation answering 202 while the exit was held.
- **Passing without having failed first, and why that is acceptable:** G1 to G4, G5 at
  the daemon and "an exit during a default change is not refused". They test what the
  admission's lock and the owned seals already gave. Each is shown able to fail by a
  reversion proof below.
- **What E's tests assert,** through the app listener: the current lifetime's exit closes
  its session, records the exit, retires the lifetime and publishes one event, and a new
  start is then admitted; the drain runs once, with the scopes stopped and live entry
  sealed, before the commit; a refusal at the first check (another archive, no archive, an
  older or a later generation) has stopped, sealed and drained nothing; a submit admitted
  before the seal is waited for and one arriving after is refused; stale at the commit and
  a failure before it both leave live entry open, the scopes published to the lifetime
  again, the lifetime current and no exit recorded, and the stale one can be repeated at
  the generation returned; an exit already recorded is acknowledged whatever is named,
  after a restart too, and for a lifetime the admission still holds; not current and
  holding the session is refused for good; not current and holding none is recorded with
  nothing else touched, also when it names another archive.
- **The completion wait, through a real daemon** (`cmd/smd`): the exit waits for a
  tracked job the test alone releases, and is not stored meanwhile; a contact whose write
  was held at the archive's write lock is stored, with its upload row, when the exit
  answers; a completion whose write fails stores nothing and the exit still answers.
- **G, forced.**
  - G1: an admission of a lifetime with its own exit under way waits, then is refused as
    retired; an exit with the lifetime's admission under way (held at the file's write
    lock) waits, then succeeds. Either way: retired, no session.
  - G2: a second lifetime's admission with the first's exit held between its seals and
    its commit waits; the first's session is closed by its own exit (`app_closed`), and
    the second gets a new one.
  - G3: setup's save with the order held waits, opens nothing meanwhile, and its session
    is for the lifetime held when the step runs.
  - G4: a save then an exit: the exit closes the session the save opened. An exit under
    way when the save arrives: the save waits and opens nothing for that lifetime.
  - G5, through a real daemon and a real default change: after the change is accepted, an
    exit that succeeds, an app start after it, and an exit that fails and gives operation
    back each leave live entry closed and TX admission sealed (an arm by the current
    lifetime is refused with the pending-change refusal, not for its caller).
  - G6: a default change, and an activation, asked for with an exit held at its commit
    take the single flight and wait, having sealed and recorded nothing; a second archive
    operation asked for meanwhile is refused at once as in progress; when the exit is let
    go it answers 200 and the waiting operation is then accepted. An exit asked for with
    a default change in flight is not refused, and the change's seals hold afterwards.
- **Through a real daemon binary:** the desktop scenario that starts a built `smd` passes
  its 19 checks with the final build.
- **Reversion proofs,** in temporary copies since removed, each change checked as applied
  and the file restored and compared with the tree's.
  - The exit, fourteen: the route behind the archive guard; a lifetime not held treated as
    held; the archive unchecked; no first generation check; no live seal; the gate's
    shared seal in place of the exit's own; the scopes not stopped; no drain; no restore
    when stale; no restore on failure; the lifetime not retired; the seal kept after the
    exit; no event. Thirteen failed at once. **One did not, and the test was missing:**
    "already recorded" not checked first. For a lifetime that is not held the storage
    function gives the same answer. The case that differs is a lifetime the admission
    still holds whose exit the file records; a test of it now fails that change.
  - FT8, four: the decode loop calling the sequencer directly; the slot not marked; the
    wait not waiting; the call made through a method value. All failed.
  - The order, eight: the exit letting go of the order in its drain; setup's step outside
    the order; the order not held for the manager; the default change not ordered; the
    activation not ordered; the order taken before the single flight (a second archive
    operation then waits when it must be refused); the exit using the shared seal; the
    exit's success also releasing the shared seal. All failed.
  - The drain's wiring, two, three runs each: the jobs not waited for; the drain not
    wired. The tracked-job test failed in all six runs. The earned-contact test failed in
    two of six: it shows the rows are stored when the exit answers, and is not by itself
    proof of the wait (without the wait the exit and the contact race for the write lock).
- **Gates, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0 issues), the
  maintainability metrics (0 regressions), `go test ./... -count=1` (66 packages ok); the
  new tests repeated under `-race` (API 8 times, daemon 4 times, FT8 20 times), and the
  FT8 and archive packages whole under `-race`.

**A defect of my own, found reading the finished change before review.** A failed exit
gave the lifetime back to its scopes without asking whether the exit had stopped them.
After a replacement that failed, the held lifetime's scopes are already stopped, and stay
so until its own admission repairs it; a failed exit of that lifetime authorized FT8
again without that repair. A test of it failed ("the scope saw ... publish" after the
failed exit); the exit now gives back only what it took. In the same pass the commit's
outcomes were made explicit: the lifetime is retired only on an outcome that says the
exit is stored.

**Named and NOT shown, each with why.**
- **A bounded look stood in for "it is waiting"** in every forced case here (300 ms, or
  250 ms in the FT8 test). *Replaced in the review below:* each waiter is now seen parked
  at its lock.
- **The FT8 hand-over through the real decode loop.** The test drives `driveSequencer`
  itself, as the loop does, with the real service, sequencer and completion policy; the
  loop's own goroutine and audio are not in it. The source test is what ties the loop to
  that call.
- **The exit's failure path against an activation's seals.** Shown against a default
  change's (G5). An activation takes the same FT8 and rig seals by the same calls; it has
  no live-entry seal. *Shown in the review below,* with the exit that succeeds.
- **Stale at the commit, through a real daemon.** Shown through the API test server.
- **G3 with a real replacement in flight.** The order is held by the test itself, in the
  place a replacement holds it, and the admission's state changed as a replacement
  changes it.
- **A cancelled exit request.** The exit does not follow the request's context (a choice,
  below), so a client that gives up does not interrupt it. *Tested in the review below.*
- **The cancel, confirm and retry actions of a default change, and a logbook deletion,**
  are not ordered with the admission. G6 names a default change and an activation asked
  for; those other operations take no seal.

**Choices made, for review.**
- The admission's own generation does not move at an exit; the lifetime is retired and
  nobody is current.
- A stale answer is the error envelope with `generation` beside it.
- The exit does not follow the request's context, as the admission's step does not.
- An exit by a lifetime that is not held is remembered as retired in memory as well as in
  the file.
- "Already recorded" is answered first for every lifetime (E1: "nothing changes and no
  scope is stopped, whoever is current"), a lifetime the admission still holds included.
  That state cannot be reached in one process, where the commit and the retirement are
  one step; if it were reached, the answer leaves that lifetime held.
- Any caller the app listener accepts can have an exit recorded for a lifetime that was
  never admitted (E7: not current and holding none is recorded). Each is one row.
- A default change or an activation holds the admission's order for its whole request,
  the wait for admitted live submits included. An app start in that time waits for the
  request to answer.
- The FT8 wait is a method of its own, used by the exit's drain only. (`RetireOperating`
  was unchanged when this was written; the review below changes the stop it shares with
  every disarm.)

### Part 2, third slice: Codex's review and what it changed (2026-11-20; committed `d49b0d4e`)

Codex reviewed the slice (codex-203) and did not close it: one blocking finding in FT8,
three answers, and the forced cases still missing. Everything named is fixed here, in the
same uncommitted change.

**The finding: a stop could discard an earned contact before any wait could see it.**
Checked in the source and reproduced by a test of my own before any fix. A Group A closing
rung (our 73 after their RR73) is decided under the sequencer's lock, which builds the
completion that will log the contact, and is admitted under the Service's lock a moment
later. Between the two no transmission exists. A stop landing there (`disarmTxLocked`)
found nothing in flight to wait for and abandoned the session, which retires its
generation; the completion is guarded by that generation, so when the slot went on, was
refused for want of an arm and called it, it refused itself. The contact was not logged.
The wait added by this slice (`AwaitCompletionHandover`) is later and could not recover
it. The loss predates this slice and was open to every stop that goes through the disarm:
the operator's disarm, the presence rule, a lost rig, a dial move, shutdown, and an app
replaced. The exit inherited it. Invariant 1 of `internal/ft8` already says such a contact
is logged; the code did not keep it in this window.

**The fix** (`internal/ft8`): the sequencer now carries the fact. When a Group A
completion is built it is kept on the contact (`contactFlags.earned`), until it runs or
the session is retired. (Codex's second review, below, showed that "when it is built" is
too late; it is now kept from the slot that takes the roger in.) The disarm's abandon (`Sequencer.abandonForStop`) first runs a
kept completion, as not transmitted, and then abandons what is left. Whichever of the
stop, the slot's own failure branch and a transmission's goroutine runs it first retires
the session; the others are refused by the generation guard, which is what makes it once.
- Not taken: waiting for the slot inside the stop. A stop holds the gates a slot takes, so
  that would reverse the lock order (Codex said so too); and it would let the closing 73
  start keying after the stop had been asked for.
- **No rule of keying changed:** nothing new can transmit; the arm is cleared before the
  completion runs, and the test requires that the 73 is not keyed after the stop.
- **The operator's Abandon is unchanged:** it discards the contact, as it did, and as
  `TestSequencer_AbandonDuringFinal73SuppressesStaleCallback` requires. Whether an Abandon
  pressed during our closing 73 should instead log the contact the partner already holds
  is a question for the operator; it is not decided or changed here.
- `docs/ft8.md` records the rule beside the exit's wait.

**The missing forced cases, added.**
- **G4, the save first, forced.** It was sequential. Now the save is held inside its
  session step, with the order taken and no session yet opened; the exit is seen waiting
  at the order; when the save is let go it opens the session and the exit that follows
  closes it.
- **G5 for an activation,** through the real daemon: after an activation is accepted, an
  exit that succeeds, the app start after it, and an exit that fails and gives operation
  back each leave the activation pending and TX admission sealed.
- **An app's start that meets an exit at its admit,** not only at its read. The start is a
  read and then an admit; the tests sent both after the exit had begun, so the start
  waited at its read and the admit never met the exit. G1 and G2 now run both ways.
- **A client that gives up** while its exit is under way: it gets no answer, the exit goes
  on to its commit, and the repeat, which is seen waiting for it, is acknowledged as
  already recorded; live entry is open again and the lifetime retired.

**Seen waiting, in place of a timed look** (G7: "None sleeps and then looks"; Codex:
"observable coordination rather than a timed look"). The Go runtime's goroutine dump gives
each parked goroutine's wait reason and stack. A small test helper
(`internal/testbarrier`, test code only) reads it: a test now waits until the other
operation is seen parked at a mutex, or at a wait group, inside the function that takes
the order for that operation, and fails at once if it answers instead. Every forced case
of this slice uses it, in `internal/api`, `cmd/smd` and `internal/ft8` (and the one older
FT8 test in the same file whose comment said nothing observable existed).
- What it showed at once: the app start in G1 and G2 was waiting at its read. That is how
  the missing admit case above was found.
- What it rests on: the wording of the runtime's dump. If a Go release changes it, these
  tests fail ("neither answered nor was seen waiting"). I wrote here that they cannot pass
  wrongly; Codex's second review showed one way they could, closed in the next section.
- What is still only a look: an operation the test itself is holding (at the archive's
  write lock, or in a hook) is checked as unanswered without a wait. It is held by
  construction.

**Codex's three answers, and one correction of mine.**
- The later gap in FT8 is as described and the slot's mark closes it; no lock cycle in
  the mark's callers.
- A default change and an activation keep the admission's order for the whole request.
- The cases missing from the forced matrix: above.
- **Correction:** claude-464 said a hung completion job is bounded by `ft8QsoLogTimeout`.
  It is not: that limit is cooperative, and the write runs under a context that is not
  cancelled, limited by the database's own limits. The exit's wait has no overall
  deadline. None is proposed: a deadline could discard an earned contact. (The claim was
  in the exchange only, not in the tree.)

**Seen.**
- **Tests first.** The FT8 test of the earlier boundary, with an exit or replacement and
  with an operator's disarm, against the code as reviewed: both failed with no contact
  logged, as Codex's reproduction did. Its neighbour, a stop at the same point with our
  R-report held (the partner has not rogered), logs nothing before and after the fix.
- **Passing without having failed first:** the four added forced cases and the cancelled
  request. Each is shown able to fail below.
- **Reversion proofs,** in temporary copies since removed, each change checked as applied
  and the file restored and compared: fourteen, all failing for the reason claimed.
  - FT8, six: the stop not completing an earned contact; the completion not kept; the
    R-report rung treated as earned (the neighbour then fails: a contact not rogered is
    logged); the operator's Abandon made to complete it (the older Abandon test fails);
    the hand-over wait not waiting; the generation guard removed (the contact is logged
    twice). The third was first written as "every rung is earned", which failed in the
    fixture for another reason, and was narrowed.
  - The order, in `internal/api`, three: setup's step outside the order (G3 and both
    forced G4 cases fail); the exit letting go of the order in its drain; the exit not
    taking the order.
  - Through the daemon, four: the default change not ordered; the activation not
    ordered; the drain not waiting for the tracked jobs; a lifetime published to FT8 also
    lifting the archive's seal (both activation cases of G5 fail).
  - The cancelled request, one, five runs: the exit made to follow its request. The
    repeat is then a first exit, not an acknowledgement, in all five.
- **Gates, by exit code, on the final tree:** `gofmt`, `go vet`, `golangci-lint` (0
  issues), the maintainability metrics (0 regressions), `go test ./... -count=1` (67
  packages ok, the helper's among them), the documentation and context checks; the new
  and changed tests repeated under `-race` (API 8 times, daemon 4 times, FT8 20 times),
  and the FT8, archive and helper packages whole under `-race`. The desktop scenario
  against a daemon built from this tree passes its 19 checks. No transmission, keyed
  test or rig command was made: the FT8 tests run against the test keyer.

**Not shown.**
- **The earlier boundary through the real exit route and a real daemon.** The pause is
  made inside the FT8 service's own test fixture; the daemon's tests cannot reach that
  point. What ties them: the exit calls `RetireOperating`, which the FT8 test calls, and
  the daemon's tests show that a contact handed over is stored before the exit answers.
- **The disarm of an already idle service** (not armed, nothing in flight) uses the same
  call. No test reaches it with an earned contact pending: with the arm already gone, the
  earlier disarm has settled it.
- **The dump's wording on another Go release or platform.** Seen on go1.26.2,
  linux/amd64, with and without the race detector.

### Part 2, third slice: Codex's second review and what it changed (2026-11-20; committed `d49b0d4e`)

Codex confirmed the first repair (codex-204: its own reproduction now passes) and kept the
review open for two more things in FT8 and one qualification of the test helper. All
three are fixed here, in the same uncommitted change.

**A rogered contact was still lost if our closing rung had been deferred.** The first
repair kept the completion from the moment it was built, and it was built only where the
closing rung is transmitted. A handler takes the partner's roger in first and can then
return without transmitting: the roger was decoded too late into our slot, or our slot had
already carried a rung. The ladder is then on its closing rung with no completion kept,
and a stop before the next slot discarded the contact. The same shape is in all three
Group A ladders (answer a CQ, answer a type-4 CQ, work a Field Day caller).
- **The fix:** the contact is earned in the slot that takes the roger in. Each Group A
  handler keeps the completion as soon as its ladder is on the closing rung, before any
  return that defers the rung (`Sequencer.earnLocked`), and the transmit site uses the
  same completion. Transmit timing is unchanged: a deferred rung is still deferred, and
  the control test shows it is still sent once in the next slot and logged once.
- **Group B is untouched.** Its final rung earns nothing until it has been on the air; no
  Group B handler calls the new function, and a test of all four Group B ladders, each
  with its final rung deferred and then a stop, logs nothing.
- The completion's snapshot of the contact is now taken at the roger and not a slot
  later. Every field in it is fixed by then (the reports are final on the closing rung;
  the rest is pinned at the start of the session or the contact).

**Two callers of one completion shared the contact they stamp.** The completion stamps
the antenna path and the session's frequency before its generation guard decides which
caller logs. Since the first repair the stop can call it at the same moment as the slot
whose rung was refused. Codex's race run reported the two touching one contact. It is
more than a report: a test that makes the two overlap in a fixed order, with the live
antenna path changing between their stamps, logged the contact with the path stamped by
the caller that did NOT log it ("L" where "S" was live when the logging caller stamped).
- **The fix:** each call of the completion works on a copy of its own.

**The helper could accept the wrong waiter.** `Waiting` looked for any goroutine parked
with the named function anywhere on its stack and never tied it to the operation the test
was watching. Codex showed it: an unrelated goroutine parked there, the operation waiting
somewhere else, and `Waiting` returned as if it had seen the operation. So my sentence in
the last section, that these tests cannot pass wrongly, was false. The helper now
enforces the condition under which the goroutine seen is the operation:
- `Expect` is called before the operation is started and fails if anything is already
  parked there;
- `Waiting` requires exactly one goroutine parked there, and fails on two;
- a goroutine counts only if the named function is the one that called the lock, not any
  function further up its stack.
- Codex's case is now a test of the helper, with the two-waiter case and the case where
  nothing is seen at all. Every call site begins its watch before it starts the
  operation.
- **What it still does not identify,** and the package says so: a mutex's address, or a
  request. A test must start only one operation that can take that lock in that function
  between the watch and the wait, and the function must take one lock. The fixtures here
  each have one server and one such operation.

**Seen.**
- **Failing first,** against the tree as Codex reviewed it: the deferred closing rung, for
  each of the three Group A ladders and each of the two deferrals (six cases), and
  through the service's real stop for an exit or replacement and for an operator's disarm
  (two): all eight failed with no contact logged. The overlap test, in both orders: both
  failed with the other caller's stamp logged. The control (no stop: sent once, logged
  once) and the Group B test passed before and after.
- **Reversion proofs,** in temporary copies since removed, each checked as applied and
  restored: thirteen, all failing for the reason claimed.
  - FT8, seven: the roger not earning the contact, once for each of the three ladders
    (only that ladder's cases fail); one contact shared by the callers (both orders log
    "L"); the stop not completing; the completion built and not kept; the R-report rung
    earning the contact (the unrogered neighbour is then logged).
  - The helper, three: a watch accepted with something already parked; two parked
    goroutines accepted; any function on the stack counting.
  - Three of the earlier order proofs, repeated with the stricter helper: the exit
    letting go of the order in its drain (G1, G2 and the cancelled request fail); the
    default change not ordered; the drain not waiting for the jobs.
- **Not given a reversion proof:** the Group B test. No line of this change makes a Group
  B rung earn; the proof would have to add code, not remove it. The nearest shown is the
  R-report proof above, on a Group A ladder.

- **Gates, by exit code, on the final tree:** `gofmt`, `go vet`, `golangci-lint` (0
  issues), the maintainability metrics (0 regressions), `go test ./... -count=1` (67
  packages ok), the documentation and context checks. Under `-race`: the FT8, archive and
  helper packages whole, the FT8 earned and operating tests 30 times, the API cases 8
  times, the daemon cases 4 times. The desktop scenario against a daemon built from this
  tree passes its 19 checks. No transmission, keyed test or rig command was made.

**Not shown, in addition to the last section's list.**
- A slot time that does not parse, as a third deferral. The contact is earned before that
  return too; no test forces it.
- The deferred closing rung through the service for the type-4 and Field Day ladders.
  Those two are tested at the sequencer, where the fix is, by the call the service's stop
  makes (`abandonForStop`); the service-level test is of the standard answer.
- A third caller overlapping: a transmission's goroutine with a stop. The disarm waits
  for the transmission before it settles, so the two do not meet there.

**Codex's third reading (codex-205, 2026-11-20): both findings closed, no remaining
blocking objection to the slice.** Codex reran its three earlier diagnostics 30 times
under `-race` (all pass) and challenged the permanent tests itself: removing one ladder's
early earning fails exactly that ladder's two deferral cases; removing the private copy
fails both stamp-order cases with "L" where "S" is expected. It found no further return
between the roger and the kept completion, and accepted the helper's precondition for
these fixtures as conditional observation, not an identity of a mutex or a request.
- **One precision from that reading, checked in the code and recorded in `earnLocked`'s
  comment.** A kept completion stays with its own contact because each of the three
  Group A ladders zeroes the contact's flags and advances the generation where its
  session starts and where it ends. That is not true of every contact start: Call-CQ
  begins a contact inside a run without doing either (`commitCqContactLocked`). Call-CQ
  is Group B and earns nothing, so it is not a counterexample; a ladder that begins its
  contacts that way must not earn through `earnLocked`.
- **Still the operator's, decided by nobody here:** committing and pushing the slice;
  whether a deleted logbook's name stays reserved; whether an Abandon pressed during our
  closing 73 should log the contact the partner has already rogered (today it discards).

### Three rulings after the third slice's review (2026-11-20)

Put to the operator with a recommendation each (Claude's; Codex had recommended no answer
to the first two and had not seen the third). The operator, in chat:

> 1. Recommended is approved
> 2. Log it. If the remote station has a QSO, we need to have it our end.
> 3. Recommended is approved

**1. A deleted logbook's name becomes reusable. Approved; not built.** The recommendation
approved was: make the name reusable, as its own piece of work outside this slice.
- Today a delete marks the row and keeps it, and the name is unique across deleted rows
  too, so creating or renaming to that name is refused with "a logbook with that name
  already exists" when no such logbook is visible. Nothing restores a deleted logbook.
- **Larger than "small", which is what Claude called it when recommending.** The rule is
  written on the column (`name TEXT NOT NULL UNIQUE`, `0001_init.up.sql`). SQLite cannot
  alter that in place: the change is a migration that rebuilds the `logbook` table, which
  other tables refer to. It needs its own acceptance criteria and review before it is
  built, and it is not ranked: `docs/backlog.md` owns that.

**2. The operator's Abandon logs a contact the partner has rogered. Built, uncommitted.**
- **What the operator sees.** Abandon still ends the contact, cuts a transmission on the
  air short and leaves TX armed. If the partner had already rogered (a Group A exchange on
  its closing rung: answer a CQ, answer a type-4 CQ, work a Field Day caller), the contact
  is now logged, once, in whichever state our courtesy rung was: on the air, decided and
  not yet admitted, or deferred to a later slot. Nothing is keyed for it afterwards, though
  TX is armed. A contact the partner has not rogered is discarded, as before, and so is a
  Group B contact whose final rung has not been on the air.
- **The nearest confusable outcomes, each tested:** logged twice (the cut transmission's
  own completion arrives after the abandon); the abandoned contact's late completion
  ending the session the operator started next; the closing rung keyed after the abandon;
  a contact short of the roger logged.
- **The change.** The settle that the disarm's abandon made is now inside the one function
  both callers share (`Sequencer.abandonNamed` calls `settleEarned`), so a third caller
  cannot leave it out. `Sequencer.abandonForStop`, named in the two sections above, is
  gone: the disarm calls `abandonNamed` again, as it did before this slice. The abandons
  scoped to one generation are not changed: each is reached only after the rung's own
  completion has run.
- **Records changed with it:** invariant 1 in `internal/ft8/AGENTS.md` now names the
  operator's Abandon; `docs/ft8.md`; the abandon entry in
  `docs/v2-design/api-endpoints.md`.
- **Failing first:** eleven cases against the code as reviewed, each failing with no
  contact logged: three through the service's real Abandon (the closing rung on the air,
  not yet admitted, deferred), six at the sequencer (three ladders, two deferrals), and
  the test that used to require the discard, rewritten
  (`TestSequencer_AbandonDuringFinal73LogsOnceAndTheStaleCallbackAddsNothing`; it now also
  starts the next contact before the stale completion arrives). The not-earned neighbours
  passed before and after.
- **Reversion proofs,** in temporary copies since removed, each checked as applied and
  restored: five, all failing for the reason claimed.
  - The abandon not settling: every stop loses the contact, the disarm's cases included.
  - The operator's Abandon alone bypassing the settle: only the Abandon cases fail.
  - The completion's generation guard removed: an Abandon during the closing rung logs
    the contact twice.
  - The R-report rung earning: the contact not yet rogered is logged by the abandon.
  - The commit gate ignoring the generation: the closing rung is admitted after the
    abandon.
- **Gates on the final tree, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0
  issues), maintainability (0 regressions), `go test ./... -count=1` (67 packages ok, 0
  failed), `task docs:check`, the context check, `git diff --check`. Under `-race`: the
  FT8 and helper packages whole, and the earned, operating and Abandon tests 30 times. The
  desktop scenario against a daemon built from this tree passes its 19 checks. No
  transmission, keyed test or rig command was made.
- **Not shown.** The Abandon through the HTTP route and a real daemon (the tests call the
  service's own Abandon, as the route does). An Abandon in an auto-work run with an earned
  Field Day contact pending (the run's existing Abandon tests pass unchanged). The app's
  display after such an Abandon: no front-end change was made or tested; the contact
  arrives as any logged contact does.

**3. One sentence in `internal/ft8/AGENTS.md` reworded. Done.** "every start" is now "every
session start": a Call-CQ contact begun inside a run does not zero the per-contact flags.

### The Abandon change: Codex's review and what it changed (2026-11-20)

Codex reviewed the Abandon change (codex-206) and kept the review open for two findings.
Both were right; each was reproduced by a failing test of Claude's before anything changed.
It asked for no new ruling. It found no caller of the abandon holding a lock the claim
takes, and no new way for an unrogered or Group B contact to be logged.

**The Abandon waited for the log hand-over before it cut the transmission.** The first
version logged the contact inside the sequencer's abandon, and the service cut the
transmission only when that returned. Whoever takes the contact can hold the hand-over up,
and the carrier then stayed up for as long. (The daemon's own sink launches a job and
returns; Codex did not observe a prolonged carrier at a station. The defect is the order.)
- **The fix: a completion now has two steps** (`earnedClaim`, `internal/ft8/finalrung.go`).
  The claim stamps the contact and retires the session: state only. It returns the
  hand-over it then owes, the call that gives the contact to the log. The abandon runs
  them apart (`Sequencer.abandonNamedThen`): claim and end the session; cut the
  transmission; hand over. A transmission's own completion runs the two together.
- **Cutting first is not the fix.** The generation must move before the cut, or a rung
  still queued is admitted after the abandon (the commit gate of 2026-07-20). A reversion
  proof that cut first passed the whole package: nothing tested that order. A test now
  forces it: the abandon is stopped where it stamps the contact, the closing rung is
  admitted meanwhile, and the abandon must still cut it.

**A late refusal of an earlier rung discarded a contact earned since.** A rung is accepted
a slot before it is keyed, and the dial check beside the key can refuse it then. The
service ends that rung's session through an abandon scoped to its generation. A rung
before the closing one carries no completion of its own, and the generation does not move
when a later slot takes the partner's roger in. So the refusal found the same generation,
now with an earned contact, and discarded it. Claude's statement in the last section that
each scoped abandon "is reached only after the rung's own completion has run" was wrong:
true of the rung that asks, not of an earlier one. This loss is older than the Abandon
change.
- **The fix:** both scoped abandons (`AbandonIfCurrent`, `abandonNamedIfCurrent`) keep a
  contact their own session has earned (`endEarnedLocked`): after the generation check,
  never before it, so a stale rung cannot complete the contact of a session that replaced
  its own. The session's last frame still carries the reason it ended (the claim takes the
  reason and sets it in the lock hold that publishes it).
- **Not raised for such an end:** the `session.terminated` alarm. The contact is in the
  log; the same is already true when the closing rung itself is refused.

**Names.** `settleEarned`, named in the last section, is now `claimEarned`.
`Sequencer.AbandonThen(stopRF)` is the operator's Abandon for the service, which passes
the cut.

**Seen.**
- **Failing first,** against the code as it stood: the closing rung still on the air with
  the hand-over held (the transmission had not ended after two seconds; it ends at once
  now, with the hand-over still held); the late refusal through the service's real
  transmission, with no contact logged; both scoped abandons at the sequencer, for all
  three ladders, with no contact logged.
- **Passing before and after:** a scoped abandon of an earlier session leaves the next
  session's earned contact alone, which then completes normally.
- **Reversion proofs,** in temporary copies since removed, each checked as applied and
  restored: six, all failing for the reason claimed.
  - The hand-over before the cut: the carrier stays up while the hand-over is held.
  - The cut before the session is retired: the rung admitted meanwhile plays on after the
    abandon returns. (This one passed until its test was added.)
  - `AbandonIfCurrent` not keeping the contact: the late refusal loses it.
  - `abandonNamedIfCurrent` not keeping the contact: its three cases lose it.
  - The claim before the generation check: an abandon of the earlier session logs the next
    session's contact.
  - The reason not carried: the last frame has no reason.
- **Gates on the final tree, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0
  issues), maintainability (0 regressions), `go test ./... -count=1` (67 packages ok, 0
  failed), `task docs:check`, the context check, `git diff --check`. Under `-race`: the
  FT8 and helper packages whole, and the earned, operating and Abandon tests 30 times. The
  desktop scenario against a daemon built from this tree passes its 19 checks. No
  transmission, keyed test or rig command was made.

**Not shown.**
- `abandonNamedIfCurrent` reached with an earned contact through a real rung. Its call
  sites are a rung's own synchronous failures, where Claude found no way to an earned
  contact; it is tested by calling it.
- The cut ordered before the retirement for a session with nothing earned. That order is
  older than this change and is not changed by it; the new test needs the contact's stamp
  as its stopping point.
- The late refusal for the type-4 and Field Day ladders through the service (tested at the
  sequencer), and a refusal for a moved dial as against an unknown one.
- As before: the Abandon through the HTTP route and a real daemon; the app's display.

### The Abandon change: Codex's second review and what it changed (2026-11-20)

Codex closed the scoped-loss finding (codex-207): its diagnostics pass, and it found no
third place where a generation is checked and a session ended without its earned contact
being kept. It kept the cancellation finding open in one remaining form, which was right.

**Log lines were still written before the transmission was cut.** The last section says the
claim is "state only". It was not: it wrote the completion's log line before returning.
The abandon's own teardown did the same with its "session abandoned" line and its
termination note, and that one is older than this work: an ordinary Abandon, with nothing
earned, has always written its line before the cut. The daemon's log writer is called
directly, so a writer that is held up held the carrier up.
- **The fix.** The claim writes nothing: its log line moved into the hand-over. The
  teardown (`finishAbandonLocked`) now returns the report it owes, the log line and the
  termination note, and the abandon makes that report after the cut. Before the cut there
  is now only the stamp under the service's lock, the sequencer's lock, and the hub's
  publish, which does not block.
- **Checked by reading, for this finding not to return:** the functions that run before
  the cut (`retireSessionLocked`, `abandonLocked`, the terminal status, the stamp) write no
  log line and call no consumer. The publish reaches the partner sink only for a frame of
  a live exchange, and the abandon's frame is terminal. The disarm writes no log line
  before it cancels.

**A wording corrected at Codex's request.** The last section says of a session ended by a
refusal that "the contact is in the log". What is true is that it is earned and handed
over for logging: the sink's later write to the database can fail.

**Codex's answers to the other two questions.** No third scoped-loss site. No alarm for an
earned exchange completed this way is consistent with the existing path, the terminal
frame carrying the reason.

**Seen.**
- **Failing first,** with a real logger whose writer is held at the one line: the closing
  rung still on the air at the completed contact's line; our R-report still on the air at
  the abandoned session's line. Each ends at once now, with the writer still held.
- **Reversion proofs,** in temporary copies since removed, each checked as applied and
  restored: two, each failing its own test. The claim writing its line; the report made
  before the cut.
- **Gates on the final tree, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0
  issues), maintainability (0 regressions), `go test ./... -count=1` (67 packages ok, 0
  failed), `task docs:check`, the context check, `git diff --check`. Under `-race`: the
  FT8 and helper packages whole, and the earned, operating and Abandon tests 30 times. The
  desktop scenario against a daemon built from this tree passes its 19 checks. No
  transmission, keyed test or rig command was made.

**Not shown.** The auto-work run's "run stopped" line held at the writer (it is in the same
report, after the cut; no test holds it). The termination note held by its consumer (the
operator's Abandon makes none; the disarm has no transmission left to cut when it makes
one).

### The Abandon change: Codex's third review and what it changed (2026-11-20)

Codex confirmed the two log lines are now written after the cut (codex-208) and found one
more write before it, which was right.

**The hub wrote its eviction record before the cut.** The frame that ends a session is
published before the transmission is cut, in the lock hold that retires the session, as
invariant 3 requires. The hub does not wait on a subscriber, but for one whose buffer is
full it ends the stream and writes a record, inside the publish. With a subscriber full at
that moment and the log writer held up, the carrier stayed up. Claude's list in the last
section, "read, so that this does not return", named the publish as non-blocking without
reading the hub. Codex had relied on the same description and corrected it too.
- **How narrow it is:** it needs a subscriber's buffer full at the moment of the Abandon
  and a log writer that is held up. The hub's own note records 0 evictions in 15 days of
  the live log. It is fixed because the stop of RF is the one place where the order is
  owed, not because it has been seen.
- **The fix.** The service's Abandon asks the hub to hold its eviction records
  (`hub.holdEvictionReports`) and has them written once the cut is made. While held, the
  hub delivers and evicts exactly as always; only the record waits. The hold is also
  released when the Abandon returns, whatever happens, so it cannot silence the record
  for good.
- **Not changed:** eviction logging stays synchronous everywhere else, as the note on
  `logEvictions` decided (codex P2 on 57619abe; the operator's reasoning in
  `internal/events/hub_eviction_test.go`). This is one caller's ordering, not an
  asynchronous path.

**Seen.**
- **Failing first,** with the service's real hub, a subscriber whose buffer is full and a
  logger whose writer is held at the eviction record: the transmission still on the air,
  for the closing rung with the contact earned and for our R-report with nothing earned.
  Each ends at once now, with the writer still held.
- **The hub's own tests:** held, a publish evicts and delivers and writes nothing; the
  release writes the same record, once; a second release counts for nothing; after the
  release the next eviction is written by its own publish; with two holds the records
  wait for the last.
- **Reversion proofs,** in temporary copies since removed, each checked as applied and
  restored: five, all failing for the reason claimed. No hold in the Abandon; the records
  written before the cut; held records dropped; the first of two releases writing; a hold
  that never ends.
- **Gates on the final tree, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0
  issues), maintainability (0 regressions), `go test ./... -count=1` (67 packages ok, 0
  failed), `task docs:check`, the context check, `git diff --check`. Under `-race`: the
  FT8 and helper packages whole, and the earned, operating, Abandon and eviction tests 30
  times. The desktop scenario against a daemon built from this tree passes its 19 checks.
  No transmission, keyed test or rig command was made.

**Read this time, in full and not by search:** every function on the Abandon's path before
the cut (the service's lifetime check, the stamp, `retireSessionLocked`, `abandonLocked`
and what it calls, the terminal status, the hub's publish, the HTTP handler). Nothing else
writes, waits or calls out.

**Not shown, and one thing seen and left alone.**
- What wraps the Abandon's HTTP route before its handler runs was not read.
- The deferred release of the hold has no test that makes the Abandon fail part-way.
- **Seen, older than this work, not changed:** the dial guard writes a log line before it
  disarms (`onDialMoved`). The same held-up writer would hold that stop up. Whether every
  stop is to be audited for this is not decided here; the note on `logEvictions` takes the
  position that a stalled log destination belongs at the logging layer.

### The operator's direction to stop and commit (2026-11-20)

Claude put it to the operator that this was the third round on "nothing may be written
before the transmission is cut", each finding a rarer case, and that the dial guard has
the same pattern in older code. The operator, in chat:

> Good call. Let's stop here. Commit and push

- **Not done, by that direction:** no audit of the other stops of TX for a log line
  written before they stop. The dial guard's line before its disarm stays as it is.
- **Committed in two commits:** the slice as Codex closed it (`d49b0d4e`), then the
  Abandon change with its three rounds of fixes. Codex had not yet answered claude-471
  (the hub's eviction record) when the second was made.

### After the two commits: the clean-room reviews (2026-11-20 and 2026-11-21)

The automatic reviews of `d49b0d4e` and `8f6f8cb4` raised the same finding, and the first
raised a second. A power cut at the station ended the session of 2026-11-20 with the fix
built and its gates run; the next day the operator directed, in chat: "rerun the gates,
then commit and push".

- **[P1, both reviews] The stop looked for an earned contact in one hold of the
  sequencer's lock and tore the session down in another.** A slot that took the partner's
  roger in between the two earned a contact the stop then cleared, unlogged. Confirmed by
  reading. **Fixed:** the look and the teardown are one
  hold (`claimEarnedLocked`; `abandonNamedThen` looks again after a claim, and the pass
  that finds nothing still holds the lock for the teardown).
  - **No failing test.** Nothing observable sits between two holds of a lock. A test that
    raced the slot against the stop hit the gap 0 times in 24,000 rounds and was not kept.
  - **Gates on the fix, rerun 2026-11-21, by exit code:** `gofmt`, `go vet`,
    `golangci-lint` (0 issues), maintainability (0 regressions), `go test ./... -count=1`
    (67 packages ok, 0 failed), `task docs:check`, the context check, `git diff --check`;
    under `-race` the FT8 and helper packages whole and the earned, operating, Abandon
    and eviction tests 30 times. The desktop scenario against a daemon built from this
    tree passes its 19 checks. No transmission, keyed test or rig command was made.
- **[P2, `d49b0d4e`] A QSO submit refused by an exit's seal is told the wrong reason.**
  It answers 409 `default_change_pending`, "the default logbook is changing and the daemon
  is restarting". Confirmed by reading (`handler_qso.go`, `LiveEntryGate.Admit`). **Not
  built:** the code and the wording for a submit refused by an exit are the operator's to
  decide.
- **CI for `8f6f8cb4`:** success (run 44794759794), read from the completed run.
- **Not done:** Codex has not answered claude-471 or claude-472.

### Codex's reading of the three commits (codex-209, 2026-11-21)

Codex closed its last finding (the hub's eviction record) and read the lock-gap fix of
`696c28f0` as sound. Three qualifications, each checked in the code and accepted.

- **The retry in the abandon is bounded by its callers, not by itself.** After a claim
  the abandon looks again for an earned contact. That the second look finds nothing rests
  on the service's Abandon and both branches of its disarm holding the start gate, so no
  Group A session can start meanwhile; what can start without the gate, the auto-work
  pick-up, is the standard work ladder, which earns nothing. A caller without the gate
  has no such bound. Recorded in a comment on `abandonNamedThen`. A count as a bound is
  refused there: giving up would discard a contact that is owed.
- **The lock-gap fix has no regression test, and the wording for why is corrected.** The
  last section says "nothing observable sits between two holds of a lock". A lost contact
  is observable; what is missing is a deterministic barrier in that window, and the raced
  test did not force it. Neither Codex nor Claude reproduced the old gap or ran a
  reversion proof for that commit. The package passing is not coverage of it.
- **One more write before the cut, outside the service: the HTTP request line.** With the
  log level at debug, the request middleware writes "http request received" before the
  handler runs (`internal/api/middleware.go`), so a held-up writer can keep a valid
  Abandon from reaching the service at all. It is older than this work, off at the info
  level, and outside the service ordering these commits repaired. **Left, with the dial
  guard's line, by the operator's direction to stop** (2026-11-20); Codex recommends
  keeping the two together for when wider work on logging is selected.

**What the three commits do and do not establish** (Codex's scoping, accepted): the order
inside the service's Abandon. Not a total deadline for a stop, and not protection from
another holder of a lock that is itself stalled in I/O.

### Two recommendations awaiting the operator (2026-11-21)

The operator asked for Claude's recommendations on the two open items and for Codex's view
of them (claude-476, codex-210). Claude and Codex agree on both. **Neither is decided.**

**1. A QSO submit refused because the app is exiting** (the clean-room finding on
`d49b0d4e`).
- **Recommended by both:** a code of its own, 409 `operating_session_closing`, in place of
  `default_change_pending`, whose message says the daemon is restarting. The existing
  code keeps its name for a default change.
- **Wording, the operator's to choose.** The app puts "QSO not logged: " in front of the
  daemon's message and keeps the draft (Codex traced it: `lib/api/qso.ts`, `main.ts`,
  `lib/operate/qso.svelte.ts`). Candidates:
  - Claude: "Station Manager is closing, so nothing was logged. If it is still open, log
    the contact again."
  - Codex: "The app is closing. This attempt logged nothing. If closing fails and the app
    stays open, retry the contact." ("This attempt" claims nothing about an earlier
    attempt whose outcome is unknown.)
- **Agreed shape:** the reason is decided inside the same lock hold as the refusal, not
  looked up afterwards. With both holders, the default change's answer is given.
- **Holders of this gate: two, not three.** A default change and an exit. Archive
  activation seals FT8 and the rig, not live entry (`internal/archive/activate.go`);
  comments that group it with this gate overstate its wiring (Codex's correction).
- **Acceptance, from Codex:** each seal alone; both together; either release leaving the
  other's reason; a double seal and release; nothing written on a refusal; live entry
  admitted after the last release; the refusal shown through the app's own path. The API
  reference changes in two places: its conventions paragraph and the submit's error list.

**2. Where a deleted logbook's name becoming reusable is ranked** (approved 2026-11-20,
not built).
- **Recommended by both:** W-0024 (Settings → Logbooks) owns it, as an explicitly open
  follow-up of an accepted dossier. Its priority is set only in `docs/backlog.md`.
- **A correction to Claude's proposal:** O-a and O-b are routed to W-0012
  (`docs/dogfood-inbox.md`), so this is not a third member of a W-0024 list.
- **Timing is priority, not a technical need.** Claude proposed it after W-0029; Codex
  agrees with deferring it while the session work unblocks A2 and A3, and notes that
  nothing makes the migration depend on W-0029 finishing.
- **For its criteria, from Codex:** create and rename reuse a deleted name; two live
  logbooks still cannot share one; the new logbook has a new id and UUID, the deleted row
  is not revived; every row, identity, reference, index, constraint and the id counter
  survive the rebuild; a failed migration rolls back; name comparison and the forwarding
  bindings' name reservations are unchanged. And code as well as schema: provisioning
  checks the name across deleted rows itself (`provisioned_logbook.go`), and what it
  should do is to be decided, not assumed.

### The operator's rulings on the two, and what was built (2026-11-21)

The operator, in chat, on the two recommendations above:

> 1. Codex
> 2. go with recommendations

**1. The closing refusal: built.** "Codex" chose Codex's wording; the code was
the one both recommended.
- **What the operator sees.** A Phone / CW contact submitted while the app's confirmed
  exit has live entry closed is refused with 409 `operating_session_closing`: "The app is
  closing. This attempt logged nothing. If closing fails and the app stays open, retry the
  contact." The app shows it after "QSO not logged: " and keeps the draft. A submit
  refused by a change of the default is answered as before. With both holding the seal the
  default change's answer is given. Nothing is written by a refused submit.
- **The nearest confusable outcomes, each tested:** the exit's refusal borrowing the
  default change's answer (the defect); the exit's answer given while a default change
  also holds the seal; the wrong reason left after one holder lets go.
- **The change.** `LiveEntryGate.Admit` returns which holder refused, decided in the same
  hold of the lock as the refusal (`internal/api/live_entry.go`); the submit handler
  answers by it (`handler_qso.go`). No front-end change: the app already carries the
  daemon's code and message through.
- **Failing first:** the exit alone, and both holders with the default change then letting
  go, each answered `default_change_pending` against the code as it stood. The other six
  cases passed before and after: the default change alone; both, in either order; both
  then the exit letting go; a double seal and one release; a submit logged after the last
  release; a deliberate submit to another logbook.
- **Also tested:** the gate's own reason through every seal and release of both holders;
  the app's submit call carrying the new code and message unchanged.
- **Reversion proofs,** in temporary copies since removed, each checked as applied and
  restored: three, all failing for the reason claimed. One answer for both holders; the
  exit's answer winning when both hold; the handler giving the exit's refusal the default
  change's code.
- **Records changed with it:** `docs/v2-design/api-endpoints.md`, in its conventions
  paragraph and in the submit's error list. A comment on the gate that grouped archive
  activation with it is corrected: activation seals FT8 and the rig, not live entry.
- **Not shown.** The refusal through a real exit and a real submit together (the exit's
  own test shows its seal answers "closing" at the gate; the handler's test shows what
  that answer becomes). The toast in a running app.

**2. The name change: recorded, not built.** W-0024 owns it as an open follow-up, deferred
while the session work unblocks A2 and A3; its record and the points for its criteria are
in that dossier, and `docs/backlog.md` carries the deferral.

**Codex's review of the closing refusal (codex-211, 2026-11-21): no blocking objection.**
It found the holder rule sound for the two holders there are, the reason fixed in the same
lock hold as the refusal, and the split coverage adequate; it applied three overlays of
its own and each failed its intended assertion. It judged "parts 1 and 2 are built" a fair
line for W-0029's backlog row. Two small points taken: a test file's header still called
the gate's own seal the activation's hold (corrected), and, for the name change's
criteria, the rebuild preserves every *other* constraint, the name's uniqueness being the
one replaced (corrected in W-0024's dossier). Committing remains the operator's decision.

### Part 3: acceptance criteria (2026-11-21; questions 1, 2 and 4 ruled, 3 withdrawn; agreed with Codex, codex-215; nothing built)

On the operator's direction ("write part 3's acceptance criteria"). Read against ADR 0100 §4
and its updates of 2026-11-19 and 2026-11-20, the brief, parts 1 and 2 as committed, and
`internal/archive/activate.go`, `cmd/smd/archive_adopt.go`, `cmd/smd/main.go`
(`startGenerations`), `cmd/smd/lifecycle.go`, `internal/api/live_entry.go`,
`internal/api/app_admission.go` and `internal/ft8/servicetx.go` (`SealTxAdmission`) as they
stand. "Proposed" marks what the ADR and the brief leave open. The questions at the end are
the operator's.

**Revised after Codex's review (codex-212), which did not agree to the first draft.** Each
point was checked in the source and taken:
- **The arrival was not replayable.** The first draft let the pending selector in
  `config.json` remember that an arrival was owed. A start that falls back clears that
  selector before the generation that owes the arrival is built (`startGenerations`,
  `recordActivationFailure`). And an arrival that could not be written was to be "logged,
  and the admission runs the same tests": the admission runs the default-epoch test, not an
  arrival, so it would have kept the old session. **A is rewritten:** the arrival is now a
  test on what the served file holds, as the default-epoch test is.
- **Closing a session in a candidate that then fails to start** changed a file the app was
  never served. With the rewritten A no session of a candidate is closed before it is
  served.
- **C2 and G5 contradicted each other.** A deliberate submit to another logbook is admitted
  past the gate and is stored in the departing archive, in the closing phase too. G5 now
  forbids a live contact, not any row.
- **The 30-second bound was false.** The logging job's context has that limit, and the
  write deliberately does not inherit it (`cmd/smd/main.go`, the note on
  `ft8QsoLogTimeout`; `lifecycle_adapters.go`). D6 and question 2 no longer claim a bound.
- **Precision:** a refusal preserves the state before the request, whatever it was (B1); a
  failed switch releases only its own seals (C6); a gate drains an admitted submit that
  fails as well as one that commits (C1); and the state after a failed re-open is named and
  its recovery required (F3).
- **Question 1's wording** must not promise the retry lands in the archive asked for: the
  start may fall back.

**Revised again after codex-213,** which accepted those and found two sequences the second
draft got wrong. The second draft made the arrival a counter in each archive file, advanced
when an archive was left and when it was arrived at. Both sequences were checked in the
source (`abortAfterPersist`, `startArchivePromote`, `ResolveEffective`) and are real:
- a switch that failed with its selector left behind opens a new session at the counter's
  current value, and a later start that falls back then finds nothing to distinguish it;
- a promotion whose durability is uncertain can be undone by a power cut after the new
  archive has served a session, and the replayed activation would advance the counter
  again and close that session.
A counter cannot tell a new activation from the same one processed again. **A is rewritten
a second time: an activation has an identity,** and an arrival is the served file
recording a different one from the activation in effect (A1 to A4). The request no longer
writes anything of this to the departing file, and nothing is written to a candidate
before it is served. The cost is a change to the configuration's schema as well as the
log's. With it, question 3 falls away (below).

**After codex-214,** which accepted that contract for both of its sequences and for the
earlier points, and found two more things. Both were checked in the source and are right:
- **A pending selector written before this change needs an identity that is the same on
  every load.** The configuration's migration runs in memory at each start, and its write
  can fail softly or not survive (`persistResolvedConfig`). An identity made afresh by the
  migration could differ from one start to the next after the first had reached an archive
  file. A1 now derives it.
- **One activation can serve A, then B, then A again** when several configuration writes
  are lost in a row, and the last arrival in A is then not seen: A's file already records
  that activation. This is not repaired here. It is put to the operator as question 4,
  with what it would cost to close.

**Agreed with Codex (codex-215), within the limit the operator accepted.** Its design
review is closed; the review of the build is still to come. Three things it asks the build
to carry, taken:
- **"Any single interruption" is shorthand** for the isolated cases of A13, each starting
  from stores that agree. It is not a promise about a start that follows earlier failures
  left unresolved: the excluded chain itself contains a power cut.
- **What the tests must show, not argue:** the paired updates of the configuration; the
  file's marker and its close in one transaction; a failed arrival followed by a
  successful admission; the stable identity of an old selector; both replay controls with
  sessions that have contacts and an unchanged app lifetime.
- **The fixture for the accepted chain** asserts what does happen (the session carried on,
  or the question asked) and that every QSO is still in the archive it was logged in. It
  does not claim the boundary was repaired. When the behaviour's references are written,
  the exception is recorded beside them; a wider exactly-once claim is not restored.

**What part 3 is.** An archive switch ends the sitting in the archive being left, and the
archive that comes up starts a new one:
- at the switch request: live Phone / CW entry is closed, FT8 logging already earned is
  waited for, and the open session is closed in the departing archive's file;
- from that close until the daemon restarts or the switch fails, a **closing phase** in
  which no session is opened;
- when an archive becomes the served one: the **arrival**, by which a session still open
  in it from an earlier visit is closed;
- and, owed from part 2 by ruling: the default-epoch test as a step of every daemon start.

It has no membership (part 5), no New or Continue route (parts 4 and 6), no restore (the
backup and restore design) and no change to the UI. **It has one more log migration, 0021,
and a change to the configuration's schema, 7 to 8, each up only** (A1, A2).

**What is already there.** Part 1 built the close itself
(`CloseOperatingSessionForSwitchWithContext`, for a request and for an arrival). Part 2
put the activation in the one order with app starts and exits, and made live-entry seals
owned. Today an activation seals FT8 and the rig and nothing else: it does not close live
entry, wait for FT8 logging, or touch a session.

**What the operator sees.**
- **A Phone / CW contact submitted during a switch is refused,** nothing logged (ruled
  2026-11-12). Its code and wording are question 1.
- **A switch asked for just after an FT8 contact waits for that contact's logging** before
  it answers. How long that is has not been measured, and nothing bounds it today
  (question 2).
- Nothing else. Every session is still empty (no contact joins one until part 5), so each
  close discards its session and no start has a question to ask.

**Nearest confusable outcome.** The switch succeeds and the new archive works, while the
record is wrong underneath: the old sitting still open in the file that was left; a session
opened during the closing phase; a session from an earlier visit to the target archive
carried on as if it were this sitting; a live contact stored in the departing archive after
its session was closed; or a refused switch that ended the sitting. Every criterion is
judged on both files' rows and the admission's state together, through the real routes and
a real daemon start.

**A. The arrival: two durable identities compared, not an event to remember.**
1. **Proposed: an activation has an identity.** The switch request writes a new one into
   `config.json` with the pending selector. The start that processes the activation,
   whether it applies it or falls back, records that identity in `config.json` as the
   **activation in effect**, in the same write that makes the activation final or records
   its failure. The default change already carries a request identity in that file in the
   same way. The configuration's schema moves to 8.
   - **A pending selector with no identity** (one written before this change) is processed
     under an identity derived from the archive it names, the same on every load, so that
     a migration that fails to persist cannot turn one activation into two (codex-214).
     Tested with an old document loaded twice, its migrated form never persisted, and a
     session served in between.
   - **The fields that end an activation change together:** the selector and its identity
     removed, the activation in effect set, and any failure recorded, in one update of the
     configuration, so no reader sees some without the others. Memory may be newer than
     the disk when that write fails; the sequences below allow for it.
   - **No other save of the configuration changes these identities.** A settings save
     carries them through untouched.
2. **Each archive file records the activation under which it was last arrived at.** Log
   migration 0021 adds that one value. It is empty in every file at the upgrade, as the
   configuration's is, so the upgrade closes nothing.
3. **The test.** The identity this process works under is the pending activation's when
   the start is processing one, and otherwise the configuration's activation in effect. A
   served file that records a different one has been arrived at: in one commit, a session
   open in it is closed with `archive_switched`, whoever holds it, its open interval ended
   unconfirmed with no time (operator ruling 2026-11-20), an empty one removed, and the
   file's value set to the process's. Where this test and the default-epoch test would
   both close a session, the reason is `archive_switched`.
4. **Once for each activation, in each archive file.** The same activation processed
   again in a file that already records it closes nothing. A session opened after an
   arrival is not closed by a replay of that arrival. **The limit of this, accepted by the
   operator (question 4, 2026-11-21):** it treats an activation as one visit to each
   archive. The sequence in which one activation serves A, then B, then A again is not
   covered.
5. **Where the test runs:** in the step of every daemon start (A8), in the admission's
   start step, and in the session's other transitions and the logging step. A read changes
   nothing, as now (part 2, D3). So an arrival is derived from what the two stores hold;
   no single run is load-bearing, and neither the process nor the selector has to
   remember that it is owed.
6. **No recovery question after an arrival.** The first app start then finds no open
   session and is given a new one, even when the session closed had contacts (tested with
   member rows the test puts in).
7. **An ordinary start is not an arrival.** With no activation processed, the two values
   agree and a start closes no session for this reason: the same app, admitted again,
   holds the session it held (part 2, A1).
8. **The step of every daemon start.** After the archive's database is open and the
   activation, if any, has been made final or has failed, and before the HTTP server
   serves: the arrival test, then the default-epoch test (part 2, question 4), read back
   from the file with no app admitted. If it cannot be written it is logged and the start
   goes on: the admission runs both tests, and does not admit an app while the file cannot
   be read or written (ruled 2026-11-20). **An arrival that is owed is therefore never
   skipped by an admission that succeeds.**
9. **Nothing is written to an archive's file before it is served,** beyond the schema
   migration every opened archive already receives. No session of a candidate that fails
   to start is changed.
10. **Returning is an arrival.** A to B and later B to A is a new activation with a new
    identity: whatever is open in A is closed and a new session opened. Nothing is
    continued silently.
11. **Not claimed:** that a switch made outside an activation (the configuration edited
    or restored by hand so that another archive is active) is an arrival. Only an
    activation gets an identity; two files can record the same one.
12. **Only the served archive's sessions are read or changed.**
13. **The sequences that must hold,** each tested from the stores as it would leave them.
    Unless said otherwise each ends the same way: the archive then served has no session
    from before, and the first app start opens a new one with no question.
    - **The start killed before the activation is final:** the selector and its identity
      are still pending; the next start processes the same activation.
    - **The write that makes it final fails:** the daemon falls back to the archive it
      left, under the failed activation's identity, and the test closes whatever is open
      there. The candidate's file is unchanged.
    - **After the activation is final, a later step of the start fails:** the daemon
      starts again on the archive now active, which is the candidate (`Resolve` reads the
      promoted selector; codex-212), and the test closes its remnant.
    - **After the activation is final, the daemon killed before the step of A8:** an
      ordinary start; the configuration's activation in effect differs from the file's,
      and the test closes the remnant.
    - **The step of A8 unable to write, the file writable again later:** the admission
      closes the remnant before it gives the app a session.
    - **A promotion whose durability is uncertain, lost to a power cut after the archive
      has served** (codex-213). B was arrived at, a session T opened there with contacts,
      and the configuration then reverts to "A active, B pending" with the same identity.
      The next start processes that activation again. B's file already records it: **T is
      not closed.** With the same app lifetime, T's identity, the generation and its
      interval are as they were.
    - **A failed switch whose selector could not be withdrawn, then a later restart**
      (codex-213). The request closed the session in A; the restart request failed and the
      selector stayed (`pending_unclear`); a new session S2 opened in A (F2) and took
      contacts. A later start processes the retained activation. **If it falls back to A,
      S2 is closed by the test,** and the next app start gets a new session with no
      question. If it is applied, B's remnant is closed, and S2 stays open in A until A is
      next arrived at.
    - **A fallback whose record could not be written to `config.json`:** the selector is
      still on disk, so the next start processes the same activation. If it falls back
      again, the candidate never having been served, a session U opened in A in between is
      not closed again.
    - **The same activation applied twice** (the promotion lost and repeated, the candidate
      served both times): the session T opened in B in between is not closed again.
    - **Not guaranteed, an accepted limit (question 4, ruled 2026-11-21): one activation
      serving A, then B, then A** (codex-214). It is a fixture all the same, asserting
      what does happen, so that the limit is seen and does not widen unnoticed.
      The fallback's record cannot be written, so A serves a session U under X with the
      selector still on disk; a later start applies X with uncertain durability, and B
      serves; a power cut restores "A active, B pending, X"; B then fails and A is served
      again. A's file already records X, so U is not closed: it is carried on, or the next
      app start is asked Continue or New for it, where a new session was due. No contact
      is lost or filed in the wrong archive.

**B. The switch request, in order** (`POST /v1/qso-archives/{id}/activate`). Each step
refuses or fails before the next begins.
1. **A refusal changes nothing.** Every refusal of today is unchanged and comes before
   anything is sealed or closed: a competing archive operation, an activation already
   pending, a pending or unconfirmed default change, an unknown or already active
   archive, no restart port, a failed identity check. After each, everything is as it was
   before the request: the session or the absence of one, its generation, the
   configuration's selector and identities, and every seal exactly as held before. The fixtures include states in which something
   is already held or absent: a pending default change with its seals, a pending
   activation with its seals, and setup pending with no session.
2. **`tx_busy` changes nothing.** FT8 armed, a contact under way or a transmission in
   flight, or the rig transmitting, refuses as today. The live-entry gate has not been
   sealed by the switch and the session is untouched. The switch does not disarm FT8.
3. **Then the live-entry gate** (C), **then the FT8 completion drain** (D), **then the
   close** (E), **then the pending selector and the restart request**, as today.
4. **A success** answers as today. Stored after it: no open session in the departing
   file; the selector pending, with the activation's identity; FT8, the rig and live entry
   sealed by the switch until the process exits.

**C. Live Phone / CW entry at a switch.**
1. The switch seals the gate after the FT8 and rig seals. Sealing waits for every submit
   already admitted to finish. One that commits is stored before the close; one whose
   write fails stores nothing and does not block the switch.
2. A live submit arriving after the seal is refused and nothing is written (the QSO row
   and its upload rows are read). **A deliberate submit to another logbook is not the
   gate's to refuse,** as now: it is admitted and stored in the departing archive, in the
   closing phase too. Both are tested.
3. **The refusal says it is a switch** (question 1, ruled 2026-11-21): 409
   `archive_switch_pending`, with the message ruled. It is the third reason at this
   gate, beside the default change's and the exit's (`01c5c69a`), and it is decided in
   the same hold of the gate's lock as the refusal.
4. **Which answer when two hold the gate.** A switch and a default change cannot both
   hold it: each refuses the other today, and that is unchanged. A switch and an exit can:
   the switch's answer is given, as the default change's is, because a restart follows.
5. **No holder releases another's seal** (part 2, G5), now including the switch's seal on
   this gate: an exit that seals and releases while a switch holds leaves live entry
   closed; a switch that fails and releases while an exit is mid-way leaves the exit's
   seal.
6. If a later step fails (D to the restart request), the switch releases its own seals,
   this one among them. Live entry opens again only if no other holder has a seal.

**D. The FT8 completion drain.**
1. After the seals, the switch waits for FT8 completion logging already earned, by the
   same wait the exit uses: first a contact the decode loop is still handing over in a
   slot under way, then every logging job already launched. The FT8 seal is held
   throughout, so nothing new can be earned.
2. **Stored, not merely finished.** With a logging job held, the switch is seen waiting
   and the session is still open; let go, the QSO row and its upload rows are in the
   departing file, committed before the session's close. A job seen to end is not a
   contact stored.
3. **None escapes.** A contact earned before the FT8 seal, whose hand-over to its logging
   job comes after the seal, is waited for: forced with a late hand-over from a slot under
   way.
4. A logging job whose write fails stores nothing and does not block the switch.
5. None is cancelled by the switch.
6. **No limit of the switch's own** (question 2, ruled 2026-11-21). Nothing bounds the
   wait today: the job's 30-second context is not inherited by its write, which runs under
   the database's own statement and transaction limits. Should a limit ever be wanted, its
   expiry would have to refuse the switch before the close, release the switch's seals and
   leave the logging running; never go on to the close or the restart, and never cancel
   the contact's write.

**E. The close.**
1. One commit in the departing file: the open session closed with `archive_switched`, its
   interval ended as confirmed at the recorded time (ruled 2026-11-20); the generation
   advanced; an empty session removed. It is written the durable way (ruled 2026-11-20).
   It does not touch the file's record of the activation it was arrived at under.
2. It closes the session whoever holds it. A switch can be asked for by a caller with no
   app lifetime.
3. With no session open (setup pending, or a closing phase already begun and failed) it
   changes nothing and the switch goes on.
4. **If the close fails, nothing is requested.** The switch's seals are released (C6); no
   selector is written; no restart is asked for; the answer is an error with a code of
   its own. The session is still open and the sitting goes on.
5. One `operating_session.changed` event for a close, after its commit, as part 2's
   events are. None when no session changed.

**F. A failure after the close.** The close stands (an accepted cost, O9).
1. **The selector cannot be recorded, or the restart cannot be requested:** the answer is
   today's (`activation_persist_failed`, `restart_failed`, `pending_unclear`); the switch's
   seals are released (C6); the closing phase ends.
2. **Then one new session in the departing archive** for the app that is current, by the
   ordinary rule, before the switch answers. With no app current, none is opened, and the
   next app start opens it.
3. **If that open fails,** the switch's answer is still its own failure, and the
   admission is left as a failed start step leaves it (part 2, B4): the scopes stopped,
   authorizing nobody, and the app's read saying it is not current. The same app's next
   admission opens the session and publishes its scopes. Tested through the real routes:
   the read says not current; the admission succeeds; the read then shows the session.
4. **Retries do not pile up sessions.** A switch asked for again closes the session just
   opened; it is empty and is removed. After any number of failed switches there is one
   open session and no closed empty ones.
5. **An uncertain selector write** is a success, as today. Either truth is safe: if the
   selector did not survive, the next start is an ordinary start (H1).
6. **A selector that could not be withdrawn** (`pending_unclear`) is still an activation
   waiting for the next start, as today. The session opened by F2 is an ordinary one until
   then; what that start does with it is A13.

**G. The closing phase:** from the close until the process exits or the switch fails.
1. **No session is opened.** An app start in the phase is admitted, by the order ruled on
   2026-11-20 (not refused), and is given none. So is a second app replacing the first.
2. **The read route names the state.** Proposed: `archive_switching`, with no session and
   the generation the close left. It is a fourth state with no session, beside none, setup
   pending and recovery pending.
3. **An exit report in the phase is acknowledged without change:** the exit is recorded,
   no session changes, the app can close cleanly. It releases no seal the switch holds.
4. **Setup's first save in the phase opens no session.** The next start's admission
   opens it.
5. **No live contact is stored in the departing archive in the phase.** A live Phone / CW
   submit is refused (C2) and FT8 is sealed, so nothing that would join a session can be
   stored there. A deliberate submit to another logbook still is (C2): it is not a live
   contact and joins no session.
6. **The phase ends** when the switch fails (F), and then only. A daemon that exits ends
   it by ending.
7. New and Continue must be refused in the phase with a code of their own, changing
   nothing. Those routes do not exist yet; the requirement is owed by parts 4 and 6.

**H. A power cut, or the daemon killed, at the request.** The next start is tested from
the files as they would be left. (Interruptions of the start itself are A13.)
1. **After the close and before the selector is durable** (including an uncertain write
   that did not survive): an ordinary start on the departing archive. No session is open;
   the app start opens a new one with no recovery question. The closed session stays
   closed, with its contacts (tested with member rows).
2. **After the selector is durable and before the restart:** the start processes the
   activation (A).
3. **A remnant in the target** (a session an earlier crash left open there): closed by the
   test once the target is served, a new one opened, no question (A3, A6).

**I. The one order** (part 2, G). The request already takes the order of app starts and
exits; what part 3 adds is the close, the gate and the phase inside it. Forced by holding
one at a seen point and observing the other parked, never by sleeping.
1. **An exit against a switch,** both orders. Exit first: the switch waits, then finds no
   session to close and goes on. Switch first: the exit waits, then is acknowledged in
   the closing phase (G3). In both: one close of the session, by whichever came first; no
   seal released by the other.
2. **An app start against a switch,** both orders. Start first: its new session is the
   one the switch closes. Switch first: the start is admitted into the closing phase and
   opens nothing.
3. **Setup's first save against a switch,** both orders: a session opened is closed by
   the switch that follows; a save after the close opens nothing.
4. **A default change against a switch:** each refuses the other, as today.
5. **A live submit against the gate's seal,** both sides of it (C1, C2).

**J. What part 3 must not do, and the reference.** No member row is written. No New or
Continue route. No restore. No Station Event for a session. No UI change. The switch does
not disarm FT8.
`docs/v2-design/api-endpoints.md` gains, in the same change: the submit's new refusal (its
conventions paragraph and its error list), the read route's new state, the activation's
new failure code for a close that failed, and what an activation now waits for.
`docs/v2-design/config.md` gains the two identities and the schema's version.

**Evidence.** Tests first. The request and the phase through the API test server and its
real routes, with a clock the test sets; the start step and the cases of A13 and H through
the daemon's real start, with two archive files. Reversion proofs in temporary copies. The
usual gates and the desktop scenario. No transmission, keyed test or rig command: FT8 is
driven against the test keyer.
- **One measurement, not a criterion:** how long an FT8 completion takes to store. It can
  be read from timings the daemon already logs. Reading the station's log is read-only
  and still needs the operator's agreement for the occasion.

**Proposed slices.** First the two schema changes, the identity written at the request
and at the start, the test and the start step (A, H2, H3). Then the rest of the request
(B to F), the phase (G), H1 and the order (I). They are units of building and review.
**Whether the first goes to the station alone is decided when it is built** (Codex,
codex-212: the first draft's first slice was not ready for that). With the first alone a
switch does not yet close the session it leaves; that session is closed when its archive
is next arrived at, with its interval's end unconfirmed.

**Left to later parts, named so they are not lost.**
- **That a contact drained at a switch is a member of the session being closed,** for
  Phone / CW and for FT8. Part 3 can show only that it is stored before the close. Owed by
  part 5; a row count in part 3 does not prove it.
- New and Continue refused in the closing phase (G7): parts 4 and 6.
- The window that redraws when the state is `archive_switching`: part 6.
- A restore treated as a switch (ruled 2026-11-12): the backup and restore design. With
  A it is an activation with an identity of its own.

**Questions for the operator. 1 and 2 ruled 2026-11-21, each as recommended ("Both
agreed."; Rulings, above). 3 is withdrawn. 4 was ruled the same day: (a).**
1. **What a submit refused during a switch says.** The 2026-11-12 ruling is "Refuse, as
   recommended — like the default change". Since then the default change and the exit
   each have their own code and words. Codex and I both recommend a third code, 409
   `archive_switch_pending`. For the words I recommend: "The archive is being switched and
   the daemon is restarting. This attempt logged nothing. After the page reloads, check
   which archive is active before you log the contact again." The default change's "log
   it again once the page reloads" would, after a switch, put the contact in another
   archive without saying so; and the words must not promise it is the archive you asked
   for, because the start can fall back (Codex's point). The other choice is to reuse the
   default change's code and words.
2. **How long a switch waits for FT8 logging.** Nothing limits that wait today. Codex and
   I both recommend adding no limit: a limit could close the session and restart while a
   contact is still being stored. What you would see in the worst case: the Activate
   request in Settings waits, and the app's own 30-second request limit can pass first,
   showing the outcome as unknown while the daemon is still waiting; that is not a failed
   switch. If you want a limit, its expiry would refuse the switch and leave the logging
   running (D6); the length would be yours to set. A third choice, refusing the switch as
   busy whenever FT8 logging is in progress, departs from ADR 0100, which says the switch
   waits.
3. **Withdrawn (2026-11-21, after codex-213).** It asked what happens if the new
   archive's file cannot be written while the switch is being completed, and I recommended
   failing the activation and falling back. With the arrival rewritten, nothing is written
   to that file before it is served, so there is no such step to fail. What is left is
   already ruled: an app is not admitted while the file cannot be read or written, and
   stays on its waiting page (2026-11-20). A candidate whose database cannot be opened or
   migrated still fails and falls back, as today. Say so if you want this case treated
   differently.

4. **How far the arrival must hold when `config.json` cannot be written reliably** (from
   codex-214). **Ruled 2026-11-21: (a).** As specified, an arrival happens once, and exactly once,
   through any single interruption: a kill or a power cut at any point, a failed start, a
   fallback, or one configuration write that fails or does not survive. What is not
   covered is a chain of them: the daemon unable to record a fallback, then a switch
   applied by a write that does not survive, then a power cut, then the new archive
   failing to start. The worst that follows is a session carried on, or a Continue-or-New
   question, where a new session was due. No contact is lost or misfiled, and nothing
   reaches the rig. The choices:
   - **(a) Accept that limit and name it.** I recommend this. Each review round has found
     a rarer chain, and this one needs three or four separate faults in a row.
   - **(b) Keep the app on its waiting page after a switch attempt until the daemon has
     durably recorded how the attempt ended.** That closes the chain, because no session
     can then open on a record that may be undone. The cost is yours to weigh: with a
     `config.json` that cannot be written, the app could not operate after a switch
     attempt, where today the daemon serves the archive it fell back to regardless.
   - **(c) More mechanism:** a third durable record of which archive was last served, kept
     outside `config.json`. It would mean another schema change and another round of
     design and review, and I have not shown it closes every chain.

**For the operator's eye, not a question.** Part 3 now changes two schemas, where the
first draft changed none: the log's (migration 0021) and the configuration's (7 to 8). Both
go up only, and a build older than this one will refuse the newer `config.json`, as ruled
on 2026-11-18. I found no way to make an arrival happen exactly once across a failed
start, a fallback and a lost configuration write without giving the activation an identity
that both stores can hold; Codex's two reviews are why.

### Part 3, first slice: the arrival (2026-11-21; committed with this record)

On the operator's direction ("commit and build"). Criteria A, H2 and H3: the two schema
changes, the activation's identity at the request and at the start, the arrival test, and
the step of every daemon start. The rest of the switch request (B to F), the closing phase
(G), H1 and the order (I) are the second slice and are not built.

**What the operator sees.** Nothing new on screen. Underneath, after a switch the archive
that comes up no longer carries on a session left open in it by an earlier visit: that
session is closed before the app is served, and the app start opens a new one with no
question. **With this slice alone a switch does not yet close the session of the archive
it leaves** (slice 2 does that); that session is closed when its archive is next arrived
at, its interval's end unconfirmed.

**What is built.**
- **Log migration 0021:** `archive_metadata.arrived_activation`, the activation a file was
  last arrived at under. Empty at the upgrade. The schema's head is 21.
- **The configuration's schema, 7 to 8:** `pending_qso_archive_activation_id`, written and
  removed with the pending selector, and `qso_archive_activation_id`, the activation in
  effect. The step stamps only. A pending selector with no identity is processed under one
  derived from the archive it names (`Config.PendingActivationIdentity`); one identity with
  no selector is refused at load.
- **The request** (`internal/archive/activate.go`): a new identity is written with the
  selector in one update, and both are withdrawn together when the restart cannot be
  requested. The activation in effect is not touched by a request.
- **The start** (`cmd/smd/archive_adopt.go`, `endActivation`): the promotion and the record
  of a failed candidate each move the pending identity into effect in the same update that
  removes the selector.
- **The test** (`internal/database/sqlite/operating_session.go`, `start`): when the process
  has said which activation it serves the file under and the file records another, the
  open session is closed with `archive_switched`, its interval's end unconfirmed, and the
  file's value is set, in the same transaction. It comes before the default-epoch test. It
  runs in the admission's start step, in New, Continue and the exit, and in the logging
  step; a read does not run it. A process that has not said what it serves under (a tool
  opening a file) tests nothing.
- **The step of every start** (`operating-session`, a new lifecycle node after
  `archive-promote` and before `http`): it reads the activation in effect, tells the
  database, and runs both tests. A failure is logged and the start goes on.

**A departure from the criteria's wording, for review.** Part 2's F2 says one
`operating_session.changed` event for each advance of the generation. The start step can
advance it and publishes none: it runs before the HTTP server serves, so no window can be
listening, and a window reads the state when it connects. Nothing is lost by it; it is
named so that it is a decision and not an omission. **Codex supports it** (codex-216): the
rule is for transitions while the daemon is serving, and the API reference now says so. A
later admission's own close, and every other change made while serving, is still
published.

**Seen.**
- **Failing first:** the database tests against stubs with no arrival logic (nine of
  eleven failed on their own assertions; the two that must pass with or without it
  passed); the configuration tests (version, the derived identity, the stored identity,
  the refused lone identity); the request's tests (no identity written); the daemon's
  tests through its real start (the first run failed at the promotion, which did not yet
  end the activation).
- **Through the daemon's real start** (`cmd/smd/operating_session_arrival_test.go`, two
  archive files, sessions with contacts), the sequences of A13. (As first written this
  said "each sequence"; Codex's review, below, found one missing and one weaker than
  claimed. Both are in the list now.)
  - **a generation that fails after its activation was made final** starts again on the
    candidate, the activation in effect surviving the failure: with the arrival already
    written by the failed generation, and with it refused there and written by the next.
    The remnant is closed once; the archive that was left is unchanged;
  - an applied switch closes the remnant of the archive arrived at, and leaves the archive
    that was left untouched;
  - an ordinary start closes nothing;
  - the same activation applied twice keeps the session opened in between, with a stored
    identity and with a selector written before identities;
  - a fallback under a retained selector closes the session of the archive fallen back
    to, once: a second fallback under the same activation closes nothing more;
  - a promotion that cannot be written falls back under the same activation, the
    candidate's file unchanged, and the request is still on disk for the next start;
  - a kill after the activation is final is still an arrival at the next start;
  - a start that cannot write leaves the arrival to the admission, which is refused while
    the file cannot be written and closes the remnant once it can;
  - the default-epoch test runs at start;
  - **the accepted limit:** one activation serving Home, Contest, then Home again carries
    the session on, and no QSO is lost or moved. The test asserts that this is what
    happens.
- **Reversion proofs,** in temporary copies since removed, each checked as applied and
  restored: thirteen, all failing for the reason claimed.
  - The database, four: no arrival test; the arrival not recorded in the file; the
    interval's end confirmed; a process that has said nothing testing anyway.
  - The configuration, two: an old selector given no identity; a lone identity accepted.
  - The request, two: no identity written; the identity left behind by a withdrawal.
  - The start, five: the activation ended with nothing recorded in effect; a failure that
    only clears the pair; the start step telling the database nothing; the start step
    testing nothing; the session's node not before `http`.
- **Gates on the final tree, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0
  issues), maintainability (0 regressions), `go test ./... -count=1` (67 packages ok, 0
  failed), `task docs:check`, the context check, `git diff --check`. Under `-race`: the
  database, configuration, archive and daemon packages whole. The desktop scenario against
  a daemon built from this tree passes its 19 checks. No transmission, keyed test or rig
  command was made. After the gates one line was restored to the promotion (it clears the
  selector whatever it names, as before this change) and the daemon package rerun.

**Codex's review of the slice (codex-216): no production defect found; two demonstrations
incomplete and one test too weak. All three were right and are done; no production code
changed.**
- **A generation failing after its promotion was not forced.** The criteria name it (A13)
  and the test that looked like it started a fresh, ordinary generation instead. It is
  forced now with a lifecycle node of the test's own that starts after the promotion and
  the session's step and fails, synchronously, in the first generation. The daemon's test
  builder gained a hook for that; it is test code.
- **The repeated-fallback control did not hold what was claimed.** The session whose
  survival it proves had no contact, and the same app was not admitted again. It now has a
  contact, the same lifetime is admitted after the second fallback and holds it unchanged,
  and the comparison covers the interval's own sequence, start and holder and the
  session's members, not only its end state. The applied-twice control compares the same.
- **The settings-save test accepted a refusal.** For an ordinary save it now requires
  success and the setting changed, as well as the identities untouched.
- **Reversion proofs for these,** in temporary copies since removed, each checked as
  applied and restored: three, all failing for the reason claimed. A late failure wiping
  the activation in effect; a replay arriving again; every save refused.
- **Codex's answers:** reading the activation in effect after the promotion or the
  failure's record is sound in every generation; no session transition runs before the
  start step today, which is a statement about today's callers and not leave for a later
  one to rely on a process that has said nothing; the derived identity's form is
  acceptable within the upgrade case it is for.
- **Gates after these changes, by exit code:** `gofmt`, `go vet`, `golangci-lint` (0
  issues), maintainability (0 regressions), `go test ./... -count=1` (67 packages ok, 0
  failed), `task docs:check`, the context check, `git diff --check`; the daemon and API
  packages under `-race`.
- **Codex's second reading (codex-217): both findings closed, no new one.** It ran the
  daemon's arrival tests under the race detector and judged the new test to cover the
  boundary in both cases. The review of this slice is closed; it is not the end of part
  3: the departure's close and the closing phase are the second slice.

**Not shown.**
- **A configuration write of uncertain durability, as such.** The tests leave
  `config.json` as a lost write would leave it, by editing it; they do not make a
  directory sync fail and then cut the power.
- **`pending_unclear` through the real request.** The retained selector is written by the
  fixture; the request's own test shows it keeps its identity.
- **The API's admission after an arrival.** The daemon's tests call the database's start
  step, which is what the admission calls (part 2's tests cover that call).
- **A real restart by the service manager,** and anything at the station.
- **The settings save's test did not fail first.** A save has never carried these fields;
  the test guards that it stays so.

**Records changed with it:** `docs/v2-design/config.md` (the two identities; the 7 to 8
step); ADR 0100, *Update 2026-11-21* (the arrival's mechanism, the accepted limit, and the
two rulings for the request, marked as not yet built).

### Part 3, first slice: its clean-room review (2026-11-21; one finding, accepted by the operator 2026-11-22)

The clean-room review of `b0c636ba` raised one finding (P2): **one crash can close the
departing archive's session falsely.** A fallback stamps the archive it fell back to with
the new activation, durably. If both of `config.json`'s writes for that activation (the
request's selector and the fallback's record) were renamed but not yet confirmed on disk
when the power is cut, `config.json` comes back as it was before the request, the file's
stamp differs from the activation in effect, and the next start closes the session opened
after the fallback as `archive_switched`, though that start processed no switch.

**Not fixed; accepted by the operator (2026-11-22: "Accepted"; the rulings list).** What
follows is the state before that ruling, kept as it was put. The operator ruled on question 4 (2026-11-21, "a")
that the arrival holds through any single interruption and that the limit beyond it is
accepted and named. The chain named then was one activation serving Home, Contest and
Home again, whose worst result is a session carried on or a Continue-or-New question.
This sequence needs two configuration writes lost together with a file write kept, which
is a chain of the same kind, **but its result is a different one: a session closed on an
ordinary start.** Claude's reading is that the ruling's reasoning covers it; Codex
supports classing it as another excluded chain and holds, rightly, that the ruling does
not say so (codex-219). **Whether it is accepted is the operator's to say.** Its cost is
one session closed early and shown as closed by a switch: no contact is lost or moved,
and the next app start opens a new session. What would close it is among the choices not
taken on question 4: admitting no app after a switch attempt until its outcome is
durably recorded, or a third durable record of the archive last served. (As first written
this section and the code's comment called it accepted, and said that only a commit
across the two files could close it. Both were wrong; codex-219.) The sequence is stated
in the code, at `startOperatingSession` (`cmd/smd/archive_adopt.go`), with the ruling. It has not been reproduced by a test. CI for `b0c636ba` passed (run
44903934961).

### Part 3, second slice: the switch request and the closing phase (2026-11-21 and 22; committed with this record)

On the operator's direction ("commit, push and move on with the build 2nd slice"). Criteria
B to G, H1 and I. With it part 3's criteria are built.

**What the operator sees.** Switching the archive now ends the sitting in the archive
being left: its session is closed, with its end confirmed, before the daemon restarts. A
Phone or CW contact submitted while the switch is under way is refused in words of its
own (the ruled message), and one already being stored when the switch began is stored
first. An FT8 contact already earned is stored before the session closes. If the switch
cannot close the session it does not switch, and says so; the sitting goes on. If it fails
later, operation is given back with a new session and no question.

**What is built.**
- **The gate's third holder** (`internal/api/live_entry.go`): `SwitchOwner`, the switch's
  own seal on live Phone / CW entry, and `LiveEntrySwitching`, decided in the same hold of
  the gate's lock as the refusal. With more than one holder the answer is the default
  change's, then the switch's, then the exit's. Each holder releases only its own.
- **The submit's refusal** (`handler_qso.go`): 409 `archive_switch_pending`, the message as
  ruled on 2026-11-21.
- **The request** (`internal/archive/activate.go`): after today's refusals and the FT8 and
  rig seals, in order: the switch's seal on live entry; the wait for FT8 completion
  logging already earned; the close; then the selector and the restart request as before.
  A close that fails releases every seal and requests nothing (`session_close_failed`,
  503). A failure after the close (`activation_persist_failed`, `restart_failed`,
  `pending_unclear`) releases the switch's seals and then ends the closing phase. The
  manager reaches the session through a port (`SwitchSession`: `Drain`, `Close`, `Failed`),
  so `internal/archive` still imports nothing of the API.
- **The session's part** (`internal/api/operating_session.go`, `Server.SwitchSession`),
  called with the admission's order held. `Drain` is the wait a confirmed exit uses.
  `Close` closes the open session whoever holds it (`archive_switched`, the interval's end
  confirmed), publishes the change, and begins the closing phase. `Failed` ends the phase
  and opens one session for the app that is current; if that cannot be done the admission
  is left as a failed start step leaves it, and the app's next admission repairs it.
- **The closing phase** (`appAdmission.closing`): the start step opens nothing, so an app
  start or a replacement is admitted and given none; setup's first save succeeds and opens
  none; the read route answers `archive_switching`, named before any other state; the
  current lifetime's exit is recorded and acknowledged whatever generation it names.
- **Wiring** (`cmd/smd/lifecycle_adapters.go`): one line, `SetSwitch`.

**Three decisions made in the build. The operator approved them on 2026-11-22** (the
rulings list); they were put for review as follows.
1. **The failed close's code is `session_close_failed`, 503.** The criteria ask for "a code
   of its own"; `operating_session_unavailable` is the admission's and the exit's.
2. **An exit in the closing phase does not answer `operating_session_stale`.** G3 says it
   is acknowledged without change. The switch has closed the session, so there is none
   whose close a stale generation could confirm by mistake, and an app that has not yet
   heard of the close can still close cleanly.
3. **Both waits are made with the archive manager's lock held,** as the FT8 and rig seals
   already are. Neither a submit nor a completion's logging comes to the manager, so what
   waits behind them is another archive request (the bindings, a provision).

**Seen.**
- **Failing first,** each on its own assertion: the gate's and the submit's tests against
  a switch holder not yet told apart from the exit's (the answer was
  `operating_session_closing`); the request's tests against a port that nothing called;
  the session's tests against a port that did nothing. **The daemon's tests were written
  after the wiring and passed at once;** what shows they test it is the reversion proofs
  (the twentieth removes the wiring and ten of them fail).
- **The gate and the submit** (`live_entry_owner_test.go`, `default_guard_test.go` DG14):
  the switch alone; with an exit, either releasing first; with a default change; released
  with nobody else holding, a submit is logged; a deliberate submit to another logbook is
  never refused.
- **The request** (`internal/archive/activate_switch_test.go`, the order read from a trace
  of its ports): a success takes its steps in order, the selector written after the close
  and before the restart, and keeps its seals; three refusals touch nothing; FT8 busy and
  the rig busy leave live entry and the session alone; a close that fails requests
  nothing and a later switch succeeds; each failure after the close releases the seals
  and then ends the phase.
- **The session's part** (`internal/api/operating_session_switch_test.go`): the close of a
  session with a contact (after the drain, the interval confirmed, one event, the read
  naming the phase), of an empty one, of a remnant with no app current, and with none
  open; a close that fails changes nothing and begins no phase; in the phase a
  replacement, a repeat and setup's save open nothing and an exit is acknowledged at a
  generation from before the close, leaving the switch's seal; a failed switch opens one
  session for the current app, none with nobody current, and three failed switches leave
  one session; a failed re-open is repaired by the app's own flow through the real
  routes.
- **Through a real daemon** (`cmd/smd/operating_session_switch_test.go`):
  - a success: the session closed and confirmed in the file, the selector with its
    identity, the restart requested, the read `archive_switching`, a live submit refused
    in the ruled words with nothing stored, a deliberate submit to another logbook
    stored, TX admission sealed, an app start admitted with no session, its exit
    acknowledged;
  - refusals: under a pending default change its seals and the session are as they were;
    under a pending activation a second changes nothing and a default change is refused;
    with no session open none is made and nothing is sealed;
  - the switch waits for an admitted live submit, which is stored before the close, and
    refuses a later one; a live submit whose write fails does not block it;
  - **the drain:** with an earned FT8 contact held at the archive's write lock the switch
    is seen in the drain, the session still open and nothing requested; released, the QSO
    and its upload row are stored and the session is closed. It waits for a tracked job
    and cancels none. A completion whose write fails does not block it;
  - a close that fails: 503 `session_close_failed`, nothing requested, the session open,
    a live submit stored, TX admission not sealed, and a later switch accepted;
  - a failure after the close (the selector cannot be recorded): the close stands with
    its contact, one new session is open, live entry and TX admission are given back, and
    two more failed switches leave two sessions in all;
  - a failed re-open: the switch answers its own failure, the app reads as not current,
    its admission is refused while the file fails and opens the session once it can;
  - **H1:** the close committed and no selector written, the daemon stopped and started:
    an ordinary start, the closed session kept with its contact, and the app start opens
    a new one with no question;
  - **the order,** each with one side held at a seen point and the other seen parked: an
    exit first and the switch first; an app start first (the switch closes the session it
    opened: two advances of the generation) and the switch first (the start waits and is
    admitted into the phase).
- **Setup's first save against a switch** (API level, I3): the save first, and the switch
  first with the save seen parked.
- **D3, the late hand-over,** by one test and one source check, and not by a
  demonstration through the daemon's switch.
  - In `internal/ft8` (`TestSealTxAdmission_AContactBeingHandedOverIsWaitedForAfterTheSeal`;
    a test only, no FT8 code is changed): with a contact the sequencer earned still being
    handed over by the decode loop, the seal is refused while TX is armed; disarmed, it is
    taken; the hand-over wait after it is seen parked and returns only when the contact
    has been handed over.
  - **The gap Codex found (codex-219), and it was right:** every daemon test, the exit's
    included, begins with the logging job already counted, so none reaches the drain's
    first wait. With that wait removed from `drainFt8Completions` they all still pass.
  - **How it was closed, and how it was not.** A daemon test was written that held a slot
    open, saw the switch parked at the hand-over wait and then at the jobs' wait, and read
    the QSO stored before the close. It needed the FT8 service's own lock, so it needed an
    exported method in `internal/ft8` that existed for the test alone. **The operator
    decided against that (2026-11-22), and the method, its guard and that test are
    removed.** In their place `cmd/smd/completion_drain_source_test.go` pins the drain at
    its source: the function is embedded and parsed, and its statements must be exactly
    the hand-over wait and then the jobs' wait. It shows the wait is made and in what
    order. It does not show a contact held at that wait and then stored.
  - **Evidence obtained once, and evidence that remains.** The removed daemon test passed
    on 2026-11-22, also under the race detector, and failed with the hand-over wait
    removed (the switch answered 202 where it should have been waiting); Codex read it and
    ran it (codex-220). That is a demonstration made once. It is not a regression test:
    nothing in the suite repeats it. What remains in the suite is the FT8 test (a contact
    the sequencer earned is handed over under the slot's mark and waited for), the
    daemon's tests of the jobs' wait (the QSO and its upload row stored before the close)
    and the source check that joins the two. Codex prefers this to the method
    (codex-221, superseding its codex-220).
- **Reversion proofs,** twenty-two, each in a temporary copy since removed, each checked as
  applied, the tree's file unchanged: all fail for the reason claimed. The last is the
  daemon's drain without its hand-over wait: only the source check fails. The gate (the
  switch answering as the exit; its holder not told apart). The request (no live seal;
  the close before the drain; a failed close going on; a failure not ending the phase;
  the phase ended before the seals are released; the live seal not released; the old
  release on a persist failure). The session's part (the phase not begun; the close not
  published; the close as an arrival; a drain that does nothing; the start step running
  in the phase; setup opening in the phase; a stale exit in the phase; a failure leaving
  the phase; a failed re-open not stopping the scopes; the read not naming the phase; a
  failure opening nothing). The wiring removed.
- **Gates,** each by its exit code: `gofmt`, `go vet ./...`, `golangci-lint` (0 issues), the
  maintainability gate (0 regressions), `go test ./...` (67 packages), the race detector
  on `internal/api`, `internal/archive`, `cmd/smd` and `internal/ft8`, `task docs:check`,
  the context check, `git diff --check`, and the desktop scenario (19 checks). No transmission, keyed test or rig command.
- **The whole local gate, once, before the push** (the operator's direction of
  2026-11-22): `task ci:local` on this slice's code as committed, exit code 0, "All CI
  gates passed locally", 13 minutes. Only this record and `docs/current.md` were edited
  after it began; the documentation gates were run again after those edits.

**A flaw in the first slice's fixture, found by this slice's full run and fixed.**
`TestArrival_AFailureAfterTheActivationIsFinalStartsAgainOnTheCandidate` failed once
(`DROP TRIGGER refuse_close: database is locked`). Its raw connection had no busy wait,
and that statement runs while a generation of the daemon is starting on the same file.
That another start node's write met it is an inference from the error, not something
observed. The connection now waits; the arrival tests then passed thirty runs in a row.
`b0c636ba`'s own CI run did not meet it.

**Not shown.**
- G7 (New and Continue refused in the phase) is owed by parts 4 and 6: those routes do not
  exist.
- B1's fixture "setup pending with no session" is shown at the daemon with no session
  open and a default present, and for the close itself at the API level with no default.
- How long an FT8 completion takes to store (the criteria's one measurement) has not been
  read from the station's log: that needs the operator's agreement for the occasion.
- Nothing here has been run at the station.

### Part 3, second slice: its clean-room review, and the refusal reworded (2026-11-23; committed with this record)

The clean-room review of `df655a2b` raised one finding (P2), and it was right. A live
submit refused by the switch was told "the daemon is restarting" and to act "after the
page reloads". That answer is given from the moment the switch seals live entry, before
its drain, its close and its selector. If the switch then fails (the close cannot be
written, the selector cannot be recorded, or the restart cannot be requested) the seals
are released and nothing restarts: the operator who was refused is left waiting for a
reload that does not come. CI for `df655a2b` passed (run 44990005034).

**Fixed, on the operator's decision (2026-11-23; the rulings list).** The message promises
neither a restart nor a reload. It says what is true however the switch ends: nothing was
logged, and the archive is to be looked at before the contact is logged again.

- **Failing first:** the submit's test (DG14, `internal/api/default_guard_test.go`) and
  the daemon's (`liveRefusedBySwitch`, used by five cases) were changed to the new words
  and failed on the old ones. DG14 now requires the message exactly, and that it names
  neither a restart nor a reload.
- **Changed:** the message and its comment (`internal/api/handler_qso.go`), which now
  states the three ways a switch can end; two comments in `live_entry.go` that said a
  restart follows; `docs/v2-design/api-endpoints.md` (the submit's conventions paragraph
  and its error list).
- **Not changed:** ADR 0100's *Update 2026-11-21* quotes the first wording. It is a
  record of that day and is left as it was; this dossier and the API reference carry the
  current words.
- **Seen:** the named tests in `internal/api` and `cmd/smd` pass, and `internal/api` in
  full. The whole local gate, once, before the push: `task ci:local`, exit code 0, "All
  CI gates passed locally", 11 minutes. Only this file was edited after it began; the
  documentation gates were run again afterwards. Codex's review of the reword is complete
  (codex-222).

### Part 4: acceptance criteria (2026-11-23; its four questions ruled 2026-11-23; supported by Codex, codex-223; nothing built)

On the operator's direction ("write part 4's acceptance criteria"). Read against ADR 0100
§3 (the start of a lifetime, Recovery), §5 (several windows, the generation guard) and §6
(the recovery question); the brief's part 4, proposals E and F, and its line that the
wording of the question is put to the operator at this part; parts 1 to 3 as committed
(`internal/database/sqlite/operating_session.go`, `internal/api/operating_session.go`,
`internal/api/app_admission.go`); and `frontend/app/src/main.ts` as it boots today.
"Proposed" marks what the ADR and the brief leave open. The questions at the end are the
operator's.

**Revised after Codex's review (codex-222),** which supported the shape, the whole routes
(C), the held-lifetime rule (B1), the count (D) and the read gate (E2), and found four
things wrong or missing. Each was checked against the code and is right.
1. **The page's reads were under-specified** (E8): an event cannot recover a change made
   while the stream was down, and nothing stopped a late read from putting an old
   question back, or one archive's state from replacing another's.
2. **A stale answer does not always return the question or a held session** (B6, E6): the
   default-epoch test can close the remnant first, leaving no session open. Every state
   an answer can return now has its outcome on the screen (E5).
3. **The order against a switch covered only a switch that succeeds** (F3). Its two
   failure ends are added, with an answer against an answer and the setup-save boundary.
4. **"Nothing at the station until part 5" was too broad.** The question cannot appear
   until then. The read gate (E2) is on from the day it is installed.

**What part 4 is.** When the app did not close normally and left a session with contacts
in it, the next start asks before it shows the workspace: continue that session, or start
a new one. Part 4 builds the question and its two answers.

**What is already there.** Part 1 built both answers as transactions in the archive file
(`NewOperatingSessionWithContext`, `ContinueOperatingSessionWithContext`), with their
tests. Part 2 made the start step leave such a session alone, and the read route name the
state `recovery_pending`. No route gives an answer, and the page never reads the state.

**What the operator sees.**
- **At the station, the question cannot appear until part 5.** No contact joins a session
  yet, so every session that ends is empty and is discarded, and no start has anything to
  ask about. The tests put the member rows in themselves.
- **One thing does change at the station at once** (E2, question 4): when the page loads
  and cannot read the session's state, it says so and offers to try again, where today
  it would show the workspace with the daemon marked as out of reach.
- **Once it can appear:** after a crash, a power cut or a close the daemon never
  acknowledged, the app opens on one screen instead of the workspace. It names the session
  (when it started, how many contacts), says that Station Manager did not close normally,
  and offers Continue and New. There is no third choice. The workspace opens after the
  answer.

**Nearest confusable outcome.** The app opens straight to the workspace with the earlier
session silently carried on: it looks like a working app, and the next contact lands in a
sitting the operator believes is over. The others: the question on a clean start; the
question shown while the workspace is still reachable behind it; an answer that seems to
work and is applied twice, or is applied to a session the screen did not show.

**A. When the question is asked.**
1. **Asked:** the app's lifetime is current, and the archive has an open session with at
   least one member row held by another lifetime (the read route's `recovery_pending`).
   The member test is the one ruled on 2026-11-10: a row counts even if its QSO has since
   been deleted.
2. **Not asked:** a clean start (the earlier exit was confirmed: no session open); an
   empty remnant (discarded by the start step, part 2); an arrival after an archive switch
   (the remnant is closed first, part 3, A6); a first run (setup pending); a session the
   lifetime already holds (a reload, a daemon restart, the same app admitted again).
3. **Asked again** at every start until it is answered. Closing the app while it is
   unanswered resolves nothing (part 2, E: that exit is recorded and closes nothing).
4. Each of 1 to 3 is shown from the files: the fixture is the archive as each case would
   leave it, read through the real start step and the real read route.

**B. The two answers, as routes.** Names proposed: `POST /v1/operating-session/new`, body
`{generation}`; `POST /v1/operating-session/continue`, body `{generation, session_id}`.
1. **Who may answer.** The lifetime the admission holds, decided under the admission's
   order as the exit is, not from what a read would say. A request with no lifetime, a
   lifetime that has been replaced, and one whose exit is recorded are each refused with
   the code the admission already gives them, and change nothing.
   - **A held lifetime whose scopes a failed step left stopped may answer.** The answer
     is applied, and it repairs nothing: the scopes stay stopped, nothing is published to
     them, the admission's generation is as it was, and the app's own admission is still
     what repairs it (part 2, B4). Tested.
   - The gate for a session control is this rule, not FT8's authorization of a scope.
2. **The archive guard** applies (`X-SM-Archive-Id`, ADR 0100 §5): another archive's page
   is refused before anything is read.
3. **New, as the recovery answer:** the remnant is closed with `unconfirmed_exit`, its
   interrupted interval's end unconfirmed with no time; it is kept, with its contacts. A
   new session is open, held by the caller, bound to the default logbook. One commit.
4. **Continue, as the recovery answer,** naming the remnant: its identity and its members
   are kept; the interrupted interval's end is unconfirmed with no time; a new interval
   held by the caller is open. The session was never closed, so it has no close reason.
5. **The answer returns the state it left,** in the read route's shape, so the page draws
   from the answer and does not read again. That state, the count included, is the
   answer's own transaction's, taken under the admission's hold: no second read follows
   the commit, so no database work stands between a committed answer and the building of
   its response. Sending it can still fail: a response lost after the commit is E7 and
   G2.
6. **The generation guard (§5).** An answer at a generation that is not the current one is
   refused as stale (`operating_session_stale`, 409), applies nothing, and returns the
   current state. So: the same answer sent twice is applied once; a retry after a lost
   response is refused and returns the state the first attempt produced; two different
   answers at one generation, the first applied wins.
7. **The default-epoch test comes first (§5).** A session it closes is committed and
   published even when the answer is then refused as stale. One case at the route.
   - **The state returned then has no session open** (`none`): the remnant is closed with
     `logbook_changed` and the stale answer opened nothing.
   - **It is reached only by a fixture today.** Every writer of the default runs at a
     daemon start or at setup's first save, and neither can meet a remnant under a
     running, admitted app. It is specified because the transaction does commit that
     close.
   - **Nothing more is built for it: it is handling of a state no sequence reaches today,
     not a limit of the product** (Codex's description, codex-223, after reading the
     default's writers, the start order and the admission; it found no sequence either).
     Were it reached, the app would have no session until its next start, or from part 5
     until its next contact (the safety net). The reason to build nothing is that it
     cannot be reached. An opening there is possible, in the same transaction or after
     it, and is not needed now.
   - **Owed by a later change:** anything that lets the default's epoch move while the
     daemon serves an admitted app must then keep §3's rule that a running app is not
     left with no session.
8. **Refusals with a code of their own, changing nothing** (names proposed): Continue
   naming a session that is not eligible (`operating_session_ineligible`); Continue naming
   a closed session while a remnant awaits its answer (`operating_session_recovery_pending`:
   the question is answered first); New with no default logbook
   (`operating_session_setup_pending`); either answer in the closing phase of an archive
   switch (`operating_session_switching`; this is part 3's G7).
9. **A failure** (the answer's transaction cannot be written) is 503
   `operating_session_unavailable`. The transaction is rolled back: the remnant is still
   open and the question still stands. With B5 no database step can fail after an answer
   has changed something; a response that does not arrive is not this case (E7).
10. **One `operating_session.changed` event** for each answer applied, after its commit.
    None for a refusal that changed nothing.
11. **An answer touches nothing else.** It does not arm TX, start, stop or resume an FT8
    run, stop or publish an operating scope, release or take a seal, or move a contact.
    Read from FT8's state and the scopes' record before and after.

**C. The answers are the Session panel's controls too** (question 1). The two routes are
built whole, as part 1 built the transactions: New by the lifetime that holds the session
(`new_session`, the end confirmed), and Continue of a closed session of the default
logbook (`continued_other`, the rebinding of §8). Part 6 then adds the controls and the
list of sessions to choose from, and no restriction has to be taken out of the daemon.
Nothing in the app calls either use before part 6. Tested at the route for both uses.

**D. What the read route gains.** The question shows a contact count, and the read has only
`has_contacts`. Proposed: `contact_count`, the session's members whose QSO is not deleted.
`has_contacts` stays as it is (any member row), and it, not the count, decides whether the
question is asked. **A named consequence:** a remnant whose every contact was deleted
afterwards is asked about, showing 0 contacts. That follows the ruling of 2026-11-10 as
written. The count is read state, not a transition. Tested with a session of mixed
members (some deleted) and with one whose members are all deleted. A count that cannot be
read is an error, never 0.

**E. The screen.**
1. **In place of the workspace.** While the state is `recovery_pending`, the main window's
   page shows the question and nothing of the workspace: no Phone / CW entry, no FT8 view,
   no rail, no Settings (question 3). The page opens no FT8 event stream while the
   question shows, so this app has no presence in FT8 and cannot arm it.
2. **The state is read before the workspace is shown,** on every load of the page,
   whatever view the page would restore, an FT8 view included. A read that fails does not
   show the workspace as if there were nothing to ask: the page says the session could
   not be read and offers to try again (ADR 0100 §3: an unreadable state is never a clean
   exit). No timer is invented for the retry.
   - A failed read is never drawn as an empty session, and opens no FT8 event stream.
   - A retry that succeeds shows what its state calls for (E5), not always the workspace.
   - **The gate is at the load.** Once the workspace has been shown on this load, a later
     read that fails does not take it away: the page keeps what it shows and reads again
     at the next reconnection or event. That is for a read that fails. A later read that
     succeeds and says recovery is pending is drawn by E5 like any other.
3. **It names the session:** when it started, its contact count, and that Station Manager
   did not close normally last time. Times as the header shows them.
4. **Two buttons, Continue and New, and no "not now".** While an answer is in flight both
   are disabled.
5. **One rule draws every state,** whether it came from a read, an answer that was
   applied or a stale refusal:

   | State | The page shows |
   |---|---|
   | `recovery_pending` | the question, for the session and at the generation returned |
   | `open` | the workspace |
   | `setup_pending` | today's first-run gate, unchanged |
   | `archive_switching` | what it shows today: the daemon's seals guard that phase (part 3) and the page's own switch flow owns the moment. No question |
   | `none` | what it shows today, the workspace: no question stands. Two sources. A failed switch whose replacement session could not be opened (F3; reachable): the scopes are stopped, an answer leaves them so, and the app's own admission repairs it. And B7 (reached only by a fixture). From part 5 a contact logged then opens a session |

6. **After a stale refusal** the page draws the state returned, by E5, and sends nothing
   again by itself. So: the workspace, if the first attempt had been applied; the
   question again at the new generation, if one still stands, which may be for another
   session; the workspace with no question, in B7's case. The route test of B7 has its
   twin in the page's tests.
7. **After a failure** (no response, or 503) the page does not assume the answer was not
   applied. It reads the state again and draws it; if that read fails, E2's screen. If
   the question still stands the operator can press again, and that press carries the
   generation shown, which is the first press's when nothing changed (§5).
8. **The stream and the reads.** The page uses the shell event stream it already has and
   the archive it is already bound to.
   - It subscribes first and reads once the subscription is established, and it reads
     again at every reconnection: the stream replays nothing, so a change made while it
     was down is found by that read and by no event.
   - An `operating_session.changed` event makes it read again and redraw. So a question
     that no longer stands goes away without a reload.
   - **It never draws a state older than the one it shows.** Generations are compared
     within one archive: a read sent before an answer and arriving after it cannot put
     the old question back.
   - **A state for another archive is not drawn:** a response that belongs to the archive
     the page was bound to before does not replace the current one's.
   - Each of the four has its own case in the page's tests. No timer is added.
9. **Setup comes first.** A first run has no session to ask about; today's first-run gate
   is unchanged.
10. **The header's clock is not changed by part 4.** It is the page's own until part 6,
    which makes it read the open interval's start.

**F. The one order** (part 2, G; part 3, I). An answer starts at the admission's order,
like an app start, an exit, setup's first save, a default change and an archive switch.
Forced by holding one at a seen point and observing the other parked, never by sleeping.
1. **An answer against an exit of the same app,** both orders. The answer first: the exit
   then closes the session the answer left to this app. The exit first: the answer is
   refused, its lifetime retired; the remnant is still open for the next start to ask
   about.
2. **An answer against a second app start,** both orders. The answer first: the second
   start meets a session held by another lifetime, and asks if it has contacts. The start
   first: the first app's answer is refused, and the second app has the question.
3. **An answer against an archive switch,** both orders, and each way the switch can end.
   - The answer first: the switch closes what the answer left.
   - The switch first, and it succeeds: the answer waits, then is refused in the closing
     phase (B8).
   - The switch first, and it fails before its close (a refusal, FT8 or the rig busy, a
     close that cannot be written): the remnant and the generation are as they were, and
     the waiting answer is applied.
   - The switch first, and it fails after its close: the switch closed the remnant, ended
     the phase and opened one session for the app (part 3, F2). The waiting answer is
     stale and returns that session, open and held by this app: the workspace.
   - The same, where that session could not be opened: the answer is stale and returns no
     session; the scopes are stopped and stay so, and the app's own admission repairs it
     (part 3, F3). The answer publishes nothing to a scope in any of these.
4. **An answer against a default change,** both orders: the answer first, and the change
   is then asked for and accepted as today; the change in flight first, and the answer
   waits for it and is then applied. A change that is pending, accepted and awaiting its
   restart, does not refuse an answer.
5. **An answer against an answer:** New and Continue at one generation, from two requests.
   The first applied wins; the other is stale and returns the state the first left.
6. **Setup's first save:** before the default exists New is refused as setup pending
   (B8); after the save has opened the first session, an answer carrying the earlier
   generation is stale.
7. Parts 2 and 3 already show these transitions and this order. What part 4 tests is the
   new routes' place in it; the evidence for what is unchanged is not repeated.

**G. Restarts.**
1. **The daemon restarts while the question shows:** the remnant is still open; the app is
   admitted again and the question still stands, at the generation the file holds.
2. **The daemon restarts after an answer committed and before its response:** the app is
   admitted again and reads a session it holds. No question, no second session.
3. **The app is closed after Continue or New** by a confirmed exit: an ordinary close of
   the session it holds (part 2, E).

**H. What part 4 must not do, and the reference.** No member row is written. No list of
closed sessions, no Session panel control, no change to the rail, the panel, the Map or
the header's clock (part 6 and later). No wait for unfinished FT8 logging at app start
(ruled for part 5). No change to the shell: the question is the page's (proposal E), and
the shell's admission is as it is. No Station Event.
`docs/v2-design/api-endpoints.md` gains, in the same change: the two routes, their bodies,
answers and refusals; the read route's `contact_count`; and that `recovery_pending` is now
acted on by the app.

**Evidence.** Tests first.
- **The routes:** through the API test server and its real routes, with a clock the test
  sets, judged on the file's rows and the admission's state together; the order cases
  with `internal/testbarrier`; the switch case through a real daemon, as part 3's are.
- **The screen:** the page's own tests (`frontend/app`), with the daemon's answers given
  by the test: each state of E5, each answer, the stale redraw in its three outcomes, the
  failure, the four cases of E8, and the read gate (a failed read, then a retry that
  succeeds into the question and into the workspace), from a Phone / CW view and from a
  restored FT8 view.
- **What only Electron shows** (the brief): proposed, one scenario in the desktop harness
  against a real `smd`, on an archive file holding a remnant with a contact: the question
  is what loads, the workspace is not; after Continue the workspace is.
- Reversion proofs in temporary copies, once for the part. Named tests and the changed
  packages while building; the whole local gate once, before the push (ruled 2026-11-22).
  No transmission, keyed test or rig command.

**Proposed slices.** First the daemon: the two routes, the read's count, the refusals and
the order (A to D, F, G). Then the screen and the desktop scenario (E). Each reviewed and
committed on direction before the next.

**Questions for the operator. All four ruled 2026-11-23, each as recommended ("go with
recommendations"; the rulings list).**
1. **Build the two routes whole now (C), or the recovery answers only?** I recommend
   whole. The transactions have covered both uses since part 1; a route limited to
   recovery would need a refusal that part 6 then removes. Nothing in the app can reach
   the other uses until part 6 adds the controls.
2. **The words on the screen.** Proposed, for you to change:
   - Heading: "The last session is still open"
   - Text: "Station Manager did not close normally last time. The session that started
     {date and time} has {n} contacts and was never closed."
   - Buttons: "Continue this session" and "Start a new session"
   - Under them: "If you start a new one, the earlier session is closed and kept, with
     its contacts."
3. **What else is on that screen?** I recommend nothing: no Settings and no rail, so
   nothing can be logged or changed before the answer. The cost: an operator who wanted
   to change a setting first has to answer first. Either answer is safe for that; neither
   loses a contact.
4. **The read gate at the page's load (E2).** It changes what you see at the station from
   the day part 4 is installed: if the page loads and cannot read the session's state, it
   shows "the session could not be read" with a button to try again, and not the
   workspace. I recommend it: ADR 0100 says an unreadable state is never treated as a
   clean exit, and once contacts join sessions a workspace shown on a failed read could
   log into a sitting you believe is over. It applies at the load only; a workspace
   already showing is not taken away by a later failed read. Codex supports it
   (codex-222).

**For the operator's eye, not a question.** The recovery question cannot be seen at the
station until part 5, so that much of part 4 has no station acceptance on its own; the
first time it can be seen there is with parts 5 and 6. The read gate can be seen at once.

### Part 4, first slice: the two controls, the count and the order (2026-11-23; committed with this record)

On the operator's direction ("commit, push and build part 4's first slice"). Criteria A to
D, F and G: the daemon's side. The screen (E) and the desktop scenario are the second
slice and are not built.

**What the operator sees.** Nothing: no part of the app calls the new routes yet, and no
contact joins a session until part 5. The daemon can now be told Continue or New.

**What is built.**
- **The contact count** (`internal/database/sqlite/operating_session.go`):
  `OperatingSession.ContactCount`, the session's members whose QSO is not deleted, read
  in `state` in the same transaction as the session it describes. `HasMembers` is
  unchanged and still decides emptiness. The count is kept out of the read the logging
  step makes for every contact.
- **The two routes** (`internal/api/operating_session_control.go`, new):
  `POST /v1/operating-session/new` and `POST /v1/operating-session/continue`, whole
  (ruled 2026-11-23). Both sit behind the archive guard, are decided in
  `answerOperatingSession` with the admission's order held from start to finish, and
  call part 1's transactions unchanged. The state answered is the transaction's own,
  named under the same hold.
- **The read** (`internal/api/operating_session.go`): `contact_count` in the session.

**The codes, as built.** `operating_session_stale` (409, with the current state beside
the error fields as `operating_session`), `operating_session_ineligible`,
`operating_session_recovery_pending`, `operating_session_setup_pending`,
`operating_session_switching` (part 3's G7), 503 `operating_session_unavailable`; and for
the caller `app_lifetime_required`, `app_lifetime_retired`, and one the criteria did not
name: **`app_lifetime_not_current`** for a lifetime this process does not hold and has
not retired (FT8's existing code for the same fact). **One difference from the criteria's
wording, for review:** B8 said Continue naming an unknown session is ineligible. While a
remnant awaits its answer, any session named other than the remnant is refused as
`operating_session_recovery_pending`, a session that does not exist included: part 1's
transaction decides it so, and the question is answered first either way.

**Seen.**
- **Failing first:** the database's count test against a field with no query behind it;
  every route test against a daemon with no such route (404) and a read with no count.
  The order tests and the daemon's were written after the routes and passed at once; the
  reversion proofs are what show they test them.
- **The count:** in the database (three contacts, one deleted, then all: 3, 2, 0, with
  members throughout; the count in a control's returned state) and through the read
  route (a mixed session; a remnant whose contacts were all deleted is still asked
  about, showing 0). **A count that cannot be read is an error by construction, and not
  by a test:** it is a statement of the state's own transaction, and its failure fails
  the state. Nothing can make that one statement fail while the rest succeeds.
- **The two recovery answers:** New (the remnant closed with `unconfirmed_exit`, its
  sitting's end unconfirmed with no time, kept with its contact; one new session held by
  the caller) and Continue (the same session, never closed, the interrupted sitting
  unconfirmed, a new one the caller's). Each: the state answered equals the read after
  it; one event; the scopes, the live-entry gate, the drain and the admission's
  generation as they were.
- **Who may answer:** no lifetime; a lifetime replaced in this process; one whose exit
  the file records, after a daemon restart; one never admitted. Each refused for both
  controls, nothing changed. **A held lifetime left stopped by a failed replacement
  answers,** and the answer repairs nothing: the scopes stay stopped, the app still
  reads as not current, and its own admission then repairs it without changing the
  session.
- **The archive guard and the body:** another archive's page, no archive named, no
  generation, Continue with no session.
- **The generation guard:** the same answer twice (applied once; the repeat is stale and
  is given the first's state, held by the caller; nothing more published); two answers
  at one generation; and B7 by fixture, for both controls: the remnant closed with
  `logbook_changed`, committed and published once, the answer stale, the state returned
  `none`.
- **The refusals,** each changing nothing: ineligible (the session the caller holds; one
  that does not exist); a closed session named while a remnant waits; New before setup;
  both controls in a switch's closing phase. **A failure** (the file cannot be written):
  503 for both, the question unchanged at its generation, and the same press then
  applied.
- **The routes whole:** New by the holder (a session with a contact kept, closed
  `new_session`, its end confirmed; an empty one discarded); Continue of a closed
  session (the current one closed `continued_other` and keeping its own contact; the
  earlier one open again with its own).
- **Restarts:** the question still stands at the file's generation; an answer whose
  response was lost is found applied, with no second session; a confirmed exit after an
  answer is an ordinary close.
- **The order, forced** (`operating_session_answer_order_test.go`; one side held, the
  other seen parked at the order): against an exit of the same app, both orders; against
  a second app start, both orders; an answer against an answer. A held control is held
  at the archive's write lock, inside the order, and the test sees the order taken
  before it sends the other side.
- **The setup-save boundary, as a before and an after, not a forced wait:** before the
  default exists New is refused as setup pending; after the save has opened the first
  session, a control carrying the earlier generation is stale and is given that
  session.
- **Through a real daemon** (`cmd/smd/operating_session_answer_test.go`), from a remnant
  left by a real stop and start:
  - against an archive switch, each way it can end: the answer first; the switch
    succeeds (refused as switching); it fails before its close (the waiting answer
    applied); it fails after its close (stale, given the session the switch opened,
    held by this app); the same with that session not opened (stale, no session, the
    app still not current);
  - against a default change: the change in flight first (the answer waits and is then
    applied; under the pending change a further New is applied too, and the change's
    seals are as it left them); the answer first, then the change accepted.
    **The second is sequential, not forced:** a default change touches no lock a held
    answer could be seen at from outside the daemon. That a default change takes the
    same order is part 2's G6.
- **Reversion proofs,** fourteen, each in a temporary copy since removed, each checked as
  applied, the tree's file unchanged: all fail for the reason claimed. The count (it
  includes deleted contacts; it is not in the answer). Who may answer (a replaced
  lifetime not refused as retired; a recorded exit not looked at; a lifetime not held
  answering; a stopped held lifetime refused). The phase (answered in it). The answer
  (no event; a stale refusal with no state; the state named for nobody; stale answered
  as applied). The request (no archive needed; Continue needing no session). The order
  (not held through the control).
- **Checked while building,** by the practice ruled on 2026-11-22: the named tests, then
  `internal/api`, `internal/database/sqlite` and `cmd/smd` in full, `gofmt` and `go vet`
  on them, and the session tests of `internal/api` and `cmd/smd` under the race
  detector.
- **The whole local gate, once, before the push:** `task ci:local`, exit code 0, "All CI
  gates passed locally", 11 minutes 42 seconds, on the tree as corrected for Codex's
  review (codex-224; its review is closed, codex-225). A first run was stopped a minute
  in so that those corrections were gated too. Only this line was added to this file
  after the run began, and the first files of the second slice were added under
  `frontend/app/src` after the page's own stage of the gate had passed; they are not part
  of this commit.

**A fault in my own test, found by the full package run and fixed.** One order case (an
answer first, a second app start behind it) failed every time when run in the full
package, though it had passed alone. The bed's usual request helper reads the default
logbook from the database for a header the admission routes do not look at; that read
did not return while the held control waited at the write lock, so the second start was
never sent and never seen at the order. The case now sends that start as bare requests,
and passed fifteen runs in a row.
- **Why, by Codex's reading of the source (codex-224), which I checked:** this fixture
  gives the server's database service one connection (`MaxOpenConns: 1`,
  `internal/api/handler_test.go`). The held control has it pinned while it waits for the
  write lock; the helper's read cannot begin; and the test releases the lock only once it
  has seen the start at the order. Each waits for the other. When the helper won the
  connection first the case passed, which is how it passed alone. Codex did not recreate
  the failure; this is a reading, not a capture.
- **What it does not show:** a read starved in the daemon. A read there can wait while
  the pool is busy, as any does; the dead end here needed the test's own rule for
  releasing the lock. No change to the pool or to the order follows from it.

**Not shown.**
- Anything of the screen (E), and the desktop scenario: the second slice.
- A4's cases of when the question is and is not asked are part 2's and part 3's tests
  (a clean start, an empty remnant, a first run, a session the lifetime holds, an
  arrival); they are not repeated here.
- Nothing has been run at the station.
