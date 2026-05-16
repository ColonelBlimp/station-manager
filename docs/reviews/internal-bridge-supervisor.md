# `internal/bridge` code review — supervisor + flash-suppression (2026-05-16)

Reviewed 6 files totaling ~2,244 lines of new/changed code across the
daemon (`internal/bridge/pipeline.go`, `service.go`, `supervisor_test.go`,
`internal/types/bridge.go`, `internal/config/config.go` validation
block) and the SPA (`frontend/logging/src/lib/states/bridge.svelte.ts`
+ its test file); found **2 substantive issues, 7 polish items, and
4 future affordances**. Overall assessment: the supervisor design is
sound and the dedup token + classified-exit pattern reads cleanly;
the SPA-side flash-suppression machine is small and well-covered; the
one real bug is a sticky-toast leak in `closeSource` that surfaces
when the operator disables CAT while a disconnect warn is on screen.

Supersedes nothing — runs alongside `internal-bridge-pipeline.md`,
whose findings stay closed. Items here only call out **new** issues
introduced by this session's changes (or pre-existing issues now
materially worse because of them).

---

## Findings

### Substantive

#### M1. `closeSource()` leaks a sticky disconnect toast (substantive)

`frontend/logging/src/lib/states/bridge.svelte.ts:234-249` —
`closeSource()` clears `pendingDisconnectToastId` but never calls
`toasts.dismiss(pendingDisconnectToastId)` on a non-null value:

```ts
function closeSource(): void {
    if (!activeSource) return;
    activeSource.close();
    activeSource = null;
    bridgeState.connected = false;
    bridgeState.rigResponding = false;
    if (pendingDisconnectTimerId !== null) {
        clearTimeout(pendingDisconnectTimerId);
        pendingDisconnectTimerId = null;
    }
    pendingDisconnectToastId = null;   // ← drops the handle without
                                       //   dismissing the toast
}
```

Concretely: the SPA is in state C (warn toast visible) when the
operator flips `configState.station.enabled` to false (or `stopBridge()`
runs for any other reason). `closeSource()` runs, the EventSource
closes, the bridge-state flags reset — but the sticky `ttl=0` warn
toast stays in `toastsState.items` forever (no auto-dismiss because
ttl=0; no programmatic dismiss because the handle was nulled without
being passed to `toasts.dismiss`). Operator sees "the rig has gone
quiet — is it powered on?" persisting even though they intentionally
disabled CAT.

