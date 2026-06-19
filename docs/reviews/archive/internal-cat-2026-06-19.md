# `internal/cat` - code review (2026-06-19)

## Scope

Review-only pass over `internal/cat` as a fresh package: ASCII CAT and CI-V codec
logic, embedded rig definitions, rigdef validation, command exposure, ACK
classification, tests/benchmarks, and the bridge/API/config/frontend contracts
that consume CAT metadata.

No production code changes were applied during this review. This document is the
only artifact from the pass.

Headline counts: **0 Critical**, **0 High**, **2 Medium**, **3 Low**.

## Medium findings

### M1. CI-V ACK classification accepts ACK-looking frames not addressed to Station Manager

**Files:**

- `internal/cat/civ.go:44`
- `internal/cat/civ.go:55`
- `internal/cat/civ.go:63`
- `internal/cat/civ.go:67`
- `internal/cat/civ.go:70`
- `internal/bridge/command.go:192`
- `internal/bridge/command.go:205`
- `internal/bridge/command.go:223`
- `internal/bridge/command.go:234`
- `internal/bridge/ft8tx.go:182`
- `internal/bridge/tune.go:232`
- `internal/cat/civ_test.go:562`
- `internal/cat/civ_test.go:573`
- `internal/cat/civ_test.go:577`

`CIVAck` documents the expected ACK as `FE FE E0 <rig> FB/FA`, but it only checks
that the frame is from the configured rig address and that byte 4 is `FB` or
`FA`. It does not require `line[2] == civControllerAddress` or the bare ACK frame
length.

On a CI-V addressed bus, a frame from the rig to another controller such as
`FE FE E1 94 FB` would be classified as Station Manager's command ACK. The bridge
uses that result to complete `writeCIVFramesAwaitAck`, including the
safety-critical FT8/tune `tx_on` and `tx_off` confirmations.

Impact:

- A false `FB` during a generic command can make the bridge report success and
  synthesize state for a command the rig did not actually acknowledge to Station
  Manager.
- A false `FB` during `tx_off` can make a CI-V release path report clean unkey
  and clear its backstop even though the matching unkey ACK was not received.
- Current tests cover FB/FA, broadcasts, and our own echo, but do not cover an
  ACK-looking frame addressed to another controller.

Suggested fix:

- In `CIVAck`, require `line[2] == civControllerAddress` and `line[3] ==
  rigAddr` before accepting `FB/FA`.
- Prefer exact bare ACK length (`len(line) == 5`) unless CI-V evidence shows ACK
  payload bytes are valid.
- Add tests for `FE FE 00 94 FB`, `FE FE E1 94 FB`, and an overlong
  `FE FE E0 94 FB 00`; all should be non-ACK unless there is a documented reason
  otherwise.

### M2. Numeric command ranges are not enforced before hardware writes

**Files:**

- `internal/cat/commands.go:63`
- `internal/cat/commands.go:65`
- `internal/cat/commands.go:111`
- `internal/cat/commands.go:117`
- `internal/api/handler_rig_command.go:83`
- `internal/api/handler_rig_command.go:89`
- `internal/bridge/command.go:89`
- `internal/bridge/command.go:93`
- `internal/bridge/command.go:131`
- `internal/bridge/command.go:135`
- `internal/cat/rigs/yaesu-ftdx10.json:29`
- `internal/cat/rigs/yaesu-ftdx10.json:36`
- `internal/cat/rigs/yaesu-ft710.json:29`
- `internal/cat/rigs/yaesu-ft710.json:36`
- `internal/cat/rigs/icom-ic7300.json:44`
- `internal/cat/civ.go:189`
- `internal/cat/civ.go:195`
- `internal/cat/civ.go:196`
- `internal/cat/civ.go:201`
- `docs/v2-design/cat-serial-reuse.md:239`
- `docs/v2-design/cat-serial-reuse.md:240`

`EncodeCommand` now rejects malformed padded values, but its own comment leaves
semantic range validation to the command endpoint. The HTTP command handler is
op-agnostic: it converts any JSON scalar to a string and passes it through
`bridge.SendCommands`. The bridge encodes and writes the command after the
identity/connection/TX gates.

