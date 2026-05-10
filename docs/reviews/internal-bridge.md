# `internal/bridge` code review (session, 2026-05-10)

Scope: `internal/bridge` package and its direct dependencies as wired
into `cmd/smd` + `internal/api`. Reviewed at commit `d3ac8cf`
("Introduce bridge subsystem (ADR 0013, ADR 0019)").

## Overall assessment

Tight, well-shaped subsystem. ~340 LOC non-test, reads cleanly, the
lifecycle pattern matches the project convention exactly, and the
test coverage is real (boundary, lifecycle, handler wire-format,
shutdown-channel). Documentation matches code intent. Most findings
below are small-grain — there are a couple of substantive items, the
rest is polish.

---

## Findings

### 1. `/v1/rig/events` is not subscriber-capped — asymmetric with `/v1/events` (medium)

`internal/api/server.go:120` wraps the firehose with `limitEventSubscribers`:

```go
mux.Handle("GET /v1/events", s.limitEventSubscribers(http.HandlerFunc(s.handleEvents)))
```

`internal/api/server.go:140` does not:

```go
mux.Handle("GET /v1/rig/events", br.HTTPHandler(s.shutdownCh, logger))
```

A misbehaving client can open unlimited rig-event SSE connections
without ever being rejected. The `limitConcurrent` middleware doesn't
apply to SSE by design (`middleware.go:64`), so this endpoint has
effectively no cap.

For a personal-scale deployment (one operator, one tab) this is fine
in practice. But the pattern divergence is unintentional — comments
in the bridge code claim "Same pattern as /v1/events" while this is
the one place that pattern is broken. Either:

- wrap with `s.limitEventSubscribers(br.HTTPHandler(...))`, or
- document explicitly that rig events are intentionally uncapped (and
  why).

### 2. `writeSSEEvent` silently drops marshal failures vs. logging them (small, but the comment is wrong)

`internal/bridge/handler.go:103-113`:

```go
data, err := json.Marshal(evt.Payload)
if err != nil {
    // Skip silently — payload types are programmer-controlled.
    // A future regression that breaks one payload type
    // shouldn't disappear; the publisher logs separately.
    return nil
}
```

The comment says "shouldn't disappear" but the code makes it
disappear. The equivalent in `internal/api/handler_events.go:127-138`
logs at WARN with event name + ID. Either the bridge handler should
log too (matching the "shouldn't disappear" intent), or the comment
should be updated to say "we accept silent drop because…". Right now
the comment lies about the behavior.

This is exactly the kind of regression-hider that bites later — a
future payload type change that breaks marshal will not surface
anywhere.

### 3. Redundant `logger` parameter on `HTTPHandler` (small)

`handler.go:32`: `func (s *Service) HTTPHandler(shutdownCh <-chan struct{}, logger *logging.Service)` —
but `s.logger` already exists from `New()` and is the same instance
(verified in `handler_test.go:34` and `cmd/smd/main.go:309`). The
parameter is dead-weight: it can never legitimately differ from
`s.logger`. Drop it; use `s.logger`. One less thing for callers to
thread.

### 4. `Stop()` idempotency races on concurrent calls (minor edge case)

`service.go:111-130`: the second concurrent caller sees `stopped=true`
and returns `nil` *before* the first caller has finished `wg.Wait()`
+ `hub.close()`. The contract "Stop returned, therefore stopped" is
technically violated for the second caller in a concurrent-stop
scenario. Same issue exists for `Subscribe()` racing with `Stop()` —
between `stopped=true` and `hub.close()`, a Subscribe can register a
real channel that gets closed milliseconds later.

In practice nothing calls Stop concurrently. Tests only cover
sequential idempotency. A common pattern is to gate idempotent Stop
on a `sync.Once` or have the late callers wait on a `done` channel
that the first caller closes after `hub.close()`. Not urgent;
flagging because the doc says "idempotent" and a strict reading would
expect the late caller to observe full shutdown.

### 5. Boundary test scope is narrow (small, future-proofing)

`boundary_test.go:79-83` walks exactly three directories:

```go
dirs := []string{"../database/sqlite", "../forwarding", "../qsoservice"}
```

