#!/usr/bin/env bash
#
# Local mirror of .github/workflows/ci.yml — runs the same gates the
# GHA "releasability" job runs, against your local checkout. Lets you
# answer "would CI pass right now?" without pushing.
#
# Same step order, same fail-fast semantics. The only differences from
# the real CI run are environmental (no fresh container, no setup-go /
# setup-node steps, no automatic dependency cache) — those don't affect
# correctness, only first-run speed.
#
# Run via `task ci:local` or directly: `bash scripts/ci-local.sh`.
#
# Env knobs:
#   SKIP_NPM_CI=1   skip the `npm ci` reinstall (use existing node_modules
#                   from your last `task frontend:install`). Saves ~10s per
#                   SPA — ~30s across the three — during tight iteration;
#                   trade-off is you won't catch package-lock drift.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Step heading helper — uses simple ANSI bold so the steps are scannable
# in a long run, but degrades cleanly when output is captured to a file
# or piped (no colour codes if stdout isn't a TTY).
if [ -t 1 ]; then
    BOLD=$'\033[1m'
    DIM=$'\033[2m'
    RESET=$'\033[0m'
else
    BOLD=""
    DIM=""
    RESET=""
fi

step() {
    printf '\n%s=== %s ===%s\n' "$BOLD" "$1" "$RESET"
}

note() {
    printf '%s%s%s\n' "$DIM" "$1" "$RESET"
}

step "Docs: agent instruction context budget"
bash scripts/check-agent-context.sh

# ───────────────────────────────────────────────────────────────────
# SPA gate — run before the Go gate so the daemon embed sees a fresh
# dist/ for EVERY embedded SPA when the Go build step runs.
#
# ALL THREE SPAs CI gates, in the same order, with the same per-SPA step
# sequence (install → lint → format → type-check → test → build). Until
# 2026-07-31 this mirror ran frontend/app ALONE, so a lint, format, type,
# test or build regression in config or logbook passed locally and failed
# on push — precisely the surprise this script exists to prevent, and a
# gap its own "local mirror" header did not admit to.
#
# It also makes the Go embed-build below honest: that step compiles the
# daemon against all three dist/ trees, and previously two of them could
# be arbitrarily stale.
#
# frontend/logging was retired 2026-07-21 (embed + route removed) and is
# deliberately not gated. config and logbook retire once app is feature
# complete (backlog.md, ADR 0044) — drop them from this list then.
# ───────────────────────────────────────────────────────────────────

for spa in config logbook app; do
    if [ "${SKIP_NPM_CI:-}" = "1" ]; then
        step "SPA/$spa: skipping npm ci (SKIP_NPM_CI=1)"
        note "Using existing node_modules — re-run 'task frontend:install' if package.json changed."
    else
        step "SPA/$spa: npm ci"
        ( cd "frontend/$spa" && npm ci )
    fi

    step "SPA/$spa: lint"
    ( cd "frontend/$spa" && npm run lint )

    # Prettier as a check, matching the gate's position (right after lint).
    # Formatting was advisory until 2026-07-31 and drift reached main unnoticed.
    # Fix with: cd frontend/$spa && npm run format
    step "SPA/$spa: prettier format check"
    ( cd "frontend/$spa" && npm run format:check )

    step "SPA/$spa: svelte-check (--fail-on-warnings)"
    ( cd "frontend/$spa" && npx svelte-check --fail-on-warnings )

    step "SPA/$spa: vitest"
    ( cd "frontend/$spa" && npx vitest run )

    step "SPA/$spa: build (produces dist/ for daemon embed)"
    ( cd "frontend/$spa" && npm run build )
done

# The generated manual is not committed (only manual/public/.gitkeep is), so —
# exactly as hosted CI does before its Go gate — build it here so the daemon
# embed-build compiles against the real page and TestManualHandler_ServesIndexAtRoot
# exercises the served content instead of skipping. Hugo is a hard prerequisite
# for this mirror, same as it is for hosted CI.
step "Manual: build (Hugo → public/ for daemon embed)"
if ! command -v hugo >/dev/null 2>&1; then
    printf 'hugo not found on PATH — required to build the embedded manual.\n' >&2
    note "Install Hugo (extended) 0.162.1 to match CI (see DEVELOPING.md)"
    exit 1
