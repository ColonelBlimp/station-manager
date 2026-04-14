# Station Manager — Bug Inventory

**Status:** v1 analysis document, 2026-04-14. A catalog of known issues in v1, both fixed and unresolved. Each entry includes severity, current state, and disposition.

**Purpose:** make the "known-wrong" list visible. Two uses:
1. Refactor path: a concrete list of things to fix.
2. Rewrite path: a concrete list of things v2 must not recreate, plus lessons about *why* each one happened.

**Severity scale:**
- **Critical** — silently corrupts data, blocks core workflows, or violates safety requirements.
- **High** — affects correctness of a workflow but has a workaround, or affects a path that's not currently exercised but would be load-bearing.
- **Medium** — design friction or maintenance burden; code works but is fragile or painful to extend.
- **Low** — dead code, aspirational scaffolding, or cosmetic issues.

---

## Fixed this session (2026-04-14)

### Hamnut-blocks-logging bug — **CRITICAL**

**Location:** `apps/logging/backend/facade/qso_init.go`, `initCountrySection` function.

**Description:** A failed hamnut.com country lookup propagated as a fatal error through `initializeQso`, blocking the operator from starting a new QSO entirely — even when the local country cache had valid rows for that callsign. The code refused to use the cached data if it couldn't confirm it against the online service.

**Impact:** any time hamnut.com was unreachable or slow, the logging app became unusable for new QSO entry. Offline logging was effectively impossible.

**Root cause:** the fallback logic in `initCountrySection` returned an error when the hamnut lookup failed, and the caller in `initializeQso` treated that error as fatal. The fallback path that returns `dbCountry` (the cached row) existed in the code but was bypassed because the error was still being propagated.

**Fix (applied):** hamnut lookup failures now log a warning and fall through. If the local row exists (cache hit), return it with `IsNewEntity = false` (because we can't confirm new-entity status without the online lookup — false positives on the "NEW ONE!" alert are worse than missing it). If no local row exists, synthesize `types.Country{Name: "Unknown"}` so logging can still proceed.

**Invariant it violated:** "Enrichment never blocks logging" (see `invariants.md`).

**Lesson for v2:** every enrichment path must have an explicit "external service is down" test. Writing that test first, before the happy path, catches this class of bug at design time.

### LogQso / UpdateQso atomicity gap — **CRITICAL**

**Location:** `apps/logging/backend/facade/facade.go`, `LogQso` and `UpdateQso`.

**Description:** The sequence `InsertQso → insertOrUpdateContactedStation → insertOrUpdateCountry → InsertQsoUpload` in `LogQso` ran as four **sequential, non-transactional** database writes. If `InsertQso` succeeded but `InsertQsoUpload` failed (DB busy, schema issue, anything), the QSO was durably stored but the caller saw an error. A caller that retried — reasonable behavior given "LogQso failed" — would produce a duplicate QSO. `UpdateQso` had the same shape.

**Impact:** silent data corruption under partial-write scenarios. Operator sees "failed to log QSO," retries, ends up with two identical QSOs and only one of them queued for forwarding.

**Root cause:** the facade's `LogQso` was written as independent calls to the DB service rather than as a transactional unit. The DB service had a `BeginTxContext` method but no transactional variants of `InsertQso` / `UpdateQso` / `InsertQsoUpload`, so the facade couldn't wrap the writes in a transaction without breaking the call shape.

**Fix (applied):** added three transactional methods to the sqlite Service (`InsertQsoTx`, `UpdateQsoTx`, `InsertQsoUploadTx`) that run against a caller-supplied `*sql.Tx`. Updated the facade's `DatabaseServiceInterface` to match. Rewrote `LogQso` to open a transaction, run the QSO insert + upload insert atomically, commit, then do the cache writes (`insertOrUpdateContactedStation`, `insertOrUpdateCountry`) outside the transaction as best-effort (because they're enrichment, not authoritative — see the enrichment-never-blocks-logging invariant). `UpdateQso` received the same treatment. Added defensive `committed` flag + deferred rollback for safety.

**Invariant it violated:** "One-fails-all-fail for QSO writes" (see `invariants.md`).

**Lesson for v2:** **the atomic unit is "authoritative write plus its related state in authoritative tables."** Cache/enrichment writes must live outside the transaction explicitly, or they'll be silently included and will start blocking the authoritative path on cache failures. Draw the transaction boundary consciously at design time, not accidentally at coding time.

---

## Open issues

### QsoAdditionalData intermediate struct — **MEDIUM**

**Location:** `internal/types/` (the struct), `internal/database/sqlite/adapters/type_to_model.go` (its usage).

