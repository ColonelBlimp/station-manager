# Development Guide

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| Go | 1.25+ | https://go.dev/dl/ |
| Node.js | 22+ | https://nodejs.org/ |
| Task | 3.x | https://taskfile.dev/installation/ |
| Wails | 2.x | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| snapcraft | latest | `sudo snap install snapcraft --classic` |

Linux system libraries (needed for Wails builds):

```bash
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
# or on Fedora:
sudo dnf install gtk3-devel webkit2gtk4.1-devel gcc pkg-config
```

---

## First-time setup

```bash
git clone https://github.com/ColonelBlimp/station-manager.git
cd station-manager

# Sync the Go workspace
task

# Install the pre-commit hook
task setup-hooks
```

---

## Task reference

Run `task --list` for a full listing. The key commands:

### Go modules

| Command | Description |
|---|---|
| `task` | Sync the Go workspace (`go work sync`) |
| `task build` | Build all Go modules (non-Wails) |
| `task test` | Run all tests (`-race -short`) |
| `task tidy` | `go mod tidy` in every module |
| `task update` | Update all module dependencies |

### Wails applications

| Command | Description |
|---|---|
| `task wails:logging APP_VERSION=dev` | Build the logging app only |
| `task wails:logbook APP_VERSION=dev` | Build the logbook app only |
| `task wails APP_VERSION=dev` | Build all Wails apps |

`APP_VERSION` is **required** — it gets baked into the binary via
`-ldflags "-X main.version=..."`. For local dev, any string works.

The build chain for each Wails app:

```
shared-utils:build → <app>:frontend-dep-install → <app>:frontend-build → <app>:wails-build
```

Built binaries land in `build/bin/`.

### Local snap testing

| Command | Description |
|---|---|
| `task snap:dev` | Full pipeline: build everything → package snap → install |
| `task snap:dev SNAP_VERSION=0.2.0` | Same, with a custom version string |
| `task snap:stage` | Copy pre-built binaries to repo root for snapcraft |
| `task snap:build` | Run `snapcraft --destructive-mode` |
| `task snap:install` | Install the `.snap` with `--devmode --dangerous` |
| `task snap:remove` | Uninstall the snap |
| `task snap:clean` | Remove staged binaries, `.snap` file, restore `snapcraft.yaml` |

#### Snap dev workflow

The one-liner for a full local snap test:

```bash
task snap:dev
```

This runs the following steps in sequence:

1. **`build`** — compiles all non-Wails Go binaries (e.g. `config`)
2. **`wails`** — builds all Wails apps (`smlogger`, `smbook`)
3. **`snap:stage`** — copies binaries to the repo root where `snap/snapcraft.yaml`
   expects them, and verifies they exist
4. **`snap:build`** — patches the version in `snapcraft.yaml` and runs
   `snapcraft --destructive-mode`
5. **`snap:install`** — installs the resulting `.snap` locally

You can then run the snap apps:

```bash
station-manager.sm-logger
station-manager.sm-logbook
station-manager.sm-config
```

Snap data is written to `$SNAP_USER_COMMON` (`~/snap/station-manager/common/`):

```
~/snap/station-manager/common/
├── config.json
├── db/data.db
└── logs/
```

To tear down:

```bash
task snap:remove   # uninstall the snap
task snap:clean    # remove staged files and restore snapcraft.yaml
```

### Other

| Command | Description |
|---|---|
| `task setup-hooks` | Install the Git pre-commit hook |

---

## Pre-commit hook

The hook at `scripts/pre-commit` runs automatically on commit. When files under
`apps/logging/` are staged, it regenerates the Wails JS/TS bindings and stages
the output:

```bash
# Manual install / update:
task setup-hooks
```

The hook requires `wails` on `PATH`. It prepends `$HOME/go/bin` automatically
for environments (like Git hooks) that don't inherit the full shell `PATH`.

---

## Project layout

```
station-manager/
├── apps/
│   ├── config/          CLI configuration tool (Go)
│   ├── logbook/         Logbook viewer (Wails + SvelteKit)
│   └── logging/         QSO logging app (Wails + SvelteKit)
├── cmd/
│   ├── server/          (placeholder)
│   └── tools/           (placeholder)
├── internal/            Shared Go libraries (adapters, adif, cat, config, ...)
├── web/
│   └── shared-utils/    TypeScript/Svelte library shared by frontends
├── build/
│   └── bin/             Compiled binaries land here
├── snap/
│   └── snapcraft.yaml   Snap package definition (all apps in one snap)
├── scripts/
│   └── pre-commit       Git hook script
├── Taskfile.yml          Root task runner
├── Taskfile.wails.yml    Wails build tasks
├── RELEASING.md          CI/CD and release process documentation
└── AGENTS.md             AI agent instructions
```

---

## Working directory resolution

All apps use `utils.WorkingDir()` (`internal/utils/working_dir.go`) to find
their data directory. The resolution order:

1. Explicit argument (if passed)
2. `SM_WORKING_DIR` environment variable
3. Directory of the running executable

In the snap, `SM_WORKING_DIR` is set to `$SNAP_USER_COMMON` automatically.
For local development outside the snap, the binary's own directory is used by
default, or you can set the env var:

```bash
SM_WORKING_DIR=/tmp/sm-dev ./build/bin/smlogger
```

---

## Key files

| File | Purpose |
|---|---|
| `go.work` | Go workspace — lists all active modules |
| `Taskfile.yml` | Root task runner (build, test, tidy, snap) |
| `Taskfile.wails.yml` | Wails app build chain (shared-utils → frontend → wails) |
| `snap/snapcraft.yaml` | Snap package definition for the full suite |
| `.github/workflows/validate.yml` | CI: vet, fmt, test, lint on every push |
| `.github/workflows/release.yml` | CI: build + snap + GitHub Release on `v*` tag |
| `scripts/pre-commit` | Git hook: regenerates Wails bindings |
| `internal/utils/working_dir.go` | `WorkingDir()` — data directory resolution |
| `RELEASING.md` | Full release process documentation |
| `AGENTS.md` | AI coding agent instructions |

