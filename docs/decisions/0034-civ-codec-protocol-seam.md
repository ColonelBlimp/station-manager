---
number: 0034
title: Protocol seam in the CAT codec for Icom CI-V
status: Accepted
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

A borrowed **Icom IC-7300** is the first non-Yaesu rig and the first **Icom
CI-V** rig. It is also the **most common HF transceiver on the air**, so the
correctness bar is high — Icom support should be *right*, not FT8-only-and-
quietly-wrong-elsewhere (bench time was extended deliberately to validate the
behaviour empirically; see "Validated on the bench" below). CI-V is a different
protocol: binary frames
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
| `none`      | no data payload (valueless single frame, bytes in `cmd`)       | INIT, tx_on, tx_off |
| `frame_seq` | valueless, emits a fixed SEQUENCE of frames (bytes in `frames`)| READ (freq + mode)  |
| `bcd_freq`  | decimal Hz → 5-byte little-endian BCD                          | set_freq           |
| `mode_seq`  | `value_map` literal → an **ordered CI-V frame sequence** (1+ frames) | set_mode     |
| `bcd_power` | percent/level → BCD level field                                | set_power          |

`mode_seq` is the one that earns the "data, not code" rule its keep. Icom mode
is **two-dimensional** — a base mode (`06`) plus an orthogonal data flag
(`1A 06`) — so a single mode literal can expand to **more than one CI-V frame**.
The rigdef spells the frames out per literal; the engine just emits them in
order, wrapped in `FE FE…FD`. It never needs to know what `1A 06` means.
Bench-validated (see below): `06` **self-clears** the data flag, so non-data
modes are one frame and data modes are two:

```jsonc
// IC-7300 commands (illustrative; exact bytes pinned when built)
"commands": [
  {"name": "READ",     "cmd": "03",   "encoding": "none"},          // read VFO freq
  {"name": "set_freq", "cmd": "05",   "encoding": "bcd_freq", "exposed": true},
  {"name": "set_mode", "cmd": "06",   "encoding": "mode_seq", "value_map": "MODE", "exposed": true},
  {"name": "tx_on",    "cmd": "1C00", "encoding": "none"},          // 1C 00 01 — never exposed
  {"name": "tx_off",   "cmd": "1C00", "encoding": "none"}
]
// MODE value_map entries carry the frame sequence per literal, e.g.
//   "USB"   → ["0601:01"]              (06 mode 01, filter 01 — 06 self-clears data)
//   "USB-D" → ["0601:01", "1A06:0101"] (set USB, then data ON)
//   "CW"    → ["0603:01"]
```

**`State`/`Marker`** decode gains: a state matches on the **command byte** at the
post-address offset (after `FE FE <to> <from>`) instead of a leading prefix, and
a marker carries a **decode kind** (`bcd_freq`, `byte` + `value_mappings`, …)
instead of a raw slice. The Kenwood marker (raw slice / index+length) stays the
default kind. Decode produces the **base mode only** (cmd `01` broadcast / `04`
reply) — the data flag is not read on the push path (see Read strategy). So the
decode mode literal is the base mode (`USB`), never `USB-D`, which is fine for
SM's needs (below).

### Read-loop echo + address filtering (`icom_civ`)

The CI-V read path keeps a frame **iff `from == civ_address`** — that drops our
own echoes (`from == controller`) and accepts both transponded replies
(`to == controller`) and unsolicited transceive broadcasts (`to == 0x00`). This
lives in the icom_civ codec/decode path, not the bridge: the bridge's read loop
calls `cat.Decode` exactly as it does today and gets `ErrNoMatch`-equivalent
behaviour for a dropped echo.

### Read strategy: push-only, no polling

The bridge's read model (ADR 0019) assumes the rig **pushes** state on change:
`INIT` arms broadcast, `READ` takes a one-shot snapshot on connect, steady state
decodes pushed lines. All three CAT families support push — Yaesu AUTO (`AI1;`),
Kenwood/Elecraft/Flex **Auto Information** (`AI1;`/`AI2;` — Yaesu inherited `AI`
from Kenwood), and Icom **CI-V Transceive** (broadcasts freq + base mode to addr
`0x00`). So the push model holds across families; the Kenwood INIT (`AI1;`) is
unchanged, and the Icom INIT enables/relies on Transceive.