**Description:** The sqlite adapter's forward direction (`types.Qso → models.Qso`) manually copies fields from `types.Qso` into a separate `types.QsoAdditionalData` struct, then marshals `QsoAdditionalData` to JSON for the `additional_data` blob column. This creates a second shape that must be kept in sync with `types.Qso` by hand, violating the "adding a new ADIF field is a one-line change" design goal of the `additional_data` pattern.

**Impact:**
- Every new ADIF field is two edits, not one (`types.Qso` AND `types.QsoAdditionalData`).
- 100+ lines of manual field copying in `type_to_model.go`.
- The forward and reverse adapter directions are asymmetric (reverse unmarshals the blob straight into `types.Qso`, which is actually the right thing).
- Forgetting to update `QsoAdditionalData` when adding a field silently drops the field from storage with no compile-time error.

**Root cause:** someone at some point reasoned "I shouldn't marshal the whole `types.Qso` into the blob because some fields are promoted to columns," and built `QsoAdditionalData` as the "correct subset" to marshal. That reasoning is wrong but non-obviously so — the duplication cost of marshaling the whole Qso is trivial (~50 bytes per row for duplicated promoted-column values), and the column-overlay-on-read pattern makes the blob's duplicate copies safely ignorable.

**Fix (planned, not yet applied):** delete `types.QsoAdditionalData`. Rewrite `QsoTypeToModel` to use `json.Marshal(qso)` for the blob and explicit column assignments for the promoted fields. The reverse direction (`QsoModelToType`) is already close to the right shape — it unmarshals the blob into `types.Qso` and overlays the column values — just needs tightening. Total adapter code collapses from ~200 lines to ~50.

**Severity rationale:** not critical because the code currently works, just painfully. Medium because the "painfully" is ongoing — every new ADIF field pays the tax — and because the maintenance burden is what caused the whole adapters package to be labeled "abandoned as too complicated."

**Invariant it violates:** "Adding a new ADIF field should be a one-line change" (see `invariants.md`).

**Disposition:** fixable in v1 as a cleanup commit (code-level fix, not architecture-level). If v2 is rebuilt, implement the simplified shape from day one.

**Related:** `design-decisions-log.md` → "QsoAdditionalData intermediate struct"; `lessons-for-v2.md` → "Mostly-blob + promoted fields pattern."

### Hardcoded QRZ forwarder in LogQso / UpdateQso — **HIGH**

**Location:** `apps/logging/backend/facade/facade.go` at `LogQso` and `UpdateQso`, calls to `InsertQsoUploadTx(..., upload.OnlineServiceQRZ)`.

**Description:** The `InsertQsoUpload` call at ingest time hardcodes `upload.OnlineServiceQRZ` as the destination service. Any additional forwarder (ClubLog, future SM-Online, email) requires editing the hardcoded site. There is no way for the upload queue to know about any other destination.

**Impact:** blocks multi-destination forwarding entirely. Currently only QRZ is wired up; adding ClubLog requires touching the ingest code rather than adding configuration.

**Root cause:** when forwarders were first introduced, there was only QRZ. The call site was written as if QRZ were the only destination, and the pattern was never revisited when ClubLog and SM-Online became part of the plan.

**Fix (planned, not yet applied):** redesign the forwarder configuration model. Probable shape: `ForwarderConfig` with name, service type, enabled flag, action filter (which actions this forwarder handles — insert/update/delete), and per-destination credentials. `LogQso` iterates over `ForwarderConfigs()` and calls `InsertQsoUploadTx` once per enabled forwarder. This is a proper redesign, not a one-line fix, because it touches the forwarder config shape, the config service API, and the facade ingest path.

**Severity rationale:** high because it blocks a load-bearing feature (multi-destination forwarding) that's central to the whole "forward to online services" core concern. Not critical because nothing silently breaks today.

**Disposition:** the author has flagged this as "probably needs a re-design." Fix is a candidate for either late v1 cleanup or early v2 design. The redesign should probably be done alongside the upload queue API in v2.

**Related:** `design-decisions-log.md` → "Hardcoded QRZ forwarder."

### DatabaseServiceInterface vs `*sqlite.Service` signature mismatch — **MEDIUM**

**Location:** `apps/logging/backend/facade/interfaces.go` (the interface), `apps/logging/backend/facade/service.go` (the concrete field), `apps/logging/backend/facade/mocks_test.go` (the mock).

