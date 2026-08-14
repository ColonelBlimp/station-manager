# Internal package-boundary audit

**Status:** review complete; actions open  
**Reviewed:** 2026-08-14  
**Scope:** production Go under `internal/`; tests and architecture documents were
used to validate intended dependency direction  
**Code changes:** none; this document is the review deliverable

## Executive summary

The production import graph is healthy at its load-bearing boundaries. It contains
**52 internal packages and 178 direct internal edges**. `internal/types` remains
standard-library-only; bridge does not reach storage, forwarding, or QSO
orchestration; FT8 reaches neither bridge nor QSO orchestration; evidence is separate
from decode and QSO storage; and the cloud server has no daemon dependency. The full
`go test ./internal/...` suite passed during this review.

The review found **four action themes**, none of them a current P0/P1 correctness
failure. The most useful work is preventative: documented import constraints are
enforced by three unrelated test implementations, while several equally explicit
constraints have no CI guard. Two current package layouts also impose avoidable
transitive dependency costs:

1. lookup providers import a package that combines their contracts and constructor
   registry with the concrete SQLite-backed orchestrator; and
2. the SM Cloud forwarder package also owns daemon reconciliation and restore-client
   behavior, so registering one forwarding plugin compiles the QSO service and SQLite
   stack.

One unused exported ISO-country-to-DXCC table remains in `utils`; it should be deleted
unless a real consumer is identified. No production source was changed during this
audit.

## Findings at a glance

| ID | Priority | Area | Disposition |
|---|---:|---|---|
| PB-1 | P2 | Documented import constraints are only partially enforced | New |
| PB-2 | P2 | Lookup contracts share a package with the concrete orchestrator | New; exposed by ADR 0062 |
| PB-3 | P2 | SM Cloud forwarding transport also owns daemon orchestration | New structural follow-up to S3/S4 |
| PB-4 | P3 | Unused DXCC mapping is exported from `utils` | New cleanup |

P2 is important architectural work that should be done when touching the affected
subsystem. P3 is useful cleanup. There is no release-gate package-boundary finding.

## Graph baseline

The baseline was generated with `go list` over `./internal/...`, counting production
imports whose path begins with this module's `internal/` prefix. Generated SQLite
models remain graph nodes because production packages compile them, but generated
source was not reviewed for ownership decisions.

The highest fan-in packages are the expected stable foundations:

| Package | Direct internal importers |
|---|---:|
| `internal/errors` | 28 |
| `internal/types` | 22 |
| `internal/logging` | 19 |
| `internal/utils` | 14 |
| `internal/enums/upload/action` | 9 |
| `internal/forwarding` | 8 |

The highest direct fan-out is concentrated at composition and orchestration layers:

| Package | Direct internal imports |
|---|---:|
| `internal/api` | 24 |
| `internal/qsoservice` | 15 |
| `internal/database/sqlite` | 12 |
| `internal/forwarding/smcloud` | 9 |
| `internal/config` | 8 |
| `internal/ft8` | 8 |

Fan-in or fan-out alone is not a defect. The findings below require a documented
direction, an extension boundary, or a concrete responsibility mismatch in addition
to the count.

## PB-1 — documented boundaries are clean but incompletely guarded (P2)

The current graph satisfies the important rules:

- `internal/types` imports only `encoding/json`, `regexp`, and `time`, exactly matching
  its load-bearing standard-library-only contract at
  [`internal/types/doc.go:3`](../../internal/types/doc.go);
- FT8 depends on injected PTT and QSO-log seams rather than importing bridge or
  `qsoservice`, as documented at
  [`internal/ft8/doc.go:43`](../../internal/ft8/doc.go);
- bridge and evidence pass their current boundary tests; and
- the cloud server remains independent of daemon-specific packages, matching
  [`internal/cloud/server/doc.go:51`](../../internal/cloud/server/doc.go).

