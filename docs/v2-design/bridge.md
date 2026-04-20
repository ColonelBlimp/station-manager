# Station Manager v2 — Serial / CAT Bridge Design

**Status:** First draft, written 2026-04-20. Scaffold only — lifts the settled direction from the `project_sm_serial_bridge` memory entry (last updated 2026-04-14) into a versioned document, and collects open questions in one place for resolution.

**Everything in this document is subject to revision.** Even sections marked "settled" may change once we start building; memory captured a design direction, not an implementation. Where something changes, update this doc explicitly (don't silently drift), and record the new reasoning.

**Purpose:** Capture the *why* of the bridge's design choices so they survive session handoff. Once enough of the open questions are closed, this becomes the input to `cmd/sm-serial-bridge` construction in milestone 3.

**How this document relates to others:**

- `docs/v1-analysis/*` — what v1 had for serial/CAT (`internal/serial/`, `internal/cat/`, the "home-grown choices are deliberate" note in CLAUDE.md).
- `docs/v2-design/structure.md` — establishes that pure Go binaries (including bridges) stay in the root module; see decision 3.
- `docs/v2-design/milestones.md` — §Milestone 3 places `cmd/sm-serial-bridge` alongside the other external-integration binaries. Scope there is the one-liner; this doc is the detail.
- `docs/v2-design/api.md` — daemon API. The bridge does **not** call the daemon. Rig control is a client-side concern; the daemon only cares about QSOs. Kept separate on purpose.
- Memory `project_sm_serial_bridge` — the source this scaffold was lifted from. When this doc and memory disagree, this doc wins.

---

## 1. Problem statement

SM and WSJT-X/JTDX both want to drive the same radio over one USB port for CAT, PTT, and (separately) audio. USB serial devices are single-owner — only one process can hold the port — so SM's own serial/CAT libraries cannot coexist with hamlib-using apps on the same physical interface. Something has to own the port and mediate shared access.

**Multi-rig is first-class from day one.** Serious stations routinely run more than one rig (SO2R, contest setups, FT8 on rig A while the operator works phone/CW on rig B). Each rig is on its own USB port and is an **independent contention domain** — one rig's PTT lease, CAT stream, and AUTO/transceive state has nothing to do with another's. One bridge process manages N physical ports with per-rig state isolation.

**Non-scenario:** FT8 and phone/CW on the *same* rig at the *same* time. If both modes are active simultaneously at a station, they are on different rigs on different ports. The bridge does not have to arbitrate this.

---

## 2. CAT model assumption: push-state AUTO / transceive

Modern rigs commonly run in push-state modes:

- **Yaesu AUTO mode** — any change to the transceiver (VFO knob, mode change, PTT, band change, whether user-driven or CAT-driven from any source) is emitted down the wire automatically.
- **Icom CI-V transceive mode** — similar: the rig pushes transceive messages when state changes.

The bridge's design assumes this mode. The rig is a **state broadcaster**, clients are **observers that occasionally poke it**, and "client B receives a frequency update it didn't ask for" is a *feature* (B's local picture stays in sync with reality), not corruption.

**Do not re-derive the classic request/response framing for CAT.** That framing ("broadcasting replies to multiple clients corrupts state") is wrong for AUTO/transceive and was corrected during the 2026-04-14 design conversation. If a specific rig forces polling mode, that's a per-driver concern, not a framing problem for the whole bridge.

---

## 3. Two frontends, one internal pipeline

The bridge owns the physical port exclusively, consumes the rig's AUTO/transceive stream once, parses it once via the CAT library, and fans the parsed state out on **two separate frontends**.

### 3a. Frontend 1 — rigctld-compat TCP (third-party interop)

- Speaks hamlib's `rigctld` wire protocol on TCP. Default port 4532 for the first rig, +1 per additional rig (4533, 4534, ...), following hamlib convention.
- **WSJT-X and JTDX natively support "Hamlib Net rigctl" as a rig type** — zero code change on their side, they just point at the bridge instead of grabbing the serial port.
- Per-TCP-connection = per-client identity. Request/response routing for commands that *do* want a targeted reply is free.
- Line-oriented ASCII protocol. Short commands: `f`/`F` (get/set freq), `m`/`M` (mode + passband), `t`/`T` (PTT), `v`/`V` (VFO), `s`/`S` (split). Replies terminated by `RPRT <errcode>`. Optional extended key-value mode.
- **Effort estimate (directional only):** ~1 week of focused work for an MVP covering the 10–15 commands WSJT-X/JTDX actually use. Confirm against hamlib source (`tests/rigctl_parse.c`, `rigctld(1)`) before committing to a schedule.

### 3b. Frontend 2 — SM-native rig-state event stream (in-house clients)

- **Transport: NDJSON over Unix domain socket.** One bidirectional connection per client, each line a JSON object with a `type` field. No HTTP layer. Decided pre-v2 and recorded in `docs/v1-analysis/design-decisions-log.md` → "Serial/CAT bridge SM-native frontend: NDJSON over Unix socket" and `docs/v1-analysis/invariants.md` §"Serial/CAT bridge SM-native frontend."
- **Why not reuse the daemon's HTTP+SSE stack?** Rig bridge traffic is continuous bidirectional streaming (AUTO/transceive push from the rig, commands going back), not request/response-dominant. HTTP+SSE is the wrong rhythm. NDJSON gives one socket per client, **connection-lifetime = lease-lifetime for free** (PTT release and subscription cleanup on socket close fall out of the transport), is debuggable with `socat`, and implements in ~30 lines of Go.
- Delivers rig state in **the CAT library's native vocabulary**, not hamlib's. SM clients are not forced to inherit hamlib's mode names, VFO model, or its limitations.
- Carries a rig identifier so clients can subscribe per-rig or to all rigs.
- Commands from these clients go through the same internal serialization queue as commands from the rigctld frontend.

### 3c. Internal pipeline (shared by both frontends)

**Upstream (clients → rig):** commands from both frontends land in one serialization queue. The bridge buffers a full command frame from one source before letting the next source's bytes hit the wire — avoids mid-frame byte interleaving. Framing awareness is per-rig-protocol (Kenwood/Yaesu `;` terminators, Icom CI-V `0xFE 0xFE … 0xFD` frames, etc.); the CAT library already encodes this.

**Downstream (rig → clients):** the AUTO/transceive stream is parsed once and fanned out to every subscriber on both frontends, each in the form that frontend expects. The rigctld side translates to hamlib vocabulary; the SM-native side passes through CAT-library vocabulary.

**PTT arbitration — simple lease model.**

- Bridge grants PTT to the first requester, auto-releases on explicit `T 0` / release, or on client disconnect.
- Second requester gets a clean rejection until released.
- In practice contention is rare. WSJT-X/JTDX is the heavy PTT user, holds PTT during FT8 TX slots (~12.6 s). SM's logging app is **not normally a PTT consumer** — it does not claim PTT at startup.
- Voice keyer is the only SM-side PTT consumer, and it's optional/ergonomic (plays a recorded CQ clip to save the operator's voice). Path: request lease → key → play clip → unkey → release, total hold time seconds. If an operator triggers the voice keyer mid-WSJT-X-TX, clean rejection — that's a user mistake, not something the bridge has to handle elegantly.

