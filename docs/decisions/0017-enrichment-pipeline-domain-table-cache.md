---
number: 0017
title: Enrichment pipeline — domain-table cache, hamnut as country source-of-truth, sequential callsign-class chain (supersedes 0005)
status: Accepted
date: 2026-05-07
---

# 0017 — Enrichment pipeline (supersedes 0005)

## Context

ADR 0005 (2026-05-01) specified an enrichment pipeline that the v2 SPA would consume: single endpoint `GET /v1/enrich/callsign?call=X`, daemon-side concurrent hamnut + QRZ lookups, aggregated response, write-through to a dedicated callsign cache with a 7-day TTL.

The implementation session (2026-05-07) surfaced model mismatches with how the operator actually wants enrichment to work. The corrections came one at a time during the design discussion:

- **Hamnut is country source-of-truth, not a peer to QRZ.** QRZ-class providers have demonstrably-wrong country/zone data — concrete example: callsign `M0CMC` is a regular English call with an English gridsquare, but QRZ records `CQ Zone 37 / ITU Zone 53` (Malawi). Treating QRZ and hamnut as concurrent peers and merging produces wrong country data.
- **The "cache" is the domain tables, not a dedicated cache.** v1 already writes hamnut results into the `country` table (keyed on prefix) and writes callsign-class results into the `contacted_station` table (keyed on callsign). The operator's mental model is "the DB IS the source of truth once populated; internet is the fallback for misses + occasional refresh." This inverts ADR 0005's "internet is truth, cache is auxiliary" framing.
- **Multiple callsign-class providers form a sequential priority chain, not a concurrent merge.** Operators differ on which service they use (QRZ.com vs HamQTH vs QRZCQ); a chain configured by the operator (priority order, first non-empty wins, fallback through the rest on empty/error) matches what they actually want. A concurrent fan-out doesn't.
- **`contacted_station` has two write paths, not one.** Enrichment lookup is one writer; QSO submit is the other (operator types a name during a QSO; that lands in `contacted_station` for next-time pre-fill, even if no provider knew the callsign).

The operator runs in Malawi where internet is unreliable; offline capability is load-bearing per memory `project_sm_operator_network`. The model in this ADR aligns the architecture with that reality: the local DB grows over time and serves the operator without internet whenever data is already there.

## Decision

**Two independent layers, domain tables as cache, hamnut as country source-of-truth.**

1. **Two layers.** Layer A is callsign-class providers (QRZ.com, HamQTH, QRZCQ, …) — operator-configured priority chain, sequential, first non-empty wins, fallback on empty or error. Layer B is hamnut — independent, country source-of-truth.
2. **Provider field scope.** Layer A providers supply name / QTH / grid / license-class only. Country / CQ-zone / ITU-zone / continent / DXCC are **hamnut-exclusive on write** — Layer A providers may return these fields, but the daemon ignores them.
3. **Domain tables as cache.** `country` (keyed on `prefix`) holds hamnut results; `contacted_station` (keyed on callsign) holds Layer A results. No dedicated cache table.
4. **Time-based staleness.** Both tables get a `last_refreshed_at` column; rows older than the operator-configurable TTL are stale. TTLs differ per table — country defaults long (DXCC turns over rarely), contacted_station defaults shorter (operators move, licenses expire). Default values pinned at implementation time.
5. **Cold-path miss-fills run concurrently.** When both `contacted_station` and `country` miss (or are stale), the chain run and the hamnut call fire concurrently — the two paths are genuinely independent.
6. **Read policy is three-state:**
   - Cold miss → block-and-refresh (do the upstream call, return the result).
   - Stale hit → serve-stale immediately; refresh runs async in the background, writes the result back when it returns.
   - Fresh hit → return immediately, no upstream call.
