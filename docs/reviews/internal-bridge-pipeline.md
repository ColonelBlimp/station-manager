# `internal/bridge` code review — pipeline + bootstrap (2026-05-10)

Scope: `internal/bridge` package as it stands at commit `35aeda9` —
covers the M3a.2 serial+CAT pipeline and the M3a.3 bootstrap-on-SSE-open
work that landed on top. Supersedes
[`internal-bridge.md`](internal-bridge.md), which reviewed the M3a.1
stub-emitter scaffold at commit `d3ac8cf`.

## Status of the prior review

All eight findings from the earlier pass are resolved in the current
tree:

| # | Prior finding | State |
|---|---|---|
| 1 | `/v1/rig/events` not subscriber-capped | Fixed — `internal/api/server.go:147` wraps `br.HTTPHandler(s.shutdownCh)` in `s.limitEventSubscribers` |
| 2 | Silent marshal-failure with contradictory comment | Fixed — `handler.go:121-125` logs at warn with event name |
| 3 | Redundant `logger` parameter on `HTTPHandler` | Fixed — signature is now `HTTPHandler(shutdownCh <-chan struct{})` |
| 4 | `Stop()` idempotency races on concurrent calls | Fixed — `sync.Once` + `stopDone` channel (`service.go:69-70, 141-158`) |
| 5 | Boundary test hard-codes three dirs | Fixed — `boundary_test.go:87+` walks `internal/*` with allowlist |
| 6 | `doc.go` semi-conceptual package names | Fixed — real paths in `doc.go:13-17` |
| 7 | `TestStart_Disabled_NoPublisher` dual-success | Fixed — clean `case <-ch / case <-time.After` |
| 8 | M3a.1 stub-emitter cleanup | Done — replaced with real `runPipeline` |

Below are findings against the current state (pipeline + bootstrap +
fan-out fully wired).

---

## Findings

### 1. `omitempty` on `SplitOverride` (and other bool/zero fields) drops legitimate "OFF" pushes (substantive)

`events.go:60-68` defines:

```go
type RigStatePayload struct {
    ...
    SplitOverride bool   `json:"splitOverride,omitempty"`
    ...
    VfoA          int64  `json:"vfoA,omitempty"`
    Power         int    `json:"power,omitempty"`
    ...
}
```

`mapStatusToPayload` correctly sets `populated = true` when the rig
pushed the field, so the event fires — but JSON marshalling drops
zero values. The SPA's `catState` merge is field-presence-driven (per
ADR 0010 — "any field omitted from a given event leaves the SPA's
catState entry untouched"). So:

- Rig pushes `ST0` (split OFF) → `SplitOverride = false` → JSON omits
  the field → SPA's `catState.split` is **not** updated to false.
- Same problem for `PC000` (0W) and the (admittedly absurd)
  `FA000000000`.

`SplitOverride` is the realistic case: turning split off is something
operators do, and the SPA will silently miss it.

The fix is one of:

- Drop `omitempty` from these fields and accept zero values on the
  wire (forces the SPA to interpret `splitOverride: false` correctly —
  currently it does, because the merge writes whatever arrives).
- Use `*bool` / `*int64` so absence vs. zero are distinguishable.
- Keep `omitempty` and emit a synthetic "always-present-when-changed"
  marker (uglier).

The cleanest is option 1: the bridge's populated-flag at
`mapStatusToPayload` already filters "field not in this push" —
`omitempty` is redundant once the bridge does that filter. **The
bridge's filter is the correct place to decide presence; JSON's
`omitempty` second-guesses it incorrectly for boolean and int zeros.**

This is the most concrete bug in the package right now.

### 2. Startup bridge-errors fire to zero subscribers (design gap, intentional but worth a doc line)

The pipeline's early-failure paths in `runPipeline`
(`pipeline.go:67-133`) all publish a `bridge-error` event:

