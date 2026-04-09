# Context Handoff: Station Manager — FT8 CLI Hardening Complete

## What was just completed

**FT8 CLI hardening** — config validation, IOCDI preseed timing fix for console logging, and `task ft8` build task.

### 1. FT8 Config Validation

Added `validate` struct tags to all `FT8Config` fields in `internal/types/ft8.go` and created `internal/ft8/service/validation.go` with `validateConfig()` (using `go-playground/validator/v10`), wired into `Initialize()` before applying defaults. 18 tests in `validation_test.go` cover all validated fields.

### 2. Console Logging Fix (IOCDI Preseed Timing)

**Root cause:** `container.Build()` auto-calls `Initialize()` on all beans in dependency order. The FT8 CLI's preseed (`ConsoleLogging: true`) was applied *after* `Build()`, by which time both `configService` and `logService` were already initialized with the on-disk `config.json` values (`console_logging: false, file_logging: true`).

**Fix:** In `cmd/ft8/cmd/root.go`, the `configService` is now created manually with the preseed set, then registered as an instance via `RegisterInstance` *before* `Build()`. The `config.Service.Initialize()` preseed logic (saves `LoggingConfig` before disk load, restores if `Level != ""`) then preserves the CLI's console-only logging config.

### 3. Build Task

Added `task ft8` to `Taskfile.yml` with incremental source tracking. Updated `AGENTS.md` and `DEVELOPING.md`.

### Files created/modified:

1. **`internal/types/ft8.go`** — added `validate` struct tags to all config fields.
2. **`internal/ft8/service/validation.go`** (new) — `validateConfig()` using validator/v10.
3. **`internal/ft8/service/validation_test.go`** (new) — 18 tests.
4. **`cmd/ft8/cmd/root.go`** — restructured `setup()`: config service created and preseeded before `Build()`; removed diagnostic code and unused imports.
5. **`Taskfile.yml`** — added `ft8` task.
6. **`AGENTS.md`** — added `task ft8` to build commands section.
7. **`DEVELOPING.md`** — added CLI tools subsection with `task ft8`.

## Current state of the project

Refer to `docs/whats-next.md` for full status.

**All milestones through FT8 CLI hardening are complete:**
- `internal/ft8/dsp/` — all 9 files (including `decimate.go`) ✅
- `internal/ft8/synth/` — gfsk.go + synth.go ✅
- `internal/audio/` — PlaySamples ✅
- `internal/ft8/service/` — RX + TX service ✅
- **Live FT8 RX decoding** from FTdx10 on 28.074 MHz ✅
- **FT8 config validation** — struct tags + validateConfig in Initialize ✅
- **FT8 CLI console logging** — IOCDI preseed timing fix ✅
- **Build task** — `task ft8` with incremental source tracking ✅

## What comes next (from docs/whats-next.md)

1. **Window alignment to 15 s wall-clock boundaries** — WSJT-X's Detector.cpp resets the accumulation buffer at T%15==0 boundaries. The current implementation just accumulates continuously. This may affect decode rate slightly.
2. **Extended live testing** — run more windows and compare decode rate against WSJT-X on the same band/time.
3. **QSO state machine** — slot selection (even/odd auto-toggle), timeout handling, duplicate suppression, RRR/RR73 handling, auto-reply sequencing.

## Key files to read for context
- `docs/whats-next.md` — master plan and status
- `docs/code-review-ft8-service.md` — code review findings (all actioned)
- `internal/ft8/dsp/decimate.go` — 48 kHz → 12 kHz FIR decimation filter
- `internal/ft8/service/service.go` — RX+TX lifecycle (Initialize/Start/Stop/Close)
- `internal/ft8/service/validation.go` — FT8 config validation
- `internal/ft8/service/tx.go` — TX orchestration (Transmit/CancelTX/executeTX)
- `internal/ft8/dsp/dsp.go` — `ProcessWindow` (the DSP pipeline entry point)
- `internal/ft8/timing/timing.go` — window boundary calculations + `WaitForNext`
- `internal/ft8/synth/synth.go` — GFSK synthesis (`Synthesize`)
- `internal/audio/capture.go` — audio capture with `Samples()` channel
- `internal/audio/playback.go` — `PlaySamples` for TX audio output
- `internal/ptt/ptt.go` — PTT control (Assert/Release/Close)
- `internal/config/service.go` — config service with preseed logic (lines 57–67)
- `internal/iocdi/container.go` — DI container, Build() auto-init (lines 258–268)
- `internal/types/ft8.go` — `FT8Config` struct (RX + TX + PTT + parity fields)
- `internal/types/services.go` — DI bean ID constants
- `cmd/ft8/cmd/root.go` — CLI entry point (preseed-before-Build pattern)
- `cmd/ft8/cmd/diag.go` — Audio diagnostics tool
- `AGENTS.md` — coding conventions and workspace structure

