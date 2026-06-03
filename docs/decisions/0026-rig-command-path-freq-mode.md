---
number: 0026
title: Inbound rig-command path — set-freq / set-mode via one semantic endpoint, confirmed by AUTO-mode push
status: Proposed
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
  body `{ "op": "...", "params": {...} }`. `op` is a fixed, rig-agnostic
  enum; the daemon maps it to the connected rig's named rigdef SET
  command and `Encode`s it. The vocabulary grows by adding an `op` value
  and a rigdef command entry — never a new route.
- **Initial vocabulary: `set_freq` and `set_mode`.**

  | op | params | FTdx10 rigdef cmd | resolution |
  |---|---|---|---|
  | `set_freq` | `{ "hz": <int> }` | `FA%09d;` | encode value verbatim (VFO-A) |
  | `set_mode` | `{ "mode": "<rig literal>" }` | `MD0%s;` | invert the **bijective** `MD0` `value_mappings` (literal → code) |

- **`set_mode` carries the rig mode literal, not an ADIF mode.** ADIF is
  the *logging* vocabulary and has no place in a CAT path. The rigdef's
  `MD0 value_mappings` (code ↔ literal, e.g. `C` ↔ `DATA-U`) is 1:1 and
  safe to invert; the daemon resolves `"DATA-U"` → `C` → `MD0C;`. The
  literal comes from operator config (below), so the SPA never hardcodes
  it.
- **Confirmation is the AUTO-mode push, not a command response.** Because
  the bridge already arms AUTO mode (`AI1;`), a successful SET makes the
  rig spontaneously push the resulting state, which flows through the
  *existing* `readLoop` → SSE → `catState` merge. The command path does
  not wait on or parse a reply. No write/read response synchronisation,
  no command-echo disambiguation.
- **Capability is advertised from the rigdef.** `GET /v1/config`'s
  `BridgeInfo` gains the set of `op`s the connected rig supports (derived
  from which named SET commands its rigdef defines). The SPA enables/hides
  controls from that set — a rig with no `set_mode` command never shows a
  mode control. An out-of-band request for an unsupported `op` returns the
  existing i18n envelope `{ code: "rig-unsupported-command", details:
  { op } }` (ADR 0010 shape) and writes nothing.
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
- **`bridge.SendCommand` is rig-ID-aware** per ADR 0019's multi-rig-ready
  API rule: `SendCommand(rigID, op, params)`, even though v1 passes one
  rig and the route stays singular (`/v1/rig/command`).

## Alternatives considered

### One HTTP route per operation (`/v1/rig/freq`, `/v1/rig/mode`, …)

The first sketch. Rejected: open-ended route sprawl — every future
operation (split, VFO select, RIT, power, antenna…) is a new endpoint.
The single-endpoint + semantic-op-enum shape keeps the HTTP surface flat
while preserving the same rig-agnostic semantics.

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

- **`bridge.SendCommand(rigID, op, params)`** plus an op→rigdef-command
  resolver inside `internal/bridge` (stays storage-free; ADR 0013 graph
  intact). For `set_mode`, the literal→code inversion of the rig's
  `MD0 value_mappings`.
- **New rigdef SET entries** in `internal/cat/rigs/*.json`: a frequency
  SET (`FA%09d;`) and a mode SET (`MD0%s;`) per rig, with the codec test
  extended to cover them. The FTdx10 is the dogfood rig and lands first;
  the FT-710 follows.
- **`POST /v1/rig/command`** handler in `internal/api`, registered only
  when the bridge is enabled (same gate as `/v1/rig/events`). Validates
  `op` + params, maps unsupported ops to the typed error.
- **`GET /v1/config` `BridgeInfo` grows a supported-ops list** derived
  from the rigdef. SPA hydrates it to gate controls.
- **Config grows `ft8.bands`** (per-band freq + mode; standard FT8 dial
  frequencies + `DATA-U` shipped as defaults).
- **SPA FT8 card** gains band buttons (compose `set_freq` + `set_mode`
  from `ft8.bands`) and freq nudge buttons (arithmetic over `set_freq`).

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
- **A combined "tune to FT8 band" atomic op is wanted** (one daemon call
  instead of the SPA firing `set_freq` + `set_mode`). Add a composite op
  if the two-call sequence proves racy or awkward in practice.
- **Multi-rig hardware lands.** `SendCommand` is already rig-ID-aware;
  the route grows to `/v1/rig/{id}/command` alongside the SSE route's
  same evolution (ADR 0019).
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
