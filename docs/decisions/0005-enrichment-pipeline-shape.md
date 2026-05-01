---
number: 0005
title: Enrichment pipeline — single daemon endpoint, aggregated response, cache-first orchestration
status: Accepted
date: 2026-05-01
---

# 0005 — Enrichment pipeline shape

## Context

When the operator Tabs out of a valid callsign field, the SPA needs callsign metadata (name, QTH, country, grid, license class) populated into the QSO draft. v1 had this concept already — a daemon-side hamnut service plus a callsign cache. v2 needs to specify the wire shape now that the SPA is the consumer.

ADR 0004 (the same day) settled the daemon-vs-SPA split: the daemon owns external-service orchestration and persistent caches; the SPA owns UX (cancellation, partial-result rendering, loading state). This ADR is the first concrete application of that rule beyond config.

The previous framing in `frontend-spa.md` had a `lib/enrichment.svelte.ts` module in the SPA orchestrating concurrent fetches to hamnut and QRZ. Per ADR 0004, that's incorrect — the SPA shouldn't hold credentials, shouldn't maintain a per-tab cache, and shouldn't orchestrate external services.

The Callsign component already exposes `onenrich?: (callsign: string) => void`. This ADR specifies what that callback does and what it talks to.

## Decision

**One daemon endpoint. Aggregated response. Cache-first.**

- **`GET /v1/enrich/callsign?call=<callsign>`** — single endpoint that the SPA hits per Tab.
- **Daemon orchestration**: cache lookup first. On cache hit (and fresh), return immediately. On cache miss or stale entry, fire concurrent hamnut + QRZ lookups with a per-source timeout. Aggregate the results, write the merged result back to cache, return to the SPA.
- **Aggregated response (not streamed)**: single JSON body with all fields the SPA needs. SSE-streamed partial results are rejected for v1 — see alternatives.
- **Response always 200**: per the `enrichment never blocks logging` invariant (memory `project_sm_design_invariants`), enrichment failure isn't a request failure. Empty or null fields when nothing was found; partial fields when only one source succeeded; a `source` field on the response indicates origin (`cache` / `hamnut` / `qrz` / `composite` / `none`). Daemon-internal errors (DB failure, etc.) still return 5xx.
- **Cancellation**: SPA uses `AbortController`; aborting the fetch closes the HTTP request; the daemon's request handler sees context cancellation and stops in-flight upstream calls. No explicit cancellation endpoint.
- **Cache TTL**: 7 days default. Operator-configurable via daemon config (per ADR 0003). Stale entries are refreshed on next miss; not eagerly background-refreshed.

### Response shape (initial — extend as consumers need fields)

```json
{
  "callsign": "M0ABC",
  "name": "Marc Veary",
  "qth": "Birmingham, UK",
  "country": "England",
  "grid": "IO92aj",
  "licenseClass": "Full",
  "source": "cache",
  "enrichedAt": "2026-04-30T10:15:00Z"
}
```

Fields are nullable when not known. `source` tells the SPA whether it can trust a cached-too-long result; `enrichedAt` is for showing "last updated" if the UI ever wants it.

## Alternatives considered

### SSE-streamed partial results

Daemon opens an SSE stream per request; emits one event per source as it returns (cache, then hamnut, then QRZ). SPA's handler renders partial state progressively — "name found via hamnut, QTH still loading from QRZ."

Rejected for v1. Three reasons: (a) complexity — SSE setup, ordering guarantees, race-with-cancellation, and reconnection-on-flaky-network all become concerns; (b) the latency window for which streaming would win is small — cache hits are <10ms (no streaming benefit), and external lookups are typically 200–500ms (full response is fast enough that progressive rendering is barely perceived); (c) the operator's Tab → next-field motion already moves their attention away by the time the response comes back, so progressive rendering of fields they're no longer looking at is wasted UX investment. **Trigger to revisit:** if first-paint latency for enrichment becomes a real complaint, or if some lookup paths grow to multi-second timing where progressive rendering would be noticed.

### SPA-side orchestration of multiple sources

`lib/enrichment.svelte.ts` in the SPA fires concurrent fetches to `/v1/lookup/hamnut?call=X` and `/v1/lookup/qrz?call=X`, merges the results, manages cancellation per-source.

Rejected per ADR 0004 — orchestration of external services lives daemon-side. This option also forces the daemon to expose per-source endpoints publicly (or relies on the SPA having credentials), forces per-source error handling into the SPA, and prevents shared caching across browser tabs. The single-endpoint shape lets the daemon evolve its source list (add, remove, reorder) without touching the SPA.

