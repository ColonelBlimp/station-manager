---
number: 0070
title: One declarative lifecycle graph for every daemon subsystem (supervisor + orchestrator)
status: Accepted (operator-ratified 2026-08-17; implemented incrementally — see phased migration)
date: 2026-08-17
---

# 0070 — One declarative lifecycle graph for every daemon subsystem

## Context

The daemon's service-lifecycle semantics are distributed across ~12 subsystems and one
~900-line `cmd/smd/run()` function, and they are maintained as **two disconnected
dependency graphs that never reference each other**:

- **Start graph.** `internal/iocdi` holds a validated topological dependency graph — with
  cycle detection — but only over the *six* injected beans (config, eventhub, logging,
  sqlite, referencedb, qsoservice), and it knows only `Initialize()`. It has no notion of
  `Start`/`Stop`/`Open`/`Close`, no stop-side ordering, and is entirely absent from
  shutdown. The other lifecycle-bearing subsystems (bridge, ft8, evidence, pskreporter,
  refresher, the HTTP server, the forwarder workers, the SM-Cloud reconciler) are
  hand-wired *outside* the container.
- **Stop graph.** The shutdown order lives as hardcoded prose in `cmd/smd/shutdown.go`
  (`gracefulShutdown`: 8 ordered stages + 3 skip rules) plus LIFO `defer`s in `run()`.
  There is no shared source of truth linking it to the start graph.

The cost of this is paid repeatedly. Three subsystems (bridge, ft8, evidence) have
*independently converged* on the same terminal-single-use pattern — `sync.Once` +
`stopDone` barrier, a `stopped`/`life` admission gate, a service-context cancelled on
Stop, an in-flight `WaitGroup`. Three copies of the same mechanism, each re-deriving its
own correctness in comments. Meanwhile pskreporter and refresher implement the pattern
*incompletely* (no completion barrier; `Start` never refuses after `Stop`), which is the
open LC-5 finding. And the recent audit fixes LC-2 (one shutdown budget + dependency
ordering), LC-3 (atomic producer cutoff + must-drain), and LC-4 (service-context
cancellation) were each established **per service, by hand**, against exactly these
invariants — invariants the architecture forces every subsystem to re-establish from
scratch:

- accepted work is drained before teardown;
- every `Stop` caller observes completion;
- cancellation cannot resurrect a stopped service;
- an in-flight statement cannot hold shutdown past its budget.

These are not per-service properties. They are one lifecycle contract, expensive only
because it is not expressed once.

## Decision

**Establish one declarative lifecycle graph for every daemon subsystem.** Extend the
dependency metadata `iocdi` already maintains, and have a new **lifecycle orchestrator**
consume that graph to coordinate initialization, running state, failure rollback, and
**bounded reverse-order shutdown**. Remove the independently-maintained lifecycle ordering
from `run()` and `shutdown.go`.

Preserve one architectural boundary so the DI container does not become a stateful service
manager:

- **`iocdi` owns dependency *metadata* and *topology*** — the graph, the edges, the
  topological sort, cycle detection. It stays declarative and stateless-per-run. It gains
  richer edge *kinds*, not lifecycle *behavior*.
- **The orchestrator owns runtime *transitions*** — it reads the graph and drives
  `initialize → running → (failure rollback) → stopping → stopped` across the fleet, with
  one daemon-level deadline.
- **A per-service `Lifecycle` supervisor** (new `internal/lifecycle`) is the small,
  *owned* primitive that the terminal-single-use services embed: the lifecycle **phase**
  (`idle → running → stopping → stopped`), a service-context cancelled on stop, **per-lane**
  admission sealing + in-flight tracking, and a `stopOnce`/`stopDone` completion barrier. It
  makes each service's honest **phase** observable to the orchestrator — which layers its
  own transition *result* on top (see below) — while the service keeps its own per-lane
  drain policy in a `teardown` closure. It is a primitive, not a framework: the *identical* mechanism is shared; the
  *variable* mechanism (what to drain, in what order) stays specific per service — passing
  the project's "three-plus real uses" bar (five today) without repeating the
  `internal/adapters/` reflection-framework mistake.