**Stuck-PTT safety — hard requirement, not a design choice.** On *any* client disconnect (TCP drop, Unix socket close, client process death), the bridge **must** force PTT to RX immediately. Stuck key-down damages finals and puts the station in continuous-transmission territory regulatorily. Only the bridge can guarantee this because clients can't clean up after their own crashes. Implement as a connection-close hook on both frontends.

---

## 4. Explicitly rejected approach: PTY virtual serial ports

Original framing was "expose N virtual USB ports per physical port and broadcast device output to each." On Linux this is buildable via `openpty()` / `tty0tty` / `socat`. Rejected because:

- WSJT-X and JTDX (the two apps driving the whole redesign) already speak rigctl-net natively, so the TCP frontend solves the interop problem with zero config on their side.
- PTY would force the bridge to parse *client-side* framing for every supported rig protocol, not just rig-side — duplicating work.
- Windows has no clean PTY equivalent (com0com is a separate kernel driver, different story).
- SM's own in-house clients are better served by a native event stream over Unix socket than by pretending to be serial devices.

Revisit only if a specific third-party app refuses to speak rigctl-net and the user actually needs it. Narrower, later problem.

---

## 5. Scope boundaries — CAT vs PTT vs audio

- **CAT** — solved by this bridge.
- **PTT** — often on a second serial/HID interface. Its own contention model (lease + auto-release on disconnect, §3c). Stuck-PTT-on-disconnect is a hard safety rule.
- **Audio** — usually shareable via pipewire/pulseaudio on Linux; different OS story on Windows/macOS. **Not addressed by this bridge.** Different problem class from serial exclusivity.