### Per-source endpoints exposed to the SPA, with the SPA composing them

A weaker form of the previous: daemon exposes `/v1/cache/callsign`, `/v1/hamnut/callsign`, `/v1/qrz/callsign` separately; SPA picks which to call.

Rejected for the same reasons plus one more: it leaks the *list* of sources to the SPA, which is implementation detail. The operator's UI cares about "what do we know about this callsign?", not "did hamnut or QRZ tell us?" One endpoint hides the source list and lets it change without SPA churn.

### Single endpoint, aggregated response (chosen)

The simplest shape that obeys ADR 0004 and the enrichment-never-blocks-logging invariant. Trades streaming-progressive-render for implementation simplicity; trades source flexibility for a stable API surface; keeps credentials and caching daemon-side where they belong.

## Consequences

**Signed up for:**

- **Daemon endpoint `GET /v1/enrich/callsign`** with cache + concurrent upstream calls + timeout + cache write-through. The daemon side is meaningful work but mostly carries forward from v1's existing hamnut/QRZ services and `internal/database/sqlite/cache.go` shape — what's new is the HTTP handler that fronts them and the timeout/cancellation discipline.
- **SPA `lib/enrichment.svelte.ts`** is a thin module: a function `enrichCallsign(call: string, signal: AbortSignal): Promise<EnrichmentResult>` plus a `$state` object holding the latest in-flight result for the QsoPanel to bind to. No multi-source orchestration. ~30 lines.
- **Callsign component's `onenrich`** wires through to the SPA's enrichment module; the parent (or a `qsoDraft` store) handles applying the result to other fields. Component itself doesn't change.
- **Cache TTL is operator config** — adds one field to the operator-config schema being designed under ADR 0003.

**Accepted costs:**

- **No progressive partial rendering** — the operator sees nothing for the duration of the lookup, then everything at once. Acceptable trade for simplicity; revisit if it becomes a complaint.
- **Cache invalidation is TTL-based, not source-based** — if a callsign holder changes (license transfer), the SPA shows stale data until 7 days pass. For a personal logging tool this is fine; multi-operator scenarios might need a "force refresh" path.

**Gained:**

- **One endpoint to evolve.** Adding a third source (e.g., `qrz.com` lookups in addition to QRZ) is daemon-internal — no SPA change needed.
- **Credentials never in the SPA bundle.** QRZ password lives only in daemon config; the SPA just hits one URL.
- **AbortController cancellation works naturally** — SPA aborts the fetch, the HTTP request closes, the daemon stops upstream calls via its request context. No bespoke cancellation protocol.
- **Cache benefits all browser tabs/sessions automatically** — single source of truth on the daemon.

## Triggers to revisit

- **If progressive partial rendering becomes a UX requirement** — e.g., one source is reliably 50ms but another is reliably 500ms+, and operators are routinely staring at empty fields waiting. SSE-streamed results become attractive.
- **If forced cache refresh is needed** — `?refresh=true` query param or a separate endpoint becomes the obvious extension.
- **If the source set grows to where one endpoint masks too much** — e.g., debugging "why didn't QRZ return anything?" requires per-source visibility. A `/v1/enrich/callsign/debug` endpoint that breaks out per-source results becomes useful.
- **If multi-operator scenarios emerge** with concurrent lookups — last-source-wins cache writes might race; explicit version/timestamps would be needed.

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — the SPA-hosted-by-daemon premise.
- ADR 0003 (`0003-spa-config-daemon-only.md`) — config that the cache-TTL field gets added to.
- ADR 0004 (`0004-daemon-vs-spa-responsibilities.md`) — the responsibility-split rule that this ADR applies.
- Memory `project_sm_design_invariants` — `enrichment never blocks logging` invariant that drives "always return 200" semantics.
- Memory `project_sm_spa_component_patterns` — Tab-on-callsign as the trigger boundary; Callsign component's `onenrich` callback.
- `frontend/logging/src/lib/ui/components/Callsign.svelte` — the SPA-side trigger.
- `internal/database/sqlite/cache.go` (v1) — shape of the existing callsign cache that v2 daemon-side enrichment carries forward.
- (Future) `frontend/logging/src/lib/enrichment.svelte.ts` — SPA module that calls `/v1/enrich/callsign`.
- (Future) Daemon-side `/v1/enrich/callsign` handler — implementation detail, not specified here beyond the contract.
