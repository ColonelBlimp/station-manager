---
number: 0101
title: Contest occurrence records, slice serials and a daemon-held contest mode
status: Proposed
date: 2026-11-06
---

# 0101 — Contest occurrence records, slice serials and a daemon-held contest mode

**Proposed (2026-11-06). Design only. Nothing is authorised or built.**

This ADR amends [ADR 0096](0096-contest-occurrences-reserved-serials-and-submission-export.md)
and does not rewrite it. It records:
- the organiser-reply rulings of 2026-10-29;
- the operator's rulings of 2026-11-06, which follow the reviewer's recommendations;
- the operator's rulings of 2026-11-07 on the former U1–U3 (migration, early Start, catalogue
  override), which also follow the reviewer's recommendations;
- the design that follows from both.

The rulings and their history are in [W-0023](../work/W-0023-contest-logging.md) (*Organiser-reply
rulings*; *Follow-on ADR: scope proposal*).

## Context

ADR 0096 (Accepted 2026-10-28) set the contest model:
- occurrences with their own identity;
- explicit membership;
- a reserved serial ledger (option B);
- typed-serial overrides (T3);
- submission-export rules.

Three things have happened since.

1. **The organiser answered** (2026-10-29), and the operator ruled on the answers (A–D, and the
   header choices). The dossier requires them in a follow-on ADR before any implementation.
2. **A weekend slice operated the real event.** It was built at the operator's direction for the
   UK/EI DX SSB event of 31 October 2026 (158 contest QSOs; submitted and **accepted** 2026-11-02).
   It deliberately diverged from ADR 0096, *for that slice only*:
   - **Serials:** a serial is consumed when the callsign is committed (Tab), and gaps are accepted.
     There is no release and no reconciliation (`contest_serial_allocation`, log schema 19;
     `POST /v1/contest-serial`).
   - **Occurrence:** named by `CONTEST_ID` plus the event's start date, stored on each QSO as
     `APP_SM_CONTEST_START`.
   - **Membership:** read from those two fields, not from an association.

   The robot accepted the log. Its acknowledgement flagged only missing UK districts.
3. **ADR 0100 (Proposed) needs this state.** Its U3 ruling suspends session-gap expiry only while
   an explicit, **daemon-owned** contest-active state is on, and every window must agree on it.
   Today the contest choice is per browser (`frontend/app/src/lib/operate/contest.svelte.ts`), so
   that state does not exist.

## Decision

1. **The organiser-reply rulings and the header choices amend ADR 0096 §4 and settle several of its
   Unresolved items** (§1 below).
2. **Serials follow the slice rule.** This supersedes ADR 0096's option B (§2) and the T3 override
   (§3).
3. **An occurrence is a record with its own UUID** in the archive, owned by one logbook. Membership
   stays an explicit association (ADR 0096 M5). The slice's data migrates onto both.
4. **Contest mode is a daemon-held state, shared by every window.** It is started explicitly and
   ends itself at the window's end. A draft in progress always keeps its occurrence and serial.
5. **Log requests are accepted as named.** They are validated, and a mismatch with the active
   contest is warned about, never refused.
6. **Contest definitions come from a catalogue** served by the daemon (`contests.json`), not from
   the SPA's hard-coded list.

### 1. Organiser-reply rulings (2026-10-29), recorded

These come from W-0023, *Organiser-reply rulings*. They amend ADR 0096 §4 and its Unresolved list.

- **A. Keep duplicates.** The submission export includes **every** explicit member, duplicate
  contacts and duplicate sent serials included, and reports them. No duplicate key ever excludes a
  member. The organiser's duplicate key is **once per band**, and it is reported, not used to
  exclude.
- **B. Serials have at least three digits:** `001` to `999`, then `1000` onward without
  truncation. This is formatting only. An explicit "no serial received" exports as `000`.
- **C. Completion time.**
  - Eligibility and the Cabrillo `QSO:` time use the resolved UTC end date and `TIME_OFF`;
    `QSO_DATE`/`TIME_ON` stay as the ADIF start.
  - **The window is `[start, end)`, as a product rule.** The organiser's exact-boundary reading
    stays recorded as unresolved.
  - **Completion is captured once,** preserved across retries, and never moves a contact across
    the boundary.
  - **An unresolved completion blocks the submission export, never logging.**
  - **Cabrillo truncates times to the minute.**
- **D. An explicit missing district.** An explicit "district not copied" record exports as `--`. A
  blank required district without that record still blocks the export. On 2026-11-02 the robot
  accepted `--` for UK contacts and flagged them, consistent with this.
