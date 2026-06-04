---
number: 0026
title: Inbound rig-command path — set-freq / set-mode via one semantic endpoint, confirmed by AUTO-mode push
status: Accepted
date: 2026-06-03
---

# 0026 — Inbound rig-command path (set-freq / set-mode)

## Context

The bridge is read-only by ADR 0019: the rig pushes state via AUTO-mode
CAT, the bridge filters and forwards over SSE, the SPA displays. ADR 0019
deferred the inbound command path explicitly and named the trigger that
would reopen it — *"The SPA needs to drive the rig … opens the inbound
command path: bridge package gets command-input methods, daemon gets an
HTTP endpoint."*

That trigger has fired. The live FT8 path (ADR 0024) wants the operator
to pick a band in the (future) FT8 card and have the rig jump straight to
that band's FT8 dial frequency **and** drop into the data mode the codec
needs — without reaching for the rig's front panel. That is two rig
writes: set the VFO frequency, set the mode. A natural second use case
(phone/CW band-hopping, frequency nudge buttons) rides on the same
primitive.

The infrastructure for writes already exists and is unused for commands:
`internal/serial` is bidirectional (`WriteCommandBytes`, concurrent-safe),
`internal/cat`'s codec already does parameterised `Encode(def, name,
args…)`, and `bridge.Service` already holds the live `activeClient` and
already issues exactly one write today (the READ bootstrap). What is
missing is a command vocabulary, a way to reach it over HTTP, and the
rigdef SET entries to encode.

The load-bearing constraints: the write path must stay **inside
`internal/bridge`** (ADR 0013/0024 import-graph invariant — no
storage/forward coupling); the SPA must stay rig-agnostic in *code* (the
read side already keeps rig literals in rigdefs/config, not in SPA
source); and the API footprint must not grow one route per operation.

## Decision

Add an inbound command path as a **partial** activation of ADR 0019's
deferred trigger — frequency and mode writes only; **PTT, PTT
arbitration, and disconnect-safety-release stay deferred** per ADR 0019
until a real PTT driver appears.

Shape:

- **One endpoint, semantic op vocabulary.** `POST /v1/rig/command` with
  body `{ "op": "...", "value": <scalar> }`. `op` is a rig-agnostic semantic
  name that **is the rigdef command name** — the daemon resolves it with no
  translation layer (`cat.EncodeCommand(def, op, value)`), so the vocabulary
  is standardised across rigdefs (every rig that can set its VFO names that
  command `set_freq`). `value` is a single JSON scalar (a number for
  `set_freq`, a string for `set_mode`); the handler renders it to a string
  with one generic conversion, so it stays op-agnostic. The vocabulary grows
  by adding a rigdef command entry — never a new route, and never new Go.
- **The body also accepts an atomic batch:** `{ commands: [ {op, value}, … ] }`
  is encoded as one CAT line (`FA…;MD0…;`, the same multi-command wire shape
  READ uses) in a single serial write. The write is atomic — nothing
  interleaves between its commands — and the rig pushes one confirmation per
  command. All-or-nothing: if any element fails to encode, the whole batch is
  rejected and nothing is written. This is how the FT8 card tunes to a band in
  one call, and it is the answer to the composite-op question below (no new op
  vocabulary needed). Validated live on the FTdx10 via the single ops; the
  multi-SET line itself is the one-curl check when the card lands.
- **Initial vocabulary: `set_freq` and `set_mode`.**

  | op | value | FTdx10 rigdef cmd | data fields | resolution |
  |---|---|---|---|---|
  | `set_freq` | `14074000` (number) | `FA%s;` | `pad: 9` | left-zero-pad Hz to the 9-digit VFO-A field |
  | `set_mode` | `"DATA-U"` (string) | `MD0%s;` | `value_map: "MAINMODE"` | invert the **bijective** `MAINMODE` `value_mappings` (literal → code) |

  Both also carry `exposed: true` (see capability gating below).

- **`set_mode` carries the rig mode literal, not an ADIF mode.** ADIF is
  the *logging* vocabulary and has no place in a CAT path. The rigdef's
  `MAINMODE` `value_mappings` (the `MD0` state's table; code ↔ literal,
  e.g. `C` ↔ `DATA-U`) is 1:1 and safe to invert; the daemon resolves
  `"DATA-U"` → `C` → `MD0C;` via the command's `value_map: "MAINMODE"`.
  The literal comes from operator config (below), so the SPA never
  hardcodes it.
- **Confirmation is the AUTO-mode push, not a command response.** Because
  the bridge already arms AUTO mode (`AI1;`), a successful SET makes the
  rig spontaneously push the resulting state, which flows through the
  *existing* `readLoop` → SSE → `catState` merge. The command path does
  not wait on or parse a reply. No write/read response synchronisation,
  no command-echo disambiguation. **Validated live on the FTdx10
  (2026-06-04), both exposed ops: `set_freq` (VFO jumped, `FA` pushed
  back) and `set_mode` (`DATA-U`→`MD0C;`, rig switched, `MD0` pushed back)
  each returned 202 with the new state arriving over `/v1/rig/events` and
  no extra read — the core assumption holds, and the `value_map`
  inversion is hardware-proven.**
- **Capability is advertised from the rigdef, gated by one flag.** A
  command opts into the external path with `exposed: true`; that single
  flag is the source of truth for **both** the endpoint gate
  (`EncodeCommand` refuses an unexposed command) **and** the advertised
  vocabulary (`GET /v1/config`'s `BridgeInfo` lists the exposed command
  names, derived by `cat.ExposedCommands`). The SPA enables/hides controls
  from that set — a rig with no exposed `set_mode` never shows a mode
  control. Adding an exposed command to a rigdef makes it both reachable
  and advertised with no Go change. An out-of-band request for an
  unsupported or unexposed `op` returns the standard HTTP error envelope
  (`{code, message, op}`, api.md §4.6) with `code: "rig_unsupported_command"`
  and writes nothing. (This is the HTTP error shape — distinct from the
  SSE `{code, details}` event envelope; the command path is request/response,
  not an event stream.)
- **No silent no-op for an unsupported op.** A rig that lacks an op
  surfaces that honestly — the SPA hides the control (absent from the
  advertised set) and the daemon rejects an out-of-band call with the typed
  envelope above. A command-layer no-op that returned success is rejected
  on two counts: it contradicts the typed-rejection rule, and it breaks
  confirm-by-push (the SPA would await a state push that never arrives). A
  future "minimum op set" is therefore a *per-SPA-surface* required-ops
  check (the FT8 card needs `{set_freq, set_mode}`; if either is missing
  that surface degrades with a reason), never a global mandate that would
  force a rig to fake ops.
- **The FT8 band plan lives in operator config**, per-band frequency and
  mode:

  ```json
  "ft8": {
    "bands": [
      { "band": "20m", "freq": 14074000, "mode": "DATA-U" },
      { "band": "40m", "freq":  7074000, "mode": "DATA-U" },
      { "band": "30m", "freq": 10136000, "mode": "DATA-U" }
    ]
  }
  ```

  The FT8 card looks up the selected band's `{freq, mode}` and composes
  `set_freq` + `set_mode`. Frequency nudge (up/down) is **client-side
  arithmetic** over `set_freq` (the SPA has the live VFO frequency from
  SSE), not a backend op.
- **`bridge.SendCommand` is rig-agnostic**, like the read path. It targets
  the daemon's one connected rig (`cfg.Cat.Driver`), so the signature is
  `SendCommand(ctx, op, value)` — no rig identifier. The SPA carries no rig
  identity in the command path either; rig identity is **display-only**
  (`BridgeInfo.RigName`/`RigModes`). This matches how the bridge already
  models multi-rig (one `Service` per rig — `service.go`'s timeout-field
  comment) and the existing write method `TriggerBootstrap(ctx)`, which
  writes to the connected rig with no rig parameter.

## Alternatives considered

### One HTTP route per operation (`/v1/rig/freq`, `/v1/rig/mode`, …)

The first sketch. Rejected: open-ended route sprawl — every future
operation (split, VFO select, RIT, power, antenna…) is a new endpoint.
The single-endpoint + semantic-op-enum shape keeps the HTTP surface flat
while preserving the same rig-agnostic semantics.

### Typed per-op params in the request body (`{ op, params: { hz } }`)

The first body sketch carried op-specific params — `{ hz }` for `set_freq`,
`{ mode }` for `set_mode`. Rejected: turning a typed params object into the
single string `EncodeCommand` wants needs a per-op switch in the handler
(which key, how to stringify), reintroducing exactly the per-op sprawl the
data-driven `cat` layer removed — one layer up, growing with every op. The
body is instead a uniform `{ op, value }` with value a JSON scalar; one
generic conversion (number → decimal string, string → itself) feeds
`EncodeCommand`, so a new op is config-only on the wire too. The lost
benefit — a JSON-typed, self-documenting `hz` — is minor, and `value` as a
JSON number still carries frequency naturally.

### Generic rig-literal passthrough (`{ name: "MD0", args: ["C"] }`)

Thinnest daemon code: the SPA names the rigdef command directly.
Rejected: it leaks rig-specific CAT vocabulary toward the client and
breaks the read-side property that the SPA deals in abstractions while
literals live in rigdefs/config. A stale SPA built against one rig's
command names would silently misfire against another.

### `set_mode` takes an ADIF mode, inverting the read `mode_mappings`

The original plan, until the FTdx10 rigdef disproved it. `mode_mappings`
(rig → ADIF) is **many-to-one** in exactly the place that matters: both
`DATA-L` and `DATA-U` map to ADIF `FT8`; `CW-U`/`CW-L` both map to `CW`;
four FM variants collapse to `FM`. It is not invertible — "ADIF FT8 →
rig literal" is ambiguous (FT8 could resolve to the wrong sideband).
Rejected in favour of carrying the rig literal and inverting the *other*,
bijective table (`MD0 value_mappings`), which has no ambiguity.

### Native band-select (`BS%02d;`) for the FT8 band buttons

`BS` jumps to a band and restores the rig's band-stacking memory (last
freq + mode for that band). Rejected for the FT8 use case: FT8 dial
frequencies are fixed, published, absolute points — an absolute
`set_freq` reaches them exactly, and band-stacking recall (a *different*
freq each time) is the opposite of what FT8 wants. `BS` earns its place
only for phone/CW band-hopping, where per-band freq+mode recall is the
feature — deferred (see Triggers).

### Native rig step (`UP;`/`DN;`) for frequency nudge

Rejected: the rig steps by whatever its current tuning increment is —
unpredictable, and the SPA can't show what a press will do. Client-side
arithmetic over `set_freq` (current ± delta, absolute) is deterministic
and self-corrects on the next push; it adds no vocabulary.

### Request/response command confirmation

Wait for the rig to echo/ack each SET before reporting success.
Rejected: AUTO mode already pushes the resulting state, so the
confirmation arrives for free over the existing read path. Waiting on a
reply would add per-command echo-vs-state-push disambiguation and a
write/read synchronisation problem that confirm-by-push avoids entirely.

### Full PTT path now (per ADR 0019's deferred scope)

Rejected — unchanged from ADR 0019. `set_freq`/`set_mode` need none of
PTT tracking, arbitration, or disconnect-safety-release; a stuck SET
does not transmit. Building the PTT machinery before a TX driver (FT8 TX
cycle, voice keyer) exists is the same speculative trap 0019 already
declined. This ADR activates only the receive-side-safe half of 0019's
trigger.

## Consequences

**Signed up for:**

- **`bridge.SendCommand(ctx, op, value)`** inside `internal/bridge`
  (stays storage-free; ADR 0013 graph intact); rig-agnostic — it targets
  the Service's one connected rig and needs no rig identifier. It resolves
  its own `def` from `cfg.Cat.Driver` and delegates encoding to
  `cat.EncodeCommand(def, op, value)`; there is **no** op→command resolver
  — the op *is* the command name. The literal→code inversion for `set_mode`
  is data-driven (the command's `value_map`), not a per-op Go helper.
- **Data-driven command fields on `cat.Command`** — `value_map` (names a
  marker whose bijective `value_mappings` invert the rig literal), `pad`
  (left-zero-pads a numeric field to a fixed width), and `exposed` (opts a
  command into the external path; default deny). Adding a command is then
  config-only; the Go surface is fixed at `HasCommand` + `EncodeCommand`.
- **New rigdef command entries** in `internal/cat/rigs/*.json`: `set_freq`
  (`FA%s;`, `pad: 9`) and `set_mode` (`MD0%s;`, `value_map: "MAINMODE"`),
  both `exposed: true`, with the codec tests extended to cover them and the
  not-exposed safety boundary (PLAYBACK/INIT/READ stay unreachable). The
  FTdx10 is the dogfood rig and lands first; the FT-710 follows.
- **`POST /v1/rig/command`** handler in `internal/api`, registered only
  when the bridge is enabled (same gate as `/v1/rig/events`). Decodes
  `{op, value}`, renders value to a string with one generic scalar
  conversion, calls `bridge.SendCommand`, and maps the cat sentinels +
  `ErrRigNotConnected` to the HTTP error envelope (`{code, message, op}`,
  snake_case codes `rig_unsupported_command` / `rig_invalid_value` /
  `rig_not_connected`). Success is `202 Accepted` — the state change is
  confirmed out-of-band by the AUTO-mode push, not in this response.
- **`GET /v1/config` `BridgeInfo` grows a supported-ops list** derived
  from the rigdef. SPA hydrates it to gate controls.
- **Config grows `ft8.bands`** (per-band freq + mode; standard FT8 dial
  frequencies + `DATA-U` shipped as defaults).
- **SPA FT8 card** gains band buttons (one `{commands:[set_freq, set_mode]}`
  batch from `ft8.bands` — atomic tune) and freq nudge buttons (arithmetic
  over `set_freq`).

**Accepted costs:**

- **Command writes interleave with the read loop on one serial port.**
  Mitigated: `serial.Client` writes are concurrent-safe, and
  confirm-by-push means the command path never competes for *read*
  responses — it writes and returns; the resulting state push is handled
  by the existing reader. The only ordering guarantee needed is that the
  bytes of one command aren't interleaved with another's, which the
  serial layer's write mutex already provides.
- **A SET fired at a wedged/disconnected rig produces no state change.**
  Fail-soft: a serial write error surfaces as a typed error to the SPA;
  the absence of a confirming push means the SPA simply shows no change.
  No new failure mode is introduced (no PTT to stick).
- **`ft8.bands` is operator-authored rig knowledge** (the `DATA-U`
  literal is FTdx10-specific). Consistent with `bridge.mode_mappings`
  already being operator-authored; defaults ship working for the tested
  Yaesu rigs.
- **`set_freq` targets VFO-A only in this cut.** A `vfo` param +
  `FB%09d;` SET is a trivial later addition; not built now (no consumer —
  split is deferred). YAGNI per the project's "build specific" lesson.

**Gained:**

- Flat API footprint (one route; vocabulary grows by value).
- SPA stays rig-agnostic in code; rig literals stay in rigdefs/config.
- The FT8 card gets tune-to-band in one click; `set_freq`/`set_mode` are
  reusable primitives for the future logging-SPA CAT controls.
- No new read-path complexity — confirm-by-push reuses the entire
  existing `readLoop` → SSE → `catState` chain.

## Triggers to revisit

- **PTT becomes real** (FT8 TX cycle, voice keyer, remote station). Then
  the deferred half of ADR 0019's trigger activates whole: per-connection
  PTT-asserted tracking, arbitration against real contention, and
  disconnect-safety-release. Built together when a TX driver exists, not
  piecemeal.
- **Phone/CW band-hopping is wanted.** Add a `set_band` op backed by
  native `BS%02d;` with a per-rig band→code table, so the rig restores
  band-stacking freq+mode recall — the case `set_freq` deliberately
  cannot serve.
- **The logging SPA wants VFO select / split.** Add `set_vfo` / `set_split`
  ops; both map to already-present bijective value tables (`VS`, `ST`)
  and need only rigdef SET entries.
- **A combined "tune to FT8 band" atomic op — RESOLVED in v1 by command
  batching.** `{commands:[set_freq, set_mode]}` is written as one
  `FA…;MD0…;` line (a single, atomic serial write); no bespoke composite op
  was needed — it is just the two existing ops on one wire.
- **Two rigs on one daemon (SO2R).** Only this — not multi-rig in general
  — needs per-rig addressing. In the field-master topology multi-rig means
  multiple *daemons*, each with a co-located rig and its own singular
  `/v1/rig/command`; the SPA stays rig-agnostic, talking to its daemon. A
  genuine single-daemon-two-radio setup is the trigger to run multiple
  bridge `Service`s and grow the route to `/v1/rig/{id}/command` — and only
  then does a rig selector appear in the SPA. Not a current goal.
- **A non-AUTO-mode rig is encountered.** Confirm-by-push breaks (the rig
  won't volunteer the post-SET state); the command path would need an
  explicit re-read after each write. Overlaps with ADR 0019's
  non-AUTO-mode trigger.
- **A rig can't express an op as a single templated command** (e.g. split
  set via `FT`/`FR` rather than one prefix, or a SET prefix that differs
  from its READ prefix). The rigdef SET entry absorbs per-rig divergence;
  revisit the resolver if a rig needs multi-command sequences per op.

## References

- ADR 0019 — read-only bridge v1; **this ADR fires 0019's first
  "trigger to revisit" (inbound command path), partially — freq/mode
  only, PTT still deferred.** 0019 stays Accepted; its read-only decision
  was correct for its time.
- ADR 0013 — narrow-daemon-scope / import-graph discipline the write path
  preserves (command path stays inside `internal/bridge`).
- ADR 0010 — rig SSE wire shape; the `{code, details}` error envelope the
  unsupported-op error reuses; the AUTO-mode push this ADR confirms by.
- ADR 0024 — live FT8 pipeline; the FT8 card driving `set_freq`/`set_mode`
  from `ft8.bands` is the motivating consumer.
- `internal/cat/rigs/yaesu-ftdx10.json` — the dogfood rigdef; its `MD0`
  `value_mappings` (bijective) is what `set_mode` inverts, and its
  `mode_mappings` (many-to-one) is the table this ADR deliberately does
  **not** invert.
- `docs/v2-design/bridge.md` — bridge architecture; §4's
  "command interleaving framing" note is the serial-write concern this
  ADR's confirm-by-push model addresses.
- Memory `project_sm_serial_bridge`.
