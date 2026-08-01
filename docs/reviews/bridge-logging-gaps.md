# URGENT TODO — `internal/bridge` logging gaps

**Status:** open · **Raised:** 2026-08-01 · **14 findings** · **Source:** package
logging review of `internal/bridge` (6,294 non-test lines, 13 files, **66 log call
sites**), operator-directed, review only — no code was changed. B1-B8 from the first
pass; **B9-B14 added the same day from a second source** and verified before filing —
one of them (B10) corrects a first-pass conclusion, and a seventh item in that batch
was dropped as a duplicate of B8.

**Sibling document:** [`ft8-logging-gaps.md`](ft8-logging-gaps.md) — same audit, same
axis, run against `internal/ft8` earlier the same day. **Finding B1 below is the same
defect as that file's finding 1** and should be fixed once, in both hubs.

**Why this is its own file.** The operator asked for it separately. `docs/backlog.md`
still owns the ranking and carries a pointer line; fold this in and delete it once the
findings ship. Two open lists is the drift `docs/README.md` exists to prevent.

**Headline, and it is not what I expected going in.** The TX-safety surfaces —
`txconfirm.go`, `ft8tx.go`, `tune.go`, `txrecheck.go`, `meters.go` — are **well
logged**, and that is not an accident: they are the files that got attention after real
on-air incidents. The gaps cluster in exactly two places: the code that decides *not*
to act (a guard declining to arm, a subscriber being dropped), and the code that
accepts a known-unfixable race and never records whether it fires.

---

## The axis used

Same as the ft8 review: **can the operator tell this apart from the nearest confusable
state?** A log line that exists but cannot separate two causes counts as a gap; a
missing line for something nobody acts on does not.

---

## Tier 1 — a safety guard that declines to run, invisibly

### B1. The drive-collapse detector declines to arm silently — and one branch is reported NOWHERE

`internal/bridge/drivealarm.go:109-146` — `armDriveWatch` has **two** early returns
that leave the transmission unmonitored, neither of which logs:

| Line | Condition | Reported on the SSE wire? | Logged? |
|---|---|---|---|
| `:120-122` | `!s.meterSeenSinceTx` — no evidence the instrument is alive | **NO — nowhere** | no |
| `:134-136` | meter selection is not PO | yes, via `RigStatePayload.DriveMonitor` | no |

**The second branch is half-covered; the first is not covered at all.**
`RigStatePayload.DriveMonitor` is published from exactly one place
(`pipeline.go:1071-1079`) and reports **the meter selection**, not whether the watch
armed. The `meterSeenSinceTx` decline is a separate, earlier condition — a rig whose
CAT link is up but which is not pushing RM frames (AI not armed, a rigdef that pushes
no meter, the first transmission after a connect). On that path the wire says nothing
new, so the SPA keeps displaying whatever the last meter selection implied — most
likely `ok` — while the detector has quietly declined to arm.

**The file's own comment names this exact shape as the worst outcome available.**
`drivealarm.go:87-90`, on `driveMonitorFor`:

> ONE rule with TWO readers, deliberately: `armDriveWatch` ACTS on it and
> `mapStatusToPayload` TELLS THE OPERATOR about it. Split into two comparisons they
> could drift, and the failure would be the worst shape available — **a banner saying
> monitoring is on while the detector has quietly declined to arm.**

That reasoning is correct and the guard it describes works — for the meter-selection
branch. The `meterSeenSinceTx` branch sits *above* it and is not covered by it.

**This also runs against a standing operator instruction.** `events.go:118-120`
records it verbatim:

> Operator's instruction 2026-07-31, after two false NO-RF alarms caused by the rig's
> meter being on ALC: **being silently unprotected is worse than being told.**

That instruction was answered on the **SSE wire only**. `smd.log` still cannot answer
"was drive-collapse detection running during that transmission?" for either branch —
and the SSE banner is transient client state, so nothing durable records it.

- **Confusable with:** a monitored transmission that produced no alarm. Those are
  opposite facts — one is positive evidence RF was leaving the rig, the other is no
  evidence at all — and they are indistinguishable in the log today.
- **Record:** one line per transmission stating whether the watch armed and, if not,
  which branch declined. The gap measurement already runs on exactly these
  transmissions by design (`:111-115`, `metergap_test.go` G6), so the summary line
  that already exists (`meters.go:347`, `:351`) is the natural carrier.
- **Note the ADR 0060 interaction:** that ADR is parked on alarm behaviour data. A
  "detector was not armed" count is part of that data and does not exist today.

