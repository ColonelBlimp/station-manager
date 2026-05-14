# Enrichment pipeline — implementation detail

This document is the operational counterpart to ADR 0017 (the "what + why" decision record). ADR 0017 captures the architecture decisions and the alternatives that were weighed; this doc captures the **how** — pipeline phases, read-state matrix, filter/merge sequencing, async refresh contract.

Read ADR 0017 first if you haven't. Decisions here aren't being made; they're being explained.

## Pipeline phases

`Orchestrator.Enrich(ctx, callsign)` is the single entry point. It runs through these phases for every Tab:

1. **Concurrent read.** Two goroutines run in parallel:
   - `readCountry` — `FetchCountryByCallsignWithContext` (longest-prefix-match against the `country` table). Cold miss blocks on `CountryProvider.LookupWithContext` (hamnut). Returns `(country, source, coldMiss, staleHit)`.
   - `readStation` — `FetchContactedStationByCallsignWithContext` (callsign-keyed). Cold miss runs `runChain` against the configured callsign-class providers. Returns `(station, source, coldMiss, staleHit)`.
2. **Synchronisation.** `Enrich` waits for both goroutines to finish via two buffered channels.
3. **Filter.** `FilterToCallsignFields(station)` zeros `Country` / `Cont` / `CQZ` / `ITUZ` / `DXCC` on the station. Necessary because callsign-class providers (QRZ, HamQTH, QRZCQ) may return country data, and per ADR 0017 #2 it's hamnut-exclusive on write.
4. **Merge.** `MergeStationFromCountry(station, country)` fills the just-zeroed country fields on the station from the country layer's data, using only-when-different semantics: each field is only set when the country value is non-empty AND differs from the station's existing value. After this step, the station's country fields are either hamnut's truth or empty (if no country source had data) — never QRZ-bug values.
5. **Synchronous write-backs (cold-miss only).**
   - If country was a cold-miss with usable data (`Prefix != ""`): `UpsertCountryWithContext` writes the country row.
   - If station was a cold-miss with usable data (`!IsEmpty(station)`): `UpsertContactedStationWithContext` writes the merged station row. Note: this write happens *after* the merge, so the persisted station row already has hamnut-truthful country fields.
6. **Async refresh schedules (stale-hit only).**
   - If country was stale: `Refresher.Schedule(...)` runs hamnut + writes the country row.
   - If station was stale: `Refresher.Schedule(...)` runs the chain + reads country (cached value at refresh time) + filter + merge + writes the station row. The async station refresh re-merges so the persisted denormalized country fields stay aligned with hamnut.
7. **Return.** Build `Result{Country, Station, CountrySource, StationSource}`. Always non-error per ADR 0017 #12 — failures collapse to "empty fields, source=none."

## Read-state matrix

Each Tab lands in one of nine cells based on the cache state of `country` (rows) × `contacted_station` (columns).

|              | **station: fresh**                              | **station: stale**                                                | **station: cold**                                                                  |
| ------------ | ----------------------------------------------- | ----------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| **country: fresh** | Read both, merge, return. No writes.        | Read both, merge, return stale station. Async chain refresh.       | Block on chain, filter, merge with cached country, write station.                 |
| **country: stale** | Read both, merge, return. Async hamnut refresh. | Read both, merge, return. Both async refreshes fire.              | Block on chain, filter, merge with cached-stale country, write station. Async hamnut refresh. |
| **country: cold**  | Block on hamnut, merge, return. Write country. | Read station, block on hamnut, merge, return stale station. Write country. Async chain refresh. | Block on chain AND on hamnut, filter, merge, write country, write station.       |

Two recurring rules implied by the matrix:

- **Cold-station writes always wait for whatever country data is available** (cached fresh, cached stale, or just-fetched from hamnut). The merge runs against whatever the country layer produced; if country is stale, we use the stale value and let the async hamnut refresh update the country row independently. The station's denormalized country fields will catch up on the next station refresh.
- **Async station refresh always re-merges from country** at refresh time, so a stale station eventually carries the country values that were current when the refresh ran (not when the QSO was logged or when the station was first cached).

## Filter / merge sequencing

The two helpers live in `internal/lookup/lookup.go` and `internal/lookup/orchestrator.go`. They run in this order on every path:

```
station ← chain result (or cache hit)
station ← FilterToCallsignFields(station)        // zero Country / Cont / CQZ / ITUZ / DXCC
station ← MergeStationFromCountry(station, country)  // fill from hamnut, only-when-different
```

**Why filter before merge** — the filter zeroes the QRZ-bug values; the merge fills with hamnut's truth. If a chain provider returned `Country="Malawi"` for `M0CMC` (the cautionary example in ADR 0017), filter strips it, and merge fills `"England"` from hamnut. If hamnut is down (no country data), merge is a no-op and the station's country fields stay empty rather than retaining `"Malawi"`. **The combination guarantees the station never carries upstream wrong values** — either it has hamnut truth or it has nothing.

