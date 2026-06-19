# internal/types code review - 2026-06-19

## Scope

Fresh review of `internal/types` at `a62fdecb`, approached as a new package
review. I read every file in `internal/types`, its direct tests, the sqlite
adapters that persist `types.Qso`, enrichment/API consumers, config accessors,
and the frontend wire types that mirror the exported DTOs.

Focus areas: correctness, performance, security, test coverage, and
documentation. This is review-only; no source fixes were made.

The worktree was not clean when this review started. Existing unrelated changes
were present in `internal/ft8/servicetx.go`, `internal/pskreporter/service.go`,
and other review artifacts under `docs/reviews`; I left those untouched.

Primary files reviewed:

- `internal/types/*.go`
- `internal/types/ft8_test.go`
- `internal/database/sqlite/adapters/type_to_model.go`
- `internal/database/sqlite/adapters/model_to_type.go`
- `internal/database/sqlite/adapters/adapters_test.go`
- `internal/database/sqlite/api_context.go`
- `internal/lookup/orchestrator.go`
- `internal/api/handler_config.go`
- `internal/api/handler_enrich.go`
- `internal/api/handler_rigs.go`
- `internal/config/config.go`
- `internal/config/validate.go`
- `internal/bridge/pipeline.go`
- `frontend/logging/src/lib/api/config.ts`
- `frontend/logging/src/lib/api/enrichment.ts`
- `frontend/config/src/lib/api/rigs.ts`
- relevant design docs around QSO additional data, rig profiles, and config

## Summary

`internal/types` is intentionally a shared DTO/domain package. The important
package invariant still holds: `go list -deps ./internal/types` shows only the
standard library plus the package itself, with direct imports limited to
`encoding/json` and `time`. That is the right shape for a package imported by
nearly every internal component.

I found no active I/O, network, goroutine, reflection, or unsafe behavior inside
`internal/types` itself. Performance risk inside the package is low: the helper
functions allocate small maps or return value structs, and none of the code is a
hot loop. The most important risks are contract risks caused by exported DTOs
being reused across storage blobs, API responses, and config surfaces.

The main correctness issue is that the documented `omitempty` discipline does
not work for the zero `time.Time` fields on `ContactedStation` and `Country`.
Those zero refresh timestamps are serialized into QSO/contact JSON, including
durable sqlite `additional_data` blobs, even though the comments and adapter
docs say the metadata should be absent or column-only. The rest of the findings
are lower-severity boundary and documentation issues.

## Findings

### M1. Zero refresh timestamps are serialized into blobs despite the documented omit-empty contract

**Area:** correctness / storage shape / API shape / documentation / test coverage  
**Files:** `internal/types/doc.go:11-19`,
`internal/types/qso.go:11-67`,
`internal/types/contacted_station.go:11-52`,
`internal/types/country.go:10-42`,
`internal/database/sqlite/adapters/type_to_model.go:53-56`,
`internal/database/sqlite/adapters/type_to_model.go:86-100`,
`internal/database/sqlite/adapters/model_to_type.go:34-37`,
`internal/database/sqlite/adapters/adapters_test.go:165-194`,
`internal/database/sqlite/adapters/adapters_test.go:268-282`

The package comments rely on a uniform `,omitempty` rule: `types.Qso` and its
embedded structs should marshal only operator-set or enriched data, not empty
noise (`qso.go:11-13`, `doc.go:17-19`). `ContactedStation` repeats that missing
fields unmarshal back to zero values, and `Country` says an unenriched QSO emits
`"country_details":{}` rather than empty keys (`contacted_station.go:11-14`,
`country.go:10-17`).

That is not true for the two refresh timestamps:

- `ContactedStation.LastRefreshedAt time.Time` is tagged
  `json:"last_refreshed_at,omitempty"` (`contacted_station.go:44-52`).
- `Country.LastRefreshedAt time.Time` has the same tag (`country.go:35-42`).

The local Go toolchain's `encoding/json` docs define `omitempty` empty values as
false, numeric zero, nil pointer/interface, and zero-length arrays/slices/maps
or strings. Struct values are not included in that definition. The docs also
show the newer `omitzero` option, which uses `IsZero()` when present. Because
`time.Time` is a struct, a zero timestamp is still marshaled as
`"0001-01-01T00:00:00Z"` with the current tags.

