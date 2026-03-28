# Development Guide

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| Go | 1.25+ | https://go.dev/dl/ |
| Node.js | 22+ | https://nodejs.org/ |
| Task | 3.x | https://taskfile.dev/installation/ |
| Wails | 2.x | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| snapcraft | latest | `sudo snap install snapcraft --classic` |
| LXD | latest | Required for `snapcraft pack --use-lxd` (see [LXD setup](#lxd-setup)) |

Linux system libraries (needed for Wails builds):

```bash
# Ubuntu / Debian
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev

# Fedora
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
| `task build` | Build all Go modules (non-Wails); outputs to `build/bin/` |
| `task test` | Run all tests (`-race -short`) in every module |
| `task tidy` | `go mod tidy` in every module |
| `task update` | Update all module dependencies (`go get -u ./...` + `go mod tidy`) |

### Wails applications

| Command | Description |
|---|---|
| `task wails:logging APP_VERSION=dev` | Build the logging app only |
| `task wails:logbook APP_VERSION=dev` | Build the logbook app only |
| `task wails APP_VERSION=dev` | Build all Wails apps |
| `task wails:logging:dev` | Run logging app in **dev mode** (hot-reload) |
| `task wails:logbook:dev` | Run logbook app in **dev mode** (hot-reload) |

`APP_VERSION` is **required** for production builds — it gets baked into the
binary via `-ldflags "-X main.version=..."`. For local dev, any string works.
If omitted, it defaults to `dev-<short-git-hash>`.

The build chain for each Wails app:

```
shared-utils:build → <app>:frontend-dep-install → <app>:wails-build
```

Built binaries land in `build/bin/`.

### Wails dev mode

For active development on a Wails app, use the `dev` tasks. These start Wails
in dev mode with hot-reload for both Go and frontend changes:

```bash
# Logging app
task wails:logging:dev

# Logbook app
task wails:logbook:dev
```

Both automatically build `shared-utils` and install frontend dependencies first.

### Local snap testing

| Command | Description |
|---|---|
| `task snap:dev` | Full pipeline: build Wails apps → stage → package snap → install |
| `task snap:dev SNAP_VERSION=0.2.0` | Same, with a custom version string |
| `task snap:stage` | Copy pre-built binaries to repo root for snapcraft |
| `task snap:build` | Run `snapcraft pack --use-lxd` |
| `task snap:install` | Install the `.snap` with `--devmode --dangerous` |
| `task snap:remove` | Uninstall the snap |
| `task snap:clean` | Remove staged binaries, `.snap` file, restore `snapcraft.yaml` |

#### Snap dev workflow

The one-liner for a full local snap test:

```bash
task snap:dev
```

This runs the following steps in sequence:

1. **`wails`** — builds all Wails apps (`smlogger`, `smbook`) with the snap version
2. **`snap:stage`** — copies binaries from `build/bin/` to the repo root where
   `snap/snapcraft.yaml` expects them, and verifies they exist
3. **`snap:build`** — patches the version in `snapcraft.yaml` and runs
   `snapcraft pack --use-lxd`
4. **`snap:install`** — installs the resulting `.snap` locally with `--devmode`

You can then run the snap apps:

```bash
station-manager.sm-logger
station-manager.sm-logbook
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

#### LXD setup

The snap build uses LXD for cross-distro compatibility (the snap targets
`core24` / Ubuntu 24.04). If LXD is not installed:

```bash
sudo snap install lxd
sudo lxd init --auto
sudo usermod -aG lxd $USER
# Log out and back in for the group change to take effect
```

### Other

| Command | Description |
|---|---|
| `task setup-hooks` | Install the Git pre-commit hook |

---

## Pre-commit hook

The hook at `scripts/pre-commit` runs automatically on commit. When files under
`apps/logging/` are staged, it regenerates the Wails JS/TS bindings
(`wails generate module`) and stages the output under
`apps/logging/frontend/src/lib/`:

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
│   ├── config/          CLI configuration tool (Go, to be converted to Wails)
│   ├── logbook/         Logbook viewer (Wails + SvelteKit)
│   └── logging/         QSO logging app (Wails + SvelteKit)
├── cmd/
│   ├── server/          (placeholder)
│   └── tools/           (placeholder)
├── internal/            Shared Go libraries — single module, many packages
├── web/
│   └── shared-utils/    TypeScript/Svelte library shared by Wails frontends
├── build/
│   └── bin/             Compiled binaries land here
├── snap/
│   ├── snapcraft.yaml   Snap package definition (all apps in one snap)
│   ├── gui/             .desktop files and icons for snap apps
│   └── local/           AppStream metainfo XML
├── scripts/
│   └── pre-commit       Git hook script
├── .github/
│   └── workflows/       CI: validate.yml, release.yml
├── Taskfile.yml          Root task runner (Go build, test, tidy, snap)
├── Taskfile.wails.yml    Wails build tasks (shared-utils, logging, logbook)
├── RELEASING.md          CI/CD and release process documentation
├── DEVELOPING.md         This file
└── AGENTS.md             AI coding agent instructions
```

### Ignored files

The `.gitignore` excludes:
- `.env` — local environment overrides (loaded by Taskfile via `dotenv`)
- `build/*` — compiled binaries and runtime artifacts (except `.gitkeep` files)
- `.task` — Task runner cache directory (used for `sources`/`generates` checks)

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

## Environment variables

| Variable | Purpose | Default |
|---|---|---|
| `SM_WORKING_DIR` | Override data directory for all apps | Executable's directory |
| `APP_VERSION` | Baked into Wails binaries at build time | `dev-<git-short-hash>` |
| `SNAP_VERSION` | Version string for local snap builds | `dev-local` |

A `.env` file in the project root is loaded automatically by the Taskfile
(`dotenv: ['.env']`). This is useful for setting `SM_WORKING_DIR` during
development without polluting your shell.

---

## Key files

| File | Purpose |
|---|---|
| `go.work` | Go workspace — lists all active modules |
| `Taskfile.yml` | Root task runner (build, test, tidy, update, snap) |
| `Taskfile.wails.yml` | Wails app build chain (shared-utils → frontend → wails) |
| `snap/snapcraft.yaml` | Snap package definition for the full suite |
| `.github/workflows/validate.yml` | CI: vet, fmt, test, lint on every push to `main` |
| `.github/workflows/release.yml` | CI: build + snap + GitHub Release on `v*` tag |
| `scripts/pre-commit` | Git hook: regenerates Wails bindings on commit |
| `internal/utils/working_dir.go` | `WorkingDir()` — data directory resolution |
| `RELEASING.md` | Full release process documentation |
| `AGENTS.md` | AI coding agent instructions |

