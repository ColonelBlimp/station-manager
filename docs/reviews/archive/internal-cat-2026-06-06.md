# internal/cat code review (2026-06-06)

## Resolution (2026-06-06)

- **M1 (empty `Cmd` passes validation)** — FIXED. `ValidateRigDefinition` now
  rejects an empty command template for every command; validation test added.
- **M2 (TX/playback commands exposable by a future rigdef)** — FIXED.
  `ValidateRigDefinition` now rejects `exposed` on `tx_on`/`tx_off`/`PLAYBACK`;
  negative + positive (non-exposed allowed) tests added. ADR 0027 updated to
  record the invariant is now schema-enforced.
- **L3 (stale FT-710 wording/tests)** — FIXED. `BridgeInfo.Ops`/`Tune` comments
  no longer cite the FT-710 as the empty-ops/no-tune example; the API-level
  `TestBridgeInfoFor_Tune` now covers both shipped Yaesu rigs.
- **L1 (decode-facing validation strictness)** — NOT actioned (deferred; the
  `Decode` path degrades gracefully and shipped rigdefs are covered).
- **L2 (`Lookup` returns mutable shared-backing definitions)** — DEFERRED (same
  call as the 2026-06-05 review; gated on `RegisterExternalDir` becoming a real
  loader / the first real mutator).

## Scope

Reviewed the current `internal/cat` package and the direct bridge/API consumers
that depend on its rig-definition contract:

- `internal/cat/codec.go`, `commands.go`, `rig.go`, `rigdb.go`, `validate.go`
- `internal/cat/rigs/yaesu-ftdx10.json`, `internal/cat/rigs/yaesu-ft710.json`
- CAT decode/encode/command/rigdb/validation tests
- Direct consumers in `internal/bridge/{pipeline,command,tune}.go`,
  `internal/api/{handler_config,handler_rig_command,handler_rig_tune}.go`,
  and `internal/serial/serial.go` where empty CAT writes matter
- Existing CAT/bridge reviews and ADR context for the command and tune paths

Not reviewed in depth: frontend rendering, live hardware behavior, or the
operator's bench-confirmation notes beyond the claims present in repository
files.

## Summary

The package is in substantially better shape than the 2026-06-05 review target:
padding validation, load-time rigdef validation, FT-710 write/tune support, and
encodeability-based tune capability are all present. Current shipped Yaesu
definitions are well covered and the focused and full test suites pass.

Remaining risk is mostly about the rigdef schema boundary. A future embedded
rigdef or the deferred external-dir loader can still satisfy `ValidateRigDefinition`
while violating write-path safety assumptions that the bridge treats as already
proven.

Headline findings: 0 critical, 0 high, 2 medium, 3 low.

## M - Medium

### M1. Empty command templates pass validation and can satisfy safety dry-runs

Files:

- `internal/cat/validate.go:37`
- `internal/cat/validate.go:54`
- `internal/cat/codec.go:118`
- `internal/cat/codec.go:122`
- `internal/bridge/tune.go:96`
- `internal/bridge/tune.go:105`
- `internal/bridge/tune.go:355`
- `internal/bridge/tune.go:393`
- `internal/serial/serial.go:209`

`ValidateRigDefinition` requires command names to be non-empty and validates
format verbs for value-bearing templates, but it never rejects `Command.Cmd == ""`.
For a valueless command, low-level `Encode` then returns an empty byte slice with
no error. That is observable as "command exists and encoded successfully" to every
caller that treats encode success as a capability proof.

The sharp edge is the tune path. `TuneSupported` and `StartTune` dry-run
`cat.Encode(def, "tx_off")` to prove the safety-critical unkey exists. If a future
rigdef accidentally defines `{"name":"tx_off","cmd":""}`, the dry-run succeeds.
`releaseTune` later writes the empty unkey bytes, and the real serial client treats
empty writes as a no-op success. The daemon can then publish `active:false` after
restore writes even though no `TX0;`-style unkey was sent.

Current shipped rigdefs are protected by explicit tests that assert `tx_on` and
`tx_off` encode to `TX1;`/`TX0;`, so this is not a live FTdx10/FT-710 bug. It is a
schema hole in the exact validator that future rigdefs and the planned external
loader are expected to trust.

Suggested direction: reject empty `Cmd` in `ValidateRigDefinition` for every
command, or at least for every command that can be reached by `EncodeCommand`,
`TuneSupported`, `StartTune`, `INIT`, or `READ`. Add a validation test for an empty
command template and a tune negative test with `tx_off` present but empty.

### M2. TX-capable command names are kept safe by current tests, not by the rigdef schema

Files:

- `internal/cat/rig.go:69`
- `internal/cat/commands.go:74`
- `internal/cat/commands.go:80`
- `internal/cat/commands_test.go:207`
- `internal/bridge/command.go:55`
- `internal/bridge/tune.go:24`
- `internal/cat/validate.go:37`

