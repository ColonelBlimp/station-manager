# URGENT TODO — `internal/qsoservice` logging gaps

**Status:** open · **Raised:** 2026-08-01 · **10 findings + 2 corrections** ·
**Source:** package logging review of `internal/qsoservice` (2,018 non-test lines,
12 files, **8 log call sites**), operator-directed, review only — no code was changed.
Q1-Q6 from the first pass; **Q7-Q10 added the same day from a second source**, verified
before filing. That second source also **overturned my own rebuttal** of a claim it had
made earlier — Correction 2 below is the most useful output of either pass, and worth
reading even if you skip everything else.

**Siblings:** [`ft8-logging-gaps.md`](ft8-logging-gaps.md) (14),
[`bridge-logging-gaps.md`](bridge-logging-gaps.md) (14),
[`api-logging-gaps.md`](api-logging-gaps.md) (12). Same audit, same axis, same day.
`backlog.md` owns the ranking; fold in and delete once shipped.

---

## READ FIRST — this review corrects two existing documents

### Correction 1: SHIP GATE item (b) is FALSE and must be edited

`backlog.md`'s operator-directed SHIP GATE entry (2026-07-31) states:

> **(b) QSO deletes write no log line.** `qsoservice.Delete` (`delete.go:37`) has no
> logger call.

**It does.** `delete.go:85-90`:

```go
s.Logger.InfoWith().
    Int64("qso_id", existing.ID).
    Str("call", existing.ContactedStation.Call).
    Str("qso_date", existing.QsoDetails.QsoDate).
    Str("time_on", existing.QsoDetails.TimeOn).
    Msg("QSO soft-deleted")
```

`git log -L` puts that line in the tree since **`d516d816`, 2026-05-17** — **two and a
half months before the SHIP GATE entry was written** — and it has been *enhanced* since
(the original carried only `qso_id`; call/date/time were added later). So the claim was
wrong when written, not stale from a later fix. `:37` is the function's signature line,
which is consistent with the claim having been made from the signature rather than the
body.

**Action: edit the SHIP GATE entry.** Item (b) is done. Leaving it there costs a future
session the work of building something that exists — the exact failure the
*verify-backlog-before-building* rule exists for, and the third instance of it.

### Correction 2: the enqueue log — and my own rebuttal of a correct claim

`api-logging-gaps.md`'s A2 amendment relayed a claim that `qsoservice` "logs only the
**non-zero** enqueues". I rebutted it: *"false — the log at `enqueue.go:202-208` fires
unconditionally after commit, zero or not."*

**My rebuttal was wrong, and the original claim was right.** There is no post-commit
path when nothing is enqueued: `enqueue.go:181` (insert backfill) and `:292` (delete
repair) both do

```go
if len(enqueueIDs) == 0 {
    return res, nil          // before the tx, before the log
}
```

so a zero-enqueue call returns without ever reaching the logging statement. What I said
about the post-commit line was true and **irrelevant** — I checked the mechanism named
in the claim, found it inaccurate, and dismissed a finding that was correct about
behaviour. The claim described *what the operator observes*; I answered about *how the
code gets there*.

This makes **Q2 worse, not better**: an enqueue where every QSO was refused by the
ClubLog no-history policy produces **no log line at all**, not a partial one. Corrected
in Q2 below and in A2 in the api file.

---

## The axis used

Same as the siblings: **can the operator tell this apart from the nearest confusable
state?**

**Headline: the CRUD path is well logged, and the gaps are the paths either side of it.**
Submit, update and delete each emit a full Info line after commit — qso_id, call, date,
time, and for submit/update also freq/band/mode. That is better than either the ft8 or
bridge packages manage. What is missing is the *recovery* verb, the *refusal* outcomes,
and the fan-out.

---

## Tier 1

### Q1. ✅ FIXED 2026-08-07 — `restore.go` is the only CRUD verb that logs nothing — and it is the disaster-recovery path

> ✅ **FIXED 2026-08-07** as prescribed: per-call Debug in `qsoservice.Restore`
> (`logRestore` — uuid, logbook_id, outcome; stored and skipped_existing
> distinguishable, which was the whole finding) + the durable per-run Info
> summary in `cmd/smd/restore.go` (requested/stored/skipped_existing/failed +
> logbook), landing in smd.log alongside the stdout report. Spec:
> `logginggaps_test.go` TestRestore_OutcomesAreDistinguishableInTheLog.

