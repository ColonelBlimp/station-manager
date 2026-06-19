# Code Review: internal/config

Date: 2026-06-19  
Scope: `internal/config`, its public service surface, config load/write paths, validation, migrations, and direct consumers in `cmd/smd`, `internal/api`, `internal/bridge`, and `internal/ft8`.

## Summary

`internal/config` has strong coverage around defaults, migrations, validation consolidation, `/v1/config` round trips, and the prior June regression fixes. I did not find performance issues in the current hot path: config load/write is startup or operator-write work, and the JSON-based `Clone` is used on rare PUT-style flows.

The main remaining risks are correctness and security issues around the file boundary and validation contract: secret-bearing config files are written world-readable on typical Unix systems, rig model strings are not validated against the embedded rig catalogue before bridge startup, `Update`'s copy contract is only shallow for nested values, and hand-edited unknown JSON keys are silently ignored.

## Findings

### M1 - Security: `WriteJSON` writes secret-bearing config files with mode `0644`

Evidence:
- `internal/config/config.go:604-623` writes `path + ".tmp"` with `os.WriteFile(tmp, data, 0o644)` before renaming it into `config.json`.
- The same config contains SMTP credentials (`internal/config/config.go:141-148`, `internal/types/email.go:34-43`), lookup provider passwords (`internal/types/lookup.go:19-27`), and opaque forwarder credentials such as API keys (`internal/types/forwarder.go:24-32`).
- `cmd/smd/main.go:1117-1141` uses `WriteJSON` for first-run config creation, and `config.Service.Update` uses it for later rewrites (`internal/config/config.go:1325-1344`).

Impact:
On multi-user Unix systems, a newly seeded or rewritten `config.json` is readable by other local users whenever the parent directory permits traversal. The project intentionally keeps these credentials out of the `/v1/config` API (`internal/api/handler_config.go:65-70`, covered by tests), but the filesystem write path still exposes them locally.

Recommendation:
Write new temp files as `0600`, and consider preserving an existing file's stricter mode when replacing it. Add a regression test next to `TestWriteJSON_RoundTrip` asserting the mode of a newly written config. If migration of existing `0644` files is desired, make that explicit and logged rather than silently changing operator-managed files.

### M2 - Correctness: rig model validation does not enforce the documented `cat.Lookup` contract

Evidence:
- `types.RigConfig.Model` is documented as a `cat.Lookup` id (`internal/types/rig.go:37-39`).
- `validateRigs` checks positive IDs, uniqueness, non-empty `Model`, default-rig resolution, and ADIF mode mappings, but never checks `cat.Lookup(rc.Model)` (`internal/config/validate.go:80-123`).
- `Validate` runs `validateBridge(cfg.ActiveBridge())` before `validateRigs`; `ActiveBridge` projects the active rig model into `bridge.cat.driver` (`internal/config/validate.go:55-70`, `internal/config/config.go:305-336`), and `validateBridge` only requires the projected driver string to be non-empty (`internal/config/config.go:1227-1236`).
- The bridge later treats an unknown driver as a permanent startup error (`internal/bridge/pipeline.go:166-174`), with tests proving it exits safely (`internal/bridge/supervisor_test.go:155-220`).

Impact:
A hand-edited config or `/v1/config` candidate can pass the config validator with `rigs[].model = "yaesu-typo"` as long as the string is non-empty. The daemon then starts far enough to construct the bridge and emit a runtime `unknown_driver` bridge error instead of rejecting the invalid config at the single validation boundary that claims values rejected there also fail at `Load`.

Recommendation:
Have `validateRigs` reject any non-empty `Model` that is not present in `cat.Lookup`. At minimum, enforce it for `DefaultRigID` when bridge or FT8 runtime projection depends on it. Add `Load` and `Validate` tests for an unknown model, and a `/v1/config` test if the writable surface can affect active rig model data.

### M3 - Correctness: `Service.Update` claims an aborted update leaves memory untouched, but nested mutations can leak through