**Why only-when-different** — avoids no-op writes that would bump `modified_at` without a real change, and makes the merge composable with cached station rows that may already have correct denormalized country fields. In the fresh-fresh case, `s.Country == c.Name` is the typical state, and the merge is a no-op.

## Return-time presentation derivation

A small set of country fields are **derived at return time, not persisted**. The orchestrator's `Enrich` runs the derivation after filter/merge but before constructing the `Result`, so every return path (cold miss, stale hit, fresh hit) produces the same shape.

**`country.local_time`** — recomputed on every Enrich call as `time.Now() shifted by country.time_offset`, RFC 3339 formatted. The persisted column is `time_offset` (a stable fact); `local_time` lives only on the wire so it's always current. `parseOffsetDuration` accepts both formats hamnut emits — the Go-duration shape (`"2h 0m"`, `"-5h 30m"`) and the RFC 3339 zone shape (`"+02:00"`). Empty or unparseable offset → empty `local_time` (no UTC default; unparseable input is a data-quality signal, not a "use zero" trigger).

**Why recompute** — if `local_time` were persisted, a cache hit would return whatever moment the upstream provider responded with, off by minutes-to-hours. If it were taken from upstream on cold miss only, cache hits would return empty and the SPA's response shape would differ across cache states. Recomputing centralises the derivation and gives the SPA a uniform shape regardless of which cache state each layer landed in.

## Provider chain semantics

`runChain(ctx, callsign)` iterates `Orchestrator.Chain` (`[]CallsignProvider`) in order. The chain is operator-configured priority order; the orchestrator does not re-sort.

