# `internal/lookup` - code review (2026-06-04)

## Scope

Read-only review of the `internal/lookup` package, focused on
enrichment correctness, cache refresh semantics, provider lifecycle,
JSON/API contract, and stale-data behavior.

Covered:

- `internal/lookup/lookup.go`
- `internal/lookup/orchestrator.go`
- `internal/lookup/hamnut/*`
- `internal/lookup/qrz/*`
- `internal/lookup/refresher/*`
- `internal/lookup/*_test.go`

Context checked:

- API boundary: `internal/api/handler_enrich.go`,
  `internal/api/handler_enrich_test.go`
- Enrichment cache persistence:
  `internal/database/sqlite/api_context.go`
- Public response/types boundary: `internal/types/country.go`,
  `internal/types/contacted_station.go`,
  `frontend/logging/src/lib/api/enrichment.ts`
- Design docs: `docs/v2-design/api.md`,
  `docs/v2-design/enrichment.md`

No production code changes were applied during this review.

## Headline verdict

The package has a solid shape: the orchestrator is deliberately
panic-safe, the country/station split is clear, and the async refresher
has thoughtful lifecycle coverage. The main risks are at the boundaries
where lookup data becomes durable cache or public JSON. The highest
impact issue is that the operator's force-refresh path still persists
station data through a partial merge, so stale fields can survive the
very action meant to clear wrong cache data.

Headline counts: **0 Critical**, **2 High**, **3 Medium**, **3 Low**.

## High findings

### H1. Force refresh does not actually replace contacted-station rows

**Files:**

- `internal/lookup/orchestrator.go:165`
- `internal/lookup/orchestrator.go:242`
- `internal/database/sqlite/api_context.go:1039`
- `internal/database/sqlite/api_context.go:1072`
- `internal/database/sqlite/api_context.go:1116`
- `internal/lookup/orchestrator_test.go:1047`

`EnrichRefresh` is documented as the operator's cache-bypass escape
valve: it skips both cache reads, calls upstream, and writes back on
success "overwriting any existing row." The station writeback path,
however, still calls `UpsertContactedStationWithContext`, whose conflict
policy is non-empty-incoming-wins and empty-incoming-keeps-existing.

That merge policy is appropriate for QSO-submit-derived partial station
snapshots, but it is wrong for an upstream refresh. If QRZ used to
return `email`, `web`, `qth`, or `gridsquare` and a later force refresh
returns that field empty, the response for the force-refresh call shows
the fresh upstream value, but the persisted cache row keeps the old
non-empty value. The next normal enrichment can then return the stale
field again.

The existing force-refresh test only proves that non-empty replacement
fields win. It does not cover a field that should be cleared.

Suggested fix:

Split contacted-station persistence by intent:

- Keep the current partial merge helper for QSO-submit-derived snapshots.
- Add a replace/refresh helper for callsign-provider writes, or add an
  explicit mode parameter that lets provider refresh clear fields.
- Use the replace mode for callsign-provider refresh writes: force
  refresh, stale async station refresh, and any rare insert race after a
  normal cold miss.

Add regression coverage where an existing station row has `Email`,
`Web`, `QTH`, and `Gridsquare`, the provider refresh omits one or more
of those fields, and the persisted row is verified to clear them.

### H2. Empty and cold-miss responses can leak bogus zero timestamps

**Files:**

- `internal/lookup/orchestrator.go:54`
- `internal/lookup/orchestrator.go:237`
- `internal/lookup/orchestrator.go:242`
- `internal/types/country.go:42`
- `internal/types/contacted_station.go:51`
- `internal/database/sqlite/api_context.go:1007`
- `internal/database/sqlite/api_context.go:1079`
- `internal/database/sqlite/api_context.go:1093`
- `frontend/logging/src/lib/api/enrichment.ts:63`

`lookup.Result` stores `Country` and `Station` as value-typed structs
tagged `omitempty`. In Go's JSON encoding, `omitempty` does not omit a
zero-value struct, and `time.Time` fields are also structs. Both nested
types carry `LastRefreshedAt time.Time`.

Two bad response shapes follow from that:

- A no-data result can still serialize `country` and `station` instead
  of omitting them, even though the SPA-side contract and tests describe
  the always-200 failure case as `source=none` with no country/station
  objects.
- On a cold miss, the orchestrator writes the fresh provider result to
  the DB, but the timestamp is set inside the DB helper. The response
  still holds the provider struct with zero `LastRefreshedAt`, so it can
  emit `last_refreshed_at` as the zero timestamp instead of the actual
  refresh time or omitting it.

Why it matters:

- The public JSON shape does not match the documented frontend contract.
- Operators or clients can see `0001-01-01T00:00:00Z` timestamps on fresh
  successful lookups.
