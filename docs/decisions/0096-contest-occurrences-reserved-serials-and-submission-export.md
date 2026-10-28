---
number: 0096
title: Contest occurrences own explicit membership and a reserved serial ledger
status: Accepted
date: 2026-10-28
---

# 0096 — Contest occurrences own explicit membership and a reserved serial ledger

**Design accepted (operator rulings, 2026-10-28); no implementation is authorised.** The working
detail — transition table, race and retry cases, sources — lives in
[W-0023](../work/W-0023-contest-logging.md). This record keeps the decisions and the alternatives
weighed. Mechanisms named here as candidates are not decided.

## Context

Contesting is step (5) of the data-model chain ruled on 2026-09-19 (W-0014). W-0011 ruled an
explicit contest mode with a declared window, stamping ADIF `CONTEST_ID`. The reference case is the
UK/EI DX Contest, whose exchange is RS(T) plus a serial running across all bands, plus a district code
from UK/EI stations. The log is Cabrillo, in order of serial sent. `types.QsoDetails` already carries
`CONTEST_ID`, `STX` and `SRX`, but no contest mode, serial counter or Cabrillo export exists.

A serial is sent over the air before the QSO is saved, and on SSB and CW only the operator knows it
was sent. A record of what was transmitted therefore cannot be rebuilt from saved QSOs. Interrupted
contacts, failed saves, restarts and two tabs logging at once all have to preserve it. Gaps are
visible to the sponsor's checker, and reuse risks two QSOs claiming one transmitted number.

Several sponsor facts remain unanswered: duplicate policy, Cabrillo header, the meaning of "Europe"
and some district codes. The organiser has not been asked; the questions are drafted in W-0023 and
unsent.

## Decision

1. **Membership.**
   - **Occurrences:** a declared contest *occurrence* has its own stable identity, distinct from
     `CONTEST_ID` and the sponsor's Cabrillo name. It lives in the archive file, is owned by one
     logbook, and has one serial sequence.
   - **Eligibility** comes from the QSO's own time inside the declared window, and its mode (SSB
     with its applicable submodes). The window boundary is not decided here; `[start, end)` is a
     proposed product boundary (Unresolved).
   - **Membership is a separate, explicit association,** written atomically with the QSO in a
     visible contest mode. It is never derived from a query over the window. An imported
     `CONTEST_ID` creates no association.
   - **After-the-fact changes:** an edit out of eligibility keeps membership and serial, and is
     reported. Membership changes after the fact are explicit and recorded.
   - **Overlap:** overlapping declarations in one logbook are refused.
   - **Intent:** every request names its occurrence; the daemon never infers it from the
     currently active contest.
2. **Serials: option B, a durable per-occurrence ledger.**
   - **Reservation:** a number is reserved by the daemon and protected from the moment it is
     displayed.
   - **Holds:** there is no timeout and no automatic retirement of a hold.
   - **Reuse:** a number is reused only after an explicit "not sent", only with no current hold
     and no permanent protection, and only under a new allocation identity.
   - **Atomicity:** saving, membership and binding commit atomically. Operations are retry-safe
     and stale writes are refused.
   - **Preservation:** the serial actually transmitted is preserved through explicit
     reconciliation. Correction, reassignment, removal and deletion never renumber history.
3. **Typed serials: T3, an explicit override.**
   - **Override:** while a number *H* is held, the operator may record a different sent number *Y*
     only through an explicit override.
   - **Resolving *H*:** the override resolves *H* as not sent, possibly sent, or both sent, in the
     same transaction. With no answer, nothing completes and *H* stays held; there is no default.
   - **Completion:** a completed override is linked to its QSO, so a second attempt is recognised
     as already saved.
   - **Both sent:** *H* is retired and preserved alongside *Y*. An unclear carried serial is
     flagged, never silently chosen.
   - **Repeats:** a repeated exchange within one contact is the same contact.
4. **Submission export.**
   - **Block** (naming the contacts): a member missing its stored sent serial; a member missing its
     received serial without an explicit "no serial received" record; ineligible members; a missing
     required received district; exchange uncertainty until it is resolved.
   - **Preserve and report:** duplicate sent serials (every affected contact); gaps (diagnostic,
     never renumbered).
   - **Received serial:** an explicit "no serial received" may export as `0`.
   - **District codes:** an unknown but well-formed district code is preserved with a warning.
     Invalid syntax is reported and never altered.
   - **Imports:** unassociated imports are reported separately and never exported as members.
   - **Saving:** no export rule ever refuses saving a contact.

## Alternatives considered

### Membership: station-global declaration in `config.json`

Rejected: contrary to ADR 0056's placement of per-logbook data in the database. A station-global
declaration would also follow the operator into an archive it does not belong to.

### Membership derived from logbook, window and mode

Every QSO in the logbook, window and mode counts as a member. Rejected: it sweeps in non-contest
contacts, and it cannot carry a sent serial that was never recorded. `CONTEST_ID` alone is likewise
insufficient. Annual editions share it, and imports carry it.

### Eligibility by submission time

Rejected: a late-entered or corrected QSO would land on the wrong side of the boundary.

### Serials A1: automatic `highest + 1`, recorded on each QSO

No allocation state exists outside saved QSOs. Rejected: it reuses a transmitted-but-unsaved number
automatically after an interruption or a restart. Two tabs display the same number. A deletion or a
membership removal frees the highest number for reuse. It meets the reuse constraint only through
the operator's memory.

### Serials A2: every serial typed by the operator, never proposed

