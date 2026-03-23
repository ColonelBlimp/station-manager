# AGENTS.md - Station Manager

## Snapshot (current repo state)
- The repo is early-stage scaffolding: most directories contain only `.gitkeep`.
- Product direction from `README.md`: Linux desktop apps for Amateur Radio, built with Go + SvelteKit + Wails.
- Design intent from `README.md`: offline-first operation; online forwarding (QRZ.com / Station Master) is optional.

## Workspace and module boundaries
- This is a Go workspace (`go.work`, Go `1.25.0`) with no root `go.mod`.
- Active modules currently listed in `go.work`:
  - `pkg/adif`
  - `pkg/enums`
  - `pkg/errors`
  - `pkg/maidenhead`
  - `pkg/types`
  - `pkg/utils`
- `apps/`, `cmd/`, `internal/`, and `web/shared-utils/` exist as structure but are mostly placeholders today.

## Build/test commands (use Task, not make)
- `task` -> runs `go work sync`
- `task build` -> runs `go build ./...`
- `task test` -> runs `go test -race ./... -short`
- `task tidy` -> loops through `pkg/* internal/* apps/* cmd/*` and runs `go mod tidy` where `go.mod` exists

## Conventions for agents
- Prefer adding reusable domain code to existing `pkg/*` modules already in `go.work`.
- If you create a new module under `pkg/`, `internal/`, `apps/`, or `cmd/`, add a local `go.mod`, then run `task` to sync workspace usage.
- Do not add a root-level `go.mod`; keep module boundaries explicit per directory.
- Keep offline-first behavior in mind when adding network features (per `README.md` requirement).
- CAT support note from `README.md`: tested rigs are Yaesu `FTdx10` and `F7-100` only.

## Where to look first
- Project intent: `README.md`
- Build/test workflow: `Taskfile.yml`
- Active Go modules: `go.work`
- Runtime artifact directories: `build/bin`, `build/db`, `build/logs`