Do not conflate the three. The bridge is a CAT mediator with a PTT arbitration rider, nothing more.

---

## 6. Open questions

These need decisions before implementation starts. Some may already have answers in the user's head; writing them down surfaces the ones that don't.

### 6a. Repo placement — monorepo or separate repo?

The bridge is in principle reusable by other ham radio projects (any station running WSJT-X/JTDX alongside another logger faces the same USB-exclusivity problem). If that reuse is a goal, a separate repo lowers the barrier for others to pick it up. If not, monorepo is simpler.

**Defaults to lean on:** `docs/v2-design/structure.md` decision 3 says pure Go binaries live in the root module. That points toward monorepo unless a reuse argument wins.

### 6b. Relationship with v1 `internal/serial` and `internal/cat`

v1 has home-grown serial and CAT packages (CLAUDE.md → "home-grown choices are deliberate"). Options:

- **Carry forward as-is** into the shared `internal/` tree; the bridge imports them.
- **Carry forward, refactor opportunistically** if the APIs want reshaping for bridge use.
- **Rewrite** — unlikely to be worth it; the libraries were judged solid in v1 analysis.

The cautionary tale from lessons-for-v2 is "characterization tests before refactoring." If we refactor, freeze behavior first.

### 6c. SM-native event stream shape — snapshot-then-deltas, or pure deltas?

**Transport is decided (NDJSON over Unix socket, §3b).** The open question is the *event shape*: rig state is stateful (a current frequency, a current mode) where the daemon's event stream is incremental (a QSO was stored, a forward succeeded). Options:

- **Snapshot-on-subscribe then deltas.** First line on a new connection is a full current-state object per rig; subsequent lines are deltas. Client doesn't need to wait for the next state change to know what the rig is doing.
- **Pure deltas.** Client subscribes, waits for the next state change, is blind until then. Matches the daemon's SSE shape but not its use case.
- **Snapshot available on demand.** A `{"type":"get_state"}` command returns the current state as a single NDJSON line; stream is pure deltas otherwise.

Leaning toward (a) — "what is the rig doing right now" is the first thing any client wants to know and making them wait for a state change is a bad UX.

### 6d. rigctld MVP command set

Memory estimates "10–15 commands WSJT-X/JTDX actually use" but doesn't enumerate them. Before committing to the week-of-work estimate we need the list — either by reading WSJT-X/JTDX source, or by running them against real `rigctld` with tracing and observing what crosses the wire.

### 6e. Configuration — how does the bridge discover rigs?

A static config file (YAML/TOML) is the obvious default, listing per-rig: device path, baud rate, CAT driver, rigctld TCP port, SM-native stream path. Open sub-questions:

