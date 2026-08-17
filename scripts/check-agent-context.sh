#!/usr/bin/env bash
# Keep the always-loaded project instruction surface small and single-sourced.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

MAX_AGENTS_BYTES=8192
MAX_CLAUDE_BYTES=256

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

printf 'Agent context: AGENTS.md %s/%s bytes; CLAUDE.md compatibility shim %s bytes.\n' \
    "$agents_bytes" "$MAX_AGENTS_BYTES" "$claude_bytes"
