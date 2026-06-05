# internal/cat code review (2026-06-05)

## Scope

Reviewed the CAT package and the write-path consumers that depend on its rig
definition contract:

- `internal/cat/codec.go`, `commands.go`, `rig.go`, `rigdb.go`
- `internal/cat/rigs/yaesu-ftdx10.json`, `internal/cat/rigs/yaesu-ft710.json`
- CAT fixture, command, rig database, and equivalence tests in `internal/cat`
- Bridge/API consumers in `internal/bridge/{pipeline,command,tune}.go` and
  `internal/api/{handler_rig_command,handler_config}.go`
- Related design context in ADR 0026, ADR 0027, and
  `docs/v2-design/cat-serial-reuse.md`

Not reviewed in depth: frontend control rendering, live-radio behavior, or fresh
manual verification beyond the repository's existing documented manual checks.

## Summary

The core carve-out succeeded: `internal/cat` is a small, pure codec with no serial
I/O, and the v1 equivalence fixtures pin the decode/encode quirks that the bridge
depends on. The command-path additions are also cleanly data-driven: exposed ops,
padding, and value-map inversion all live in rig definitions rather than in API
switch statements.

Headline findings: 0 critical, 3 medium, 2 low. The main theme is trust boundaries.
The package now sits on a hardware write path, but both runtime command values and
static rig JSONs are still trusted more than that path can safely justify.

## M - Medium

### M1. Padded command values are not validated before reaching the serial write path

Files:

- `internal/cat/commands.go:61`
- `internal/cat/commands.go:94`
- `internal/cat/commands.go:141`
- `internal/api/handler_rig_command.go:83`
- `internal/api/handler_rig_command.go:119`
- `internal/bridge/command.go:66`
- `internal/cat/rigs/yaesu-ftdx10.json:28`
- `internal/cat/rigs/yaesu-ftdx10.json:35`

`EncodeCommand` explicitly says range and width validation is the command endpoint's
responsibility, then only applies `leftZeroPad` when `Pad > 0`. `leftZeroPad`
returns over-wide values unchanged and does not require digits. The API handler is
intentionally op-agnostic: it accepts any JSON string or number, renders it to a
string, and passes it to `bridge.SendCommands`. The bridge encodes the whole batch
and writes the bytes when a verified client is connected.

For the current FTdx10 rigdef, this affects externally exposed commands:

- `set_freq` is `FA%s;` with `pad: 9`
- `set_freq_b` is `FB%s;` with `pad: 9`
- `set_power` is `PC%s;` with `pad: 3`

That means values such as `1.5`, `-1`, `abc`, or an over-wide frequency/power string
can become syntactically malformed CAT lines instead of a `rig_invalid_value`
response. For example, the current code shape would allow a dotted JSON number to be
padded into an `FA...;` field and a non-digit three-character string to become
`PCabc;`.

Suggested direction: make `Pad` a validation contract, not only a formatting hint.
At minimum, `EncodeCommand` should reject non-ASCII digits and `len(value) > Pad` for
padded commands, returning a sentinel that the API maps to `rig_invalid_value`.
Range checks can start coarse (`set_power` 5..100 for the shipped Yaesu rigs) or be
made data-driven later, but width and digit validation should be generic now. Add
CAT tests for invalid padded values and a bridge/API test proving a bad value writes
nothing even with an active fake serial client.

### M2. Embedded rig definitions are not schema-validated before registration

Files:

- `internal/cat/rigdb.go:39`
- `internal/cat/rigdb.go:43`
- `internal/cat/rigdb.go:46`
- `internal/cat/codec.go:106`
- `internal/cat/commands.go:76`
- `internal/cat/commands.go:121`
- `internal/cat/reference_test.go:60`
- `docs/v2-design/cat-serial-reuse.md:303`
- `docs/v2-design/cat-serial-reuse.md:311`

The embedded loader only checks that JSON parses, `id` is non-empty, and the id is
not duplicated. It does not validate command templates, duplicate command names,
state prefixes, marker bounds, `value_map` references, value-map injectivity, serial
defaults, or the exposed-command contract.