- Unknown CAT driver
- Serial config build failure
- Missing READ command in rigdef
- `serial.Open` failure
- Missing INIT command in rigdef
- INIT write failure

These fire from the goroutine that started immediately after
`Service.Start`. SSE subscribers don't exist yet — the SPA tab
connects later, by which point the pipeline goroutine has already
exited and the events are gone (the hub doesn't replay; M3a.1 chose
against a cache per ADR 0019).

End-state: an operator with a typo'd `bridge.cat.driver` will see
neither a SPA toast nor any indication in the logging UI; only the
daemon log mentions it. The SPA shows no rig data and no error —
looks identical to "rig is off."

Two ways to close this:

- **Defer the pipeline kickoff to first-subscribe.** First SSE-open
  triggers pipeline start → its early errors land in that subscriber's
  hub channel synchronously. (Adds latency to first SSE connection;
  the bootstrap-poll latency cost is the existing precedent for
  accepting that.)
- **Cache the most recent bridge-error and replay it to new
  subscribers.** Cheap, contradicts the "no cache" stance of ADR 0019
  in spirit but not letter (it's not rig state, it's a one-shot
  operator-actionable error).

ADR 0019 doesn't address this case; the test
`TestPipeline_BridgeError_UnknownDriver` only works because it
subscribes before Start. The handler tests (`TestHTTPHandler_*`)
connect AFTER Start so they don't observe this gap. Worth either an
ADR follow-up or at minimum a comment in `runPipeline` and `doc.go`
admitting that startup-error visibility for the SPA is "rig restart
of daemon required to retry" until a later milestone closes it.

### 3. `TriggerBootstrap` race window can hit a closed client (benign, but worth tightening)

`service.go:195-204`:

```go
func (s *Service) TriggerBootstrap(ctx context.Context) error {
    s.mu.Lock()
    cl := s.activeClient
    bb := s.bootstrapBytes
    s.mu.Unlock()
    if cl == nil || len(bb) == 0 {
        return nil
    }
    return cl.WriteCommandBytes(ctx, bb)
}
```

Pipeline shutdown (`pipeline.go:108-114`) closes the client *before*
clearing `activeClient`:

```go
defer func() {
    _ = client.Close()      // step 1: close
    s.mu.Lock()
    s.activeClient = nil    // step 2: clear (under mu)
    s.bootstrapBytes = nil
    s.mu.Unlock()
}()
```

A handler can capture `cl` between step 1 and step 2 and call
`WriteCommandBytes` on the closed client. Per `serial.Client` contract
this returns `ErrClosed` rather than panicking, the SSE handler logs
at warn and continues — so it's behaviourally fine. But the code's
structure invites a future bug: if anyone changes the order of
close/clear, or adds a hook between them, the contract may stop
holding. Cheap fixes:

- Clear `activeClient` *before* closing the port (under mu), so the
  captured `cl == nil` after the lock release means the late caller
  is a no-op.
- Or move the `Close()` inside the mu-locked region (one line; the
  contract is fine because Close is documented idempotent and
  serialized).

Either makes the invariant "if `cl != nil`, it's still open"
enforceable rather than incidental.

### 4. INIT encoding happens after `openClient`; READ encoding happens before — asymmetric (small)

`pipeline.go:87-126`:

```go
readBytes, err := cat.Encode(def, readCommandName)   // line 87 — pre-open
...
client, err := s.openClient(serialCfg)               // line 98
...
initBytes, err := cat.Encode(def, initCommandName)   // line 116 — post-open
```

Both encodes are pure (no I/O). If a rigdef is missing INIT, we open
the port (touching hardware, possibly blocking on permission denial)
only to discover the rigdef is incomplete and bail. Trivial fix:
encode INIT alongside READ before `s.openClient`. Both cases get
fast-fail symmetry, no port acquisition for a config error.

### 5. `TestPipeline_TerminalSerialErrorEmitsDisconnect` has a hard-coded `time.Sleep(20ms)` (flaky on CI)

