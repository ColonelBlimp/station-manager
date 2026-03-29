# Release Process

## Overview

Station Manager uses a **trunk-based** workflow (no branching). All development
is committed directly to `main`. Releases are triggered by pushing a **git tag**.

### Two CI workflows

| Workflow | Trigger | File |
|---|---|---|
| **Validate** | Every push to `main` | `.github/workflows/validate.yml` |
| **Release** | Push of a `v*` tag | `.github/workflows/release.yml` |

---

## Day-to-day: push to `main`

Every push to `main` runs **Validate** — four parallel jobs:

1. **Go Vet** — `go vet ./...` in each non-Wails module
2. **Go Format** — `gofmt -l` on all tracked `.go` files
3. **Unit Tests** — `go test -race ./... -short` in each non-Wails module
4. **Lint** — `golangci-lint run ./...` in each non-Wails module

Wails app modules (`apps/logging`, `apps/logbook`) are **skipped** during
validation because their `go:embed` directives require a frontend build.

### Pre-commit hook

A Git pre-commit hook auto-regenerates Wails JS/TS bindings when
`apps/logging/` files are staged:

```bash
# Install once (or after any change to scripts/pre-commit):
task setup-hooks
```

---

## Creating a release

### 1. Tag the commit

```bash
git tag v1.2.3
git push origin v1.2.3
```

The tag **must** start with `v` (e.g. `v0.1.0`, `v1.0.0-rc.1`).

### 2. What happens automatically

Pushing the tag triggers the **Release** workflow, which runs three sequential
stages:

```
validate ── build-wails ── release
```

#### Stage 1 — Validate

Same checks as the daily workflow (vet, fmt, test, lint), run as a single job
to gate the build stages.

#### Stage 2 — Build Wails Apps (`build-wails`)

Installs Node 22, GTK/WebKit system libraries, and the Wails CLI, then runs:

```bash
task wails APP_VERSION=<tag>
```

This builds **all** Wails applications via `Taskfile.wails.yml`:

| App | Source | Binary | Version injected via |
|---|---|---|---|
| sm-logger | `apps/logging` | `build/bin/smlogger` | `-ldflags "-X main.version=<tag>"` |
| sm-logbook | `apps/logbook` | `build/bin/smbook` | `-ldflags "-X main.version=<tag>"` |

The build chain for each Wails app is:

1. `shared-utils:build` — `npm install` + `tsc` in `web/shared-utils/`
2. `<app>:frontend-dep-install` — `npm install` in `apps/<app>/frontend/`
3. `<app>:frontend-build` — `npm run build` (Vite) in `apps/<app>/frontend/`
4. `<app>:wails-build` — `wails build` in `apps/<app>/`

Outputs uploaded as artifact `wails-apps-linux-amd64`.

#### Stage 3 — Create GitHub Release (`release`)

Downloads the build artifacts and creates a GitHub Release with:

- **Tag name** and **release name** set to the tag (e.g. `v1.2.3`)
- **Auto-generated release notes** from commits since the last tag
- **Attached assets**: Wails app binaries (smlogger, smbook)

---

## Local build commands

```bash
# Sync Go workspace
task

# Build all Go modules (non-Wails)
task build

# Run all tests
task test

# Build a single Wails app
task wails:logging APP_VERSION=$(git rev-parse --short HEAD)

# Build all Wails apps
task wails APP_VERSION=$(git rev-parse --short HEAD)

# Update all Go module dependencies
task update

# Tidy all Go modules
task tidy

# Install pre-commit hook
task setup-hooks
```

---

## Key files

| File | Purpose |
|---|---|
| `Taskfile.yml` | Root task runner — Go build, test, tidy, update, hooks |
| `Taskfile.wails.yml` | Wails app build tasks (shared-utils, logging, logbook) |
| `.github/workflows/validate.yml` | CI: runs on every push to `main` |
| `.github/workflows/release.yml` | CI: runs on `v*` tag push — build + GitHub Release |
| `scripts/pre-commit` | Git hook: regenerates Wails bindings on commit |
| `internal/utils/working_dir.go` | `WorkingDir()` — reads `SM_WORKING_DIR` env var |