That matters because `Encode` delegates directly to `fmt.Appendf`, while
`EncodeCommand` decides whether a command is value-bearing by checking whether the
template contains any `%`. A rig JSON typo such as `%03d`, two `%s` verbs, a missing
`%s` on a padded command, or duplicate value-map literals would not fail at load
time. It would surface later as malformed CAT bytes, silently selected duplicate
mapping entries, or a capability advertised by `GET /v1/config` that cannot encode
correctly.

The tests catch many current FTdx10 cases, but the protection is patchy:
`TestEncodeCommandBijection` only covers `MAINMODE` on the FTdx10, while `set_band`
uses `BAND` and future rigdefs can add other `value_map` commands. The frozen
reference encode comment also points back to v1's `%s`-only validation contract, but
there is no equivalent validator in the current loader.

Suggested direction: add a `ValidateRigDefinition` path and call it from the
embedded loader, from any future external-dir loader, and from a
`TestEmbeddedRigDefinitionsValidate` test. Useful checks include: unique non-empty
command names; unique non-empty state prefixes; positive marker lengths; non-negative
marker indices; value-bearing commands have exactly one `%s` and no other format
verbs; valueless commands have no format verbs; every `value_map` references an
existing marker tag with injective `Value` entries; `Pad >= 0`; exposed commands
encode with representative values; serial/timing fields are sane.

### M3. The FT-710 rigdef is still read-only even though the accepted command-path ADR says it follows

Files:

- `internal/cat/rigs/yaesu-ft710.json:7`
- `internal/cat/rigs/yaesu-ft710.json:24`
- `internal/cat/commands_test.go:114`
- `internal/api/handler_config.go:80`
- `internal/api/handler_config.go:86`
- `internal/api/handler_config.go:389`
- `docs/decisions/0026-rig-command-path-freq-mode.md:235`

The FT-710 definition says it mostly mirrors the FTdx10 and has the same `FA`, `FB`,
`PC`, `PB`, `AI`, and `VS` command formats. ADR 0026 also lists the FT-710 as the
rig that follows the FTdx10 for command entries. In the current JSON, though, the
FT-710 command table only contains `INIT`, `READ`, and `PLAYBACK`, with no exposed
`set_freq`, `set_mode`, `set_power`, or band/VFO operations.

This is visible to users, not just an internal gap. `BridgeInfo.Ops` is populated
from `cat.ExposedCommands`, and `BridgeInfo.Tune` comes from `bridge.TuneSupported`.
For an FT-710 configuration, the SPA will see no generic rig-control ops and no tune
support, despite the rigdef description and ADR text indicating that the command
formats are available or intended to follow.

Suggested direction: decide whether FT-710 is intentionally read-only. If yes, make
that explicit in the rigdef description, ADR follow-up notes, and tests. If no, add
the known-safe FT-710 command entries and expose the non-transmitting ones with the
same tests the FTdx10 has. TX/tune entries should only be added after manual or live
verification of the FT-710 `TX` behavior, but `set_freq`, `set_freq_b`, `set_mode`,
and `set_power` appear to be the immediate capability gap.

## L - Low

### L1. Tune capability is advertised by command-name presence, not by encodeability

Files:

- `internal/bridge/tune.go:330`
- `internal/bridge/tune.go:355`
- `internal/bridge/tune.go:382`
- `internal/api/handler_config.go:390`

`TuneSupported` returns true when the rigdef has commands named `set_mode`,
`set_power`, `tx_on`, and `tx_off`. The actual tune-on path is stricter: it calls
`cat.EncodeCommand` for `set_mode` and `set_power`, which requires `Exposed`, a
working value map, a value, and a valid template. It then calls low-level `Encode`
for `tx_on`, and `StartTune` separately preflights `tx_off`.

The current FTdx10 definition satisfies all of that, and `StartTune` will refuse
before keying if encoding fails. The issue is advertisement drift: a future rigdef
with the four command names present but one broken template, missing `MAINMODE`
mapping, or unexposed `set_power` can make `GET /v1/config` report `tune: true`
while the tune button always fails at runtime.

Suggested direction: make `TuneSupported` use the same dry-run contract as
`StartTune`, for example by checking that `EncodeCommand(def, "set_mode", "RTTY-U")`,
`EncodeCommand(def, "set_power", "20")`, `Encode(def, "tx_on")`, and
`Encode(def, "tx_off")` all succeed. This becomes more important once rigdefs are
added beyond the single FTdx10 tune implementation.