`pipeline_test.go:316-318`:

```go
// Give the pipeline a moment to enter ReadResponseBytes (it has
// to send INIT first, which races against Close on a busy CI).
time.Sleep(20 * time.Millisecond)
if err := fake.Close(); err != nil { ... }
```

A loaded CI agent can take longer than 20ms to schedule the
goroutine. The test then closes before INIT lands → INIT's
`WriteCommandBytes` returns `ErrClosed` → pipeline publishes a
bridge-error (operator-facing message about INIT failure) instead of
a `rig-disconnected`. Test fails on the wrong channel. Cheap fix:
poll on `len(fake.recordedWrites()) >= 1` instead of `time.Sleep`,
mirroring the pattern other tests already use.

### 6. Boundary test reverse-direction: manual depth-limited walk (minor future-proofing)

`boundary_test.go:113-136` — the comment says "Glob with ** isn't
standard; fall back to manual walk", and the manual walk hard-codes
three depth levels. A package nested four levels deep
(`internal/foo/bar/baz/quux/`) wouldn't be checked. None exist today;
flagging because the manual walk's structure invites the same kind of
"missed by the test" pattern the prior review fixed.

`filepath.WalkDir` would replace the three-loop manual recursion with
one call and remove the depth limit:

```go
filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
    if err != nil || d.IsDir() ||
        !strings.HasSuffix(path, ".go") ||
        strings.HasSuffix(path, "_test.go") {
        return nil
    }
    // parse + check imports
    return nil
})
```

Same coverage, depth-unlimited, half the LOC.

### 7. `delimiterFromString` returns 0 with a comment about `serial.newPort`'s default (leaky abstraction, cosmetic)

`pipeline.go:408-416`:

```go
func delimiterFromString(s string) (byte, error) {
    if s == "" {
        return 0, nil // serial.newPort defaults zero to '\r'
    }
    ...
}
```

`internal/bridge` is reading a private contract of `internal/serial`
(zero byte → fall back to `\r`). A future change to `serial.newPort`
that picks a different default (or that errors on zero) would
silently break the bridge. Either:

- Always pass an explicit delimiter — the FT-710/FTDX10 rigdefs both
  define `;`, so the empty-string branch is unreachable in practice
  anyway, and an error there would surface a misconfigured rigdef
  early.
- Move the "empty → `\r`" decision into the bridge, and pass `'\r'`
  explicitly.

Either makes the cross-package contract explicit.

### 8. `Stop()` before `Start()` works but doesn't propagate `started=true` (very minor)

If `Stop()` is called on a Service that was constructed but never
`Start()`ed, `s.cancel` is nil → no cancel call, `wg.Wait()` returns
immediately, `hub.close()` runs, `stopDone` closes. Behaviourally
correct.

But `s.started` stays false. A subsequent `Start()` call would see
`started == false` and try to spin up the pipeline again — only to
find that `stopOnce` has fired and the hub is closed (`Subscribe`
returns an already-closed channel, publishes are no-ops). The Service
is "post-Stop dead" but `Start` lies and returns nil. There's no
realistic call site that does this; flagging because the lifecycle
pattern says all transitions should be sane. Adding a "stopped" flag
set by the `stopOnce` body, checked at the head of `Start`, closes
the gap.

### 9. Identity verification fires once per pipeline lifecycle (correct, but think about rig swap)

`pipeline.go:201-211` sets `identityVerified = true` after the first
IDENTITY observation. If the operator hot-swaps rigs (unlikely but
possible: park rig 1, plug in rig 2 on the same port without
restarting the daemon), the new rig's identity won't be re-checked.
Acceptable for v1 (port disconnect on cable yank → terminal serial
error → pipeline exits → daemon restart needed); worth a one-line
comment that the verification is per-pipeline-instance, not
per-rig-instance.

