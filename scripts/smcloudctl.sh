#!/usr/bin/env bash
#
# smcloudctl — friendly control wrapper for the SYSTEM-level smcloud unit.
# Installed to /usr/bin/smcloudctl by the smcloud RPM. The smd counterpart is
# smctl (scripts/smctl.sh); this mirrors it, with three differences that come
# from smcloud being a system service, not a user one:
#   - it drives `systemctl` (system bus), so start/stop/restart/enable/disable
#     need root — the wrapper prefixes sudo automatically when not already
#     root; status/is-active are unprivileged reads and run as-is;
#   - there is no `import` subcommand — smcloud keeps no local database (all
#     state is in Postgres), so there is no single-writer dance to wrap;
#   - `enable`/`disable` control BOOT autostart. On-crash restart is already
#     built into the unit (Restart=on-failure, RestartSec=5), so there is
#     nothing to toggle for that — a crashed smcloud comes back on its own.
#
# Why the is-active gate matters (same as smctl): the unit is Type=simple with
# Restart=on-failure, so `systemctl start smcloud` returns 0 the instant the
# process is spawned — even if a bad token/DSN makes it exit a moment later.
# Checking is-active after a short settle is what makes the success line honest.
# NB smcloud's fail-loud boot validation exits non-zero, which on-failure then
# restart-LOOPS every 5 s — so a red "failed to start" here means "check the
# env file", and `journalctl` shows the exact variable at fault.
#
# Usage:
#   smcloudctl start|stop|restart|status
#   smcloudctl enable|disable          # start-at-boot on/off (autostart)

set -euo pipefail

UNIT="smcloud"

if [ -t 1 ]; then
    BOLD=$'\033[1m'
    DIM=$'\033[2m'
    RED=$'\033[31m'
    GREEN=$'\033[32m'
    RESET=$'\033[0m'
else
    BOLD=""; DIM=""; RED=""; GREEN=""; RESET=""
fi

ok()   { printf '%s%s%s\n' "$GREEN$BOLD" "$1" "$RESET"; }
note() { printf '%s%s%s\n' "$DIM" "$1" "$RESET"; }
fail() {
    printf '%s%s%s\n' "$RED$BOLD" "$1" "$RESET" >&2
    printf '%sSee:  journalctl -u %s -n 50 --no-pager%s\n' "$DIM" "$UNIT" "$RESET" >&2
}

# sc runs a systemctl subcommand, elevating with sudo only when the caller
# isn't already root (start/stop/restart/enable/disable are privileged).
sc() {
    if [ "$(id -u)" -eq 0 ]; then
        systemctl "$@"
    else
        sudo systemctl "$@"
    fi
}

is_enabled() { systemctl is-enabled --quiet "$UNIT" 2>/dev/null; }

# unit_state echoes systemd's raw active-state (active|activating|inactive|
# failed|deactivating|reloading) and always returns 0, so `set -e` and command
# substitution are safe. `systemctl is-active` prints the state even when it
# exits non-zero.
unit_state() { systemctl is-active "$UNIT" 2>/dev/null || true; }

# is_stopped is true only for a genuinely-down unit. A crash-looping unit sits
# in activating(auto-restart) between attempts — where `systemctl is-active`
# exits NON-ZERO yet the process keeps coming back — so a naive `! is-active`
# gate would wrongly call it stopped and skip the actual stop, leaving the loop
# running (review 2026-07-20 #1).
is_stopped() {
    case "$(unit_state)" in
        active|activating|reloading|deactivating) return 1 ;;
        *) return 0 ;; # inactive | failed
    esac
}

# settleSecs is how long start/restart watch the unit before declaring success.
# It MUST exceed smcloud's own start-up work: the unit is Type=simple, so it
# reports "active" the instant the process forks — BEFORE the Postgres ping
# (5 s timeout), migrations, and tenant provisioning run (review 2026-07-20 #2).
# A DB-unreachable or bad-config boot flips the unit to failed/auto-restart
# around the 5 s mark, so we watch that it STAYS active across this window
# rather than trust a one-second snapshot. Residual: a failure LATER than the
# window isn't caught — the /v1/health endpoint is the true readiness signal;
# this is the systemd-only best effort.
settleSecs=8

# stays_active confirms the unit reaches AND HOLDS "active" for settleSecs,
# bailing the moment it drops out (crash / auto-restart). Returns 0 = healthy.
stays_active() {
    local i
    for (( i = 0; i < settleSecs; i++ )); do
        sleep 1
        [ "$(unit_state)" = active ] || return 1
    done
    [ "$(unit_state)" = active ]
}

cmd_start() {
    if [ "$(unit_state)" = active ]; then
        note "smcloud is already running."
        return 0
    fi
    sc start "$UNIT"
    if stays_active; then
        ok "smcloud Started."
    else
        fail "smcloud failed to start (crashed or DB unreachable within ${settleSecs}s)."
        return 1
    fi
}

cmd_stop() {
    if is_stopped; then
        note "smcloud is not running."
        return 0
    fi
    # Always issue stop — including from activating(auto-restart), which a gate
    # on is-active would skip, leaving the crash loop running (#1). systemctl
    # stop cancels the pending auto-restart.
    sc stop "$UNIT"
    if is_stopped; then
        ok "smcloud Stopped."
    else
        fail "smcloud did not stop."
        return 1
    fi
}

cmd_restart() {
    # A real restart (not stop+start) so it also swaps the binary after an RPM
    # upgrade — the trap that a bare `systemctl enable --now` does NOT cover.
    sc restart "$UNIT"
    if stays_active; then
        ok "smcloud Restarted."
    else
        fail "smcloud failed to restart (crashed or DB unreachable within ${settleSecs}s)."
        return 1
    fi
}

cmd_enable() {
    sc enable "$UNIT"
    ok "smcloud autostart ENABLED (starts on boot)."
    if [ "$(unit_state)" != active ]; then
        note "Not running now — 'smcloudctl start' to start it immediately."
    fi
}

cmd_disable() {
    sc disable "$UNIT"
    ok "smcloud autostart DISABLED (won't start on boot)."
    if [ "$(unit_state)" = active ]; then
        note "Still running now — 'smcloudctl stop' to stop the current process."
    fi
}

cmd_status() {
    local state
    state="$(unit_state)"
    if [ "$state" = active ]; then
        ok "smcloud is running."
    else
        note "smcloud is not running (${state})."
    fi
    if is_enabled; then
        note "autostart: enabled (starts on boot)"
    else
        note "autostart: disabled"
    fi
    systemctl --no-pager status "$UNIT" || true # human detail; its exit is display-only
    # Honest exit code for monitoring/scripts (review 2026-07-20 #3): a bare
    # `systemctl status || true` always exits 0, reporting a dead service as
    # healthy. Reflect the real running state instead.
    [ "$state" = active ]
}

case "${1:-}" in
    start)   cmd_start ;;
    stop)    cmd_stop ;;
    restart) cmd_restart ;;
    enable)  cmd_enable ;;
    disable) cmd_disable ;;
    status)  cmd_status ;;
    *)
        echo "usage: $0 {start|stop|restart|status|enable|disable}" >&2
        exit 2
        ;;
esac