If a *new* internal package later imports `internal/bridge`
inappropriately (say a future `internal/uploadhistory` that doesn't
exist yet), this test won't catch it. The forward direction
(`bridge → forbidden`) is robust because it walks `*.go` in the
bridge dir; the reverse direction is hard-coded.

Cheap fix: walk `internal/*/`, skip `internal/bridge/` itself + the
allowlist (`internal/cat`, `internal/serial`, etc.), assert none of
the rest import the bridge package. Or accept the cost ("when we add
a new package, update this test") and leave a comment.

### 6. Doc.go uses semi-conceptual package names that don't match real paths (cosmetic)

`doc.go:13-14`:

```
internal/storage and internal/forwarder MUST NOT import internal/bridge
```

Real paths: `internal/database/sqlite` (storage) and
`internal/forwarding` (forwarder). The boundary test uses the real
paths. Not a bug — the conceptual names are clearer for the prose —
but the inconsistency cost a moment of "wait, do those packages
exist?" when reading. A parenthetical with the real paths (or just
using the real paths) would help future readers.

### 7. `TestStart_Disabled_NoPublisher` has a confusing dual-success path (small)

`service_test.go:90-99`: the test passes if either (a) a non-empty
event arrives (which it shouldn't), (b) the channel is closed with a
zero-value event (which it won't be — Stop is deferred, so it doesn't
fire during the select), or (c) the timeout fires. The timeout is the
only realistic outcome. The `if evt.Name != ""` branch and the
in-comment hand-wave about "channel-closed on Stop is fine" are both
unreachable. Reads as if the author was hedging.

Just collapse to:

```go
select {
case <-ch:
    t.Errorf("disabled bridge published event; want silence")
case <-time.After(stubEventInterval * 5):
    // expected
}
```

### 8. Stub-emitter lifetime / decommissioning (M3a.2 hand-off)

`service.go:13-18` and the Start log line at `service.go:104` clearly
mark this as a temporary stub. Worth confirming: when M3a.2 lands,
the plan is to *replace* `runStubEmitter` (and `stubEventInterval`) —
not to keep it behind a config flag. The current code makes that easy
because there's a single `s.hub.publish` call site and no other
coupling. Just flagging because the doc.go also says "the M3a.1 stub"
— make sure the cleanup commit removes the stub references from
`doc.go`, not just the code.

---

## What's clearly good

- **Boundary discipline is enforced in code, not just docs.**
  AST-walking import test is the right call — CI catches violations
  loudly.
- **Lifecycle pattern matches project convention**
  (`Initialize`/`Start`/`Stop`, all idempotent, nil-safe `Enabled()`).
- **`hub` is a deliberate copy of `events.Hub`** with a one-line
  justification — exactly the "build specific not generic" pattern
  from `lessons-for-v2.md`. The justification (typed `Event` avoids
  `any`-assertion at the SSE handler) holds up.
- **Slow-subscriber eviction policy is correct and matches
  `events.Hub`.** Publisher never blocks on a stuck client; ADR 0010
  invariant respected.
- **Shutdown-channel handling is correct** — both `r.Context()` *and*
  `shutdownCh` are observed in the SSE select, which is the bug the
  api package's handler also fixed (and is non-obvious —
  `http.Server.Shutdown` doesn't cancel idle SSE contexts).
- **Tests exercise the real wire format** (`bufio.Scanner` over the
  SSE response in `handler_test.go:98-116`), not just the channel
  mechanics.
- **Config validation surfaces missing serial/CAT fields at startup**
  rather than at first-rig-poll, matching the project's "loud failure
  at boot" pattern.
- **`Enabled()` is nil-safe** — small, but the kind of paper-cut that
  the api package would otherwise need to defend against.

---

## Suggested action ordering

1. **Decide on subscriber cap for `/v1/rig/events`** (finding #1) —
   actual behavior choice, not just code.
2. **Fix the marshal-failure logging contradiction** (finding #2) —
   five-line change, removes a future-regression hider.
3. Drop redundant `logger` param (finding #3) — pure cleanup.
4. The rest are polish; can be left for a follow-up or rolled into
   M3a.2.
