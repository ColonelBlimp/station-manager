# internal/lookup code review - 2026-06-19

## Scope

Fresh review of `internal/lookup` and the adjacent boundaries needed to reason
about it:

- `internal/lookup`, `internal/lookup/hamnut`, `internal/lookup/qrz`,
  `internal/lookup/refresher`
- `/v1/enrich/callsign` handler wiring in `internal/api`
- lookup config validation and startup wiring in `internal/config` and
  `cmd/smd`
- lookup cache persistence paths in `internal/database/sqlite`
- enrichment documentation under `docs/v2-design`

Focus areas: correctness, performance, security, test coverage, and
documentation. This is review-only; no source fixes were made.

## Summary

No critical correctness issue surfaced in the core cache/merge/orchestration
model. The country/station split is coherent, stale/fresh/cold behavior is well
tested, QRZ's country fields are filtered before merge, and the refresher has
bounded concurrency plus race coverage.

The material risks are at the HTTP/provider boundary and cancellation edge:
successful provider responses are read without a size limit, QRZ credentials can
be sent through non-HTTPS operator-configured URLs, and `Enrich` waits for
provider goroutines without its own `ctx.Done()` fallback. There are also small
documentation drifts around shipped enrichment wiring and provider names.

## Findings

### M1. Successful provider response bodies are unbounded

**Area:** performance / security / availability  
**Files:** `internal/lookup/hamnut/service.go:221-230`,
`internal/lookup/qrz/internal.go:65-75`,
`internal/lookup/qrz/service.go:279-290`

Both providers cap non-2xx response bodies to `errorBodyLimit`, but successful
2xx bodies are read with plain `io.ReadAll(resp.Body)`:

- Hamnut caps error bodies at `service.go:221-225`, then reads success bodies
  unbounded at `service.go:230`.
- QRZ session fetch caps error bodies at `internal.go:65-67`, then reads
  successful auth XML unbounded at `internal.go:69`.
- QRZ lookup caps error bodies at `service.go:279-282`, then reads successful
  lookup XML unbounded at `service.go:285`.

The upstreams are operator-configured URLs and remote services. A misbehaving
server, captive portal, compromised DNS path, or incorrectly configured URL can
return a very large 200 response and force the daemon to buffer it entirely.
The always-200 API contract then hides the provider failure from HTTP status,
so this is mainly an availability risk: memory pressure or process termination
during lookup.

**Recommendation:** apply the same bounded-read discipline to successful
responses. A fixed provider body limit such as 1 MiB is generous for QRZ XML and
Hamnut JSON while preventing unbounded memory use. Return a structured provider
error when the limit is exceeded and add tests where a 2xx response over the
limit fails without reading the full stream.

**Test gap:** current tests cover non-2xx handling and cancellation, but not a
successful response body limit.

### M2. QRZ credentials can be sent through non-HTTPS URLs

**Area:** security  
**Files:** `internal/lookup/qrz/internal.go:42-52`,
`internal/lookup/qrz/internal.go:205-227`,
`internal/config/config.go:1182-1238`,
`internal/config/lookup_test.go:64-80`

QRZ session authentication builds a GET URL with `username`, `password`, and
`agent` query parameters at `internal.go:42-50`, then sends it with
`http.NewRequestWithContext` at `internal.go:52`.

The default config uses QRZ's HTTPS endpoint, but validation does not require
HTTPS:

- `config.validateLookupProvider` only requires a non-empty URL and positive
  timeout for enabled providers (`config.go:1226-1238`).
- QRZ provider validation only checks that the parsed URL has a scheme and host
  (`internal.go:205-211`).
- Existing provider tests use `http://x` as a valid scheme in broken-config
  test cases (`service_test.go` via the reviewed test list), which confirms
  the scheme is not constrained.

Because QRZ credentials are in the request URL, allowing `http://` makes an
operator typo or malicious local endpoint enough to expose the password on the
wire and to intermediary/request logs. Even with HTTPS, query credentials are
likely to appear in upstream logs because the QRZ XML API is URL-query based;
the daemon cannot fully avoid that if the upstream contract requires GET, but
it can still reject insecure transport.

**Recommendation:** require `https` for enabled credentialed lookup providers,
at minimum QRZ, in both config validation and provider validation. If tests need
`httptest.Server`, provide a test-only constructor override or allow HTTP only
for loopback/test clients, not arbitrary operator config. Also avoid logging
full request URLs anywhere near provider errors.

**Test gap:** add config/provider tests that enabled QRZ rejects `http://` and
unsupported schemes, while the default HTTPS endpoint remains valid.

### M3. `Enrich` can outlive request cancellation if a dependency ignores `ctx`

