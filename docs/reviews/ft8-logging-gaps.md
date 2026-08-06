# URGENT TODO — `../../internal/ft8` logging gaps

**Status:** open · **Raised:** 2026-08-01 · **14 findings** · **Source:** package
logging review of `../../internal/ft8` (~7.6k non-test lines, 32 files, ~90 log call sites),
operator-directed, review only — no code was changed. Findings 1-10 from the first
pass; **11-14 added the same day from a second source** and verified against the code
before filing — one of them (12) corrects a first-pass conclusion.

**Why this is its own file and not a backlog line.** The operator asked for it
separately. `../backlog.md` still owns the ranking (see `../README.md` Tier 1) —
there is a one-line pointer to this file in its P2 block. Fold this file into the
backlog and delete it once the items ship; do not let the two drift.

**Relationship to the SHIP GATE entry.** The SHIP GATE log-coverage item
(`../backlog.md`, P2, operator-directed 2026-07-31) is the same defect shape — *an event
occurs and the operator can never learn it did* — found in the config/QSO-delete/
notification/build-version surfaces. This file is that same audit run against
`../../internal/ft8`. Findings 1–5 and 8 are pre-ship for the same reason: shipping to 7Q8AC
means the person diagnosing a fault is not the operator, and none of these is
recoverable retrospectively.

**Relationship to ADR 0061.** Nothing here needs the event store. Every item is a
line that should exist in `smd.log` regardless of where a future operator-facing
store lands. Findings 1–5 are the class the ADR's alarm pilot exists for (a daemon
safety action visible as behaviour, unreconstructable afterwards); 7–8 are the `qso`
category. Treat this as ship-gate work, not event-store scope.

---

## The axis used

Findings are sorted on the acceptance-criterion axis this project already uses
(root `../../CLAUDE.md` → Acceptance criteria): **can the operator tell this apart from the
nearest confusable state?**

A log line that exists but cannot separate two causes counts as a gap. A missing line
for something nobody acts on does not. Each finding below names its confusable state
explicitly — that clause is the load-bearing one and it is what makes the item
checkable.

**All items are "record the fact the code already has, at the point it already
decides"** — except finding 6, which carries a genuine open decision for the operator
and must not be pre-empted.

---

## Tier 1 — a safety action or QSO outcome the log cannot explain

### 1. Hub slow-reader eviction is silent, and it can end a live QSO

> ✅ **FIXED 2026-08-01** as a THREE-hub class fix — the audits found two sites, and
> review found a third the same day: `internal/events/hub.go`, which feeds the map's
> event stream. All three had the identical bare `close(ch); delete(...)`.
>
> Each now emits ONE Warn per evicted subscriber carrying `subscriber_id`, `event`,
> `queue_depth`, `queue_capacity`, `subs_before`, `subs_after` — the last two being
> what reveals a LAST-subscriber eviction, which in `internal/ft8` is the one with a
> TX consequence. Emitted after the mutex is released, like every other publish path.
>
> **The teardown is UNCHANGED — operator's ruling, and the reasoning is the part to
> preserve:** the enforced proxy is a FUNCTIONING SSE subscription, not an open
> browser tab. Once the channel overflows, operator-facing state is no longer
> flowing; EventSource reconnect plus the existing linger already IS the recovery
> distinction. Exempting eviction could leave TX running behind a dead display, or
> create a phantom subscriber that can never later unsubscribe. **Buffers stay at
> 8 (ft8) / 64 (bridge, events) until these new records show HEALTHY clients being
> evicted.**
>
> Canonical criterion: `internal/events/hub_eviction_test.go`. Rules in all three
> packages (E1-E8, H1-H4, F1-F5), with the three logging sites proven SEPARATELY —
> reverting one turns only its own package red, which is the "one fix, N sites, N
> proofs" lesson from earlier the same day. Production wiring for the general hub is
> guarded by an AST check on `cmd/smd/main.go`, because every test in
> `internal/events` installs its own logger and deleting the one real call left the
> whole suite green.
>
> **The TX-behaviour rules took two rounds and the first version of this note was
> wrong.** F3 originally called the handler's unsubscribe BY HAND, so deleting
> `defer unsub()` from the SSE handler would not have failed it — it now drives the
> real `HTTPHandler` with a client that stalls mid-write, which is what a slow
> reader is. And F4 was said to need a "double revert" because two mechanisms keep a
> reconnect from being torn down; that is true but MISLEADING, because the two cover
> different interleavings and F4 can only reach one. `onSubscriberAdded`'s
> `Timer.Stop` handles a reconnect before the timer fires; `onLingerExpired`'s
> subCount re-check handles one that lands AFTER it fired (service.go:394), which
> Stop cannot help with. **F5 covers that case and proves the guard alone.**