fi
( cd manual && hugo --quiet )

# ───────────────────────────────────────────────────────────────────
# Go gate — gofmt drift first (fastest fail), then vet, then race-test,
# then the daemon embed-build smoke.
# ───────────────────────────────────────────────────────────────────

step "Go: gofmt drift check"
drift="$(gofmt -l ./cmd/ ./internal/)"
if [ -n "$drift" ]; then
    printf 'gofmt drift detected:\n%s\n' "$drift" >&2
    note "Fix with: gofmt -w <files>"
    exit 1
fi
note "gofmt clean"

step "Go: enumerate packages (excluding npm-vendored Go code)"
# `npm ci` populates the SPAs' node_modules/ dirs, which contain
# third-party packages that ship their own Go code (notably flatted's
# Golang variant). `go test ./...` happily descends into them — fine
# today, but a future broken npm dep with broken Go code would fail
# our build for an unrelated reason. Filter once at the source.
PKGS="$(go list ./... | grep -v /node_modules/)"

step "Go: vet"
go vet $PKGS

# Maintainability metrics (.golangci.yml): cognitive/cyclomatic complexity,
# duplication, maintainability index. Scope is ./cmd/... ./internal/... to match
# the measured baseline; the config's own path rules drop generated models and
# npm-vendored Go. Skipped cleanly when not installed, like the CGO checks below
# — CI always runs it, so a missing local binary cannot hide a gate failure, it
# only means you find out on push.
if command -v golangci-lint >/dev/null 2>&1; then
    step "Go: golangci-lint (maintainability metrics)"
    golangci-lint run ./cmd/... ./internal/...
else
    step "Go: golangci-lint — SKIPPED (not installed)"
    note "Install with: sudo dnf install golangci-lint  (CI pins 2.11.3)"
fi

# The destructive smcloud integration tests SKIP here unless the operator opts
# in (SMCLOUD_TEST_DSN=<disposable>, or SMCLOUD_TEST_ALLOW_DEFAULT=1 against a
# disposable `task db:pg:up`). This local mirror deliberately does NOT set that
# itself: it runs on a dev box where localhost:5432 may be a real/dogfood
# smcloud database, and a routine `task ci:local` must never authorize wiping it
# (store.ResolveTestDSN; codex round-3 P1 on 8fd6c7e5). GitHub CI runs these
# against its own ephemeral Postgres service container, so the reconcile
# invariants stay defended there regardless.

# Race detector in -short mode (matches CI): the heavy full-pipeline FT8
# decode tests skip under -short — running a CPU-bound decode under -race
# adds no race-detection value and used to blow the time budget. The full
# run below exercises them without the race detector.
step "Go: test (race detector, -short, 5m timeout)"
go test -race -short -timeout 5m $PKGS

step "Go: test (full, no race, 12m timeout)"
go test -timeout 12m $PKGS

# CGO_ENABLED=0 explicit: with gcc present a bare `go build` links CGO and
# wouldn't exercise the shipped CGO-free/static shape. Mirrors release-rpm.sh.
step "Go: build daemon, static (verifies SPA embed + shipped CGO-free shape)"
CGO_ENABLED=0 go build -o build/bin/smd ./cmd/smd

step "Go: build all cmd/ binaries"
go build ./cmd/...

# PocketFFT (CGO) opt-in backend — not the shipped default, but gated here
# so the CGO path can't rot. Needs a C toolchain; skip cleanly without one
# (CI always has gcc and runs this).
if command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1; then
    step "Go: build daemon (PocketFFT/CGO backend)"
    CGO_ENABLED=1 go build -tags pocketfft -o build/bin/smd-pocketfft ./cmd/smd

    step "Go: FT8 decode test (PocketFFT/CGO backend)"
    CGO_ENABLED=1 go test -tags pocketfft ./internal/ft8/...
else
    step "Go: PocketFFT (CGO) checks — SKIPPED (no C compiler)"
    note "Install gcc to exercise the opt-in CGO FFT backend locally; CI always runs it."
fi

step "Build boundary: ClubLog key stays out of public artifacts (ST-7)"
bash scripts/test-build-boundary.sh

printf '\n%sAll CI gates passed locally.%s\n' "$BOLD" "$RESET"
