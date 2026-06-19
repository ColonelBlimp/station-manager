# internal/iocdi code review - 2026-06-19

Scope: fresh review of `internal/iocdi` as a new codebase, plus its daemon
callers in `cmd/smd` and the injected service contracts in config, logging,
SQLite, and QSO service. Reviewed at `efcb6dff`.

Focus areas: correctness, performance, security, test coverage, and
documentation. This is a review artifact only; no production code was changed.

## Summary

The package is small and readable, and the core single-threaded startup path used
by `cmd/smd` is covered by tests and works in the current tree. It supports the
project's service lifecycle: register services, inject tagged dependencies, run
`Initializer.Initialize()` in dependency order, then resolve singleton instances.

The main risks are around the container's public contract rather than the current
daemon wiring. The code and README claim registration/build are guarded, but the
dependency map and build-closed state are not protected as one transaction. Several
misconfigurations also build or register successfully and fail later, which is the
wrong failure point for a startup DI container.

## Findings

### M1 - Register/Build locking does not actually make concurrent registration safe

`Register` and `RegisterInstance` check `built` before taking `regMu`, then call
`checkForDependency`, and only later take `regMu` to write `registeredBeans`
(`internal/iocdi/container.go:59-88`, `internal/iocdi/container.go:104-133`).
`checkForDependency` writes `requiredDependency` as it discovers tagged fields
(`internal/iocdi/internal.go:90-119`). `Build` takes `regMu` only after its own
early `built` check, then iterates `requiredDependency` and `registeredBeans`
(`internal/iocdi/container.go:146-164`).

That leaves two races:

- A registration can pass the `built` check, lose the race to `Build`, then insert
  a new bean after `Build` has completed and set `built=true`.
- A registration can be mutating `requiredDependency` while `Build` is iterating it,
  because dependency discovery happens before the registration lock is acquired.

Impact: a concurrent `ResolveSafe`/`Build` plus registration can panic on a map race,
miss a dependency during validation, or accept a post-build registration that will
never be instantiated. The current `cmd/smd` path registers in one goroutine before
building, but the package-level README explicitly advertises internal locking
(`internal/iocdi/README.md:102-106`), so the implementation should match that
contract.

Recommendation: protect the whole registration transaction with the same state
lock: normalize/check `built`, discover dependencies, validate uniqueness, and
mutate both maps under one lock. Re-check `built` while holding the lock. Add a
race test that runs `Register`/`RegisterInstance` concurrently with `Build` or
`ResolveSafe`.

### M2 - Invalid or duplicate registrations are accepted too early

`Register` documents support only for structs and pointers to structs
(`internal/iocdi/container.go:50-51`), but any pointer kind passes the switch
(`internal/iocdi/container.go:65-75`). A `*int` registration is accepted,
`Build` silently skips instantiation because it only creates pointer-to-struct
beans (`internal/iocdi/container.go:196-210`), and the caller does not see the
problem until `ResolveSafe` reports the bean is not initialized
(`internal/iocdi/container.go:306-308`).

The same registration path also overwrites existing bean IDs without error
(`internal/iocdi/container.go:86-88`, `internal/iocdi/container.go:131-133`) even
though the container comments describe bean identifiers as unique
(`internal/iocdi/container.go:32-34`). A duplicate `ServiceName` typo in startup
wiring can replace a valid singleton or type registration and move the failure
away from the registration line that caused it.

Impact: DI miswiring fails later than it should, and duplicate service IDs are
hard to diagnose from startup logs. For `cmd/smd`, the registration lines are the
right place to fail because they already wrap errors with service-specific
context (`cmd/smd/main.go:213-227`).

Recommendation: reject `reflect.Ptr` values whose element is not a struct in
`Register`, and reject duplicate bean IDs unless a deliberate override API is
added. Cover both cases with tests.

### M3 - Tagged fields can remain unset while Build succeeds

Dependency discovery compresses requirements into `requiredDependency[id] = type`
(`internal/iocdi/internal.go:98-119`). If two receiver fields use the same tag ID
with different field types, the later discovery overwrites the earlier type and
`Build` validates only the last stored requirement (`internal/iocdi/container.go:161-193`).

