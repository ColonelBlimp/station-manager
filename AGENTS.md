# AGENTS.md - Station Manager

## Snapshot (current repo state)
- Product direction from `README.md`: Linux desktop apps for Amateur Radio, built with Go + SvelteKit + Wails.
- Design intent from `README.md`: offline-first operation; online forwarding (QRZ.com, ClubLog, Station Manager — all configurable) is optional.
- Trunk-based workflow: all commits go directly to `main`, releases triggered by `v*` tags.

## Workspace and module boundaries
- This is a Go workspace (`go.work`, Go `1.25.0`) with no root `go.mod`.
- **One consolidated `internal/` module** (`internal/go.mod`) contains all shared domain packages (adapters, adif, cat, config, database, etc.) as plain Go packages — not individual modules.
- App modules live under `apps/*` (e.g., `apps/logging`, `apps/logbook`, `apps/config`); each has its own `go.mod` with a single `replace` directive pointing to `../../internal`.
- CLI tools live under `cmd/*` (e.g., `cmd/importer` — a Cobra-based ADIF import tool); same `go.mod` + `replace` pattern as apps.
- `web/shared-utils/` is a TypeScript/Svelte library consumed by Wails app frontends.
- Do not add a root-level `go.mod`; do not split `internal/` back into per-package modules.

## Build/test commands (use Task, not make)
- `task` → runs `go work sync`
- `task build` → `go build ./...` for all Go modules
- `task test` → `go test -race ./... -short` for all Go modules
- `task tidy` → `go mod tidy` in each module
- `task update` → `go get -u ./...` + `go mod tidy` in each module
- `task ft8` → build the FT8 RX/TX CLI tool → `build/bin/ft8`
- `task wails:logging APP_VERSION=<ver>` → build the logging Wails app
- `task wails:logbook APP_VERSION=<ver>` → build the logbook Wails app
- `task wails:config APP_VERSION=<ver>` → build the config Wails app
- `task wails APP_VERSION=<ver>` → build all Wails apps
- `task wails:logging:dev` / `task wails:logbook:dev` / `task wails:config:dev` → Wails dev mode with hot-reload
- `task release:local` → full local release pipeline (validate → build → wails → package)
- `task release:validate` → run CI-equivalent checks locally (vet, fmt, test, lint)
- `task wails:frontend-update` → update npm dependencies for all frontends
- `task setup-hooks` → install Git pre-commit hook

## CI and release process
- **Every push/PR to `main`**: `.github/workflows/validate.yml` runs vet, fmt, test, lint.
- **On `v*` tag push**: `.github/workflows/release.yml` runs validate → build Wails apps → GitHub Release.
- Wails modules are skipped during validation (their `go:embed` requires a frontend build).
- Full release process documented in `RELEASING.md`.

## Runtime data
- All apps use `utils.WorkingDir()` (`internal/utils/working_dir.go`) to resolve data paths.
- Resolution order: explicit argument → `SM_WORKING_DIR` env var → executable's directory.

## Conventions for agents
- Add new reusable domain code as packages under `internal/` (e.g., `internal/newpkg/`). No new `go.mod` needed — they are packages within the single `internal` module.
- If you create a new app under `apps/`, add a local `go.mod` with `require` + `replace` for the internal module, then add it to `go.work`.
- Keep offline-first behavior in mind when adding network features (per `README.md` requirement).
- CAT support note from `README.md`: tested rigs are Yaesu `FTdx10` and `FT-710` only.
- **`internal/types` is dependency-free**: it must not import any other Station Manager package — only Go stdlib. This prevents cyclic dependencies (see `internal/types/README.md`).
- **DI wiring via `iocdi`**: services use `di.inject:"<beanId>"` struct tags for dependency injection. Register types by `reflect.TypeOf`, call `container.Build()`, then `ResolveSafe(id)`. See `cmd/importer/cmd/root.go` for a working example.
- **ServiceName constants**: each service exposes a `ServiceName` constant used as its DI bean ID. Shared service names are centralized in `internal/types/services.go` (e.g., `types.SqliteServiceName`). App facades define their own `ServiceName` locally (e.g., `facade.ServiceName = "logging-app-facade"`).
- **Service lifecycle**: services follow `Initialize()` → `Open()`/`Start(ctx)` → `Close()`/`Stop()`. All lifecycle methods are idempotent. `Initialize()` validates config/deps; `Start`/`Open` begins operation; `Stop`/`Close` does graceful shutdown. See `internal/database/service.go` for the pattern.
- **Wails app facade pattern**: each Wails app has a `backend/facade/` package containing a `Service` struct that bridges the Wails frontend and internal services. The facade receives all dependencies via DI (`di.inject` tags) and exposes methods callable from the frontend. See `apps/logging/backend/facade/service.go`.
- **Listener handler plugins**: network packet handlers self-register at `init()` time via `handlers.Register("name", constructor)` and are activated by blank imports — e.g., `_ ".../listeners/handlers/wsjtx"`. To add a new handler, create a package under `internal/listeners/handlers/`, implement the `PacketHandler` interface, and register in `init()`. See `internal/listeners/handlers/README.md`.
- **Structured errors**: use `errors.Op` for operation-scoped error wrapping — e.g., `const op errors.Op = "pkg.FuncName"` then `errors.New(op).Err(err).Msg("context")`. See `internal/errors/`.
- **Database models are generated by `sqlboiler`**: do not hand-edit files under `internal/database/sqlite/models/` or `internal/database/postgres/models/`. Regeneration steps are in `internal/database/README.md`.

## Where to look first
- Project intent: `README.md`
- Developer setup & tasks: `DEVELOPING.md`
- Release process: `RELEASING.md`
- Build/test workflow: `Taskfile.yml`, `Taskfile.wails.yml`
- Active Go modules: `go.work`
- CI workflows: `.github/workflows/`
- DI bean IDs: `internal/types/services.go`
- Runtime artifact directories: `build/bin`, `build/db`, `build/logs`