7. **Failure mode is implicit fall-through.** Hamnut/provider failures (timeout, DNS fail, TCP refused, 5xx) just fall through — the daemon returns whatever's in the local DB (even stale, even empty). No global "is the internet up?" state is maintained.
8. **Chain semantics: first non-empty wins.** A `200 OK` from QRZ with "no record of this callsign" is treated like a transport failure — the chain proceeds to HamQTH. Only an actual data response stops the chain.
9. **No-data writes nothing.** Chain fully bottoms out with no provider holding the callsign → no row written to `contacted_station`. Hamnut returns "I don't have this prefix" → no row written to `country`. The operator may still populate `contacted_station` later via the QSO-submit write path.
10. **`contacted_station` two-path.** Enrichment lookup writes on chain hit (full row). QSO submit upserts whatever operator-collected fields are populated on the QSO (best-effort, outside the QSO transaction per the cache/enrichment-writes-are-best-effort half of the one-fails-all-fail invariant).
11. **Refresh data wins on conflict, no per-field tracking.** When a provider refresh runs against a row the operator previously populated via QSO submit, the refresh overwrites. Reasoning: the `qso` row preserves "what was true at QSO time" as historical truth; `contacted_station` is a "next-time pre-fill" cache, allowed to be overwritten when a more current source catches up. No data is lost at the system level.
12. **API surface mostly carries forward from ADR 0005.** URL stays `GET /v1/enrich/callsign?call=X`, always-200 stays (per the `enrichment never blocks logging` invariant), AbortController cancellation stays. The response **body** shape changes — ADR 0005's single `source` field with values `cache | hamnut | qrz | composite | none` doesn't fit the two-layer model; the replacement is one source indicator per layer (e.g., `countrySource: "country_table" | "hamnut" | "none"`, `stationSource: "contacted_station" | "qrz" | "hamqth" | "qrzcq" | "none"`). Exact JSON shape pinned at implementation time.
13. **Always merge country into station at return time.** On every read state — fresh hit, stale hit, cold miss — the orchestrator's last step before responding is `MergeStationFromCountry(station, country)`: hamnut country fields populate the station's denormalized `Country` / `Cont` / `CQZ` / `ITUZ` / `DXCC` columns. Filter (`FilterToCallsignFields`) runs first to zero any QRZ-bug values from a chain provider; merge runs second to fill with hamnut truth. Pairing the two means the station's country fields are deterministically either hamnut's truth or empty — never the upstream's wrong values, even when hamnut is down.
14. **Merge uses "only when different" semantics.** `MergeStationFromCountry` overwrites a station field only when the country source has a non-empty value AND that value differs from the station's existing value. Avoids no-op writes that would bump `modified_at` without a real change, and pairs correctly with the filter step (filter zeros → merge fills) without special-casing the cold path.
15. **Read-state matrix is documented in `docs/v2-design/enrichment.md`.** The 9 cold/stale/fresh × cold/stale/fresh combinations and their write/refresh actions live in the design doc rather than this ADR — they're operational detail derived from rules 5–11 + 13–14, and would push the ADR past the convention's "two pages ceiling."

## Alternatives considered

### ADR 0005 as-written — concurrent hamnut+QRZ + dedicated cache + 7-day TTL

Rejected. Three reasons. (a) Concurrent merge of hamnut+QRZ produces wrong country data — QRZ's country/zone records are demonstrably untrustworthy, and a merge that treats them as peer to hamnut propagates the wrong values into the QSO. (b) The operator's actual mental model is the local DB as source-of-truth; a dedicated cache duplicates the domain tables for no gain. (c) Multiple callsign-class providers (HamQTH, QRZCQ) need fallback semantics, not concurrent fan-out — operators pick the service they have an account with, and the chain is configured to honour that.

### Per-field operator-vs-provider tracking on `contacted_station`

Each field carries a touched-by-operator marker; provider refreshes only update fields the operator hasn't touched. Preserves operator-typed notes indefinitely.

Rejected as over-engineered for the use case. The "data lost on refresh" worry is illusory — the QSO row preserves the operator's typed value as historical truth at QSO time. `contacted_station` is just a next-time-pre-fill cache; allowing the refresh to overwrite is correct because the more current source has caught up. Per-field tracking adds schema complexity (touched-mask or per-field source columns) for a problem that doesn't exist at the system level.

### Sticky failure flag for "hamnut down"

A failed hamnut call sets a daemon-internal cooldown flag (e.g., 60s) so subsequent cold Tabs skip hamnut entirely instead of paying the timeout per Tab. Adds resilience to the rapid-cold-Tab scenario during a band opening on a flaky link.

