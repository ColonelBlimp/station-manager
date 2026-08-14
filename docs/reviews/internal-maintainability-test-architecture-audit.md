# Internal maintainability and test-architecture audit

**Status:** review complete; actions open  
**Reviewed:** 2026-08-14  
**Scope:** production/test shape, complexity and duplication controls, obsolete
compatibility surfaces, test seams, generated-code workflows, build-tag coverage,
and deterministic regression coverage under `internal/`; CI and developer tooling
read where they define those controls  
**Code changes:** none; this document is the review deliverable

## Executive summary

The internal tree has a stronger maintenance posture than raw size suggests. It has
292 production Go files and 327 test files; most stateful packages have substantial
behavioral and race coverage. CI runs both `-race -short` and a full non-race suite,
builds the shipped CGO-free daemon, and separately builds/tests the PocketFFT/CGO
variant. Generated SQLite models are consistently marked `DO NOT EDIT`, the fake
forwarder is excluded from release builds with the `dev` tag, and the repository has
an unusually explicit, version-pinned maintainability-metrics baseline.

The review nevertheless found **seven action themes**: four P2 and three P3. The only
immediately reproducible functional defect is in the optional `logging_debug` build:
an existing logging test panics because the tagged `trackLocation` implementation
dereferences a nil `LoggingConfig`. CI never tests that tag, so the default suite stays
green while the diagnostic variant is broken.

The remaining findings are maintenance multipliers rather than release failures. The
complexity gate blanket-exempts the very functions most likely to grow dangerously;
its own configuration accurately documents that weakness. The duplication gate also
exempts whole files, including a 2,965-line handwritten SQLite API with three current
duplicate pairs. A 26-method v1 compatibility facade has no production callers but is
kept alive by tests. Several packages compile test-only methods, branches, delays and
mutable global hooks into production, making test isolation depend on global cleanup.

Finally, generated-model reproducibility is asymmetric. SQLite has checked-in output
but no task that creates its schema deterministically; its generator reads an ignored,
ambient database and the documentation names a migration path that no longer exists.
Cloud has a generation task and setup instructions but deliberately has no generated
models. Both instruct developers to install the latest generator even though checked-in
SQLite output records SQLBoiler 4.19.7. The boundary is marked clearly, but it is not
reproducible or drift-gated.

## Findings at a glance

| ID | Priority | Area | Disposition |
|---|---:|---|---|
| MT-1 | P2 | The untested `logging_debug` build panics in an existing logging test | New, reproduced |
| MT-2 | P2 | Complexity exemptions cannot detect growth in the worst existing functions | Known weakness; current debt measured |
| MT-3 | P2 | Handwritten SQLite/QRZ duplication is hidden by whole-file exemptions | Known debt; suppression scope is wider than the debt |
| MT-4 | P3 | Twenty-six context-free SQLite compatibility methods have no production callers | Removal trigger has arrived |
| MT-5 | P3 | Test-only methods, controls and mutable globals are compiled into production | New consolidation |
| MT-6 | P2 | Generated-model workflows are ambient, version-unpinned and internally inconsistent | New workflow gap |
| MT-7 | P3 | New leaf boundaries lack direct/deterministic tests | New coverage-hardening gap |

Priority meanings follow the other internal reviews: P0 is release-gate work, P1
should be closed before a serious release, P2 is important correctness or
operability work, and P3 is useful hardening.

## MT-1 — the `logging_debug` build panics and is not gated (P2)

The optional logging diagnostic implementation checks:

```go
if s == nil || !s.LoggingConfig.ShutdownTimeoutWarning {
    return ""
}
```

at [`internal/logging/debug_tracking.go:25`](../../internal/logging/debug_tracking.go).
It does not check `s.LoggingConfig` itself for nil. The default build compiles the
no-op implementation in
[`internal/logging/debug_tracking_nop.go:17`](../../internal/logging/debug_tracking_nop.go),
so its tests cannot expose this difference.

The repository's CI explicitly exercises the default, CGO-free, CGO and PocketFFT
build shapes at [`.github/workflows/ci.yml:273`](../../.github/workflows/ci.yml), but
never compiles or tests `logging_debug`. Running one existing test under that tag
reliably panics before writing the event:

```text
go test -tags logging_debug ./internal/logging \
  -run '^TestVersionStamp_EveryRecordCarriesTheBuildVersion$' -count=1

panic: runtime error: invalid memory address or nil pointer dereference
internal/logging.trackLocation(...): debug_tracking.go:26
internal/logging.logEventBuilder(...): helper.go:131
```

This is not a shipped-release defect because the tag is opt-in. It is still P2: the
variant exists specifically to diagnose shutdown-time logging faults, and a diagnostic
path that changes ordinary logger behavior without being tested can fail exactly when
it is most needed.

### Action

1. Make `trackLocation` treat a nil `LoggingConfig` as tracking disabled, matching its
   stated condition that tracking is active only when the shutdown warning is enabled.
   Alternatively, make a non-nil config an enforced constructor invariant everywhere;
   the former is the narrower and more robust diagnostic behavior.
2. Add tagged tests for nil config, warning disabled, warning enabled, increment,
   decrement and snapshot-copy behavior.
3. Add `go test -tags logging_debug ./internal/logging` to CI. The package is small
   enough that this is cheaper and more meaningful than build-only coverage.

### Required tests

The existing version-stamp test must pass under both default and `logging_debug`
builds. A focused tagged test must prove that a partially constructed/no-op Service
does not panic and that a fully configured Service records and clears locations.

## MT-2 — the complexity gate cannot ratchet its exempted hotspots (P2)

The maintainability configuration is unusually candid and well measured. It records
the original distribution, pins golangci-lint 2.11.3, disables output suppression, and
uses high thresholds to keep the debt list visible at
[`.golangci.yml:33`](../../.golangci.yml). That is a good foundation.

The weakness is also documented verbatim in the configuration: an exempted function
can get worse without the gate noticing at
[`.golangci.yml:93`](../../.golangci.yml). The exclusions match by file plus function
text and suppress every finding from the selected metric. They are not score ceilings.
For example, `readLoop`, `onSlotCalling`, `handlePutConfig`, `applyDefaults`,
`injectDependencies`, `ReadWAV`, `prepareQso` and `Update` can all gain more branches
while CI continues to pass.

A fresh production-only scan of `internal/` at the more diagnostic default threshold
found 36 functions with cognitive complexity above 30, 10 with cyclomatic complexity
above 30, and nine below the maintainability-index floor. The current high end includes:

| Function | Cognitive | Cyclomatic | Maintainability index |
|---|---:|---:|---:|
| `bridge.(*Service).readLoop` | 79 | 35 | 15 |
| `ft8.(*Sequencer).onSlotCalling` | 79 | 51 | 13 |
| `api.(*Server).handlePutConfig` | 64 | 40 | 16 |
| `config.applyDefaults` | 61 | 50 | 14 |
| `qsoservice.(*Service).Update` | 54 | 47 | 11 |

These figures do not prove defects and should not prompt casual refactoring of RF
safety state machines. They do show that the current gate is a one-way fence around
new functions, not a ratchet on the existing risk concentration.

### Action

1. Preserve the current global thresholds and exception list, but add a small
   checked-in per-function baseline for exempted production functions. Parse the
   linters' machine-readable output and fail only when an exempted function's score
   worsens beyond its recorded value.
2. Make the baseline command deterministic and version-pinned alongside CI. A score
   improvement should require lowering the stored ceiling in the same change.
3. Refactor in risk order, not score order: first isolate pure parsing/decision helpers
   from the stateful orchestration in `handlePutConfig`, `applyDefaults`, QSO update and
   DI injection. Treat RF read/sequencer loops as staged, invariant-preserving work.
4. Keep the existing source guards and behavioral tests around every extraction; a
   lower metric is not evidence that behavior survived.

## MT-3 — current duplication is hidden by whole-file exclusions (P2)

The duplication baseline exempts all of
[`internal/database/sqlite/api_context.go`](../../internal/database/sqlite/api_context.go)
and [`internal/forwarding/qrz/response.go`](../../internal/forwarding/qrz/response.go)
at [`.golangci.yml:203`](../../.golangci.yml). The comment correctly calls the SQLite
file a genuine refactor candidate, but a path-level exclusion hides future duplication
anywhere in those files as well as the known pairs.

A raw `dupl` run at the configured 150-token threshold confirmed three handwritten
SQLite pairs (reported twice each):