Four verbs, three logged:

| Verb | Line | Logged |
|---|---|---|
| Submit | `submit.go:482` | `QSO stored` — qso_id, uuid, logbook_id, call, date, time, freq, band, mode |
| Update | `update.go:343` | `QSO updated` — same shape |
| Delete | `delete.go:85` | `QSO soft-deleted` — qso_id, call, date, time |
| **Restore** | `restore.go` — **89 lines, 0 log calls** | **nothing, on either outcome** |

`Restore` (ADR 0040 S5) inserts a QSO from an SM Cloud export. Both its outcomes —
`RestoreStored` (`:88`) and `RestoreSkippedExisting` (`:61`) — return silently.

**This is the path you run when something has already gone wrong.** The workstream it
belongs to exists because the dogfood DB was lost around 2026-07-16. A restore run
writes rows into the operator's logbook, and the daemon keeps no record of how many
landed versus how many were skipped as already-present.

- **Confusable with:** a restore that did nothing. An idempotent re-run (every row
  `skipped_existing`) and a real recovery (every row `stored`) produce identical
  silence — and telling those apart is the entire question during a recovery.
- **Note the invariant angle:** *"the only thing that should stop logging is a broken
  local DB."* Restore is what runs after exactly that, so its record is the one least
  able to rely on anything else having survived.
- **Record:** per-call status at Debug (a restore is a bulk loop) plus a summary line
  per run at Info — stored / skipped counts and the target logbook.

### Q2. ✅ FIXED 2026-08-07 — The enqueue log omits three of five outcomes — including the ClubLog compliance refusal

> ✅ **FIXED 2026-08-07** as prescribed, both amendments included: one outcome
> line (`logEnqueueResult`) fires on EVERY successful return — the zero-enqueue
> early return included, so the pure ClubLog-compliance case (all refused) is
> no longer the same silence as never-invoked — carrying `requested` plus all
> five counts, lengths only. The delete-repair path got the sibling
> (`logEnqueueDeleteResult`: every return + `not_found`). Spec:
> `logginggaps_test.go` (all-refused / five-counts / delete-zero rules). The
> handler-layer half of the pairing (api A2) remains open — the count is now
> durable at the service layer either way.

`EnqueueResult` (`enqueue.go:20-32`) carries five outcome fields. The log at `:202-208`
carries two of them plus `force`:

| Field | Logged? |
|---|---|
| `Enqueued` | yes |
| `SkippedUploaded` | yes |
| `SkippedDeleted` | **no** |
| `NotFound` | **no** |
| `SkippedNoHistory` | **no** |

The third omission is the one that matters. `SkippedNoHistory` is the ClubLog
`realtime.php` refusal — QSOs withheld because ClubLog's grant condition forbids
catch-up batches on that endpoint, a condition SM confirmed back to them on 2026-07-19
and whose violation gets the API key blocked.

**So SM refuses to send QSOs in order to honour a written commitment to a third party,
and keeps no record that it did.** If ClubLog ever asks, the evidence is a count that
was returned to a browser and discarded.

**AMENDED 2026-08-01 — the zero-enqueue case is worse than "an incomplete line".**
`:181` and `:292` return before the transaction and before the log when
`len(enqueueIDs) == 0`. So a selection in which **every** QSO was refused — the pure
ClubLog-compliance case — writes **nothing at all**. Neither is the requested selection
size recorded anywhere, so "I selected 300 and nothing happened" has no counterpart in
the log. The delete-repair path (`:310-317`) additionally omits `NotFound`.

- **Confusable with:** an enqueue where nothing was refused (`{enqueued: 12}` vs
  `{enqueued: 12, skipped_no_history: 300}` — same line), **and** with the operator
  never having pressed the button at all (all-refused vs never-invoked — same silence).
- **Record:** log on **every** return path, including the early one, with the requested
  selection size and the counts of all five outcomes. Lengths only for the `[]string`
  fields at Info; the UUID lists belong at Debug if anywhere.