Injection then silently ignores fields that cannot be set or whose tagged
dependency type is incompatible: unexported tagged fields hit `!fv.CanSet()` and
continue, and incompatible tagged fields fall through with a comment saying to
leave the field untouched (`internal/iocdi/helpers.go:54-57`,
`internal/iocdi/helpers.go:97-104`). In those cases `Build` can return nil even
though a tagged dependency was not injected.

Impact: a service can start with a nil dependency despite an explicit `di.inject`
tag. The failure then happens at first use, outside the dependency graph error
path and usually far from the misconfigured field.

Recommendation: store dependency edges per receiver field, not only one
`requiredDependency` type per bean ID. During build, validate every tagged field:
the field must be exported/settable, the bean must exist or be supplied by the
literal provider, and the value must be assignable. Treat any tagged-but-unset
field as a build error. Add tests for same-ID type conflicts, unexported tagged
fields, and incompatible tagged field types.

### M4 - Initializers run while the registry lock is held

`Build` takes `regMu` at the start of validation and releases it only in the
deferred footer (`internal/iocdi/container.go:151-159`). It computes initializer
order and then calls each `Initializer.Initialize()` before releasing that lock
(`internal/iocdi/container.go:218-266`).

Initializers are arbitrary service code. Current services can touch the
filesystem or validate config during initialization, for example logging creates
the log directory and writers (`internal/logging/service.go:80-130`), SQLite
checks the database directory (`internal/database/sqlite/service.go:35-75`), and
config resolves the working directory (`internal/config/config.go:1530-1542`).

Impact: slow initializer work blocks all container readers/writers. More
importantly, an initializer that tries to resolve from or register with the same
container will deadlock because `Build` still holds the lock and has not yet set
`built=true`. If an initializer fails after earlier initializers ran, `built`
stays false and a later `Build` retry can run already-successful initializers
again. Some current services are idempotent, but the container contract does not
state or enforce that requirement.

Recommendation: keep the graph validation and order computation under lock, then
release the registry lock before invoking user initializers. Add an explicit
`building` state so registrations are rejected while initializers are running.
Document retry semantics, or require and test initializer idempotence.

### L1 - Empty-struct dependencies do not preserve singleton identity

`RegisterInstance` says registered instances are singletons
(`internal/iocdi/container.go:92-93`), and `Build` turns type registrations into
singleton instances (`internal/iocdi/container.go:196-210`). However,
`injectIntoStruct` special-cases pointers to empty structs by allocating a fresh
instance instead of injecting the registered dependency
(`internal/iocdi/helpers.go:61-69`).

Impact: a registered `*T` where `T` is an empty struct is not the instance
actually injected into exact `*T` fields. Empty structs can still have methods and
can be used as marker services, so this violates the singleton rule even if it was
added to avoid pointer-equality surprises in tests.

Recommendation: remove the empty-struct special case and inject the registered
instance consistently. If a test needs distinct dependencies, register distinct
non-zero test types or assert by bean ID behavior rather than pointer inequality
for zero-sized objects.

### L2 - README and comments are stale or misleading

The README quick start resolves `"bervicebean"`, which does not match the
registered `"ServiceBean"` (`internal/iocdi/README.md:37-56`). Its literal-provider
example checks `id == "WorkingDir"` even though tags are lowercased before provider
lookup (`internal/iocdi/README.md:72-79`, `internal/iocdi/internal.go:90-94`). The
README also says interfaces are not supported (`internal/iocdi/README.md:108-112`),
but the package has interface injection tests and implementation
(`internal/iocdi/interface_test.go:18-44`, `internal/iocdi/container.go:179-186`).

The `Register` and `RegisterInstance` comments still say bean IDs are
case-sensitive and must match tag case (`internal/iocdi/container.go:44-51`,
`internal/iocdi/container.go:92-97`), while both methods lowercase IDs and tags
before matching (`internal/iocdi/container.go:63`, `internal/iocdi/container.go:108`,
`internal/iocdi/internal.go:92-94`).

