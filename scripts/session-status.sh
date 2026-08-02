#!/usr/bin/env bash
# session-status.sh — surface the freshest orientation at session start.
#
# Wired as a Claude Code SessionStart hook (see .claude/settings.json). Its
# stdout is injected into the session's context, so it guarantees the one thing
# that is otherwise NOT auto-loaded on --resume: where the project currently is.
#
# It also detects the failure that motivated it (2026-07-05): a session that
# ended WITHOUT updating the handoff leaves a stale "as of" date, and the next
# session reads stale "open" lines and re-opens closed work.
#
# ---------------------------------------------------------------------------
# WHY THIS SCRIPT IS SIZE-OBSESSED (the 2026-08-02 failure — read before editing)
#
# The previous version stopped printing at a prose marker, /Earlier arc/. That
# marker was removed from the handoff at some point and NOTHING NOTICED, so the
# awk never exited and printed from "## Current state" to end of file: 231 KB.
# The harness caps injected output, so the session received `Output too large`
# plus a 2 KB preview — about 40 lines — and silently dropped the rest.
#
# The RECONCILE warning was printed AFTER that block, so it was never delivered
# at all. The staleness guard had been unreachable for as long as the marker had
# been missing. A guard that fails silently is worse than no guard.
#
# Three rules follow, and each is enforced below rather than trusted:
#   1. THE WARNING GOES FIRST. Anything that can be truncated must come after
#      the thing that must not be.
#   2. INJECT A BOUNDED SECTION, NOT A GROWING ONE. Only "## Now" is emitted.
#      It is short by editorial rule; "## Current state" is a rolling record
#      that grows forever and must never be injected wholesale. Slicing a
#      growing section on a marker was the original mistake.
#   3. HARD BYTE CAP, and say so when it bites. Never emit an unbounded slice of
#      a file that grows without limit.
#
# Pure read-only: git log + awk. Never fails the session (always exits 0).

set -uo pipefail
cd "${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null || echo .)}" || exit 0

HANDOFF="docs/session-handoff.md"
# Budget for the whole injection. Small on purpose: this is orientation, not
# the record. Everything else is one grep away and is pointed at below.
MAX_BYTES=6000

echo "== Session orientation (injected by scripts/session-status.sh) =="
echo "Today (UTC): $(date -u +%Y-%m-%d)"
echo

[ -f "$HANDOFF" ] || { echo "(no ${HANDOFF} found)"; exit 0; }

# --- 1. STALENESS CHECK, FIRST -------------------------------------------
asof="$(grep -m1 -oE '## Current state \(as of ([0-9]{4}-[0-9]{2}-[0-9]{2})' "$HANDOFF" \
        | grep -oE '[0-9]{4}-[0-9]{2}-[0-9]{2}')"
last_commit="$(git log -1 --format=%cs 2>/dev/null)"

if [ -z "$asof" ]; then
  echo "⚠  HANDOFF FORMAT PROBLEM — no '## Current state (as of YYYY-MM-DD)' heading found."
  echo "   The staleness guard cannot run. Fix the heading before trusting this doc."
  echo
elif [ -n "$last_commit" ] && [[ "$last_commit" > "$asof" ]]; then
  echo "⚠  RECONCILE BEFORE ACTING — the handoff may be stale."
  echo "   Handoff 'as of': ${asof}   |   newest commit: ${last_commit}"
  echo "   Commits landed after the handoff was last updated, so its Current-state"
  echo "   and any 'open' items may not reflect finished work. Confirm an item is"
  echo "   still open BEFORE re-implementing it. A stale line is not a work order."
  echo
  echo "   Commits since the handoff's as-of date:"
  git log --since="${asof} 00:00:00" --format='     %cs %h %s' 2>/dev/null | head -12
  echo
fi

# --- 2. THE "## Now" SECTION ---------------------------------------------
# One heading-to-next-heading slice, and deliberately ONLY that one. The whole
# point of the 2026-08-02 restructure is that orientation and the record are
# different documents that happened to share a file: "## Now" is bounded by
# editorial discipline (~25 lines, stated in its own HTML comment), whereas
# "## Current state" grows without limit by design. Injecting the bounded one
# is what makes the cap below a backstop rather than the primary mechanism.
# HTML comments are dropped — they are instructions to the editor, not context.
section() {
  awk -v want="$1" '
    $0 ~ want   { inside = 1; print; next }
    !inside     { next }
    /^## /      { exit }
    /^<!--/     { skip = 1 }
    skip        { if (/-->/) skip = 0; next }
    { print; found = 1 }
    END { exit(found ? 0 : 1) }
  ' "$HANDOFF"
}

# --- 3. EMIT, UNDER THE CAP ----------------------------------------------
# Collected first, then truncated as a whole. The warning above is already out
# and is never at risk; only this part can be cut, and it says so when it is.
# An unenforced cap is the bug this script exists to not repeat, so the
# truncation is applied here rather than assumed from the section sizes.
body="$(
  if ! section '^## Now'; then
    echo "⚠  No '## Now' section in ${HANDOFF}. Orientation is DEGRADED — that"
    echo "   section is the ONLY one injected here; without it this hook shows"
    echo "   nothing about current state. Read ${HANDOFF} directly this session."
  fi
)"

if [ "${#body}" -gt "$MAX_BYTES" ]; then
  printf '%s\n' "${body:0:$MAX_BYTES}"
  echo
  echo "⚠  TRUNCATED at ${MAX_BYTES} bytes (block was ${#body}). The newest block has"
  echo "   outgrown the orientation budget — trim it in ${HANDOFF}, and read that"
  echo "   file directly this session."
else
  printf '%s\n' "$body"
fi

echo
echo "  … full session log + older arcs: ${HANDOFF} · docs/session-handoff-archive.md"
echo "  … doc map (which docs are live vs historical): docs/README.md"