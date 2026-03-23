# AGENTS.md — Station Manager

## Project Overview
Station Manager is a suite of Linux desktop applications for Amateur Radio (ham radio) station management. Built with **Go** (backend/services) + **SvelteKit** (UI) bound together via the **Wails** framework. The stack targets Linux only and is designed to operate **fully offline**—internet features (forwarding QSOs to QRZ.com/Station Master) are optional addons.

## Architecture
The repo is a **Go multi-module workspace** (`go.work`, Go 1.25). Each subdirectory under `apps/`, `internal/`, `pkg/`, and `cmd/` is intended to be its own Go module with its own `go.mod`.

```
apps/           # Wails desktop apps (each is a standalone app)
  config/       # Station configuration app
  logbook/      # QSO logbook app
  logging/      # Logging/debugging app
cmd/
  server/       # Background service / API server entrypoint
  tools/        # CLI utilities
internal/       # Private packages (not importable externally)
  apikey/       # API key management for online services
  cat/          # Computer Aided Transceiver (CAT) control — Yaesu FTdx10 & FT7-100
  config/       # Station configuration loading/persistence
  database/     # SQLite (assumed) database layer
  email/        # Email notification support
  forwarding/   # QSO forwarding to external logbooks (QRZ.com, Station Master)
  iocdi/        # IoC / dependency injection container
  listeners/    # Event listeners / hardware event bus
  logging/      # Structured internal logging
  lookup/       # Callsign/entity lookup (offline-capable)
  serial/       # Serial port communication (CAT, keyer, etc.)
pkg/            # Public/reusable packages
  adapters/     # Port adapters (hexagonal-style)
  adif/         # ADIF file format (Amateur Data Interchange Format) parser/writer
  enums/        # Shared enumeration types
  errors/       # Project-wide error types
  maidenhead/   # Maidenhead grid locator utilities
  types/        # Shared domain types
  utils/        # General-purpose utilities
web/
  shared-utils/ # Shared SvelteKit utilities across apps
build/
  bin/          # Compiled binaries output
  db/           # SQLite DB files at runtime
  logs/         # Application log files
```

## Build & Developer Workflow
This project uses [Task](https://taskfile.dev) (`task`) as its build runner — **not** `make`.

| Command | Description |
|---|---|
| `task` | Sync the Go workspace (`go work sync`) |
| `task build` | Build all modules (`go build ./...`) |
| `task test` | Run all tests with race detector, short mode (`go test -race ./... -short`) |
| `task tidy` | Run `go mod tidy` across every module that has a `go.mod` |

When creating a new module under `apps/`, `internal/`, `pkg/`, or `cmd/`, run `task` (workspace sync) afterward and ensure the module has its own `go.mod`.

## Domain Terminology
- **QSO** — a radio contact/conversation between two operators
- **ADIF** — Amateur Data Interchange Format; the standard log exchange format
- **CAT** — Computer Aided Transceiver; serial protocol to control radios
- **Maidenhead** — grid locator system used to identify geographic positions in ham radio
- **CW** — Morse code (Continuous Wave) operation mode
- **QRZ.com / Station Master** — online ham radio logbook services (optional forwarding targets)

## Key Conventions
- **Hexagonal / ports-and-adapters**: `pkg/adapters/` and `internal/` separation signals this pattern—business logic lives in `internal/`, I/O adapters in `pkg/adapters/`.
- **Offline-first**: All features must degrade gracefully without internet. Internet-dependent code belongs in `internal/forwarding/` or `internal/apikey/` and must be gated.
- **IoC/DI via `internal/iocdi/`**: Dependency injection is handled by a project-specific container; prefer wiring through it rather than direct construction in app entry points.
- **Targeted CAT hardware**: Only Yaesu FTdx10 and FT7-100 have been tested. CAT code in `internal/cat/` and `internal/serial/` should treat other rigs as untested/best-effort.
- **No single root `go.mod`**: The workspace is managed via `go.work` only. Do not create a root-level `go.mod`.

