# Station Manager v2 — Serial / CAT Bridge Design

> **2026-05-10 banner — read this first.** ADR 0019 ([`../decisions/0019-bridge-subsystem-v1-design.md`](../decisions/0019-bridge-subsystem-v1-design.md)) is the canonical v1 internal design. **v1 is read-only** — rig pushes state, bridge filters and forwards to SPA, SPA displays. NO PTT and NO inbound command path in v1. ADR 0019 supersedes parts of this document:
>
> - **§3 (SM-internal Unix-socket multiplexer)** — parked, not v1. Will revisit when a non-browser in-house client (FT8 stack, future CAT control SPA) needs it.
> - **§4 frontend list** — v1 ships ONE frontend (SSE for the logging SPA). rigctld-compat TCP is parked; NDJSON Unix socket is parked. Both build when their drivers appear.
> - **§6 (YAGNI question)** — answered by ADR 0013 (bridge IS built, as a subsystem) and ADR 0019 (with the v1 internal shape: read-only).
> - **§3e PTT safety + §4 PTT convention** — **deferred along with the rest of PTT.** v1 has no PTT awareness because the SPA doesn't assert PTT in v1. When the inbound command path lands later, the disconnect-safety-release rule from §3e fires for real (it's the right design, just not v1).
> - **§7 performance discussion** — still canonical (and reaffirmed by ADR 0019).
> - **§2 AUTO-mode CAT assumption** — still canonical.
>
> Treat the *protocol shape* (AUTO-mode push-state) as canonical via §2. Treat the *wire shape* (`/v1/rig/events` SSE, three event types) as canonical via ADR 0010 (cache section revised by ADR 0019; M3a.3 added the `splitOverride` `*bool` clarification + `bridge-error` hub-cache exception). Treat the *internal design* (read-only, stateless filter, poll-on-SSE-open, no rig-state cache, no PTT) as canonical only via ADR 0019. The PTT safety net design from §3e is canonical but deferred — it lands when the inbound command path does.
>
> **Implementation status (2026-05-11):** M3a.1 + M3a.2 + M3a.3 + M3a.4 all shipped. **M3a (bridge subsystem v1) is closed.** Pipeline reads AUTO-mode rig pushes, decodes via `internal/cat`, filters to the SPA-relevant field set, fans out over SSE at `/v1/rig/events`. SPA's `frontend/logging/src/lib/states/bridge.svelte.ts` is a real EventSource consumer that merges `rig-state` events into `catState`, toasts `rig-disconnected` + `bridge-error`, and gates open/close on `configState.station.enabled` (now daemon-authoritative via the `bridge.enabled` field on `/v1/config`). Live-tested on the real FTdx10 — VFO updates on dial, mode reflects, identity + power populate the My Station Equipment panel read-only when CAT is live. Daemon-side `responseRecorder.Unwrap()` fix in `internal/api/middleware.go` was required to let `http.ResponseController.SetWriteDeadline` traverse the access-log middleware and disable the server's 30s WriteTimeout for SSE streams. `BridgeInfo` on `/v1/config` now exposes `Driver`, `RigName`, `RigModes` (rigdef MAINMODE value list), and `ModeMappings` (merged view of rigdef defaults + operator overrides).
>
> **Rig-mode → ADIF translation SHIPPED session 51 (2026-05-11)** — the M3a.4 follow-up that was parked. Two-layer per-rig translation: rigdef-shipped defaults inside each `internal/cat/rigs/*.json` under `mode_mappings`, operator overrides in `config.json` under `bridge.mode_mappings`, merged at `/v1/config` GET time (operator wins per-rig-string on collision; PUT diffs against defaults so only deviations persist). Bridge stays a pure pass-through (rig literal on the wire — no mapping logic added there); SPA's `displayedState.mode` / `.subMode` derivations consult the merged table when CAT is live, returning ADIF-resolved values directly. New My Station → Mode Mappings sub-tab edits the override layer (one row per rig mode, free-text MODE + SUBMODE inputs, validated daemon-side on save).
>
> **`internal/enums/modes` is now data-driven** (side-effect of session 51): the hardcoded Go enum was replaced by an embedded `adif-modes.json` baseline (~50 ADIF 3.x main modes incl. FT8/FT4/FST4/JS8/JT65 promoted from MFSK submodes per current spec) plus an optional `$SM_WORKING_DIR/modes.json` operator override loaded at daemon startup via `modes.LoadOverride(workingDir)`. An ADIF spec release no longer strictly requires a daemon binary update — the operator can extend their override file immediately.
>
> **Bridge-event payloads switched to error codes + i18n** (session 51 continuation, 2026-05-12; ADR 0010 revision 6): `rig-disconnected` payload is now `{code, details?}` instead of `{reason}`; `bridge-error` is now `{code, details?}` instead of `{message}`. The SPA's new `lib/i18n/` catalogue (English baseline at `lib/i18n/en.ts`) renders operator-facing wording from the code + details, with the future Tumbuka and Chichewa localizations planned. Bridge-side cache also grew a `lastRigDisconnected` slot paralleling the existing `lastBridgeError` so a fresh SPA tab sees the toast for an off-rig that never came up. Hub clears `lastRigDisconnected` on the next `rig-state` event (auto-recovery), since the implicit-reconnect flow per ADR 0009 makes the cached disconnect stale. `rig-state` payload is unchanged.

