# URGENT TODO — `internal/forwarding` logging gaps

**Status:** open · **Raised:** 2026-08-01 · **16 findings** · **Source:** package
logging review of `internal/forwarding` (3,558 non-test lines, 11 files, **21 log call
sites**), operator-directed, review only — no code was changed. F1-F3 from the first
pass; **F4-F16 added the same day from a second source**, each verified against the code
before filing — all thirteen were real, none duplicated the first three.

**The two passes found opposite halves of one defect.** The first says the package's two
huge lines carry no provenance; the second says its interesting transitions carry no
line at all. Decide F1's restructuring with F5/F6/F7 in view — those are the lines that
*should* exist.

**Siblings:** [`ft8-logging-gaps.md`](ft8-logging-gaps.md) (14),
[`bridge-logging-gaps.md`](bridge-logging-gaps.md) (14),
[`api-logging-gaps.md`](api-logging-gaps.md) (12),
[`qsoservice-logging-gaps.md`](qsoservice-logging-gaps.md) (10). Same audit, same axis,
same day. `backlog.md` owns the ranking; fold in and delete once shipped.

**Fewest findings of the five, and the shape of the problem is inverted.** Every other
package in this series was too quiet. **This one is by far the loudest thing in the
daemon** — 43.3% of `smd.log` by bytes, from two message types — and its gap is that
all that output cannot answer the question it is closest to.

**All figures below were measured on the LIVE log today** (`SM_WORKING_DIR`,
`~/.local/share/station-manager/log/smd.log`), not quoted from the 2026-07-31 session.
They independently reproduce that session's numbers. **A stale copy at
`~/pCloudDrive/station-manager/log/smd.log` (dated 2026-07-03) gives completely
different proportions** — 2.0% rather than 23.5% for `forwarding: success`. Measure the
`SM_WORKING_DIR` file, not the synced one.

---

## The axis used

Same as the siblings: **can the operator tell this apart from the nearest confusable
state?**

---

## Tier 1

### F1. This package is 43.3% of the daemon log, and neither of its two big lines carries provenance

Measured on the live log (84,151 lines / 14.04 MB):

| Message | Lines | Avg | Share of bytes |
|---|---|---|---|
| `forwarding: success` (`worker.go:376`) | 17,143 | 202 B | **23.5%** |
| `forwarding: submit` (`worker.go:253`) | 17,433 | 151 B | **18.0%** |
| `forwarding: transient — will retry` | 200 | 895 B | 1.2% |
| `forwarding: host unreachable` | 86 | 1,101 B | 0.6% |
| `forwarding: terminal failure` | 4 | 758 B | 0.0% |

**34,576 lines — 41% of every line the daemon has written — are a matched
submit/success pair.** `success` repeats `forwarder`, `qso_id`, `action` and `call`
verbatim from `submit` and adds one field (`upstream_id`).

**Neither line records WHY the row was queued.** Live logging, manual backfill,
stamp-sync re-enqueue, and reconcile repair all produce identical lines.

**Worked example — and its resolution is the evidence for the finding.** The submit
breakdown looks alarming:

| Forwarder | Submits | vs 1,196 QSOs stored |
|---|---|---|
| smcloud | 15,271 | **12.8 per QSO** |
| qrz | 1,263 | ~1 per QSO |
| clublog | 903 | ~1 per QSO |

That reads like a re-enqueue loop. **It is not** — bucketing by day shows 12,037 of the
15,271 landed on 2026-07-18 alone (steady state is 56–912/day), i.e. the one-off initial
backfill of the existing logbook. **No defect.**

But establishing that took bucketing 15,271 lines by day and cross-referencing a
different message type (`manual upload backfill enqueued`, 24 lines, emitted by a
different package). That is a reconstruction, not a lookup — and it is the routine
question about the loudest subsystem in the daemon. One `origin` field on the submit
line would have answered it directly.

- **Confusable with:** each other. A live QSO upload, a manual backfill row, and a
  stamp-sync mirror re-enqueue are indistinguishable, which is exactly what makes
  "why is this forwarder busy?" and "did my backfill actually run?" hard.
- **This directly obstructs an operator-directed decision.** SHIP GATE item (d) puts
  the build-version stamp at **+22%** of log size for a full version string. That cost
  is being paid disproportionately by these two lines — so the cheapest way to afford
  version stamping is to fix the redundancy here first. The two items should be decided
  together, not separately.