### B2. Hub slow-reader eviction is silent — SAME DEFECT AS `ft8-logging-gaps.md` FINDING 1

`internal/bridge/hub.go:157-158` — `publish` closes and deletes a full subscriber's
channel with no log. The file has **260 lines and zero log calls**.

```go
for id, ch := range h.subs {
    select {
    case ch <- evt:
    default:
        close(ch)        // <- operator-facing rig state discarded, silently
        delete(h.subs, id)
    }
}
```

Consequences specific to this hub:

- The operator's rig display stops updating. The SPA's `EventSource` reconnects, so it
  usually self-heals — which is precisely why it needs a log line: a recurring
  eviction is invisible.
- Eviction runs the unsubscribe path, so it silently changes the multi-tab client
  count (`service.go:780-806` → `publishClientCount`) and can flip another tab's
  "another tab is controlling this rig" banner with no cause on record.
- **Better than the ft8 case in one respect, and worth stating so the fix is not
  over-scoped:** a standing tx-alarm survives eviction, because `lastTxAlarm` is
  hub-cached (`hub.go:149-151`) and replayed to the reconnecting subscriber. The
  safety banner is not lost. Occupancy-style staleness is the ft8 hub's problem, not
  this one.

- **Confusable with:** a normal tab close, or a network blip.
- **Record:** an eviction line at Warn with the subscriber id and the event name that
  overran. **Fix both hubs together** — the ft8 review filed the identical defect at
  `internal/ft8/hub.go:72-75`.

---

## Tier 2 — an outcome the log misreports

### B3. A failed key whose defensive unkey succeeds reads as a normal transmission

`ft8tx.go:140` — when the `tx-on` write fails, the recovery path is careful and
correct: clear state, fire a best-effort `tx_off`, then confirm or alarm.

But the branches are logged asymmetrically:

- defensive `tx_off` **fails** → `ErrorWith` (`:166-167`) + `raiseTxAlarm` (logged at
  `txconfirm.go:358`). Loud, correct.
- defensive `tx_off` **succeeds** → `confirmTxIdle` logs
  `bridge: tx state confirmed idle` (`txconfirm.go:233`) — **the same line a normal,
  successful transmission produces on its normal release.**

So in the bridge's own log, "we keyed, transmitted for 12.6 s, unkeyed cleanly" and
"the key write failed, nothing went on air, we defensively unkeyed" are the same
entry. The key-write failure itself is never logged here.

- **Mitigating fact, stated so this is not over-ranked:** the error returns to
  `internal/ft8`, which logs `ft8 tx: transmission failed` (`servicetx.go:624`). The
  episode is reconstructable — but only by joining two packages' lines, and only
  because a caller happens to log. The bridge's own record is wrong on its face.
- **Confusable with:** a successful transmission. Same line, opposite meaning.
- **Record:** log the `tx-on` write failure at the point it happens, before the
  recovery path branches.

### B4. The accepted CI-V late-ACK race has no evidence path

`command.go:305-332`. The doc comment is unusually good and explicitly accepts the
risk:

> Protocol-inherent limitation (F4, review 2026-07-01, **accepted not fixed**): CI-V
> FB/FA ACKs carry no correlation id, so a LATE ack … cannot be told apart from the
> next command's ack. … **The window is narrow and bounded** … documented rather than
> papered over.

The dismissal may well be right. But `deliverAck` drops a no-waiter ACK at `:325-327`
with no log and no counter, so **there is no way to find out** whether the window is
being hit on the operator's actual rig. A late ACK that lands with no waiter installed
is the cheap, safe observable that would answer it — and it is discarded.

This is the shape the root `CLAUDE.md` warns about directly:

> A risk you NOTICE and dismiss must be dismissed on evidence, not on estimated cost.
> The words to catch yourself on are "narrow race", "not worth testing" … check
> whether the system ALREADY CARRIES the fact that would settle it.

The system does carry it, at `:325`, and throws it away.

- **Confusable with:** nothing going wrong. A spurious accept manifests as a command
  the operator believes applied and did not — a wrong frequency or mode with no trace.
- **Record:** a counter plus a Debug (or rate-limited Info) line on the no-waiter drop.
  This does not close the race — nothing in code can — but it converts an assumption
  into a measurement.

### B5. `ErrCommandNoAck` states its own uncertainty and does not record it

`command.go:61-65`, `:253-256` — the comment is explicit:

> The command **may or may not have applied**; the bridge can't confirm, so it
> surfaces the uncertainty rather than synthesizing a state it didn't see acknowledged.