Impact: the examples do not reliably teach the current API, and future callers
can write providers or tags against the wrong case contract.

Recommendation: update README and comments to one contract: bean IDs and tags are
case-insensitive after lowercasing, interfaces are supported, and `internal/iocdi`
is an internal package rather than something installed with `go get
github.com/ColonelBlimp/iocdi`.

## Security notes

I did not find a direct security boundary in `internal/iocdi`; it is startup
wiring code and not exposed to network clients. The security-relevant property is
fail-fast wiring: dependencies should be missing or incompatible at build time,
not later on the request path where a nil service can turn into a panic or a
partially initialized subsystem.

## Test coverage notes

The package has useful tests for happy-path injection, string literal provider
behavior, interface injection, cycle detection, resolver helpers, and initializer
ordering. Missing coverage is concentrated in misconfiguration and concurrency
edges: concurrent register/build, duplicate bean IDs, pointer-to-non-struct
registration, conflicting same-ID dependency types, unexported tagged fields,
incompatible tagged fields, empty-struct singleton identity, and initializer
failure/retry behavior.

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go test ./internal/iocdi ./cmd/smd ./internal/logging ./internal/database/sqlite ./internal/qsoservice ./internal/config`
- `GOCACHE=/tmp/go-build go test -race ./internal/iocdi`
- `GOCACHE=/tmp/go-build go vet ./internal/iocdi ./cmd/smd ./internal/logging ./internal/database/sqlite ./internal/qsoservice ./internal/config`

All passed in the current worktree.

## Resolution (2026-06-19)

Operator scoped this to the low-risk fail-fast wins + docs: **M2, L1, L2 fixed
now; M1, M3, M4 deferred** (the daemon registers single-threaded then builds, so
the concurrency/contract findings aren't exercised today) — logged in
`docs/backlog.md`.

- **M2 (fixed).** `Register` now rejects a pointer to a non-struct (e.g. `*int`)
  with `ErrBeanTypeNotSupported` instead of registering it and silently skipping
  it at Build. Both `Register` and `RegisterInstance` reject a duplicate bean ID
  (new `ErrDuplicateBeanID`) under `regMu` rather than overwriting the earlier
  bean — so a `ServiceName` typo in startup wiring fails at the registration line.
  Confirmed the live `cmd/smd` wiring still builds clean (no latent
  double-registration). Tests: `TestRegister_RejectsPointerToNonStruct`,
  `TestRegister_RejectsDuplicateBeanID`.
- **L1 (fixed).** Removed the pointer-to-empty-struct special case in
  `injectIntoStruct` (it allocated a fresh instance, breaking the singleton
  contract for zero-size marker services); the registered instance is now injected
  consistently. Note: pointer identity for zero-size types is unobservable (Go
  aliases them to `runtime.zerobase`), so the guard test asserts the empty-struct
  dependency injects non-nil rather than identity. Test:
  `TestInject_EmptyStructDependency`.
- **L2 (fixed).** README + doc comments corrected to one contract: bean IDs/tags
  are case-INSENSITIVE (lower-cased); the literal-provider example matches the
  lower-cased id (`"workingdir"`); interfaces ARE supported; the `bervicebean`
  typo fixed; the `go get github.com/ColonelBlimp/iocdi` line replaced with the
  internal-package import path. The concurrency note now states the real contract
  (register single-threaded before Build; concurrent registration is a known gap,
  not a guarantee) rather than over-claiming locking that M1 hasn't delivered.
- **M1 + M3 + M4 (deferred).** Transactional registration (M1), per-field
  build-time tag validation (M3 — flagged to first confirm no latent unsatisfied
  `di.inject` tag, or it becomes a startup failure), and running initializers
  outside the registry lock + a `building` state + retry semantics (M4). All
  harden a concurrent/contract path the single-goroutine daemon never exercises;
  backlogged.

Verified: `gofmt`/`go vet` clean; CGO-free `go build ./...`; `internal/iocdi`,
`cmd/smd`, and the injected-service packages (logging, sqlite, qsoservice,
config) pass; `go test -race ./internal/iocdi` clean.