For the shipped Yaesu rigdefs, `set_freq` only needs to be 9 digits and
`set_power` only needs to be 3 digits. The design doc records the manual ranges
for those fields (`FA/FB` frequency range and `PC 005-100`), but neither CAT nor
API enforces them. For the IC-7300, `set_power` with `scale_max_watts=100` clamps
over-range values to level 255 instead of rejecting them.

Impact:

- A direct `POST /v1/rig/command` can send syntactically valid but
  semantically invalid Yaesu CAT commands such as `PC999;` or an out-of-range
  frequency; Yaesu has no command ACK path, so the API still returns 202.
- On the IC-7300, `set_power` values above the rigdef scale are silently coerced
  to full scale rather than rejected as invalid input.
- The existing tests cover non-digit and over-wide values, but not in-range
  boundaries or over-range-but-width-valid values.

Suggested fix:

- Add data-driven range metadata to `Command` for numeric arguments, or add a
  small op-aware validator at the API/bridge boundary for the current exposed
  numeric ops.
- At minimum, reject `set_power` outside the rig's documented safe range and
  reject `EncodingBCDPower` watts above `ScaleMaxWatts` instead of clamping.
- Add CAT tests for boundary values and API/bridge tests proving invalid numeric
  commands return `rig_invalid_value` and write nothing.

## Low findings

### L1. CI-V `sets_state` validation checks only tag existence, not value compatibility

**Files:**

- `internal/cat/rig.go:197`
- `internal/cat/rig.go:204`
- `internal/cat/validate.go:270`
- `internal/cat/validate.go:284`
- `internal/cat/commands.go:147`
- `internal/cat/commands.go:151`
- `internal/bridge/command.go:174`
- `internal/bridge/command.go:177`
- `internal/bridge/command.go:304`
- `internal/bridge/command.go:309`
- `internal/bridge/pipeline.go:744`
- `internal/bridge/pipeline.go:756`
- `internal/bridge/pipeline.go:782`
- `internal/cat/civ_test.go:621`
- `internal/cat/civ_test.go:631`

`ValidateRigDefinition` rejects a CI-V `sets_state` value that names no marker,
but it does not verify that the command's value shape can populate that marker.
The bridge treats any non-empty `sets_state` as enough to skip read-back after ACK
and synthesize a payload from `cat.Status{tag: commandedValue}`.

Impact:

- A future CI-V rigdef can pass validation with a mismatched pair such as a mode
  command setting a frequency tag, a power command setting `MAINMODE`, or a
  valueless command declaring `sets_state`.
- In that case the bridge may skip the read-back path and publish nothing or the
  wrong partial state, leaving the UI stale after a successful ACK.
- Current tests cover only the unknown-tag case.

Suggested fix:

- Extend CI-V validation so `sets_state` is compatible with command encoding:
  `bcd_freq` maps to a BCD frequency marker, `bcd_power` maps to a BCD level
  marker, `mode_seq` maps to a marker whose mapped values include the mode
  literals, and valueless commands cannot declare `sets_state`.
- Add negative validation tests for incompatible `sets_state` pairs and one
  positive test for each shipped IC-7300 `sets_state` command.

### L2. `Lookup` returns mutable rig definitions sharing global backing storage

**Files:**

- `internal/cat/rigdb.go:14`
- `internal/cat/rigdb.go:17`
- `internal/cat/rigdb.go:64`
- `internal/cat/rigdb.go:66`
- `internal/cat/rig.go:77`
- `internal/cat/rig.go:87`
- `internal/cat/rig.go:88`
- `internal/cat/rig.go:89`
- `internal/cat/rigdb.go:79`
- `internal/cat/rigdb.go:87`

`Lookup` returns a `RigDefinition` by value, but its slices and maps still point
at the process-global `rigDB` entry. A caller can mutate `def.Commands`,
`def.States`, nested `ValueMappings`, or `def.ModeMappings` and affect later
lookups in the same process.

