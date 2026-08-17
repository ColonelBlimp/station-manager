# Daemon lifecycle: supervisor + orchestrator (design)

**Status:** draft (phase 1 of ADR 0070). Companion to
[`docs/decisions/0070-daemon-lifecycle-graph.md`](../decisions/0070-daemon-lifecycle-graph.md),
which fixes the *semantics and invariants*. This doc fixes the *signatures and metadata
types*. The ADR is authoritative where they disagree.

Three pieces, one boundary (ADR 0070): **`iocdi`** owns dependency *metadata + topology* — via a
lifecycle-node registry independent of bean registration, exposing one **immutable plan**; the
**orchestrator** (`internal/lifecycle/orchestrator`) owns runtime *transitions*; a per-service
**supervisor** (`internal/lifecycle`) is the owned primitive that makes each service's phase
observable. Nothing abstracts across service shapes — divergent signatures stay specific and are
adapted at the orchestrator boundary by closures bound to node IDs.

**Phase-1 traversal is deterministic and sequential** — one node at a time. Parallel teardown is
unnecessary migration risk and makes "the node that exhausted the budget" ambiguous; the
partial-order test still does not assert the operational tiebreak.

---

## 1. Phase, Milestone, Result (three vocabularies, kept separate)

```go
// package lifecycle — owned by the service's Supervisor.
type Phase uint8
const (
	Idle     Phase = iota // constructed; Start may run (also the state after a cleaned-up start failure)
	Running               // Start's acquire succeeded; admission open
	Stopping              // Stop sealed admission; teardown in progress
	Stopped               // teardown completed
)
```

```go
// package orchestrator.
// Milestone: the highest transition the orchestrator successfully drove for a node; Rollback
// unwinds from here. It exists because Phase has no "initialized": a node that completed
// Initialize but not Start is Idle-phase yet Initialized-milestone (rollback-eligible).
type Milestone uint8
const ( MilestoneNone Milestone = iota; MilestoneInitialized; MilestoneRunning )

// Result: the orchestrator's settled verdict about the stop transition it drove. Never a Phase.
type Result uint8
const (
	Pending  Result = iota
	Drained               // Stop returned cleanly within budget
	Failed                // Stop returned an error
	TimedOut              // budget fired while still Stopping (phase stays Stopping)
	Skipped               // a prerequisite was not satisfied; not attempted
	Inactive              // CONFIG-DISABLED only — pruned; its edges are vacuously satisfied
)
```

**Satisfaction is positive (blocker 4 — transitive skip).** A drain edge is satisfied **only** by
a prerequisite result of `Drained` or `Inactive`. *Every other settled result — `Failed`,
`TimedOut`, and `Skipped` itself — propagates `Skipped`.* Without the `Skipped`→`Skipped` case,
`ft8` `TimedOut` → `qso-log` `Skipped` would leave the hub eligible and closed under a live
publisher.

**`Inactive` means config-disabled only.** An *active* node that never reached `Running` (a real
`Start` failed after `Initialize`) is **not** relabelled `Inactive` — it is rollback-eligible
(§4.2), handled on the start-failure path, never in the shutdown drain.

---

## 2. Supervisor — the owned per-service primitive

A terminal-single-use service **owns** a `*Supervisor` (a field built in `New`). It replaces the
hand-rolled `stopOnce`/`stopDone`/`stopped`/`life`/`cancel`/admission-`WaitGroup` that bridge,
ft8, and evidence each carry.

