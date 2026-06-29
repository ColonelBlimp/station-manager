---
number: 0038
title: Forwarder retries connectivity failures indefinitely; `failed` is reserved for upstream rejections
status: Accepted
date: 2026-06-29
---

# 0038 — Forwarder retries connectivity failures indefinitely; `failed` reserved for upstream rejections

## Context

The `qso_upload` queue + worker exist to make uploads survive a flaky link: a QSO is
enqueued at log time, the worker drains it, and a failed attempt is retried with backoff.
The implicit promise an operator reads into that is "whenever the internet comes back —
an hour or ten days later — my QSOs upload."

That promise does not hold today. The `Forwarder.Submit` contract classifies an outcome
as `OutcomeTransient` for *"network errors, rate limits, temporary upstream outages"* —
lumping **"the link is down"** in with **"the upstream is briefly busy."** Transient
outcomes are subject to the per-forwarder retry budget, and the QRZ/ClubLog defaults are
`MaxAttempts: 5`, `InitialBackoffSec: 60`, `MaxBackoffSec: 1800`. So a row that keeps
failing is retried at roughly +60s, +120s, +240s, +480s and, on the 5th failure
(`attempts >= MaxAttempts`), promoted to terminal **`failed`** — about **15 minutes of
runway**. A `failed` row is never re-claimed (the worker's claim query selects only
`pending`/`in_progress`), and a daemon restart does not rescue it (`attempts` is persisted
on the row). The 30-minute backoff cap never even engages — the attempt limit is hit first.

This is wrong for the audience SM actually serves. Operators here run on laptops with **no
internet at all** for stretches — you buy an internet bundle, and no money means no link;
nationwide outages happen; a DXpedition on a remote island has intermittent or
satellite-only connectivity. For all of them, an outbound link down for more than ~15
minutes (routine) silently strands every QSO logged during it in `failed`, requiring manual
intervention to revive. A logger that abandons valid QSOs because the link blinked is not
acceptable for offline-first operation. (Surfaced while designing the database-management
SPA, whose queue screen needs well-defined "waiting" vs "needs attention" states.)

## Decision

Add a fourth Submit outcome, **`OutcomeUnreachable`** — "could not reach the host at all"
(the HTTP client returned an error with no response: DNS failure, connection refused,
timeout, no route). The worker retries `Unreachable` rows **indefinitely**: the row stays
`pending` (re-claimable), backoff saturates at `MaxBackoffSec`, and the row **never counts
against `MaxAttempts` and is never promoted to `failed`**. `failed` is reserved strictly
for host-up rejections — `OutcomeTerminal` (definitive: bad data, revoked credentials,
dedupe) and `OutcomeTransient` exhaustion (host responded but stayed temporarily busy past
the bounded budget). Forwarders classify the obvious way: `http.Client.Do` returns an error
→ `Unreachable`; a response came back → inspect the status → `Transient`/`Terminal`/`Success`.

## Alternatives considered

### Status quo — network errors are `Transient`, 5-attempt budget

Leave the classification as-is; rely on a manual "retry failed" UI to revive stranded rows.
Rejected. The give-up window is ~15 minutes, far shorter than the hours-to-days outages
that are routine for the target operators, so the manual-retry burden would be constant and
QSOs would sit `failed` (invisible, un-uploaded) until someone noticed. The queue is meant
to be resilient to outages, not just to blips.

### Blunt config-only fix — crank `MaxAttempts` huge / raise the backoff cap

No code change: set `MaxAttempts` to a very large number so even network-classified errors
effectively never give up. Rejected. It also makes a genuinely stuck *host-up* transient
(a persistent 5xx, a forever-rate-limited key) retry forever, muddying the `failed` state
that should surface real problems for operator attention. It treats the symptom (budget too
small) without fixing the cause (link-down and upstream-busy are different events). A
band-aid, not a fix.

### Infinite retry for all non-terminal outcomes (merge `Transient` into `Unreachable`)

Drop the bounded budget entirely; only `Terminal` ever fails. Rejected — though it is close.
We keep a bounded `Transient` so that a host that is reachable but *persistently* refusing
(stuck 5xx, auth-temporarily-unavailable that never clears) eventually lands in `failed` and
surfaces to the operator, rather than silently churning forever. The line between "briefly
busy" and "persistently broken" is fuzzy, but in practice real outages are `Unreachable`
(no response at all), so the common offline case is covered without erasing the
host-up-but-broken signal.

### Central connectivity oracle — one probe gates all workers