Impact:

- Current callers appear read-only, and the race suite is clean.
- The package API still exposes a mutable global-data foot-gun, which becomes
  more likely to matter when external rigdef loading or operator overrides move
  beyond the current `RegisterExternalDir` stub.

Suggested fix:

- Add a deep-copy helper such as `CloneRigDefinition` and use it at mutation
  boundaries, or have `Lookup` return a deep copy.
- Add a regression test that mutating a looked-up definition cannot affect a
  second lookup if deep-copy-on-lookup is chosen.

### L3. Current docs/comments still describe old CAT capability state

**Files:**

- `docs/install.md:124`
- `docs/install.md:126`
- `frontend/logging/src/lib/api/config.ts:99`
- `frontend/logging/src/lib/api/config.ts:102`
- `frontend/logging/src/lib/ui/components/TuneButton.svelte:13`
- `frontend/logging/src/lib/ui/components/TuneButton.svelte:15`
- `internal/cat/rigs/icom-ic7300.json:2`
- `internal/cat/rigs/icom-ic7300.json:10`
- `internal/cat/rigs/yaesu-ft710.json:8`
- `internal/api/handler_config.go:99`
- `internal/api/handler_config.go:106`

The server-side CAT/API comments now say both shipped Yaesu rigs advertise tune,
and the rig catalogue includes the IC-7300 with CI-V commands and FT8 TX keying.
Some active documentation still says otherwise:

- `docs/install.md` says Yaesu FT-710 and FTdx10 are the two tested drivers.
- `frontend/logging/src/lib/api/config.ts` still gives FT-710 as the example of
  a rig that cannot tune.
- `TuneButton.svelte` says an unsupported rig such as FT-710 should not show the
  tune affordance.

Impact:

- Operator/developer-facing docs understate current CAT support and can send
  maintainers looking for the wrong capability boundary.
- The stale frontend comments are close to the UI gate that consumes
  `BridgeInfo.Tune`, so they are easy to copy into future behavior changes.

Suggested fix:

- Refresh user-facing docs to list the current supported/tested rig families,
  including IC-7300 if that support is considered shipped.
- Update frontend comments so FT-710 is no longer the unsupported-tune example;
  use a generic "rig with no tune support" wording or add an intentionally
  read-only test rig if one exists.

## Security notes

- `internal/cat` has no direct network or filesystem input path beyond embedded
  rig JSONs today, but it is on a hardware write path through
  `POST /v1/rig/command`, tune, and FT8 keying.
- The current unsafe-command exposure invariant is good: `ValidateRigDefinition`
  rejects exposed `tx_on`, `tx_off`, and `PLAYBACK`, and tests cover shipped
  Yaesu/IC-7300 exposure.
- The remaining security-relevant issues are hardware-control correctness: CI-V
  ACKs should be addressed to this controller, and numeric commands should reject
  unsafe/out-of-range values before they reach the rig.

## Performance notes

- The package remains pure and small; no serial or network I/O is inside
  `internal/cat`.
- Current CAT microbenchmarks are fast for the present rigdef sizes:
  `BenchmarkDecode` around 214 ns/op and `BenchmarkLookupState` around 66 ns/op
  on this machine.
- I did not find a performance issue worth raising for current shipped rigdefs.
  If many more CI-V states are added, the per-decode hex parsing in
  `lookupStateCIV` may be worth revisiting, but it is not a current bottleneck.

## Test coverage notes

Strong coverage already exists for:

- ASCII decode equivalence against the frozen reference path.
- ASCII command exposure, value-map inversion, padding validation, and shipped
  Yaesu command bytes.
- Embedded rigdef validation at load and in tests.
- CI-V BCD frequency/power encode/decode, frame sequencing, IC-7300 golden bytes,
  exposed operation list, `RigModes`, and basic ACK classification.

Coverage gaps tied to the findings:

- No CI-V ACK tests for wrong recipient address or overlong ACK-looking frames.
- No CAT/API tests for numeric semantic ranges: over-range but width-valid
  frequency/power values, lower/upper boundaries, and IC-7300 watts above
  `ScaleMaxWatts`.