The software never proposes a number, so it never reuses one automatically. Rejected as the normal
path: the whole burden of tracking the sequence, across interruptions, restarts and two windows,
falls on the operator. The software can only warn about a number already bound. A2 survives as
the fallback when the daemon is unreachable before a reservation: an *unreserved* number, accounted
for as externally bound when saved.

### Typed serials T1: no typing

*H* is read-only, and a different number enters only by correct, reconcile or the unreserved path.
Rejected: a contact that carried *Y* is first saved with a wrong `STX` and then corrected. That
takes two steps and leaves a transient wrong record.

### Typed serials T2: free typing

The operator overwrites *H* before saving. Rejected: *Y* still needs accounting. *H* was displayed,
so it was possibly transmitted, yet it is left held with no answer, or it tempts an automatic
return, which the reuse rule forbids.

### Export: refuse duplicate serials, or renumber them

Rejected: both contacts may genuinely have carried the number. Refusing would force a falsified
log, and renumbering rewrites what was transmitted.

### Export: write `--` or `0` for any missing received field

Rejected: rule 6 establishes `0` for a missing received serial. The samples establish `--` only for
DX stations. Neither covers a missing UK/EI district, so inventing a value would misstate the
exchange.

### Export: a provisional duplicate key

Rejected until the sponsor answers: an invented key would present a guess as the sponsor's policy.
An all-contact diagnostic export is acceptable, but it is not labelled sponsor-ready.

## Consequences

- **New durable state:** the archive gains per-logbook contest occurrences, membership
  associations and a durable allocation history in which no number's history is lost, kept for the
  occurrence's life. Its storage form is not chosen here. A schema migration follows, with the
  whole-tree test consequences of a head bump.
- **Daemon-side transactions:** every serial operation — reserve, save, return, retire, reconcile,
  correct, reassign, remove and override — needs daemon-side transactional handling identified by a
  request id. How the operations map onto API calls is not decided here. Saving still needs the
  daemon, as today. Reservation adds no further condition, because the unreserved path covers its
  absence.
- **Operator-visible state:** held numbers stay visible until the operator answers for them, and an
  unanswered hold never expires. The cost is a prompt whenever the operator abandons a contact whose
  held number is unresolved; a successful save needs none.
- **Reports and blocks:** gaps, duplicates, corrections, unreserved numbers and uncertainty appear
  as reports. A submission export can be blocked while logging continues.
- **Serial order:** serial order and time order can interleave across tabs. Cabrillo orders by
  serial, as the sponsor asks.

## Unresolved (not decided by this record)

**Sponsor-dependent.** These need the organiser's answers or further sources; the questions are
prepared in W-0023 and not sent:

- **Duplicate policy:** the duplicate key, and how duplicates are represented in the file (export
  ruling 7; W-0023 questions 1 and 11).
- **Cabrillo header:**
  - the `CONTEST:` value;
  - `NON-ASSISTED` or `UNASSISTED`;
  - required `CATEGORY-*` fields;
  - whether power must be stated (question 2).
- **"Europe"** for the points table, and therefore scoring, which is out of the first slice.
- **District codes:**
  - the London codes and the dual-entity codes (question 4);
  - the handling of a missing UK/EI district (export ruling 4b, blocked meanwhile).
- **Window boundary:** `[start, end)` remains a **proposed** product boundary, not a ruling, and
  not a confirmed reading of the sponsor's "1200z to 1200z".
- **Duplicate serials:** whether the sponsor accepts them.

**Provisional mechanisms** (outcomes required, mechanisms not chosen):

- the request record's placement, as the first write under a unique key;
- the representation of the completion link;
- the entries recording *H* as possibly or confirmed transmitted;
- where the exchange-uncertainty flag is stored;
- `SRX_STRING` for the received district;
- the schema of occurrences, associations and the ledger.

**Product questions still open** (W-0023):

- where the exchange is entered: dedicated fields or a parsed field (question 7);
- whether received RS is logged as heard or defaulted (question 8);
- segment warnings (question 9);
- whether auto runs skip in-window repeats, and the lifetime grey-out outside a window (W-0011).

## Triggers to revisit

- If the sponsor's answers define a duplicate key or representation, export ruling 7 is settled, and
  ruling 2 is revisited if duplicate serials are not accepted.
- If a contest needs per-band or per-mode serial sequences, the one-sequence-per-occurrence rule (M1)
  must change.
- If unanswered holds prove burdensome in real operation, revisit "no timeout" with evidence. The
  rule is not to be relaxed speculatively.
- If a contest under another callsign enters scope (W-0014), occurrences must follow its logbook and
  ADR 0056 bindings.
- If FT8/FT4 contests with serials enter scope, transmission becomes observable, and the conservative
  "possibly sent once displayed" rule may be refined there.

## References

- [W-0023](../work/W-0023-contest-logging.md): rules, sources (S1 [UK/EI DX Contest
  rules](https://www.ukeicc.com/dx-contest-rules.php)), transition table, acceptance criteria.
- W-0011 (contest mode), W-0014 (data-model chain), W-0021 (archives and logbooks).
- ADR 0049 (no daemon-owned operating sessions), ADR 0056 (per-logbook placement), ADR 0041 (time
  format), ADR 0071 (archives).
- [Invariants](../v1-analysis/invariants.md): "Contest dupe check and general ingest dedupe are two
  different things".
- `internal/types/qso_details.go` (`CONTEST_ID`, `STX`, `SRX`).