It surfaces it to the caller. It does not record it. For the generic command path this
is covered (see NOT-gaps below — it exits via HTTP and lands on the access log with its
code). For `writeKeyedLine`'s keyed callers the coverage is uneven: some log
(`tune.go:201`, `ft8tx.go:166`), and a mid-batch CI-V failure in `sendCommandsCIV`
(`:208-221`) leaves earlier ops applied and later ones not — a **partially applied
batch**, which the comment acknowledges CI-V cannot make atomic — with no line naming
which op the batch stopped at beyond the returned error.

- **Confusable with:** a batch that fully applied, or one that fully failed. A "tune to
  band" that set the frequency and not the mode is the concrete case.
- **Record:** log the op that failed and the number applied before it, at the point the
  batch aborts.

---

## Tier 3 — lower value, record for completeness

### B6. Multi-tab client-count changes are unlogged

`service.go:780-806` publishes `rig-clients` on join and leave but logs nothing. With
two tabs open, a command could have come from either, and the daemon keeps no record of
how many were attached at a given time.

`events.go:62-64` is right that this is advisory and that the dangerous cases
(double-key, mic-steal, write-mid-TX) are already prevented by `keyMu`/`ErrTxActive`.
Filed low for that reason — but "how many tabs were open" is the sort of fact that is
free at the time and unrecoverable afterwards, and a full operating lock is named there
as future work.

### B7. `TriggerBootstrap`'s safe no-op is indistinguishable from a bootstrap that ran

`service.go:832+` is documented as a deliberate no-op when the pipeline is not running.
The SSE handler logs a bootstrap *failure* (`handler.go:95`) but nothing distinguishes
"bootstrap wrote the READ command" from "bootstrap silently did nothing because no rig
was up". The operator-visible symptom is the same either way: the SPA shows defaults.

### B8. `pipeline.go:307` discards a client close error

`_ = client.Close()` — the only discarded error in the package (the whole package has
just one such site, which is worth noting as a positive). Serial-port close failures
matter here because the supervisor reopens the port on the next cycle, and a port that
did not close is the reason an open fails as busy.

---

## Second pass — findings B9-B14 (added 2026-08-01, separate source)

Raised independently after the first eight and verified against the code the same day.
A seventh item in that batch — serial close errors discarded at `pipeline.go:307` — is
an exact duplicate of **B8** and was not re-filed. **B10 corrects a claim in the
"NOT gaps" section below**, which has been struck and amended rather than deleted.

### B9. A liveness-probe write failure loses its root cause — Tier 1

`pipeline.go:666-676`. When the no-data re-arm/re-probe write fails, `werr` goes into
the SSE details map and the pipeline returns `exitTransient`. **It is never logged.**

**The argument for fixing this is written in the file, eight lines below, and applied
only to the sibling branch.** `:690-694`, on the terminal-read path:

> Log the real cause here: the pipeline returns only an exit classification (not err)
> to the supervisor, and **the SPA renders `serial_port_error` from the code (ignoring
> `details.error`)**, so this is the one place the underlying reason reaches `smd.log`
> for debugging.

Both branches publish the *same* code (`RigCodeSerialError`) with the cause in
`details` — which that comment states the SPA discards. So at `:675` the error reaches
**nobody**: not the log, not the operator, not the supervisor (which receives only the
exit class). The reasoning was correct and was applied to one of two adjacent branches.

- **Confusable with:** the terminal-read failure at `:695-698`, which produces an
  identical SSE code *and* a log line naming the cause. Same operator symptom, one
  diagnosable and one not.
- **Record:** the same `WarnWith().Err(werr)` line the sibling branch already has.

### B10. The identity paths log nothing — including a PERMANENT halt — Tier 1

Three sites, and the third is the serious one:

| Site | What happens | Logged? |
|---|---|---|
| `pipeline.go:736` | identity re-probe READ write failed | **Debug** only |
| `pipeline.go:814-817` | unrecognised ID → all operator write paths blocked **indefinitely** | no |
| `pipeline.go:825-826` | ID mismatch → `exitPermanent`, pipeline halts for the process lifetime | **no** |

The mismatch case is the sharpest thing in this document. Trace it end to end:
`readLoop:825` publishes the SSE event and returns `exitPermanent` → `runPipeline`
propagates it → `runSupervisor:552` does `case exitContextCancelled, exitPermanent:
return` — **with no log**. The bridge is then dead until the daemon restarts, and
`smd.log` contains **not one line about it**.