Enforcement does not cover all of those facts. The only general production-import
guards found under `internal/` are:

- bridge's three-package outward blacklist plus reverse bridge-import scan at
  [`internal/bridge/boundary_test.go:35`](../../internal/bridge/boundary_test.go);
- evidence's five-package blacklist at
  [`internal/evidence/boundary_test.go:19`](../../internal/evidence/boundary_test.go);
  and
- API's deliberate high-water import ratchet at
  [`internal/api/boundary_test.go:91`](../../internal/api/boundary_test.go).

Evidence then implements a second overlapping scan inside an acceptance test at
[`internal/evidence/sync_test.go:594`](../../internal/evidence/sync_test.go). Its list
adds `config` and `adif`, while the package-level boundary list adds FT8, SQLite and
bridge. The two implementations also match imports differently (`Contains` versus
exact path or subpackage). A future edit can therefore update one apparent source of
truth and leave the other behind.

There is no equivalent CI rule for the explicit `types` standard-library-only
constraint, FT8's injected bridge/QSO direction, or the cloud server's daemon
independence. The graph is clean today, but a future import can violate one of those
documented invariants without making the relevant boundary test fail.

### Action

Create one test-only architecture package, for example
`internal/architecture/boundary_test.go`, with a reusable import scanner and a small
table of **declared** rules. Do not freeze every package in the repository. Start
with:

- `types`: standard library only;
- FT8: no bridge, SQLite, forwarding, or `qsoservice` import;
- cloud server: only the shared cloud/domain/cross-cutting packages its package
  contract allows;
- bridge: preserve the ADR 0013 rule in both directions; and
- evidence: merge the two current lists into one rule.

Use exact module-relative package matching with explicit subpackage semantics. Keep
API's existing high-water ratchet package-local: it records an accepted hotspot and
has different shrink-only behavior from an invariant allowlist.

### Required tests

Table-test the scanner itself with temporary source fixtures: exact forbidden import,
forbidden subpackage, similarly named allowed package, standard-library import,
external import, test-file exclusion, and a nested package. Each architecture rule
should include a synthetic violation so the test cannot pass merely because it never
walked the intended files.

## PB-2 — lookup providers inherit the concrete orchestrator's dependencies (P2)

`internal/lookup` owns two different layers:

- provider interfaces, normalization, and the constructor registry; and
- the 754-line concrete orchestration path, including `Result`, cache policy,
  background refresh scheduling, logging, DXCC handling, and two concrete
  `*sqlite.Service` fields at
  [`internal/lookup/orchestrator.go:96`](../../internal/lookup/orchestrator.go).

Provider packages must import `internal/lookup` to implement its interfaces and
register constructors. That import therefore pulls the SQLite-backed orchestrator
into every provider even though QRZ and Hamnut only need the contracts and registry.
The transitive production graph makes the cost visible:

| Imported package | Direct internal imports | Transitive internal packages, including self |
|---|---:|---:|
| `internal/lookup/qrz` | 7 | 23 |
| `internal/lookup/hamnut` | 7 | 23 |
| `internal/forwarding/qrz` | 5 | 7 |

The 23-package provider closure includes SQLite models/adapters, ADIF, forwarding
registries, DXCC data, and `safego`; none is part of making an HTTP provider request.
This is not only a metric smell. ADR 0062 encountered the resulting cycle and had to
put descriptors in a separate `lookupdef` leaf because `config -> lookup -> sqlite ->
config` is impossible. The dependency is recorded in
[`docs/decisions/0062-lookup-provider-registry.md:40`](../decisions/0062-lookup-provider-registry.md).