Rejected for v1 — the simpler implicit fall-through is good enough until cold-Tab pile-ups become a real complaint. Adding it later is a small change (one timestamp + one branch in the handler).

### Active probe for "is internet up"

Daemon periodically pings a known endpoint, maintains an "internet up" flag. Hamnut and provider chain run only when the flag says up.

Rejected. Heavier than needed for a personal-operator tool — adds a background poller, raises the question of which endpoint to probe, doesn't actually save much over implicit fall-through (one timeout per cold Tab during an outage is acceptable).

### Filter-only, no merge — leave station country fields empty after stripping QRZ-bug values

Strip QRZ-bug `Country` / `CQZ` / `ITUZ` / `DXCC` / `Cont` from chain results, persist the empty values, and rely on the SPA composing country from `Result.Country` and station from `Result.Station` at render time. Simpler — no `MergeStationFromCountry` helper, no async re-merge logic.

Rejected. Two reasons. (a) `contacted_station` is the source of country fields for ADIF export and for the `types.Qso.ContactedStation` embed at QSO-submit time — empty country columns there break ADIF export and force every consumer to compose country from two layers. (b) The merge is the natural place to centralise "country is hamnut-exclusive on write" — without it, every consumer of `contacted_station.country` has to know to overlay hamnut's truth, which is exactly the kind of drift the canonical-DTO rule guards against. Filter alone produces empty-but-correct rows; filter + merge produces full-and-correct rows at the same orchestrator boundary.

### Negative caching for unknown prefixes/callsigns

When hamnut/chain return "no record," write a marker row to `country`/`contacted_station` so subsequent Tabs on the same unknown callsign/prefix skip the upstream call until the marker goes stale.

Rejected. Two reasons. (a) Unknown prefixes are rare for hamnut at DXCC scale; the savings don't justify the complexity. (b) For callsigns, the operator argued that some operators only use HamQTH/QRZCQ and not QRZ.com — caching "we tried QRZ and got nothing" would mask the fact that a different provider in the chain might have the data tomorrow. Eager retry beats cached negative results in this context.

### SSE-streamed partial results

Carried forward from ADR 0005's alternatives — daemon streams per-source events as they return, SPA renders partial state progressively.

Still rejected for the same reasons given in ADR 0005: complexity not justified by the latency window, operator's Tab→next-field motion already moves their attention away by the time the response comes back.

## Consequences

**Signed up for:**

- **Schema migration.** Add `last_refreshed_at` to both `country` and `contacted_station` (`DATETIME` nullable; `NULL` means "never refreshed, treat as stale on first read"). Pre-production, so amended in place to `0001_init.up.sql` rather than chained as `0002_*.sql`.
- **Provider abstraction.** Interface for callsign-class providers, registry, priority-chain runner. Hamnut treated as a separate "country provider" — not in the chain.
- **Filter + merge helpers at the orchestrator boundary.** `FilterToCallsignFields` zeros QRZ-bug country fields; `MergeStationFromCountry` fills them from hamnut truth using only-when-different semantics. The two helpers run on every read path (sync return + async refresh) so the station's denormalized country fields stay aligned with hamnut.
- **Port v1 hamnut + QRZ-lookup.** Lift v1's `internal/lookup/hamnut/` and `internal/lookup/qrz/` into v2 idioms (`internal/errors` Op constants, `internal/iocdi` wiring, `types.*` canonical DTOs).
- **Async-refresh machinery.** Bounded goroutine pool integrated with daemon shutdown, so a stale-hit Tab doesn't leak a goroutine on `smd` exit.
- **HTTP handler `GET /v1/enrich/callsign?call=X`** implementing the three-state read policy.
- **Operator config schema.** Hamnut config block, provider chain (ordered list with per-provider enabled flag + creds), TTLs per table.
- **`contacted_station` upsert in QSO submit path.** Best-effort write outside the QSO transaction, populated from operator-typed name/QTH/grid/license-class fields on the QSO.

**Accepted costs:**

