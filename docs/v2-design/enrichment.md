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

## Out of scope for this doc

- **Operator config schema** — task #62. How `LookupConfig` flows into the orchestrator's `Country` and `Chain` fields, where TTLs come from, the per-provider enabled flag.
- **HTTP handler** — task #63. Wiring `/v1/enrich/callsign?call=X` to `Orchestrator.Enrich`, response JSON shape, AbortController propagation.
- **`AsyncRefresher` production implementation** — task #61. Bounded goroutine pool, daemon shutdown integration, in-flight count limits.
- **QSO-submit upsert** — task #64. The second write path on `contacted_station` from `qsoservice.Submit` (ADR 0017 #10), best-effort outside the QSO transaction.
- **SPA wiring** — deferred to a separate session. `lib/enrichment.svelte.ts`, `Callsign.onenrich`, `QsoPanel` field application.

## References

- **ADR 0017** — `docs/decisions/0017-enrichment-pipeline-domain-table-cache.md`. Architecture decisions, alternatives, consequences, triggers to revisit.
- **ADR 0005** (superseded by 0017) — `docs/decisions/0005-enrichment-pipeline-shape.md`. Earlier framing; useful for context on what changed.
- **ADR 0014** — upstream-forwarding deferral. Same deferred-with-prep pattern; its `additional_data` provenance fields are the analogue for QSO-time origin (ADR 0017 #10's QSO-submit path leans on this for "where did this row come from").
- **ADR 0004** — daemon-vs-SPA responsibilities. The orchestration-lives-daemon-side rule.
- **`internal/lookup/lookup.go`** — `Provider`, `CountryProvider`, `CallsignProvider` interfaces; `FilterToCallsignFields` helper.
- **`internal/lookup/orchestrator.go`** — `Orchestrator`, `Result`, `MergeStationFromCountry`, `IsEmpty`, source constants, `AsyncRefresher` interface.
- **`internal/lookup/hamnut/`** — country provider against the Hamnut API.
- **`internal/lookup/qrz/`** — callsign-class provider against the QRZ.com XML API.
- **`docs/v1-analysis/invariants.md`** — `enrichment never blocks logging` invariant; load-bearing for ADR 0017 #12 (always-200 contract).