Model this as a **typed lifecycle graph**: construction edges and drain edges are
*distinct* edge sets over the same nodes and need not mirror each other. **Shutdown is a
plain topological traversal of the *drain* graph** — a node tears down only after its
declared drain-prerequisites reach the required result — derived from the drain edges
directly, not by reversing the construction graph. (It is only *conventionally* the reverse
of construction/start order; calling it "reverse-topological over the drain graph" would
double-count the direction, since a drain edge already points prerequisite-first.) Every
ordering constraint is a predicate over an **observed result**, which removes
arrow-direction ambiguity and makes failure propagation testable — e.g. *"ft8's stop must
result in `drained` before evidence may begin draining."*

This forces a separation the draft blurred: **supervisor phase** and **orchestrator result**
are different things and must not be conflated. The *supervisor* exposes the service's honest
phase — `idle / running / stopping / stopped`. The *orchestrator* records, per node, the
**result** of the transition it drove — `drained` / `failed` / `timed-out` / `skipped`. They
do not coincide, and a result must never falsify a phase: a node whose Stop overran the
budget has result `timed-out` while its phase is *still* `stopping` (the abandoned Stop may
run on against the exiting process); a node the orchestrator never reached has result
`skipped` while its phase is *still* `running`. Drain-dependency predicates inspect the
prerequisite's **orchestrator result**, not its service phase. Both representations' Go form
is left to the companion design doc.

The vocabulary stays small — six concepts (the draft wrongly folded *activation* away):

1. **Construction dependencies** — build/`Initialize` order; iocdi already holds these.
2. **Drain dependencies** — a node may begin its teardown only after named prerequisites
   reach a required result (typically `drained`).
3. **Work disposition, per work lane** — a service owns *one or more* supervised work
   groups, and disposition is a property of the **lane**, not the service: each lane is
   either *must-drain* (sealed at stop, then awaited) or *cancellable* (bound to the
   service-context, interrupted at stop). Evidence is the proof — its writer lane must
   drain while its sync lane is cancelled.
4. **Activation and rollback-eligibility** — the orchestrator tracks each node's **highest
   successfully completed transition** (none → initialized → running), and this decides two
   *independent* things that must not be collapsed:
   - **Config-disabled** nodes never enter the active graph: they own no resources, are
     pruned, and their drain edges are *vacuously satisfied* — a disabled publisher lets the
     hub close and does **not** force a skip (the *opposite* of a failure-skip).
   - An **active but partially-initialized** node — one that completed `Initialize` but
     never reached `running` (logging is a live example: it opens its log file at
     `Initialize`) — is **rollback-eligible**: it still receives its own initialization
     cleanup (`Close`), *even though*, having never produced live work, it *vacuously
     satisfies* any dependent's drain prerequisite. "Vacuous for others" and "needs its own
     rollback" are orthogonal; the highest-completed-transition is what tells them apart.
     Pruning such a node would leak the resource `Initialize` acquired.
5. **State-qualified failure/skip propagation** — an *active* dependent is skipped when an
   *active* prerequisite's **result** is not the required one (a live producer that
   `timed-out` or `failed` — not merely one that is absent).
6. **A narrowly-scoped safety-priority policy** — the bridge fence, next.

**Topological, budget-bounded drain of the *drain* graph is the default derived behavior**
(prerequisite-first; conventionally the reverse of start order). Any exception is an
*explicit* edge or policy — never prose in `shutdown.go`.

**The one policy — `stopPriority: rf-critical` on the bridge — is a completion-or-deadline
*fence*, not a schedule hint.** It means bridge runs *exclusively*, with the whole
remaining budget available to it, and **no other teardown begins until bridge's transition
returns OR the deadline fires** (the behavior at `cmd/smd/shutdown.go:123` today). A *failed*
transition can return without leaving the service in a terminal phase, so the fence releases
on the transition *returning*, not on a terminal state.
This is strictly stronger than "scheduled first among eligible nodes," and it is what
guarantees the RF unkey its unobstructed attempt. Because the fence stands alone, PSK needs
no relationship to it and stays genuinely unconstrained and best-effort.

## The design's acceptance test

