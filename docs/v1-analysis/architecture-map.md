# Station Manager v1 — Architecture Map

**Status:** First draft, 2026-04-14. Part of the v1 analysis effort preceding the v2 rewrite / refactor decision (see `project_sm_v2_analysis` memory note). This document is meant to be reviewed and corrected by the author — places where I'm inferring or guessing are marked clearly with `?` or "unverified" so they can be fixed up.

**Purpose:** Ground truth for "what v1 actually is." Every other analysis document (`design-decisions-log.md`, `bug-inventory.md`, `external-surfaces.md`, `lessons-for-v2.md`) builds on this one. If something isn't mapped here accurately, the downstream documents will be wrong.

---

## Top-level shape

Station Manager is a **multi-module Go monorepo** using `go.work` to compose seven related modules into one development workspace. The monorepo also contains:

- Three **Wails desktop applications** (Go backend + TypeScript/Svelte frontend each, each its own Go module).
- A handful of **CLI binaries** in `cmd/` (each its own Go module except for reserved-but-empty slots).
- A shared **`internal/` module** containing all the library packages that the apps and CLIs consume.
- Shared **web utilities** in `web/shared-utils/` for the TypeScript side.
- Build and runtime-state directories (`build/`, `assets/`, `scripts/`, `docs/`).

The monorepo layout is **already partially decomposed** — the three Wails apps are separate modules today, not one blob. The restructure we've been discussing (daemon + clients) is a further step on a decomposition path that's already been started.

## Go modules (from `go.work`)

Seven modules are active in the workspace:

| Module path | Purpose (inferred) | Maturity (inferred) |
|---|---|---|
| `./apps/config` | Wails app — configuration editor / settings UI | Thin (facade has 3 files) |
| `./apps/logbook` | Wails app — logbook management (CRUD over logbooks?) | Medium (facade has 7 files) |
| `./apps/logging` | Wails app — **the main logging application** (QSO entry, contesting, forwarding, CAT status display) | Mature (facade has 20 files, substantial test coverage) |
| `./cmd/ft8` | FT8 CLI tool (?) | Unverified |
| `./cmd/ft8test` | FT8 test/diagnostics CLI | Unverified |
| `./cmd/importer` | ADIF bulk importer CLI (?) | Unverified |
| `./internal` | Shared library packages, consumed by all apps and CLIs | Large, ~30 packages |

**Not in `go.work`:**
- `cmd/server/` — empty directory, only a `.gitkeep`. Reserved for future work, probably the planned SM-Online server module that isn't written yet.
- `cmd/tools/` — empty directory, only a `.gitkeep`. Reserved for future dev/ops tools.

These two empty slots tell a story: there is already a *planned* server binary (`cmd/server`) that the `internal/database/postgres` and `internal/apikey` packages are presumably headed toward, and there is already a planned generic tools area (`cmd/tools`). Neither is populated yet.

> **For the author to confirm:** Are `cmd/server` and `cmd/tools` reserved for the public SM-Online server and dev tooling respectively, or are they leftovers from an abandoned direction that should be deleted?

## Wails applications (`apps/`)

Three separate desktop apps, each following the same structural pattern:

```
apps/<app>/
├── assets/         # app-specific static assets
├── backend/        # Go side (Wails-bound services)
│   └── facade/     # the Wails-facing service layer (Facade pattern)
├── frontend/       # TypeScript/Svelte(?)/Vite frontend
└── go.mod
```

Each `backend/facade` package implements the **Facade design pattern** (per `apps/logging/backend/facade/doc.go`): it's the single entry point registered with Wails for frontend binding, coordinating the various internal services while hiding their complexity from the UI.

### `apps/logging` — the primary application

**Largest and most developed app.** Its facade package (`apps/logging/backend/facade/`) is where most of the session work has happened — it contains:

- `facade.go` — the public Wails-bound methods (`NewQso`, `LogQso`, `UpdateQso`, `CurrentSessionQsoSlice`, `FetchUiConfig`, `FetchCatStateValues`, `OpenInBrowser`, `ForwardSessionQsosByEmail`, etc.)
- `qso_init.go` — the composed `initializeQso` flow (fetch contacted station → country → bearing/distance → contact history → assembled QSO draft)
- `crud_database.go` — DB open/migrate, startup logbook/session loading, contacted-station and country cache writes
- `contests.go` — `IsContestDuplicate`, `TotalQsosByLogbookId`
- `forwarding.go` — the forwarding worker goroutines (poller, workerLoop, dbWriteWorkerLoop)
- `listener.go` — **CAT status listener** (Wails event emitter — NOT the WSJT-X UDP listener; see `internal/listeners` for that)
- `internal.go` — helper functions including `lookupCallsignOnline`, `calculateBearingAndDistance`, forwarding primitives
- `interfaces.go` — `DatabaseServiceInterface`, `ConfigServiceInterface`, `CatServiceInterface`, `LookupServiceInterface`, etc. (see `bug-inventory.md` — these don't all match their concrete types)
- `service.go` — Service struct, lifecycle (Initialize, Start, Stop), DI field tags
- `validation.go`, `helpers.go`, `error_msgs.go`, `binding_only.go`, `doc.go`
- `mocks_test.go`, `facade_integration_test.go`, `qso_init_test.go`, `forwarding_test.go`, `validation_test.go`, `helpers_test.go`

**What it does:** manual QSO entry (SSB, CW, phone), contest logging, displays rig CAT state, runs the forwarding worker pool that pushes stored QSOs to online services (currently hardcoded to QRZ — see `bug-inventory.md`), handles session lifecycle.

### `apps/logbook` — logbook management and QSO editing

**Standalone Wails app** for managing the operator's collection of logbooks: creating new logbooks, deleting old ones, exporting them (presumably to ADIF), and editing historical QSOs. Medium-sized facade (7 files). Shares the same SQLite database as `apps/logging` — changes in one are visible in the other.

**Why it's a separate app from `apps/logging`:** mixing these management features into the logging app would bloat its UI and cross concerns. Logging is a "I'm operating right now" app (real-time entry, CAT display, forwarding); logbook is a "I'm managing my accumulated record" app (occasional-use, bulk operations, editing). Different workflows, different latency expectations, different UI focus — deliberate separation.

### `apps/config` — configuration editor

**Standalone Wails app** for editing Station Manager's configuration, which is stored in `config.json` and surfaced to the rest of the codebase via `internal/config`. Smallest facade (3 files).

**Why it's a separate app:** configuration is a distinct concern from logging and logbook management, and it changes infrequently. A dedicated app gives config editing a focused UI without cluttering the everyday-use apps.

**Relationship to `internal/config`:** the config app is the *editor*; `internal/config` is the *reader* used by all three apps at runtime. `config.json` is the shared source of truth.

## CLI binaries (`cmd/`)

### Active (in `go.work`)

- **`cmd/ft8`** — FT8 test package / CLI. Part of the in-tree FT8 work that has since been extracted to a separate repo. **Slated for removal** along with the rest of `internal/ft8*` (see Cleanup targets).
- **`cmd/ft8test`** — Separate from `cmd/ft8`. Originally a test harness but retained as a standalone binary because its CLI has **diagnostic value** for future FT8 work. Goes away with the rest of the FT8 code in this repo.
- **`cmd/importer`** — **ADIF-only** bulk importer for populating the database from historical logs exported from other software. No other formats currently supported.

### Reserved but empty

- **`cmd/server/`** — reserved slot for a future SM-Online public server binary. Origin: author was "thinking about SM-online" when the slot was created. Not dead code, not yet written. Keep.
- **`cmd/tools/`** — reserved slot for future dev/ops/admin CLI tools. Some tools are planned for later (beyond the existing `cmd/importer`). Keep.

## Internal library packages (`internal/`)

Grouped by concern. Some groupings are guesses — the user should correct misplacements.

### Database layer

- **`internal/database/sqlite`** — the active local DB package. sqlboiler-based. Contains:
  - `service.go` — the `Service` struct, DB handle management, `BeginTxContext`, etc.
  - `api.go` — high-level non-context methods (background-context wrappers)
  - `api_context.go` — the real implementations, all with `ctx` parameters. Contains the `*WithContext` methods and (newly added 2026-04-14) the `*Tx` transactional variants (`InsertQsoTx`, `UpdateQsoTx`, `InsertQsoUploadTx`) used by the atomic `LogQso`/`UpdateQso` fix.
  - `models/` — sqlboiler-generated row models
  - `adapters/` — **the per-driver type↔model translation layer that is being kept** (see `design-decisions-log.md`). Four files: `type_to_model.go`, `model_to_type.go`, `error_msgs.go`, `adapters_test.go`.

    **Pattern:** real database columns exist only for queryable/indexed/filterable fields (`call`, `band`, `mode`, `freq`, `qso_date`, `time_on`, etc.); everything else is serialized into an `additional_data` JSON blob column. This is a load-bearing design decision that absorbs ADIF spec evolution without schema migrations — adding a new ADIF field should be a one-line change to `types.Qso`, with the storage carrying it through automatically via JSON marshaling. Schema only changes when a field is *opted in* to being a real column, which is a feature decision (for indexing or querying), not a spec-tracking obligation. See `invariants.md` → "additional_data absorbs ADIF evolution."

    **Current implementation has a bug that violates the pattern's goal:** the forward direction (`QsoTypeToModel`) builds a separate `types.QsoAdditionalData` struct manually and marshals that instead of marshaling `types.Qso` directly, which means every new ADIF field is two edits instead of one. Slated for simplification — see `bug-inventory.md` → "QsoAdditionalData" and `lessons-for-v2.md` → "Mostly-blob + promoted fields pattern."
  - `migrations/` — migration files
  - `meta/` — unverified
  - `example/` — unverified
  - `validation.go`, `consts.go`, `enums.go`, `error_msgs.go`, `internal.go`, `helpers.go`, `migrations.go`, `sqlboiler.toml`
- **`internal/database/postgres`** — **diverged from sqlite, under reconsideration.** Originally intended for the future public SM-Online server (`cmd/server`). Should probably move to that server's repo when it exists (see `design-decisions-log.md`).

### Generic adapter framework (ABANDONED — for deletion)

- **`internal/adapters`** — a sophisticated **reflection-based struct-to-struct adapter framework** with field converters, tag-based ignores, and `AdditionalData` JSON handling. Has 30+ test files, a builder API, generics, a converters/ subtree with common, sqlite, and postgres subpackages. Per the package doc comment, this was built to be a generic solution to the same problem `database/sqlite/adapters` solves manually.
  - **Status: user has confirmed this is "abandoned as too complicated" and is to be dumped.** Kept in the tree but should be deleted as part of the cleanup.
  - **Important distinction:** this is NOT the same package as `internal/database/sqlite/adapters`. The user wants to keep the latter (simple per-driver) and delete this one (generic reflection framework). Do not conflate them.
  - The `converters/common/`, `converters/sqlite/`, `converters/postgres/` subpackages all go with it.

### Dependency injection

- **`internal/iocdi`** — home-grown IoC/DI container. The `di.inject:"..."` struct tags on Service fields (seen in `apps/logging/backend/facade/service.go`) are consumed by this package. Contains `container.go`, `initializer.go`, `hooks.go`, plus tests. **Keep.** Author considers it a good fit: lightweight, flexible enough for service lifecycle management, preferred over alternatives like Uber fx or Google wire. Revisit only if maintenance burden grows or a better lightweight alternative surfaces.

### Radio / hardware I/O

- **`internal/serial`** + **`internal/serial/cmd`** — the user's own serial port library, kept in-repo for now. Reported as more robust than hamlib's serial handling.
- **`internal/cat`** — the user's own CAT (Computer Aided Transceiver) control library.
- **`internal/ptt`** — PTT handling, separate from CAT.
- **`internal/audio`** — audio I/O (for the FT8 pipeline and presumably voice-keyer support). Per `docs/whats-next.md`, item 1 "Audio I/O" is marked complete.

### Callsign / country lookup services

- **`internal/lookup/hamnut`** — hamnut.com lookup client (source of truth for Country per the user).
- **`internal/lookup/qrz`** — QRZ.com lookup client (secondary callsign data; QRZ also appears as a *forwarder* destination — see `internal/forwarding/qrz`).

### Forwarding (outbound to online services)

- **`internal/forwarding/qrz`** — QRZ.com logbook forwarder. The only forwarder currently implemented.
- Note: `internal/forwarding/` itself exists as a package root; the structure suggests additional forwarders (ClubLog, future SM-Online) would live as sibling subpackages.

### Listeners (inbound from external software)

- **`internal/listeners`** — listener infrastructure (service.go, listener.go, internal.go, helpers.go, error_msgs.go, listeners_test.go). Generic framework for registering UDP/TCP listeners.
- **`internal/listeners/handlers/wsjtx`** — WSJT-X UDP listener: `parser.go`, `handler.go`, plus tests. Parses WSJT-X's native UDP protocol and was intended to translate incoming logged QSOs into database writes.

  **⚠️ Status: NON-FUNCTIONAL in v1.** This code exists but does not run in any working configuration. The reason: WSJT-X and JTDX both want exclusive access to the USB serial port for CAT/PTT, and so does the author's own serial library. They cannot coexist on one physical port. Running WSJT-X *at all* on the same machine means giving up control of the rig from Station Manager, so the UDP ingest path from WSJT-X was never actually exercised end-to-end.

  This is the same serial-contention problem that motivates the serial/CAT bridge design (see `project_sm_serial_bridge` memory note). The bridge is a **prerequisite** for this listener ever being functional — without it, WSJT-X can't coexist with Station Manager on a single rig at all. Writing the user's own FT8 library (now in its own repo) was the alternative path the author took when the UDP ingest approach hit this wall.

  **Implication for v1 cleanup:** this code is dead-on-arrival in the current repo state and should be treated as a **false start**, not a foundation. Not something to port forward into v2. V2's plan is "WSJT-X (or whatever FT8 code is used) logs QSOs via the daemon's API, through a `wsjtx-bridge` client process that translates UDP → daemon HTTP" — which is a different shape and lives outside the daemon, not inside `internal/listeners`.

- **`internal/listeners/handlers`** — the interface / shared code for handler implementations. If WSJT-X is removed (as the only current handler), the listener framework itself may also be unused. Verify during cleanup.

### Domain types and formats

- **`internal/types`** — **the central application types package.** `types.Qso`, `types.ContactedStation`, `types.Country`, `types.Logbook`, `types.LoggingStation`, `types.QsoDetails`, `types.QsoUpload`, `types.RigConfig`, `types.UiConfig`, etc. Used by every app and nearly every internal package.

  **Load-bearing design intent: `types.Qso` is shaped to mirror the ADIF specification.** Every ADIF tag the software supports has (or will have) a corresponding field in `types.Qso` or one of its nested sub-structs. The Go-level nesting (`QsoDetails`, `ContactedStation`, `LoggingStation`, `CountryDetails`) is an organizational convenience for grouping related fields — ADIF itself is flat, so the nesting has no semantic load. In the long run, `types.Qso` will follow ADIF faithfully. This is not incidental; it's a deliberate design decision that shapes the whole data model and must be preserved in v2. See `invariants.md` → "types.Qso follows ADIF."

  **Note:** `types.QsoAdditionalData` exists in this package as an intermediate struct used by the sqlite adapter's forward direction. It's a design mistake slated for deletion; see `bug-inventory.md` → "QsoAdditionalData intermediate struct."
- **`internal/adif`** — ADIF format parser/serializer. Used by the importer, the WSJT-X listener (probably), the forwarders, and any ingest path.
- **`internal/maidenhead`** — grid square / bearing / distance calculations.

### Enums

- **`internal/enums`** and its subpackages:
  - `bands` — amateur radio band definitions
  - `cmds` — (probably CAT or internal command enumeration?)
  - `events` — event type constants (used by the Wails event stream — saw `events.Status.String()` in listener code)
  - `modes` — operating mode enumeration (CW, SSB, FT8, etc.)
  - `tags` — (ADIF tag constants?)
  - `upload` — forwarder-related enums: the `upload` root contains `OnlineService` (QRZ, ClubLog, SM-Online, etc.), plus subpackages `upload/action` (Insert, Update, Delete) and `upload/status` (Pending, Uploaded, Failed).

**Structural note:** The split into per-concept subpackages is unusual for Go but is **intentional** — it grew organically from a single `internal/enums` package and the author found it cleaner to group related enums together as they multiplied. Not a common pattern, but it works for this project and keeps enums easy to locate. Keep.

### Infrastructure

- **`internal/errors`** — custom error package with `errors.Op` operation-tagging pattern (`errors.New(op).Err(err).Msg(...)`). Used pervasively throughout the codebase. **Design review target for v2** — the HTTP-serialization question is open.
- **`internal/logging`** — logging abstraction (probably zerolog-based given the `LoggerService.InfoWith().Str(...).Msg(...)` call shape).
- **`internal/utils`** — general utilities (saw `utils.DateNowAsYYYYMMDD()` in `crud_database.go`).
- **`internal/config`** — application configuration reading/writing. Separate from `apps/config` (which is the Wails UI *for* editing the config).
- **`internal/email`** — email sending (used by `ForwardSessionQsosByEmail` — operator can email their session log as ADIF).
- **`internal/apikey`** — API key handling. Per the `database/sqlite/README.md`, this is for authenticating against the future SM-Online server: the client stores the full key (on the logbook row), the server would store only a hash/HMAC. **Status: undecided — blocked on v2 client/server split design.** The package currently lives in the shared internal library but may need to split or move. The client needs key storage/retrieval/sending; the server needs hashing/validation/revocation. Minimal shared Go code in practice — probably two independent `apikey` packages eventually, one per side, sharing only a key-format constants file. Revisit during v2 design of the SM-Online server.

### FT8 (being removed)

- **`internal/ft8`** with subpackages `codec`, `dsp`, `message`, `service`, `synth`, `timing` — the FT8 implementation. This work spawned the `go-ft8` / `goft8x` projects that now live in separate repos.
- **`internal/ft8x`** — parallel FT8 experiment, also slated for removal.
- **Status: confirmed, remove.** FT8 is no longer a concern for this repo. Both directories (plus `cmd/ft8`, `cmd/ft8test`, and the FT8-related docs in `docs/`) are to be deleted as part of v1 cleanup or dropped entirely from the v2 starting point. See **Cleanup targets** below.

## Web side

- **`web/shared-utils/`** — TypeScript/JavaScript shared utilities package (`src/`, `dist/`, `docs/` subdirectories). Contains common helper functions for formatting and other shared frontend logic. **Consumed by all three Wails frontends** (`apps/logging/frontend`, `apps/logbook/frontend`, `apps/config/frontend`) to avoid duplication.

## Build / runtime state / dev

- **`build/`** — contains `bin/` (compiled binaries), `bin/db/` and `bin/logs/` (runtime state from dev builds?), `db/` and `logs/` (development DB files and logs), and `release/` (release artifacts). Mostly gitignored output directories.
- **`assets/`** with `assets/xdg/` — static assets, probably desktop entry files for Linux XDG integration.
- **`scripts/`** — dev/build scripts.
- **`docs/`** — existing documentation. Currently contains FT8-related docs (`ft8-library-assessment.md`, `ft8-ft4-implementation-research.md`, `ft8-callsign-constants-verification.md`, `ft8-decoder-testing-handoff.md`), `usb-serial-setup.md`, `whats-next.md`, and `context-handoff.md`. **This file (`architecture-map.md`) is the first entry in the new `docs/v1-analysis/` subdirectory.**

## Dependency sketch (inferred, unverified)

A rough picture of what depends on what. **This section is the one I'm least confident about** — I'm inferring from package names and partial reads, not tracing imports. The user should verify or correct.

```
apps/logging/backend/facade
├── internal/types                    (heavy — types everywhere)
├── internal/database/sqlite          (DB layer)
├── internal/database/sqlite/adapters (via sqlite package)
├── internal/errors                   (pervasive)
├── internal/logging                  (pervasive)
├── internal/config                   (ConfigService)
├── internal/cat                      (CatService)
├── internal/lookup/hamnut            (HamnutLookupService)
├── internal/lookup/qrz               (QrzLookupService)
├── internal/forwarding/qrz           (QRZ forwarder)
├── internal/enums/upload/*           (upload state vocab)
├── internal/enums/events             (Wails event names)
├── internal/maidenhead               (bearing/distance)
├── internal/email                    (session-by-email forwarding)
├── internal/adif                     (ADIF format handling?)
├── internal/utils                    (date helpers)
├── internal/iocdi                    (DI container — via di.inject tags)
└── github.com/wailsapp/wails/v2      (Wails framework)

internal/listeners/handlers/wsjtx   ⚠️ NON-FUNCTIONAL — never runs in any working configuration
├── internal/listeners                (generic listener infrastructure)
├── internal/adif                     (parsing ADIF from WSJT-X UDP — aspirational)
├── internal/types                    (types.Qso — aspirational)
└── (no working path to storage — see Listeners section for why)

apps/logbook  — shares SQLite DB with apps/logging
├── internal/database/sqlite          (same DB as logging — changes visible in both apps)
├── internal/types                    (types.Qso, types.Logbook)
├── internal/database/sqlite/adapters (via sqlite)
├── internal/errors
├── internal/logging
├── internal/iocdi
└── github.com/wailsapp/wails/v2

apps/config — reads/writes config.json via internal/config
├── internal/config                   (config reader used by all apps)
├── internal/errors
├── internal/logging
├── internal/iocdi
└── github.com/wailsapp/wails/v2

cmd/importer
├── internal/adif                     (ADIF parsing)
├── internal/database/sqlite          (bulk insert)
└── internal/types                    (types.Qso)

cmd/ft8, cmd/ft8test
├── internal/ft8/*                    (the in-tree FT8 code)
└── internal/audio                    (WAV and DSP input)
```

**Resolved:** the WSJT-X listener doesn't actually have a data path to storage because the listener never runs in a working configuration (see the Listeners section). This isn't a refactoring question — it's a dead-code removal. V2's ingest path from WSJT-X will be a separate `wsjtx-bridge` client process talking to the daemon's HTTP API, not anything built on the current `internal/listeners` framework.

## Observations and things worth flagging

1. **The monorepo is already partially decomposed.** Three separate Wails modules, a shared internal module, separate CLI modules. The v2 target (daemon + clients + bridge) is on a trajectory the repo is already moving along, not a from-scratch restructure. This is good news for the refactor path if that's chosen, and it's good news for v2 because the module boundaries that already exist can be preserved.

2. **`internal/adapters` and `internal/database/sqlite/adapters` are two separate packages**, and the user wants one and not the other. This distinction is critical for the design review and was the source of earlier confusion in this session. `internal/adapters` is a reflection-based generic framework that's "too complicated and abandoned"; `database/sqlite/adapters` is simple per-driver manual mapping that's the approach going forward. **Action item: plan to delete `internal/adapters` and its sub-tree (`converters/`) during v1 cleanup or v2 design.**

3. **Two "listener" concepts are easy to confuse:**
   - `apps/logging/backend/facade/listener.go` — **CAT status listener** (emits radio status updates to the Wails frontend as events).
   - `internal/listeners/handlers/wsjtx/` — **WSJT-X UDP listener** (ingests QSOs from WSJT-X over UDP).
   These are unrelated and should not be mentally merged. Earlier in the session I initially confused them.

4. **The three Wails apps follow the same structural pattern** (facade + assets + frontend + go.mod), but their maturity varies dramatically: logging has 20 backend files, logbook has 7, config has 3. The decomposition of responsibilities across them is not yet clear to me and needs the author to explain.

5. **`cmd/server` and `cmd/tools` are empty reservations**, not dead code. Tells us the public SM-Online server and dev tooling areas are planned but not yet started.

6. **The git status at session start** showed three FT8-related files as added-but-deleted: `internal/ft8/dsp/_write_test.py`, `internal/ft8/service/config.go`, `internal/ft8x/ft8x_diag_test.go`. FT8 extraction is in progress but not complete. The state of `internal/ft8*` in this repo is in flux.

7. **The facade pattern is consistent across all three apps and is the Wails binding layer.** Any v2 restructure has to decide what replaces it. If v2 is a daemon with a thin client, the current facade's responsibilities split: the Wails binding stays in the client (calls the daemon API), and the domain logic (QSO init, log/forward orchestration) moves into the daemon.

8. **The `DatabaseServiceInterface` in `apps/logging/backend/facade/interfaces.go` and the concrete `*sqlite.Service` have divergent signatures** (noted in `bug-inventory.md` when it's written). The interface is unused scaffolding and the mocks built on it are aspirational. The field is declared as `*sqlite.Service` directly, not via the interface. Worth cleaning up regardless of path.

9. **The WSJT-X UDP listener (`internal/listeners/handlers/wsjtx`) is non-functional in v1.** It was written in anticipation of an ingest path that the USB/serial contention problem made impossible — WSJT-X/JTDX and the author's own serial library cannot coexist on one physical port. The listener code has never run in a working configuration end-to-end. It represents a false start, not a foundation, and should not be carried forward into v2. V2's WSJT-X ingest will live in a separate `wsjtx-bridge` client that talks to the daemon's HTTP API. This is the biggest single thing I got wrong in my initial read of the repo.

10. **The three-concerns split across `apps/logging` / `apps/logbook` / `apps/config` is deliberate and clean, and must carry forward into v2.** Logging is real-time QSO entry; logbook is management and historical editing; config is settings. Different workflows, different latency profiles, different UI focus. The v2 daemon needs to serve *all three* concerns over its API, which means the daemon's API surface is wider than my earlier sketches suggested — the logging-centric sketch I produced earlier was missing the entire logbook-management surface (create/delete/rename/export logbooks, batch QSO edits, etc.). Worth a dedicated API-shape exercise before v2 design starts.

## Cleanup targets (from author answers, 2026-04-14)

Consolidated list of code and documentation slated for removal based on the answers in the Q&A section below. This belongs in `bug-inventory.md` as the "what v1 carries that v2 doesn't need" section. **This list describes *intent*, not completed deletions** — the actual removals are a separate cleanup task.

### Definitely delete

- **`internal/adapters/`** including the whole `converters/{common,sqlite,postgres}/` subtree. The generic reflection-based adapter framework that was "abandoned as too complicated." 30+ test files, builder API, generics. Do not conflate with `internal/database/sqlite/adapters/` which is a different package and is being kept.
- **`internal/ft8/`** (all subpackages: `codec`, `dsp`, `message`, `service`, `synth`, `timing`). FT8 is extracted to a separate repo.
- **`internal/ft8x/`** — parallel FT8 experiment.
- **`cmd/ft8/`** — FT8 test binary. Remove from `go.work`.
- **`cmd/ft8test/`** — FT8 diagnostic CLI. Remove from `go.work`.
- **`internal/listeners/handlers/wsjtx/`** — non-functional UDP listener that never ran in a working configuration. V2's ingest approach is different (a separate bridge client talking to the daemon API).
- **FT8-related documentation** in `docs/`:
  - `docs/whats-next.md` (FT8 DSP pipeline roadmap)
  - `docs/ft8-library-assessment.md`
  - `docs/ft8-ft4-implementation-research.md`
  - `docs/ft8-callsign-constants-verification.md`
  - `docs/ft8-decoder-testing-handoff.md`
  - Consider whether any of these should instead move to the extracted FT8 repo rather than being deleted outright.
- **`apps/logging/backend/facade/mocks_test.go`** (or the interfaces it mocks) — the `DatabaseServiceInterface` and its mocks are unused scaffolding that doesn't match the concrete `*sqlite.Service`. Clean up the interface-vs-concrete mismatch.

### Investigate before deleting

- **`internal/audio/`** — was part of the FT8 pipeline per `docs/whats-next.md` ("item 1: Audio I/O ✅ complete"). If its *only* consumer was FT8, it goes. If it has other users (voice keyer? SSB monitoring? general WAV handling?), it stays. Needs a reverse-dependency check before the cleanup commit.
- **`internal/listeners/`** (the framework itself, not just the wsjtx handler) — if wsjtx was the only handler using this infrastructure, the framework may also be dead. Verify no other consumers before removing. If there are other handlers planned or present that I missed, the framework stays.
- **`internal/database/postgres/`** — the diverged postgres package. User has confirmed generic-across-backends adapters is dead; postgres was intended for the future SM-Online server. Probably moves to *that* repo (whenever `cmd/server` becomes real), rather than being deleted entirely. For now, mark as "stage for relocation" rather than "delete."

### Keep (explicitly confirmed)

Listed here for completeness so they don't get caught in a cleanup sweep by accident:

- `internal/iocdi/` — home-grown DI, keep.
- `internal/database/sqlite/adapters/` — simple per-driver mapping, keep (distinct from `internal/adapters/`).
- `internal/enums/*` — per-concept split is intentional.
- `cmd/server/` and `cmd/tools/` — empty reservations for future work, keep the slots.

## Open questions requiring the author

Consolidated list of everything I flagged above. Each should be resolved (even briefly) before this document is considered final:

1. `cmd/server` and `cmd/tools` — reserved for future work, or leftover slots to delete?

**Answer:** There are some tools planned for later, for instance an importer CLI for populating the database from ADIF files. The server was me thinking about SM-online.

2. `apps/logbook` — what is it for, how does it relate to `apps/logging`, shared DB or separate?

**Answer:** Logbook is for managing logbooks, so creating new ones, deleting old ones, exporting them, etc. and allows the editing of QSOs
It's a separate app from the logging app. These features it provides would make the logging too complicated, and it would cross concerns.
It shares the same database as the logging app, so changes in one are reflected in the other.

3. `apps/config` — what is it for, how does it relate to `internal/config`?

**Answer:** The config app is for editing the configuration, which is stored in the `config.json` and surfaced to the rest of the codebase via `internal/config`. It's a separate app
because it's a distinct concern from logging and logbook management, and it allows for a more focused UI for configuration tasks which don't change that often.

4. `cmd/ft8` vs `cmd/ft8test` — why separate?

**Answer:** `cmd/ft8` These are test packages. However, the ft8test has a useful CLI which might be of help to any future ft8 work, so I kept it separate. It's not just for testing, it's also a diagnostic tool.

5. `cmd/importer` — ADIF only, or other formats too?

**Answer:** ADIF only for now. See pint 1 above.

6. `internal/iocdi` — keep, replace, or delete?

**Answer:** I think it's worth keeping. It provides a way to manage dependencies and lifecycle of
services in a way that's more flexible than manual wiring, without being too heavy. If there is a better alternative that is still lightweight, I'm open to it,
but I haven't found one that I like better than this home-grown solution.

7. `internal/enums/*` — deliberate per-concept split, or accidental?

**Answer:** It's a bit of both. It started as a single `internal/enums` package, but as more enums were added, it felt cleaner to split them into subpackages by concept.
It's not a common pattern in Go, but it helps keep related enums together and makes it easier to find them.

8. FT8 extraction status — is `internal/ft8*` still in active use here, or mid-removal?

**Answer:** remove

9. `web/shared-utils` — contents and consumers?

**Answer:** This is a shared utilities package for the frontend code. It contains mostly common helper functions for formatting, and any other code that is shared across the three Wails frontends.
All three frontends consume it to avoid duplication of common logic.

10. WSJT-X listener data path — how does `internal/listeners/handlers/wsjtx` actually write QSOs to storage? Through the logging app's facade, directly to sqlite, or somewhere else?

**Answer:** This led to the ft8 implementation attempt (go-ft8 and goft8x) which are now in separate repos. This does not work
as WSJT-X/JTDX clash with my serial port library, so the WSJT-X listener is currently non-functional and not writing to storage at all. The plan for v2 is to have the WSJT-X (or whatever ft8 codebas is used) log QSOs via the daemon's API.

11. `internal/apikey` — planned to move to the server repo, or stays in the client?

**Answer:** This is a good question. The API key handling is currently in the shared internal library because it's was planned to used by the logging app to authenticate against the future SM-Online server, but it does feel a bit out of place there.
If the server is going to be a separate repo, it might make sense to move `internal/apikey` there as well, since it's more relevant to the server's responsibilities. On the other hand, if the client needs to generate or manage API keys as well, it might need to stay in a shared location.
This is something that would need to be decided based on how the responsibilities are split between the client and server in v2.

12. Is there anything major missing from this map? Any package I haven't listed that matters?

**Answer (assistant's own read, 2026-04-14):** After incorporating the author's answers 1–11, the biggest thing I had wrong in the first draft was **the status of the WSJT-X listener** — I described it as an active ingest path and wondered how it wrote to storage, when in fact it's non-functional code that never runs. That's now fixed in the Listeners section, the dependency sketch, and observation #9.

Smaller things the map doesn't currently cover that might belong in dedicated follow-up documents rather than this one:

- **Frontend framework specifics** — I assumed Svelte/Vite for the Wails frontends but didn't verify. A `frontend-stack.md` doc could cover the TS/Svelte/Vite/build-tool choices across all three apps, and how `web/shared-utils` is consumed. Probably lives alongside `external-surfaces.md`.
- **Test coverage and test organization patterns** — this map notes which packages have tests but doesn't analyze testing patterns (integration vs unit, mocks-vs-real-DB, fixture strategy). Belongs in a separate `test-landscape.md` or in `bug-inventory.md` as part of "regression-test gap."
- **Third-party dependencies** — `go.sum` content not analyzed. A `dependency-inventory.md` covering which external packages are load-bearing (sqlboiler, Wails, aarondl/null, goccy/go-json, zerolog presumably, etc.) would help the v2 "what do we keep using" decision.
- **Build / release / dev-mode mechanics** — the `build/` and `scripts/` directories have runtime state and automation. Worth a short `build-and-release.md` covering how the dev loop works today.
- **Database schema details beyond the package structure** — the actual column definitions, indexes, migrations, and the `additional_data` JSON-blob conventions. Belongs in `design-decisions-log.md` when the sqlite adapter design gets its own entry.

None of these are urgent for the v2-vs-refactor decision. They're follow-ups that would be useful to have before v2 design starts in earnest.

## Next steps after this document is finalized

Once the author has corrected the above, the downstream analysis documents build on it:

- **`design-decisions-log.md`** — every major shape decision with a keep/change/undecided verdict. Many of the "open questions" above will convert into entries here.
- **`bug-inventory.md`** — known issues, fixed and open. Starts with the two fixes from this session (hamnut, LogQso atomicity) and the issues we found but haven't acted on (hardcoded QRZ, interface mismatch, adapters three-shapes problem, `internal/adapters` to delete).
- **`invariants.md`** — the load-bearing rules (source material already in `project_sm_design_invariants` memory note).
- **`external-surfaces.md`** — what must not break: Wails frontend bindings, ADIF formats supported, online service APIs, existing QSO data in production DB files.
- **`lessons-for-v2.md`** — synthesis document, written last. The one that actually informs the v2-vs-refactor decision.