- **The header:**
  - `CONTEST: UKEIDXSSB` / `UKEIDXCW`, the same strings as ADIF `CONTEST_ID`;
  - `START-OF-LOG: 3.0`;
  - `CATEGORY-POWER` always written;
  - `NON-ASSISTED`.

### 2. Serials: the slice rule

This supersedes ADR 0096 §2 (option B) and §3 (T3).

- **Allocation at Tab.**
  - Committing the callsign of a contest draft allocates the next serial of the draft's
    occurrence, durably, before it is shown.
  - The allocation is keyed by the draft's identity. A re-Tab, a callsign correction or a lost
    response returns **the same number**, whatever other drafts took in between.
- **Consumed for good.** A duplicate, a Clear, an abandoned or unsaved draft, or a power loss
  leaves the number consumed. **Gaps are accepted and expected.** Nothing is ever released,
  reconciled or handed out twice.
- **The next number** is above both the highest allocated and the highest `STX` saved for the
  occurrence. So a serial typed by hand is never handed out again.
- **A typed serial is recorded as typed.**
  - The draft's Sent # stays editable, and what is saved as `STX` is what the operator says was
    sent.
  - The allocated number it replaced stays consumed, as a gap.
  - There is no override prompt and no "possibly sent" bookkeeping (T3 withdrawn). Duplicate sent
    serials are preserved and reported at export (ruling A).
- **The sequence's owner:** the occurrence record (§3), replacing the slice's
  `(logbook_id, contest_id, occurrence-date)` key.

### 3. Occurrence records and membership