- **Pairs with `api-logging-gaps.md` A2** (the handler doesn't log its summary either).
  Neither layer records it; fix both or the count still only exists in the response.

### Q3. ✅ FIXED 2026-08-07 — A duplicate submit is silent while a stored submit logs

> ✅ **FIXED 2026-08-07** as prescribed: `logDuplicateRefused` (Info —
> logbook, submitted call/date/time, colliding uuid + qso_id) on BOTH
> duplicate returns — the dedupe pre-check and the unique-index race path —
> via one shared helper so the two records cannot drift. Spec:
> `logginggaps_test.go` TestSubmit_DuplicateRefusalIsLogged.

`submit.go` returns `SubmitResult{Status: "duplicate", …}` at `:377` and `:430`. Neither
path logs. The `stored` path at `:482` does.

So the QSO that **was** written leaves a record and the QSO that **was not** leaves
none — which is backwards for diagnosis, because the operator saw a rejection and will
be the one asking about it.

This is live during operating: the operator clicks Log, gets a duplicate warning, and
nothing durable says which contact was refused or which existing row it collided with.
Both return paths already hold `existing.UUID` and `existing.ID`.

- **Confusable with:** the submit never having been attempted. Also with a validation
  rejection, which at least reaches the API access log with a code.
- **Note the deliberate-duplicate interaction:** `allow_duplicate` / `force` threads
  operator intent through to `Submit` (shipped 2026-07-26), and the FT8
  duplicate-detection backlog item is about accidental pairs already in the log. Neither
  a forced duplicate nor a refused one is currently recorded, so the data that would
  inform that item is not being collected.
- **Record:** the refusal at Info with the submitted call/date/time and the colliding
  UUID.

---

## Tier 2

### Q4. The stamp-sync re-enqueue logs at Debug, so the fix for a measured bandwidth problem is unobservable in production

`stamp_sync.go:69-73` logs at **Debug**.

That function is the built half of the backlog's *smcloud stamp-drift → reconcile
bandwidth churn* item. Its whole purpose is to keep `in_sync` true so `RunOnce` stays on
the cheap hash-only check instead of dropping to the full cloud-manifest GET —
**~110 B/row, ~650 KB per drifted hour at 5.7k rows, growing forever, on a Malawi
link.**

Its failure mode is silent non-firing, which returns the daemon to the expensive path.
At the default log level there is no way to confirm from `smd.log` that the mechanism is
working, and no way to notice when it stops.

- **Confusable with:** the stamp sync not running at all. Both produce no output.
- **Record:** promote to Info. Volume is bounded by stamp events (QRZ success stamps and
  session-email stamps), not by traffic — the observed figures were 7/39/34-row heals
  through an evening, and 94 after an email batch. That is a handful of lines a day.

### Q5. A stored QSO's forwarder fan-out is not recorded, and two silent branches decide it

`submit.go:461-476` loops the configured forwarders and inserts an upload row per match,
inside the QSO transaction. **Two branches skip silently:**

- `if !shouldEnqueue(fwd, action.Insert) { continue }` — disabled forwarder, or an
  `action_filter` that excludes inserts. `forwarders.go` is 36 lines with zero log calls.
- `if isImport && !forwarderNamed(forwardTo, fwd.Name) { continue }` — an import that
  didn't name this forwarder.

`QSO stored` (`:482`) then records the QSO comprehensively and says **nothing about
where it was queued to**. `Delete` has the same shape at `:61-69`.

So "why did this QSO never reach ClubLog?" has no answer in the log: it could have been
queued and failed, queued and be pending, or never queued at all — and those are three
different problems with three different fixes.

- **Confusable with:** each other, and with an upload that failed later.
- **Record:** the destination names the QSO was queued to on the existing `QSO stored`
  line. It is already a structured line; this is one more field, computed in the loop
  that is already running.

---

## Tier 3

### Q6. Every `_ = tx.Rollback()` discards its error — 14 sites, in the package that owns one-fails-all-fail

`enqueue.go` ×2, `update.go` ×3, `delete.go` ×4, `submit.go` ×2, `submit_batch.go` ×2,
`stamp_sync.go` ×1.

This package owns **one-fails-all-fail for QSO writes** — the invariant that a QSO row
and its upload-queue rows are atomic. A rollback that itself fails is precisely the case
where that promise may not have held, and these 14 sites are the only places that could
observe it.

The **original** error is returned and does reach `writeServerError` at ERR, so the
operation failure is recorded. What is not recorded is whether the cleanup succeeded.

- **Confusable with:** a clean rollback. Nothing distinguishes "we rolled back cleanly,
  nothing persisted" — the invariant's actual promise — from "the rollback also failed
  and the disposition is unknown".
- **Ranked Tier 3 honestly:** a failed rollback on SQLite is rare, and the connection is
  typically discarded afterwards. But the reason to log it is that it is the *only*
  evidence the invariant was violated, and per the standing rule a risk noticed and
  dismissed must be dismissed on evidence — here there is none either way.
- **Record:** Warn on a non-nil rollback error, alongside the operation's own error.

---

## Second pass — findings Q7-Q10 (added 2026-08-01, separate source)

Raised independently after the first six and verified before filing. Two items in that
batch were folded rather than re-filed: the omitted enqueue classifications into **Q2**
(where the same source **overturned my rebuttal** — see Correction 2, it is the most
useful thing either pass produced) and the fan-out into **Q5**. The same source
independently reached Correction 1 about `Delete`, which is now confirmed twice.

### Q7. `Restore`'s existence probe treats a database failure as "not found" — Tier 1

`restore.go:60`:

```go
if _, err := s.DB.FetchQsoByUUIDIncludingDeletedWithContext(ctx, qso.UUID); err == nil {
    return RestoreSkippedExisting, nil
}
// every error — including a real DB fault — falls through to the insert
```

Only `err == nil` is handled. Any other error, transient or terminal, is treated
identically to a genuine miss and the function proceeds to insert. **If the insert then
succeeds, the database error has vanished completely** — no return value carries it, no
log line records it, and the caller is told `stored`.

- **Confusable with:** a genuine miss, which is the normal case and the one this
  function is built around. Infrastructure failure and expected absence are the same
  code path.
- **This is the same defect as `api-logging-gaps.md` A7** — a discarded non-not-found DB
  error becoming a wrong answer — in a second package. Two independent instances of one
  idiom (`if err == nil` / `stderr.Is(err, ErrNotFound)` with an implicit else) is worth
  treating as a pattern to grep for daemon-wide, not two isolated fixes.
- **Sharper here than in A7:** A7 fails *closed* (refuses to transmit), which is safe
  even while misreported. This one fails *open* — it writes a row. And the idempotence
  guarantee the probe exists to provide ("an existing row, live OR tombstone, wins —
  restore fills gaps, never overwrites", `:58-59`) is silently not in force when the
  probe errors.
- **Record:** log the non-not-found error before falling through, with the UUID. Whether
  it should also *refuse* is a design question for the operator, not a logging one —
  flag it, do not decide it.

### Q8. The import batch fallback discards the error that triggered it — Tier 1

`submit_batch.go:180` and `:188`:

```go
if ierr != nil {
    _ = tx.Rollback()
    return s.importBatchFallback(ctx, logbookID, batch, baseIndex, forwardTo, res)
}
```

Both the QSO-insert error (`ierr`) and the upload-queue error (`uerr`) are dropped on
the floor as the code rolls back and retries the batch record-by-record.

The fallback exists to salvage a batch when one row is bad. When it works — the common
case, and the point of the design — **there is no evidence anywhere that the efficient
path failed, let alone why.** An import that silently degrades to per-record inserts for
every batch looks, in the log, exactly like an import that never had a problem.

- **Confusable with:** an import that ran cleanly on the batch path. The user-visible
  totals are the same; only the throughput differs, and nothing records the difference.
- **Compounding:** because the trigger is unrecorded, a systematic cause (a constraint
  the importer keeps hitting, a schema drift) presents as "imports are slow" with no
  thread to pull.
- **Record:** the triggering error at Warn before calling the fallback, with the batch's
  base index. Two lines, both at sites that already have the error in hand.

### Q9. A forced dedupe bypass is not recorded — Tier 2

`submit.go:373` gates the entire dedupe check on `if !force`. With `force` set, the
duplicate lookup does not run at all — a **core storage invariant is deliberately
bypassed** — and the `QSO stored` line at `:482` carries no `forced` flag and no
submission source.

The API access log cannot fill this in: it deliberately logs `r.URL.Path` and not
`RawQuery` (see `api-logging-gaps.md` NOT-gaps — that omission is a credential-leak
defence and must stay). So the override arrives as a query parameter that is
deliberately never recorded, and lands in a log line that does not mention it.

- **Confusable with:** an ordinary store. A deliberate duplicate and a first-time
  contact are byte-identical in `smd.log`.
- **Directly relevant to open work:** the FT8 duplicate-QSO backlog item is about
  telling *deliberate* repeats from *accidental* pairs already in the log. `force`
  carries exactly that operator intent, and it is the field not being written down.
  With **Q3** (refused duplicates unlogged) this means neither side of the duplicate
  question is being recorded while the item stays open.
- **Record:** `forced` and the submission source on the existing `QSO stored` line. Both
  are already parameters of `Submit`.

### Q10. Import and restore have no durable completion summary — Tier 2

`SubmitImportBatch` returns its totals at `submit_batch.go:104` without logging them.
The `smd import` command logs only that it is starting; its terminal summary goes to
**stdout**. `Restore` logs neither start nor completion (**Q1**).

So `smd.log` cannot distinguish a completed bulk operation from an interrupted one. For
an operator-run CLI that is a nuisance; for a restore — run when the database has
already been lost — it is the record of what was recovered.

- **Confusable with:** an interrupted run. stdout is gone the moment the terminal
  closes, and neither operation writes a terminal marker.
- **Refines Q1's recommendation, and this is the better shape:** aggregate at the
  **command/batch boundary**, not per restored QSO. A per-row Info line on a 6,000-row
  restore is its own problem; one start line and one completion line with the totals is
  what answers the question. Q1's "per-call status at Debug" stands only as an optional
  extra.
- **Record:** start (source, target logbook, expected count) and completion
  (stored/skipped/failed) at Info, at the command boundary.

---

## Verified NOT gaps — do not re-open these

Checked against the code 2026-08-01.

- **The three CRUD lines are good and should be the template for the rest of the
  daemon** — `submit.go:482`, `update.go:343`, `delete.go:85` each log after commit with
  the QSO's identifying fields. This is the shape the other packages' missing
  success-path lines should copy.
- **The best-effort cache warm is correctly logged** — `submit.go:521-525` warns on the
  `contacted_station` upsert failure and says in the message that the QSO is already
  stored, which is exactly the "enrichment never blocks logging" invariant made
  legible.
- **`submit_batch.go:100`** warns per-row on an import upsert failure with the callsign.
  Correct for a bulk path.
- **Validation and pre-commit errors need no lines here.** They return typed errors that
  reach `writeServerError` (logged at ERR) or `writeError` (access log with a code) in
  `internal/api`. Adding daemon-side lines would duplicate — the same conclusion the ft8
  and bridge reviews reached about their refusal surfaces.
- **`dedupe.go` (53 lines) and `validation.go` (77 lines) correctly have zero** — pure
  functions, no decisions an operator can observe.
- **`service.go` (109 lines) correctly has zero** — DI wiring and accessors. Its
  `Initialize` fails loudly on a missing dependency (`:70-71`), which is the right shape
  for a startup fault.
- **Do not log QSO payloads or `before_image` blobs.** The audit trail already carries
  the pre-image in `qso_history` (ADR 0016) inside the database; `smd.log` is `0644`.
  Field-level identifiers (call, date, time, band, mode) are what the existing lines
  carry and what any new line should carry.

---

## Suggested order

1. **Correction 1** — edit the SHIP GATE entry before anything else. It is currently
   pointing a future session at completed work. (Done in `backlog.md` 2026-08-01.)
2. **Q2** — the compliance refusal is the one finding here with a third party attached,
   and the all-refused case is currently **silent**, not merely incomplete. Do it with
   `api-logging-gaps.md` A2, or the count still only exists in the response.
3. **Q7** and **Q8** — both discard an error that has already occurred, at a site that
   holds it. Cheapest fixes in the file; Q7 also has a design question for the operator
   attached (should it refuse, not just log?) that should be asked, not assumed.
4. **Q1 with Q10** — one job, and Q10 has the better shape: aggregate at the command
   boundary, not per QSO.
5. **Q3 with Q9** — the two halves of the duplicate question, and the FT8
   duplicate-detection backlog item needs both to be collecting data before it can be
   worked.
6. **Q5** — the fan-out, on the three lifecycle lines that already exist.
7. **Q4** — a one-word change (`DebugWith` → `InfoWith`) with the volume already
   measured.
8. **Q6** whenever these files are open.

**One cross-package pattern worth grepping for rather than fixing twice:** Q7 and
`api-logging-gaps.md` A7 are the same idiom — `if err == nil` / `stderr.Is(err,
ErrNotFound)` with an implicit else that swallows every other database error. Two
independent instances in two packages suggests a third.

Per the standing TDD directive, the behaviour statement for each is the
confusable-state clause above. Assert that the two confusable states produce
**distinguishable** output.
