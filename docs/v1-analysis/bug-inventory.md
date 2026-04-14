# Station Manager — Bug Inventory

**Status:** v1 analysis document, 2026-04-14 (updated same day after the v1.0.0 cleanup). A catalog of known issues in v1, both fixed and unresolved. Each entry includes severity, current state, and disposition.

**Purpose:** make the "known-wrong" list visible. v2 is now the chosen path (decided 2026-04-14), so this list is primarily "things v2 must not recreate, plus lessons about *why* each one happened." Entries that were fixed in v1 before the v1.0.0 tag are retained here for the historical record and because the lessons behind them carry forward.

> **Update 2026-04-14 (post-v1.0.0):** three cleanup items landed in commit `0e158ec` (tagged `v1.0.0`): the FT8 code/CLIs, the FT8/legacy docs, and the README/audio-README/.gitignore FT8 references. Entries marked FIXED with that date range refer to this cleanup.

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

### ~~QsoAdditionalData intermediate struct~~ — **FIXED (2026-04-14)**

**Location:** `internal/types/` (the struct), `internal/database/sqlite/adapters/type_to_model.go` (its usage).

**Description:** The sqlite adapter's forward direction (`types.Qso → models.Qso`) manually copies fields from `types.Qso` into a separate `types.QsoAdditionalData` struct, then marshals `QsoAdditionalData` to JSON for the `additional_data` blob column. This creates a second shape that must be kept in sync with `types.Qso` by hand, violating the "adding a new ADIF field is a one-line change" design goal of the `additional_data` pattern.

**Impact:**
- Every new ADIF field is two edits, not one (`types.Qso` AND `types.QsoAdditionalData`).
- 100+ lines of manual field copying in `type_to_model.go`.
- The forward and reverse adapter directions are asymmetric (reverse unmarshals the blob straight into `types.Qso`, which is actually the right thing).
- Forgetting to update `QsoAdditionalData` when adding a field silently drops the field from storage with no compile-time error.

**Root cause:** someone at some point reasoned "I shouldn't marshal the whole `types.Qso` into the blob because some fields are promoted to columns," and built `QsoAdditionalData` as the "correct subset" to marshal. That reasoning is wrong but non-obviously so — the duplication cost of marshaling the whole Qso is trivial (~50 bytes per row for duplicated promoted-column values), and the column-overlay-on-read pattern makes the blob's duplicate copies safely ignorable.

**Fix (applied 2026-04-14):**
- Deleted `internal/types/additional_data.go` entirely (contained only `types.QsoAdditionalData` and `types.ContactedStationAdditionalData`, both removed).
- Rewrote `QsoTypeToModel` in `internal/database/sqlite/adapters/type_to_model.go` to call `json.Marshal(qso)` directly for the blob and keep the explicit column assignments for promoted fields.
- Rewrote `ContactedStationTypeToModel` the same way — `json.Marshal(station)` for the blob.
- Rewrote `ContactedStationModelToType` in `model_to_type.go` to `json.Unmarshal` directly into `types.ContactedStation` and overlay the promoted columns, instead of going through the intermediate struct. `QsoModelToType` already had the right shape and just needed a comment clarifying the pattern.
- Added doc comments on all four functions explaining the promoted-columns + blob pattern and why the duplication is safe (columns are authoritative on read).
- Removed the five dead test functions for `QsoAdditionalData` and `ContactedStationAdditionalData` from `internal/types/json_test.go`. All other tests in the file preserved.
- `CountryTypeToModel`, `LogbookTypeToModel`, `CountryModelToType`, `LogbookModelToType` were already clean and not touched.

**Verification:** existing adapter tests (`internal/database/sqlite/adapters/adapters_test.go`) pass unchanged, which serves as the regression guard. The round-trip tests (`TestQsoRoundTrip`, `TestContactedStationRoundTrip`) verify that a populated struct survives marshaling and unmarshaling with the new adapter. Full `internal/...` test suite passes. Full `apps/logging/...` test suite passes.

**Impact of the fix:**
- Adapter code for Qso/ContactedStation collapsed from ~200 lines across both files to ~60 lines.
- Adding a new ADIF field to `types.Qso` is now a one-line change — the adapter doesn't need to be touched.
- No more three-shapes maintenance problem: `types.Qso` is the single source of truth; the blob is shaped like `types.Qso`; the reverse overlays columns.

