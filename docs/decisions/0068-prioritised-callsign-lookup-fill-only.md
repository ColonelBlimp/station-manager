---
number: 0068
title: Prioritise callsign providers and fill only blank fields
status: Accepted (operator-ratified 2026-08-14; implementation pending)
date: 2026-08-14
---

# 0068 — Prioritise callsign providers and fill only blank fields

## Context

ADR 0017 gave callsign enrichment a sequential provider chain, but its rule is
**first non-empty result wins**. The chain advances only when a provider errors,
returns not-found, or returns no substantive station data at all. A provider
that returns a name but no gridsquare therefore stops the chain, even when a
lower provider could supply the missing location. This became concrete when
QRZCQ joined QRZ as the second callsign provider.

The JSON array order is also the provider priority today. That is fragile once
providers are registry-seeded, disabled independently, round-tripped through
the Settings API, and eventually reordered in the UI: moving an array element
silently changes upstream authority. The operator wants priority to be explicit
and exclusive — exactly one provider at priority 1, one at priority 2, and so
on.

The lookup must remain a good upstream client. It must not fan out to every
enabled service just because more data might exist, and an incompletely known
station must not cause fresh network traffic every time the callsign is shown.
At the same time, once a fallback call is necessary, discarding useful fields
that happened to arrive in that response would waste both the call and the
data.

## Decision

Callsign providers have an explicit positive `priority`. Priorities are unique
and contiguous across every configured callsign provider, including disabled
providers. Runtime construction sorts by `priority` and then filters disabled
entries; JSON array order is not authoritative.

`lookup.continue_if_blank` is the one chain-wide completion policy. Its initial
policy is:

```json
"continue_if_blank": ["name", "gridsquare"]
```

After each provider response, the daemon normalises the response and merges it
into an accumulator. A lower-priority provider may fill **any** blank
callsign-owned field, but it never overwrites a non-blank field contributed by
a higher-priority provider. The next enabled provider is called when either:

1. no provider has yet contributed substantive station data; or
2. one or more fields named by `continue_if_blank` remain blank.

The chain stops as soon as every completion field is present, or after the last
enabled provider. `name` and `gridsquare` decide whether another network call is
made; fields such as QTH, address, email, web, coordinates and IOTA are still
filled opportunistically whenever that call was already necessary. Missing QTH
alone does not cause another call.

An upstream error, not-found response, or empty response advances to the next
provider without discarding accumulated data. A lower provider's conflicting
value never wins merely because the higher provider omitted some other field.
For example:

```text
priority 1: name="Marc",      qth="",           gridsquare="IO83"
priority 2: name="Different", qth="Manchester", gridsquare=""
result:     name="Marc",      qth="Manchester", gridsquare="IO83"
```

"Blank" means empty after whitespace trimming and provider-boundary
normalisation. Invalid coordinates and placeholder grids such as `AA00` are
rejected before the completion check, so they remain eligible for a fallback
fill. `Call` is the canonical lookup input and is never replaced. Country,
continent, DXCC, CQ and ITU fields remain owned by the ADR 0017 country layer;
callsign-provider values for them are filtered before merging.

The completion policy applies when the provider chain already runs: cold miss,
stale refresh, or explicit force-refresh. It does **not** make a fresh cache hit
call upstream merely because a field is still blank. A merged partial result is
cached and retried at the normal station TTL (or when the operator forces
re-enrichment), bounding traffic when no configured provider knows a completion
field.

### Configuration migration

- An existing chain in which every `priority` is absent/zero is assigned
  priorities from its current array order, preserving its authority order.
- A mixed configuration containing both explicit and missing priorities is
  rejected rather than guessed.
- Duplicate, negative, zero or non-contiguous priorities — and duplicate or
  unknown completion-field names — are rejected with a config-path-specific
  error.
- Registry seeding gives a newly discovered provider the next priority and
  leaves it disabled, preserving ADR 0062's non-sparse provider list.
- Disabling a provider does not renumber the rest. Enabling it later restores
  its predeclared place without creating a priority collision.
- An explicitly empty `continue_if_blank` list is the escape hatch for the
  legacy first-substantive-result behaviour.

The existing singular `station_source` remains the highest-priority provider
that contributed substantive data (or `contacted_station` / `none` on the
existing cache/no-data paths). Durable per-field source provenance is outside
this decision; the priority and fill-only rule determines authority without
requiring a cache-schema expansion.

## Alternatives considered

### Keep array order and first-non-empty-wins