**Area:** correctness / shutdown behavior / performance  
**Files:** `internal/api/handler_enrich.go:47-50`,
`internal/lookup/orchestrator.go:261-274`,
`internal/lookup/orchestrator.go:526-543`

The API handler correctly passes `r.Context()` to the orchestrator, and provider
implementations generally propagate that context into HTTP calls. However, the
orchestrator launches country and station reads, then waits with unconditional
channel receives:

```go
c := <-cCh
s := <-sCh
```

There is no `select` on `ctx.Done()` around those waits. If a provider, DB call,
or future chain provider does not honor context cancellation, the request
handler remains blocked until that dependency returns. This weakens the handler
comment's cancellation guarantee and can also delay daemon shutdown because an
HTTP handler may still be waiting on a stuck lookup.

The current providers are mostly well-behaved, and the HTTP clients have
configured timeouts, so the common path is bounded. The issue is that the
orchestrator itself has no cancellation backstop despite using buffered result
channels that would make an early return straightforward.

**Recommendation:** collect both results through a small helper that selects on
the result channel and `ctx.Done()`. On cancellation, return `SourceNone` for
the missing layer and let the goroutine finish later into the buffered channel.
Also consider checking `ctx.Err()` between providers in `runChain` so a canceled
request does not continue walking a multi-provider chain.

**Test gap:** add an orchestrator-level cancellation test with a stub provider
that deliberately ignores `ctx` and blocks. The expected behavior should be that
`Enrich` returns promptly after context cancellation.

### L1. Direct `Orchestrator` construction is easy to miswire

**Area:** correctness / maintainability  
**Files:** `internal/lookup/orchestrator.go:104-112`,
`internal/lookup/orchestrator.go:303-313`,
`internal/lookup/orchestrator.go:347-348`,
`cmd/smd/main.go:1044-1052`

`Orchestrator` is a public struct with no constructor or validation. Production
startup wires it correctly in `cmd/smd`, but tests and future internal callers
can construct it directly with missing dependencies.

Some nil/miswired cases degrade gracefully because `readCountry` and
`readStation` run under `safego.Go`, but not all of them are contained. For
example, a force-refresh with a country provider and nil `DB` can reach the
synchronous write-back path and panic at `o.DB.UpsertCountryWithContext`; the
new-entity check can similarly dereference `o.DB` after the goroutines return.

This is not a production defect in the current `cmd/smd` path, but the package
API invites invalid states and relies on call-site discipline.

**Recommendation:** introduce a small constructor such as `lookup.NewOrchestrator`
that validates required dependencies (`DB` and at least one provider/refresher
shape as appropriate), normalizes nil slices, and centralizes defaults. Keep the
struct literal available only if needed by tests, or add an `Enrich` guard that
returns empty sources when `DB` is nil.

**Test gap:** add a narrow test for nil/miswired orchestrator behavior if direct
construction remains part of the package contract.

### L2. Documentation has shipped-state drift around enrichment wiring

**Area:** documentation  
**Files:** `docs/v2-design/enrichment.md:114-119`,
`internal/lookup/lookup.go:37-46`

The design doc still lists operator config schema and HTTP handler wiring as
"Out of scope" task #62/#63 items, even though the current tree has config,
provider startup wiring, the refresher, API handler, and SPA-facing behavior
implemented. The same section does correctly note later SPA wiring as shipped,
so the doc now mixes historical task framing with current-state guidance.

There is also a stale provider-name comment in `lookup.go`: it describes
Hamnut's provider name as `"hamnut"`, but `hamnut.Service.Name()` returns
`"hamnutlookupservice"` while `"hamnut"` is the fixed country source constant
used in API results. A reader new to the code has to reconcile that distinction
from multiple files.

**Recommendation:** refresh `docs/v2-design/enrichment.md` with a short
"historical design; current implementation lives in..." banner or move shipped
items out of the out-of-scope list. Update the provider-name comment to
distinguish clearly between provider `Name()` values and public source values.

## Test Coverage Notes

Strong coverage observed:

- Orchestrator tests cover cache states, lower-case canonicalization,
  force-refresh replacement semantics, stale refresh scheduling, panic recovery,
  local-time derivation, and merge/filter behavior.
- Provider tests cover disabled sentinels, broken config, happy paths,
  not-found handling, transport errors, QRZ session expiry/re-auth, and context
  cancellation for provider HTTP calls.
- API tests cover malformed callsign handling, nil orchestrator behavior,
  always-200 enrichment failures, `refresh=true`, cache bypass, cache-hit
  behavior, and real-provider e2e wiring against `httptest`.
- Refresher tests cover capacity drops, context propagation, `Stop()` waiting
  and cancellation, idempotency, panic recovery, and the prior Schedule/Stop
  WaitGroup race.