- `FetchQsoByIdWithContext` and `FetchLogbookByIDWithContext`;
- `FetchCountryByNameWithContext` and `FetchCountryByPrefixWithContext`; and
- `MarkUploadSuccessWithContext` and `MarkUploadFailedWithContext`.

The same run confirmed `classifyInsert` and `classifyUpdate` in QRZ as one further
pair. The latter is especially drift-prone: both branches enforce the same success
and required-LOGID contract but construct action-specific operation names and error
messages. A future protocol amendment can easily land in only one classifier.

The SQLite issue is not file length by itself. The 2,965-line file mixes QSO lookup,
reference-cache access, logbook CRUD, upload queue state transitions, history reads
and transaction primitives under one review/conflict unit. The current duplicated
transition pair has already accumulated subtle stale-worker and zero-row rules, so
behavioral drift there has persistence consequences.

### Action

1. Split `api_context.go` by bounded storage concern without changing the public
   Service API: QSO, reference cache, logbook, upload queue/history and transaction
   primitives. This reduces conflict/review scope before abstraction work begins.
2. Extract the upload-completion compare-and-set skeleton into a narrow helper that
   accepts only the fields that legitimately differ. Keep the SQL and zero-row
   classification visible; do not introduce a generic CRUD framework.
3. Share QRZ insert/update classification for the common RESULT/LOGID state machine,
   passing the action/op text explicitly so wire behavior and diagnostics stay exact.
4. Remove the whole-file `dupl` exclusions once the known pairs are closed. If one
   intentional pair remains, narrow the suppression to the smallest supported scope
   and add a comment explaining its semantic independence.

### Required tests

Run the existing SQLite service/queue race tests and QRZ response tables unchanged
through the extraction. Add paired-table cases proving insert and update classify
every RESULT/LOGID combination identically except for action text. Run raw `dupl` on
both files and make the normal CI configuration cover them again.

## MT-4 — the v1 context-free SQLite facade has no production callers (P3)

[`internal/database/sqlite/api.go`](../../internal/database/sqlite/api.go) contains 26
context-free methods which delegate to `FooWithContext(context.Background(), ...)`.
The package documentation says they exist for v1 compatibility and may be removed
when no caller uses them at
[`internal/database/sqlite/doc.go:35`](../../internal/database/sqlite/doc.go).

A repository-wide symbol search found **zero non-test callers for all 26 methods**.
Every method is used only by tests (between one and ten test files each). The shipping
daemon already uses the context-aware surface. Because this is an `internal` package,
there is no supported out-of-module consumer that can preserve a hidden compatibility
requirement.

The tests have therefore become the only reason the obsolete facade survives. Besides
the 130-line wrapper file, that keeps two public ways to perform the same database
operation and makes new tests more likely to choose the less representative one.

### Action

1. Convert the remaining tests to `WithContext`, normally using `t.Context()` or a
   deliberately bounded context.
2. Delete the 26 wrappers and the obsolete v1-compatibility documentation.
3. Add a source/API guard only if the context-aware convention has regressed before;
   otherwise normal compilation is sufficient.

This can be actioned mechanically in small batches by domain and is a good precursor
to the MT-3 file split.

## MT-5 — test-only machinery leaks into production (P3)

Several useful concurrency and fault-injection tests depend on controls declared in
ordinary production files:

- package-global evidence delays/hooks (`writerDelay`, `statusQueryDelay`,
  `checkpointHook`, `measureFailHook`) at
  [`internal/evidence/service.go:76`](../../internal/evidence/service.go), plus
  `profileFaultForTest` at
  [`internal/evidence/profiles.go:52`](../../internal/evidence/profiles.go);
- package-global `afterTombstoneProbeHook` in the cloud store and `afterReadHook` in
  PSK Reporter at
  [`internal/cloud/store/evidence.go:30`](../../internal/cloud/store/evidence.go) and
  [`internal/pskreporter/identity.go:12`](../../internal/pskreporter/identity.go);
- receiver methods used only by tests: `compactForTest`, `statusForTest` and
  `exchangePathForTest` at
  [`internal/evidence/service.go:1009`](../../internal/evidence/service.go),
  [`internal/ft8/sequencer.go:995`](../../internal/ft8/sequencer.go), and
  [`internal/ft8/servicetx.go:1161`](../../internal/ft8/servicetx.go);