- A frontend that treats the presence of `country` as displayable data
  can render blank enrichment panels on `source=none`.

Suggested fix:

Represent optional result layers as pointers, or add a custom
`MarshalJSON` for `lookup.Result` that omits country/station when
`SourceNone` or when the layer is empty. Also make the write path return
or assign the refresh timestamp used for the DB write so cold-miss
responses carry a real `last_refreshed_at` when a layer is present.

Add API-level JSON tests for:

- all providers down: no `country`, no `station`, source values both
  `none`;
- cold country/station miss: no zero `last_refreshed_at` in the response;
- cache hit: populated `last_refreshed_at` remains present.

## Medium findings

### M1. QRZ session expiry has no recovery path

**Files:**

- `internal/lookup/qrz/internal.go:19`
- `internal/lookup/qrz/internal.go:108`
- `internal/lookup/qrz/service.go:252`
- `internal/lookup/orchestrator.go:460`

The QRZ provider fetches a session key during `Initialize`, stores it,
and reuses it for all lookups. The code comments acknowledge that QRZ
session keys can expire. When a lookup later receives a session error
such as `Session expired`, `unmarshalResponse` returns a normal error,
and the orchestrator logs/falls through to the next provider. Nothing
tries to fetch a new session key.

For a long-running daemon, this can permanently remove QRZ from the
chain until restart. If QRZ is the only station provider, station
enrichment quietly degrades to cache-only or source `none` after expiry.

Suggested fix:

Teach `LookupWithContext` to classify authentication/session errors,
refresh the session key under a mutex or singleflight guard, and retry
the lookup once. Avoid retry loops for credential failures. Add tests
for `Session expired` followed by successful session refresh and lookup.

### M2. QRZ soft-disable contradicts its own lookup contract

**Files:**

- `internal/lookup/qrz/service.go:84`
- `internal/lookup/qrz/service.go:133`
- `internal/lookup/qrz/service.go:152`
- `internal/lookup/qrz/service.go:162`
- `internal/lookup/qrz/service.go:196`
- `internal/lookup/qrz/service.go:204`

`Initialize` says that if QRZ session-key fetch fails, the service is
marked disabled and any later lookup short-circuits. The implementation
sets `Config.Enabled=false` and returns from the `sync.Once` closure
before `isInitialized.Store(true)`.

`LookupWithContext` checks `isInitialized` before it checks
`Config.Enabled`, so a direct caller after soft-disable gets `service is
not initialized` instead of the disabled sentinel described by the
provider contract.

The production `cmd/smd` path currently skips the provider after
`Initialize` flips `Enabled=false`, so this is mostly a lifecycle/API
contract bug today. It becomes more important if config reload, manual
provider retry, or direct service use is added.

Suggested fix:

After a deliberate soft-disable, still mark the service initialized, or
move the disabled short-circuit before the initialized guard when config
is available. Also consider replacing `sync.Once` for initialization
paths that are expected to retry after transient startup failures.

### M3. Disabled enrichment pipeline bypasses input validation and loses the callsign

**Files:**

- `internal/api/handler_enrich.go:15`
- `internal/api/handler_enrich.go:53`
- `internal/api/handler_enrich.go:62`
- `internal/api/handler_enrich.go:68`
- `internal/api/handler_enrich_test.go:199`

When `s.enrich == nil`, `handleEnrichCallsign` returns `200 OK` before
reading or validating the `call` query parameter. It also calls
`emptyEnrichmentResult("")`, so a request like
`/v1/enrich/callsign?call=M0CMC` returns `callsign: ""`.

This contradicts the handler's documented contract that malformed input
is the only non-200 path, and it makes the disabled-provider response
less useful to the SPA than the helper already supports.

Suggested fix:

Normalize and validate `call` before the nil-orchestrator branch. If the
orchestrator is absent after validation, return
`emptyEnrichmentResult(callsign)`.

Add tests for:

- nil orchestrator plus valid call preserves `callsign`;
- nil orchestrator plus missing/invalid call still returns 400.

## Low findings

### L1. `IsEmpty` ignores several substantive station fields

**Files:**

- `internal/lookup/orchestrator.go:81`
- `internal/lookup/orchestrator.go:87`
- `internal/types/contacted_station.go:19`
- `internal/lookup/orchestrator_test.go:127`

`IsEmpty` decides whether a callsign-provider result is worth accepting.
It counts name, QTH, grid, email, web, address, and contacted operator,
and intentionally ignores call plus country-owned fields. It does not
count other non-country station fields such as `Lat`, `Lon`, `Age`,
`Altitude`, `EqCall`, `Iota`, `Sig`, `SigInfo`, or `WwffRef`.

If a current or future provider returns only one of those fields, the
orchestrator treats the result as empty and advances to the next
provider or returns `station_source=none`.

