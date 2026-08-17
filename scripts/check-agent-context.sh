#!/usr/bin/env bash
# Keep the always-loaded project instruction surface small and single-sourced.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

MAX_AGENTS_BYTES=8192
MAX_CLAUDE_BYTES=256
MAX_CURRENT_BYTES=2048
MAX_ORDINARY_CONTEXT_BYTES=10240

agents_bytes="$(wc -c < AGENTS.md)"
if [ "$agents_bytes" -gt "$MAX_AGENTS_BYTES" ]; then
    printf 'AGENTS.md is %s bytes; the automatic-context budget is %s bytes.\n' \
        "$agents_bytes" "$MAX_AGENTS_BYTES" >&2
    exit 1
fi

claude_bytes="$(wc -c < CLAUDE.md)"
if [ "$claude_bytes" -gt "$MAX_CLAUDE_BYTES" ]; then
    printf 'CLAUDE.md is %s bytes; it must remain a small AGENTS.md compatibility shim.\n' \
        "$claude_bytes" >&2
    exit 1
fi

expected_claude="# Claude Code project instructions

@AGENTS.md"
if [ "$(cat CLAUDE.md)" != "$expected_claude" ]; then
    printf 'CLAUDE.md must contain only the project heading and @AGENTS.md import.\n' >&2
    exit 1
fi

current_bytes="$(wc -c < docs/current.md)"
if [ "$current_bytes" -gt "$MAX_CURRENT_BYTES" ]; then
    printf 'docs/current.md is %s bytes; the current-context budget is %s bytes.\n' \
        "$current_bytes" "$MAX_CURRENT_BYTES" >&2
    exit 1
fi

required_fields=(
    Goal
    State
    Next
    "Decisions not to revisit"
    "Do not"
    "Relevant files"
    Coordination
)
for field in "${required_fields[@]}"; do
    if ! grep -Fq -- "- **${field}:**" docs/current.md; then
        printf 'docs/current.md is missing the required "%s" field.\n' "$field" >&2
        exit 1
    fi
done

session_payload_bytes="$(bash scripts/session-status.sh | wc -c)"
ordinary_context_bytes=$(( agents_bytes + claude_bytes + session_payload_bytes ))
if [ "$ordinary_context_bytes" -gt "$MAX_ORDINARY_CONTEXT_BYTES" ]; then
    printf 'Ordinary automatic context is %s bytes; the budget is %s bytes.\n' \
        "$ordinary_context_bytes" "$MAX_ORDINARY_CONTEXT_BYTES" >&2
    exit 1
fi

printf 'Agent context: kernel %s/%s bytes; current %s/%s bytes; automatic total %s/%s bytes.\n' \
    "$agents_bytes" "$MAX_AGENTS_BYTES" "$current_bytes" "$MAX_CURRENT_BYTES" \
    "$ordinary_context_bytes" "$MAX_ORDINARY_CONTEXT_BYTES"