```go
type Supervisor struct { /* mu, phase, ctx, cancel, stopOnce, stopDone, stopErr, lanes */ }
func New() *Supervisor

// RegisterLane declares a named work lane BEFORE Start (no lazy string lookup on the hot
// producer path). Disposition is fixed at registration; the returned handle is held by the
// service and used directly.
func (s *Supervisor) RegisterLane(name string, d Disposition) *Lane
type Disposition uint8
const ( MustDrain Disposition = iota; Cancellable )

// Start owns the WHOLE start transition (blocker 1) under the TRANSITION LOCK, so it is atomic
// with respect to Stop (which takes the same lock). Under that lock, IN ORDER: (1) derive the
// service context from parent; (2) acquire — fallible resource acquisition, while public
// admission is still CLOSED; (3) launch — infallible, using the private StartScope to pre-register
// long-lived worker counts and start them while admission remains closed; (4) the COMMIT POINT —
// publish Running and open public Lane.Admit; (5) release the lock. NO public Admit succeeds
// before the commit point, so status never reports Running with workers not yet registered. On
// acquire failure it cancels the derived context, stays Idle (retryable; acquire cleans up its own
// partial resources), and returns the error without launching or committing. No-op if already
// Running; terminal refusal if Stopped.
//
// INVARIANT: resources acquired, worker counts registered, admission opened, and Start completed
// form ONE transition with respect to Stop AND to producers — public Admit stays closed until the
// commit point. The transition lock is SEPARATE from the phase mutex Admit reads.
func (s *Supervisor) Start(
	parent context.Context,
	acquire func(ctx context.Context) error,           // fallible: open resources (admission closed)
	launch  func(ctx context.Context, sc *StartScope),  // infallible: pre-register + start workers (admission still closed)
) error

// StartScope is the private token launch uses to register long-lived workers BEFORE the commit
// point (public Admit is still closed). Use Track for CANCELLABLE workers — Stop cancels the
// context, they exit, and Stop's lane-wait completes. A TEARDOWN-SIGNALLED worker (one that exits
// only when teardown closes its quit channel) must NOT be Track'd: Stop waits lanes BEFORE
// teardown, so that would deadlock; those stay on the service's own WaitGroup, waited inside
// teardown.
type StartScope struct { /* supervisor-owned */ }
func (sc *StartScope) Track(lane *Lane) (done func())

// Context returns the service context, valid while Running, cancelled at Stop. Cancellable-lane
// work binds to it.
func (s *Supervisor) Context() context.Context

// Stop seals EVERY lane (Running→Stopping) under the same mutex Admit reads (LC-3 cutoff), cancels
// the context (Cancellable-lane work interrupts and releases its locks — LC-4), waits EVERY lane's
// admitted work (cancellable AND must-drain) to finish, runs teardown exactly once, stores its
// error, then → Stopped and releases every caller. ALL callers return the SAME teardown error.
// Cancel-before-wait is deliberate: it lets cancellable DB work release locks that must-drain work
// needs. Stop-before-Start is terminal and runs no teardown.
func (s *Supervisor) Stop(teardown func() error) error

func (s *Supervisor) Phase() Phase

type Lane struct { /* disposition, wg, sealed ref */ }

// Admit registers one unit of in-flight work on EITHER lane kind. ok is true ONLY while
// Phase == Running — refused BEFORE Start (admission not yet open) and once sealed at Stop — so a
// pre-Start producer is never accepted. The caller does nothing when ok is false. A true return
// MUST be paired with exactly one done(). Stop waits for admitted work on both kinds; the only
// difference is that a Cancellable lane's work also binds to Context() and is interrupted at Stop,
// while a MustDrain lane's runs to completion uninterrupted.
func (l *Lane) Admit() (done func(), ok bool)
```

**Stop sequence:** `stopOnce.Do`, holding the **transition lock** (so it cannot interleave with
Start): seal *every* lane under the phase `mu` (phase→`Stopping`; `Admit` now `ok=false`) →
`cancel()` the context → `wg.Wait()` *every* lane (both kinds) → `stopErr = teardown()` →
phase→`Stopped`, `close(stopDone)`. The **budget** is the orchestrator's concern (§4); an overrun
is abandoned there with `TimedOut` while the phase stays `Stopping`.

**Teardown finalization.** Everything runs inside `stopOnce.Do`, and `defer close(s.stopDone)` is
registered **first** so it releases every waiting caller even if teardown panics. A teardown panic
is **recovered and converted to the shared `stopErr`** — the panic value *and* stack folded into
the error — so every caller returns it and the orchestrator records (and logs) `Failed`. It is
**not** re-panicked: during shutdown a propagating panic would abort the teardown of every *other*
service. `internal/lifecycle` stays independent of logging: `New` takes no handler and the
orchestrator owns the log. Concurrent callers are never stranded — `close(stopDone)` always runs
and all callers read the same `stopErr`; the phase still advances to `Stopped` (the stop attempt is
over, with an error).

### Adoption sketch (evidence — the per-lane proof)