### L2. `Lookup` returns mutable rig definitions with shared slice and map backing storage

Files:

- `internal/cat/rigdb.go:58`
- `internal/cat/rig.go:22`
- `internal/cat/rig.go:23`
- `internal/cat/rig.go:24`

`Lookup` returns a `RigDefinition` by value, but the struct contains slices and a
map. The top-level struct is copied; the `Commands`, `States`, marker slices, value
mapping slices, and `ModeMappings` map still share backing storage with the
process-global `rigDB` entry.

Current callers appear to treat rig definitions as read-only, and the CAT race test
passes. The foot-gun is still real for future internal code: a caller that modifies
`def.Commands[0].Cmd`, edits a marker mapping, or writes to `def.ModeMappings` can
corrupt the global definition for every later lookup in the process. That is
especially easy to do accidentally when merging operator overrides or building test
fixtures from a looked-up definition.

Suggested direction: either deep-copy definitions in `Lookup` or document a strict
read-only contract and provide explicit copy helpers for tests/overrides. Deep copy
is the safer default because the package already exposes the mutable structs.

## Test Results

Passed:

```text
go test ./internal/cat ./internal/bridge ./internal/api
go test -race ./internal/cat
```

The combined test command first failed inside the sandbox because `httptest` could
not bind a localhost listener. It passed when rerun outside the sandbox. The CAT
race test passed inside the sandbox.

## Triage + decisions (2026-06-05)

All 5 findings validated against the code (3 parallel read-only passes) — **all code-accurate**.
Severity is modest for the single-operator localhost FTdx10 deployment, but the write-path
trust gaps are worth closing. Verdicts + plan:

- **M1** (padded values unvalidated) — **FIX (Batch A).** Reachable only via a hand-crafted
  `POST /v1/rig/command` (SPA sends clamped ints; value-map ops reject garbage; no TX dimension).
  Fix: make `Pad` a validation contract — `EncodeCommand` rejects non-ASCII-digit and
  `len(value) > Pad` for `Pad>0` commands, new sentinel → API `rig_invalid_value`. Scope to
  padded commands only; `>` not `>=`; do NOT hardcode a power range (rigdef data if ever wanted).
- **M2** (rigdefs not schema-validated) — **FIX (Batch B).** Loader checks only parse/id/dup;
  `Encode` is `fmt.Appendf` so template typos become silent garbage; `BAND` value-map isn't
  injectivity-tested. Fix: `ValidateRigDefinition` from the embedded loader (panic, matching the
  existing pattern) + `TestEmbeddedRigDefinitionsValidate`. High-value checks: template-verb
  (exactly one `%s`, no other verb for value-bearing; none for valueless), value-map
  existence + injectivity, unique command names, exposed-commands-encode. Skip prefix-uniqueness
  + serial-defaults (decode/serial layers already degrade gracefully).
- **L1** (`TuneSupported` name-presence not encodeability) — **FIX (Batch C).** Future-rigdef
  drift only. Fix: dry-run `EncodeCommand(set_mode, tuneCarrierMode)` + `EncodeCommand(set_power,
  Itoa(defaultTunePowerW))` + `Encode(tx_on)` + `Encode(tx_off)` — byte-for-byte the StartTune gate.
- **M3** (FT-710 read-only) — **DEFERRED (operator, 2026-06-05): planned, hardware-gated.** Not a
  defect (ADR 0026 says FT-710 "follows"; tests + doc comments pin "FT-710 today" as expected).
  The operator HAS an FT-710 on the bench and will add the write entries (set_freq/set_freq_b/
  set_mode, then set_power/set_band, then TX/tune) with live verification per the project's
  confirm-on-hardware standard — NOT blind-added from the manual. (set_band especially: the FT-710
  rigdef has no BAND state yet.)
- **L2** (`Lookup` shallow-copy aliasing) — **DEFERRED (operator, 2026-06-05): fold into the first
  real mutator.** Latent today (no caller mutates a looked-up def; the mode-mappings merge copies
  into a fresh map). When `RegisterExternalDir`/operator-override loading lands, add a `Clone()`
  deep-copy helper + a read-only contract doc + a boundary test then. Deep-copy-on-every-Lookup
  is overkill for the hot read-only path.