**Status:** Draft, last revised 2026-04-20 during a design re-examination session. **Read with the 2026-05-02 topology decisions in mind:** [ADR 0013](../decisions/0013-daemon-owns-bridge-as-subsystem.md) settles that the bridge is an internal subsystem of the daemon binary in the default deployment (`internal/bridge` package, registered into the daemon's HTTP server, gated by a `bridge.enabled` config flag). [topology.md](topology.md) was rewritten the same day to reflect that. The split-host deployment (separately-built `cmd/bridge` binary using the same `internal/bridge` package) remains supported as an opt-in. **Several "is the bridge a separate process?" assumptions in this document predate ADR 0013**; treat the *protocol shape*, *frontend list*, *AUTO-mode CAT assumption*, and *PTT safety rules* in this document as canonical, but treat the *deployment shape* as superseded by ADR 0013 / topology.md.

The 2026-04-20 revision also re-examined whether the bridge needs to exist at all (§6 below). [ADR 0010](../decisions/0010-rig-sse-wire-shape.md) and [ADR 0013](../decisions/0013-daemon-owns-bridge-as-subsystem.md) effectively answer "yes, build the bridge as a subsystem" — the SPA needs the rig SSE for live VFO display regardless of whether other in-house clients exist. The §6 YAGNI question is therefore closed for v2: build it, but as a daemon subsystem, not as its own binary by default.

The 2026-04-20 revision also dropped rigctld-compat TCP and PTY frontends. **That decision was reversed** by [invariants.md](../v1-analysis/invariants.md) "Two-frontend bridge shape": rigctld-compat TCP + SM-native event stream is canonical. Treat the §-discussions in this document that drop rigctld TCP as superseded; the SM-native frontend is now SSE on `/v1/rig/events` per [ADR 0010](../decisions/0010-rig-sse-wire-shape.md), not Unix-socket NDJSON.

**Everything in this document is still subject to revision.** Even sections marked "decided" may change once construction starts.

**Purpose:** Capture the *why* of the bridge's design so it survives session handoff. Once enough of the open questions are closed, this becomes the input to `cmd/sm-serial-bridge` construction — if we build it at all (see §6).

**How this document relates to others:**

- `docs/v1-analysis/invariants.md` §"Two-frontend bridge shape" and §"Serial/CAT bridge SM-native frontend: NDJSON over Unix socket" — the pre-v2 thinking. The "two-frontend" shape described there is the one this document now steps back from; the NDJSON-over-Unix-socket transport carries forward.
- `docs/v1-analysis/design-decisions-log.md` §"Serial/CAT bridge SM-native frontend: NDJSON over Unix socket" — same. NDJSON transport stays decided.
- `docs/v2-design/structure.md` — pure Go binaries (including this one if built) live in the root module.
- `docs/v2-design/milestones.md` §Milestone 3 — `cmd/sm-serial-bridge` is listed as an external-integration binary. Scope there is a one-liner; this doc is the detail.
- `docs/v2-design/api.md` — daemon API. The bridge does **not** call the daemon. Rig control and QSO logging are separate concerns.

---

## 1. Problem statement (revised)

**v1 version of the problem:** SM and WSJT-X/JTDX both want to drive the same radio over one USB port. USB serial devices are single-owner. Something has to mediate shared access.

**What changed in v2:** the daemon absorbed the QSO-logging concern. The reason multiple apps used to want the same serial port was that the logging app *was* the database, so any app that logged a QSO needed to be the logging app, so it needed the port. With a separate daemon, every app logs QSOs via HTTP to the daemon over a Unix socket — **port ownership decouples from logging**.

**What this means in practice:**

- **WSJT-X / JTDX own their own rig's port directly.** They log QSOs via UDP logging packets (their existing protocol) → a future `cmd/wsjtx-bridge` translates UDP → daemon HTTP. No serial contention with SM.
- **A bespoke SM FT8 app, if built, runs on its own rig** (user point 5 from the 2026-04-20 re-examination). Different port, no contention.
- **The logging app owns its rig's port directly** when running. Uses `internal/cat` + `internal/serial` without a bridge in between.
- **The only scenario that *might* want a multiplexing bridge is: logging app + a future CAT control app on the *same* rig, both wanting CAT access simultaneously.** The logging app as read-only listener, the CAT control app as listener+setter.

User quote (2026-04-20): "Typically, two apps writing to the same serial device is rare to non-existent — a deliberate effort to do what should not be done." So even the remaining contention scenario is SM-internal and uncommon.

**Multi-rig is still first-class** — a serious station routinely runs more than one rig (SO2R, contest). Each rig is its own contention domain; multi-rig resolves by ports being distinct, not by any multiplexing magic.

---

## 2. CAT model assumption: push-state AUTO / transceive

Unchanged from earlier thinking. Modern rigs run in push-state modes (Yaesu AUTO, Icom CI-V transceive) — state changes propagate from the rig automatically without being polled. The bridge's design assumes this. The rig is a state broadcaster; clients observe and occasionally poke. "Client B sees a frequency update it didn't ask for" is a feature, not corruption.

Do not re-derive classic request/response framing for CAT. See `docs/v1-analysis/invariants.md` §"CAT protocol model" for the fuller discussion.

---

## 3. Design (if built): Unix-socket-only, SM-internal multiplexer

The bridge is an **SM-internal** fan-out. Third-party apps (WSJT-X, JTDX) do not connect to it; they own their own rigs' ports directly.

### 3a. Topology

- One `sm-serial-bridge` process
- Owns N physical rig ports (one worker per rig)
- Exposes **one Unix socket per rig**: e.g. `/run/smbridge/rig1.sock`, `/run/smbridge/rig2.sock`
- M concurrent endpoints per rig socket — whoever `connect()`s gets an endpoint; no fixed count. In practice M = 1 or 2 (logging app + CAT control app).

One-socket-per-rig means the rig binding is implicit in which socket you connect to. No rig identifier in the wire protocol.

### 3b. Wire format: NDJSON

Decided pre-v2 (see `docs/v1-analysis/design-decisions-log.md`). One bidirectional connection per client, each line a JSON object with a `type` field, no HTTP layer.

Shape (indicative — exact schema is open question §8.1):

- Client → bridge: `{"type":"set_mode","mode":"USB"}`, `{"type":"set_freq","hz":14250000}`, `{"type":"ptt","on":true}`, `{"type":"get_state"}`
- Bridge → client: `{"type":"state","freq":14250000,"mode":"USB","split":false,...}` (full snapshot on connect + after every delta), `{"type":"error","msg":"..."}`

### 3c. Per-rig pipeline

Correct layering (corrected 2026-04-20 during the session):

- `internal/serial` — owns the physical port, byte-level I/O, **no protocol knowledge**
- `internal/cat` — CAT protocol encoder/decoder (Yaesu / Icom / Kenwood), **pure logic, no I/O**
- Bridge — glue between them

**Upstream (client command → rig):**
1. NDJSON command arrives on endpoint: `{"type":"set_mode","mode":"USB"}`
2. Bridge calls `internal/cat` to encode → wire bytes for this rig's driver (e.g. `"MD2;"` for Yaesu/Kenwood, a CI-V frame for Icom)
3. Bridge writes those bytes to the port via `internal/serial`

**Downstream (rig → client):**
1. `internal/serial` yields bytes from the port
2. Bridge feeds them to `internal/cat` to parse into a structured update
3. Bridge broadcasts that update as NDJSON to all endpoints on this rig's socket

### 3d. Concurrency and framing

**Upstream serialisation:** one send queue per rig. Commands from all endpoints go through it. Because SM's own apps cooperate (one command per `write()` syscall, which the kernel guarantees atomic up to `PIPE_BUF`), the bridge does **not** need per-rig-protocol client-side framing logic. This was over-engineered in the earlier scaffold; the actual work is "read a line of NDJSON from an endpoint, pass the encoded bytes through the queue to the wire."

**Downstream broadcast:** parse once, fan-out to all endpoints on this rig's socket. No per-endpoint filtering — every endpoint sees every update.

### 3e. PTT safety

Single-owner-per-rig means no arbitration complexity — the OS enforces port exclusivity. But **stuck-PTT on client crash** is still a real risk. If the logging app crashes while holding PTT via a CAT command (e.g. `"TX;"` sent, no `"RX;"` follow-up), the rig stays in TX until the rig's own CAT watchdog kicks in (not all rigs have one) or the operator unkeys manually. Continuous key-down damages finals.

**Safety rule for the bridge:** track "did this endpoint assert PTT?" On disconnect of an endpoint that had PTT asserted, bridge sends PTT-release. One-line safety net.

### 3f. Rig driver family note

Yaesu, Kenwood, and Icom are the three families the bridge needs to support via `internal/cat`. Yaesu and Kenwood use the same protocol family: ASCII commands terminated by `;` (e.g. `"FA014250000;"`). Icom CI-V is binary (`0xFE 0xFE … 0xFD` frames). Earlier discussion incorrectly suggested Kenwood might be a "not supported" outlier; it's not — it's in the same family as Yaesu. All three are handled via `internal/cat`.

---

## 4. What this design collapses out

Compared to the 2026-04-14 two-frontend design:

- **No rigctld-compat TCP frontend.** WSJT-X and JTDX own their own rigs' ports directly; they never talk to the bridge. If a future third-party app ever *does* need to share a rig with the logging app, rigctld TCP can be added as a second frontend — but that's a "when it becomes real" problem.
- **No PTY / virtual serial ports.** Same reason — no third-party app needs serial-device shape from the bridge.
- **No hamlib vocabulary translation.** The bridge speaks SM's native CAT vocabulary end-to-end because all its endpoints are SM's own apps.
- **No complex PTT arbitration.** Simple convention within SM-land: only the logging app asserts PTT (for its optional voice keyer). The CAT control app is listener + state-setter, not a PTT consumer. One-line rule, not arbitration logic.
- **No command interleaving smarts.** SM's own apps cooperate on write boundaries.

---

## 5. Scope boundaries — CAT vs PTT vs audio

- **CAT** — addressed by this bridge (if built).
- **PTT** — often on a second serial/HID interface. Its own contention model (single-owner with safety-release on disconnect, §3e). Stuck-PTT-on-disconnect is the only hard safety rule.
- **Audio** — usually shareable via pipewire/pulseaudio on Linux; different OS story on Windows/macOS. **Not addressed by this bridge.** Different problem class from serial exclusivity.

Do not conflate the three.

---

## 6. The YAGNI question — should we build this bridge at all?

> **Resolved 2026-05-02 by ADR 0013.** Build the bridge — but as a daemon subsystem (`internal/bridge`), not a separate process. Default deployment is single-binary: `cmd/smd` imports the bridge package and registers it as a subsystem when `bridge.enabled: true` in config. The split-host shape (separately-built `cmd/bridge` running on the rig host) remains supported as an opt-in for topologies where the rig PC is not the daemon host. See ADR 0013 and `topology.md`. The "deferred-with-pluggable-transport-abstraction" path below was the leaning at session end of 2026-04-20; ADR 0013 resolved the question by collapsing the bridge into the daemon, so the transport-abstraction work is no longer needed for deferral reasons (it might still appear for split-host support).

Surfaced during the 2026-04-20 re-examination and **still open** as of session end.

**The case for building it now:**

- The shape is clear and small (§3 above fits in one page).
- A CAT control app is a "strong possibility" (user point 2 from the 2026-04-20 design conversation), and when built it'll want the bridge.
- Better to design it alongside `internal/cat`'s transport abstraction than bolt it on later.

**The case for deferring (YAGNI):**

- **Nothing today needs the bridge.** The logging app, the only SM rig-facing app that currently exists, can own its rig's port directly via `internal/cat` + `internal/serial`. Zero added latency, zero new code.
- The CAT control app is speculative. Until it's real, the bridge is infrastructure looking for a user.
- A deferred bridge costs nothing to future-you **if** `internal/cat` is designed from the start with a **pluggable transport abstraction**:
  - **"serial transport"** — `internal/cat` opens a real port via `internal/serial` directly. Used by the logging app today.
  - **"bridge socket transport"** — `internal/cat` connects to a Unix socket that speaks NDJSON. Used the day a second app exists that wants the same rig.
  - Apps choose transport via config. Flipping transport = config change, not a code rewrite.
- This keeps the logging app's CAT path as direct as possible (no IPC hop) until multiplexing is actually needed.

**User lean at session end:** leaning toward deferring, based on performance concern (§7) and "nothing currently needs this."

**Decision (2026-05-02, ADR 0013):** Build the bridge as a daemon subsystem (`internal/bridge`), not a separate process. Default single-binary; split-host opt-in. Resolves both the "nothing currently needs this" concern (the SPA needs `/v1/rig/events` SSE for live rig display) and the "should the rig PC be a separate host" concern (yes, optionally, via the split-host shape). See ADR 0013.

---

## 7. Performance — examined during the 2026-04-20 re-examination

User raised a real concern: v1 had UI lag with "no bridge, JSON in between." Would adding a Unix socket hop make it worse?

### 7a. Actual cost of the bridge hop

- JSON encode/decode: microseconds each
- Unix socket write → read: tens of microseconds on Linux
- Total added latency per event: **well under 1ms**

Unix sockets are what low-latency audio systems and game engines use for cross-process IPC. You'd need thousands of events per second to see accumulated delay in the single-digit ms range.

### 7b. What likely caused v1 UI lag

v1's JSON wasn't the bridge (there was no bridge) — it was **Wails backend ↔ Svelte frontend IPC**. Wails serialises Go↔JS calls over JSON via its message bridge; each call has ~ms-range overhead (10-100x a Unix socket hop). Likely v1 lag suspects in order:

1. Wails IPC call overhead (events emitted one-per-message, not batched)
2. DOM update thrashing on the Svelte side (re-renders per event, not coalesced to animation frame)
3. Serial port polling interval (if `internal/serial` polled instead of blocking-read)
4. JSON encoding a large state object when a small delta would do

**None of these are affected by whether a Unix socket bridge sits between `internal/serial` and `internal/cat`.** The Wails layer was the bottleneck; adding a bridge hop underneath it would add <1ms to a pipeline that was already spending 10-100x that elsewhere.

### 7c. Implication for the bridge design

If the bridge *is* built, these v1 lessons still apply to the **apps** that consume its output:

- Batch high-frequency rig updates at the frontend (e.g. coalesce to animation frame)
- Don't emit one Wails message per CAT event; buffer and ship periodically if appropriate
- Send deltas, not full state snapshots, after the initial connect-time snapshot

The bridge itself isn't the performance risk. The app that displays the bridge's output might be.

---

## 8. Open questions (narrowed)

### 8.1. Exact NDJSON message schema

`type` names, field names, field types. Indicative shapes in §3b but not finalised. Worth pinning down before coding — each rig driver will need to emit and consume this vocabulary consistently.

### 8.2. Snapshot-on-connect vs pure deltas

**Lean: snapshot-on-connect + deltas after.** Client needs to know the rig's current state immediately without waiting for the next change. Earlier open question §6c; keeping here until confirmed.

### 8.3. `internal/cat` transport abstraction shape

This is the load-bearing design call for the YAGNI path (§6). Approximate shape:

```go
// internal/cat
type Transport interface {
    Send(ctx context.Context, cmd Command) error
    Events() <-chan Event
    Close() error
}

// Serial transport: opens a real port via internal/serial.
// Used when the app owns the rig directly (logging app today).
func SerialTransport(cfg SerialConfig) (Transport, error) { ... }

// Socket transport: connects to a bridge Unix socket, speaks NDJSON.
// Used when the bridge is mediating (future CAT control app scenario).
func SocketTransport(cfg SocketConfig) (Transport, error) { ... }
```

App-level config picks which. The rest of `internal/cat` (command encoders, AUTO-mode parsers, rig-family dispatch) is transport-agnostic.

If we commit to this shape, the bridge itself can be deferred to milestone 3+ without any infrastructure pain. Logging app uses `SerialTransport` today; drop in `SocketTransport` the day the CAT control app exists.

### 8.4. Configuration shape

Per-rig: device path, baud, CAT driver (yaesu/icom/kenwood), socket path. Format: YAML or TOML (match whatever the daemon picks). Sub-questions:

- Hot-reload on config change? Probably not for v2.
- How does a client discover which rigs exist? For one-socket-per-rig, "check if the socket path exists" is the answer.

### 8.5. USB cable yank / reconnect behaviour

If a cable is yanked mid-session, what does the bridge do? Options:

- Fail-fast, let systemd/user restart. Simple, lossy.
- Retry loop, rig reappears on same path. Complex, preserves endpoint connections.
- Expose a status event on the NDJSON stream ("rig disconnected") so clients can react. Middle ground.

### 8.6. Repo placement

Memory hedged "undecided." Default per `docs/v2-design/structure.md` decision 3: pure Go binaries live in the root module. That points toward monorepo unless a reuse argument wins. No reuse argument yet — mark as "monorepo, root module" unless something changes.

### 8.7. Testing strategy

- **Stub CAT driver** — analogous to the stub forwarder. In-memory pipe pretends to be a rig. Good for bridge-internal logic. Essential.
- **`socat`-backed PTY pair** — if we need to test real serial I/O without real hardware. Nice-to-have.
- **Manual matrix against real rigs** — always happens at release acceptance time.

### 8.8. `internal/cat` carry-forward strategy

Use v1's `internal/cat` as-is in v2, refactor opportunistically, or reshape the API? The transport abstraction in §8.3 forces at least a minor API reshape. Characterisation tests first (per the "characterization tests before refactoring" lesson), then reshape.

---

## 9. Closed questions (from the earlier scaffold, now resolved)

| Question | Resolution | Source |
| -------- | ---------- | ------ |
| Third-party interop frontend (rigctld) | **Not needed.** WSJT-X/JTDX own their own rigs' ports directly; no overlap with SM-internal bridge scenarios. Can be added later if a specific app forces it. | 2026-04-20 re-examination |
| PTY virtual serial ports | **Rejected.** Same reason as rigctld; no third-party app needs it. | 2026-04-20 re-examination (consistent with 2026-04-14) |
| Two-frontend design (TCP + Unix socket) | **Collapsed to Unix-socket-only.** | 2026-04-20 re-examination |
| Hamlib vocabulary translation | **Not needed.** All bridge endpoints are SM's own apps. | 2026-04-20 re-examination |
| Complex PTT arbitration (lease model, rejection logic) | **Not needed.** Convention: only logging app asserts PTT. | 2026-04-20 re-examination |
| Command interleaving framing logic | **Not needed.** SM apps cooperate on write boundaries; kernel `PIPE_BUF` atomicity covers the rest. | 2026-04-20 re-examination |
| Kenwood as outlier | **Incorrect framing.** Kenwood is same family as Yaesu (ASCII + `;`). | 2026-04-20 re-examination |

---

## 10. Not yet addressed

- **Windows and macOS support.** Linux is primary. Unix socket transport is Linux/macOS-clean; Windows needs named pipes or AF_UNIX on modern Windows 10+. Revisit when cross-platform becomes real.
- **Observability.** Logs, metrics, "what is rig 1 currently doing" introspection. Probably a small read-only HTTP endpoint if the bridge runs.
- **Security.** Unix socket is local-user via filesystem perms; sufficient for single-user desktop. Revisit for headless/networked scenarios.
- **Relationship with `cmd/wsjtx-bridge` and `cmd/udp-bridge`** (the other two milestone-3 bridges). Near-zero shared code expected (different concerns: QSO ingest vs CAT mediation); confirm at implementation time.
- **Multi-bridge coordination.** Running two bridge instances for different rig groups. Probably trivial (ports don't overlap), but confirm.

---

## 11. Decision log

| Date | Question | Decision | Rationale |
| ---- | -------- | -------- | --------- |
| 2026-04-14 | CAT framing model | Push-state AUTO / transceive is the assumed mode | Modern rigs broadcast state; request/response framing was wrong starting point |
| 2026-04-14 | Multi-rig support | First-class from day one | Serious stations routinely run >1 rig |
| pre-v2 | SM-native frontend transport | NDJSON over Unix domain socket | Continuous bidirectional streaming, not HTTP+SSE rhythm; connection-lifetime = lease-lifetime for free |
| 2026-04-20 | Third-party interop frontend (rigctld TCP) | **Dropped.** Not currently needed | WSJT-X/JTDX own their own rigs' ports directly in the v2 architecture; no shared-rig scenario with them |
| 2026-04-20 | PTY virtual serial ports | **Dropped.** Not currently needed | Same reason as rigctld |
| 2026-04-20 | Design shape | **Unix-socket-only, SM-internal multiplexer** | Daemon already solves the QSO-logging-port-ownership coupling that motivated multiplexing |
| 2026-04-20 | Layering clarification | `internal/serial` for port I/O, `internal/cat` for protocol encoding/decoding, bridge as glue | User correction during re-examination |
| **OPEN** | **YAGNI — build the bridge now or defer?** | **Pending.** User lean: defer, with `internal/cat` transport abstraction enabling the deferred path at zero cost | See §6, §7, §8.3 |

---

## 12. Session pick-up point (next session)

**Where we left off:** the bridge design is now small and clear. Three live threads to resolve next session, in recommended order:

1. **Answer §6 — build now or defer?** Everything else depends on this.
2. **If deferred:** settle `internal/cat`'s transport abstraction shape (§8.3). This is the only design work the logging app depends on before milestone 2 can start.
3. **If built now:** sequence is (a) `internal/cat` transport abstraction, (b) NDJSON schema (§8.1), (c) bridge implementation, (d) logging app wired through `SocketTransport`, (e) defer CAT control app until its own design session.

My recommendation: **defer the bridge, but do §8.3 as a design-only exercise now.** That keeps the logging app on the fastest path (direct `SerialTransport`) without foreclosing the bridge. When the CAT control app becomes real, the switch is mechanical.
