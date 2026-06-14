---
number: 0034
title: Protocol seam in the CAT codec for Icom CI-V
status: Proposed
date: 2026-06-14
---

# 0034 — Protocol seam in the CAT codec for Icom CI-V

## Context

The `cat` codec (`internal/cat`) was built around one protocol family:
Kenwood-style ASCII CAT. Commands are `;`-terminated text with a two-letter
prefix and ASCII-digit fields (`FA00014074000;`, `MD2;`); `cat.Decode` matches
a **leading text prefix** then cuts fields by byte index/length from the tail,
optionally string→string mapped; `cat.Encode`/`EncodeCommand` fill a `%s`
template with `value_map` (literal→wire code) + `pad` (left-zero-pad ASCII
digits). The whole vocabulary lives in the rigdef JSON, so adding a Yaesu
FT-710 alongside the FTdx10 is pure data, no code (the "no code to add a rigdef"
rule, ADR 0028 / config.md §10).

A borrowed **Icom IC-7300** (time-boxed) is the first non-Yaesu rig and the
first **Icom CI-V** rig. CI-V is a different protocol: binary frames
`FE FE <to> <from> <cmd> [<subcmd>] [<data>] FD`, addressed on a shared bus
(the rig echoes the controller's own sends), with **frequency as little-endian
BCD** and mode as a byte code. Three of the ASCII codec's core assumptions do
not survive:

1. **The discriminator is not at the head.** Every frame starts
   `FE FE <to> <from>`; the command byte that identifies the frame sits at
   offset 4, not offset 0, so the leading-prefix match cannot find it.
2. **BCD data.** A frequency is 5 bytes of little-endian BCD — markers that
   slice bytes and string-map them cannot assemble it, and `value_map`/`pad`
   are ASCII-only.
3. **Echo on the shared bus.** CI-V echoes our own sent frames back, so the
   read loop must drop frames that did not originate at the rig.

The transport is **not** in tension: `internal/serial` frames reads by
splitting on a single delimiter byte and auto-appends it on write, so
`terminator: "ý"` (0xFD) gives CI-V framing for free — the `FE FE`
preamble is just frame data. The pressure is entirely in the codec.

A protocol survey confirms the shape of the problem. Modern amateur HF/VHF CAT
is essentially **two families**: Kenwood-style ASCII (Kenwood — the origin —
plus Yaesu, Elecraft, FlexRadio, and most others) and Icom CI-V. Apache Labs
ANAN SDRs present a Kenwood (TS-2000) CAT emulation via their host software, so
they too are the ASCII family — what an ANAN raises is a *transport* question
(openHPSDR/UDP, or a virtual-serial/TCP CAT seam), not a third codec.

## Decision

Add a **`protocol` discriminator** to `RigDefinition` selecting a per-family
codec behind a small internal seam, and extend the codec's data vocabulary with
a fixed set of **encoding kinds** so a CI-V rig is described entirely in its
rigdef:

- `protocol: "kenwood"` — the default; the existing ASCII path, unchanged.
  (Named for the family, not `yaesu_ascii`: the FTdx10's commands *are* Kenwood
  commands.)
- `protocol: "icom_civ"` — a new pure-Go CI-V engine: `FE FE…FD` framing, BCD
  pack/unpack, address/echo handling. The engine knows the *mechanics*; it reads
  *which* command byte and *which* data encoding per op from the rigdef.

The IC-7300 brings the `icom_civ` engine with it (a one-time protocol addition —
BCD and `FE FE…FD` cannot be JSON). **Every Icom thereafter (IC-705, IC-9700)
is a rigdef and nothing else**, and a Kenwood/Elecraft/Flex rig is a rigdef on
the existing path — the "no code to add a rigdef" rule holds for every rig after
the first of each family.

### Schema additions (data-driven, protocol-generic)

**`RigDefinition`** gains:

```jsonc
"protocol":    "icom_civ",   // "kenwood" (default) | "icom_civ"
"civ_address": "94",         // hex; the rig's CI-V bus address (icom_civ only)
```

The controller address (conventionally `0xE0`) is an engine constant for now
(a `civ_controller_address` rigdef field is the escape hatch if a rig ever needs
a different one — not added speculatively).

**`Command`** gains an `encoding` field naming a kind from a fixed vocabulary.
`encoding` defaults to `ascii`, so every existing Kenwood command is unchanged.
For `icom_civ`, `cmd` holds the **hex command bytes** (command + optional
subcommand) rather than an ASCII template, and `encoding` says how the value
becomes the data payload:

| `encoding`  | meaning                                                        | used by            |
|-------------|----------------------------------------------------------------|--------------------|
| `ascii`     | today's behaviour — `%s` template + `value_map` + `pad`        | Kenwood (default)  |
| `none`      | no data payload (valueless command)                            | read_freq, read_mode |
| `bcd_freq`  | decimal Hz → 5-byte little-endian BCD                          | set_freq           |
| `mode_byte` | `value_map` literal → 1 mode byte (+ optional filter byte)     | set_mode           |
| `bcd_power` | percent/level → BCD level field                                | set_power          |

```jsonc
// IC-7300 commands (illustrative; exact bytes pinned when built)
"commands": [
  {"name": "READ",     "cmd": "03",   "encoding": "none"},          // read VFO freq
  {"name": "set_freq", "cmd": "05",   "encoding": "bcd_freq", "exposed": true},
  {"name": "set_mode", "cmd": "06",   "encoding": "mode_byte", "value_map": "MODE", "exposed": true},
  {"name": "tx_on",    "cmd": "1C00", "encoding": "none"},          // 1C 00 01 — never exposed
  {"name": "tx_off",   "cmd": "1C00", "encoding": "none"}
]
```

**`State`/`Marker`** decode gains: a state matches on the **command byte** at the
post-address offset (after `FE FE <to> <from>`) instead of a leading prefix, and
a marker carries a **decode kind** (`bcd_freq`, `byte` + `value_mappings`, …)
instead of a raw slice. The Kenwood marker (raw slice / index+length) stays the
default kind.

### Read-loop echo + address filtering (`icom_civ`)

The CI-V read path keeps a frame **iff `from == civ_address`** — that drops our
own echoes (`from == controller`) and accepts both transponded replies
(`to == controller`) and unsolicited transceive broadcasts (`to == 0x00`). This
lives in the icom_civ codec/decode path, not the bridge: the bridge's read loop
calls `cat.Decode` exactly as it does today and gets `ErrNoMatch`-equivalent
behaviour for a dropped echo.

### Read strategy: push-only; poll deferred

The bridge's read model (ADR 0019) assumes the rig **pushes** state on change:
`INIT` arms broadcast, `READ` takes a one-shot snapshot on connect, steady state
decodes pushed lines, and `livenessTimeout` silence triggers a re-probe. All
three CAT families support push — Yaesu AUTO (`AI1;`), Kenwood/Elecraft/Flex
**Auto Information** (`AI1;`/`AI2;` — Yaesu inherited `AI` from Kenwood), and
Icom **CI-V Transceive** (broadcasts freq/mode changes to addr `0x00`). So the
push model holds across families and the Kenwood INIT (`AI1;`) is unchanged.

Two caveats are Icom-specific: CI-V Transceive is a **menu setting, not a
guaranteed wire command** (default-ON on the IC-7300, but no universal
"AI1;"-equivalent to force it), and it is sometimes deliberately *off* (it can
contend with other software on the shared bus), so many Icom integrations
**poll** instead.

**Decision: stay push-only for now.** The IC-7300 rigdef ships on the push model
and **CI-V Transceive ON is a documented operator prerequisite** (a first-run /
install.md setup step, alongside baud and CI-V address) — not something the
daemon forces or falls back from. A **rigdef-configurable poll mode** (a `poll`
block — interval + read command — that sends `READ` on a timer and flips
liveness so that a *missed poll*, not silence, is the disconnect signal) was
designed and **explicitly deferred**: it is a real amendment to ADR 0019's
push-only assumption and not worth the bridge change (poll timer +
liveness-semantics swap) before there is demand. The escape hatch is clean —
adding the `poll` block later is additive data + one read-loop branch, with
push-only rigs unaffected. See the trigger below.

### Bridge surface: unchanged

The bridge consumes the codec through four functions only — `cat.Encode`,
`cat.EncodeCommand`, `cat.Decode`, and `def.Terminator` — plus `RigModes` /
`HasCommand` / `ExposedCommands`. The seam is internal to `cat`: those functions
dispatch on `def.Protocol`. No bridge change, so the tune controller (ADR 0027),
the FT8-TX keyer (ADR 0030), and the command path (ADR 0026) work over CI-V the
moment the rigdef declares its `tx_on`/`tx_off`/`set_mode`/`set_power` commands —
and `tx_on`/`tx_off` stay **unexposed** exactly as on the Yaesus.

## Alternatives considered

### Cram CI-V into the existing marker/value_map mini-language

Express BCD and addressing with new JSON marker attributes on the *current*
engine (no protocol field, no second codec). Rejected: BCD assembly and
`FE FE…FD` framing are not slice-and-string-map operations, so this means
inventing a second binary mini-language *inside* the ASCII engine and branching
the existing functions on ad-hoc field presence. That is exactly the
"clever generic framework" the project's lessons warn against
(`lessons-for-v2.md`: build specific, not generic) and would make the Kenwood
path harder to read for no Kenwood benefit.

### Hardcode CI-V command numbers in the icom_civ engine

The engine owns `set_freq = 0x05`, `set_mode = 0x06`, etc.; the rigdef carries
only address + mode table + exposed-ops. Rejected: CI-V command numbers are
*mostly* universal but not perfectly so across the Icom range, so a future Icom
that deviates on any one op would require a **code change to add a rig** —
violating the "no code to add a rigdef" rule. Keeping the command byte in the
rigdef `cmd` field is the more robust reading of the rule and costs only a
slightly richer command schema (the `encoding` kinds), which is generic data,
not per-rig code.

### A separate `icom` driver package parallel to `cat`

A full second package with its own `RigDefinition` analogue and a driver-select
layer in the bridge. Rejected as too heavy: it would fork the rigdef type, the
DI wiring, and the bridge's consumption surface, and force a driver-selection
abstraction the single seam inside `cat` does not need. The per-family split is
real, but it lives one level down (a codec function dispatch), not at the
package boundary.

### Vendor-named discriminator (`yaesu_ascii`)

Keep the implicit "this is the Yaesu engine" framing. Rejected: the protocol is
Kenwood's — Yaesu adopted it — so `yaesu_ascii` mis-describes the family and
would make a future Kenwood TS-590 rigdef declare `"protocol": "yaesu_ascii"`,
which reads as a bug. `kenwood` names the family honestly.

## Consequences

- **One-time engine, then data.** Adding the IC-7300 = the `icom_civ` engine +
  a rigdef. Adding any later Icom, or any Kenwood/Elecraft/Flex, = a rigdef
  only. The rule is preserved for every rig after the first of each family.
- **Richer command/state schema.** `Command` grows `encoding`; `State`/`Marker`
  grow a match-on-command-byte + decode-kind. All default to the current ASCII
  behaviour, so the two shipping Yaesu rigdefs and their tests are untouched
  (the `protocol` field defaults to `kenwood`, `encoding` to `ascii`).
- **`cat` stays transport-free.** The codec gains no serial/UDP import; framing
  stays in `internal/serial` (delimiter `0xFD`). This keeps the ANAN/transport
  seam (below) reachable without disturbing the codec.
- **Validation surface grows.** `cat/validate.go` must validate CI-V rigdefs:
  `civ_address` is valid hex, `cmd` bytes are valid hex, `encoding` is a known
  kind, `mode_byte` commands have a `value_map`, the mode table is injective on
  its value side (the `EncodeCommand` send-side inversion, as for Yaesu MAINMODE).
- **Echo filtering is codec-owned, not bridge-owned.** A subtle CI-V behaviour
  (drop `from != civ_address`) lives in the decode path; the bridge read loop is
  unchanged. A bug here looks like "the rig's own state never updates" rather
  than a transport fault — worth a pinned decode fixture.
- **FT8 / DATA-mode wrinkle to absorb in the rigdef, not the engine.** FT8 on the
  IC-7300 is USB + DATA, and Icom sets DATA via a separate `1A 06` sub-command
  rather than a distinct mode byte. The `mode_byte` encoding (mode + optional
  filter/data bytes) and the `value_map`/`mode_mappings` data must express
  "DATA-U → command 06 with the data-mode bytes" without an engine special-case;
  if it can't, that is a finding against this schema, caught when the rigdef is
  built.

## Triggers to revisit

- **If a future Icom needs a different controller address than `0xE0`**, add the
  `civ_controller_address` rigdef field (the escape hatch named above) — no
  redesign, just the field.
- **If the `mode_byte` encoding cannot express the IC-7300 DATA-mode `1A 06`
  sub-command as pure data**, reconsider whether mode is one `encoding` kind or
  whether CI-V needs a small per-command "data template" (hex with typed
  substitution slots) instead of named kinds.
- **If an ANAN (or any network SDR) is onboarded**, that is the *transport* seam
  (`bridge.md §8.3`, pluggable transport — virtual-serial/TCP, or native
  openHPSDR/UDP), not this codec decision; its CAT protocol is already the
  Kenwood family.
- **If a third *protocol family* appears** (e.g. the legacy Yaesu FT-817/857/897
  5-byte binary CAT), it is a third `protocol` value + engine — the same seam,
  not a redesign.
- **If push-only proves insufficient for Icom** — an operator who can't or won't
  keep CI-V Transceive ON, bus contention with other software, or state that
  Transceive doesn't broadcast — add the deferred **rigdef `poll` block**
  (interval + read command, with liveness flipped so a missed poll is the
  disconnect signal). Additive: push-only rigs are unaffected. Decide then
  whether one missed poll flips to disconnected or N-strikes are required.

## References

- ADR 0026 (rig command path), ADR 0027 (tune carrier), ADR 0028 (rig profiles),
  ADR 0030 (FT8-TX PTT/keyer) — all consume the codec unchanged through this seam.
- `internal/cat/{codec.go,commands.go,rig.go,validate.go}` — the functions the
  `protocol` dispatch is added to.
- `internal/serial/serial.go` — delimiter-byte framing (already CI-V-capable).
- `docs/v2-design/bridge.md` §3c (pure-codec layering) + §8.3 (pluggable transport).
- Memory `project_sm_ic7300_borrowed` (time-box), `project_sm_serial_bridge`.