The gate is **not** "reproduce the exact eight-stage sequence" — that would overclaim. PSK,
HTTP, and the forwarder workers are intentionally *unconstrained* relative to several other
nodes; their present order does not "fall out" of the graph and must not be asserted. Any
deterministic tiebreak among simultaneously-eligible nodes is *operational*, not
architectural.

The real gate is: **can the graph reproduce the derived *partial order* — every safety
ordering constraint, the RF-priority fence, and every skip — from declared dependencies +
observed results?** If a *constraint or skip* cannot be derived, the model is missing a
lifecycle concept (add it); a missing total-order *tiebreak* is fine. Worked against the
current implementation:

| Constraint / skip (the architectural part) | Derives from |
|---|---|
| bridge is the sole teardown attempted until it completes or the deadline fires | fence policy `stopPriority: rf-critical` |
| evidence drains only after ft8 result `= drained` | drain dependency `evidence → ft8` (sole producer) |
| ft8-qso-log drains only after ft8 result `= drained` | drain dependency `qso-log → ft8` |
| hub closes only after http, workers, ft8-qso-log all result `= drained` | drain dependencies `hub → {http, workers, qso-log}` (publishers) |
| skip evidence / ft8-qso-log when ft8 result `≠ drained` | state-qualified skip on `→ ft8` |
| skip hub when any *active* hub-prerequisite result `≠ drained` | state-qualified skip on hub's edges |
| a *disabled* publisher lets the hub close (no skip) | activation — pruned from the active subgraph, edge vacuously satisfied |
| psk, and the mutual order of psk / http / workers | **unconstrained** — operational tiebreak, *not asserted* |

Every safety constraint and skip reduces to construction deps + drain deps +
per-lane work-disposition + activation + state-qualified skip, plus the one fence policy. The hub skip
in particular *improves* correctness: **"skip when a prerequisite did not drain" states the
send-on-closed-channel safety invariant directly, where the current "skip when the clock
expired" was only an indirect approximation of it.** So the redesign relocates
orchestration *and* sharpens a safety rule. This partial-order derivation is the executable
acceptance criterion phase 1 must satisfy.

LC-2, LC-3, LC-4 then become *consequences of the architecture*: LC-2 is the orchestrator's
one deadline + topological drain of the drain graph; LC-3 is the supervisor's admission-seal +
must-drain WaitGroup; LC-4 is the supervisor's service-context threaded through cancellable
work. New subsystems inherit all three by declaring a node and embedding the supervisor.

## Alternatives considered

- **Extend `iocdi` into a full service manager** (it drives Start/Stop and holds runtime
  state). Rejected: it collapses the metadata/transition boundary and makes the DI
  container stateful, which is precisely the coupling that makes lifecycle hard to reason
  about today. Keeping iocdi declarative (graph only) and putting transitions in a separate
  orchestrator is the whole point.
- **Shared supervisor primitive only; keep the hardcoded shutdown ordering.** Rejected: it
  de-duplicates the three barrier copies but leaves LC-2's ordering as prose in
  `shutdown.go` and the two graphs still disconnected. The leverage — the safety partial
  order and skips *falling out* of declarations — is exactly what's lost. Half the win, and the harder half
  stays hand-maintained.
- **Fix LC-5 as three specific patches** (add a `stopped` gate to psk/refresher, serialize
  sqlite Open/Close) and stop there. Rejected as the *primary* answer: it closes the P3
  finding but leaves the architecture that keeps generating LC-2/3/4/5-shaped work. (It
  remains the correct *fallback* if this ADR is not ratified, and its content is subsumed
  here as the psk/refresher migration.)
- **A generic reflection-based lifecycle framework.** Rejected on sight — the
  `internal/adapters/` cautionary tale. The supervisor is a concrete ~2-method primitive
  with no reflection; the orchestrator consumes concrete declarations. Neither abstracts
  *across* service shapes: divergent signatures (`Start(ctx)`/`Open()`/`Run(ctx)`/two-phase
  `StopAccepting`+`Shutdown`) stay specific, adapted at the orchestrator boundary by
  closures, exactly as `teardownDeps` already does.
