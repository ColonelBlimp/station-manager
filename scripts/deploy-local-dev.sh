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
#      daemon's version, make sure the desktop RPM of that same version is at
#      hand, building and checking it if it is not (see "The desktop package"
#      below) — before the daemon is built or anything is stopped.
#   1. scripts/dev-rpm.sh  — SPA build → Go build → nfpm pack
#   2. rpm -U --test: rpm's own simulation of the install, without installing.
#      What it refuses (a missing dependency, a conflict, too little disk
#      space) is refused here, with the daemon still running. It is a
#      simulation: passing it does not guarantee the real install succeeds.
#   3. systemctl --user stop smd  (only if currently active)
#   4. sudo rpm -Uvh --replacepkgs <the daemon RPM> [<the desktop RPM>]
#      (rpm -U handles install / upgrade / downgrade; --replacepkgs
#       allows reinstalling the same NVR — the dev version is now
#       git-derived, so a fresh commit IS a real upgrade, but rebuilding
#       the same commit with a dirty tree keeps the same NVR and would
#       otherwise be a no-op).
#   5. systemctl --user daemon-reload  (cheap; catches smd.service edits)
#   6. systemctl --user enable smd, then start it. Enabling is idempotent and
#      only changes anything on a first install (or after reset:local:dev):
#      without it the daemon would not start at the next login (ruling
#      2026-09-27). Lingering — running with no login session — stays the
#      operator's explicit choice (`loginctl enable-linger`), never set here.
#   7. systemctl --user is-active smd  (final smoke)
#
# The station's daemon. It is stopped only once rpm's simulation has accepted
# the install (step 2). From the stop until it has been started again, however
# this script ends — a failed install, Ctrl+C at the sudo prompt, a termination —
# it tries to start a daemon that was running before (restore_daemon below); one
# that was not running is left alone. That is an attempt to get the station back,
# not a package rollback: it starts whatever is installed, and it says so if the
# start fails. A kill -9 of this script cannot be caught.
# On 2026-11-12 the old order — stop, then install — met an install rpm refused,
# and left the station without its daemon.
#
# The desktop package. station-manager-desktop requires EXACTLY the daemon
# version it was built with (ADR 0102 ruling Q4; it holds for development builds
# too, operator 2026-11-13), so once it is installed a daemon of another version
# can only be installed together with the desktop RPM of that version, in one
# transaction. When that pair is needed and
# build/release/station-manager-desktop.x86_64.rpm does not have this deploy's
# version, the script builds it (operator ruling 2026-11-13): the build
# (scripts/desktop-rpm.sh, which downloads Electron), then the content check and
# the production smoke check of the artifact. Those are the steps of
# `task rpm:desktop` that examine the RPM; the tests of those checks run in CI.
# A desktop RPM that already has the version is not rebuilt, but it is checked
# like a new one: only an RPM this run has checked goes into the transaction.
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

