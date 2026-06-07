#!/usr/bin/env bash
#
# One-command local-dogfood update: build dev RPM → stop daemon →
# reinstall RPM → start daemon. Use after a code change to update the
# locally-installed copy with minimum ceremony.
#
# What runs:
#   1. scripts/dev-rpm.sh  — SPA build → Go build → nfpm pack
#   2. systemctl --user stop smd  (only if currently active)
#   3. sudo rpm -Uvh --replacepkgs build/release/station-manager-dev.x86_64.rpm
#      (rpm -U handles install / upgrade / downgrade; --replacepkgs
#       allows reinstalling the same NVR — the dev version is now
#       git-derived, so a fresh commit IS a real upgrade, but rebuilding
#       the same commit with a dirty tree keeps the same NVR and would
#       otherwise be a no-op).
#   4. systemctl --user daemon-reload  (cheap; catches smd.service edits)
#   5. systemctl --user start smd
#   6. systemctl --user is-active smd  (final smoke)
#
# The systemd unit is user-level (per nfpm.yaml: smd.service installs
# to /usr/lib/systemd/user/), so no sudo for systemctl — sudo only
# needed for the package install. Will prompt for sudo password on
# first invocation per session.
#
# Run via `task deploy:local:dev` or directly: `bash scripts/deploy-local-dev.sh`.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Default the dogfood deploy to the PocketFFT (CGO) backend so live FT8 capture
# + decode work out of the box — the pure-Go static default leaves the FT8
# subsystem idle ("capture unavailable") because audio capture needs CGO.
# Exported so the child `bash scripts/dev-rpm.sh` (run below) inherits it.
# Override for the static CGO-free build with a non-"pocketfft" value, e.g.
#   SM_FFT=gonum task deploy:local:dev
# `task rpm:dev` calls dev-rpm.sh directly (not via this script) and is
# unaffected — it stays CGO-free by default.
export SM_FFT="${SM_FFT:-pocketfft}"

if [ -t 1 ]; then
    BOLD=$'\033[1m'
    DIM=$'\033[2m'
    RESET=$'\033[0m'
else
    BOLD=""
    DIM=""
    RESET=""
fi

step() {
    printf '\n%s── %s ──%s\n' "$BOLD" "$1" "$RESET"
}

note() {
    printf '%s%s%s\n' "$DIM" "$1" "$RESET"
}

RPM_PATH="build/release/station-manager-dev.x86_64.rpm"

step "1/5  Build dev RPM"
bash scripts/dev-rpm.sh

if [ ! -f "$RPM_PATH" ]; then
    echo "error: expected RPM at $RPM_PATH after build, not found" >&2
    exit 1
fi

step "2/5  Stop daemon (if running)"
if systemctl --user is-active --quiet smd; then
    systemctl --user stop smd
    note "smd stopped."
else
    note "smd not running — skip."
fi

step "3/5  Install / reinstall RPM (sudo)"
sudo rpm -Uvh --replacepkgs "$RPM_PATH"

step "4/5  systemd daemon-reload (catches smd.service edits)"
systemctl --user daemon-reload

step "5/5  Start daemon"
systemctl --user start smd

# Brief wait so the post-start active check isn't racing the unit's
# transition from "activating" to "active." Five seconds matches
# RestartSec in the unit file — anything that fails to come up in
# that window has a real problem the operator will want to see.
sleep 1

if systemctl --user is-active --quiet smd; then
    printf '\n%ssmd is active.%s  Journal: journalctl --user -u smd -f\n' "$BOLD" "$RESET"
else
    printf '\n%ssmd failed to start.%s  See:  journalctl --user -u smd -n 50 --no-pager\n' "$BOLD" "$RESET" >&2
    exit 1
fi