`internal/ft8/hub.go:72-75` — `publish` closes and deletes a subscriber channel whose
buffer is full, with no log line.

The consequence chain is not local, which is why this ranks first:

```
hub.publish buffer full  (hub.go:72)
  → close(ch) + delete
  → SSE handler returns   (handler.go:127-131)
  → Subscribe's unsub     (service.go:54-64)
  → onSubscriberRemoved   (service.go:382)
  → captureLinger elapses
  → onLingerExpired       (service.go:398)
  → disarmTx(false)       (service.go:420)
  → PTT dropped + any active contact abandoned
```

So an 8-deep (`subscriberBufferSize`) channel overrun **ends a live contact**. The
only trace is `ft8 tx: disarmed` followed by `ft8 seq: session abandoned reason=""`.

- **Confusable with:** the operator closing the browser. Same two log lines, byte for
  byte. One is normal operation; one is a defect in SM that just cost a QSO.
- **Record:** an eviction line carrying the subscriber id and the event name that
  overran, at Warn — eviction means SM discarded operator-facing data.
- **Not an ft8 outlier:** `../../internal/bridge/hub.go` has zero log calls and the same
  `close`+`delete` shape (`:157-158`, `:228`, `:246-247`). This is a hub-family gap;
  fix the pattern, not one file.

### 2. `ft8 seq: session abandoned` carries no callsign, and usually a blank reason

> ✅ **FIXED — reason 2026-08-04 (commit `3531e1ed`), callsign 2026-08-04 (`4665b5a9`).**
> The reason half shipped first as its own arc: `Abandon()` is reached from TWELVE
> places, not four, and only the two dial paths staged anything. The callsign half
> needed a NEW accessor — `ActiveCallsign()` returns OUR call (the TX identity), not
> the partner's, so the review's suggestion would not have worked as written;
> `partnerCallLocked()` reads the exchange BEFORE `abandonLocked` clears the pointers.
> Rules: `abandoncause_test.go` R1-R23.

`internal/ft8/sequencer.go:701` logs `Str("reason", reason)`, where `reason` is
`s.pendingEndReason` — staged only by the dial guard (`servicetx.go:1181`).

The four commonest routes into `Abandon()` via `disarmTx` stage nothing, so all four
log `reason: ""`:

| Route | Entry point |
|---|---|
| operator disarm | `ArmTx(false)` → `servicetx.go:133` |
| browser closed (linger expiry) | `onLingerExpired` → `service.go:420` |
| CAT dropped | `reconcileCat` → `service.go:496` |
| daemon Stop | `disarmTx(true)` |

No path logs **who** was being worked, though `ActiveCallsign()` is on the same struct
and already used by the decode loop (`service.go:856`).

- **Confusable with:** each other. Four different operator actions, one
  indistinguishable line — and none of them says which contact was lost.
- **Record:** the active callsign and mode on the abandon line; give the reasonless
  callers a reason (`operator`, `unattended`, `cat_lost`, `shutdown`).
- **Explicitly NOT a gap:** `AbandonIfCurrent` (`sequencer.go:863`) is fine — every
  caller passes a reason (`endReasonForRefusal`, `EndReasonDialMoved`).

### 3. `disarmTx` logs one message for five distinct causes