# Recovery ownership (codex-100). RESTORE is yes from just before the stop of a
# running daemon until that daemon has been started again. Whatever ends the
# script in between runs restore_daemon: a command that fails (set -e), an
# explicit exit, or a signal — the INT/TERM/HUP traps turn each into an exit, so
# the EXIT trap is the one place recovery happens. Bash runs a trap only after
# the foreground command has ended, so the package command is never still
# replacing files when the daemon is started.
RESTORE=no
restore_daemon() {
    local status=$?
    trap - EXIT INT TERM HUP
    if [ "$RESTORE" = yes ]; then
        RESTORE=no
        echo "error: this deploy ended before it had started the daemon again." >&2
        if systemctl --user start smd; then
            echo "smd was running before this deploy and has been restarted, on whatever is installed." >&2
        else
            echo "smd was running before this deploy and could NOT be restarted:" >&2
            echo "    journalctl --user -u smd -n 50 --no-pager" >&2
        fi
    fi
    exit "$status"
}
trap restore_daemon EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

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
# before the daemon is built. rpm's simulation in step 2 remains the check of
# the transaction; this makes sure the pair it needs exists.
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
# WITH_DESKTOP is yes only once THIS run has checked the desktop RPM it will
# install. Having the right version is not having passed the checks: the build
# writes the file before it is checked, so a failed check leaves a file of the
# right version behind for the next run to find (codex-101).
WITH_DESKTOP=no
if [ "$DESKTOP_INSTALLED" = yes ]; then
    step "Preflight  The desktop package is installed"
    INSTALLED_VERSION=$(rpm -q --qf '%{VERSION}' station-manager 2>/dev/null || true)
    if [ "$INSTALLED_VERSION" = "$BUILD_VERSION" ]; then
        note "This deploy rebuilds the installed version ($BUILD_VERSION); the daemon alone will do."
    else
        note "$DESKTOP_PKG requires exactly the daemon version it was built with, so this"
        note "deploy (daemon $BUILD_VERSION, replacing ${INSTALLED_VERSION:-none}) installs both together."
        desktop_failed() {
            cat >&2 <<EOF
error: $1
The desktop RPM of version $BUILD_VERSION is needed to upgrade the daemon while
$DESKTOP_PKG is installed, and it could not be made ready.
Nothing was stopped or installed. If smd was running, it is still running.
EOF
            exit 1
        }
        HAVE=$(desktop_rpm_version)
        if [ "$HAVE" != "$BUILD_VERSION" ]; then
            note "The desktop RPM on disk is ${HAVE:-missing}; building $BUILD_VERSION now."
            step "Preflight  Build the desktop RPM (downloads Electron)"
            bash scripts/desktop-rpm.sh || desktop_failed "the desktop RPM build failed (see above; it needs the Internet)."
            HAVE=$(desktop_rpm_version)
            if [ "$HAVE" != "$BUILD_VERSION" ]; then
                desktop_failed "the desktop RPM just built is version ${HAVE:-unknown}, not $BUILD_VERSION."
            fi
        else
            note "The desktop RPM on disk already has version $BUILD_VERSION; it is not rebuilt."
        fi
        # Just built or found on disk, it is checked by this run before it is used.
        step "Preflight  Check the desktop RPM (contents, then the production smoke check)"
        python3 -I scripts/desktop-rpm-check.py "$DESKTOP_RPM_PATH" \
            --version "$BUILD_VERSION" --app-version "$(sm_git_version)" \
            || desktop_failed "the desktop RPM failed its content check (see above)."
        python3 -I scripts/desktop-smoke.py "$DESKTOP_RPM_PATH" \
            || desktop_failed "the desktop RPM failed its production smoke check (see above)."
        WITH_DESKTOP=yes
        note "The daemon and the desktop app will be upgraded together to $BUILD_VERSION."
    fi
fi

step "1/6  Build dev RPM"
bash scripts/dev-rpm.sh

if [ ! -f "$RPM_PATH" ]; then
    echo "error: expected RPM at $RPM_PATH after build, not found" >&2
    exit 1
fi

# The transaction: the daemon RPM, and the desktop RPM this run checked above.
# No other desktop RPM is ever installed from here, whatever is on disk.
RPMS=("$RPM_PATH")
if [ "$WITH_DESKTOP" = yes ]; then
    RPMS+=("$DESKTOP_RPM_PATH")
fi

step "2/6  Simulate the install before stopping anything (rpm -U --test)"
if ! rpm -U --test --replacepkgs "${RPMS[@]}"; then
    cat >&2 <<EOF
error: rpm's simulation refuses this install (its reason is above).
Nothing was stopped or installed. If smd was running, it is still running.
EOF
    exit 1
fi
note "rpm's simulation accepts: ${RPMS[*]}"

step "3/6  Stop daemon (if running)"
if systemctl --user is-active --quiet smd; then
    # From here until the start in step 6, restore_daemon owns getting it back.
    RESTORE=yes
    systemctl --user stop smd
    note "smd stopped."
else
    note "smd not running — skip."
fi

step "4/6  Install / reinstall RPM (sudo)"
if ! sudo rpm -Uvh --replacepkgs "${RPMS[@]}"; then
    echo "error: the install failed (see above)." >&2
    exit 1 # restore_daemon starts a daemon that was running
fi

step "5/6  systemd daemon-reload (catches smd.service edits)"
systemctl --user daemon-reload

step "6/6  Enable and start daemon"
systemctl --user enable smd
systemctl --user start smd
RESTORE=no

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