**Description:** `DatabaseServiceInterface` defines methods like `InsertQsoUpload(qsoId int64, action interface{}, service interface{}) error`. The concrete `*sqlite.Service` defines `InsertQsoUpload(id int64, action action.Action, service upload.OnlineService) error`. These signatures are **not compatible** — a method with typed enum parameters does not satisfy an interface method with `interface{}` parameters.

The facade's Service struct declares `DatabaseService *sqlite.Service` (concrete type, not the interface). The interface is used only by `MockDatabaseService` in tests. The mock is never actually instantiated — all integration tests use real `&sqlite.Service{}` instances with in-memory databases.

**Impact:** aspirational scaffolding that doesn't actually serve its purpose. A refactor that tried to switch the facade to interface-based typing would find the interface doesn't match reality and would have to be updated across every method. Dead weight on the codebase.

**Root cause:** the interface and mocks were probably added with an eye toward future unit testing, but tests ended up using real DBs (the right call), so the interface was never exercised. It drifted from the concrete type over time.

**Fix (planned, not yet applied):** two options:
1. **Delete the interface and its mocks entirely.** Cleanest if you don't plan to unit-test with mocked DBs. The integration-test pattern (`&sqlite.Service{}` with in-memory DB) is actually better anyway.
2. **Update the interface to match the concrete type.** Keep it if you might want interface-based testing or interface-based swapping in the future.

Recommended: option 1 (delete) for minimal cleanup. Option 2 only if there's a real need for the abstraction.

**Severity rationale:** medium because it's not a bug per se, but it's dead weight that misleads readers into thinking there's an abstraction where there isn't one.

**Disposition:** v1 cleanup commit.

**Related:** `design-decisions-log.md` → "Interface vs concrete-type mismatch."

### `internal/adapters/` — generic reflection-based adapter framework — **LOW (dead code, slated for deletion)**

**Location:** `internal/adapters/` including `converters/{common,sqlite,postgres}/`.

**Description:** A sophisticated reflection-based struct-to-struct adapter framework with 30+ test files, builder API, generics, field converters, tag-based ignores, and `AdditionalData` JSON handling. Was built to be a generic solution for the same problem `database/sqlite/adapters` solves manually.

**Impact:** ~40 files of dead code that nobody uses but everybody reading the repo has to mentally classify. The package name is confusingly similar to `database/sqlite/adapters` and has caused real confusion during this very analysis session.

**Root cause:** "abandoned as too complicated to maintain and use correctly," per the author. This is the archetypal example of over-generalizing: the design looked clean at the whiteboard stage and became unmaintainable in practice. Generic Go frameworks that try to abstract over different backends' conventions almost always end up this way.

**Fix (planned, not yet applied):** delete the entire `internal/adapters/` tree. Do not carry into v2. Do not try to rebuild a lighter version — per-driver (`database/sqlite/adapters/`) is the settled pattern and works.

**Severity rationale:** low because it doesn't break anything, it's just dead weight. But impact on analysis is real: it caused confusion during this session's review and would cause the same confusion for anyone else reading the repo.

**Disposition:** v1 cleanup commit.

**Lesson for v2:** see `lessons-for-v2.md` → "Build specific, not generic."

### WSJT-X UDP listener is dead code — **LOW (but architecturally significant)**

**Location:** `internal/listeners/handlers/wsjtx/` (parser, handler, tests).

**Description:** The WSJT-X UDP listener exists in the repo but **has never run in a working configuration end-to-end**. WSJT-X and JTDX both require exclusive access to the USB serial port for CAT/PTT, which conflicts with Station Manager's own serial library. Running WSJT-X on the same machine requires giving up rig control from Station Manager, so the UDP ingest path was never actually exercised.

**Impact:** code that looks like an active ingest path but isn't. Confuses analysis (as it confused me during this session — I initially wrote the architecture map assuming this was a working path). Represents a false start on the WSJT-X integration problem.

**Root cause:** the listener was written in anticipation of an ingest path that the serial-contention problem made impossible before it could be exercised. The attempt to solve that problem led to writing the user's own FT8 library (now in a separate repo), which is why the WSJT-X listener sits unused.

**Fix (planned, not yet applied):** delete `internal/listeners/handlers/wsjtx/`. Investigate whether `internal/listeners/` itself is now also dead (if wsjtx was the only handler). V2's WSJT-X ingest plan is different: a separate `wsjtx-bridge` client process that translates UDP → the daemon's HTTP API, which has nothing to do with `internal/listeners`. This requires the serial/CAT bridge to exist first, so WSJT-X and Station Manager can coexist on one rig.

