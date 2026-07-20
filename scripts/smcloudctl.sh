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

is_active()  { systemctl is-active  --quiet "$UNIT"; }
is_enabled() { systemctl is-enabled --quiet "$UNIT" 2>/dev/null; }

cmd_start() {
    if is_active; then
        note "smcloud is already running."
        return 0
    fi
    sc start "$UNIT"
    # Let RestartSec / an immediate boot-validation crash settle first.
    sleep 1
    if is_active; then
        ok "smcloud Started."
    else
        fail "smcloud failed to start."
        return 1
    fi
}

cmd_stop() {
    if ! is_active; then
        note "smcloud is not running."
        return 0
    fi
    sc stop "$UNIT"
    if ! is_active; then
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
    sleep 1
    if is_active; then
        ok "smcloud Restarted."
    else
        fail "smcloud failed to restart."
        return 1
    fi
}

cmd_enable() {
    sc enable "$UNIT"
    ok "smcloud autostart ENABLED (starts on boot)."
    if ! is_active; then
        note "Not running now — 'smcloudctl start' to start it immediately."
    fi
}

cmd_disable() {
    sc disable "$UNIT"
    ok "smcloud autostart DISABLED (won't start on boot)."
    if is_active; then
        note "Still running now — 'smcloudctl stop' to stop the current process."
    fi
}

cmd_status() {
    if is_active; then
        ok "smcloud is running."
    else
        note "smcloud is not running."
    fi
    if is_enabled; then
        note "autostart: enabled (starts on boot)"
    else
        note "autostart: disabled"
    fi
    systemctl --no-pager status "$UNIT" || true
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
