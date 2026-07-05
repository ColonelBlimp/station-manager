#!/usr/bin/env bash
# session-status.sh — surface the freshest orientation at session start.
#
# Wired as a Claude Code SessionStart hook (see .claude/settings.json). Its
# stdout is injected into the session's context, so it guarantees the one thing
# that is otherwise NOT auto-loaded on --resume: the session-handoff "Current
# state" block. It also detects the specific failure that motivated it
# (2026-07-05): a session that ended WITHOUT updating the handoff leaves a stale
# "as of" date, and the next session reads stale backlog "open" lines and
# re-opens closed work. If commits exist after the handoff's as-of date, this
# prints a loud RECONCILE warning so the next session detects-and-recovers
# instead of walking into the trap.
#
# Pure read-only: git log + sed. Never fails the session (always exits 0).

set -uo pipefail
cd "${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null || echo .)}" || exit 0

HANDOFF="docs/session-handoff.md"
today="$(date -u +%Y-%m-%d)"

echo "== Session orientation (injected by scripts/session-status.sh) =="
echo "Today (UTC): ${today}"
echo

[ -f "$HANDOFF" ] || { echo "(no ${HANDOFF} found)"; exit 0; }

# The "Current state" heading + ONLY the most-recent "Recent arc" paragraph
# (stop at the first "Earlier arc"). The full inventory below it duplicates
# CLAUDE.md (already auto-loaded), so surfacing just the freshest arc is the
# orientation that is otherwise missing on --resume — kept short on purpose.
awk '
  /^## Current state/ { p=1 }
  p && /Earlier arc/  { exit }
  p                   { print }
' "$HANDOFF"
echo "  … (full Current-state, session log + Next steps: ${HANDOFF})"

# Staleness check: compare the handoff's as-of date against the newest commit.
asof="$(grep -m1 -oE '## Current state \(as of ([0-9]{4}-[0-9]{2}-[0-9]{2})' "$HANDOFF" \
        | grep -oE '[0-9]{4}-[0-9]{2}-[0-9]{2}')"
last_commit="$(git log -1 --format=%cs 2>/dev/null)"

if [ -n "$asof" ] && [ -n "$last_commit" ] && [[ "$last_commit" > "$asof" ]]; then
  echo
  echo "⚠  RECONCILE BEFORE ACTING — the handoff may be stale."
  echo "   Handoff 'as of': ${asof}   |   newest commit: ${last_commit}"
  echo "   Commits landed after the handoff was last updated, so its Current-state"
  echo "   and the backlog's 'open' items may not reflect finished work. Check"
  echo "   'git log --oneline' since ${asof} and confirm an item is still open"
  echo "   BEFORE re-implementing it. A stale backlog line is not a work order."
  echo
  echo "   Recent commits since the handoff's as-of date:"
  git log --since="${asof} 00:00:00" --format='     %cs %h %s' 2>/dev/null | head -12
fi
exit 0
