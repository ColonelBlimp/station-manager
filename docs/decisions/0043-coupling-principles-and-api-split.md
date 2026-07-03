---
number: 0043
title: Coupling principles for v2, and the internal/api split
status: Proposed
date: 2026-07-03
---

# 0043 — Coupling principles for v2, and the internal/api split

## Context

A coupling audit of the tree (measured from the intra-module import graph — 55
packages, 185 internal edges — plus per-package instability, abstractness, and
git churn) found the structure healthy overall but surfaced one clear outlier:
`internal/api` has efferent coupling **Ce = 22** (it imports `bridge`, `ft8`,
`qsoservice`, `forwarding`, `lookup`, `email`, `config`, `database/sqlite`,
`events`, six enum packages, plus the `frontend`/`manual` embeds) and the
second-highest churn in the tree (139 commits). Its instability I = 0.96 is
*correct* for a top-level HTTP layer; the problem is **breadth** — a single
package that imports the whole system is where incidental coupling accretes one
import at a time. The root cause is visible in `server.go`: one god-`Server`
struct holding every concrete service, an 11-argument constructor, and ~40
handler files (already grouped by concern) fused into one package.

The audit also confirmed the parts that are *right*: stable foundations are
tightly coupled on purpose (`errors` fan-in 25 / churn 9; `types.Qso` additive —
verified strictly additive since the v2 restructure), and the most volatile
subsystem (`ft8`, churn 224) is loosely coupled (no `qsoservice`/`bridge`
import; injected seams; SSE). That contrast is the whole lesson, and it wasn't
written down anywhere as doctrine.

Separately, the announcement infrastructure is further along than a first
reading suggested. `bridge` announces rig state, and `qsoservice` **already
publishes** `qso.stored`/`qso.updated`/`qso.deleted` on the main `events.Hub`
for every QSO (submit/update/delete alike). Those payloads are deliberately
minimal (`{qso_id, logbook_id}` — "clients re-query for details") and currently
have **no SPA consumer**: the logging SPA subscribes only to `/v1/rig/events`
and `/v1/ft8/events`. The session log is therefore populated by two *other*
paths — Phone/CW rows added client-side from the `POST /v1/qso` response, FT8
rows from the rich `ft8-logged` event on the FT8 hub. The forwarding worker
drains the durable `qso_upload` table on a `time.Ticker` poll; the upload-queue
row is written atomically with the QSO row (one-fails-all-fail, per the
invariant). These facts shape how far "event-based" can go — and mean the
useful change-notification primitive already exists, in the right minimal shape.

> **Correction (2026-07-03).** An earlier draft of this ADR asserted
> "`qsoservice` does not announce after a submit." That was **wrong** — it
> publishes `qso.stored`/`updated`/`deleted`. The error was caught while
> implementing ADR 0043 (reading the code directly rather than trusting a grep).
> The Consequences "announce" item is reframed accordingly: the spine exists and
> is correctly minimal; the open work is a *consumer*, deferred.

## Decision

Adopt seven coupling principles as v2 doctrine, unified by one meta-rule —
**tighten what's stable, loosen what's uncertain** — and apply them first by
splitting `internal/api` into per-surface handler packages behind
**consumer-defined port interfaces**, guarded by boundary tests, with data
translated and validated at the HTTP edge.

The seven, each with its SM mechanism:

1. **Hide information behind APIs** — per-surface handler packages exposing
   `New(...) + Mount(mux)`; a shared `internal/api/httpkit` leaf hides
   response/body/limit/recovery mechanics.
2. **Guard boundaries between services** — extend the `internal/bridge/
   boundary_test.go` import-scan idiom to the api layer (see Consequences).
3. **Translate & validate at the edge** — decode + `validate.Struct` + translate
   to a domain type *in the handler*; inward of a port, inputs are trusted.
4. **Announce, don't command** — best-effort reactions ride the events hub;
   commands are reserved for the atomic durable write. **The event boundary is
   the transaction boundary.**
5. **Consume parsimoniously, produce generously** — ports expose the 2–4 methods
   a surface actually calls; event payloads stay rich (e.g. `LoggedQso` carries
   country/DXCC/zones/UUID) so consumers self-serve.
6. **Use messages unless semantics are fixed** — fixed-semantics interactions
   (ADIF/`types.Qso`) keep a shared type + direct call; uncertain/evolving ones
   (FT8, rig display) use messages/events.
7. **Tighten what's stable, loosen what's uncertain** — the meta-rule; the
   instability/abstractness/churn metrics are how compliance is *measured*.

## Alternatives considered

### Leave `internal/api` as one package

Ce = 22 is a smell, not a bug — everything works and the handlers are already
file-grouped. Rejected because the audit shows the breadth grew *unopposed*
(nothing fails when a handler adds a subsystem import), so it will keep
accreting; and it directly violates principle 7 by tightly fusing the most
volatile surfaces (ft8, rig) with stable ones. The cost of acting rises with
every added handler.

### Split by surface but keep the shared `*Server` god-struct

Move handlers into sub-files/sub-packages but let them all take `*Server`.
Rejected because it doesn't reduce the package's import fan-out (the god-struct
still holds every concrete service) and doesn't hide information (every handler
still *can* reach every service). It's cosmetic — the coupling the metrics
measure is unchanged.

### Extract interfaces everywhere for mockability