- Config tests cover lookup defaults, TTL preservation, duplicate provider
  names, hamnut collision, negative TTLs, and lookup provider accessors.

Coverage gaps worth adding with the fixes:

- Successful provider response body limits for Hamnut, QRZ session fetch, and
  QRZ lookup.
- QRZ HTTPS enforcement and unsupported-scheme rejection at both config and
  provider validation layers.
- Orchestrator cancellation when a dependency ignores `ctx`.
- Direct/miswired `Orchestrator` behavior if the struct remains the public
  construction API.

## Verification

Commands run:

```sh
GOCACHE=/tmp/go-build go test ./internal/lookup/...
GOCACHE=/tmp/go-build go test -race ./internal/lookup/...
GOCACHE=/tmp/go-build go test ./internal/api ./internal/config ./internal/database/sqlite
GOCACHE=/tmp/go-build go test -race ./internal/api ./internal/config ./internal/database/sqlite
GOCACHE=/tmp/go-build go vet ./internal/lookup/... ./internal/api ./internal/config ./internal/database/sqlite
```

Results:

- `internal/lookup/...`: pass.
- `internal/lookup/...` under `-race`: pass.
- Adjacent API/config/sqlite packages: pass.
- Adjacent API/config/sqlite packages under `-race`: pass.
- `go vet` over the reviewed package set: pass.

The first sandboxed runs of listener-backed `httptest` suites failed with
`listen tcp6 [::1]:0: socket: operation not permitted`; rerunning the same
commands with localhost binding allowed passed.

## Resolution (2026-06-19)

All five findings fixed (real correctness/security/availability in the
enrichment path — nothing deferred).

- **M1 (fixed).** Successful (2xx) provider bodies are now bounded to
  `successBodyLimit` (1 MiB). Hamnut reads via `io.LimitReader` + a size check;
  QRZ's two reads (session-auth + lookup) go through a shared `readLimitedBody`
  helper. An oversized 200 returns a structured error instead of buffering the
  stream. Tests: `hamnut.TestLookup_RejectsOversizedSuccessBody`,
  `qrz.TestReadLimitedBody_RejectsOversized`.
- **M2 (fixed).** A credentialed lookup provider (QRZ — username/password ride in
  the request URL) must use `https`; plain `http` is allowed only to a loopback
  host (so httptest/local mocks still work). Enforced at both layers: generically
  in `config.validateLookupProvider` (any provider with Username/Password set) via
  `lookupTransportSecure`, and QRZ-specifically in the provider's `validateConfig`
  via `secureOrLoopbackURL`. The default QRZ endpoint is https, so no operator
  config breaks. Tests:
  `config.TestValidateLookup_CredentialedProviderRequiresSecureTransport` +
  a new rejection case in `qrz.TestInitialize_RejectsBrokenConfig` (and that
  test's other cases switched to `https://` so they exercise their intended
  check, not the transport gate).
- **M3 (fixed).** `Enrich` now waits for the country/station layers with
  `select` on the result channel AND `ctx.Done()`, so a provider/DB call that
  ignores ctx can't hang the handler. The goroutines finish later into the
  buffered (cap-1) channels — no leak — and the SourceNone zero-values make every
  downstream conditional (cold-miss write-backs, stale refresh, new-entity query)
  a no-op. Test: `TestEnrich_ReturnsPromptlyOnContextCancel` (blocking,
  ctx-ignoring provider).
- **L1 (fixed, minimal).** Added `o.DB != nil` guards to the three synchronous
  DB-deref sites in `Enrich` (cold-miss country upsert, contacted-station replace,
  new-entity query) so a miswired/test Orchestrator with a nil DB degrades instead
  of panicking. Chose this targeted guard over a full `NewOrchestrator`
  constructor (heavier, changes all call sites) since cmd/smd already wires DB
  correctly and L1 is not a production defect — the constructor remains the
  heavier alternative if direct construction becomes a problem.
- **L2 (fixed, docs).** `lookup.go`'s `Provider.Name()` comment now distinguishes
  the DI service NAME (`"hamnutlookupservice"`) from the public country SOURCE
  constant (`"hamnut"`). `docs/v2-design/enrichment.md` got a Tier-2 historical
  banner (per `docs/README.md`) noting the "out of scope #62/#63" framing is
  authoring-time and those have shipped — pointing at the live refs
  (api-endpoints.md, config.md) rather than rewriting the frozen brief.

Verified: `gofmt`/`go vet` clean; `internal/lookup/...`, `internal/config`,
`internal/api` pass; `go test -race ./internal/lookup/...` clean; CGO-free
`./cmd/smd` builds.
