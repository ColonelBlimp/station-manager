# AGENTS.md - Station Manager

## Snapshot (current repo state)
- Product direction from `README.md`: Linux desktop apps for Amateur Radio, built with Go + SvelteKit + Wails.
- Design intent from `README.md`: offline-first operation; online forwarding (QRZ.com / Station Master) is optional.
- Trunk-based workflow: all commits go directly to `main`, releases triggered by `v*` tags.

## Workspace and module boundaries
- This is a Go workspace (`go.work`, Go `1.25.0`) with no root `go.mod`.
- Active modules are listed in `go.work` under `internal/*`, `apps/*`, `cmd/*`.
- `web/shared-utils/` is a TypeScript/Svelte library consumed by Wails app frontends.
- Do not add a root-level `go.mod`; keep module boundaries explicit per directory.

## Build/test commands (use Task, not make)
- `task` → runs `go work sync`
- `task build` → `go build ./...` for all Go modules
- `task test` → `go test -race ./... -short` for all Go modules
- `task tidy` → `go mod tidy` in each module
- `task update` → `go get -u ./...` + `go mod tidy` in each module
- `task wails:logging APP_VERSION=<ver>` → build the logging Wails app
- `task wails APP_VERSION=<ver>` → build all Wails apps
- `task setup-hooks` → install Git pre-commit hook

## CI and release process
- **Every push to `main`**: `.github/workflows/validate.yml` runs vet, fmt, test, lint.
- **On `v*` tag push**: `.github/workflows/release.yml` runs validate → build → snap → GitHub Release.
- Wails modules are skipped during validation (their `go:embed` requires a frontend build).
- Full release process documented in `RELEASING.md`.

## Snap packaging
- Single snap `station-manager` bundles all apps: `snap/snapcraft.yaml`.
- `SM_WORKING_DIR` env var is set to `$SNAP_USER_COMMON` in the snap environment.
- All apps use `utils.WorkingDir()` (`internal/utils/working_dir.go`) to resolve data paths.

## Conventions for agents
- Prefer adding reusable domain code to existing `internal/*` modules already in `go.work`.
- If you create a new module under `internal/`, `apps/`, or `cmd/`, add a local `go.mod`, then run `task` to sync workspace usage.
- Keep offline-first behavior in mind when adding network features (per `README.md` requirement).
- CAT support note from `README.md`: tested rigs are Yaesu `FTdx10` and `FT-710` only.

## Where to look first
- Project intent: `README.md`
- Release process: `RELEASING.md`
- Build/test workflow: `Taskfile.yml`, `Taskfile.wails.yml`
- Active Go modules: `go.work`
- CI workflows: `.github/workflows/`
- Snap definition: `snap/snapcraft.yaml`
- Runtime artifact directories: `build/bin`, `build/db`, `build/logs`