- **Do nothing (status quo).** Rejected: the repeated-repair cost is the finding. But note
  the current code is *correct* under the daemon's ordered single-use calls — this is a
  leverage/maintainability decision, not a bug fix, which is why it is Proposed for
  deliberate ratification rather than rushed.

## Consequences

Good:

- One source of truth for lifecycle order; the safety constraints + skips become derived
  and tested (the architectural content of today's 8 stages), not prose. `run()` and
  `shutdown.go` shed their hand-ordered lifecycle logic.
- LC-2/3/4 become structural guarantees; a new service inherits testable shutdown
  semantics by declaring a node + embedding the supervisor.
- Uniform status: lifecycle state + degradation reported the same way across subsystems
  (the supervisor exposes it; the evidence `StatusContext` degraded-shape is the template).
- The three inconsistent shapes (psk, refresher) and the LC-5 gaps are fixed *by adoption*,
  not by three more bespoke patches.

Bad / costs:

- Real migration churn. Bridge and ft8 are adapted as graph nodes (phase 3, mandatory) but
  **retain their proven internal barriers**; replacing those barriers with the shared
  primitive is deferred to an optional later phase, so the guaranteed-stop discipline is
  never put at risk merely to complete the architecture.
- A new `internal/lifecycle` package and richer iocdi metadata — new surface to learn.
  Justified only by the five real uses; not to be extended speculatively.
- sqlite stays deliberately **restartable** (contract #2, Open/Close, reopen supported) —
  it is *not* forced into the terminal supervisor; it gets the specific Open/Close
  serialization fix and a node with the right edge kind. api.Server's two-phase
  (StopAccepting/Shutdown) likewise stays specific, adapted at the orchestrator boundary.

Phased migration (each phase independently shippable, TDD + reversion-proofed, the
acceptance-test partial-order assertion added early and kept green):

1. **Supervisor primitive** + the graph-metadata extension + orchestrator skeleton, with
   the acceptance test asserting the derived partial order + skips match today's
   `gracefulShutdown` for the *current* hand-declared graph.
2. **Adopt the supervisor on the inconsistent + fresh services first** (lowest risk,
   highest clarity): evidence (freshest), then pskreporter and refresher — this lands LC-5.
3. **Move ordering into the orchestrator, and adapt *every* lifecycle subsystem as a graph
   node — including bridge and ft8.** This is what makes it genuinely *one* orchestration
   graph, and it is mandatory for ADR completion. Bridge and ft8 **retain their proven
   internal barriers** here and are adapted only at the node boundary (their observable
   phase/result, and bridge's `stopPriority: rf-critical` fence). Replace `gracefulShutdown`'s
   stages with the derived drain; keep LC-2's budget + `safetyNetStop` as orchestrator
   policy; sqlite's Open/Close serialization + node, and api's two-phase / hub / logging as
   adapted nodes.
4. **Optional, later de-duplication — NOT an ADR-completion requirement:** replace bridge's
   and ft8's internal barriers with the shared `lifecycle` supervisor. The architectural
   win is *one orchestration graph* (phase 3), not one barrier implementation; do this only
   if the DRY is worth touching RF/TX-safety-critical code, behind their suites.

Detailed supervisor/orchestrator API and the metadata schema will be fleshed out in a
`docs/v2-design/lifecycle.md` companion during phase 1; this ADR fixes the decision and
the boundary, not the signatures.

## Triggers to revisit

- If, during phase 1, the acceptance test **cannot** reproduce a safety *constraint* or
  *skip* from declarations + observed results, stop: the graph model is missing a concept
  (add it) — do not reintroduce a hardcoded exception. (A missing total-order *tiebreak*
  among unconstrained nodes is not a trigger — that ordering is operational.)
- If the supervisor grows per-service hooks/config to fit divergent services, it is turning
  into the framework the `adapters` lesson warns against — pull the variation back out into
  the service-specific `teardown`.
- If the orchestrator starts holding domain/service state beyond lifecycle transitions, the
  metadata/transition boundary has leaked — the DI-container-as-service-manager failure
  mode this ADR exists to avoid.
- If the fleet shrinks back toward one or two lifecycle services (unlikely), the
  three-plus-uses justification weakens and specific implementations regain the edge.
