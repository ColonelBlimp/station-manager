#!/usr/bin/env bash
#
# One-command local-dogfood install or update: build dev RPM → check the install
# → stop daemon → reinstall RPM → enable and start daemon. Use after a code
# change to update the locally-installed copy with minimum ceremony.
#
# What runs:
#   0. Preflight — abort if :8080 is held by a foreign process (a dev
#      `task run:smd`); the dogfood + dev daemons can't run together.
#      Then, if the desktop package is installed and this deploy changes the
#      daemon's version, require the desktop RPM of that same version (see
#      "The desktop package" below) — before anything is built or stopped.
#   1. scripts/dev-rpm.sh  — SPA build → Go build → nfpm pack
#   2. rpm -U --test: try the install without installing. Whatever rpm would
#      refuse is refused here, with the daemon still running.
#   3. systemctl --user stop smd  (only if currently active)
#   4. sudo rpm -Uvh --replacepkgs <the daemon RPM> [<the desktop RPM>]
#      (rpm -U handles install / upgrade / downgrade; --replacepkgs
#       allows reinstalling the same NVR — the dev version is now
#       git-derived, so a fresh commit IS a real upgrade, but rebuilding
#       the same commit with a dirty tree keeps the same NVR and would
#       otherwise be a no-op). If this fails, a daemon that was running is
#       started again on whatever is installed.
#   5. systemctl --user daemon-reload  (cheap; catches smd.service edits)
#   6. systemctl --user enable smd, then start it. Enabling is idempotent and
#      only changes anything on a first install (or after reset:local:dev):
#      without it the daemon would not start at the next login (ruling
#      2026-09-27). Lingering — running with no login session — stays the
#      operator's explicit choice (`loginctl enable-linger`), never set here.
#   7. systemctl --user is-active smd  (final smoke)
#
# The station's daemon is stopped only once the install is known to be
# acceptable (step 2), and is never left stopped by a failed install (step 4).
# On 2026-11-12 the old order — stop, then install — met an install rpm
# refused, and left the station without its daemon.
#
# The desktop package. station-manager-desktop requires EXACTLY the daemon
# version it was built with (ADR 0102 ruling Q4), so once it is installed a
# daemon of another version can only be installed together with the desktop RPM
# of that version, in one transaction. This script does not build the desktop
# RPM (that downloads Electron and runs its own checks: `task rpm:desktop`); it
# uses build/release/station-manager-desktop.x86_64.rpm when it has this
# deploy's version, and refuses early when the pair is needed and it does not.
# Tests: scripts/deploy_local_dev_test.py.
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

RPM_PATH="build/private/station-manager-dev.x86_64.rpm"
DESKTOP_RPM_PATH="build/release/station-manager-desktop.x86_64.rpm"
DESKTOP_PKG="station-manager-desktop"

# Preflight: refuse to deploy into a running dev daemon. `task run:smd` holds
# :8080 (and the audio/serial devices); the systemd smd this script starts would
# then fail to bind and restart-flap. We stop the systemd smd ourselves below, so
# if :8080 is held while the systemd unit is NOT active, the holder is a foreign
# process — almost certainly a dev `task run:smd`. Fail fast, before the build.
step "Preflight  Check for a conflicting dev daemon on :8080"
if ss -Hltn 'sport = :8080' 2>/dev/null | grep -q .; then
    if systemctl --user is-active --quiet smd; then
        note ":8080 held by the systemd smd — it'll be stopped in step 3."
    else
        cat >&2 <<'EOF'
error: 127.0.0.1:8080 is already in use, but the systemd smd service is NOT
running — a dev daemon (task run:smd) is almost certainly holding the port
(and the audio + serial devices). The dogfood and dev daemons cannot run at the
same time. Stop the dev daemon first (Ctrl-C in its terminal, or kill the
`go run … ./cmd/smd`), then re-run this deploy.
EOF
        exit 1
    fi
else
    note ":8080 free."
fi