The safety model says `tx_on`, `tx_off`, and other TX-capable commands must not be
`Exposed`; only the tune controller may key TX. The current Yaesu rigdefs follow
that, and `TestTxCommands_NotExposed` pins it for both shipped drivers.

The schema does not enforce the rule. A future rigdef can set
`{"name":"tx_on","cmd":"TX1;","exposed":true}` and pass `ValidateRigDefinition`.
`ExposedCommands` would advertise it, `EncodeCommand` would accept it as a
valueless exposed command, and `SendCommands` would write it through the generic
`POST /v1/rig/command` path. That bypasses the tune controller's guaranteed-stop
machinery: no mode/power snapshot, no auto-off timer, no release-on-disconnect
state, and no tune-state event.

Suggested direction: make "not externally exposed" a validation invariant for
known unsafe command names used by this project, at least `tx_on`, `tx_off`, and
`PLAYBACK`. Keep the current tests, but move the invariant into
`ValidateRigDefinition` so a future embedded or external rigdef cannot publish a
TX op by typo.

## L - Low

### L1. Decode-facing rigdef typos still load and then disappear silently at runtime

Files:

- `internal/cat/validate.go:71`
- `internal/cat/validate.go:73`
- `internal/cat/codec.go:82`
- `internal/cat/codec.go:95`
- `internal/bridge/pipeline.go:587`
- `internal/bridge/pipeline.go:591`

The validator now catches encode-critical issues, but it still allows several
decode-facing typos that the runtime handles by skipping or publishing unusable
keys: empty state prefixes, empty marker tags, duplicate value-mapping keys, and
empty mapping values. `Decode` intentionally preserves v1's permissive behavior,
including empty-string fallback for unmapped values, and the bridge drops unknown
or empty fields later.

That permissiveness is fine for noisy rig input. It is weaker as a load-time
contract for hand-authored JSON. A misspelled or empty tag on `MAINMODE`, `TXPWR`,
or `IDENTITY` would pass validation and leave downstream features broken or
identity-gated forever without a rigdb load failure.

Suggested direction: keep `Decode` permissive, but make rigdef validation stricter:
non-empty `State.Prefix`, non-empty `Marker.Tag`, unique value-mapping keys within
a marker, and non-empty mapping values for tags that feed UI/config surfaces.

### L2. `Lookup` still returns mutable definitions sharing global backing storage

Files:

- `internal/cat/rigdb.go:17`
- `internal/cat/rigdb.go:64`
- `internal/cat/rig.go:22`
- `internal/cat/rig.go:23`
- `internal/cat/rig.go:24`

This is the known residual from the 2026-06-05 review. `Lookup` returns
`RigDefinition` by value, but slices and maps inside it still point at the
process-global `rigDB` entry. A caller can mutate `def.Commands`, nested marker
maps, or `def.ModeMappings` and corrupt later lookups.

Current callers appear read-only and `go test -race ./internal/cat` is clean. The
risk grows when `RegisterExternalDir` becomes a real loader, because merging
operator overrides and constructing test variants are exactly where accidental
mutation tends to happen.

Suggested direction: add a `CloneRigDefinition`/`RigDefinition.Clone` helper and
use it at mutation boundaries. Deep-copy-on-every-`Lookup` is optional, but the
package should expose a safe copy path before external rigdefs land.

### L3. FT-710 tune capability is correct in code, but nearby API comments/tests still describe the old state

Files:

- `internal/cat/rigs/yaesu-ft710.json:7`
- `internal/bridge/tune_test.go:82`
- `internal/api/handler_config.go:96`
- `internal/api/handler_config.go:102`
- `internal/api/handler_rig_tune_test.go:75`

The FT-710 rigdef now carries `tx_on`/`tx_off`, `TuneSupported(ft710)` is expected
to be true, and the rig description says live tune was confirmed on 2026-06-06.
That matches the current bridge tests.

Some API-facing comments still use the pre-update wording: `BridgeInfo.Ops` and
`BridgeInfo.Tune` comments cite "the FT-710 today" as the example for empty ops or
no tune. The API config test also pins only FTdx10 tune advertisement, while the
bridge test now covers FT-710.

This is documentation/test drift, not a behavior bug. It matters because these
comments are the wire-contract explanation for the SPA surface, and FT-710 support
changed recently.

Suggested direction: update the stale `BridgeInfo` comments and add an API-level
`bridgeInfoFor` assertion that FT-710 advertises `Tune == true` and the expected
ops, matching the bridge-level tests.

## Test Results

Passed:

```text
go test ./internal/cat ./internal/bridge ./internal/api
go test -race ./internal/cat
go test ./...
```

## Positive Notes

- `internal/cat` remains a pure codec/rigdef package with no serial dependency.
- The v1 equivalence tests continue to pin permissive decode behavior explicitly.
- `EncodeCommand` now validates padded numeric fields before bridge/API writes.
- Embedded rigdefs are validated at load, and shipped FTdx10/FT-710 command
  coverage is strong.
- The tune support check now proves encodeability rather than command-name
  presence.