That leaks into durable storage. `QsoTypeToModel` marshals the entire
`types.Qso` into `AdditionalData` (`type_to_model.go:53-56`), and
`ContactedStationTypeToModel` marshals the entire `types.ContactedStation`
before also writing `LastRefreshedAt` to a promoted column
(`type_to_model.go:86-100`). The reverse adapter comment says
`last_refreshed_at` lives only in the column and is cache plumbing, not blob
shape (`model_to_type.go:34-37`), but the forward adapter currently puts it in
the blob whenever the struct is marshaled.

The `Qso` path is a little worse than plain extra metadata. `Qso` embeds
`ContactedStation`, so a minimal QSO can carry a top-level
`last_refreshed_at:"0001-01-01T00:00:00Z"` in its JSON blob. `Qso.CountryDetails`
is a value-typed `Country` with `omitempty`; since structs are not empty for
`omitempty`, the field is serialized, and because `Country.LastRefreshedAt` also
serializes when zero, an unenriched QSO is not just `"country_details":{}`. It
can become a country object containing a meaningless zero refresh timestamp.

Impact:

- The persisted `additional_data` blobs contain cache metadata the comments say
  should be absent or column-only.
- API responses that marshal `types.Qso`, `types.ContactedStation`, or present
  enrichment layers can expose `"0001-01-01T00:00:00Z"` as if it were a real
  refresh value.
- The added JSON noise undermines ADR 0015's "operator-set/enriched data only"
  property and makes tests less able to distinguish meaningful enrichment from
  default struct state.
- Existing adapter tests only assert `AdditionalData` is non-empty; they do not
  assert the blob contract (`adapters_test.go:193`, `adapters_test.go:281`).

Recommendation:

Decide whether `LastRefreshedAt` is wire data or storage metadata. If it should
remain present in public enrichment responses but disappear when zero, change
the tags to `json:"last_refreshed_at,omitzero"`; `go.mod` targets Go 1.26.2 and
the local `encoding/json` supports `omitzero`. If it is column-only metadata,
use `json:"-"` on the storage DTO and expose a narrower API response shape where
the timestamp is deliberately part of the contract. If older Go compatibility
matters, use `*time.Time` plus `omitempty` or custom marshalers instead.

Add tests that pin the intended behavior:

- `json.Marshal(types.ContactedStation{})` does not include
  `last_refreshed_at`.
- `json.Marshal(types.Country{})` does not include `last_refreshed_at`.
- A minimal `types.Qso` additional-data blob has no zero refresh timestamp and,
  if the value-typed country is retained, only the intended empty country shape.
- `ContactedStationTypeToModel` does not duplicate column-only
  `last_refreshed_at` into `AdditionalData` unless that is explicitly intended.

### L1. `/v1/config` reuses full `types.RigConfig` for `default_rig`, blurring the API boundary

**Area:** security boundary / API contract / documentation / test coverage  
**Files:** `internal/types/rig.go:20-86`,
`internal/api/handler_config.go:14-40`,
`internal/api/handler_config.go:117-121`,
`internal/api/handler_config.go:366-410`,
`internal/api/handler_rigs.go:10-20`,
`frontend/logging/src/lib/api/config.ts:201-205`,
`frontend/config/src/lib/api/rigs.ts:34-44`

`ConfigResponse` exposes `DefaultRig` as the full shared `types.RigConfig`
(`handler_config.go:36-40`). On GET, `buildConfigResponse` starts with a bare
ID and then replaces it with the active rig profile from `cfg.Rigs`
(`handler_config.go:366-410`). That means `/v1/config` can include `port`,
`audio`, `overrides`, `mode_mappings`, `ft8_mode`, and `my_rig` whenever those
fields are present on the active rig.

This is not a credential leak, but it is a hardware-topology leak and a contract
ambiguity. The same file's `BridgeInfo` docs say `Port` stays off the wire
because hardware config is not a logging-SPA concern (`handler_config.go:117-121`).
At the same time, the logging frontend's `DefaultRigFields` explicitly includes
`port` (`frontend/logging/src/lib/api/config.ts:201-205`), while the config SPA
has a separate `/v1/rigs` surface that intentionally returns full rig profiles
(`handler_rigs.go:10-20`, `frontend/config/src/lib/api/rigs.ts:34-44`).

The problem is not the current data sensitivity level; the problem is that a
write-oriented config DTO is being reused as a read-oriented logging API DTO.
Future fields added to `types.RigConfig` will automatically appear on
`/v1/config` unless every caller remembers this side effect.

