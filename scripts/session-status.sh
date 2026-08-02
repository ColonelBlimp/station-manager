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

# Built into a variable, not echoed directly, so its size can be charged
# against the same budget as the body below (codex 381ccdf4 P1: the warning is
# unbounded — up to 12 commit subjects — so capping only the body caps nothing).
warning="$(
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
  # Each subject CUT to 100 chars. This is the only unbounded input in the whole
  # script — a subject has no length limit, and this repo routinely writes
  # 250-300 character ones, so twelve of them was ~4 KB before the fixed text
  # (codex 1fd07b96 P1). Orientation needs to IDENTIFY the commits, not reproduce
  # them; the hash is right there for anything that needs reading in full.
  git log --since="${asof} 00:00:00" --format='     %cs %h %s' 2>/dev/null \
    | head -12 | cut -c 1-100
  echo
fi
)"

# Belt and braces: the per-line cut above bounds the usual case, but it counts
# CHARACTERS, so a subject of multibyte glyphs could still be 4x its length in
# bytes. The warning is the one thing that must never be dropped, so it is
# bounded here in bytes rather than trusted to stay small — otherwise a large
# warning pushes the body onto its floor and the TOTAL exceeds MAX_BYTES, which
# is the output-too-large failure this script exists to prevent.
WARN_MAX=$(( MAX_BYTES / 2 ))
if [ "$(printf '%s' "$warning" | wc -c)" -gt "$WARN_MAX" ]; then
  warning="$(printf '%s' "$warning" | head -c "$WARN_MAX" \
    | { iconv -c -f UTF-8 -t UTF-8 2>/dev/null || cat; })
   … commit list truncated; run: git log --since=${asof}"
fi
[ -n "$warning" ] && printf '%s\n' "$warning"

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
# Collected first, then truncated as a whole. The warning is already out and is
# never cut; the body absorbs whatever budget the warning left.
#
# MEASURED IN BYTES, NOT CHARACTERS (codex 381ccdf4 P1). `${#s}` and `${s:0:n}`
# count CHARACTERS under a UTF-8 locale, so a cap written that way undercounts
# every multibyte glyph — and this very file's "## Now" is full of em-dashes and
# ⚠. A section of emoji measured 6,000 "bytes" and emitted 24,843, which is the
# precise failure the cap exists to prevent, reintroduced by the fix for it.
# wc -c and head -c both count bytes, so they agree with the harness.
body="$(
  # Anchored with a boundary: a bare '^## Now' also matches '## Nowhere' and
  # '## Now archived', so a MISSING section could silently inject an unrelated
  # one and suppress the degraded-orientation warning (codex 381ccdf4 P2).
  if ! section '^## Now([[:space:]]|$)'; then
    echo "⚠  No '## Now' section in ${HANDOFF}. Orientation is DEGRADED — that"
    echo "   section is the ONLY one injected here; without it this hook shows"
    echo "   nothing about current state. Read ${HANDOFF} directly this session."
  fi
)"

warn_bytes=$(printf '%s' "$warning" | wc -c)
body_bytes=$(printf '%s' "$body" | wc -c)
# What is left after the warning, which is never truncated. Floored at 500 so a
# pathological warning cannot reduce the body to nothing without saying so.
# Reserve for the truncation notice and the trailing pointers, which are emitted
# AFTER the body: charging only the body left the total over MAX_BYTES, which is
# the one number this whole block exists to hold.
budget=$(( MAX_BYTES - warn_bytes - 500 ))
[ "$budget" -lt 500 ] && budget=500

if [ "$body_bytes" -gt "$budget" ]; then
  # head -c can slice a multibyte character in half, so the tail is re-encoded
  # to drop an incomplete trailing sequence. iconv -c discards exactly that and
  # keeps every COMPLETE character.
  #
  # An earlier attempt dropped the last LINE instead (`sed '$d'`) and was worse
  # than the problem: a section that is one long line has its entire payload on
  # that line, so the cut removed everything and the hook emitted a truncation
  # notice with nothing above it — silence, which is the original failure. Cut
  # by character, never by line.
  #
  # Falls back to the raw bytes where iconv is missing: one mangled glyph is a
  # far smaller problem than losing the orientation.
  if command -v iconv >/dev/null 2>&1; then
    printf '%s' "$body" | head -c "$budget" | iconv -c -f UTF-8 -t UTF-8 2>/dev/null
  else
    printf '%s' "$body" | head -c "$budget"
  fi
  echo
  echo
  echo "⚠  TRUNCATED at ${budget} bytes (block was ${body_bytes}). '## Now' has"
  echo "   outgrown the orientation budget — TRIM IT in ${HANDOFF} rather than"
  echo "   raising the cap, and read that file directly this session."
else
  printf '%s\n' "$body"
fi

echo
echo "  … full session log + older arcs: ${HANDOFF} · docs/session-handoff-archive.md"
echo "  … doc map (which docs are live vs historical): docs/README.md"