- No validation tests for incompatible `sets_state` metadata.
- No test protects `Lookup` callers from mutating shared rigdef backing storage.

## Verification

Commands run during this review:

- `GOCACHE=/tmp/go-build go test ./internal/cat -count=1`
  - Pass.
- `GOCACHE=/tmp/go-build go test -race ./internal/cat -count=1`
  - Pass.
- `GOCACHE=/tmp/go-build go test ./internal/cat -bench . -benchmem -run '^$'`
  - Pass; benchmark results noted above.
- `GOCACHE=/tmp/go-build go vet ./internal/cat ./internal/bridge ./internal/api ./internal/config ./internal/types`
  - Pass.
- `GOCACHE=/tmp/go-build go test ./internal/cat ./internal/bridge ./internal/api ./internal/config ./internal/types -count=1`
  - First sandboxed attempt failed because `httptest.NewServer` could not bind
    loopback in the restricted environment.
  - Rerun with loopback allowed: pass.
- `GOCACHE=/tmp/go-build go test -race ./internal/cat ./internal/bridge ./internal/api -count=1`
  - Pass.

## Resolution (2026-06-19)

M1, M2, L2, L3 fixed; L1 deferred to backlog (operator decision).

- **M1 (fixed).** `CIVAck` now requires the frame be addressed TO the controller
  (`line[2] == civControllerAddress`), FROM the rig (`line[3] == rigAddr`), and
  be EXACTLY 5 bytes — so an ACK-looking frame for another controller (…E1 94
  FB), a broadcast (…00 94 FB), or one carrying payload can't be mistaken for our
  command's ACK (which would let a safety-critical tx_on/tx_off confirm complete
  on a frame the rig never sent us). +3 `TestCIVAck` cases.
- **M2 (fixed, option 1 — data-driven range metadata).** Added optional
  `Min`/`Max` to the rigdef `Command` (natural units — Hz for set_freq, watts for
  set_power); `EncodeCommand` (Kenwood padded path) and the CI-V `bcd_freq` path
  reject in-width-but-out-of-range values via a shared `checkCommandRange` →
  `ErrInvalidPaddedValue` (→ API `rig_invalid_value`, no write).
  `ValidateRigDefinition` now rejects a malformed/unenforceable range
  (`validateCommandRange`). Populated both Yaesu rigdefs (`FA/FB` 30k–75M Hz,
  `PC` 5–100 W). Separately, the CI-V `EncodingBCDPower` path now **rejects**
  watts > `ScaleMaxWatts` instead of silently clamping to full-scale level 255.
  Tests: `TestEncodeCommand` range cases (incl. the review's `PC999`),
  `TestEmbeddedIC7300_PowerOverRangeRejected`, `TestValidateRigDefinition` range
  cases.
- **L1 (deferred).** `sets_state` value-compatibility validation — not an active
  bug (shipped IC-7300 pairs are correct) and only reachable once external rigdef
  loading is real. Logged in `docs/backlog.md` (Features/enhancements) with the
  full fix shape.
- **L2 (fixed — clone helper, `Lookup` unchanged).** Added `CloneRigDefinition`
  (deep-copies Commands/States/ModeMappings + nested slices + the Serial `*bool`
  pointers) for callers that need to mutate a looked-up rigdef. `Lookup` stays
  zero-cost on the per-command read path; the foot-gun is opt-in. Test:
  `TestCloneRigDefinition_IsolatesFromGlobal`.
- **L3 (fixed).** Refreshed the stale FT-710-can't-tune wording in `install.md`
  (now lists FT-710/FTdx10/IC-7300 as tested), `config.ts`, and
  `TuneButton.svelte` (generic "rig with no tune support").

Verified: `gofmt`/`go vet` clean; `go build ./...`; `internal/cat`,
`internal/bridge`, `internal/api`, `cmd/smd` pass; `go test -race ./internal/cat`
clean; prettier clean on the touched SPA files.