```go
type Service struct {
	life   *lifecycle.Supervisor
	writer *lifecycle.Lane // MustDrain — tracks CaptureSlot HANDOFFS (producers), NOT the writer goroutine
	sync   *lifecycle.Lane // Cancellable — the sync worker (Track'd in launch)
	wg     sync.WaitGroup  // the writer + queueloss goroutines: TEARDOWN-signalled, waited inside teardown
}
func New(...) *Service {
	l := lifecycle.New()
	return &Service{life: l,
		writer: l.RegisterLane("writer", lifecycle.MustDrain),
		sync:   l.RegisterLane("sync", lifecycle.Cancellable)}
}
func (s *Service) Start() error { return s.life.Start(context.Background(), s.acquire, s.launch) }
// acquire: open + migrate the DB (fallible; transactional cleanup on error); admission still closed.
func (s *Service) launch(ctx context.Context, sc *lifecycle.StartScope) {
	// writer + queueloss are TEARDOWN-signalled ⇒ service wg, NOT a lane (a lane Stop waits before
	// teardown would DEADLOCK — teardown is what signals them to exit). They must NOT bind to the
	// supervisor ctx: Stop cancels it at the SEAL, but the writer must keep draining until teardown,
	// and its DB writes are non-cancellable during shutdown (LC-4). They exit on s.quit; their DB
	// work uses context.Background().
	s.wg.Add(2)
	go func() { defer s.wg.Done(); s.writerLoop() }()
	go func() { defer s.wg.Done(); s.queueLossLoop() }()
	// sync is a CANCELLABLE worker ⇒ it binds to the supervisor ctx (cancelled at the seal) and is
	// lane-tracked, so Stop waits it out AFTER cancelling the context.
	done := sc.Track(s.sync)
	go func() { defer done(); s.syncLoop(ctx) }()
}
func (s *Service) CaptureSlot(sc SlotCapture) {
	done, ok := s.writer.Admit() // pre-Start or sealed ⇒ refuse, uncounted (producer handoff)
	if !ok { return }
	defer done()
	// stamp + non-blocking send
}
func (s *Service) teardown() error {
	close(s.quit); s.wg.Wait() // signal + drain the writer/queueloss goroutines, THEN close the DB
	return s.db.Close()
}
// PUBLIC signature UNCHANGED — evidence.Stop() returns nothing (cmd/smd wires stopEvidence func()):
func (s *Service) Stop() { _ = s.life.Stop(s.teardown) }
// The orchestrator adapter exposes the func(ctx) error shape SEPARATELY, wrapping the same call:
//   Stop: func(ctx context.Context) error { return s.life.Stop(s.teardown) }
```

---

## 3. Node metadata — an iocdi lifecycle-node registry (independent of beans)

Half the lifecycle subsystems are **not** iocdi beans, so construction order cannot come from
`di.inject` tags alone (blocker 3). iocdi gains a **lifecycle-node registry** — separate from bean
registration — carrying declarative metadata for *every* lifecycle node and exposing **one
immutable plan**. The orchestrator consumes that plan and binds adapters to node IDs; metadata has
a single source of truth.

```go
// package iocdi (lifecycle-node registry; declarative only — no behavior).
type Node struct {
	Name         string   // stable id, e.g. "evidence"
	StartAfter   []string // start prerequisites, EXPLICIT (covers non-bean nodes; bean nodes may
	                      // list them or have them derived from the di graph)
	DrainAfter   []string // drain prerequisites: this node's Stop waits until each is Drained/Inactive
	StopPriority Priority // Normal | RFCritical (the fence, §4.4)
}
type Priority uint8
const ( Normal Priority = iota; RFCritical )
```

Activation is **not** declarative metadata (blocker 3): `Active` is evaluated once at startup and
latched by the orchestrator (§4.1), never re-read from mutable config during shutdown.

### 3.1 Registry, plan, and validation

```go
// package iocdi — the lifecycle-node registry.
func (c *Container) RegisterNode(n Node) error // preserves registration ORDER (the shutdown tiebreak)
func (c *Container) Plan() (*Plan, error)       // builds + validates the immutable plan once

type Plan struct { /* nodes in registration order; start graph; drain graph */ }
func (p *Plan) Nodes() []Node        // stable registration order
func (p *Plan) StartOrder() []string // topological over the MERGED start graph; ties by registration order
```

`Plan()` is the single source of truth and **fails (the daemon refuses to start)** on:

- an empty or duplicate node ID;
- a `StartAfter`/`DrainAfter` reference to an unknown node;
- a cycle in the start graph **OR** (separately) the drain graph — the two are checked
  *independently*, because a legal drain order need not be the reverse of a legal start order;