Evidence:
- The `Update` comment promises `fn` applies to a copy and that, on `fn` error, "neither the in-memory state nor the file is touched" (`internal/config/config.go:1315-1324`).
- The implementation uses `next := s.Cfg`, which is a shallow value copy (`internal/config/config.go:1333-1338`). Nested slices, maps, and pointers still alias the live config.
- `Snapshot` explicitly documents this shallow-copy hazard and tells mutating callers to use `Clone` (`internal/config/config.go:1271-1281`), but `Update` and `UpdateInMemoryThenPersist` do not.

Impact:
Current production callers mostly assign scalars or replace the whole config (`cmd/smd/main.go:189-192`, `cmd/smd/main.go:806-808`, `internal/api/handler_config.go:253-255`), so I did not find a current caller that triggers the bug. The exported service contract is still unsafe: a future closure that appends to `cfg.Forwarders`, edits `cfg.Rigs[0].ModeMappings`, or mutates another nested value before returning an error or hitting a write failure can change live in-memory config despite `Update` reporting failure.

Recommendation:
Use `s.Cfg.Clone()` as the working value inside `Update`; do the same for `UpdateInMemoryThenPersist` unless its intended contract is explicitly "closure may mutate live nested state." Add a regression test where an `Update` closure mutates a nested field and returns an error, then assert `Snapshot()` is unchanged.

### L1 - Correctness/Test Coverage: unknown JSON keys are silently ignored in hand-edited config

Evidence:
- `Load` uses `json.Unmarshal(data, &cfg)` (`internal/config/config.go:501-503`), so unknown fields are ignored by the standard decoder.
- This package contains many typo guards for values (`internal/config/config.go:1151-1213`, `internal/config/validate.go:125-183`) and the daemon explicitly advertises `config.json` as hand-editable (`cmd/smd/main.go:1117-1124`), but there is no equivalent warning or error for key typos.

Impact:
An operator can misspell a field such as `bridge.enable`, `smtp.timeot_sec`, or `server.max_body_byte`; the daemon will silently use the default or old value. That is especially confusing for hardware and network settings because the current validation error messages only catch malformed values that actually land in typed fields.

Recommendation:
Consider a strict decode path with `json.Decoder.DisallowUnknownFields` after raw migrations, or a warning-only unknown-key linter if forward compatibility is a concern. Add tests for an unknown top-level key and an unknown nested key so the intended behavior is pinned either way.

### L2 - Documentation: package and design comments are stale after config expansion

Evidence:
- `internal/config/doc.go:3-21` still describes the v2 package as "deliberately minimal" milestone-1 config and says runtime `/v1/config` updates are forthcoming, even though `/v1/config` PUT is implemented and the package now owns forwarders, enrichment, SMTP, bridge, FT8, rig profiles, QSL defaults, PSK Reporter, validation, and migrations.
- `internal/config/config.go:312-314` says `RigConfig.Overrides` are not wired, while `ActiveBridge` now projects them at `internal/config/config.go:329-333` and `TestActiveBridge_ProjectsRigSerialOverrides` covers the behavior (`internal/config/config_test.go:1008-1026`).
- `docs/v2-design/config.md:124-128` also says rig audio/serial pieces are not wired, contradicting the current `ActiveBridge`/`ActiveFt8` code and tests (`internal/config/config_test.go:719-749`).

Impact:
The code is clearer than the docs here, but these stale comments send new reviewers and future implementers to the wrong mental model. In particular, the package doc understates the security-sensitive credentials now stored by config, and the rig-profile docs understate current runtime projection behavior.

Recommendation:
Refresh the package doc to describe the current responsibilities and `/v1/config` write path. Update the `ActiveBridge` comment and the design doc status bullets for serial overrides and per-direction audio projection.

## Coverage Notes

Strong current coverage:
- Defaults, first-run writes, loading, migrations, validation collection, bridge timeout validation, lookup validation, forwarder validation, rig profile projection, FT8 display config, QSL defaults, and mode mapping round trips are all covered in `internal/config`, `internal/api`, and `cmd/smd` tests.
- Prior fixes for operator/owner callsign normalization and config clone independence are covered in `internal/config/review_findings_test.go`.
- Adjacent bridge tests prove an unknown driver is fail-safe at runtime, even though config validation should reject it earlier.