**Every other `exitPermanent` site logs before returning** — `:182` (unknown driver),
`:191` (serial config), `:217` (missing INIT), `:227` (missing READ). Identity mismatch
is the single permanent halt that does not, and it is the one most likely to be hit in
the field: it fires when the operator has the wrong `bridge.cat.driver`, which is a
first-run configuration mistake, on a deployment where the person reading the log is
not the operator.

The unrecognised case is nearly as bad in a different way: it blocks every write path
(`ErrRigIdentityUnverified`) for as long as the condition lasts, while state display
keeps working — so the rig looks fine and every command is refused, with the only
record being one SSE toast the operator may have missed or dismissed.

- **Confusable with:** a bridge that was never enabled, or a rig that is simply quiet.
  All three produce a daemon that reports nothing.
- **Record:** log at `:814` and `:825` before publishing; promote `:736` from Debug —
  a re-probe that keeps failing is why identity never confirms.

### B11. Passive liveness loss and recovery have no durable transition — Tier 2

`pipeline.go:657-660` announces the quiet rig on the SSE only (`announcedDisconnect`
gate, no log). `:706-715` resets `announcedDisconnect`, `livenessDeadline` and
`noDataStrikes` on the first returning rig frame — silently.

So "the rig went unreachable at T1 and came back at T2" is unreconstructable from the
log. That matters more than it looks, because `noDataStrikes` drives `RigConnected`,
which gates FT8 capture: the *effect* is logged in the other package
(`internal/ft8/service.go:499`, "rig/CAT dropped — releasing capture") but the
bridge-side cause and timing are not, so the two cannot be joined.

- **Confusable with:** a rig that was never connected. Also, on the recovery side, a
  session that never dropped at all.
- **Record:** a transition line each way (lost / restored) with the strike count. These
  are edges, not per-frame events, so the volume is trivial.

### B12. Normal tune key/unkey is silent — Tier 2

`tune.go:225` (`publishTuneState(true); return nil`) and `finishTune` (`:373` region)
produce **no log** on a successful operator-driven tune. Yet the abnormal paths all
log: auto-off (`:417`), disconnect-release (`:438`), write failures (`:201`, `:308`,
`:317`, `:337`, `:361`).

This is a **transmitting** feature — a real RF carrier into an amplifier — and the
ordinary case of the operator keying and unkeying it leaves no trace at all.

- **Confusable with:** no tune having happened. "Was the carrier up when the amp
  faulted?" is not answerable.
- **Same shape as `ft8-logging-gaps.md` finding 6** (a successful transmission logs
  nothing at the Service layer). Two packages, one habit: failure paths got the
  attention, the success path is where the missing evidence is. Consider settling both
  with one decision about what a keyed-transmission record should carry.

### B13. Restore-encoding failures are dropped before any write, so the write-failure logs never fire — Tier 2

- `tune.go:624` `encodeTuneRestore` — **two** independent `if …; err == nil { append }`
  guards, one for power and one for mode.
- `ft8tx.go:341` — the mode restore is inside `if m, err := …; err == nil`, so an
  encode failure skips the logged write at `:344-347` entirely.

The tune case is the worse of the two: because power and mode are encoded separately
and appended independently, a partial encode yields a **partial restore** — the rig
left on tune power, or left in RTTY — and the function returns a non-empty line, so
the write succeeds and every existing log line reports success.

The in-code justification (`:619-622`) is that the values came from decoded rig pushes
and therefore encode, so a miss is defensive. That is a reasonable prior, and it is
also exactly the kind of "cannot happen" that the root `CLAUDE.md` says must be
dismissed on evidence rather than estimate. A dropped encode is free to record.

- **Confusable with:** a clean restore. The operator's rig is quietly left in the wrong
  mode or at tune power, and the log says the tune ended normally.
- **Record:** log each dropped encode with the command name and value.

### B14. Malformed frequency and power values vanish — Tier 3, but not cosmetic

`pipeline.go:1027-1031` (`VFOAFREQ`/`VFOBFREQ` via `parseFreqHz`) and `:1065-1069`
(`TXPWR` via `strconv.Atoi`) discard the parse error and simply leave the field
unpopulated.

**Why this is worth more than its severity suggests.** Both halves verified:
an unpopulated field means `captureDialFreq` (`tune.go:525-532`) takes its
`if p.VfoA != 0` branch and **preserves the previous value**. So a rig emitting
malformed frequency frames leaves `CurrentDialMHz` **stale rather than unknown** — and
stale is the worse of the two, because `internal/ft8`'s pre-key gate compares the
current reading against the pinned one and a stale value *matches*. The FT8 invariant
"never key unless the daemon can positively confirm the rig's frequency" is satisfied
by a reading that is no longer being updated.