This is the ADR 0017 behaviour. It avoids a config migration and makes the
fewest calls, but loses useful gridsquares whenever a higher provider returns
only a name. Array order is also too implicit for a Settings surface that will
show registry-seeded enabled and disabled providers. Rejected because it cannot
express the operator's authority or completeness policy directly.

### Call every enabled provider and merge all responses

This maximises field coverage and could run concurrently. Rejected because it
makes unnecessary premium API calls even when priority 1 already supplied the
two fields Station Manager depends on, increases latency/load, and creates
avoidable conflict arbitration. Priority should determine both authority and
whether another call is justified.

### Let lower-priority providers overwrite higher-priority fields

Whole-response replacement or ordinary "last non-empty wins" merging is
simple, but makes priority mean call order rather than authority. A fallback
called to obtain a grid could silently replace the preferred provider's name or
address. Rejected because it makes the final record depend on incidental gaps
rather than the configured trust order.

### Merge only the named completion fields

Under this rule a fallback called for a missing grid would discard a QTH,
address or web page it also returned. Rejected because the upstream cost has
already been paid and blank fields have no higher-priority data to protect.
Fill every blank field, while using only the completion list as the call gate.

### Give each provider its own completion list

This allows different stop rules after each priority, but makes the policy hard
to reason about: whether priority 3 runs would depend on which earlier provider
last returned data, not simply on the accumulated station record. Rejected for
the initial design. One chain has one definition of "complete enough to stop."

## Acceptance criteria

1. An unsorted config executes enabled providers in numeric priority order; a
   duplicate or incomplete priority assignment is refused before startup/save.
2. A priority-1 result containing both name and a valid gridsquare makes no
   lower-priority call.
3. If either name or gridsquare remains blank, the next provider runs and fills
   every blank field it supplies without changing any existing field.
4. Empty, not-found and errored providers fall through while preserving prior
   contributions; cancellation stops the remaining chain promptly.
5. Invalid/placeholder location data is normalised to blank before deciding
   whether to continue.
6. If every provider is exhausted with a partial result, that merged result is
   returned and cached; its next fresh cache hit makes no provider call.
7. Callsign-provider country/DXCC/CQ/ITU values never enter the accumulator.
8. Config JSON, masked Settings GET/PUT, config diffing, registry seeding and
   restart construction all preserve the same priorities and completion list.

These are feature-level ATDD scenarios; focused unit tests may cover the field
catalogue, migration and fill-only helper beneath them.

## Consequences

- QRZ can remain priority 1 and QRZCQ priority 2: QRZCQ is contacted only when
  the accumulated record lacks a name or usable gridsquare.
- QTH and every other callsign-owned field gain opportunistic coverage without
  becoming reasons for extra network traffic.
- Priority-1 data is stable in the face of contradictory fallback responses.
- Worst-case cold/stale lookup latency becomes the sum of the enabled provider
  calls, as it already does for empty/error fall-through. Calls remain
  sequential because later calls depend on the accumulated completeness state.
- The Settings enrichment surface must become a generic provider list with an
  exclusive priority control and a chain-wide completion-field control; its
  current QRZ-specific form cannot represent the decision.
- Config normalisation, validation, masking/merge, diff reporting and provider
  construction all gain priority/completion handling. Only one field catalogue
  should define valid completion names and the explicit fill-only merge to
  prevent reflection/tag drift.
- Changing priority or completion policy does not invalidate fresh station
  cache rows automatically. The operator can force re-enrichment immediately;
  otherwise the new policy takes effect as rows reach their station TTL.

## Triggers to revisit

- If premium-service quotas or observed latency make sequential fallback too
  expensive, reconsider per-provider budgets or a narrower completion policy —
  not unconditional fan-out.
- If operators need to know which provider supplied each durable field, add
  per-field provenance to the contacted-station cache; the singular
  `station_source` deliberately does not claim that detail.
- If different operating profiles genuinely need different definitions of
  completion, reconsider the one chain-wide list or move it into per-logbook
  configuration.
- If callsign providers are ever allowed to arbitrate country/DXCC data, revisit
  ADR 0017 first; this ADR deliberately preserves Hamnut ownership.

## References

- ADR 0017 — enrichment pipeline, domain-table cache, sequential callsign chain
  and Hamnut country ownership.
- ADR 0062 — self-registering lookup providers and disabled registry seeding.
- `internal/types/lookup.go` — enrichment and provider configuration shape.
- `internal/lookup/orchestrator.go` — chain execution, normalisation and cache
  boundaries.
- `cmd/smd/main.go` `buildEnrichment` — runtime provider construction.