For each provider:
- **Provider returns non-empty `ContactedStation`** → return immediately with that result and the provider's `Name()` as the station source. The chain stops.
- **Provider returns `(ContactedStation{}, errors.ErrNotFound)`** → advance to the next provider silently. ErrNotFound is the structured "I don't have this callsign" signal from QRZ / HamQTH / etc.
- **Provider returns any other error (transport / parse / auth / 5xx)** → log + advance to the next provider. ADR 0017 #8 treats these the same as ErrNotFound at the dispatch layer; the per-provider log is for operator-facing debugging.
- **Provider returns `IsEmpty(ContactedStation)` with no error** → advance. (Some providers' "no record" path is `(empty, nil)` rather than `(empty, ErrNotFound)`; both shapes work.)

After the loop, if no provider produced data: return `(ContactedStation{}, SourceNone)`. The caller writes no row per ADR 0017 #9.

`IsEmpty` deliberately excludes `Call`, `Country`, `CQZ`, `ITUZ`, `DXCC`, `Cont` from the "has data" check — `Call` is the input echoed back, the country fields are hamnut-exclusive (the chain shouldn't be deciding the chain has data based on values that get filtered out anyway).

## Async refresh contract

`AsyncRefresher.Schedule(fn)` is the orchestrator's way of saying "run this work in the background." The orchestrator knows nothing about the implementation — production wiring is task #61's bounded goroutine pool; tests use a synchronous stub.

**The fn passed to `Schedule`** receives a context that the implementation manages — typically tied to the daemon's lifetime so shutdown cancels in-flight refreshes. The fn closes over the orchestrator's dependencies (`o.DB`, `o.Country`, etc.) and the callsign string; it must not capture the request's `ctx` because that gets cancelled when the HTTP response is sent.

**Country refresh fn:**
```
res ← Country.LookupWithContext(ctx, callsign)
if err or res.Name == "Unknown" or res.Prefix == "" → return quietly
else → DB.UpsertCountryWithContext(ctx, res)
```

**Station refresh fn:**
```
station, source ← runChain(ctx, callsign)
if source == SourceNone → return quietly
station ← FilterToCallsignFields(station)
country ← DB.FetchCountryByCallsignWithContext(ctx, callsign)   // cached value, no upstream call
station ← MergeStationFromCountry(station, country)
DB.UpsertContactedStationWithContext(ctx, station)
```

The station refresh deliberately doesn't trigger a hamnut call — country has its own staleness path, and the station refresh just consumes whatever's currently cached. If country happens to be stale at refresh time, the station gets stale country values; the country's own async refresh will update it independently.

**Failure handling.** Refresh fn errors are logged (or fail silently for transport-style failures) and not retried. Per ADR 0017 #7, the next Tab against the same callsign will trigger a fresh attempt.

### Production implementation

`internal/lookup/refresher/` provides the bounded production worker that satisfies `lookup.AsyncRefresher`. Concrete shape:

- **Bounded by `MaxInFlight`.** Default 4; operator-tunable via config (task #62). Slots tracked via a buffered-channel semaphore. Schedule attempts when at capacity drop the request with a warning log + counter bump (`Dropped()` accessor) rather than queueing — under outage / Tab pile-up an unbounded queue would just delay the same outcome while hoarding memory, and ADR 0017 #7's implicit-fall-through model means the next Tab against the same key will trigger another attempt anyway.
- **Lifecycle.** Standard project shape: `Initialize()` → `Start(ctx)` → `Stop()`. `Start` binds the worker to a parent context (typically the daemon's main lifecycle context); `Stop` cancels that context and waits for in-flight refreshes to drain. Stop is idempotent and has **no hard deadline** — refresh fns must honour `ctx` cancellation themselves; if a fn ignores `ctx`, `Stop` blocks until it returns. This is intentional — the alternative (force-kill after timeout) doesn't compose with goroutines, and daemon shutdown is operator-triggered with no SLA pressure.
- **Panic recovery.** Each scheduled fn runs under `safego.GoTracked`; a panicking fn is recovered + logged + the semaphore slot is released via a deferred block so panic doesn't leak slots. `respawn=false` because refresh fns are one-shot — the next Tab triggers another attempt if needed.
- **Schedule before `Start` or after `Stop` is dropped (logged + counted).** The orchestrator's stale-hit branch calls `Schedule` without coordinating lifecycle; keeping the no-op-when-not-running behaviour in the worker means the orchestrator stays simple.

The orchestrator depends on the `lookup.AsyncRefresher` interface, not the concrete refresher type — tests use a synchronous stub (`syncRefresher` in `orchestrator_test.go`) that runs scheduled work immediately so behaviour is deterministic; production wires the bounded worker.

## Out of scope for this doc

- **Operator config schema** — task #62. How `LookupConfig` flows into the orchestrator's `Country` and `Chain` fields, where TTLs come from, the refresher's `MaxInFlight` knob, the per-provider enabled flag.
- **HTTP handler** — task #63. Wiring `/v1/enrich/callsign?call=X` to `Orchestrator.Enrich`, response JSON shape, AbortController propagation, DI wiring of orchestrator + refresher into `cmd/smd`.
- **QSO-submit upsert** — task #64. The second write path on `contacted_station` from `qsoservice.Submit` (ADR 0017 #10), best-effort outside the QSO transaction.
- **SPA wiring** — shipped 2026-05-08, sessions 44–45. `frontend/logging/src/lib/api/enrichment.ts` is the thin fetch wrapper (discriminated outcome union: `'ok' | 'validation' | 'server' | 'aborted' | 'network'`; the `'aborted'` arm + optional `signal?: AbortSignal` param were added session 54, 2026-05-13). Outcome tests pinned session 55, 2026-05-14 — `enrichment.test.ts` covers all five arms plus the always-200 ADR 0017 #12 contract and AbortSignal passthrough. `QsoPanel.handleEnrich` populates `qsoDraft.name` / `qsoDraft.qth` on `outcome.kind === 'ok'` and emits a `Lookup: <CALL> not found` warn-toast when `station_source === 'none'` (the toast gate is on station_source alone — country lookup is longest-prefix-match so almost any callsign hits the country layer; the form auto-fill is what the operator cares about). `lib/states/enrichment.svelte.ts` holds the latest result + short/long path selection; `lib/ui/panels/CountryPanel.svelte` displays country name (with `*` for new DXCC), bundled `flag-icons` flag, short/long path distance/bearing pair with the active path highlighted in `text-indigo-700`, short/long radio, and local time + offset. Path selection drives ADIF `ANT_AZ` on submit. A 500ms-delayed sticky info-toast `Looking up <CALL>...` covers slow-internet lookups; cache hits never see it.

## References

- **ADR 0017** — `docs/decisions/0017-enrichment-pipeline-domain-table-cache.md`. Architecture decisions, alternatives, consequences, triggers to revisit.
- **ADR 0005** (superseded by 0017) — `docs/decisions/0005-enrichment-pipeline-shape.md`. Earlier framing; useful for context on what changed.
- **ADR 0014** — upstream-forwarding deferral. Same deferred-with-prep pattern; its `additional_data` provenance fields are the analogue for QSO-time origin (ADR 0017 #10's QSO-submit path leans on this for "where did this row come from").
- **ADR 0004** — daemon-vs-SPA responsibilities. The orchestration-lives-daemon-side rule.
- **`internal/lookup/lookup.go`** — `Provider`, `CountryProvider`, `CallsignProvider` interfaces; `FilterToCallsignFields` helper.
- **`internal/lookup/orchestrator.go`** — `Orchestrator`, `Result`, `MergeStationFromCountry`, `IsEmpty`, source constants, `AsyncRefresher` interface.
- **`internal/lookup/hamnut/`** — country provider against the Hamnut API.
- **`internal/lookup/qrz/`** — callsign-class provider against the QRZ.com XML API.
- **`internal/lookup/refresher/`** — production `AsyncRefresher` implementation: bounded goroutine pool, lifecycle, panic recovery via `internal/safego`.
- **`internal/safego/`** — panic-recovering goroutine wrappers (`Go` / `GoTracked`); the refresher uses `GoTracked` for shutdown drain.
- **`docs/v1-analysis/invariants.md`** — `enrichment never blocks logging` invariant; load-bearing for ADR 0017 #12 (always-200 contract).