Recommendation:

Make the boundary explicit. If the logging SPA truly needs only ID/model/port,
introduce a narrow `DefaultRigInfo` response type and keep full rig profiles on
`/v1/rigs`. If the broader exposure is intentional, update the comments and add
a response-shape test that asserts exactly which `default_rig` fields are
allowed. That prevents future `types.RigConfig` additions from expanding the
wire surface accidentally.

### L2. Mutable nested fields in shared config DTOs make shallow accessors less defensive than documented

**Area:** correctness / concurrency / test coverage / documentation  
**Files:** `internal/types/forwarder.go:24-42`,
`internal/types/lookup.go:45-51`,
`internal/config/config.go:1442-1457`,
`internal/config/config.go:1459-1484`,
`internal/config/config.go:1610-1630`,
`internal/config/review_findings_test.go:81-118`

This is an adjacent-consumer issue caused by the shape of the shared `types`
DTOs. `ForwarderConfig` contains mutable nested fields: `json.RawMessage`,
`[]string`, and `*RetryConfig` (`forwarder.go:24-42`). `EnrichmentConfig`
contains a mutable `[]LookupConfig` chain (`lookup.go:45-51`).

`config.Service.Snapshot` correctly documents itself as shallow and tells
mutating callers to use `Clone()` (`config.go:1442-1457`). `Clone()` deep-copies
via JSON round trip (`config.go:1459-1484`), and tests cover nested independence
for that path (`review_findings_test.go:81-118`).

`Service.Forwarders()` is documented more strongly: it says callers receive a
defensive copy and cannot mutate the live config slice (`config.go:1610-1613`).
The implementation copies only the outer slice (`config.go:1614-1622`). A caller
cannot replace the live slice header, but it can still mutate shared nested
storage such as `out[0].ActionFilter[0]`, `out[0].Credentials[0]`, or
`out[0].Retry.MaxAttempts`. `Service.Enrichment()` returns the `Lookup` struct
by value and documents that callers should not mutate it (`config.go:1625-1630`);
its `Chain` slice still aliases live config.

Current production callers appear to range and read these values, so this is a
latent contract issue rather than an observed bug. The risk is that a future
caller follows the defensive-copy comment, mutates a returned nested field, and
changes live config outside the service lock, potentially creating a data race
with `Update`.

Recommendation:

Either deep-copy nested fields in these accessors or weaken the comments to make
the read-only contract explicit. The safer path is a small helper that copies
`ForwarderConfig.Credentials`, `ForwarderConfig.ActionFilter`, `RetryConfig`,
and `EnrichmentConfig.Chain`, with tests that mutate the returned values and
assert `s.Cfg` remains unchanged.

### L3. `RigConfig` comments lag the runtime composition and delimiter contract

**Area:** documentation / maintainability  
**Files:** `internal/types/rig.go:15-19`,
`internal/types/rig.go:108-121`,
`internal/bridge/pipeline.go:820-896`,
`internal/bridge/pipeline.go:1007-1035`,
`internal/cat/rigs/icom-ic7300.json:17-20`,
`docs/v2-design/cat-serial-reuse.md:153-154`,
`docs/v2-design/cat-serial-reuse.md:344`

`types.RigConfig` still says the serial composition step lives in a future
`internal/rigconfig` package that will land when the first consumer is built
(`rig.go:15-19`). The runtime composition is now in the bridge:
`buildSerialConfig` layers `RigConfig.Overrides` over the rig definition and
returns `serial.Config` (`pipeline.go:820-896`).

`RigOverrides` also says `LineDelimiter` is a single-character string
(`rig.go:108-121`). The bridge now supports either a single byte or `0xNN` hex
form (`pipeline.go:1007-1035`), and the IC-7300 rig definition uses `"0xFD"`
(`icom-ic7300.json:17-20`).

This stale documentation is not currently breaking runtime behavior, but it
misdirects future maintainers toward a package that does not exist and omits a
supported delimiter form needed by binary CI-V rigs.

Recommendation:

Update `internal/types/rig.go` and the remaining design-doc references to say
the bridge currently owns runtime composition, and document
`line_delimiter` as "single byte or 0xNN hex form." If the long-term plan is
still to extract `internal/rigconfig`, make that an explicit future refactor
rather than describing it as current ownership.