> ✅ **FIXED 2026-08-04 (`4665b5a9`), together with finding 14 as the review advised.**
> Six call sites (not five — the retune path was missed by the audit) now name their
> cause: `operator` / `unattended` / `cat_lost` / `shutdown` / `band_change` /
> `dial_moved`. The old `closing bool` is DERIVED from the cause rather than carried
> beside it — two parameters that must agree are a bug waiting. R21 pairs
> operator-vs-unattended rather than checking either alone, per this file's own
> instruction that a "a line was emitted" assertion is weaker than the rule.

`internal/ft8/servicetx.go:352` — `ft8 tx: disarmed`, reached from the operator, linger
expiry, CAT drop, dial move, and Stop.

Two of the five happen to have an adjacent line: `service.go:499` (`rig/CAT dropped`,
logged **after** the disarm) and `servicetx.go:1174` (`rig left the frequency`, logged
**before** it). Correlating either requires knowing the ordering.

The linger-expiry cause — the **only presence check the daemon actually enforces**
(root `../../CLAUDE.md`: the FT8 view must stay open; `service.go:398-429`) — logs nothing
of its own at all.

- **Confusable with:** an operator disarm. The one automatic safety teardown SM
  performs is indistinguishable from a button press.
- **Record:** a cause field on the disarm line. One argument through `disarmTx` /
  `disarmTxLocked` covers all five.

### 4. Dial-moved slot suppression is silent

