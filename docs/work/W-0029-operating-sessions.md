# W-0029 — Daemon-owned operating sessions: building ADR 0100

**Status:** Opened 2026-11-18 (operator: "W-0029 is fine, open the dossier"); ranked in
[`docs/backlog.md`](../backlog.md). The build brief is drafted for review (operator, the same
day: "start the ADR 0100 brief") and none of its questions is ruled. Nothing is built. **The
build waits for the removal of the operating-client switch** (operator, 2026-11-18, below):
nothing here is to be built or tested for the check's off state. Each part starts on the operator's direction,
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

The design's own rulings are in ADR 0100 and are not repeated here.

## Record

### Build brief (2026-11-18; revised once after review; nothing ruled)

Written on the operator's direction ("start the ADR 0100 brief"). It is a plan for building an
accepted design, for review by the operator and Codex before any code. ADR 0100 decides the
behaviour and is not restated; where this brief proposes an order, a name, a shape or a
mechanism, that is a proposal. The questions at the end are the operator's. Codex's first
review (codex-146) found three things, all accepted: the first draft would have switched
membership on at the station before anything could ask Continue or New; its exit step checked
and waited where it had to exclude; and starting at the admission needed a rule that survives
a daemon restart and a lost answer. The order, proposals A, C and D, and the questions are
rewritten for them.

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
  daemon restart forgets it and the running app is admitted again. With the
  operating-client check off there is no lifetime at all. The shell sends nothing when it
  quits (`desktop/src/main.mjs`).
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
  current. I propose the session's start step runs there, after the scopes are stopped and
  before the lifetime is published, not as a request of its own. The admission is in memory
  and the same app is admitted again after every daemon restart, so the step decides only
  from what the archive file holds. For a lifetime L becoming current:

  | The archive holds | The step does |
  |---|---|
  | an open session held by L | nothing. This is a reconnect, a daemon restart, or a retry after a start that committed and whose answer was lost |
  | an open session held by another lifetime, with members | nothing. The state is "recovery pending": L does **not** hold it, and nothing continues it until the operator answers (part 4). This is the one case in which an admitted app holds no session |
  | an open session held by another lifetime, with no members | waits for completion work the earlier lifetime had earned (ADR 0100 §3), judges again, and if still empty discards it and opens a new one held by L |
  | no open session, and L's own exit is recorded (C) | refuses the admission as retired. A committed clean close is never reopened |
  | no open session otherwise | opens a new one held by L |

  Every row is idempotent: running it twice gives the state of running it once. An
  unreadable state fails the admission, which the shell shows as its waiting page.
- **B. Withdrawn.** The first draft stored a digest of the lifetime. The lifetime is "a
  label, not a secret" (`internal/api/app_admission.go`), so the record holds the label, and
  the current-lifetime and generation checks are what protect an action, as today.
- **C. A recorded exit retires the lifetime, durably.** The close of a confirmed exit records
  which lifetime exited. Retirement in memory is not enough: after the close commits, the
  answer is lost and the daemon restarts, the retired set is gone and the still-exiting app
  would otherwise be admitted and given a new session. With the record, row 4 of A answers
  it "retired", which that app's close flow takes as its acknowledgement. The app invents no
  new action to learn what committed.
- **D. One close protocol for a confirmed exit,** made of the gates that exist, in the order
  an archive switch uses (ADR 0100 §4):
  1. **Validate:** the caller is the current lifetime, the archive is the served one, and
     the generation is the expected one. A refusal here has sealed nothing.
  2. **Exclude new work:** under the admission's lock, seal and stop every scope by the
     replacement's own step (a no-op on a scope the app has already disarmed, and the
     backstop if it has not), then seal the live-entry gate, which waits for Phone/CW
     submits already admitted and refuses later ones with its own code.
  3. **Drain what was earned:** wait for the FT8 completion jobs already launched. The drain
     holds nothing those jobs need in order to finish.
  4. **Close:** one transaction closes the session (discarding it if empty), records the
     exit for C and advances the generation.
  5. **Then, before any answer:** retire the lifetime in the admission, release the
     live-entry gate, publish, answer.
  - **On a failure or a cancellation in 2 to 4:** release the gate, publish the lifetime to
    its scopes again, leave the session open, and answer the error. The exit is then
    unconfirmed unless the app retries.
  - **No contact is refused or cancelled by this.** A submit admitted before the seal
    commits into the session being closed. A completion job is never cancelled; its write is
    not bound by the job's own time limit today (`context.WithoutCancel`), so the drain's
    bound is the database's own limits, and no exit timeout is invented here.
  - **A finished job is not a stored contact.** The tests look at the rows: the QSO, its
    upload-queue rows and its membership in the closed session. A completion whose write
    failed is not stored, as today, and does not block the close; that case has its own test.
  - **Against the other transitions:** a replacement, New, Continue, an archive switch and a
    default change each take the admission's lock or the session's serialization first, so
    they are ordered against an exit and none interleaves with it.
- **The lock order, to be fixed in part 2 before A is wired:** the admission's lock; then a
  scope's own gate; then the live-entry gate; then the session's serialization; then the
  database transaction. Logging takes only the last three, in that order. Membership uses
  the QSO's own transaction, handed to it as the upload-queue write is.
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
  over the real listeners. There is no "check off" case: that state is removed first.
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

**Questions for the operator. None is ruled.**
1. **The order and what is switched on when** (the table above): parts 1 to 4 and A3 first,
   each on at the station and none of them showing; then membership and the panel together.
2. **Withdrawn (operator, 2026-11-18).** This asked what a session should do with the
   operating-client check off. The operator directed that the temporary switch be removed
   first (Rulings, above), so there is no off state to design for.
3. **Proposal A:** the session's start at the admission, not as a request of its own.
4. **Proposal F:** no fallback to a closed session during the question and the closing phase.
5. **Proposal G:** plain SQL, although the ADR's text says regeneration.
6. **The rank.** W-0029 stands second in the backlog's P2 list, under W-0027, because two of
   W-0027's slices wait on it. That position is my proposal and the backlog says so. The
   backlog alone owns priority; move it if that is wrong.
