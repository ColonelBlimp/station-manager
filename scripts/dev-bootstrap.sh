#!/usr/bin/env bash
#
# Bring a clean Fedora workstation up to a working Station Manager (v2) dev
# environment. This is the scripted form of DEVELOPING.md §2 — read the doc for
# the *why*, run this for the *how*. Walked command-by-command against a clean
# Fedora 41 install on 2026-07-03.
#
# What runs (each step is idempotent — safe to re-run):
#   1. sudo dnf upgrade -y         — update the base system FIRST. A clean
#      Fedora ships an anaconda-base sqlite-libs that lacks sqlite3session_attach,
#      which Fedora's libnode links → node/npm break until this upgrade lands.
#   2. sudo dnf install -y ...      — git, gcc, nodejs/npm, hugo, pipewire-libs.
#      (golang is NOT installed here — Fedora's is too old; see step 3.)
#   3. upstream Go → /usr/local/go  — version pinned to go.mod's `go` directive
#      (Fedora 41's 1.24.10 is too old to build the daemon). PATH added to
#      ~/.bashrc, upstream first so it shadows the transitive Fedora golang.
#   4. go install task + nfpm       — into ~/go/bin.
#   5. sudo usermod -aG dialout     — serial (rig CAT) access.
#   6. scaffold .env if missing     — SM_WORKING_DIR runtime data dir.
#
# sudo is used for steps 1, 2, 3 (writes /usr/local), 5. It will prompt for a
# password. Everything else runs as the invoking user.
#
# NOT run here (do them by hand after — they need a fresh shell for PATH, and
# are the real toolchain smoke test):
#   source ~/.bashrc   # or open a new terminal
#   task frontend:install
#   task               # vet + build + test — confirms the whole toolchain
#
# Run directly: bash scripts/dev-bootstrap.sh

set -euo pipefail

# Resolve repo root from this script's location (scripts/ lives at the root).
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

say()  { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m warning:\033[0m %s\n' "$*"; }

# --- Sanity: this is a Fedora-targeted script -------------------------------
if [ -r /etc/os-release ] && ! grep -qi '^ID=fedora' /etc/os-release; then
  warn "This script targets Fedora. On another distro, follow DEVELOPING.md §2 by hand."
fi

# --- 1. Update the base system (fixes the node/sqlite session-symbol break) --
say "Updating base system (sudo dnf upgrade)"
sudo dnf upgrade -y

# --- 2. System packages ------------------------------------------------------
say "Installing system packages (git gcc nodejs npm hugo pipewire-libs)"
sudo dnf install -y git gcc nodejs npm hugo pipewire-libs

# --- 3. Upstream Go, pinned to go.mod ----------------------------------------
# Derive the exact version from go.mod so this never drifts from what builds.
GO_VERSION="$(awk '/^go /{print $2; exit}' go.mod)"
if [ -z "$GO_VERSION" ]; then
  echo "could not read the go version from go.mod" >&2
  exit 1
fi

if /usr/local/go/bin/go version 2>/dev/null | grep -q "go${GO_VERSION} "; then
  say "Upstream Go ${GO_VERSION} already installed at /usr/local/go — skipping"
else
  say "Installing upstream Go ${GO_VERSION} → /usr/local/go"
  tarball="go${GO_VERSION}.linux-amd64.tar.gz"
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  curl -fsSL "https://go.dev/dl/${tarball}" -o "${tmp}/${tarball}"
  # Replace any prior /usr/local/go (upstream tars extract into ./go).
  sudo rm -rf /usr/local/go
  sudo tar -C /usr/local -xzf "${tmp}/${tarball}"
fi

# Persist PATH: upstream Go first (shadows the transitive Fedora golang), then
# ~/go/bin for `go install`-ed tools (task, nfpm).
BASHRC="${HOME}/.bashrc"
if ! grep -q '/usr/local/go/bin' "$BASHRC" 2>/dev/null; then
  say "Adding Go to PATH in ${BASHRC}"
  echo 'export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH' >> "$BASHRC"
fi
# Use the same PATH for the remainder of THIS run.
export PATH="/usr/local/go/bin:${HOME}/go/bin:${PATH}"

say "Go: $(go version)"

# --- 4. Go-installed tools ---------------------------------------------------
say "Installing Task (go-task/v3) and nfpm into ~/go/bin"
go install github.com/go-task/task/v3/cmd/task@latest
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

# --- 5. Serial (rig CAT) group ----------------------------------------------
if id -nG "$USER" | tr ' ' '\n' | grep -qx dialout; then
  say "Already in the 'dialout' group — skipping"
else
  say "Adding $USER to the 'dialout' group (serial/CAT access)"
  sudo usermod -aG dialout "$USER"
  warn "Log out and back in (or 'newgrp dialout') for dialout membership to take effect."
fi

# --- 6. .env scaffold --------------------------------------------------------
if [ -f .env ]; then
  say ".env already exists — leaving it untouched"
else
  say "Creating .env with a default SM_WORKING_DIR"
  cat > .env <<EOF
# Runtime data directory (SQLite DB, config.json, logs) for task run / run:smd.
SM_WORKING_DIR=${HOME}/.local/share/station-manager
# Optional: live QRZ integration tests (task test:qrz-live) — a throwaway logbook.
# QRZ_TEST_API_KEY=
# QRZ_TEST_CALLSIGN=
EOF
fi

# --- Done --------------------------------------------------------------------
cat <<EOF

$(say "Bootstrap complete.")
Next steps (need a fresh shell so PATH picks up Go, then run from the repo root):

  source ~/.bashrc          # or open a new terminal
  task frontend:install     # install the logging SPA deps
  task                      # vet + build + test — the toolchain smoke test

If 'go version' still reports 1.24.x, your shell hasn't picked up the new PATH.
EOF