There is a second removable edge. Both providers still directly import
`internal/config` and expose a `ConfigService` DI path at
[`internal/lookup/qrz/service.go:89`](../../internal/lookup/qrz/service.go) and
[`internal/lookup/hamnut/service.go:81`](../../internal/lookup/hamnut/service.go).
Production constructor registration already receives a resolved
`*types.LookupConfig` and passes it to `NewService` at
[`internal/lookup/qrz/service.go:76`](../../internal/lookup/qrz/service.go) and
[`internal/lookup/hamnut/service.go:60`](../../internal/lookup/hamnut/service.go).
No production call site constructs either lookup provider through the IOC container.
The comments that describe zero-value DI as the production path are now stale.

### Action

Separate provider-facing code from concrete orchestration. The least disruptive
target is:

1. leave provider interfaces, normalization, and constructor registration in the
   root `internal/lookup` package;
2. move `Orchestrator`, `Result`, source constants, refresh port, and their concrete
   SQLite/DXCC behavior to `internal/lookup/orchestrator`; and
3. have API and `cmd/smd` import the orchestrator package, while provider packages
   continue to import only the narrow root contract.

An inverse split into a leaf `lookup/provider` package is also valid, but should not
create parallel copies of the provider interfaces. Preserve the existing two-registry
behavior from ADR 0062.

Then remove `ConfigService` and the `*config.Service` constructor parameter from QRZ
and Hamnut unless a real non-daemon consumer is identified. Require a resolved
`LookupConfig`; this is already how `buildEnrichment` constructs them. Do not add a
new port solely to improve a graph metric—the package move is enough to restore the
direction.

### Required tests

Keep all provider and enrichment behavior tests. Add an architecture assertion that
the provider-contract package cannot import SQLite and that concrete provider
packages cannot import SQLite or the orchestrator package. Keep the registry
descriptor/constructor parity test. After the move, remeasure the QRZ and Hamnut
transitive closures and record the reduction in the change description.

## PB-3 — the SM Cloud forwarding package owns daemon orchestration too (P2)

The transport implementation itself has the expected forwarding-plugin shape: its
production imports are `forwarding`, upload action, `errors`, and `types` at
[`internal/forwarding/smcloud/smcloud.go:37`](../../internal/forwarding/smcloud/smcloud.go).
The package also contains two other responsibilities:

- `reconcile.go` imports SQLite, `qsoservice`, logging, upload origin, and the shared
  hash protocol at
  [`internal/forwarding/smcloud/reconcile.go:12`](../../internal/forwarding/smcloud/reconcile.go);
  and
- `export.go` implements the restore/export HTTP client and its wire records at
  [`internal/forwarding/smcloud/export.go:20`](../../internal/forwarding/smcloud/export.go).

Because Go compiles at package granularity, any import used only to register or test
the SM Cloud forwarder still acquires the whole reconciliation service. The package
has nine direct internal imports and a 23-package transitive internal closure. The
QRZ forwarding plugin has five direct imports and a seven-package closure.

This layout has also outgrown its own documentation. The S3 design describes
`internal/forwarding/smcloud` as a forwarder beside QRZ/ClubLog, while S4 calls
reconciliation a daemon routine. The final invariant nevertheless says “`smcloud` is
just a `Forwarder`” at
[`docs/v2-design/sm-cloud-p1.md:261`](../v2-design/sm-cloud-p1.md), which is no longer
true at package granularity.

The shared `internal/cloud/reconcile` hash package is **not** the problem. S4 requires
both sides to use the same canonicalization, explicitly documented at
[`docs/v2-design/sm-cloud-p1.md:134`](../v2-design/sm-cloud-p1.md). Keep that shared
compile-time protocol.

### Action

Restore the forwarding extension boundary:

1. keep `internal/forwarding/smcloud` limited to registration and `Forwarder`
   submission behavior;
2. move the daemon-owned reconciliation loop and its SQLite/QSO orchestration into a
   package such as `internal/smcloudsync`; and
3. place credential parsing and general SM Cloud HTTP/export behavior in one narrow
   client package if both forwarding and reconcile/restore need it.