**Bench finding (validated, see below): the IC-7300 never broadcasts the
data-mode flag (`1A 06`) via Transceive** — not on a USB↔USB-D toggle, not on a
band change (which silently restores a band's stored data flag). Push delivers
frequency (`00`) and *base* mode (`01`) only; `USB-D` is indistinguishable from
`USB` on the push stream. The *only* ways to know the data flag are to **ask the
rig** (`1A 06`) or to **already know because SM set the mode itself**.

**Decision: push-only, and explicitly NO polling.** Periodic timer-based polling
is a known friction source (bus contention, timing tuning, races, latency, and
it fights other software on the shared bus) and is rejected outright. Crucially,
SM does not need it, because for everything SM actually does the data flag is
either set-by-SM or don't-care:

- **FT8** — SM *commands* `USB-D` before keying (the `mode_seq` above), so **SM
  already knows** the data flag; it never asks. FT8 QSOs log as `FT8` regardless
  (the FT8 subsystem owns the mode, not the CAT read).
- **SSB / CW / AM logging + display** — the *base* mode is the correct answer and
  push delivers it live. No data dimension to miss.

The one accepted gap: the **USB-vs-USB-D distinction for a mode SM did not set
itself** is not tracked. It surfaces in exactly two narrow places — **tune-restore**
of a rig the operator hand-set to a data mode (restores to plain USB), and
**logging a non-FT8 *data* QSO** via SM's form (records the base mode). Both are
treated as a **documented limitation**, not engineered around.

**Non-polling escape hatch (deferred, recorded — not built).** If that gap ever
becomes a real annoyance, the fix is a **single event-triggered `1A 06` query** —
fired *once* when the base mode changes (on an `01` broadcast or band change),
fire-and-forget. CI-V replies are not a synchronous transaction: the answer
arrives as just another frame in the same RX stream the bridge already decodes
(a reply is `from rig → to controller`; a broadcast is `from rig → to 0x00`; the
echo filter passes both), so it updates the tracked mode with no blocking and no
request/response machinery. **This is explicitly NOT periodic polling** — it is
one query per actual base-mode change, event-driven, riding the existing decode
loop. Even this is deferred; push-only ships first.

**CI-V Transceive ON is a documented operator prerequisite** (install.md, with
the `CI-V USB Port = Link to [REMOTE]` + baud notes below) — the daemon neither
forces nor falls back from it.

### Bridge surface: unchanged

The bridge consumes the codec through four functions only — `cat.Encode`,
`cat.EncodeCommand`, `cat.Decode`, and `def.Terminator` — plus `RigModes` /
`HasCommand` / `ExposedCommands`. The seam is internal to `cat`: those functions
dispatch on `def.Protocol`. No bridge change, so the tune controller (ADR 0027),
the FT8-TX keyer (ADR 0030), and the command path (ADR 0026) work over CI-V the
moment the rigdef declares its `tx_on`/`tx_off`/`set_mode`/`set_power` commands —
and `tx_on`/`tx_off` stay **unexposed** exactly as on the Yaesus.

## Implementation notes (codec + rigdef shipped 2026-06-15)

The schema above is the decision; three refinements landed when the engine
(`internal/cat/civ.go`) and the `icom-ic7300.json` rigdef were built. None
change a decision — they pin the "illustrative; exact bytes pinned when built"
parts:

- **`mode_seq` is inline on the command, not a `value_map`.** The per-literal
  frame sequence lives in a `Command.ModeSeq` field (`[{mode, frames}]`), not in
  a marker's `value_mappings`. CI-V's settable mode set is a *superset* of the
  broadcast base modes (the data flag is never pushed), so the decode MAINMODE
  marker (base modes only) cannot also carry the settable frame sequences — they
  are genuinely two tables. `RigModes` therefore sources the settable list from
  `set_mode`'s `ModeSeq` for CI-V. The `value_map: "MODE"` shown in the
  illustration is dropped (it would point at a non-existent marker).
- **A new `frame_seq` encoding kind** joins the table for valueless multi-frame
  commands. `READ` must poll freq (`03`) **and** mode (`04`) as two frames to
  snapshot state on connect (Transceive only broadcasts on *change*, never on
  connect — the CI-V analogue of the Kenwood `READ` packing `ID;FA;FB;…`).
  `INIT` is a single `none` `03` freq-read poke (CI-V Transceive is an operator
  prerequisite, so there is nothing for `INIT` to *arm* — the daemon neither
  forces nor falls back).
- **The delimiter is declared as `"0xFD"`** in the rigdef (`terminator` +
  `serial.line_delimiter`). A raw 0xFD byte cannot be a JSON string (it is not
  valid UTF-8), and `"ý"` unmarshals to the 2-byte UTF-8 of U+00FD, not the
  single byte. The `"0xFD"` hex form keeps the rigdef self-describing (the
  existing "every rigdef declares its delimiter explicitly" rule); the serial
  glue learns to parse it in the transport step.

**Shipped in RX-safe layers (build order: ADR 0019 → 0026 → TX).** The first
`icom-ic7300.json` carries only RX state (INIT/READ + freq/mode decode) and the
two inbound ops `set_freq` + `set_mode` (ADR 0026, no TX). **`tx_on`/`tx_off`
and `set_power` are deferred to the on-rig TX-validation step** — the illustration
above shows them, but TX on the IC-7300 needs the DTR/RTS de-assert (the USB SEND
keying finding) wired *and* on-rig validation before any keying command ships.
`bcd_power` is implemented in the engine (decimal 0–255 level → 2-byte
big-endian BCD) but the IC-7300's level field is a 0–255 scale, **not watts** —
the tune controller passes watts, so a watts→level mapping is owed before
`set_power` ships for Icom. So the shipped RX rigdef has **no exposed TX command
at all**, which the codec test asserts.

## Validated on the bench (IC-7300, 2026-06-14)

A read-only probe (`cmd/civ-probe`, throwaway) talked CI-V to the rig before any
codec was written. Confirmed:

**Connection settings (the rigdef seed + install.md table):**

| Setting | Value | Note |
|---|---|---|
| CI-V address | `0x94` | replies came from `94` |
| Transceive address | `0x00` | broadcasts go *to* `00` |
| **Effective baud** | **19200**, 8N1 | see baud gotcha below |
| USB Echo Back | On | echo filter (`from == rig`) is required, not optional |
| CI-V USB Port | Link to [REMOTE] | so Transceive reaches the USB port |
| CI-V Transceive | On | push available (operator prerequisite) |

- **BCD frequency** decodes/encodes as 5-byte little-endian (e.g.
  `00 40 07 14 00` → 14 074 000 Hz). Confirmed both polled (`03`) and broadcast (`00`).
- **Mode is two-dimensional and `06` self-clears data.** With the rig in USB-D,
  sending base-mode-only (`06 01`) cleared the data flag to OFF. So `set_mode`
  for a non-data mode is **one** frame; for a data mode it is **two** (`06 …`
  then `1A 06 01`). This is the `mode_seq` design.
- **The data flag is never broadcast.** Across both a USB↔USB-D toggle and a
  full band-hop (17→15→12→10→20 m), every mode broadcast was `01 01` (USB base)
  with zero `1A 06` frames. Basis for the push-only read strategy + accepted gap.
- **Baud gotcha (install.md-worthy):** the IC-7300 has *two* baud settings —
  `CI-V Baud Rate` (rear `[REMOTE]` jack) and `CI-V USB Baud Rate` (USB port,
  used **only when `CI-V USB Port = Unlink`**). With `Link to [REMOTE]` (the
  common setup), the USB port rides the REMOTE bus at the **`CI-V Baud Rate`**,
  and the `CI-V USB Baud Rate` is dormant. So the baud SM connects at is the
  `CI-V Baud Rate`, **not** the `CI-V USB Baud Rate` — easy to get backwards.
  (Confirmed empirically: comms worked at 19200 = `CI-V Baud Rate`; the USB-baud
  value of 115200 gave echo-but-no-reply because it was dormant.)

**PTT / serial control lines — a hard requirement.** The IC-7300's `USB SEND`
function can map PTT to **RTS or DTR of the CAT port**. `go.bug.st/serial`
asserts DTR+RTS on open, so **merely opening the port keyed the rig** during the
probe. Consequences for the rigdef + transport:

- SM must key the IC-7300 **via CAT (CI-V `1C 00`)** — the existing
  `tx_on`/`tx_off` pattern (ADR 0027/0030), staying unexposed. There is no
  separate PTT port (single CP2102; the FTdx10's second CP2105 port has no Icom
  analogue).
- The Icom serial config must **not** inherit the Yaesu `rts:true/dtr:true`; the
  transport should **de-assert DTR+RTS on open** so a stray `USB SEND` mapping
  can't transmit. `USB SEND = OFF` is also a documented operator prerequisite.

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
  kind, `mode_seq`/`value_map` commands have a `value_map`, the mode table is
  injective on its value side (the `EncodeCommand` send-side inversion, as for
  Yaesu MAINMODE), and each `mode_seq` entry's frames are valid hex.
- **Echo filtering is codec-owned, not bridge-owned.** A subtle CI-V behaviour
  (drop `from != civ_address`) lives in the decode path; the bridge read loop is
  unchanged. A bug here looks like "the rig's own state never updates" rather
  than a transport fault — worth a pinned decode fixture. Validated: USB Echo
  Back is On, so this filter is required, not defensive.
- **DATA-mode is `mode_seq`, resolved (was the open schema risk).** FT8 on the
  IC-7300 is USB + DATA, set via a separate `1A 06` sub-command. The `mode_seq`
  encoding expresses this as pure data (a per-literal frame sequence), validated
  on the bench — the engine never special-cases `1A 06`. The earlier worry that
  the schema might not express it is **closed**.
- **Accepted limitation: USB-vs-USB-D is not read back.** Push never carries the
  data flag and SM does not poll, so a mode SM did not set itself is tracked as
  its base mode. Affects only tune-restore-of-a-data-mode and non-FT8 data-mode
  logging (both documented). FT8 is unaffected (SM sets the mode). The
  non-polling escape hatch (single event-triggered `1A 06`) is recorded, not built.
- **Serial transport must de-assert DTR+RTS for Icom.** `USB SEND` can map PTT to
  a control line, and opening the port asserts them — so the Icom serial config
  must drop DTR+RTS on open and never inherit the Yaesu `rts:true/dtr:true`.
  Keying is CAT-only (`1C 00`, unexposed). This is a behavioural requirement on
  the transport, not just rigdef data.

## Triggers to revisit

- **If a future Icom needs a different controller address than `0xE0`**, add the
  `civ_controller_address` rigdef field (the escape hatch named above) — no
  redesign, just the field.
- **If `mode_seq`'s per-literal frame list proves too rigid** (e.g. an Icom needs
  conditional or computed data in a mode set), reconsider a small per-command
  "data template" (hex with typed substitution slots). Not needed for the IC-7300
  — the fixed frame list covers it (validated).
