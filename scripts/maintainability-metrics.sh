#!/usr/bin/env bash
# Produce the complete Go/frontend maintainability report and enforce its debt ratchet.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

OUTPUT_DIR="${MAINTAINABILITY_OUTPUT_DIR:-/tmp/station-manager-maintainability}"
GO_REPORT="$OUTPUT_DIR/golangci.json"
FRONTEND_REPORT="$OUTPUT_DIR/eslint.json"
NORMALIZED_REPORT="$OUTPUT_DIR/report.json"
MARKDOWN_REPORT="$OUTPUT_DIR/summary.md"

mkdir -p "$OUTPUT_DIR"

read -r EXPECTED_GOLANGCI_VERSION < .golangci-lint-version
actual_golangci_version="$(golangci-lint --version)"
if [[ "$actual_golangci_version" != *"version $EXPECTED_GOLANGCI_VERSION "* ]]; then
    printf 'golangci-lint version mismatch: want %s; got %s\n' \
        "$EXPECTED_GOLANGCI_VERSION" "$actual_golangci_version" >&2
    exit 1
fi

golangci-lint run \
    --config .golangci.metrics.yml \
    --issues-exit-code=0 \
    --output.text.path=/dev/null \
    --output.json.path="$GO_REPORT" \
    --show-stats=false \
    ./cmd/... ./internal/...

(
    cd frontend/app
    npm exec eslint -- \
        --no-inline-config \
        --format json \
        --output-file "$FRONTEND_REPORT" \
        --rule 'complexity: [1, 0]' \
        --rule 'max-lines-per-function: [1, {"max": 0, "skipBlankLines": true, "skipComments": true}]' \
        --rule 'max-depth: [1, 0]' \
        --rule 'svelte/prefer-svelte-reactivity: 1' \
        src
)

status=0
go run ./cmd/maintreport \
    -root "$REPO_ROOT" \
    -go-report "$GO_REPORT" \
    -frontend-report "$FRONTEND_REPORT" \
    -baseline quality/maintainability-baseline.json \
    -json-output "$NORMALIZED_REPORT" \
    -markdown-output "$MARKDOWN_REPORT" || status=$?

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    cp "$MARKDOWN_REPORT" "$GITHUB_STEP_SUMMARY"
fi

printf 'Maintainability outputs:\n  %s\n  %s\n' "$MARKDOWN_REPORT" "$NORMALIZED_REPORT"
exit "$status"