**Invariant it violated (now upheld):** "Adding a new ADIF field should be a one-line change" (see `invariants.md`).

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

### `internal/adapters/` — generic reflection-based adapter framework — **RECLASSIFIED: server-layer dependency, not dead code**

**Location:** `internal/adapters/` including `converters/{common,sqlite,postgres}/`.

**Description:** A sophisticated reflection-based struct-to-struct adapter framework with 30+ test files, builder API, generics, field converters, tag-based ignores, and `AdditionalData` JSON handling.

**Correction (2026-04-14):** earlier analysis in this document classified `internal/adapters` as "dead code, slated for deletion." **That classification was wrong.** Verified via import graph: `internal/adapters` is an **active dependency** of the top-level `internal/database/` package (specifically `service.go`, `helpers.go`, and `crud_user.go`), which is the server-side database layer that lives alongside the client-side `internal/database/sqlite/` layer we've been working with. It is not dead code in the client repo; it is server-layer infrastructure awaiting relocation.

**Actual disposition:** `internal/adapters/` relocates with the server-side database layer when postgres/SM-Online work moves to a separate repo. The top-level `internal/database/` package as a whole — including its `postgres/` and `sqlite/` subdirectories, its crud files, its service layer, and its adapter framework dependency (`internal/adapters/`) — is a cohesive cluster that travels together. Not delete, **relocate**.

**What's still true about it:** the author's label of "abandoned as too complicated to maintain and use correctly" applies. It's the archetypal example of over-generalizing, and the per-driver pattern (`internal/database/sqlite/adapters/`) that the client side uses is the shape v2 should adopt for any backend-specific adapter work. But "abandoned for client-side use" ≠ "unused in the repo" — it's still running on the server-layer side.

**Important distinction to preserve:** do not conflate `internal/adapters/` (the generic reflection framework, server-layer) with `internal/database/sqlite/adapters/` (the simple per-driver adapter, client-layer, just simplified this session). They solve similar problems with different approaches. The client uses the simple one; the server uses the generic one. They do not share code.

**Wrinkle:** `internal/adif/slice_test.go` imports `internal/adapters` as a test dependency. When `internal/adapters` relocates out of this repo, that test file will need updating — either by providing a local test helper or by removing the specific test that uses the dependency. Minor TODO for the eventual relocation commit.

**Lesson for v2:** see `lessons-for-v2.md` → "Build specific, not generic." The lesson about the framework's design trap still stands, even though the framework itself isn't dead in the current repo.

### WSJT-X UDP listener is dead code — **LOW (but architecturally significant)**

**Location:** `internal/listeners/handlers/wsjtx/` (parser, handler, tests).

**Description:** The WSJT-X UDP listener exists in the repo but **has never run in a working configuration end-to-end**. WSJT-X and JTDX both require exclusive access to the USB serial port for CAT/PTT, which conflicts with Station Manager's own serial library. Running WSJT-X on the same machine requires giving up rig control from Station Manager, so the UDP ingest path was never actually exercised.

**Impact:** code that looks like an active ingest path but isn't. Confuses analysis (as it confused me during this session — I initially wrote the architecture map assuming this was a working path). Represents a false start on the WSJT-X integration problem.

**Root cause:** the listener was written in anticipation of an ingest path that the serial-contention problem made impossible before it could be exercised. The attempt to solve that problem led to writing the user's own FT8 library (now in a separate repo), which is why the WSJT-X listener sits unused.

**Fix (planned, not yet applied):** delete `internal/listeners/handlers/wsjtx/`. Investigate whether `internal/listeners/` itself is now also dead (if wsjtx was the only handler). V2's WSJT-X ingest plan is different: a separate `wsjtx-bridge` client process that translates UDP → the daemon's HTTP API, which has nothing to do with `internal/listeners`. This requires the serial/CAT bridge to exist first, so WSJT-X and Station Manager can coexist on one rig.

**Severity rationale:** low *per se* because it's dead code that doesn't affect anything running. But architecturally significant as the clearest single example of "v1 accumulated code in response to a problem that the architecture couldn't actually solve" — which is one of the strongest motivations for considering v2.