## Security Review

`internal/types` itself does not perform I/O, parse untrusted input, run
commands, or hold global mutable state. The package is mostly passive data.

Secret-bearing DTOs exist by design:

- `types.SmtpConfig.Password`
- `types.LookupConfig.Password`
- `types.ForwarderConfig.Credentials`

The reviewed API surfaces do not appear to expose SMTP credentials:
`ConfigResponse.Mailer` is a narrowed projection and the tests assert SMTP
password/host/from are absent from `/v1/config`
(`handler_config_test.go:723-772`). Lookup and forwarder credentials are not
part of the reviewed `/v1/config` response shape. The main security-adjacent
concern is the broad `DefaultRig types.RigConfig` reuse described in L1: it
does not expose secrets, but it can expose device paths and audio device names
to a broader API surface than the comments imply.

## Performance Review

No performance-sensitive code lives directly in `internal/types`.

The helper functions in `ft8.go` are cheap and appropriately defensive:

- `DefaultFt8Frequencies()` returns a fresh map, avoiding shared mutable package
  state.
- `ResolveFt8Frequencies()` copies defaults and overlays only positive
  overrides.
- `ResolveFt8Display()` clamps `HistoryMax` and returns a value struct.

The `time.Time` serialization issue in M1 has a small storage and wire-size
cost, but the correctness/documentation impact is the real concern.

## Test Coverage Review

Direct `internal/types` tests are concentrated on FT8 resolver helpers:

- `TestResolveFt8Display`
- `TestFt8FeedModeValid`
- `TestResolveFt8CallerAnswerMode`
- `TestResolveFt8MaxRepeats`
- `TestResolveFt8Frequencies`

Statement coverage reports 97.3%, but that number is misleading for this
package: most DTO contract risk is in struct tags, JSON shape, and consumers
that marshal the types, not in executable statements.

Important missing coverage:

- JSON shape tests for `ContactedStation`, `Country`, and minimal `Qso`,
  especially zero `LastRefreshedAt` behavior.
- Adapter blob-shape tests that inspect `AdditionalData`, not just
  `assert.NotEmpty`.
- API response-shape tests for `/v1/config default_rig` so `types.RigConfig`
  additions cannot silently widen the logging API.
- Accessor aliasing tests for `Forwarders()` and `Enrichment()` if those methods
  continue to claim defensive-copy behavior.
- Documentation-backed tests for `RigOverrides.LineDelimiter` accepted forms if
  composition remains outside `internal/types`.

## Verification

Commands run:

- `go list -deps ./internal/types` - passed; stdlib-only package dependency
  invariant holds.
- `GOCACHE=/tmp/go-build go test ./internal/types` - passed.
- `GOCACHE=/tmp/go-build go test -race ./internal/types` - passed.
- `GOCACHE=/tmp/go-build go vet ./internal/types` - passed.
- `GOCACHE=/tmp/go-build go test -cover ./internal/types` - passed,
  `coverage: 97.3% of statements`.
- `GOCACHE=/tmp/go-build go test ./internal/config ./internal/api ./internal/database/sqlite/adapters ./internal/lookup ./internal/ft8` -
  initial sandbox run passed `internal/config`, `internal/database/sqlite/adapters`,
  and `internal/lookup`, then failed in `internal/api` and `internal/ft8` because
  `httptest.NewServer` could not bind localhost under the sandbox.
- Escalated rerun:
  `GOCACHE=/tmp/go-build go test ./internal/api ./internal/ft8` - passed.
- `GOCACHE=/tmp/go-build go test -race ./internal/config ./internal/api ./internal/database/sqlite/adapters ./internal/lookup` -
  initial sandbox run passed `internal/config`, `internal/database/sqlite/adapters`,
  and `internal/lookup`, then failed in `internal/api` for the same localhost
  listener restriction.
- Escalated rerun:
  `GOCACHE=/tmp/go-build go test -race ./internal/api ./internal/ft8` - passed.
- `GOCACHE=/tmp/go-build go test ./internal/database/sqlite ./internal/qsoservice ./internal/adif` -
  passed.
- `GOCACHE=/tmp/go-build go test -race ./internal/database/sqlite ./internal/qsoservice ./internal/adif` -
  passed.

I did not run `go test ./...`; this was a focused package review with adjacent
packages selected for `internal/types` storage, API, config, and QSO/ADIF
contracts.