- **The occurrence record** lives in the archive file:
  - `uuid` (UUIDv7), its stable identity;
  - `logbook` (the logbook's UUID);
  - `contest_id` (the ADIF `CONTEST_ID`; also the Cabrillo `CONTEST:` value for the reference
    contest);
  - `start` and `end` (UTC instants; the window is `[start, end)`);
  - the eligible mode(s);
  - the catalogue entry it was created from (§6), so later catalogue edits never rewrite a
    declared occurrence silently.

  It is **unique per (logbook, `contest_id`, start)**. ADR 0096 D4 stands: overlapping occurrences
  in one logbook are refused, adjacent half-open windows do not overlap, and other logbooks are
  independent.
- **Creation.** An occurrence is created from a catalogue entry the first time the operator starts
  it (§4) in a logbook. Starting it again finds the existing record.
- **Membership** is the explicit association of ADR 0096 M5, `(qso_uuid, occurrence_uuid)`,
  written atomically with the QSO by the live logging path in contest mode. Everything else in ADR
  0096 §1 stands:
  - eligibility is separate from membership;
  - an edit out of eligibility keeps membership and serial, and is reported;
  - membership changes after the fact are explicit and recorded;
  - an imported `CONTEST_ID` creates no association.
- **On the QSO.** `CONTEST_ID` and `APP_SM_CONTEST_START` stay on the QSO and in exported ADIF, for
  other loggers and for round-tripping. They are **not** the membership.
- **Migrating the slice.** One explicit migration step:
  - creates the occurrence record for each `(logbook, contest_id, start date)` the slice used;
  - re-keys `contest_serial_allocation` onto it;
  - associates the slice's members.

  **Members come from an explicitly reviewed list of QSO UUIDs** (ruled 2026-11-07):
  - Candidates are generated from the contest id and occurrence date, then reviewed with the
    operator against the original event log. The selected QSO UUIDs are recorded, scoped to their
    archive and logbook. The documented 158 contest QSOs are a cross-check, not proof.
  - No automatic rule is used. The fields can also appear on a round-tripped import, and a matching
    allocated serial does not establish identity: the allocation table has no QSO link, and the
    slice accepts typed serials it never allocated (`AllocateContestSerialWithContext`).
  - Reviewed membership is kept even where a QSO has a missing serial or an eligibility problem;
    those stay export-validation issues.
  - **Every allocation is kept**, including those of abandoned drafts.

### 4. Contest mode: a daemon-held, shared state

- **What it is:** the daemon holds, per archive, at most one **active contest**: an occurrence, plus
  a state generation for stale-action protection. It is **not** a per-tab or per-browser choice.
  Every window shows it and follows its events. It is the state ADR 0100's U3 suspension reads.
- **Start (explicit).**
  - The operator chooses an occurrence (§6) and presses Start.
  - Start is refused if the occurrence's window has already ended.
  - **An early Start is allowed** (ruled 2026-11-07). The state is active immediately, so ADR
    0100's gap suspension begins at that Start. Every window shows a persistent, shared message, for
    example "Contest mode on — event opens at 12:00 UTC".
    - QSOs completed before the opening stay ineligible under ruling C.
    - Committing a callsign before the opening consumes a real serial, under the normal rule.
  - **A contest is never started merely because its window opens.** An operator who stopped it
    before the opening starts it again.
- **Stop (explicit, early).** The operator can stop it at any time.
- **Ending itself.** At the window's end the daemon ends the state, and new drafts stop being
  contest contacts.
- **Drafts are preserved, at the end of the window and at a Stop.**
  - **A draft already in progress** keeps its occurrence and its allocated serial, so the operator
    can finish and save it. It saves under that occurrence (§5).
  - **Its eligibility** stays a separate check against its recorded completion time (ruling C).
  - **The end never interrupts the draft.** That is what "never mid-QSO" means here: the contest
    state ends and the draft lives on.
  - **The draft does not keep the state alive.** An abandoned draft keeps neither contest mode nor
    ADR 0100's session-gap suspension active.
- **Stale-action protection.** Start and Stop carry the expected generation and a request id, with
  the replay and refusal rules of ADR 0100 §5:
  - a delayed Start or Stop from a window showing an older state is refused, and the window
    redraws;
  - after any change, every window refreshes its controls for **subsequent** drafts.
- **Restart.**
  - A contest that was **active** is restored if its window's end has not passed, whether or not
    the window has opened yet (an early Start is restored too).
  - A contest that was **stopped**, or whose window has ended, **stays stopped**.
  - An archive switch carries no active contest into the new archive.
- **ADR 0100's U3.** The gap suspension lasts exactly while this state is active. Under
  [ADR 0100 P2](0100-daemon-owned-operating-sessions-with-explicit-membership.md#p2-the-baseline-when-the-contest-state-ends-completes-u3s-open-choice)
  (ruled 2026-11-07), ending it applies overdue session expiry immediately; neither Stop nor
  automatic end counts as activity or moves `last_activity_at`.

### 5. Log requests are accepted as named

The 2026-10-28 concurrency ruling stands: every contest log request **names its occurrence**, and
the daemon never infers it from the active contest.

- **Accepted as named:** a request naming occurrence A is filed under A, even while B is active.
  A QSO begun under A, before another window started B, stays saveable under A.
- **Validated:**
  - the named occurrence must exist;
  - it must belong to the request's logbook and archive (the existing `X-SM-Archive-Id` guard);
  - the draft's allocated serial must belong to it.

  A request that fails validation is refused with a stable code, and nothing is stored.
- **Warned, never refused, for a mismatch.** When the named occurrence is not the active one, or no
  contest is active, the form shows a clear warning. It never discards or rewrites the draft.
- **Eligibility is separate.** Time or mode ineligibility stays visible and follows the eligibility
  and export rules (ADR 0096 §1, ruling C).

### 6. The contest catalogue

- **Source.** Contest definitions come from an embedded `contests.json`, plus an operator override
  file in the working directory, following the existing `modes.json` pattern. The daemon serves them
  (`GET /v1/contests`).
- **Override rule: whole contests, matched by contest id** (ruled 2026-11-07).
  - Ids only in the embedded catalogue stay available; ids only in the operator file add contests.
  - A matching operator entry replaces that entire definition, including its editions and nested
    lists.
  - Omitted fields follow the schema's defaults or validation requirements. They never inherit
    embedded values.
  - Accepted trade-off: overriding one field means copying the whole definition, and that contest
    no longer receives embedded updates.
- **It replaces** the hard-coded catalogue in `frontend/app/src/lib/operate/contest.svelte.ts`.
- **First-slice fields:**
  - the contest id and Cabrillo name;
  - its editions (start and length);
  - the eligible mode and submodes;
  - the bands and the sponsor's frequency segments;
  - the exchange fields;
  - the received-serial default;
  - the district list;
  - the off-time minimum.
- **Not in it yet:** scoring rules, which are out of scope.
- **The catalogue defines; the occurrence record (§3) declares.** Editing the catalogue never
  changes an occurrence already created.

## Alternatives considered

### Serials: keep ADR 0096's option B ledger

Reservation, holds that never expire, explicit "not sent" release, reconciliation, and the T3
override. Rejected by the 2026-11-06 ruling:
- **The cost:** a prompt for every abandoned contact, and a transition table of holds.
- **The evidence:** the robot accepted the 2026 log with serial gaps (004, 020, 058, 161; inbox
  2026-11-02). Its acknowledgement flagged only the missing districts. That a gap is diagnostic, not
  an invalid log, is this project's own ruling (ADR 0096 D3), not a sponsor statement.
- **What the slice rule keeps:** it never hands a number out twice, and it records exactly what was
  typed.
- **Revisit** if a sponsor penalises gaps (*Triggers*).

### Occurrence: keep the slice's `CONTEST_ID` plus start-date key

No occurrence table: the identity is the two QSO fields. Rejected:
- **The window and logbook** would live only in the catalogue, so overlap refusal (D4) and
  ownership validation (§5) would have nothing durable to check against.
- **A catalogue edit** could silently change what an existing edition means.
- **Membership** would again be derivable from fields, which ADR 0096 M5 rejected: an import can
  carry them.

### Contest mode: per tab or per browser, as today

Rejected:
- ADR 0100's U3 requires one daemon-owned state on which every window agrees.
- Per-tab state cannot survive a restart consistently.
- Two windows can disagree today about whether a contest is on.

### Contest mode: automatic while a window is open

Active whenever a selected occurrence's window is open, with no Start or Stop. Rejected by the
ruling:
- the operator works non-contest contacts during a contest's hours, as on 2026-11-01 (FT8 on 80 m);
- an automatic start would also suspend session expiry unasked.

### Contest mode: explicit only, never ending by itself

Rejected by the ruling: a forgotten Stop would keep contest mode, and the session-gap suspension,
on indefinitely.

### Refuse a log request whose occurrence is not the active one

Rejected (the 2026-11-06 ruling on question 4):
- It would make a draft begun under A unsaveable once another window started B, or once the window
  ended mid-contact.
- That would lose a transmitted exchange and its serial.
- Validation (§5) still refuses requests that are malformed or that name a foreign occurrence.

## Consequences

- **A log-schema migration:**
  - an occurrence table, a membership association, and the active-contest state with its
    generation and applied-request records, all in the archive file;
  - `contest_serial_allocation` re-keyed onto occurrences;
  - the slice migration (§3).

  It brings the whole-tree test consequences of a head bump.
- **New API:**
  - `GET /v1/contests`;
  - contest Start and Stop, with expected generation and request id;
  - reading the active contest;
  - a contest-mode event on the shell stream;
  - log requests naming an occurrence by UUID;
  - serial allocation keyed by occurrence.

  `docs/v2-design/api-endpoints.md` and `config.md` change with them.
- **SPA:** the header checkbox and dropdown become controls over the daemon's state. A draft's
  occurrence and serial are kept through an end or a Stop. A mismatch warning is added.
- **The slice's rollback note changes.** Rolling back past this migration loses the occurrence and
  association records, as the slice's rollback already loses the allocation record. Export first.
- **Simplification:** no hold or override UI and no transition table, because gaps are accepted.

## Unresolved (not decided by this record)

- **U1–U3 were ruled on 2026-11-07** (the migration list in §3, the early Start in §4, the override
  rule in §6). The reviewer's recommendations, as confirmed, are recorded in
  [W-0023](../work/W-0023-contest-logging.md).
- **Carried from ADR 0096:** the exact boundary reading, the "Europe" definition and scoring.
- **Later work, not in this record:**
  - the per-contest panel;
  - the in-product Cabrillo export and Logbook ADIF export, which replace the temporary
    `scripts/ukeicc-cabrillo.py`;
  - the Score Distributor feed;
  - FT8 contest modes;
  - different-call contests (W-0014).

## Triggers to revisit

- A sponsor penalises serial gaps: revisit §2 and option B.
- A contest needs per-band or per-mode serial sequences: revisit one sequence per occurrence.
- An operator needs two contests active at once in one archive (overlapping events in different
  logbooks): revisit "at most one active contest per archive".
- ADR 0100 is rejected or changes U3: §4 still stands for shared presentation and the end-of-window
  rule, but its session coupling goes.

## References

- [ADR 0096](0096-contest-occurrences-reserved-serials-and-submission-export.md) (amended),
  [ADR 0100](0100-daemon-owned-operating-sessions-with-explicit-membership.md) (U3; §5 stale-action
  pattern), ADR 0056 (per-logbook placement), ADR 0071 (archives).
- [W-0023](../work/W-0023-contest-logging.md): *Organiser-reply rulings*, *Weekend slice*,
  *Follow-on ADR: scope proposal*.
- `internal/database/sqlite/migrations/log/0019_contest_serial.up.sql`,
  `internal/api/handler_contest_serial.go`, `frontend/app/src/lib/operate/contest.svelte.ts`,
  `scripts/ukeicc-cabrillo.py`.
- [`docs/dogfood-inbox.md`](../dogfood-inbox.md), 2026-10-31: the contest config system, contest
  mode in and out, the per-contest panel, and the Logbook export.