- Is hot-reload on config change worth the complexity? (Probably not for v2.)
- How does a client know which rigs exist? (Probably a small HTTP endpoint on the bridge, or an initial snapshot over the SM-native stream.)

### 6f. PTT serial interface discovery

On many rigs PTT is on a second serial/HID interface (often RTS/DTR on a different `/dev/ttyUSB*`). Config needs to express this. Decide the shape together with 6e so we don't have two config models.

### 6g. Per-driver CAT adapters

v1 supported multiple rig families. The v2 bridge needs the same. Design question: where does the driver-selection boundary live — in the CAT library (bridge just calls `cat.Open(driver, port)`) or in the bridge (bridge holds a driver registry analogous to `internal/forwarding/Register`)?

Forwarding's pattern is a good reference point — it worked out well and is worth copying if the shapes are similar.

### 6h. Bridge process lifecycle and supervision

- Run as a standalone binary (`cmd/sm-serial-bridge`) — decided, matches the structure doc.
- systemd unit vs. user runs it manually — probably systemd for dogfood, but not a v2 blocker.
- How does the bridge behave if a USB port goes away mid-session (cable yanked)? Auto-reconnect? Fail-fast and let systemd restart? Open.

### 6i. Testing strategy

Integration tests against real hardware are not reproducible. Options:

- A `stub` CAT driver (analogous to the stub forwarder) that simulates a rig over an in-memory pipe. Good for bridge-internal logic.
- `socat`-backed pty pair for end-to-end tests of the rigctld frontend — real TCP, real bytes, no real rig.
- Manual test matrix against the user's actual rigs for release acceptance.

Decide which are worth building; (a) is probably essential, (b) nice-to-have, (c) always happens.

### 6j. Relationship to milestone 3's other bridges

Milestone 3 also lists `cmd/udp-bridge` and `cmd/importer`. Those are daemon-adjacent (they feed QSOs in); the serial bridge is rig-adjacent (it mediates CAT). Almost no shared code between them. Confirm that's actually true before designing — if `udp-bridge` and `sm-serial-bridge` both want a common "small Unix-socket HTTP client + event subscriber" helper, extract it once.

---

## 7. Not yet addressed

Things that are conspicuously absent from this scaffold and will need their own sections when the time comes:

- **Windows and macOS support.** Linux is primary; other platforms are "works someday" not "works now." Port enumeration, PTY story, audio story all differ.
- **Multi-bridge coordination.** If the operator has *more* rigs than one bridge instance is willing to manage, does a second bridge instance coexist on different ports? (Probably yes, trivially, since ports don't overlap.)
- **Observability.** Logs, metrics, a way to see "what is rig 1 currently doing" without attaching a debugger. Probably a small read-only HTTP endpoint.
- **Security.** The Unix socket is local-user by filesystem perms; the rigctld TCP port is typically localhost-only. Revisit if the user wants to run the bridge on a headless station accessed from a laptop on the LAN.

---

## Decision log (to be filled in as open questions close)

| Date | Question | Decision | Rationale |
| ---- | -------- | -------- | --------- |
| 2026-04-14 | CAT framing model | Push-state AUTO/transceive is the assumed mode | Original "request/response" framing was wrong for modern rigs; corrected during design conversation |
| 2026-04-14 | PTY virtual serial ports | Rejected | WSJT-X/JTDX already speak rigctl-net; PTY adds client-side framing work for no gain |
| 2026-04-14 | PTT arbitration | Simple lease + auto-release on disconnect | Contention is rare in practice; stuck-PTT-on-disconnect is the only hard rule |
| 2026-04-14 | Multi-rig support | First-class from day one | Serious stations routinely run >1 rig; per-rig state isolation required |
| pre-v2 | SM-native frontend transport | NDJSON over Unix domain socket (not HTTP+SSE) | Rig bridge traffic is continuous+bidirectional; connection-lifetime = lease-lifetime falls out for free; see `docs/v1-analysis/design-decisions-log.md` and `invariants.md` |