- **Cold Tab during full outage pays the upstream-call timeout per Tab.** Rapid-cold-Tab pile-ups (band opening on flaky internet) eat one timeout each. Sticky-flag mitigation is deferred.
- **Operator's QSO-time notes overwritten by next provider refresh.** Acceptable per reasoning #11 above — `qso` row preserves the historical value.
- **Stale row served on the first Tab after TTL expiry.** Operator sees one Tab of stale data; the next Tab against the same key sees fresh. Bounded by the async-refresh machinery actually completing the upstream call before the next Tab.
- **Repeated upstream calls for unknown callsigns/prefixes.** No negative caching; if the operator types the same unknown call 10 times in 30 minutes, the chain runs 10 times.

**Gained:**

- **Offline-capable database grows naturally.** Every QSO logged adds rows to `contacted_station` (via the QSO-submit path) and `country` (via hamnut on first prefix encounter). Over time the operator's local DB reaches a state where most Tabs are fresh hits and the internet is barely needed — directly supporting the Malawi-internet-is-unreliable constraint.
- **One chain to evolve.** Adding HamQTH or QRZCQ later is a provider-shaped change in `internal/lookup/`; the chain runner doesn't change.
- **No SPA orchestration.** Per ADR 0004, all this lives daemon-side. SPA hits one URL, gets one aggregated response.
- **No data loss on refresh.** The two-table model + QSO-row historical truth means the system loses nothing when `contacted_station` gets overwritten.
- **Hamnut errors don't pollute QSOs.** QRZ's wrong CQ/ITU/DXCC values never reach the QSO because Layer A's country fields are ignored on write.

## Triggers to revisit

- **Cold-Tab pile-ups during outages become a real complaint.** Add the sticky failure flag (60s cooldown after a hamnut/provider error). Small change, one timestamp + one branch.
- **Operator-typed QSO notes consistently overwritten by lower-quality provider data.** If this happens often enough that the operator notices in `contacted_station`, revisit per-field tracking. Probably triggered by a specific provider (e.g., one returns truncated names).
- **`contacted_station` / `country` grow unbounded and per-row refresh becomes expensive.** Eager refresh-on-stale-hit may need to switch to lazy refresh-on-explicit-request. Triggered by table size + TTL combination making background refresh churn the DB too hard.
- **Debugging "why didn't HamQTH return anything?" becomes a real need.** Per-source visibility (`/v1/enrich/callsign/debug` or similar) becomes useful. Triggered by chain providers misbehaving in opaque ways.
- **A second operator joins.** The single-operator assumption breaks; concurrent refresh of the same row could race. Triggered by SM Cloud (deferred per ADR 0016) or any multi-operator scenario.
- **Hamnut goes away or becomes unreliable.** Country source-of-truth needs replacing — the architecture supports this (swap one provider) but the operator would need to pick the replacement. Triggered by hamnut.com shutting down or persistent data quality issues.

## References

- ADR 0005 (`0005-enrichment-pipeline-shape.md`) — superseded by this ADR. Marked as such in 0005's frontmatter.
- ADR 0003 (`0003-spa-config-daemon-only.md`) — config that the TTL and provider-chain fields get added to.
- ADR 0004 (`0004-daemon-vs-spa-responsibilities.md`) — the responsibility-split rule that puts external-service orchestration daemon-side.
- ADR 0014 (`0014-upstream-forwarding-deferred.md`) — `additional_data` provenance pattern; analogous "best-effort cache writes outside the transaction" idea.
- Memory `project_sm_design_invariants` — `enrichment never blocks logging` invariant driving the always-200 semantics + serve-stale-on-stale-hit policy.
- Memory `project_sm_operator_network` — Malawi-internet-is-unreliable constraint that makes the offline-first inversion of ADR 0005 load-bearing.
- v1 `internal/lookup/hamnut/`, `internal/lookup/qrz/` — carry-forward source for the v2 provider implementations.
- v1 `internal/database/sqlite/cache.go` (if present) — historical context on how v1 layered the cache; v2's domain-table model supersedes whatever shape this took.
- (Future) `internal/lookup/` package in v2 — provider registry + chain runner.
- (Future) `frontend/logging/src/lib/enrichment.svelte.ts` — SPA module that calls `/v1/enrich/callsign` (deferred to a separate session per the option-2 MVP scope).