Missing focused coverage:
- New `WriteJSON` file mode.
- Unknown `RigConfig.Model` rejection at `Validate`/`Load`.
- Nested mutation rollback behavior for failed `Service.Update`.
- Unknown JSON-key behavior.

## Verification

Commands run:

```text
GOCACHE=/tmp/go-build go test ./internal/config -count=1
GOCACHE=/tmp/go-build go test ./internal/config ./internal/types ./internal/cat -count=1
GOCACHE=/tmp/go-build go test ./cmd/smd -run 'TestLoadConfig|TestEnsureDefaultLogbook|TestSpawnForwarderWorkers' -count=1
GOCACHE=/tmp/go-build go test -race ./internal/config -count=1
GOCACHE=/tmp/go-build go test ./internal/api ./internal/bridge ./internal/ft8 -count=1
GOCACHE=/tmp/go-build go vet ./internal/config ./cmd/smd ./internal/api ./internal/bridge
GOCACHE=/tmp/go-build go test ./cmd/smd -count=1
GOCACHE=/tmp/go-build go test ./internal/config -run 'TestWriteJSON|TestApplyRigProfiles_Errors|TestValidate|TestLoad_' -count=1
GOCACHE=/tmp/go-build go test -race ./cmd/smd -run 'TestLoadConfig|TestEnsureDefaultLogbook|TestSpawnForwarderWorkers' -count=1
GOCACHE=/tmp/go-build go test -race ./internal/config ./cmd/smd -run 'TestService_UpdateAccessorRace|TestEnsureDefaultLogbook|TestLoadConfig' -count=1
```

Result:
- All focused config, cmd/smd, race, and vet checks passed.
- The first sandboxed run of `go test ./internal/api ./internal/bridge ./internal/ft8 -count=1` failed because `httptest` could not bind `[::1]:0` in the sandbox. Rerunning the same command outside the sandbox passed for all three packages.

## Resolution (2026-06-19)

All five findings addressed.

- **M1 (fixed).** `WriteJSON` now writes config 0600 (it holds plaintext SMTP /
  lookup / forwarder secrets) — a legacy 0644 is tightened on the next write, an
  operator's stricter 0400 is preserved, and an explicit `Chmod` defeats a
  permissive umask. Test: `TestWriteJSON_FileMode`.
- **M2 (fixed).** `validateRigs` now rejects a non-empty `Model` that isn't in
  `cat.Lookup` (config already imports cat — no cycle), so a typo'd driver fails
  at the single config boundary instead of becoming a runtime `unknown_driver`
  bridge error. Test: `TestValidateRigs_RejectsUnknownModel`.
- **M3 (fixed).** `Update` and `UpdateInMemoryThenPersist` now work on
  `s.Cfg.Clone()` (deep), so a closure that mutates a nested slice/map and then
  returns an error leaves the live in-memory config untouched. Test:
  `TestService_Update_NestedMutationRollback`.
- **L1 (fixed — option 1, warning-only).** New pure `UnknownKeys([]byte)` walks
  the post-migration document against the `Config` schema (reflective json-tag
  diff, descending struct blocks; slices/maps opaque) and returns dotted paths;
  `cmd/smd` logs each at startup beside the existing `Warnings`. Load behaviour
  is unchanged (forward-compat preserved) — a hand-edit typo is surfaced, not
  fatal. Test: `TestUnknownKeys` (incl. a zero-false-positive check on the
  default config).
- **L2 (fixed).** Refreshed `doc.go` (current responsibilities + the secrets/0600
  note + the Update/Clone write path), the `ActiveBridge` comment (per-rig
  `Overrides` ARE projected), and `config.md`'s `RigConfig` section (runtime
  projection + the new model validation).

Verified: `gofmt`/`go vet` clean; `go build ./...`; `internal/config`,
`cmd/smd`, `internal/api` pass; `go test -race ./internal/config ./cmd/smd`
clean.

## Worktree Note

I did not modify `internal/config` or any production code. The worktree already contained unrelated API/bridge/FT8/type edits and unrelated review documents; this review only adds this file.