Suggested fix:

Define the substantive field set once as "all callsign-provider-owned
fields except call and hamnut-owned country fields", and extend
`IsEmpty` plus its table tests accordingly.

### L2. Provider source names drift between code, docs, and frontend types

**Files:**

- `internal/lookup/lookup.go:37`
- `internal/lookup/orchestrator.go:18`
- `internal/lookup/orchestrator.go:452`
- `internal/types/services.go:15`
- `docs/v2-design/api.md:553`
- `frontend/logging/src/lib/api/enrichment.ts:85`

The provider interface comment says `Name()` returns short lowercase
source labels such as `hamnut` and `qrz`, and the API docs list
`station_source` values like `qrz`, `hamqth`, and `qrzcq`. The concrete
QRZ service returns the DI/config service name `qrzlookupservice`, and
frontend tests already expect that value. The frontend type for
`country_source` also still says `"hamnut" | "cache" | "none"`, while
the daemon emits `"country_table"` for cache hits.

The current UI mostly treats station source as a string, so this is not
an immediate runtime failure, but it is a public contract drift that
will keep leaking into docs, tests, and future providers.

Suggested fix:

Either update the public contract to the service-name values everywhere,
or split provider identity into separate concepts:

- DI/config service name, for wiring;
- public source label, for API responses and operator-facing text.

Then pin the chosen values in daemon API tests and frontend type tests.

### L3. Orchestrator does not normalize callsigns at its own boundary

**Files:**

- `internal/lookup/orchestrator.go:178`
- `internal/api/handler_enrich.go:62`
- `internal/database/sqlite/api_context.go:714`
- `internal/database/sqlite/api_context.go:1058`

The HTTP handler uppercases callsigns before calling the orchestrator,
but `Orchestrator.enrich` itself only trims whitespace. Direct package
callers can therefore query or write lower/mixed-case callsigns. The
station cache fetch is an exact equality lookup, and the contacted
station upsert preserves the incoming call's case.

Today the API path hides this, but the package contract does not state
that callers must uppercase first.

Suggested fix:

Normalize to uppercase inside `Orchestrator.enrich` after trimming, and
add a package-level test that lower-case input still hits/writes the
canonical station row.

## Verification

Attempted:

```sh
go test ./internal/lookup/...
GOCACHE=/tmp/station-manager-go-cache go test ./internal/lookup/...
```

The first command failed because the sandbox cannot write to
`/home/mveary/.cache/go-build`. The second kept the Go build cache in
`/tmp`, but the package build is currently blocked by unrelated dirty
worktree state:

```text
internal/types/bridge.go:31:7: undefined: BridgeTuneConfig
```

I did not modify the unrelated bridge/config worktree changes.

## Resolution — Batch B (2026-06-04)

All eight findings were independently re-verified against the source before any
change; **two were overstated** and recalibrated:

- **H2** — the claimed visible `0001-01-01T00:00:00Z` timestamp does NOT occur:
  `LastRefreshedAt` is `omitempty` on a `time.Time`, and a zero `time.Time` is
  omitted, so the wire never carries a bogus timestamp. The real (lesser)
  defects are empty `country:{}`/`station:{}` objects on `source=none` and a
  *missing* `last_refreshed_at` on a cold-miss success — cosmetic for the
  current SPA (which keys display on `*_source`, not object presence).
  Effectively Medium, not High.
- **L1** — no CURRENT provider can trigger the `IsEmpty` gap (QRZ only ever
  populates `Lat`/`Lon`/`EqCall` among the ignored set, never in isolation from
  counted fields). A latent guard for a future provider, not a live bug.

Also confirmed beyond the review: **H1's defective upsert is shared by the
async stale-station-refresh path**, not just force-refresh — a complete fix
must cover both.

**Batch B landed — the three cheap, decision-free fixes** (`go test
./internal/lookup/... ./internal/api ./internal/qsoservice`, `go vet`, `gofmt`
clean):

- **M2 — fixed.** `qrz.Service.Initialize`'s soft-disable path no longer
  `return`s before `isInitialized.Store(true)` — it falls through, so a
  soft-disabled service is "initialized but disabled" and `LookupWithContext`
  returns the documented disabled sentinel instead of "service is not
  initialized." Test: `TestInitialize_SessionKeyFailureDisablesService`
  extended.
- **M3 — fixed.** `handleEnrichCallsign` validates `call` BEFORE the
  nil-orchestrator branch and echoes the validated callsign in the empty
  result. Tests: `TestEnrich_NilOrchestrator_Returns200WithEmpty` extended +
  `TestEnrich_NilOrchestrator_InvalidCallStill400`.
- **L3 — fixed.** `Orchestrator.enrich` uppercases the callsign at its boundary
  (after trim), so a direct package caller with lower/mixed case hits the
  canonical cache row. Test: `TestEnrich_LowercaseInput_HitsCanonicalRow`.