Actionable batches: **A (M1), B (M2), C (L1)** — all decision-free.

## Resolutions

### Batch A — M1 padded-value validation. **DONE 2026-06-05.**
`internal/cat/{codec,commands}.go` + `internal/api/handler_rig_command.go` + tests:
- New sentinel `cat.ErrInvalidPaddedValue`. `EncodeCommand` now rejects a value that is
  not all ASCII digits, or is wider than `Pad`, for padded commands (`c.Pad > 0 &&
  c.ValueMap == ""`) — via `isASCIIDigits` + a `len(v) > c.Pad` check — before
  `leftZeroPad`/`Encode`. So `set_freq`/`set_freq_b`/`set_power` can no longer pad a
  bad value (`"abc"`, `"1.5"`, `"-1"`, over-wide) into a malformed CAT line. Digit check
  scoped to padded-without-value_map (value-map commands validate via their map; valueless
  ignore value). Width bound is `>` (a value exactly `Pad` wide is valid). NO range check —
  semantic range (e.g. power 5..100) deliberately left to data/endpoint per no-magic-numbers.
- API: `writeRigCommandError` maps `cat.ErrInvalidPaddedValue` → `rig_invalid_value` (400),
  alongside `ErrUnmappedValue`/`ErrMissingValue`.
- Tests: 7 new `TestEncodeCommand` cases (non-digit / dotted / negative / over-wide reject;
  at-width + padded still encode), and 2 bridge `TestSendCommand_RejectsBeforeWrite` cases
  proving a bad padded value writes nothing **with an active, identity-verified fake client**.
- `go test ./...` + `-race ./internal/cat` + build green; vet + gofmt clean.

### Batch B — M2 ValidateRigDefinition at load. **DONE 2026-06-05.**
New `internal/cat/validate.go` — `ValidateRigDefinition(def)` called from `rigdb.go`'s
embedded loader (panic on failure, matching the existing id/dup load-time invariants).
Checks (encode-critical only): unique non-empty command names; value-bearing templates
(ValueMap / Pad / `%`) have exactly one `%s` and no other format verb (closes the silent
`fmt.Appendf` typo hole — `countFormatVerbs` treats `%%` as a literal); `Pad >= 0`; every
command `ValueMap` references an existing marker tag that's non-empty + injective on its
Value side (`validateValueMap` — closes the BAND coverage gap generically, scanning all
markers carrying the tag); markers have `Length > 0` and `Index >= 0`. Deliberately skipped
state-prefix uniqueness (longest-prefix-wins tolerates dups) and serial-defaults (validated
at port open) — gold-plating per the validation pass. Tests: `TestValidateRigDefinition`
(11 bad-def cases + a good baseline) + `TestEmbeddedRigDefinitionsValidate` (both shipped
rigdefs pass). Left `TestEncodeCommandBijection` (a concrete MAINMODE round-trip — overlaps
but complements the structural injectivity check). `go test ./...` + `-race ./internal/cat`
+ build green; vet + gofmt clean.

### Batch C — L1 TuneSupported dry-run. **DONE 2026-06-05.**
`internal/bridge/tune.go`: `TuneSupported` no longer checks command-name presence
(`cat.HasCommand`) — it now dry-runs the exact encodes `StartTune` performs:
`EncodeCommand(set_mode, tuneCarrierMode)` + `EncodeCommand(set_power, Itoa(defaultTunePowerW))`
+ `Encode(tx_on)` + `Encode(tx_off)`, all must succeed. So `BridgeInfo.Tune` can't advertise
`tune:true` for a rigdef whose set_mode/set_power is unexposed/broken/missing its value-map or
whose tx_on/tx_off won't encode. Probe values mirror `encodeTuneOn`/`encodeTuneUnkey` exactly
(low-level `Encode` for the non-Exposed tx_*). Test `TestTuneSupported_RequiresEncodeableCommands`
(four names present but set_mode unexposed → false, where name-presence would've said true).
`go test ./...` + `-race ./internal/bridge` + build green; vet + gofmt clean.

**Review complete — all actionable findings resolved (M1, M2, L1).**
**Deferred (operator): M3 (FT-710 write entries — hardware-gated, operator has the rig),
L2 (Lookup deep-copy/Clone — fold into the first real mutator / external-dir loader).**