> ✅ **FIXED 2026-08-06.** One Info line per suppressed slot ("ft8: slot
> suppressed") naming the rule (`dial_moved` / `unplaceable`), its SCOPE
> (`decode+sequencer+occupancy` vs `occupancy` — the two rules withhold
> different things, which this review's text glossed), the slot ref and the
> dial. TX slots deliberately excluded (expected every other slot of a run;
> they stay on the per-slot Debug record, which existed with all the fields
> but at a level the production log filters — the trap this fix names).
> Tests: `slotsuppression_test.go` G1–G3, confusable-state form: the same run
> feeds a quiet slot and a moved slot and asserts the DISTINCTION.

`internal/ft8/service.go:841`, `:850`, `:880` — when `slot.DialChanged` is true the
slot is: not decoded, **not fed to the sequencer**, and not published as occupancy.
None of the three writes a log line.

The operator observes Band Activity blank for a slot and a ladder that does not
advance. Nothing explains it.

`unplaceable` (`service.go:903` — CAT present but the dial could not be read)
suppresses occupancy the same way, also silently.

- **Confusable with:** a genuinely quiet band, or a stalled decoder.
- **Precedent — this exact failure has already cost a day.** The dial guard's
  *session-end* half had the same problem: per `../../internal/ft8/CLAUDE.md` invariant 5,
  *"the first on-air read of a WORKING dial guard was 'moving the dial does not stop
  TX', and it took a log dive to establish that it had (dogfood 2026-07-27)."* That
  was fixed by putting `end_reason` on the terminal SSE frame. The **per-slot** half
  got nothing, and it is the same class of invisible safety action.
- **Record:** one line per suppressed slot with the slot ref and which rule fired
  (`dial_moved` / `unplaceable` / `tx_slot`). Rate is bounded by slots, so Info is
  affordable; if not, Warn for the two safety cases and leave `tx_slot` at Debug —
  a TX slot is expected, the other two are not.

---

## Tier 2 — daemon decisions with on-air consequences, unlogged

### 5. Stalled-caller cool-off is unlogged, both when set and when it skips

- set: `work_sequencer.go:501` `coolOffStalledCallerLocked`
- skip: `work_sequencer.go:511` `inStallCooloffLocked`, consulted at
  `caller_sequencer.go:398`
- round exclusion: `stalledCalls`, consulted at `caller_sequencer.go:395`

SM sees a station calling and declines to answer — for 5 slots (75 s), or for the rest
of the CQ round. The *stall* is logged (`work_sequencer.go:196`, Warn, with callsign
and attempts); the resulting exclusion is not.

- **Confusable with:** nobody calling. From the operator's chair, "SM ignored a caller
  it can hear" and "the band went quiet" look identical.
- **The asymmetry is the tell.** Eleven lines below the cool-off skip, the
  *unencodable-caller* skip **is** logged at Info
  (`caller_sequencer.go:406-407`, "skipping answerer — our reply does not encode").
  The file already agrees that "why we did not answer this station" deserves a line.
  Two of the three reasons do not get one.
- **Also blocks measurement.** `stallCooloffSlots = 5` is flagged in-code as
  **the operator's number** (`work_sequencer.go:490`, 2026-07-31). With no log line
  there is no way to observe whether 5 is right.
- **Record:** callsign + expiry when the cool-off is set; callsign + reason when a
  pick is skipped for either exclusion.

### 6. A successful transmission logs nothing at the Service layer — DECIDED + FIXED

> ✅ **DECIDED 2026-08-04 (operator) and FIXED the same day (`4665b5a9`).**
> **The ruling: TWO INDEPENDENT WITNESSES per keyed transmission** — the wall
> key-to-unkey time and the sample count handed to the device. Each covers the
> other's blind spot: `keyed_ms` catches a play that returned instantly even when the
> audio layer reports success; `samples` catches a truncated or empty waveform even
> when the timing looks right. The diagnostic is their RELATIONSHIP.
>
> **WHAT IT DOES NOT PROVE, written into `txcontroller.go` so it is not overclaimed:**
> `samples` is what SM SUBMITTED, not what the device emitted. A device that accepts a
> full waveform and radiates nothing still logs a healthy line. THAT case is the drive
> alarm's (`internal/bridge/drivealarm.go`, watching the rig's PO meter); this record
> is what the alarm gets correlated against. It does NOT close the 2026-07-28 incident.
>
> `keyed_ms` is measured INSIDE the unkey closure, with the log defer registered
> BEFORE the unkey defer so LIFO runs it after — otherwise it would stop at the return
> statement and could not show a short transmission. A FAILED rung emits nothing.
> Rules: `txcontroller_test.go` T1-T3.

`../../internal/ft8/servicetx.go` has **6 log calls in 1415 lines**: armed (`:219`),
disarmed (`:352`), panicked (`:622`), failed (`:624`), retune (`:1134`), dial-moved
(`:1174`).

There is an intent line from the sequencer (`sequencer.go:1128`,
`ft8 seq: transmitting rung`) and a completion line only for the **final** rung
(`finalrung.go:152`). An intermediate rung therefore logs its intent and then nothing
— and silence means success.

**This is the shape of a real, expensive incident.** `internal/ft8/idleinhibit.go:8-15`
records it:

> On 2026-07-28 an unattended 80m run went silent mid-session: the daemon kept keying
> and the decode log kept recording "Transmitting", but no audio reached the rig for
> 24 minutes and 48 CQ calls.

The log asserted transmission and carried no evidence of it — no played sample count,
no play duration, no device identity. A healthy run and a run playing into a dead
device handle are byte-identical in `smd.log`.

- **Confusable with:** itself, one slot later, with the audio path dead.
- **OPERATOR'S DECISION — do not invent this.** What counts as evidence a
  transmission actually happened? Candidates, cheapest first: played sample count and
  derived duration (already in hand at `txcontroller.go:283`), the truncation amount
  (`:245`), the output device name/index (`txDeviceSpec`, `servicetx.go:1387`), a
  measured key-to-unkey wall time. The right answer depends on what would have made
  the 2026-07-28 incident visible in the log within one slot rather than 24 minutes.
- **Related but out of scope here:** there is a `deadSourceMonitor` for *capture*
  (`deadsource.go`) and no equivalent for *playback*. That is a detection gap, not a
  logging gap — file it separately if wanted; `idleinhibit.go:17-21` already says the
  inhibitor is a mitigation and *"not a substitute for making the playback path
  recover on its own."*

---

## Tier 3 — silent degradation of stored and forwarded data

### 7. `qsolog.go` has zero log calls and four silent degradations

All four land on data that is **stored and forwarded to QRZ / ClubLog / SM Cloud** —
durable, outbound, and not correctable by the operator after the fact.

| Line | Degradation | Result |
|---|---|---|
| `qsolog.go:40` | `utils.FrequencyToBand` returns `""` for an unrecognised dial (`internal/utils/frequency.go:102`) | QSO stored and forwarded with an empty BAND |
| `qsolog.go:48-51` | zero `c.StartedAt` falls back to the completion instant | wrong TIME_ON |
| `qsolog.go:77` | `utils.GridPath` returns `ok=false` on an unparseable grid (`internal/utils/bearing.go:31`) | ANT_AZ / DISTANCE silently unset |
| `qsolog.go:128` | malformed freq/time/date degrade to zero/blank in `NewLoggedQso` | blank fields in the SPA session row |

The second is the sharpest: the in-code comment calls it *"a path that failed to stamp
a start"* — i.e. it is a **known defect indicator**, and it is swallowed. If that
branch ever fires in production, nothing anywhere records that it did.

- **Confusable with:** a QSO that was genuinely fine. An empty BAND on a forwarded row
  looks like an odd dial, a parser bug, or a config problem, and there is no way to
  tell which.
- **Record:** Warn on each, with the input that failed to resolve. Do **not** change
  the fail-soft behaviour — degrading is correct (`enrichment never blocks logging`);
  degrading *invisibly* is the defect.

### 8. A completed exchange with no sink wired is discarded silently

`internal/ft8/service.go:274` — `if s.qsoLogger != nil { ... }`, with no `else`.

`finalrung.go:152` still logs `QSO complete` on the same path, so the log **affirms a
QSO that was never handed anywhere**.

- **Confusable with:** a QSO that was logged. The log says it completed; the logbook
  disagrees, and nothing bridges the two.
- Low likelihood in production — `../../cmd/smd` always wires the sink — but the whole
  "a completed *exchange* is a QSO" invariant rests on this call, and the `else` is
  free.
- **Record:** Error on the nil-sink branch. This is a wiring bug, not a runtime
  condition.

---

## Tier 4 — lower value, record for completeness

### 9. Idle inhibition logs only its failure

`internal/ft8/idleinhibit.go:60` logs the acquire failure. The successful acquire
(`:72`) and the release (`takeIdleReleaseLocked`, `:80`) log nothing.

The file exists *because* of a suspected host-sleep event mid-run. "Inhibition held
from T1 to T2" is precisely the fact needed to reconstruct one, and it is the fact
not recorded.

### 10. Pre-key safety refusals share a message with audio failures

`preKeyDialCheck` (`servicetx.go:1188`) refuses inside the TX goroutine, so the refusal
surfaces as the generic `ft8 tx: transmission failed` at `servicetx.go:624` — the same
message an audio play error produces.

The sentinel (`ErrTxDialUnknown` / `ErrTxSuperseded`) is in the error chain and so is
greppable, but *"SM declined to key for safety"* and *"the audio device failed"* are
not separable by message, and only the first is a working guard doing its job.

---

## Second pass — findings 11-14 (added 2026-08-01, separate source)

Raised independently after the first ten and verified against the code the same day.
Numbering continues rather than re-sorting, so existing references stay valid; each
carries the tier it belongs to. **Finding 12 corrects an entry in the "NOT gaps"
section below** — that entry has been amended rather than deleted, so the correction
is visible.

### 11. Late-slot transmit deferrals are silent — Tier 2

**8 sites**, not one — every sequencer family plus the immediate opening:

| Site | Path |
|---|---|
| `sequencer.go:1065` | standard answer-a-CQ |
| `sequencer.go:1218` | Field Day answer |
| `sequencer.go:1455` | `fireOpening` — the immediate opening, **all** families |
| `caller_sequencer.go:199` | Call-CQ |
| `work_sequencer.go:140` | work-a-caller |
| `work_sequencer.go:387` | work-a-caller (FD) |
| `type4_sequencer.go:134` | type-4 answer |
| `type4_sequencer.go:332` | type-4 work |

When the decode lands outside `txLateWindowSec` the rung is skipped with no log at any
level. Repeated lateness leaves a session active indefinitely with no explanation for
the missing RF.

Three things make this worse than the one-line summary:

- **Two different causes share one silent branch.** Every site reads
  `if dt < 0 || dt > txLateWindowSec`. `dt > txLateWindowSec` is "the decode was too
  slow" — expected, and the thing ADR 0032's truncation budget exists for. `dt < 0` is
  "our slot has not started yet", which is a clock or slot-ref fault, a completely
  different problem. They are indistinguishable today because neither is recorded.
- **There is a second, sibling silent skip at 7 more sites** — the `lastTxSlot` dedup
  (`sequencer.go:1076`, `:1226`; `caller_sequencer.go:207`; `work_sequencer.go:148`,
  `:395`; `type4_sequencer.go:142`, `:332`), which skips a rung because one already went
  out in this physical slot. Also unlogged. So there are three distinct reasons a slot
  passes without RF, and all three look identical.
- **`fireOpening` is the quietest of the eight.** The `OnSlot` handlers at least
  republish status before returning (`sequencer.go:1066-1067`); `:1455-1457` returns
  with no log **and** no SSE frame.

- **Confusable with:** a quiet band, a session waiting for the partner, or a wedged
  sequencer. From the operator's chair all four are "no RF and no explanation".
- **Blocks measurement, exactly like finding 5.** `txLateWindowSec = 4.5`
  (`sequencer.go:39`) is a package var with no operator-observable behaviour attached.
  How often the guard fires is the whole justification for the PocketFFT build being
  preferred for TX (`../../internal/ft8/CLAUDE.md`), and it is currently unmeasurable on a
  live station.
- **Record:** the deferral with `dt_s`, the rung, and which of the three rules fired.
  These sites already have `dt` in hand. Rate is bounded by slots. Note the successful
  path already logs `dt_s` on `ft8 seq: transmitting rung` (`:1128`, `:1470`) — so the
  fast rungs are measurable and the deferred ones are not, which is backwards.

### 12. Decode-log line loss is reported only at Close — Tier 2

`decodelog.go:183` — `enqueue` drops the line and bumps `d.dropped` (`:190`) with no
log. The warning exists only in `Close()` (`:246`), which runs when the capture session
is released.

So a long-running capture loses lines for **hours** with no notification, and the
operator learns of it only when they close the FT8 view — by which point the ALL.TXT
record they would have consulted is already incomplete.

- **Confusable with:** a quiet band. The decode log simply has fewer lines in it, and
  nothing distinguishes "nothing was heard" from "the disk stalled and SM threw the
  lines away".
- **Record:** a rate-limited Warn on the first drop of a session (and thereafter on a
  timer or a growth threshold), not just the total at Close. The counter already exists.
- **This corrects the "NOT gaps" entry below**, which credited `decodelog.go` with
  covering dropped lines. The line exists; the coverage does not.

### 13. The final decode-log flush and close discard their errors — Tier 3

`decodelog.go:152-153`, in `run`'s deferred cleanup:

```go
_ = d.w.Flush()
_ = d.wc.Close()
```

Data lost during shutdown or capture release therefore has no diagnostic record.

**The asymmetry is the tell, same shape as finding 5.** Three lines below, the
normal-path flush **does** log its error (`:155-158`,
`"ft8: decode log flush failed"`). The file already agrees a flush error is worth a
line — and the one flush that carries the most (everything buffered at teardown, after
the drain loop at `:168-175`) is the one that doesn't.

Same class, same file: `_, _ = d.w.WriteString(line)` at `:163` and `:171` discards
every write error outright, so a disk that fails mid-session is invisible until the
deferred flush also fails silently.

- **Confusable with:** a clean shutdown. The decode log just ends.
- **Record:** log both deferred errors at Warn. `d.log` is already on the struct and
  already used inside this same deferred function (`:150`).

### 14. TX teardown reports success after a cleanup failure — Tier 4

> ✅ **FIXED 2026-08-04 (`4665b5a9`), with finding 3 as advised — same line, different axis.**

`servicetx.go:347-350` discards `dev.Stop()` and `dev.Close()`, then `:351-353` logs
`ft8 tx: disarmed` unconditionally on `wasArmed`.

Logging really is the only exposure path here, and the note is right that it is closed:
`disarmTx` returns nothing, and `ArmTx(false)` returns `nil` unconditionally
(`:130-135`), so the HTTP layer answers 202 regardless. An audio-backend cleanup
failure — the device handle not released, which is the shape that would leave the next
arm unable to acquire it — is unobservable from anywhere.

Same file, same pattern: `armTx`'s error path discards `_ = player.Close()` at `:180`.

- **Confusable with:** a clean disarm.
- **Shares a line with finding 3, on a different axis.** Finding 3 is that
  `ft8 tx: disarmed` cannot say *why* it happened; this is that it cannot say *whether
  it worked*. Both fixes touch `servicetx.go:352` — do them together.
- **Record:** the Stop/Close errors at Warn, and do not assert a clean teardown that
  was not verified.

---

## Verified NOT gaps — do not re-open these

Recorded deliberately: a later pass that re-derives this list will otherwise re-file
them. Each was checked against the code on 2026-08-01.

- **The ~30 TX/QSO refusal sentinels are already logged.** They exit through HTTP, and
  `logRequests` puts `code`, `error` and `op` on the access line
  (`internal/api/middleware.go:308-313`). `ft8_tx_not_armed`, `rig_dial_unknown`,
  `ft8_qso_in_progress` etc. are queryable today. **Adding daemon-side lines here
  would duplicate**, not improve.
- **The four sequencer files are well covered** (`sequencer.go`, `caller_sequencer.go`,
  `work_sequencer.go`, `type4_sequencer.go` — ~70 of the package's ~90 call sites):
  rung intent, transmit failure, superseded drop, repeat-cap abandon, skip-if-silent,
  completion, auto-work pickup, next-answerer, unencodable-answerer skip.
- **Per-decode detail is deliberately Debug**, with the reasoning written down
  (`decode.go:75-83`): 12–16 decodes/slot is a firehose at Info, and the live view is
  the SPA Band Activity, not the log. Correct as-is — do not promote it.
- **`scheduler.go`** logs both the lateness skip (`:279`) and the consumer-backpressure
  drop (`:339`), each with a counter.
- **`deadsource.go`** has zero log calls by design — its callback is logged with the
  reason at `service.go:611`.
- **`decodelog.go`** covers open failure, dir-create failure, permission failure,
  normal-path flush failure (`:156`), and writer panic. ~~and dropped lines (`:246`)~~
  — **CORRECTED 2026-08-01 by finding 12:** the dropped-lines warning exists but fires
  only in `Close()`, so drops are invisible for the life of a capture session. The
  deferred flush/close and the per-line writes discard their errors entirely
  (finding 13). Do not read this bullet as clearing `decodelog.go`.
- **Pure files correctly have none:** `modulate.go`, `wav.go`, `ring.go`, `convert.go`,
  `type4.go`, `caller.go`, `field_day.go`, `sequence.go`.

---

## Progress (2026-08-06)

**6 of 14 closed:** 1 (2026-08-01), then 6, 2, 3, 14 (2026-08-04), then 4
(2026-08-06). **8 remain:** 5, 11 (the rest of the "a slot passed and the log
cannot say why" cluster — do together, NEXT), 7, 8 (data integrity, small),
12, 13 (one file — NB the decode log became SERVICE-lifetime on 2026-08-06,
which makes 12 MORE pressing: line-loss reporting now defers to daemon
shutdown, not view close), 9, 10 (whenever adjacent).

## Suggested order

1. **6** first — it is the only item carrying an open decision, and the answer shapes
   what finding 4's suppression lines should carry.
2. **1, 2, 3** together — they are one story (a contact ended, and the log cannot say
   why), and 1 spans `../../internal/bridge/hub.go` too.
3. **4**, then **5** and **11** together — all three are "a slot passed and the log
   cannot say why", and 11 is the highest-frequency of them.
4. **7, 8** — small, independent, and the highest data-integrity value per line
   changed.
5. **12, 13** together — one file, and 12 is the one an operator would actually hit.
6. **9, 10, 14** whenever adjacent code is open. **14 must be done with 3** — same
   line, different axis.

Per the standing TDD directive: state the behaviour, write the test, then the code —
and for each of these the behaviour statement is the confusable-state clause already
written above. A test that asserts only "a line was emitted" is weaker than the rule;
assert that the two confusable states produce **distinguishable** output.