- more than one `RFCritical` node (the fence is singular);
- (checked at `Orchestrator.Start`, since adapters are the orchestrator's) a plan node with zero or
  more than one adapter binding — **exactly one adapter per plan node**.

**Merging start edges deterministically:** a bean node's effective start prerequisites are the
UNION of its explicit `StartAfter` and the edges derived from its `di.inject` dependencies;
duplicates merge; the start graph is topologically sorted with ties broken by **registration
order**. Non-bean nodes rely on `StartAfter` alone. Registration order is also the deterministic
tiebreak for the sequential shutdown traversal (§4.3), so `RegisterNode` must preserve it.

**Immutability.** `Plan()` **freezes** the registry — a subsequent `RegisterNode` fails — and both
`Nodes()` and `StartOrder()` return defensive **deep** copies (the `StartAfter`/`DrainAfter` slices
are copied too), so no consumer can mutate the shared graph after the plan is built.

---

## 4. Orchestrator — runtime transitions

### 4.0 Splitting iocdi's Build (blocker 1 — one owner for Initialize)

Today `Container.Build()` both wires *and* `Initialize()`s every bean (`container.go:274`) — a
second initialization owner. Split it:

- **`Container.Wire() error`** — constructs + injects in dependency order, but does **not**
  `Initialize`.
- The **orchestrator** exclusively drives `Initialize` (through adapters) and records milestones.
- **`Build()` stays as a compatibility wrapper** (`Wire()` + Initialize-all in topo order) for the
  `import`/`restore` subcommands, which want the beans up without orchestration.

**Resolution ensures wiring only — never initialization.** Today `ResolveSafe` calls `Build()`
whenever `built == false` (`container.go:293`), so a legacy `Resolve` *after* orchestrator startup
would re-`Build()` and double-initialize. Rather than add a `MarkInitialized` back-channel from the
orchestrator into iocdi, change resolution:

- **`Resolve` / `ResolveAs` ensure *wiring* only** — they `Wire()` if not yet wired and return the
  constructed bean **without** `Initialize`. This is the orchestrator's path *and* removes the
  double-init route entirely (there is no lazy initialize-on-resolve to trip).
- **Callers that need initialized beans call `Build()` explicitly first** — `import`, `restore`,
  and today's daemon startup already do exactly that, so the boundary is preserved without a
  back-channel.

**Guardrail (single initialization owner).** A given `Container` instance uses **either** explicit
`Build()` initialization **or** orchestrator-owned initialization — **never both**. The daemon uses
the orchestrator; `import`/`restore` use `Build()`; a single container must not mix them, or a bean
could be initialized twice. This is enforced, not just documented (a container that has run
`Build()` refuses orchestrator initialization, and vice versa).

**Test coverage required:** `Wire()` → `Resolve` (build adapters, no init) → `Orchestrator.Start`
drives `Initialize`, asserting **each initializer runs exactly once**; and a later stray `Resolve`
does **not** re-initialize (because `Resolve` is wire-only).

### 4.1 Adapter and Orchestrator

```go
type Adapter struct {
	NodeID      string                           // binds to ONE immutable plan node (not a copied Node)
	Active      func() bool                      // evaluated ONCE at Start, latched;  nil ⇒ always active
	Initialize  func() error                     // → MilestoneInitialized;             nil ⇒ trivially initialized
	Start       func(ctx context.Context) error  // → MilestoneRunning (wraps Supervisor.Start);
	                                             //   nil ⇒ AUTO-PROMOTE to Running once Initialized (construction-only nodes)
	PrepareStop func()                            // optional, NON-BLOCKING (§4.4);      nil ⇒ no-op
	Stop        func(ctx context.Context) error  // supervised, budget-bounded; error ⇒ Failed;  nil ⇒ trivially Drained
	Rollback    func(reached Milestone) error    // start-failure cleanup;              nil ⇒ no-op
	Phase       func() lifecycle.Phase           // observable phase;                   nil ⇒ inferred from milestone/result
}

type Orchestrator struct { /* plan, adapters-by-id, milestones, results, latchedActive */ }
func (o *Orchestrator) Register(a Adapter)          // NodeID must exist in the iocdi plan
func (o *Orchestrator) Start(ctx context.Context) error
func (o *Orchestrator) Shutdown(budget time.Duration)
func (o *Orchestrator) Result(node string) Result
```

**Nil-hook behavior is defined for every field** (above): a construction-only node — `Initialize`
+ `Stop` but nil `Start` (e.g. logging: `Initialize` opens the file, `Close` closes it) —
**auto-promotes to `MilestoneRunning`** the moment `Initialize` succeeds, so normal shutdown sees
it as a `Running` node and drives its `Stop` (= `Close`). That keeps "shutdown sees only `Running`
or `Inactive`" true (blocker 4).

### 4.2 Start, rollback, and why shutdown sees only Running/Inactive

`Start` latches every node's `Active()` **once** (config-disabled → latched inactive, pruned from
all traversals), then in `StartAfter` topological order drives `Initialize` then `Start` for each
active node, recording its `Milestone` as each step succeeds. **On any failure it rolls back**
every node from its recorded milestone (reverse order) — `MilestoneRunning` → its `Stop`,
`MilestoneInitialized` → its `Rollback(MilestoneInitialized)` — and returns the error; the daemon
exits without a shutdown traversal. So by the time **shutdown** runs, every active node is
`Running` (real or auto-promoted) and every config-disabled node is `Inactive`; the drain never
meets a partially-initialized node.