- **Record / restructure — and note the trade-off rather than just deleting a line.**
  The `submit` line's real job is *"we are about to call an external service"*, which
  has value if the daemon dies or hangs mid-upload; that is not nothing. But it does not
  need to be at Info for the ~98% of attempts that succeed within a tick. Options, in
  the order I would put them to the operator:
  1. `submit` → Debug, keep every outcome line at Info. Halves the package's footprint,
     keeps a full record of what happened, loses the in-flight breadcrumb at default
     level.
  2. Keep both, drop the fields `success` duplicates (`action`, `call`) and keep
     `qso_id` + `upstream_id`. Smaller win, no behavioural change.
  3. Log once, at the outcome, carrying a `duration_ms`. Loses the in-flight
     breadcrumb entirely.
  **Whichever is chosen, add an `origin` field naming what enqueued the row.** That is
  the part that adds information rather than removing it, and it is the actual finding.

---

## Tier 2

### F2. The stamp-sync chain is invisible at the default level from end to end

`worker.go:572-574`, inside `markSuccess`:

```go
if w.cfg.OnQsoStamped != nil {
    w.cfg.OnQsoStamped(ctx, row.QsoID)   // fires silently
}
```

That hook is the **trigger** for the row-mirror re-enqueue. Its **other half** — the
re-enqueue itself, `qsoservice/stamp_sync.go:69` — logs at **Debug**
(`qsoservice-logging-gaps.md` **Q4**).

So both ends of the mechanism are invisible in production. It is the built half of the
backlog's *smcloud stamp-drift → reconcile bandwidth churn* item, whose whole purpose is
to keep `in_sync` true so smcloud stays on the cheap hash check instead of dropping to
the full manifest GET — **~650 KB per drifted hour on a Malawi link, growing with the
logbook.**

Its failure mode is silent non-firing, which returns the daemon to the expensive path
with no signal at either end.

- **Confusable with:** the mechanism working. Also with it not being wired at all
  (`OnQsoStamped == nil` — a nil hook and a hook that fires produce the same silence).
- **Record:** fix with **Q4** as one change. The stamp-sync line moving Debug → Info is
  probably sufficient on its own — it carries the QSO id and the queued count — so this
  finding may need no new line here at all, only the knowledge that the trigger side is
  silent so nobody adds a second one.
- **Volume is not a concern here**, unlike F1: the observed heal rates were 7/39/34 rows
  through an evening and 94 after an email batch.

---

## Tier 3

### F3. A row retrying forever has no age, and the queue has no depth

`OutcomeUnreachable` retries **forever by design** (ADR 0038 — an outage must never
abandon a QSO). Each attempt logs `forwarding: host unreachable — will retry (no
give-up)`: 86 lines in the current log, at 1,101 B each.

What no line carries is **how long this row has been retrying** or **how many rows are
waiting behind it.** During an outage the queue grows and nothing records how deep it
got; afterwards, nothing distinguishes "one row retried 86 times over three days" from
"86 rows each failed once".

- **Confusable with:** each other, and with a healthy system that had a brief blip.
- **Ranked Tier 3 honestly, with the cost stated:** the retry-age fix is cheap (the
  queue row already carries its attempt count and first-queued time). **A queue-depth
  line is not** — `ClaimPendingUploadsWithContext` returns a batch, not a depth, so this
  would need a new count query on the worker tick. That may not be worth it; the SPA
  already surfaces queue state live. If only one is done, do the age.

---

## Second pass — findings F4-F16 (added 2026-08-01, separate source)

Raised independently after the first three and **each verified against the code before
filing** — all thirteen are real. None duplicates F1-F3. Cross-links to the sibling
files are noted per finding.

The first pass found the package too *loud*; this pass found what it is loud about.
Both halves are needed: **F1 says the two big lines carry no provenance, and F4-F16 say
the interesting transitions carry no line at all.** They are the same defect seen from
two ends, and the restructuring decision in F1 should be made with F5/F6/F7 in view —
those are the lines that should exist.

### F4. `registry.go` documents a startup-fatal log that does not exist — Tier 1

`registry.go:63-64` states, as the justification for a security rule:

> A constructor's error MUST NOT embed a credential value. These errors are **logged as
> a startup fatal by spawnForwarderWorkers** and raised again by the config PUT's
> startup probe, so an echoed value lands in the daemon log…