- exported registry reset functions, including an entirely unused
  `lookup.ResetConstructorsForTests`, at
  [`internal/lookup/constructors.go:115`](../../internal/lookup/constructors.go); and
- `httpkit.Kit.MaxBody`/`SetMaxBody`, whose repository callers are tests only, at
  [`internal/api/httpkit/httpkit.go:38`](../../internal/api/httpkit/httpkit.go).

The global hooks are the main architectural concern. They affect every instance in a
package, are read/written without synchronization, and make safe `t.Parallel()` use
depend on every test knowing about and cleaning up shared hidden state. The existing
tests serialize those uses, so no current race was reproduced. The shape nevertheless
turns future parallelism and failure cleanup into package-wide premises.

Not all test seams have this problem. `bridge.Service.openClient` is instance-scoped
specifically so parallel tests do not share a hook at
[`internal/bridge/service.go:44`](../../internal/bridge/service.go), and the API's
FT8 interleaving gap is per Server. Those are useful patterns to retain.

### Action

1. Move receiver helpers referenced only by same-package tests into `_test.go` files;
   Go permits test-only methods on production receiver types.
2. Replace package-global fault/delay hooks with instance-scoped fields or unexported
   core functions that accept an explicit hook. Production wrappers pass nil; tests
   call the core or construct an instance with the hook.
3. Remove unused `ResetConstructorsForTests`. For the cross-package lookup registry
   reset, prefer an injectable registry instance/snapshot over exporting a production
   mutation whose name declares it is not production API.
4. Make `httpkit.Kit` immutable after construction. Tests can construct a Kit with a
   small limit rather than mutate a shared one.

### Required tests

After conversion, run the affected packages under `-race` and mark independent tests
parallel where practical. Add a source check that production files do not introduce
new `ForTest` exports or package-global hook names without an explicit exception.

## MT-6 — model generation is not reproducible from a clean checkout (P2)

The generated-code ownership boundary itself is clear: all 11 files under
`internal/database/sqlite/models` carry SQLBoiler's generated `DO NOT EDIT` header,
and the linter excludes that directory as generated output. The problem is the input
and regeneration contract.

SQLite's configuration reads `../../../build/db/data.db` at
[`internal/database/sqlite/sqlboiler.toml:9`](../../internal/database/sqlite/sqlboiler.toml).
That database is ignored by Git. There is no `models:sqlite` task that creates a fresh
database, applies the log and reference migration sets, verifies the expected schema,
and invokes SQLBoiler. Two developers at the same commit can therefore regenerate
from differently migrated ambient databases.

The package documentation is stale as well: it says the models come from
`migrations/0001_init.up.sql` at
[`internal/database/sqlite/doc.go:29`](../../internal/database/sqlite/doc.go), but the
schema is now split into `migrations/log` and `migrations/reference`, with seven and
three versions respectively.

The cloud side has the opposite mismatch. `task models:cloud` is described as
“Regenerate” in [`Taskfile.yml:59`](../../Taskfile.yml) and the developer guide lists
it as a normal loop, while the package correctly states that no `models/` directory
has ever been generated and nothing imports one at
[`internal/cloud/store/doc.go:21`](../../internal/cloud/store/doc.go). Running the task
would create a new, currently unused package rather than regenerate an artifact.

Both setup paths tell developers to install SQLBoiler `@latest` at
[`DEVELOPING.md:155`](../../DEVELOPING.md), while the checked-in SQLite headers and
module dependency identify 4.19.7. Generator-version drift can rewrite output even
when the schema is unchanged.

### Action

1. Pin SQLBoiler and its drivers to the version that owns checked-in output, using a
   checked-in tools module/script or an equally reproducible tool container.
2. Add `models:sqlite` that starts from a fresh temporary schema artifact, applies the
   authoritative migrations in a documented composition, runs the generator, formats
   output and fails if regeneration leaves unexplained drift.
3. Decide whether cloud generated models are part of the design today. If not, remove
   the task from the normal developer loop and label the config as a dormant scaffold.
   If yes, generate/check in the package, add a real consumer and drift-gate it.
4. Update SQLite's package documentation to name the current migration sets and the
   single canonical regeneration command.

### Required tests