The exact client package name is less important than the direction: a forwarding
plugin must not acquire SQLite or `qsoservice` merely because another daemon feature
uses the same endpoint. Do not duplicate credentials or the manifest/hash wire
contract to achieve the split.

### Required tests

Move the existing reconciler and export tests with their owners, retaining the real
SQLite/QSO end-to-end reconciliation coverage. Add a boundary test that every
concrete `internal/forwarding/<type>` package is free of SQLite, `qsoservice`, and
`config` imports. Preserve the current real-cloud-server transport test as a test-only
dependency. Remeasure the forwarder package closure after the split; it should be
comparable to QRZ/ClubLog rather than the daemon orchestration layer.

## PB-4 — an unused DXCC mapping remains exported from `utils` (P3)

[`internal/utils/dxcc_iso2.go:5`](../../internal/utils/dxcc_iso2.go) exports
`DXCCFromISO2` and maintains a hand-written partial ISO 3166-1-to-ADIF table. Repository
search found no production caller; only its own tests reference it. `utils` package
documentation still presents DXCC lookup as a consumed shared helper at
[`internal/utils/doc.go:13`](../../internal/utils/doc.go).

The active enrichment path now uses the data-driven `internal/enums/dxcc` catalogue,
which maps a hamnut primary prefix to an ADIF entity. ISO country and DXCC prefix are
not interchangeable—the existing helper correctly calls out split entities—so the
two tables should **not** be merged mechanically. Keeping an unused second mapping,
however, creates an exported API and data-maintenance obligation with no consumer.

### Action

Delete `dxcc_iso2.go`, its tests, and the corresponding claim in `utils/doc.go` unless
an imminent production consumer can be named. If ISO-country-to-DXCC conversion is
needed later, add it to a dedicated DXCC domain package from an authoritative dataset
and define how ambiguous/split entities are represented; do not restore it as an
ad-hoc `utils` map.

## Reviewed and not raised as new findings

- **API breadth:** `internal/api` remains the largest fan-out package at 24 direct
  internal imports. This is already the subject of proposed ADR 0043, and the current
  shrink-only import ratchet is working. The design explicitly makes migration
  incremental, so this review does not create a duplicate action.
- **Stable shared hubs:** high fan-in for `errors`, `types`, `logging`, and `utils` is
  intentional. `types` and `errors` have no internal dependencies; their current
  direction is correct.
- **Cloud wire and hash packages:** `internal/cloud/evidencewire` and
  `internal/cloud/reconcile` are deliberately shared protocol packages. Sharing them
  prevents client/server canonicalization drift and is preferable to parallel DTOs
  or hash implementations.
- **Locally declared export/PUT envelopes:** the daemon and cloud server deliberately
  declare storage-edge envelopes locally while sharing `types.Qso`; real end-to-end
  tests pin compatibility. No duplicate-contract action is recommended.
- **Audio/CAT/serial:** CGO remains isolated below `audio/capture` and
  `audio/playback`; pure CAT codecs do not import serial I/O. The current dependency
  direction is clean.

## Recommended action order

1. Implement PB-1 first. It is small, makes the current clean graph durable, and
   gives PB-2/PB-3 objective acceptance tests.
2. Do PB-2 when lookup/enrichment is next changed. Move the package boundary and
   remove the unused configuration injection path in the same change so comments and
   construction have one truth.
3. Do PB-3 before adding another SM Cloud daemon feature or forwarding destination;
   otherwise the package-level extension cost keeps spreading.
4. PB-4 is an independent cleanup suitable for a small change.

## Verification performed

- enumerated all production `internal/...` imports with `go list`;
- measured direct fan-in/fan-out and transitive closures for lookup and forwarding
  extension packages;
- checked every package-local import-boundary test and the relevant ADR/package
  contracts;
- searched all production call sites for the lookup `ConfigService` path and
  `DXCCFromISO2`; and
- ran `/usr/lib/golang/bin/go test ./internal/...` successfully.
