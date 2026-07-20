#!/usr/bin/env bash
#
# smctl — friendly control wrapper for the user-level smd systemd unit.
# Installed to /usr/bin/smctl by the RPM. Use in place of
# `systemctl --user start|stop smd` to get a confirmed result line:
# the "SM Started." / "SM Stopped." message only prints once the daemon
# state is verified, not the instant systemd accepts the request.
#
# Why the is-active gate matters: the smd unit is Type=simple with
# Restart=on-failure, so `systemctl --user start smd` returns 0 as soon as
# the process is spawned — even if it crashes a moment later. Checking
# is-active after a short settle is what makes the success message honest.
#
# Usage:
#   smctl start|stop|restart|status
#   smctl import [smd-import-flags] <file.adi>
#
# `smctl import` wraps the single-writer dance: smd uses an exclusive
# SQLite handle, so the daemon must be down while `smd import` runs. This
# stops smd (if running), runs the import, and restarts smd only if it was
# running before — so the operator runs one command and never sees a
# database-locked error. Import uploads NOTHING by default; pass
# `--forward <name>` to queue the imported QSOs to a forwarder.

set -euo pipefail

UNIT="smd"

# smd lives next to smctl (both installed to /usr/bin by the RPM); fall back
# to PATH for a dev checkout.
SMD_BIN="$(dirname "$(readlink -f "$0")")/smd"
[ -x "$SMD_BIN" ] || SMD_BIN="$(command -v smd || true)"

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
    printf '%sSee:  journalctl --user -u %s -n 50 --no-pager%s\n' "$DIM" "$UNIT" "$RESET" >&2
}

# unit_state echoes systemd's raw active-state (active|activating|inactive|
# failed|deactivating|reloading) and always returns 0, so `set -e` and command
# substitution are safe. `systemctl is-active` prints the state even when it
# exits non-zero.
unit_state() { systemctl --user is-active "$UNIT" 2>/dev/null || true; }

# is_stopped is true only for a genuinely-down unit. A crash-looping unit sits
# in activating(auto-restart) between attempts — where `systemctl is-active`
# exits NON-ZERO yet the process keeps coming back — so a naive `! is-active`
# gate would wrongly call it stopped and skip the actual stop, leaving the loop
# running (review 2026-07-20; ported from smcloudctl #1).
is_stopped() {
    case "$(unit_state)" in
        active|activating|reloading|deactivating) return 1 ;;
        *) return 0 ;; # inactive | failed
    esac
}

# settleSecs is how long start/restart watch the unit before declaring success.
# The unit is Type=simple, so it reports "active" the instant the process forks
# — BEFORE smd opens its SQLite DB, binds the HTTP port, and starts its
# subsystems, any of which can fail (locked DB, port in use, bad config) and
# crash-loop it via Restart=on-failure/RestartSec=5s (review 2026-07-20; ported
# from smcloudctl #2). 8s comfortably exceeds one restart cycle, so a loop is
# caught. Residual: a failure LATER than the window isn't caught — a health
# probe would be the true readiness signal; this is the systemd-only best
# effort.
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
        note "SM is already running."
        return 0
    fi
    systemctl --user start "$UNIT"
    if stays_active; then
        ok "SM Started."
    else
        fail "SM failed to start (crashed within ${settleSecs}s — locked DB, port in use, or bad config)."
        return 1
    fi
}

cmd_stop() {
    if is_stopped; then
        note "SM is not running."
        return 0
    fi
    # Always issue stop — including from activating(auto-restart), which a gate
    # on is-active would skip, leaving the crash loop running (#1). systemctl
    # stop cancels the pending auto-restart.
    systemctl --user stop "$UNIT"
    if is_stopped; then
        ok "SM Stopped."
    else
        fail "SM did not stop."
        return 1
    fi
}

cmd_restart() {
    systemctl --user restart "$UNIT"
    if stays_active; then
        ok "SM Restarted."
    else
        fail "SM failed to restart (crashed within ${settleSecs}s — locked DB, port in use, or bad config)."
        return 1
    fi
}

cmd_import() {
    if [ -z "$SMD_BIN" ] || [ ! -x "$SMD_BIN" ]; then
        fail "smd binary not found (looked next to smctl and on PATH)."
        return 1
    fi
    if [ "$#" -eq 0 ]; then
        echo "usage: $0 import [--forward <name>] <file.adi>" >&2
        echo "  $0 import mylog.adi                  # seed the logbook, upload nothing" >&2
        echo "  $0 import --forward qrz mylog.adi    # also queue an upload to the 'qrz' forwarder" >&2
        return 2
    fi

    # Free the database: smd holds an exclusive SQLite handle.
    local was_active=0
    if [ "$(unit_state)" = active ]; then
        was_active=1
        note "Stopping SM for import (exclusive database access)…"
        cmd_stop
    fi

    # Run the import; capture its exit so we always restart afterwards.
    local rc=0
    "$SMD_BIN" import "$@" || rc=$?

    if [ "$was_active" -eq 1 ]; then
        note "Restarting SM…"
        cmd_start
    fi

    if [ "$rc" -ne 0 ]; then
        fail "Import failed (exit $rc)."
    fi
    return "$rc"
}

cmd_status() {
    local state
    state="$(unit_state)"
    if [ "$state" = active ]; then
        ok "SM is running."
    else
        note "SM is not running (${state})."
    fi
    systemctl --user --no-pager status "$UNIT" || true # human detail; its exit is display-only
    # Honest exit code for monitoring/scripts (review 2026-07-20 #3): a bare
    # `systemctl status || true` always exits 0, reporting a dead service as
    # healthy. Reflect the real running state instead.
    [ "$state" = active ]
}

case "${1:-}" in
    start)   cmd_start ;;
    stop)    cmd_stop ;;
    restart) cmd_restart ;;
    status)  cmd_status ;;
    import)  shift; cmd_import "$@" ;;
    *)
        echo "usage: $0 {start|stop|restart|status|import}" >&2
        exit 2
        ;;
esac