**Severity rationale:** low *per se* because it's dead code that doesn't affect anything running. But architecturally significant as the clearest single example of "v1 accumulated code in response to a problem that the architecture couldn't actually solve" — which is one of the strongest motivations for considering v2.

**Disposition:** v1 cleanup commit.

**Related:** `architecture-map.md` → observation #9; `design-decisions-log.md` → "WSJT-X listener deletion."

### `internal/ft8/` and `internal/ft8x/` — **LOW (slated for removal)**

**Location:** `internal/ft8/` with subpackages `codec`, `dsp`, `message`, `service`, `synth`, `timing`; plus `internal/ft8x/`.

**Description:** The FT8 implementation work that spawned the separate `go-ft8` and `goft8x` projects in other repos. Per the author, FT8 has been extracted and these directories should be removed.

**Impact:** dead weight in the monorepo. Not harmful, just confusing for anyone reading the repo expecting FT8 to be part of Station Manager.

**Disposition:** delete both directories. Also delete `cmd/ft8/` and `cmd/ft8test/` (which depend on them), remove them from `go.work`, and archive or delete the FT8-related docs in `docs/` (`whats-next.md`, `ft8-library-assessment.md`, `ft8-ft4-implementation-research.md`, `ft8-callsign-constants-verification.md`, `ft8-decoder-testing-handoff.md`). The docs may be better moved to the FT8 repo rather than deleted outright.

### `internal/audio/` — **LOW (investigation needed)**

**Location:** `internal/audio/`.

**Description:** Audio I/O package. Per `docs/whats-next.md`, it was item 1 of the FT8 pipeline ("✅ Complete"). Unclear whether it has any consumer outside FT8.

**Impact:** if FT8-only, it's dead weight once FT8 is removed. If it has other uses (voice-keyer playback, SSB recording, general WAV handling), it stays.

**Disposition:** reverse-dependency check before the FT8 cleanup commit. Search for imports of `internal/audio` — if only `internal/ft8*` references it, delete; otherwise keep and note what's still using it.

### `internal/listeners/` framework — **LOW (investigation needed)**

**Location:** `internal/listeners/` (service.go, listener.go, internal.go, helpers.go, error_msgs.go, listeners_test.go).

**Description:** Generic listener framework for registering UDP/TCP listeners. The only handler in `internal/listeners/handlers/` is `wsjtx/`, which is slated for deletion.

**Impact:** if wsjtx is the only consumer, the framework has no active users and is dead code. If there are other handlers planned or present that weren't spotted, it stays.

**Disposition:** reverse-dependency check when wsjtx is being deleted. If the framework has no remaining consumers, delete it too.

### `internal/database/postgres/` — **LOW (relocate, not delete)**

**Location:** `internal/database/postgres/`.

**Description:** The postgres database package, diverged from sqlite and not used by any current client code. Originally intended for the future public SM-Online server.

**Impact:** dead weight in the client repo. Not harmful, just not serving its intended purpose.

**Disposition:** do **not** simply delete. Stage for **relocation** to the future server repo (whenever that exists). Until then, keep the current state tagged in git history for recovery when needed. The short-term action is "remove from this repo" rather than "delete entirely" — the eventual home is elsewhere.

---

## Secondary observations (not bugs, worth tracking)

### Lack of regression test safety net — **MEDIUM**

**Description:** Regression tests are "virtually non-existent" (per the author). The `facade_integration_test.go` file exists and uses the correct pattern (real `&sqlite.Service{}` with in-memory DB), but coverage of the QSO lifecycle end-to-end is thin.

**Impact:** any refactor (including the adapters simplification and the hardcoded-forwarder fix listed above) is riskier than it should be because silent behavior drift won't show up in CI. The fixes landed this session (hamnut, atomicity) also lack regression tests.

**Disposition:** before any substantial refactor work, add characterization tests for the critical QSO lifecycle paths. See `lessons-for-v2.md` → "Characterization tests before refactoring." At minimum: one test per failure mode of the enrichment-never-blocks-logging invariant; one test per failure mode of the one-fails-all-fail invariant; end-to-end round-trip tests for `NewQso → LogQso → FetchQso` and `UpdateQso → FetchQso`.

### Dead doc files in `docs/` — **LOW**

**Description:** Several FT8-related docs (`whats-next.md`, `ft8-library-assessment.md`, `ft8-ft4-implementation-research.md`, `ft8-callsign-constants-verification.md`, `ft8-decoder-testing-handoff.md`) describe work that has been extracted to separate repos. They have no ongoing relevance to this repo.

**Disposition:** move to the FT8 repo if still relevant there, delete otherwise. Same cleanup commit as the FT8 code deletion.
