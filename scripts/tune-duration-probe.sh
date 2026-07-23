#!/usr/bin/env bash
#
# tune-duration-probe.sh — characterise the 2026-07-23 stuck-tune failure.
#
# HYPOTHESIS: a very short tune (~2 s) leaves the FTdx10 mid-TX-ramp, where it
# drops the unkey — while a longer tune stops cleanly. That would explain why the
# 15 s FT8 cadence never shows the fault, and why the 06:36 incident (tune ON
# 06:36:27, OFF 06:36:29) ended with the rig answering "CAT TX still on".
#
# THIS SCRIPT TRANSMITS. Dummy load, low power, and be ready to unkey at the rig.
# SM clamps tune power to 20 W and restores your operating power afterwards.
# Nets if a carrier sticks: the daemon's 15 s tune auto-off, then the rig's TOT.
#
# RUN IT ON THE BUILD THAT HAS NO RE-UNKEY RETRY (i.e. before deploying the
# 2026-07-23 bridge work). The retry would fight a stuck carrier and rescue it,
# masking the raw rig behaviour this is trying to observe.
#
# Usage:
#   scripts/tune-duration-probe.sh <seconds> [repeats]
#   scripts/tune-duration-probe.sh 5 3      # baseline: three 5-second tunes
#   scripts/tune-duration-probe.sh 2 3      # the incident length
#
# Disposable — delete once the question is settled.

set -uo pipefail

HOST="${SM_HOST:-127.0.0.1:8080}" # 127.0.0.1, NOT localhost: the daemon binds
                                  # IPv4-only, and localhost may resolve to ::1
                                  # first and hang/refuse.
LOG="${SM_LOG:-$HOME/.local/share/station-manager/log/smd.log}"
SETTLE="${SM_SETTLE:-8}" # seconds between trials, for the rig to return to RX

DURATION="${1:-}"
REPEATS="${2:-3}"

if [[ -z "$DURATION" ]]; then
    printf 'usage: %s <seconds> [repeats]\n' "$0" >&2
    exit 2
fi

tune() { # $1 = "true" | "false"
    curl -s -o /dev/null -w '%{http_code}' --max-time 5 \
        -X POST "http://${HOST}/v1/rig/tune" \
        -H 'Content-Type: application/json' \
        -d "{\"active\":${1}}"
}

# Count alarm lines so each trial can be judged by the DELTA, not by whatever
# the log already contained from earlier incidents.
alarm_count() { grep -ac 'CAT TX still keyed after unkey' "$LOG" 2>/dev/null || echo 0; }

printf '=== tune-duration probe: %ss × %s ===\n' "$DURATION" "$REPEATS"
printf 'daemon %s | log %s\n\n' "$HOST" "$LOG"

# Reachability check first — no RF, just proves the plumbing. With the rig off
# this returns 503, which is a perfectly good dry run.
probe=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://${HOST}/v1/version")
if [[ "$probe" != "200" ]]; then
    printf 'daemon not reachable at %s (got %s) — is smd running?\n' "$HOST" "$probe" >&2
    exit 1
fi

printf 'THIS WILL TRANSMIT. Dummy load connected? [y/N] '
read -r ok
[[ "$ok" == "y" || "$ok" == "Y" ]] || { printf 'aborted.\n'; exit 0; }

stuck=0
for ((i = 1; i <= REPEATS; i++)); do
    before=$(alarm_count)
    on=$(tune true)
    start=$(date +%H:%M:%S)
    sleep "$DURATION"
    off=$(tune false)
    sleep 2 # let the confirm cycle resolve (3 s timeout, answer usually in ms)
    after=$(alarm_count)

    if [[ "$after" -gt "$before" ]]; then
        printf '  trial %d/%d  %s  ON:%s OFF:%s  *** STUCK — rig reported still transmitting ***\n' \
            "$i" "$REPEATS" "$start" "$on" "$off"
        stuck=$((stuck + 1))
        printf '\n  CHECK THE RADIO. Stopping so the state can be inspected.\n'
        printf '  Recent log:\n'
        grep -aE 'tune|tx_still_keyed|TX ALARM|confirmed idle' "$LOG" | tail -6 | sed 's/^/    /'
        break
    fi

    printf '  trial %d/%d  %s  ON:%s OFF:%s  clean\n' "$i" "$REPEATS" "$start" "$on" "$off"
    [[ "$i" -lt "$REPEATS" ]] && sleep "$SETTLE"
done

printf '\nRESULT: %d of %d trials stuck at %ss.\n' "$stuck" "$i" "$DURATION"
printf 'Record this even when it is 0 — "0 of 3 at 5s, 2 of 3 at 2s" is the finding.\n'
