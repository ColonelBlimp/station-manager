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

Pushing the tag triggers the **Release** workflow, which runs five sequential
stages:

```
validate ──┬── build-go ────┐
           │                │
           └── build-wails ─┼── snap ── release
                            │
                            └───────────────┘
```

#### Stage 1 — Validate

Same checks as the daily workflow (vet, fmt, test, lint), run as a single job
to gate the build stages.

#### Stage 2a — Build Go Binaries (`build-go`)

Builds all non-Wails `package main` modules under `apps/` and `cmd/`.
Binary names are derived from the directory name (e.g. `apps/config` → `config`).
Outputs uploaded as artifact `go-binaries-linux-amd64`.

#### Stage 2b — Build Wails Apps (`build-wails`)

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

#### Stage 3 — Build & Publish Snap (`snap`)

Requires both `build-go` and `build-wails` to complete. Downloads all binaries
into the repo root, patches `snap/snapcraft.yaml` with the version from the tag
(stripping the `v` prefix: `v1.2.3` → `1.2.3`), then:

1. Runs `snapcraft` via `snapcore/action-build@v1`
2. Publishes to the **Snap Store `edge` channel** via `snapcore/action-publish@v1`
3. Uploads the `.snap` file as artifact `snap-linux-amd64`

##### Snap package contents

The snap `station-manager` bundles all apps in a single package:

| Snap command | Binary | Type |
|---|---|---|
| `station-manager.sm-logger` | `smlogger` | Wails GUI |
| `station-manager.sm-logbook` | `smbook` | Wails GUI |
| `station-manager.sm-config` | `config` | CLI |

##### Snap runtime data

All apps read `SM_WORKING_DIR` (set to `$SNAP_USER_COMMON` in the snap
environment). Data persists across snap refreshes:

```
$SNAP_USER_COMMON/
├── config.json
├── db/data.db
└── logs/
```

#### Stage 4 — Create GitHub Release (`release`)

Downloads all three artifacts and creates a GitHub Release with:

- **Tag name** and **release name** set to the tag (e.g. `v1.2.3`)
- **Auto-generated release notes** from commits since the last tag
- **Attached assets**: Go binaries, Wails binaries, and the `.snap` file

---

## Snap Store promotion

Releases are published to the `edge` channel automatically. To promote:

```bash
# Promote to beta
snapcraft release station-manager <revision> beta

# Promote to stable
snapcraft release station-manager <revision> stable
```

Find the revision number on https://snapcraft.io/station-manager/releases or
via `snapcraft revisions station-manager`.

---

## Required GitHub secrets

| Secret | Purpose | How to generate |
|---|---|---|
| `SNAPCRAFT_STORE_CREDENTIALS` | Authenticate `snapcraft upload/release` | `snapcraft export-login --snaps=station-manager --channels=edge,beta,candidate,stable -` |

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
| `snap/snapcraft.yaml` | Snap package definition for the full suite |
| `.github/workflows/validate.yml` | CI: runs on every push to `main` |
| `.github/workflows/release.yml` | CI: runs on `v*` tag push — build + snap + GitHub Release |
| `scripts/pre-commit` | Git hook: regenerates Wails bindings on commit |
| `internal/utils/working_dir.go` | `WorkingDir()` — reads `SM_WORKING_DIR` env var |

