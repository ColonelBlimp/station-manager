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
#                   from your last `task frontend:install`). Saves ~10s
#                   per run during tight iteration; trade-off is you won't
#                   catch package-lock drift.

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

# ───────────────────────────────────────────────────────────────────
# SPA gate — run before Go gate so the daemon embed sees the fresh
# dist/ when the Go build step runs.
# ───────────────────────────────────────────────────────────────────

if [ "${SKIP_NPM_CI:-}" = "1" ]; then
    step "SPA: skipping npm ci (SKIP_NPM_CI=1)"
    note "Using existing node_modules — re-run 'task frontend:install' if package.json changed."
else
    step "SPA: npm ci"
    ( cd frontend/logging && npm ci )
fi

step "SPA: lint"
( cd frontend/logging && npm run lint )

step "SPA: svelte-check (--fail-on-warnings)"
( cd frontend/logging && npx svelte-check --fail-on-warnings )

step "SPA: vitest"
( cd frontend/logging && npx vitest run )

step "SPA: build (produces dist/ for daemon embed)"
( cd frontend/logging && npm run build )

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
# `npm ci` populates frontend/logging/node_modules/, which contains
# third-party packages that ship their own Go code (notably flatted's
# Golang variant). `go test ./...` happily descends into them — fine
# today, but a future broken npm dep with broken Go code would fail
# our build for an unrelated reason. Filter once at the source.
PKGS="$(go list ./... | grep -v /node_modules/)"

step "Go: vet"
go vet $PKGS

step "Go: test (race detector, 5m timeout)"
go test -race -timeout 5m $PKGS

step "Go: build daemon (verifies SPA embed)"
go build -o build/bin/smd ./cmd/smd

step "Go: build all cmd/ binaries"
go build ./cmd/...

printf '\n%sAll CI gates passed locally.%s\n' "$BOLD" "$RESET"