- **If an ANAN (or any network SDR) is onboarded**, that is the *transport* seam
  (`bridge.md §8.3`, pluggable transport — virtual-serial/TCP, or native
  openHPSDR/UDP), not this codec decision; its CAT protocol is already the
  Kenwood family.
- **If a third *protocol family* appears** (e.g. the legacy Yaesu FT-817/857/897
  5-byte binary CAT), it is a third `protocol` value + engine — the same seam,
  not a redesign.
- **If the USB-vs-USB-D gap becomes a real annoyance** (tune-restore of a
  hand-set data mode, or non-FT8 data-mode logging), add the **single
  event-triggered `1A 06` query** (fire-and-forget on a base-mode change, answer
  rides the existing decode stream). This is **not** periodic polling — that
  remains rejected. Decide then whether to fire only for tune-snapshot or also to
  keep a live display mode.
- **If a rig genuinely does not push at all** (no Transceive/AI equivalent),
  *that* is the periodic-poll question — separate from the Icom data-flag gap,
  and still deferred. No such rig is in scope.

## References

- ADR 0026 (rig command path), ADR 0027 (tune carrier), ADR 0028 (rig profiles),
  ADR 0030 (FT8-TX PTT/keyer) — all consume the codec unchanged through this seam.
- `internal/cat/{codec.go,commands.go,rig.go,validate.go}` — the functions the
  `protocol` dispatch is added to.
- `internal/serial/serial.go` — delimiter-byte framing (already CI-V-capable);
  the transport must de-assert DTR+RTS on open for Icom (USB SEND finding).
- `docs/v2-design/bridge.md` §3c (pure-codec layering) + §8.3 (pluggable transport).
- Bench validation: `cmd/civ-probe` (throwaway read/`-set`/`-listen` probe,
  2026-06-14) — confirmed the settings table, BCD, `06` self-clears data, and
  data-flag-never-broadcast.
- `docs/install.md` (future): IC-7300 prerequisites — CI-V Transceive ON,
  `CI-V USB Port = Link to [REMOTE]`, baud = `CI-V Baud Rate` (not USB baud),
  `USB SEND = OFF`.
- Memory `project_sm_ic7300_borrowed`, `project_sm_serial_bridge`.