This needs corrupt serial traffic or rigdef drift to occur, which is precisely the
argument for the log line: it is otherwise undetectable, and there is no other signal
that frequency decoding has stopped working.

- **Confusable with:** a rig that simply is not pushing frequency.
- **Record:** a rate-limited Warn with the raw value. Rate-limiting matters here — a
  broken rigdef would otherwise emit per frame.

---

## Verified NOT gaps — do not re-open these

Checked against the code on 2026-08-01. Recorded so a later pass does not re-file them.

- **`SendCommands`' five refusal paths are already logged.** `ErrRigNotConnected`,
  `ErrRigIdentityUnverified`, not-writable, `ErrTxActive`, `ErrTxUncertain`
  (`command.go:135-163`) all return without a log line — but the function has **exactly
  one caller**, `internal/api/handler_rig_command.go:70`, so every refusal exits through
  HTTP and lands on the access-log line with its `code`/`error`/`op` fields
  (`internal/api/middleware.go:308-313`). Adding daemon-side lines would duplicate.
  This is the same conclusion the ft8 review reached about its TX sentinels.
- **The supervisor's SSE dedup is NOT a logging gap.** `publishExitBridgeError` /
  `publishExitDisconnect` (`pipeline.go:953`, `:972`) suppress *repeat toasts* during
  retry storms — but the log line at each exit site is **unconditional and precedes the
  publish** (e.g. `:217-222`, `:227-232`, `:250-272`). So every retry cycle is in
  `smd.log` while the operator gets one toast. That is the correct split, deliberately
  made; leave it alone.
- **The TX-safety files are well covered** — 28 of the package's 66 call sites:
  `txconfirm.go` (9, including `CHECK YOUR RADIO` at `:330` and the alarm code at
  `:358`), `ft8tx.go` (7), `tune.go` (7), `txrecheck.go` (7, with the probe path at
  Debug by design). Key/unkey write failures, auto-off firings, disconnect-during-TX,
  and alarm raise/clear all log with reasons.
- **`meters.go` per-transmission summary exists** (`:347`, `:351`, via `withMeterGap`)
  and carries the gap measurement — this is the line finding B1 should extend, not
  replace.
- **`events.go` (283 lines, 0 log calls) is correct** — it is type and constant
  declarations only, with no logic.
- **`service.go`'s config-clamp warnings** (`:425`, `:442`, `:451`, `:457`, `:470`)
  cover every safety ceiling that gets clamped at construction. Good as-is.
- **`pipeline.go` has the most call sites in the package** (18): serial open failures by
  category, RTS/DTR pulse warning, INIT/READ/POLL encode failures, teardown unkey
  (encode, write, and the confirmed-unkeyed line), poll-loop errors, and supervisor
  give-up. ~~and identity handling~~ ~~best-covered~~ — **CORRECTED 2026-08-01 by
  B10:** the identity paths log nothing at all, including the `exitPermanent` mismatch
  that halts the bridge for the process lifetime, and B9/B11/B14 are in this file too.
  Count of call sites is not coverage. Do not read this bullet as clearing
  `pipeline.go`.

---

## Suggested order

1. **B10** first — a permanent, silent halt on a first-run configuration mistake, on a
   deployment where the person reading the log is not the operator. Cheapest fix here,
   highest consequence.
2. **B1** — the only finding where a safety guard is silently not running, and it has a
   standing operator instruction behind it.
3. **B9** and **B11** together — one file, one subject (why the pipeline exited and
   when the rig came back), and B9 is a two-line copy of the sibling branch.
4. **B2** — do it in the same commit as `ft8-logging-gaps.md` finding 1; one defect,
   two hubs.
5. **B3**, **B5**, **B12**, **B13** — all four are "the log asserts an outcome it did
   not verify". **B12 should be settled together with `ft8-logging-gaps.md` finding 6**:
   both ask what a successful keyed transmission ought to record, and that is one
   operator decision, not two.
6. **B4** — small, and converts an accepted assumption into a measurement.
7. **B14**, then **B6, B7, B8** whenever adjacent code is open.

Per the standing TDD directive, the behaviour statement for each of these is the
confusable-state clause above. A test asserting only "a line was emitted" is weaker
than the rule: assert that the two confusable states produce **distinguishable**
output.