**Remaining (deferred — each needs a decision):**

- **H1** — force-refresh + async-refresh keep stale fields → a replace/clear-mode
  write path (route both provider-refresh writes through it; keep merge for
  QSO-submit snapshots).
- **H2** — pointer-ise `Result.Country`/`Station` (or add `MarshalJSON`) so
  `source=none` omits the layers; assign the cold-miss write timestamp back.
- **M1** — classify QRZ session errors, refresh the key once under a guard,
  retry the lookup once.
- **L2** — fix the SPA `country_source` union (`'country_table'`, not
  `'cache'`) + stale `api.md`; optionally split DI-name vs public source label.
- **L1** — defensive `IsEmpty` field-set extension for when a non-QRZ provider
  lands.

## Resolution — Batch D (2026-06-04)

The two cheap, decision-free contract-hygiene fixes landed (`go test
./internal/lookup/...`, `go vet`, `gofmt`, and SPA `check`/`lint`/`format` all
clean):

- **L1 — fixed.** `IsEmpty` now tests every station-provider-owned field —
  added `Age`, `Altitude`, `EqCall`, `Iota`, `IotaIslandId`, `Lat`, `Lon`,
  `Sig`, `SigInfo`, `WwffRef` to the original name/QTH/grid/email/web/address/
  contacted-op set — still excluding `Call`, the hamnut-owned country fields
  (stripped by `FilterToCallsignFields`), and storage metadata (`CSID`,
  `LastRefreshedAt`). `TestIsEmpty` extended with per-field cases.
- **L2 — fixed (cheap path).** Corrected the SPA's wrong `country_source` union
  (`'cache'` → `'country_table'`, the value the daemon actually emits) and the
  `station_source` comment (`"contacted_station"` for a cache hit, not
  "cache"); fixed the stale `api.md` `station_source` example/list (`"qrz"` →
  `"qrzlookupservice"`); and corrected the `Provider.Name()` doc (`lookup.go`)
  plus the orchestrator Source comment, both of which claimed `Name()` returns
  `"qrz"` when it returns the DI service name. No SPA consumer compared
  `country_source` to `'cache'`, so the union change is svelte-check-clean. The
  full DI-name vs public-label split is deferred (the M option — a decision,
  not hygiene).

**Remaining: Batch C only** — H1 (force-refresh + async-refresh replace-mode),
H2 (result-shape pointer/MarshalJSON + cold-miss timestamp), M1 (QRZ session
re-auth). Each needs a design decision; see the action plan.

## Resolution — Batch C (2026-06-04)

The three decision-bearing fixes landed (operator: "go with your
recommendations"). `go test ./internal/lookup/... ./internal/database/sqlite
./internal/api ./internal/qsoservice`, `go test -race ./internal/lookup/qrz`,
`go vet`, `gofmt`, and SPA `check` are clean. **This closes the review — all 8
findings resolved.**

- **H1 — fixed.** Added `ReplaceContactedStationWithContext` (a shared
  `writeContactedStation(merge bool)` backing both it and the merge `Upsert`):
  it overwrites all columns and clears fields the incoming leaves empty. The
  orchestrator's cold-miss/force-refresh write AND the async stale-refresh
  write now use Replace; the QSO-submit partial snapshot keeps the merge
  Upsert. Test: `TestEnrichRefresh_ReplacesRow_ClearsEmptiedField` (a refresh
  that drops a field clears it in the cache, not retains it).
- **H2 — fixed via `MarshalJSON`, not a struct-pointer change.** A custom
  `Result.MarshalJSON` omits the country/station layer when its source is
  `none` — same wire outcome as pointer-ising the fields, but without churning
  every orchestrator test that reads `got.Country.Name`. The cold-miss write
  timestamp is reflected onto the returned layer so a cold-miss success carries
  `last_refreshed_at`. Tests: `TestResult_MarshalJSON_OmitsLayersOnSourceNone`,
  `_IncludesPresentLayer`, `TestEnrich_ColdMiss_CarriesLastRefreshedAt`. (The
  validation already established the claimed bogus `0001-01-01` timestamp never
  actually occurred; this is the empty-object + missing-stamp cleanup.)
- **M1 — fixed.** `unmarshalResponse` classifies an expired/invalid session
  (`errSessionExpired`); `LookupWithContext` re-authenticates once under a
  `sessionMu` guard and retries the lookup once (no loop). Tests:
  `TestLookupWithContext_SessionExpiry_ReAuthsAndRetries`,
  `_PersistentSessionExpiry_NoLoop`; race-clean.

The only L2 piece still open is the **optional** DI-name-vs-public-label split
(the M option) — a future cleanup, not a defect.