Define an interface for every service and inject mocks in tests. Rejected on the
project's own cautionary tale (`internal/adapters`, the abandoned reflection
framework) and the standing lesson "if an interface's only consumer is a mock,
delete it." Ports here are justified *only* where they narrow a genuinely wide
concrete surface at the outer HTTP boundary (qsoservice's 13 methods → 3), and
are exercised by the **real** service in integration tests — not manufactured
for mocking. Already-narrow services (`events.Hub`, `buildinfo`) are passed
concrete.

### Go fully event-driven — message everything, including the QSO write

Make the QSO row and its upload-queue row separate event-driven reactions.
Rejected because it breaks the load-bearing **one-fails-all-fail** invariant: an
ephemeral event cannot carry a durable atomicity guarantee, and v1's cautionary
tale is exactly a QSO stored without its upload-queue row. Hence principle 4's
sharp form — command inside the transaction, announce outside it; durable work
rides the queue *table* as system-of-record, and an announcement only *notifies*
to cut latency.

### Parallel request/response DTOs for every endpoint

A dedicated `XRequest`/`XResponse` per route. Rejected where it collides with the
"reuse `types.X`, don't build parallel structs" idiom and the `QsoAdditionalData`
asymmetric-round-trip cleanup: where the wire shape *is* the domain shape
(`types.Qso` is the ADIF wire model), validate it in place (unmarshal-overlay +
stash-restore). An edge DTO is warranted only where the wire contract genuinely
differs from any domain type (rig command, `/ft8/qso/start`, `config` PUT).

## Consequences

- **Target layout:** root `internal/api` (lifecycle + route composition + global
  middleware) → `api/httpkit` (leaf) → one package per surface (`qso`,
  `logbook`, `rig`, `ft8`, `config`, `enrich`, `forwarders`, `session`,
  `stream`, `static`, `meta`), each importing at most one subsystem service.
- **Measured goal:** root `api` Ce **22 → ~9**; each handler coupled to a
  2–4-method port rather than a concrete service; `httpkit` becomes a new
  low-instability, high-fan-in leaf on the main sequence (like `logging`).
- **Boundary guards (new `internal/api/boundary_test.go`):** (1) siblings
  isolated — `api/X` must not import `api/Y`; (2) sub-packages must not import
  root `api` (acyclic; they may import only `httpkit`); (3) **the anti-regression
  ratchet** — each `api/<surface>` imports **at most one** subsystem service
  package, so a handler group growing a second dependency fails CI.
- **Announce-don't-command — spine present, consumer deferred.** The change
  primitive already exists: `qsoservice` publishes minimal `qso.stored`/
  `updated`/`deleted` on the main hub. It is intentionally left **unconsumed and
  minimal** — a change-notify + re-query shape, which is the *correct* primitive
  for future live-sync (fat events are a sync anti-pattern), so it should not be
  fattened. Unifying the session log onto it (making the SPA subscribe, retiring
  the `POST`-response add and `ft8-logged`) is DRY-against-two-working-paths and
  is **deferred** until a real second consumer (live multi-device sync, *beyond*
  the ADR-0040 P1 backup/restore scope) pulls the shape. A best-effort "enqueued"
  notification to wake the forwarding worker (currently `Tick`-polled) stays a
  noted optional latency optimization, not worth it on a single-user install now.
- **Cost — the test suite:** ~9,000 lines of api tests are built around
  constructing one `Server`. Migration is incremental, not big-bang (see below);
  during transition the root `Server` delegates to the extracted groups so tests
  stay green surface by surface.
- **Migration order:** (1) extract `httpkit`, convert `Server` helpers to
  delegates — mechanical, no behaviour change; (2) peel one isolated surface
  (`enrich` or `ft8`) as the pattern-setter; (3) repeat most-isolated →
  most-tangled (`config`/`qso` last); (4) delete the god-struct when the last
  surface moves.
- **Cost — more packages** (~11 where there was 1). Accepted: each is small,
  single-purpose, and independently testable, which is the point.

## Triggers to revisit

- If the `httpkit` extraction (step 1) can't be done test-green in one pass, the
  helper surface is more entangled than assumed — stop and reassess the seam.
- If a surface's port stabilises to a single concrete impl with no second caller
  *and* no integration value, collapse it back to the concrete type (honour the
  "delete mock-only interfaces" lesson).
- If the split pushes cross-surface logic into `httpkit` until `httpkit` itself
  grows a subsystem import, the boundary is in the wrong place — that shared
  logic belongs in a domain package, not the HTTP kit.
- If a second client/topology (e.g. the parked split-host `cmd/bridge`, or a
  non-SPA consumer) appears, re-check whether the surface groupings still match
  the consumers (principle: enumerate all API consumers before designing).

## References

- ADR 0013 — narrow daemon scope (package-boundary enforcement; the precedent
  for boundary tests and the `internal/bridge/boundary_test.go` idiom).
- ADR 0009 — hub replay caches; ADR 0026/0027 — inbound command / tune paths
  (the existing command-vs-announce surfaces).
- `docs/v1-analysis/invariants.md` — one-fails-all-fail; enrichment never blocks
  logging; narrow daemon scope; `types.Qso` follows ADIF.
- `docs/v1-analysis/lessons-for-v2.md` — build specific not generic
  (`internal/adapters`); delete mock-only interfaces; asymmetric round-trips
  (`QsoAdditionalData`).
- Anchored code: `internal/api/server.go` (god-`Server` + 11-arg `New`);
  `internal/qsoservice/{submit_batch,delete}.go` (atomic QSO + upload-queue tx);
  `internal/forwarding/worker/worker.go` (Ticker poll); `internal/bridge/
  events.go` (existing announcements).
