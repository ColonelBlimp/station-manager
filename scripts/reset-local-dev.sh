#!/usr/bin/env bash
#
# Completely uninstall the local dogfood install — leave the system "as if
# Station Manager had never been installed":
#   1. disable + stop the smd user service (systemctl --user disable --now)
#   2. disable user lingering (loginctl disable-linger) so nothing starts at boot
#   3. remove the package (sudo dnf remove station-manager) — drops the smd
#      binary AND the systemd user unit file (/usr/lib/systemd/user/smd.service)
#   4. systemctl --user daemon-reload — clear systemd's in-memory view of the
#      now-removed unit. This is the ONE case where the reload is needed: a unit
#      FILE was removed (by the package removal), not just a service stopped.
#   5. DELETE the working directory ($HOME/.local/share/station-manager):
#      config.json, the QSO database, and logs — runtime data the RPM doesn't own,
#      so rpm -e leaves it behind.
#
# DESTRUCTIVE + needs sudo (for the package removal). Removing the working dir
# drops the dogfood QSO database irreversibly. Prompts for confirmation unless
# SM_RESET_YES=1. Re-install later with `task deploy:local:dev`.
#
# Run via `task reset:local:dev` or directly: `bash scripts/reset-local-dev.sh`.

set -euo pipefail

UNIT="smd"
PKG="station-manager"
# The systemd unit pins SM_WORKING_DIR=%h/.local/share/station-manager
# (packaging/smd.service), so the dogfood working dir is always this path.
DATA_DIR="$HOME/.local/share/station-manager"

if [ -t 1 ]; then
    BOLD=$'\033[1m'
    RESET=$'\033[0m'
else
    BOLD=""
    RESET=""
fi

printf '%sThis will UNINSTALL Station Manager (leave the system as if never installed):%s\n' "$BOLD" "$RESET"
echo "  • disable + stop the smd user service (systemctl --user disable --now)"
echo "  • disable user lingering (loginctl disable-linger)"
echo "  • remove the ${PKG} package (sudo dnf remove) — smd binary + systemd unit"
echo "  • DELETE ${DATA_DIR}"
echo "    (config.json, QSO database, logs) — IRREVERSIBLE"
echo

if [ "${SM_RESET_YES:-}" != "1" ]; then
    read -r -p "Proceed? Type 'yes' to confirm: " reply
    if [ "$reply" != "yes" ]; then
        echo "Aborted — nothing changed."
        exit 0
    fi
fi

# 1. Stop + disable while the unit still exists. Tolerate "not enabled" /
#    "not running" / "no such unit".
systemctl --user disable --now "$UNIT" 2>/dev/null || true
echo "stopped + disabled ${UNIT}."

# 2. Disable user lingering (counterpart to the enable-linger that made the
#    dogfood autostart at boot). May need polkit/sudo auth; tolerate failure.
loginctl disable-linger "$USER" 2>/dev/null || true
echo "disabled user lingering for ${USER}."

# 3. Remove the package — drops the binary + the systemd unit file. Needs sudo.
#    `dnf remove` is the Fedora-canonical removal (the dev RPM went in via
#    `rpm -Uvh` but dnf reads the same rpmdb). -y because the script already
#    confirmed above — no second prompt. Skip cleanly if it isn't installed.
if rpm -q --quiet "$PKG"; then
    sudo dnf remove -y "$PKG"
    echo "removed package ${PKG}."
else
    echo "${PKG} package not installed — skip."
fi

# 4. daemon-reload — appropriate HERE: the package removal dropped the unit file,
#    so refresh systemd's view to clear the orphaned unit (a plain disable/stop
#    wouldn't need this — only a unit-file add/remove does).
systemctl --user daemon-reload || true

# 5. Remove the working dir (runtime data; not owned by the RPM).
if [ -d "$DATA_DIR" ]; then
    rm -rf "$DATA_DIR"
    echo "removed ${DATA_DIR}."
else
    echo "${DATA_DIR} not present — skip."
fi

echo
printf '%sDone — Station Manager fully removed.%s Re-install with: task deploy:local:dev\n' "$BOLD" "$RESET"
