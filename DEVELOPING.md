# Developing Station Manager (v2)

This is the developer setup for the v2 daemon (`main` branch). It covers a
**fresh Linux workstation** — what to install, how to build, run, and test.

The instructions are written for **Fedora** (the dogfood/development host),
with the equivalent package names noted for Debian/Ubuntu where they differ.
For project conventions, invariants, and the documentation map, read
[`CLAUDE.md`](CLAUDE.md) and [`docs/README.md`](docs/README.md) first.

> Developing v1 instead? v1 (the last released Wails app, still used for
> day-to-day operating) lives on its own branch: `git checkout v1 && cat
> DEVELOPING.md`.

---

## 1. The toolchain at a glance

| Tool | Version | Why it's needed |
|------|---------|-----------------|
| **Go** | 1.26.2 (see `go.mod`) | The daemon and all `cmd/...` tools. |
| **gcc** + C toolchain | any recent | CGO builds — live FT8 audio capture (miniaudio) and the optional PocketFFT FFT backend. The default dev build (`task build`) is **CGO-on**. |
| **Node.js + npm** | Node ≥ 22 | The three Svelte SPAs (`frontend/logging`, `frontend/config`, `frontend/logbook`). |
| **Hugo (extended)** | 0.162.1 | The operator manual (`manual/`), embedded into the daemon via `go:embed`. The daemon build compiles against it, so it must be built first. |
| **Task** (go-task) | v3 | The task runner — every build/run/test entrypoint is a `task` target (`Taskfile.yml`). |
| **nfpm** | latest | Builds the dev/release RPMs without `rpmbuild`. Only needed for `task rpm:dev` / `deploy:local:dev`. |
| **podman** | any | Release-only — the shipped RPM is built in an AlmaLinux 8 container (`scripts/release.sh`). Not needed for day-to-day dev. |
| **actionlint** | latest | Lints `.github/workflows/*.yml` (`task ci:lint`). Optional. |

---

## 2. Fedora: install everything

### System packages (dnf)

```bash
sudo dnf install -y \
  git \
  gcc \
  golang \
  nodejs npm \
  hugo \
  alsa-lib pipewire-libs
```

Notes:

- **`gcc`** is required even though most of the code is pure Go — the default
  dev build enables CGO for live FT8 audio capture. Without it,
  `task build` fails with `cgo: C compiler "gcc" not found`. (You can still
  build the CGO-free static shape with `task build:smd:static`.)
- **`golang`** from Fedora's repos tracks recent Go closely. If `go version`
  is older than the `go.mod` line (`go 1.26.2`), install the upstream
  toolchain instead — see §2.1.
- **`hugo`** in Fedora's repos is the *extended* build, which is what's
  needed. If it lags behind 0.162.1 and a template breaks, grab the pinned
  release the way CI does:
  ```bash
  HUGO_VERSION=0.162.1
  curl -fsSL "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-amd64.tar.gz" \
    | sudo tar -xz -C /usr/local/bin hugo
  ```
- **`alsa-lib` / `pipewire-libs`** are *runtime* audio backends. miniaudio
  `dlopen`s an audio backend at run time for live FT8 capture; on Fedora 43
  that's PipeWire (with the ALSA compat layer). They're almost always already
  installed as part of the desktop — listed here for a truly minimal install.
  No `-devel` headers are needed (miniaudio bundles its own C).

### 2.1 Go toolchain (if Fedora's is too old)

```bash
GO_VERSION=1.26.2
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
  | sudo tar -xz -C /usr/local
# Add to your shell profile (~/.bashrc):
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH
```

`$HOME/go/bin` on PATH is also where `go install`-ed tools (Task, nfpm,
actionlint) land — make sure it's there.

### 2.2 Go-installed tools

```bash
# Task runner — the primary entrypoint for everything below.
go install github.com/go-task/task/v3/cmd/task@latest

# Only if you'll build RPMs (task rpm:dev / deploy:local:dev):
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

# Optional — lints the GitHub Actions workflow (task ci:lint):
go install github.com/rhysd/actionlint/cmd/actionlint@latest
```

> Task is also packaged as `go-task` in some distros, but `go install` is the
> version-agnostic route and guarantees v3.

### 2.3 Serial port access (rig CAT control)

Rig CAT control talks to a USB serial device (`/dev/ttyUSB*` or
`/dev/ttyACM*`), which is owned by the **`dialout`** group. Add yourself once:

```bash
sudo usermod -aG dialout $USER
# Log out and back in (or `newgrp dialout`) for it to take effect.
```

Without this, the daemon can't open the serial port and the bridge/CAT
subsystem stays disconnected.

### Debian/Ubuntu equivalents

`gcc` → `build-essential`; `nodejs npm` (use NodeSource for Node ≥ 22);
`hugo` is often too old — use the pinned-release `curl` above; audio runtime
is `libasound2` / `libpipewire-0.3-0`; serial group is also `dialout`.

---

## 3. Project setup

```bash
git clone https://github.com/ColonelBlimp/station-manager.git
cd station-manager

# Install SPA dependencies (logging is the main one; config + logbook
# have their own install tasks if you work on them).
task frontend:install

# Sanity check the full toolchain — vet + build + test, the CI-equivalent
# local smoke check.
task
```

### The `.env` file

The Taskfile loads `.env` from the repo root (`dotenv: ['.env']`). Create one:

```bash
# .env  (gitignored — never commit it)
SM_WORKING_DIR=/home/you/.local/share/station-manager
```

`SM_WORKING_DIR` is the runtime data directory (SQLite DB, `config.json`,
logs) used by `task run` / `task run:smd`. For the live-QRZ integration tests
you'd also add `QRZ_TEST_API_KEY` and `QRZ_TEST_CALLSIGN` (a throwaway test
logbook), but those are optional.

---

## 4. Everyday workflow

All entrypoints are `task` targets — run `task --list` for the full set. The
ones you'll use most:

### Build

```bash
task build          # CGO-on dev build of the whole module + build/bin/smd (live FT8)
task build:smd      # daemon with freshly embedded SPAs + manual (CGO, gonum FFT)
task build:smd:pocketfft   # ~2x faster FT8 decode (CGO PocketFFT) — needs gcc
task build:smd:static      # CGO-free static build — no FT8 capture (release shape)
```

### Run

```bash
task run            # build + run the daemon on :8080 (stops the systemd smd first)
```

For SPA development with hot-reload, run two terminals:

```bash
task run:smd        # terminal 1: daemon via `go run` (CGO-on, live FT8)
task frontend:dev   # terminal 2: Vite on :5173, proxies /v1/* to the daemon
```

Both `run` targets automatically stop a running user-level `smd` systemd unit
first so they don't fight over `:8080` and the audio/serial ports. Restart it
afterwards with `systemctl --user start smd`.

### Test

```bash
task test           # go test -race -short ./...
task ci:local       # full local mirror of the CI gate (SPA lint/check/test/build
                    # + Go gofmt/vet/race-test + daemon embed-build)
task ci             # actionlint + ci:local — run before pushing
```

### Dogfood deploy (install over your running copy)

```bash
task deploy:local:dev    # build dev RPM → stop smd → rpm -Uvh → reload → start
```

One command to refresh the dogfooded systemd install after a code change.
Defaults to the PocketFFT (CGO) backend for live FT8; prompts for sudo once
for the package install. Override the backend with `SM_FFT=gonum task
deploy:local:dev` for the static build.

---

## 5. CGO, FFT backends, and FT8 in one paragraph

The default dev builds (`task build`, `run`, `run:smd`) are **CGO-on** so live
FT8 audio capture works without a full deploy. There are two FFT backends:
**gonum** (pure Go, the CGO-on default) and **PocketFFT** (`SM_FFT=pocketfft`
or the `:pocketfft` task variants — CGO C code, ~2× faster decode, dynamically
linked). The **CGO-free static build** (`build:smd:static`, `CGO_ENABLED=0`)
drops audio capture entirely — the FT8 subsystem logs "capture unavailable;
subsystem idle" — and is the shipped-release shape plus the right build for a
headless/aggregator node. If you only touch logging/forwarding/bridge code and
don't care about FT8, the static build needs no gcc and no audio libraries.

---

## 6. Where the durable context lives

- [`CLAUDE.md`](CLAUDE.md) — conventions, idioms, load-bearing invariants.
  Read first.
- [`docs/README.md`](docs/README.md) — the authoritative documentation map
  (which docs are live vs. historical).
- [`docs/install.md`](docs/install.md) — end-user (operator) install, as
  opposed to this developer setup.
- [`RELEASING.md`](RELEASING.md) — tagging and building the shipped RPM.
- [`docs/session-handoff.md`](docs/session-handoff.md) — rolling
  cross-session state: what's done, what's next.
