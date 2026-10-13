---
number: 0093
title: The daemon's HTTP listener closes once and keeps its close result
status: Accepted
date: 2026-10-13
---

# 0093 — The daemon's HTTP listener closes once and keeps its close result

## Context

Shutdown stops the daemon's HTTP server in two steps. `StopAccepting` closes the listener at once,
so the socket or port is released and new requests are refused while the subsystems tear down.
Later, `Shutdown` drains in-flight requests. Underneath, `http.Server.Serve` tracks the listener
inside its own close-once wrapper and untracks it only when `Serve` returns.
`http.Server.Shutdown` closes every listener still tracked and returns the first close error. After
a drain timeout it returns only the deadline error (net/http go1.26 `server.go:3154`).

So when `Shutdown` ran before `Serve` had untracked the listener, it closed an already-closed
listener and failed with "use of closed network connection". That failed the `http` lifecycle node,
and its dependent nodes were skipped: CI run 38356688161, attempt 1, in
`TestIdentityWire_SW6_NoRetryWhileHeld`. A repeated close is benign. A genuine close error, or a
drain timeout, is not, and must still fail the node. `Shutdown` without `StopAccepting` is a
supported rollback path.

## Decision

`ListenAndServe` wraps the bound listener in a close-once owner. The first close, from whichever
caller, closes it and keeps the result; every later close returns nil. `Shutdown` reports the
drain result together with that one close result (`errors.Join`, so neither a timeout nor a close
error is lost), whichever caller closed first.

## Alternatives considered

### Treat `net.ErrClosed` from `Shutdown` as success

Leave the two closes in place and classify the second one's error as benign. Rejected: it tolerates
the second close instead of removing it, and it would also accept a listener closed by anything
else. It would also still lose a genuine close error behind a drain timeout, since net/http
returns only the deadline then.

### Record only `StopAccepting`'s close result

This was the first version built (W-0022). Rejected in review: when `Shutdown` closes first (the
rollback path) and then times out, net/http returns only the deadline, so the close error
disappeared. The result belongs to the close-once owner, not to one caller.

### Close the listener only in `Shutdown`

Rejected: the listener must be released immediately at `StopAccepting`, so a restarting daemon can
bind and new requests are refused during teardown.

## Consequences

- A benign repeated close no longer fails the `http` node, so its dependents drain.
- A genuine close error or a drain timeout still fails the node, and its dependents are still
  skipped by the lifecycle rules. Those stay strict.
- One small wrapper type and two unexported test hooks (beneath and above the owner) in
  `internal/api`. The hooks are nil in production.

## Triggers to revisit

- If net/http stops returning listener-close errors from `Shutdown`, or starts tracking listeners
  differently, re-check whether the owner is still needed.
- If the daemon serves more than one listener, each needs its own owner, and `Shutdown` must
  report every one.

## References

- `docs/work/W-0022-http-shutdown-double-close.md` (cause, reproducer, rulings, proofs)
- `internal/api/server.go` (`ListenAndServe`, `StopAccepting`, `Shutdown`, `closeOnceListener`)