## Resolution (2026-06-19)

All four findings fixed. Operator decisions: M1 → column-only (`json:"-"`); L1 →
a narrow `DefaultRigInfo` read DTO; L2 → deep-copy the nested fields.

- **M1 (fixed — correctness / storage shape).** `ContactedStation.LastRefreshedAt`
  and `Country.LastRefreshedAt` are now `json:"-"`. They were the one
  contract-violating exception to ADR 0015's omit-empty discipline: as
  `time.Time` structs they fell outside `encoding/json`'s `omitempty` (which
  ignores structs), so a zero value serialized as `"0001-01-01T00:00:00Z"` into
  the durable `additional_data` blob (and into `country_details` inside QSO
  blobs) and onto enrichment API responses. The field is cache plumbing: the DB
  `last_refreshed_at` column is authoritative on read-back (the adapters overlay
  it over the blob) and the in-memory struct field still feeds the orchestrator's
  three-state freshness policy — `json:"-"` removes only the dead, noisy blob/wire
  copy. Struct-doc comments on `Qso`, `ContactedStation`, `Country` updated; the
  `model_to_type.go` "column-only" comment now notes it's enforced by the tag.
  The unused TS mirror fields (`enrichment.ts` `EnrichmentCountry` /
  `EnrichmentStation`) were dropped with a why-note. Tests:
  `types.TestContactedStation_LastRefreshedAtNeverMarshaled`,
  `TestCountry_LastRefreshedAtNeverMarshaled`,
  `TestQso_BlobShapeHasNoZeroRefreshTimestamp`, and adapter
  `TestContactedStationTypeToModel_LastRefreshedAtColumnOnly` (column populated,
  blob clean, zero→NULL).
- **L1 (fixed — API boundary).** `/v1/config`'s `default_rig` was the full
  write-oriented `types.RigConfig`, leaking `port`/`audio`/`overrides`/
  `mode_mappings`/`ft8_mode`/`my_rig` to the logging SPA and set up to
  auto-widen on every future `RigConfig` field. Replaced with a narrow read-only
  `DefaultRigInfo{ID, Model, Port}` — exactly what the logging SPA's
  `DefaultRigFields` already declared (so no frontend change needed). Full rig
  profiles stay on the config SPA's `/v1/rigs`. The PUT path already ignored
  `req.DefaultRig` (default-rig selection is `PUT /v1/rigs` `default_rig_id`), so
  making it read-only changes no write behaviour; the stale "honours DefaultRig.ID"
  doc was corrected. Test: `api.TestHandleGetConfig_DefaultRigNarrowShape`
  (asserts the exact `default_rig` key set and that the wider values don't appear
  anywhere in the payload).
- **L2 (fixed — accessor contract / concurrency).** `Service.Forwarders()` and
  `Service.Enrichment()` documented defensive copies but only copied the outer
  slice / returned the struct by value — `Credentials`, `ActionFilter`, `Retry`,
  and the enrichment `Chain` still aliased live config, so a future caller
  following the contract could mutate config outside the lock and race `Update`.
  `Forwarders()` now deep-copies each entry via `cloneForwarder` (clones
  `Credentials`/`ActionFilter`/`Retry`); `Enrichment()` clones the `Chain` slice
  (`LookupConfig` is all scalars, so a value copy per element suffices). Tests:
  `config.TestForwarders_DeepCopyIsolatesNestedFields`,
  `TestEnrichment_DeepCopyIsolatesChain`.
- **L3 (fixed — docs).** `types.RigConfig`'s top comment pointed at the
  never-built `internal/rigconfig`; updated to name `internal/rigserial` (which
  landed in the serial review) as the current composition owner, called by the
  bridge and `cmd/catcli`. The `RigOverrides.LineDelimiter` "single byte OR
  0xNN" wording was already corrected in the serial review. The Tier-2 design
  brief `docs/v2-design/cat-serial-reuse.md` was deliberately left frozen per the
  doc-map rule (historical reasoning trail, not freshened to current state).

Verified: `gofmt`/`go vet` clean; `internal/types`, `internal/config`,
`internal/database/sqlite/adapters`, `internal/api`, `internal/lookup`,
`internal/qsoservice`, `internal/database/sqlite` build + pass; `-race` clean on
types / config / adapters / api; SPA `npm run check` clean (0 errors);
`CGO_ENABLED=0 go build ./...` succeeds.