# Preflight: the desktop package. The version dev-rpm.sh is about to build comes
# from the same place it takes it (scripts/version.sh), so this is decided
# before the build. The dry run in step 2 is the authority; this only refuses
# sooner and says what to do.
# shellcheck source=scripts/version.sh
. scripts/version.sh
BUILD_VERSION=$(sm_rpm_version "$(sm_git_version)")
DESKTOP_INSTALLED=no
if rpm -q "$DESKTOP_PKG" >/dev/null 2>&1; then
    DESKTOP_INSTALLED=yes
fi
desktop_rpm_version() {
    if [ -f "$DESKTOP_RPM_PATH" ]; then
        rpm -qp --qf '%{VERSION}' "$DESKTOP_RPM_PATH" 2>/dev/null || true
    fi
}
if [ "$DESKTOP_INSTALLED" = yes ]; then
    step "Preflight  The desktop package is installed"
    INSTALLED_VERSION=$(rpm -q --qf '%{VERSION}' station-manager 2>/dev/null || true)
    if [ "$INSTALLED_VERSION" = "$BUILD_VERSION" ]; then
        note "This deploy rebuilds the installed version ($BUILD_VERSION); the daemon alone will do."
    else
        HAVE=$(desktop_rpm_version)
        if [ "$HAVE" != "$BUILD_VERSION" ]; then
            cat >&2 <<EOF
error: $DESKTOP_PKG is installed and requires exactly the daemon version it was
built with, so this deploy (daemon $BUILD_VERSION, replacing ${INSTALLED_VERSION:-none})
has to install the desktop RPM of the same version in the same step.
  wanted: $DESKTOP_RPM_PATH at version $BUILD_VERSION
  found:  ${HAVE:-no such file}
Build it first, then run this deploy again:
    task rpm:desktop
Nothing was built, stopped or installed. smd is untouched.
EOF
            exit 1
        fi
        note "The daemon and the desktop app will be upgraded together to $BUILD_VERSION."
    fi
fi

step "1/6  Build dev RPM"
bash scripts/dev-rpm.sh

if [ ! -f "$RPM_PATH" ]; then
    echo "error: expected RPM at $RPM_PATH after build, not found" >&2
    exit 1
fi

# The transaction: the daemon RPM, and the desktop RPM when the desktop package
# is installed and that RPM has the version just built.
RPMS=("$RPM_PATH")
if [ "$DESKTOP_INSTALLED" = yes ] && [ "$(desktop_rpm_version)" = "$(rpm -qp --qf '%{VERSION}' "$RPM_PATH")" ]; then
    RPMS+=("$DESKTOP_RPM_PATH")
fi

step "2/6  Check the install before stopping anything (rpm -U --test)"
if ! rpm -U --test --replacepkgs "${RPMS[@]}"; then
    cat >&2 <<EOF
error: rpm would refuse this install (its reason is above).
Nothing was stopped or installed. If smd was running, it is still running.
EOF
    exit 1
fi
note "rpm accepts: ${RPMS[*]}"

step "3/6  Stop daemon (if running)"
WAS_ACTIVE=no
if systemctl --user is-active --quiet smd; then
    WAS_ACTIVE=yes
    systemctl --user stop smd
    note "smd stopped."
else
    note "smd not running — skip."
fi

step "4/6  Install / reinstall RPM (sudo)"
if ! sudo rpm -Uvh --replacepkgs "${RPMS[@]}"; then
    echo "error: the install failed (see above)." >&2
    # Never leave the station without the daemon it had: start it again on
    # whatever is installed. A daemon that was not running stays that way.
    if [ "$WAS_ACTIVE" = yes ]; then
        if systemctl --user start smd; then
            echo "smd was running before this deploy and has been restarted." >&2
        else
            echo "smd was running before this deploy and could NOT be restarted:" >&2
            echo "    journalctl --user -u smd -n 50 --no-pager" >&2
        fi
    fi
    exit 1
fi

step "5/6  systemd daemon-reload (catches smd.service edits)"
systemctl --user daemon-reload

step "6/6  Enable and start daemon"
systemctl --user enable smd
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