### 4.3 Shutdown (derives LC-2's stages + skips, sequentially)

1. **PrepareStop** (§4.4) — run every active node's non-blocking `PrepareStop`, *before* the budget
   and fence.
2. **Budget** — one `context.WithTimeout(budget)` bounds everything below.
3. **Fence** — the `RFCritical` node is the **sole** teardown attempted until its `Stop` *returns*
   or the deadline fires (via the same goroutine+race as step 4); no other `Stop` begins until then.
4. **Sequential topological drain** — repeatedly pick the next eligible node (all `DrainAfter`
   prerequisites satisfied — `Drained` or `Inactive`) in deterministic registration order, one at a
   time. **Bound each `Stop` (blocker 3):** run it in a goroutine with a **buffered** done channel
   and race completion against the shared deadline (exactly `shutdownCoord.run` today) — a service
   that ignores its context cannot block the traversal. On completion → `Drained`/`Failed` (from the
   error); on the deadline → `TimedOut` (abandoned Stop runs on against the exiting process). The
   result is **latched**, and completion **wins** a tie with expiry (a second non-blocking recv on
   the buffered channel).
5. **Transitive skip** — a node with any prerequisite whose settled result is not `Drained`/
   `Inactive` is `Skipped` (not attempted), and its own `Skipped` propagates onward (§1).
6. **One warning** — the first node to exhaust the budget is named once (unambiguous because the
   traversal is sequential).

### 4.4 PrepareStop (blocker 5 — bring the last two transitions in)

`server.StopAccepting()` and `workerCancel()` currently live in `run()`, outside orchestration.
They become each node's optional, non-blocking `PrepareStop`, run before the budget/fence: HTTP's
seals its accept listener, the forwarder-workers' cancels their context. **Evidence's `PrepareStop`
is nil** — its admission must stay open until FT8 drains, so it seals only inside its own `Stop`.

---

## 5. Acceptance test (phase-1 gate)

Build the **current** hand-declared plan (real nodes + `DrainAfter` + fence + latched activation)
and assert the derived **partial order + skips** match today's `gracefulShutdown`:

- every drain constraint holds (evidence after `ft8=Drained`, hub after all publishers, …);
- the RF fence holds (bridge is the sole teardown until its Stop returns or the deadline fires);
- skips fire and are **transitive** (`ft8` `TimedOut` → `qso-log` `Skipped` → hub `Skipped`);
- an *inactive* publisher (FT8 config-disabled) does **not** force a hub skip;
- a construction-only node (logging) drains normally after everything else;
- the sequential tiebreak among psk/http/workers is **not** asserted (operational).

---

## Resolved decisions (were open questions)

1. **Packages** — `internal/lifecycle` (supervisor, `Phase`, `Lane`); `internal/lifecycle/orchestrator`
   (`Milestone`, `Result`, `Adapter`, `Orchestrator`).
2. **Metadata home** — an iocdi-owned lifecycle-node registry *independent* of bean registration,
   exposing one immutable plan; `StartAfter` makes start order explicit for non-bean nodes; adapters
   bind by `NodeID`.
3. **iocdi Build split** — `Wire()` (construct+inject, no Initialize) is the new primitive; the
   orchestrator owns Initialize; `Build()` remains a compatibility wrapper.
4. **Lanes** — named `MustDrain` and `Cancellable` lanes, both admitting tracked work, registered
   before Start; Stop waits both kinds after cancelling the context.
5. **Milestone replaces HighWater**; nil-hook behavior is defined per adapter field (nil `Start` ⇒
   auto-promote to Running).
6. **Traversal** — deterministic sequential in phase 1; each `Stop` goroutine-raced against the one
   budget.