### 10. `cat.Decode` `ErrNoMatch` skip is silent — including for unrecognized rigdef-known frames (small observability gap)

`pipeline.go:188-193`:

```go
if decErr != nil {
    // ErrNoMatch is normal — rig pushes lines for tags we
    // don't have a parser for (S-meter, waterfall, etc.).
    // Silent skip per the codec doc; logging would flood.
    continue
}
```

The comment is correct that logging every ErrNoMatch would flood. But
a different decode error class (e.g. malformed-frame, out-of-spec
value) is also masked here — the skip is unconditional. A debug-level
structured log that's *off by default* (gated by a log level the
operator already controls) would let "why isn't my rig sending mode
pushes" debugging happen without a code change. Not a bug; future
affordance.

---

## What's good

The implementation has tightened up substantially since the prior
review.

- **All eight prior findings fixed**, including the substantive ones
  (subscriber cap, marshal logging, stop idempotency).
- **Pipeline architecture is clean**: `runPipeline` is the open/init
  dance; `readLoop` is the steady-state loop, separable for tests.
  The split is the kind of factoring that pays off when you later
  want to test the loop without test-doubles for `cat.Lookup` and
  `s.openClient`.
- **`fakeSerial` is a real test double, not a mock**: implements the
  full `serial.Client` interface, exercises the same code paths the
  real port would. Matches the project's "integration tests over
  mocks" preference.
- **Test coverage is genuine**: wire format (handler tests scan SSE
  bytes), lifecycle (start/stop/idempotency), pipeline decode,
  identity verification, dedup of disconnect events, multi-subscriber
  fan-out, terminal error path, bootstrap on subscribe.
- **`stopOnce + stopDone` is the right idiom for the prior #4 issue**
  — late callers wait for the first caller's teardown to complete
  before observing the "stopped" state.
- **Boundary test was generalized** per the prior #5 — adding a new
  internal package gets enforcement for free.
- **Marshal-failure logging contradicts the prior #2 comment
  correctly now** — the comment matches the code.
- **`buildSerialConfig` keeps cat/serial/JSON-translation glue work
  in the bridge** rather than leaking into either `internal/cat`
  (pure codec) or `internal/serial` (pure I/O). Right place for it.
- **`mapStatusToPayload` is the right shape** — explicit per-field
  dispatch, easy to read, easy to extend, no reflection.
- **`Enabled()` is nil-safe** and the wiring layer at
  `internal/api/server.go:146` correctly gates route registration on
  it.
- **Test against the real rigdef in
  `TestBuildSerialConfig_FromYaesuRigDef`** — no schema drift slips
  through unnoticed.

---

## Suggested action ordering

1. **Fix the `omitempty` bug for `SplitOverride`** (#1) — concrete
   operator-visible bug. Drop `omitempty` from the boolean and the
   int fields where zero is a real value (`SplitOverride`, possibly
   `Power`); the bridge's populated-flag is the correct presence
   filter.
2. **Tighten the bootstrap race** (#3) — clear `activeClient` before
   `client.Close()`, both under mu. Two-line change, prevents future
   regression.
3. **Move INIT encode before `openClient`** (#4) — fast-fail for
   missing-INIT rigdefs. Five-line change.
4. **Replace `time.Sleep(20ms)` with a poll on writes** in
   `TestPipeline_TerminalSerialErrorEmitsDisconnect` (#5) — matches
   the pattern other tests use.
5. **Decide on startup-error visibility for the SPA** (#2) — design
   call. Either ADR follow-up to ADR 0019 (cache last bridge-error,
   or lazy-start on subscribe), or document the gap explicitly in
   `doc.go` so future-you doesn't rediscover it.
6. The rest are polish — `WalkDir` for the boundary test (#6),
   explicit delimiter handling (#7), `Stop`-before-`Start` flag (#8),
   per-pipeline identity verification comment (#9), debug-level
   decode log (#10).