**Disposition:** v1 cleanup commit.

**Related:** `architecture-map.md` → observation #9; `design-decisions-log.md` → "WSJT-X listener deletion."

### ~~`internal/ft8/` and `internal/ft8x/`~~ — **FIXED (2026-04-14, commit `0e158ec`)**

**Location (now deleted):** `internal/ft8/` with subpackages `codec`, `dsp`, `message`, `service`, `synth`, `timing`; plus `internal/ft8x/`.

**Description:** The FT8 implementation work that spawned the separate `go-ft8` and `goft8x` projects in other repos. Per the author, FT8 has been extracted and these directories are no longer wanted in the client repo.

**Fix applied:** deleted `internal/ft8/`, `internal/ft8x/`, `cmd/ft8/`, `cmd/ft8test/`. Updated `go.work` to drop `./cmd/ft8` and `./cmd/ft8test` (7 modules → 5). Removed the FT8/FT4 section from `README.md`, replaced the `internal/ft8/synth` example in `internal/audio/README.md` with a generic caller-supplied samples example, and dropped FT8 patterns from `.gitignore`. The experiment tree prior to the cleanup is preserved under the `pre-ft8-removal` tag at commit `1ae516d` so anything useful can be recovered from history. Build + vet pass clean across all five remaining workspace modules.

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

### Architecture-map gap: top-level `internal/database/` package missing — **LOW (doc bug)**

**Location:** `docs/v1-analysis/architecture-map.md`.

**Description:** The architecture map I drafted earlier in this session describes `internal/database/sqlite/` and `internal/database/postgres/` as the database layer, but it completely omits the top-level `internal/database/` package itself. That top-level package is a separate, distinct layer with its own `service.go`, `interface.go`, `migrations.go`, `validation.go`, `helpers.go`, `adapters_cache.go`, `README.md`, and a set of `crud_*.go` files (`crud_user.go`, `crud_apikey.go`, `crud_qso.go`, `crud_logbook.go`, `crud_contacted_station.go`, `crud_country.go`, `crud_sessions.go`). It imports `internal/adapters` (the generic framework) and contains its own `postgres/` and `sqlite/` subdirectories.

The presence of `crud_user.go` and `crud_apikey.go` strongly suggests this is the **server-side database layer** for the planned SM-Online server — user management and API key authentication are server concerns, not client concerns. The client-side database layer is `internal/database/sqlite/` (one level deeper into the sqlite subdir from the top-level), which is a completely separate package.

**Impact:** an incomplete architecture map that misrepresents the relationships between database-related packages. Specifically, it means:
- The "internal/adapters is dead code" classification in this inventory (now corrected above) was a consequence of not seeing the top-level `internal/database/` package.
- Any future analysis of the client-vs-server split will need to account for this top-level package.
- The map's "Internal library packages → Database layer" section needs an entry for the top-level `internal/database/` package, clearly distinguished from the client-side `internal/database/sqlite/`.

**Disposition:** update `architecture-map.md` to add a section on the top-level `internal/database/` package. Describe it as "server-side database layer, scheduled for relocation to the SM-Online server repo along with `internal/adapters/`." Preserve the existing entries for `internal/database/sqlite/` (client-side) and `internal/database/postgres/` (currently part of the top-level cluster, same relocation fate).

**Severity rationale:** low because it's a documentation gap, not a code issue. But it caused a real analysis error (the "delete internal/adapters" classification) that had to be corrected, so it's worth fixing to prevent similar errors in future sessions.

### ~~Dead doc files in `docs/`~~ — **FIXED (2026-04-14, commit `0e158ec`)**

**Description:** Several FT8-related docs and legacy handoff/setup docs had no ongoing relevance.

**Fix applied:** deleted all top-level `.md` files from `docs/` — the FT8 set (`whats-next.md`, `ft8-library-assessment.md`, `ft8-ft4-implementation-research.md`, `ft8-callsign-constants-verification.md`, `ft8-decoder-testing-handoff.md`) plus legacy `context-handoff.md` and `usb-serial-setup.md`. Only `docs/v1-analysis/` and the new `docs/session-handoff.md` remain in `docs/`.