**`spawnForwarderWorkers` does not log it.** `cmd/smd/main.go:1294-1297` only *returns*
the error; it propagates to `main.go:132`, which does
`fmt.Fprintf(os.Stderr, …)` + `os.Exit(ExitError)`. **stderr, never `smd.log`.**

So a credential or configuration fault that stops the daemon produces `smd starting`
followed by `smd stopped` in the log an operator or remote admin reads, with the cause
only in the terminal or the journal.

- **Confusable with:** a clean shutdown, or a crash. This is the same defect as
  `api-logging-gaps.md` **A9** (`http.Server.ErrorLog` unset → stderr) and pairs with
  **A1** (no shutdown line at all): three separate routes by which the reason the daemon
  stopped lands somewhere other than `smd.log`.
- **The security rule itself is correct and must stay** — but its stated reason is
  wrong. Fix the log, then the comment becomes true; do not "fix" the comment by
  deleting the rule.
- **Record:** log the Build failure at Error in `spawnForwarderWorkers`, naming the
  forwarder and the fault — never the value, per the rule above.

### F5. Internally-caused transients enter the retry machine silently — Tier 1

`markTransientInternal` (`worker.go:475-484`) has **zero log calls**. It is reached from
the three fetch-failure sites (`:286`, `:310`, `:332`) and either schedules a retry or —
at the attempt cap — calls `markFailed`, terminally failing the row.

The asymmetry is the tell, again: a **forwarder-caused** transient logs
`forwarding: transient — will retry` (`:442`) and its exhaustion logs `retry budget
exhausted` (`:428`). The **internally-caused** one — a database failure, the more serious
cause — logs neither.

Worse, it self-erases: `last_error` is the only record, and a later success clears it.
So a row that spent a day cycling on DB errors and then succeeded leaves no trace at all.

- **Confusable with:** a row that uploaded first time. And an internal exhaustion is
  indistinguishable from a row that was never attempted.
- **Record:** mirror the two lines the forwarder-caused path already has, tagged as
  internal.

### F6. `forwarding: success` is logged BEFORE the local completion is persisted — Tier 1

`persistOutcome` logs success at `:370-378`, then calls `markSuccess` at `:379`.

Most persistence failures inside `markSuccess` do produce a later Error line — but the
**re-arm** case does not: `reArmed` (`:491`) logs at **Debug**. When it fires, the
transition never committed, the row stays pending, and **it will be submitted again**.

At production level, an upstream-accepted row that is still queued and will be re-sent
looks exactly like a completed upload.

- **Confusable with:** a completed upload — and the duplicate that follows will look
  like a fresh one, because of F1 (no provenance on the submit line). The two findings
  compound.
- **Record:** move the success line after the persistence call, or add an outcome field.
  The re-arm case is legitimately not an error, so Debug is defensible **only** if the
  Info line above it does not already claim success.

---

### F7. Soft-delete and missing-QSO terminal transitions are silent — Tier 2

`fetchQsoForAction:318` and `:322` call `markFailed` with a reason
(`"qso soft-deleted before insert forwarded"`, `"qso soft-deleted; delete row
supersedes"`) and **no log line**. `markFailed` reached via `persistOutcome` is preceded
by a Warn; these two are not.

An SSE event fires and `last_error` is written, but neither is durable — so "why did
this QSO never reach QRZ?" has no file answer, which is the same question
`qsoservice-logging-gaps.md` **Q5** (unrecorded fan-out) leaves open from the other end.

### F8. A reconcile can partially mutate the queue and then log only "run failed" — Tier 2

`reconcile.go:213-227` enqueues upserts, then deletes. If the delete enqueue fails, the
upserts are **already committed** and `RunOnce` returns the partial summary alongside
the error. `Run:129-131` then does `if sum, err := …; err != nil` and logs only
`Err(err)` — **discarding `sum`**.

So a run that queued 400 upserts and then failed logs the same line as one that did
nothing.

### F9. On-demand reconcile success has no summary log — Tier 2

`logSummary` (`:138`) is called **only** from the periodic `Run` loop. The API callback
invokes `RunOnce` directly (`cmd/smd/main.go:905`), so an operator-triggered reconcile
leaves only `POST /v1/smcloud/reconcile 200`.

**This is the forwarding half of `api-logging-gaps.md` A2** — fix both or the summary
still exists only in the HTTP response.

### F10. Reconcile summaries lose discovered, skipped and remaining work — Tier 2

Two losses in the same function:

- `:205-210` truncates upserts/deletes at `maxEnqueuePerRun` and records only
  `Truncated = true` — **not how much remains**, so the log cannot say whether one more
  run will finish or fifty.
- `:218` / `:227` copy only `res.Enqueued` from the `EnqueueResult`, dropping
  `SkippedDeleted`, `NotFound` and `SkippedNoHistory`.

That last one is the **second** place those classifications are discarded —
`qsoservice-logging-gaps.md` **Q2** is the first, at the producer. Fixing Q2 alone will
not surface them here, because this consumer drops them before they reach a log line.

### F11. SM Cloud's `applied=0` acknowledgement is logged as ordinary success — Tier 2

`smcloud.go:~309` deliberately accepts `applied=0` — "the stale-push guard held a newer
cloud copy" — as success, which is correct for a backup. But the disposition is
discarded and the worker emits the same `forwarding: success` line as `applied=1`.

**The two code paths disagree about how interesting this is:** reconcile treats
cloud-newer rows as an anomaly worth a **Warn** (`reconcile.go`, *"cloud rows NEWER than
local — unexpected in single-writer P1"*). The immediate upload path meets the same
condition and says nothing.

### F12. Graceful cancellation is misreported as a forwarding failure — Tier 2

QRZ, ClubLog and SM Cloud all classify any `client.Do` error as `OutcomeUnreachable` —
and QRZ's own comment lists `"or ctx cancel mid-flight"` among the causes
(`qrz.go:~243`). So on shutdown, in-flight uploads log `host unreachable — will retry`
plus, commonly, a persistence Error, because the completion write uses the cancelled
context.

**The claim path gets this right** — `tickOnce:155` suppresses the cancellation case
explicitly (`if ctx.Err() == nil`), with a comment saying shutdown is noise, not an
operational failure. The submit path does not.

- **Confusable with:** a real network outage. Every clean shutdown mid-upload leaves
  outage-shaped evidence, which is what makes the 86 `host unreachable` lines in the
  live log hard to read.

### F13. A ClubLog build with no injected API key looks healthy at startup — Tier 2

`clublog.go:247` deliberately constructs an **unusable** forwarder when the build-time
`CLUBLOG_API_KEY` is absent, rather than failing `Build` — and the reasoning
(`:318-330`) is sound: a `Build` error aborts the whole daemon, and returning
`Unreachable` rather than `Terminal` keeps the backlog queued so a later keyed build
ships it.

But nothing is logged at construction. The worker logs "started", and only when a row
arrives does `:324` report the missing key — as `OutcomeUnreachable`, producing
**`forwarding: host unreachable — will retry (no give-up)`** for a code path that
**makes no network request at all**.

- **Confusable with:** ClubLog actually being down. And because Unreachable retries
  forever by design, this emits a permanent stream of a message that is simply false —
  which is a concrete instance of **F3** (a row retrying forever with no age).
- **Record:** a startup Warn at construction. The per-attempt line then has context, and
  the operator learns at boot rather than never.

---

### F14. Other accepted upstream dispositions collapse into generic success — Tier 3

QRZ's insert `REPLACE`, update `OK` meaning newly-inserted, and delete `FAIL` meaning
already-absent (`qrz/response.go:~130`), plus ClubLog's OK / Modified / Duplicate 2xx
variants (`clublog.go:~476`). All are valid successes; the original disposition is lost.
Same shape as **F11**, lower stakes.

### F15. Worker startup omits the effective retry policy — Tier 3

`cmd/smd/main.go:1341` records tick and batch but not max attempts or backoff bounds.
Type defaults come from `RegisterDefaultRetry` and need not appear in `config.json`, so
later retry behaviour cannot be reconstructed from the log alone.

### F16. An unrecognised outcome's error is omitted from its own warning — Tier 3

`worker.go:402-411` logs `outcome` but not `res.Err`, while passing `errText(res.Err)`
into `markFailed` on the very next line. The cause survives in `last_error` and SSE, so
this is low priority — but the diagnostic detail is in hand and one field away.

---

## Verified NOT gaps — do not re-open these

Checked against the code and the live log on 2026-08-01.

- **NO CREDENTIAL LEAK — verified, not assumed.** The large error lines
  (`transient` 895 B, `host unreachable` 1,101 B average) are the obvious place to worry,
  since QRZ and ClubLog authenticate with API keys. **Checked two ways.** In the code:
  the QRZ key goes into the POST **form body** (`buildForm(f.apiKey, …)`,
  `qrz.go:227`), never the URL, and the error paths wrap `f.client.Do` and `f.endpoint`
  — an endpoint with no query string. In the data: a keyword scan of every
  `forwarding:*` line's `error` field in the live log returns **0 matches** for
  key/apikey/password/token/secret. Those lines are large because of Go error chains
  plus `errors.Op` context, not payloads. **Do not "sanitise" these lines** — and
  equally, do not assume a future forwarder inherits this property; the safety comes
  from the key being in the body, which is a per-forwarder choice.
- **The four forwarder implementations having ZERO log calls is correct architecture,
  not a gap.** `clublog` (601), `qrz` (375+275), `smcloud` (361+98), `stub` (176) —
  1,886 lines, no logging. Every outcome is returned as a typed `forwarding.Result` and
  logged centrally by `worker.persistOutcome` (`worker.go:367-411`), which handles
  Success / Terminal / Transient / Unreachable and has an explicit branch for an
  unrecognised outcome (`:411`). Adding lines inside the forwarders would duplicate the
  worker's and split the record across two places.
- **`registry.go` (507 lines, 0 log calls) is correct.** It is registration plumbing that
  **panics** on programmer error (duplicate type, nil constructor, invalid default
  retry) — the right response at init time — and `Build` returns typed errors that
  `cmd/smd:1294-1297` turns into a **startup failure**, deliberately: *"better to refuse
  to run than silently drop a destination the operator thought was active."*
- **Worker startup IS logged** — at the caller (`cmd/smd`, one Info line per spawned
  worker), which is the right place since that is where the one-worker-per-forwarder
  invariant is enforced. `Worker.Run` exits only on `ctx.Done()`, so there is no
  unexpected-exit case needing a line.
- **The worker's persistence-failure paths are well covered** — `markSuccess`,
  `markFailed` and the re-arm path each log at Error with forwarder, upload_id, qso_id
  and the error (e.g. `worker.go:561-567`).
- **Per-row panics are caught and logged** (`worker.go:201`, `forwarder: panic
  processing row; resetting to retry`), and the row is reset to retry rather than lost.
- **`forwarder: rows claimed` at Debug (`worker.go:167`) is the right level** — it fires
  per tick per forwarder and would be pure volume at Info. `tickOnce` correctly returns
  silently when there is nothing to claim.
- **`smcloud/reconcile.go`'s 4 sites are adequate** — `smcloud reconciler started` (78)
  and `smcloud reconcile: run complete` (165) in the live log give the run boundaries.
- **The 401-is-terminal problem is a BEHAVIOUR bug, not a logging gap.** It is already
  filed in `backlog.md` (a token rotation strands in-flight uploads). The terminal
  failure itself logs correctly at Warn with the cause — 4 occurrences in the live log,
  findable. Do not re-file it here.

---

## Suggested order

1. **F4** first — a one-line fix that closes the third of three routes by which "why did
   the daemon stop?" bypasses `smd.log` (with `api-logging-gaps.md` A1 and A9), and it
   makes an existing security comment true instead of aspirational.
2. **F1 with F5, F6, F7 and SHIP GATE item (d)** — one conversation. (d) adds ~22% to
   every line; F1 is where 43% of the lines are; F5/F6/F7 are the lines that should
   exist instead. Deciding them apart means paying the version cost on redundant lines
   while the informative ones stay missing.
3. **F13** — a startup Warn that stops a permanent stream of a message that is false.
4. **F12** — align the submit path with the claim path, which already suppresses
   cancellation correctly; this is what makes the 86 `host unreachable` lines readable.
5. **F11**, **F8**, **F10** — `F10` must be done with `qsoservice-logging-gaps.md`
   **Q2**: fixing Q2 alone will not surface the classifications, because this consumer
   drops them first.
6. **F9** with `api-logging-gaps.md` **A2** — two halves of one summary.
7. **F2** — resolve as part of `qsoservice-logging-gaps.md` **Q4**; likely no new line
   is needed on this side.
8. **F3** (retry-age half only), then **F14, F15, F16** whenever adjacent code is open.

Per the standing TDD directive, the behaviour statement for each is the
confusable-state clause above. For F1 specifically, the acceptance criterion is worth
writing as: *given a `forwarding: submit` line, I can tell a live QSO upload from a
manual backfill from a stamp-sync re-enqueue **without consulting another message
type**.*
