#!/usr/bin/env bash
# Inject bounded current orientation plus facts derived from Git. Read-only and
# deliberately non-failing: a broken orientation must not block a session.

set -uo pipefail
cd "${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null || echo .)}" || exit 0

CURRENT="docs/current.md"
HANDOFF="docs/session-handoff.md"
MAX_CURRENT_BYTES=2048
MAX_OUTPUT_BYTES=3000

utf8_trim() {
    if command -v iconv >/dev/null 2>&1; then
        iconv -c -f UTF-8 -t UTF-8 2>/dev/null
    else
        LC_ALL=C sed 's/[\xC2-\xF4][\x80-\xBF]*$//'
    fi
}

header="$(printf '== Session orientation (scripts/session-status.sh) ==\nToday (UTC): %s' \
    "$(date -u +%Y-%m-%d)")"

warning=""
capsule=""
if [ ! -f "$CURRENT" ]; then
    warning="⚠  ORIENTATION DEGRADED — ${CURRENT} is missing."
else
    capsule="$(cat "$CURRENT")"
    current_bytes="$(wc -c < "$CURRENT")"
    updated="$(sed -nE 's/^Updated: ([0-9]{4}-[0-9]{2}-[0-9]{2})$/\1/p' "$CURRENT" | head -1)"
    last_commit_date="$(git log -1 --format=%cs 2>/dev/null || true)"

    if [ "$current_bytes" -gt "$MAX_CURRENT_BYTES" ]; then
        warning="⚠  ORIENTATION TOO LARGE — ${CURRENT} is ${current_bytes} bytes; limit ${MAX_CURRENT_BYTES}."
    elif [ -z "$updated" ]; then
        warning="⚠  ORIENTATION FORMAT PROBLEM — ${CURRENT} has no 'Updated: YYYY-MM-DD' line."
    # Strict ">" is deliberate: both values are date-only (YYYY-MM-DD), so a
    # same-day commit — including the very commit that refreshes this capsule —
    # is not stale. ">=" would fire on every capsule update, training the reader
    # to ignore the warning. Intra-day staleness is not representable here and is
    # accepted as out of scope.
    elif [ -n "$last_commit_date" ] && [[ "$last_commit_date" > "$updated" ]]; then
        warning="⚠  RECONCILE BEFORE ACTING — ${CURRENT} was updated ${updated}; newest commit is ${last_commit_date}."
    fi
fi

branch="$(git branch --show-current 2>/dev/null || true)"
[ -n "$branch" ] || branch="detached@$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
dirty_count="$(git status --porcelain=v1 2>/dev/null | wc -l | tr -d ' ')"
upstream="$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)"

if [ -n "$upstream" ]; then
    counts="$(git rev-list --left-right --count "${upstream}...HEAD" 2>/dev/null || true)"
    read -r behind ahead <<< "$counts"
    upstream_state="${upstream}: ahead ${ahead:-?}, behind ${behind:-?}"
else
    upstream_state="no upstream configured"
fi

recent="$(git log -3 --format='  %h %s' 2>/dev/null | cut -c 1-100)"
repo_state="$(printf 'Repository facts (live):\n- branch: %s\n- worktree: %s changed path(s)\n- upstream: %s\n- recent commits:\n%s' \
    "$branch" "$dirty_count" "$upstream_state" "$recent")"

pointers="$(printf 'More context on demand: %s · docs/README.md' "$HANDOFF")"

payload="$header"
[ -n "$warning" ] && payload="${payload}

${warning}"
payload="${payload}

${repo_state}"
[ -n "$capsule" ] && payload="${payload}

${capsule}"
payload="${payload}

${pointers}"

payload_bytes="$(printf '%s\n' "$payload" | wc -c)"
if [ "$payload_bytes" -le "$MAX_OUTPUT_BYTES" ]; then
    printf '%s\n' "$payload"
    exit 0
fi

notice="⚠  OUTPUT TRUNCATED at ${MAX_OUTPUT_BYTES} bytes. Trim ${CURRENT}; do not raise the cap."
notice_bytes="$(printf '\n\n%s\n' "$notice" | wc -c)"
body_budget=$(( MAX_OUTPUT_BYTES - notice_bytes ))
printf '%s' "$payload" | head -c "$body_budget" | utf8_trim
printf '\n\n%s\n' "$notice"
exit 0