A daemon-level "is the internet up" probe that pauses every forwarder worker while offline
and wakes them when it returns. Deferred. The per-forwarder "Submit reports its own outcome"
model already produces correct behaviour: each tick probes, backs off if down, drains when
up. A central oracle is an *efficiency* optimisation (avoid waking N workers to each
rediscover the link is down) and can be added later without changing the outcome semantics.

### Periodic "resurrect failed" sweeper

Leave classification alone but periodically move `failed` rows back to `pending`. Rejected.
It papers over the real problem: if connectivity failures are classified correctly they
never become `failed` in the first place, so there is nothing to resurrect. A sweeper would
also blindly re-try genuine rejections, defeating the purpose of `failed`.

## Consequences

**Signed up for:**

- **Offline-first actually works.** Log for days with no link; the queue holds everything at
  `pending` and drains the moment connectivity returns. The 7Q8AC no-bundle case and the
  remote-island DXpedition case are both covered with no operator action.
- **`failed` becomes meaningful.** It now means "the upstream is reachable and said no" —
  exactly the set the DB-manager queue screen should surface as *needs attention / retry*.
  A connectivity outage never produces a `failed` row, so the screen is not polluted with
  "your wifi was off" noise.
- **Small, contained change.** One enum value (`internal/forwarding/forwarding.go`), the
  error-classification branch in the two forwarders (`qrz`, `clublog`), the worker's give-up
  logic skipping the `Unreachable` class (`internal/forwarding/worker/worker.go`), and
  tests. It finishes a distinction the contract already names ("the forwarder is responsible
  for distinguishing try-again-later from don't-bother").

**Accepted costs:**

- **A dead link is probed forever while the daemon runs.** With backoff saturated at
  `MaxBackoffSec` (30 min default), each forwarder makes one cheap, failing attempt every
  ~30 min indefinitely. Negligible CPU/network; flagged for battery/metered-link awareness.
- **Backlog drain rate after a long outage is unchanged** — `batch=5` per `poll=120s`
  (~3,600/day). Correctness is fine (it *will* finish); throughput for a large DXpedition
  pile is a separate tuning knob (drain faster when healthy + backlogged), not solved here.
- **`attempts` keeps incrementing for `Unreachable`** (useful diagnostics — "tried 412 times
  over 9 days") but no longer drives failure for that class; backoff saturates regardless.
- **Auth failure is still classed host-up `Terminal`.** A revoked/bad API key fails the QSO
  rather than pausing the forwarder with a "check your key" status. A smarter per-forwarder
  auth-halt is possible future refinement, out of scope here.

## Triggers to revisit

- **Probe overhead matters** — if waking N workers every 30 min to each rediscover a dead
  link costs measurable battery or metered-link bytes, add the central connectivity oracle.
- **Drain throughput too slow** — if real DXpedition volumes drain too slowly after an
  outage, add adaptive batch/poll when the queue is healthy and backlogged.
- **The `Transient`/`Unreachable` line is wrong in practice** — if operators report
  host-up-but-persistent issues silently retrying forever (or real outages misclassified as
  `Transient`), reconsider whether `Transient` should be bounded at all.
- **Auth failure needs its own class** — if revoked-credential churn becomes a real problem,
  promote it from `Terminal` to a forwarder-level pause + operator notification.

## References

- ADR 0022 (`0022-forwarder-enqueue-by-config-presence.md`) — complementary: 0022 guarantees
  a `qso_upload` row is *created*; 0038 guarantees a connectivity outage never *abandons* it.
- `internal/forwarding/forwarding.go` — the `Outcome` enum + `Result`/`Forwarder` contract; where `OutcomeUnreachable` is added.
- `internal/forwarding/worker/worker.go` — give-up logic (the `attempts >= MaxAttempts` promotion to `failed`) that must skip `Unreachable`.
- `internal/forwarding/worker/backoff.go` — backoff cap/saturation that bounds the indefinite-retry probe interval.
- `internal/forwarding/qrz/qrz.go`, `internal/forwarding/clublog/clublog.go` — per-forwarder classification + retry defaults (`MaxAttempts: 5`, `60`/`1800`s).
- `internal/types/forwarder.go` — `RetryConfig` (`max_attempts`, `initial_backoff_sec`, `max_backoff_sec`).
- `docs/v2-design/forwarding.md` §5 — retry policy; needs an editing pass when this lands.
- Memory `project_sm_operator_network` (slow/unreliable link), `project_sm_user_base` (external operators incl. 7Q8AC, DXpedition framing).