The state-machine doc in ADR 0020's second revision says exactly this
flow ("`closeSource()` clears both pending fields and the timer;
without this a deferred warn would fire after the EventSource closed,
pushing a stale toast for an event no longer relevant.") — the intent
was clearly to leave no trace, but the implementation only cancels
the *pending* path (state B) and forgets the *visible* path (state C).

Fix: before `pendingDisconnectToastId = null`, call
`toasts.dismiss(pendingDisconnectToastId)` when non-null. Same pattern
as the C → A transition in the `rig-state` handler at line 173-175.

**Why it matters:** the toast's whole point is to give the operator
trustworthy notification. A toast that survives the cause's resolution
is worse than no toast — it actively misleads. Tested manually by
mentally walking the C-state cleanup; there is also no test coverage
for this case in `bridge.test.ts` (the existing tests for `closeSource`
in the "lifecycle" describe block run with `configState.station.enabled
= false` before any disconnect, so they never enter state C before
closing).

#### M2. The dedup-token doc-comment in `service.go:104` invites an unsafe test pattern (substantive)

`internal/bridge/service.go:104` says the timeout snapshot fields can
be overridden "by overwriting these fields directly after construction":

```go
// Timeout snapshot — captured at New from cfg.Timeouts with
// package-var fallback for any zero/unset values. Read at runtime
// by runSupervisor and readLoop. Per-Service so future multi-rig
// can tune per rig, and so tests can override either via package-
// var dialing (legacy pattern, applied at New time) or by
// overwriting these fields directly after construction.
livenessTimeout                time.Duration
```

The fields are written exactly once (in `New`) and then read by the
supervisor + readLoop goroutines without a lock. The happens-before
established by `go s.runSupervisor(runCtx)` in `Start` makes the
single write visible to the goroutine — so the current code is
race-free.

But the comment's "overwriting these fields directly after
construction" license is dangerous: if Start has already been called
(supervisor is running), any direct field write races the supervisor's
read. `go test -race` would flag it. Nothing in the tree exercises
this pattern today, but the comment is an explicit invitation for a
future test to add one.

Fix: either tighten the comment to "overwriting these fields BEFORE
Start" (the actual safe boundary), or — if the future flexibility
is wanted — mu-guard the reads in the supervisor/readLoop and the
writes. The current shape doesn't need the latter; the simpler fix is
the comment correction.

**Why it matters:** the project's lessons-for-v2 emphasise integration
tests over mocks and using real `&sqlite.Service{}` patterns; that
pattern includes mutating service state. A future contributor reading
this comment will reasonably believe field-mutation-after-construction
is supported, write a test, and it'll pass on the contributor's
laptop while occasionally failing under `-race` on CI. The cost of
the misleading comment is bounded but real.

---

### Polish

#### m1. `TestSupervisor_DedupesIdenticalOpenFailureAcrossRetries` could exceed the 10s steady-state threshold on slow CI

`internal/bridge/supervisor_test.go:227-288` calls
`scaleSupervisorBackoff(t, 1*time.Millisecond, 2*time.Millisecond,
10*time.Second)` — backoff is ~ms-scale but `steadyState` stays at
the production default (10s). The test then waits 200ms for ≥4
attempts and 20ms more to drain.

The dedup contract under test assumes each pipeline iteration returns
before `steadyState` (otherwise the supervisor resets the dedup token
between cycles and the second open failure publishes a new
bridge-error, failing the `collected != 1` assertion). Each iteration
is "open returns immediately, publish, return exitTransient" so the
real wall-clock per iteration is microseconds — nowhere near 10s.

But a sufficiently overloaded CI agent (or one running tests under a
sanitiser / `-race` with many GC pauses) could theoretically take 10s+
to complete one iteration. Unlikely, but the test would then fail with
"got 2, want 1" — and the failure message wouldn't immediately point
at scheduling pressure as the cause.

Fix: dial `steadyState` down too — `scaleSupervisorBackoff(t, 1*time.Millisecond,
2*time.Millisecond, 50*time.Millisecond)`. Then the test is explicitly
asserting "within the dedup window, identical exits suppress" rather
than relying on a wall-clock that happens to be much smaller than the
production threshold.

#### m2. The supervisor's `default` branch logs to `s.logger.ErrorWith()` but never explicitly returns the exit class — recovery path is opaque

`internal/bridge/pipeline.go:281-292` — the `default` branch in the
supervisor's switch covers a hypothetical fourth `pipelineExitClass`
that doesn't exist today. Defensive. The log message is good ("guard
against a future fourth class being added without a matching
supervisor branch"). But there's no test exercising this path — the
exhaustive switch + panic-like log is essentially documentation.

Cheap improvement: the comment in the code is good but a `// nolint:unreachable`
or a test that injects an invalid exit class (via a test-only seam)
would prove the guard actually fires the log line rather than being
silently optimised away. Not worth a lot of effort; flagging because
the prior review's #10 ("debug-level decode log is silent") pointed
at the same "untested defensive code" pattern.

#### m3. `runSupervisor`'s `time.After(backoff)` leaks until backoff elapses on Stop

`internal/bridge/pipeline.go:271-275`:

```go
select {
case <-ctx.Done():
    return
case <-time.After(backoff):
}
```

`time.After` allocates a new timer that the GC can't collect until
it fires (it's referenced by the runtime's timer heap). When Stop
cancels ctx during a long backoff (up to 30s by default), the
supervisor returns immediately — but the underlying timer holds the
goroutine's stack frame alive in the runtime until the duration
elapses. Memory cost is trivial (a few hundred bytes per Stop call
that races a backoff sleep) and there's only ever one supervisor per
Service.

Fix (Go idiom): use `time.NewTimer(backoff)` + `defer timer.Stop()`
inside a small wrapper, or `context.WithTimeout` over the parent ctx.
Project-wide pattern — used by the forwarder's worker; bridge should
match. Pure polish; no behavioural impact.

#### m4. The dedup token doc-comment can mislead about `RigCodeNoData` semantics

`internal/bridge/service.go:78-86` (Service.lastPublishedExitKey
doc-comment) describes the dedup as suppressing "the publish if it
matches — so the operator sees ONE toast per failure cycle." That's
true for *exit-causing* publishes, but `RigCodeNoData` is fired by
the mid-loop `publishDisconnect` (not `publishExitDisconnect`) and
isn't tracked here at all.

The actual scoping is: `RigCodeNoData` is deduped per-pipeline-instance
by the `announcedDisconnect` flag (correct — it's a recoverable mid-loop
state); `RigCodeSerialError` (terminal-read + probe-write failures) is
deduped cross-supervisor-cycle via `lastPublishedExitKey`. Two-axis
dedup, two different scopes.

The doc-comment at line 78-86 only describes one axis. A reader looking
for "why does the operator sometimes see two disconnect toasts in
quick succession?" (no-data, then probe-init fails) would be confused.

Fix: a sentence in the comment naming the second axis ("Mid-loop
no-data emissions are independently deduped by the readLoop's
per-instance `announcedDisconnect` flag — that dedup spans a single
pipeline run, not multiple supervisor retries"). Clarification, not a
code change.

#### m5. The `error`-event handler on the SPA side doesn't reset the disconnect-toast state machine

`frontend/logging/src/lib/states/bridge.svelte.ts:145-148` — when the
EventSource fires `error` (transport drop), `bridgeState.connected`
and `rigResponding` both go false, but `pendingDisconnectTimerId` and
`pendingDisconnectToastId` are untouched. Consequences:

- If `error` fires in state B (timer pending), the timer eventually
  fires and pushes a "rig has gone quiet" toast — even though the
  cause is actually transport-level (browser disconnected from the
  daemon), not rig-level (rig went silent on the bridge).
- If `error` fires in state C (toast visible), the sticky toast keeps
  saying "rig has gone quiet" until either the EventSource auto-reconnects
  and rig-state arrives (the natural C → A path resolves it) or
  closeSource runs (currently has its own M1 leak; assume that's fixed).

For most real scenarios (intermittent network blip, daemon restart)
the wording is at worst slightly off ("rig went silent" vs "lost
contact with the daemon"). Browser SSE auto-reconnect resolves both
within seconds.

Not a bug worth fixing on its own — the current design's bias toward
keeping warn-level signals around for the operator's benefit is
defensible. Flagging because it's a subtle interaction with the new
flash-suppression machine that's invisible from reading either
component in isolation.

#### m6. `mergeRigState` silently drops unknown `selectedVfo` values; pipeline-side keeps them

`frontend/logging/src/lib/states/bridge.svelte.ts:267-269`:

```ts
if (payload.selectedVfo === 'A' || payload.selectedVfo === 'B') {
    catState.selectedVfo = payload.selectedVfo;
}
```

vs. `internal/bridge/pipeline.go:563-572`'s `vfoLabelToTag` which
passes unknown values through unchanged (per its doc-comment: "so a
future rig that uses a different convention surfaces in logs rather
than getting silently dropped").

Asymmetric — daemon preserves for debugging visibility, SPA drops
silently. A future rig that emits VFO-C or some other tag would show
up in daemon logs (good) but never reach catState (silent SPA bug).

Fix: either accept the unknown value in the SPA (matching the daemon's
"surface in logs" intent) or add a `console.warn` for unknown values.
The cost of accepting is that `catState.selectedVfo` becomes loosely
typed; the cost of warning is one line of code. Both better than the
silent drop.

#### m7. The flash-suppression test for "latest disconnect wins" doesn't cover the case where the second disconnect arrives AFTER the toast became visible

`frontend/logging/src/lib/states/bridge.test.ts:387-405`'s test
"latest disconnect wins when a new disconnect replaces a pending one"
advances 200ms (inside the suppression window), then sends a second
disconnect, then advances 800ms — all while in state B. The C → B
transition (toast already visible when a new disconnect with a
different code arrives) isn't directly tested.

The handler at line 199-202 does handle this case (dismisses the
visible toast, schedules new), but the test coverage doesn't pin it.
A regression that breaks the C → B transition (e.g. forgetting to
dismiss the prior toast, leaving two warns stacked) wouldn't surface
as a test failure today.

Fix: a third test case that fully advances past the suppression window
(reaching state C), then sends a second disconnect with a different
code, then advances another 800ms — assert exactly one visible toast
carrying the second code.

---

### Future affordances

#### F1. Supervisor backoff log noise during long rig-off periods

ADR 0020's Accepted Costs notes: "Log noise during prolonged rig-off
periods. Each open attempt logs at ERROR level. If the rig stays off
for hours, journalctl accumulates ~120 ERROR entries per hour (1 per
30s steady-state). Acceptable — operators rarely leave the daemon
running with the rig off for long, and the journal is grep-friendly."

Worth a future pass when there's a real complaint from the operator:
either downgrade the open-failure log to WARN after the dedup token
has been published once (the toast carries the operator-actionable
signal; the journal entry's purpose then shifts to diagnostics, where
INFO/WARN is more appropriate), or add a backoff-aware "log every Nth
attempt" pattern. Not for this session.

#### F2. The probe-write error path can't distinguish "transient I/O hiccup" from "operator revoked port permission"

`internal/bridge/pipeline.go:331-344` — both probe writes returning
an error are classified `exitTransient` and the supervisor retries.
That's correct for the common case (USB jitter, brief port glitch);
incorrect for permission revocation (operator `chmod 000` on the
device while the daemon's running — the only recovery is a chmod or
daemon restart, both operator-actions, and the supervisor will loop
forever with the same publish suppressed by dedup).

The current behaviour is benign in the loop-forever sense (the daemon
isn't burning CPU, just waking up every 30s to try open + fail). And
permission revocation is rare. But the operator gets exactly ONE
toast for it (the first), and then radio silence — even though the
state is "permanently broken until you fix the perms". Not for this
session; flag for a future "permission-class errors are permanent"
refinement once a real driver appears.

#### F3. Multi-rig will need per-rig dedup tokens + per-rig supervisor instances

ADR 0019's multi-rig API-ready posture means `Service` is currently
single-rig; the dedup token, the timeout-snapshot fields, the
`activeClient`/`bootstrapBytes` slots are all single-valued. When
multi-rig lands, each rig needs its own slot.

The cleanest shape is probably to factor out a `rigSupervisor` struct
holding everything currently in `Service` that's per-rig (mu, timeout
snapshots, activeClient, bootstrapBytes, lastPublishedExitKey,
runPipeline + runSupervisor as methods), with the `Service` owning a
map of them keyed by rig ID. Mentioned in ADR 0020's Triggers section
already; flagging here because the current Service+method layout will
need an extraction step rather than just adding a map.

#### F4. Test pattern documentation — `scaleSupervisorBackoff` vs per-Service overrides

The current tests use `scaleSupervisorBackoff` (mutates package vars
BEFORE `New`) for supervisor tests, and individual `livenessTimeout =
N` assignments (mutates package var BEFORE `New`) for pipeline tests.
Both work, both are correct, both rely on the package-var-then-New
ordering.

With timeouts now also living on `Service`, a future test author
might reach for "construct a Service, then dial down its fields"
which (per M2) is racy if Start has been called. A test-helper file
documenting "tests dial package vars BEFORE New; tests do not mutate
Service fields after Start" would prevent the misleading-code
contagion. Could live as a comment block at the top of
`supervisor_test.go`, or as a `test_doc.go` in the package.

---

## What's good

The substantive changes this session reads cleanly:

- **Classified-exit pattern (`pipelineExitClass`) keeps the
  decide-to-retry policy local to each failure site.** The supervisor
  doesn't have to guess from an opaque error whether retrying makes
  sense; the call site that knows the failure mode classifies it.
  Matches the project's "build specific, not generic" heuristic — no
  reflection-based policy table, no "is this error a retryable one?"
  matcher.
- **The exhaustive switch in `runSupervisor` with the `default`
  panic-like-log path** is exactly the right guard for the
  add-a-case-without-touching-the-switch failure mode that's bitten
  every long-running Go codebase. Cheap insurance.
- **The no-data probe (re-issuing INIT+READ on every liveness
  timeout) is the genuinely clever bit.** It's how the FTdx10's
  USB-stays-open behaviour (and any other rig whose port doesn't
  surface kernel-level disconnects) gets handled without a separate
  health-check goroutine. The fact that it also self-heals false-
  positive disconnects during legitimate idle is a nice property
  fall-out that justifies lowering the default `liveness_ms` from
  30s to 5s.
- **`publishExit*` helpers vs. plain `publishDisconnect` /
  `publishBridgeError`** — the two-tier dedup (cross-cycle for exits,
  per-instance for mid-loop) is a clean separation. The mid-loop
  no-data dedup via `announcedDisconnect` predates this session but
  the exit-site dedup is the new shape and slots in cleanly alongside.
- **Config-promoted timeouts with package-var fallback** preserves
  the existing test pattern (`livenessTimeout = 30 * time.Millisecond`
  before `New`) while letting the operator tune without rebuilding.
  Backwards-compatible test changes for the new shape are minimal.
- **`validateBridge`'s timeout range checks** are concrete and
  field-named in the error message — typos caught at startup with a
  pointer at the bad field. `backoff_initial_ms > backoff_max_ms`
  catches the most likely operator mis-edit specifically.
- **`TestServiceNew_SnapshotsConfiguredTimeouts`** has both
  sub-cases (non-zero overrides + zero falls through) which prove
  the `resolveTimeout` helper's full contract, not just one branch.
  Good test discipline.
- **The SPA's three-state machine has its states named in code
  comments at every transition.** Reading the handler is easy — the
  comment at line 160 says "State B → A" and the next block says
  "State C → A" and the disconnect handler says "Cancel any prior
  scheduled push (state B)". Cuts the cognitive cost of mentally
  tracking machine state during reading.
- **Test coverage of the suppressed-cycle path
  (`SUPPRESSES the flash when rig-state arrives within the suppression
  window`)** is the high-value test that pins the load-bearing UX
  decision from this session — a regression that broke the timer
  cancellation would surface here loud and clear.
- **`bridge.reconnected` i18n key** follows the established
  `bridge.disconnected.<code>` / `bridge.error.<code>` namespace
  convention. Not a daemon-emitted code (SPA-derived), but it's
  clearly named per the "SPA-derived, not a daemon event" comment in
  `en.ts`.

---

## Suggested action ordering

1. **M1 — fix `closeSource()` to dismiss the visible toast** before
   nulling the handle. Single-line change in
   `bridge.svelte.ts:248`; add a regression test for "closeSource
   while in state C dismisses the visible toast".
2. **M2 — correct the dedup-token doc-comment** in `service.go:104`
   to forbid post-Start field mutation. Comment-only.
3. **m1 — dial `steadyState` down** in
   `TestSupervisor_DedupesIdenticalOpenFailureAcrossRetries`. One-line
   test change; future-proofs against slow-CI flakes.
4. **m4 — supplement the dedup-token doc-comment** with the two-axis
   scoping clarification. Reader-facing only.
5. **m3, m6, m7** — polish; pick up when the surrounding code is
   being changed for another reason.
6. **m2, m5** — defensive / minor UX; not for this session unless
   the supervisor switch grows a fourth class or the
   transport-error-during-disconnect interaction becomes operator-
   visible.
7. **F1–F4** — future affordances, file under "next session if
   relevant context arises."