Run regeneration twice from clean temporary databases and require byte-identical
tracked output. In CI or a periodic toolchain job, regenerate and require a clean
`git diff`. Include a schema assertion that every modelled table/column is present and
that no migration-only metadata table is generated.

## MT-7 — leaf-boundary tests are absent or host-vacuous (P3)

Two relatively new/narrow packages illustrate gaps that aggregate package counts hide.

`internal/api/httpkit` is intended to be the single owner of JSON response envelopes,
server-error redaction and size-capped body parsing, as its package comment explains at
[`internal/api/httpkit/httpkit.go:1`](../../internal/api/httpkit/httpkit.go). It has no
direct test file and reports 0% when tested as a package. Higher-level API tests cover
several paths indirectly, but a leaf package should pin its own small contract without
constructing the large API Server fixture.

`internal/hardware` has tests, but `TestSerialPorts_NoError` calls the real host and
then only checks returned entries at
[`internal/hardware/hardware_test.go:50`](../../internal/hardware/hardware_test.go).
On a normal CI runner with no serial devices, the loop executes zero assertions and
the test passes. The important stable `/dev/serial/by-id` selection, nil-entry skip,
sorting and enumeration-error paths cannot be driven because the enumerator and
filesystem root are hard-coded. The local short coverage measurement was 28.6%.

### Action

1. Add direct `httpkit` table tests for content type/status, `ErrorNoter`, generic 5xx
   redaction, exact/max+1 body sizes, typed `MaxBytesError`, other read failures, empty
   JSON and malformed JSON.
2. Extract a pure hardware mapping core which accepts enumerated details and a small
   symlink/filesystem seam. Keep one thin host wrapper around the real enumerator.
3. Test zero, nil and multiple devices; stable by-id match and fallback; sorting;
   metadata label priority; and enumeration/readlink failures deterministically.
4. Do not introduce one global coverage percentage as a proxy for correctness. Gate
   these stable leaf contracts directly, and use coverage only to spot newly
   unexercised branches.

## Recommended action order

1. **MT-1:** fix and gate `logging_debug`; it is a present, reproduced panic.
2. **MT-6:** make generated models reproducible before the next schema change.
3. **MT-3:** split the SQLite file and close known duplicate state machines, then
   remove blanket `dupl` exclusions.
4. **MT-2:** add score ceilings for exempted complexity hotspots before more features
   enlarge them.
5. **MT-4:** migrate tests and remove the context-free compatibility facade.
6. **MT-5 and MT-7:** localize test seams and make leaf tests deterministic as packages
   are next touched.

## Validation performed

- Counted production, test and generated Go files and mapped every internal package's
  source/test-file shape.
- Ran golangci-lint 2.11.3 directly without the repository's complexity/duplication
  exclusions to measure the current internal tail and reproduce the four known
  duplicate pairs.
- Searched all 26 SQLite wrapper symbols across production and tests; no production
  caller was found.
- Audited build constraints and confirmed the dev stub is release-excluded and the
  default/CGO/PocketFFT variants are explicitly covered by CI.
- Ran the focused `logging_debug` reproduction; it failed with the nil dereference
  recorded in MT-1. Default `internal/logging` tests passed in the short coverage run.
- Ran direct tests for `internal/api/httpkit`, `internal/buildinfo`,
  `internal/enums/source` and `internal/hardware`; the first three have no tests and
  hardware reported 28.6% coverage.
- Ran `go test -short -coverprofile=... ./internal/...` outside the network-restricted
  sandbox so loopback fixtures could execute. Every buildable package other than
  `internal/api` completed; that package currently has an unrelated compile mismatch
  in `handler_evidence_test.go:69` (`*int64` compared with an integer). Cloud store
  integration coverage was not measured because no disposable Postgres was started.
- No production source or test file was changed.

## Positive controls worth preserving

- Keep the dual short-race/full-test CI strategy and explicit CGO/PocketFFT builds.
- Keep generated-file headers and generated-directory exclusions.
- Keep the fake forwarder behind the `dev` build tag.
- Keep the maintainability tool version pinned and output suppressions disabled.
- Keep behavioral/source invariants as the authority for RF and persistence safety;
  metrics guide refactoring but do not prove correctness.
- Prefer instance-scoped seams such as `bridge.Service.openClient` when deterministic
  concurrency tests genuinely need an interleaving point.
